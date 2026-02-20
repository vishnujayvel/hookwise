/**
 * Tests for HookResult assertion methods.
 *
 * Verifies:
 * - Each assertion method with passing and failing cases
 * - JSON parsing of stdout (valid, invalid, empty)
 * - Descriptive error messages on assertion failure
 */

import { describe, it, expect } from "vitest";
import { HookResult } from "../../src/testing/hook-result.js";

describe("HookResult", () => {
  describe("constructor", () => {
    it("stores stdout, stderr, exitCode, and durationMs", () => {
      const result = new HookResult("out", "err", 0, 42);
      expect(result.stdout).toBe("out");
      expect(result.stderr).toBe("err");
      expect(result.exitCode).toBe(0);
      expect(result.durationMs).toBe(42);
    });
  });

  describe("json getter", () => {
    it("parses valid JSON stdout", () => {
      const result = new HookResult('{"decision":"block","reason":"test"}', "", 0, 10);
      expect(result.json).toEqual({ decision: "block", reason: "test" });
    });

    it("returns null for invalid JSON stdout", () => {
      const result = new HookResult("not json", "", 0, 10);
      expect(result.json).toBeNull();
    });

    it("returns null for empty stdout", () => {
      const result = new HookResult("", "", 0, 10);
      expect(result.json).toBeNull();
    });

    it("returns null for non-object JSON (number)", () => {
      const result = new HookResult("42", "", 0, 10);
      expect(result.json).toBeNull();
    });

    it("returns null for non-object JSON (string)", () => {
      const result = new HookResult('"hello"', "", 0, 10);
      expect(result.json).toBeNull();
    });

    it("caches the parsed result", () => {
      const result = new HookResult('{"key":"value"}', "", 0, 10);
      const first = result.json;
      const second = result.json;
      expect(first).toBe(second); // Same reference (cached)
    });
  });

  describe("assertAllowed", () => {
    it("passes when exit code is 0 and no block decision", () => {
      const result = new HookResult("", "", 0, 10);
      expect(() => result.assertAllowed()).not.toThrow();
    });

    it("passes with non-block decision", () => {
      const result = new HookResult(
        '{"decision":"confirm"}',
        "",
        0,
        10
      );
      expect(() => result.assertAllowed()).not.toThrow();
    });

    it("fails when exit code is non-zero", () => {
      const result = new HookResult("", "", 2, 10);
      expect(() => result.assertAllowed()).toThrow(/exit code/i);
    });

    it("fails when stdout contains block decision", () => {
      const result = new HookResult(
        '{"decision":"block","reason":"denied"}',
        "",
        0,
        10
      );
      expect(() => result.assertAllowed()).toThrow(/block/i);
    });
  });

  describe("assertBlocked", () => {
    it("passes when stdout has block decision", () => {
      const result = new HookResult(
        '{"decision":"block","reason":"forbidden"}',
        "",
        2,
        10
      );
      expect(() => result.assertBlocked()).not.toThrow();
    });

    it("passes with reason check when reason contains substring", () => {
      const result = new HookResult(
        '{"decision":"block","reason":"dangerous command detected"}',
        "",
        2,
        10
      );
      expect(() => result.assertBlocked("dangerous")).not.toThrow();
    });

    it("fails when no block decision", () => {
      const result = new HookResult("", "", 0, 10);
      expect(() => result.assertBlocked()).toThrow(/block/i);
    });

    it("fails when decision is not block", () => {
      const result = new HookResult(
        '{"decision":"confirm"}',
        "",
        0,
        10
      );
      expect(() => result.assertBlocked()).toThrow(/block/i);
    });

    it("fails when reason doesn't contain expected substring", () => {
      const result = new HookResult(
        '{"decision":"block","reason":"access denied"}',
        "",
        2,
        10
      );
      expect(() => result.assertBlocked("force push")).toThrow(
        /force push/
      );
    });
  });

  describe("assertWarns", () => {
    it("passes when stdout has warn decision", () => {
      const result = new HookResult(
        '{"decision":"warn","reason":"caution"}',
        "",
        0,
        10
      );
      expect(() => result.assertWarns()).not.toThrow();
    });

    it("passes when stderr contains warning text", () => {
      const result = new HookResult("", "WARNING: something", 0, 10);
      expect(() => result.assertWarns()).not.toThrow();
    });

    it("passes with message check", () => {
      const result = new HookResult(
        '{"decision":"warn","reason":"caution advised"}',
        "",
        0,
        10
      );
      expect(() => result.assertWarns("caution")).not.toThrow();
    });

    it("fails when no warning found", () => {
      const result = new HookResult("", "", 0, 10);
      expect(() => result.assertWarns()).toThrow(/warn/i);
    });

    it("fails when message doesn't contain expected substring", () => {
      const result = new HookResult(
        '{"decision":"warn","reason":"general warning"}',
        "",
        0,
        10
      );
      expect(() => result.assertWarns("specific text")).toThrow(
        /specific text/
      );
    });
  });

  describe("assertSilent", () => {
    it("passes when no output and exit code 0", () => {
      const result = new HookResult("", "", 0, 10);
      expect(() => result.assertSilent()).not.toThrow();
    });

    it("passes with whitespace-only output", () => {
      const result = new HookResult("  \n", "  \t", 0, 10);
      // Actually these are NOT empty after trim
      // Let's check: "  \n".trim() === "" is true
      expect(() => result.assertSilent()).not.toThrow();
    });

    it("fails when stdout has content", () => {
      const result = new HookResult("some output", "", 0, 10);
      expect(() => result.assertSilent()).toThrow(/stdout/i);
    });

    it("fails when stderr has content", () => {
      const result = new HookResult("", "some error", 0, 10);
      expect(() => result.assertSilent()).toThrow(/stderr/i);
    });

    it("fails when exit code is non-zero", () => {
      const result = new HookResult("", "", 1, 10);
      expect(() => result.assertSilent()).toThrow(/exit code/i);
    });
  });

  describe("assertAsks", () => {
    it("passes when stdout has confirm decision", () => {
      const result = new HookResult(
        '{"decision":"confirm"}',
        "",
        0,
        10
      );
      expect(() => result.assertAsks()).not.toThrow();
    });

    it("fails when no confirm decision", () => {
      const result = new HookResult("", "", 0, 10);
      expect(() => result.assertAsks()).toThrow(/confirm/i);
    });

    it("fails when decision is block instead of confirm", () => {
      const result = new HookResult(
        '{"decision":"block"}',
        "",
        2,
        10
      );
      expect(() => result.assertAsks()).toThrow(/confirm/i);
    });
  });
});
