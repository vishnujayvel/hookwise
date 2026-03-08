# Creating a Recipe

Recipes are self-contained, shareable hook configurations. Each recipe is a directory containing a config, a handler, and documentation.

## Recipe Structure

```
recipes/
  my-category/
    my-recipe/
      hooks.yaml     # Recipe configuration (required)
      handler.sh     # Recipe handler script (required)
      README.md      # Recipe documentation (required)
```

## hooks.yaml

The recipe config file defines metadata, event subscriptions, and default configuration:

```yaml
name: my-awesome-recipe
version: "1.0.0"
description: "A brief description of what this recipe does"

# Which hook events this recipe listens to
events:
  - PreToolUse
  - PostToolUse

# Default configuration values (lowest priority -- project config overrides)
config:
  my_setting: true
  threshold: 42

# Optional: guard rules to prepend to the project's guard list
guards:
  - match: "Bash"
    action: warn
    when: 'tool_input.command contains "dangerous"'
    reason: "Recipe detected potentially dangerous command"

# Optional: handlers to append to the project's handler list
handlers:
  - name: my-recipe-handler
    type: script
    events:
      - PostToolUse
    command: "bash handler.sh"
    phase: side_effect
```

### Required Fields

| Field | Type | Description |
|-------|------|-------------|
| `name` | string | Unique recipe identifier |
| `version` | string | Semantic version |
| `events` | string[] | List of valid event types this recipe handles |
| `config` | object | Default configuration values |

### Optional Fields

| Field | Type | Description |
|-------|------|-------------|
| `description` | string | Human-readable description |
| `guards` | array | Guard rules (prepended to project guards) |
| `handlers` | array | Custom handlers (appended to project handlers) |

## handler.sh

The handler is a script that implements the recipe's logic. It receives the hook payload on stdin as JSON and writes results to stdout:

```bash
#!/bin/bash
# my-awesome-recipe handler
#
# Reads the hook payload from stdin, processes it, and writes
# a JSON result to stdout.

INPUT=$(cat)
TOOL_NAME=$(echo "$INPUT" | jq -r '.tool_name // "unknown"')

# Your recipe logic here
echo "{\"additionalContext\": \"Recipe processed: $TOOL_NAME\"}"
```

### Handler Output Format

The handler can output JSON with any of these fields:

```json
{
  "decision": "block | warn | confirm | null",
  "reason": "Reason for the decision",
  "additionalContext": "Context to inject into the AI's prompt",
  "output": {}
}
```

### Exit Codes

| Code | Meaning |
|------|---------|
| 0 | Success -- stdout parsed as HandlerResult |
| 2 | Block -- stdout must contain `{"decision": "block", ...}` |
| Other | Error -- logged and skipped |

## README.md

Every recipe should include documentation:

```markdown
# My Awesome Recipe

Brief description of what this recipe does and why.

## Configuration

| Setting | Default | Description |
|---------|---------|-------------|
| `my_setting` | `true` | What this setting controls |
| `threshold` | `42` | Threshold for triggering |

## Usage

Add to your hookwise.yaml:

\`\`\`yaml
includes:
  - recipes/my-category/my-awesome-recipe
\`\`\`

## How it works

Detailed explanation of the recipe's behavior.
```

## Including Recipes

Users include your recipe in their `hookwise.yaml`:

```yaml
includes:
  - recipes/my-category/my-recipe
```

### Resolution Order

hookwise searches for recipes in this order:

1. `./recipes/<name>` (local project directory)
2. `~/.hookwise/recipes/<name>` (user-level recipes)
3. Absolute path (if the include path is absolute)

### Merge Semantics

When a recipe is included:

- **Config values** serve as defaults. The project's config always overrides recipe values.
- **Guards** from the recipe are **prepended** to the project's guard list (recipe guards check first).
- **Handlers** from the recipe are **appended** to the project's handler list (project handlers run first).

## Validation

hookwise validates recipes when they are loaded. A recipe must have:

- `name` (string)
- `version` (string)
- `events` (array of valid event type strings)
- `config` (object)

Invalid recipes are logged and skipped -- they never break the user's setup.

## Example: Built-in Recipe

Here is the `safety/block-dangerous-commands` recipe as a reference:

```
recipes/safety/block-dangerous-commands/
  hooks.yaml
  handler.sh
  README.md
```

The `hooks.yaml` defines guard rules that block `rm -rf /`, `rm -rf ~`, and force pushes. No custom handler is needed because the guards section handles everything declaratively.

## Testing Recipes

Use the Go test helpers from `pkg/hookwise/testing` to test recipe guards:

```go
import "github.com/vishnujayvel/hookwise/pkg/hookwise/testing"

func TestRecipeGuards(t *testing.T) {
    tester := hwtesting.NewGuardTester(t, "hookwise.yaml")
    result := tester.TestToolCall("Bash", map[string]any{"command": "rm -rf /"})
    assert.Equal(t, "block", result.Action)
}
```

For integration testing, use `hookwise test` which runs guard scenarios defined in your `hookwise.yaml`.
