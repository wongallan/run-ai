package provider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// openAIProvider implements Provider for OpenAI-compatible APIs using the
// Responses API (/v1/responses).
type openAIProvider struct {
	endpoint string
	apiKey   string
	model    string
	client   http.Client
}

func (p *openAIProvider) Name() string { return "openai" }

// --- Request/Response types ---

type openAIInput struct {
	Role       string `json:"role"`
	Content    string `json:"content"`
	ToolCallID string `json:"tool_call_id,omitempty"`
}

type openAITool struct {
	Type     string         `json:"type"`
	Function openAIFunction `json:"function"`
}

type openAIFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

type openAIRequest struct {
	Model       string        `json:"model"`
	Input       []openAIInput `json:"input"`
	Stream      bool          `json:"stream,omitempty"`
	MaxTokens   int           `json:"max_output_tokens,omitempty"`
	Temperature *float64      `json:"temperature,omitempty"`
	Tools       []openAITool  `json:"tools,omitempty"`
}

type openAIResponseOutput struct {
	Type    string `json:"type"`
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content,omitempty"`
	Summary []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"summary,omitempty"`
	ID        string `json:"id,omitempty"`
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
	CallID    string `json:"call_id,omitempty"`
}

type openAIResponse struct {
	ID     string                 `json:"id"`
	Output []openAIResponseOutput `json:"output"`
	Error  *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error,omitempty"`
}

// --- Non-streaming ---

func (p *openAIProvider) Complete(ctx context.Context, req Request) (Response, error) {
	body := p.buildRequest(req, false)
	data, err := json.Marshal(body)
	if err != nil {
		return Response{}, fmt.Errorf("marshal request: %w", err)
	}

	url := strings.TrimRight(p.endpoint, "/") + "/responses"
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(data))
	if err != nil {
		return Response{}, err
	}
	p.setHeaders(httpReq)

	httpResp, err := p.client.Do(httpReq)
	if err != nil {
		return Response{}, fmt.Errorf("openai request: %w", err)
	}
	defer httpResp.Body.Close()

	respBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return Response{}, fmt.Errorf("reading response: %w", err)
	}

	if httpResp.StatusCode != http.StatusOK {
		return Response{}, NormalizeHTTPError("openai", httpResp.StatusCode, string(respBody))
	}

	var oaiResp openAIResponse
	if err := json.Unmarshal(respBody, &oaiResp); err != nil {
		return Response{}, fmt.Errorf("unmarshal response: %w", err)
	}

	if oaiResp.Error != nil {
		return Response{}, fmt.Errorf("openai error: %s", oaiResp.Error.Message)
	}

	return p.parseResponse(oaiResp), nil
}

// --- Streaming ---

func (p *openAIProvider) Stream(ctx context.Context, req Request) (<-chan StreamEvent, error) {
	body := p.buildRequest(req, true)
	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	url := strings.TrimRight(p.endpoint, "/") + "/responses"
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	p.setHeaders(httpReq)

	httpResp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("openai stream request: %w", err)
	}

	if httpResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(httpResp.Body)
		httpResp.Body.Close()
		return nil, NormalizeHTTPError("openai", httpResp.StatusCode, string(body))
	}

	ch := make(chan StreamEvent, 16)
	go func() {
		defer close(ch)
		defer httpResp.Body.Close()
		p.readSSE(ctx, httpResp.Body, ch)
	}()
	return ch, nil
}

func (p *openAIProvider) readSSE(ctx context.Context, body io.Reader, ch chan<- StreamEvent) {
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
			continue // skip malformed events
		}

		switch event.Type {
		case "response.output_text.delta":
			if event.Delta != "" {
				ch <- StreamEvent{Text: event.Delta}
			}
		case "response.reasoning_summary_text.delta":
			if event.Delta != "" {
				ch <- StreamEvent{ReasoningSummary: event.Delta}
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

// --- Helpers ---

func (p *openAIProvider) buildRequest(req Request, stream bool) openAIRequest {
	var input []openAIInput
	for _, m := range req.Messages {
		input = append(input, openAIInput{Role: m.Role, Content: m.Content, ToolCallID: m.ToolCallID})
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

func (p *openAIProvider) setHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.apiKey)
}

func (p *openAIProvider) parseResponse(resp openAIResponse) Response {
	var result Response
	for _, out := range resp.Output {
		switch out.Type {
		case "message":
			for _, c := range out.Content {
				if c.Type == "text" {
					result.Content += c.Text
				}
			}
		case "reasoning", "reasoning_summary":
			for _, s := range out.Summary {
				if s.Type == "text" {
					result.ReasoningSummary += s.Text
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
