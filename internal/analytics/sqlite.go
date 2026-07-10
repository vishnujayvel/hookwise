// Package analytics provides the SQLite-backed database layer for hookwise.
//
// It wraps the pure-Go modernc.org/sqlite driver behind a thin DB type that
// keeps a single writer connection (ARCH-2) and creates the schema on first
// open. The legacy Dolt embedded backend has been replaced; see
// openspec/changes/dolt-to-sqlite for the migration design.
package analytics

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite" // registers the pure-Go "sqlite" sql driver

	"github.com/vishnujayvel/hookwise/internal/core"
)

// ---------------------------------------------------------------------------
// DB – the SQLite database wrapper
// ---------------------------------------------------------------------------

// DB wraps a *sql.DB pointing at a SQLite analytics database opened in WAL
// mode. All public methods are safe to call from any goroutine.
type DB struct {
	db     *sql.DB
	dbPath string // absolute path to the analytics.db file
}

// DefaultDoltDir returns the conventional legacy Dolt data directory.
//
// Retained so doctor and migration tooling can locate (and archive) any
// pre-existing Dolt data. New data lives in the SQLite file at DefaultDBPath.
func DefaultDoltDir() string {
	return filepath.Join(core.HomeDir(), ".hookwise", "dolt")
}

// DefaultDBPath returns the conventional SQLite analytics database file path,
// resolved under core.GetStateDir() so HOOKWISE_STATE_DIR is honored at call
// time (mirrors DefaultSnapshotsDir / PR #227's tuiPIDPath pattern).
func DefaultDBPath() string {
	return filepath.Join(core.GetStateDir(), "analytics.db")
}

// Open creates (if needed) and opens the SQLite analytics database.
//
// dbPath is the path to the analytics.db file. If empty, DefaultDBPath() is
// used. The parent directory is created if missing.
//
// On first open the complete schema (10 tables + indexes) is created. If a
// legacy Dolt data directory still exists alongside, it is archived to
// "<dir>.dolt.bak" (design §3); this is skipped once analytics.db exists.
func Open(dbPath string) (*DB, error) {
	if dbPath == "" {
		dbPath = DefaultDBPath()
	}

	absPath, err := filepath.Abs(dbPath)
	if err != nil {
		return nil, fmt.Errorf("analytics: abs path: %w", err)
	}

	// Ensure the parent directory exists.
	if err := os.MkdirAll(filepath.Dir(absPath), 0o700); err != nil {
		return nil, fmt.Errorf("analytics: mkdir %s: %w", filepath.Dir(absPath), err)
	}

	// First-run: archive legacy Dolt data (design §3). Idempotent — only runs
	// when analytics.db does not yet exist.
	if _, statErr := os.Stat(absPath); os.IsNotExist(statErr) {
		archiveLegacyDolt(filepath.Dir(absPath))
	}

	// WAL pragmas applied on connection open via the DSN _pragma params.
	dsn := absPath +
		"?_pragma=journal_mode(WAL)" +
		"&_pragma=synchronous(NORMAL)" +
		"&_pragma=busy_timeout(5000)"

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("analytics: sql.Open: %w", err)
	}

	// ARCH-2: keep a single writer connection. This is now a deliberate
	// single-writer safety choice — WAL permits concurrent readers plus one
	// writer — NOT a backend limitation as it was under Dolt embedded.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	d := &DB{
		db:     db,
		dbPath: absPath,
	}

	if err := d.initSchema(context.Background()); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("analytics: init schema: %w", err)
	}

	return d, nil
}

