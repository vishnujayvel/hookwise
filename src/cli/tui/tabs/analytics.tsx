/**
 * Analytics tab — session stats, AI ratio bar, tool breakdown, cost summary.
 */

import React, { useMemo } from "react";
import { Text, Box } from "ink";
import { existsSync } from "node:fs";
import { AIRatioBar } from "../components/ai-ratio-bar.js";
import { SessionChart } from "../components/session-chart.js";
import { AnalyticsDB, queryStats } from "../../../core/analytics/index.js";
import { DEFAULT_DB_PATH } from "../../../core/constants.js";
import type { StatsResult, HooksConfig } from "../../../core/types.js";

export interface AnalyticsTabProps {
  config: HooksConfig;
}

interface AnalyticsData {
  stats: StatsResult | null;
  error: string | null;
}

function loadAnalytics(config: HooksConfig): AnalyticsData {
  const dbPath = config.analytics.dbPath ?? DEFAULT_DB_PATH;

  if (!existsSync(dbPath)) {
    return {
      stats: null,
      error: "No analytics database found. Enable analytics first.",
    };
  }

  let db: AnalyticsDB | null = null;
  try {
    db = new AnalyticsDB(dbPath);
    return { stats: queryStats(db, { days: 7 }), error: null };
  } catch (err) {
    return {
      stats: null,
      error: err instanceof Error ? err.message : String(err),
    };
  } finally {
    db?.close();
  }
}

export function AnalyticsTab({
  config,
}: AnalyticsTabProps): React.ReactElement {
  const data = useMemo(() => loadAnalytics(config), [config]);

  if (data.error) {
    return (
      <Box flexDirection="column">
        <Text bold underline>
          Analytics
        </Text>
        <Box marginTop={1}>
          <Text color="yellow">{data.error}</Text>
        </Box>
      </Box>
    );
  }

  const stats = data.stats!;

  return (
    <Box flexDirection="column">
      <Text bold underline>
        Analytics (Last 7 Days)
      </Text>

      {/* Session Chart */}
      <Box marginTop={1}>
        <SessionChart data={stats.daily} />
      </Box>

      {/* AI Ratio */}
      <Box flexDirection="column" marginTop={1}>
        <Text bold>AI Authorship Ratio</Text>
        <AIRatioBar aiScore={stats.authorship.weightedAIScore} />
      </Box>

      {/* Tool Breakdown */}
      <Box flexDirection="column" marginTop={1}>
        <Text bold>Top Tools</Text>
        {stats.toolBreakdown.length === 0 ? (
          <Text dimColor>  No tool usage data</Text>
        ) : (
          stats.toolBreakdown.slice(0, 5).map((t) => (
            <Text key={t.toolName}>
              {"  "}
              {t.toolName}: {t.count} calls (+{t.linesAdded}/-{t.linesRemoved})
            </Text>
          ))
        )}
      </Box>

      {/* Summary */}
      <Box flexDirection="column" marginTop={1}>
        <Text bold>Summary</Text>
        <Text>
          {"  "}Total entries: {stats.authorship.totalEntries} | Lines changed:{" "}
          {stats.authorship.totalLinesChanged}
        </Text>
      </Box>
    </Box>
  );
}
