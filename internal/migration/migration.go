// Package migration handles data migration from the TypeScript hookwise
// installation (SQLite analytics.db + cost-state.json) into the Go Dolt
// database. It implements R8.1-R8.5 from the migration requirements.
//
// Design principles:
//   - ARCH-1: fail-open -- migration errors are reported, never crash
//   - ARCH-8: batch Dolt commit after migration
//   - Read-only access to original files (R8.4: non-destructive)
//   - Dry-run mode prints what would happen without writing (R8.3)
package migration

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/vishnujayvel/hookwise/internal/analytics"
	"github.com/vishnujayvel/hookwise/internal/core"
	_ "modernc.org/sqlite" // pure-Go SQLite driver
)

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

// MigrationOpts configures a migration run.
type MigrationOpts struct {
	// DryRun prints what would be migrated without writing.
	DryRun bool
	// DoltDataDir is the Dolt data directory (empty = default).
	DoltDataDir string
	// HomeDir overrides the user home directory (for testing).
	HomeDir string
	// ProjectDir is the project directory for config validation.
	ProjectDir string
	// Writer receives progress output (defaults to os.Stdout).
	Writer io.Writer
}

// MigrationResult summarises what was done.
type MigrationResult struct {
	SQLiteDetected   bool
	CostStateDetected bool
	SessionsImported int
	EventsImported   int
	AuthorshipImported int
	CostStateImported bool
	ConfigValid      bool
	DoltCommitHash   string
	Errors           []string
}

// CostStateJSON matches the structure of cost-state.json written by the
// TypeScript implementation.
type CostStateJSON struct {
	DailyCosts   map[string]float64 `json:"dailyCosts"`
	SessionCosts map[string]float64 `json:"sessionCosts"`
	Today        string             `json:"today"`
	TotalToday   float64            `json:"totalToday"`
}

// ---------------------------------------------------------------------------
// Detection
// ---------------------------------------------------------------------------

// hookwiseDir returns the resolved ~/.hookwise directory.
func hookwiseDir(opts MigrationOpts) string {
	home := opts.HomeDir
	if home == "" {
		var err error
		home, err = os.UserHomeDir()
		if err != nil {
			home = os.TempDir()
		}
	}
	return filepath.Join(home, ".hookwise")
}

// DetectTypeScript checks for the TypeScript installation artifacts.
// Returns (sqlitePath, costStatePath) -- each is empty if not found.
func DetectTypeScript(opts MigrationOpts) (sqlitePath, costStatePath string) {
	dir := hookwiseDir(opts)

	sqliteCandidate := filepath.Join(dir, "analytics.db")
	if info, err := os.Stat(sqliteCandidate); err == nil && !info.IsDir() {
		sqlitePath = sqliteCandidate
	}

	costStateCandidate := filepath.Join(dir, "state", "cost-state.json")
	if info, err := os.Stat(costStateCandidate); err == nil && !info.IsDir() {
		costStatePath = costStateCandidate
	}

	return sqlitePath, costStatePath
}

// ---------------------------------------------------------------------------
// SQLite migration (Task 10.1)
// ---------------------------------------------------------------------------

// MigrateSQLite reads sessions, events, and authorship data from the SQLite
// analytics.db and writes them into the Dolt database.
//
// The SQLite database is opened read-only. If dryRun is true, data is read
// and counted but not written to Dolt.
func MigrateSQLite(ctx context.Context, db *analytics.DB, sqlitePath string, dryRun bool, w io.Writer) (sessions, events, authorship int, errs []string) {
	// Open SQLite in read-only mode using the pure-Go driver.
	sqliteDB, err := sql.Open("sqlite", sqlitePath+"?mode=ro")
	if err != nil {
		errs = append(errs, fmt.Sprintf("failed to open SQLite %s: %v", sqlitePath, err))
		return
	}
	defer sqliteDB.Close()

	// Migrate sessions.
	sessions, sessionErrs := migrateSessions(ctx, db, sqliteDB, dryRun, w)
	errs = append(errs, sessionErrs...)

	// Migrate events.
	events, eventErrs := migrateEvents(ctx, db, sqliteDB, dryRun, w)
	errs = append(errs, eventErrs...)

	// Migrate authorship ledger.
	authorship, authErrs := migrateAuthorship(ctx, db, sqliteDB, dryRun, w)
	errs = append(errs, authErrs...)

	return sessions, events, authorship, errs
}

