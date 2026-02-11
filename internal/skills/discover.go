package skills

import (
	"fmt"
	"os"
	"path/filepath"
)

const (
	raiDirName    = ".rai"
	skillsDirName = "skills"
	skillFileName = "SKILL.md"
)

// SkillsDir returns the path to the skills directory for a base directory.
func SkillsDir(baseDir string) string {
	return filepath.Join(baseDir, raiDirName, skillsDirName)
}

// Discover scans .rai/skills/ for valid skill directories.
// Each immediate subdirectory that contains a SKILL.md file is treated as a skill.
// Invalid or unparseable skills are collected as warnings rather than hard errors
// so that one bad skill doesn't prevent discovery of the rest.
func Discover(baseDir string) ([]Skill, []string, error) {
	dir := SkillsDir(baseDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, nil
		}
		return nil, nil, fmt.Errorf("reading skills directory: %w", err)
	}

	var skills []Skill
	var warnings []string

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}

		skillDir := filepath.Join(dir, e.Name())
		skillFile := filepath.Join(skillDir, skillFileName)

		if _, err := os.Stat(skillFile); err != nil {
			// Directory without SKILL.md — silently skip.
			continue
		}

		skill, err := ParseSkillFile(skillFile, skillDir)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("skill %s: %v", e.Name(), err))
			continue
		}

		skills = append(skills, skill)
	}

	return skills, warnings, nil
}
