package client

import "testing"

func TestNormalizeTypeResolvesTheWaterfallAlias(t *testing.T) {
	for _, spelling := range []string{"pageSpeed", "pagespeed", "PageSpeed"} {
		if got := NormalizeType(spelling); got != "waterfall" {
			t.Fatalf("NormalizeType(%q) = %q, want waterfall", spelling, got)
		}
	}
}

func TestNormalizeTypeLeavesEverythingElseAlone(t *testing.T) {
	for _, token := range []string{"http", "ping", "sslExp", "somethingNewerThanThisProvider"} {
		if got := NormalizeType(token); got != token {
			t.Fatalf("NormalizeType(%q) = %q, want it unchanged", token, got)
		}
	}
}

func TestSameTypeComparesThroughTheAlias(t *testing.T) {
	if !SameType("pageSpeed", "waterfall") {
		t.Fatal("expected pageSpeed and waterfall to name the same type")
	}
	if SameType("http", "ping") {
		t.Fatal("expected http and ping to be different types")
	}
}

func TestPreferredTypeKeepsTheWrittenSpelling(t *testing.T) {
	// The configuration says pageSpeed, the API reads back waterfall. State
	// keeps what was written, or every plan would show a change.
	if got := PreferredType("pageSpeed", "waterfall"); got != "pageSpeed" {
		t.Fatalf("PreferredType = %q, want pageSpeed", got)
	}
}

func TestPreferredTypeAdoptsARealChange(t *testing.T) {
	if got := PreferredType("http", "ping"); got != "ping" {
		t.Fatalf("PreferredType = %q, want ping", got)
	}
}

func TestPreferredTypeTakesTheAPIsWordOnImport(t *testing.T) {
	// An import has no prior spelling at all.
	if got := PreferredType("", "waterfall"); got != "waterfall" {
		t.Fatalf("PreferredType = %q, want waterfall", got)
	}
}
