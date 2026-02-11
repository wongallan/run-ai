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
- **Runtime dirs:**: local state and logs live under `.rai/` (e.g. `.rai/log/`, `.rai/skills/`).
- **Providers supported:**: OpenAI-compatible (Responses API), Anthropic, Google (Gemini), GitHub Copilot (enterprise & cloud).

## Codebase Patterns

- **Commands:**: `cmd/rai` houses the CLI entrypoint.
- **Packages:**: `internal/config`, `internal/agent`, `internal/provider`, `internal/skills`, `internal/output`, `internal/session`.
- **Skills:**: consumption-only; discovered under `.rai/skills/` and executed by the runner.
