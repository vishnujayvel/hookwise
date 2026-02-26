/**
 * Tests for the setup CLI command.
 *
 * Captures console.log output and mocks environment variables.
 * Does NOT mock React/Ink — setup commands use plain stdout.
 *
 * Covers Task 7.2:
 * - setup calendar: prints setup instructions when no client ID
 * - setup calendar: prints not implemented when client ID present
 * - setup calendar: prints already configured when credentials exist
 * - setup unknown: prints error for unknown target
 *
 * Requirements: FR-10.1, FR-10.2, FR-10.3, FR-10.4, FR-10.5, NFR-3
 */

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";

// --- Mock node:fs ---
vi.mock("node:fs", async () => {
  const actual = await vi.importActual<typeof import("node:fs")>("node:fs");
  return {
    ...actual,
    existsSync: vi.fn(),
  };
});

import { runSetupCommand } from "../../src/cli/commands/setup.js";
import { existsSync } from "node:fs";

const mockedExistsSync = vi.mocked(existsSync);

describe("setup CLI command", () => {
  let logSpy: ReturnType<typeof vi.spyOn>;
  let errorSpy: ReturnType<typeof vi.spyOn>;
  const originalEnv = { ...process.env };

  beforeEach(() => {
    logSpy = vi.spyOn(console, "log").mockImplementation(() => {});
    errorSpy = vi.spyOn(console, "error").mockImplementation(() => {});
    process.exitCode = undefined;
    // Clear relevant env vars
    delete process.env.HOOKWISE_GOOGLE_CLIENT_ID;
    delete process.env.HOOKWISE_GOOGLE_CLIENT_SECRET;
  });

  afterEach(() => {
    logSpy.mockRestore();
    errorSpy.mockRestore();
    vi.restoreAllMocks();
    process.exitCode = undefined;
    // Restore env
    process.env = { ...originalEnv };
  });

  describe("calendar", () => {
    it("prints setup instructions when no client ID env vars are set", async () => {
      mockedExistsSync.mockReturnValue(false);

      await runSetupCommand("calendar");

      const allOutput = logSpy.mock.calls.map((c) => c[0]).join("\n");
      expect(allOutput).toContain("Setting up Google Calendar integration");
      expect(allOutput).toContain("Google API credentials not found");
      expect(allOutput).toContain("console.cloud.google.com");
      expect(allOutput).toContain("HOOKWISE_GOOGLE_CLIENT_ID");
      expect(allOutput).toContain("HOOKWISE_GOOGLE_CLIENT_SECRET");
    });

    it("prints not implemented when client ID is present", async () => {
      mockedExistsSync.mockReturnValue(false);
      process.env.HOOKWISE_GOOGLE_CLIENT_ID = "test-client-id";
      process.env.HOOKWISE_GOOGLE_CLIENT_SECRET = "test-client-secret";

      await runSetupCommand("calendar");

      const allOutput = logSpy.mock.calls.map((c) => c[0]).join("\n");
      expect(allOutput).toContain("Google API credentials detected");
      expect(allOutput).toContain("OAuth flow not yet implemented. Coming in a future release.");
    });

    it("prints already configured when credentials file exists", async () => {
      mockedExistsSync.mockReturnValue(true);

      await runSetupCommand("calendar");

      const allOutput = logSpy.mock.calls.map((c) => c[0]).join("\n");
      expect(allOutput).toContain("already configured");
    });
  });

  describe("unknown target", () => {
    it("prints error for unknown setup target", async () => {
      await runSetupCommand("slack");

      expect(errorSpy).toHaveBeenCalledWith("Unknown setup target: slack");
      expect(errorSpy).toHaveBeenCalledWith("Available targets: calendar");
      expect(process.exitCode).toBe(1);
    });
  });
});
