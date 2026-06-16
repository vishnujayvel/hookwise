package core

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLogLevelFromEnv(t *testing.T) {
	// Note: t.Setenv forbids t.Parallel — do not parallelize.
	tests := []struct {
		name     string
		envValue string // empty string means unset
		unset    bool
		want     slog.Level
	}{
		{name: "unset returns Info", unset: true, want: slog.LevelInfo},
		{name: "debug lowercase returns Debug", envValue: "debug", want: slog.LevelDebug},
		{name: "DEBUG uppercase returns Debug", envValue: "DEBUG", want: slog.LevelDebug},
		{name: "info returns Info", envValue: "info", want: slog.LevelInfo},
		{name: "garbage returns Info", envValue: "garbage", want: slog.LevelInfo},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.unset {
				t.Setenv("HOOKWISE_LOG_LEVEL", "")
			} else {
				t.Setenv("HOOKWISE_LOG_LEVEL", tc.envValue)
			}
			got := logLevelFromEnv()
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestRotateLogIfNeeded(t *testing.T) {
	t.Run("file larger than threshold is rotated", func(t *testing.T) {
		dir := t.TempDir()
		logPath := filepath.Join(dir, "hookwise.log")

		// Write 200 bytes — above the 100-byte test threshold.
		content := make([]byte, 200)
		for i := range content {
			content[i] = 'a'
		}
		require.NoError(t, os.WriteFile(logPath, content, 0o600))

		rotateLogIfNeeded(logPath, 100)

		assert.NoFileExists(t, logPath, "original log should have been renamed away")
		backup := logPath + ".1"
		assert.FileExists(t, backup, "backup .1 should exist")

		got, err := os.ReadFile(backup)
		require.NoError(t, err)
		assert.Equal(t, content, got, "backup should contain original content")
	})

	t.Run("file at or below threshold is not rotated", func(t *testing.T) {
		dir := t.TempDir()
		logPath := filepath.Join(dir, "hookwise.log")

		content := make([]byte, 50)
		require.NoError(t, os.WriteFile(logPath, content, 0o600))

		rotateLogIfNeeded(logPath, 100)

		assert.FileExists(t, logPath, "log file should still exist")
		assert.NoFileExists(t, logPath+".1", "backup should not have been created")
	})

	t.Run("missing file is a no-op", func(t *testing.T) {
		dir := t.TempDir()
		logPath := filepath.Join(dir, "nonexistent.log")

		// Must not panic.
		rotateLogIfNeeded(logPath, 100)

		assert.NoFileExists(t, logPath)
		assert.NoFileExists(t, logPath+".1")
	})

	t.Run("existing backup is overwritten", func(t *testing.T) {
		dir := t.TempDir()
		logPath := filepath.Join(dir, "hookwise.log")
		backupPath := logPath + ".1"

		newContent := make([]byte, 200)
		for i := range newContent {
			newContent[i] = 'n'
		}
		oldContent := []byte("old")

		require.NoError(t, os.WriteFile(logPath, newContent, 0o600))
		require.NoError(t, os.WriteFile(backupPath, oldContent, 0o600))

		rotateLogIfNeeded(logPath, 100)

		assert.NoFileExists(t, logPath, "original log should have been renamed away")
		got, err := os.ReadFile(backupPath)
		require.NoError(t, err)
		assert.Equal(t, newContent, got, "backup should now contain the new (rotated) content, overwriting the old backup")
	})
}
