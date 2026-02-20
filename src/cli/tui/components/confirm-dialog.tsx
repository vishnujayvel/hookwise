/**
 * ConfirmDialog component — yes/no confirmation prompt.
 */

import React from "react";
import { Text, Box, useInput } from "ink";

export interface ConfirmDialogProps {
  message: string;
  onConfirm: () => void;
  onCancel: () => void;
}

export function ConfirmDialog({
  message,
  onConfirm,
  onCancel,
}: ConfirmDialogProps): React.ReactElement {
  useInput((input) => {
    if (input === "y" || input === "Y") {
      onConfirm();
    } else if (input === "n" || input === "N" || input === "q") {
      onCancel();
    }
  });

  return (
    <Box flexDirection="column" borderStyle="round" borderColor="yellow" paddingX={1}>
      <Text bold color="yellow">
        {message}
      </Text>
      <Text>
        Press <Text bold>y</Text> to confirm, <Text bold>n</Text> to cancel
      </Text>
    </Box>
  );
}
