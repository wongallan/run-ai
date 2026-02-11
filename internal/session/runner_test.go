package session

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"time"

	"run-ai/internal/output"
	"run-ai/internal/provider"
	"run-ai/internal/skills"
)

func nowFunc() func() time.Time {
	return func() time.Time { return time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC) }
}

// mockProvider returns a provider that resolves against a mock HTTP server.
func mockProvider(t *testing.T, handler http.HandlerFunc) provider.Provider {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	p, err := provider.Resolve(map[string]string{
		"endpoint": srv.URL,
		"api-key":  "test",
		"model":    "test-model",
	})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	return p
}

// --- Basic prompt test ---

func TestRunBasicPrompt(t *testing.T) {
	p := mockProvider(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		fmt.Fprintln(w, `data: {"type":"response.output_text.delta","delta":"Hello from AI"}`)
		flusher.Flush()
		fmt.Fprintln(w, `data: {"type":"response.completed"}`)
		flusher.Flush()
	})

	var buf bytes.Buffer
	sink, _ := output.NewSink(output.Options{Console: &buf, Now: nowFunc()})

	err := Run(context.Background(), Config{
		Provider:   p,
		Sink:       sink,
		UserPrompt: "hello",
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	sink.Close()

	if !strings.Contains(buf.String(), "Hello from AI") {
		t.Fatalf("expected 'Hello from AI' in output, got %q", buf.String())
	}
}

func TestRunStreamsToConsole(t *testing.T) {
	p := mockProvider(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		fmt.Fprintln(w, `data: {"type":"response.output_text.delta","delta":"Hello"}`)
		flusher.Flush()
		fmt.Fprintln(w, `data: {"type":"response.output_text.delta","delta":" world"}`)
		flusher.Flush()
		fmt.Fprintln(w, `data: {"type":"response.completed"}`)
		flusher.Flush()
	})

	var buf bytes.Buffer
	sink, _ := output.NewSink(output.Options{Console: &buf, Now: nowFunc()})

	err := Run(context.Background(), Config{
		Provider:   p,
		Sink:       sink,
		UserPrompt: "hello",
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	sink.Close()

	out := buf.String()
	if !strings.Contains(out, "[AI] Hello") || !strings.Contains(out, "[AI]  world") {
		t.Fatalf("expected streamed AI output, got %q", out)
	}
}

// --- System prompt test ---

func TestRunWithSystemPrompt(t *testing.T) {
	var receivedSystem string

	p := mockProvider(t, func(w http.ResponseWriter, r *http.Request) {
		body := make([]byte, 4096)
		n, _ := r.Body.Read(body)
		var req struct {
			Input []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"input"`
		}
		json.Unmarshal(body[:n], &req)
		for _, m := range req.Input {
			if m.Role == "system" {
				receivedSystem = m.Content
			}
		}

		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		fmt.Fprintln(w, `data: {"type":"response.output_text.delta","delta":"ok"}`)
		flusher.Flush()
		fmt.Fprintln(w, `data: {"type":"response.completed"}`)
		flusher.Flush()
	})

	var buf bytes.Buffer
	sink, _ := output.NewSink(output.Options{Console: &buf, Now: nowFunc()})

	err := Run(context.Background(), Config{
		Provider:     p,
		Sink:         sink,
		SystemPrompt: "You are a test bot.",
		UserPrompt:   "hi",
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if receivedSystem != "You are a test bot." {
		t.Fatalf("system prompt = %q", receivedSystem)
	}
}

// --- Skills context injection test ---

func TestRunWithSkillsContext(t *testing.T) {
	var receivedSystem string

	p := mockProvider(t, func(w http.ResponseWriter, r *http.Request) {
		body := make([]byte, 8192)
		n, _ := r.Body.Read(body)
		var req struct {
			Input []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"input"`
		}
		json.Unmarshal(body[:n], &req)
		for _, m := range req.Input {
			if m.Role == "system" {
				receivedSystem = m.Content
			}
		}

		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		fmt.Fprintln(w, `data: {"type":"response.output_text.delta","delta":"done"}`)
		flusher.Flush()
		fmt.Fprintln(w, `data: {"type":"response.completed"}`)
		flusher.Flush()
	})

	var buf bytes.Buffer
	sink, _ := output.NewSink(output.Options{Console: &buf, Now: nowFunc()})

	testSkills := []skills.Skill{
		{Name: "test-skill", Description: "Does testing.", Dir: "/test"},
	}

	err := Run(context.Background(), Config{
		Provider:     p,
		Sink:         sink,
		SystemPrompt: "Be helpful.",
		UserPrompt:   "hi",
		Skills:       testSkills,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if !strings.Contains(receivedSystem, "<available_skills>") {
		t.Fatalf("expected skills XML in system prompt, got %q", receivedSystem)
	}
	if !strings.Contains(receivedSystem, "Be helpful.") {
		t.Fatalf("expected original system prompt preserved")
	}
}

// --- Silent mode test ---

func TestRunSilentMode(t *testing.T) {
	p := mockProvider(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		fmt.Fprintln(w, `data: {"type":"response.output_text.delta","delta":"silent response"}`)
		flusher.Flush()
		fmt.Fprintln(w, `data: {"type":"response.completed"}`)
		flusher.Flush()
	})

	var buf bytes.Buffer
	sink, _ := output.NewSink(output.Options{Console: &buf, Silent: true, Now: nowFunc()})

	err := Run(context.Background(), Config{
		Provider:   p,
		Sink:       sink,
		UserPrompt: "hello",
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	sink.Close()

	// Final response should still appear.
	if !strings.Contains(buf.String(), "silent response") {
		t.Fatalf("expected final response in silent mode, got %q", buf.String())
	}
}

// --- Provider error test ---

func TestRunProviderError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte(`{"error":"service down"}`))
	}))
	defer srv.Close()

	p, _ := provider.Resolve(map[string]string{
		"endpoint": srv.URL,
		"api-key":  "test",
		"model":    "test",
	})

	var buf bytes.Buffer
	sink, _ := output.NewSink(output.Options{Console: &buf, Now: nowFunc()})

	err := Run(context.Background(), Config{
		Provider:   p,
		Sink:       sink,
		UserPrompt: "hi",
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(buf.String(), "[ERR]") {
		t.Fatalf("expected error event, got %q", buf.String())
	}
}

// --- buildMessages test ---

func TestBuildMessages(t *testing.T) {
	msgs := buildMessages(Config{
		SystemPrompt: "Be helpful.",
		UserPrompt:   "What is Go?",
	})

	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	if msgs[0].Role != "system" || msgs[0].Content != "Be helpful." {
		t.Fatalf("system msg: %+v", msgs[0])
	}
	if msgs[1].Role != "user" || msgs[1].Content != "What is Go?" {
		t.Fatalf("user msg: %+v", msgs[1])
	}
}

func TestBuildMessagesNoSystem(t *testing.T) {
	msgs := buildMessages(Config{UserPrompt: "hello"})
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].Role != "user" {
		t.Fatalf("expected user role, got %q", msgs[0].Role)
	}
}

func TestExecuteToolCallTerminal(t *testing.T) {
	cmd := "echo hello"
	if runtime.GOOS == "windows" {
		cmd = "echo hello"
	}

	res, err := executeToolCall(provider.ToolCall{
		Name:      "terminal",
		Arguments: fmt.Sprintf(`{"command":"%s"}`, cmd),
	}, Config{BaseDir: t.TempDir()})
	if err != nil {
		t.Fatalf("executeToolCall: %v", err)
	}
	if !strings.Contains(res, "hello") {
		t.Fatalf("expected command output, got %q", res)
	}
}
