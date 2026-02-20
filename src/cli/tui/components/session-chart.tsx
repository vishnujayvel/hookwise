/**
 * SessionChart component — simple text-based bar chart of daily events.
 */

import React from "react";
import { Text, Box } from "ink";
import type { DailySummary } from "../../../core/types.js";

export interface SessionChartProps {
  data: DailySummary[];
  maxWidth?: number;
}

export function SessionChart({
  data,
  maxWidth = 30,
}: SessionChartProps): React.ReactElement {
  if (data.length === 0) {
    return <Text dimColor>No session data available</Text>;
  }

  const maxEvents = Math.max(...data.map((d) => d.totalEvents), 1);

  return (
    <Box flexDirection="column">
      <Text bold>Events per Day</Text>
      {data.map((d) => {
        const barLen = Math.max(1, Math.round((d.totalEvents / maxEvents) * maxWidth));
        return (
          <Box key={d.date} gap={1}>
            <Text>{d.date.slice(5)}</Text>
            <Text color="cyan">{"\u2588".repeat(barLen)}</Text>
            <Text dimColor> {d.totalEvents}</Text>
          </Box>
        );
      })}
    </Box>
  );
}
