# Configuration Management

## Job Story

When I need to customize the behavior of the CLI tool,
I want to set configuration values locally or via environment,
So that I can adapt the tool to my specific needs and environment without global state.

## Context

Different projects may need different LLM endpoints, API keys, or default parameters. Users want project-specific configuration that doesn't interfere with other projects. They also want to avoid global configuration files that can cause conflicts or make the tool behavior unpredictable across different directories.

## Acceptance Criteria

### Local Configuration Only

**Job:** Set project-specific configuration

- Configuration is stored in `.rai/` subdirectory of the current working directory
- No global configuration file or directory (e.g., no `~/.rai/` or `~/.config/rai/`)
- Each project can have its own isolated configuration
- Configuration does not leak between projects

### Configuration Sources (Precedence Order)

**Job:** Understand which configuration applies

1. Command line arguments (highest priority)
2. Agent file YAML parameters
3. Local `.rai/` configuration files
4. Environment variables
5. Built-in defaults (lowest priority)

### Configuration Management CLI

**Job:** Set configuration values from command line

- Set a config value: `rai config key value`
  - Example: `rai config endpoint https://api.openai.com/v1`
  - Example: `rai config model gpt-4`
- Values are stored in `.rai/config` or similar local file
- The `.rai/` directory is created automatically if it doesn't exist

### Minimal Configuration Surface

**Job:** Configure only what's necessary

Essential configuration parameters:
- LLM endpoint URL
- API authentication (key, token, or auth method)
- Default model name
- Optional: default temperature, max tokens, etc.

Non-essential features should not require configuration.

### Environment Variables

**Job:** Configure via environment for CI/CD or containers

- All configuration values can be set via environment variables
- Environment variable naming convention: `RAI_ENDPOINT`, `RAI_API_KEY`, `RAI_MODEL`, etc.
- Environment variables are overridden by local config and command line args

## Success Metrics

- Users can configure a new project in < 2 minutes
- Configuration is self-documenting (clear parameter names)
- No confusion about which config value is being used
- Easy to share project configuration via git (`.rai/config` in repo)

## Related Jobs

- See [LLM Provider Integration](./04-provider-integration.md) for provider-specific configuration
- See [Agent Management](./02-agent-management.md) for agent-level parameter overrides