// migrateSessions reads sessions from SQLite and writes to Dolt.
func migrateSessions(ctx context.Context, doltDB *analytics.DB, sqliteDB *sql.DB, dryRun bool, w io.Writer) (int, []string) {
	var errs []string

	rows, err := sqliteDB.QueryContext(ctx,
		`SELECT id, started_at, ended_at, total_tool_calls, file_edits_count,
		        ai_authored_lines, human_verified_lines, estimated_cost_usd
		 FROM sessions`)
	if err != nil {
		// Table may not exist -- that's OK, report and continue.
		errs = append(errs, fmt.Sprintf("reading SQLite sessions: %v", err))
		return 0, errs
	}
	defer rows.Close()

	count := 0
	a := analytics.NewAnalytics(doltDB)
	for rows.Next() {
		var (
			id         string
			startedAt  string
			endedAt    sql.NullString
			toolCalls  sql.NullInt64
			fileEdits  sql.NullInt64
			aiLines    sql.NullInt64
			humanLines sql.NullInt64
			costUSD    sql.NullFloat64
		)
		if err := rows.Scan(&id, &startedAt, &endedAt, &toolCalls, &fileEdits, &aiLines, &humanLines, &costUSD); err != nil {
			errs = append(errs, fmt.Sprintf("scanning session row: %v", err))
			continue
		}

		if !dryRun {
			startTime := parseTimeFlexible(startedAt)
			if err := a.StartSession(ctx, id, startTime); err != nil {
				errs = append(errs, fmt.Sprintf("inserting session %s: %v", id, err))
				continue
			}

			if endedAt.Valid && endedAt.String != "" {
				endTime := parseTimeFlexible(endedAt.String)
				stats := analytics.SessionStats{
					TotalToolCalls:     int(toolCalls.Int64),
					FileEditsCount:     int(fileEdits.Int64),
					AIAuthoredLines:    int(aiLines.Int64),
					HumanVerifiedLines: int(humanLines.Int64),
					EstimatedCostUSD:   costUSD.Float64,
				}
				if err := a.EndSession(ctx, id, endTime, stats); err != nil {
					errs = append(errs, fmt.Sprintf("ending session %s: %v", id, err))
					continue
				}
			}
		}
		count++
	}
	if err := rows.Err(); err != nil {
		errs = append(errs, fmt.Sprintf("iterating session rows: %v", err))
	}

	if dryRun {
		fmt.Fprintf(w, "  [dry-run] Would migrate %d sessions\n", count)
	} else {
		fmt.Fprintf(w, "  Migrating sessions... %d rows imported\n", count)
	}

	return count, errs
}

