package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadMissingConfig(t *testing.T) {
	tempDir := t.TempDir()

	values, err := Load(tempDir)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(values) != 0 {
		t.Fatalf("expected empty config, got %v", values)
	}
}

func TestSetCreatesAndUpdatesConfig(t *testing.T) {
	tempDir := t.TempDir()

	if err := Set(tempDir, "endpoint", "http://example.test"); err != nil {
		t.Fatalf("expected Set to succeed, got %v", err)
	}

	if err := Set(tempDir, "model", "gpt-test"); err != nil {
		t.Fatalf("expected Set to succeed, got %v", err)
	}

	values, err := Load(tempDir)
	if err != nil {
		t.Fatalf("expected Load to succeed, got %v", err)
	}

	if values["endpoint"] != "http://example.test" {
		t.Fatalf("expected endpoint to be set, got %q", values["endpoint"])
	}
	if values["model"] != "gpt-test" {
		t.Fatalf("expected model to be set, got %q", values["model"])
	}

	configPath := ConfigPath(tempDir)
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("expected config file to exist, got %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "endpoint = \"http://example.test\"") {
		t.Fatalf("expected endpoint entry, got %q", content)
	}
	if !strings.Contains(content, "model = \"gpt-test\"") {
		t.Fatalf("expected model entry, got %q", content)
	}

	if _, err := os.Stat(filepath.Dir(configPath)); err != nil {
		t.Fatalf("expected config dir to exist, got %v", err)
	}
}
