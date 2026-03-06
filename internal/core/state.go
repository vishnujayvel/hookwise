package core

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// GetStateDir returns the hookwise state directory.
// Priority: HOOKWISE_STATE_DIR env var, then ~/.hookwise/
func GetStateDir() string {
	if dir := os.Getenv("HOOKWISE_STATE_DIR"); dir != "" {
		return dir
	}
	return DefaultStateDir
}

// EnsureDir creates a directory recursively with the given permissions.
// Idempotent: does nothing if the directory already exists.
func EnsureDir(dirPath string, mode os.FileMode) error {
	return os.MkdirAll(dirPath, mode)
}

// AtomicWriteJSON writes JSON data to a file atomically via temp file + rename.
func AtomicWriteJSON(filePath string, data interface{}) error {
	dir := filepath.Dir(filePath)
	if err := EnsureDir(dir, DefaultDirMode); err != nil {
		return fmt.Errorf("ensure dir: %w", err)
	}

	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		return fmt.Errorf("random bytes: %w", err)
	}
	suffix := hex.EncodeToString(b)
	tmpPath := filepath.Join(dir, ".tmp-"+suffix)

	content, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal json: %w", err)
	}
	content = append(content, '\n')

	if err := os.WriteFile(tmpPath, content, 0o600); err != nil {
		return fmt.Errorf("write temp: %w", err)
	}

	if err := os.Rename(tmpPath, filePath); err != nil {
		os.Remove(tmpPath) // best-effort cleanup
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}

// SafeReadJSON reads and parses a JSON file, returning the fallback on any error.
func SafeReadJSON(filePath string, target interface{}, fallback interface{}) interface{} {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return fallback
	}
	if err := json.Unmarshal(data, target); err != nil {
		return fallback
	}
	return target
}
