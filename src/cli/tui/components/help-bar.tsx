/**
 * HelpBar component — displays keyboard shortcuts at the bottom of the TUI.
 */

import React from "react";
import { Text, Box } from "ink";

export interface Shortcut {
  key: string;
  label: string;
}

export interface HelpBarProps {
  shortcuts: Shortcut[];
}

export function HelpBar({ shortcuts }: HelpBarProps): React.ReactElement {
  return (
    <Box marginTop={1} gap={2}>
      {shortcuts.map((s, i) => (
        <Text key={i}>
          <Text bold color="cyan">
            {s.key}
          </Text>{" "}
          {s.label}
        </Text>
      ))}
    </Box>
  );
}