// migrateEvents reads events from SQLite and writes to Dolt.
func migrateEvents(ctx context.Context, doltDB *analytics.DB, sqliteDB *sql.DB, dryRun bool, w io.Writer) (int, []string) {
	var errs []string

	rows, err := sqliteDB.QueryContext(ctx,
		`SELECT session_id, event_type, tool_name, timestamp, file_path,
		        lines_added, lines_removed, ai_confidence_score
		 FROM events`)
	if err != nil {
		errs = append(errs, fmt.Sprintf("reading SQLite events: %v", err))
		return 0, errs
	}
	defer rows.Close()

	count := 0
	a := analytics.NewAnalytics(doltDB)
	for rows.Next() {
		var (
			sessionID string
			eventType string
			toolName  sql.NullString
			timestamp string
			filePath  sql.NullString
			linesAdd  sql.NullInt64
			linesRem  sql.NullInt64
			aiScore   sql.NullFloat64
		)
		if err := rows.Scan(&sessionID, &eventType, &toolName, &timestamp, &filePath, &linesAdd, &linesRem, &aiScore); err != nil {
			errs = append(errs, fmt.Sprintf("scanning event row: %v", err))
			continue
		}

		if !dryRun {
			event := analytics.EventRecord{
				EventType:         eventType,
				ToolName:          toolName.String,
				Timestamp:         parseTimeFlexible(timestamp),
				FilePath:          filePath.String,
				LinesAdded:        int(linesAdd.Int64),
				LinesRemoved:      int(linesRem.Int64),
				AIConfidenceScore: aiScore.Float64,
			}
			if err := a.RecordEvent(ctx, sessionID, event); err != nil {
				errs = append(errs, fmt.Sprintf("inserting event for session %s: %v", sessionID, err))
				continue
			}
		}
		count++
	}
	if err := rows.Err(); err != nil {
		errs = append(errs, fmt.Sprintf("iterating event rows: %v", err))
	}

	if dryRun {
		fmt.Fprintf(w, "  [dry-run] Would migrate %d events\n", count)
	} else {
		fmt.Fprintf(w, "  Migrating events... %d rows imported\n", count)
	}

	return count, errs
}

// migrateAuthorship reads the authorship_ledger from SQLite and writes to Dolt.
func migrateAuthorship(ctx context.Context, doltDB *analytics.DB, sqliteDB *sql.DB, dryRun bool, w io.Writer) (int, []string) {
	var errs []string

	rows, err := sqliteDB.QueryContext(ctx,
		`SELECT session_id, file_path, tool_name, lines_changed, ai_score, timestamp
		 FROM authorship_ledger`)
	if err != nil {
		errs = append(errs, fmt.Sprintf("reading SQLite authorship_ledger: %v", err))
		return 0, errs
	}
	defer rows.Close()

	count := 0
	a := analytics.NewAnalytics(doltDB)
	for rows.Next() {
		var (
			sessionID    string
			filePath     string
			toolName     string
			linesChanged int
			aiScore      float64
			timestamp    string
		)
		if err := rows.Scan(&sessionID, &filePath, &toolName, &linesChanged, &aiScore, &timestamp); err != nil {
			errs = append(errs, fmt.Sprintf("scanning authorship row: %v", err))
			continue
		}

		if !dryRun {
			entry := analytics.AuthorshipEntry{
				SessionID:    sessionID,
				FilePath:     filePath,
				ToolName:     toolName,
				LinesChanged: linesChanged,
				AIScore:      aiScore,
				Timestamp:    parseTimeFlexible(timestamp),
			}
			if err := a.RecordAuthorship(ctx, entry); err != nil {
				errs = append(errs, fmt.Sprintf("inserting authorship for session %s: %v", sessionID, err))
				continue
			}
		}
		count++
	}
	if err := rows.Err(); err != nil {
		errs = append(errs, fmt.Sprintf("iterating authorship rows: %v", err))
	}

	if dryRun {
		fmt.Fprintf(w, "  [dry-run] Would migrate %d authorship entries\n", count)
	} else {
		fmt.Fprintf(w, "  Migrating authorship... %d rows imported\n", count)
	}

	return count, errs
}

// ---------------------------------------------------------------------------
// Cost state migration (Task 10.2)
// ---------------------------------------------------------------------------

