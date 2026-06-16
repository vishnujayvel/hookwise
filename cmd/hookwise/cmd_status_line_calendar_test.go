package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestCalendarRelativeTime covers the relative-time labelling for calendar
// events, including the midnight-crossing regression: an imminent event a few
// hours away that happens to fall on the next calendar day must read as a
// countdown ("in 2h 30m"), not "tomorrow 1:30am". The day-based labels
// ("tomorrow"/weekday) are reserved for events that are genuinely far out.
func TestCalendarRelativeTime(t *testing.T) {
	utc := time.UTC
	at := func(y int, mo time.Month, d, h, mi int) time.Time {
		return time.Date(y, mo, d, h, mi, 0, 0, utc)
	}

	tests := []struct {
		name  string
		now   time.Time
		event time.Time
		want  string
	}{
		{
			name:  "imminent under a minute is now",
			now:   at(2026, 6, 15, 10, 0),
			event: at(2026, 6, 15, 10, 0),
			want:  "now",
		},
		{
			name:  "under an hour shows minutes",
			now:   at(2026, 6, 15, 10, 0),
			event: at(2026, 6, 15, 10, 30),
			want:  "in 30m",
		},
		{
			name:  "same day a few hours shows hours and minutes",
			now:   at(2026, 6, 15, 10, 0),
			event: at(2026, 6, 15, 13, 30),
			want:  "in 3h 30m",
		},
		{
			name:  "same day exact hour omits minutes",
			now:   at(2026, 6, 15, 10, 0),
			event: at(2026, 6, 15, 13, 0),
			want:  "in 3h",
		},
		{
			// Regression: 2h30m away but crosses midnight -> must be a countdown.
			name:  "imminent crossing midnight is a countdown not tomorrow",
			now:   at(2026, 6, 15, 23, 0),
			event: at(2026, 6, 16, 1, 30),
			want:  "in 2h 30m",
		},
		{
			// Regression: 61 minutes away across midnight (minutes < 5 -> "in 1h").
			name:  "just over an hour crossing midnight is a countdown",
			now:   at(2026, 6, 15, 23, 30),
			event: at(2026, 6, 16, 0, 31),
			want:  "in 1h",
		},
		{
			// Just under the 6h threshold, crosses midnight -> still a countdown.
			name:  "five hours crossing midnight is a countdown",
			now:   at(2026, 6, 15, 20, 0),
			event: at(2026, 6, 16, 1, 0),
			want:  "in 5h",
		},
		{
			// Beyond the threshold on the next calendar day -> "tomorrow" label.
			name:  "next-day morning beyond threshold is tomorrow",
			now:   at(2026, 6, 15, 22, 0),
			event: at(2026, 6, 16, 18, 0),
			want:  "tomorrow 6:00pm",
		},
		{
			// Classic next-day afternoon (28h out) stays "tomorrow".
			name:  "next-day afternoon is tomorrow",
			now:   at(2026, 6, 15, 10, 0),
			event: at(2026, 6, 16, 14, 0),
			want:  "tomorrow 2:00pm",
		},
		{
			// Several days out -> weekday label. 2026-06-18 is a Thursday.
			name:  "several days out shows weekday",
			now:   at(2026, 6, 15, 10, 0),
			event: at(2026, 6, 18, 9, 0),
			want:  "Thu 9:00am",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, calendarRelativeTime(tc.now, tc.event))
		})
	}
}
