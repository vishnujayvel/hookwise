# Creating a Recipe

Recipes are self-contained, shareable hook configurations. Each recipe is a directory containing a config, a handler, and documentation.

## Recipe Structure

```
recipes/
  my-category/
    my-recipe/
      hooks.yaml     # Recipe configuration (required)
      handler.ts     # Recipe handler implementation (required)
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
    command: "node handler.js"
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

## handler.ts

The handler is a TypeScript file that implements the recipe's logic. It receives the hook payload on stdin and writes results to stdout:

```typescript
/**
 * my-awesome-recipe handler
 *
 * Reads the hook payload from stdin, processes it, and writes
 * a result to stdout.
 */

import { readFileSync } from "node:fs";

// Read payload from stdin
const input = readFileSync(0, "utf-8");
const payload = JSON.parse(input);

// Your recipe logic here
const toolName = payload.tool_name ?? "unknown";
const sessionId = payload.session_id ?? "";

// Output a result (optional)
const result = {
  additionalContext: `Recipe processed: ${toolName}`,
};

process.stdout.write(JSON.stringify(result));
```

### Handler Output Format

The handler can output JSON with any of these fields:

```typescript
interface HandlerResult {
  // For guard handlers: "block", "warn", "confirm", or null
  decision?: "block" | "warn" | "confirm" | null;
  // Reason for the decision
  reason?: string;
  // Context to inject into the AI's prompt
  additionalContext?: string;
  // Arbitrary output data (for side effects)
  output?: Record<string, unknown>;
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

1. `node_modules/hookwise/recipes/<name>` (npm-installed hookwise)
2. `./recipes/<name>` (local project directory)
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
  handler.ts
  README.md
```

The `hooks.yaml` defines guard rules that block `rm -rf /`, `rm -rf ~`, and force pushes. No custom handler is needed because the guards section handles everything declaratively.

## Testing Recipes

Use `GuardTester` from `hookwise/testing` to test recipe guards:

```typescript
import { GuardTester } from "hookwise/testing";

// Load a config that includes your recipe
const tester = new GuardTester("hookwise.yaml");

// Verify your recipe's guards work
const result = tester.evaluate("Bash", { command: "rm -rf /" });
expect(result.action).toBe("block");
```

For script handlers, use `HookRunner` to test the full subprocess pipeline:

```typescript
import { HookRunner } from "hookwise/testing";

const runner = new HookRunner("hookwise.yaml");
const result = await runner.run("PreToolUse", {
  session_id: "test",
  tool_name: "Bash",
  tool_input: { command: "ls -la" },
});

result.assertAllowed();
```
