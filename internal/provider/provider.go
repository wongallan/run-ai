// Package provider defines the interface for LLM providers and routing logic.
//
// Each provider (OpenAI-compatible, Anthropic, Google, GitHub Copilot) implements
// the Provider interface.  The router selects the appropriate provider based on
// explicit configuration or endpoint heuristics.
//
// Why an interface?  The CLI needs a single code path for prompt execution
// regardless of which backend handles the request.  Streaming is first-class
// because the spec requires real-time output.
package provider

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
)

// Message represents a single message in a conversation.
type Message struct {
	Role    string // "system", "user", "assistant", "tool"
	Content string
}

// ToolCall represents a tool invocation requested by the provider.
type ToolCall struct {
	ID        string
	Name      string
	Arguments string // JSON-encoded arguments
}

// ToolDef describes a tool available to the provider.
type ToolDef struct {
	Name        string
	Description string
	Parameters  string // JSON Schema for parameters
}

// StreamEvent represents a chunk of streaming output from a provider.
type StreamEvent struct {
	// Exactly one of these is set per event.
	Text      string     // Incremental text content.
	ToolCalls []ToolCall // Tool invocation requests.
	Done      bool       // End of stream marker.
	Error     error      // Provider-side error.
}

// Request holds everything needed to send a prompt to a provider.
type Request struct {
	Messages    []Message
	Tools       []ToolDef
	Model       string
	MaxTokens   int
	Temperature *float64 // nil means provider default
}

// Response is the complete, non-streaming result of a provider call.
type Response struct {
	Content   string
	ToolCalls []ToolCall
}

// Provider is the interface every LLM backend must implement.
type Provider interface {
	// Name returns a human-readable provider identifier (e.g. "openai", "anthropic").
	Name() string

	// Complete sends a request and returns the full response.
	Complete(ctx context.Context, req Request) (Response, error)

	// Stream sends a request and returns a channel of streaming events.
	// The channel is closed when the stream ends.  Callers must read until
	// the channel closes or cancel the context.
	Stream(ctx context.Context, req Request) (<-chan StreamEvent, error)
}

// ErrNoProvider is returned when no provider matches the configuration.
var ErrNoProvider = errors.New("no provider configured: set endpoint, api-key, and model via 'rai config' or environment variables")

// ErrAuthRequired is returned when credentials are missing.
var ErrAuthRequired = errors.New("authentication required: set api-key via 'rai config api-key <key>' or RAI_API_KEY")

// ErrModelRequired is returned when no model is specified.
var ErrModelRequired = errors.New("model required: set model via 'rai config model <name>' or RAI_MODEL")

// Resolve selects and configures a Provider from merged configuration values.
// Provider selection order:
//  1. Explicit "provider" key (e.g. "github-copilot")
//  2. Endpoint URL heuristics
//  3. Error if nothing matches
func Resolve(cfg map[string]string) (Provider, error) {
	explicit := strings.TrimSpace(cfg["provider"])

	switch explicit {
	case "github-copilot", "github-copilot-enterprise":
		return newCopilotProvider(cfg, explicit)
	}

	endpoint := strings.TrimSpace(cfg["endpoint"])
	if endpoint == "" {
		return nil, ErrNoProvider
	}

	apiKey := cfg["api-key"]
	if apiKey == "" {
		apiKey = cfg["api_key"]
	}

	model := cfg["model"]

	// Heuristic: detect provider from endpoint URL.
	switch {
	case strings.Contains(endpoint, "anthropic"):
		return newAnthropicProvider(endpoint, apiKey, model, cfg)
	case strings.Contains(endpoint, "generativelanguage.googleapis.com"):
		return newGoogleProvider(endpoint, apiKey, model, cfg)
	default:
		// Default to OpenAI-compatible (Responses API).
		return newOpenAIProvider(endpoint, apiKey, model, cfg)
	}
}

// newOpenAIProvider creates an OpenAI-compatible provider stub.
func newOpenAIProvider(endpoint, apiKey, model string, cfg map[string]string) (Provider, error) {
	if apiKey == "" {
		return nil, ErrAuthRequired
	}
	if model == "" {
		return nil, ErrModelRequired
	}
	return &openAIProvider{
		endpoint: endpoint,
		apiKey:   apiKey,
		model:    model,
	}, nil
}

// newAnthropicProvider creates an Anthropic provider stub.
func newAnthropicProvider(endpoint, apiKey, model string, cfg map[string]string) (Provider, error) {
	if apiKey == "" {
		return nil, ErrAuthRequired
	}
	if model == "" {
		return nil, ErrModelRequired
	}
	return &anthropicProvider{
		endpoint: endpoint,
		apiKey:   apiKey,
		model:    model,
	}, nil
}

// newGoogleProvider creates a Google/Gemini provider stub.
func newGoogleProvider(endpoint, apiKey, model string, cfg map[string]string) (Provider, error) {
	if apiKey == "" {
		return nil, ErrAuthRequired
	}
	if model == "" {
		return nil, ErrModelRequired
	}
	return &googleProvider{
		endpoint: endpoint,
		apiKey:   apiKey,
		model:    model,
	}, nil
}

// newCopilotProvider creates a GitHub Copilot provider.
// Token is sourced from api-key/api_key/copilot-token in the config map.
// The CLI layer is responsible for loading stored tokens into the config.
func newCopilotProvider(cfg map[string]string, providerID string) (Provider, error) {
	token := cfg["api-key"]
	if token == "" {
		token = cfg["api_key"]
	}
	if token == "" {
		token = cfg["copilot-token"]
	}
	if token == "" {
		return nil, fmt.Errorf("GitHub Copilot token required: authenticate with 'rai copilot-login' or set api-key")
	}

	enterpriseURL := ""
	if providerID == "github-copilot-enterprise" {
		enterpriseURL = cfg["enterprise-url"]
		if enterpriseURL == "" {
			enterpriseURL = cfg["enterprise_url"]
		}
	}

	model := cfg["model"]
	if model == "" {
		model = "gpt-5-mini" // default free Copilot model
	}

	baseURL := CopilotBaseURL(enterpriseURL)

	return &copilotProvider{
		baseURL: baseURL,
		token:   token,
		model:   model,
	}, nil
}

// --- Shared streaming helper ---

// CollectStream reads all events from a stream channel and assembles the response.
func CollectStream(ch <-chan StreamEvent, w io.Writer) (Response, error) {
	var resp Response
	for ev := range ch {
		if ev.Error != nil {
			return resp, ev.Error
		}
		if ev.Text != "" {
			resp.Content += ev.Text
			if w != nil {
				io.WriteString(w, ev.Text)
			}
		}
		if len(ev.ToolCalls) > 0 {
			resp.ToolCalls = append(resp.ToolCalls, ev.ToolCalls...)
		}
	}
	return resp, nil
}
