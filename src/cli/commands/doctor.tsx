/**
 * Doctor command — validates the hookwise installation.
 *
 * Checks:
 * - Node.js version (20+)
 * - ~/.claude/ exists
 * - hookwise.yaml in current directory
 * - State directory exists with correct permissions
 */

import React, { useMemo } from "react";
import { Text, Box } from "ink";
import { existsSync, statSync } from "node:fs";
import { join } from "node:path";
import { homedir } from "node:os";
import { Header } from "../components/header.js";
import { StatusBadge, type BadgeStatus } from "../components/status-badge.js";
import { getStateDir } from "../../core/state.js";
import { loadConfig } from "../../core/config.js";
import { PROJECT_CONFIG_FILE, DEFAULT_DIR_MODE } from "../../core/constants.js";

interface Check {
  label: string;
  status: BadgeStatus;
  detail: string;
}

export interface DoctorCommandProps {
  projectDir?: string;
}

function runChecks(dir: string): Check[] {
  const results: Check[] = [];

  // Check 1: Node.js version
  const nodeVersion = process.versions.node;
  const major = parseInt(nodeVersion.split(".")[0], 10);
  results.push({
    label: "Node.js",
    status: major >= 20 ? "pass" : "fail",
    detail: `v${nodeVersion} (>= 20 required)`,
  });

  // Check 2: ~/.claude/ exists
  const claudeDir = join(homedir(), ".claude");
  results.push({
    label: "Claude directory",
    status: existsSync(claudeDir) ? "pass" : "warn",
    detail: existsSync(claudeDir)
      ? `${claudeDir} found`
      : `${claudeDir} not found — Claude Code may not be installed`,
  });

  // Check 3: hookwise.yaml existence + syntax/schema validation
  const configPath = join(dir, PROJECT_CONFIG_FILE);
  if (!existsSync(configPath)) {
    results.push({
      label: "Config file",
      status: "fail",
      detail: `${PROJECT_CONFIG_FILE} not found — run "hookwise init"`,
    });
  } else {
    // File exists — validate syntax and schema by attempting to load it
    try {
      loadConfig(dir);
      results.push({
        label: "Config file",
        status: "pass",
        detail: `${PROJECT_CONFIG_FILE} found — valid syntax and schema`,
      });
    } catch (err) {
      const message = err instanceof Error ? err.message : String(err);
      results.push({
        label: "Config file",
        status: "fail",
        detail: `${PROJECT_CONFIG_FILE} found but invalid: ${message}`,
      });
    }
  }

  // Check 4: State directory
  const stateDir = getStateDir();
  if (existsSync(stateDir)) {
    try {
      const stats = statSync(stateDir);
      const mode = stats.mode & 0o777;
      results.push({
        label: "State directory",
        status: mode === DEFAULT_DIR_MODE ? "pass" : "warn",
        detail:
          mode === DEFAULT_DIR_MODE
            ? `${stateDir} (permissions OK)`
            : `${stateDir} (permissions ${mode.toString(8)}, expected ${DEFAULT_DIR_MODE.toString(8)})`,
      });
    } catch {
      results.push({
        label: "State directory",
        status: "warn",
        detail: `${stateDir} (could not check permissions)`,
      });
    }
  } else {
    results.push({
      label: "State directory",
      status: "fail",
      detail: `${stateDir} not found — run "hookwise init"`,
    });
  }

  return results;
}

export function DoctorCommand({
  projectDir,
}: DoctorCommandProps): React.ReactElement {
  const dir = projectDir ?? process.cwd();
  const checks = useMemo(() => runChecks(dir), [dir]);

  const allPass = checks.every((c) => c.status === "pass");
  const hasFailure = checks.some((c) => c.status === "fail");

  return (
    <Box flexDirection="column">
      <Header />
      <Text bold>System Health Check</Text>
      <Box flexDirection="column" marginTop={1}>
        {checks.map((check, i) => (
          <Box key={i} gap={1}>
            <StatusBadge status={check.status} />
            <Text bold>{check.label}:</Text>
            <Text>{check.detail}</Text>
          </Box>
        ))}
      </Box>
      <Box marginTop={1}>
        {allPass ? (
          <Text color="green" bold>
            All checks passed!
          </Text>
        ) : hasFailure ? (
          <Text color="red" bold>
            Some checks failed. Fix the issues above.
          </Text>
        ) : (
          <Text color="yellow" bold>
            Some checks have warnings.
          </Text>
        )}
      </Box>
    </Box>
  );
}
