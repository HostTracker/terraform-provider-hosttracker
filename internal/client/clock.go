package client

import (
	"strconv"
	"strings"
	"time"
)

// SameClock reports whether two clock times name the same instant of the
// day. The API stores a duration and reads it back as `HH:MM:SS`, so
// `"09:00"` and `"09:00:00"` are the same window written two ways.
func SameClock(a, b string) bool {
	left, leftOK := parseClock(a)
	right, rightOK := parseClock(b)
	if !leftOK || !rightOK {
		return a == b
	}
	return left == right
}

// PreferredClock keeps the practitioner's spelling of a clock time while
// it still names the hour the API reports, and adopts the API's spelling
// once it does not.
func PreferredClock(prior, server string) string {
	if prior != "" && SameClock(prior, server) {
		return prior
	}
	return server
}

// PreferredZone keeps the configured time-zone id when the API answers a
// different one.
//
// Several IANA zones share one stored zone, so a window written as
// `Europe/Rome` reads back as `Europe/Berlin` - the same clock and the
// same daylight-saving rules under the group's representative name. Taking
// the API's spelling there would make the applied value differ from the
// configured one, which Terraform reports as an inconsistent apply rather
// than as drift. The cost is that a zone changed outside Terraform is not
// reported; a zone written for the first time, or read on import, is
// always the API's own.
func PreferredZone(prior, server string) string {
	if prior != "" && server != "" {
		return prior
	}
	return server
}

// UnixToRFC3339 renders an instant in UTC, for the computed twins that
// make a plan readable. Zero, which the API uses for "no such instant",
// renders empty.
func UnixToRFC3339(seconds int64) string {
	if seconds == 0 {
		return ""
	}
	return time.Unix(seconds, 0).UTC().Format(time.RFC3339)
}

// parseClock reads `H:MM`, `HH:MM` or `HH:MM:SS` into seconds since
// midnight.
func parseClock(v string) (int, bool) {
	parts := strings.Split(strings.TrimSpace(v), ":")
	if len(parts) < 2 || len(parts) > 3 {
		return 0, false
	}
	total := 0
	scale := []int{3600, 60, 1}
	for i, part := range parts {
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 {
			return 0, false
		}
		total += n * scale[i]
	}
	return total, true
}
