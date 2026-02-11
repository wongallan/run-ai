# Logging and Output

## Job Story

When I run AI queries and commands,
I want to see what's happening in real-time and optionally keep records,
So that I can debug issues, audit actions, and understand the AI's reasoning.

## Context

Transparency is critical when AI agents execute commands or make decisions. Users need to see the AI's reasoning, the commands being run, and their outputs. For debugging, auditing, or learning purposes, users may also want to keep persistent logs of their sessions.

## Acceptance Criteria

### Console Output (Default Behavior)

**Job:** See what the AI is doing in real-time

By default, all of the following are printed to the console:
- AI reasoning and thought process
- Terminal commands being executed
- Terminal command outputs
- Skill invocations (if skills are used)
- Skill outputs
- Errors and warnings

Output should be:
- Clearly formatted and readable
- Distinguishable by type (AI text, commands, output, etc.)
- Streamed in real-time as it happens
- Not buffered excessively

### Silent Mode

**Job:** Suppress output for automation

- Flag: `rai -silent "your query"`
- Suppresses all reasoning, commands, and outputs
- Only final response is shown
- Useful for scripts and automation where you only care about the result
- Errors and critical warnings still shown

### Log Files

**Job:** Keep a record of AI interactions

- Flag: `rai -log "your query"`
- Everything that goes to console also goes to a log file
- Log files stored in `.rai/log/` directory
- Log filename format: `rai-log-YYYYMMDD.HHMMSS.log`
  - Example: `rai-log-20240315.143022.log` (March 15, 2024 at 14:30:22)

### Log File Format

**Job:** Review and audit past sessions

Log file structure:
```
=== RAI Session Log ===
Started: 2024-03-15 14:30:22

--- Command Line Arguments ---
agent: ./agents/code-reviewer.md
log: true
model: gpt-4

--- Agent File ---
[full agent file content if used]

--- User Prompt ---
[user's prompt]

--- Session Log ---
[timestamp] [AI] [reasoning text]
[timestamp] [CMD] [command being executed]
[timestamp] [OUT] [command output]
[timestamp] [AI] [more reasoning]
...
```

Each log entry is timestamped with format: `[YYYY-MM-DD HH:MM:SS.mmm]`

### Combined Flags

**Job:** Log without console output

- Both flags can be used together: `rai -silent -log "query"`
- Logs everything to file but shows minimal console output
- Useful for background tasks that need audit trail

### Log Management

**Job:** Manage log files

- Logs are never automatically deleted
- Users can manually delete old logs from `.rai/log/`
- Each session creates a new log file (no appending to existing logs)
- Large outputs are not truncated in logs

## Success Metrics

- Users can understand what happened by reading logs
- Logs are complete enough for debugging
- Console output provides good real-time feedback
- Silent mode works reliably for automation
- Log files are parseable by both humans and tools

## Related Jobs

- See [Core CLI Usage](./01-core-cli-usage.md) for basic command execution
- See [Agent Skills](./05-agent-skills.md) for skill execution logging
- See [Configuration Management](./03-configuration-management.md) for log directory location
