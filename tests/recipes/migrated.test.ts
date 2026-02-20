/**
 * Tests for the 7 migrated recipe handlers.
 *
 * Each recipe has at least one test verifying the handler produces correct output.
 */

import { describe, it, expect } from "vitest";
import type { HookPayload } from "../../src/core/types.js";

// Import handlers from recipe directories
import { checkDangerousCommand } from "../../recipes/safety/block-dangerous-commands/handler.js";
import type { BlockDangerousCommandsConfig } from "../../recipes/safety/block-dangerous-commands/handler.js";

import { checkSecrets } from "../../recipes/safety/secret-scanning/handler.js";
import type { SecretScanningConfig } from "../../recipes/safety/secret-scanning/handler.js";

import { trackAuthorship, getAIRatio, checkRatio } from "../../recipes/behavioral/ai-dependency-tracker/handler.js";
import type { AIDependencyConfig, AuthorshipState } from "../../recipes/behavioral/ai-dependency-tracker/handler.js";

import { checkAndEmitPrompt } from "../../recipes/behavioral/metacognition-prompts/handler.js";
import type { MetacognitionPromptsConfig, MetacognitionState } from "../../recipes/behavioral/metacognition-prompts/handler.js";

import { classifyMode, computeAlertLevel, checkBuilderTrap } from "../../recipes/behavioral/builder-trap-detection/handler.js";
import type { BuilderTrapConfig, BuilderTrapState } from "../../recipes/behavioral/builder-trap-detection/handler.js";

import { estimateToolCost, trackCost } from "../../recipes/compliance/cost-tracking/handler.js";
import type { CostTrackingRecipeConfig, CostTrackingState } from "../../recipes/compliance/cost-tracking/handler.js";

import { formatTranscript, generateFilename, handleTranscriptBackup } from "../../recipes/productivity/transcript-backup/handler.js";
import type { TranscriptBackupConfig } from "../../recipes/productivity/transcript-backup/handler.js";

// --- Helper ---

function makePayload(overrides: Partial<HookPayload> = {}): HookPayload {
  return {
    session_id: "test-session-123",
    ...overrides,
  };
}

// --- 1. Block Dangerous Commands ---

describe("block-dangerous-commands handler", () => {
  const config: BlockDangerousCommandsConfig = {
    enabled: true,
    patterns: ["rm -rf", "force push", "git reset --hard", "DROP TABLE"],
  };

  it("blocks rm -rf commands", () => {
    const event = makePayload({
      tool_name: "Bash",
      tool_input: { command: "rm -rf /tmp/important" },
    });
    const result = checkDangerousCommand(event, config);
    expect(result.action).toBe("block");
    expect(result.reason).toContain("rm -rf");
  });

  it("blocks force push commands", () => {
    const event = makePayload({
      tool_name: "Bash",
      tool_input: { command: "git push --force push origin main" },
    });
    const result = checkDangerousCommand(event, config);
    expect(result.action).toBe("block");
    expect(result.reason).toContain("force push");
  });

  it("blocks DROP TABLE commands", () => {
    const event = makePayload({
      tool_name: "Bash",
      tool_input: { command: "psql -c 'DROP TABLE users'" },
    });
    const result = checkDangerousCommand(event, config);
    expect(result.action).toBe("block");
    expect(result.reason).toContain("DROP TABLE");
  });

  it("allows safe commands", () => {
    const event = makePayload({
      tool_name: "Bash",
      tool_input: { command: "ls -la" },
    });
    const result = checkDangerousCommand(event, config);
    expect(result.action).toBe("allow");
  });

  it("allows non-Bash tools", () => {
    const event = makePayload({
      tool_name: "Write",
      tool_input: { content: "rm -rf" },
    });
    const result = checkDangerousCommand(event, config);
    expect(result.action).toBe("allow");
  });

  it("allows when disabled", () => {
    const event = makePayload({
      tool_name: "Bash",
      tool_input: { command: "rm -rf /tmp" },
    });
    const result = checkDangerousCommand(event, { enabled: false, patterns: ["rm -rf"] });
    expect(result.action).toBe("allow");
  });
});

// --- 2. Secret Scanning ---