// MigrateCostState reads cost-state.json and writes it into the Dolt cost_state
// table. If dryRun is true, the JSON is read and validated but not written.
func MigrateCostState(ctx context.Context, db *analytics.DB, jsonPath string, dryRun bool, w io.Writer) (bool, []string) {
	var errs []string

	data, err := os.ReadFile(jsonPath)
	if err != nil {
		errs = append(errs, fmt.Sprintf("reading %s: %v", jsonPath, err))
		return false, errs
	}

	var costJSON CostStateJSON
	if err := json.Unmarshal(data, &costJSON); err != nil {
		errs = append(errs, fmt.Sprintf("parsing cost-state.json: %v", err))
		return false, errs
	}

	// Ensure maps are non-nil.
	if costJSON.DailyCosts == nil {
		costJSON.DailyCosts = make(map[string]float64)
	}
	if costJSON.SessionCosts == nil {
		costJSON.SessionCosts = make(map[string]float64)
	}

	if dryRun {
		fmt.Fprintf(w, "  [dry-run] Would import cost state: today=%s totalToday=%.2f dailyCosts=%d entries sessionCosts=%d entries\n",
			costJSON.Today, costJSON.TotalToday, len(costJSON.DailyCosts), len(costJSON.SessionCosts))
		return true, errs
	}

	costState := &analytics.CostState{
		DailyCosts:   costJSON.DailyCosts,
		SessionCosts: costJSON.SessionCosts,
		Today:        costJSON.Today,
		TotalToday:   costJSON.TotalToday,
	}

	if err := db.WriteCostState(ctx, costState); err != nil {
		errs = append(errs, fmt.Sprintf("writing cost state to Dolt: %v", err))
		return false, errs
	}

	fmt.Fprintf(w, "  Migrating cost state... imported (today=%s, totalToday=$%.2f)\n",
		costJSON.Today, costJSON.TotalToday)

	return true, errs
}

// ---------------------------------------------------------------------------
// Config parity validation (Task 10.2, R8.5)
// ---------------------------------------------------------------------------

// ValidateConfig loads the hookwise.yaml via the Go config loader and reports
// any parse or validation issues. This ensures config parity between the
// TypeScript and Go implementations.
func ValidateConfig(projectDir string, w io.Writer) (bool, []string) {
	var errs []string

	if projectDir == "" {
		var err error
		projectDir, err = os.Getwd()
		if err != nil {
			errs = append(errs, fmt.Sprintf("cannot determine working directory: %v", err))
			return false, errs
		}
	}

	configPath := filepath.Join(projectDir, core.ProjectConfigFile)
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		fmt.Fprintf(w, "  Config: %s not found (skipping validation)\n", configPath)
		return true, errs // Not an error -- config is optional.
	}

	config, err := core.LoadConfig(projectDir)
	if err != nil {
		errs = append(errs, fmt.Sprintf("Go config loader failed: %v", err))
		return false, errs
	}

	// Basic sanity checks.
	if config.Version < 1 {
		errs = append(errs, "config version is not set or < 1")
	}

	fmt.Fprintf(w, "  Config: hookwise.yaml parsed successfully (version=%d, %d guards, analytics.enabled=%v)\n",
		config.Version, len(config.Guards), config.Analytics.Enabled)

	return len(errs) == 0, errs
}

// ---------------------------------------------------------------------------
// Orchestrator (Task 10.3)
// ---------------------------------------------------------------------------

