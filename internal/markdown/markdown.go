package markdown

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"charm.land/glamour/v2"
	glamouransi "charm.land/glamour/v2/ansi"
	"codeberg.org/stelzo/dock/internal/navigation"
	"codeberg.org/stelzo/dock/internal/themes"
	"codeberg.org/stelzo/dock/internal/ui"
)

const (
	admonitionStartMarker = "@@DOCK_ADMONITION@@"
	admonitionEndMarker   = "@@END_DOCK_ADMONITION@@"
)

type CodeBlock struct {
	Lang      string
	Content   string
	Preview   string
	LineCount int
	Line      int
	StartCol  int
	EndCol    int
	StartLine int
	EndLine   int
}

var fenceRe = regexp.MustCompile("^(`{3,}|~{3,})(\\S*).*$")

func ExtractCodeBlocks(md, rendered string) []CodeBlock {
	var blocks []CodeBlock
	lines := strings.Split(md, "\n")
	i := 0
	for i < len(lines) {
		line := lines[i]
		m := fenceRe.FindStringSubmatch(strings.TrimSpace(line))
		if m == nil {
			i++
			continue
		}
		indent := ""
		if idx := strings.Index(line, m[0]); idx > 0 {
			indent = line[:idx]
		}
		marker := m[1]
		lang := m[2]
		i++

		var content []string
		for i < len(lines) {
			l := lines[i]
			if strings.HasPrefix(strings.TrimSpace(l), marker) {
				i++
				break
			}
			content = append(content, strings.TrimPrefix(l, indent))
			i++
		}
		body := strings.Join(content, "\n")
		preview := ""
		for _, l := range content {
			if t := strings.TrimSpace(l); t != "" {
				preview = t
				break
			}
		}
		blocks = append(blocks, CodeBlock{
			Lang:      lang,
			Content:   body,
			Preview:   preview,
			LineCount: len(content),
			Line:      -1,
			StartLine: -1,
			EndLine:   -1,
		})
	}
	return locateCodeBlocks(blocks, rendered)
}

func locateCodeBlocks(blocks []CodeBlock, rendered string) []CodeBlock {
	rendLines := strings.Split(rendered, "\n")
	searchFrom := 0
	for i := range blocks {
		needle := strings.TrimSpace(blocks[i].Preview)
		codeLines := nonEmptyLines(blocks[i].Content)
		if needle == "" && len(codeLines) == 0 {
			continue
		}
		startNeedle := strings.ToLower(needle)
		if startNeedle == "" && len(codeLines) > 0 {
			startNeedle = strings.ToLower(codeLines[0])
		}

		matchNeedle := startNeedle
		if len(matchNeedle) > 30 {
			matchNeedle = matchNeedle[:30]
		}

		for li := searchFrom; li < len(rendLines); li++ {
			plain := ui.AnsiEscRe.ReplaceAllString(rendLines[li], "")
			idx := strings.Index(strings.ToLower(plain), matchNeedle)
			if idx >= 0 {
				blocks[i].Line = li
				blocks[i].StartCol = idx
				blocks[i].EndCol = idx + len([]rune(matchNeedle))
				blocks[i].StartLine = li
				blocks[i].EndLine = findCodeBlockEnd(rendLines, li, blocks[i].Content)
				searchFrom = li + 1
				break
			}
		}
	}
	return blocks
}

func nonEmptyLines(s string) []string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

func findCodeBlockEnd(rendered []string, start int, sourceContent string) int {
	codeLines := nonEmptyLines(sourceContent)
	if len(codeLines) == 0 {
		return start
	}

	end := start
	currentLineInCode := 0
	currentLineInCode++

	for li := start + 1; li < len(rendered) && currentLineInCode < len(codeLines); li++ {
		plain := strings.ToLower(ui.AnsiEscRe.ReplaceAllString(rendered[li], ""))
		needle := strings.ToLower(codeLines[currentLineInCode])

		matchNeedle := needle
		if len(matchNeedle) > 30 {
			matchNeedle = matchNeedle[:30]
		}

		if strings.Contains(plain, matchNeedle) {
			end = li
			currentLineInCode++
		}
	}
	return end
}

