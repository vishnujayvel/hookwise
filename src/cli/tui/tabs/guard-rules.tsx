/**
 * Guard Rules tab — table view with CRUD operations and inline tester.
 */

import React, { useState } from "react";
import { Text, Box, useInput } from "ink";
import { StatusBadge } from "../../components/status-badge.js";
import { GuardEditor } from "../components/guard-editor.js";
import { GuardTester } from "../components/guard-tester.js";
import { HelpBar } from "../components/help-bar.js";
import type { GuardRuleConfig, HooksConfig } from "../../../core/types.js";

export interface GuardRulesTabProps {
  config: HooksConfig;
  onConfigChange: (config: HooksConfig) => void;
}

type Mode = "list" | "add" | "edit" | "test";

export function GuardRulesTab({
  config,
  onConfigChange,
}: GuardRulesTabProps): React.ReactElement {
  const [selectedIndex, setSelectedIndex] = useState(0);
  const [mode, setMode] = useState<Mode>("list");
  const rules = config.guards;

  useInput((input, key) => {
    if (mode !== "list") return;

    if (key.upArrow && selectedIndex > 0) {
      setSelectedIndex(selectedIndex - 1);
    }
    if (key.downArrow && selectedIndex < rules.length - 1) {
      setSelectedIndex(selectedIndex + 1);
    }
    if (input === "a") {
      setMode("add");
    }
    if (input === "e" && rules.length > 0) {
      setMode("edit");
    }
    if (input === "d" && rules.length > 0) {
      const newRules = [...rules];
      newRules.splice(selectedIndex, 1);
      onConfigChange({ ...config, guards: newRules });
      if (selectedIndex >= newRules.length && selectedIndex > 0) {
        setSelectedIndex(selectedIndex - 1);
      }
    }
    if (input === "t") {
      setMode("test");
    }
  });

  const handleSave = (rule: GuardRuleConfig) => {
    if (mode === "add") {
      onConfigChange({ ...config, guards: [...rules, rule] });
    } else if (mode === "edit") {
      const newRules = [...rules];
      newRules[selectedIndex] = rule;
      onConfigChange({ ...config, guards: newRules });
    }
    setMode("list");
  };

  const handleCancel = () => {
    setMode("list");
  };

  if (mode === "add") {
    return <GuardEditor onSave={handleSave} onCancel={handleCancel} />;
  }

  if (mode === "edit" && rules[selectedIndex]) {
    return (
      <GuardEditor
        rule={rules[selectedIndex]}
        onSave={handleSave}
        onCancel={handleCancel}
      />
    );
  }

  if (mode === "test") {
    return <GuardTester rules={rules} onClose={handleCancel} />;
  }

  return (
    <Box flexDirection="column">
      <Text bold underline>
        Guard Rules
      </Text>

      {rules.length === 0 ? (
        <Box marginTop={1}>
          <Text dimColor>
            No guard rules configured. Press "a" to add one.
          </Text>
        </Box>
      ) : (
        <Box flexDirection="column" marginTop={1}>
          {/* Header */}
          <Box>
            <Box width={4}>
              <Text bold>#</Text>
            </Box>
            <Box width={25}>
              <Text bold>Match</Text>
            </Box>
            <Box width={10}>
              <Text bold>Action</Text>
            </Box>
            <Box width={25}>
              <Text bold>Condition</Text>
            </Box>
            <Box>
              <Text bold>Reason</Text>
            </Box>
          </Box>

          {/* Rows */}
          {rules.map((rule, i) => {
            const isSelected = i === selectedIndex;
            const actionColor =
              rule.action === "block"
                ? "red"
                : rule.action === "warn"
                  ? "yellow"
                  : "blue";
            return (
              <Box key={i}>
                <Box width={4}>
                  <Text inverse={isSelected}>{i + 1}</Text>
                </Box>
                <Box width={25}>
                  <Text inverse={isSelected}>{rule.match}</Text>
                </Box>
                <Box width={10}>
                  <Text color={actionColor} inverse={isSelected}>
                    {rule.action}
                  </Text>
                </Box>
                <Box width={25}>
                  <Text dimColor inverse={isSelected}>
                    {rule.when ?? rule.unless ?? "-"}
                  </Text>
                </Box>
                <Box>
                  <Text inverse={isSelected}>{rule.reason}</Text>
                </Box>
              </Box>
            );
          })}
        </Box>
      )}

      <HelpBar
        shortcuts={[
          { key: "a", label: "add" },
          { key: "e", label: "edit" },
          { key: "d", label: "delete" },
          { key: "t", label: "test" },
          { key: "\u2191\u2193", label: "navigate" },
        ]}
      />
    </Box>
  );
}
