# Analytics Guide

hookwise tracks session activity, tool usage, and AI authorship in a local SQLite database. All data stays on your machine -- nothing is sent to external services.

## Enabling Analytics

```yaml
analytics:
  enabled: true
  # Optional: custom database path (default: ~/.hookwise/analytics.db)
  # db_path: "/custom/path/analytics.db"
```

Once enabled, hookwise records events on every `PostToolUse` and `SessionEnd` dispatch.

## What Gets Tracked

### Sessions

Each Claude Code session is tracked with:
- Session ID
- Start and end timestamps
- Total tool calls
- Total file edits
- Estimated cost (if cost tracking is enabled)

### Tool Calls

Every tool invocation is recorded:
- Tool name (e.g., `Bash`, `Write`, `Read`, `Edit`)
- Timestamp
- File path (for file-editing tools)
- Lines added and removed

### AI Authorship

hookwise classifies every file edit with an AI confidence score:

| Classification | Score Range | Meaning |
|----------------|-------------|---------|
| `high_probability_ai` | 0.8 -- 1.0 | Almost certainly AI-generated |
| `likely_ai` | 0.6 -- 0.8 | Probably AI-generated |
| `mixed_verified` | 0.3 -- 0.6 | Mix of AI and human edits |
| `human_authored` | 0.0 -- 0.3 | Primarily human-written |

The scoring considers:
- Whether the edit came from an AI tool call (Write, Edit)
- The size of the change relative to the file
- Whether human edits followed the AI edit in the same session

## Using `hookwise stats`

The `stats` command displays analytics from the CLI:

```bash
hookwise stats
```

### Flags

| Flag | Description |
|------|-------------|
| `--json` | Output structured JSON instead of formatted text |
| `--agents` | Include multi-agent activity summary |
| `--cost` | Include cost breakdown by model and session |
| `--streaks` | Include coding streak information |

### Examples

Basic daily summary:

```bash
hookwise stats
```

Full JSON export for scripting:

```bash
hookwise stats --json --agents --cost --streaks
```

Cost breakdown only:

```bash
hookwise stats --cost
```

### Output

The default output shows:

- **Daily summary** -- tool calls, lines added/removed, session count for recent days
- **Tool breakdown** -- which tools are called most often, with line counts
- **Authorship metrics** -- weighted AI score and classification breakdown

With `--agents`, you also see:
- Agent spawn count
- File conflicts between overlapping agents

With `--cost`, you see:
- Estimated cost per session
- Daily total vs. budget (if cost tracking is configured)

With `--streaks`, you see:
- Current coding streak (consecutive days with sessions)
- Longest streak

## TUI Analytics Tab

The TUI (`hookwise tui`, tab 4: Analytics) provides a visual dashboard with the same data:

- Daily summary table
- Tool usage breakdown
- Authorship pie chart (classification distribution)
- Cost summary (if enabled)

## Database Location

The analytics database is stored at `~/.hookwise/analytics.db` by default. It uses SQLite with the following security:

- Directory permissions: `0o700` (owner only)
- File permissions: `0o600` (owner read/write only)

You can query the database directly with any SQLite client:

```bash
sqlite3 ~/.hookwise/analytics.db ".tables"
```

## Privacy

- All data is stored locally in SQLite
- No data is sent to external services
- No telemetry or usage reporting
- The database contains tool names and file paths from your sessions -- treat it as sensitive
