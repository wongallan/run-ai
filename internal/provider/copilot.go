package provider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
)

// copilotProvider implements Provider for GitHub Copilot (both github.com and
// Enterprise).  It routes between the Chat API (/chat/completions) and the
// Responses API (/responses) based on the model ID.
type copilotProvider struct {
	baseURL string // e.g. https://api.githubcopilot.com
	token   string // GitHub OAuth access token
	model   string
	client  http.Client
}

func (p *copilotProvider) Name() string { return "github-copilot" }

var gptVersionRe = regexp.MustCompile(`^gpt-(\d+)`)

// shouldUseResponsesAPI returns true for GPT-5+ models except gpt-5-mini.
// All other models (Claude, Gemini, O-series, gpt-5-mini) use the Chat API.
func shouldUseResponsesAPI(modelID string) bool {
	m := gptVersionRe.FindStringSubmatch(modelID)
	if m == nil {
		return false
	}
	v, err := strconv.Atoi(m[1])
	if err != nil {
		return false
	}
	return v >= 5 && !strings.HasPrefix(modelID, "gpt-5-mini")
}

// --- Entry points ---

func (p *copilotProvider) Complete(ctx context.Context, req Request) (Response, error) {
	model := req.Model
	if model == "" {
		model = p.model
	}
	if shouldUseResponsesAPI(model) {
		return p.completeResponses(ctx, req)
	}
	return p.completeChat(ctx, req)
}

func (p *copilotProvider) Stream(ctx context.Context, req Request) (<-chan StreamEvent, error) {
	model := req.Model
	if model == "" {
		model = p.model
	}
	if shouldUseResponsesAPI(model) {
		return p.streamResponses(ctx, req)
	}
	return p.streamChat(ctx, req)
}

// =========================================================================
// Chat API  (/chat/completions)
// =========================================================================

type copilotChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type copilotChatTool struct {
	Type     string              `json:"type"`
	Function copilotChatFunction `json:"function"`
}

type copilotChatFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

type copilotChatRequest struct {
	Model       string               `json:"model"`
	Messages    []copilotChatMessage `json:"messages"`
	Stream      bool                 `json:"stream,omitempty"`
	MaxTokens   int                  `json:"max_tokens,omitempty"`
	Temperature *float64             `json:"temperature,omitempty"`
	Tools       []copilotChatTool    `json:"tools,omitempty"`
}

type copilotChatChoice struct {
	Message struct {
		Role      string `json:"role"`
		Content   string `json:"content"`
		ToolCalls []struct {
			ID       string `json:"id"`
			Type     string `json:"type"`
			Function struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			} `json:"function"`
		} `json:"tool_calls"`
	} `json:"message"`
	FinishReason string `json:"finish_reason"`
}

type copilotChatResponse struct {
	ID      string              `json:"id"`
	Choices []copilotChatChoice `json:"choices"`
	Error   *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error,omitempty"`
}

// --- Chat Complete ---

func (p *copilotProvider) completeChat(ctx context.Context, req Request) (Response, error) {
	body := p.buildChatRequest(req, false)
	data, err := json.Marshal(body)
	if err != nil {
		return Response{}, fmt.Errorf("marshal request: %w", err)
	}

	apiURL := strings.TrimRight(p.baseURL, "/") + "/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewReader(data))
	if err != nil {
		return Response{}, err
	}
	p.setHeaders(httpReq)

	httpResp, err := p.client.Do(httpReq)
	if err != nil {
		return Response{}, fmt.Errorf("copilot chat request: %w", err)
	}
	defer httpResp.Body.Close()

	respBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return Response{}, fmt.Errorf("reading response: %w", err)
	}

	if httpResp.StatusCode != http.StatusOK {
		return Response{}, normalizeCopilotError(httpResp.StatusCode, string(respBody))
	}

	var chatResp copilotChatResponse
	if err := json.Unmarshal(respBody, &chatResp); err != nil {
		return Response{}, fmt.Errorf("unmarshal response: %w", err)
	}

	if chatResp.Error != nil {
		return Response{}, fmt.Errorf("copilot error: %s", chatResp.Error.Message)
	}

	return p.parseChatResponse(chatResp), nil
}

// --- Chat Stream ---

func (p *copilotProvider) streamChat(ctx context.Context, req Request) (<-chan StreamEvent, error) {
	body := p.buildChatRequest(req, true)
	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	apiURL := strings.TrimRight(p.baseURL, "/") + "/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	p.setHeaders(httpReq)

	httpResp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("copilot chat stream: %w", err)
	}

	if httpResp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(httpResp.Body)
		httpResp.Body.Close()
		return nil, normalizeCopilotError(httpResp.StatusCode, string(errBody))
	}

	ch := make(chan StreamEvent, 16)
	go func() {
		defer close(ch)
		defer httpResp.Body.Close()
		p.readChatSSE(ctx, httpResp.Body, ch)
	}()
	return ch, nil
}

