/**
 * Table component for TUI views.
 *
 * Renders a data table with selectable rows and column definitions.
 */

import React from "react";
import { Text, Box } from "ink";

export interface ColumnDef {
  key: string;
  header: string;
  width?: number;
}

export interface TableProps<T extends Record<string, unknown>> {
  columns: ColumnDef[];
  data: T[];
  selectedIndex?: number;
  onSelect?: (index: number) => void;
}

function truncate(s: string, width: number): string {
  if (s.length <= width) return s.padEnd(width);
  return s.slice(0, width - 1) + "\u2026";
}

export function Table<T extends Record<string, unknown>>({
  columns,
  data,
  selectedIndex,
}: TableProps<T>): React.ReactElement {
  return (
    <Box flexDirection="column">
      {/* Header row */}
      <Box>
        {columns.map((col) => (
          <Box key={col.key} width={col.width ?? 20}>
            <Text bold underline>
              {truncate(col.header, col.width ?? 20)}
            </Text>
          </Box>
        ))}
      </Box>
      {/* Data rows */}
      {data.map((row, i) => {
        const isSelected = i === selectedIndex;
        return (
          <Box key={i}>
            {columns.map((col) => (
              <Box key={col.key} width={col.width ?? 20}>
                <Text
                  inverse={isSelected}
                >
                  {truncate(String(row[col.key] ?? ""), col.width ?? 20)}
                </Text>
              </Box>
            ))}
          </Box>
        );
      })}
      {data.length === 0 && (
        <Text dimColor>  No data</Text>
      )}
    </Box>
  );
}
