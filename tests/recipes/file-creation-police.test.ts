/**
 * Tests for the File Creation Police recipe handler.
 */

import { describe, it, expect } from "vitest";
import type { HookPayload } from "../../src/core/types.js";
import {
  trackFileCreation,
  findSimilarFiles,
  stripTrailingDigits,
} from "../../recipes/quality/file-creation-police/handler.js";
import type {
  FileCreationConfig,
  FileCreationState,
} from "../../recipes/quality/file-creation-police/handler.js";

function makePayload(overrides: Partial<HookPayload> = {}): HookPayload {
  return {
    session_id: "test-session",
    ...overrides,
  };
}

function freshState(): FileCreationState {
  return {
    filesCreatedThisSession: [],
    creationCount: 0,
    warningEmitted: false,
  };
}

const config: FileCreationConfig = {
  enabled: true,
  maxNewFiles: 5,
  ignorePatterns: ["*.test.ts", "*.spec.ts"],
};

describe("stripTrailingDigits", () => {
  it("strips trailing digits from filename", () => {
    expect(stripTrailingDigits("utils2.ts")).toBe("utils");
    expect(stripTrailingDigits("helper123.ts")).toBe("helper");
    expect(stripTrailingDigits("test_001.py")).toBe("test_");
  });

  it("preserves filename without trailing digits", () => {
    expect(stripTrailingDigits("config.ts")).toBe("config");
    expect(stripTrailingDigits("index.js")).toBe("index");
  });

  it("handles filename that is all digits", () => {
    // If stripping removes everything, keep original stem
    expect(stripTrailingDigits("123.ts")).toBe("123");
  });

  it("handles filename with digits in the middle", () => {
    expect(stripTrailingDigits("file2name3.ts")).toBe("file2name");
  });

  it("handles filename without extension", () => {
    expect(stripTrailingDigits("Dockerfile")).toBe("Dockerfile");
  });

  it("strips only from the stem, not the extension", () => {
    expect(stripTrailingDigits("data.mp3")).toBe("data");
  });
});

describe("findSimilarFiles", () => {
  it("finds similar files by stripped stem", () => {
    const target = "/app/utils2.ts";
    const existing = ["/app/utils.ts", "/app/utils3.ts", "/app/helpers.ts"];

    const similar = findSimilarFiles(target, existing);
    expect(similar).toContain("/app/utils.ts");
    expect(similar).toContain("/app/utils3.ts");
    expect(similar).not.toContain("/app/helpers.ts");
  });

  it("only matches same directory", () => {
    const target = "/app/src/utils2.ts";
    const existing = ["/app/utils.ts", "/app/src/utils.ts"];

    const similar = findSimilarFiles(target, existing);
    expect(similar).toContain("/app/src/utils.ts");
    expect(similar).not.toContain("/app/utils.ts");
  });

  it("only matches same extension", () => {
    const target = "/app/config2.ts";
    const existing = ["/app/config.ts", "/app/config.js", "/app/config.json"];

    const similar = findSimilarFiles(target, existing);
    expect(similar).toContain("/app/config.ts");
    expect(similar).not.toContain("/app/config.js");
    expect(similar).not.toContain("/app/config.json");
  });

  it("is case-insensitive", () => {
    const target = "/app/Utils2.ts";
    const existing = ["/app/utils.ts"];

    const similar = findSimilarFiles(target, existing);
    expect(similar).toContain("/app/utils.ts");
  });

  it("does not match the exact same file", () => {
    const target = "/app/utils.ts";
    const existing = ["/app/utils.ts"];

    const similar = findSimilarFiles(target, existing);
    expect(similar).toHaveLength(0);
  });

  it("returns empty for no matches", () => {
    const target = "/app/brand-new-file.ts";
    const existing = ["/app/existing.ts", "/app/other.ts"];

    const similar = findSimilarFiles(target, existing);
    expect(similar).toHaveLength(0);
  });
});

