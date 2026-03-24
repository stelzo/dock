package ui

import (
	"regexp"
	"strings"
	"unicode/utf8"

	"charm.land/lipgloss/v2"
)

// AnsiEscRe strips ANSI CSI sequences and OSC hyperlinks so visible-width
// calculations stay accurate when Glamour emits OSC 8 links.
var AnsiEscRe = regexp.MustCompile(
	`\x1b\[[0-?]*[ -/]*[@-~]` +
		`|\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)` +
		`|\x1b[P^_].*?\x1b\\` +
		`|\x1b[@-_]`,
)

func VisWidth(s string) int {
	return lipgloss.Width(AnsiEscRe.ReplaceAllString(s, ""))
}

func Truncate(s string, max int) string {
	if max < 1 {
		return ""
	}
	if VisWidth(s) <= max {
		return s
	}
	var b strings.Builder
	w := 0
	for _, r := range s {
		rw := lipgloss.Width(string(r))
		if w+rw > max-1 {
			break
		}
		b.WriteRune(r)
		w += rw
	}
	return b.String() + "…"
}

func JoinHints(width int, parts []string) string {
	if len(parts) == 0 {
		return ""
	}
	var lines []string
	var cur []string
	curLen := 0
	for _, p := range parts {
		pVis := VisWidth(p)
		if curLen > 0 && curLen+3+pVis > width {
			lines = append(lines, strings.Join(cur, " · "))
			cur = nil
			curLen = 0
		}
		cur = append(cur, p)
		if curLen > 0 {
			curLen += 3 // " · "
		}
		curLen += pVis
	}
	if len(cur) > 0 {
		lines = append(lines, strings.Join(cur, " · "))
	}
	return strings.Join(lines, "\n")
}

func HardWrap(s string, width int) string {
	if width < 1 {
		return s
	}
	var out strings.Builder
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		if i > 0 {
			out.WriteByte('\n')
		}

		lb := []byte(line)
		vis := 0
		j := 0
		for j < len(lb) {
			if loc := AnsiEscRe.FindIndex(lb[j:]); loc != nil && loc[0] == 0 {
				out.Write(lb[j : j+loc[1]])
				j += loc[1]
				continue
			}

			r, size := utf8.DecodeRune(lb[j:])
			out.WriteRune(r)
			vis += lipgloss.Width(string(r))
			j += size

			if vis >= width && j < len(lb) {
				out.WriteByte('\n')
				vis = 0
			}
		}
	}
	return out.String()
}

func OverlayAtX(bg, fg string, x int) string {
	return OverlayAtLine(bg, fg, x, 0)
}

func OverlayAtLine(bg, fg string, x, lineIdx int) string {
	lines := strings.Split(bg, "\n")
	if lineIdx < 0 || lineIdx >= len(lines) {
		return bg
	}

	target := lines[lineIdx]
	fgVisLen := VisWidth(fg)
	var out strings.Builder
	bgb := []byte(target)
	i, vis := 0, 0
	for i < len(bgb) && vis < x {
		if loc := AnsiEscRe.FindIndex(bgb[i:]); loc != nil && loc[0] == 0 {
			out.Write(bgb[i : i+loc[1]])
			i += loc[1]
		} else {
			r, size := utf8.DecodeRune(bgb[i:])
			out.Write(bgb[i : i+size])
			i += size
			vis += lipgloss.Width(string(r))
		}
	}
	for vis < x {
		out.WriteRune(' ')
		vis++
	}
	out.WriteString("\x1b[0m")
	out.WriteString(fg)
	out.WriteString("\x1b[0m")
	skip := 0
	for i < len(bgb) && skip < fgVisLen {
		if loc := AnsiEscRe.FindIndex(bgb[i:]); loc != nil && loc[0] == 0 {
			i += loc[1]
		} else {
			r, size := utf8.DecodeRune(bgb[i:])
			i += size
			skip += lipgloss.Width(string(r))
		}
	}
	out.Write(bgb[i:])
	lines[lineIdx] = out.String()
	return strings.Join(lines, "\n")
}

func OverlayCenter(bg, fg string) string {
	bgLines := strings.Split(bg, "\n")
	fgLines := strings.Split(fg, "\n")
	bgW := 0
	for _, l := range bgLines {
		if w := VisWidth(l); w > bgW {
			bgW = w
		}
	}
	fgW := 0
	for _, l := range fgLines {
		if w := VisWidth(l); w > fgW {
			fgW = w
		}
	}
	startY := max(0, (len(bgLines)-len(fgLines))/2)
	startX := max(0, (bgW-fgW)/2)
	result := make([]string, len(bgLines))
	copy(result, bgLines)
	for i, fgLine := range fgLines {
		if idx := startY + i; idx < len(result) {
			result[idx] = OverlayAtX(result[idx], fgLine, startX)
		}
	}
	return strings.Join(result, "\n")
}
