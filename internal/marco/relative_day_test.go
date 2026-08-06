package marco

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// Marco used to be handed only an RFC3339 scheduled_at and left to work out
// which weekday it fell on. Asked to adjust "Thursday's prep" with a Friday
// prep and a Thursday prep on file, it emitted the Friday one and called it
// "Thursday's prep against Clara" — a well-formed token pointing at the wrong
// match. weekday and day are derived here so that is a lookup, not arithmetic.
func TestRelativeDay(t *testing.T) {
	now := time.Date(2026, 8, 6, 21, 30, 0, 0, time.UTC) // Thursday evening

	tests := []struct {
		name      string
		scheduled time.Time
		want      string
	}{
		{
			name:      "later the same day",
			scheduled: time.Date(2026, 8, 6, 23, 0, 0, 0, time.UTC),
			want:      "today",
		},
		{
			// Earlier the same day is still "today" — a prep at 09:00 on the
			// day you ask about it is not yesterday's.
			name:      "earlier the same day",
			scheduled: time.Date(2026, 8, 6, 9, 0, 0, 0, time.UTC),
			want:      "today",
		},
		{
			// Half an hour later on the clock, but the next calendar day: the
			// comparison is on dates, not on a 24-hour distance.
			name:      "just after midnight tomorrow",
			scheduled: time.Date(2026, 8, 7, 0, 15, 0, 0, time.UTC),
			want:      "tomorrow",
		},
		{
			name:      "tomorrow evening",
			scheduled: time.Date(2026, 8, 7, 19, 30, 0, 0, time.UTC),
			want:      "tomorrow",
		},
		{
			// The QA fixture case: next Thursday carries no relative label, so
			// the weekday is what has to identify it.
			name:      "a week out",
			scheduled: time.Date(2026, 8, 13, 20, 0, 0, 0, time.UTC),
			want:      "",
		},
		{
			name:      "the day after tomorrow",
			scheduled: time.Date(2026, 8, 8, 18, 0, 0, 0, time.UTC),
			want:      "",
		},
		{
			name:      "yesterday",
			scheduled: time.Date(2026, 8, 5, 18, 0, 0, 0, time.UTC),
			want:      "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, relativeDay(tt.scheduled, now))
		})
	}
}

// scheduled_at is wall-clock-in-UTC, so the day it names must be read in UTC.
// Reading it in the server's local zone would shift an evening prep onto the
// next or previous date and hand Marco the wrong weekday.
func TestRelativeDay_ComparesInUTC(t *testing.T) {
	scheduled := time.Date(2026, 8, 7, 19, 30, 0, 0, time.UTC)

	// The same instant expressed in zones either side of UTC.
	for _, offset := range []int{-11, -5, 0, 2, 9, 13} {
		zone := time.FixedZone("test", offset*60*60)
		now := time.Date(2026, 8, 6, 21, 30, 0, 0, time.UTC).In(zone)
		assert.Equal(t, "tomorrow", relativeDay(scheduled.In(zone), now),
			"offset %+d", offset)
	}
}

func TestWeekday_MatchesTheScheduledDate(t *testing.T) {
	tests := []struct {
		scheduled time.Time
		want      string
	}{
		{time.Date(2026, 8, 6, 19, 30, 0, 0, time.UTC), "Thursday"},
		{time.Date(2026, 8, 7, 19, 30, 0, 0, time.UTC), "Friday"},
		{time.Date(2026, 8, 13, 20, 0, 0, 0, time.UTC), "Thursday"},
		{time.Date(2099, 1, 15, 18, 0, 0, 0, time.UTC), "Thursday"}, // golden fixture
	}
	for _, tt := range tests {
		t.Run(tt.want+"_"+tt.scheduled.Format("2006-01-02"), func(t *testing.T) {
			assert.Equal(t, tt.want, tt.scheduled.UTC().Weekday().String())
		})
	}
}
