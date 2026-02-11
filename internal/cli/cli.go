package cli

import (
	"fmt"
	"io"
	"strings"

	"run-ai/internal/agent"
	"run-ai/internal/config"
	"run-ai/internal/output"
)

// Parsed holds parsed CLI arguments.
type Parsed struct {
	Command   string   // "config", "skills", "" (prompt mode)
	SubArgs   []string // sub-command arguments
	Prompt    string   // user prompt (prompt mode)
	AgentPath string   // --agent flag
	Silent    bool     // -silent flag
	Log       bool     // -log flag
	ShowHelp  bool     // -h / --help / help
}

// ParseArgs separates flags from positional arguments.
func ParseArgs(args []string) Parsed {
	var p Parsed
	var positional []string

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-h", "--help", "help":
			p.ShowHelp = true
			return p
		case "-silent":
			p.Silent = true
		case "-log":
			p.Log = true
		case "--agent":
			if i+1 < len(args) {
				i++
				p.AgentPath = args[i]
			}
		default:
			if strings.HasPrefix(args[i], "--agent=") {
				p.AgentPath = strings.TrimPrefix(args[i], "--agent=")
			} else {
				positional = append(positional, args[i])
			}
		}
	}

	if len(positional) == 0 {
		return p
	}

	switch positional[0] {
	case "config":
		p.Command = "config"
		p.SubArgs = positional[1:]
	case "skills":
		p.Command = "skills"
		p.SubArgs = positional[1:]
	default:
		p.Prompt = strings.TrimSpace(strings.Join(positional, " "))
	}
	return p
}

// Run executes the CLI command and returns an exit code.
func Run(args []string, stdout, stderr io.Writer, baseDir string) int {
	if len(args) == 0 {
		writeUsage(stderr)
		return 2
	}

	parsed := ParseArgs(args)

	if parsed.ShowHelp {
		writeUsage(stdout)
		return 0
	}

	switch parsed.Command {
	case "config":
		return runConfig(parsed.SubArgs, stdout, stderr, baseDir)
	case "skills":
		return runSkills(parsed.SubArgs, stdout, stderr, baseDir)
	default:
		if parsed.Prompt == "" {
			writeUsage(stderr)
			return 2
		}
		return runPrompt(parsed, stdout, stderr, baseDir)
	}
}

// runPrompt handles the prompt command with output sink, optional agent, and logging.
func runPrompt(p Parsed, stdout, stderr io.Writer, baseDir string) int {
	sink, err := output.NewSink(output.Options{
		Silent:  p.Silent,
		Log:     p.Log,
		BaseDir: baseDir,
		Console: stdout,
	})
	if err != nil {
		fmt.Fprintf(stderr, "output error: %v\n", err)
		return 1
	}
	defer sink.Close()

	// Load agent if specified.
	var ag agent.Agent
	if p.AgentPath != "" {
		ag, err = agent.ParseFile(p.AgentPath)
		if err != nil {
			fmt.Fprintf(stderr, "agent error: %v\n", err)
			return 1
		}
		for _, w := range ag.Warnings {
			sink.Emit(output.EventERR, w)
		}
	}

	// Build log header arguments.
	headerArgs := map[string]string{}
	if p.AgentPath != "" {
		headerArgs["agent"] = p.AgentPath
	}
	if p.Silent {
		headerArgs["silent"] = "true"
	}
	if p.Log {
		headerArgs["log"] = "true"
	}

	sink.WriteHeader(headerArgs, ag.SystemPrompt, p.Prompt)

	// Inform user where the log file is being written.
	if logPath := sink.LogPath(); logPath != "" {
		fmt.Fprintf(stderr, "log: %s\n", logPath)
	}

	// TODO(milestone-11): send prompt to provider via session runner.
	// For now emit the prompt so we have observable output.
	sink.EmitFinal(fmt.Sprintf("prompt: %s", p.Prompt))
	return 0
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

func runSkills(args []string, stdout, stderr io.Writer, baseDir string) int {
	if len(args) == 0 || args[0] != "list" {
		writeUsage(stderr)
		return 2
	}
	// TODO(milestone-5): implement skills discovery.
	fmt.Fprintln(stdout, "no skills found")
	return 0
}

func writeUsage(writer io.Writer) {
	fmt.Fprintln(writer, "Usage:")
	fmt.Fprintln(writer, "  rai <prompt>")
	fmt.Fprintln(writer, "  rai --agent <file> <prompt>")
	fmt.Fprintln(writer, "  rai -silent <prompt>")
	fmt.Fprintln(writer, "  rai -log <prompt>")
	fmt.Fprintln(writer, "  rai config <key> <value>")
	fmt.Fprintln(writer, "  rai skills list")
}
