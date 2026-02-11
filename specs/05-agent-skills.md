# Agent Skills

## Job Story

When I want my AI agent to perform specialized tasks,
I want to provide it with callable skills,
So that it can execute complex operations beyond simple terminal commands.

## Context

While the core tool only supports terminal commands, the Agent Skills specification (https://agentskills.io/integrate-skills) provides a standardized way to extend agent capabilities with custom skills. This allows agents to perform file operations, API calls, or other specialized tasks while maintaining a consistent interface.

## Acceptance Criteria

### Skills Directory

**Job:** Organize and discover skills

- Skills are stored in `.rai/skills/` directory
- Only the `.rai/skills/` directory is scanned for skills
- Skills are automatically discovered and made available to agents
- Skills follow the https://agentskills.io/integrate-skills specification

### Skills Integration

**Job:** Use skills in agent interactions

- Skills are exposed to the LLM according to the agentskills.io spec
- The LLM can discover available skills via the standard skill listing mechanism
- When the LLM requests a skill execution, the tool invokes it
- Skill execution results are returned to the LLM for continued reasoning

### Skills Format (Consumption Only)

**Job:** Use existing skills

Skills must already exist and follow the agentskills.io specification:
- Each skill is defined in its own file or directory
- Skill metadata (name, description, parameters) follows the spec format
- Skills can be executables, scripts, or configuration files
- The CLI does not create or share skills; it only discovers and executes them

Example skill structure:
```
.rai/skills/
  ├── read-file/
  │   ├── skill.yaml      # Skill metadata
  │   └── execute.sh      # Skill implementation
  ├── make-api-call/
  │   ├── skill.yaml
  │   └── execute.py
  └── search-docs/
      └── skill.yaml      # Can be self-contained
```

### Skills Execution

**Job:** Execute skills safely and transparently

- Skills run in a controlled environment
- Skill execution is logged to console (unless `-silent` flag is used)
- Skill errors are captured and returned to the LLM
- Skills have access to necessary context (working directory, environment, etc.)

### Skills Discovery

**Job:** Understand which skills are available

- Users can list available skills: `rai skills list`
- Each skill's description and parameters are shown
- Skills are only loaded from `.rai/skills/` (no global skills directory)

## Success Metrics

- Users can add a new skill and use it in < 10 minutes
- Skills are self-documenting via metadata
- Skill execution is transparent and debuggable
- Skills are discovered and used without manual configuration

## Implementation Notes

- Strict adherence to https://agentskills.io/integrate-skills specification
- No custom skill format or extensions
- Skills directory is local only (`.rai/skills/`)
- Skills are optional - the tool works without any skills

## Related Jobs

- See [Agent Management](./02-agent-management.md) for defining agents that use skills
- See [Logging and Output](./06-logging-output.md) for skill execution logging
- See [Configuration Management](./03-configuration-management.md) for skills directory location
