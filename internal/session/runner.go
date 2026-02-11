// Package session implements the prompt-to-response execution loop.
//
// The runner assembles the conversation (system prompt, user prompt, skill
// context), sends it to the resolved provider, streams the response through
// the output sink, and handles tool calls by executing skills or terminal
// commands and feeding results back.
//
// Why a loop?  LLM providers may respond with tool calls that require execution
// before the model can produce a final text answer.  The runner repeats until
// the model responds with text only or the iteration limit is reached.
package session

import (
	"context"
	"fmt"

	"run-ai/internal/output"
	"run-ai/internal/provider"
	"run-ai/internal/skills"
)

const maxToolIterations = 10

// Config holds everything the runner needs to execute one session.
type Config struct {
	Provider     provider.Provider
	Sink         *output.Sink
	SystemPrompt string
	UserPrompt   string
	Skills       []skills.Skill
	BaseDir      string
}

// Run executes a single prompt session: send to provider, stream output,
// handle tool calls, and repeat until a final text response is produced.
func Run(ctx context.Context, cfg Config) error {
	messages := buildMessages(cfg)

	for i := 0; i < maxToolIterations; i++ {
		req := provider.Request{
			Messages: messages,
		}

		// Add skill tools if available.
		if len(cfg.Skills) > 0 {
			req.Tools = buildToolDefs(cfg.Skills)
		}

		ch, err := cfg.Provider.Stream(ctx, req)
		if err != nil {
			cfg.Sink.Emit(output.EventERR, fmt.Sprintf("provider error: %v", err))
			return err
		}

		var fullText string
		var toolCalls []provider.ToolCall

		for ev := range ch {
			if ev.Error != nil {
				cfg.Sink.Emit(output.EventERR, fmt.Sprintf("stream error: %v", ev.Error))
				return ev.Error
			}
			if ev.Text != "" {
				fullText += ev.Text
			}
			if len(ev.ToolCalls) > 0 {
				toolCalls = append(toolCalls, ev.ToolCalls...)
			}
		}

		// No tool calls — emit the final response and return.
		if len(toolCalls) == 0 {
			cfg.Sink.EmitFinal(fullText)
			return nil
		}

		// Record assistant response in conversation history.
		if fullText != "" {
			cfg.Sink.Emit(output.EventAI, fullText)
		}
		messages = append(messages, provider.Message{Role: "assistant", Content: fullText})

		// Execute each tool call.
		for _, tc := range toolCalls {
			cfg.Sink.Emit(output.EventCMD, fmt.Sprintf("tool: %s(%s)", tc.Name, tc.Arguments))

			result, err := executeToolCall(tc, cfg)
			if err != nil {
				errMsg := fmt.Sprintf("tool error: %v", err)
				cfg.Sink.Emit(output.EventERR, errMsg)
				result = errMsg
			} else {
				cfg.Sink.Emit(output.EventOUT, result)
			}

			// Feed tool result back into conversation.
			messages = append(messages, provider.Message{
				Role:    "tool",
				Content: fmt.Sprintf("[%s result]\n%s", tc.Name, result),
			})
		}
	}

	cfg.Sink.Emit(output.EventERR, "maximum tool call iterations reached")
	return fmt.Errorf("exceeded %d tool call iterations", maxToolIterations)
}

func buildMessages(cfg Config) []provider.Message {
	var msgs []provider.Message

	// System prompt: combine agent instructions + skill context.
	systemParts := ""
	if cfg.SystemPrompt != "" {
		systemParts = cfg.SystemPrompt
	}
	if len(cfg.Skills) > 0 {
		skillCtx := skills.FormatContext(cfg.Skills)
		if skillCtx != "" {
			if systemParts != "" {
				systemParts += "\n\n"
			}
			systemParts += skillCtx
		}
	}
	if systemParts != "" {
		msgs = append(msgs, provider.Message{Role: "system", Content: systemParts})
	}

	msgs = append(msgs, provider.Message{Role: "user", Content: cfg.UserPrompt})
	return msgs
}

func buildToolDefs(discovered []skills.Skill) []provider.ToolDef {
	var tools []provider.ToolDef
	for _, s := range discovered {
		tools = append(tools, provider.ToolDef{
			Name:        s.Name,
			Description: s.Description,
			Parameters:  `{"type":"object","properties":{}}`,
		})
	}
	return tools
}

func executeToolCall(tc provider.ToolCall, cfg Config) (string, error) {
	// Find matching skill.
	for _, s := range cfg.Skills {
		if s.Name == tc.Name {
			// For now, treat the skill's body as the result (instructions).
			// Full script execution will be wired when skills have scripts/.
			return fmt.Sprintf("[skill: %s]\n%s", s.Name, s.Body), nil
		}
	}

	return "", fmt.Errorf("unknown tool: %s", tc.Name)
}
