# Core CLI Usage

## Job Story

When I need to quickly interact with an LLM from my terminal,
I want to run a lightweight CLI command,
So that I can get AI assistance without leaving my command line environment.

## Context

Developers and power users frequently work in terminal environments and need quick access to AI assistance for coding, problem-solving, and automation tasks. They want a tool that is fast, lightweight, and integrates naturally with their existing workflow.

## Acceptance Criteria

### Basic Invocation

**Job:** Run a simple AI query from the command line

- The command line tool is named `rai`
- Users can invoke it with a simple prompt: `rai "what is the capital of France?"`
- The tool should respond quickly without heavy initialization
- Output is printed directly to the console

### Terminal-Only Tool Support

**Job:** Execute commands and see results in my workflow

- The tool only supports the terminal as a tool (no web browsing, file system operations, etc.)
- When the LLM suggests terminal commands, they can be executed
- Command execution and output are shown in the console
- The interaction is transparent and traceable

### Minimal Dependencies

**Job:** Install and use the tool without complex setup

- The tool should be a single binary with minimal dependencies
- No complex installation process or configuration required to get started
- Works immediately after installation with sensible defaults

## Success Metrics

- Time from command invocation to first response < 2 seconds
- Installation process takes < 1 minute
- Users can complete their first query without reading documentation

## Related Jobs

- See [Agent Management](./02-agent-management.md) for using predefined agent configurations
- See [Configuration Management](./03-configuration-management.md) for customizing behavior
- See [Logging and Output](./06-logging-output.md) for controlling output verbosity
