/**
 * Tests for TUI tab components.
 *
 * Verifies:
 * - Dashboard tab rendering
 * - Coaching tab rendering
 * - Recipes tab rendering
 * - Status Preview tab rendering
 * - AI Ratio Bar component
 * - Session Chart component
 * - Help Bar component
 * - Table component
 * - Confirm Dialog component
 */

import { describe, it, expect } from "vitest";
import React from "react";
import { render } from "ink-testing-library";
import { DashboardTab } from "../../src/cli/tui/tabs/dashboard.js";
import { CoachingTab } from "../../src/cli/tui/tabs/coaching.js";
import { RecipesTab } from "../../src/cli/tui/tabs/recipes.js";
import { StatusPreviewTab } from "../../src/cli/tui/tabs/status-preview.js";
import { AIRatioBar } from "../../src/cli/tui/components/ai-ratio-bar.js";
import { SessionChart } from "../../src/cli/tui/components/session-chart.js";
import { HelpBar } from "../../src/cli/tui/components/help-bar.js";
import { Table } from "../../src/cli/tui/components/table.js";
import { ConfirmDialog } from "../../src/cli/tui/components/confirm-dialog.js";
import { getDefaultConfig } from "../../src/core/config.js";
import type { HooksConfig, DailySummary } from "../../src/core/types.js";

function createConfig(
  overrides: Partial<HooksConfig> = {}
): HooksConfig {
  return { ...getDefaultConfig(), ...overrides };
}

describe("DashboardTab", () => {
  it("renders dashboard title", () => {
    const { lastFrame } = render(
      <DashboardTab config={createConfig()} />
    );
    expect(lastFrame()!).toContain("Dashboard");
  });

  it("shows guard count", () => {
    const config = createConfig({
      guards: [
        { match: "Bash", action: "block", reason: "test" },
        { match: "Edit", action: "warn", reason: "test" },
      ],
    });
    const { lastFrame } = render(<DashboardTab config={config} />);
    expect(lastFrame()!).toContain("2 rule(s)");
  });

  it("shows coaching status", () => {
    const { lastFrame } = render(
      <DashboardTab config={createConfig()} />
    );
    const frame = lastFrame()!;
    expect(frame).toContain("Metacognition");
    expect(frame).toContain("Builder's Trap");
    expect(frame).toContain("Communication");
  });

  it("shows features status", () => {
    const { lastFrame } = render(
      <DashboardTab config={createConfig()} />
    );
    const frame = lastFrame()!;
    expect(frame).toContain("Analytics");
    expect(frame).toContain("Greeting");
    expect(frame).toContain("Status Line");
    expect(frame).toContain("Cost Tracking");
  });

  it("shows handler count", () => {
    const { lastFrame } = render(
      <DashboardTab config={createConfig()} />
    );
    expect(lastFrame()!).toContain("0 custom handler(s)");
  });

  it("shows ON/OFF for enabled features", () => {
    const config = createConfig({
      analytics: { enabled: true },
    });
    const { lastFrame } = render(<DashboardTab config={config} />);
    expect(lastFrame()!).toContain("ON");
  });
});

describe("CoachingTab", () => {
  it("renders coaching title", () => {
    const { lastFrame } = render(
      <CoachingTab config={createConfig()} onConfigChange={() => {}} />
    );
    expect(lastFrame()!).toContain("Coaching Configuration");
  });

  it("shows metacognition settings", () => {
    const { lastFrame } = render(
      <CoachingTab config={createConfig()} onConfigChange={() => {}} />
    );
    const frame = lastFrame()!;
    expect(frame).toContain("Metacognition");
    expect(frame).toContain("300s");
  });

  it("shows builder's trap thresholds", () => {
    const { lastFrame } = render(
      <CoachingTab config={createConfig()} onConfigChange={() => {}} />
    );
    const frame = lastFrame()!;
    expect(frame).toContain("Builder's Trap");
    expect(frame).toContain("yellow: 30m");
  });

  it("shows communication coach settings", () => {
    const { lastFrame } = render(
      <CoachingTab config={createConfig()} onConfigChange={() => {}} />
    );
    const frame = lastFrame()!;
    expect(frame).toContain("Communication Coach");
    expect(frame).toContain("gentle");
  });
});

describe("RecipesTab", () => {
  it("shows empty state when no includes", () => {
    const { lastFrame } = render(
      <RecipesTab config={createConfig()} onConfigChange={() => {}} />
    );
    expect(lastFrame()!).toContain("No recipes included");
  });

  it("shows included recipes", () => {
    const config = createConfig({
      includes: ["recipes/safety/block-dangerous.yaml"],
    });
    const { lastFrame } = render(
      <RecipesTab config={config} onConfigChange={() => {}} />
    );
    const frame = lastFrame()!;
    expect(frame).toContain("safety");
    expect(frame).toContain("block-dangerous");
  });

  it("groups recipes by category", () => {
    const config = createConfig({
      includes: [
        "recipes/safety/block-dangerous.yaml",
        "recipes/safety/secret-scanning.yaml",
        "recipes/behavioral/ai-tracker.yaml",
      ],
    });
    const { lastFrame } = render(
      <RecipesTab config={config} onConfigChange={() => {}} />
    );
    const frame = lastFrame()!;
    expect(frame).toContain("safety");
    expect(frame).toContain("behavioral");
  });
});

