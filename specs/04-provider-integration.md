# LLM Provider Integration

## Job Story

When I want to use different LLM providers,
I want to configure and switch between them easily,
So that I can use the best model for each task or work within my organization's constraints.

## Context

Different users have access to different LLM providers. Some use OpenAI, others use Anthropic, Google, or GitHub Copilot through their organization. The tool should support multiple providers without requiring users to change their workflow significantly.

## Acceptance Criteria

### Supported Providers

**Job:** Use my preferred LLM provider

The tool must support these providers:

1. **OpenAI-compatible APIs** (OpenAI, Azure OpenAI, etc.)
   - Uses the Responses API (`/v1/responses`)
   - Supports streaming responses
   - Standard authentication via API key in header

2. **Anthropic-compatible APIs** (Claude)
   - Uses the Messages API (`/v1/messages`)
   - Supports streaming responses
   - Authentication via `x-api-key` header

3. **Google-compatible APIs** (Gemini)
   - Uses the `generateContent` API
   - Supports streaming responses
   - Authentication via API key

4. **GitHub Copilot**
   - Most complex authentication flow
   - Implementation based on the OpenCode writeup in [07-opencode-github-implementation.md](./07-opencode-github-implementation.md)
   - Supports OAuth device flow or token-based auth
   - Must support both GitHub.com and GitHub Enterprise

### Provider Configuration

**Job:** Configure my LLM provider

- Provider is selected automatically based on endpoint URL or explicitly via parameter
- Configuration includes:
  - Endpoint URL
  - API key/authentication credentials
  - Model name (provider-specific)
  - Optional: organization ID, API version, etc.

Example configurations:
```bash
# OpenAI
rai config endpoint https://api.openai.com/v1
rai config api-key sk-...
rai config model gpt-4

# Anthropic
rai config endpoint https://api.anthropic.com
rai config api-key sk-ant-...
rai config model claude-3-opus-20240229

# Google
rai config endpoint https://generativelanguage.googleapis.com
rai config api-key AIza...
rai config model gemini-pro

# GitHub Copilot
rai config provider github-copilot
# Uses OAuth flow or GitHub token
```

When users run `rai config provider github-copilot`, the CLI should automatically
start the OAuth device flow, open the verification URL in the default browser
when possible, and persist the token on success. The CLI should only require
manual copy/paste if the platform cannot open the browser or GitHub does not
provide a complete verification URL.

### Provider Abstraction

**Job:** Switch providers without changing my workflow

- The core CLI interface remains the same regardless of provider
- Provider-specific quirks are handled internally
- Error messages are normalized across providers
- Streaming output works consistently

### GitHub Copilot Integration

**Job:** Use GitHub Copilot as my LLM provider

- Authentication flow based on [07-opencode-github-implementation.md](./07-opencode-github-implementation.md)
- Supports both OAuth device flow and token-based authentication
- Supports both GitHub.com and GitHub Enterprise
- Clear error messages when authentication fails or expires

## Success Metrics

- Users can switch between providers in < 1 minute
- Provider-specific errors are clear and actionable
- Authentication flows are reliable and well-documented
- GitHub Copilot integration stays compatible with upstream changes

## Related Jobs

- See [Configuration Management](./03-configuration-management.md) for setting provider configuration
- See [Core CLI Usage](./01-core-cli-usage.md) for basic invocation patterns
