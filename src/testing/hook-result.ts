/**
 * HookResult — assertion methods for hook test results.
 *
 * Provides fluent assertions for checking hook output:
 * allowed, blocked, warns, silent, and confirm decisions.
 */

/**
 * Result of a hook command execution with assertion methods.
 *
 * Captures stdout, stderr, exit code, and duration from a subprocess
 * hook execution. Provides assertion methods that throw descriptive
 * errors on failure.
 */
export class HookResult {
  readonly stdout: string;
  readonly stderr: string;
  readonly exitCode: number;
  readonly durationMs: number;
  private _json: Record<string, unknown> | null | undefined = undefined;

  constructor(
    stdout: string,
    stderr: string,
    exitCode: number,
    durationMs: number
  ) {
    this.stdout = stdout;
    this.stderr = stderr;
    this.exitCode = exitCode;
    this.durationMs = durationMs;
  }

  /**
   * Parse stdout as JSON, caching the result.
   * Returns null if stdout is not valid JSON.
   */
  get json(): Record<string, unknown> | null {
    if (this._json !== undefined) return this._json;

    try {
      if (!this.stdout || this.stdout.trim() === "") {
        this._json = null;
        return null;
      }
      const parsed = JSON.parse(this.stdout);
      if (typeof parsed === "object" && parsed !== null) {
        this._json = parsed as Record<string, unknown>;
      } else {
        this._json = null;
      }
    } catch {
      this._json = null;
    }
    return this._json;
  }

  /**
   * Assert the hook allowed the operation.
   * Expects exit code 0 and no block decision in stdout.
   */
  assertAllowed(): void {
    if (this.exitCode !== 0) {
      throw new Error(
        `Expected hook to allow (exit code 0), but got exit code ${this.exitCode}\n` +
          `stdout: ${this.stdout}\nstderr: ${this.stderr}`
      );
    }

    const parsed = this.json;
    if (parsed && parsed.decision === "block") {
      throw new Error(
        `Expected hook to allow, but got decision: "block"\n` +
          `stdout: ${this.stdout}`
      );
    }
  }

  /**
   * Assert the hook blocked the operation.
   * Expects stdout to contain a block decision.
   *
   * @param reasonContains - Optional substring to check in the reason
   */
  assertBlocked(reasonContains?: string): void {
    const parsed = this.json;
    if (!parsed || parsed.decision !== "block") {
      throw new Error(
        `Expected hook to block, but got decision: ${parsed?.decision ?? "none"}\n` +
          `stdout: ${this.stdout}\nstderr: ${this.stderr}`
      );
    }

    if (reasonContains) {
      const reason = String(parsed.reason ?? "");
      if (!reason.includes(reasonContains)) {
        throw new Error(
          `Expected block reason to contain "${reasonContains}", but got: "${reason}"`
        );
      }
    }
  }

  /**
   * Assert the hook emitted a warning.
   * Checks stdout or stderr for warning content.
   *
   * @param messageContains - Optional substring to check in warning output
   */
  assertWarns(messageContains?: string): void {
    const parsed = this.json;
    const hasWarnDecision = parsed?.decision === "warn";
    const hasWarningText =
      this.stderr.toLowerCase().includes("warn") ||
      this.stdout.toLowerCase().includes("warn");

    if (!hasWarnDecision && !hasWarningText) {
      throw new Error(
        `Expected hook to warn, but no warning found\n` +
          `stdout: ${this.stdout}\nstderr: ${this.stderr}`
      );
    }

    if (messageContains) {
      const combined = this.stdout + this.stderr;
      if (!combined.includes(messageContains)) {
        throw new Error(
          `Expected warning to contain "${messageContains}", but got:\n` +
            `stdout: ${this.stdout}\nstderr: ${this.stderr}`
        );
      }
    }
  }

  /**
   * Assert the hook was silent: no stdout, no stderr, exit code 0.
   */
  assertSilent(): void {
    if (this.exitCode !== 0) {
      throw new Error(
        `Expected silent hook (exit code 0), but got exit code ${this.exitCode}`
      );
    }
    if (this.stdout.trim() !== "") {
      throw new Error(
        `Expected no stdout, but got: ${this.stdout}`
      );
    }
    if (this.stderr.trim() !== "") {
      throw new Error(
        `Expected no stderr, but got: ${this.stderr}`
      );
    }
  }

  /**
   * Assert the hook requested confirmation.
   * Expects stdout to contain a confirm decision.
   */
  assertAsks(): void {
    const parsed = this.json;
    if (!parsed || parsed.decision !== "confirm") {
      throw new Error(
        `Expected hook to ask for confirmation (decision: "confirm"), ` +
          `but got decision: ${parsed?.decision ?? "none"}\n` +
          `stdout: ${this.stdout}\nstderr: ${this.stderr}`
      );
    }
  }
}