describe("StatusPreviewTab", () => {
  it("renders status line preview title", () => {
    const { lastFrame } = render(
      <StatusPreviewTab config={createConfig()} onConfigChange={() => {}} />
    );
    expect(lastFrame()!).toContain("Status Line Preview");
  });

  it("shows enabled status", () => {
    const { lastFrame } = render(
      <StatusPreviewTab config={createConfig()} onConfigChange={() => {}} />
    );
    expect(lastFrame()!).toContain("Enabled:");
  });

  it("shows delimiter", () => {
    const { lastFrame } = render(
      <StatusPreviewTab config={createConfig()} onConfigChange={() => {}} />
    );
    expect(lastFrame()!).toContain("Delimiter:");
  });

  it("shows no segments message when empty", () => {
    const { lastFrame } = render(
      <StatusPreviewTab config={createConfig()} onConfigChange={() => {}} />
    );
    expect(lastFrame()!).toContain("no segments");
  });
});

describe("AIRatioBar", () => {
  it("renders AI and Human labels", () => {
    const { lastFrame } = render(<AIRatioBar aiScore={0.5} />);
    const frame = lastFrame()!;
    expect(frame).toContain("AI");
    expect(frame).toContain("Human");
  });

  it("shows percentage", () => {
    const { lastFrame } = render(<AIRatioBar aiScore={0.75} />);
    expect(lastFrame()!).toContain("75%");
  });

  it("clamps to 0-100%", () => {
    const { lastFrame: f1 } = render(<AIRatioBar aiScore={-0.5} />);
    expect(f1()!).toContain("0%");

    const { lastFrame: f2 } = render(<AIRatioBar aiScore={1.5} />);
    expect(f2()!).toContain("100%");
  });
});

describe("SessionChart", () => {
  it("shows empty message when no data", () => {
    const { lastFrame } = render(<SessionChart data={[]} />);
    expect(lastFrame()!).toContain("No session data");
  });

  it("renders bars for data", () => {
    const data: DailySummary[] = [
      {
        date: "2026-02-20",
        totalEvents: 10,
        totalToolCalls: 5,
        linesAdded: 100,
        linesRemoved: 20,
        sessions: 1,
      },
    ];
    const { lastFrame } = render(<SessionChart data={data} />);
    const frame = lastFrame()!;
    expect(frame).toContain("02-20");
    expect(frame).toContain("10");
  });

  it("renders title", () => {
    const { lastFrame } = render(<SessionChart data={[]} />);
    // Empty data doesn't show title, but non-empty does
    const data: DailySummary[] = [
      {
        date: "2026-02-20",
        totalEvents: 5,
        totalToolCalls: 3,
        linesAdded: 50,
        linesRemoved: 10,
        sessions: 1,
      },
    ];
    const { lastFrame: f2 } = render(<SessionChart data={data} />);
    expect(f2()!).toContain("Events per Day");
  });
});

describe("HelpBar", () => {
  it("renders shortcuts", () => {
    const { lastFrame } = render(
      <HelpBar
        shortcuts={[
          { key: "q", label: "quit" },
          { key: "h", label: "help" },
        ]}
      />
    );
    const frame = lastFrame()!;
    expect(frame).toContain("q");
    expect(frame).toContain("quit");
    expect(frame).toContain("h");
    expect(frame).toContain("help");
  });
});

describe("Table", () => {
  it("renders headers", () => {
    const { lastFrame } = render(
      <Table
        columns={[
          { key: "name", header: "Name", width: 20 },
          { key: "value", header: "Value", width: 20 },
        ]}
        data={[]}
      />
    );
    const frame = lastFrame()!;
    expect(frame).toContain("Name");
    expect(frame).toContain("Value");
  });

  it("renders data rows", () => {
    const { lastFrame } = render(
      <Table
        columns={[
          { key: "name", header: "Name", width: 20 },
        ]}
        data={[{ name: "Hello" }]}
      />
    );
    expect(lastFrame()!).toContain("Hello");
  });

  it("shows empty message when no data", () => {
    const { lastFrame } = render(
      <Table
        columns={[{ key: "name", header: "Name", width: 20 }]}
        data={[]}
      />
    );
    expect(lastFrame()!).toContain("No data");
  });
});

describe("ConfirmDialog", () => {
  it("renders message", () => {
    const { lastFrame } = render(
      <ConfirmDialog
        message="Are you sure?"
        onConfirm={() => {}}
        onCancel={() => {}}
      />
    );
    const frame = lastFrame()!;
    expect(frame).toContain("Are you sure?");
    expect(frame).toContain("confirm");
    expect(frame).toContain("cancel");
  });
});
