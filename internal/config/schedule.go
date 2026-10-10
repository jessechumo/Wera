package config

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// Schedule is a list of daily run times in one time zone, e.g.
// RUN_SCHEDULE="09:00,13:00" with RUN_TIMEZONE="America/Chicago".
type Schedule struct {
	Times    []time.Duration // offsets from local midnight, sorted
	Location *time.Location
}

// ParseSchedule parses a comma-separated list of HH:MM times in loc.
// An empty spec returns nil (no schedule: the worker uses RUN_INTERVAL).
func ParseSchedule(spec string, loc *time.Location) (*Schedule, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return nil, nil
	}
	s := &Schedule{Location: loc}
	seen := map[time.Duration]bool{}
	for _, part := range strings.Split(spec, ",") {
		part = strings.TrimSpace(part)
		t, err := time.Parse("15:04", part)
		if err != nil {
			return nil, fmt.Errorf("bad time %q (want HH:MM, 24-hour)", part)
		}
		off := time.Duration(t.Hour())*time.Hour + time.Duration(t.Minute())*time.Minute
		if !seen[off] {
			seen[off] = true
			s.Times = append(s.Times, off)
		}
	}
	sort.Slice(s.Times, func(i, j int) bool { return s.Times[i] < s.Times[j] })
	return s, nil
}

// Next returns the first scheduled time strictly after now. Wall-clock
// times are resolved per day, so DST changes keep runs at 09:00 local.
func (s *Schedule) Next(now time.Time) time.Time {
	local := now.In(s.Location)
	for day := 0; day <= 1; day++ {
		y, m, d := local.AddDate(0, 0, day).Date()
		for _, off := range s.Times {
			h, min := int(off/time.Hour), int(off%time.Hour/time.Minute)
			t := time.Date(y, m, d, h, min, 0, 0, s.Location)
			if t.After(now) {
				return t
			}
		}
	}
	// Unreachable with at least one time, but stay safe.
	return now.Add(24 * time.Hour)
}

// String renders the schedule like "09:00, 13:00 America/Chicago".
func (s *Schedule) String() string {
	parts := make([]string, len(s.Times))
	for i, off := range s.Times {
		parts[i] = fmt.Sprintf("%02d:%02d", int(off/time.Hour), int(off%time.Hour/time.Minute))
	}
	return strings.Join(parts, ", ") + " " + s.Location.String()
}
