# Implementation Plan

This plan translates the JTBD specs into a Go implementation. It assumes a single-binary build, minimal dependencies, and red/green TDD.

## Current Blockers

None.

## Goals and Constraints

- Single static-ish binary (standard Go build; no external runtime services).
- Minimal dependencies (standard library first; small, well-vetted libs only when necessary).
- Cross-platform behavior (Windows, macOS, Linux).
- Streaming output by default, silent/logging modes per spec.
- Providers: OpenAI-compatible (Responses API), Anthropic, Google, GitHub Copilot.
- GitHub Copilot must support GitHub.com and GitHub Enterprise.
- Skills are consumption-only (discover, list, execute), no creation/sharing UX.
- Config file format: simple key/value pairs using `key = "value"`.

## Architecture Overview

- cmd/rai
  - main.go (argument parsing, command dispatch)
- internal/cli
  - flags.go (CLI flags parsing)
  - commands.go (config, skills list)
- internal/config
  - config.go (load/merge precedence)
  - env.go (RAI_ env parsing)
  - files.go (.rai/config read/write)
- internal/agent
  - agent.go (agent file parsing, YAML frontmatter)
- internal/provider
  - provider.go (provider interface, routing)
  - openai_compat.go (Responses API client)
  - anthropic.go (Messages API client)
  - google.go (Gemini client)
  - copilot/
    - auth.go (device flow, enterprise support)
    - client.go (headers, endpoints, streaming)
    - models.go (model registry and selection)
- internal/skills
  - discover.go (scan .rai/skills)
  - execute.go (invoke skill, return output)
- internal/output
  - console.go (streaming output)
  - log.go (log file writer)
- internal/session
  - runner.go (prompt assembly, tool execution loop)

## Dependencies (Minimal)

- gopkg.in/yaml.v3 (YAML frontmatter parsing)
- Any HTTP client, JSON parsing from stdlib (net/http, encoding/json).

## TDD Strategy (Red/Green)

- Each bullet below is a test-first unit or integration slice.
- For every feature:
  - Red: write test against the expected behavior from the spec.
  - Green: implement minimal code to pass.
  - Refactor: keep interfaces clean, avoid extra deps.

## Milestones and Tasks

### 1) Project Skeleton and CLI Entry

- [x] Red: CLI invokes `rai "prompt"` and prints mock output.
- [x] Green: minimal main, parse args, route to runner.
- [x] Red: `rai config key value` updates local config file.
- [x] Green: implement config command and file creation.

### 2) Configuration and Precedence

- [x] Red: precedence order is enforced (CLI > agent YAML > .rai/config > env > defaults).
- [x] Green: config loader merges sources correctly.
- [x] Red: env variables map to config keys (RAI_ENDPOINT, RAI_API_KEY, RAI_MODEL, etc.).
- [x] Green: implement env parsing.

### 3) Agent File Parsing

Note: Known keys list is limited; warn on unknown keys.

- [x] Red: markdown-only agent file becomes system prompt.
- [x] Red: YAML frontmatter merges into config and warnings on unknown keys.
- [x] Green: parse frontmatter, merge, preserve markdown body.

### 4) Output and Logging

- [x] Red: default output includes reasoning, commands, outputs.
- [x] Red: `-silent` prints only final response but errors still show.
- [x] Red: `-log` writes full session log to .rai/log/.
- [x] Green: implement console/log writers with shared sink.
- [x] Red: `-silent -log` combined mode logs everything, console shows only final + errors.
- [x] Green: CLI flag parsing for `-silent`, `-log`, `--agent`, `help`.
- [x] Green: log file naming format `rai-log-YYYYMMDD.HHMMSS.log`.
- [x] Green: log header with session metadata (args, agent, prompt, timestamps).

### 5) Skills (Consumption-Only)

