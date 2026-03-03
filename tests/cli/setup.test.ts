/**
 * Tests for the setup CLI command.
 *
 * Captures console.log output and mocks environment variables, fs, and child_process.
 * Does NOT mock React/Ink — setup commands use plain stdout.
 *
 * Covers:
 * - setup calendar: prints already configured when token exists
 * - setup calendar: prints instructions when env vars missing
 * - setup calendar: writes credentials JSON when env vars present
 * - setup calendar: runs OAuth flow via Python script
 * - setup calendar: handles OAuth flow failure gracefully
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
    writeFileSync: vi.fn(),
    mkdirSync: vi.fn(),
  };
});

// --- Mock node:child_process ---
vi.mock("node:child_process", async () => {
  const actual = await vi.importActual<typeof import("node:child_process")>("node:child_process");
  return {
    ...actual,
    execFileSync: vi.fn(),
  };
});

import { runSetupCommand } from "../../src/cli/commands/setup.js";
import { existsSync, writeFileSync, mkdirSync } from "node:fs";
import { execFileSync } from "node:child_process";

const mockedExistsSync = vi.mocked(existsSync);
const mockedWriteFileSync = vi.mocked(writeFileSync);
const mockedMkdirSync = vi.mocked(mkdirSync);
const mockedExecFileSync = vi.mocked(execFileSync);

describe("setup CLI command", () => {
  let logSpy: ReturnType<typeof vi.spyOn>;
  let errorSpy: ReturnType<typeof vi.spyOn>;
  const originalEnv = { ...process.env };

  beforeEach(() => {
    vi.clearAllMocks();
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
    it("prints already configured when token file exists", async () => {
      // Token file exists — already set up
      mockedExistsSync.mockReturnValue(true);

      await runSetupCommand("calendar");

      const allOutput = logSpy.mock.calls.map((c) => c[0]).join("\n");
      expect(allOutput).toContain("already configured");
      expect(process.exitCode).toBeUndefined();
    });

    it("prints setup instructions when no env vars are set", async () => {
      mockedExistsSync.mockReturnValue(false);

      await runSetupCommand("calendar");

      const allOutput = logSpy.mock.calls.map((c) => c[0]).join("\n");
      expect(allOutput).toContain("Setting up Google Calendar integration");
      expect(allOutput).toContain("Google API credentials not found");
      expect(allOutput).toContain("console.cloud.google.com");
      expect(allOutput).toContain("HOOKWISE_GOOGLE_CLIENT_ID");
      expect(allOutput).toContain("HOOKWISE_GOOGLE_CLIENT_SECRET");
      expect(process.exitCode).toBe(1);
    });

    it("writes credentials JSON when env vars are present", async () => {
      // Token doesn't exist, credentials don't exist
      mockedExistsSync.mockReturnValue(false);
      // Make Python setup succeed and final token check succeed
      mockedExecFileSync.mockReturnValue("Calendar OAuth setup complete. Token saved to /tmp/token.json");
      process.env.HOOKWISE_GOOGLE_CLIENT_ID = "test-client-id";
      process.env.HOOKWISE_GOOGLE_CLIENT_SECRET = "test-client-secret";

      // After OAuth flow, token exists on final check
      let callCount = 0;
      mockedExistsSync.mockImplementation((path: unknown) => {
        const p = String(path);
        // First call: token check (false)
        // Second call: credentials check (false — need to write)
        // Third call: final token verification (true)
        if (p.includes("calendar-token")) {
          callCount++;
          return callCount > 1; // false first time, true second time
        }
        return false;
      });

      await runSetupCommand("calendar");

      // Should have written the credentials file
      expect(mockedWriteFileSync).toHaveBeenCalledTimes(1);
      const writtenContent = JSON.parse(mockedWriteFileSync.mock.calls[0][1] as string);
      expect(writtenContent.installed.client_id).toBe("test-client-id");
      expect(writtenContent.installed.client_secret).toBe("test-client-secret");
      expect(writtenContent.installed.auth_uri).toBe("https://accounts.google.com/o/oauth2/auth");
      expect(writtenContent.installed.token_uri).toBe("https://oauth2.googleapis.com/token");
      expect(writtenContent.installed.redirect_uris).toEqual(["http://localhost"]);
    });

    it("runs Python script in --setup mode when env vars are present", async () => {
      process.env.HOOKWISE_GOOGLE_CLIENT_ID = "test-client-id";
      process.env.HOOKWISE_GOOGLE_CLIENT_SECRET = "test-client-secret";

      let tokenCheckCount = 0;
      mockedExistsSync.mockImplementation((path: unknown) => {
        const p = String(path);
        if (p.includes("calendar-token")) {
          tokenCheckCount++;
          return tokenCheckCount > 1; // false first, true after OAuth
        }
        return false; // credentials file doesn't exist
      });

      mockedExecFileSync.mockReturnValue("Calendar OAuth setup complete.\n");

      await runSetupCommand("calendar");

      // Verify Python script was called with --setup
      expect(mockedExecFileSync).toHaveBeenCalledTimes(1);
      const args = mockedExecFileSync.mock.calls[0];
      expect(args[0]).toBe("python3");
      expect(args[1]).toContain("--setup");
      expect(args[1]).toContain("--credentials");

      const allOutput = logSpy.mock.calls.map((c) => c[0]).join("\n");
      expect(allOutput).toContain("Calendar setup complete");
      expect(process.exitCode).toBeUndefined();
    });

    it("prints error when OAuth flow fails gracefully (no crash)", async () => {
      process.env.HOOKWISE_GOOGLE_CLIENT_ID = "test-client-id";
      process.env.HOOKWISE_GOOGLE_CLIENT_SECRET = "test-client-secret";

      // Token never exists
      mockedExistsSync.mockReturnValue(false);

      // Python script throws
      const execError = new Error("Python script failed") as Error & { stderr: string };
      execError.stderr = "OAuth consent was cancelled";
      mockedExecFileSync.mockImplementation(() => {
        throw execError;
      });

      await runSetupCommand("calendar");

      // Should not crash — fail-open principle
      const allErrors = errorSpy.mock.calls.map((c) => c[0]).join("\n");
      expect(allErrors).toContain("OAuth");
      expect(process.exitCode).toBe(2);
    });

    it("skips writing credentials if file already exists", async () => {
      process.env.HOOKWISE_GOOGLE_CLIENT_ID = "test-client-id";
      process.env.HOOKWISE_GOOGLE_CLIENT_SECRET = "test-client-secret";

      let tokenCheckCount = 0;
      mockedExistsSync.mockImplementation((path: unknown) => {
        const p = String(path);
        if (p.includes("calendar-token")) {
          tokenCheckCount++;
          return tokenCheckCount > 1; // false first, true after OAuth
        }
        if (p.includes("calendar-credentials")) {
          return true; // credentials already exist
        }
        return false;
      });

      mockedExecFileSync.mockReturnValue("Calendar OAuth setup complete.\n");

      await runSetupCommand("calendar");

      // Should NOT have written credentials — they already existed
      expect(mockedWriteFileSync).not.toHaveBeenCalled();

      const allOutput = logSpy.mock.calls.map((c) => c[0]).join("\n");
      expect(allOutput).toContain("Using existing credentials");
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
