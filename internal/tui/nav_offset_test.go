package tui

import (
	"fmt"
	"testing"

	"codeberg.org/stelzo/dock/internal/navigation"
)

func TestEnsureCursorVisibleClampsOffset(t *testing.T) {
	m := NewModel("# Intro\n\nBody\n", 120, 30)
	m.Entries = make([]navigation.NavEntry, 0, 40)
	for i := 0; i < 40; i++ {
		m.Entries = append(m.Entries, navigation.NavEntry{Title: fmt.Sprintf("p%d", i), FilePath: "x"})
	}
	m.Cursor = 39
	m.NavOffset = 999
	m = m.ensureCursorVisible(10)
	if m.NavOffset < 0 || m.NavOffset > 30 {
		t.Fatalf("offset out of range: %d", m.NavOffset)
	}
	if m.Cursor < m.NavOffset || m.Cursor >= m.NavOffset+10 {
		t.Fatalf("cursor not visible: cursor=%d offset=%d", m.Cursor, m.NavOffset)
	}
}
