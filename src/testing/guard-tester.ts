/**
 * GuardTester — in-process guard rule testing for hookwise v1.0
 *
 * Tests guard rules without spawning subprocesses. Loads rules from
 * config files, config dicts, or direct rule arrays, then evaluates
 * tool calls against them using the core guard engine.
 */

import { evaluate } from "../core/guards.js";
import { loadConfig } from "../core/config.js";
import type {
  GuardRule,
  GuardResult,
  GuardRuleConfig,
  TestScenario,
  ScenarioResult,
} from "../core/types.js";

/**
 * Options for creating a GuardTester.
 *
 * Three mutually exclusive ways to provide guards:
 * - configPath: load from a YAML config file
 * - configDict: provide a parsed config object
 * - guards: provide guard rules directly
 */
export interface GuardTesterOptions {
  configPath?: string;
  configDict?: Record<string, unknown>;
  guards?: GuardRuleConfig[];
}

/**
 * In-process guard rule tester.
 *
 * Evaluates tool calls against guard rules using the core guard engine
 * without spawning subprocesses. Supports assertion methods and batch
 * scenario testing.
 */
export class GuardTester {
  private _rules: GuardRule[];

  /**
   * Create a GuardTester with guards from one of three sources.
   *
   * @param options - Configuration source for guard rules
   */
  constructor(options: GuardTesterOptions) {
    if (options.guards) {
      this._rules = options.guards.map((g) => ({
        match: g.match,
        action: g.action,
        reason: g.reason,
        when: g.when,
        unless: g.unless,
      }));
    } else if (options.configDict) {
      const guards = options.configDict.guards as GuardRuleConfig[] | undefined;
      this._rules = (guards ?? []).map((g) => ({
        match: g.match,
        action: g.action,
        reason: g.reason,
        when: g.when,
        unless: g.unless,
      }));
    } else if (options.configPath) {
      const config = loadConfig(options.configPath);
      this._rules = config.guards.map((g) => ({
        match: g.match,
        action: g.action,
        reason: g.reason,
        when: g.when,
        unless: g.unless,
      }));
    } else {
      this._rules = [];
    }
  }

  /**
   * Get the loaded guard rules.
   */
  get rules(): GuardRule[] {
    return this._rules;
  }

  /**
   * Test a tool call against the loaded guard rules.
   *
   * @param toolName - Name of the tool being called
   * @param toolInput - Optional tool input parameters
   * @returns GuardResult with action, reason, and matched rule
   */
  testToolCall(
    toolName: string,
    toolInput?: Record<string, unknown>
  ): GuardResult {
    return evaluate(toolName, toolInput ?? {}, this._rules);
  }

  /**
   * Assert that a tool call is blocked.
   * Throws if the tool call is not blocked.
   *
   * @param toolName - Name of the tool
   * @param toolInput - Optional tool input
   * @param reasonContains - Optional substring to check in the block reason
   */
  assertBlocked(
    toolName: string,
    toolInput?: Record<string, unknown>,
    reasonContains?: string
  ): void {
    const result = this.testToolCall(toolName, toolInput);
    if (result.action !== "block") {
      throw new Error(
        `Expected "${toolName}" to be blocked, but got action: "${result.action}"`
      );
    }
    if (reasonContains && result.reason) {
      if (!result.reason.includes(reasonContains)) {
        throw new Error(
          `Expected block reason to contain "${reasonContains}", but got: "${result.reason}"`
        );
      }
    }
  }

  /**
   * Assert that a tool call is allowed.
   * Throws if the tool call is not allowed.
   *
   * @param toolName - Name of the tool
   * @param toolInput - Optional tool input
   */
  assertAllowed(
    toolName: string,
    toolInput?: Record<string, unknown>
  ): void {
    const result = this.testToolCall(toolName, toolInput);
    if (result.action !== "allow") {
      throw new Error(
        `Expected "${toolName}" to be allowed, but got action: "${result.action}" ` +
          `(reason: ${result.reason ?? "none"})`
      );
    }
  }

  /**
   * Assert that a tool call triggers a warning.
   * Throws if the tool call does not warn.
   *
   * @param toolName - Name of the tool
   * @param toolInput - Optional tool input
   */
  assertWarns(
    toolName: string,
    toolInput?: Record<string, unknown>
  ): void {
    const result = this.testToolCall(toolName, toolInput);
    if (result.action !== "warn") {
      throw new Error(
        `Expected "${toolName}" to warn, but got action: "${result.action}"`
      );
    }
  }

  /**
   * Run a batch of test scenarios and return pass/fail results.
   *
   * @param scenarios - Array of test scenarios to evaluate
   * @returns Array of results with pass/fail status
   */
  runScenarios(scenarios: TestScenario[]): ScenarioResult[] {
    return scenarios.map((scenario) => {
      const guardResult = this.testToolCall(
        scenario.toolName,
        scenario.toolInput
      );
      const passed = guardResult.action === scenario.expected;
      return { scenario, guardResult, passed };
    });
  }
}
