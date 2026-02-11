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

func TestEnvValues(t *testing.T) {
	t.Setenv("RAI_ENDPOINT", "http://env.test")
	t.Setenv("RAI_API_KEY", "secret")
	t.Setenv("OTHER", "skip")

	values := EnvValues()
	if values["endpoint"] != "http://env.test" {
		t.Fatalf("expected endpoint to be set, got %q", values["endpoint"])
	}
	if values["api_key"] != "secret" {
		t.Fatalf("expected api_key to be set, got %q", values["api_key"])
	}
	if _, ok := values["other"]; ok {
		t.Fatalf("expected OTHER to be ignored")
	}
}

func TestMergePrecedence(t *testing.T) {
	defaults := map[string]string{"model": "default", "endpoint": "default"}
	env := map[string]string{"model": "env", "endpoint": "env"}
	file := map[string]string{"endpoint": "file"}
	agent := map[string]string{"model": "agent"}
	cli := map[string]string{"model": "cli"}

	merged := MergePrecedence(defaults, env, file, agent, cli)
	if merged["model"] != "cli" {
		t.Fatalf("expected model to be cli, got %q", merged["model"])
	}
	if merged["endpoint"] != "file" {
		t.Fatalf("expected endpoint to be file, got %q", merged["endpoint"])
	}
}

func TestLoadMerged(t *testing.T) {
	tempDir := t.TempDir()
	if err := Set(tempDir, "endpoint", "file"); err != nil {
		t.Fatalf("expected Set to succeed, got %v", err)
	}

	t.Setenv("RAI_MODEL", "env-model")

	defaults := map[string]string{"model": "default"}
	agent := map[string]string{"model": "agent"}
	cli := map[string]string{"model": "cli"}

	merged, err := LoadMerged(tempDir, agent, cli, defaults)
	if err != nil {
		t.Fatalf("expected LoadMerged to succeed, got %v", err)
	}
	if merged["endpoint"] != "file" {
		t.Fatalf("expected endpoint to be file, got %q", merged["endpoint"])
	}
	if merged["model"] != "cli" {
		t.Fatalf("expected model to be cli, got %q", merged["model"])
	}
}
