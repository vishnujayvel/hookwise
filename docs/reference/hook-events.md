# Hook Events Reference

hookwise supports all 13 hook event types defined by the Claude Code hooks system. Each event fires at a specific point in the AI assistant's workflow.

## Event Types

### UserPromptSubmit

**When:** Fired after the user submits a prompt but before it is processed.

**Payload fields:**
- `session_id` -- Current session identifier
- `prompt` -- The user's submitted text

**Common uses:**
- Input validation
- Prompt logging
- Session initialization

---

### PreToolUse

**When:** Fired before a tool call is executed. This is the primary event for guard rules.

**Payload fields:**
- `session_id` -- Current session identifier
- `tool_name` -- Name of the tool being called (e.g., `"Bash"`, `"Write"`, `"Read"`)
- `tool_input` -- Tool-specific input parameters (e.g., `{ command: "ls -la" }` for Bash)

**Common uses:**
- Guard rules (block, confirm, warn)
- Command filtering
- Cost estimation
- Permission checks

**Guard handler output:**
If a handler returns `{ decision: "block", reason: "..." }`, the tool call is rejected.

---

### PostToolUse

**When:** Fired after a tool call completes successfully.

**Payload fields:**
- `session_id` -- Current session identifier
- `tool_name` -- Name of the tool that was called
- `tool_input` -- Tool input parameters
- `tool_output` -- Tool execution output/result

**Common uses:**
- Analytics tracking
- Authorship scoring
- Side effect monitoring
- Output validation

---

### PostToolUseFailure

**When:** Fired when a tool call fails with an error.

**Payload fields:**
- `session_id` -- Current session identifier
- `tool_name` -- Name of the tool that failed
- `tool_input` -- Tool input parameters
- `error` -- Error message or details

**Common uses:**
- Error tracking
- Failure pattern detection
- Recovery suggestions

---

### Notification

**When:** Fired when a notification is triggered (e.g., permission requests, status updates).

**Payload fields:**
- `session_id` -- Current session identifier
- `message` -- Notification content

**Common uses:**
- Sound notifications
- Desktop alerts
- Notification logging

---

### Stop

**When:** Fired when the AI assistant stops generating (end of turn).

**Payload fields:**
- `session_id` -- Current session identifier

**Common uses:**
- Turn analytics
- Coaching prompts (metacognition, builder's trap)
- Status line updates
- Session cost calculation

---

### SubagentStart

**When:** Fired when a sub-agent (team member) starts.

**Payload fields:**
- `session_id` -- Current session identifier
- `agent_id` -- Sub-agent identifier

**Common uses:**
- Multi-agent observability
- Agent conflict detection
- Resource tracking

---

### SubagentStop

**When:** Fired when a sub-agent completes or stops.

**Payload fields:**
- `session_id` -- Current session identifier
- `agent_id` -- Sub-agent identifier

**Common uses:**
- Agent lifecycle tracking
- Conflict resolution
- Performance monitoring

---

### PreCompact

**When:** Fired before context compaction occurs (when the context window is approaching its limit).

**Payload fields:**
- `session_id` -- Current session identifier

**Common uses:**
- Context window monitoring
- Important state preservation
- Analytics snapshot

---

### SessionStart

**When:** Fired when a new Claude Code session begins.

**Payload fields:**
- `session_id` -- New session identifier

**Common uses:**
- Session greeting
- State initialization
- Analytics session start
- Context injection (daily reminders, goals)

---

### SessionEnd

**When:** Fired when a Claude Code session ends.

**Payload fields:**
- `session_id` -- Ending session identifier

**Common uses:**
- Session summary
- Analytics finalization
- Transcript backup
- Cost reporting

---

### PermissionRequest

**When:** Fired when the AI requests permission for an operation.

**Payload fields:**
- `session_id` -- Current session identifier
- `permission` -- Permission details

**Common uses:**
- Permission policy enforcement
- Audit logging
- Auto-approve/deny rules

---

### Setup

**When:** Fired during initial setup or configuration.

**Payload fields:**
- `session_id` -- Current session identifier

**Common uses:**
- Environment validation
- Dependency checking
- Initial configuration

## Dispatch Phases

When an event fires, hookwise routes it through three execution phases:

| Phase | Purpose | Behavior on error |
|-------|---------|-------------------|
| **Guard** | Decide if the action should proceed | First block wins, short-circuits |
| **Context** | Enrich the AI's context | Errors skipped, other handlers continue |
| **Side Effect** | Observe and record | Errors logged and swallowed |

## Handler Configuration

Handlers subscribe to events in `hookwise.yaml`:

```yaml
handlers:
  # Listen to specific events
  - name: my-guard
    type: inline
    events:
      - PreToolUse
      - PermissionRequest
    phase: guard
    action:
      decision: block
      reason: "Custom guard rule"

  # Listen to all events
  - name: universal-logger
    type: script
    events: "*"
    phase: side_effect
    command: "bash log-everything.sh"
```

## Event Type Constants

In Go, all event types are defined as typed constants in `internal/core/types.go`:

```go
import "github.com/vishnujayvel/hookwise/internal/core"

// core.EventPreToolUse, core.EventPostToolUse, core.EventSessionStart, ...

if core.IsValidEventType(someString) {
    // someString is a valid EventType
}
```
