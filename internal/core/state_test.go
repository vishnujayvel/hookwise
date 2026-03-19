package core

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseTimeFlex(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"RFC3339", "2026-03-10T14:30:00Z", false},
		{"RFC3339 with offset", "2026-03-10T14:30:00+05:30", false},
		{"RFC3339Nano", "2026-03-10T14:30:00.123456789Z", false},
		{"RFC3339Nano with offset", "2026-03-10T14:30:00.999999999+05:30", false},
		{"bare datetime with Z", "2026-03-10T14:30:00Z", false},
		{"bare datetime space", "2026-03-10 14:30:00", false},
		{"date only", "2026-03-10", false},
		{"empty string", "", true},
		{"garbage", "not-a-time", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ParseTimeFlex(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
				assert.True(t, result.IsZero())
			} else {
				require.NoError(t, err)
				assert.False(t, result.IsZero())
			}
		})
	}
}

func TestParseTimeFlex_RFC3339Nano_Precision(t *testing.T) {
	// Verify that nanosecond precision is preserved.
	input := "2026-03-10T14:30:00.123456789Z"
	result, err := ParseTimeFlex(input)
	require.NoError(t, err)

	expected := time.Date(2026, 3, 10, 14, 30, 0, 123456789, time.UTC)
	assert.Equal(t, expected, result)
}
