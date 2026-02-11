package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --- ParseArgs tests ---

func TestParseArgsPrompt(t *testing.T) {
	p := ParseArgs([]string{"what is 1+1"})
	if p.Prompt != "what is 1+1" {
		t.Fatalf("prompt = %q, want %q", p.Prompt, "what is 1+1")
	}
	if p.Command != "" {
		t.Fatalf("command = %q, want empty", p.Command)
	}
}

func TestParseArgsHelp(t *testing.T) {
	for _, flag := range []string{"-h", "--help", "help"} {
		p := ParseArgs([]string{flag})
		if !p.ShowHelp {
			t.Errorf("ParseArgs(%q): ShowHelp = false, want true", flag)
		}
	}
}

func TestParseArgsSilent(t *testing.T) {
	p := ParseArgs([]string{"-silent", "hello"})
	if !p.Silent {
		t.Fatal("Silent = false, want true")
	}
	if p.Prompt != "hello" {
		t.Fatalf("prompt = %q, want %q", p.Prompt, "hello")
	}
}

func TestParseArgsLog(t *testing.T) {
	p := ParseArgs([]string{"-log", "hello"})
	if !p.Log {
		t.Fatal("Log = false, want true")
	}
	if p.Prompt != "hello" {
		t.Fatalf("prompt = %q, want %q", p.Prompt, "hello")
	}
}

func TestParseArgsAgent(t *testing.T) {
	p := ParseArgs([]string{"--agent", "./reviewer.md", "review code"})
	if p.AgentPath != "./reviewer.md" {
		t.Fatalf("AgentPath = %q, want %q", p.AgentPath, "./reviewer.md")
	}
	if p.Prompt != "review code" {
		t.Fatalf("prompt = %q, want %q", p.Prompt, "review code")
	}
}

func TestParseArgsAgentEquals(t *testing.T) {
	p := ParseArgs([]string{"--agent=./reviewer.md", "review code"})
	if p.AgentPath != "./reviewer.md" {
		t.Fatalf("AgentPath = %q, want %q", p.AgentPath, "./reviewer.md")
	}
}

func TestParseArgsCombinedFlags(t *testing.T) {
	p := ParseArgs([]string{"-silent", "-log", "--agent", "a.md", "do stuff"})
	if !p.Silent {
		t.Error("Silent = false")
	}
	if !p.Log {
		t.Error("Log = false")
	}
	if p.AgentPath != "a.md" {
		t.Errorf("AgentPath = %q", p.AgentPath)
	}
	if p.Prompt != "do stuff" {
		t.Errorf("Prompt = %q", p.Prompt)
	}
}

func TestParseArgsConfig(t *testing.T) {
	p := ParseArgs([]string{"config", "key", "value"})
	if p.Command != "config" {
		t.Fatalf("Command = %q, want config", p.Command)
	}
	if len(p.SubArgs) != 2 || p.SubArgs[0] != "key" || p.SubArgs[1] != "value" {
		t.Fatalf("SubArgs = %v, want [key value]", p.SubArgs)
	}
}

func TestParseArgsSkillsList(t *testing.T) {
	p := ParseArgs([]string{"skills", "list"})
	if p.Command != "skills" {
		t.Fatalf("Command = %q, want skills", p.Command)
	}
	if len(p.SubArgs) != 1 || p.SubArgs[0] != "list" {
		t.Fatalf("SubArgs = %v, want [list]", p.SubArgs)
	}
}

// --- Run integration tests ---

func TestRunNoArgs(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run(nil, &stdout, &stderr, t.TempDir())
	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "Usage") {
		t.Fatalf("expected usage in stderr, got %q", stderr.String())
	}
}

func TestRunHelp(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"--help"}, &stdout, &stderr, t.TempDir())
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if !strings.Contains(stdout.String(), "Usage") {
		t.Fatalf("expected usage in stdout, got %q", stdout.String())
	}
}

func TestRunPrompt(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"what is go"}, &stdout, &stderr, t.TempDir())
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if !strings.Contains(stdout.String(), "prompt: what is go") {
		t.Fatalf("expected prompt echo, got %q", stdout.String())
	}
}