type chatToolAcc struct {
	id   string
	name string
	args string
}

func (p *copilotProvider) readChatSSE(ctx context.Context, body io.Reader, ch chan<- StreamEvent) {
	scanner := bufio.NewScanner(body)
	toolCalls := map[int]*chatToolAcc{}

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			ch <- StreamEvent{Error: ctx.Err()}
			return
		default:
		}

		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		payload := strings.TrimPrefix(line, "data: ")
		if payload == "[DONE]" {
			// Flush any remaining tool calls.
			p.flushToolCalls(toolCalls, ch)
			ch <- StreamEvent{Done: true}
			return
		}

		var chunk struct {
			Choices []struct {
				Delta struct {
					Content   string `json:"content"`
					ToolCalls []struct {
						Index    int    `json:"index"`
						ID       string `json:"id"`
						Function struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						} `json:"function"`
					} `json:"tool_calls"`
				} `json:"delta"`
				FinishReason *string `json:"finish_reason"`
			} `json:"choices"`
		}
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			continue
		}
		if len(chunk.Choices) == 0 {
			continue
		}

		delta := chunk.Choices[0].Delta

		if delta.Content != "" {
			ch <- StreamEvent{Text: delta.Content}
		}

		for _, tc := range delta.ToolCalls {
			acc, ok := toolCalls[tc.Index]
			if !ok {
				acc = &chatToolAcc{}
				toolCalls[tc.Index] = acc
			}
			if tc.ID != "" {
				acc.id = tc.ID
			}
			if tc.Function.Name != "" {
				acc.name = tc.Function.Name
			}
			acc.args += tc.Function.Arguments
		}

		// Emit tool calls when finish_reason indicates completion.
		fr := chunk.Choices[0].FinishReason
		if fr != nil && (*fr == "tool_calls" || *fr == "stop") && len(toolCalls) > 0 {
			p.flushToolCalls(toolCalls, ch)
			toolCalls = map[int]*chatToolAcc{}
		}
	}

	if err := scanner.Err(); err != nil {
		ch <- StreamEvent{Error: err}
	}
}

func (p *copilotProvider) flushToolCalls(acc map[int]*chatToolAcc, ch chan<- StreamEvent) {
	for _, tc := range acc {
		if tc.name != "" {
			ch <- StreamEvent{ToolCalls: []ToolCall{{
				ID:        tc.id,
				Name:      tc.name,
				Arguments: tc.args,
			}}}
		}
	}
}

// --- Chat helpers ---

func (p *copilotProvider) buildChatRequest(req Request, stream bool) copilotChatRequest {
	var messages []copilotChatMessage
	for _, m := range req.Messages {
		messages = append(messages, copilotChatMessage{
			Role:    m.Role,
			Content: m.Content,
		})
	}

	chatReq := copilotChatRequest{
		Model:       p.model,
		Messages:    messages,
		Stream:      stream,
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
	}
	if req.Model != "" {
		chatReq.Model = req.Model
	}

	for _, t := range req.Tools {
		chatReq.Tools = append(chatReq.Tools, copilotChatTool{
			Type: "function",
			Function: copilotChatFunction{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  json.RawMessage(t.Parameters),
			},
		})
	}
	return chatReq
}

func (p *copilotProvider) parseChatResponse(resp copilotChatResponse) Response {
	var result Response
	if len(resp.Choices) == 0 {
		return result
	}
	choice := resp.Choices[0]
	result.Content = choice.Message.Content
	for _, tc := range choice.Message.ToolCalls {
		result.ToolCalls = append(result.ToolCalls, ToolCall{
			ID:        tc.ID,
			Name:      tc.Function.Name,
			Arguments: tc.Function.Arguments,
		})
	}
	return result
}

// =========================================================================
// Responses API  (/responses)
// =========================================================================
//
// Reuses the same request/response types as the openAIProvider since the
// Copilot Responses API is OpenAI-compatible.

func (p *copilotProvider) completeResponses(ctx context.Context, req Request) (Response, error) {
	body := p.buildResponsesRequest(req, false)
	data, err := json.Marshal(body)
	if err != nil {
		return Response{}, fmt.Errorf("marshal request: %w", err)
	}

	apiURL := strings.TrimRight(p.baseURL, "/") + "/responses"
	httpReq, err := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewReader(data))
	if err != nil {
		return Response{}, err
	}
	p.setHeaders(httpReq)

	httpResp, err := p.client.Do(httpReq)
	if err != nil {
		return Response{}, fmt.Errorf("copilot responses request: %w", err)
	}
	defer httpResp.Body.Close()

	respBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return Response{}, fmt.Errorf("reading response: %w", err)
	}

	if httpResp.StatusCode != http.StatusOK {
		return Response{}, normalizeCopilotError(httpResp.StatusCode, string(respBody))
	}

	var oaiResp openAIResponse
	if err := json.Unmarshal(respBody, &oaiResp); err != nil {
		return Response{}, fmt.Errorf("unmarshal response: %w", err)
	}
	if oaiResp.Error != nil {
		return Response{}, fmt.Errorf("copilot error: %s", oaiResp.Error.Message)
	}

	return p.parseResponsesOutput(oaiResp), nil
}

