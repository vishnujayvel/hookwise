/**
 * Status command — shows current hookwise configuration summary.
 *
 * Displays enabled handlers by event type, guard rules count,
 * and coaching config summary.
 */

import React, { useMemo } from "react";
import { Text, Box } from "ink";
import { Header } from "../components/header.js";
import { StatusBadge } from "../components/status-badge.js";
import { loadConfig } from "../../core/config.js";
import type { HooksConfig } from "../../core/types.js";

export interface StatusCommandProps {
  configPath?: string;
}

interface StatusInfo {
  guardCount: number;
  coaching: {
    metacognition: boolean;
    builderTrap: boolean;
    communication: boolean;
  };
  analytics: boolean;
  greeting: boolean;
  sounds: boolean;
  statusLine: boolean;
  costTracking: boolean;
  transcriptBackup: boolean;
  handlerCount: number;
}

function getStatusInfo(config: HooksConfig): StatusInfo {
  return {
    guardCount: config.guards.length,
    coaching: {
      metacognition: config.coaching.metacognition.enabled,
      builderTrap: config.coaching.builderTrap.enabled,
      communication: config.coaching.communication.enabled,
    },
    analytics: config.analytics.enabled,
    greeting: config.greeting.enabled,
    sounds: config.sounds.enabled,
    statusLine: config.statusLine.enabled,
    costTracking: config.costTracking.enabled,
    transcriptBackup: config.transcriptBackup.enabled,
    handlerCount: config.handlers.length,
  };
}

function EnabledBadge({
  enabled,
  label,
}: {
  enabled: boolean;
  label: string;
}): React.ReactElement {
  return (
    <Box gap={1}>
      <StatusBadge status={enabled ? "pass" : "warn"} />
      <Text>
        {label}: <Text bold>{enabled ? "enabled" : "disabled"}</Text>
      </Text>
    </Box>
  );
}

export function StatusCommand({
  configPath,
}: StatusCommandProps): React.ReactElement {
  const result = useMemo(() => {
    try {
      const config = loadConfig(configPath);
      return { info: getStatusInfo(config), error: null };
    } catch (err) {
      return {
        info: null,
        error: err instanceof Error ? err.message : String(err),
      };
    }
  }, [configPath]);

  if (result.error) {
    return (
      <Box flexDirection="column">
        <Header />
        <Text color="red">Error loading config: {result.error}</Text>
      </Box>
    );
  }

  const info = result.info!;

  return (
    <Box flexDirection="column">
      <Header />
      <Text bold>Configuration Status</Text>

      <Box flexDirection="column" marginTop={1}>
        <Text bold underline>Guards</Text>
        <Text>  {info.guardCount} rule(s) configured</Text>
      </Box>

      <Box flexDirection="column" marginTop={1}>
        <Text bold underline>Coaching</Text>
        <Box flexDirection="column" paddingLeft={2}>
          <EnabledBadge
            enabled={info.coaching.metacognition}
            label="Metacognition"
          />
          <EnabledBadge
            enabled={info.coaching.builderTrap}
            label="Builder's Trap"
          />
          <EnabledBadge
            enabled={info.coaching.communication}
            label="Communication"
          />
        </Box>
      </Box>

      <Box flexDirection="column" marginTop={1}>
        <Text bold underline>Features</Text>
        <Box flexDirection="column" paddingLeft={2}>
          <EnabledBadge enabled={info.analytics} label="Analytics" />
          <EnabledBadge enabled={info.greeting} label="Greeting" />
          <EnabledBadge enabled={info.sounds} label="Sounds" />
          <EnabledBadge enabled={info.statusLine} label="Status Line" />
          <EnabledBadge enabled={info.costTracking} label="Cost Tracking" />
          <EnabledBadge
            enabled={info.transcriptBackup}
            label="Transcript Backup"
          />
        </Box>
      </Box>

      <Box flexDirection="column" marginTop={1}>
        <Text bold underline>Handlers</Text>
        <Text>  {info.handlerCount} custom handler(s)</Text>
      </Box>
    </Box>
  );
}