describe("secret-scanning handler", () => {
  const config: SecretScanningConfig = {
    enabled: true,
    sensitiveFilePatterns: [".env", "credentials.json", ".pem"],
    apiKeyPatterns: ["AKIA[0-9A-Z]{16}", "sk-[a-zA-Z0-9]{48}"],
  };

  it("warns on .env file writes", () => {
    const event = makePayload({
      tool_name: "Write",
      tool_input: { file_path: "/app/.env", content: "SECRET=abc" },
    });
    const result = checkSecrets(event, config);
    expect(result.action).toBe("warn");
    expect(result.reason).toContain(".env");
  });

  it("warns on credentials.json writes", () => {
    const event = makePayload({
      tool_name: "Write",
      tool_input: { file_path: "/app/credentials.json", content: "{}" },
    });
    const result = checkSecrets(event, config);
    expect(result.action).toBe("warn");
  });

  it("warns on AWS key patterns in content", () => {
    const event = makePayload({
      tool_name: "Write",
      tool_input: {
        file_path: "/app/config.ts",
        content: 'const key = "AKIAIOSFODNN7EXAMPLE";',
      },
    });
    const result = checkSecrets(event, config);
    expect(result.action).toBe("warn");
    expect(result.reason).toContain("API key");
  });

  it("allows safe file writes", () => {
    const event = makePayload({
      tool_name: "Write",
      tool_input: { file_path: "/app/index.ts", content: "console.log('hello')" },
    });
    const result = checkSecrets(event, config);
    expect(result.action).toBe("allow");
  });

  it("allows non-Write tools", () => {
    const event = makePayload({
      tool_name: "Bash",
      tool_input: { command: "cat .env" },
    });
    const result = checkSecrets(event, config);
    expect(result.action).toBe("allow");
  });

  it("allows when disabled", () => {
    const event = makePayload({
      tool_name: "Write",
      tool_input: { file_path: "/app/.env", content: "SECRET=abc" },
    });
    const result = checkSecrets(event, { ...config, enabled: false });
    expect(result.action).toBe("allow");
  });
});

// --- 3. AI Dependency Tracker ---

describe("ai-dependency-tracker handler", () => {
  const config: AIDependencyConfig = {
    enabled: true,
    aiRatioThreshold: 0.8,
    warnOnHighRatio: true,
    trackTools: ["Write", "Edit"],
  };

  function freshState(): AuthorshipState {
    return {
      sessionId: "test",
      totalChanges: 0,
      aiChanges: 0,
      humanChanges: 0,
      fileTracker: {},
    };
  }

  it("tracks Write tool authorship", () => {
    const state = freshState();
    const event = makePayload({
      tool_name: "Write",
      tool_input: { file_path: "/app/index.ts", content: "line1\nline2\nline3" },
    });

    trackAuthorship(event, state, config);
    expect(state.totalChanges).toBe(3);
    expect(state.aiChanges).toBe(3);
    expect(state.fileTracker["/app/index.ts"].ai).toBe(3);
  });

  it("calculates AI ratio", () => {
    const state = freshState();
    state.totalChanges = 100;
    state.aiChanges = 80;

    expect(getAIRatio(state)).toBe(0.8);
  });

  it("returns 0 ratio for no changes", () => {
    expect(getAIRatio(freshState())).toBe(0);
  });

  it("warns when AI ratio exceeds threshold", () => {
    const state = freshState();
    state.totalChanges = 100;
    state.aiChanges = 90;

    const result = checkRatio(state, config);
    expect(result).not.toBeNull();
    expect(result!.decision).toBe("warn");
    expect(result!.reason).toContain("90%");
  });

  it("does not warn below threshold", () => {
    const state = freshState();
    state.totalChanges = 100;
    state.aiChanges = 70;

    const result = checkRatio(state, config);
    expect(result).toBeNull();
  });

  it("does not warn with too few changes", () => {
    const state = freshState();
    state.totalChanges = 5;
    state.aiChanges = 5;

    const result = checkRatio(state, config);
    expect(result).toBeNull();
  });

  it("skips non-tracked tools", () => {
    const state = freshState();
    const event = makePayload({
      tool_name: "Bash",
      tool_input: { command: "ls" },
    });

    trackAuthorship(event, state, config);
    expect(state.totalChanges).toBe(0);
  });
});

// --- 4. Metacognition Prompts ---

