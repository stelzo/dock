package search

import (
	"os"
	"sort"
	"strings"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"go.steado.tech/dock/internal/navigation"
	"go.steado.tech/dock/internal/ui"
)

const (
	HiOn         = "\x1b[7m"     // reverse video — all non-current matches
	HiOff        = "\x1b[27m"    // reverse off
	HiOnCurrent  = "\x1b[43;30m" // yellow bg + black fg — current match
	HiOffCurrent = "\x1b[49;39m" // reset bg + fg
)

type TextSpan struct{ S, E int }

func WordSpans(plain, word string) []TextSpan {
	rp := []rune(strings.ToLower(plain))
	rw := []rune(strings.ToLower(word))
	if len(rw) == 0 || len(rw) > len(rp) {
		return nil
	}
	var out []TextSpan
	for i := 0; i <= len(rp)-len(rw); {
		ok := true
		for j := range rw {
			if rp[i+j] != rw[j] {
				ok = false
				break
			}
		}
		if ok {
			out = append(out, TextSpan{i, i + len(rw)})
			i += len(rw)
		} else {
			i++
		}
	}
	return out
}

func MergeSpans(spans []TextSpan) []TextSpan {
	if len(spans) <= 1 {
		return spans
	}
	sort.Slice(spans, func(i, j int) bool { return spans[i].S < spans[j].S })
	out := []TextSpan{spans[0]}
	for _, s := range spans[1:] {
		last := &out[len(out)-1]
		if s.S <= last.E {
			if s.E > last.E {
				last.E = s.E
			}
		} else {
			out = append(out, s)
		}
	}
	return out
}

func InjectSpans(line string, spans []TextSpan, on, off string) string {
	if len(spans) == 0 {
		return line
	}
	var out strings.Builder
	plainIdx, spanIdx := 0, 0
	inHL := false
	lb := []byte(line)
	i := 0
	for i < len(lb) {
		if inHL && spanIdx < len(spans) && plainIdx == spans[spanIdx].E {
			out.WriteString(off)
			inHL = false
			spanIdx++
		}
		if !inHL && spanIdx < len(spans) && plainIdx == spans[spanIdx].S {
			out.WriteString(on)
			inHL = true
		}
		if loc := ui.AnsiEscRe.FindIndex(lb[i:]); loc != nil && loc[0] == 0 {
			esc := lb[i : i+loc[1]]
			out.Write(esc)
			i += loc[1]
			if inHL && isSGRReset(esc) {
				out.WriteString(on)
			}
			continue
		}
		r, size := utf8.DecodeRune(lb[i:])
		out.WriteRune(r)
		plainIdx++
		i += size
	}
	if inHL {
		out.WriteString(off)
	}
	return out.String()
}

func isSGRReset(esc []byte) bool {
	if len(esc) < 3 || esc[0] != 0x1b || esc[1] != '[' || esc[len(esc)-1] != 'm' {
		return false
	}
	p := string(esc[2 : len(esc)-1])
	return p == "" || p == "0" || p == "00"
}

func QueryWords(query string) []string {
	return strings.Fields(strings.ToLower(strings.TrimSpace(query)))
}

func LineMatchesAll(plain string, words []string) bool {
	lp := strings.ToLower(plain)
	for _, w := range words {
		if !strings.Contains(lp, w) {
			return false
		}
	}
	return true
}

func FuzzyMatch(haystack, needle string) bool {
	_, ok := FuzzyScore(haystack, needle)
	return ok
}

func FuzzyScore(haystack, needle string) (int, bool) {
	h := strings.ToLower(haystack)
	n := strings.ToLower(needle)
	if len(n) == 0 {
		return 0, true
	}
	hr := []rune(h)
	nr := []rune(n)
	if len(nr) > len(hr) {
		return 0, false
	}

	score := 0
	searchFrom := 0
	prev := -1
	first := -1
	for _, needleRune := range nr {
		found := -1
		for i := searchFrom; i < len(hr); i++ {
			if hr[i] == needleRune {
				found = i
				break
			}
		}
		if found < 0 {
			return 0, false
		}
		if first < 0 {
			first = found
		}
		if prev >= 0 {
			score += found - prev - 1
		}
		prev = found
		searchFrom = found + 1
	}

	// Prefer prefix and tighter matches.
	score += first * 2
	score += len(hr) - len(nr)
	return score, true
}

