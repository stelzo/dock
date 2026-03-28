package tui

import (
	"fmt"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"codeberg.org/stelzo/dock/internal/navigation"
	"codeberg.org/stelzo/dock/internal/ui"
)

func plainView(s string) []string {
	clean := ui.AnsiEscRe.ReplaceAllString(s, "")
	clean = strings.TrimRight(clean, "\n")
	return strings.Split(clean, "\n")
}

func sampleDocs() string {
	return "# Intro\n\nSome paragraph.\n\n## Features\n\n- one\n- two\n\n## Sync\n\nmore text\n"
}

func TestLayoutKeepsTopAndBottomBordersWithStatus(t *testing.T) {
	m := NewModel(sampleDocs(), 120, 30)
	m.Focus = "nav"
	if !m.Ready {
		t.Fatal("model not ready")
	}

	lines := plainView(m.View().Content)
	if len(lines) == 0 {
		t.Fatal("empty view")
	}
	if len(lines) > m.Height {
		t.Fatalf("view overflow: got %d, height %d", len(lines), m.Height)
	}

	if !strings.Contains(lines[0], "╭") {
		t.Fatalf("missing top border in first line: %q", lines[0])
	}

	if len(lines) < 2 {
		t.Fatalf("not enough lines for border+status: %d", len(lines))
	}
	bottomBorder := lines[len(lines)-2]
	if !strings.Contains(bottomBorder, "╰") {
		t.Fatalf("missing bottom border above status: %q", bottomBorder)
	}
}

func TestStatusIsSingleLineAndNotWrapped(t *testing.T) {
	m := NewModel(sampleDocs(), 80, 24)
	if strings.Contains(m.statusLine(), "\n") {
		t.Fatalf("statusLine unexpectedly wrapped: %q", m.statusLine())
	}
	if h := m.statusHeight(); h != 1 {
		t.Fatalf("statusHeight should be 1, got %d", h)
	}
}

// TestLayoutDoesNotOverflowAfterNavigation ensures that cursor movement never
// produces a frame taller than the terminal height. The leading-newline bug in
// the "edge to edge" commit caused every render to be one row taller, which
// scrolled the alt-screen upward on each keypress.
func TestLayoutDoesNotOverflowAfterNavigation(t *testing.T) {
	m := NewModel(sampleDocs(), 120, 30)
	if !m.Ready {
		t.Fatal("model not ready")
	}
	for step := 0; step < len(m.Entries)*2+4; step++ {
		m = m.moveCursor(1)
		lines := plainView(m.View().Content)
		if len(lines) > m.Height {
			t.Fatalf("view overflow at step %d: got %d lines, height %d", step, len(lines), m.Height)
		}
	}
	for step := 0; step < len(m.Entries)*2+4; step++ {
		m = m.moveCursor(-1)
		lines := plainView(m.View().Content)
		if len(lines) > m.Height {
			t.Fatalf("view overflow at step %d (reverse): got %d lines, height %d", step, len(lines), m.Height)
		}
	}
}

