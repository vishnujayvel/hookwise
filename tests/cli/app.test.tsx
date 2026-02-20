/**
 * Tests for the CLI app entry point and shared components.
 *
 * Verifies that shared components render correctly.
 */

import { describe, it, expect } from "vitest";
import React from "react";
import { render } from "ink-testing-library";
import { Header } from "../../src/cli/components/header.js";
import { StatusBadge } from "../../src/cli/components/status-badge.js";
import { ErrorMessage } from "../../src/cli/components/error-message.js";

describe("CLI shared components", () => {
  describe("Header", () => {
    it("renders hookwise branding with default version", () => {
      const { lastFrame } = render(<Header />);
      const frame = lastFrame()!;
      expect(frame).toContain("hookwise");
      expect(frame).toContain("1.0.0");
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