type DocLink struct {
	Label        string
	RawText      string // original markdown link text, used for line detection
	URL          string
	IsInternal   bool
	ResolvedPath string
	NavIdx       int
	Line         int // 0-based line in rendered content (-1 if not found)
	StartCol     int
	EndCol       int
}

var (
	imgStripRe   = regexp.MustCompile(`!\[[^\]]*\]\([^)]*\)`)
	mdLinkRe     = regexp.MustCompile(`\[([^\]]+)\]\(([^)\s]+)\)`)
	urlRe        = regexp.MustCompile(`https?://[^\s)]+`)
	mdInlineRe   = regexp.MustCompile("`+|\\*+|_{1,2}")
	inlineCodeRe = regexp.MustCompile("`[^`\n]+`")
)

// stripCodeContent removes fenced code block content and inline code spans
// from markdown so that URLs inside code are not mistaken for real links.
func stripCodeContent(md string) string {
	lines := strings.Split(md, "\n")
	out := make([]string, 0, len(lines))
	inFence := false
	var fenceMarker string
	for _, line := range lines {
		if !inFence {
			if m := fenceRe.FindStringSubmatch(strings.TrimSpace(line)); m != nil {
				inFence = true
				fenceMarker = m[1]
				continue
			}
			out = append(out, line)
		} else {
			if strings.HasPrefix(strings.TrimSpace(line), fenceMarker) {
				inFence = false
			}
		}
	}
	plain := strings.Join(out, "\n")
	return inlineCodeRe.ReplaceAllString(plain, "")
}

func ExtractLinks(md, currentFile, rendered string, entries []navigation.NavEntry) []DocLink {
	stripped := stripCodeContent(imgStripRe.ReplaceAllString(md, ""))
	var links []DocLink
	seen := map[string]bool{}

	for _, m := range mdLinkRe.FindAllStringSubmatch(stripped, -1) {
		text, rawURL := strings.TrimSpace(m[1]), m[2]
		if seen[rawURL] || strings.HasPrefix(rawURL, "#") {
			continue
		}
		seen[rawURL] = true
		lk := DocLink{Label: text, RawText: text, URL: rawURL, NavIdx: -1, Line: -1}
		mdPath := rawURL
		if idx := strings.IndexByte(rawURL, '#'); idx != -1 {
			mdPath = rawURL[:idx]
		}
		if strings.HasSuffix(mdPath, ".md") && !strings.HasPrefix(mdPath, "http") {
			resolved := filepath.Clean(filepath.Join(filepath.Dir(currentFile), mdPath))
			lk.IsInternal = true
			lk.ResolvedPath = resolved
			for i, e := range entries {
				if e.FilePath == resolved {
					lk.NavIdx = i
					lk.Label = e.Title
					break
				}
			}
		}
		links = append(links, lk)
	}

	for _, rawURL := range urlRe.FindAllString(stripped, -1) {
		if seen[rawURL] {
			continue
		}
		seen[rawURL] = true
		links = append(links, DocLink{
			Label:   rawURL,
			RawText: rawURL,
			URL:     rawURL,
			Line:    -1,
		})
	}

	rendLines := strings.Split(rendered, "\n")
	searchFrom := 0
	for i := range links {
		// Prefer OSC 8 URL matching: it survives word-wrap and skips
		// headings that share text with a link label.
		if li, s, e := findOSC8Link(rendLines, links[i].URL, searchFrom); li >= 0 {
			links[i].Line = li
			links[i].StartCol = s
			links[i].EndCol = e
			searchFrom = li
			continue
		}
		// Fallback: plain-text label search (strips inline markdown markers).
		raw := strings.ToLower(links[i].RawText)
		if raw == "" {
			continue
		}
		stripped := mdInlineRe.ReplaceAllString(raw, "")
		for li := searchFrom; li < len(rendLines); li++ {
			plain := strings.ToLower(ui.AnsiEscRe.ReplaceAllString(rendLines[li], ""))
			needle := raw
			idx := strings.Index(plain, needle)
			if idx < 0 && stripped != raw && stripped != "" {
				needle = stripped
				idx = strings.Index(plain, needle)
			}
			if idx >= 0 {
				links[i].Line = li
				links[i].StartCol = idx
				links[i].EndCol = idx + len([]rune(needle))
				searchFrom = li
				break
			}
		}
	}
	return links
}

