## Build & Run

Minimal steps to build and run the project locally.

- **Build:**: `go build -ldflags "-s -w" ./...`
- **Run (dev):**: `go run ./cmd/rai` or `go run ./cmd/rai -- help`
- **Cross-platform:**: standard Go build supports Windows/macOS/Linux.

## Validation

Run these to get immediate feedback after changes.

- **Tests:**: `go test ./...`
- **Typecheck:**: `go vet ./...` (or `staticcheck` if available)
- **Lint:**: `golangci-lint run` (if configured)

## Operational Notes

- **Single-binary:**: repo targets a single static-like binary (see `cmd/rai`).
- **Config sources:**: precedence is CLI > agent YAML > .rai/config > env (RAI_*) > defaults.
- **Runtime dirs:**: local state and logs live under `.rai/` (e.g. `.rai/log/`, `.rai/skills/`, `.rai/copilot-token`).
- **Providers supported:**: OpenAI-compatible (Responses API), Anthropic, Google (Gemini), GitHub Copilot (enterprise & cloud).
- **Pre-commit hooks:**: mixed-line-ending (CRLF) and gofmt hooks reformat new files; first commit often fails — re-stage with `git add -A` and commit again.
- **Copilot auth:**: token stored in `.rai/copilot-token`; CLI injects it into merged config before provider resolution.

## Codebase Patterns

- **Commands:**: `cmd/rai` houses the CLI entrypoint.
- **Packages:**: `internal/config`, `internal/agent`, `internal/provider`, `internal/skills`, `internal/output`, `internal/session`.
- **Skills:**: consumption-only; discovered under `.rai/skills/` and executed by the runner.
- **Provider routing:**: `Resolve()` in `provider.go` picks provider by explicit `provider` key or endpoint URL heuristics.
- **Copilot API:**: Chat API (`/chat/completions`) for most models; Responses API (`/responses`) for GPT-5+ (except gpt-5-mini). Selection via `shouldUseResponsesAPI()`.
- **Error normalization:**: all provider HTTP errors → `ProviderError` with status code, message, and actionable guidance string.
