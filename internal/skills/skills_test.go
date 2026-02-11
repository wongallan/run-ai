package skills

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// --- SKILL.md parsing tests ---

func TestParseSkillContent(t *testing.T) {
	content := "---\nname: pdf-processing\ndescription: Extracts text from PDFs.\n---\nUse this skill when you need to work with PDF files.\n"
	skill, err := parseSkillContent(content, "/skills/pdf-processing")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if skill.Name != "pdf-processing" {
		t.Fatalf("name = %q, want pdf-processing", skill.Name)
	}
	if skill.Description != "Extracts text from PDFs." {
		t.Fatalf("description = %q", skill.Description)
	}
	if skill.Dir != "/skills/pdf-processing" {
		t.Fatalf("dir = %q", skill.Dir)
	}
	if !strings.Contains(skill.Body, "Use this skill when") {
		t.Fatalf("body = %q", skill.Body)
	}
}

func TestParseSkillMissingFrontmatter(t *testing.T) {
	_, err := parseSkillContent("No frontmatter here.", "/s")
	if err == nil {
		t.Fatal("expected error for missing frontmatter")
	}
}

func TestParseSkillMissingName(t *testing.T) {
	content := "---\ndescription: Some desc.\n---\nBody.\n"
	_, err := parseSkillContent(content, "/s")
	if err == nil || !strings.Contains(err.Error(), "name") {
		t.Fatalf("expected missing name error, got %v", err)
	}
}

func TestParseSkillMissingDescription(t *testing.T) {
	content := "---\nname: test-skill\n---\nBody.\n"
	_, err := parseSkillContent(content, "/s")
	if err == nil || !strings.Contains(err.Error(), "description") {
		t.Fatalf("expected missing description error, got %v", err)
	}
}

func TestParseSkillMissingClosingDelimiter(t *testing.T) {
	content := "---\nname: test\ndescription: test\n"
	_, err := parseSkillContent(content, "/s")
	if err == nil || !strings.Contains(err.Error(), "closing delimiter") {
		t.Fatalf("expected closing delimiter error, got %v", err)
	}
}

func TestParseSkillBOM(t *testing.T) {
	content := "\ufeff---\nname: bom-skill\ndescription: Has BOM.\n---\nBody.\n"
	skill, err := parseSkillContent(content, "/s")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if skill.Name != "bom-skill" {
		t.Fatalf("name = %q", skill.Name)
	}
}

func TestParseSkillCRLF(t *testing.T) {
	content := "---\r\nname: crlf-skill\r\ndescription: CRLF.\r\n---\r\nBody.\r\n"
	skill, err := parseSkillContent(content, "/s")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if skill.Name != "crlf-skill" {
		t.Fatalf("name = %q", skill.Name)
	}
}

// --- Discovery tests ---

func TestDiscoverNoDir(t *testing.T) {
	dir := t.TempDir()
	skills, warnings, err := Discover(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(skills) != 0 {
		t.Fatalf("expected no skills, got %d", len(skills))
	}
	if len(warnings) != 0 {
		t.Fatalf("expected no warnings, got %v", warnings)
	}
}

func TestDiscoverEmptyDir(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".rai", "skills"), 0o755)
	skills, _, err := Discover(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(skills) != 0 {
		t.Fatalf("expected no skills, got %d", len(skills))
	}
}

func TestDiscoverValidSkills(t *testing.T) {
	dir := t.TempDir()
	skillsDir := filepath.Join(dir, ".rai", "skills")

	// Create two valid skills.
	for _, s := range []struct{ name, desc string }{
		{"data-analysis", "Analyzes datasets."},
		{"code-review", "Reviews code quality."},
	} {
		sdir := filepath.Join(skillsDir, s.name)
		os.MkdirAll(sdir, 0o755)
		content := "---\nname: " + s.name + "\ndescription: " + s.desc + "\n---\nInstructions.\n"
		os.WriteFile(filepath.Join(sdir, "SKILL.md"), []byte(content), 0o644)
	}

	skills, warnings, err := Discover(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", warnings)
	}
	if len(skills) != 2 {
		t.Fatalf("expected 2 skills, got %d", len(skills))
	}

	// Check both were found by name.
	names := map[string]bool{}
	for _, s := range skills {
		names[s.Name] = true
	}
	if !names["data-analysis"] || !names["code-review"] {
		t.Fatalf("expected both skills found, got %v", names)
	}
}

