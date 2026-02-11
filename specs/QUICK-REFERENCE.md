# RAI CLI - Quick Reference

This quick reference extracts key technical details from the JTBD specifications.

## Command Line Interface

### Basic Usage
```bash
rai "your prompt here"
rai --agent ./path/to/agent.md "your prompt"
rai -silent "quiet mode"
rai -log "save to log file"
rai -silent -log "quiet but logged"
```

### Configuration Commands
```bash
rai config key value
rai config endpoint https://api.openai.com/v1
rai config api-key sk-...
rai config model gpt-4
```

### Skills Management
```bash
rai skills list
```

## Configuration Precedence

1. Command line arguments (highest)
2. Agent file YAML parameters
3. Local `.rai/` configuration
4. Environment variables
5. Built-in defaults (lowest)

## Directory Structure

```
project/
├── .rai/
│   ├── config              # Local configuration
│   ├── skills/             # Agent skills (agentskills.io spec)
│   │   ├── skill-name/
│   │   │   ├── skill.yaml
│   │   │   └── execute.sh
│   │   └── ...
│   └── log/                # Session logs (with -log flag)
│       ├── rai-log-20240315.143022.log
│       └── ...
└── agents/                 # Agent files (can be anywhere)
    ├── code-reviewer.md
    └── ...
```

## Agent File Format

### Markdown Only
```markdown
You are a helpful coding assistant specialized in Python...
```

### YAML Frontmatter + Markdown
```yaml
---
model: gpt-4
temperature: 0.7
max-tokens: 2000
---

You are a helpful coding assistant specialized in Python...
```

## Supported LLM Providers

### OpenAI-Compatible
- **API**: `/v1/chat/completions`
- **Auth**: API key in header
- **Config Example**:
  ```bash
  rai config endpoint https://api.openai.com/v1
  rai config api-key sk-...
  rai config model gpt-4
  ```

### Anthropic (Claude)
- **API**: `/v1/messages`
- **Auth**: `x-api-key` header
- **Config Example**:
  ```bash
  rai config endpoint https://api.anthropic.com
  rai config api-key sk-ant-...
  rai config model claude-3-opus-20240229
  ```

### Google (Gemini)
- **API**: `generateContent`
- **Auth**: API key
- **Config Example**:
  ```bash
  rai config endpoint https://generativelanguage.googleapis.com
  rai config api-key AIza...
  rai config model gemini-pro
  ```

### GitHub Copilot
- **Auth**: OAuth device flow or token (from https://github.com/anomalyco/opencode)
- **Config Example**:
  ```bash
  rai config provider github-copilot
  ```

## Environment Variables

All configuration values can be set via environment variables:
```bash
export RAI_ENDPOINT=https://api.openai.com/v1
export RAI_API_KEY=sk-...
export RAI_MODEL=gpt-4
```

## Log File Format

Filename: `rai-log-YYYYMMDD.HHMMSS.log`

Structure:
```
=== RAI Session Log ===
Started: 2024-03-15 14:30:22

--- Command Line Arguments ---
agent: ./agents/code-reviewer.md
log: true
model: gpt-4

--- Agent File ---
[full agent file content]

--- User Prompt ---
[user's prompt]

--- Session Log ---
[2024-03-15 14:30:22.001] [AI] [reasoning text]
[2024-03-15 14:30:22.050] [CMD] [command being executed]
[2024-03-15 14:30:22.100] [OUT] [command output]
...
```

## Key Principles

1. **No Global Config** - All configuration is local (`.rai/`) or environment variables
2. **Terminal Only** - Only supports terminal commands, no other tools
3. **Local Skills** - Skills only loaded from `.rai/skills/`
4. **Transparent** - All operations visible in console unless `-silent`
5. **Lightweight** - Minimal dependencies, fast startup
6. **Standards-Based** - Follows agentskills.io for skills, standard provider APIs

## For More Details

See the full JTBD specifications in the `specs/` directory:
- [01-core-cli-usage.md](./01-core-cli-usage.md)
- [02-agent-management.md](./02-agent-management.md)
- [03-configuration-management.md](./03-configuration-management.md)
- [04-provider-integration.md](./04-provider-integration.md)
- [05-agent-skills.md](./05-agent-skills.md)
- [06-logging-output.md](./06-logging-output.md)
