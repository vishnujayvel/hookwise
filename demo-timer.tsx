/**
 * hookwise React/Ink Live Timer Demo
 *
 * Run: npx tsx demo-timer.tsx
 * Press q to quit.
 */

import React, { useState, useEffect } from "react";
import { render, Text, Box, useApp } from "ink";

const IS_TTY = process.stdin.isTTY ?? false;

// ── Helpers ──────────────────────────────────────────────────

function formatDuration(totalSec: number): string {
  const h = Math.floor(totalSec / 3600);
  const m = Math.floor((totalSec % 3600) / 60);
  const s = totalSec % 60;
  return `${String(h).padStart(2, "0")}:${String(m).padStart(2, "0")}:${String(s).padStart(2, "0")}`;
}

function progressBar(pct: number, width: number): { filled: string; empty: string } {
  const clamped = Math.max(0, Math.min(1, pct));
  const filledLen = Math.round(clamped * width);
  return {
    filled: "\u2588".repeat(filledLen),
    empty: "\u2591".repeat(width - filledLen),
  };
}

const SPINNER_FRAMES = ["⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"];

// ── Components ───────────────────────────────────────────────

function AIRatioBar({ aiScore, width = 30 }: { aiScore: number; width?: number }) {
  const clamped = Math.max(0, Math.min(1, aiScore));
  const aiWidth = Math.round(clamped * width);
  const humanWidth = width - aiWidth;
  const pct = Math.round(clamped * 100);
  return (
    <Box gap={1}>
      <Text color="red">AI </Text>
      <Text color="red">{"\u2588".repeat(aiWidth)}</Text>
      <Text color="green">{"\u2588".repeat(humanWidth)}</Text>
      <Text color="green"> Human</Text>
      <Text dimColor> ({pct}% AI)</Text>
    </Box>
  );
}

function StatusSegment({ label, value, color }: { label: string; value: string; color: string }) {
  return (
    <Text>
      <Text dimColor>{label}: </Text>
      <Text color={color} bold>{value}</Text>
    </Text>
  );
}

// ── Main App ─────────────────────────────────────────────────

function TimerApp({ duration }: { duration?: number }) {
  const { exit } = useApp();
  const [elapsed, setElapsed] = useState(0);
  const [toolCalls, setToolCalls] = useState(0);
  const [frame, setFrame] = useState(0);

  // Tick every second
  useEffect(() => {
    const timer = setInterval(() => {
      setElapsed((prev) => prev + 1);
      setFrame((prev) => (prev + 1) % SPINNER_FRAMES.length);
    }, 1000);
    return () => clearInterval(timer);
  }, []);

  // Simulate tool calls ramping up
  useEffect(() => {
    const interval = setInterval(() => {
      setToolCalls((prev) => prev + Math.floor(Math.random() * 3));
    }, 2000);
    return () => clearInterval(interval);
  }, []);

  // Auto-exit after duration (for non-interactive mode)
  useEffect(() => {
    if (duration) {
      const timer = setTimeout(() => exit(), duration * 1000);
      return () => clearTimeout(timer);
    }
  }, [duration, exit]);

  const costEstimate = (toolCalls * 0.003 + elapsed * 0.001).toFixed(3);
  const sessionProgress = Math.min(1, elapsed / 120); // "fills up" over 2 min
  const bar = progressBar(sessionProgress, 25);
  const aiRatio = Math.min(0.95, 0.3 + elapsed * 0.005);
  const spinner = SPINNER_FRAMES[frame];

  return (
    <Box flexDirection="column" padding={1}>
      {/* Header */}
      <Box borderStyle="double" borderColor="cyan" paddingX={2} justifyContent="center">
        <Text bold color="cyan">
          {spinner} hookwise — Live Session Dashboard {spinner}
        </Text>
      </Box>

      {/* Status Line (the star of the show) */}
      <Box marginTop={1} gap={1}>
        <Text dimColor>{">"}</Text>
        <StatusSegment label="Session" value={formatDuration(elapsed)} color="yellow" />
        <Text dimColor>|</Text>
        <StatusSegment label="Tools" value={String(toolCalls)} color="cyan" />
        <Text dimColor>|</Text>
        <StatusSegment label="Cost" value={`$${costEstimate}`} color={parseFloat(costEstimate) > 0.5 ? "red" : "green"} />
        <Text dimColor>|</Text>
        <StatusSegment label="Guards" value="6 active" color="green" />
      </Box>

      {/* Progress bar */}
      <Box marginTop={1} gap={1}>
        <Text dimColor>Session Progress: </Text>
        <Text color="cyan">{bar.filled}</Text>
        <Text dimColor>{bar.empty}</Text>
        <Text> {Math.round(sessionProgress * 100)}%</Text>
      </Box>

      {/* AI Confidence Ratio */}
      <Box marginTop={1} flexDirection="column">
        <Text bold>AI Confidence Score</Text>
        <AIRatioBar aiScore={aiRatio} />
      </Box>

      {/* Simulated event log */}
      <Box marginTop={1} flexDirection="column" borderStyle="single" borderColor="gray" paddingX={1}>
        <Text bold>Recent Guard Events</Text>
        {elapsed > 3 && (
          <Text>
            <Text color="green">  ALLOW </Text>
            <Text dimColor>PreToolUse:Read</Text>
            <Text> — no guard matched</Text>
          </Text>
        )}
        {elapsed > 7 && (
          <Text>
            <Text color="yellow">  WARN  </Text>
            <Text dimColor>PreToolUse:Write</Text>
            <Text> — creating new file (file-creation-police)</Text>
          </Text>
        )}
        {elapsed > 12 && (
          <Text>
            <Text color="red">  BLOCK </Text>
            <Text dimColor>PreToolUse:Bash</Text>
            <Text> — rm -rf detected (safety-net)</Text>
          </Text>
        )}
        {elapsed > 18 && (
          <Text>
            <Text color="green">  ALLOW </Text>
            <Text dimColor>PreToolUse:Edit</Text>
            <Text> — no guard matched</Text>
          </Text>
        )}
        {elapsed > 25 && (
          <Text>
            <Text color="cyan">  COACH </Text>
            <Text dimColor>PostToolUse</Text>
            <Text> — metacognition: "Are you solving the right problem?"</Text>
          </Text>
        )}
      </Box>

      {/* Footer */}
      <Box marginTop={1}>
        <Text dimColor>Press </Text>
        <Text bold color="red">q</Text>
        <Text dimColor> to quit — built with React {React.version} + Ink</Text>
      </Box>
    </Box>
  );
}

// ── Render ────────────────────────────────────────────────────

const autoExit = IS_TTY ? undefined : 30; // auto-exit after 30s if non-interactive
render(<TimerApp duration={autoExit} />, { exitOnCtrlC: true });
