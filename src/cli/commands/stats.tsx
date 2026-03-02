/**
 * Stats command — displays session analytics and tool usage.
 *
 * Queries the analytics SQLite database for session stats,
 * tool breakdown, and AI authorship ratio.
 */

import React, { useMemo } from "react";
import { Text, Box } from "ink";
import { existsSync } from "node:fs";
import { Header } from "../components/header.js";
import { AnalyticsDB, queryStats } from "../../core/analytics/index.js";
import { DEFAULT_DB_PATH } from "../../core/constants.js";
import { loadCostState } from "../../core/cost.js";
import { getStateDir } from "../../core/state.js";
import type { StatsResult, CostState } from "../../core/types.js";

export interface StatsCommandProps {
  options?: {
    json?: boolean;
    agents?: boolean;
    cost?: boolean;
    streaks?: boolean;
  };
  dbPath?: string;
}

interface AgentSummary {
  agentCount: number;
  fileConflicts: number;
}

interface StatsData {
  stats: StatsResult | null;
  agents: AgentSummary | null;
  costState: CostState | null;
  streaks: number | null;
  error: string | null;
}

function loadAgentSummary(db: AnalyticsDB): AgentSummary {
  try {
    const rawDb = (db as unknown as { db: { prepare: (sql: string) => { get: () => { count: number } | undefined } } }).db;
    const row = rawDb.prepare("SELECT COUNT(*) as count FROM agent_spans").get();
    return { agentCount: row?.count ?? 0, fileConflicts: 0 };
  } catch {
    return { agentCount: 0, fileConflicts: 0 };
  }
}

function calculateStreaks(stats: StatsResult): number {
  if (stats.daily.length === 0) return 0;

  // Sort dates descending
  const sortedDates = stats.daily
    .map((d) => d.date)
    .sort()
    .reverse();

  let streak = 1;
  for (let i = 1; i < sortedDates.length; i++) {
    const curr = new Date(sortedDates[i - 1]);
    const prev = new Date(sortedDates[i]);
    const diffDays = (curr.getTime() - prev.getTime()) / (1000 * 60 * 60 * 24);
    if (diffDays <= 1) {
      streak++;
    } else {
      break;
    }
  }
  return streak;
}

function loadStats(
  dbPath: string,
  options: StatsCommandProps["options"] = {}
): StatsData {
  if (!existsSync(dbPath)) {
    return {
      stats: null,
      agents: null,
      costState: null,
      streaks: null,
      error: `No analytics database found. Run "hookwise init --preset analytics" to enable, then use Claude Code to generate data.`,
    };
  }

  let db: AnalyticsDB | null = null;
  try {
    db = new AnalyticsDB(dbPath);
    const result = queryStats(db, { days: 7 });

    const isEmpty =
      result.daily.length === 0 &&
      result.toolBreakdown.length === 0 &&
      result.authorship.totalEntries === 0;
    if (isEmpty) {
      return {
        stats: null,
        agents: null,
        costState: null,
        streaks: null,
        error: `Analytics database exists but has no data yet. Use Claude Code with hookwise active to generate session data.`,
      };
    }

    let agents: AgentSummary | null = null;
    if (options.agents) {
      agents = loadAgentSummary(db);
    }

    let costState: CostState | null = null;
    if (options.cost) {
      try {
        costState = loadCostState(getStateDir());
      } catch {
        costState = null;
      }
    }

    let streaks: number | null = null;
    if (options.streaks) {
      streaks = calculateStreaks(result);
    }

    return { stats: result, agents, costState, streaks, error: null };
  } catch (err) {
    return {
      stats: null,
      agents: null,
      costState: null,
      streaks: null,
      error: err instanceof Error ? err.message : String(err),
    };
  } finally {
    db?.close();
  }
}