describe("trackFileCreation", () => {
  it("allows first file creation", () => {
    const state = freshState();
    const event = makePayload({
      tool_name: "Write",
      tool_input: { file_path: "/app/index.ts" },
    });

    const result = trackFileCreation(event, state, config);
    expect(result.action).toBe("allow");
    expect(state.creationCount).toBe(1);
  });

  it("tracks created files in state", () => {
    const state = freshState();
    const event = makePayload({
      tool_name: "Write",
      tool_input: { file_path: "/app/new-file.ts" },
    });

    trackFileCreation(event, state, config);
    expect(state.filesCreatedThisSession).toContain("/app/new-file.ts");
  });

  it("warns when max files exceeded", () => {
    const state = freshState();
    // Create 5 files with distinct names (no trailing-digit similarity)
    const distinctNames = ["alpha.ts", "bravo.ts", "charlie.ts", "delta.ts", "echo.ts"];
    for (const name of distinctNames) {
      const event = makePayload({
        tool_name: "Write",
        tool_input: { file_path: `/app/${name}` },
      });
      trackFileCreation(event, state, config);
    }

    // 6th file triggers warning
    const event = makePayload({
      tool_name: "Write",
      tool_input: { file_path: "/app/foxtrot.ts" },
    });
    const result = trackFileCreation(event, state, config);
    expect(result.action).toBe("warn");
    expect(result.message).toContain("6 new files");
  });

  it("only emits max-files warning once", () => {
    const state = freshState();
    // Create 6 files with distinct names
    const distinctNames = ["alpha.ts", "bravo.ts", "charlie.ts", "delta.ts", "echo.ts", "foxtrot.ts"];
    for (const name of distinctNames) {
      const event = makePayload({
        tool_name: "Write",
        tool_input: { file_path: `/app/${name}` },
      });
      trackFileCreation(event, state, config);
    }

    expect(state.warningEmitted).toBe(true);

    // 7th file should not re-warn for max files (already emitted)
    const event = makePayload({
      tool_name: "Write",
      tool_input: { file_path: "/app/golf.ts" },
    });
    const result = trackFileCreation(event, state, config);
    // It should allow now since the warning was already emitted
    expect(result.action).toBe("allow");
  });

  it("warns on similar file creation", () => {
    const state = freshState();
    // Create utils.ts
    const event1 = makePayload({
      tool_name: "Write",
      tool_input: { file_path: "/app/utils.ts" },
    });
    trackFileCreation(event1, state, config);

    // Create utils2.ts — should detect similarity
    const event2 = makePayload({
      tool_name: "Write",
      tool_input: { file_path: "/app/utils2.ts" },
    });
    const result = trackFileCreation(event2, state, config);
    expect(result.action).toBe("warn");
    expect(result.similarFile).toBe("/app/utils.ts");
  });

  it("ignores test files via ignorePatterns", () => {
    const state = freshState();
    // Create 10 test files — should not count toward limit
    for (let i = 0; i < 10; i++) {
      const event = makePayload({
        tool_name: "Write",
        tool_input: { file_path: `/app/file${i}.test.ts` },
      });
      const result = trackFileCreation(event, state, config);
      expect(result.action).toBe("allow");
    }
    expect(state.creationCount).toBe(0);
  });

  it("ignores non-Write tools", () => {
    const state = freshState();
    const event = makePayload({
      tool_name: "Edit",
      tool_input: { file_path: "/app/index.ts" },
    });

    const result = trackFileCreation(event, state, config);
    expect(result.action).toBe("allow");
    expect(state.creationCount).toBe(0);
  });

  it("allows when disabled", () => {
    const state = freshState();
    const event = makePayload({
      tool_name: "Write",
      tool_input: { file_path: "/app/index.ts" },
    });

    const result = trackFileCreation(event, state, { ...config, enabled: false });
    expect(result.action).toBe("allow");
  });

  it("does not double-count same file", () => {
    const state = freshState();
    const event = makePayload({
      tool_name: "Write",
      tool_input: { file_path: "/app/index.ts" },
    });

    trackFileCreation(event, state, config);
    trackFileCreation(event, state, config);
    expect(state.creationCount).toBe(1);
  });

  it("handles missing file_path", () => {
    const state = freshState();
    const event = makePayload({
      tool_name: "Write",
      tool_input: {},
    });

    const result = trackFileCreation(event, state, config);
    expect(result.action).toBe("allow");
  });
});
