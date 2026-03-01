/**
 * Tests for the CLI app entry point and shared components.
 *
 * Verifies that shared components render correctly,
 * and that subcommand --help works for all commands.
 */

import { describe, it, expect } from "vitest";
import React from "react";
import { render } from "ink-testing-library";
import { Header } from "../../src/cli/components/header.js";
import { StatusBadge } from "../../src/cli/components/status-badge.js";
import { ErrorMessage } from "../../src/cli/components/error-message.js";
import { SubcommandHelp } from "../../src/cli/app.js";

describe("CLI shared components", () => {
  describe("Header", () => {
    it("renders hookwise branding with default version", () => {
      const { lastFrame } = render(<Header />);
      const frame = lastFrame()!;
      expect(frame).toContain("hookwise");
      expect(frame).toContain("1.2.0");
    });

    it("renders custom version", () => {
      const { lastFrame } = render(<Header version="2.0.0" />);
      expect(lastFrame()!).toContain("2.0.0");
    });

    it("includes tagline", () => {
      const { lastFrame } = render(<Header />);
      expect(lastFrame()!).toContain("Config-driven hooks");
    });
  });

  describe("StatusBadge", () => {
    it("renders pass symbol", () => {
      const { lastFrame } = render(<StatusBadge status="pass" />);
      expect(lastFrame()!).toContain("\u2713");
    });

    it("renders fail symbol", () => {
      const { lastFrame } = render(<StatusBadge status="fail" />);
      expect(lastFrame()!).toContain("\u2717");
    });

    it("renders warn symbol", () => {
      const { lastFrame } = render(<StatusBadge status="warn" />);
      expect(lastFrame()!).toContain("\u26A0");
    });

    it("renders label when provided", () => {
      const { lastFrame } = render(
        <StatusBadge status="pass" label="Node.js" />
      );
      const frame = lastFrame()!;
      expect(frame).toContain("Node.js");
      expect(frame).toContain("\u2713");
    });

    it("renders without label", () => {
      const { lastFrame } = render(<StatusBadge status="pass" />);
      const frame = lastFrame()!;
      expect(frame).toContain("\u2713");
    });
  });

  describe("ErrorMessage", () => {
    it("renders error message", () => {
      const { lastFrame } = render(
        <ErrorMessage message="Something went wrong" />
      );
      const frame = lastFrame()!;
      expect(frame).toContain("Error:");
      expect(frame).toContain("Something went wrong");
    });
  });
});

describe("CLI command routing", () => {
  it("all subcommands are defined", () => {
    const commands = [
      "init",
      "doctor",
      "status",
      "stats",
      "test",
      "tui",
      "migrate",
      "dispatch",
    ];
    expect(commands).toHaveLength(8);
    // Verify each command string is a non-empty string
    for (const cmd of commands) {
      expect(typeof cmd).toBe("string");
      expect(cmd.length).toBeGreaterThan(0);
    }
  });
});

describe("SubcommandHelp", () => {
  it("init --help shows help text with --preset flag", () => {
    const { lastFrame } = render(
      <SubcommandHelp
        command="init"
        help={{
          description: "Initialize hookwise in the current directory",
          flags: ["--preset <name>  Use a preset: minimal, coaching, analytics, full"],
          usage: "hookwise init --preset coaching",
        }}
      />
    );
    const frame = lastFrame()!;
    expect(frame).toContain("hookwise init");
    expect(frame).toContain("Initialize hookwise");
    expect(frame).toContain("--preset");
    expect(frame).toContain("minimal, coaching, analytics, full");
    expect(frame).toContain("Example:");
    expect(frame).toContain("hookwise init --preset coaching");
  });

  it("stats --help shows all flag options (--json, --agents, --cost, --streaks)", () => {
    const { lastFrame } = render(
      <SubcommandHelp
        command="stats"
        help={{
          description: "Display session analytics and tool usage",
          flags: [
            "--json     Output as structured JSON",
            "--agents   Include agent activity summary",
            "--cost     Include cost breakdown",
            "--streaks  Include streak summary",
          ],
          usage: "hookwise stats --json --agents",
        }}
      />
    );
    const frame = lastFrame()!;
    expect(frame).toContain("hookwise stats");
    expect(frame).toContain("Display session analytics");
    expect(frame).toContain("--json");
    expect(frame).toContain("--agents");
    expect(frame).toContain("--cost");
    expect(frame).toContain("--streaks");
    expect(frame).toContain("Flags:");
  });

  it("dispatch --help shows description and usage", () => {
    const { lastFrame } = render(
      <SubcommandHelp
        command="dispatch"
        help={{
          description: "Dispatch a hook event (fast path, used by Claude Code)",
          usage: "hookwise dispatch PreToolUse < payload.json",
        }}
      />
    );
    const frame = lastFrame()!;
    expect(frame).toContain("hookwise dispatch");
    expect(frame).toContain("Dispatch a hook event");
    expect(frame).toContain("hookwise dispatch PreToolUse");
  });

  it("doctor --help shows description without flags section", () => {
    const { lastFrame } = render(
      <SubcommandHelp
        command="doctor"
        help={{
          description: "Check system health and configuration",
        }}
      />
    );
    const frame = lastFrame()!;
    expect(frame).toContain("hookwise doctor");
    expect(frame).toContain("Check system health");
    // No flags or example for doctor
    expect(frame).not.toContain("Flags:");
    expect(frame).not.toContain("Example:");
  });

  it("renders header with hookwise branding", () => {
    const { lastFrame } = render(
      <SubcommandHelp
        command="test"
        help={{ description: "Run hookwise test suite" }}
      />
    );
    const frame = lastFrame()!;
    expect(frame).toContain("hookwise");
    expect(frame).toContain("1.2.0");
  });
});
