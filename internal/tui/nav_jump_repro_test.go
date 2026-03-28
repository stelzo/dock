package tui

import (
	"fmt"
	"testing"

	"go.steado.tech/dock/internal/navigation"
)

// Reproduces nav movement with interleaved section headers.
func TestNavOffsetDoesNotAdvanceEarlyWithHeaders(t *testing.T) {
	m := NewModel("# t\n", 120, 30)
	m.Entries = []navigation.NavEntry{}
	// header + 9 files + header + 9 files
	m.Entries = append(m.Entries, navigation.NavEntry{Title: "A", FilePath: ""})
	for i := 0; i < 9; i++ {
		m.Entries = append(m.Entries, navigation.NavEntry{Title: fmt.Sprintf("a%d", i), FilePath: "x"})
	}
	m.Entries = append(m.Entries, navigation.NavEntry{Title: "B", FilePath: ""})
	for i := 0; i < 9; i++ {
		m.Entries = append(m.Entries, navigation.NavEntry{Title: fmt.Sprintf("b%d", i), FilePath: "x"})
	}

	space := m.navEntrySpace()
	if space < 6 {
		t.Skip("space too small")
	}

	// Start on first file
	m.Cursor = 1
	m.NavOffset = 0
	prevOffset := m.NavOffset
	// Move down several steps and ensure offset only changes when cursor would leave viewport.
	for step := 0; step < 12; step++ {
		m = m.moveCursor(1)
		if m.Cursor < m.NavOffset || m.Cursor >= m.NavOffset+space {
			t.Fatalf("cursor out of view: cursor=%d offset=%d space=%d", m.Cursor, m.NavOffset, space)
		}
		if m.NavOffset > prevOffset+1 {
			t.Fatalf("offset jumped by more than one: prev=%d now=%d", prevOffset, m.NavOffset)
		}
		prevOffset = m.NavOffset
	}
}
