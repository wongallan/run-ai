package cli

import (
	"fmt"
	"io"
	"strings"

	"run-ai/internal/config"
)

// Run executes the CLI command and returns an exit code.
func Run(args []string, stdout, stderr io.Writer, baseDir string) int {
	if len(args) == 0 {
		writeUsage(stderr)
		return 2
	}

	switch args[0] {
	case "-h", "--help", "help":
		writeUsage(stdout)
		return 0
	case "config":
		return runConfig(args[1:], stdout, stderr, baseDir)
	default:
		prompt := strings.TrimSpace(strings.Join(args, " "))
		if prompt == "" {
			writeUsage(stderr)
			return 2
		}
		fmt.Fprintf(stdout, "prompt: %s\n", prompt)
		return 0
	}
}

func runConfig(args []string, stdout, stderr io.Writer, baseDir string) int {
	if len(args) != 2 {
		writeUsage(stderr)
		return 2
	}
	key := strings.TrimSpace(args[0])
	value := args[1]

	if err := config.Set(baseDir, key, value); err != nil {
		fmt.Fprintf(stderr, "config error: %v\n", err)
		return 1
	}

	fmt.Fprintln(stdout, "config updated")
	return 0
}

func writeUsage(writer io.Writer) {
	fmt.Fprintln(writer, "Usage:")
	fmt.Fprintln(writer, "  rai config <key> <value>")
	fmt.Fprintln(writer, "  rai <prompt>")
}