describe("metacognition-prompts handler", () => {
  const config: MetacognitionPromptsConfig = {
    enabled: true,
    intervalSeconds: 300,
    prompts: ["Prompt A", "Prompt B", "Prompt C"],
  };

  function freshState(): MetacognitionState {
    return {
      lastPromptAt: "",
      promptIndex: 0,
      promptHistory: [],
    };
  }

  it("emits prompt when interval has elapsed", () => {
    const state = freshState();
    // Set lastPromptAt to a long time ago
    state.lastPromptAt = new Date(Date.now() - 600000).toISOString();

    const event = makePayload({ tool_name: "Write" });
    const result = checkAndEmitPrompt(event, state, config);

    expect(result).not.toBeNull();
    expect(result!.additionalContext).toContain("[Metacognition]");
    expect(result!.additionalContext).toContain("Prompt A");
  });

  it("does not emit prompt before interval", () => {
    const state = freshState();
    state.lastPromptAt = new Date().toISOString();

    const event = makePayload({ tool_name: "Write" });
    const result = checkAndEmitPrompt(event, state, config);

    expect(result).toBeNull();
  });

  it("cycles through prompts", () => {
    const state = freshState();
    state.promptIndex = 2;
    state.lastPromptAt = new Date(Date.now() - 600000).toISOString();

    const event = makePayload({ tool_name: "Write" });
    const result = checkAndEmitPrompt(event, state, config);

    expect(result).not.toBeNull();
    expect(result!.additionalContext).toContain("Prompt C");
  });

  it("returns null when disabled", () => {
    const state = freshState();
    const event = makePayload({ tool_name: "Write" });
    const result = checkAndEmitPrompt(event, state, { ...config, enabled: false });
    expect(result).toBeNull();
  });

  it("returns null with empty prompts list", () => {
    const state = freshState();
    state.lastPromptAt = new Date(Date.now() - 600000).toISOString();
    const event = makePayload({ tool_name: "Write" });
    const result = checkAndEmitPrompt(event, state, { ...config, prompts: [] });
    expect(result).toBeNull();
  });

  it("updates state after emitting prompt", () => {
    const state = freshState();
    state.lastPromptAt = new Date(Date.now() - 600000).toISOString();

    const event = makePayload({ tool_name: "Write" });
    checkAndEmitPrompt(event, state, config);

    expect(state.promptIndex).toBe(1);
    expect(state.promptHistory).toContain("Prompt A");
    expect(state.lastPromptAt).not.toBe("");
  });
});

// --- 5. Builder's Trap Detection ---

describe("builder-trap-detection handler", () => {
  const config: BuilderTrapConfig = {
    enabled: true,
    thresholds: { yellow: 30, orange: 60, red: 90 },
    toolingPatterns: ["npm", "pip", "docker"],
    practiceTools: ["vitest", "jest"],
  };

  function freshState(): BuilderTrapState {
    return {
      currentMode: "neutral",
      modeStartedAt: new Date().toISOString(),
      toolingMinutes: 0,
      alertLevel: "none",
      todayDate: new Date().toISOString().slice(0, 10),
    };
  }

  it("classifies coding tools correctly", () => {
    const event = makePayload({ tool_name: "Write", tool_input: {} });
    expect(classifyMode(event, config)).toBe("coding");
  });

  it("classifies tooling commands correctly", () => {
    const event = makePayload({
      tool_name: "Bash",
      tool_input: { command: "npm install express" },
    });
    expect(classifyMode(event, config)).toBe("tooling");
  });

  it("classifies practice tools correctly", () => {
    const event = makePayload({
      tool_name: "Bash",
      tool_input: { command: "npx vitest run" },
    });
    expect(classifyMode(event, config)).toBe("practice");
  });

  it("classifies neutral tools correctly", () => {
    const event = makePayload({
      tool_name: "Bash",
      tool_input: { command: "ls -la" },
    });
    expect(classifyMode(event, config)).toBe("neutral");
  });

  it("computes correct alert levels", () => {
    const thresholds = { yellow: 30, orange: 60, red: 90 };
    expect(computeAlertLevel(10, thresholds)).toBe("none");
    expect(computeAlertLevel(35, thresholds)).toBe("yellow");
    expect(computeAlertLevel(65, thresholds)).toBe("orange");
    expect(computeAlertLevel(95, thresholds)).toBe("red");
  });

  it("alerts on escalation from none to yellow", () => {
    const state = freshState();
    state.toolingMinutes = 29;

    const event = makePayload({
      tool_name: "Bash",
      tool_input: { command: "npm install" },
    });

    // Force toolingMinutes over threshold
    state.toolingMinutes = 31;
    const result = checkBuilderTrap(event, state, config);

    expect(result).not.toBeNull();
    expect(result!.decision).toBe("warn");
    expect(result!.reason).toContain("YELLOW");
  });

  it("does not alert when level stays the same", () => {
    const state = freshState();
    state.toolingMinutes = 35;
    state.alertLevel = "yellow";

    const event = makePayload({
      tool_name: "Bash",
      tool_input: { command: "npm install" },
    });
    const result = checkBuilderTrap(event, state, config);
    expect(result).toBeNull();
  });

  it("returns null when disabled", () => {
    const state = freshState();
    state.toolingMinutes = 100;
    const event = makePayload({ tool_name: "Bash", tool_input: { command: "npm install" } });
    const result = checkBuilderTrap(event, state, { ...config, enabled: false });
    expect(result).toBeNull();
  });
});

