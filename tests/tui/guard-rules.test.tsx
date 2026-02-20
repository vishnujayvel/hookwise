/**
 * Tests for the Guard Rules TUI tab.
 *
 * Verifies:
 * - Table rendering with guard rules
 * - Empty state
 * - Guard editor and tester components
 */

import { describe, it, expect } from "vitest";
import React from "react";
import { render } from "ink-testing-library";
import { GuardRulesTab } from "../../src/cli/tui/tabs/guard-rules.js";
import { GuardEditor } from "../../src/cli/tui/components/guard-editor.js";
import { GuardTester } from "../../src/cli/tui/components/guard-tester.js";
import { getDefaultConfig } from "../../src/core/config.js";
import type { HooksConfig } from "../../src/core/types.js";

function createConfig(
  guards: HooksConfig["guards"] = []
): HooksConfig {
  return { ...getDefaultConfig(), guards };
}

describe("GuardRulesTab", () => {
  it("shows empty state when no rules", () => {
    const config = createConfig();
    const { lastFrame } = render(
      <GuardRulesTab config={config} onConfigChange={() => {}} />
    );
    expect(lastFrame()!).toContain("No guard rules");
  });

  it("renders guard rules table", () => {
    const config = createConfig([
      { match: "Bash", action: "warn", reason: "Be careful with Bash" },
    ]);
    const { lastFrame } = render(
      <GuardRulesTab config={config} onConfigChange={() => {}} />
    );
    const frame = lastFrame()!;
    expect(frame).toContain("Bash");
    expect(frame).toContain("warn");
    expect(frame).toContain("Be careful with Bash");
  });

  it("renders header row", () => {
    const config = createConfig([
      { match: "Bash", action: "block", reason: "Blocked" },
    ]);
    const { lastFrame } = render(
      <GuardRulesTab config={config} onConfigChange={() => {}} />
    );
    const frame = lastFrame()!;
    expect(frame).toContain("Match");
    expect(frame).toContain("Action");
    expect(frame).toContain("Reason");
  });

  it("shows multiple rules", () => {
    const config = createConfig([
      { match: "Bash", action: "block", reason: "Danger" },
      { match: "mcp__gmail__*", action: "confirm", reason: "Email access" },
    ]);
    const { lastFrame } = render(
      <GuardRulesTab config={config} onConfigChange={() => {}} />
    );
    const frame = lastFrame()!;
    expect(frame).toContain("Bash");
    expect(frame).toContain("mcp__gmail__*");
  });

  it("shows help bar with shortcuts", () => {
    const config = createConfig();
    const { lastFrame } = render(
      <GuardRulesTab config={config} onConfigChange={() => {}} />
    );
    const frame = lastFrame()!;
    expect(frame).toContain("add");
    expect(frame).toContain("edit");
    expect(frame).toContain("delete");
    expect(frame).toContain("test");
  });

  it("shows condition when present", () => {
    const config = createConfig([
      {
        match: "Bash",
        action: "block",
        reason: "No force push",
        when: 'command contains "force push"',
      },
    ]);
    const { lastFrame } = render(
      <GuardRulesTab config={config} onConfigChange={() => {}} />
    );
    expect(lastFrame()!).toContain("force push");
  });
});

describe("GuardEditor", () => {
  it("renders new guard form", () => {
    const { lastFrame } = render(
      <GuardEditor onSave={() => {}} onCancel={() => {}} />
    );
    const frame = lastFrame()!;
    expect(frame).toContain("New Guard Rule");
    expect(frame).toContain("Match");
    expect(frame).toContain("Action");
    expect(frame).toContain("Reason");
  });

  it("renders edit form with existing rule", () => {
    const rule = {
      match: "Bash",
      action: "block" as const,
      reason: "Dangerous",
    };
    const { lastFrame } = render(
      <GuardEditor rule={rule} onSave={() => {}} onCancel={() => {}} />
    );
    const frame = lastFrame()!;
    expect(frame).toContain("Edit Guard Rule");
  });

  it("shows instruction text", () => {
    const { lastFrame } = render(
      <GuardEditor onSave={() => {}} onCancel={() => {}} />
    );
    const frame = lastFrame()!;
    expect(frame).toContain("Tab:");
    expect(frame).toContain("Enter:");
    expect(frame).toContain("Esc:");
  });
});

describe("GuardTester", () => {
  it("renders tester form", () => {
    const { lastFrame } = render(
      <GuardTester rules={[]} onClose={() => {}} />
    );
    const frame = lastFrame()!;
    expect(frame).toContain("Guard Tester");
    expect(frame).toContain("Tool");
  });

  it("shows instruction text", () => {
    const { lastFrame } = render(
      <GuardTester rules={[]} onClose={() => {}} />
    );
    const frame = lastFrame()!;
    expect(frame).toContain("Tab:");
    expect(frame).toContain("Enter:");
    expect(frame).toContain("Esc:");
  });
});
