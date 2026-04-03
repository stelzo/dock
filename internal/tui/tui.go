package tui

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"charm.land/log/v2"
	"github.com/charmbracelet/ssh"
	"github.com/charmbracelet/x/ansi/kitty"
	"github.com/charmbracelet/x/mosaic"
	sixel "github.com/mattn/go-sixel"
	xdraw "golang.org/x/image/draw"
	"golang.org/x/term"

	"go.steado.tech/dock/internal/config"
	"go.steado.tech/dock/internal/graphics"
	"go.steado.tech/dock/internal/images"
	"go.steado.tech/dock/internal/markdown"
	"go.steado.tech/dock/internal/navigation"
	"go.steado.tech/dock/internal/search"
	"go.steado.tech/dock/internal/themes"
	"go.steado.tech/dock/internal/ui"
)

type GraphicsReader struct {
	R io.Reader
	P *tea.Program
	b string
}

func (g *GraphicsReader) Fd() uintptr {
	type fdReader interface {
		Fd() uintptr
	}
	if f, ok := g.R.(fdReader); ok {
		return f.Fd()
	}
	return 0
}

func (g *GraphicsReader) Name() string {
	type namer interface {
		Name() string
	}
	if f, ok := g.R.(namer); ok {
		return f.Name()
	}
	return ""
}

func (g *GraphicsReader) Write(p []byte) (int, error) {
	if w, ok := g.R.(io.Writer); ok {
		return w.Write(p)
	}
	return 0, io.ErrClosedPipe
}

func (g *GraphicsReader) Close() error {
	if c, ok := g.R.(io.Closer); ok {
		return c.Close()
	}
	return nil
}

func (g *GraphicsReader) Read(p []byte) (int, error) {
	n, err := g.R.Read(p)
	if n <= 0 || g.P == nil {
		return n, err
	}

	clean, msg, found, pending := stripGraphicsResponses(g.b + string(p[:n]))
	g.b = pending
	if found {
		g.P.Send(msg)
	}
	return copy(p, clean), err
}

func stripGraphicsResponses(in string) (clean string, msg graphics.GraphicsMsg, found bool, pending string) {
	const (
		daPrefix    = "\x1b["
		kittyPrefix = "\x1b_G"
	)

	var out strings.Builder
	pos := 0
	for pos < len(in) {
		daIdx := strings.Index(in[pos:], daPrefix)
		if daIdx >= 0 {
			daIdx += pos
		}
		kittyIdx := strings.Index(in[pos:], kittyPrefix)
		if kittyIdx >= 0 {
			kittyIdx += pos
		}

		var start int
		kind := ""
		switch {
		case daIdx >= 0 && (kittyIdx == -1 || daIdx < kittyIdx):
			start = daIdx
			kind = "da"
		case kittyIdx >= 0:
			start = kittyIdx
			kind = "kitty"
		default:
			out.WriteString(in[pos:])
			return out.String(), msg, found, ""
		}

		out.WriteString(in[pos:start])
		rest := in[start:]

		switch kind {
		case "da":
			if !strings.HasPrefix(rest, "\x1b[?") {
				out.WriteByte(rest[0])
				pos = start + 1
				continue
			}
			end := strings.IndexByte(rest, 'c')
			if end == -1 {
				return out.String(), msg, found, rest
			}
			res := rest[:end+1]
			if strings.Contains(res, ";4") || strings.Contains(res, "?4") {
				msg.Sixel = true
				found = true
				pos = start + end + 1
				continue
			}
			out.WriteString(res)
			pos = start + end + 1
		case "kitty":
			end := strings.Index(rest, "\x1b\\")
			if end == -1 {
				return out.String(), msg, found, rest
			}
			res := rest[:end+2]
			if strings.Contains(res, "OK") {
				msg.Kitty = true
				found = true
				pos = start + end + 2
				continue
			}
			out.WriteString(res)
			pos = start + end + 2
		}
	}
	return out.String(), msg, found, ""
}

func Run(docs string) {
	w, h, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil || w == 0 {
		w = 80
	}
	if h == 0 {
		h = 24
	}

	m := NewModel(docs, w, h)
	termEnv := os.Getenv("TERM")
	m.IsTmux = strings.HasPrefix(termEnv, "tmux") || os.Getenv("TMUX") != ""
	m.IsScreen = strings.HasPrefix(termEnv, "screen")

	if term.IsTerminal(int(os.Stdin.Fd())) {
		if !m.SixelSupported {
			m.SixelSupported = graphics.IsSixelTerminal(termEnv)
		}
		if !m.KittySupported {
			m.KittySupported = graphics.IsKittyTerminal(termEnv) ||
				graphics.IsKittyTerminal(os.Getenv("TERM_PROGRAM")) ||
				os.Getenv("KITTY_WINDOW_ID") != ""
		}
	}

	m.UpdateCellPix()

	p := tea.NewProgram(m)

	go watchFiles(docs, p)

	if _, err := p.Run(); err != nil {
		log.Fatal("TUI error", "err", err)
	}
}

const (
	preferredNavInnerW = 26
	helpPanelHeight    = 7
)

type layoutMetrics struct {
	safeW         int
	statusH       int
	vpW           int
	vpH           int
	bodyH         int
	navOuterW     int
	navInnerW     int
	navInnerY     int
	contentOuterX int
	contentOuterW int
	contentInnerX int
	helpPanelH    int
	contentInnerY int
}

type Model struct {
	Docs       string
	Entries    []navigation.NavEntry
	Cursor     int
	NavOffset  int
	NavXOffset int
	NavHidden  bool
	Vp         viewport.Model
	Width      int
	Height     int
	Ready      bool
	Focus      string
	ThemeIdx   int

	CodeBlocks []markdown.CodeBlock
	CopyIdx    int
	Links      []markdown.DocLink
	LinkIdx    int
	StatusMsg  string

	RawContent     string
	SearchQuery    string
	SearchActive   bool
	SearchMatches  []int
	SearchMatchIdx int

	GsearchQuery   string
	GsearchResults []search.GlobalResult
	GsearchCursor  int

	PsearchQuery   string
	PsearchResults []int
	PsearchCursor  int
	DocSeq         uint64

	NavHistory     []navHistEntry
	RestoreYOffset int

	ImageRefs          []images.ImageRef
	ImageIdx           int
	CodeActive         bool
	LinkActive         bool
	ImageActive        bool
	HelpOverlay        bool
	ImageOverlay       bool
	ImageOverlaySrc    string
	ImageOverlaySixel  string
	ImageOverlayKitty  string
	ImageOverlayW      int
	ImageOverlayH      int
	NeedsGraphicsClear bool

	SixelSupported bool
	KittySupported bool
	IsTmux         bool
	IsScreen       bool
	CellPixW       int
	CellPixH       int
	SshSession     ssh.Session

	NavScrollSeq      uint64
	LastNavScrollTime time.Time
}

type navScrollDebounceMsg uint64

func navScrollDebounceCmd(seq uint64) tea.Cmd {
	return tea.Tick(150*time.Millisecond, func(_ time.Time) tea.Msg {
		return navScrollDebounceMsg(seq)
	})
}

func NewModel(docs string, w, h int) Model {
	entries := navigation.BuildNav(docs)
	m := Model{
		Docs:           docs,
		Entries:        entries,
		Width:          w,
		Height:         h,
		Focus:          "nav",
		Ready:          w > 0 && h > 0,
		ThemeIdx:       config.DefaultThemeIdx,
		RestoreYOffset: -1,
	}
	fileCount := 0
	for i, e := range entries {
		if e.FilePath != "" {
			if fileCount == 0 {
				m.Cursor = i
			}
			fileCount++
		}
	}
	if fileCount == 1 {
		m.NavHidden = true
		m.Focus = "content"
	}
	if m.Ready {
		m.Vp = viewport.New(viewport.WithWidth(m.vpW()), viewport.WithHeight(m.vpH()))
		m.Vp.MouseWheelDelta = 1
	}
	return m
}

func (m Model) currentTheme() themes.Theme { return themes.Themes[m.ThemeIdx] }

func (m Model) wrapTmux(s string) string {
	if !m.IsTmux {
		return s
	}
	s = strings.ReplaceAll(s, "\x1b", "\x1b\x1b")
	return "\x1bPtmux;" + s + "\x1b\\"
}

func (m Model) wrapScreen(s string) string {
	if !m.IsScreen {
		return s
	}
	return "\x1bP" + s + "\x1b\\"
}

func (m Model) wrapGraphics(s string) string {
	if m.IsTmux {
		return m.wrapTmux(s)
	}
	if m.IsScreen {
		return m.wrapScreen(s)
	}
	return s
}

func (m Model) vpW() int {
	return m.layoutMetrics().vpW
}

