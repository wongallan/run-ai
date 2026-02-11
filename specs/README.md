# RAI CLI - Jobs To Be Done Specifications

This directory contains the JTBD (Jobs To Be Done) specifications for the `rai` CLI tool. Each specification focuses on a specific area of concern and describes the jobs users are trying to accomplish.

## Overview

The `rai` CLI is a lightweight command-line tool for interacting with Large Language Models (LLMs). It's designed to be simple, fast, and integrate naturally with terminal-based workflows.

## Specifications by Topic

The specifications are organized by topic of concern:

1. **[Core CLI Usage](./01-core-cli-usage.md)** - Basic command-line interaction, tool invocation, and core functionality
2. **[Agent Management](./02-agent-management.md)** - Creating, configuring, and using reusable agent files
3. **[Configuration Management](./03-configuration-management.md)** - Project-local configuration and environment variables
4. **[LLM Provider Integration](./04-provider-integration.md)** - Support for OpenAI, Anthropic, Google, and GitHub Copilot
5. **[Agent Skills](./05-agent-skills.md)** - Extending agents with callable skills following agentskills.io spec
6. **[Logging and Output](./06-logging-output.md)** - Console output, silent mode, and persistent logging

## What is JTBD?

Jobs To Be Done (JTBD) is a framework for understanding user needs by focusing on the "job" users are trying to accomplish rather than specific features. Each spec includes:

- **Job Story**: A narrative describing the user's goal
- **Context**: Background on why this job matters
- **Acceptance Criteria**: Specific requirements for the job to be done successfully
- **Success Metrics**: How we measure if the job is done well
- **Related Jobs**: Links to other relevant specifications

## Reading the Specs

Each specification is independent but may reference others for related functionality. Start with [Core CLI Usage](./01-core-cli-usage.md) for the foundational concepts, then explore other topics based on your interests.

## Implementation Notes

These specifications describe *what* users need to accomplish, not *how* to implement it. Implementation details should be guided by these jobs while considering:

- Minimal dependencies
- Fast execution
- Clear, transparent operation
- Consistent cross-platform behavior
- Standard Go idioms and best practices
