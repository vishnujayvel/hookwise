//go:build deps

// This file ensures direct dependencies stay in go.mod before they
// are imported in production code. Remove individual imports as real
// usage is added.
package hookwise

import (
	_ "github.com/cenkalti/backoff/v4"
	_ "github.com/charmbracelet/lipgloss"
	_ "github.com/gobwas/glob"
	_ "github.com/stretchr/testify/assert"
	_ "gopkg.in/yaml.v3"
)