func (m Model) statusLeft() string {
	switch {
	case m.StatusMsg != "":
		return m.StatusMsg
	case m.Focus == "search":
		s := "/" + m.SearchQuery + "█"
		if len(m.SearchMatches) > 0 {
			s += fmt.Sprintf("  [%d/%d]", m.SearchMatchIdx+1, len(m.SearchMatches))
		} else if m.SearchQuery != "" {
			s += "  no matches"
		}
		return s
	case m.Focus == "pagesearch":
		return m.statusLeftBase()
	case m.Focus == "gsearch":
		return m.statusLeftBase()
	case m.Focus == "theme":
		return "Theme · " + themes.Themes[m.ThemeIdx].Name
	case m.CodeActive:
		return fmt.Sprintf("Code  %d/%d", m.CopyIdx+1, len(m.CodeBlocks))
	case m.LinkActive:
		if m.LinkIdx < len(m.Links) && !m.Links[m.LinkIdx].IsInternal {
			return fmt.Sprintf("Link  %d/%d  ·  %s", m.LinkIdx+1, len(m.Links), m.Links[m.LinkIdx].URL)
		}
		return fmt.Sprintf("Link  %d/%d", m.LinkIdx+1, len(m.Links))
	case m.ImageActive:
		return fmt.Sprintf("Image  %d/%d", m.ImageIdx+1, len(m.ImageRefs))
	case m.ImageOverlay && m.ImageIdx < len(m.ImageRefs):
		return m.ImageRefs[m.ImageIdx].Alt
	case m.Focus == "nav" && m.NavXOffset > 0:
		return fmt.Sprintf("%s  ·  nav x:%d", m.statusLeftBase(), m.NavXOffset)
	case m.SearchActive:
		return fmt.Sprintf("/%s  [%d/%d]", m.SearchQuery, m.SearchMatchIdx+1, len(m.SearchMatches))
	default:
		return m.statusLeftBase()
	}
}

func (m Model) statusLeftBase() string {
	switch {
	case m.Cursor >= 0 && m.Cursor < len(m.Entries):
		if title := m.Entries[m.Cursor].Title; title != "" {
			return title
		}
	}
	return config.DocsTitle
}

func clipHorizontal(s string, off, width int) string {
	if width <= 0 {
		return ""
	}
	r := []rune(s)
	if off < 0 {
		off = 0
	}
	if off >= len(r) {
		return ""
	}
	end := off + width
	if end > len(r) {
		end = len(r)
	}
	return string(r[off:end])
}

func (m *Model) navPan(delta int) {
	if m.Focus != "nav" {
		return
	}
	m.NavXOffset += delta
	if m.NavXOffset < 0 {
		m.NavXOffset = 0
	}
}

func (m *Model) autoAdjustNavXOffset() {
	lm := m.layoutMetrics()
	niw := lm.navInnerW
	if niw <= 0 || m.Cursor < 0 || m.Cursor >= len(m.Entries) {
		m.NavXOffset = 0
		return
	}
	e := m.Entries[m.Cursor]
	if e.FilePath == "" {
		m.NavXOffset = 0
		return
	}
	body := strings.Repeat("  ", e.Depth) + e.Title
	avail := max(1, niw-ui.VisWidth("▶ "))
	bodyRunes := []rune(body)
	n := len(bodyRunes)
	if n <= avail {
		m.NavXOffset = 0
		return
	}
	nameRunes := []rune(e.Title)
	nameLen := len(nameRunes)
	if nameLen >= avail {
		m.NavXOffset = max(0, n-avail)
		return
	}
	nameStart := n - nameLen
	target := nameStart - max(0, (avail-nameLen)/2)
	if target < 0 {
		target = 0
	}
	maxOff := n - avail
	if target > maxOff {
		target = maxOff
	}
	m.NavXOffset = target
}

func (m Model) navRenderEntry(prefix, body string, width int) string {
	avail := max(0, width-ui.VisWidth(prefix))
	if avail == 0 {
		return ui.Truncate(prefix, width)
	}
	clipped := clipHorizontal(body, m.NavXOffset, avail)
	if clipped == "" && m.NavXOffset > 0 {
		clipped = clipHorizontal(body, max(0, len([]rune(body))-avail), avail)
	}
	if m.NavXOffset > 0 && clipped != "" {
		clipped = "…" + clipHorizontal(clipped, 1, max(0, avail-1))
	}
	return ui.Truncate(prefix+clipped, width)
}

func (m Model) statusHints() string {
	switch {
	case m.CodeActive:
		return "  c/C next · y copy · esc  "
	case m.LinkActive:
		if m.LinkIdx < len(m.Links) && !m.Links[m.LinkIdx].IsInternal {
			return "  l/L next · y copy · esc  "
		}
		return "  l/L next · enter open · esc  "
	case m.ImageActive:
		return "  i/I next · enter view · esc  "
	case m.Focus == "nav":
		return "  ↑↓ navigate · enter open · p page · / global  "
	default:
		var parts []string
		if len(m.CodeBlocks) > 0 {
			parts = append(parts, "c code")
		}
		if len(m.Links) > 0 {
			parts = append(parts, "l link")
		}
		if len(m.ImageRefs) > 0 {
			parts = append(parts, "i img")
		}
		parts = append(parts, "/ search")
		return "  " + strings.Join(parts, " · ") + "  "
	}
}

func (m Model) statusLine() string {
	th := m.currentTheme()
	lm := m.layoutMetrics()

	helpText := " ? Help "
	if m.HelpOverlay {
		helpText = " ? Close "
	}
	hintsText := m.statusHints()
	total := m.Vp.TotalLineCount()
	visible := m.Vp.Height()
	pct := 100.0
	if total > visible {
		pct = float64(m.Vp.YOffset()) / float64(total-visible) * 100
		if pct > 100 {
			pct = 100
		}
		if pct < 0 {
			pct = 0
		}
	}
	scrollText := fmt.Sprintf("  %3.f%% ", pct)

	rightW := ui.VisWidth(hintsText) + ui.VisWidth(scrollText) + ui.VisWidth(helpText)
	left := " " + ui.Truncate(m.statusLeft(), max(1, lm.safeW-rightW-1))
	padding := strings.Repeat(" ", max(0, lm.safeW-ui.VisWidth(left)-rightW))

	mainStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(th.StatusFg)).
		Background(lipgloss.Color(th.BorderInactive))
	hintsStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(th.SectionHdrFg)).
		Background(lipgloss.Color(th.BorderInactive))
	scrollStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(th.SectionHdrFg)).
		Background(lipgloss.Color(th.BorderInactive))
	helpStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(th.NavSelFg)).
		Background(lipgloss.Color(th.NavSelBg))

	return mainStyle.Render(left+padding) + hintsStyle.Render(hintsText) + scrollStyle.Render(scrollText) + helpStyle.Render(helpText)
}

func (m Model) statusHeight() int {
	return m.layoutMetrics().statusH
}

func (m Model) vpH() int {
	return m.layoutMetrics().vpH
}

func (m Model) navHeaderRows() int {
	if config.DocsTitle != "" || m.Focus == "pagesearch" || m.Focus == "gsearch" {
		return 2
	}
	return 0
}

func (m Model) layoutMetrics() layoutMetrics {
	safeW := max(1, m.Width)
	statusH := 1
	helpH := 0
	if m.HelpOverlay {
		helpH = helpPanelHeight
	}
	vpH := m.Height - 2 - statusH - helpH
	if vpH < 1 {
		vpH = 1
	}
	navOuterW := 0
	navInnerW := 0
	if !m.NavHidden && m.Width > 40 {
		navInnerW = preferredNavInnerW
		navOuterW = navInnerW + 4
		if safeW-navOuterW < 20 {
			navInnerW = max(10, safeW-20-4)
			navOuterW = navInnerW + 4
		}
	}

	contentOuterX := navOuterW
	contentOuterW := max(1, safeW-navOuterW)
	contentInnerX := contentOuterX + 1
	contentInnerY := 1
	if m.NavHidden {
		vpH = m.Height - statusH - helpH
		if vpH < 1 {
			vpH = 1
		}
		contentOuterW = safeW
		contentInnerX = contentOuterX
		contentInnerY = 0
	}
	vpW := contentOuterW - 2
	if m.NavHidden {
		vpW = contentOuterW
	}
	if vpW < 1 {
		vpW = 1
	}
	return layoutMetrics{
		safeW:         safeW,
		statusH:       statusH,
		vpW:           vpW,
		vpH:           vpH,
		bodyH:         max(1, m.Height-statusH),
		navOuterW:     navOuterW,
		navInnerW:     navInnerW,
		navInnerY:     1,
		contentOuterX: contentOuterX,
		contentOuterW: contentOuterW,
		contentInnerX: contentInnerX,
		contentInnerY: contentInnerY,
		helpPanelH:    helpH,
	}
}

