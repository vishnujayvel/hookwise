/**
 * Test command — runs hookwise test suite.
 *
 * Discovers and runs tests using vitest programmatically,
 * showing a results summary.
 */

import React, { useEffect, useState } from "react";
import { Text, Box } from "ink";
import { exec as execCb } from "node:child_process";
import { Header } from "../components/header.js";
import { StatusBadge } from "../components/status-badge.js";

export interface TestCommandProps {
  projectDir?: string;
  /** Injected exec function for testing. Defaults to child_process.exec. */
  execFn?: typeof execCb;
}

interface TestResult {
  passed: number;
  failed: number;
  total: number;
}

export function TestCommand({
  projectDir,
  execFn,
}: TestCommandProps): React.ReactElement {
  const [result, setResult] = useState<TestResult | null>(null);
  const [running, setRunning] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    const dir = projectDir ?? process.cwd();
    const execImpl = execFn ?? execCb;

    const child = execImpl(
      "npx vitest run 2>&1 || true",
      { cwd: dir, encoding: "utf-8", timeout: 120000 },
      (err, stdout, _stderr) => {
        if (err && !stdout) {
          setError(err instanceof Error ? err.message : String(err));
          setRunning(false);
          return;
        }

        const output = typeof stdout === "string" ? stdout : "";
        const passedMatch = output.match(/(\d+) passed/);
        const failedMatch = output.match(/(\d+) failed/);
        const passed = passedMatch ? parseInt(passedMatch[1], 10) : 0;
        const failed = failedMatch ? parseInt(failedMatch[1], 10) : 0;

        setResult({ passed, failed, total: passed + failed });
        setRunning(false);
      }
    );

    return () => {
      try {
        child.kill();
      } catch {
        // Process may have already exited
      }
    };
  }, [projectDir, execFn]);

  if (running) {
    return (
      <Box flexDirection="column">
        <Header />
        <Text>Running tests...</Text>
      </Box>
    );
  }

  if (error) {
    return (
      <Box flexDirection="column">
        <Header />
        <Text color="red">Test runner error: {error}</Text>
      </Box>
    );
  }

  if (!result) {
    return (
      <Box flexDirection="column">
        <Header />
        <Text>No test results</Text>
      </Box>
    );
  }

  return (
    <Box flexDirection="column">
      <Header />
      <Text bold>Test Results</Text>
      <Box flexDirection="column" marginTop={1}>
        <Box gap={1}>
          <StatusBadge status={result.failed === 0 ? "pass" : "fail"} />
          <Text>
            {result.passed} passed, {result.failed} failed ({result.total}{" "}
            total)
          </Text>
        </Box>
      </Box>
    </Box>
  );
}