// Run executes the full migration pipeline:
//  1. Detect TypeScript artifacts
//  2. Migrate SQLite data (sessions, events, authorship)
//  3. Migrate cost-state.json
//  4. Validate config parity
//  5. Commit to Dolt (unless dry-run)
func Run(opts MigrationOpts) MigrationResult {
	result := MigrationResult{}
	ctx := context.Background()
	w := opts.Writer
	if w == nil {
		w = os.Stdout
	}

	if opts.DryRun {
		fmt.Fprintln(w, "hookwise upgrade --dry-run")
	} else {
		fmt.Fprintln(w, "hookwise upgrade")
	}
	fmt.Fprintln(w, "Detecting TypeScript installation...")

	// Step 1: Detect.
	sqlitePath, costStatePath := DetectTypeScript(opts)
	result.SQLiteDetected = sqlitePath != ""
	result.CostStateDetected = costStatePath != ""

	if !result.SQLiteDetected && !result.CostStateDetected {
		fmt.Fprintln(w, "  No TypeScript installation detected. Nothing to migrate.")
		result.ConfigValid = true
		return result
	}

	if result.SQLiteDetected {
		fmt.Fprintf(w, "  Found: %s\n", sqlitePath)
	}
	if result.CostStateDetected {
		fmt.Fprintf(w, "  Found: %s\n", costStatePath)
	}

	// Step 2: Open Dolt DB (unless pure dry-run with no actual writes needed).
	var db *analytics.DB
	if !opts.DryRun {
		var err error
		db, err = analytics.Open(opts.DoltDataDir)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("failed to open Dolt DB: %v", err))
			fmt.Fprintf(w, "ERROR: %v\n", err)
			return result
		}
		defer db.Close()
	}

	// Step 3: Migrate SQLite.
	if result.SQLiteDetected {
		fmt.Fprintln(w, "\nMigrating SQLite data...")
		sessions, events, authorship, errs := MigrateSQLite(ctx, db, sqlitePath, opts.DryRun, w)
		result.SessionsImported = sessions
		result.EventsImported = events
		result.AuthorshipImported = authorship
		result.Errors = append(result.Errors, errs...)
	}

	// Step 4: Migrate cost state.
	if result.CostStateDetected {
		fmt.Fprintln(w, "\nMigrating cost state...")
		imported, errs := MigrateCostState(ctx, db, costStatePath, opts.DryRun, w)
		result.CostStateImported = imported
		result.Errors = append(result.Errors, errs...)
	}

	// Step 5: Validate config.
	fmt.Fprintln(w, "\nValidating config...")
	valid, errs := ValidateConfig(opts.ProjectDir, w)
	result.ConfigValid = valid
	result.Errors = append(result.Errors, errs...)

	// Step 6: Dolt commit (unless dry-run).
	if !opts.DryRun && db != nil {
		fmt.Fprintln(w, "\nCommitting to Dolt...")
		hash, err := db.Commit(ctx, "migration:upgrade from-typescript")
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("Dolt commit failed: %v", err))
		} else if hash != "" {
			result.DoltCommitHash = hash
			fmt.Fprintf(w, "  Dolt commit: %s\n", hash)
		} else {
			fmt.Fprintln(w, "  No data changes to commit.")
		}
	} else if opts.DryRun {
		fmt.Fprintln(w, "\n[dry-run] Would commit to Dolt with message: \"migration:upgrade from-typescript\"")
	}

	// Summary.
	fmt.Fprintln(w, "\n--- Migration summary ---")
	if opts.DryRun {
		fmt.Fprintln(w, "Mode: dry-run (no changes made)")
	} else {
		fmt.Fprintln(w, "Mode: live")
	}
	fmt.Fprintf(w, "Sessions: %d\n", result.SessionsImported)
	fmt.Fprintf(w, "Events: %d\n", result.EventsImported)
	fmt.Fprintf(w, "Authorship: %d\n", result.AuthorshipImported)
	fmt.Fprintf(w, "Cost state: %v\n", result.CostStateImported)
	fmt.Fprintf(w, "Config valid: %v\n", result.ConfigValid)
	if len(result.Errors) > 0 {
		fmt.Fprintf(w, "Errors: %d\n", len(result.Errors))
		for _, e := range result.Errors {
			fmt.Fprintf(w, "  - %s\n", e)
		}
	}

	return result
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// parseTimeFlexible attempts to parse a timestamp string using several common formats.
func parseTimeFlexible(s string) time.Time {
	formats := []string{
		time.RFC3339,
		"2006-01-02T15:04:05Z",
		"2006-01-02 15:04:05",
		"2006-01-02",
	}
	for _, f := range formats {
		if t, err := time.Parse(f, s); err == nil {
			return t
		}
	}
	return time.Now()
}