func (m *Model) UpdateCellPix() {
	m.CellPixW, m.CellPixH = graphics.GetCellPixelSize(m.SshSession)
}

type docMsg struct {
	Seq        uint64
	content    string
	codeBlocks []markdown.CodeBlock
	links      []markdown.DocLink
	imageRefs  []images.ImageRef
}

type clearStatusMsg struct{}
type clearGraphicsMsg struct{}
type fileChangedMsg struct{}

func (m Model) openInEditor() tea.Cmd {
	if m.SshSession != nil {
		return nil
	}
	if m.Cursor < 0 || m.Cursor >= len(m.Entries) {
		return nil
	}
	path := m.Entries[m.Cursor].FilePath
	if path == "" {
		return nil
	}
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = os.Getenv("VISUAL")
	}
	if editor == "" {
		editor = "vi"
	}
	cmd := exec.Command(editor, path)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return tea.ExecProcess(cmd, func(_ error) tea.Msg {
		return fileChangedMsg{}
	})
}

func watchFiles(docs string, p *tea.Program) {
	snapshot := func() map[string]time.Time {
		m := map[string]time.Time{}
		_ = filepath.WalkDir(docs, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".md") {
				return nil
			}
			if info, err := d.Info(); err == nil {
				m[path] = info.ModTime()
			}
			return nil
		})
		return m
	}
	prev := snapshot()
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for range ticker.C {
		cur := snapshot()
		changed := len(cur) != len(prev)
		if !changed {
			for k, v := range cur {
				if prev[k] != v {
					changed = true
					break
				}
			}
		}
		if changed {
			prev = cur
			p.Send(fileChangedMsg{})
		}
	}
}

type imageOverlayMsg struct {
	rendered string
	sixel    string
	kitty    string
	charW    int
	charH    int
}

func blankImagePlaceholder(w, h int) string {
	if w < 1 || h < 1 {
		return ""
	}
	line := strings.Repeat(" ", w)
	lines := make([]string, h)
	for i := range lines {
		lines[i] = line
	}
	return strings.Join(lines, "\n")
}

func kittyImagePayloadPNG(img image.Image, pixW, pixH, cols, rows int) string {
	if img == nil {
		return ""
	}
	opts := &kitty.Options{
		Action:          kitty.TransmitAndPut,
		Format:          kitty.PNG,
		Transmission:    kitty.Direct,
		ImageWidth:      pixW,
		ImageHeight:     pixH,
		Columns:         cols,
		Rows:            rows,
		Z:               1,
		DoNotMoveCursor: true,
		Quite:           2,
		Chunk:           true,
	}
	var out bytes.Buffer
	if err := kitty.EncodeGraphics(&out, img, opts); err != nil {
		return ""
	}
	return out.String()
}

func (m Model) loadImageOverlay() tea.Cmd {
	if m.ImageIdx >= len(m.ImageRefs) {
		return nil
	}
	ref := m.ImageRefs[m.ImageIdx]
	maxW := m.vpW() - 4
	maxH := m.vpH() - 4
	if maxW < 4 {
		maxW = 4
	}
	if maxH < 4 {
		maxH = 4
	}
	useSixel := m.SixelSupported
	useKitty := m.KittySupported
	cellPixW, cellPixH := m.CellPixW, m.CellPixH
	return func() tea.Msg {
		imgPath := ref.ResolvedPath
		if strings.HasPrefix(imgPath, "http://") || strings.HasPrefix(imgPath, "https://") {
			p, err := images.CacheImage(imgPath)
			if err != nil {
				return imageOverlayMsg{rendered: "[Download failed: " + ref.Alt + "]"}
			}
			imgPath = p
		}
		if imgPath == "" {
			return imageOverlayMsg{rendered: "[Image not found: " + ref.Alt + "]"}
		}
		f, err := os.Open(imgPath)
		if err != nil {
			return imageOverlayMsg{rendered: "[Cannot open: " + ref.Alt + "]"}
		}
		defer func() { _ = f.Close() }()
		img, _, err := image.Decode(f)
		if err != nil {
			return imageOverlayMsg{rendered: "[Cannot decode: " + ref.Alt + "]"}
		}
		charW, charH := images.ImageOverlaySize(img.Bounds().Dx(), img.Bounds().Dy(), maxW, maxH)
		mo := mosaic.New().Width(charW).Height(charH)
		mosaicRendered := mo.Render(img)
		if useKitty || useSixel {
			cw := cellPixW
			if cw <= 0 {
				cw = 8
			}
			ch := cellPixH
			if ch <= 0 {
				ch = 16
			}
			pixW, pixH := charW*cw, charH*ch
			dst := image.NewRGBA(image.Rect(0, 0, pixW, pixH))
			draw.Draw(dst, dst.Bounds(), &image.Uniform{color.White}, image.Point{}, draw.Src)
			xdraw.CatmullRom.Scale(dst, dst.Bounds(), img, img.Bounds(), xdraw.Over, nil)

			if useKitty {
				kittyPayload := kittyImagePayloadPNG(dst, pixW, pixH, charW, charH)
				if kittyPayload != "" {
					return imageOverlayMsg{
						rendered: blankImagePlaceholder(charW, charH),
						kitty:    kittyPayload,
						charW:    charW,
						charH:    charH,
					}
				}
			}

			if useSixel {
				var buf bytes.Buffer
				if encErr := sixel.NewEncoder(&buf).Encode(dst); encErr == nil {
					return imageOverlayMsg{
						rendered: blankImagePlaceholder(charW, charH),
						sixel:    buf.String(),
						charW:    charW,
						charH:    charH,
					}
				}
			}
		}
		return imageOverlayMsg{rendered: mosaicRendered, charW: charW, charH: charH}
	}
}

func loadDoc(path string, th themes.Theme, width int, entries []navigation.NavEntry, seq uint64) tea.Cmd {
	return func() tea.Msg {
		raw, err := os.ReadFile(path)
		if err != nil {
			return docMsg{Seq: seq, content: "*Error loading file: " + err.Error() + "*"}
		}
		src := string(raw)
		rendered := markdown.RenderMarkdown(markdown.PreprocessMarkdown(src), th, width)
		return docMsg{
			Seq:        seq,
			content:    rendered,
			codeBlocks: markdown.ExtractCodeBlocks(src, rendered),
			links:      markdown.ExtractLinks(src, path, rendered, entries),
			imageRefs:  images.ExtractImageRefs(src, path, rendered),
		}
	}
}

type navHistEntry struct {
	Cursor  int
	YOffset int
}

const navHistMax = 100

func (m *Model) appendNavHistory(e navHistEntry) {
	m.NavHistory = append(m.NavHistory, e)
	if len(m.NavHistory) > navHistMax {
		m.NavHistory = m.NavHistory[len(m.NavHistory)-navHistMax:]
	}
}

func (m *Model) openCurrent() tea.Cmd {
	if m.Cursor < 0 || m.Cursor >= len(m.Entries) {
		return nil
	}
	e := m.Entries[m.Cursor]
	if e.FilePath == "" {
		return nil
	}
	m.DocSeq++
	renderW := m.markdownWidth()
	return tea.Batch(
		loadDoc(e.FilePath, m.currentTheme(), renderW, m.Entries, m.DocSeq),
		m.setTitleCmd(e.Title),
	)
}

func (m Model) markdownWidth() int {
	return max(1, m.vpW()-1)
}

func (m Model) navEntrySpace() int {
	h := m.vpH() - m.navHeaderRows()
	if h < 0 {
		return 0
	}
	return h
}

func (m Model) moveCursor(d int) Model {
	n := len(m.Entries)
	if n == 0 {
		return m
	}
	for i := 0; i < n; i++ {
		m.Cursor = (m.Cursor + d + n) % n
		if m.Entries[m.Cursor].FilePath != "" {
			break
		}
	}
	space := m.navEntrySpace()
	m = m.ensureCursorVisible(space)
	return m
}

func (m Model) ensureCursorVisible(space int) Model {
	if space < 1 {
		space = 1
	}
	maxOffset := max(0, len(m.Entries)-space)
	if m.NavOffset < 0 {
		m.NavOffset = 0
	}
	if m.NavOffset > maxOffset {
		m.NavOffset = maxOffset
	}
	if m.Cursor < m.NavOffset {
		m.NavOffset = m.Cursor
	} else if m.Cursor >= m.NavOffset+space {
		m.NavOffset = m.Cursor - space + 1
	}
	if m.NavOffset < 0 {
		m.NavOffset = 0
	}
	if m.NavOffset > maxOffset {
		m.NavOffset = maxOffset
	}
	return m
}

