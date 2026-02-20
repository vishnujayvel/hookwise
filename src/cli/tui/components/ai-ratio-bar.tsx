/**
 * AIRatioBar component — visual bar showing AI vs human authorship ratio.
 */

import React from "react";
import { Text, Box } from "ink";

export interface AIRatioBarProps {
  aiScore: number; // 0-1
  width?: number;
}

export function AIRatioBar({
  aiScore,
  width = 40,
}: AIRatioBarProps): React.ReactElement {
  const clamped = Math.max(0, Math.min(1, aiScore));
  const aiWidth = Math.round(clamped * width);
  const humanWidth = width - aiWidth;
  const pct = Math.round(clamped * 100);

  return (
    <Box gap={1}>
      <Text>AI </Text>
      <Text color="red">{"\u2588".repeat(aiWidth)}</Text>
      <Text color="green">{"\u2588".repeat(humanWidth)}</Text>
      <Text> Human</Text>
      <Text dimColor> ({pct}% AI)</Text>
    </Box>
  );
}