func TestDiscoverSkipsFilesAndMissingSKILL(t *testing.T) {
	dir := t.TempDir()
	skillsDir := filepath.Join(dir, ".rai", "skills")
	os.MkdirAll(skillsDir, 0o755)

	// Regular file in skills dir (not a directory).
	os.WriteFile(filepath.Join(skillsDir, "readme.txt"), []byte("hi"), 0o644)

	// Directory without SKILL.md.
	os.MkdirAll(filepath.Join(skillsDir, "empty-dir"), 0o755)

	skills, warnings, err := Discover(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(skills) != 0 {
		t.Fatalf("expected 0 skills, got %d", len(skills))
	}
	if len(warnings) != 0 {
		t.Fatalf("expected no warnings, got %v", warnings)
	}
}

func TestDiscoverInvalidSkillProducesWarning(t *testing.T) {
	dir := t.TempDir()
	skillsDir := filepath.Join(dir, ".rai", "skills")
	sdir := filepath.Join(skillsDir, "bad-skill")
	os.MkdirAll(sdir, 0o755)

	// SKILL.md with missing name.
	os.WriteFile(filepath.Join(sdir, "SKILL.md"), []byte("---\ndescription: oops\n---\n"), 0o644)

	skills, warnings, err := Discover(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(skills) != 0 {
		t.Fatalf("expected 0 valid skills, got %d", len(skills))
	}
	if len(warnings) != 1 {
		t.Fatalf("expected 1 warning, got %d: %v", len(warnings), warnings)
	}
	if !strings.Contains(warnings[0], "bad-skill") {
		t.Fatalf("warning should mention skill name: %s", warnings[0])
	}
}

// --- FormatContext tests ---

func TestFormatContextEmpty(t *testing.T) {
	result := FormatContext(nil)
	if result != "" {
		t.Fatalf("expected empty string for no skills, got %q", result)
	}
}

func TestFormatContextXML(t *testing.T) {
	skills := []Skill{
		{Name: "pdf-processing", Description: "Handles PDFs.", Dir: "/skills/pdf-processing"},
		{Name: "data-analysis", Description: "Analyzes data.", Dir: "/skills/data-analysis"},
	}
	xml := FormatContext(skills)

	if !strings.Contains(xml, "<available_skills>") {
		t.Fatal("missing opening tag")
	}
	if !strings.Contains(xml, "</available_skills>") {
		t.Fatal("missing closing tag")
	}
	if !strings.Contains(xml, "<name>pdf-processing</name>") {
		t.Fatal("missing pdf-processing name")
	}
	if !strings.Contains(xml, "<description>Handles PDFs.</description>") {
		t.Fatal("missing pdf-processing description")
	}
	if !strings.Contains(xml, "<location>/skills/data-analysis/SKILL.md</location>") {
		t.Fatal("missing data-analysis location")
	}

	// Verify sorted order (data-analysis before pdf-processing).
	dataIdx := strings.Index(xml, "data-analysis")
	pdfIdx := strings.Index(xml, "pdf-processing")
	if dataIdx > pdfIdx {
		t.Fatalf("expected data-analysis before pdf-processing in sorted output")
	}
}

// --- FormatList tests ---

func TestFormatListEmpty(t *testing.T) {
	result := FormatList(nil)
	if result != "no skills found" {
		t.Fatalf("expected 'no skills found', got %q", result)
	}
}

func TestFormatList(t *testing.T) {
	skills := []Skill{
		{Name: "b-skill", Description: "Desc B.", Dir: "/b"},
		{Name: "a-skill", Description: "Desc A.", Dir: "/a"},
	}
	out := FormatList(skills)
	if !strings.Contains(out, "a-skill") || !strings.Contains(out, "b-skill") {
		t.Fatalf("expected both skills, got %q", out)
	}
	// Sorted: a-skill should come first.
	if strings.Index(out, "a-skill") > strings.Index(out, "b-skill") {
		t.Fatalf("expected a-skill before b-skill")
	}
}

// --- Execute tests ---

func TestExecutePathEscape(t *testing.T) {
	skill := Skill{Name: "test", Dir: "/tmp/skill"}
	_, err := Execute(skill, "../../etc/passwd", nil, "/tmp")
	if err == nil || !strings.Contains(err.Error(), "escapes skill directory") {
		t.Fatalf("expected path escape error, got %v", err)
	}
}

func TestExecuteScript(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script test skipped on Windows")
	}

	dir := t.TempDir()
	scriptsDir := filepath.Join(dir, "scripts")
	os.MkdirAll(scriptsDir, 0o755)

	script := filepath.Join(scriptsDir, "hello.sh")
	os.WriteFile(script, []byte("#!/bin/sh\necho hello world\n"), 0o755)

	skill := Skill{Name: "test-skill", Dir: dir}
	result, err := Execute(skill, "scripts/hello.sh", nil, dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result.Stdout, "hello world") {
		t.Fatalf("expected 'hello world' in stdout, got %q", result.Stdout)
	}
	if result.ExitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", result.ExitCode)
	}
}