func highlightCodes(bg, fg string) (string, string) {
	st := lipgloss.NewStyle().
		Background(lipgloss.Color(bg)).
		Foreground(lipgloss.Color(fg)).
		Bold(true)
	rendered := st.Render("X")
	idx := strings.Index(rendered, "X")
	if idx < 0 {
		return search.HiOnCurrent, search.HiOffCurrent
	}
	return rendered[:idx], rendered[idx+1:]
}

func (m *Model) clearContentTargetModes() {
	m.CodeActive = false
	m.LinkActive = false
	m.ImageActive = false
}

func (m *Model) showRawContent() {
	m.Vp.SetContent(m.RawContent)
}

func (m *Model) highlightCurrentCodeBlock() {
	if !m.CodeActive || len(m.CodeBlocks) == 0 {
		m.showRawContent()
		return
	}
	ref := m.CodeBlocks[m.CopyIdx]
	if ref.StartLine < 0 {
		m.showRawContent()
		return
	}
	th := m.currentTheme()
	on, off := highlightCodes(th.NavSelBg, th.NavSelFg)
	endCol := ref.StartCol + 2
	if endCol <= ref.StartCol {
		endCol = ref.StartCol + 2
	}
	m.Vp.SetContent(search.HighlightTargetLineWithCodes(m.RawContent, ref.StartLine, ref.StartCol, endCol, on, off))
	if ref.StartLine < m.Vp.YOffset() || ref.StartLine >= m.Vp.YOffset()+m.Vp.Height() {
		m.Vp.SetYOffset(max(0, ref.StartLine-m.Vp.Height()/2))
	}
}

func (m *Model) highlightCurrentLink() {
	if !m.LinkActive || len(m.Links) == 0 {
		m.showRawContent()
		return
	}
	ref := m.Links[m.LinkIdx]
	if ref.Line < 0 {
		m.showRawContent()
		return
	}
	th := m.currentTheme()
	on, off := highlightCodes(th.NavSelBg, th.NavSelFg)
	m.Vp.SetContent(search.HighlightTargetLineWithCodes(m.RawContent, ref.Line, ref.StartCol, ref.EndCol, on, off))
	if ref.Line < m.Vp.YOffset() || ref.Line >= m.Vp.YOffset()+m.Vp.Height() {
		m.Vp.SetYOffset(max(0, ref.Line-m.Vp.Height()/2))
	}
}

func (m *Model) highlightCurrentImage() {
	if !m.ImageActive || len(m.ImageRefs) == 0 {
		m.showRawContent()
		return
	}
	target := m.ImageRefs[m.ImageIdx].Line
	th := m.currentTheme()
	on, off := highlightCodes(th.NavSelBg, th.NavSelFg)
	m.Vp.SetContent(search.HighlightLineRangeWithCodes(m.RawContent, target, target, on, off))
	if target < m.Vp.YOffset() || target >= m.Vp.YOffset()+m.Vp.Height() {
		m.Vp.SetYOffset(max(0, target-m.Vp.Height()/2))
	}
}

func (m *Model) jumpToNav() tea.Cmd {
	var cmd tea.Cmd
	m.clearContentTargetModes()
	m.HelpOverlay = false
	m.ImageOverlay = false
	m.ImageOverlaySrc = ""
	m.ImageOverlaySixel = ""
	m.ImageOverlayKitty = ""
	if m.NavHidden {
		m.NavHidden = false
		m.Vp.SetWidth(m.vpW())
		cmd = m.openCurrent()
	}
	if m.NeedsGraphicsClear || m.SixelSupported || m.KittySupported {
		m.NeedsGraphicsClear = true
		if cmd != nil {
			cmd = tea.Batch(cmd, clearGraphicsCmd())
		} else {
			cmd = clearGraphicsCmd()
		}
	}
	m.showRawContent()
	m.Focus = "nav"
	*m = m.ensureCursorVisible(m.navEntrySpace())
	return cmd
}

func clearGraphicsCmd() tea.Cmd {
	return tea.Tick(50*time.Millisecond, func(time.Time) tea.Msg { return clearGraphicsMsg{} })
}

func statusCmd(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(time.Time) tea.Msg { return clearStatusMsg{} })
}

func (m Model) setTitleCmd(title string) tea.Cmd {
	return func() tea.Msg {
		var w io.Writer = os.Stdout
		if m.SshSession != nil {
			w = m.SshSession
		}
		fullTitle := title
		if config.DocsTitle != "" {
			if title != "" {
				fullTitle = fmt.Sprintf("%s · %s", title, config.DocsTitle)
			} else {
				fullTitle = config.DocsTitle
			}
		}
		if fullTitle != "" {
			seq := fmt.Sprintf("\x1b]0;%s\x07", fullTitle)

			if m.IsTmux {
				_, _ = fmt.Fprintf(w, "%s", m.wrapTmux(seq))
				_, _ = fmt.Fprintf(w, "\x1bk%s\x1b\\", fullTitle)
			} else if m.IsScreen {
				_, _ = fmt.Fprintf(w, "%s", m.wrapScreen(seq))
				_, _ = fmt.Fprintf(w, "\x1bk%s\x1b\\", fullTitle)
			} else {
				_, _ = fmt.Fprint(w, seq)
			}
		}
		return nil
	}
}

func (m Model) writeGraphics(seq string) {
	if seq == "" {
		return
	}
	var w io.Writer = os.Stdout
	if m.SshSession != nil {
		w = m.SshSession
	}
	_, _ = fmt.Fprint(w, m.wrapGraphics(seq))
}

func (m *Model) closeImageOverlay() tea.Cmd {
	m.ImageOverlay = false
	m.ImageOverlaySrc = ""
	m.ImageOverlaySixel = ""
	m.ImageOverlayKitty = ""
	m.NeedsGraphicsClear = true
	return clearGraphicsCmd()
}

func (m *Model) scrollDebounce() bool {
	now := time.Now()
	if now.Sub(m.LastNavScrollTime) < 60*time.Millisecond {
		return false
	}
	m.LastNavScrollTime = now
	return true
}

