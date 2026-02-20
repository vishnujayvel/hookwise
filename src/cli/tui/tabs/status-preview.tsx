/**
 * Status Preview tab — live status line preview with segment configurator.
 */

import React, { useState } from "react";
import { Text, Box } from "ink";
import { SegmentConfigurator } from "../components/segment-configurator.js";
import type { HooksConfig } from "../../../core/types.js";

export interface StatusPreviewTabProps {
  config: HooksConfig;
  onConfigChange: (config: HooksConfig) => void;
}

export function StatusPreviewTab({
  config,
  onConfigChange,
}: StatusPreviewTabProps): React.ReactElement {
  const [selectedIndex, setSelectedIndex] = useState(0);
  const [disabledSegments, setDisabledSegments] = useState<Set<number>>(
    new Set()
  );
  const { statusLine } = config;

  const handleToggle = (index: number) => {
    const newDisabled = new Set(disabledSegments);
    if (newDisabled.has(index)) {
      // Re-enable the segment
      newDisabled.delete(index);
    } else {
      // Disable the segment
      newDisabled.add(index);
    }
    setDisabledSegments(newDisabled);

    // Persist: save only enabled segments to config
    const enabledSegments = statusLine.segments.filter(
      (_seg, i) => !newDisabled.has(i)
    );
    onConfigChange({
      ...config,
      statusLine: { ...statusLine, segments: enabledSegments },
    });

    // Adjust selectedIndex if it overflows
    if (selectedIndex >= statusLine.segments.length && selectedIndex > 0) {
      setSelectedIndex(statusLine.segments.length - 1);
    }
  };

  // Build a preview of the status line (only enabled segments)
  const previewParts = statusLine.segments
    .filter((_seg, i) => !disabledSegments.has(i))
    .map((seg) => {
      if (seg.builtin) return `[${seg.builtin}]`;
      if (seg.custom?.label) return `[${seg.custom.label}]`;
      if (seg.custom?.command) return `[${seg.custom.command}]`;
      return "[?]";
    });
  const preview = previewParts.join(statusLine.delimiter);

  return (
    <Box flexDirection="column">
      <Text bold underline>
        Status Line Preview
      </Text>

      <Box flexDirection="column" marginTop={1}>
        <Text bold>Enabled:</Text>
        <Text>  {statusLine.enabled ? "Yes" : "No"}</Text>
      </Box>

      <Box flexDirection="column" marginTop={1}>
        <Text bold>Delimiter:</Text>
        <Text>  "{statusLine.delimiter}"</Text>
      </Box>

      {/* Live Preview */}
      <Box flexDirection="column" marginTop={1}>
        <Text bold>Preview:</Text>
        <Box borderStyle="single" paddingX={1}>
          <Text>{preview || "(no segments)"}</Text>
        </Box>
      </Box>

      {/* Segment List */}
      <Box marginTop={1}>
        <SegmentConfigurator
          segments={statusLine.segments}
          selectedIndex={selectedIndex}
          onSelect={setSelectedIndex}
          onToggle={handleToggle}
        />
      </Box>
    </Box>
  );
}
