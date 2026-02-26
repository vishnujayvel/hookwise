/**
 * Default constants for hookwise v1.0
 */

import { join } from "node:path";
import { homedir } from "node:os";

/** Default state directory: ~/.hookwise/ */
export const DEFAULT_STATE_DIR = join(homedir(), ".hookwise");

/** Default analytics database path: ~/.hookwise/analytics.db */
export const DEFAULT_DB_PATH = join(DEFAULT_STATE_DIR, "analytics.db");

/** Default status-line cache path: ~/.hookwise/state/status-line-cache.json */
export const DEFAULT_CACHE_PATH = join(
  DEFAULT_STATE_DIR,
  "state",
  "status-line-cache.json"
);

/** Default log directory: ~/.hookwise/logs/ */
export const DEFAULT_LOG_PATH = join(DEFAULT_STATE_DIR, "logs");

/** Default handler timeout in seconds */
export const DEFAULT_HANDLER_TIMEOUT = 10;

/** Default status line segment delimiter */
export const DEFAULT_STATUS_DELIMITER = " | ";

/** Max error log file size in bytes (10 MB) */
export const MAX_LOG_SIZE_BYTES = 10 * 1024 * 1024;

/** Number of rotated log files to keep */
export const MAX_LOG_ROTATIONS = 3;

/** Default directory permissions (owner-only) */
export const DEFAULT_DIR_MODE = 0o700;

/** Default database file permissions (user-only) */
export const DEFAULT_DB_MODE = 0o600;

/** Default transcript backup directory: ~/.hookwise/transcripts/ */
export const DEFAULT_TRANSCRIPT_DIR = join(DEFAULT_STATE_DIR, "transcripts");

/** Project-level config file name */
export const PROJECT_CONFIG_FILE = "hookwise.yaml";

/** Global config file path: ~/.hookwise/config.yaml */
export const GLOBAL_CONFIG_PATH = join(DEFAULT_STATE_DIR, "config.yaml");

/** Default daemon PID file path: ~/.hookwise/daemon.pid */
export const DEFAULT_PID_PATH = join(DEFAULT_STATE_DIR, "daemon.pid");

/** Default daemon log file path: ~/.hookwise/daemon.log */
export const DEFAULT_DAEMON_LOG_PATH = join(DEFAULT_STATE_DIR, "daemon.log");

/** Default calendar credentials path: ~/.hookwise/calendar-credentials.json */
export const DEFAULT_CALENDAR_CREDENTIALS_PATH = join(DEFAULT_STATE_DIR, "calendar-credentials.json");

/** Default feed timeout in seconds */
export const DEFAULT_FEED_TIMEOUT = 10; // seconds