func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{m.openCurrent()}
	if m.SshSession != nil {
		cmds = append(cmds, func() tea.Msg {
			graphics.SendProbe(m.SshSession, m.IsTmux)
			return nil
		})
	}
	return tea.Batch(cmds...)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.Width, m.Height = msg.Width, msg.Height
		if !m.Ready {
			m.Vp = viewport.New(viewport.WithWidth(m.vpW()), viewport.WithHeight(m.vpH()))
			m.Vp.MouseWheelDelta = 1
			m.Ready = true
		} else {
			m.Vp.SetWidth(m.vpW())
			m.Vp.SetHeight(m.vpH())
		}
		m.UpdateCellPix()
		m.ImageOverlay = false
		m.ImageOverlaySrc = ""
		m.ImageOverlaySixel = ""
		m.ImageOverlayKitty = ""
		m.NeedsGraphicsClear = true
		cmds = append(cmds, m.openCurrent(), clearGraphicsCmd())

	case graphics.GraphicsMsg:
		if msg.Sixel {
			m.SixelSupported = true
		}
		if msg.Kitty {
			m.KittySupported = true
		}
		cmds = append(cmds, m.openCurrent())

	case navScrollDebounceMsg:
		if uint64(msg) == m.NavScrollSeq && m.Cursor >= 0 && m.Cursor < len(m.Entries) && m.Entries[m.Cursor].FilePath != "" {
			cmds = append(cmds, m.openCurrent())
		}

	case docMsg:
		if msg.Seq != m.DocSeq {
			return m, nil
		}
		m.RawContent = msg.content
		m.Vp.SetContent(m.RawContent)
		m.Vp.GotoTop()
		if m.RestoreYOffset >= 0 {
			m.Vp.SetYOffset(m.RestoreYOffset)
			m.RestoreYOffset = -1
		}
		m.CodeBlocks = msg.codeBlocks
		m.CopyIdx = 0
		m.Links = msg.links
		m.LinkIdx = 0
		m.ImageRefs = msg.imageRefs
		m.ImageIdx = 0
		m.CodeActive = false
		m.LinkActive = false
		m.ImageActive = false
		m.ImageOverlay = false
		m.ImageOverlaySrc = ""
		m.ImageOverlaySixel = ""
		m.ImageOverlayKitty = ""
		m.ImageOverlayW = 0
		m.ImageOverlayH = 0
		if m.SearchActive && m.SearchQuery != "" {
			m.SearchMatches = search.FindMatchLines(m.RawContent, m.SearchQuery)
			m.SearchMatchIdx = 0
			curLine := -1
			if len(m.SearchMatches) > 0 {
				curLine = m.SearchMatches[0]
			}
			highlighted := search.ApplyHighlights(m.RawContent, m.SearchQuery, curLine)
			m.Vp.SetContent(highlighted)
			if curLine >= 0 {
				m.Vp.SetYOffset(curLine)
			}
		}

	case fileChangedMsg:
		currentPath := ""
		if m.Cursor >= 0 && m.Cursor < len(m.Entries) {
			currentPath = m.Entries[m.Cursor].FilePath
		}
		savedOffset := m.Vp.YOffset()
		m.Entries = navigation.BuildNav(m.Docs)
		if m.Cursor >= len(m.Entries) {
			m.Cursor = max(0, len(m.Entries)-1)
		}
		for i, e := range m.Entries {
			if e.FilePath == currentPath {
				m.Cursor = i
				break
			}
		}
		m = m.ensureCursorVisible(m.navEntrySpace())
		m.RestoreYOffset = savedOffset
		cmds = append(cmds, m.openCurrent())

	case search.GsearchMsg:
		if msg.Query == m.GsearchQuery {
			m.GsearchResults = msg.Results
			m.GsearchCursor = 0
		}

	case clearStatusMsg:
		m.StatusMsg = ""

	case clearGraphicsMsg:
		m.NeedsGraphicsClear = false
		m.writeGraphics("\x1b_Ga=d,d=A\x1b\\")
		return m, nil

	case imageOverlayMsg:
		m.ImageOverlaySrc = msg.rendered
		m.ImageOverlaySixel = msg.sixel
		m.ImageOverlayKitty = msg.kitty
		m.ImageOverlayW = msg.charW
		m.ImageOverlayH = msg.charH
		m.ImageOverlay = true
		m.writeGraphics(m.imageOverlayInject())

	case tea.MouseMsg:
		mouse := msg.Mouse()
		lm := m.layoutMetrics()
		inNav := !m.NavHidden && mouse.X >= 0 && mouse.X < lm.navOuterW
		inContent := mouse.X >= lm.contentOuterX && mouse.X < lm.contentOuterX+lm.contentOuterW
		inRows := mouse.Y >= lm.contentInnerY && mouse.Y < lm.contentInnerY+lm.vpH

		followLink := func(lk markdown.DocLink) {
			m.Focus = "content"
			if lk.IsInternal && lk.NavIdx >= 0 {
				m.appendNavHistory(navHistEntry{m.Cursor, m.Vp.YOffset()})
				m.Cursor = lk.NavIdx
				m = m.ensureCursorVisible(m.navEntrySpace())
				cmds = append(cmds, m.openCurrent())
			} else if lk.IsInternal {
				m.StatusMsg = "Page not found in nav"
				cmds = append(cmds, statusCmd(2*time.Second))
			} else {
				cmds = append(cmds, tea.SetClipboard(lk.URL))
				m.StatusMsg = "Link copied to clipboard"
				cmds = append(cmds, statusCmd(2*time.Second))
			}
		}

		switch ev := msg.(type) {
		case tea.MouseWheelMsg:
			switch mouse.Button {
			case tea.MouseWheelUp:
				switch m.Focus {
				case "theme":
					if m.scrollDebounce() && m.ThemeIdx > 0 {
						m.ThemeIdx--
						cmds = append(cmds, m.openCurrent())
					}
					return m, tea.Batch(cmds...)
				default:
					if inNav {
						if m.scrollDebounce() {
							m = m.moveCursor(-1)
							m.NavScrollSeq++
							cmds = append(cmds, navScrollDebounceCmd(m.NavScrollSeq))
						}
						return m, tea.Batch(cmds...)
					}
					var cmd tea.Cmd
					m.Vp, cmd = m.Vp.Update(ev)
					cmds = append(cmds, cmd)
				}
			case tea.MouseWheelDown:
				switch m.Focus {
				case "theme":
					if m.scrollDebounce() && m.ThemeIdx < len(themes.Themes)-1 {
						m.ThemeIdx++
						cmds = append(cmds, m.openCurrent())
					}
					return m, tea.Batch(cmds...)
				default:
					if inNav {
						if m.scrollDebounce() {
							m = m.moveCursor(1)
							m.NavScrollSeq++
							cmds = append(cmds, navScrollDebounceCmd(m.NavScrollSeq))
						}
						return m, tea.Batch(cmds...)
					}
					var cmd tea.Cmd
					m.Vp, cmd = m.Vp.Update(ev)
					cmds = append(cmds, cmd)
				}
			}
		case tea.MouseClickMsg, tea.MouseReleaseMsg:
			if m.ImageOverlay {
				fg := m.renderImageOverlay()
				startX, startY := m.overlayStart(fg)
				fgLines := strings.Split(fg, "\n")
				fgH := len(fgLines)
				fgW := 0
				for _, l := range fgLines {
					if w := ui.VisWidth(l); w > fgW {
						fgW = w
					}
				}
				outside := mouse.X < startX || mouse.X >= startX+fgW || mouse.Y < startY || mouse.Y >= startY+fgH
				if outside {
					cmds = append(cmds, m.closeImageOverlay())
					break
				}
			}
			switch m.Focus {
			case "theme":
				if idx := m.overlayItemAt(mouse.Y, m.renderThemePanel(), len(themes.Themes)); idx >= 0 {
					m.ThemeIdx = idx
					cmds = append(cmds, m.openCurrent())
				}
			case "pagesearch":
				itemRow := mouse.Y - 3
				maxVis := lm.vpH - 2
				start := max(0, m.PsearchCursor-maxVis/2)
				if start+maxVis > len(m.PsearchResults) {
					start = max(0, len(m.PsearchResults)-maxVis)
				}
				idx := start + itemRow
				if inNav && itemRow >= 0 && idx < len(m.PsearchResults) {
					m.Cursor = m.PsearchResults[idx]
					m = m.ensureCursorVisible(m.navEntrySpace())
					m.Focus = "content"
					m.PsearchQuery = ""
					m.PsearchResults = nil
					m.PsearchCursor = 0
					cmds = append(cmds, m.openCurrent())
				}
			default:
				if inNav {
					m.Focus = "nav"
					if inRows {
						titleOffset := 0
						if config.DocsTitle != "" {
							titleOffset = 2
						}
						visIdx := (mouse.Y - lm.navInnerY) - titleOffset
						navIdx := m.NavOffset + visIdx
						if visIdx >= 0 && navIdx >= 0 && navIdx < len(m.Entries) {
							if m.Entries[navIdx].FilePath != "" && navIdx != m.Cursor {
								m.appendNavHistory(navHistEntry{m.Cursor, m.Vp.YOffset()})
							}
							m.Cursor = navIdx
							if m.Entries[navIdx].FilePath != "" {
								cmds = append(cmds, m.openCurrent())
							}
						}
					}
				} else if inContent && inRows {
					contentLine := m.Vp.YOffset() + (mouse.Y - lm.contentInnerY)
					handled := false
					for i, ref := range m.ImageRefs {
						if ref.Line == contentLine {
							m.ImageActive = true
							m.ImageIdx = i
							m.Vp.SetContent(search.HighlightImageLine(m.RawContent, ref.Line))
							cmds = append(cmds, m.loadImageOverlay())
							m.Focus = "content"
							handled = true
							break
						}
					}
					if !handled {
						for _, lk := range m.Links {
							if lk.Line == contentLine {
								mouseX := mouse.X - lm.contentInnerX
								if mouseX >= lk.StartCol && mouseX < lk.EndCol {
									followLink(lk)
									handled = true
									break
								}
							}
						}
					}
					if !handled {
						m.Focus = "content"
					}
				}
			}
		}

	case tea.KeyPressMsg:
		switch msg.String() {
		case " ", "space":
			cmds = append(cmds, m.jumpToNav())
			return m, tea.Batch(cmds...)
		}
		switch m.Focus {
		case "theme":
			switch msg.String() {
			case "up", "k":
				if m.ThemeIdx > 0 {
					m.ThemeIdx--
					cmds = append(cmds, m.openCurrent())
				}
			case "down", "j":
				if m.ThemeIdx < len(themes.Themes)-1 {
					m.ThemeIdx++
					cmds = append(cmds, m.openCurrent())
				}
			case "enter", "t":
				m.Focus = "content"
			case "esc", "q":
				m.Focus = "content"
			case "ctrl+c":
				return m, tea.Quit
			}
		case "search":
			recompute := func() {
				m.SearchMatches = search.FindMatchLines(m.RawContent, m.SearchQuery)
				m.SearchMatchIdx = 0
				if m.SearchQuery != "" {
					curLine := -1
					if len(m.SearchMatches) > 0 {
						curLine = m.SearchMatches[0]
					}
					m.Vp.SetContent(search.ApplyHighlights(m.RawContent, m.SearchQuery, curLine))
					if curLine >= 0 {
						m.Vp.SetYOffset(curLine)
					}
				} else {
					m.Vp.SetContent(m.RawContent)
				}
			}
			switch msg.String() {
			case "esc":
				m.Focus = "content"
				m.SearchQuery = ""
				m.SearchActive = false
				m.SearchMatches = nil
				m.Vp.SetContent(m.RawContent)
			case "ctrl+c":
				return m, tea.Quit
			case "enter":
				m.Focus = "content"
				if len(m.SearchMatches) > 0 {
					m.SearchActive = true
					target := m.SearchMatches[m.SearchMatchIdx]
					if target < m.Vp.YOffset() || target >= m.Vp.YOffset()+m.Vp.Height() {
						m.Vp.SetYOffset(max(0, target-m.Vp.Height()/2))
					}
				} else {
					m.SearchActive = false
				}
			case "backspace", "ctrl+h":
				if len(m.SearchQuery) > 0 {
					runes := []rune(m.SearchQuery)
					m.SearchQuery = string(runes[:len(runes)-1])
					recompute()
				}
			default:
				if text := msg.Key().Text; text != "" {
					m.SearchQuery += text
					recompute()
				}
			}
		case "gsearch":
			switch msg.String() {
			case "esc":
				m.Focus = "nav"
				m.GsearchQuery = ""
				m.GsearchResults = nil
				m.GsearchCursor = 0
			case "ctrl+c":
				return m, tea.Quit
			case "enter":
				if len(m.GsearchResults) > 0 {
					m.appendNavHistory(navHistEntry{m.Cursor, m.Vp.YOffset()})
					r := m.GsearchResults[m.GsearchCursor]
					m.Cursor = r.NavIdx
					m = m.ensureCursorVisible(m.navEntrySpace())
					m.SearchQuery = m.GsearchQuery
					m.SearchActive = true
					m.Focus = "content"
					m.GsearchQuery = ""
					m.GsearchResults = nil
					m.GsearchCursor = 0
					cmds = append(cmds, m.openCurrent())
				}
			case "up", "k":
				if m.GsearchCursor > 0 {
					m.GsearchCursor--
				}
			case "down", "j":
				if m.GsearchCursor < len(m.GsearchResults)-1 {
					m.GsearchCursor++
				}
			case "backspace", "ctrl+h":
				if len(m.GsearchQuery) > 0 {
					runes := []rune(m.GsearchQuery)
					m.GsearchQuery = string(runes[:len(runes)-1])
					if m.GsearchQuery == "" {
						m.GsearchResults = nil
						m.GsearchCursor = 0
					} else {
						cmds = append(cmds, search.GlobalSearch(m.Entries, m.GsearchQuery))
					}
				}
			default:
				if text := msg.Key().Text; text != "" {
					m.GsearchQuery += text
					m.GsearchCursor = 0
					cmds = append(cmds, search.GlobalSearch(m.Entries, m.GsearchQuery))
				}
			}
		case "pagesearch":
			refilter := func() {
				m.PsearchResults = search.PsearchFilter(m.Entries, m.PsearchQuery)
				m.PsearchCursor = 0
			}
			switch msg.String() {
			case "esc", "ctrl+c":
				m.Focus = "nav"
				m.PsearchQuery = ""
				m.PsearchResults = nil
				m.PsearchCursor = 0
			case "enter":
				m.appendNavHistory(navHistEntry{m.Cursor, m.Vp.YOffset()})
				if len(m.PsearchResults) > 0 {
					m.Cursor = m.PsearchResults[m.PsearchCursor]
					m = m.ensureCursorVisible(m.navEntrySpace())
					m.Focus = "content"
					m.PsearchQuery = ""
					m.PsearchResults = nil
					m.PsearchCursor = 0
					cmds = append(cmds, m.openCurrent())
				}
			case "up", "k":
				if m.PsearchCursor > 0 {
					m.PsearchCursor--
				}
			case "down", "j":
				if m.PsearchCursor < len(m.PsearchResults)-1 {
					m.PsearchCursor++
				}
			case "backspace", "ctrl+h":
				if len(m.PsearchQuery) > 0 {
					runes := []rune(m.PsearchQuery)
					m.PsearchQuery = string(runes[:len(runes)-1])
					refilter()
				}
			default:
				if text := msg.Key().Text; text != "" {
					m.PsearchQuery += text
					refilter()
				}
			}
		case "nav":
			switch msg.String() {
			case "q", "ctrl+c":
				return m, tea.Quit
			case "enter", "right", "l":
				m.Focus = "content"
			case "left", "h":
				m.navPan(-2)
			case "p":
				m.Focus = "pagesearch"
				m.PsearchQuery = ""
				m.PsearchResults = search.PsearchFilter(m.Entries, "")
				m.PsearchCursor = 0
			case "/":
				m.Focus = "gsearch"
				m.GsearchQuery = ""
				m.GsearchResults = nil
				m.GsearchCursor = 0
			case "tab":
				m.Focus = "content"
			case "t":
				m.Focus = "theme"
			case "\\":
				m.NavHidden = true
				m.Focus = "content"
				m.Vp.SetWidth(m.vpW())
				cmds = append(cmds, m.openCurrent())
			case "up", "k":
				oldCursor := m.Cursor
				m = m.moveCursor(-1)
				m.autoAdjustNavXOffset()
				if m.Cursor != oldCursor && m.Cursor >= 0 && m.Cursor < len(m.Entries) && m.Entries[m.Cursor].FilePath != "" {
					m.appendNavHistory(navHistEntry{oldCursor, m.Vp.YOffset()})
					cmds = append(cmds, m.openCurrent())
				}
			case "down", "j":
				oldCursor := m.Cursor
				m = m.moveCursor(1)
				m.autoAdjustNavXOffset()
				if m.Cursor != oldCursor && m.Cursor >= 0 && m.Cursor < len(m.Entries) && m.Entries[m.Cursor].FilePath != "" {
					m.appendNavHistory(navHistEntry{oldCursor, m.Vp.YOffset()})
					cmds = append(cmds, m.openCurrent())
				}
			case "L":
				m.navPan(2)
			case "?":
				m.HelpOverlay = !m.HelpOverlay
				m.Vp.SetHeight(m.vpH())
			}
		default:
			switch msg.String() {
			case "q", "ctrl+c":
				return m, tea.Quit
			case "left", "h":
				if m.NavHidden {
					m.NavHidden = false
					m.Vp.SetWidth(m.vpW())
					cmds = append(cmds, m.openCurrent())
				}
				m.Focus = "nav"
				m.autoAdjustNavXOffset()
			case "/":
				m.Focus = "search"
				m.SearchQuery = ""
				m.SearchActive = false
				m.Vp.SetContent(m.RawContent)
			case "n":
				if m.SearchActive && len(m.SearchMatches) > 0 {
					m.SearchMatchIdx = (m.SearchMatchIdx + 1) % len(m.SearchMatches)
					target := m.SearchMatches[m.SearchMatchIdx]
					highlighted := search.ApplyHighlights(m.RawContent, m.SearchQuery, target)
					m.Vp.SetContent(highlighted)
					if target < m.Vp.YOffset() || target >= m.Vp.YOffset()+m.Vp.Height() {
						m.Vp.SetYOffset(max(0, target-m.Vp.Height()/2))
					}
				} else {
					var cmd tea.Cmd
					m.Vp, cmd = m.Vp.Update(msg)
					cmds = append(cmds, cmd)
				}
			case "N":
				if m.SearchActive && len(m.SearchMatches) > 0 {
					m.SearchMatchIdx = (m.SearchMatchIdx - 1 + len(m.SearchMatches)) % len(m.SearchMatches)
					target := m.SearchMatches[m.SearchMatchIdx]
					highlighted := search.ApplyHighlights(m.RawContent, m.SearchQuery, target)
					m.Vp.SetContent(highlighted)
					if target < m.Vp.YOffset() || target >= m.Vp.YOffset()+m.Vp.Height() {
						m.Vp.SetYOffset(max(0, target-m.Vp.Height()/2))
					}
				}
			case "i":
				if len(m.ImageRefs) > 0 {
					if !m.ImageActive {
						m.clearContentTargetModes()
						m.ImageActive = true
						m.ImageIdx = 0
					} else {
						m.ImageIdx = (m.ImageIdx + 1) % len(m.ImageRefs)
					}
					m.highlightCurrentImage()
				} else {
					m.StatusMsg = "No images on this page"
					cmds = append(cmds, statusCmd(2*time.Second))
				}
			case "I":
				if m.ImageActive && len(m.ImageRefs) > 0 {
					m.ImageIdx = (m.ImageIdx - 1 + len(m.ImageRefs)) % len(m.ImageRefs)
					m.highlightCurrentImage()
				}
			case "enter":
				if m.ImageOverlay {
					cmds = append(cmds, m.closeImageOverlay())
				} else if m.CodeActive && len(m.CodeBlocks) > 0 {
					cmds = append(cmds, tea.SetClipboard(m.CodeBlocks[m.CopyIdx].Content))
					m.StatusMsg = "Code copied to clipboard"
					cmds = append(cmds, statusCmd(2*time.Second))
				} else if m.LinkActive && len(m.Links) > 0 {
					lk := m.Links[m.LinkIdx]
					if lk.IsInternal && lk.NavIdx >= 0 {
						m.clearContentTargetModes()
						m.appendNavHistory(navHistEntry{m.Cursor, m.Vp.YOffset()})
						m.Cursor = lk.NavIdx
						m = m.ensureCursorVisible(m.navEntrySpace())
						cmds = append(cmds, m.openCurrent())
					} else if lk.IsInternal {
						m.StatusMsg = "Page not found in nav"
						cmds = append(cmds, statusCmd(2*time.Second))
					} else {
						cmds = append(cmds, tea.SetClipboard(lk.URL))
						m.StatusMsg = "Link copied to clipboard"
						cmds = append(cmds, statusCmd(2*time.Second))
					}
				} else if m.ImageActive {
					cmds = append(cmds, m.loadImageOverlay())
				} else {
					cmds = append(cmds, m.openInEditor())
				}
			case "y":
				if m.CodeActive && len(m.CodeBlocks) > 0 {
					cmds = append(cmds, tea.SetClipboard(m.CodeBlocks[m.CopyIdx].Content))
					m.StatusMsg = "Code copied to clipboard"
					cmds = append(cmds, statusCmd(2*time.Second))
				} else if m.LinkActive && len(m.Links) > 0 {
					cmds = append(cmds, tea.SetClipboard(m.Links[m.LinkIdx].URL))
					m.StatusMsg = "Link copied to clipboard"
					cmds = append(cmds, statusCmd(2*time.Second))
				}
			case "esc":
				if m.ImageOverlay {
					cmds = append(cmds, m.closeImageOverlay())
				} else if m.ImageActive {
					m.clearContentTargetModes()
					m.ImageOverlay = false
					m.ImageOverlaySixel = ""
					m.ImageOverlayKitty = ""
					m.NeedsGraphicsClear = true
					cmds = append(cmds, clearGraphicsCmd())
					m.showRawContent()
				} else if m.CodeActive || m.LinkActive {
					m.clearContentTargetModes()
					m.showRawContent()
				} else if m.SearchActive {
					m.SearchActive = false
					m.SearchQuery = ""
					m.SearchMatches = nil
					m.showRawContent()
				}
			case "backspace":
				if len(m.NavHistory) > 0 {
					entry := m.NavHistory[len(m.NavHistory)-1]
					m.NavHistory = m.NavHistory[:len(m.NavHistory)-1]
					m.Cursor = entry.Cursor
					m = m.ensureCursorVisible(m.navEntrySpace())
					m.RestoreYOffset = entry.YOffset
					cmds = append(cmds, m.openCurrent())
				}
			case "tab":
				if m.NavHidden {
					oldCursor := m.Cursor
					m = m.moveCursor(1)
					if m.Cursor != oldCursor {
						m.appendNavHistory(navHistEntry{oldCursor, m.Vp.YOffset()})
					}
					cmds = append(cmds, m.openCurrent())
				} else {
					m.Focus = "nav"
					m.autoAdjustNavXOffset()
				}
			case "shift+tab":
				if m.NavHidden {
					oldCursor := m.Cursor
					m = m.moveCursor(-1)
					if m.Cursor != oldCursor {
						m.appendNavHistory(navHistEntry{oldCursor, m.Vp.YOffset()})
					}
					cmds = append(cmds, m.openCurrent())
				} else {
					m.Focus = "nav"
					m.autoAdjustNavXOffset()
				}
			case "up", "down", "j", "k", "pgup", "pageup", "pgdn", "pgdown", "pagedown":
				var cmd tea.Cmd
				m.Vp, cmd = m.Vp.Update(msg)
				cmds = append(cmds, cmd)
			case "t":
				m.Focus = "theme"
			case "l":
				if len(m.Links) > 0 {
					if !m.LinkActive {
						m.clearContentTargetModes()
						m.LinkActive = true
						m.LinkIdx = 0
					} else {
						m.LinkIdx = (m.LinkIdx + 1) % len(m.Links)
					}
					m.highlightCurrentLink()
				}
			case "L":
				if m.LinkActive && len(m.Links) > 0 {
					m.LinkIdx = (m.LinkIdx - 1 + len(m.Links)) % len(m.Links)
					m.highlightCurrentLink()
				}
			case "c":
				if len(m.CodeBlocks) > 0 {
					if !m.CodeActive {
						m.clearContentTargetModes()
						m.CodeActive = true
						m.CopyIdx = 0
					} else {
						m.CopyIdx = (m.CopyIdx + 1) % len(m.CodeBlocks)
					}
					m.highlightCurrentCodeBlock()
				}
			case "C":
				if m.CodeActive && len(m.CodeBlocks) > 0 {
					m.CopyIdx = (m.CopyIdx - 1 + len(m.CodeBlocks)) % len(m.CodeBlocks)
					m.highlightCurrentCodeBlock()
				}
			case "\\":
				m.NavHidden = !m.NavHidden
				m.Vp.SetWidth(m.vpW())
				cmds = append(cmds, m.openCurrent())
			case "?":
				m.HelpOverlay = !m.HelpOverlay
				m.Vp.SetHeight(m.vpH())
			}
		}
	}
	return m, tea.Batch(cmds...)
}

