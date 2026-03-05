/**
 * Integration tests for dispatcher → side-effect → state wiring.
 *
 * Verifies that dispatch() writes expected state to real cache files
 * and databases. Uses real temp directories and real dispatch() calls
 * (ARCH-1: no mocking of internal modules).
 *
 * Tasks: 3.1 (heartbeat/cwd cache writes)
 *        3.2 (analytics DB wiring — skipped for GH#9)
 *        3.3 (fail-open and fault isolation)
 */

import { describe, it, expect, beforeEach, afterEach } from "vitest";
import { existsSync, writeFileSync, readFileSync } from "node:fs";
import { join } from "node:path";
import { dispatch } from "../../src/core/dispatcher.js";
import { readKey } from "../../src/core/feeds/cache-bus.js";
import { createTestEnv, makePayload, readAnalyticsDB } from "./helpers.js";
import type { TestEnv } from "./helpers.js";
import type { CacheEntry } from "../../src/core/types.js";

// ---------------------------------------------------------------------------
// Task 3.1 — Heartbeat and CWD cache writes
// ---------------------------------------------------------------------------

describe("dispatcher-wiring: heartbeat and CWD cache writes", () => {
  let env: TestEnv;
  const savedStateDir = process.env.HOOKWISE_STATE_DIR;

  beforeEach(() => {
    env = createTestEnv();
    // Point HOOKWISE_STATE_DIR to temp dir so loadConfig() doesn't touch ~/.hookwise
    process.env.HOOKWISE_STATE_DIR = env.tmpDir;
  });

  afterEach(() => {
    env.cleanup();
    if (savedStateDir !== undefined) {
      process.env.HOOKWISE_STATE_DIR = savedStateDir;
    } else {
      delete process.env.HOOKWISE_STATE_DIR;
    }
  });

  it("dispatch(any event) writes _dispatch_heartbeat to real cache file with recent timestamp", () => {
    const beforeMs = Date.now();

    dispatch("PostToolUse", makePayload(), {
      config: env.config,
      projectDir: env.tmpDir,
    });

    const afterMs = Date.now();

    // Read _dispatch_heartbeat from the real cache file on disk
    const heartbeat = readKey<CacheEntry & { value: number }>(
      env.config.statusLine.cachePath,
      "_dispatch_heartbeat",
    );

    expect(heartbeat).not.toBeNull();
    expect(heartbeat!.value).toBeGreaterThanOrEqual(beforeMs);
    expect(heartbeat!.value).toBeLessThanOrEqual(afterMs);
    expect(heartbeat!.ttl_seconds).toBe(999999);
  });

  it("dispatch(any event) writes _cwd to real cache file with correct value", () => {
    dispatch("SessionStart", makePayload(), {
      config: env.config,
      projectDir: env.tmpDir,
    });

    // Read _cwd from the real cache file on disk
    const cwd = readKey<CacheEntry & { value: string }>(
      env.config.statusLine.cachePath,
      "_cwd",
    );

    expect(cwd).not.toBeNull();
    expect(cwd!.value).toBe(process.cwd());
    expect(cwd!.ttl_seconds).toBe(999999);
  });
});

// ---------------------------------------------------------------------------
// Task 3.2 — Analytics DB wiring (GH#9 — now wired)
// ---------------------------------------------------------------------------

describe("dispatcher-wiring: analytics DB wiring", () => {
  let env: TestEnv;
  const savedStateDir = process.env.HOOKWISE_STATE_DIR;

  beforeEach(() => {
    env = createTestEnv();
    process.env.HOOKWISE_STATE_DIR = env.tmpDir;
  });

  afterEach(() => {
    env.cleanup();
    if (savedStateDir !== undefined) {
      process.env.HOOKWISE_STATE_DIR = savedStateDir;
    } else {
      delete process.env.HOOKWISE_STATE_DIR;
    }
  });

  it("dispatch(PostToolUse) → analytics DB has event row", () => {
    // Start a session first (FK constraint)
    dispatch("SessionStart", makePayload(), {
      config: env.config,
      projectDir: env.tmpDir,
    });

    dispatch("PostToolUse", makePayload({
      tool_name: "Bash",
      tool_input: { command: "ls -la" },
    }), {
      config: env.config,
      projectDir: env.tmpDir,
    });

    const { events } = readAnalyticsDB(env.dbPath);
    const toolEvents = events.filter((e: any) => e.event_type === "PostToolUse");
    expect(toolEvents.length).toBeGreaterThanOrEqual(1);
    expect((toolEvents[0] as any).tool_name).toBe("Bash");
    expect((toolEvents[0] as any).session_id).toBe("integ-test-session");
  });

  it("dispatch(SessionStart) → session row created", () => {
    dispatch("SessionStart", makePayload({
      session_id: "analytics-session-1",
    }), {
      config: env.config,
      projectDir: env.tmpDir,
    });

    const { sessions } = readAnalyticsDB(env.dbPath);
    expect(sessions).toHaveLength(1);
    expect((sessions[0] as any).id).toBe("analytics-session-1");
    expect((sessions[0] as any).started_at).toBeDefined();
    expect((sessions[0] as any).ended_at).toBeNull();
  });

  it("dispatch(SessionEnd) → session row updated", () => {
    // Start then end a session
    dispatch("SessionStart", makePayload({
      session_id: "analytics-session-2",
    }), {
      config: env.config,
      projectDir: env.tmpDir,
    });

    dispatch("SessionEnd", makePayload({
      session_id: "analytics-session-2",
    }), {
      config: env.config,
      projectDir: env.tmpDir,
    });

    const { sessions } = readAnalyticsDB(env.dbPath);
    expect(sessions).toHaveLength(1);
    expect((sessions[0] as any).id).toBe("analytics-session-2");
    expect((sessions[0] as any).ended_at).not.toBeNull();
  });
});