// --- 6. Cost Tracking ---

describe("cost-tracking handler", () => {
  const config: CostTrackingRecipeConfig = {
    enabled: true,
    rates: { "claude-sonnet": 0.003 },
    dailyBudget: 10,
    enforcement: "warn",
  };

  function freshState(): CostTrackingState {
    return {
      dailyCosts: {},
      sessionCosts: {},
      today: new Date().toISOString().slice(0, 10),
      totalToday: 0,
    };
  }

  it("estimates cost from tool input", () => {
    const event = makePayload({
      tool_name: "Write",
      tool_input: { content: "a".repeat(4000) },
    });
    const cost = estimateToolCost(event, config);
    expect(cost).toBeGreaterThan(0);
  });

  it("returns 0 cost when disabled", () => {
    const event = makePayload({ tool_name: "Write", tool_input: {} });
    const cost = estimateToolCost(event, { ...config, enabled: false });
    expect(cost).toBe(0);
  });

  it("warns when budget exceeded", () => {
    const state = freshState();
    state.totalToday = 9.99;

    const event = makePayload({
      tool_name: "Write",
      tool_input: { content: "a".repeat(100000) },
    });
    const result = trackCost(event, state, config);

    expect(result).not.toBeNull();
    expect(result!.decision).toBe("warn");
    expect(result!.reason).toContain("budget");
  });

  it("blocks when enforcement is enforce", () => {
    const state = freshState();
    state.totalToday = 9.99;

    const enforceConfig = { ...config, enforcement: "enforce" as const };
    const event = makePayload({
      tool_name: "Write",
      tool_input: { content: "a".repeat(100000) },
    });
    const result = trackCost(event, state, enforceConfig);

    expect(result).not.toBeNull();
    expect(result!.decision).toBe("block");
  });

  it("returns null when under budget", () => {
    const state = freshState();
    const event = makePayload({
      tool_name: "Write",
      tool_input: { content: "small content" },
    });
    const result = trackCost(event, state, config);
    expect(result).toBeNull();
  });

  it("accumulates session costs", () => {
    const state = freshState();
    const event = makePayload({
      tool_name: "Write",
      tool_input: { content: "some content" },
    });

    trackCost(event, state, config);
    expect(state.sessionCosts["test-session-123"]).toBeGreaterThan(0);
  });
});

// --- 7. Transcript Backup ---

describe("transcript-backup handler", () => {
  const config: TranscriptBackupConfig = {
    enabled: true,
    backupDir: "/tmp/hookwise-test-backups",
    maxSizeMb: 100,
  };

  it("formats transcript entry", () => {
    const event = makePayload({ tool_name: "PreCompact" });
    const entry = formatTranscript(event, config);

    expect(entry).not.toBeNull();
    expect(entry!.sessionId).toBe("test-session-123");
    expect(entry!.eventType).toBe("PreCompact");
    expect(entry!.timestamp).toBeDefined();
  });

  it("generates timestamped filename", () => {
    const filename = generateFilename();
    expect(filename).toMatch(/^\d{4}-\d{2}-\d{2}T\d{2}-\d{2}-\d{2}/);
    expect(filename.endsWith(".json")).toBe(true);
  });

  it("handles transcript backup event", () => {
    const event = makePayload({ tool_name: "PreCompact" });
    const result = handleTranscriptBackup(event, config);

    expect(result).not.toBeNull();
    expect(result!.output).toBeDefined();
    expect((result!.output as Record<string, unknown>).saved).toBe(true);
  });

  it("returns null when disabled", () => {
    const event = makePayload({ tool_name: "PreCompact" });
    const result = handleTranscriptBackup(event, { ...config, enabled: false });
    expect(result).toBeNull();
  });

  it("returns null for disabled format", () => {
    const event = makePayload({ tool_name: "PreCompact" });
    const entry = formatTranscript(event, { ...config, enabled: false });
    expect(entry).toBeNull();
  });
});
