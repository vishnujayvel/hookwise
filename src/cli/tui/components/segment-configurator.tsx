/**
 * SegmentConfigurator component — configure status line segments.
 */

import React from "react";
import { Text, Box, useInput } from "ink";
import type { SegmentConfig } from "../../../core/types.js";

export interface SegmentConfiguratorProps {
  segments: SegmentConfig[];
  selectedIndex: number;
  onSelect: (index: number) => void;
  onToggle: (index: number) => void;
}

export function SegmentConfigurator({
  segments,
  selectedIndex,
  onSelect,
  onToggle,
}: SegmentConfiguratorProps): React.ReactElement {
  useInput((_input, key) => {
    if (key.upArrow && selectedIndex > 0) {
      onSelect(selectedIndex - 1);
    }
    if (key.downArrow && selectedIndex < segments.length - 1) {
      onSelect(selectedIndex + 1);
    }
    if (_input === " ") {
      onToggle(selectedIndex);
    }
  });

  return (
    <Box flexDirection="column">
      <Text bold>Status Line Segments</Text>
      {segments.map((seg, i) => {
        const name = seg.builtin ?? seg.custom?.label ?? seg.custom?.command ?? "unknown";
        const isSelected = i === selectedIndex;
        return (
          <Box key={i} gap={1}>
            <Text inverse={isSelected}>
              {isSelected ? ">" : " "} {name}
            </Text>
          </Box>
        );
      })}
      {segments.length === 0 && (
        <Text dimColor>  No segments configured</Text>
      )}
    </Box>
  );
}