// findOSC8Link searches rendered lines for the first OSC 8 hyperlink sequence
// whose URL matches rawURL (also tries with a leading "./" stripped or added).
// Returns the line index and the visible start/end columns of the link text,
// or (-1, 0, 0) if not found.
func findOSC8Link(rendLines []string, rawURL string, searchFrom int) (line, startCol, endCol int) {
	candidates := []string{rawURL}
	if strings.HasPrefix(rawURL, "./") {
		candidates = append(candidates, rawURL[2:])
	} else if !strings.Contains(rawURL, "://") {
		candidates = append(candidates, "./"+rawURL)
	}
	const osc8Open = "\x1b]8;"
	const osc8Close = "\x1b]8;;\a"
	for li := searchFrom; li < len(rendLines); li++ {
		raw := rendLines[li]
		for _, u := range candidates {
			marker := ";" + u + "\a"
			idx := strings.Index(raw, marker)
			if idx < 0 {
				continue
			}
			// Find the \x1b]8; that opens this sequence.
			osc8Idx := strings.LastIndex(raw[:idx], osc8Open)
			if osc8Idx < 0 {
				continue
			}
			startCol = ui.VisWidth(raw[:osc8Idx])
			afterMarker := idx + len(marker)
			closeIdx := strings.Index(raw[afterMarker:], osc8Close)
			var linkText string
			if closeIdx >= 0 {
				linkText = raw[afterMarker : afterMarker+closeIdx]
			} else {
				linkText = raw[afterMarker:]
			}
			endCol = startCol + ui.VisWidth(linkText)
			return li, startCol, endCol
		}
	}
	return -1, 0, 0
}

var (
	admonitionRe  = regexp.MustCompile(`^([?!]{3})\+?\s+(\w+)(?:\s+"([^"]*)")?$`)
	tabRe         = regexp.MustCompile(`^={3}\s+"([^"]*)"`)
	kbdRe         = regexp.MustCompile(`<kbd>(.*?)</kbd>`)
	tildeRe       = regexp.MustCompile(`^(~~~+)(\w*).*$`)
	imgAttrRe     = regexp.MustCompile(`!\[([^\]]*)\]\([^)]*\)\s*\{[^}]*\}`)
	imgPlainRe    = regexp.MustCompile(`!\[([^\]]*)\]\([^)]*\)`)
	captionRe     = regexp.MustCompile(`^///\s*(?:caption)?`)
	orderedItemRe = regexp.MustCompile(`^\d+[.)]\s+`)
)

