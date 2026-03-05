/**
 * Tests for the agents status line segment.
 */

import { describe, it, expect } from "vitest";
import { BUILTIN_SEGMENTS } from "../../../src/core/status-line/segments.js";
import { strip } from "../../../src/core/status-line/ansi.js";

const agentsRenderer = BUILTIN_SEGMENTS.agents;

function nowUnix(): number {
  return Math.floor(Date.now() / 1000);
}

describe("agents segment", () => {
  it("returns empty string when no agents data in cache", () => {
    const result = agentsRenderer({}, {});
    expect(result).toBe("");
  });

  it("returns empty string when agents array is empty", () => {
    const cache = {
      agents: { agents: [], team_name: "", strategy: "" },
    };
    const result = agentsRenderer(cache, {});
    expect(result).toBe("");
  });

  it("renders a single working agent", () => {
    const now = nowUnix();
    const cache = {
      agents: {
        agents: [
          {
            agent_id: "abc123",
            name: "explorer",
            status: "working",
            started_at: now - 180, // 3 minutes ago
          },
        ],
        team_name: "",
        strategy: "",
      },
    };
    const result = agentsRenderer(cache, {});
    const stripped = strip(result);
    expect(stripped).toContain("explorer");
    expect(stripped).toContain("working");
    expect(stripped).toContain("3m");
  });

  it("renders 3 agents with mixed states", () => {
    const now = nowUnix();
    const cache = {
      agents: {
        agents: [
          {
            agent_id: "a1",
            name: "builder",
            status: "working",
            started_at: now - 120,
          },
          {
            agent_id: "a2",
            name: "tester",
            status: "done",
            started_at: now - 300,
            stopped_at: now - 60,
          },
          {
            agent_id: "a3",
            name: "reviewer",
            status: "failed",
            started_at: now - 90,
          },
        ],
        team_name: "deploy-team",
        strategy: "parallel",
      },
    };
    const result = agentsRenderer(cache, {});
    const stripped = strip(result);

    // Team header
    expect(stripped).toContain("TEAM: deploy-team (parallel)");

    // All 3 agents
    expect(stripped).toContain("builder");
    expect(stripped).toContain("tester");
    expect(stripped).toContain("reviewer");

    // Tree drawing characters
    expect(stripped).toContain("|--");
    expect(stripped).toContain("+--");

    // Last agent uses +-- prefix
    const lines = stripped.split("\n");
    const lastAgentLine = lines[lines.length - 1];
    expect(lastAgentLine).toContain("+--");
  });

  it("filters out stale entries older than 10 minutes", () => {
    const now = nowUnix();
    const cache = {
      agents: {
        agents: [
          {
            agent_id: "fresh",
            name: "fresh-agent",
            status: "working",
            started_at: now - 60, // 1 minute ago
          },
          {
            agent_id: "stale",
            name: "stale-agent",
            status: "working",
            started_at: now - 700, // 11+ minutes ago
          },
        ],
        team_name: "",
        strategy: "",
      },
    };
    const result = agentsRenderer(cache, {});
    const stripped = strip(result);
    expect(stripped).toContain("fresh-agent");
    expect(stripped).not.toContain("stale-agent");
  });

  it("returns empty when all entries are stale", () => {
    const now = nowUnix();
    const cache = {
      agents: {
        agents: [
          {
            agent_id: "old",
            name: "old-agent",
            status: "done",
            started_at: now - 700,
          },
        ],
        team_name: "",
        strategy: "",
      },
    };
    const result = agentsRenderer(cache, {});
    expect(result).toBe("");
  });

  it("renders team header without strategy when strategy is empty", () => {
    const now = nowUnix();
    const cache = {
      agents: {
        agents: [
          {
            agent_id: "x",
            name: "worker",
            status: "working",
            started_at: now - 30,
          },
        ],
        team_name: "my-team",
        strategy: "",
      },
    };
    const result = agentsRenderer(cache, {});
    const stripped = strip(result);
    expect(stripped).toContain("TEAM: my-team");
    expect(stripped).not.toContain("()");
  });

  it("shows relative time for done agents", () => {
    const now = nowUnix();
    const cache = {
      agents: {
        agents: [
          {
            agent_id: "done1",
            name: "finisher",
            status: "done",
            started_at: now - 200,
            stopped_at: now - 60,
          },
        ],
        team_name: "",
        strategy: "",
      },
    };
    const result = agentsRenderer(cache, {});
    const stripped = strip(result);
    expect(stripped).toContain("done");
    expect(stripped).toContain("1m ago");
  });

  it("uses ANSI colors for different statuses", () => {
    const now = nowUnix();
    const cache = {
      agents: {
        agents: [
          {
            agent_id: "w",
            name: "working-one",
            status: "working",
            started_at: now - 10,
          },
          {
            agent_id: "d",
            name: "done-one",
            status: "done",
            started_at: now - 60,
            stopped_at: now - 5,
          },
          {
            agent_id: "f",
            name: "failed-one",
            status: "failed",
            started_at: now - 30,
          },
        ],
        team_name: "",
        strategy: "",
      },
    };
    const result = agentsRenderer(cache, {});
    // Yellow for working
    expect(result).toContain("\x1b[33m");
    // Green for done
    expect(result).toContain("\x1b[32m");
    // Red for failed
    expect(result).toContain("\x1b[31m");
  });
});