- [x] Red: `rai skills list` lists skills under .rai/skills/.
- [x] Red: tool discovery ignores global locations.
- [x] Green: implement discovery and rendering.
- [x] Green: SKILL.md frontmatter parsing per agentskills.io (name, description required).
- [x] Green: FormatContext generates XML for system prompt injection.
- [x] Green: FormatList generates human-readable listing.
- [x] Red: when provider requests skill execution, invoke and return output.
- [x] Green: implement skill execution runtime and error handling.
- [x] Fix: updated spec 05-agent-skills.md to use SKILL.md (agentskills.io) instead of skill.yaml.

### 6) Provider Core Abstraction

- [x] Red: provider selected by explicit config or endpoint heuristics.
- [x] Green: provider interface with streaming and non-streaming support.
- [x] Green: Resolve() function with heuristic-based provider selection.
- [x] Green: CollectStream helper for consuming streaming channels.
- [x] Green: Sentinel errors (ErrNoProvider, ErrAuthRequired, ErrModelRequired).

### 7) OpenAI-Compatible (Responses API)

- [x] Red: requests sent to /v1/responses with correct body fields.
- [x] Red: streaming events are mapped to output events.
- [x] Green: implement client with streaming and non-streaming handling.
- [x] Green: tool call support in requests and responses.

### 8) Anthropic (Messages API)

- [x] Red: requests sent to /v1/messages with correct headers and body.
- [x] Red: streaming events render in order.
- [x] Green: implement client with system message separation, anthropic-version header.

### 9) Google (Gemini)

- [x] Red: requests to generateContent with API key auth.
- [x] Green: implement client and streaming conversion (JSON array format).
- [x] Green: role mapping (assistant → model), system instruction support.

### 10) GitHub Copilot Provider

- [ ] Red: OAuth device flow works for GitHub.com.
- [ ] Red: OAuth device flow works for GitHub Enterprise with enterprise domain normalization.
- [ ] Red: auth data stored with enterpriseUrl and provider id.
- [ ] Green: implement auth flow per [specs/07-opencode-github-implementation.md](specs/07-opencode-github-implementation.md).
- [ ] Red: request headers include Authorization, User-Agent, Openai-Intent, x-initiator, and vision detection.
- [ ] Green: implement request wrapper.
- [ ] Red: chat vs responses selection uses Copilot model routing rules.
- [ ] Green: implement model routing and model registry.
- [ ] Red: responses output and streaming conversion match Copilot specifics.
- [ ] Green: implement parsing and streaming logic.

### 11) End-to-End Runner

- [x] Red: end-to-end prompt with provider results in expected output.
- [x] Red: tool calls are executed, results fed back to provider.
- [x] Green: implement prompt assembly and loop.
- [x] Green: system prompt + skill context injection.
- [x] Green: streaming output through sink.
- [x] Green: CLI wired to session runner with provider resolution fallback.

### 12) Hardening and UX

- [ ] Red: clear error messages for auth, missing models, and rate errors.
- [ ] Green: normalize provider errors and surface actionable guidance.
- [ ] Red: CLI exit codes reflect failures.
- [ ] Green: consistent error handling and exit codes.

## Testing Matrix

- Unit tests: config merge, agent parsing, skill discovery, model routing.
- Integration tests: each provider request/response mapping (mock HTTP).
- E2E tests: CLI invocation with mocked provider server.
- Regression tests: Copilot device flow, enterprise base URL, x-initiator logic.

## Build and Release

- `go test ./...` for CI
- `go build -ldflags "-s -w"` for release binaries
- Keep dependencies pinned and minimal.

## Open Questions and Assumptions

- Tooling boundaries: spec mentions terminal-only tools while skills can do file/API ops; clarify allowed capabilities per execution path.
- "Works immediately" conflicts with provider credentials/config requirements; define expected first-run behavior and prompts.
- Provider auto-detection rules are underspecified; confirm heuristics and precedence when endpoints overlap.
- Env var mapping list is incomplete despite key/value .rai/config format; finalize supported RAI_* keys and docs.
- Whether to allow token-based Copilot auth in addition to device flow (spec allows it).
- Model list source for Copilot (static list vs periodically updated).
- Skill execution sandboxing (process isolation vs trust by local repo).