func (m Model) overlayStart(overlay string) (int, int) {
	lm := m.layoutMetrics()
	fgH := lipgloss.Height(overlay)
	fgW := 0
	for _, l := range strings.Split(overlay, "\n") {
		if w := ui.VisWidth(l); w > fgW {
			fgW = w
		}
	}
	startX := max(0, (lm.safeW-fgW)/2)
	startY := max(0, (lm.bodyH-fgH)/2)
	return startX, startY
}

func (m Model) overlayItemAt(y int, overlay string, count int) int {
	_, startY := m.overlayStart(overlay)
	itemRow := y - (startY + 3)
	if itemRow >= 0 && itemRow < count {
		return itemRow
	}
	return -1
}

func (m Model) overlayBox(title, help string, items []string) string {
	th := m.currentTheme()
	titleStr := lipgloss.NewStyle().
		Foreground(lipgloss.Color(th.SectionHdrFg)).
		Bold(true).
		Render(title)
	footer := lipgloss.NewStyle().
		Foreground(lipgloss.Color(th.StatusFg)).
		Render(help)

	var content []string
	content = append(content, titleStr, "")
	content = append(content, items...)
	content = append(content, "", footer)

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(th.BorderActive)).
		Padding(0, 1).
		Render(lipgloss.JoinVertical(lipgloss.Left, content...))
}

