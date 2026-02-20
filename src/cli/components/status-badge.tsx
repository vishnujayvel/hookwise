/**
 * StatusBadge component for pass/fail/warn indicators.
 *
 * Renders a colored symbol with optional label text.
 */

import React from "react";
import { Text } from "ink";

export type BadgeStatus = "pass" | "fail" | "warn";

export interface StatusBadgeProps {
  status: BadgeStatus;
  label?: string;
}

const STATUS_CONFIG: Record<BadgeStatus, { symbol: string; color: string }> = {
  pass: { symbol: "\u2713", color: "green" },
  fail: { symbol: "\u2717", color: "red" },
  warn: { symbol: "\u26A0", color: "yellow" },
};

export function StatusBadge({
  status,
  label,
}: StatusBadgeProps): React.ReactElement {
  const config = STATUS_CONFIG[status];
  return (
    <Text color={config.color}>
      {config.symbol}
      {label ? ` ${label}` : ""}
    </Text>
  );
}
