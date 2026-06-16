package core

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// --- Custom Error Types ---

// HookwiseError is the base error type.
type HookwiseError struct {
	Message string
}

func (e *HookwiseError) Error() string { return e.Message }

// ConfigError represents configuration-related errors.
type ConfigError struct {
	HookwiseError
}

func NewConfigError(msg string) *ConfigError {
	return &ConfigError{HookwiseError{Message: msg}}
}

// HandlerTimeoutError is raised when a script handler exceeds its timeout.
type HandlerTimeoutError struct {
	HookwiseError
	HandlerName string
	TimeoutMs   int
}

func NewHandlerTimeoutError(name string, timeoutMs int) *HandlerTimeoutError {
	return &HandlerTimeoutError{
		HookwiseError: HookwiseError{
			Message: fmt.Sprintf("handler %q timed out after %dms", name, timeoutMs),
		},
		HandlerName: name,
		TimeoutMs:   timeoutMs,
	}
}

// DispatchTimeoutError is raised when the overall dispatch pipeline exceeds its timeout.
type DispatchTimeoutError struct {
	HookwiseError
	EventType string
	TimeoutMs int
	ElapsedMs int64
	Phase     string
}

func NewDispatchTimeoutError(eventType string, timeoutMs int, elapsedMs int64, phase string) *DispatchTimeoutError {
	return &DispatchTimeoutError{
		HookwiseError: HookwiseError{
			Message: fmt.Sprintf("dispatch for %q timed out after %dms (limit %dms, phase %s)", eventType, elapsedMs, timeoutMs, phase),
		},
		EventType: eventType,
		TimeoutMs: timeoutMs,
		ElapsedMs: elapsedMs,
		Phase:     phase,
	}
}

// StateError represents state management errors.
type StateError struct {
	HookwiseError
}

func NewStateError(msg string) *StateError {
	return &StateError{HookwiseError{Message: msg}}
}

// AnalyticsError represents analytics/DB errors.
type AnalyticsError struct {
	HookwiseError
}

func NewAnalyticsError(msg string) *AnalyticsError {
	return &AnalyticsError{HookwiseError{Message: msg}}
}

// --- Structured Logging via slog ---

// maxLogBytes is the log file size threshold (10 MB) that triggers rotation.
const maxLogBytes int64 = 10 * 1024 * 1024

// logLevelFromEnv returns slog.LevelDebug if HOOKWISE_LOG_LEVEL == "debug"
// (case-insensitive), otherwise slog.LevelInfo.
func logLevelFromEnv() slog.Level {
	if strings.EqualFold(os.Getenv("HOOKWISE_LOG_LEVEL"), "debug") {
		return slog.LevelDebug
	}
	return slog.LevelInfo
}

// rotateLogIfNeeded renames logPath to logPath+".1" (overwriting any existing
// backup) when the file exists and its size exceeds maxBytes. Errors are
// swallowed — logging setup must never crash dispatch (ARCH-1 fail-open).
func rotateLogIfNeeded(logPath string, maxBytes int64) {
	info, err := os.Stat(logPath)
	if err != nil {
		// File does not exist or is inaccessible — nothing to rotate.
		return
	}
	if info.Size() > maxBytes {
		// Best-effort: ignore rename errors.
		_ = os.Rename(logPath, logPath+".1")
	}
}

var (
	logger     *slog.Logger
	loggerOnce sync.Once
)

// Logger returns the singleton slog.Logger, writing to ~/.hookwise/logs/hookwise.log.
// Falls back to stderr if the log file can't be opened.
func Logger() *slog.Logger {
	loggerOnce.Do(func() {
		logDir := filepath.Join(GetStateDir(), "logs")
		if err := os.MkdirAll(logDir, DefaultDirMode); err != nil {
			logger = slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
			return
		}
		logPath := filepath.Join(logDir, "hookwise.log")
		rotateLogIfNeeded(logPath, maxLogBytes)
		f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			logger = slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
			return
		}
		logger = slog.New(slog.NewJSONHandler(f, &slog.HandlerOptions{Level: logLevelFromEnv()}))
	})
	return logger
}

// SafeDispatch wraps a function with panic recovery for fail-open guarantee.
// On panic, logs the error and returns exit code 0 (fail-open per ARCH-1).
func SafeDispatch(fn func() DispatchResult) (result DispatchResult) {
	defer func() {
		if r := recover(); r != nil {
			Logger().Error("panic in dispatch", "recovered", fmt.Sprintf("%v", r))
			result = DispatchResult{ExitCode: 0}
		}
	}()
	return fn()
}

// SafeDispatchWithWarnings wraps a function with panic recovery and warning
// collection. On panic, the warning is recorded in the collector AND logged.
// Exit code remains 0 in all cases (fail-open per ARCH-1).
func SafeDispatchWithWarnings(wc *WarningCollector, fn func() DispatchResult) (result DispatchResult) {
	defer func() {
		if r := recover(); r != nil {
			msg := fmt.Sprintf("panic: %v", r)
			Logger().Error("panic in dispatch", "recovered", msg)
			if wc != nil {
				wc.Add("dispatch", msg)
			}
			result = DispatchResult{ExitCode: 0}
		}
	}()
	return fn()
}
