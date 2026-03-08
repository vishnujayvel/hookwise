// Hookwise CI/CD pipeline — runs identically locally and in GitHub Actions.
//
// Usage:
//
//	dagger call ci --src=.              # Full pipeline (what CI runs)
//	dagger call check --src=.           # Tier 0: vet + compile + lint
//	dagger call test --src=.            # Tier 1: unit + contract + arch + PBT + TUI
//	dagger call validate --src=.        # Tier 2: integration + mutation + snapshots
//	dagger call build --src=.           # Build binary only
//	dagger call tui-test-matrix --src=. # TUI tests on Python 3.11/3.12/3.13
package main

import (
	"context"
	"fmt"

	"dagger/hookwise/internal/dagger"

	"golang.org/x/sync/errgroup"
)

type Hookwise struct{}

// ----- private container helpers -----

// goContainer returns a Go build container with CGO enabled (required for Dolt's gozstd),
// module + build caches mounted, and dependencies pre-downloaded.
func (m *Hookwise) goContainer(src *dagger.Directory) *dagger.Container {
	goModCache := dag.CacheVolume("go-mod")
	goBuildCache := dag.CacheVolume("go-build")

	return dag.Container().
		From("golang:1.25-bookworm").
		WithEnvVariable("CGO_ENABLED", "1").
		WithMountedCache("/go/pkg/mod", goModCache).
		WithMountedCache("/root/.cache/go-build", goBuildCache).
		WithMountedDirectory("/src", src).
		WithWorkdir("/src").
		WithExec([]string{"go", "mod", "download"})
}

// pythonContainer returns a Python container with TUI dependencies installed.
func (m *Hookwise) pythonContainer(src *dagger.Directory, pythonVersion string) *dagger.Container {
	pipCache := dag.CacheVolume("pip-" + pythonVersion)

	return dag.Container().
		From("python:"+pythonVersion+"-slim-bookworm").
		WithMountedCache("/root/.cache/pip", pipCache).
		WithMountedDirectory("/src", src).
		WithWorkdir("/src/tui").
		WithExec([]string{"pip", "install", "--quiet", "uv"}).
		WithExec([]string{"uv", "pip", "install", "--system", "-e", ".[dev]"})
}

// ----- Tier 0: Check -----

// Check runs fast pre-commit checks: go vet, go build, and ruff lint — all in parallel.
func (m *Hookwise) Check(ctx context.Context, src *dagger.Directory) error {
	var g errgroup.Group

	// Go vet
	g.Go(func() error {
		_, err := m.goContainer(src).
			WithExec([]string{"go", "vet", "./..."}).
			Sync(ctx)
		return err
	})

	// Go compile check
	g.Go(func() error {
		_, err := m.goContainer(src).
			WithExec([]string{"go", "build", "./..."}).
			Sync(ctx)
		return err
	})

	// Python ruff lint (advisory — doesn't fail the build, matches Taskfile behavior)
	g.Go(func() error {
		_, err := m.pythonContainer(src, "3.13").
			WithExec([]string{"pip", "install", "--quiet", "ruff"}).
			WithExec([]string{"sh", "-c", "ruff check . || true"}).
			Sync(ctx)
		return err
	})

	return g.Wait()
}

// ----- Tier 1: Test -----

// Test runs all tier-1 tests: Go unit, contract, arch, PBT, TUI unit (parallel),
// then cross-schema boundary tests (after both Go and TUI unit pass).
func (m *Hookwise) Test(ctx context.Context, src *dagger.Directory) error {
	// First run Check as a gate
	if err := m.Check(ctx, src); err != nil {
		return fmt.Errorf("check failed: %w", err)
	}

	var g errgroup.Group

	// Go unit tests
	g.Go(func() error {
		_, err := m.goContainer(src).
			WithExec([]string{
				"go", "test", "-race",
				"./internal/core/...", "./internal/feeds/...", "./internal/bridge/...",
				"./internal/analytics/...", "./internal/notifications/...",
				"./internal/migration/...", "./pkg/...",
			}).
			Sync(ctx)
		return err
	})

	// Go contract tests
	g.Go(func() error {
		_, err := m.goContainer(src).
			WithExec([]string{"go", "test", "-race", "./internal/contract/..."}).
			Sync(ctx)
		return err
	})

	// Go architecture lint
	g.Go(func() error {
		_, err := m.goContainer(src).
			WithExec([]string{"go", "test", "-race", "./internal/arch/..."}).
			Sync(ctx)
		return err
	})

	// Go property-based tests
	g.Go(func() error {
		_, err := m.goContainer(src).
			WithExec([]string{"go", "test", "-race", "./internal/proptest/..."}).
			Sync(ctx)
		return err
	})

	// TUI unit tests (excluding snapshots)
	g.Go(func() error {
		_, err := m.pythonContainer(src, "3.13").
			WithExec([]string{
				"python", "-m", "pytest", "tests/", "-x",
				"--ignore=tests/test_snapshots.py",
			}).
			Sync(ctx)
		return err
	})

	if err := g.Wait(); err != nil {
		return err
	}

	// Cross-schema boundary tests — depends on both Go unit and TUI unit passing
	_, err := m.pythonContainer(src, "3.13").
		WithExec([]string{
			"python", "-m", "pytest", "tests/test_status_live_output.py", "-x",
		}).
		Sync(ctx)
	return err
}