func (p *copilotProvider) streamResponses(ctx context.Context, req Request) (<-chan StreamEvent, error) {
	body := p.buildResponsesRequest(req, true)
	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	apiURL := strings.TrimRight(p.baseURL, "/") + "/responses"
	httpReq, err := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	p.setHeaders(httpReq)

	httpResp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("copilot responses stream: %w", err)
	}

	if httpResp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(httpResp.Body)
		httpResp.Body.Close()
		return nil, normalizeCopilotError(httpResp.StatusCode, string(errBody))
	}

	ch := make(chan StreamEvent, 16)
	go func() {
		defer close(ch)
		defer httpResp.Body.Close()
		p.readResponsesSSE(ctx, httpResp.Body, ch)
	}()
	return ch, nil
}

// readResponsesSSE parses Responses API SSE events (same wire-format as
// the standard OpenAI Responses API).
func (p *copilotProvider) readResponsesSSE(ctx context.Context, body io.Reader, ch chan<- StreamEvent) {
	scanner := bufio.NewScanner(body)
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			ch <- StreamEvent{Error: ctx.Err()}
			return
		default:
		}

		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		payload := strings.TrimPrefix(line, "data: ")
		if payload == "[DONE]" {
			ch <- StreamEvent{Done: true}
			return
		}

		var event struct {
			Type  string `json:"type"`
			Delta string `json:"delta"`
			Item  *struct {
				Type      string `json:"type"`
				ID        string `json:"id"`
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
				CallID    string `json:"call_id"`
			} `json:"item,omitempty"`
		}
		if err := json.Unmarshal([]byte(payload), &event); err != nil {
			continue
		}

		switch event.Type {
		case "response.output_text.delta":
			if event.Delta != "" {
				ch <- StreamEvent{Text: event.Delta}
			}
		case "response.function_call_arguments.done":
			if event.Item != nil {
				ch <- StreamEvent{ToolCalls: []ToolCall{{
					ID:        event.Item.CallID,
					Name:      event.Item.Name,
					Arguments: event.Item.Arguments,
				}}}
			}
		case "response.completed":
			ch <- StreamEvent{Done: true}
			return
		}
	}
	if err := scanner.Err(); err != nil {
		ch <- StreamEvent{Error: err}
	}
}

// --- Responses helpers ---

func (p *copilotProvider) buildResponsesRequest(req Request, stream bool) openAIRequest {
	var input []openAIInput
	for _, m := range req.Messages {
		input = append(input, openAIInput{Role: m.Role, Content: m.Content})
	}

	oaiReq := openAIRequest{
		Model:       p.model,
		Input:       input,
		Stream:      stream,
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
	}
	if req.Model != "" {
		oaiReq.Model = req.Model
	}

	for _, t := range req.Tools {
		oaiReq.Tools = append(oaiReq.Tools, openAITool{
			Type: "function",
			Function: openAIFunction{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  json.RawMessage(t.Parameters),
			},
		})
	}
	return oaiReq
}

func (p *copilotProvider) parseResponsesOutput(resp openAIResponse) Response {
	var result Response
	for _, out := range resp.Output {
		switch out.Type {
		case "message":
			for _, c := range out.Content {
				if c.Type == "text" {
					result.Content += c.Text
				}
			}
		case "function_call":
			result.ToolCalls = append(result.ToolCalls, ToolCall{
				ID:        out.CallID,
				Name:      out.Name,
				Arguments: out.Arguments,
			})
		}
	}
	return result
}

// =========================================================================
// Headers and error handling
// =========================================================================

func (p *copilotProvider) setHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.token)
	req.Header.Set("User-Agent", "rai/0.1.0")
	req.Header.Set("Openai-Intent", "conversation-edits")
	req.Header.Set("x-initiator", "user")
}

// normalizeCopilotError wraps NormalizeHTTPError with Copilot-specific
// guidance for 401 and 403 responses.
func normalizeCopilotError(statusCode int, body string) *ProviderError {
	pe := NormalizeHTTPError("github-copilot", statusCode, body)

	switch statusCode {
	case 401:
		pe.Guidance = "re-authenticate with 'rai copilot-login' or check your GitHub token"
	case 403:
		if strings.Contains(body, "not supported") {
			pe.Message = "model not available"
			pe.Guidance = "enable the model at https://github.com/settings/copilot/features"
		} else {
			pe.Guidance = "re-authenticate with 'rai copilot-login' or verify your Copilot subscription"
		}
	}

	return pe
}
