/**
 * Setup CLI command for configuring external integrations.
 *
 * Uses plain stdout (not React/Ink) so it works in scripts and pipelines.
 * Called directly from runCli() in app.tsx, bypassing the render() path.
 *
 * Currently supports: calendar (Google Calendar OAuth).
 *
 * Flow for `hookwise setup calendar`:
 *   1. Check if token already exists (already configured)
 *   2. Check for env vars HOOKWISE_GOOGLE_CLIENT_ID / HOOKWISE_GOOGLE_CLIENT_SECRET
 *   3. Write credentials JSON from env vars
 *   4. Run Python script in --setup mode to trigger OAuth consent flow
 *   5. Verify token file was created
 *
 * Requirements: FR-10.1, FR-10.2, FR-10.3, FR-10.4, FR-10.5, NFR-3
 */

import {
  DEFAULT_CALENDAR_CREDENTIALS_PATH,
  DEFAULT_CALENDAR_TOKEN_PATH,
} from "../../core/constants.js";
import { existsSync, writeFileSync, mkdirSync } from "node:fs";
import { execFileSync } from "node:child_process";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

/**
 * Run the setup CLI command.
 *
 * @param target - The integration to set up (e.g., "calendar")
 */
export async function runSetupCommand(target: string): Promise<void> {
  switch (target) {
    case "calendar":
      await setupCalendar();
      break;

    default:
      console.error(`Unknown setup target: ${target}`);
      console.error("Available targets: calendar");
      console.error("Usage: hookwise setup <target>");
      process.exitCode = 1;
      break;
  }
}

/**
 * Resolve the path to scripts/calendar-feed.py relative to this module.
 */
function getScriptPath(): string {
  const thisFile = fileURLToPath(import.meta.url);
  // Navigate from dist/cli/commands/ up to package root
  const projectRoot = resolve(dirname(thisFile), "..", "..", "..");
  return resolve(projectRoot, "scripts", "calendar-feed.py");
}

/**
 * Write a Google OAuth credentials JSON file from client ID and secret.
 * Creates parent directories if needed.
 */
function writeCredentialsFile(
  path: string,
  clientId: string,
  clientSecret: string,
): void {
  const credentials = {
    installed: {
      client_id: clientId,
      client_secret: clientSecret,
      redirect_uris: ["http://localhost"],
      auth_uri: "https://accounts.google.com/o/oauth2/auth",
      token_uri: "https://oauth2.googleapis.com/token",
    },
  };

  mkdirSync(dirname(path), { recursive: true, mode: 0o700 });
  writeFileSync(path, JSON.stringify(credentials, null, 2), {
    encoding: "utf-8",
    mode: 0o600,
  });
}

/**
 * Run the Python calendar-feed.py script in --setup mode.
 * Returns true on success (exit 0), false on failure.
 */
function runOAuthSetup(credentialsPath: string): boolean {
  const scriptPath = getScriptPath();

  try {
    const stdout = execFileSync(
      "python3",
      [scriptPath, "--setup", "--credentials", credentialsPath],
      {
        encoding: "utf-8",
        stdio: ["pipe", "pipe", "pipe"],
        timeout: 120_000, // 2 minutes for user to complete OAuth in browser
      },
    );
    if (stdout.trim()) {
      console.log(stdout.trim());
    }
    return true;
  } catch (error: unknown) {
    const err = error as { stderr?: string; message?: string };
    if (err.stderr) {
      console.error(err.stderr.trim());
    } else if (err.message) {
      console.error(`OAuth setup failed: ${err.message}`);
    }
    return false;
  }
}

/**
 * Set up Google Calendar integration.
 *
 * 1. Check for existing token (already configured)
 * 2. Check for env vars with client credentials
 * 3. Write credentials JSON from env vars
 * 4. Run Python OAuth flow
 * 5. Verify token was created
 */
async function setupCalendar(): Promise<void> {
  console.log("Setting up Google Calendar integration...\n");

  // Step 1: Check for existing token — means OAuth already completed
  if (existsSync(DEFAULT_CALENDAR_TOKEN_PATH)) {
    console.log("Calendar integration is already configured.");
    console.log(`Token: ${DEFAULT_CALENDAR_TOKEN_PATH}`);
    return;
  }

  // Step 2: Check for Google client credentials in env vars
  const clientId = process.env.HOOKWISE_GOOGLE_CLIENT_ID;
  const clientSecret = process.env.HOOKWISE_GOOGLE_CLIENT_SECRET;

  if (!clientId || !clientSecret) {
    console.log("Google API credentials not found.\n");
    console.log("To set up Google Calendar integration:\n");
    console.log("  1. Go to https://console.cloud.google.com/");
    console.log("  2. Create a new project (or select an existing one)");
    console.log("  3. Enable the Google Calendar API");
    console.log("  4. Create OAuth 2.0 credentials (Desktop application type)");
    console.log("  5. Set the following environment variables:\n");
    console.log("     export HOOKWISE_GOOGLE_CLIENT_ID=<your-client-id>");
    console.log("     export HOOKWISE_GOOGLE_CLIENT_SECRET=<your-client-secret>\n");
    console.log("  6. Run 'hookwise setup calendar' again");
    // Fail gracefully — don't crash, just set non-zero exit
    process.exitCode = 1;
    return;
  }

  // Step 3: Write credentials JSON file from env vars
  console.log("Google API credentials detected.");

  // Only write the credentials file if it doesn't already exist
  if (!existsSync(DEFAULT_CALENDAR_CREDENTIALS_PATH)) {
    try {
      writeCredentialsFile(DEFAULT_CALENDAR_CREDENTIALS_PATH, clientId, clientSecret);
      console.log(`Credentials written to ${DEFAULT_CALENDAR_CREDENTIALS_PATH}`);
    } catch (error: unknown) {
      const msg = error instanceof Error ? error.message : String(error);
      console.error(`Failed to write credentials file: ${msg}`);
      process.exitCode = 2;
      return;
    }
  } else {
    console.log(`Using existing credentials at ${DEFAULT_CALENDAR_CREDENTIALS_PATH}`);
  }

  // Step 4: Run Python OAuth flow
  console.log("\nStarting OAuth flow — a browser window will open...\n");
  const success = runOAuthSetup(DEFAULT_CALENDAR_CREDENTIALS_PATH);

  if (!success) {
    console.error("\nOAuth setup did not complete successfully.");
    console.log("You can retry with: hookwise setup calendar");
    process.exitCode = 2;
    return;
  }

  // Step 5: Verify token was created
  if (existsSync(DEFAULT_CALENDAR_TOKEN_PATH)) {
    console.log("\nCalendar setup complete! You can now enable the calendar feed in hookwise.yaml:");
    console.log("\n  feeds:");
    console.log("    calendar:");
    console.log("      enabled: true");
  } else {
    console.error("\nSetup appeared to succeed but token file was not found.");
    console.error(`Expected at: ${DEFAULT_CALENDAR_TOKEN_PATH}`);
    console.log("You can retry with: hookwise setup calendar");
    process.exitCode = 2;
  }
}

// Exported for testing
export { writeCredentialsFile, runOAuthSetup, getScriptPath };
