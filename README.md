# run-ai (rai)

`rai` is a minimal, fast CLI for running LLM prompts from your terminal. It is a single-binary Go tool that keeps configuration local to your repo, supports multiple providers, and can optionally load agent files and skills.

For the full JTBD specs and implementation notes, see [specs/README.md](specs/README.md).

## Contents

- [Quick start](#quick-start)
- [Install and build](#install-and-build)
- [How it works](#how-it-works)
- [CLI usage](#cli-usage)
- [Configuration](#configuration)
- [Agents](#agents)
- [Skills](#skills)
- [Providers](#providers)
- [Logging and output](#logging-and-output)
- [Directory layout](#directory-layout)
- [Troubleshooting](#troubleshooting)

## Quick start

```bash
rai "explain this error message"
rai --agent ./agents/code-reviewer.md "review this diff"
rai -silent -log "summarize these files"
```

## Install and build

Build a local binary:

```bash
go build -ldflags "-s -w" ./...
```

Run in dev mode:

```bash
go run ./cmd/rai
go run ./cmd/rai -- help
```

## How it works

At a high level:

1. `rai` collects configuration from (highest to lowest): CLI flags, agent YAML frontmatter, local `.rai/config`, environment variables, then defaults.
2. It selects a provider based on explicit `provider` config or endpoint heuristics.
3. It builds the model request (system prompt + user prompt, optional tools/skills).
4. It streams output to the console and optionally to a log file.
5. If the model calls a skill, the skill is executed and results are returned to the model.

The tool is terminal-only: no browsing or file system tools are built-in. Any additional capabilities must be provided via skills.

## CLI usage

Basic invocation:

```bash
rai "your prompt here"
```

Use an agent file:

```bash
rai --agent ./path/to/agent.md "your prompt"
```

Silent mode and logging:

```bash
rai -silent "quiet mode"
rai -log "save to log file"
rai -silent -log "quiet but logged"
```

Config from the CLI:

```bash
rai config endpoint https://api.openai.com/v1
rai config api-key sk-...
rai config model gpt-4
```

List skills:

```bash
rai skills list
```

## Configuration

Configuration is always local to the current working directory.

### Precedence order

1. Command line arguments
2. Agent file YAML parameters
3. Local `.rai/config`
4. Environment variables (`RAI_*`)
5. Built-in defaults

### Local config

Use `rai config key value` to set values. The `.rai/` directory is created if missing.

Common keys:

- `endpoint`
- `api-key`
- `model`
- `provider` (optional explicit provider override)
- `temperature`, `max-tokens` (optional)

### Environment variables

All config values can be provided via `RAI_*` env vars:

```bash
export RAI_ENDPOINT=https://api.openai.com/v1
export RAI_API_KEY=sk-...
export RAI_MODEL=gpt-4
```

## Agents

Agent files are reusable system prompts stored anywhere on disk.

Supported formats:

### Markdown only

```markdown
You are a helpful coding assistant...
```

### YAML frontmatter + Markdown

```yaml
---
model: gpt-4
temperature: 0.7
max-tokens: 2000
custom-param: value
---

You are a helpful coding assistant...
```

Notes:

- YAML keys map to CLI parameters.
- Unknown keys warn but do not fail.
- CLI flags always override agent YAML settings.

## Skills

Skills extend `rai` using the agentskills.io specification. This tool only consumes skills; it does not create or publish them.

Key points:

- Skills are discovered only in `.rai/skills/`.
- The model can call skills exposed by the local skill registry.
- Skill invocations and outputs are logged unless `-silent` is used.
- Skills are optional; `rai` works without any skills present.

Example layout:

```
.rai/skills/
	read-file/
		SKILL.md
		scripts/execute.sh
	make-api-call/
		SKILL.md
		scripts/execute.py
```

## Providers

`rai` supports multiple providers with a consistent CLI experience:

### OpenAI-compatible

- Endpoint: `/v1/responses`
- Auth: API key

```bash
rai config endpoint https://api.openai.com/v1
rai config api-key sk-...
rai config model gpt-4
```

### Anthropic (Claude)

- Endpoint: `/v1/messages`
- Auth: `x-api-key`

```bash
rai config endpoint https://api.anthropic.com
rai config api-key sk-ant-...
rai config model claude-3-opus-20240229
```

### Google (Gemini)

- Endpoint: `generateContent`
- Auth: API key

```bash
rai config endpoint https://generativelanguage.googleapis.com
rai config api-key AIza...
rai config model gemini-pro
```

### GitHub Copilot (cloud and enterprise)

- Auth: OAuth device flow or token-based auth
- Supports GitHub.com and GitHub Enterprise
- Model routing uses chat vs responses API depending on the model (GPT-5+ uses Responses API except `gpt-5-mini`)

```bash
rai config provider github-copilot
```

For deep implementation notes, see [specs/07-opencode-github-implementation.md](specs/07-opencode-github-implementation.md).

## Logging and output

Default output shows AI reasoning, commands, and command output in real time.

Flags:

- `-silent` hides reasoning and command output; only final response shows.
- `-log` writes a full session log to `.rai/log/`.

Log file format:

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
[user prompt]

--- Session Log ---
[2024-03-15 14:30:22.001] [AI] [reasoning text]
[2024-03-15 14:30:22.050] [CMD] [command being executed]
[2024-03-15 14:30:22.100] [OUT] [command output]
```

## Directory layout

```
project/
	.rai/
		config
		skills/
			<skill-name>/
				SKILL.md
				scripts/
					execute.sh
		log/
			rai-log-YYYYMMDD.HHMMSS.log
	agents/
		code-reviewer.md
```

## Troubleshooting

- If you see provider auth errors, confirm `endpoint`, `api-key`, and `model` are set and that CLI flags or agent YAML are not overriding them.
- For Copilot, remove stale token data in `.rai/copilot-token` and re-auth.
- If skills are not detected, verify they are under `.rai/skills/` and follow the agentskills.io spec.
- If output is noisy in scripts, use `-silent` or `-silent -log`.
