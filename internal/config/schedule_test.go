package config

import (
	"testing"
	"time"
)

func TestScheduleNext(t *testing.T) {
	loc, err := time.LoadLocation("America/Chicago")
	if err != nil {
		t.Fatal(err)
	}
	s, err := ParseSchedule("13:00, 09:00,11:00,09:00", loc)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Times) != 3 {
		t.Fatalf("want 3 deduped times, got %v", s.Times)
	}
	at := func(y int, m time.Month, d, h, min int) time.Time {
		return time.Date(y, m, d, h, min, 0, 0, loc)
	}
	tests := []struct {
		now, want time.Time
	}{
		{at(2026, 10, 9, 8, 0), at(2026, 10, 9, 9, 0)},
		{at(2026, 10, 9, 9, 0), at(2026, 10, 9, 11, 0)}, // strictly after
		{at(2026, 10, 9, 12, 59), at(2026, 10, 9, 13, 0)},
		{at(2026, 10, 9, 22, 0), at(2026, 10, 10, 9, 0)},
		// DST ends 2026-11-01 in Chicago; the next run is still 09:00 local.
		{at(2026, 10, 31, 20, 0), at(2026, 11, 1, 9, 0)},
	}
	for _, tc := range tests {
		if got := s.Next(tc.now); !got.Equal(tc.want) {
			t.Errorf("Next(%s) = %s, want %s", tc.now, got, tc.want)
		}
	}
}

func TestParseScheduleErrors(t *testing.T) {
	if s, err := ParseSchedule("", time.UTC); s != nil || err != nil {
		t.Fatalf("empty spec: got %v, %v", s, err)
	}
	for _, bad := range []string{"9am", "25:00", "09:00,"} {
		if _, err := ParseSchedule(bad, time.UTC); err == nil {
			t.Errorf("%q: expected an error", bad)
		}
	}
}
