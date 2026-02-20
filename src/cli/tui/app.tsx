/**
 * TUI Shell — full-screen interactive terminal UI for hookwise.
 *
 * Tab navigation with number keys:
 * 1: Dashboard, 2: Guards, 3: Coaching, 4: Analytics, 5: Recipes, 6: Status
 * q/Escape to exit.
 */

import React, { useState, useCallback } from "react";
import { Text, Box, useInput, useApp } from "ink";
import { loadConfig, saveConfig } from "../../core/config.js";
import { getStateDir } from "../../core/state.js";
import { PROJECT_CONFIG_FILE } from "../../core/constants.js";
import { join } from "node:path";
import type { HooksConfig } from "../../core/types.js";

import { HelpBar } from "./components/help-bar.js";
import { DashboardTab } from "./tabs/dashboard.js";
import { GuardRulesTab } from "./tabs/guard-rules.js";
import { CoachingTab } from "./tabs/coaching.js";
import { AnalyticsTab } from "./tabs/analytics.js";
import { RecipesTab } from "./tabs/recipes.js";
import { StatusPreviewTab } from "./tabs/status-preview.js";

const TAB_NAMES = [
  "Dashboard",
  "Guards",
  "Coaching",
  "Analytics",
  "Recipes",
  "Status",
] as const;

type TabName = (typeof TAB_NAMES)[number];

export interface TuiAppProps {
  configPath?: string;
  stateDir?: string;
}

export function TuiApp({
  configPath,
  stateDir,
}: TuiAppProps): React.ReactElement {
  const { exit } = useApp();
  const [activeTab, setActiveTab] = useState<number>(0);
  const [config, setConfig] = useState<HooksConfig>(() =>
    loadConfig(configPath)
  );
  const [error, setError] = useState<string | null>(null);

  const handleConfigChange = useCallback(
    (newConfig: HooksConfig) => {
      setConfig(newConfig);
      try {
        const dir = configPath ?? process.cwd();
        saveConfig(newConfig, join(dir, PROJECT_CONFIG_FILE));
      } catch (err) {
        setError(err instanceof Error ? err.message : String(err));
      }
    },
    [configPath]
  );

  useInput((input, key) => {
    // Number keys for tab switching
    const num = parseInt(input, 10);
    if (num >= 1 && num <= TAB_NAMES.length) {
      setActiveTab(num - 1);
      return;
    }

    // Quit
    if (input === "q" || key.escape) {
      exit();
    }
  });

  const renderTab = (): React.ReactElement => {
    switch (activeTab) {
      case 0:
        return <DashboardTab config={config} />;
      case 1:
        return (
          <GuardRulesTab
            config={config}
            onConfigChange={handleConfigChange}
          />
        );
      case 2:
        return (
          <CoachingTab config={config} onConfigChange={handleConfigChange} />
        );
      case 3:
        return <AnalyticsTab config={config} />;
      case 4:
        return (
          <RecipesTab config={config} onConfigChange={handleConfigChange} />
        );
      case 5:
        return (
          <StatusPreviewTab
            config={config}
            onConfigChange={handleConfigChange}
          />
        );
      default:
        return <DashboardTab config={config} />;
    }
  };

  return (
    <Box flexDirection="column">
      {/* Tab bar */}
      <Box gap={2} marginBottom={1}>
        <Text bold color="cyan">
          hookwise TUI
        </Text>
        <Text dimColor>|</Text>
        {TAB_NAMES.map((name, i) => (
          <Text
            key={name}
            bold={i === activeTab}
            color={i === activeTab ? "cyan" : undefined}
            dimColor={i !== activeTab}
          >
            {i + 1}:{name}
          </Text>
        ))}
      </Box>

      {/* Error banner */}
      {error && (
        <Box marginBottom={1}>
          <Text color="red">Error: {error}</Text>
        </Box>
      )}

      {/* Active tab content */}
      <Box flexDirection="column">{renderTab()}</Box>

      {/* Help bar */}
      <HelpBar
        shortcuts={[
          { key: "1-6", label: "switch tab" },
          { key: "q/Esc", label: "quit" },
        ]}
      />
    </Box>
  );
}
