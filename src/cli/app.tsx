/**
 * CLI root for hookwise v1.0
 *
 * React/Ink-based CLI with command routing.
 * Parses subcommands and renders appropriate components.
 */

import React from "react";
import { render, Text, Box } from "ink";
import { Header } from "./components/header.js";
import { InitCommand } from "./commands/init.js";
import { DoctorCommand } from "./commands/doctor.js";
import { StatusCommand } from "./commands/status.js";
import { StatsCommand } from "./commands/stats.js";
import { TestCommand } from "./commands/test.js";
import { MigrateCommand } from "./commands/migrate.js";

const COMMANDS = [
  "init",
  "doctor",
  "status",
  "stats",
  "test",
  "tui",
  "migrate",
  "dispatch",
] as const;

function HelpMessage(): React.ReactElement {
  return (
    <Box flexDirection="column">
      <Header />
      <Text bold>Usage: hookwise {"<command>"}</Text>
      <Box flexDirection="column" marginTop={1}>
        <Text bold underline>
          Commands:
        </Text>
        <Text>  init      Initialize hookwise in the current directory</Text>
        <Text>  doctor    Check system health and configuration</Text>
        <Text>  status    Show current configuration status</Text>
        <Text>  stats     Display session analytics and tool usage</Text>
        <Text>  test      Run hookwise test suite</Text>
        <Text>  tui       Launch interactive TUI</Text>
        <Text>  migrate   Migrate from Python hookwise</Text>
        <Text>  dispatch  Dispatch a hook event (fast path)</Text>
      </Box>
      <Box marginTop={1}>
        <Text dimColor>
          Run "hookwise {"<command>"} --help" for more information.
        </Text>
      </Box>
    </Box>
  );
}

/**
 * Parse CLI flags from args array.
 * Returns flags as key-value pairs.
 */
function parseFlags(args: string[]): Record<string, string | boolean> {
  const flags: Record<string, string | boolean> = {};
  for (let i = 0; i < args.length; i++) {
    const arg = args[i];
    if (arg.startsWith("--")) {
      const key = arg.slice(2);
      const next = args[i + 1];
      if (next && !next.startsWith("--")) {
        flags[key] = next;
        i++;
      } else {
        flags[key] = true;
      }
    }
  }
  return flags;
}

/**
 * Run the CLI with the given arguments.
 * Routes to appropriate React/Ink command components.
 */
export async function runCli(args: string[]): Promise<void> {
  const command = args[0];
  const restArgs = args.slice(1);
  const flags = parseFlags(restArgs);

  let element: React.ReactElement;

  switch (command) {
    case "init":
      element = (
        <InitCommand preset={typeof flags.preset === "string" ? flags.preset : undefined} />
      );
      break;

    case "doctor":
      element = <DoctorCommand />;
      break;

    case "status":
      element = (
        <StatusCommand
          configPath={typeof flags.config === "string" ? flags.config : undefined}
        />
      );
      break;

    case "stats":
      element = (
        <StatsCommand
          options={{
            json: flags.json === true,
            agents: flags.agents === true,
            cost: flags.cost === true,
            streaks: flags.streaks === true,
          }}
        />
      );
      break;

    case "test":
      element = <TestCommand />;
      break;

    case "tui": {
      const { TuiApp } = await import("./tui/app.js");
      element = <TuiApp />;
      break;
    }

    case "migrate":
      element = <MigrateCommand dryRun={flags["dry-run"] === true} />;
      break;

    default:
      element = <HelpMessage />;
      break;
  }

  const instance = render(element);
  await instance.waitUntilExit();
}
