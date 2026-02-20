/**
 * ErrorMessage component for displaying errors.
 *
 * Renders a red error message with a prefix symbol.
 */

import React from "react";
import { Text, Box } from "ink";

export interface ErrorMessageProps {
  message: string;
}

export function ErrorMessage({
  message,
}: ErrorMessageProps): React.ReactElement {
  return (
    <Box>
      <Text color="red" bold>
        Error:{" "}
      </Text>
      <Text color="red">{message}</Text>
    </Box>
  );
}
