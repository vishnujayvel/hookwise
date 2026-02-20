/**
 * Coaching tab — metacognition settings, builder's trap thresholds,
 * communication coach toggle.
 */

import React from "react";
import { Text, Box } from "ink";
import { StatusBadge } from "../../components/status-badge.js";
import type { HooksConfig } from "../../../core/types.js";

export interface CoachingTabProps {
  config: HooksConfig;
  onConfigChange: (config: HooksConfig) => void;
}

function SettingRow({
  label,
  value,
}: {
  label: string;
  value: string;
}): React.ReactElement {
  return (
    <Box gap={1}>
      <Text>  {label}:</Text>
      <Text bold>{value}</Text>
    </Box>
  );
}

export function CoachingTab({
  config,
}: CoachingTabProps): React.ReactElement {
  const { metacognition, builderTrap, communication } = config.coaching;

  return (
    <Box flexDirection="column">
      <Text bold underline>
        Coaching Configuration
      </Text>

      {/* Metacognition */}
      <Box flexDirection="column" marginTop={1}>
        <Box gap={1}>
          <StatusBadge status={metacognition.enabled ? "pass" : "warn"} />
          <Text bold>Metacognition</Text>
        </Box>
        <SettingRow
          label="Interval"
          value={`${metacognition.intervalSeconds}s`}
        />
        {metacognition.promptsFile && (
          <SettingRow label="Prompts File" value={metacognition.promptsFile} />
        )}
      </Box>

      {/* Builder's Trap */}
      <Box flexDirection="column" marginTop={1}>
        <Box gap={1}>
          <StatusBadge status={builderTrap.enabled ? "pass" : "warn"} />
          <Text bold>Builder's Trap Detector</Text>
        </Box>
        <SettingRow
          label="Thresholds"
          value={`yellow: ${builderTrap.thresholds.yellow}m, orange: ${builderTrap.thresholds.orange}m, red: ${builderTrap.thresholds.red}m`}
        />
        <SettingRow
          label="Tooling Patterns"
          value={`${builderTrap.toolingPatterns.length} pattern(s)`}
        />
        <SettingRow
          label="Practice Tools"
          value={`${builderTrap.practiceTools.length} tool(s)`}
        />
      </Box>

      {/* Communication Coach */}
      <Box flexDirection="column" marginTop={1}>
        <Box gap={1}>
          <StatusBadge status={communication.enabled ? "pass" : "warn"} />
          <Text bold>Communication Coach</Text>
        </Box>
        <SettingRow label="Frequency" value={String(communication.frequency)} />
        <SettingRow
          label="Min Length"
          value={`${communication.minLength} chars`}
        />
        <SettingRow label="Tone" value={communication.tone} />
        <SettingRow
          label="Rules"
          value={`${communication.rules.length} rule(s)`}
        />
      </Box>
    </Box>
  );
}
