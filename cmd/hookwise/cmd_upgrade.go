package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/vishnujayvel/hookwise/internal/migration"
)

func newUpgradeCmd() *cobra.Command {
	var (
		dryRun     bool
		dataDir    string
		projectDir string
	)

	cmd := &cobra.Command{
		Use:   "upgrade",
		Short: "Migrate data from TypeScript hookwise installation",
		Long: `Detects an existing TypeScript hookwise installation (~/.hookwise/analytics.db
and ~/.hookwise/state/cost-state.json), imports the data into the Go Dolt
database, and validates config parity.

Use --dry-run to preview what would be migrated without making changes.
Original files are never modified (non-destructive).`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if projectDir == "" {
				var err error
				projectDir, err = os.Getwd()
				if err != nil {
					projectDir = "."
				}
			}

			result := migration.Run(migration.MigrationOpts{
				DryRun:      dryRun,
				DoltDataDir: dataDir,
				ProjectDir:  projectDir,
				Writer:      cmd.OutOrStdout(),
			})

			if len(result.Errors) > 0 {
				return fmt.Errorf("migration completed with %d error(s)", len(result.Errors))
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Preview migration without making changes")
	cmd.Flags().StringVar(&dataDir, "data-dir", "", "Dolt data directory (defaults to ~/.hookwise/dolt)")
	cmd.Flags().StringVar(&projectDir, "project-dir", "", "Project directory for config validation (defaults to cwd)")

	return cmd
}
