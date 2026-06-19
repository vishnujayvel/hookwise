package analytics

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// metaKeyTSMigration marks that the one-time TypeScript->Go data migration has
// been applied to this database. It is stored in the generic schema_meta
// key-value table so `hookwise upgrade` can short-circuit on re-run, rather than
// erroring on duplicate session primary keys and double-importing events.
const metaKeyTSMigration = "ts_migration_done"

// TSMigrationDone reports whether the TypeScript->Go data migration marker is
// set on this database.
func (d *DB) TSMigrationDone(ctx context.Context) (bool, error) {
	var v string
	err := d.QueryRow(ctx,
		`SELECT meta_value FROM schema_meta WHERE meta_key = ?`, metaKeyTSMigration).Scan(&v)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("analytics: read migration marker: %w", err)
	}
	return true, nil
}

// MarkTSMigrationDone records the TypeScript->Go migration as complete, so a
// subsequent `hookwise upgrade` is a no-op. Upserts on the meta_key so repeated
// marks are harmless.
func (d *DB) MarkTSMigrationDone(ctx context.Context, at time.Time) error {
	_, err := d.Exec(ctx,
		`INSERT INTO schema_meta (meta_key, meta_value) VALUES (?, ?)
		 ON CONFLICT(meta_key) DO UPDATE SET meta_value = excluded.meta_value`,
		metaKeyTSMigration, at.UTC().Format(time.RFC3339))
	if err != nil {
		return fmt.Errorf("analytics: set migration marker: %w", err)
	}
	return nil
}
