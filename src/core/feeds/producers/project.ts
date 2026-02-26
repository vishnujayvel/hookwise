/**
 * Project feed producer: emits git repository metadata for the current project.
 *
 * Reads the current working directory from `cache._cwd` (written by the
 * dispatcher on every invocation), then queries git for:
 *   - Repository root directory name
 *   - Current branch (or short SHA for detached HEAD)
 *   - Last commit timestamp
 *
 * Returns null when:
 *   - `_cwd` is not set in cache
 *   - The cached CWD is not inside a git repository
 *
 * Requirements: FR-5.1, FR-5.2, FR-5.3, FR-5.4, FR-5.5, FR-5.6, FR-5.7
 */

import { execSync } from "node:child_process";
import { basename } from "node:path";
import type { FeedProducer } from "../../types.js";
import { readAll } from "../cache-bus.js";

export interface ProjectData {
  repo: string;            // basename of git root
  branch: string;          // current branch or short SHA
  last_commit_ts: number;  // unix timestamp (seconds since epoch)
  detached: boolean;
  has_commits: boolean;
}

/**
 * Execute a git command in the given working directory.
 * Returns trimmed stdout. Throws on non-zero exit.
 */
function gitExec(command: string, cwd: string): string {
  return execSync(command, {
    cwd,
    encoding: "utf-8",
    stdio: ["pipe", "pipe", "pipe"],
    timeout: 5000,
  }).trim();
}

/**
 * Create a FeedProducer for the project feed.
 *
 * @param cachePath - Path to the status-line cache JSON file
 */
export function createProjectProducer(cachePath: string): FeedProducer {
  return async (): Promise<Record<string, unknown> | null> => {
    const cache = readAll(cachePath);
    const cwdEntry = cache._cwd as Record<string, unknown> | undefined;
    const cwd = cwdEntry?.value as string | undefined;
    if (!cwd) return null;

    try {
      // Get repo root
      const root = gitExec("git rev-parse --show-toplevel", cwd);
      const repo = basename(root);

      // Get current branch
      let branch = gitExec("git branch --show-current", cwd);
      let detached = false;

      if (!branch) {
        // Detached HEAD -- fall back to short commit SHA
        branch = gitExec("git rev-parse --short HEAD", cwd);
        detached = true;
      }

      // Get last commit timestamp
      let lastCommitTs = 0;
      let hasCommits = true;

      try {
        const ts = gitExec("git log -1 --format=%ct", cwd);
        lastCommitTs = parseInt(ts, 10);
        if (Number.isNaN(lastCommitTs)) {
          lastCommitTs = 0;
          hasCommits = false;
        }
      } catch {
        // No commits in the repository
        hasCommits = false;
      }

      const result: ProjectData = {
        repo,
        branch,
        last_commit_ts: lastCommitTs,
        detached,
        has_commits: hasCommits,
      };

      return result as unknown as Record<string, unknown>;
    } catch {
      // Not a git repo or other git failure
      return null;
    }
  };
}
