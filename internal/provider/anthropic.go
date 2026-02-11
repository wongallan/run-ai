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

// anthropicProvider implements Provider for Anthropic's Messages API (/v1/messages).
type anthropicProvider struct {
	endpoint string
	apiKey   string
	model    string
	client   http.Client
}

func (p *anthropicProvider) Name() string { return "anthropic" }

// --- Request/Response types ---

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicToolDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema"`
}

type anthropicRequest struct {
	Model       string             `json:"model"`
	Messages    []anthropicMessage `json:"messages"`
	System      string             `json:"system,omitempty"`
	MaxTokens   int                `json:"max_tokens"`
	Stream      bool               `json:"stream,omitempty"`
	Temperature *float64           `json:"temperature,omitempty"`
	Tools       []anthropicToolDef `json:"tools,omitempty"`
}

type anthropicContentBlock struct {
	Type  string          `json:"type"`
	Text  string          `json:"text,omitempty"`
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
}

type anthropicResponse struct {
	Content []anthropicContentBlock `json:"content"`
	Error   *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// --- Non-streaming ---

func (p *anthropicProvider) Complete(ctx context.Context, req Request) (Response, error) {
	body := p.buildRequest(req, false)
	data, err := json.Marshal(body)
	if err != nil {
		return Response{}, fmt.Errorf("marshal request: %w", err)
	}

	url := strings.TrimRight(p.endpoint, "/") + "/v1/messages"
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(data))
	if err != nil {
		return Response{}, err
	}
	p.setHeaders(httpReq)

	httpResp, err := p.client.Do(httpReq)
	if err != nil {
		return Response{}, fmt.Errorf("anthropic request: %w", err)
	}
	defer httpResp.Body.Close()

	respBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return Response{}, fmt.Errorf("reading response: %w", err)
	}

	if httpResp.StatusCode != http.StatusOK {
		return Response{}, NormalizeHTTPError("anthropic", httpResp.StatusCode, string(respBody))
	}

	var antResp anthropicResponse
	if err := json.Unmarshal(respBody, &antResp); err != nil {
		return Response{}, fmt.Errorf("unmarshal response: %w", err)
	}

	if antResp.Error != nil {
		return Response{}, fmt.Errorf("anthropic error: %s", antResp.Error.Message)
	}

	return p.parseResponse(antResp), nil
}

// --- Streaming ---

func (p *anthropicProvider) Stream(ctx context.Context, req Request) (<-chan StreamEvent, error) {
	body := p.buildRequest(req, true)
	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	url := strings.TrimRight(p.endpoint, "/") + "/v1/messages"
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	p.setHeaders(httpReq)

	httpResp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("anthropic stream request: %w", err)
	}

	if httpResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(httpResp.Body)
		httpResp.Body.Close()
		return nil, NormalizeHTTPError("anthropic", httpResp.StatusCode, string(body))
	}

	ch := make(chan StreamEvent, 16)
	go func() {
		defer close(ch)
		defer httpResp.Body.Close()
		p.readSSE(ctx, httpResp.Body, ch)
	}()
	return ch, nil
}

func (p *anthropicProvider) readSSE(ctx context.Context, body io.Reader, ch chan<- StreamEvent) {
	scanner := bufio.NewScanner(body)
	var currentEventType string

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			ch <- StreamEvent{Error: ctx.Err()}
			return
		default:
		}

		line := scanner.Text()

		if strings.HasPrefix(line, "event: ") {
			currentEventType = strings.TrimPrefix(line, "event: ")
			continue
		}

		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		payload := strings.TrimPrefix(line, "data: ")

		switch currentEventType {
		case "content_block_delta":
			var delta struct {
				Type  string `json:"type"`
				Delta struct {
					Type        string `json:"type"`
					Text        string `json:"text,omitempty"`
					PartialJSON string `json:"partial_json,omitempty"`
				} `json:"delta"`
				Index int `json:"index"`
			}
			if err := json.Unmarshal([]byte(payload), &delta); err == nil {
				if delta.Delta.Type == "text_delta" && delta.Delta.Text != "" {
					ch <- StreamEvent{Text: delta.Delta.Text}
				}
			}

		case "content_block_start":
			var block struct {
				ContentBlock struct {
					Type string `json:"type"`
					ID   string `json:"id"`
					Name string `json:"name"`
				} `json:"content_block"`
			}
			if err := json.Unmarshal([]byte(payload), &block); err == nil {
				// Tool use blocks will be accumulated via deltas.
				_ = block
			}

		case "message_stop":
			ch <- StreamEvent{Done: true}
			return

		case "error":
			var errData struct {
				Error struct {
					Message string `json:"message"`
				} `json:"error"`
			}
			if err := json.Unmarshal([]byte(payload), &errData); err == nil {
				ch <- StreamEvent{Error: fmt.Errorf("anthropic: %s", errData.Error.Message)}
			}
			return
		}
	}

	if err := scanner.Err(); err != nil {
		ch <- StreamEvent{Error: err}
	}
}

// --- Helpers ---

func (p *anthropicProvider) buildRequest(req Request, stream bool) anthropicRequest {
	var system string
	var messages []anthropicMessage

	for _, m := range req.Messages {
		if m.Role == "system" {
			system = m.Content
			continue
		}
		messages = append(messages, anthropicMessage{Role: m.Role, Content: m.Content})
	}

	// Ensure at least one message exists.
	if len(messages) == 0 {
		messages = []anthropicMessage{{Role: "user", Content: ""}}
	}

	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 4096 // Anthropic requires max_tokens.
	}

	antReq := anthropicRequest{
		Model:       p.model,
		Messages:    messages,
		System:      system,
		MaxTokens:   maxTokens,
		Stream:      stream,
		Temperature: req.Temperature,
	}

	if req.Model != "" {
		antReq.Model = req.Model
	}

	for _, t := range req.Tools {
		antReq.Tools = append(antReq.Tools, anthropicToolDef{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: json.RawMessage(t.Parameters),
		})
	}

	return antReq
}

func (p *anthropicProvider) setHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", p.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")
}

func (p *anthropicProvider) parseResponse(resp anthropicResponse) Response {
	var result Response
	for _, block := range resp.Content {
		switch block.Type {
		case "text":
			result.Content += block.Text
		case "tool_use":
			result.ToolCalls = append(result.ToolCalls, ToolCall{
				ID:        block.ID,
				Name:      block.Name,
				Arguments: string(block.Input),
			})
		}
	}
	return result
}
