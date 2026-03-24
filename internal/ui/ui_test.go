package ui

import "testing"

func TestVisWidthCountsWideGlyphs(t *testing.T) {
	if got := VisWidth("💡"); got != 2 {
		t.Fatalf("VisWidth(💡) = %d, want 2", got)
	}
}

func TestTruncatePreservesDisplayWidthBudget(t *testing.T) {
	got := Truncate("💡abc", 4)
	if got != "💡a…" {
		t.Fatalf("Truncate returned %q, want %q", got, "💡a…")
	}
	if VisWidth(got) > 4 {
		t.Fatalf("Truncate width = %d, want <= 4", VisWidth(got))
	}
}
