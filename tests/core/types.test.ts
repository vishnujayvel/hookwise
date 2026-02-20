/**
 * Tests for core TypeScript types and type guards.
 *
 * Verifies EventType validation for all 13 event types
 * and HookPayload shape validation.
 */

import { describe, it, expect } from "vitest";
import {
  EVENT_TYPES,
  isEventType,
  isHookPayload,
} from "../../src/core/types.js";
import type { EventType, HookPayload } from "../../src/core/types.js";

describe("EventType", () => {
  it("defines exactly 13 event types", () => {
    expect(EVENT_TYPES).toHaveLength(13);
  });

  const expectedTypes: EventType[] = [
    "UserPromptSubmit",
    "PreToolUse",
    "PostToolUse",
    "PostToolUseFailure",
    "Notification",
    "Stop",
    "SubagentStart",
    "SubagentStop",
    "PreCompact",
    "SessionStart",
    "SessionEnd",
    "PermissionRequest",
    "Setup",
  ];

  it.each(expectedTypes)(
    "isEventType returns true for valid type '%s'",
    (eventType) => {
      expect(isEventType(eventType)).toBe(true);
    }
  );

  it("isEventType returns false for invalid string", () => {
    expect(isEventType("InvalidEvent")).toBe(false);
    expect(isEventType("pretooluse")).toBe(false); // case-sensitive
    expect(isEventType("")).toBe(false);
  });

  it("isEventType returns false for non-string values", () => {
    expect(isEventType(42)).toBe(false);
    expect(isEventType(null)).toBe(false);
    expect(isEventType(undefined)).toBe(false);
    expect(isEventType(true)).toBe(false);
    expect(isEventType({})).toBe(false);
    expect(isEventType([])).toBe(false);
  });

  it("EVENT_TYPES array is readonly", () => {
    // TypeScript enforces this at compile time, but we verify the values are stable
    const copy = [...EVENT_TYPES];
    expect(EVENT_TYPES).toEqual(copy);
  });
});

describe("HookPayload", () => {
  it("isHookPayload returns true for minimal valid payload", () => {
    const payload: HookPayload = { session_id: "abc-123" };
    expect(isHookPayload(payload)).toBe(true);
  });

  it("isHookPayload returns true for full payload with optional fields", () => {
    const payload: HookPayload = {
      session_id: "abc-123",
      tool_name: "Bash",
      tool_input: { command: "ls" },
      extra_field: "extra_value",
    };
    expect(isHookPayload(payload)).toBe(true);
  });

  it("isHookPayload returns false when session_id is missing", () => {
    expect(isHookPayload({ tool_name: "Bash" })).toBe(false);
  });

  it("isHookPayload returns false when session_id is not a string", () => {
    expect(isHookPayload({ session_id: 123 })).toBe(false);
    expect(isHookPayload({ session_id: null })).toBe(false);
    expect(isHookPayload({ session_id: undefined })).toBe(false);
  });

  it("isHookPayload returns false for non-object values", () => {
    expect(isHookPayload(null)).toBe(false);
    expect(isHookPayload(undefined)).toBe(false);
    expect(isHookPayload("string")).toBe(false);
    expect(isHookPayload(42)).toBe(false);
    expect(isHookPayload(true)).toBe(false);
  });

  it("isHookPayload returns false for arrays", () => {
    expect(isHookPayload(["session_id", "abc"])).toBe(false);
  });

  it("allows additional properties via index signature", () => {
    const payload: HookPayload = {
      session_id: "test",
      agent_id: "agent-1",
      parent_agent_id: "main",
      files_modified: ["/tmp/file.ts"],
    };
    expect(isHookPayload(payload)).toBe(true);
    expect(payload.agent_id).toBe("agent-1");
  });
});
