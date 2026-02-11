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
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"run-ai/internal/output"
	"run-ai/internal/provider"
	"run-ai/internal/skills"
)

const maxToolIterations = 10
const terminalToolName = "terminal"

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
			Tools:    buildToolDefs(cfg.Skills),
		}

		ch, err := cfg.Provider.Stream(ctx, req)
		if err != nil {
			cfg.Sink.Emit(output.EventERR, fmt.Sprintf("provider error: %v", err))
			return err
		}

		var fullText string
		var reasoningSummary string
		var toolCalls []provider.ToolCall
		var streamingAI bool

		for ev := range ch {
			if ev.Error != nil {
				if streamingAI {
					cfg.Sink.EndAIStream(fullText)
				}
				cfg.Sink.Emit(output.EventERR, fmt.Sprintf("stream error: %v", ev.Error))
				return ev.Error
			}
			if ev.Text != "" {
				fullText += ev.Text
				if !cfg.Sink.IsSilent() {
					if !streamingAI {
						cfg.Sink.BeginAIStream()
						streamingAI = true
					}
					cfg.Sink.EmitAIChunk(ev.Text)
				}
			}
			if ev.ReasoningSummary != "" {
				reasoningSummary += ev.ReasoningSummary
			}
			if len(ev.ToolCalls) > 0 {
				toolCalls = append(toolCalls, ev.ToolCalls...)
			}
		}

		if streamingAI {
			cfg.Sink.EndAIStream(fullText)
		}

		if reasoningSummary == "" {
			reasoningSummary = inferReasoningSummary(fullText)
		}

		if fullText != "" && !(cfg.Sink.IsSilent() && len(toolCalls) == 0) {
			cfg.Sink.EmitLog(output.EventAI, fullText)
		}

		// No tool calls — emit the final response and return.
		if len(toolCalls) == 0 {
			if cfg.Sink.IsSilent() {
				cfg.Sink.EmitFinal(fullText)
			}
			if reasoningSummary != "" {
				if cfg.Sink.IsSilent() {
					cfg.Sink.EmitLog(output.EventReasoning, reasoningSummary)
				} else {
					cfg.Sink.Emit(output.EventReasoning, reasoningSummary)
				}
			}
			return nil
		}

		if reasoningSummary != "" {
			if cfg.Sink.IsSilent() {
				cfg.Sink.EmitLog(output.EventReasoning, reasoningSummary)
			} else {
				cfg.Sink.Emit(output.EventReasoning, reasoningSummary)
			}
		}

		// Record assistant response in conversation history.
		messages = append(messages, provider.Message{
			Role:      "assistant",
			Content:   fullText,
			ToolCalls: toolCalls,
		})

		// Execute each tool call.
		for _, tc := range toolCalls {
			cmdLabel := fmt.Sprintf("tool: %s(%s)", tc.Name, tc.Arguments)
			if tc.Name == terminalToolName {
				args, err := parseTerminalArgs(tc.Arguments)
				if err != nil {
					cfg.Sink.Emit(output.EventERR, fmt.Sprintf("tool error: %v", err))
					messages = append(messages, provider.Message{
						Role:    "tool",
						Content: fmt.Sprintf("[%s result]\n%s", tc.Name, err.Error()),
					})
					continue
				}
				cmdLabel = args.Command
			}
			cfg.Sink.Emit(output.EventCMD, cmdLabel)

			result, err := executeToolCall(tc, cfg)
			toolResult := result
			if err != nil {
				errMsg := fmt.Sprintf("tool error: %v", err)
				cfg.Sink.Emit(output.EventERR, errMsg)
				if result != "" {
					cfg.Sink.Emit(output.EventOUT, result)
					toolResult = errMsg + "\n" + result
				} else {
					toolResult = errMsg
				}
			} else {
				cfg.Sink.Emit(output.EventOUT, result)
			}

			// Feed tool result back into conversation.
			messages = append(messages, provider.Message{
				Role:       "tool",
				Content:    fmt.Sprintf("[%s result]\n%s", tc.Name, toolResult),
				ToolCallID: tc.ID,
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

func inferReasoningSummary(text string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		lower := strings.ToLower(trimmed)
		if strings.HasPrefix(lower, "work:") || strings.HasPrefix(lower, "reasoning:") || strings.HasPrefix(lower, "steps:") {
			return strings.TrimSpace(strings.Join(lines[i:], "\n"))
		}
	}
	return ""
}

func buildToolDefs(discovered []skills.Skill) []provider.ToolDef {
	tools := []provider.ToolDef{terminalToolDef()}
	for _, s := range discovered {
		tools = append(tools, provider.ToolDef{
			Name:        s.Name,
			Description: s.Description,
			Parameters:  `{"type":"object","properties":{}}`,
		})
	}
	return tools
}

func terminalToolDef() provider.ToolDef {
	return provider.ToolDef{
		Name:        terminalToolName,
		Description: "Run a shell command in the current workspace.",
		Parameters:  `{"type":"object","properties":{"command":{"type":"string","description":"Shell command to run."}},"required":["command"]}`,
	}
}

func executeToolCall(tc provider.ToolCall, cfg Config) (string, error) {
	if tc.Name == terminalToolName {
		args, err := parseTerminalArgs(tc.Arguments)
		if err != nil {
			return "", err
		}
		return runTerminalCommand(args.Command, cfg.BaseDir)
	}

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

type terminalArgs struct {
	Command string `json:"command"`
}

func parseTerminalArgs(raw string) (terminalArgs, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return terminalArgs{}, errors.New("terminal tool requires command")
	}

	var args terminalArgs
	if err := json.Unmarshal([]byte(trimmed), &args); err != nil {
		var str string
		if err2 := json.Unmarshal([]byte(trimmed), &str); err2 == nil {
			args.Command = str
		} else {
			return terminalArgs{}, fmt.Errorf("invalid terminal arguments: %w", err)
		}
	}

	args.Command = strings.TrimSpace(args.Command)
	if args.Command == "" {
		return terminalArgs{}, errors.New("terminal tool requires command")
	}
	return args, nil
}

func runTerminalCommand(command, workDir string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	command = normalizeWindowsCommand(command)

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "cmd.exe", "/C", command)
	} else {
		cmd = exec.CommandContext(ctx, "sh", "-c", command)
	}
	if workDir != "" {
		cmd.Dir = workDir
	}

	output, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return string(output), fmt.Errorf("command timed out")
	}
	if err != nil {
		return string(output), fmt.Errorf("command failed: %w", err)
	}
	return string(output), nil
}

func normalizeWindowsCommand(command string) string {
	if runtime.GOOS != "windows" {
		return command
	}

	trimmed := strings.TrimSpace(command)
	if trimmed == "" {
		return command
	}

	lower := strings.ToLower(trimmed)
	if !strings.HasPrefix(lower, "ls") {
		return command
	}
	if len(lower) > 2 {
		next := lower[2]
		if next != ' ' && next != '\t' {
			return command
		}
	}

	fields := strings.Fields(trimmed)
	if len(fields) == 0 || strings.ToLower(fields[0]) != "ls" {
		return command
	}

	showAll := false
	var paths []string
	for _, f := range fields[1:] {
		if strings.HasPrefix(f, "-") {
			if strings.Contains(f, "a") {
				showAll = true
			}
			continue
		}
		paths = append(paths, f)
	}

	rewritten := "dir"
	if showAll {
		rewritten += " /a"
	}
	if len(paths) > 0 {
		rewritten += " " + strings.Join(paths, " ")
	}
	return rewritten
}