func (m Model) renderThemePanel() string {
	th := m.currentTheme()
	ow := min(m.vpW()-4, 52)
	inner := ow - 2
	var items []string
	for i, t := range themes.Themes {
		swatch := lipgloss.NewStyle().
			Background(lipgloss.Color(t.BorderActive)).
			Foreground(lipgloss.Color(t.BorderActive)).
			Render("  ")
		if i == m.ThemeIdx {
			items = append(items, lipgloss.NewStyle().
				Background(lipgloss.Color(th.OverlaySelBg)).
				Foreground(lipgloss.Color(th.OverlaySelFg)).
				Bold(true).Width(inner).
				Render("▶ "+t.Name+" "+swatch))
		} else {
			items = append(items, lipgloss.NewStyle().
				Foreground(lipgloss.Color(th.OverlayItemFg)).Width(inner).
				Render("  "+t.Name+" "+swatch))
		}
	}
	return m.overlayBox("Select Theme", "↑↓/jk · enter apply · esc cancel", items)
}

func (m Model) renderImageOverlay() string {
	th := m.currentTheme()
	titleStr := lipgloss.NewStyle().
		Foreground(lipgloss.Color(th.SectionHdrFg)).
		Bold(true).
		Render("Image Preview")
	footer := lipgloss.NewStyle().
		Foreground(lipgloss.Color(th.StatusFg)).
		Render("esc/enter: close")
	content := lipgloss.JoinVertical(lipgloss.Left, titleStr, "", m.ImageOverlaySrc, "", footer)
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(th.BorderActive)).
		Padding(0, 1).
		Render(content)
}

func (m Model) imageOverlayInject() string {
	if !m.ImageOverlay {
		return ""
	}
	fg := m.renderImageOverlay()
	startX, startY := m.overlayStart(fg)
	termRow := startY + 4
	termCol := startX + 3
	switch {
	case m.ImageOverlayKitty != "":
		return fmt.Sprintf("\x1b7\x1b[%d;%dH%s\x1b8", termRow, termCol, m.ImageOverlayKitty)
	case m.ImageOverlaySixel != "":
		return fmt.Sprintf("\x1b7\x1b[%d;%dH%s\x1b8", termRow, termCol, m.ImageOverlaySixel)
	default:
		return ""
	}
}

