// Package skills implements Agent Skills consumption per agentskills.io.
//
// Skills are directories containing a SKILL.md file located under .rai/skills/.
// The SKILL.md frontmatter provides name and description metadata.  The body
// contains activation instructions for the LLM.
//
// This package handles discovery, metadata parsing, context formatting for
// system prompts, and script execution.
package skills

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Skill holds parsed metadata and instructions from a single SKILL.md file.
type Skill struct {
	Name        string // required; lowercase alphanumeric + hyphens
	Description string // required; what the skill does and when to use it
	Dir         string // absolute path to the skill directory
	Body        string // markdown body (activation instructions)
}

// ParseSkillFile reads and parses a SKILL.md file at the given path.
func ParseSkillFile(path, dir string) (Skill, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Skill{}, err
	}
	return parseSkillContent(string(data), dir)
}

func parseSkillContent(content, dir string) (Skill, error) {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	if strings.HasPrefix(content, "\ufeff") {
		content = strings.TrimPrefix(content, "\ufeff")
	}

	if !strings.HasPrefix(content, "---\n") {
		return Skill{}, errors.New("SKILL.md missing required YAML frontmatter")
	}

	lines := strings.Split(content, "\n")
	end := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			end = i
			break
		}
	}
	if end == -1 {
		return Skill{}, errors.New("SKILL.md frontmatter missing closing delimiter")
	}

	yamlBlock := strings.Join(lines[1:end], "\n")
	body := strings.Join(lines[end+1:], "\n")
	body = strings.TrimPrefix(body, "\n")

	var fm struct {
		Name        string `yaml:"name"`
		Description string `yaml:"description"`
	}
	if err := yaml.Unmarshal([]byte(yamlBlock), &fm); err != nil {
		return Skill{}, fmt.Errorf("invalid SKILL.md frontmatter: %w", err)
	}

	if fm.Name == "" {
		return Skill{}, errors.New("SKILL.md missing required 'name' field")
	}
	if fm.Description == "" {
		return Skill{}, errors.New("SKILL.md missing required 'description' field")
	}

	return Skill{
		Name:        fm.Name,
		Description: fm.Description,
		Dir:         dir,
		Body:        body,
	}, nil
}

// FormatContext builds XML describing available skills for injection into
// system prompts, following the agentskills.io recommendation.
func FormatContext(skills []Skill) string {
	if len(skills) == 0 {
		return ""
	}

	// Sort for deterministic output.
	sorted := make([]Skill, len(skills))
	copy(sorted, skills)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })

	var b strings.Builder
	b.WriteString("<available_skills>\n")
	for _, s := range sorted {
		b.WriteString("  <skill>\n")
		b.WriteString(fmt.Sprintf("    <name>%s</name>\n", s.Name))
		b.WriteString(fmt.Sprintf("    <description>%s</description>\n", s.Description))
		b.WriteString(fmt.Sprintf("    <location>%s/SKILL.md</location>\n", s.Dir))
		b.WriteString("  </skill>\n")
	}
	b.WriteString("</available_skills>")
	return b.String()
}

// FormatList returns a human-readable listing of skills for `rai skills list`.
func FormatList(skills []Skill) string {
	if len(skills) == 0 {
		return "no skills found"
	}

	sorted := make([]Skill, len(skills))
	copy(sorted, skills)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })

	var b strings.Builder
	for i, s := range sorted {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(fmt.Sprintf("%s\n  %s\n  %s", s.Name, s.Description, s.Dir))
	}
	return b.String()
}
