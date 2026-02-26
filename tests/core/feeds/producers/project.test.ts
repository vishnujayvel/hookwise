/**
 * Tests for the project feed producer.
 *
 * Covers Task 4.2:
 * - Normal git repo: returns repo name, branch, last commit timestamp
 * - Detached HEAD: falls back to short commit SHA
 * - No commits: has_commits is false, last_commit_ts is 0
 * - Not a git repo: returns null
 * - Missing _cwd in cache: returns null
 *
 * All git commands are mocked via vi.mock("node:child_process").
 *
 * Requirements: FR-5.1, FR-5.2, FR-5.3, FR-5.4, FR-5.5, FR-5.6, FR-5.7
 */

import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { mkdtempSync, writeFileSync, rmSync } from "node:fs";
import { join } from "node:path";
import { tmpdir } from "node:os";

// Mock child_process before importing the module under test
vi.mock("node:child_process", () => ({
  execSync: vi.fn(),
}));

import { execSync } from "node:child_process";
import { createProjectProducer } from "../../../../src/core/feeds/producers/project.js";

const mockExecSync = vi.mocked(execSync);

describe("createProjectProducer", () => {
  let tempRoot: string;
  let cachePath: string;

  beforeEach(() => {
    tempRoot = mkdtempSync(join(tmpdir(), "hookwise-project-"));
    cachePath = join(tempRoot, "cache.json");
    vi.clearAllMocks();
  });

  afterEach(() => {
    rmSync(tempRoot, { recursive: true, force: true });
  });

  // --- Helper: set up mock responses for a normal repo ---

  function mockNormalRepo(overrides?: {
    root?: string;
    branch?: string;
    commitTs?: string;
  }) {
    const root = overrides?.root ?? "/home/user/projects/my-app";
    const branch = overrides?.branch ?? "main";
    const commitTs = overrides?.commitTs ?? "1708617600"; // 2024-02-22T16:00:00Z

    mockExecSync.mockImplementation((cmd: string) => {
      const command = cmd as string;
      if (command.includes("rev-parse --show-toplevel")) return `${root}\n`;
      if (command.includes("branch --show-current")) return `${branch}\n`;
      if (command.includes("log -1 --format=%ct")) return `${commitTs}\n`;
      return "";
    });
  }

  // --- Normal repo ---

  it("returns repo, branch, and last commit for a normal repo (FR-5.1, FR-5.2)", async () => {
    writeFileSync(cachePath, JSON.stringify({ _cwd: { value: "/home/user/projects/my-app", updated_at: new Date().toISOString(), ttl_seconds: 999999 } }));
    mockNormalRepo();

    const producer = createProjectProducer(cachePath);
    const result = await producer();

    expect(result).not.toBeNull();
    expect(result!.repo).toBe("my-app");
    expect(result!.branch).toBe("main");
    expect(result!.last_commit_ts).toBe(1708617600);
    expect(result!.detached).toBe(false);
    expect(result!.has_commits).toBe(true);
  });

  it("uses basename of git root, not the full path (FR-5.1)", async () => {
    writeFileSync(cachePath, JSON.stringify({ _cwd: { value: "/deep/nested/path/cool-project", updated_at: new Date().toISOString(), ttl_seconds: 999999 } }));
    mockNormalRepo({ root: "/deep/nested/path/cool-project" });

    const producer = createProjectProducer(cachePath);
    const result = await producer();

    expect(result).not.toBeNull();
    expect(result!.repo).toBe("cool-project");
  });

  it("uses the cached _cwd for git commands, not process.cwd (FR-5.7)", async () => {
    const cachedCwd = "/specific/project/dir";
    writeFileSync(cachePath, JSON.stringify({ _cwd: { value: cachedCwd, updated_at: new Date().toISOString(), ttl_seconds: 999999 } }));
    mockNormalRepo({ root: "/specific/project/dir" });

    const producer = createProjectProducer(cachePath);
    await producer();

    // Verify all execSync calls received the correct cwd option
    for (const call of mockExecSync.mock.calls) {
      const options = call[1] as { cwd: string };
      expect(options.cwd).toBe(cachedCwd);
    }
  });

  it("handles feature branch names (FR-5.2)", async () => {
    writeFileSync(cachePath, JSON.stringify({ _cwd: { value: "/project", updated_at: new Date().toISOString(), ttl_seconds: 999999 } }));
    mockNormalRepo({ root: "/project", branch: "feature/add-login" });

    const producer = createProjectProducer(cachePath);
    const result = await producer();

    expect(result).not.toBeNull();
    expect(result!.branch).toBe("feature/add-login");
    expect(result!.detached).toBe(false);
  });

  // --- Detached HEAD ---

  it("falls back to short SHA when HEAD is detached (FR-5.3)", async () => {
    writeFileSync(cachePath, JSON.stringify({ _cwd: { value: "/project", updated_at: new Date().toISOString(), ttl_seconds: 999999 } }));

    mockExecSync.mockImplementation((cmd: string) => {
      const command = cmd as string;
      if (command.includes("rev-parse --show-toplevel")) return "/project\n";
      if (command.includes("branch --show-current")) return "\n"; // empty = detached
      if (command.includes("rev-parse --short HEAD")) return "abc1234\n";
      if (command.includes("log -1 --format=%ct")) return "1708617600\n";
      return "";
    });

    const producer = createProjectProducer(cachePath);
    const result = await producer();

    expect(result).not.toBeNull();
    expect(result!.branch).toBe("abc1234");
    expect(result!.detached).toBe(true);
    expect(result!.has_commits).toBe(true);
  });

  // --- No commits ---

  it("handles repo with no commits (FR-5.4)", async () => {
    writeFileSync(cachePath, JSON.stringify({ _cwd: { value: "/project", updated_at: new Date().toISOString(), ttl_seconds: 999999 } }));

    mockExecSync.mockImplementation((cmd: string) => {
      const command = cmd as string;
      if (command.includes("rev-parse --show-toplevel")) return "/project\n";
      if (command.includes("branch --show-current")) return "main\n";
      if (command.includes("log -1 --format=%ct")) {
        throw new Error("fatal: your current branch 'main' does not have any commits yet");
      }
      return "";
    });

    const producer = createProjectProducer(cachePath);
    const result = await producer();

    expect(result).not.toBeNull();
    expect(result!.repo).toBe("project");
    expect(result!.branch).toBe("main");
    expect(result!.last_commit_ts).toBe(0);
    expect(result!.detached).toBe(false);
    expect(result!.has_commits).toBe(false);
  });

  // --- Not a git repo ---

  it("returns null when not inside a git repo (FR-5.5)", async () => {
    writeFileSync(cachePath, JSON.stringify({ _cwd: { value: "/not-a-repo", updated_at: new Date().toISOString(), ttl_seconds: 999999 } }));

    mockExecSync.mockImplementation(() => {
      throw new Error("fatal: not a git repository");
    });

    const producer = createProjectProducer(cachePath);
    const result = await producer();

    expect(result).toBeNull();
  });

  it("returns null when rev-parse fails (e.g., broken git)", async () => {
    writeFileSync(cachePath, JSON.stringify({ _cwd: { value: "/broken", updated_at: new Date().toISOString(), ttl_seconds: 999999 } }));

    mockExecSync.mockImplementation((cmd: string) => {
      const command = cmd as string;
      if (command.includes("rev-parse --show-toplevel")) {
        throw new Error("git: command not found");
      }
      return "";
    });

    const producer = createProjectProducer(cachePath);
    const result = await producer();

    expect(result).toBeNull();
  });

  // --- Missing _cwd ---

  it("returns null when _cwd is not set in cache (FR-5.6)", async () => {
    writeFileSync(cachePath, JSON.stringify({ session: { id: "abc" } }));

    const producer = createProjectProducer(cachePath);
    const result = await producer();

    expect(result).toBeNull();
  });

  it("returns null when cache is empty object", async () => {
    writeFileSync(cachePath, JSON.stringify({}));

    const producer = createProjectProducer(cachePath);
    const result = await producer();

    expect(result).toBeNull();
  });

  it("returns null when cache file does not exist", async () => {
    const producer = createProjectProducer(join(tempRoot, "nonexistent.json"));
    const result = await producer();

    expect(result).toBeNull();
  });

  it("returns null when _cwd is undefined", async () => {
    writeFileSync(cachePath, JSON.stringify({ _cwd: undefined }));

    const producer = createProjectProducer(cachePath);
    const result = await producer();

    expect(result).toBeNull();
  });

  // --- Edge cases ---

  it("trims whitespace from git command outputs", async () => {
    writeFileSync(cachePath, JSON.stringify({ _cwd: { value: "/project", updated_at: new Date().toISOString(), ttl_seconds: 999999 } }));

    mockExecSync.mockImplementation((cmd: string) => {
      const command = cmd as string;
      if (command.includes("rev-parse --show-toplevel")) return "  /project  \n";
      if (command.includes("branch --show-current")) return "  develop  \n";
      if (command.includes("log -1 --format=%ct")) return "  1708617600  \n";
      return "";
    });

    const producer = createProjectProducer(cachePath);
    const result = await producer();

    expect(result).not.toBeNull();
    expect(result!.repo).toBe("project");
    expect(result!.branch).toBe("develop");
    expect(result!.last_commit_ts).toBe(1708617600);
  });

  it("preserves other cache keys when reading _cwd", async () => {
    writeFileSync(
      cachePath,
      JSON.stringify({
        _cwd: { value: "/project", updated_at: new Date().toISOString(), ttl_seconds: 999999 },
        session: { id: "sess-1" },
        cost: { totalToday: 2.50 },
      }),
    );
    mockNormalRepo({ root: "/project" });

    const producer = createProjectProducer(cachePath);
    const result = await producer();

    // Producer should work correctly regardless of other cache keys
    expect(result).not.toBeNull();
    expect(result!.repo).toBe("project");
  });

  it("returns correct data shape with all expected fields", async () => {
    writeFileSync(cachePath, JSON.stringify({ _cwd: { value: "/project", updated_at: new Date().toISOString(), ttl_seconds: 999999 } }));
    mockNormalRepo({ root: "/project", branch: "main", commitTs: "1708617600" });

    const producer = createProjectProducer(cachePath);
    const result = await producer();

    expect(result).not.toBeNull();
    expect(result).toHaveProperty("repo");
    expect(result).toHaveProperty("branch");
    expect(result).toHaveProperty("last_commit_ts");
    expect(result).toHaveProperty("detached");
    expect(result).toHaveProperty("has_commits");
    expect(typeof result!.repo).toBe("string");
    expect(typeof result!.branch).toBe("string");
    expect(typeof result!.last_commit_ts).toBe("number");
    expect(typeof result!.detached).toBe("boolean");
    expect(typeof result!.has_commits).toBe("boolean");
  });
});
