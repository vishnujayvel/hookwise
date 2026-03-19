// Package analytics provides the Dolt embedded database layer for hookwise.
//
// It wraps the Dolt embedded driver (github.com/dolthub/driver) behind a thin
// DB type that enforces serialised writes (ARCH-2) and batch commits per
// dispatch cycle (ARCH-8).
package analytics

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	_ "github.com/dolthub/driver" // registers "dolt" sql driver
	"github.com/vishnujayvel/hookwise/internal/core"
)

// validDoltRef matches valid Dolt refs (commit hashes, branch names, tags).
// Only alphanumeric, dots, hyphens, underscores, slashes, tildes, and carets.
var validDoltRef = regexp.MustCompile(`^[a-zA-Z0-9._/~^-]+$`)

// ---------------------------------------------------------------------------
// Dolt diff / log result types
// ---------------------------------------------------------------------------

// DiffEntry represents a single row returned by DOLT_DIFF().
type DiffEntry struct {
	TableName  string
	DiffType   string // "added", "modified", "removed"
	FromCommit string
	ToCommit   string
	RowData    map[string]interface{} // remaining columns keyed by name
}

// LogEntry represents a single commit returned by DOLT_LOG.
type LogEntry struct {
	CommitHash string
	Committer  string
	Email      string
	Date       time.Time
	Message    string
}

// ---------------------------------------------------------------------------
// DB – the Dolt embedded database wrapper
// ---------------------------------------------------------------------------

// DB wraps a *sql.DB pointing at an embedded Dolt instance.
// All public methods are safe to call from any goroutine; however, the
// underlying Dolt embedded engine serialises execution on a single connection,
// so callers should expect writes to queue behind reads.
type DB struct {
	db      *sql.DB
	dataDir string // parent directory (Dolt databases live as sub-dirs)
	dbName  string // logical database name inside Dolt
}

// DefaultDoltDir returns the conventional Dolt data directory.
func DefaultDoltDir() string {
	return filepath.Join(core.HomeDir(), ".hookwise", "dolt")
}

// Open creates (if needed) and opens the Dolt embedded database at dataDir.
//
// dataDir is the *parent* directory that Dolt uses to store database
// subdirectories.  The logical database name is "hookwise".
//
// On first open the complete DDL schema (10 tables) is applied idempotently.
func Open(dataDir string) (*DB, error) {
	if dataDir == "" {
		dataDir = DefaultDoltDir()
	}

	// Ensure the parent directory exists.
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, fmt.Errorf("analytics: mkdir %s: %w", dataDir, err)
	}

	absDir, err := filepath.Abs(dataDir)
	if err != nil {
		return nil, fmt.Errorf("analytics: abs path: %w", err)
	}

	const dbName = "hookwise"
	dsn := fmt.Sprintf("file://%s?commitname=hookwise&commitemail=hookwise@local&database=%s",
		absDir, dbName)

	db, err := sql.Open("dolt", dsn)
	if err != nil {
		return nil, fmt.Errorf("analytics: sql.Open: %w", err)
	}

	// Force a single connection – Dolt embedded is single-threaded.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	d := &DB{
		db:      db,
		dataDir: absDir,
		dbName:  dbName,
	}

	if err := d.initSchema(context.Background()); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("analytics: init schema: %w", err)
	}

	return d, nil
}

// Close releases the underlying database connection.
func (d *DB) Close() error {
	if d.db != nil {
		return d.db.Close()
	}
	return nil
}

// ---------------------------------------------------------------------------
// Query helpers
// ---------------------------------------------------------------------------

// Exec runs a write query.
func (d *DB) Exec(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	return d.db.ExecContext(ctx, query, args...)
}

// Query runs a read query and returns rows.
func (d *DB) Query(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error) {
	return d.db.QueryContext(ctx, query, args...)
}

// QueryRow runs a read query expected to return at most one row.
func (d *DB) QueryRow(ctx context.Context, query string, args ...interface{}) *sql.Row {
	return d.db.QueryRowContext(ctx, query, args...)
}

// ---------------------------------------------------------------------------
// Dolt commit strategy (Task 5.2)
// ---------------------------------------------------------------------------

// Commit stages all changes and creates a Dolt commit.
//
// The message follows the convention: "dispatch:{eventType} session:{sessionId}".
// If there are no staged changes the function returns ("", nil) without error.
func (d *DB) Commit(ctx context.Context, message string) (string, error) {
	// Stage everything.
	if _, err := d.db.ExecContext(ctx, "CALL DOLT_ADD('-A')"); err != nil {
		return "", fmt.Errorf("analytics: dolt_add: %w", err)
	}

	// Attempt to commit.  Dolt returns an error when there is nothing to
	// commit ("nothing to commit"), which we treat as a no-op.
	// We use --author to ensure the committer is "hookwise" rather than
	// the default "root" user that the Dolt engine uses internally.
	row := d.db.QueryRowContext(ctx,
		"CALL DOLT_COMMIT('-m', ?, '--author', 'hookwise <hookwise@local>')", message)

	var hash string
	if err := row.Scan(&hash); err != nil {
		errStr := err.Error()
		if isNothingToCommitError(errStr) {
			return "", nil
		}
		return "", fmt.Errorf("analytics: dolt_commit: %w", err)
	}
	return hash, nil
}

