/**
 * Init command — generates hookwise.yaml and state directory.
 *
 * Supports presets: minimal, coaching, analytics, full.
 */

import React, { useMemo } from "react";
import { Text, Box } from "ink";
import { existsSync, writeFileSync, mkdirSync, chmodSync } from "node:fs";
import { join } from "node:path";
import yaml from "js-yaml";
import { Header } from "../components/header.js";
import { StatusBadge } from "../components/status-badge.js";
import { getDefaultConfig } from "../../core/config.js";
import { getStateDir } from "../../core/state.js";
import { PROJECT_CONFIG_FILE, DEFAULT_DIR_MODE } from "../../core/constants.js";
import type { HooksConfig } from "../../core/types.js";
import { deepCamelToSnake } from "../../core/config.js";

export type Preset = "minimal" | "coaching" | "analytics" | "full";

interface Step {
  label: string;
  status: "pass" | "fail" | "warn";
  message: string;
}

function applyPreset(config: HooksConfig, preset: Preset): HooksConfig {
  const result = { ...config };

  // TUI auto-launch enabled for ALL presets — it's how users discover hookwise
  result.tui = { ...result.tui, autoLaunch: true };

  switch (preset) {
    case "minimal":
      // Guards + TUI — everything else disabled
      break;

    case "coaching":
      result.coaching = {
        ...result.coaching,
        metacognition: { ...result.coaching.metacognition, enabled: true },
        builderTrap: { ...result.coaching.builderTrap, enabled: true },
        communication: { ...result.coaching.communication, enabled: true },
      };
      break;

    case "analytics":
      result.analytics = { ...result.analytics, enabled: true };
      break;

    case "full":
      result.coaching = {
        ...result.coaching,
        metacognition: { ...result.coaching.metacognition, enabled: true },
        builderTrap: { ...result.coaching.builderTrap, enabled: true },
        communication: { ...result.coaching.communication, enabled: true },
      };
      result.analytics = { ...result.analytics, enabled: true };
      result.greeting = { ...result.greeting, enabled: true };
      result.statusLine = { ...result.statusLine, enabled: true };
      result.feeds = {
        ...result.feeds,
        pulse: { ...result.feeds.pulse, enabled: true },
        project: { ...result.feeds.project, enabled: true },
        calendar: { ...result.feeds.calendar, enabled: true },
        news: { ...result.feeds.news, enabled: true },
        insights: { ...result.feeds.insights, enabled: true },
        practice: { ...result.feeds.practice, enabled: true },
      };
      result.daemon = { ...result.daemon, autoStart: true };
      break;
  }

  return result;
}

const VALID_PRESETS = ["minimal", "coaching", "analytics", "full"];

function runInit(preset: string, dir: string): Step[] {
  const results: Step[] = [];

  if (!VALID_PRESETS.includes(preset)) {
    results.push({
      label: "Preset",
      status: "fail",
      message: `Unknown preset "${preset}". Valid presets: ${VALID_PRESETS.join(", ")}`,
    });
    return results;
  }

  const effectivePreset = preset as Preset;

  // Step 1: Generate hookwise.yaml
  const configPath = join(dir, PROJECT_CONFIG_FILE);
  if (existsSync(configPath)) {
    results.push({
      label: PROJECT_CONFIG_FILE,
      status: "warn",
      message: "already exists, skipping",
    });
  } else {
    try {
      let yamlContent: string;
      if (effectivePreset === "minimal") {
        yamlContent = `# hookwise configuration\n# Preset: minimal\n# Docs: https://github.com/vishnujayvel/hookwise\n\nversion: 1\nguards: []\ntui:\n  auto_launch: true\n`;
      } else {
        const config = applyPreset(getDefaultConfig(), effectivePreset);
        const snakeCased = deepCamelToSnake(config);
        yamlContent =
          `# hookwise configuration\n# Preset: ${effectivePreset}\n# Docs: https://github.com/vishnujayvel/hookwise\n\n` +
          yaml.dump(snakeCased, {
            indent: 2,
            lineWidth: 120,
            noRefs: true,
            sortKeys: false,
          });
      }
      writeFileSync(configPath, yamlContent, "utf-8");
      results.push({
        label: PROJECT_CONFIG_FILE,
        status: "pass",
        message: `created with "${effectivePreset}" preset`,
      });
    } catch (error) {
      results.push({
        label: PROJECT_CONFIG_FILE,
        status: "fail",
        message: error instanceof Error ? error.message : String(error),
      });
    }
  }

  // Step 2: Create state directory
  const stateDir = getStateDir();
  if (existsSync(stateDir)) {
    results.push({
      label: "State directory",
      status: "pass",
      message: `exists at ${stateDir}`,
    });
  } else {
    try {
      mkdirSync(stateDir, { recursive: true, mode: DEFAULT_DIR_MODE });
      chmodSync(stateDir, DEFAULT_DIR_MODE);
      results.push({
        label: "State directory",
        status: "pass",
        message: `created at ${stateDir}`,
      });
    } catch (error) {
      results.push({
        label: "State directory",
        status: "fail",
        message: error instanceof Error ? error.message : String(error),
      });
    }
  }

  return results;
}

export interface InitCommandProps {
  preset?: string;
  projectDir?: string;
}

export function InitCommand({
  preset = "minimal",
  projectDir,
}: InitCommandProps): React.ReactElement {
  const dir = projectDir ?? process.cwd();
  const steps = useMemo(() => runInit(preset, dir), [preset, dir]);

  return (
    <Box flexDirection="column">
      <Header />
      <Text bold>Initializing hookwise...</Text>
      <Box flexDirection="column" marginTop={1}>
        {steps.map((step) => (
          <Box key={step.label} gap={1}>
            <StatusBadge status={step.status} />
            <Text bold>{step.label}:</Text>
            <Text>{step.message}</Text>
          </Box>
        ))}
      </Box>
      {!steps.some((s) => s.status === "fail") && (
        <Box marginTop={1}>
          <Text color="green" bold>
            Done! Run "hookwise doctor" to verify your setup.
          </Text>
        </Box>
      )}
    </Box>
  );
}
