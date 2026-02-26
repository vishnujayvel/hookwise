/**
 * Header component with hookwise branding.
 *
 * Shows the project name and version in a styled banner.
 */

import React from "react";
import { Text, Box } from "ink";

export interface HeaderProps {
  version?: string;
}

export function Header({ version = "1.1.0" }: HeaderProps): React.ReactElement {
  return (
    <Box flexDirection="column" marginBottom={1}>
      <Text bold color="cyan">
        hookwise v{version}
      </Text>
      <Text dimColor>Config-driven hooks for Claude Code</Text>
    </Box>
  );
}
