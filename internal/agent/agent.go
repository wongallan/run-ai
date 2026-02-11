package agent

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Agent holds parsed agent file data.
type Agent struct {
	SystemPrompt string
	Config       map[string]string
	Warnings     []string
}

var knownKeys = map[string]struct{}{
	"api-key":           {},
	"endpoint":          {},
	"max-tokens":        {},
	"max_tokens":        {},
	"model":             {},
	"org":               {},
	"organization":      {},
	"provider":          {},
	"temperature":       {},
	"top-p":             {},
	"top_p":             {},
	"tool-choice":       {},
	"tool_choice":       {},
	"max-output-tokens": {},
	"max_output_tokens": {},
}

// ParseFile loads and parses an agent file from disk.
func ParseFile(path string) (Agent, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Agent{}, err
	}
	return Parse(string(data))
}

// Parse reads agent file content and returns the parsed agent.
func Parse(content string) (Agent, error) {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	if strings.HasPrefix(content, "\ufeff") {
		content = strings.TrimPrefix(content, "\ufeff")
	}

	if !strings.HasPrefix(content, "---\n") && content != "---" && !strings.HasPrefix(content, "---\r\n") {
		return Agent{
			SystemPrompt: content,
			Config:       map[string]string{},
			Warnings:     nil,
		}, nil
	}

	lines := strings.Split(content, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return Agent{SystemPrompt: content, Config: map[string]string{}}, nil
	}

	end := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			end = i
			break
		}
	}
	if end == -1 {
		return Agent{}, errors.New("agent frontmatter missing closing delimiter")
	}

	yamlBlock := strings.Join(lines[1:end], "\n")
	body := strings.Join(lines[end+1:], "\n")
	body = strings.TrimPrefix(body, "\n")

	parsed := map[string]interface{}{}
	if err := yaml.Unmarshal([]byte(yamlBlock), &parsed); err != nil {
		return Agent{}, fmt.Errorf("invalid agent frontmatter: %w", err)
	}

	config := map[string]string{}
	warnings := []string{}
	keys := make([]string, 0, len(parsed))
	for key := range parsed {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		value := parsed[key]
		config[key] = fmt.Sprint(value)
		if _, ok := knownKeys[key]; !ok {
			warnings = append(warnings, fmt.Sprintf("unknown agent key: %s", key))
		}
	}

	return Agent{
		SystemPrompt: body,
		Config:       config,
		Warnings:     warnings,
	}, nil
}
