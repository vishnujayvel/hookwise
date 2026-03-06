package core

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
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
		f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			logger = slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
			return
		}
		logger = slog.New(slog.NewJSONHandler(f, &slog.HandlerOptions{Level: slog.LevelDebug}))
	})
	return logger
}

// SafeDispatch wraps a function with panic recovery for fail-open guarantee.
// On panic, logs the error and returns exit code 0.
func SafeDispatch(fn func() DispatchResult) DispatchResult {
	defer func() {
		if r := recover(); r != nil {
			Logger().Error("panic in dispatch", "recovered", fmt.Sprintf("%v", r))
		}
	}()
	return fn()
}
