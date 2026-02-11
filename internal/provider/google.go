package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// googleProvider implements Provider for Google's Gemini generateContent API.
type googleProvider struct {
	endpoint string
	apiKey   string
	model    string
	client   http.Client
}

func (p *googleProvider) Name() string { return "google" }

// --- Request/Response types ---

type geminiContent struct {
	Role  string       `json:"role,omitempty"`
	Parts []geminiPart `json:"parts"`
}

type geminiPart struct {
	Text         string              `json:"text,omitempty"`
	FunctionCall *geminiFunctionCall `json:"functionCall,omitempty"`
}

type geminiFunctionCall struct {
	Name string          `json:"name"`
	Args json.RawMessage `json:"args"`
}

type geminiToolDecl struct {
	FunctionDeclarations []geminiFuncDecl `json:"functionDeclarations,omitempty"`
}

type geminiFuncDecl struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

type geminiRequest struct {
	Contents          []geminiContent  `json:"contents"`
	SystemInstruction *geminiContent   `json:"systemInstruction,omitempty"`
	Tools             []geminiToolDecl `json:"tools,omitempty"`
	GenerationConfig  *geminiGenConfig `json:"generationConfig,omitempty"`
}

type geminiGenConfig struct {
	MaxOutputTokens int      `json:"maxOutputTokens,omitempty"`
	Temperature     *float64 `json:"temperature,omitempty"`
}

type geminiCandidate struct {
	Content struct {
		Parts []geminiPart `json:"parts"`
	} `json:"content"`
}

type geminiResponse struct {
	Candidates []geminiCandidate `json:"candidates"`
	Error      *struct {
		Message string `json:"message"`
		Code    int    `json:"code"`
	} `json:"error,omitempty"`
}

// --- Non-streaming ---

func (p *googleProvider) Complete(ctx context.Context, req Request) (Response, error) {
	body := p.buildRequest(req)
	data, err := json.Marshal(body)
	if err != nil {
		return Response{}, fmt.Errorf("marshal request: %w", err)
	}

	url := p.buildURL(false)
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(data))
	if err != nil {
		return Response{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	httpResp, err := p.client.Do(httpReq)
	if err != nil {
		return Response{}, fmt.Errorf("google request: %w", err)
	}
	defer httpResp.Body.Close()

	respBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return Response{}, fmt.Errorf("reading response: %w", err)
	}

	if httpResp.StatusCode != http.StatusOK {
		return Response{}, NormalizeHTTPError("google", httpResp.StatusCode, string(respBody))
	}

	var gemResp geminiResponse
	if err := json.Unmarshal(respBody, &gemResp); err != nil {
		return Response{}, fmt.Errorf("unmarshal response: %w", err)
	}

	if gemResp.Error != nil {
		return Response{}, fmt.Errorf("google error: %s", gemResp.Error.Message)
	}

	return p.parseResponse(gemResp), nil
}

// --- Streaming ---

// Stream implements streaming via Gemini's streamGenerateContent endpoint.
// Gemini returns newline-delimited JSON array fragments.
func (p *googleProvider) Stream(ctx context.Context, req Request) (<-chan StreamEvent, error) {
	body := p.buildRequest(req)
	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	url := p.buildURL(true)
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	httpResp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("google stream request: %w", err)
	}

	if httpResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(httpResp.Body)
		httpResp.Body.Close()
		return nil, NormalizeHTTPError("google", httpResp.StatusCode, string(body))
	}

	ch := make(chan StreamEvent, 16)
	go func() {
		defer close(ch)
		defer httpResp.Body.Close()
		p.readStream(ctx, httpResp.Body, ch)
	}()
	return ch, nil
}

// readStream parses Gemini's streaming format.
// Gemini streams a JSON array where each element is a generateContent response.
func (p *googleProvider) readStream(ctx context.Context, body io.Reader, ch chan<- StreamEvent) {
	decoder := json.NewDecoder(body)

	// Expect opening bracket of array.
	tok, err := decoder.Token()
	if err != nil {
		ch <- StreamEvent{Error: fmt.Errorf("reading stream start: %w", err)}
		return
	}
	if delim, ok := tok.(json.Delim); !ok || delim != '[' {
		// Not an array — try to parse as a single response.
		ch <- StreamEvent{Error: fmt.Errorf("unexpected stream format")}
		return
	}

	for decoder.More() {
		select {
		case <-ctx.Done():
			ch <- StreamEvent{Error: ctx.Err()}
			return
		default:
		}

		var chunk geminiResponse
		if err := decoder.Decode(&chunk); err != nil {
			ch <- StreamEvent{Error: fmt.Errorf("decoding stream chunk: %w", err)}
			return
		}

		if chunk.Error != nil {
			ch <- StreamEvent{Error: fmt.Errorf("google error: %s", chunk.Error.Message)}
			return
		}

		resp := p.parseResponse(chunk)
		if resp.Content != "" {
			ch <- StreamEvent{Text: resp.Content}
		}
		for _, tc := range resp.ToolCalls {
			ch <- StreamEvent{ToolCalls: []ToolCall{tc}}
		}
	}

	ch <- StreamEvent{Done: true}
}

// --- Helpers ---

func (p *googleProvider) buildRequest(req Request) geminiRequest {
	var system *geminiContent
	var contents []geminiContent

	for _, m := range req.Messages {
		if m.Role == "system" {
			system = &geminiContent{
				Parts: []geminiPart{{Text: m.Content}},
			}
			continue
		}
		role := m.Role
		if role == "assistant" {
			role = "model" // Gemini uses "model" instead of "assistant"
		}
		contents = append(contents, geminiContent{
			Role:  role,
			Parts: []geminiPart{{Text: m.Content}},
		})
	}

	gemReq := geminiRequest{
		Contents:          contents,
		SystemInstruction: system,
	}

	if req.MaxTokens > 0 || req.Temperature != nil {
		gemReq.GenerationConfig = &geminiGenConfig{
			MaxOutputTokens: req.MaxTokens,
			Temperature:     req.Temperature,
		}
	}

	if len(req.Tools) > 0 {
		var decls []geminiFuncDecl
		for _, t := range req.Tools {
			decls = append(decls, geminiFuncDecl{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  json.RawMessage(t.Parameters),
			})
		}
		gemReq.Tools = []geminiToolDecl{{FunctionDeclarations: decls}}
	}

	return gemReq
}

func (p *googleProvider) buildURL(stream bool) string {
	base := strings.TrimRight(p.endpoint, "/")
	model := p.model

	if stream {
		return fmt.Sprintf("%s/v1beta/models/%s:streamGenerateContent?alt=sse&key=%s", base, model, p.apiKey)
	}
	return fmt.Sprintf("%s/v1beta/models/%s:generateContent?key=%s", base, model, p.apiKey)
}

func (p *googleProvider) parseResponse(resp geminiResponse) Response {
	var result Response
	for _, cand := range resp.Candidates {
		for _, part := range cand.Content.Parts {
			if part.Text != "" {
				result.Content += part.Text
			}
			if part.FunctionCall != nil {
				result.ToolCalls = append(result.ToolCalls, ToolCall{
					Name:      part.FunctionCall.Name,
					Arguments: string(part.FunctionCall.Args),
				})
			}
		}
	}
	return result
}
