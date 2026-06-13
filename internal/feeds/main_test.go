package feeds

import (
	"os"
	"testing"
)

// TestMain redirects HOOKWISE_STATE_DIR to a throwaway temp directory before any
// test in this package runs. core.Logger() resolves its log path from
// GetStateDir() inside a sync.Once, so without this the feed/producer tests
// (e.g. the calendar fail-open and httptest cases) write to the user's REAL
// ~/.hookwise/logs/hookwise.log — polluting the live log and muddying debugging.
// See issue #91. The env var must be set before the first Logger() call, which
// is exactly what TestMain guarantees (it runs before m.Run()).
func TestMain(m *testing.M) {
	tmp, err := os.MkdirTemp("", "hookwise-feeds-test-")
	if err == nil {
		_ = os.Setenv("HOOKWISE_STATE_DIR", tmp)
	}

	code := m.Run()

	if tmp != "" {
		_ = os.RemoveAll(tmp)
	}
	os.Exit(code)
}
