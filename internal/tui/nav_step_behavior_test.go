package tui

import (
	"testing"

	"go.steado.tech/dock/internal/navigation"
)

func TestMoveCursorSkipsHeaders(t *testing.T) {
	m := NewModel("# t\n", 120, 30)
	m.Entries = []navigation.NavEntry{
		{Title: "H1", FilePath: ""},
		{Title: "A", FilePath: "a"},
		{Title: "H2", FilePath: ""},
		{Title: "B", FilePath: "b"},
	}
	m.Cursor = 1
	m = m.moveCursor(1)
	// Should skip header at index 2 and land on file at index 3
	if m.Cursor != 3 {
		t.Fatalf("expected skip header, land on file (3), got %d", m.Cursor)
	}
	m = m.moveCursor(1)
	// Should skip header at index 0, land on file at index 1 (wraps)
	if m.Cursor != 1 {
		t.Fatalf("expected wrap to file (1), got %d", m.Cursor)
	}
}
