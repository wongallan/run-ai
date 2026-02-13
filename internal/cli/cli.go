package cli

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"

	"run-ai/internal/agent"
	"run-ai/internal/config"
	"run-ai/internal/output"
	"run-ai/internal/provider"
	"run-ai/internal/session"
	"run-ai/internal/skills"
)

var copilotDeviceAuth = provider.DeviceAuth
var copilotSaveToken = provider.SaveCopilotToken

// Parsed holds parsed CLI arguments.
type Parsed struct {
	Command    string   // "config", "skills", "" (prompt mode)
	SubArgs    []string // sub-command arguments
	Prompt     string   // user prompt (prompt mode)
	PromptPath string   // --prompt-file flag
	AgentPath  string   // --agent flag
	Silent     bool     // -silent flag
	Log        bool     // -log flag
	LogLevel   string   // optional: when -log is followed by a level (e.g. DEBUG)
	ShowHelp   bool     // -h / --help / help
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
			// Support `-log DEBUG` without changing the existing `-log <prompt>` behavior.
			if i+1 < len(args) && strings.EqualFold(args[i+1], "DEBUG") {
				p.LogLevel = "DEBUG"
				i++
			}
		case "--prompt-file":
			if i+1 < len(args) {
				i++
				p.PromptPath = args[i]
			}
		case "--agent":
			if i+1 < len(args) {
				i++
				p.AgentPath = args[i]
			}
		default:
			if strings.HasPrefix(args[i], "--agent=") {
				p.AgentPath = strings.TrimPrefix(args[i], "--agent=")
			} else if strings.HasPrefix(args[i], "--prompt-file=") {
				p.PromptPath = strings.TrimPrefix(args[i], "--prompt-file=")
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
	case "copilot-login":
		p.Command = "copilot-login"
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
	case "copilot-login":
		return runCopilotLogin(parsed.SubArgs, stdout, stderr, baseDir)
	default:
		if parsed.Prompt != "" && parsed.PromptPath != "" {
			fmt.Fprintln(stderr, "prompt error: provide either a prompt string or --prompt-file, not both")
			return 2
		}
		if parsed.Prompt == "" && parsed.PromptPath == "" {
			writeUsage(stderr)
			return 2
		}
		return runPrompt(parsed, stdout, stderr, baseDir)
	}
}

// runPrompt handles the prompt command with output sink, optional agent, and logging.
func runPrompt(p Parsed, stdout, stderr io.Writer, baseDir string) int {
	if p.PromptPath != "" {
		prompt, err := loadPromptFile(p.PromptPath)
		if err != nil {
			fmt.Fprintf(stderr, "prompt error: %v\n", err)
			return 1
		}
		p.Prompt = prompt
	}

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
	if p.PromptPath != "" {
		headerArgs["prompt-file"] = p.PromptPath
	}
	if p.Silent {
		headerArgs["silent"] = "true"
	}
	if p.Log {
		headerArgs["log"] = "true"
	}
	if p.LogLevel != "" {
		headerArgs["log-level"] = p.LogLevel
	}

	sink.WriteHeader(headerArgs, ag.SystemPrompt, p.Prompt)

	// Inform user where the log file is being written.
	if logPath := sink.LogPath(); logPath != "" {
		fmt.Fprintf(stderr, "log: %s\n", logPath)
	}

	// Merge configuration: defaults < env < file < agent < cli.
	defaults := map[string]string{}
	merged, err := config.LoadMerged(baseDir, ag.Config, map[string]string{}, defaults)
	if err != nil {
		fmt.Fprintf(stderr, "config error: %v\n", err)
		return 1
	}

	// Internal-only debug hooks: allow providers to append raw HTTP JSON bodies
	// to the active session log when `-log DEBUG` is used.
	if strings.EqualFold(p.LogLevel, "DEBUG") {
		if lp := sink.LogPath(); lp != "" {
			merged["_log_level"] = "DEBUG"
			merged["_log_path"] = lp
		}
	}

	// Load stored Copilot token when provider is github-copilot and no key yet.
	provID := merged["provider"]
	if (provID == "github-copilot" || provID == "github-copilot-enterprise") &&
		merged["api-key"] == "" && merged["api_key"] == "" {
		if tok := provider.LoadCopilotToken(baseDir); tok != "" {
			merged["api-key"] = tok
		}
	}

	// Resolve provider.
	prov, err := provider.Resolve(merged)
	if err != nil {
		// No provider configured — fall back to echo mode for basic usage.
		sink.EmitFinal(fmt.Sprintf("prompt: %s", p.Prompt))
		return 0
	}

	// Discover skills.
	discovered, skillWarnings, _ := skills.Discover(baseDir)
	for _, w := range skillWarnings {
		sink.Emit(output.EventERR, w)
	}

	// Run the session.
	ctx := context.Background()
	if err := session.Run(ctx, session.Config{
		Provider:     prov,
		Sink:         sink,
		SystemPrompt: ag.SystemPrompt,
		UserPrompt:   p.Prompt,
		Skills:       discovered,
		BaseDir:      baseDir,
	}); err != nil {
		fmt.Fprintf(stderr, "session error: %v\n", err)
		return 1
	}

	return 0
}

