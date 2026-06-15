# Proposal: `hookwise init --wire` — one safe command to go live

## Why
Bringing hookwise live in Claude Code currently means a careful ~10-step manual
flow (back up settings.json → matcher-aware safety check → add dispatch +
status-line hooks → validate JSON → write a rollback note → restart daemon →
doctor). I did this by hand during dogfooding on 2026-06-13. It's error-prone
(a malformed settings.json breaks *every* Claude Code session) and not
discoverable. Productize it as the top "skillify" candidate from the dev loop.

## Outcome
`hookwise init --wire` wires hookwise's `dispatch` (Pre/PostToolUse) and
`statusLine` into the user's Claude Code `settings.json` **idempotently,
safely, and reversibly**, then prints next steps.

## Behavior
1. **Locate** settings.json: `--settings <path>` → `$CLAUDE_CONFIG_DIR/settings.json`
   → `~/.claude/settings.json`.
2. **Back up** to `settings.json.bak-<RFC3339Z>` before any write.
3. **Pre-flight safety** (reuse `internal/hooks`): run the matcher-aware scan.
   If there are TRUE duplicates or a FAIL-level always-fire sprawl, **warn and
   refuse** unless `--force` — don't pile hookwise onto a broken hook setup.
4. **Idempotent wire**: add a `{matcher:"", command:"hookwise dispatch <Event>"}`
   group per `--events` (default `PreToolUse,PostToolUse`) and set
   `statusLine = {type:command, command:"hookwise status-line"}` unless
   `--no-status-line`. Skip any entry already present (no duplicates on re-run).
5. **Validate**: re-parse the written JSON; on failure, **restore the backup**
   and exit non-zero. Never leave a malformed settings.json.
6. **Rollback note**: write `~/.hookwise/dogfood-rollback.md` with the exact
   restore command (`cp <backup> <settings>`).
7. **Print next steps**: `hookwise daemon start`, `hookwise doctor`.

## Flags
- `--dry-run` — print the unified diff of what would change; write nothing.
- `--events Pre,Post` — which events to wire (default PreToolUse,PostToolUse).
  Deliberately NOT all 13 — only hot-path + whatever the user opts into.
- `--no-status-line` — skip the statusLine entry.
- `--settings <path>` — override the settings.json location (also makes tests
  hermetic).
- `--force` — wire even if pre-flight finds problems.
- `--unwire` — reverse: remove hookwise's own dispatch/status-line entries
  (matched by command prefix), leaving all other hooks untouched; back up first.

## Out of scope
- Editing or deduping the user's *other* hooks (init --wire only adds/removes
  hookwise's own entries; the doctor surfaces the rest).
- The daemon-dispatch thin-client (separate `openspec/changes/daemon-dispatch/`).

## Test strategy (hermetic, TDD)
All tests pass `--settings <tmp>` so they never touch the real file:
- backup file created before write; rollback note written.
- hooks added with matcher "" + statusLine set; **re-run is a no-op** (idempotent).
- malformed-on-write (inject a marshal that produces bad JSON, or a post-write
  corruption) → backup restored, non-zero exit.
- `--dry-run` writes nothing (file mtime unchanged) but prints the diff.
- `--unwire` removes only hookwise entries, preserves others.
- `--no-status-line` / `--events` honored.

## Done when
- `hookwise init --wire --settings <tmp>` produces a valid settings.json with
  `grep -c hookwise` ≥ 3 (2 dispatch + statusLine), a timestamped backup, and a
  rollback note — verified by the hermetic tests above, guarded gate green.
- `--unwire` cleanly reverses it.
- Wired into `cmd/hookwise/cmd_init.go` (extend the existing `init` command).
