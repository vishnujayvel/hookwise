/**
 * TextInput component — labeled text input field.
 */

import React from "react";
import { Text, Box } from "ink";
import InkTextInput from "ink-text-input";

export interface TextInputProps {
  label: string;
  value: string;
  onChange: (value: string) => void;
  placeholder?: string;
}

export function TextInput({
  label,
  value,
  onChange,
  placeholder,
}: TextInputProps): React.ReactElement {
  return (
    <Box gap={1}>
      <Text bold>{label}:</Text>
      <InkTextInput
        value={value}
        onChange={onChange}
        placeholder={placeholder ?? ""}
      />
    </Box>
  );
}