// TestLayoutNoOverflowWithDeepNavAndSmallTerminal simulates a realistic nav
// with section headers (including consecutive ones) on a small terminal, and
// ensures no frame overflows when navigating through all entries.
func TestLayoutNoOverflowWithDeepNavAndSmallTerminal(t *testing.T) {
	// Mimic minot docs: headers + files at various depths, consecutive headers
	entries := []navigation.NavEntry{
		{Title: "Home", FilePath: "index.md"},
		{Title: "Getting Started", FilePath: ""},
		{Title: "Query", FilePath: "intro/query.md"},
		{Title: "Share", FilePath: "intro/share.md"},
		{Title: "Pubsub", FilePath: "intro/pubsub.md"},
		{Title: "Installation", FilePath: ""},
		{Title: "Ros", FilePath: "installation/ros.md"},
		{Title: "Script", FilePath: "installation/script.md"},
		{Title: "Tools", FilePath: "installation/tools.md"},
		{Title: "Packages", FilePath: "installation/packages.md"},
		{Title: "Features", FilePath: ""},
		{Title: "Features", FilePath: ""},
		{Title: "Bagquery", FilePath: "features/bagquery.md"},
		{Title: "Varshare", FilePath: "features/varshare.md"},
		{Title: "Services", FilePath: "features/services.md"},
		{Title: "Sync", FilePath: ""},
		{Title: "Overview", FilePath: "features/sync/overview.md"},
		{Title: "Keybindings", FilePath: "features/sync/keybindings.md"},
		{Title: "Extra", FilePath: "lore.md"},
	}
	for _, h := range []int{24, 30, 40} {
		for _, w := range []int{80, 120} {
			t.Run(fmt.Sprintf("h%d_w%d", h, w), func(t *testing.T) {
				m := NewModel("# t\n", w, h)
				m.Entries = entries
				// start cursor on first file
				m.Cursor = 0
				for i, e := range entries {
					if e.FilePath != "" {
						m.Cursor = i
						break
					}
				}
				n := len(entries)
				// Navigate forward through all entries multiple times
				for step := 0; step < n*3; step++ {
					m = m.moveCursor(1)
					lines := plainView(m.View().Content)
					if len(lines) > m.Height {
						t.Fatalf("forward overflow step %d (cursor=%d): got %d lines, height %d", step, m.Cursor, len(lines), m.Height)
					}
				}
				// Navigate backward
				for step := 0; step < n*3; step++ {
					m = m.moveCursor(-1)
					lines := plainView(m.View().Content)
					if len(lines) > m.Height {
						t.Fatalf("backward overflow step %d (cursor=%d): got %d lines, height %d", step, m.Cursor, len(lines), m.Height)
					}
				}
			})
		}
	}
}

// TestNavAndContentBordersAlign checks that the nav panel bottom border and the
// content panel bottom border appear on the same row. Misalignment was caused by
// the two panels rendering at different heights. Also tests alignment after
// navigating to the last entry in the nav.
func TestNavAndContentBordersAlign(t *testing.T) {
	for _, h := range []int{24, 30} {
		t.Run(fmt.Sprintf("h%d_initial", h), func(t *testing.T) {
			m := NewModel(sampleDocs(), 120, h)
			if !m.Ready {
				t.Fatal("model not ready")
			}
			checkBorders(t, m)
		})

		t.Run(fmt.Sprintf("h%d_last_entry", h), func(t *testing.T) {
			m := NewModel(sampleDocs(), 120, h)
			if !m.Ready {
				t.Fatal("model not ready")
			}
			// Navigate to the very last file entry
			n := len(m.Entries)
			for step := 0; step < n*2; step++ {
				m = m.moveCursor(1)
			}
			checkBorders(t, m)
		})
	}
}

func checkBorders(t *testing.T, m Model) {
	t.Helper()
	lines := plainView(m.View().Content)
	if len(lines) < 2 {
		t.Fatal("too few lines")
	}
	if len(lines) > m.Height {
		t.Fatalf("view overflow: got %d lines, height %d", len(lines), m.Height)
	}
	borderLine := lines[len(lines)-2]
	count := strings.Count(borderLine, "╰")
	if count < 2 {
		t.Fatalf("expected both nav and content bottom borders on same line, got %d '╰' in: %q", count, borderLine)
	}
}

func TestRenderedFrameLeavesWidthGuardBand(t *testing.T) {
	m := NewModel(sampleDocs(), 120, 30)
	if !m.Ready {
		t.Fatal("model not ready")
	}
	lines := plainView(m.View().Content)
	maxW := 0
	for _, line := range lines {
		if w := lipgloss.Width(line); w > maxW {
			maxW = w
		}
	}
	if maxW > m.Width {
		t.Fatalf("frame width = %d, want at most terminal width %d", maxW, m.Width)
	}
}

func TestMarkdownWidthLeavesSafetyMargin(t *testing.T) {
	m := NewModel(sampleDocs(), 120, 30)
	if got, want := m.markdownWidth(), m.vpW()-1; got != want {
		t.Fatalf("markdownWidth = %d, want %d", got, want)
	}
}
