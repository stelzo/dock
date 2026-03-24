package markdown

import (
	"strings"
	"testing"

	"codeberg.org/stelzo/dock/internal/themes"
	"codeberg.org/stelzo/dock/internal/ui"
)

func TestEffectiveWrapWidthAccountsForQuoteTokenOverhead(t *testing.T) {
	cfg, ok := themes.Themes[0].GlamourConfig()
	if !ok {
		t.Fatal("expected glamour config")
	}

	got := effectiveWrapWidth(cfg, 80)
	if got != 79 {
		t.Fatalf("effectiveWrapWidth = %d, want 79", got)
	}
}

func TestPreprocessMarkdownReflowsAdmonitionParagraphs(t *testing.T) {
	src := strings.Join([]string{
		`!!! tip "From Comparing to full E2E Testing"`,
		"",
		`    By just writing a "proc1 == proc2", and adding 4 lines of code in total,`,
		`    you can check the equality of a variable from two different processes`,
		`    (proc1, proc2) that don't need to share the language or system.`,
		"",
		`    - list item stays a list`,
		`    - second item`,
	}, "\n")

	got := PreprocessMarkdown(src)

	wantPara := `By just writing a "proc1 == proc2", and adding 4 lines of code in total, you can check the equality of a variable from two different processes (proc1, proc2) that don't need to share the language or system.`
	if !strings.Contains(got, wantPara) {
		t.Fatalf("preprocessed admonition did not reflow paragraph:\n%s", got)
	}
	if !strings.Contains(got, admonitionStartMarker+"|tip|From Comparing to full E2E Testing") {
		t.Fatalf("preprocessed admonition did not emit admonition marker:\n%s", got)
	}
	if !strings.Contains(got, "- list item stays a list") {
		t.Fatalf("preprocessed admonition did not preserve list items:\n%s", got)
	}
}

func TestRenderMarkdownRendersAdmonitionWithoutBareContinuationLines(t *testing.T) {
	src := strings.Join([]string{
		`!!! tip "From Comparing to full E2E Testing"`,
		"",
		`    By just writing a "proc1 == proc2", and adding 4 lines of code in total, you can check the equality of a variable from two different processes (proc1, proc2) that don't need to share the language or system. Comparing is just syncing both variables with the Sync UI.`,
	}, "\n")

	got := RenderMarkdown(PreprocessMarkdown(src), themes.Themes[0], 60)
	for _, line := range strings.Split(got, "\n") {
		plain := strings.TrimSpace(ui.AnsiEscRe.ReplaceAllString(line, ""))
		if plain == "check" || plain == "need" || plain == "share" || plain == "variables" {
			t.Fatalf("found bare continuation line %q in rendered admonition:\n%s", plain, got)
		}
	}
}

func TestExtractCodeBlocksTracksRenderedBlockRange(t *testing.T) {
	src := strings.Join([]string{
		"~~~awk",
		"first line",
		"second line",
		"third line",
		"~~~",
	}, "\n")

	rendered := RenderMarkdown(PreprocessMarkdown(src), themes.Themes[0], 80)
	blocks := ExtractCodeBlocks(src, rendered)
	if len(blocks) != 1 {
		t.Fatalf("got %d code blocks, want 1", len(blocks))
	}
	if blocks[0].StartLine < 0 {
		t.Fatalf("StartLine = %d, want >= 0", blocks[0].StartLine)
	}
	if blocks[0].EndLine <= blocks[0].StartLine {
		t.Fatalf("EndLine = %d, want > StartLine %d", blocks[0].EndLine, blocks[0].StartLine)
	}
}
