package skills

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const defaultTimeout = 30 * time.Second

// ExecResult holds the output of a skill script execution.
type ExecResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

// Execute runs a script from a skill's scripts/ directory.
// The script is resolved relative to the skill directory. The working directory
// for execution is workDir (typically the project root).
func Execute(skill Skill, scriptPath string, args []string, workDir string) (ExecResult, error) {
	fullPath := filepath.Join(skill.Dir, scriptPath)

	// Verify the script exists and is within the skill directory.
	absScript, err := filepath.Abs(fullPath)
	if err != nil {
		return ExecResult{}, fmt.Errorf("resolving script path: %w", err)
	}
	absSkillDir, err := filepath.Abs(skill.Dir)
	if err != nil {
		return ExecResult{}, fmt.Errorf("resolving skill directory: %w", err)
	}
	if !strings.HasPrefix(absScript, absSkillDir) {
		return ExecResult{}, fmt.Errorf("script path %q escapes skill directory", scriptPath)
	}

	ctx, cancel := context.WithTimeout(context.Background(), defaultTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, absScript, args...)
	cmd.Dir = workDir

	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Run()
	result := ExecResult{
		Stdout: stdout.String(),
		Stderr: stderr.String(),
	}

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
		} else {
			return result, fmt.Errorf("executing skill script: %w", err)
		}
	}

	return result, nil
}
