/**
 * Setup CLI command for configuring external integrations.
 *
 * Uses plain stdout (not React/Ink) so it works in scripts and pipelines.
 * Called directly from runCli() in app.tsx, bypassing the render() path.
 *
 * Currently supports: calendar (Google Calendar OAuth - stub).
 *
 * Requirements: FR-10.1, FR-10.2, FR-10.3, FR-10.4, FR-10.5, NFR-3
 */

import { DEFAULT_CALENDAR_CREDENTIALS_PATH } from "../../core/constants.js";
import { existsSync } from "node:fs";

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
 * Set up Google Calendar integration.
 *
 * This is a stub implementation. The full OAuth flow requires
 * Google API credentials which will be implemented in a future release.
 */
async function setupCalendar(): Promise<void> {
  console.log("Setting up Google Calendar integration...\n");

  // Step 1: Check for existing credentials
  if (existsSync(DEFAULT_CALENDAR_CREDENTIALS_PATH)) {
    console.log(`Existing credentials found at ${DEFAULT_CALENDAR_CREDENTIALS_PATH}`);
    console.log("Calendar integration is already configured.");
    return;
  }

  console.log("No existing credentials found.\n");

  // Step 2: Check for Google client ID
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
    return;
  }

  // Step 3: Client ID present but OAuth not implemented yet
  console.log("Google API credentials detected.");
  console.log("OAuth flow not yet implemented. Coming in a future release.");
  console.log(`\nCredentials will be stored at: ${DEFAULT_CALENDAR_CREDENTIALS_PATH}`);
}
