package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// --- Resolve tests ---

func TestResolveNoProvider(t *testing.T) {
	_, err := Resolve(map[string]string{})
	if err != ErrNoProvider {
		t.Fatalf("expected ErrNoProvider, got %v", err)
	}
}

func TestResolveNoAPIKey(t *testing.T) {
	_, err := Resolve(map[string]string{"endpoint": "https://api.openai.com/v1"})
	if err != ErrAuthRequired {
		t.Fatalf("expected ErrAuthRequired, got %v", err)
	}
}

func TestResolveNoModel(t *testing.T) {
	_, err := Resolve(map[string]string{
		"endpoint": "https://api.openai.com/v1",
		"api-key":  "sk-test",
	})
	if err != ErrModelRequired {
		t.Fatalf("expected ErrModelRequired, got %v", err)
	}
}

func TestResolveOpenAI(t *testing.T) {
	p, err := Resolve(map[string]string{
		"endpoint": "https://api.openai.com/v1",
		"api-key":  "sk-test",
		"model":    "gpt-4",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Name() != "openai" {
		t.Fatalf("name = %q, want openai", p.Name())
	}
}

func TestResolveAnthropic(t *testing.T) {
	p, err := Resolve(map[string]string{
		"endpoint": "https://api.anthropic.com",
		"api-key":  "sk-ant-test",
		"model":    "claude-3-opus",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Name() != "anthropic" {
		t.Fatalf("name = %q, want anthropic", p.Name())
	}
}

func TestResolveGoogle(t *testing.T) {
	p, err := Resolve(map[string]string{
		"endpoint": "https://generativelanguage.googleapis.com",
		"api-key":  "AIza-test",
		"model":    "gemini-pro",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Name() != "google" {
		t.Fatalf("name = %q, want google", p.Name())
	}
}

func TestResolveGitHubCopilot(t *testing.T) {
	p, err := Resolve(map[string]string{
		"provider": "github-copilot",
		"api-key":  "gho_test-token",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Name() != "github-copilot" {
		t.Fatalf("name = %q, want github-copilot", p.Name())
	}
}

func TestResolveGitHubCopilotNoToken(t *testing.T) {
	_, err := Resolve(map[string]string{"provider": "github-copilot"})
	if err == nil {
		t.Fatal("expected error for missing token")
	}
	if !strings.Contains(err.Error(), "token required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestResolveGitHubCopilotEnterprise(t *testing.T) {
	p, err := Resolve(map[string]string{
		"provider":       "github-copilot-enterprise",
		"api-key":        "gho_test",
		"enterprise-url": "company.ghe.com",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Name() != "github-copilot" {
		t.Fatalf("name = %q", p.Name())
	}
}

func TestResolveGitHubCopilotDefaultModel(t *testing.T) {
	p, err := Resolve(map[string]string{
		"provider": "github-copilot",
		"api-key":  "gho_test",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	cp := p.(*copilotProvider)
	if cp.model != "gpt-5-mini" {
		t.Fatalf("default model = %q, want gpt-5-mini", cp.model)
	}
}

func TestResolveAPIKeyUnderscore(t *testing.T) {
	p, err := Resolve(map[string]string{
		"endpoint": "https://api.openai.com/v1",
		"api_key":  "sk-test",
		"model":    "gpt-4",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Name() != "openai" {
		t.Fatalf("name = %q", p.Name())
	}
}

// --- OpenAI Complete test with mock server ---

func TestOpenAIComplete(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Fatalf("missing auth header")
		}

		body, _ := io.ReadAll(r.Body)
		var req openAIRequest
		json.Unmarshal(body, &req)
		if req.Model != "gpt-4" {
			t.Fatalf("model = %q", req.Model)
		}

		resp := openAIResponse{
			ID: "resp-1",
			Output: []openAIResponseOutput{
				{
					Type: "reasoning",
					Summary: []struct {
						Type string `json:"type"`
						Text string `json:"text"`
					}{{Type: "text", Text: "Paris is the capital."}},
				},
				{
					Type: "message",
					Content: []struct {
						Type string `json:"type"`
						Text string `json:"text"`
					}{{Type: "text", Text: "Paris is the capital of France."}},
				},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	p, _ := Resolve(map[string]string{
		"endpoint": srv.URL,
		"api-key":  "test-key",
		"model":    "gpt-4",
	})

	resp, err := p.Complete(context.Background(), Request{
		Messages: []Message{{Role: "user", Content: "What is the capital of France?"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(resp.Content, "Paris") {
		t.Fatalf("expected Paris in response, got %q", resp.Content)
	}
	if !strings.Contains(resp.ReasoningSummary, "Paris is the capital") {
		t.Fatalf("expected reasoning summary, got %q", resp.ReasoningSummary)
	}
}

// --- OpenAI Stream test ---

func TestOpenAIStream(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)

		events := []string{
			`{"type":"response.reasoning_summary_text.delta","delta":"Reasoning summary."}`,
			`{"type":"response.output_text.delta","delta":"Hello"}`,
			`{"type":"response.output_text.delta","delta":" world"}`,
			`{"type":"response.completed"}`,
		}
		for _, e := range events {
			fmt.Fprintf(w, "data: %s\n\n", e)
			flusher.Flush()
		}
	}))
	defer srv.Close()

	p, _ := Resolve(map[string]string{
		"endpoint": srv.URL,
		"api-key":  "test-key",
		"model":    "gpt-4",
	})

	ch, err := p.Stream(context.Background(), Request{
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var buf bytes.Buffer
	resp, err := CollectStream(ch, &buf)
	if err != nil {
		t.Fatalf("collect error: %v", err)
	}
	if resp.Content != "Hello world" {
		t.Fatalf("content = %q, want 'Hello world'", resp.Content)
	}
	if buf.String() != "Hello world" {
		t.Fatalf("written = %q", buf.String())
	}
	if resp.ReasoningSummary != "Reasoning summary." {
		t.Fatalf("reasoning = %q", resp.ReasoningSummary)
	}
}

// --- Anthropic Complete test ---

func TestAnthropicComplete(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/v1/messages") {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("x-api-key") != "ant-key" {
			t.Fatalf("missing x-api-key header")
		}
		if r.Header.Get("anthropic-version") == "" {
			t.Fatalf("missing anthropic-version header")
		}

		resp := anthropicResponse{
			Content: []anthropicContentBlock{
				{Type: "text", Text: "I'm Claude."},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	p, _ := Resolve(map[string]string{
		"endpoint": srv.URL + "/anthropic", // heuristic match
		"api-key":  "ant-key",
		"model":    "claude-3",
	})

	resp, err := p.Complete(context.Background(), Request{
		Messages: []Message{
			{Role: "system", Content: "You are helpful."},
			{Role: "user", Content: "Who are you?"},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(resp.Content, "Claude") {
		t.Fatalf("expected Claude in response, got %q", resp.Content)
	}
}

// --- Anthropic Stream test ---

func TestAnthropicStream(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)

		lines := []string{
			"event: content_block_delta",
			`data: {"type":"content_block_delta","delta":{"type":"text_delta","text":"Hi"}}`,
			"",
			"event: content_block_delta",
			`data: {"type":"content_block_delta","delta":{"type":"text_delta","text":" there"}}`,
			"",
			"event: message_stop",
			`data: {}`,
			"",
		}
		for _, l := range lines {
			fmt.Fprintln(w, l)
			flusher.Flush()
		}
	}))
	defer srv.Close()

	p, _ := Resolve(map[string]string{
		"endpoint": srv.URL + "/anthropic",
		"api-key":  "ant-key",
		"model":    "claude-3",
	})

	ch, err := p.Stream(context.Background(), Request{
		Messages: []Message{{Role: "user", Content: "hello"}},
	})
	if err != nil {
		t.Fatalf("stream error: %v", err)
	}

	resp, err := CollectStream(ch, nil)
	if err != nil {
		t.Fatalf("collect error: %v", err)
	}
	if resp.Content != "Hi there" {
		t.Fatalf("content = %q, want 'Hi there'", resp.Content)
	}
}

// --- Google Complete test ---

func TestGoogleComplete(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "generateContent") {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.URL.Query().Get("key") != "google-key" {
			t.Fatalf("missing API key in URL")
		}

		resp := geminiResponse{
			Candidates: []geminiCandidate{{
				Content: struct {
					Parts []geminiPart `json:"parts"`
				}{
					Parts: []geminiPart{{Text: "I'm Gemini."}},
				},
			}},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	p, _ := Resolve(map[string]string{
		"endpoint": srv.URL + "/generativelanguage.googleapis.com",
		"api-key":  "google-key",
		"model":    "gemini-pro",
	})

	resp, err := p.Complete(context.Background(), Request{
		Messages: []Message{{Role: "user", Content: "Who are you?"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(resp.Content, "Gemini") {
		t.Fatalf("expected Gemini in response, got %q", resp.Content)
	}
}

// --- Google Stream test ---

func TestGoogleStream(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "streamGenerateContent") {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")

		// Gemini streams as JSON array.
		chunks := []geminiResponse{
			{Candidates: []geminiCandidate{{
				Content: struct {
					Parts []geminiPart `json:"parts"`
				}{
					Parts: []geminiPart{{Text: "Hello"}},
				},
			}}},
			{Candidates: []geminiCandidate{{
				Content: struct {
					Parts []geminiPart `json:"parts"`
				}{
					Parts: []geminiPart{{Text: " Gemini"}},
				},
			}}},
		}

		w.Write([]byte("["))
		for i, c := range chunks {
			if i > 0 {
				w.Write([]byte(","))
			}
			data, _ := json.Marshal(c)
			w.Write(data)
		}
		w.Write([]byte("]"))
	}))
	defer srv.Close()

	p, _ := Resolve(map[string]string{
		"endpoint": srv.URL + "/generativelanguage.googleapis.com",
		"api-key":  "google-key",
		"model":    "gemini-pro",
	})

	ch, err := p.Stream(context.Background(), Request{
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("stream error: %v", err)
	}

	resp, err := CollectStream(ch, nil)
	if err != nil {
		t.Fatalf("collect error: %v", err)
	}
	if resp.Content != "Hello Gemini" {
		t.Fatalf("content = %q, want 'Hello Gemini'", resp.Content)
	}
}

// --- Tool call tests ---

func TestOpenAIToolCall(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req openAIRequest
		json.Unmarshal(body, &req)

		if len(req.Tools) != 1 || req.Tools[0].Function.Name != "get_weather" {
			t.Fatalf("expected get_weather tool, got %+v", req.Tools)
		}

		resp := openAIResponse{
			Output: []openAIResponseOutput{{
				Type:      "function_call",
				Name:      "get_weather",
				CallID:    "call-1",
				Arguments: `{"city":"Paris"}`,
			}},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	p, _ := Resolve(map[string]string{
		"endpoint": srv.URL,
		"api-key":  "test-key",
		"model":    "gpt-4",
	})

	resp, err := p.Complete(context.Background(), Request{
		Messages: []Message{{Role: "user", Content: "weather in Paris?"}},
		Tools: []ToolDef{{
			Name:        "get_weather",
			Description: "Get weather",
			Parameters:  `{"type":"object","properties":{"city":{"type":"string"}}}`,
		}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(resp.ToolCalls))
	}
	if resp.ToolCalls[0].Name != "get_weather" {
		t.Fatalf("tool name = %q", resp.ToolCalls[0].Name)
	}
}

// --- CollectStream test ---

func TestCollectStreamWithError(t *testing.T) {
	ch := make(chan StreamEvent, 2)
	ch <- StreamEvent{Text: "partial"}
	ch <- StreamEvent{Error: fmt.Errorf("connection lost")}
	close(ch)

	_, err := CollectStream(ch, nil)
	if err == nil || !strings.Contains(err.Error(), "connection lost") {
		t.Fatalf("expected connection lost error, got %v", err)
	}
}

// --- Error handling tests ---

func TestOpenAIHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"error":{"message":"rate limited"}}`))
	}))
	defer srv.Close()

	p, _ := Resolve(map[string]string{
		"endpoint": srv.URL,
		"api-key":  "test-key",
		"model":    "gpt-4",
	})

	_, err := p.Complete(context.Background(), Request{
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error")
	}

	// Should be a ProviderError with actionable guidance.
	pe, ok := err.(*ProviderError)
	if !ok {
		t.Fatalf("expected ProviderError, got %T: %v", err, err)
	}
	if pe.StatusCode != 429 {
		t.Fatalf("status = %d, want 429", pe.StatusCode)
	}
	if !strings.Contains(pe.Guidance, "wait") {
		t.Fatalf("expected rate limit guidance, got %q", pe.Guidance)
	}
}

func TestNormalizeHTTPError401(t *testing.T) {
	pe := NormalizeHTTPError("openai", 401, "Unauthorized")
	if pe.StatusCode != 401 {
		t.Fatalf("status = %d", pe.StatusCode)
	}
	if !strings.Contains(pe.Message, "authentication") {
		t.Fatalf("message = %q", pe.Message)
	}
	if !strings.Contains(pe.Guidance, "api-key") {
		t.Fatalf("guidance = %q", pe.Guidance)
	}
}

func TestNormalizeHTTPError403(t *testing.T) {
	pe := NormalizeHTTPError("anthropic", 403, "Forbidden")
	if !strings.Contains(pe.Message, "access denied") {
		t.Fatalf("message = %q", pe.Message)
	}
}

func TestNormalizeHTTPError404(t *testing.T) {
	pe := NormalizeHTTPError("google", 404, "Not found")
	if !strings.Contains(pe.Message, "not found") {
		t.Fatalf("message = %q", pe.Message)
	}
	if !strings.Contains(pe.Guidance, "endpoint") {
		t.Fatalf("guidance = %q", pe.Guidance)
	}
}

func TestNormalizeHTTPError500(t *testing.T) {
	pe := NormalizeHTTPError("openai", 502, "Bad Gateway")
	if !strings.Contains(pe.Message, "server error") {
		t.Fatalf("message = %q", pe.Message)
	}
	if !strings.Contains(pe.Guidance, "try again") {
		t.Fatalf("guidance = %q", pe.Guidance)
	}
}

func TestNormalizeHTTPErrorUnknown(t *testing.T) {
	pe := NormalizeHTTPError("test", 418, "I'm a teapot")
	if pe.StatusCode != 418 {
		t.Fatalf("status = %d", pe.StatusCode)
	}
	if !strings.Contains(pe.Message, "I'm a teapot") {
		t.Fatalf("message = %q", pe.Message)
	}
}

func TestProviderErrorString(t *testing.T) {
	pe := &ProviderError{
		Provider: "openai",
		Message:  "auth failed",
		Guidance: "check api-key",
	}
	s := pe.Error()
	if !strings.Contains(s, "openai: auth failed") {
		t.Fatalf("error = %q", s)
	}
	if !strings.Contains(s, "check api-key") {
		t.Fatalf("error = %q", s)
	}
}

// --- shouldUseResponsesAPI tests ---

func TestShouldUseResponsesAPI(t *testing.T) {
	tests := []struct {
		model string
		want  bool
	}{
		{"gpt-5", true},
		{"gpt-5.2", true},
		{"gpt-5.2-codex", true},
		{"gpt-5-nano", true},
		{"gpt-6", true},
		{"gpt-5-mini", false}, // explicitly excluded
		{"gpt-4", false},      // too old
		{"claude-sonnet-4-5", false},
		{"o3", false},
		{"gemini-3-flash-preview", false},
		{"", false},
	}
	for _, tt := range tests {
		got := shouldUseResponsesAPI(tt.model)
		if got != tt.want {
			t.Errorf("shouldUseResponsesAPI(%q) = %v, want %v", tt.model, got, tt.want)
		}
	}
}

// --- NormalizeDomain tests ---

func TestNormalizeDomain(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"company.ghe.com", "company.ghe.com"},
		{"https://company.ghe.com", "company.ghe.com"},
		{"https://company.ghe.com/", "company.ghe.com"},
		{"http://company.ghe.com:443/path", "company.ghe.com"},
		{"github.com", "github.com"},
		{"", ""},
	}
	for _, tt := range tests {
		got := NormalizeDomain(tt.input)
		if got != tt.want {
			t.Errorf("NormalizeDomain(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

// --- CopilotBaseURL tests ---

func TestCopilotBaseURL(t *testing.T) {
	tests := []struct {
		enterpriseURL string
		want          string
	}{
		{"", "https://api.githubcopilot.com"},
		{"github.com", "https://api.githubcopilot.com"},
		{"company.ghe.com", "https://copilot-api.company.ghe.com"},
		{"https://corp.example.org", "https://copilot-api.corp.example.org"},
	}
	for _, tt := range tests {
		got := CopilotBaseURL(tt.enterpriseURL)
		if got != tt.want {
			t.Errorf("CopilotBaseURL(%q) = %q, want %q", tt.enterpriseURL, got, tt.want)
		}
	}
}

// --- Copilot Chat Complete test ---

func TestCopilotChatComplete(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer cop-token" {
			t.Fatalf("bad auth header: %s", r.Header.Get("Authorization"))
		}
		if r.Header.Get("Openai-Intent") != "conversation-edits" {
			t.Fatalf("missing Openai-Intent header")
		}
		if r.Header.Get("x-initiator") != "user" {
			t.Fatalf("missing x-initiator header")
		}

		resp := copilotChatResponse{
			ID: "chatcmpl-1",
			Choices: []copilotChatChoice{{
				Message: struct {
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
				}{
					Role:    "assistant",
					Content: "Hello from Copilot!",
				},
				FinishReason: "stop",
			}},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	p, err := Resolve(map[string]string{
		"provider": "github-copilot",
		"api-key":  "cop-token",
		"model":    "claude-sonnet-4-5", // Chat API model
		"endpoint": srv.URL,             // not used by copilot but harmless
	})
	if err != nil {
		t.Fatalf("resolve error: %v", err)
	}

	// Override baseURL to point to test server.
	cp := p.(*copilotProvider)
	cp.baseURL = srv.URL

	resp, err := cp.Complete(context.Background(), Request{
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("complete error: %v", err)
	}
	if !strings.Contains(resp.Content, "Copilot") {
		t.Fatalf("content = %q", resp.Content)
	}
}

// --- Copilot Chat Stream test ---

func TestCopilotChatStream(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)

		events := []string{
			`{"choices":[{"delta":{"content":"Hello"},"index":0}]}`,
			`{"choices":[{"delta":{"content":" Copilot"},"index":0}]}`,
			`{"choices":[{"delta":{},"finish_reason":"stop","index":0}]}`,
		}
		for _, e := range events {
			fmt.Fprintf(w, "data: %s\n\n", e)
			flusher.Flush()
		}
		fmt.Fprintf(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer srv.Close()

	cp := &copilotProvider{baseURL: srv.URL, token: "tok", model: "o4-mini"}
	ch, err := cp.Stream(context.Background(), Request{
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("stream error: %v", err)
	}

	var buf bytes.Buffer
	resp, err := CollectStream(ch, &buf)
	if err != nil {
		t.Fatalf("collect error: %v", err)
	}
	if resp.Content != "Hello Copilot" {
		t.Fatalf("content = %q", resp.Content)
	}
}

// --- Copilot Chat tool calls via streaming ---

func TestCopilotChatStreamToolCalls(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)

		events := []string{
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call-1","function":{"name":"get_weather","arguments":""}}]},"index":0}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"city\":"}}]},"index":0}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"Paris\"}"}}]},"index":0}]}`,
			`{"choices":[{"delta":{},"finish_reason":"tool_calls","index":0}]}`,
		}
		for _, e := range events {
			fmt.Fprintf(w, "data: %s\n\n", e)
			flusher.Flush()
		}
		fmt.Fprintf(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer srv.Close()

	cp := &copilotProvider{baseURL: srv.URL, token: "tok", model: "claude-sonnet-4-5"}
	ch, err := cp.Stream(context.Background(), Request{
		Messages: []Message{{Role: "user", Content: "weather?"}},
	})
	if err != nil {
		t.Fatalf("stream error: %v", err)
	}

	resp, err := CollectStream(ch, nil)
	if err != nil {
		t.Fatalf("collect error: %v", err)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(resp.ToolCalls))
	}
	if resp.ToolCalls[0].Name != "get_weather" {
		t.Fatalf("tool name = %q", resp.ToolCalls[0].Name)
	}
	if resp.ToolCalls[0].Arguments != `{"city":"Paris"}` {
		t.Fatalf("tool args = %q", resp.ToolCalls[0].Arguments)
	}
}

// --- Copilot Responses API Complete test ---

func TestCopilotResponsesComplete(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}

		resp := openAIResponse{
			ID: "resp-1",
			Output: []openAIResponseOutput{{
				Type: "message",
				Content: []struct {
					Type string `json:"type"`
					Text string `json:"text"`
				}{{Type: "text", Text: "GPT-5 response"}},
			}},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	cp := &copilotProvider{baseURL: srv.URL, token: "tok", model: "gpt-5"}
	resp, err := cp.Complete(context.Background(), Request{
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("complete error: %v", err)
	}
	if resp.Content != "GPT-5 response" {
		t.Fatalf("content = %q", resp.Content)
	}
}

// --- Copilot Responses API Stream test ---

func TestCopilotResponsesStream(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)

		events := []string{
			`{"type":"response.output_text.delta","delta":"GPT-5"}`,
			`{"type":"response.output_text.delta","delta":" streaming"}`,
			`{"type":"response.completed"}`,
		}
		for _, e := range events {
			fmt.Fprintf(w, "data: %s\n\n", e)
			flusher.Flush()
		}
	}))
	defer srv.Close()

	cp := &copilotProvider{baseURL: srv.URL, token: "tok", model: "gpt-5"}
	ch, err := cp.Stream(context.Background(), Request{
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("stream error: %v", err)
	}

	resp, err := CollectStream(ch, nil)
	if err != nil {
		t.Fatalf("collect error: %v", err)
	}
	if resp.Content != "GPT-5 streaming" {
		t.Fatalf("content = %q", resp.Content)
	}
}

// --- Copilot error normalization ---

func TestNormalizeCopilotError401(t *testing.T) {
	pe := normalizeCopilotError(401, "Unauthorized")
	if !strings.Contains(pe.Guidance, "copilot-login") {
		t.Fatalf("guidance = %q", pe.Guidance)
	}
}

func TestNormalizeCopilotError403ModelNotSupported(t *testing.T) {
	pe := normalizeCopilotError(403, "The requested model is not supported")
	if !strings.Contains(pe.Message, "model not available") {
		t.Fatalf("message = %q", pe.Message)
	}
	if !strings.Contains(pe.Guidance, "settings/copilot/features") {
		t.Fatalf("guidance = %q", pe.Guidance)
	}
}

func TestNormalizeCopilotError403Generic(t *testing.T) {
	pe := normalizeCopilotError(403, "Forbidden")
	if !strings.Contains(pe.Guidance, "copilot-login") {
		t.Fatalf("guidance = %q", pe.Guidance)
	}
}

// --- Token persistence tests ---

func TestSaveAndLoadCopilotToken(t *testing.T) {
	dir := t.TempDir()
	token := "gho_test_token_123"

	if err := SaveCopilotToken(dir, token); err != nil {
		t.Fatalf("save error: %v", err)
	}

	loaded := LoadCopilotToken(dir)
	if loaded != token {
		t.Fatalf("loaded = %q, want %q", loaded, token)
	}
}

func TestLoadCopilotTokenMissing(t *testing.T) {
	dir := t.TempDir()
	loaded := LoadCopilotToken(dir)
	if loaded != "" {
		t.Fatalf("expected empty, got %q", loaded)
	}
}

// --- Copilot HTTP error test ---

func TestCopilotHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":"invalid token"}`))
	}))
	defer srv.Close()

	cp := &copilotProvider{baseURL: srv.URL, token: "bad", model: "o4-mini"}
	_, err := cp.Complete(context.Background(), Request{
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error")
	}
	pe, ok := err.(*ProviderError)
	if !ok {
		t.Fatalf("expected ProviderError, got %T: %v", err, err)
	}
	if pe.StatusCode != 401 {
		t.Fatalf("status = %d", pe.StatusCode)
	}
}