func (m Model) renderHelpPanel() string {
	th := m.currentTheme()
	lm := m.layoutMetrics()
	keyStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(th.SectionHdrFg))
	descStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(th.StatusFg))

	type pair struct{ key, desc string }
	left := []pair{
		{"↑↓ / jk", "move / scroll"},
		{"enter / →", "open page"},
		{"/", "search   n/N  next/prev"},
		{"c / l / i", "code · link · image  (enter/y=copy)"},
		{"q / ctrl+c", "quit"},
	}
	right := []pair{
		{"tab / space", "switch focus"},
		{"\\", "toggle nav"},
		{"backspace", "go back"},
		{"t", "theme picker"},
		{"?", "close help"},
	}

	const keyW = 12
	colW := max(1, lm.safeW/2)
	buildEntry := func(k, d string) string {
		sp := strings.Repeat(" ", max(0, keyW-ui.VisWidth(k)))
		return "  " + keyStyle.Render(k) + sp + "  " + descStyle.Render(d)
	}
	lines := make([]string, 0, helpPanelHeight)
	lines = append(lines, "")
	for i := range left {
		leftCell := lipgloss.NewStyle().Width(colW).Render(buildEntry(left[i].key, left[i].desc))
		lines = append(lines, leftCell+buildEntry(right[i].key, right[i].desc))
	}
	lines = append(lines, "")
	return strings.Join(lines, "\n")
}

func (m Model) View() tea.View {
	if !m.Ready {
		v := tea.NewView("\nInitializing…\n")
		v.AltScreen = true
		v.MouseMode = tea.MouseModeCellMotion
		return v
	}

	th := m.currentTheme()
	lm := m.layoutMetrics()
	m.Vp.SetWidth(lm.vpW)
	m.Vp.SetHeight(lm.vpH)

	bc := th.BorderInactive
	if m.Focus == "content" || m.Focus == "search" {
		bc = th.BorderActive
	}
	contentStyle := lipgloss.NewStyle().Padding(0, 0)
	if !m.NavHidden {
		contentStyle = contentStyle.
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color(bc))
	}

	var layout string
	containerW := lm.contentOuterW

	if m.NavHidden {
		contentPanel := contentStyle.Width(containerW).Height(lm.vpH).Render(m.Vp.View())
		layout = contentPanel
	} else {
		navPanel := m.renderNav()
		contentPanel := contentStyle.Width(containerW).Height(lm.vpH).Render(m.Vp.View())
		layout = lipgloss.JoinHorizontal(lipgloss.Top, navPanel, contentPanel)
	}

	if m.Focus == "theme" {
		layout = ui.OverlayCenter(layout, m.renderThemePanel())
	} else if m.ImageOverlay {
		fg := m.renderImageOverlay()
		layout = ui.OverlayCenter(layout, fg)
	}

	hint := m.statusLine()
	status := lipgloss.NewStyle().
		Foreground(lipgloss.Color(th.StatusFg)).
		Background(lipgloss.Color(th.BorderInactive)).
		Width(lm.safeW).
		Render(hint)

	body := layout
	bodyLines := strings.Split(body, "\n")
	bodyMaxLines := max(1, m.Height-1-lm.helpPanelH)
	if len(bodyLines) > bodyMaxLines {
		bodyLines = bodyLines[:bodyMaxLines]
	}
	for len(bodyLines) < bodyMaxLines {
		bodyLines = append(bodyLines, "")
	}
	if lm.helpPanelH > 0 {
		bodyLines = append(bodyLines, strings.Split(m.renderHelpPanel(), "\n")...)
	}
	for len(bodyLines) < m.Height {
		bodyLines = append(bodyLines, "")
	}
	body = strings.Join(bodyLines, "\n")
	out := ui.OverlayAtLine(body, status, 0, m.Height-1)
	out = m.wrapTmux("\x1b[?2026h") + out + m.wrapTmux("\x1b[?2026l")
	v := tea.NewView(out)
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	return v
}

func (m Model) renderNav() string {
	th := m.currentTheme()
	lm := m.layoutMetrics()
	niw := lm.navInnerW
	var lines []string
	space := m.navEntrySpace()
	headerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(th.SectionHdrFg)).
		Bold(true).
		Width(niw)
	switch m.Focus {
	case "pagesearch":
		q := ui.Truncate("p: "+m.PsearchQuery+"█", niw)
		lines = append(lines, headerStyle.Render(q), "")

		if len(m.PsearchResults) == 0 {
			if m.PsearchQuery != "" {
				lines = append(lines, lipgloss.NewStyle().
					Foreground(lipgloss.Color(th.StatusFg)).
					Width(niw).
					Render("  no matches"))
			}
		} else {
			start := 0
			if len(m.PsearchResults) > space {
				start = m.PsearchCursor - space/2
				if start < 0 {
					start = 0
				}
				if start+space > len(m.PsearchResults) {
					start = max(0, len(m.PsearchResults)-space)
				}
			}
			end := min(len(m.PsearchResults), start+space)
			for i := start; i < end; i++ {
				navIdx := m.PsearchResults[i]
				label := m.Entries[navIdx].Title
				style := lipgloss.NewStyle().Width(niw)
				if i == m.PsearchCursor {
					style = style.Background(lipgloss.Color(th.NavSelBg)).
						Foreground(lipgloss.Color(th.NavSelFg)).
						Bold(true)
					lines = append(lines, style.Render(m.navRenderEntry("▶ ", label, niw)))
				} else {
					style = style.Foreground(lipgloss.Color(th.NavItemFg))
					lines = append(lines, style.Render(m.navRenderEntry("  ", label, niw)))
				}
			}
		}
	case "gsearch":
		q := ui.Truncate("/ "+m.GsearchQuery+"█", niw)
		lines = append(lines, headerStyle.Render(q), "")

		if len(m.GsearchResults) == 0 {
			if m.GsearchQuery != "" {
				lines = append(lines, lipgloss.NewStyle().
					Foreground(lipgloss.Color(th.StatusFg)).
					Width(niw).
					Render("  no matches"))
			}
		} else {
			start := 0
			if len(m.GsearchResults) > space {
				start = m.GsearchCursor - space/2
				if start < 0 {
					start = 0
				}
				if start+space > len(m.GsearchResults) {
					start = max(0, len(m.GsearchResults)-space)
				}
			}
			end := min(len(m.GsearchResults), start+space)
			for i := start; i < end; i++ {
				r := m.GsearchResults[i]
				title := m.Entries[r.NavIdx].Title
				prefix := fmt.Sprintf("[%d] ", r.HitCount)
				style := lipgloss.NewStyle().Width(niw)
				if i == m.GsearchCursor {
					style = style.Background(lipgloss.Color(th.NavSelBg)).
						Foreground(lipgloss.Color(th.NavSelFg)).
						Bold(true)
					lines = append(lines, style.Render(m.navRenderEntry("▶ "+prefix, title, niw)))
				} else {
					style = style.Foreground(lipgloss.Color(th.NavItemFg))
					lines = append(lines, style.Render(m.navRenderEntry("  "+prefix, title, niw)))
				}
			}
		}
	default:
		if config.DocsTitle != "" {
			title := ui.Truncate(config.DocsTitle, niw)
			centered := lipgloss.PlaceHorizontal(niw, lipgloss.Center, title)
			lines = append(lines, lipgloss.NewStyle().
				Foreground(lipgloss.Color(th.SectionHdrFg)).
				Bold(true).
				Render(centered), "")
		}

		visibleEntries := m.Entries[m.NavOffset:min(len(m.Entries), m.NavOffset+space)]
		for i, e := range visibleEntries {
			absIdx := m.NavOffset + i
			indent := strings.Repeat("  ", e.Depth)
			style := lipgloss.NewStyle().Width(niw)

			if e.FilePath == "" {
				title := m.navRenderEntry("", indent+strings.ToUpper(e.Title), niw)
				style = style.Foreground(lipgloss.Color(th.SectionHdrFg)).Bold(true)
				lines = append(lines, style.Render(title))
			} else {
				if absIdx == m.Cursor {
					style = style.Background(lipgloss.Color(th.NavSelBg)).
						Foreground(lipgloss.Color(th.NavSelFg)).
						Bold(true)
					lines = append(lines, style.Render(m.navRenderEntry("▶ ", indent+e.Title, niw)))
				} else {
					style = style.Foreground(lipgloss.Color(th.NavItemFg))
					lines = append(lines, style.Render(m.navRenderEntry("  ", indent+e.Title, niw)))
				}
			}
		}
	}
	for len(lines) < m.vpH() {
		lines = append(lines, "")
	}

	bc := th.BorderInactive
	if m.Focus == "nav" {
		bc = th.BorderActive
	}

	return lipgloss.NewStyle().
		Padding(0, 1).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(bc)).
		Height(m.vpH()).
		Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
}
