/**
 * Dashboard tab — summary grid with guard count, coaching status,
 * analytics snapshot, recipes, and status line preview.
 */

import React from "react";
import { Text, Box } from "ink";
import { StatusBadge } from "../../components/status-badge.js";
import type { HooksConfig } from "../../../core/types.js";

export interface DashboardTabProps {
  config: HooksConfig;
}

function FeatureRow({
  label,
  enabled,
}: {
  label: string;
  enabled: boolean;
}): React.ReactElement {
  return (
    <Box gap={1}>
      <StatusBadge status={enabled ? "pass" : "warn"} />
      <Text>
        {label}: <Text bold>{enabled ? "ON" : "OFF"}</Text>
      </Text>
    </Box>
  );
}

export function DashboardTab({
  config,
}: DashboardTabProps): React.ReactElement {
  const coachingEnabled =
    config.coaching.metacognition.enabled ||
    config.coaching.builderTrap.enabled ||
    config.coaching.communication.enabled;

  return (
    <Box flexDirection="column">
      <Text bold underline>
        Dashboard
      </Text>

      <Box flexDirection="column" marginTop={1}>
        <Text bold>Guards</Text>
        <Text>  {config.guards.length} rule(s) configured</Text>
      </Box>

      <Box flexDirection="column" marginTop={1}>
        <Text bold>Coaching</Text>
        <Box flexDirection="column" paddingLeft={2}>
          <FeatureRow
            label="Metacognition"
            enabled={config.coaching.metacognition.enabled}
          />
          <FeatureRow
            label="Builder's Trap"
            enabled={config.coaching.builderTrap.enabled}
          />
          <FeatureRow
            label="Communication"
            enabled={config.coaching.communication.enabled}
          />
        </Box>
      </Box>

      <Box flexDirection="column" marginTop={1}>
        <Text bold>Features</Text>
        <Box flexDirection="column" paddingLeft={2}>
          <FeatureRow label="Analytics" enabled={config.analytics.enabled} />
          <FeatureRow label="Greeting" enabled={config.greeting.enabled} />
          <FeatureRow label="Status Line" enabled={config.statusLine.enabled} />
          <FeatureRow label="Cost Tracking" enabled={config.costTracking.enabled} />
          <FeatureRow
            label="Transcript Backup"
            enabled={config.transcriptBackup.enabled}
          />
        </Box>
      </Box>

      <Box flexDirection="column" marginTop={1}>
        <Text bold>Handlers</Text>
        <Text>  {config.handlers.length} custom handler(s)</Text>
      </Box>

      <Box flexDirection="column" marginTop={1}>
        <Text bold>Includes</Text>
        <Text>  {config.includes.length} include(s)</Text>
      </Box>
    </Box>
  );
}
