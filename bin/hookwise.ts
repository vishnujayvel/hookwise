#!/usr/bin/env node

/**
 * hookwise CLI entry point
 *
 * Fast-path routing: `dispatch` subcommand loads only core modules (no React/Ink).
 * All other subcommands load the full CLI with React/Ink components.
 */

import type { EventType } from "../src/core/types.js";

const subcommand = process.argv[2];

if (subcommand === "dispatch") {
  // Fast path: load only core modules, no React/Ink
  const { dispatch, readStdinPayload, safeDispatch } = await import(
    "../src/core/dispatcher.js"
  );
  const { isEventType } = await import("../src/core/types.js");

  const eventType = process.argv[3];

  if (!eventType || !isEventType(eventType)) {
    // Unknown event type: fail-open (exit 0)
    process.exit(0);
  }

  const payload = readStdinPayload();
  const result = safeDispatch(() => dispatch(eventType, payload));

  if (result.stdout) {
    process.stdout.write(result.stdout);
  }
  if (result.stderr) {
    process.stderr.write(result.stderr);
  }
  process.exit(result.exitCode);
} else if (subcommand === "--version") {
  // Quick version check without loading heavy modules
  const { readFileSync, existsSync } = await import("node:fs");
  const { resolve, dirname } = await import("node:path");
  const { fileURLToPath } = await import("node:url");
  let dir = dirname(fileURLToPath(import.meta.url));
  // Walk up to find package.json (works from both src and dist)
  while (dir !== dirname(dir)) {
    const candidate = resolve(dir, "package.json");
    if (existsSync(candidate)) {
      const pkg = JSON.parse(readFileSync(candidate, "utf-8"));
      console.log(`hookwise ${pkg.version}`);
      process.exit(0);
    }
    dir = dirname(dir);
  }
  console.log("hookwise (unknown version)");
} else {
  // CLI path: load React/Ink for rich terminal output
  const { runCli } = await import("../src/cli/app.js");
  await runCli(process.argv.slice(2));
}
