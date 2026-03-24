package tui

import (
	"fmt"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

func TestJoinVerticalTrailingEmpty(t *testing.T) {
	for n := 3; n <= 30; n++ {
		lines := make([]string, n)
		for i := 0; i < n-2; i++ {
			lines[i] = fmt.Sprintf("line%d", i)
		}
		lines[n-2] = ""
		lines[n-1] = ""
		joined := lipgloss.JoinVertical(lipgloss.Left, lines...)
		rendered := lipgloss.NewStyle().
			Padding(0, 1).
			Border(lipgloss.RoundedBorder()).
			Height(n).
			Render(joined)
		rawLines := strings.Split(rendered, "\n")
		if len(rawLines) != n+2 {
			t.Errorf("n=%d: expected %d rows, got %d (joined has %d newlines)",
				n, n+2, len(rawLines), strings.Count(joined, "\n"))
		}
	}
}
