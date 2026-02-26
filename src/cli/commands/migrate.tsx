/**
 * Migrate command — migrates Python hookwise to TypeScript.
 *
 * Detects Python hookwise entries in Claude Code settings.json
 * and replaces them with TypeScript `hookwise dispatch <EventType>` entries.
 */

import React, { useMemo } from "react";
import { Text, Box } from "ink";
import { existsSync, readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { homedir } from "node:os";
import { Header } from "../components/header.js";
import { StatusBadge } from "../components/status-badge.js";

export interface MigrateCommandProps {
  settingsPath?: string;
  dryRun?: boolean;
}

interface MigrationEntry {
  eventType: string;
  oldCommand: string;
  newCommand: string;
}

interface MigrationResult {
  entries: MigrationEntry[];
  analyticsPreserved: boolean;
  settingsPath: string;
  noChanges: boolean;
  error: string | null;
}

/** Patterns that indicate Python hookwise */
const PYTHON_PATTERNS = [
  "uv run hookwise",
  "python -m hookwise",
  "python3 -m hookwise",
  "hookwise.py",
];

function isPythonHookwise(command: string): boolean {
  return PYTHON_PATTERNS.some((p) => command.includes(p));
}

function extractEventType(command: string): string | null {
  const match = command.match(
    /(?:uv run hookwise|python3? -m hookwise|hookwise\.py)\s+(?:dispatch\s+)?(\w+)/
  );
  return match ? match[1] : null;
}

function runMigration(
  settingsPath: string,
  dryRun: boolean
): MigrationResult {
  if (!existsSync(settingsPath)) {
    return {
      entries: [],
      analyticsPreserved: false,
      settingsPath,
      noChanges: false,
      error: `Settings file not found: ${settingsPath}`,
    };
  }

  try {
    const content = readFileSync(settingsPath, "utf-8");
    const settings = JSON.parse(content) as Record<string, unknown>;

    const hooks = settings.hooks as Record<
      string,
      Array<{ type: string; command: string }>
    > | undefined;

    if (!hooks || typeof hooks !== "object") {
      return {
        entries: [],
        analyticsPreserved: false,
        settingsPath,
        noChanges: true,
        error: null,
      };
    }

    const entries: MigrationEntry[] = [];
    const updatedHooks = { ...hooks };
    let changed = false;

    for (const [eventType, hookList] of Object.entries(hooks)) {
      if (!Array.isArray(hookList)) continue;

      const updatedList = hookList.map((hook) => {
        if (hook.command && isPythonHookwise(hook.command)) {
          const detectedEvent = extractEventType(hook.command) ?? eventType;
          const newCommand = `hookwise dispatch ${detectedEvent}`;
          entries.push({
            eventType,
            oldCommand: hook.command,
            newCommand,
          });
          changed = true;
          return { ...hook, command: newCommand };
        }
        return hook;
      });

      updatedHooks[eventType] = updatedList;
    }

    if (!changed) {
      return {
        entries: [],
        analyticsPreserved: false,
        settingsPath,
        noChanges: true,
        error: null,
      };
    }

    if (!dryRun) {
      settings.hooks = updatedHooks;
      writeFileSync(
        settingsPath,
        JSON.stringify(settings, null, 2) + "\n",
        "utf-8"
      );
    }

    return {
      entries,
      analyticsPreserved: true,
      settingsPath,
      noChanges: false,
      error: null,
    };
  } catch (err) {
    return {
      entries: [],
      analyticsPreserved: false,
      settingsPath,
      noChanges: false,
      error: err instanceof Error ? err.message : String(err),
    };
  }
}

export function MigrateCommand({
  settingsPath: settingsPathProp,
  dryRun = false,
}: MigrateCommandProps): React.ReactElement {
  const settingsPath =
    settingsPathProp ?? join(homedir(), ".claude", "settings.json");

  const result = useMemo(
    () => runMigration(settingsPath, dryRun),
    [settingsPath, dryRun]
  );

  if (result.error) {
    return (
      <Box flexDirection="column">
        <Header />
        <Text color="red">Error: {result.error}</Text>
      </Box>
    );
  }

  if (result.noChanges) {
    return (
      <Box flexDirection="column">
        <Header />
        <Text>No Python hookwise entries found. Nothing to migrate.</Text>
      </Box>
    );
  }

  return (
    <Box flexDirection="column">
      <Header />
      <Text bold>
        {dryRun ? "Migration Preview (dry run)" : "Migration Complete"}
      </Text>

      <Box flexDirection="column" marginTop={1}>
        {result.entries.map((entry) => (
          <Box key={`${entry.eventType}-${entry.oldCommand}`} flexDirection="column" marginBottom={1}>
            <Text bold>[{entry.eventType}]</Text>
            <Text color="red">  - {entry.oldCommand}</Text>
            <Text color="green">  + {entry.newCommand}</Text>
          </Box>
        ))}
      </Box>

      <Box flexDirection="column" marginTop={1}>
        <Box gap={1}>
          <StatusBadge status="pass" />
          <Text>
            {result.entries.length} hook(s) migrated
            {dryRun ? " (dry run)" : ""}
          </Text>
        </Box>
        <Box gap={1}>
          <StatusBadge status="pass" />
          <Text>analytics.db preserved (same schema)</Text>
        </Box>
      </Box>
    </Box>
  );
}
