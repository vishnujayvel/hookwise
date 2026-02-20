/**
 * GuardEditor component — inline form for editing a guard rule.
 */

import React, { useState } from "react";
import { Text, Box, useInput } from "ink";
import InkTextInput from "ink-text-input";
import type { GuardRuleConfig } from "../../../core/types.js";

export interface GuardEditorProps {
  rule?: GuardRuleConfig;
  onSave: (rule: GuardRuleConfig) => void;
  onCancel: () => void;
}

type EditField = "match" | "action" | "reason" | "when" | "unless";

const FIELDS: EditField[] = ["match", "action", "reason", "when", "unless"];
const ACTIONS: GuardRuleConfig["action"][] = ["block", "warn", "confirm"];

export function GuardEditor({
  rule,
  onSave,
  onCancel,
}: GuardEditorProps): React.ReactElement {
  const [match, setMatch] = useState(rule?.match ?? "");
  const [action, setAction] = useState<GuardRuleConfig["action"]>(
    rule?.action ?? "block"
  );
  const [reason, setReason] = useState(rule?.reason ?? "");
  const [when, setWhen] = useState(rule?.when ?? "");
  const [unless, setUnless] = useState(rule?.unless ?? "");
  const [activeField, setActiveField] = useState<EditField>("match");

  useInput((input, key) => {
    if (key.escape) {
      onCancel();
      return;
    }

    if (key.tab) {
      const idx = FIELDS.indexOf(activeField);
      const next = FIELDS[(idx + 1) % FIELDS.length];
      setActiveField(next);
      return;
    }

    if (key.return) {
      if (match && reason) {
        onSave({
          match,
          action,
          reason,
          ...(when ? { when } : {}),
          ...(unless ? { unless } : {}),
        });
      }
      return;
    }

    // Cycle action with space when on action field
    if (activeField === "action" && input === " ") {
      const idx = ACTIONS.indexOf(action);
      setAction(ACTIONS[(idx + 1) % ACTIONS.length]);
    }
  });

  return (
    <Box
      flexDirection="column"
      borderStyle="round"
      borderColor="blue"
      paddingX={1}
    >
      <Text bold color="blue">
        {rule ? "Edit Guard Rule" : "New Guard Rule"}
      </Text>
      <Box gap={1} marginTop={1}>
        <Text bold inverse={activeField === "match"}>
          Match:
        </Text>
        {activeField === "match" ? (
          <InkTextInput value={match} onChange={setMatch} />
        ) : (
          <Text>{match || "(empty)"}</Text>
        )}
      </Box>
      <Box gap={1}>
        <Text bold inverse={activeField === "action"}>
          Action:
        </Text>
        <Text color={action === "block" ? "red" : action === "warn" ? "yellow" : "blue"}>
          {action}
        </Text>
        {activeField === "action" && (
          <Text dimColor> (Space to cycle)</Text>
        )}
      </Box>
      <Box gap={1}>
        <Text bold inverse={activeField === "reason"}>
          Reason:
        </Text>
        {activeField === "reason" ? (
          <InkTextInput value={reason} onChange={setReason} />
        ) : (
          <Text>{reason || "(empty)"}</Text>
        )}
      </Box>
      <Box gap={1}>
        <Text bold inverse={activeField === "when"}>
          When:
        </Text>
        {activeField === "when" ? (
          <InkTextInput value={when} onChange={setWhen} placeholder="optional" />
        ) : (
          <Text dimColor>{when || "(none)"}</Text>
        )}
      </Box>
      <Box gap={1}>
        <Text bold inverse={activeField === "unless"}>
          Unless:
        </Text>
        {activeField === "unless" ? (
          <InkTextInput value={unless} onChange={setUnless} placeholder="optional" />
        ) : (
          <Text dimColor>{unless || "(none)"}</Text>
        )}
      </Box>
      <Box marginTop={1}>
        <Text dimColor>Tab: next field | Enter: save | Esc: cancel</Text>
      </Box>
    </Box>
  );
}