func runConfig(args []string, stdout, stderr io.Writer, baseDir string) int {
	if len(args) != 2 {
		writeUsage(stderr)
		return 2
	}
	key := strings.TrimSpace(args[0])
	value := args[1]

	if key == "provider" && (value == "github-copilot" || value == "github-copilot-enterprise") {
		if err := configureCopilotProvider(value, stdout, stderr, baseDir); err != nil {
			fmt.Fprintf(stderr, "config error: %v\n", err)
			return 1
		}
		fmt.Fprintln(stdout, "config updated")
		return 0
	}

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

	discovered, warnings, err := skills.Discover(baseDir)
	if err != nil {
		fmt.Fprintf(stderr, "skills error: %v\n", err)
		return 1
	}
	for _, w := range warnings {
		fmt.Fprintf(stderr, "warning: %s\n", w)
	}

	fmt.Fprintln(stdout, skills.FormatList(discovered))
	return 0
}

func runCopilotLogin(args []string, stdout, stderr io.Writer, baseDir string) int {
	domain := "github.com"
	if len(args) > 0 {
		domain = args[0]
	}
	if err := authenticateCopilot(domain, stdout, stderr, baseDir); err != nil {
		return 1
	}

	// Persist provider selection.
	providerID := "github-copilot"
	if domain != "" && domain != "github.com" {
		providerID = "github-copilot-enterprise"
		_ = config.Set(baseDir, "enterprise-url", domain)
	}
	_ = config.Set(baseDir, "provider", providerID)
	return 0
}

func configureCopilotProvider(providerID string, stdout, stderr io.Writer, baseDir string) error {
	domain := "github.com"
	if providerID == "github-copilot-enterprise" {
		values, err := config.Load(baseDir)
		if err != nil {
			return err
		}
		domain = strings.TrimSpace(values["enterprise-url"])
		if domain == "" {
			return fmt.Errorf("enterprise-url must be set before configuring github-copilot-enterprise")
		}
	}

	if err := authenticateCopilot(domain, stdout, stderr, baseDir); err != nil {
		return err
	}

	if providerID == "github-copilot-enterprise" {
		_ = config.Set(baseDir, "enterprise-url", domain)
	}
	return config.Set(baseDir, "provider", providerID)
}

func authenticateCopilot(domain string, stdout, stderr io.Writer, baseDir string) error {
	domain = provider.NormalizeDomain(domain)
	if domain == "" {
		domain = "github.com"
	}

	ctx := context.Background()
	auth, err := copilotDeviceAuth(ctx, domain, stdout)
	if err != nil {
		fmt.Fprintf(stderr, "authentication failed: %v\n", err)
		return err
	}

	if err := copilotSaveToken(baseDir, auth.Token); err != nil {
		fmt.Fprintf(stderr, "saving token: %v\n", err)
		return err
	}

	fmt.Fprintln(stdout, "authenticated successfully")
	return nil
}

func writeUsage(writer io.Writer) {
	fmt.Fprintln(writer, "Usage:")
	fmt.Fprintln(writer, "  rai <prompt>")
	fmt.Fprintln(writer, "  rai --agent <file> <prompt>")
	fmt.Fprintln(writer, "  rai --prompt-file <file>")
	fmt.Fprintln(writer, "  rai -silent <prompt>")
	fmt.Fprintln(writer, "  rai -log <prompt>")
	fmt.Fprintln(writer, "  rai config <key> <value>")
	fmt.Fprintln(writer, "  rai skills list")
	fmt.Fprintln(writer, "  rai copilot-login [domain]")
}

func loadPromptFile(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("prompt file path is empty")
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("prompt file: %w", err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("prompt file %q is a directory", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("prompt file: %w", err)
	}
	if len(data) > 0 && (bytes.IndexByte(data, 0) >= 0 || !utf8.Valid(data)) {
		return "", fmt.Errorf("prompt file %q is not valid UTF-8 text", path)
	}
	return strings.TrimRight(string(data), "\n"), nil
}