func PreprocessMarkdown(src string) string {
	lines := strings.Split(src, "\n")
	out := make([]string, 0, len(lines))
	i := 0
	for i < len(lines) {
		ln := lines[i]

		if m := admonitionRe.FindStringSubmatch(strings.TrimSpace(ln)); m != nil {
			adType, title := m[2], m[3]
			if title == "" {
				title = navigation.TitleCase(adType)
			}
			i++
			var body []string
			for i < len(lines) {
				l := lines[i]
				trimmed := strings.TrimSpace(l)
				if strings.HasPrefix(l, "    ") || (l == "" && i+1 < len(lines) && strings.HasPrefix(lines[i+1], "    ")) {
					body = append(body, strings.TrimPrefix(l, "    "))
					i++
				} else if trimmed == "" && i+1 < len(lines) && strings.HasPrefix(lines[i+1], "    ") {
					body = append(body, "")
					i++
				} else {
					break
				}
			}
			bodySrc := strings.Join(normalizeAdmonitionBody(body), "\n")
			bodySrc = PreprocessMarkdown(bodySrc)
			out = append(out, fmt.Sprintf("%s|%s|%s", admonitionStartMarker, adType, title))
			if bodySrc != "" {
				out = append(out, strings.Split(bodySrc, "\n")...)
			}
			out = append(out, admonitionEndMarker)
			out = append(out, "")
			continue
		}

		if m := tabRe.FindStringSubmatch(strings.TrimSpace(ln)); m != nil {
			out = append(out, "", "**"+m[1]+"**", "")
			i++
			var tabLines []string
			for i < len(lines) {
				l := lines[i]
				if strings.HasPrefix(l, "    ") || (l == "" && i+1 < len(lines) && strings.HasPrefix(lines[i+1], "    ")) {
					tabLines = append(tabLines, strings.TrimPrefix(l, "    "))
					i++
				} else if l == "" {
					if i+1 < len(lines) && (strings.HasPrefix(lines[i+1], "    ") || tabRe.MatchString(strings.TrimSpace(lines[i+1]))) {
						tabLines = append(tabLines, "")
						i++
					} else {
						i++
						break
					}
				} else {
					break
				}
			}
			if len(tabLines) > 0 {
				processed := PreprocessMarkdown(strings.Join(tabLines, "\n"))
				out = append(out, strings.Split(processed, "\n")...)
			}
			continue
		}

		if m := tildeRe.FindStringSubmatch(strings.TrimSpace(ln)); m != nil {
			fence := strings.Repeat("`", len(m[1]))
			out = append(out, fence+m[2])
			i++
			closing := m[1]
			for i < len(lines) {
				l := lines[i]
				if strings.HasPrefix(strings.TrimSpace(l), closing) {
					out = append(out, fence)
					i++
					break
				}
				out = append(out, l)
				i++
			}
			continue
		}

		if captionRe.MatchString(ln) {
			i++
			var captionLines []string
			for i < len(lines) {
				l := lines[i]
				if captionRe.MatchString(l) {
					i++
					break
				}
				captionLines = append(captionLines, strings.TrimSpace(l))
				i++
			}
			if len(captionLines) > 0 {
				out = append(out, "*"+strings.Join(captionLines, " ")+"*", "")
			}
			continue
		}

		ln = imgAttrRe.ReplaceAllStringFunc(ln, func(s string) string {
			if m := imgAttrRe.FindStringSubmatch(s); m[1] != "" {
				return "[Image: " + m[1] + "]"
			}
			return ""
		})
		ln = imgPlainRe.ReplaceAllStringFunc(ln, func(s string) string {
			if m := imgPlainRe.FindStringSubmatch(s); m[1] != "" {
				return "[Image: " + m[1] + "]"
			}
			return ""
		})
		ln = kbdRe.ReplaceAllString(ln, "`$1`")

		out = append(out, ln)
		i++
	}
	return strings.Join(out, "\n")
}

func normalizeAdmonitionBody(lines []string) []string {
	var out []string
	var para []string
	inFence := false

	flushPara := func() {
		if len(para) == 0 {
			return
		}
		out = append(out, strings.Join(para, " "))
		para = nil
	}

	for _, line := range lines {
		line = strings.TrimRight(line, " \t")
		trimmed := strings.TrimSpace(line)

		if trimmed == "" {
			flushPara()
			out = append(out, "")
			continue
		}
		if isFenceLine(trimmed) {
			flushPara()
			out = append(out, trimmed)
			inFence = !inFence
			continue
		}
		if inFence || isStructuredAdmonitionLine(trimmed) || strings.HasPrefix(line, "    ") || strings.HasPrefix(line, "\t") {
			flushPara()
			out = append(out, line)
			continue
		}

		para = append(para, trimmed)
	}

	flushPara()
	return out
}

func isFenceLine(s string) bool {
	return strings.HasPrefix(s, "```") || strings.HasPrefix(s, "~~~")
}

func isStructuredAdmonitionLine(s string) bool {
	switch {
	case strings.HasPrefix(s, "- "),
		strings.HasPrefix(s, "* "),
		strings.HasPrefix(s, "+ "),
		strings.HasPrefix(s, "> "),
		strings.HasPrefix(s, "#"),
		strings.HasPrefix(s, "|"),
		strings.HasPrefix(s, "[ ] "),
		strings.HasPrefix(s, "[x] "),
		strings.HasPrefix(s, "[X] "),
		orderedItemRe.MatchString(s):
		return true
	default:
		return false
	}
}