export function StatsCommand({
  options = {},
  dbPath,
}: StatsCommandProps): React.ReactElement {
  const effectivePath = dbPath ?? DEFAULT_DB_PATH;
  const data = useMemo(
    () => loadStats(effectivePath, options),
    [effectivePath, options]
  );

  if (data.error) {
    return (
      <Box flexDirection="column">
        <Header />
        <Text color="red">Error: {data.error}</Text>
      </Box>
    );
  }

  if (options.json && data.stats) {
    // JSON output: render as text
    return (
      <Box>
        <Text>{JSON.stringify(data.stats, null, 2)}</Text>
      </Box>
    );
  }

  const stats = data.stats!;
  const { daily, toolBreakdown, authorship } = stats;

  return (
    <Box flexDirection="column">
      <Header />
      <Text bold>Session Analytics (Last 7 Days)</Text>

      {/* Daily Summary */}
      <Box flexDirection="column" marginTop={1}>
        <Text bold underline>Daily Summary</Text>
        {daily.length === 0 ? (
          <Text dimColor>  No data available</Text>
        ) : (
          daily.map((d) => (
            <Text key={d.date}>
              {"  "}
              {d.date}: {d.totalEvents} events, {d.totalToolCalls} tool calls,
              +{d.linesAdded}/-{d.linesRemoved} lines, {d.sessions} session(s)
            </Text>
          ))
        )}
      </Box>

      {/* Tool Breakdown */}
      <Box flexDirection="column" marginTop={1}>
        <Text bold underline>Tool Breakdown</Text>
        {toolBreakdown.length === 0 ? (
          <Text dimColor>  No tool usage data</Text>
        ) : (
          toolBreakdown.slice(0, 10).map((t) => (
            <Text key={t.toolName}>
              {"  "}
              {t.toolName}: {t.count} calls (+{t.linesAdded}/-{t.linesRemoved})
            </Text>
          ))
        )}
      </Box>

      {/* AI Authorship */}
      <Box flexDirection="column" marginTop={1}>
        <Text bold underline>AI Authorship</Text>
        <Text>
          {"  "}Entries: {authorship.totalEntries} | Lines:{" "}
          {authorship.totalLinesChanged} | Weighted AI Score:{" "}
          {authorship.weightedAIScore.toFixed(2)}
        </Text>
        {authorship.totalEntries > 0 && (
          <Box flexDirection="column" paddingLeft={2}>
            <Text>
              High AI: {authorship.classificationBreakdown.high_probability_ai} |
              Likely AI: {authorship.classificationBreakdown.likely_ai} |
              Mixed: {authorship.classificationBreakdown.mixed_verified} |
              Human: {authorship.classificationBreakdown.human_authored}
            </Text>
          </Box>
        )}
      </Box>

      {/* Agents section (--agents flag) */}
      {options.agents && (
        <Box flexDirection="column" marginTop={1}>
          <Text bold underline>Agent Summary</Text>
          {data.agents && data.agents.agentCount > 0 ? (
            <Box flexDirection="column" paddingLeft={2}>
              <Text>Agent spans: {data.agents.agentCount}</Text>
              <Text>File conflicts: {data.agents.fileConflicts}</Text>
            </Box>
          ) : (
            <Text dimColor>  No agent data available</Text>
          )}
        </Box>
      )}

      {/* Cost section (--cost flag) */}
      {options.cost && (
        <Box flexDirection="column" marginTop={1}>
          <Text bold underline>Cost Tracking</Text>
          {data.costState ? (
            <Box flexDirection="column" paddingLeft={2}>
              <Text>
                Today ({data.costState.today}): $
                {data.costState.totalToday.toFixed(4)}
              </Text>
              {Object.keys(data.costState.sessionCosts).length > 0 ? (
                Object.entries(data.costState.sessionCosts)
                  .slice(0, 5)
                  .map(([sid, cost]) => (
                    <Text key={sid}>
                      Session {sid.slice(0, 8)}...: ${cost.toFixed(4)}
                    </Text>
                  ))
              ) : (
                <Text dimColor>No session cost data</Text>
              )}
            </Box>
          ) : (
            <Text dimColor>  No cost data available</Text>
          )}
        </Box>
      )}

      {/* Streaks section (--streaks flag) */}
      {options.streaks && (
        <Box flexDirection="column" marginTop={1}>
          <Text bold underline>Streaks</Text>
          {data.streaks !== null && data.streaks > 0 ? (
            <Text>  Current streak: {data.streaks} consecutive day(s)</Text>
          ) : (
            <Text dimColor>  No streak data available</Text>
          )}
        </Box>
      )}
    </Box>
  );
}
