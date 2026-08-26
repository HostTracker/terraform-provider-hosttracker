package client

import "testing"

func TestSameClockReadsTheSameHourTwoWays(t *testing.T) {
	for _, c := range []struct {
		left, right string
		same        bool
	}{
		{"09:00", "09:00:00", true},
		{"9:00", "09:00:00", true},
		{"09:00:30", "09:00:00", false},
		{"18:00:00", "18:00:00", true},
		{"not a time", "18:00:00", false},
		{"not a time", "not a time", true},
	} {
		if got := SameClock(c.left, c.right); got != c.same {
			t.Errorf("SameClock(%q, %q) = %v, want %v", c.left, c.right, got, c.same)
		}
	}
}

func TestPreferredClockKeepsTheConfiguredSpelling(t *testing.T) {
	if got := PreferredClock("09:00", "09:00:00"); got != "09:00" {
		t.Fatalf("expected the configured spelling to survive, got %q", got)
	}
	if got := PreferredClock("09:00", "10:00:00"); got != "10:00:00" {
		t.Fatalf("expected a real change to be adopted, got %q", got)
	}
	if got := PreferredClock("", "10:00:00"); got != "10:00:00" {
		t.Fatalf("expected an unconfigured member to take the API's value, got %q", got)
	}
}

func TestPreferredZoneKeepsTheConfiguredId(t *testing.T) {
	// Several ids share one stored zone, so the API can answer with the
	// group's representative rather than with what was sent.
	if got := PreferredZone("Europe/Rome", "Europe/Berlin"); got != "Europe/Rome" {
		t.Fatalf("expected the configured id to survive, got %q", got)
	}
	if got := PreferredZone("", "Europe/Berlin"); got != "Europe/Berlin" {
		t.Fatalf("expected an imported window to take the API's id, got %q", got)
	}
}

func TestUnixToRFC3339RendersInUTC(t *testing.T) {
	if got := UnixToRFC3339(1785712608); got != "2026-08-02T23:16:48Z" {
		t.Fatalf("unexpected rendering %q", got)
	}
	if got := UnixToRFC3339(0); got != "" {
		t.Fatalf("expected no instant to render empty, got %q", got)
	}
}