// archiveLegacyDolt renames a pre-existing Dolt data dir to "<dir>.dolt.bak"
// so the new SQLite backend starts clean. No Dolt data is read. Best-effort:
// failures are logged but do not block opening the new database.
func archiveLegacyDolt(analyticsDir string) {
	doltDir := filepath.Join(analyticsDir, "dolt")
	info, err := os.Stat(doltDir)
	if err != nil || !info.IsDir() {
		return // nothing to archive
	}
	bak := doltDir + ".dolt.bak"
	if _, err := os.Stat(bak); err == nil {
		return // already archived previously
	}
	if err := os.Rename(doltDir, bak); err != nil {
		core.Logger().Warn("analytics: failed to archive legacy Dolt dir",
			"from", doltDir, "to", bak, "error", err)
		return
	}
	core.Logger().Info("analytics: archived legacy Dolt data", "archive", bak)
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
// Commit (auto-commit after dispatch) — now a no-op
// ---------------------------------------------------------------------------

// Commit is a no-op under the SQLite backend.
//
// Phase: snapshots replace Dolt auto-commit. The events table is itself the
// audit trail; point-in-time history is captured by periodic snapshots
// (see openspec/changes/dolt-to-sqlite Phase 2), not per-dispatch commits.
func (d *DB) Commit(_ context.Context, _ string) (string, error) {
	return "", nil
}

// CommitDispatch is a convenience wrapper kept for API compatibility. It is a
// no-op under the SQLite backend (see Commit).
func (d *DB) CommitDispatch(_ context.Context, _, _ string) (string, error) {
	return "", nil
}

// ---------------------------------------------------------------------------
// Schema initialisation
// ---------------------------------------------------------------------------

// initSchema creates the full schema (10 tables + indexes) idempotently using
// CREATE TABLE IF NOT EXISTS / CREATE INDEX IF NOT EXISTS.
func (d *DB) initSchema(ctx context.Context) error {
	for _, ddl := range schemaDDL {
		if _, err := d.db.ExecContext(ctx, ddl); err != nil {
			return fmt.Errorf("ddl: %w\nquery: %s", err, ddl)
		}
	}

	// Migration: add ttl_seconds to notifications (v2). SQLite has no
	// "IF NOT EXISTS" for ADD COLUMN, so tolerate the "duplicate column" error
	// on re-open but surface any other failure (lock, corruption, permissions)
	// instead of silently swallowing it.
	if _, err := d.db.ExecContext(ctx,
		"ALTER TABLE notifications ADD COLUMN ttl_seconds INTEGER DEFAULT 86400"); err != nil &&
		!strings.Contains(err.Error(), "duplicate column") {
		return fmt.Errorf("migrate ttl_seconds: %w", err)
	}

	// Migration: add dispatch_latency_ms to events (gh#37). Nullable, no
	// default: rows written before this column existed stay NULL so readers
	// can distinguish "not measured" from a real 0ms dispatch.
	if _, err := d.db.ExecContext(ctx,
		"ALTER TABLE events ADD COLUMN dispatch_latency_ms INTEGER"); err != nil &&
		!strings.Contains(err.Error(), "duplicate column") {
		return fmt.Errorf("migrate dispatch_latency_ms: %w", err)
	}

	return nil
}

// schemaDDL contains the ten table definitions plus their indexes, translated
// to SQLite dialect (design §4). Inline MySQL/Dolt INDEX clauses are split out
// into separate CREATE INDEX statements. Each statement is idempotent.
var schemaDDL = []string{
	`CREATE TABLE IF NOT EXISTS sessions (
		id TEXT PRIMARY KEY,
		started_at TEXT NOT NULL,
		ended_at TEXT,
		duration_seconds INTEGER,
		total_tool_calls INTEGER DEFAULT 0,
		file_edits_count INTEGER DEFAULT 0,
		ai_authored_lines INTEGER DEFAULT 0,
		human_verified_lines INTEGER DEFAULT 0,
		classification TEXT,
		estimated_cost_usd REAL DEFAULT 0.0
	)`,

	`CREATE TABLE IF NOT EXISTS events (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		session_id TEXT NOT NULL,
		event_type TEXT NOT NULL,
		tool_name TEXT,
		timestamp TEXT NOT NULL,
		file_path TEXT,
		lines_added INTEGER DEFAULT 0,
		lines_removed INTEGER DEFAULT 0,
		ai_confidence_score REAL,
		dispatch_latency_ms INTEGER
	)`,
	`CREATE INDEX IF NOT EXISTS idx_events_session ON events(session_id)`,
	`CREATE INDEX IF NOT EXISTS idx_events_timestamp ON events(timestamp)`,

	`CREATE TABLE IF NOT EXISTS authorship_ledger (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		session_id TEXT NOT NULL,
		file_path TEXT NOT NULL,
		tool_name TEXT NOT NULL,
		lines_changed INTEGER NOT NULL,
		ai_score REAL NOT NULL,
		classification TEXT NOT NULL,
		timestamp TEXT NOT NULL
	)`,
	`CREATE INDEX IF NOT EXISTS idx_authorship_session ON authorship_ledger(session_id)`,

	`CREATE TABLE IF NOT EXISTS metacognition_logs (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		session_id TEXT NOT NULL,
		timestamp TEXT NOT NULL,
		trigger_type TEXT NOT NULL,
		prompt_shown TEXT
	)`,

	`CREATE TABLE IF NOT EXISTS agent_spans (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		session_id TEXT NOT NULL,
		agent_id TEXT NOT NULL,
		parent_agent_id TEXT,
		agent_type TEXT,
		started_at TEXT NOT NULL,
		stopped_at TEXT,
		files_modified TEXT
	)`,
	`CREATE INDEX IF NOT EXISTS idx_agents_session ON agent_spans(session_id)`,

	`CREATE TABLE IF NOT EXISTS feed_cache (
		cache_key TEXT PRIMARY KEY,
		data TEXT NOT NULL,
		updated_at TEXT NOT NULL,
		ttl_seconds INTEGER NOT NULL
	)`,

	`CREATE TABLE IF NOT EXISTS cost_state (
		id INTEGER PRIMARY KEY DEFAULT 1,
		daily_costs TEXT,
		session_costs TEXT,
		today TEXT,
		total_today REAL DEFAULT 0
	)`,

	`CREATE TABLE IF NOT EXISTS notifications (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		producer TEXT NOT NULL,
		notification_type TEXT NOT NULL,
		content TEXT NOT NULL,
		created_at TEXT NOT NULL,
		surfaced_at TEXT,
		acted_on INTEGER DEFAULT 0,
		branch TEXT,
		ttl_seconds INTEGER DEFAULT 86400
	)`,

	`CREATE TABLE IF NOT EXISTS schema_meta (
		meta_key TEXT PRIMARY KEY,
		meta_value TEXT NOT NULL
	)`,
}