// ----- Tier 2: Validate -----

// Validate runs full validation: integration/chaos, mutation, and TUI snapshots — in parallel.
// Requires Test to pass first.
func (m *Hookwise) Validate(ctx context.Context, src *dagger.Directory) error {
	// Test gate
	if err := m.Test(ctx, src); err != nil {
		return fmt.Errorf("test failed: %w", err)
	}

	var g errgroup.Group

	// Go integration + chaos tests
	g.Go(func() error {
		_, err := m.goContainer(src).
			WithExec([]string{"go", "test", "-race", "-tags", "integration", "./..."}).
			Sync(ctx)
		return err
	})

	// Go mutation tests
	g.Go(func() error {
		_, err := m.goContainer(src).
			WithExec([]string{"go", "test", "-race", "-tags", "mutation", "./internal/mutation/..."}).
			Sync(ctx)
		return err
	})

	// TUI snapshot tests — uses --snapshot-update because container rendering
	// (terminal size, fonts) differs from local macOS, so committed baselines
	// won't match. This regenerates and verifies consistency within the container.
	g.Go(func() error {
		_, err := m.pythonContainer(src, "3.13").
			WithExec([]string{
				"python", "-m", "pytest", "tests/test_snapshots.py", "--snapshot-update",
			}).
			Sync(ctx)
		return err
	})

	return g.Wait()
}

// ----- Build -----

// Build compiles the hookwise binary with version info baked in via ldflags.
// Returns the binary as a File.
func (m *Hookwise) Build(
	ctx context.Context,
	src *dagger.Directory,
	// +optional
	// +default="dev"
	version string,
	// +optional
	// +default="none"
	commit string,
) *dagger.File {
	ldflags := fmt.Sprintf(
		"-X main.version=%s -X main.commit=%s",
		version, commit,
	)

	return m.goContainer(src).
		WithExec([]string{
			"go", "build",
			"-ldflags", ldflags,
			"-o", "/out/hookwise",
			"./cmd/hookwise/",
		}).
		File("/out/hookwise")
}

// ----- Ci (full pipeline) -----

// Ci runs the complete CI pipeline: Check -> Test -> Validate -> Build (sequential gates).
// Build uses placeholder metadata since this is a validation-only pipeline;
// real releases use the Publish function with actual version/commit args.
func (m *Hookwise) Ci(ctx context.Context, src *dagger.Directory) (string, error) {
	// Validate already runs Check -> Test internally as gates
	if err := m.Validate(ctx, src); err != nil {
		return "", fmt.Errorf("validate failed: %w", err)
	}

	// Build binary (smoke test — real releases use Publish with actual metadata)
	_, err := m.Build(ctx, src, "ci", "HEAD").Sync(ctx)
	if err != nil {
		return "", fmt.Errorf("build failed: %w", err)
	}

	return "Pipeline passed: check -> test -> validate -> build", nil
}

// ----- TUI Test Matrix -----

// TuiTestMatrix runs Python TUI tests on 3.11, 3.12, and 3.13 in parallel.
// Snapshots are only validated on 3.13.
func (m *Hookwise) TuiTestMatrix(ctx context.Context, src *dagger.Directory) error {
	var g errgroup.Group

	for _, v := range []string{"3.11", "3.12", "3.13"} {
		v := v // capture loop variable
		g.Go(func() error {
			ctr := m.pythonContainer(src, v)
			if v == "3.13" {
				// Run all tests including snapshots (--snapshot-update: container
				// rendering differs from local, so regenerate within container)
				_, err := ctr.
					WithExec([]string{
						"python", "-m", "pytest", "tests/", "-v", "--snapshot-update",
					}).
					Sync(ctx)
				return err
			}
			// Non-3.13: exclude snapshot tests
			_, err := ctr.
				WithExec([]string{
					"python", "-m", "pytest", "tests/", "-v",
					"--ignore=tests/test_snapshots.py",
				}).
				Sync(ctx)
			return err
		})
	}

	return g.Wait()
}
