# Agent Management

## Job Story

When I have repetitive AI tasks with specific instructions or context,
I want to define reusable agent configurations,
So that I can consistently execute tasks without retyping instructions.

## Context

Users often need to run similar AI queries with specific system prompts, constraints, or parameters. Instead of remembering and retyping these every time, they want to save agent configurations that can be invoked with a simple command.

## Acceptance Criteria

### Agent File Format

**Job:** Create and use agent configuration files

- Agent files can be stored anywhere in the filesystem
- Agent files support two formats:
  1. Markdown only: The entire file is treated as the system prompt/instructions
  2. YAML frontmatter + Markdown: YAML block (between `---` delimiters) contains parameters, followed by markdown instructions

Example YAML frontmatter format:
```yaml
---
model: gpt-4
temperature: 0.7
max-tokens: 2000
custom-param: value
---

You are a helpful coding assistant...
```

- Keys in the YAML block correspond to command line parameters
- Unknown keys in YAML should trigger a warning but not fail
- The markdown section (or entire file if no YAML) becomes the system prompt

### Agent Invocation

**Job:** Use an agent file for a query

- Users pass an agent file as a command line argument: `rai --agent ./agents/code-reviewer.md "review this code"`
- The agent configuration is loaded and merged with any additional command line arguments
- Command line arguments override agent file parameters
- The agent's system prompt is prepended to the user's query

### Agent File Discovery

**Job:** Organize and locate agent files

- Agent files can be located anywhere the user has access
- No special directory structure required for basic agent files
- Users reference agents by their file path (relative or absolute)

## Success Metrics

- Users can create and use a new agent file in < 5 minutes
- Agent files are portable and shareable
- Clear error messages when agent files are malformed

## Related Jobs

- See [Agent Skills](./05-agent-skills.md) for extending agents with callable skills
- See [Configuration Management](./03-configuration-management.md) for parameter precedence