func admonIcon(t string) string {
	switch strings.ToLower(t) {
	case "note", "info":
		return "ℹ"
	case "tip", "hint", "important":
		return "💡"
	case "warning", "caution", "attention":
		return "⚠"
	case "danger", "error":
		return "✗"
	case "question", "help", "faq":
		return "?"
	case "success", "check", "done":
		return "✓"
	case "bug":
		return "⚙"
	case "example":
		return "»"
	default:
		return "›"
	}
}

func RenderMarkdown(md string, th themes.Theme, width int) string {
	if strings.Contains(md, admonitionStartMarker) {
		return renderMarkdownWithAdmonitions(md, th, width)
	}
	return renderMarkdownChunk(md, th, width)
}

func renderMarkdownChunk(md string, th themes.Theme, width int) string {
	cfg, ok := th.GlamourConfig()
	if !ok {
		return md
	}

	r, err := glamour.NewTermRenderer(
		glamour.WithStyles(cfg),
		glamour.WithWordWrap(effectiveWrapWidth(cfg, width)),
		glamour.WithInlineTableLinks(true),
	)
	if err != nil {
		return md
	}
	out, err := r.Render(md)
	if err != nil {
		return md
	}
	return out
}

func renderMarkdownWithAdmonitions(md string, th themes.Theme, width int) string {
	lines := strings.Split(md, "\n")
	var out strings.Builder
	var chunk []string

	flushChunk := func() {
		if len(chunk) == 0 {
			return
		}
		out.WriteString(renderMarkdownChunk(strings.Join(chunk, "\n"), th, width))
		chunk = nil
	}

	for i := 0; i < len(lines); i++ {
		line := lines[i]
		if !strings.HasPrefix(line, admonitionStartMarker+"|") {
			chunk = append(chunk, line)
			continue
		}

		flushChunk()

		parts := strings.SplitN(line, "|", 3)
		adType, title := "", ""
		if len(parts) >= 3 {
			adType, title = parts[1], parts[2]
		}

		i++
		var body []string
		for i < len(lines) && lines[i] != admonitionEndMarker {
			body = append(body, lines[i])
			i++
		}

		out.WriteString(renderAdmonition(adType, title, strings.Join(body, "\n"), th, width))
	}

	flushChunk()
	return out.String()
}

func renderAdmonition(adType, title, body string, th themes.Theme, width int) string {
	cfg, ok := th.GlamourConfig()
	if !ok {
		return body
	}

	token := "│ "
	if cfg.BlockQuote.IndentToken != nil && *cfg.BlockQuote.IndentToken != "" {
		token = *cfg.BlockQuote.IndentToken
	}
	innerWidth := max(1, width-ui.VisWidth(token))

	titleRendered := strings.Trim(renderMarkdownChunk(fmt.Sprintf("**%s %s**", admonIcon(adType), title), th, innerWidth), "\n")
	bodyRendered := strings.Trim(renderMarkdownChunk(body, th, innerWidth), "\n")

	var lines []string
	if titleRendered != "" {
		lines = append(lines, prefixBlockLines(titleRendered, token)...)
	}
	lines = append(lines, token)
	if bodyRendered != "" {
		lines = append(lines, prefixBlockLines(bodyRendered, token)...)
	}
	return strings.Join(lines, "\n") + "\n"
}

func prefixBlockLines(s, prefix string) []string {
	rawLines := strings.Split(s, "\n")
	lines := make([]string, 0, len(rawLines))
	for _, line := range rawLines {
		if strings.TrimSpace(ui.AnsiEscRe.ReplaceAllString(line, "")) == "" {
			lines = append(lines, prefix)
			continue
		}
		lines = append(lines, prefix+line)
	}
	return lines
}

func effectiveWrapWidth(cfg glamouransi.StyleConfig, width int) int {
	extra := blockIndentOverhead(cfg.BlockQuote)
	if extra >= width {
		return 1
	}
	return max(1, width-extra)
}

func blockIndentOverhead(block glamouransi.StyleBlock) int {
	if block.IndentToken == nil {
		return 0
	}
	tokenW := ui.VisWidth(*block.IndentToken)
	indent := 0
	if block.Indent != nil {
		indent = int(*block.Indent)
	}
	if tokenW <= indent {
		return 0
	}
	return tokenW - indent
}