func TestRunSilentPrompt(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"-silent", "hello"}, &stdout, &stderr, t.TempDir())
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	out := stdout.String()
	// In silent mode the final response is still shown.
	if !strings.Contains(out, "prompt: hello") {
		t.Fatalf("expected final prompt echo in silent mode, got %q", out)
	}
}

func TestRunLogCreatesFile(t *testing.T) {
	dir := t.TempDir()
	var stdout, stderr bytes.Buffer
	code := Run([]string{"-log", "test prompt"}, &stdout, &stderr, dir)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}

	// stderr should have the log path.
	if !strings.Contains(stderr.String(), "log:") {
		t.Fatalf("expected log path in stderr, got %q", stderr.String())
	}

	// A log file should exist in .rai/log/.
	logDir := filepath.Join(dir, ".rai", "log")
	entries, err := os.ReadDir(logDir)
	if err != nil {
		t.Fatalf("reading log dir: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("expected log file to be created")
	}

	// Log file should contain the prompt.
	data, _ := os.ReadFile(filepath.Join(logDir, entries[0].Name()))
	if !strings.Contains(string(data), "test prompt") {
		t.Fatalf("expected prompt in log, got %q", string(data))
	}
}

func TestRunConfigCommand(t *testing.T) {
	dir := t.TempDir()
	var stdout, stderr bytes.Buffer
	code := Run([]string{"config", "model", "gpt-4"}, &stdout, &stderr, dir)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if !strings.Contains(stdout.String(), "config updated") {
		t.Fatalf("expected confirmation, got %q", stdout.String())
	}

	// Verify the value was persisted.
	data, _ := os.ReadFile(filepath.Join(dir, ".rai", "config"))
	if !strings.Contains(string(data), "gpt-4") {
		t.Fatalf("config file missing value, got %q", string(data))
	}
}

func TestRunWithAgent(t *testing.T) {
	dir := t.TempDir()

	// Create a simple agent file.
	agentPath := filepath.Join(dir, "agent.md")
	agentContent := "You are a test agent.\n"
	if err := os.WriteFile(agentPath, []byte(agentContent), 0o644); err != nil {
		t.Fatalf("write agent: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := Run([]string{"--agent", agentPath, "do something"}, &stdout, &stderr, dir)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if !strings.Contains(stdout.String(), "prompt: do something") {
		t.Fatalf("expected prompt echo, got %q", stdout.String())
	}
}

func TestRunWithAgentWarnings(t *testing.T) {
	dir := t.TempDir()

	// Agent with unknown keys produces warnings.
	agentPath := filepath.Join(dir, "agent.md")
	content := "---\nmodel: gpt-4\ncustom-key: val\n---\nSystem prompt.\n"
	if err := os.WriteFile(agentPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write agent: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := Run([]string{"--agent", agentPath, "query"}, &stdout, &stderr, dir)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}

	// Warning should be emitted as an ERR event to console.
	if !strings.Contains(stdout.String(), "unknown agent key: custom-key") {
		t.Fatalf("expected agent warning on console, got %q", stdout.String())
	}
}

func TestRunWithMissingAgent(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"--agent", "/does/not/exist.md", "query"}, &stdout, &stderr, t.TempDir())
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "agent error") {
		t.Fatalf("expected agent error, got %q", stderr.String())
	}
}

func TestRunSkillsList(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"skills", "list"}, &stdout, &stderr, t.TempDir())
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
}

func TestRunLogWithAgent(t *testing.T) {
	dir := t.TempDir()

	agentPath := filepath.Join(dir, "agent.md")
	if err := os.WriteFile(agentPath, []byte("You are helpful.\n"), 0o644); err != nil {
		t.Fatalf("write agent: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := Run([]string{"-log", "--agent", agentPath, "hello"}, &stdout, &stderr, dir)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}

	// Log should contain agent content and prompt.
	logDir := filepath.Join(dir, ".rai", "log")
	entries, _ := os.ReadDir(logDir)
	if len(entries) == 0 {
		t.Fatal("expected log file")
	}
	data, _ := os.ReadFile(filepath.Join(logDir, entries[0].Name()))
	content := string(data)
	if !strings.Contains(content, "You are helpful.") {
		t.Fatalf("expected agent content in log")
	}
	if !strings.Contains(content, "hello") {
		t.Fatalf("expected prompt in log")
	}
}
