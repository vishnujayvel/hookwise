/**
 * Tests for ANSI color utilities.
 */

import { describe, it, expect } from "vitest";
import { color, strip, RED, GREEN, YELLOW, DIM, BOLD, CYAN } from "../../../src/core/status-line/ansi.js";

describe("color", () => {
  it("wraps text in ANSI escape codes", () => {
    const result = color("hello", RED);
    expect(result).toBe("\x1b[31mhello\x1b[0m");
  });

  it("handles GREEN code", () => {
    const result = color("ok", GREEN);
    expect(result).toBe("\x1b[32mok\x1b[0m");
  });

  it("handles YELLOW code", () => {
    const result = color("warn", YELLOW);
    expect(result).toBe("\x1b[33mwarn\x1b[0m");
  });

  it("handles DIM code", () => {
    const result = color("dim", DIM);
    expect(result).toBe("\x1b[2mdim\x1b[0m");
  });

  it("handles BOLD code", () => {
    const result = color("bold", BOLD);
    expect(result).toBe("\x1b[1mbold\x1b[0m");
  });

  it("handles CYAN code", () => {
    const result = color("info", CYAN);
    expect(result).toBe("\x1b[36minfo\x1b[0m");
  });

  it("returns text unchanged when colorCode is empty", () => {
    const result = color("plain", "");
    expect(result).toBe("plain");
  });

  it("handles empty text", () => {
    const result = color("", RED);
    expect(result).toBe("\x1b[31m\x1b[0m");
  });
});

describe("strip", () => {
  it("removes ANSI escape codes", () => {
    const colored = "\x1b[31mhello\x1b[0m";
    expect(strip(colored)).toBe("hello");
  });

  it("removes multiple ANSI codes", () => {
    const colored = "\x1b[1m\x1b[31mbold red\x1b[0m\x1b[0m";
    expect(strip(colored)).toBe("bold red");
  });

  it("returns plain text unchanged", () => {
    expect(strip("no colors here")).toBe("no colors here");
  });

  it("handles empty string", () => {
    expect(strip("")).toBe("");
  });

  it("strips from color() output", () => {
    const colored = color("test", GREEN);
    expect(strip(colored)).toBe("test");
  });
});