func PsearchFilter(entries []navigation.NavEntry, query string) []int {
	if strings.TrimSpace(query) == "" {
		var out []int
		for i, e := range entries {
			if e.FilePath != "" {
				out = append(out, i)
			}
		}
		return out
	}

	type ranked struct {
		idx   int
		score int
	}
	var rankedEntries []ranked
	for i, e := range entries {
		if e.FilePath == "" {
			continue
		}
		if score, ok := FuzzyScore(e.Title, query); ok {
			rankedEntries = append(rankedEntries, ranked{idx: i, score: score})
		}
	}
	sort.SliceStable(rankedEntries, func(i, j int) bool {
		if rankedEntries[i].score != rankedEntries[j].score {
			return rankedEntries[i].score < rankedEntries[j].score
		}
		li := strings.ToLower(entries[rankedEntries[i].idx].Title)
		lj := strings.ToLower(entries[rankedEntries[j].idx].Title)
		if li != lj {
			return li < lj
		}
		return rankedEntries[i].idx < rankedEntries[j].idx
	})

	out := make([]int, 0, len(rankedEntries))
	for _, r := range rankedEntries {
		out = append(out, r.idx)
	}
	return out
}

func FindMatchLines(content, query string) []int {
	words := QueryWords(query)
	if len(words) == 0 {
		return nil
	}
	var out []int
	for i, line := range strings.Split(content, "\n") {
		plain := ui.AnsiEscRe.ReplaceAllString(line, "")
		if LineMatchesAll(plain, words) {
			out = append(out, i)
		}
	}
	return out
}

func ApplyHighlights(content, query string, currentLine int) string {
	words := QueryWords(query)
	if len(words) == 0 {
		return content
	}
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		plain := ui.AnsiEscRe.ReplaceAllString(line, "")
		if !LineMatchesAll(plain, words) {
			continue
		}
		var spans []TextSpan
		for _, w := range words {
			spans = append(spans, WordSpans(plain, w)...)
		}
		if len(spans) == 0 {
			continue
		}
		on, off := HiOn, HiOff
		if i == currentLine {
			on, off = HiOnCurrent, HiOffCurrent
		}
		lines[i] = InjectSpans(line, MergeSpans(spans), on, off)
	}
	return strings.Join(lines, "\n")
}

func HighlightImageLine(content string, targetLine int) string {
	return HighlightLineRangeWithCodes(content, targetLine, targetLine, HiOnCurrent, HiOffCurrent)
}

func HighlightLineRangeWithCodes(content string, startLine, endLine int, on, off string) string {
	lines := strings.Split(content, "\n")
	for i := range lines {
		if i < startLine || i > endLine {
			continue
		}
		plain := ui.AnsiEscRe.ReplaceAllString(lines[i], "")
		spans := []TextSpan{{S: 0, E: len([]rune(plain))}}
		lines[i] = InjectSpans(lines[i], spans, on, off)
	}
	return strings.Join(lines, "\n")
}

func HighlightTargetLine(content string, targetLine, startCol, endCol int) string {
	return HighlightTargetLineWithCodes(content, targetLine, startCol, endCol, HiOnCurrent, HiOffCurrent)
}

func HighlightTargetLineWithCodes(content string, targetLine, startCol, endCol int, on, off string) string {
	lines := strings.Split(content, "\n")
	if targetLine < 0 || targetLine >= len(lines) {
		return content
	}
	if startCol >= 0 && endCol > startCol {
		lines[targetLine] = InjectSpans(lines[targetLine], []TextSpan{{S: startCol, E: endCol}}, on, off)
	} else {
		plain := ui.AnsiEscRe.ReplaceAllString(lines[targetLine], "")
		lines[targetLine] = InjectSpans(lines[targetLine], []TextSpan{{S: 0, E: len([]rune(plain))}}, on, off)
	}
	return strings.Join(lines, "\n")
}

type GlobalResult struct {
	NavIdx   int
	HitCount int
}

type GsearchMsg struct {
	Query   string
	Results []GlobalResult
}

func GlobalSearch(entries []navigation.NavEntry, query string) tea.Cmd {
	return func() tea.Msg {
		words := QueryWords(query)
		if len(words) == 0 {
			return GsearchMsg{Query: query}
		}
		var results []GlobalResult
		for i, e := range entries {
			if e.FilePath == "" {
				continue
			}
			raw, err := os.ReadFile(e.FilePath)
			if err != nil {
				continue
			}
			hits := 0
			for line := range strings.SplitSeq(string(raw), "\n") {
				if LineMatchesAll(line, words) {
					hits++
				}
			}
			if hits > 0 {
				results = append(results, GlobalResult{NavIdx: i, HitCount: hits})
			}
		}
		sort.Slice(results, func(i, j int) bool {
			if results[i].HitCount != results[j].HitCount {
				return results[i].HitCount > results[j].HitCount
			}
			return strings.ToLower(entries[results[i].NavIdx].Title) < strings.ToLower(entries[results[j].NavIdx].Title)
		})
		return GsearchMsg{Query: query, Results: results}
	}
}