// ---------------------------------------------------------------------------
// Task 3.3 — Fail-open and fault isolation
// ---------------------------------------------------------------------------

describe("dispatcher-wiring: fail-open and fault isolation", () => {
  let env: TestEnv;
  const savedStateDir = process.env.HOOKWISE_STATE_DIR;

  beforeEach(() => {
    env = createTestEnv();
    process.env.HOOKWISE_STATE_DIR = env.tmpDir;
  });

  afterEach(() => {
    env.cleanup();
    if (savedStateDir !== undefined) {
      process.env.HOOKWISE_STATE_DIR = savedStateDir;
    } else {
      delete process.env.HOOKWISE_STATE_DIR;
    }
  });

  it("side-effect handler throws → exitCode still 0 (fail-open)", () => {
    // Create a script that exits non-zero (simulating a throw)
    const failScript = join(env.tmpDir, "fail-handler.sh");
    writeFileSync(failScript, "#!/bin/bash\nexit 1\n", { mode: 0o755 });

    // Config with a side-effect handler that fails
    const config = {
      ...env.config,
      handlers: [
        {
          name: "failing-side-effect",
          type: "script" as const,
          events: ["PostToolUse" as const],
          phase: "side_effect" as const,
          command: `bash ${failScript}`,
        },
      ],
    };

    const result = dispatch("PostToolUse", makePayload(), {
      config,
      projectDir: env.tmpDir,
    });

    // Fail-open: exitCode must be 0 even when side-effect handler fails
    expect(result.exitCode).toBe(0);
  });

  it("side-effect handler throws → other side-effects still execute (fault isolation)", () => {
    // Create a script that exits non-zero (the "failing" handler)
    const failScript = join(env.tmpDir, "fail-handler.sh");
    writeFileSync(failScript, "#!/bin/bash\nexit 1\n", { mode: 0o755 });

    // Create a script that writes a marker file (the "surviving" handler)
    const markerPath = join(env.tmpDir, "side-effect-marker.txt");
    const successScript = join(env.tmpDir, "success-handler.sh");
    writeFileSync(
      successScript,
      `#!/bin/bash\necho "executed" > "${markerPath}"\n`,
      { mode: 0o755 },
    );

    // Config with TWO side-effect handlers: first fails, second writes a marker
    const config = {
      ...env.config,
      handlers: [
        {
          name: "failing-side-effect",
          type: "script" as const,
          events: ["PostToolUse" as const],
          phase: "side_effect" as const,
          command: `bash ${failScript}`,
        },
        {
          name: "surviving-side-effect",
          type: "script" as const,
          events: ["PostToolUse" as const],
          phase: "side_effect" as const,
          command: `bash ${successScript}`,
        },
      ],
    };

    const result = dispatch("PostToolUse", makePayload(), {
      config,
      projectDir: env.tmpDir,
    });

    // Exit code is still 0 (fail-open)
    expect(result.exitCode).toBe(0);

    // The surviving handler must have executed and written the marker
    expect(existsSync(markerPath)).toBe(true);
    const marker = readFileSync(markerPath, "utf-8").trim();
    expect(marker).toBe("executed");

    // Heartbeat + CWD also written (they run before handlers)
    const heartbeat = readKey<CacheEntry & { value: number }>(
      config.statusLine.cachePath,
      "_dispatch_heartbeat",
    );
    expect(heartbeat).not.toBeNull();
  });
});
