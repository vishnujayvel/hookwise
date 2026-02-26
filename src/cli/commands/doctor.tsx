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
import { execSync } from "node:child_process";
import { join } from "node:path";
import { homedir } from "node:os";
import { Header } from "../components/header.js";
import { StatusBadge, type BadgeStatus } from "../components/status-badge.js";
import { getStateDir } from "../../core/state.js";
import { loadConfig } from "../../core/config.js";
import { PROJECT_CONFIG_FILE, DEFAULT_DIR_MODE, DEFAULT_CALENDAR_CREDENTIALS_PATH } from "../../core/constants.js";
import { BUILTIN_SEGMENTS } from "../../core/status-line/index.js";
import { isRunning } from "../../core/feeds/daemon-manager.js";

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
  let loadedConfig: import("../../core/types.js").HooksConfig | null = null;
  if (!existsSync(configPath)) {
    results.push({
      label: "Config file",
      status: "fail",
      detail: `${PROJECT_CONFIG_FILE} not found — run "hookwise init"`,
    });
  } else {
    // File exists — validate syntax and schema by attempting to load it
    try {
      loadedConfig = loadConfig(dir);
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

  // Check 5: Validate segment names (reuses config from Check 3)
  if (loadedConfig) {
    const segments = loadedConfig.statusLine.segments;
    if (segments.length > 0) {
      const unknownSegments = segments
        .filter((seg) => seg.builtin && !(seg.builtin in BUILTIN_SEGMENTS))
        .map((seg) => seg.builtin!);

      if (unknownSegments.length > 0) {
        results.push({
          label: "Segment names",
          status: "warn",
          detail: `Unknown segment(s): ${unknownSegments.join(", ")}. Valid: ${Object.keys(BUILTIN_SEGMENTS).join(", ")}`,
        });
      } else {
        results.push({
          label: "Segment names",
          status: "pass",
          detail: `${segments.length} segment(s) validated`,
        });
      }
    }
  }

  // Check 6: Feed daemon status (only if any feed is enabled)
  if (loadedConfig) {
    const anyFeedEnabled =
      loadedConfig.feeds.pulse.enabled ||
      loadedConfig.feeds.project.enabled ||
      loadedConfig.feeds.calendar.enabled ||
      loadedConfig.feeds.news.enabled ||
      loadedConfig.feeds.insights.enabled ||
      loadedConfig.feeds.custom.some((f) => f.enabled);

    if (anyFeedEnabled) {
      try {
        if (!isRunning()) {
          results.push({
            label: "Feed daemon",
            status: "warn",
            detail: "Feeds configured but daemon not running \u2014 run 'hookwise daemon start'",
          });
        } else {
          results.push({
            label: "Feed daemon",
            status: "pass",
            detail: "Daemon is running",
          });
        }
      } catch {
        results.push({
          label: "Feed daemon",
          status: "warn",
          detail: "Could not check daemon status",
        });
      }
    }
  }

  // Check 7: Insights usage data (only if insights feed is enabled)
  if (loadedConfig?.feeds?.insights?.enabled) {
    const usageDataPath = join(homedir(), ".claude", "usage-data");
    const sessionMetaPath = join(usageDataPath, "session-meta");
    if (!existsSync(usageDataPath)) {
      results.push({
        label: "Insights data",
        status: "warn",
        detail: `${usageDataPath} not found \u2014 insights feed has no data to read. Use Claude Code to generate sessions.`,
      });
    } else if (!existsSync(sessionMetaPath)) {
      results.push({
        label: "Insights data",
        status: "warn",
        detail: `${sessionMetaPath} not found \u2014 insights feed has no session data yet`,
      });
    } else {
      results.push({
        label: "Insights data",
        status: "pass",
        detail: `${sessionMetaPath} found`,
      });
    }
  }

  // Check 8: Calendar credentials (only if calendar feed is enabled)
  if (loadedConfig?.feeds?.calendar?.enabled) {
    if (!existsSync(DEFAULT_CALENDAR_CREDENTIALS_PATH)) {
      results.push({
        label: "Calendar",
        status: "warn",
        detail: "Calendar enabled but no credentials \u2014 run 'hookwise setup calendar'",
      });
    } else {
      results.push({
        label: "Calendar",
        status: "pass",
        detail: "Calendar credentials found",
      });
    }
  }

  // Check 8: Python3 availability (only if calendar feed is enabled)
  if (loadedConfig?.feeds?.calendar?.enabled) {
    try {
      execSync("python3 --version", { stdio: "pipe" });
      results.push({
        label: "Python3",
        status: "pass",
        detail: "python3 available",
      });
    } catch {
      results.push({
        label: "Python3",
        status: "warn",
        detail: "Calendar feed requires python3 but it was not found",
      });
    }
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
        {checks.map((check) => (
          <Box key={check.label} gap={1}>
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
