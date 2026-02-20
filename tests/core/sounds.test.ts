/**
 * Tests for notification sounds handler.
 *
 * Verifies:
 * - getPlayCommand returns afplay on darwin, aplay on linux, null on others
 * - playSound doesn't throw on valid or missing files
 * - playSound uses non-blocking spawn behavior
 */

import { describe, it, expect, vi, afterEach, beforeEach } from "vitest";

// We need hoisted mocks for ESM modules
vi.mock("node:child_process", () => {
  const mockUnref = vi.fn();
  const mockOn = vi.fn();
  const mockSpawn = vi.fn().mockReturnValue({
    unref: mockUnref,
    on: mockOn,
  });
  return { spawn: mockSpawn };
});

let platformOverride: string | null = null;

vi.mock("node:os", () => ({
  platform: () => platformOverride ?? "darwin",
}));

// Import after mocks are set up
import { getPlayCommand, playSound } from "../../src/core/sounds.js";
import { spawn } from "node:child_process";

// Type the mock for convenience
const mockSpawn = spawn as unknown as ReturnType<typeof vi.fn>;

describe("getPlayCommand", () => {
  afterEach(() => {
    platformOverride = null;
  });

  it("returns afplay on darwin", () => {
    platformOverride = "darwin";
    const result = getPlayCommand();
    expect(result).not.toBeNull();
    expect(result!.command).toBe("afplay");
  });

  it("returns aplay on linux", () => {
    platformOverride = "linux";
    const result = getPlayCommand();
    expect(result).not.toBeNull();
    expect(result!.command).toBe("aplay");
  });

  it("returns null on unsupported platform (win32)", () => {
    platformOverride = "win32";
    const result = getPlayCommand();
    expect(result).toBeNull();
  });

  it("returns null on unsupported platform (freebsd)", () => {
    platformOverride = "freebsd";
    const result = getPlayCommand();
    expect(result).toBeNull();
  });
});

describe("playSound", () => {
  beforeEach(() => {
    mockSpawn.mockClear();
    const child = mockSpawn.mock.results[0]?.value;
    if (child) {
      child.unref?.mockClear?.();
      child.on?.mockClear?.();
    }
    mockSpawn.mockReturnValue({
      unref: vi.fn(),
      on: vi.fn(),
    });
    platformOverride = "darwin";
  });

  afterEach(() => {
    platformOverride = null;
  });

  it("does not throw on valid path", () => {
    expect(() => playSound("/some/sound.wav")).not.toThrow();
  });

  it("does not throw on missing file", () => {
    expect(() => playSound("/nonexistent/file.wav")).not.toThrow();
  });

  it("calls spawn with the sound file path", () => {
    playSound("/some/sound.wav");
    expect(mockSpawn).toHaveBeenCalled();
    const args = mockSpawn.mock.calls[0];
    expect(args[1]).toContain("/some/sound.wav");
  });

  it("calls spawn with detached:true and stdio:ignore", () => {
    playSound("/some/sound.wav");
    const options = mockSpawn.mock.calls[0][2];
    expect(options.detached).toBe(true);
    expect(options.stdio).toBe("ignore");
  });

  it("calls unref() on the spawned child", () => {
    playSound("/some/sound.wav");
    const child = mockSpawn.mock.results[0].value;
    expect(child.unref).toHaveBeenCalled();
  });

  it("does not throw when spawn itself fails", () => {
    mockSpawn.mockImplementation(() => {
      throw new Error("spawn failed");
    });
    expect(() => playSound("/some/sound.wav")).not.toThrow();
  });

  it("does not call spawn on unsupported platform", () => {
    platformOverride = "win32";
    mockSpawn.mockClear();
    playSound("/some/sound.wav");
    expect(mockSpawn).not.toHaveBeenCalled();
  });
});
