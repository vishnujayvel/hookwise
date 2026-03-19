package core

import (
	"fmt"
	"os"
	"path/filepath"
)

var (
	// DefaultStateDir is ~/.hookwise/
	DefaultStateDir = filepath.Join(HomeDir(), ".hookwise")

	// DefaultDBPath is ~/.hookwise/analytics.db
	DefaultDBPath = filepath.Join(DefaultStateDir, "analytics.db")

	// DefaultCachePath is ~/.hookwise/state/status-line-cache.json
	DefaultCachePath = filepath.Join(DefaultStateDir, "state", "status-line-cache.json")

	// DefaultLogPath is ~/.hookwise/logs/
	DefaultLogPath = filepath.Join(DefaultStateDir, "logs")

	// DefaultTranscriptDir is ~/.hookwise/transcripts/
	DefaultTranscriptDir = filepath.Join(DefaultStateDir, "transcripts")

	// GlobalConfigPath is ~/.hookwise/config.yaml
	GlobalConfigPath = filepath.Join(DefaultStateDir, "config.yaml")

	// DefaultPIDPath is ~/.hookwise/daemon.pid
	DefaultPIDPath = filepath.Join(DefaultStateDir, "daemon.pid")

	// DefaultSocketPath is ~/.hookwise/daemon.sock
	DefaultSocketPath = filepath.Join(DefaultStateDir, "daemon.sock")

	// DefaultDaemonLogPath is ~/.hookwise/daemon.log
	DefaultDaemonLogPath = filepath.Join(DefaultStateDir, "daemon.log")

	// DefaultCalendarCredentialsPath is ~/.hookwise/calendar-credentials.json
	DefaultCalendarCredentialsPath = filepath.Join(DefaultStateDir, "calendar-credentials.json")

	// DefaultCalendarTokenPath is ~/.hookwise/calendar-token.json
	DefaultCalendarTokenPath = filepath.Join(DefaultStateDir, "calendar-token.json")

	// LastStatusOutputPath is ~/.hookwise/cache/last-status-output.txt
	LastStatusOutputPath = filepath.Join(DefaultStateDir, "cache", "last-status-output.txt")
)

const (
	// DefaultHandlerTimeout in seconds.
	DefaultHandlerTimeout = 10

	// DefaultStatusDelimiter for status line segments.
	DefaultStatusDelimiter = " | "

	// MaxLogSizeBytes is the max error log file size (10 MB).
	MaxLogSizeBytes = 10 * 1024 * 1024

	// MaxLogRotations is the number of rotated log files to keep.
	MaxLogRotations = 3

	// DefaultDirMode is owner-only directory permissions.
	DefaultDirMode = 0o700

	// DefaultDBMode is user-only file permissions.
	DefaultDBMode = 0o600

	// ProjectConfigFile is the project-level config file name.
	ProjectConfigFile = "hookwise.yaml"

	// DefaultFeedTimeout in seconds.
	DefaultFeedTimeout = 10

	// DefaultDispatchTimeoutMs is the total dispatch pipeline timeout in milliseconds.
	DefaultDispatchTimeoutMs = 500
)

// HomeDir returns the user's home directory, falling back to the system
// temp directory if os.UserHomeDir() is unavailable.
func HomeDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		tmp := os.TempDir()
		fmt.Fprintf(os.Stderr, "hookwise: UserHomeDir unavailable, falling back to %s: %v\n", tmp, err)
		return tmp
	}
	return home
}