// CommitDispatch is a convenience wrapper that formats the commit message
// in the standard dispatch format.
func (d *DB) CommitDispatch(ctx context.Context, eventType, sessionID string) (string, error) {
	msg := fmt.Sprintf("dispatch:%s session:%s", eventType, sessionID)
	return d.Commit(ctx, msg)
}

// isNothingToCommitError returns true if the error string indicates there
// was nothing staged to commit.
func isNothingToCommitError(errStr string) bool {
	lower := strings.ToLower(errStr)
	return strings.Contains(lower, "nothing to commit") ||
		strings.Contains(lower, "no changes")
}

// ---------------------------------------------------------------------------
// Diff and Log queries (Task 5.3)
// ---------------------------------------------------------------------------

// Diff returns table-level diffs between two Dolt refs (commit hashes,
// branch names, tags, etc.) using the dolt_diff_summary table function.
//
// Note: The DOLT_DIFF() row-level table function has a known nil-pointer
// panic in the embedded driver v0.2.0 when tables were created between
// commits.  We use dolt_diff_summary instead, which is reliable and gives
// per-table change summaries (diff_type, data_change, schema_change).
func (d *DB) Diff(ctx context.Context, fromRef, toRef string) ([]DiffEntry, error) {
	// Validate refs to prevent SQL injection — these are interpolated into
	// the query because Dolt table functions require string literals.
	if !validDoltRef.MatchString(fromRef) {
		return nil, fmt.Errorf("analytics: invalid fromRef %q", fromRef)
	}
	if !validDoltRef.MatchString(toRef) {
		return nil, fmt.Errorf("analytics: invalid toRef %q", toRef)
	}

	q := fmt.Sprintf(
		"SELECT from_table_name, to_table_name, diff_type, data_change, schema_change FROM dolt_diff_summary('%s', '%s')",
		fromRef, toRef,
	)
	rows, err := d.db.QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("analytics: diff_summary: %w", err)
	}
	defer rows.Close()

	var entries []DiffEntry
	for rows.Next() {
		var fromTable, toTable, diffType string
		var dataChange, schemaChange bool
		if err := rows.Scan(&fromTable, &toTable, &diffType, &dataChange, &schemaChange); err != nil {
			return nil, fmt.Errorf("analytics: diff_summary scan: %w", err)
		}

		tableName := toTable
		if tableName == "" {
			tableName = fromTable
		}

		entries = append(entries, DiffEntry{
			TableName:  tableName,
			DiffType:   diffType,
			FromCommit: fromRef,
			ToCommit:   toRef,
			RowData: map[string]interface{}{
				"from_table_name": fromTable,
				"to_table_name":   toTable,
				"data_change":     dataChange,
				"schema_change":   schemaChange,
			},
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("analytics: diff_summary rows: %w", err)
	}

	return entries, nil
}

// Log returns the most recent Dolt commits, up to limit.
func (d *DB) Log(ctx context.Context, limit int) ([]LogEntry, error) {
	q := "SELECT commit_hash, committer, email, date, message FROM dolt_log"
	if limit > 0 {
		q += fmt.Sprintf(" LIMIT %d", limit)
	}

	rows, err := d.db.QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("analytics: dolt_log: %w", err)
	}
	defer rows.Close()

	var entries []LogEntry
	for rows.Next() {
		var e LogEntry
		var dateStr string
		if err := rows.Scan(&e.CommitHash, &e.Committer, &e.Email, &dateStr, &e.Message); err != nil {
			return nil, fmt.Errorf("analytics: dolt_log scan: %w", err)
		}
		// dolt_log returns datetime as string; parse flexibly.
		e.Date, _ = core.ParseTimeFlex(dateStr)
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

// ---------------------------------------------------------------------------
// Schema initialisation (Task 5.1)
// ---------------------------------------------------------------------------

// initSchema creates the logical database (if missing) and applies the
// full DDL schema idempotently using CREATE TABLE IF NOT EXISTS.
func (d *DB) initSchema(ctx context.Context) error {
	// Create the database and switch into it.
	createDB := fmt.Sprintf("CREATE DATABASE IF NOT EXISTS %s", d.dbName)
	if _, err := d.db.ExecContext(ctx, createDB); err != nil {
		return fmt.Errorf("create database: %w", err)
	}
	useDB := fmt.Sprintf("USE %s", d.dbName)
	if _, err := d.db.ExecContext(ctx, useDB); err != nil {
		return fmt.Errorf("use database: %w", err)
	}

	// Apply each table definition in order.
	for _, ddl := range schemaDDL {
		if _, err := d.db.ExecContext(ctx, ddl); err != nil {
			return fmt.Errorf("ddl: %w\nquery: %s", err, ddl)
		}
	}

	// Migration: add ttl_seconds to notifications (v2).
	// Ignore error — column may already exist.
	_, _ = d.db.ExecContext(ctx, "ALTER TABLE notifications ADD COLUMN ttl_seconds INT DEFAULT 86400")

	// Commit any schema changes so the working tree starts clean.
	// On subsequent opens this will be a no-op (nothing to commit).
	if _, err := d.db.ExecContext(ctx, "CALL DOLT_ADD('-A')"); err != nil {
		return fmt.Errorf("schema dolt_add: %w", err)
	}
	row := d.db.QueryRowContext(ctx,
		"CALL DOLT_COMMIT('-m', 'hookwise: init schema', '--author', 'hookwise <hookwise@local>')")
	var hash string
	if err := row.Scan(&hash); err != nil {
		errStr := err.Error()
		if !isNothingToCommitError(errStr) {
			return fmt.Errorf("schema commit: %w", err)
		}
	}

	return nil
}

// schemaDDL contains the ten table definitions from the design doc.  Each
// statement is idempotent (CREATE TABLE IF NOT EXISTS).
var schemaDDL = []string{
	`CREATE TABLE IF NOT EXISTS sessions (
		id VARCHAR(255) PRIMARY KEY,
		started_at TEXT NOT NULL,
		ended_at TEXT,
		duration_seconds INT,
		total_tool_calls INT DEFAULT 0,
		file_edits_count INT DEFAULT 0,
		ai_authored_lines INT DEFAULT 0,
		human_verified_lines INT DEFAULT 0,
		classification TEXT,
		estimated_cost_usd DOUBLE DEFAULT 0.0
	)`,

	`CREATE TABLE IF NOT EXISTS events (
		id INT PRIMARY KEY AUTO_INCREMENT,
		session_id VARCHAR(255) NOT NULL,
		event_type VARCHAR(50) NOT NULL,
		tool_name VARCHAR(255),
		timestamp VARCHAR(30) NOT NULL,
		file_path TEXT,
		lines_added INT DEFAULT 0,
		lines_removed INT DEFAULT 0,
		ai_confidence_score DOUBLE,
		INDEX idx_events_session (session_id),
		INDEX idx_events_timestamp (timestamp)
	)`,

	`CREATE TABLE IF NOT EXISTS authorship_ledger (
		id INT PRIMARY KEY AUTO_INCREMENT,
		session_id VARCHAR(255) NOT NULL,
		file_path TEXT NOT NULL,
		tool_name VARCHAR(255) NOT NULL,
		lines_changed INT NOT NULL,
		ai_score DOUBLE NOT NULL,
		classification VARCHAR(50) NOT NULL,
		timestamp TEXT NOT NULL,
		INDEX idx_authorship_session (session_id)
	)`,

	`CREATE TABLE IF NOT EXISTS metacognition_logs (
		id INT PRIMARY KEY AUTO_INCREMENT,
		session_id VARCHAR(255) NOT NULL,
		timestamp TEXT NOT NULL,
		trigger_type VARCHAR(50) NOT NULL,
		prompt_shown TEXT
	)`,

	`CREATE TABLE IF NOT EXISTS agent_spans (
		id INT PRIMARY KEY AUTO_INCREMENT,
		session_id VARCHAR(255) NOT NULL,
		agent_id VARCHAR(255) NOT NULL,
		parent_agent_id VARCHAR(255),
		agent_type VARCHAR(100),
		started_at TEXT NOT NULL,
		stopped_at TEXT,
		files_modified TEXT,
		INDEX idx_agents_session (session_id)
	)`,

	`CREATE TABLE IF NOT EXISTS feed_cache (
		cache_key VARCHAR(255) PRIMARY KEY,
		data JSON NOT NULL,
		updated_at TEXT NOT NULL,
		ttl_seconds INT NOT NULL
	)`,

	`CREATE TABLE IF NOT EXISTS coaching_state (
		id INT PRIMARY KEY DEFAULT 1,
		last_prompt_at TEXT,
		prompt_history JSON,
		current_mode VARCHAR(20),
		mode_started_at TEXT,
		tooling_minutes DOUBLE DEFAULT 0,
		alert_level VARCHAR(20) DEFAULT 'none',
		today_date TEXT,
		practice_count INT DEFAULT 0,
		last_large_change JSON
	)`,

	`CREATE TABLE IF NOT EXISTS cost_state (
		id INT PRIMARY KEY DEFAULT 1,
		daily_costs JSON,
		session_costs JSON,
		today TEXT,
		total_today DOUBLE DEFAULT 0
	)`,

	`CREATE TABLE IF NOT EXISTS notifications (
		id INT PRIMARY KEY AUTO_INCREMENT,
		producer VARCHAR(100) NOT NULL,
		notification_type VARCHAR(50) NOT NULL,
		content TEXT NOT NULL,
		created_at TEXT NOT NULL,
		surfaced_at TEXT,
		acted_on INT DEFAULT 0,
		branch VARCHAR(255),
		ttl_seconds INT DEFAULT 86400
	)`,

	`CREATE TABLE IF NOT EXISTS schema_meta (
		meta_key VARCHAR(255) PRIMARY KEY,
		meta_value TEXT NOT NULL
	)`,
}
