package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/vishnujayvel/hookwise/internal/core"
	"github.com/vishnujayvel/hookwise/internal/feeds"
)

// registerCustomFeeds wires user-defined custom feeds (#124) into the daemon's
// producer registry. Valid entries register regardless of their Enabled flag
// (the poll loop gates enabled/interval); malformed entries (empty name or
// command) are skipped so they don't register a no-op producer.
func TestRegisterCustomFeeds_RegistersValidEntries(t *testing.T) {
	r := feeds.NewRegistry()

	n := registerCustomFeeds(r, []core.CustomFeedConfig{
		{Name: "stocks", Command: `echo '{"ok":true}'`, IntervalSeconds: 120, Enabled: true},
		{Name: "disabled-still-registered", Command: "echo {}", Enabled: false},
		{Name: "", Command: "echo {}"},  // skipped: empty name
		{Name: "no-cmd", Command: ""},   // skipped: empty command
	})

	assert.Equal(t, 2, n, "two valid entries registered (enabled flag does not gate registration)")

	_, ok := r.Get("stocks")
	assert.True(t, ok, "valid custom feed must be registered")

	_, ok = r.Get("disabled-still-registered")
	assert.True(t, ok, "disabled feeds are still registered; the daemon gates them at poll time")

	_, ok = r.Get("no-cmd")
	assert.False(t, ok, "empty-command entry must be skipped")
}

// An empty custom list is a no-op (the common case: no custom feeds configured).
func TestRegisterCustomFeeds_EmptyList(t *testing.T) {
	r := feeds.NewRegistry()
	assert.Equal(t, 0, registerCustomFeeds(r, nil))
	assert.Empty(t, r.All())
}
