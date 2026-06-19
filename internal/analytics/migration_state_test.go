package analytics

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTSMigrationDone_FalseOnFreshDB(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "analytics.db"))
	require.NoError(t, err)
	defer db.Close()

	done, err := db.TSMigrationDone(context.Background())
	require.NoError(t, err)
	assert.False(t, done, "a fresh DB must report the migration as not done")
}

func TestMarkTSMigrationDone_ThenDoneIsTrue(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "analytics.db"))
	require.NoError(t, err)
	defer db.Close()

	ctx := context.Background()
	require.NoError(t, db.MarkTSMigrationDone(ctx, time.Now()))

	done, err := db.TSMigrationDone(ctx)
	require.NoError(t, err)
	assert.True(t, done, "after marking, the migration must report as done")
}

func TestMarkTSMigrationDone_Idempotent(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "analytics.db"))
	require.NoError(t, err)
	defer db.Close()

	ctx := context.Background()
	// Marking twice must not error (upsert on meta_key).
	require.NoError(t, db.MarkTSMigrationDone(ctx, time.Now()))
	require.NoError(t, db.MarkTSMigrationDone(ctx, time.Now().Add(time.Hour)))

	done, err := db.TSMigrationDone(ctx)
	require.NoError(t, err)
	assert.True(t, done)
}
