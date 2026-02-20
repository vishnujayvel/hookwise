/**
 * GuardTester component — inline guard rule tester.
 *
 * Allows testing a tool name + input against the current guard rules
 * and shows the result.
 */

import React, { useState } from "react";
import { Text, Box, useInput } from "ink";
import InkTextInput from "ink-text-input";
import { evaluate } from "../../../core/guards.js";
import type { GuardRuleConfig, GuardResult } from "../../../core/types.js";

export interface GuardTesterProps {
  rules: GuardRuleConfig[];
  onClose: () => void;
}

export function GuardTester({
  rules,
  onClose,
}: GuardTesterProps): React.ReactElement {
  const [toolName, setToolName] = useState("");
  const [toolInput, setToolInput] = useState("");
  const [result, setResult] = useState<GuardResult | null>(null);
  const [activeField, setActiveField] = useState<"tool" | "input">("tool");

  useInput((input, key) => {
    if (key.escape) {
      onClose();
      return;
    }

    if (key.tab) {
      setActiveField(activeField === "tool" ? "input" : "tool");
      return;
    }

    if (key.return && toolName) {
      let parsedInput: Record<string, unknown> = {};
      if (toolInput) {
        try {
          parsedInput = JSON.parse(toolInput);
        } catch {
          parsedInput = { command: toolInput };
        }
      }
      const guardResult = evaluate(toolName, parsedInput, rules);
      setResult(guardResult);
    }
  });

  const actionColor =
    result?.action === "block"
      ? "red"
      : result?.action === "warn"
        ? "yellow"
        : result?.action === "confirm"
          ? "blue"
          : "green";

  return (
    <Box
      flexDirection="column"
      borderStyle="round"
      borderColor="magenta"
      paddingX={1}
    >
      <Text bold color="magenta">
        Guard Tester
      </Text>
      <Box gap={1} marginTop={1}>
        <Text bold inverse={activeField === "tool"}>
          Tool:
        </Text>
        {activeField === "tool" ? (
          <InkTextInput
            value={toolName}
            onChange={setToolName}
            placeholder="e.g., Bash"
          />
        ) : (
          <Text>{toolName || "(empty)"}</Text>
        )}
      </Box>
      <Box gap={1}>
        <Text bold inverse={activeField === "input"}>
          Input:
        </Text>
        {activeField === "input" ? (
          <InkTextInput
            value={toolInput}
            onChange={setToolInput}
            placeholder='e.g., {"command": "rm -rf /"}'
          />
        ) : (
          <Text dimColor>{toolInput || "(empty)"}</Text>
        )}
      </Box>

      {result && (
        <Box flexDirection="column" marginTop={1}>
          <Text>
            Result:{" "}
            <Text bold color={actionColor}>
              {result.action.toUpperCase()}
            </Text>
          </Text>
          {result.reason && <Text>Reason: {result.reason}</Text>}
          {result.matchedRule && (
            <Text dimColor>Matched: {result.matchedRule.match}</Text>
          )}
        </Box>
      )}

      <Box marginTop={1}>
        <Text dimColor>Tab: switch field | Enter: test | Esc: close</Text>
      </Box>
    </Box>
  );
}
