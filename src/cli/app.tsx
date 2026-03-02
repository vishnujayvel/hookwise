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
import { runDaemonCommand } from "./commands/daemon.js";
import { runSetupCommand } from "./commands/setup.js";
import { runStatusLineCommand } from "./commands/status-line.js";
import { FeedsCommand } from "./commands/feeds.js";

const COMMANDS = [
  "init",
  "doctor",
  "status",
  "status-line",
  "stats",
  "test",
  "tui",
  "migrate",
  "dispatch",
  "daemon",
  "setup",
  "feeds",
] as const;

type CommandName = (typeof COMMANDS)[number];

interface CommandHelp {
  description: string;
  flags?: string[];
  usage?: string;
}

const COMMAND_HELP: Record<string, CommandHelp> = {
  init: {
    description: "Initialize hookwise in the current directory",
    flags: ["--preset <name>  Use a preset: minimal, coaching, analytics, full"],
    usage: "hookwise init --preset coaching",
  },
  doctor: {
    description: "Check system health and configuration",
  },
  status: {
    description: "Show current configuration status",
    flags: ["--config <path>  Path to config file"],
  },
  stats: {
    description: "Display session analytics and tool usage",
    flags: [
      "--json     Output as structured JSON",
      "--agents   Include agent activity summary",
      "--cost     Include cost breakdown",
      "--streaks  Include streak summary",
    ],
    usage: "hookwise stats --json --agents",
  },
  test: {
    description: "Run hookwise test suite",
  },
  tui: {
    description: "Launch interactive terminal UI",
  },
  migrate: {
    description: "Migrate from Python hookwise",
    flags: ["--dry-run  Preview changes without applying"],
  },
  dispatch: {
    description: "Dispatch a hook event (fast path, used by Claude Code)",
    usage: "hookwise dispatch PreToolUse < payload.json",
  },
  daemon: {
    description: "Manage the background feed daemon",
    flags: ["--config <path>  Path to config file"],
    usage: "hookwise daemon start",
  },
  setup: {
    description: "Set up external integrations",
    usage: "hookwise setup calendar",
  },
  feeds: {
    description: "Live feed dashboard — shows daemon, feed health, and cache bus",
    flags: [
      "--once     Show snapshot and exit (no auto-refresh)",
      "--config <path>  Path to config file",
    ],
    usage: "hookwise feeds",
  },
  "status-line": {
    description: "Render two-tier status line (reads stdin JSON from Claude Code)",
    usage: 'echo \'{"session_id":"test","context_window":{"used_percentage":0.67}}\' | hookwise status-line',
  },
};

export function SubcommandHelp({
  command,
  help,
}: {
  command: string;
  help: CommandHelp;
}): React.ReactElement {
  return (
    <Box flexDirection="column">
      <Header />
      <Text bold>hookwise {command}</Text>
      <Box marginTop={1}>
        <Text>{help.description}</Text>
      </Box>
      {help.flags && help.flags.length > 0 && (
        <Box flexDirection="column" marginTop={1}>
          <Text bold underline>
            Flags:
          </Text>
          {help.flags.map((flag) => (
            <Text key={flag}>  {flag}</Text>
          ))}
        </Box>
      )}
      {help.usage && (
        <Box flexDirection="column" marginTop={1}>
          <Text bold underline>
            Example:
          </Text>
          <Text>  $ {help.usage}</Text>
        </Box>
      )}
    </Box>
  );
}

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
        <Text>  status-line Render two-tier status line (stdin JSON)</Text>
        <Text>  stats     Display session analytics and tool usage</Text>
        <Text>  test      Run hookwise test suite</Text>
        <Text>  tui       Launch interactive TUI</Text>
        <Text>  migrate   Migrate from Python hookwise</Text>
        <Text>  dispatch  Dispatch a hook event (fast path)</Text>
        <Text>  daemon    Manage the background feed daemon</Text>
        <Text>  feeds     Live feed dashboard</Text>
        <Text>  setup     Set up external integrations</Text>
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

  // --help on subcommand: show help instead of running the command
  if (flags.help === true && command && COMMAND_HELP[command]) {
    element = <SubcommandHelp command={command} help={COMMAND_HELP[command]} />;
    const instance = render(element);
    await instance.waitUntilExit();
    return;
  }

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
      const { execSync } = await import("node:child_process");
      const { existsSync: tuiExists } = await import("node:fs");
      const { join: tuiJoin } = await import("node:path");

      // Try the bundled venv first, then system python
      const tuiVenvPython = tuiJoin(
        import.meta.dirname ?? ".",
        "..",
        "..",
        "tui",
        ".venv",
        "bin",
        "python3",
      );
      const pythonCmd = tuiExists(tuiVenvPython) ? tuiVenvPython : "python3";

      try {
        execSync(`${pythonCmd} -m hookwise_tui`, {
          stdio: "inherit",
          cwd: process.cwd(),
          env: {
            ...process.env,
            HOOKWISE_CONFIG:
              typeof flags.config === "string" ? flags.config : process.cwd(),
          },
        });
      } catch {
        console.error(
          "Python TUI not found. Install with: pip install hookwise-tui",
        );
        console.error("Or from source: cd tui && pip install -e .");
        process.exitCode = 1;
      }
      return;
    }

    case "migrate":
      element = <MigrateCommand dryRun={flags["dry-run"] === true} />;
      break;

    case "daemon": {
      const subcommand = restArgs[0];
      if (!subcommand) {
        console.error("Usage: hookwise daemon <start|stop|status>");
        process.exitCode = 1;
        return;
      }
      await runDaemonCommand(
        subcommand,
        typeof flags.config === "string" ? flags.config : undefined,
      );
      return;
    }

    case "status-line": {
      await runStatusLineCommand();
      return;
    }

    case "feeds":
      element = (
        <FeedsCommand
          configPath={typeof flags.config === "string" ? flags.config : undefined}
          once={flags.once === true}
        />
      );
      break;

    case "setup": {
      const target = restArgs[0];
      if (!target) {
        console.error("Usage: hookwise setup <target>");
        console.error("Available targets: calendar");
        process.exitCode = 1;
        return;
      }
      await runSetupCommand(target);
      return;
    }

    default:
      element = <HelpMessage />;
      break;
  }

  const instance = render(element);
  await instance.waitUntilExit();
}
