package agent

import (
	"strings"
	"testing"
)

func TestParseMarkdownOnly(t *testing.T) {
	content := "You are a helpful coding assistant.\n"
	parsed, err := Parse(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if parsed.SystemPrompt != content {
		t.Fatalf("expected system prompt to match content")
	}
	if len(parsed.Config) != 0 {
		t.Fatalf("expected empty config")
	}
	if len(parsed.Warnings) != 0 {
		t.Fatalf("expected no warnings")
	}
}

func TestParseFrontmatter(t *testing.T) {
	content := strings.Join([]string{
		"---",
		"model: gpt-4",
		"temperature: 0.7",
		"max-tokens: 2000",
		"custom-param: value",
		"---",
		"You are a helpful coding assistant.",
		"",
	}, "\n")

	parsed, err := Parse(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if parsed.SystemPrompt != "You are a helpful coding assistant.\n" {
		t.Fatalf("unexpected system prompt: %q", parsed.SystemPrompt)
	}
	if parsed.Config["model"] != "gpt-4" {
		t.Fatalf("expected model to be parsed")
	}
	if parsed.Config["temperature"] != "0.7" {
		t.Fatalf("expected temperature to be parsed")
	}
	if parsed.Config["max-tokens"] != "2000" {
		t.Fatalf("expected max-tokens to be parsed")
	}
	if parsed.Config["custom-param"] != "value" {
		t.Fatalf("expected custom-param to be parsed")
	}
	if len(parsed.Warnings) != 1 {
		t.Fatalf("expected one warning, got %d", len(parsed.Warnings))
	}
	if parsed.Warnings[0] != "unknown agent key: custom-param" {
		t.Fatalf("unexpected warning: %s", parsed.Warnings[0])
	}
}

func TestParseFrontmatterMissingDelimiter(t *testing.T) {
	content := strings.Join([]string{
		"---",
		"model: gpt-4",
		"You are a helpful coding assistant.",
	}, "\n")

	_, err := Parse(content)
	if err == nil {
		t.Fatalf("expected error")
	}
}
