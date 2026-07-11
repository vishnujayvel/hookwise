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

// TestCalendarRelativeTime_NonUTCOffsets pins label behavior when now and the
// event carry different UTC offsets (hw-8d9k). Google Calendar delivers event
// times in the calendar's own timezone (e.g. +05:30), while render-time "now"
// is the machine's local clock, so mixed-offset inputs are the normal case.
// Durations ("in 30m") are instant-based and must ignore offsets entirely;
// the day labels ("tomorrow 9:00am", "Fri 2:30pm") show the EVENT's own wall
// clock while the tomorrow/weekday split is computed from each side's own
// calendar day — these cases document that current behavior.
func TestCalendarRelativeTime_NonUTCOffsets(t *testing.T) {
	ist := time.FixedZone("UTC+05:30", 5*3600+30*60)
	pst := time.FixedZone("UTC-08:00", -8*3600)

	tests := []struct {
		name  string
		now   time.Time
		event time.Time
		want  string
	}{
		{
			name:  "offset event minutes away from UTC now",
			now:   time.Date(2026, 6, 15, 10, 0, 0, 0, time.UTC),
			event: time.Date(2026, 6, 15, 16, 0, 0, 0, ist), // 10:30 UTC
			want:  "in 30m",
		},
		{
			name:  "same instant across zones is now",
			now:   time.Date(2026, 6, 15, 10, 0, 0, 0, time.UTC),
			event: time.Date(2026, 6, 15, 15, 30, 0, 0, ist), // 10:00 UTC
			want:  "now",
		},
		{
			name:  "non-UTC now vs offset event counts real hours",
			now:   time.Date(2026, 6, 15, 2, 0, 0, 0, pst),  // 10:00 UTC
			event: time.Date(2026, 6, 15, 19, 0, 0, 0, ist), // 13:30 UTC
			want:  "in 3h 30m",
		},
		{
			name:  "countdown crossing midnight in the event zone",
			now:   time.Date(2026, 6, 15, 16, 0, 0, 0, pst), // 2026-06-16 00:00 UTC
			event: time.Date(2026, 6, 16, 11, 0, 0, 0, ist), // 2026-06-16 05:30 UTC
			want:  "in 5h 30m",
		},
		{
			name:  "tomorrow label shows the event's own wall clock",
			now:   time.Date(2026, 6, 15, 10, 0, 0, 0, time.UTC),
			event: time.Date(2026, 6, 17, 9, 0, 0, 0, ist), // 2026-06-16 03:30 UTC, 41.5h out
			want:  "tomorrow 9:00am",
		},
		{
			// 2026-06-15 is a Monday; the event instant is Friday in both zones.
			name:  "weekday label shows the event's own wall clock",
			now:   time.Date(2026, 6, 15, 10, 0, 0, 0, time.UTC),
			event: time.Date(2026, 6, 19, 14, 30, 0, 0, ist), // 2026-06-19 09:00 UTC
			want:  "Fri 2:30pm",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, calendarRelativeTime(tc.now, tc.event))
		})
	}
}
