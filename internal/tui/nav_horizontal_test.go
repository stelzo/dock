package tui

import (
	"testing"

	"codeberg.org/stelzo/dock/internal/navigation"
)

func TestClipHorizontal(t *testing.T) {
	if got := clipHorizontal("abcdef", 2, 3); got != "cde" {
		t.Fatalf("clipHorizontal()=%q want %q", got, "cde")
	}
	if got := clipHorizontal("abc", 10, 3); got != "" {
		t.Fatalf("clipHorizontal out-of-range=%q want empty", got)
	}
}

func TestNavRenderEntryUsesHorizontalOffset(t *testing.T) {
	m := NewModel("", 80, 24)
	body := "deeply/nested/path/file.md"
	got0 := m.navRenderEntry("▶ ", body, 14)
	m.NavXOffset = 8
	got1 := m.navRenderEntry("▶ ", body, 14)
	if got0 == got1 {
		t.Fatalf("expected different render with nav offset, got same %q", got0)
	}
	if got1 == "" {
		t.Fatal("expected non-empty render after horizontal offset")
	}
}

func TestNavPanClampsAtZero(t *testing.T) {
	m := NewModel("", 80, 24)
	m.Focus = "nav"
	m.navPan(-5)
	if m.NavXOffset != 0 {
		t.Fatalf("expected nav offset clamp at 0, got %d", m.NavXOffset)
	}
	m.navPan(4)
	if m.NavXOffset != 4 {
		t.Fatalf("expected nav offset 4, got %d", m.NavXOffset)
	}
}

func TestAutoAdjustNavXOffsetForDeepNestedEntry(t *testing.T) {
	m := NewModel("", 80, 24)
	m.Focus = "nav"
	m.Entries = []navigation.NavEntry{
		{Title: "DOCS", FilePath: "", Depth: 0},
		{Title: "a-very-long-file-name.md", FilePath: "x", Depth: 6},
	}
	m.Cursor = 1
	m.autoAdjustNavXOffset()
	if m.NavXOffset <= 0 {
		t.Fatalf("expected positive auto nav offset, got %d", m.NavXOffset)
	}
}
