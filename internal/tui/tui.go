package tui

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"charm.land/log/v2"
	"github.com/charmbracelet/ssh"
	"github.com/charmbracelet/x/mosaic"
	sixel "github.com/mattn/go-sixel"
	xdraw "golang.org/x/image/draw"
	"golang.org/x/term"

	"codeberg.org/stelzo/dock/internal/config"
	"codeberg.org/stelzo/dock/internal/graphics"
	"codeberg.org/stelzo/dock/internal/images"
	"codeberg.org/stelzo/dock/internal/markdown"
	"codeberg.org/stelzo/dock/internal/navigation"
	"codeberg.org/stelzo/dock/internal/search"
	"codeberg.org/stelzo/dock/internal/themes"
	"codeberg.org/stelzo/dock/internal/ui"
)

type GraphicsReader struct {
	R io.Reader
	P *tea.Program
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

	clean := string(p[:n])
	msg := graphics.GraphicsMsg{}
	found := false

	for {
		start := strings.Index(clean, "\x1b[?")
		if start == -1 {
			start = strings.Index(clean, "\x1b[")
		}
		if start == -1 {
			break
		}
		end := strings.IndexByte(clean[start:], 'c')
		if end == -1 {
			break
		}
		res := clean[start : start+end+1]
		if strings.Contains(res, ";4") || strings.Contains(res, "?4") {
			msg.Sixel = true
		}
		clean = clean[:start] + clean[start+end+1:]
		found = true
	}

	for {
		start := strings.Index(clean, "\x1b_G")
		if start == -1 {
			break
		}
		end := strings.Index(clean[start:], "\x1b\\")
		if end == -1 {
			break
		}
		res := clean[start : start+end+2]
		if strings.Contains(res, "OK") {
			msg.Kitty = true
		}
		clean = clean[:start] + clean[start+end+2:]
		found = true
	}

	if found {
		g.P.Send(msg)
		newN := copy(p, []byte(clean))
		return newN, err
	}
	return n, err
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
			m.KittySupported = graphics.IsKittyTerminal(termEnv)
		}
	}

	m.UpdateCellPix()

	gr := &GraphicsReader{R: os.Stdin}
	p := tea.NewProgram(m, tea.WithInput(gr))
	gr.P = p

	if _, err := p.Run(); err != nil {
		log.Fatal("TUI error", "err", err)
	}
}

const (
	preferredNavInnerW = 26
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
	contentInnerY int
}

type Model struct {
	Docs      string
	Entries   []navigation.NavEntry
	Cursor    int
	NavOffset int
	NavHidden bool
	Vp        viewport.Model
	Width     int
	Height    int
	Ready     bool
	Focus     string
	ThemeIdx  int

	CodeBlocks  []markdown.CodeBlock
	CopyIdx     int
	Links       []markdown.DocLink
	LinkIdx     int
	PendingCopy string
	StatusMsg   string

	RawContent     string
	SearchQuery    string
	SearchActive   bool
	SearchMatches  []int
	SearchMatchIdx int

	GsearchQuery   string
	GsearchResults []search.GlobalResult
	GsearchCursor  int

	PsearchQuery   string
	PsearchResults []int // navIdx of matching entries
	PsearchCursor  int
	DocSeq         uint64

	ImageRefs          []images.ImageRef
	ImageIdx           int
	CodeActive         bool
	LinkActive         bool
	ImageActive        bool
	CodeOverlay        bool
	CodeOverlaySrc     string
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
}

func NewModel(docs string, w, h int) Model {
	entries := navigation.BuildNav(docs)
	m := Model{
		Docs:     docs,
		Entries:  entries,
		Width:    w,
		Height:   h,
		Focus:    "nav",
		Ready:    w > 0 && h > 0,
		ThemeIdx: config.DefaultThemeIdx,
	}
	for i, e := range entries {
		if e.FilePath != "" {
			m.Cursor = i
			break
		}
	}
	if m.Ready {
		m.Vp = viewport.New(viewport.WithWidth(m.vpW()), viewport.WithHeight(m.vpH()))
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

func (m Model) vpW() int {
	return m.layoutMetrics().vpW
}

func (m Model) getHint() string {
	if m.StatusMsg != "" {
		return " " + m.StatusMsg
	}
	if m.Focus == "search" {
		h := " /" + m.SearchQuery + "█"
		if len(m.SearchMatches) > 0 {
			h += fmt.Sprintf("  [%d/%d]", m.SearchMatchIdx+1, len(m.SearchMatches))
		} else if m.SearchQuery != "" {
			h += "  [no matches]"
		} else {
			h += "  type to search · esc cancel"
		}
		return h
	}
	if m.Focus == "pagesearch" {
		return " ↑↓/jk: select · enter: jump · esc: cancel"
	}
	if m.Focus == "gsearch" {
		return " ↑↓/jk: select · enter: open · esc: cancel"
	}
	if m.ImageOverlay {
		return fmt.Sprintf(" [%d/%d] %s · enter/esc: close",
			m.ImageIdx+1, len(m.ImageRefs), m.ImageRefs[m.ImageIdx].Alt)
	}
	if m.CodeOverlay {
		return " [code] esc/enter: close"
	}
	if m.CodeActive {
		return fmt.Sprintf(" [code %d/%d] c/C: next/prev · enter/y: copy · esc: exit",
			m.CopyIdx+1, len(m.CodeBlocks))
	}
	if m.LinkActive {
		return fmt.Sprintf(" [link %d/%d] l/L: next/prev · enter: follow/copy · esc: exit",
			m.LinkIdx+1, len(m.Links))
	}
	if m.ImageActive {
		return fmt.Sprintf(" [image %d/%d] i: next · I: prev · enter: show · esc: exit image nav",
			m.ImageIdx+1, len(m.ImageRefs))
	}
	if m.SearchActive {
		return fmt.Sprintf(" /%s  [%d/%d] · n: next · N: prev · esc: clear",
			m.SearchQuery, m.SearchMatchIdx+1, len(m.SearchMatches))
	}

	var parts []string
	switch m.Focus {
	case "nav":
		parts = []string{
			"↑↓/jk\u00A0navigate",
			"p:\u00A0page\u00A0search",
			"/:\u00A0global\u00A0search",
			"tab:\u00A0content",
			"space:\u00A0nav",
			"\\:\u00A0hide\u00A0nav",
			"t:\u00A0theme",
			"q\u00A0quit",
		}
	case "theme":
		parts = []string{"↑↓/jk", "enter\u00A0apply", "esc\u00A0cancel"}
	default:
		parts = append(parts, "↑↓/jk/pgup/pgdn\u00A0scroll")
		if m.NavHidden {
			parts = append(parts, "\\:\u00A0show\u00A0nav")
		} else {
			parts = append(parts, "tab:\u00A0nav")
		}
		parts = append(parts, "space:\u00A0nav", "/:\u00A0search", "t:\u00A0theme", "q\u00A0quit")
		if n := len(m.CodeBlocks); n > 0 {
			parts = append(parts, "c:\u00A0copy\u00A0code")
		}
		if n := len(m.Links); n > 0 {
			parts = append(parts, "l:\u00A0links")
		}
		if n := len(m.ImageRefs); n > 0 {
			parts = append(parts, "i:\u00A0images")
		}
	}
	return " " + ui.JoinHints(max(1, m.Width-2), parts)
}

func (m Model) statusLine() string {
	h := strings.ReplaceAll(m.getHint(), "\n", " · ")
	h = strings.ReplaceAll(h, "\r", "")
	return ui.Truncate(h, max(1, m.Width-1))
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
	safeW := max(1, m.Width-1)
	statusH := 1
	vpH := m.Height - 2 - statusH
	if vpH < 1 {
		vpH = 1
	}
	navOuterW := 0
	navInnerW := 0
	if !m.NavHidden && m.Width > 40 {
		navInnerW = preferredNavInnerW
		navOuterW = navInnerW + 4
		if safeW-navOuterW < 20 {
			// shrink nav to give content at least 20 columns
			navInnerW = max(10, safeW-20-4)
			navOuterW = navInnerW + 4
		}
	}

	contentOuterX := navOuterW
	contentOuterW := max(1, safeW-navOuterW)
	contentInnerX := contentOuterX + 1 // border only
	contentInnerY := 1                 // top border occupies row 0
	if m.NavHidden {
		vpH = m.Height - statusH
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
		navInnerY:     1, // top border occupies row 0
		contentOuterX: contentOuterX,
		contentOuterW: contentOuterW,
		contentInnerX: contentInnerX,
		contentInnerY: contentInnerY,
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

type clearCopyMsg struct{}
type clearStatusMsg struct{}
type clearGraphicsMsg struct{}
type copyResultMsg struct {
	ok bool
}
type imageOverlayMsg struct {
	rendered string
	sixel    string
	kitty    string
	charW    int
	charH    int
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

			placeholder := strings.Repeat(strings.Repeat(" ", charW)+"\n", charH)
			placeholder = strings.TrimRight(placeholder, "\n")

			if useKitty {
				var buf bytes.Buffer
				if encErr := png.Encode(&buf, dst); encErr == nil {
					b64 := base64.StdEncoding.EncodeToString(buf.Bytes())
					return imageOverlayMsg{
						rendered: placeholder,
						kitty:    "\x1b_Gf=100,a=T,t=d,i=1;" + b64 + "\x1b\\",
						charW:    charW,
						charH:    charH,
					}
				}
			}

			if useSixel {
				var buf bytes.Buffer
				if encErr := sixel.NewEncoder(&buf).Encode(dst); encErr == nil {
					return imageOverlayMsg{
						rendered: placeholder,
						sixel:    buf.String(),
						charW:    charW,
						charH:    charH,
					}
				}
			}
		}
		mo := mosaic.New().Width(charW).Height(charH)
		return imageOverlayMsg{rendered: mo.Render(img), charW: charW, charH: charH}
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

func (m *Model) openCurrent() tea.Cmd {
	if m.Cursor < 0 || m.Cursor >= len(m.Entries) {
		return nil
	}
	e := m.Entries[m.Cursor]
	if e.FilePath == "" {
		return nil
	}
	m.DocSeq++
	// Render markdown to the same width as the viewport's text area.
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

func clearCopyCmd() tea.Cmd {
	return func() tea.Msg { return clearCopyMsg{} }
}

func copyToClipboardCmd(text string, useOSC52 bool) tea.Cmd {
	return func() tea.Msg {
		if useOSC52 {
			return copyResultMsg{ok: true}
		}
		if !useOSC52 && runtime.GOOS == "darwin" {
			cmd := exec.Command("pbcopy")
			cmd.Stdin = strings.NewReader(text)
			if err := cmd.Run(); err == nil {
				return copyResultMsg{ok: true}
			}
		}
		return copyResultMsg{ok: false}
	}
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
	m.CodeOverlay = false
	m.CodeOverlaySrc = ""
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

func (m Model) Init() tea.Cmd {
	return tea.Batch(
		m.openCurrent(),
		func() tea.Msg {
			var w io.Writer = os.Stdout
			if m.SshSession != nil {
				w = m.SshSession
			}
			graphics.SendProbe(w, m.IsTmux)
			return nil
		},
	)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.Width, m.Height = msg.Width, msg.Height
		if !m.Ready {
			m.Vp = viewport.New(viewport.WithWidth(m.vpW()), viewport.WithHeight(m.vpH()))
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

	case docMsg:
		if msg.Seq != m.DocSeq {
			return m, nil
		}
		m.RawContent = msg.content
		m.Vp.SetContent(m.RawContent)
		m.Vp.GotoTop()
		m.CodeBlocks = msg.codeBlocks
		m.CopyIdx = 0
		m.Links = msg.links
		m.LinkIdx = 0
		m.ImageRefs = msg.imageRefs
		m.ImageIdx = 0
		m.CodeActive = false
		m.LinkActive = false
		m.ImageActive = false
		m.CodeOverlay = false
		m.CodeOverlaySrc = ""
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

	case search.GsearchMsg:
		if msg.Query == m.GsearchQuery {
			m.GsearchResults = msg.Results
			m.GsearchCursor = 0
		}

	case clearCopyMsg:
		m.PendingCopy = ""

	case clearStatusMsg:
		m.StatusMsg = ""

	case copyResultMsg:
		if !msg.ok && m.PendingCopy == "" {
			m.StatusMsg = "Clipboard copy unavailable"
			cmds = append(cmds, statusCmd(2*time.Second))
		}

	case clearGraphicsMsg:
		m.NeedsGraphicsClear = false
		return m, nil

	case imageOverlayMsg:
		m.ImageOverlaySrc = msg.rendered
		m.ImageOverlaySixel = msg.sixel
		m.ImageOverlayKitty = msg.kitty
		m.ImageOverlayW = msg.charW
		m.ImageOverlayH = msg.charH
		m.ImageOverlay = true

	case tea.MouseMsg:
		mouse := msg.Mouse()
		lm := m.layoutMetrics()
		inNav := !m.NavHidden && mouse.X >= 0 && mouse.X < lm.navOuterW
		inContent := mouse.X >= lm.contentOuterX && mouse.X < lm.contentOuterX+lm.contentOuterW
		inRows := mouse.Y >= lm.contentInnerY && mouse.Y < lm.contentInnerY+lm.vpH

		followLink := func(lk markdown.DocLink) {
			m.Focus = "content"
			if lk.IsInternal && lk.NavIdx >= 0 {
				m.Cursor = lk.NavIdx
				m = m.ensureCursorVisible(m.navEntrySpace())
				cmds = append(cmds, m.openCurrent())
			} else if lk.IsInternal {
				m.StatusMsg = "Page not found in nav"
				cmds = append(cmds, statusCmd(2*time.Second))
			} else {
				m.PendingCopy = lk.URL
				m.StatusMsg = "✓ URL copied: " + ui.Truncate(lk.URL, 50)
				cmds = append(cmds, clearCopyCmd(), statusCmd(4*time.Second))
			}
		}

		switch ev := msg.(type) {
		case tea.MouseWheelMsg:
			switch mouse.Button {
			case tea.MouseWheelUp:
				switch m.Focus {
				case "theme":
					if m.ThemeIdx > 0 {
						m.ThemeIdx--
						cmds = append(cmds, m.openCurrent())
					}
					return m, nil
				default:
					if inNav {
						oldCursor := m.Cursor
						m = m.moveCursor(-1)
						if m.Cursor != oldCursor && m.Cursor >= 0 && m.Cursor < len(m.Entries) && m.Entries[m.Cursor].FilePath != "" {
							cmds = append(cmds, m.openCurrent())
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
					if m.ThemeIdx < len(themes.Themes)-1 {
						m.ThemeIdx++
						cmds = append(cmds, m.openCurrent())
					}
					return m, nil
				default:
					if inNav {
						oldCursor := m.Cursor
						m = m.moveCursor(1)
						if m.Cursor != oldCursor && m.Cursor >= 0 && m.Cursor < len(m.Entries) && m.Entries[m.Cursor].FilePath != "" {
							cmds = append(cmds, m.openCurrent())
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
					m.ImageOverlay = false
					m.ImageOverlaySrc = ""
					m.ImageOverlaySixel = ""
					m.ImageOverlayKitty = ""
					m.NeedsGraphicsClear = true
					cmds = append(cmds, clearGraphicsCmd())
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
				if inNav && inRows {
					titleOffset := 0
					if config.DocsTitle != "" {
						titleOffset = 2
					}
					visIdx := (mouse.Y - lm.navInnerY) - titleOffset
					navIdx := m.NavOffset + visIdx
					if visIdx >= 0 && navIdx >= 0 && navIdx < len(m.Entries) {
						m.Cursor = navIdx
						if m.Entries[navIdx].FilePath != "" {
							m.Focus = "content"
							cmds = append(cmds, m.openCurrent())
						} else {
							m.Focus = "nav"
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
				if m.Cursor != oldCursor && m.Cursor >= 0 && m.Cursor < len(m.Entries) && m.Entries[m.Cursor].FilePath != "" {
					cmds = append(cmds, m.openCurrent())
				}
			case "down", "j":
				oldCursor := m.Cursor
				m = m.moveCursor(1)
				if m.Cursor != oldCursor && m.Cursor >= 0 && m.Cursor < len(m.Entries) && m.Entries[m.Cursor].FilePath != "" {
					cmds = append(cmds, m.openCurrent())
				}
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
				if m.CodeOverlay {
					m.CodeOverlay = false
					m.CodeOverlaySrc = ""
				} else if m.ImageOverlay {
					m.ImageOverlay = false
					m.ImageOverlaySrc = ""
					m.ImageOverlaySixel = ""
					m.ImageOverlayKitty = ""
					m.NeedsGraphicsClear = true
					cmds = append(cmds, clearGraphicsCmd())
				} else if m.CodeActive && len(m.CodeBlocks) > 0 {
					if m.SshSession != nil {
						m.CodeOverlay = true
						m.CodeOverlaySrc = m.CodeBlocks[m.CopyIdx].Content
					} else {
						m.StatusMsg = "✓ Copied code block to clipboard"
						cmds = append(cmds, copyToClipboardCmd(m.CodeBlocks[m.CopyIdx].Content, false), clearCopyCmd(), statusCmd(2*time.Second))
					}
				} else if m.LinkActive && len(m.Links) > 0 {
					lk := m.Links[m.LinkIdx]
					if lk.IsInternal && lk.NavIdx >= 0 {
						m.clearContentTargetModes()
						m.Cursor = lk.NavIdx
						m = m.ensureCursorVisible(m.navEntrySpace())
						cmds = append(cmds, m.openCurrent())
					} else if lk.IsInternal {
						m.StatusMsg = "Page not found in nav"
						cmds = append(cmds, statusCmd(2*time.Second))
					} else {
						if m.SshSession != nil {
							m.PendingCopy = lk.URL
						}
						m.StatusMsg = "✓ URL copied: " + ui.Truncate(lk.URL, 50)
						cmds = append(cmds, copyToClipboardCmd(lk.URL, m.SshSession != nil), clearCopyCmd(), statusCmd(4*time.Second))
					}
				} else if m.ImageActive {
					cmds = append(cmds, m.loadImageOverlay())
				} else {
					var cmd tea.Cmd
					m.Vp, cmd = m.Vp.Update(msg)
					cmds = append(cmds, cmd)
				}
			case "y":
				if m.CodeActive && len(m.CodeBlocks) > 0 {
					if m.SshSession != nil {
						m.PendingCopy = m.CodeBlocks[m.CopyIdx].Content
					}
					m.StatusMsg = "✓ Copied code block to clipboard"
					cmds = append(cmds, copyToClipboardCmd(m.CodeBlocks[m.CopyIdx].Content, m.SshSession != nil), clearCopyCmd(), statusCmd(2*time.Second))
				}
			case "esc":
				if m.CodeOverlay {
					m.CodeOverlay = false
					m.CodeOverlaySrc = ""
				} else if m.ImageOverlay {
					m.ImageOverlay = false
					m.ImageOverlaySrc = ""
					m.ImageOverlaySixel = ""
					m.ImageOverlayKitty = ""
					m.NeedsGraphicsClear = true
					cmds = append(cmds, clearGraphicsCmd())
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
			case "tab":
				if m.NavHidden {
					m = m.moveCursor(1)
					cmds = append(cmds, m.openCurrent())
				} else {
					m.Focus = "nav"
				}
			case "shift+tab":
				if m.NavHidden {
					m = m.moveCursor(-1)
					cmds = append(cmds, m.openCurrent())
				} else {
					m.Focus = "nav"
				}
			case "up", "down", "j", "k":
				var cmd tea.Cmd
				m.Vp, cmd = m.Vp.Update(msg)
				cmds = append(cmds, cmd)
			case "pgup", "pageup":
				var cmd tea.Cmd
				m.Vp, cmd = m.Vp.Update(msg)
				cmds = append(cmds, cmd)
			case "pgdn", "pgdown", "pagedown":
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

func (m Model) renderCodeOverlay() string {
	lm := m.layoutMetrics()
	return lipgloss.NewStyle().
		Width(lm.safeW).
		Height(m.Height).
		Render(m.CodeOverlaySrc)
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
	// Keep viewport dimensions in sync before rendering its content.
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
	var imageInject string

	if m.CodeOverlay {
		layout = m.renderCodeOverlay()
		bodyLines := strings.Split(layout, "\n")
		bodyMaxLines := max(1, m.Height-1)
		if len(bodyLines) > bodyMaxLines {
			bodyLines = bodyLines[:bodyMaxLines]
		}
		for len(bodyLines) < bodyMaxLines {
			bodyLines = append(bodyLines, "")
		}
		body := strings.Join(bodyLines, "\n")
		status := lipgloss.NewStyle().
			Foreground(lipgloss.Color(th.StatusFg)).
			Background(lipgloss.Color(th.BorderInactive)).
			Width(lm.safeW).
			MaxWidth(lm.safeW).
			Render(m.statusLine())
		out := body + "\n" + status
		out = m.wrapTmux("\x1b[?2026h") + out + m.wrapTmux("\x1b[?2026l")
		v := tea.NewView(out)
		v.AltScreen = true
		v.MouseMode = tea.MouseModeCellMotion
		return v
	}

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
		startX, startY := m.overlayStart(fg)
		layout = ui.OverlayCenter(layout, fg)
		termRow := startY + 3 + 1
		termCol := startX + 2 + 1
		if m.ImageOverlayKitty != "" {
			imageInject = fmt.Sprintf("\x1b7\x1b[%d;%dH%s\x1b8", termRow, termCol, m.ImageOverlayKitty)
		} else if m.ImageOverlaySixel != "" {
			imageInject = fmt.Sprintf("\x1b7\x1b[%d;%dH%s\x1b8", termRow, termCol, m.ImageOverlaySixel)
		}
	} else if m.NeedsGraphicsClear {
		imageInject = "\x1b_Ga=d,d=A\x1b\\\x1b[J"
	}

	hint := m.statusLine()
	status := lipgloss.NewStyle().
		Foreground(lipgloss.Color(th.StatusFg)).
		Background(lipgloss.Color(th.BorderInactive)).
		Width(lm.safeW).
		Render(hint)

	body := layout
	bodyLines := strings.Split(body, "\n")
	bodyMaxLines := max(1, m.Height-1)
	if len(bodyLines) > bodyMaxLines {
		bodyLines = bodyLines[:bodyMaxLines]
	}
	for len(bodyLines) < m.Height {
		bodyLines = append(bodyLines, "")
	}
	body = strings.Join(bodyLines, "\n")
	out := ui.OverlayAtLine(body, status, 0, m.Height-1)
	if imageInject != "" {
		out += m.wrapTmux(imageInject)
	}

	if m.PendingCopy != "" {
		b64 := base64.StdEncoding.EncodeToString([]byte(m.PendingCopy))
		out = m.wrapTmux("\x1b]52;c;"+b64+"\x07") + out
	}

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
				e := m.Entries[navIdx]
				indent := strings.Repeat("  ", e.Depth)
				style := lipgloss.NewStyle().Width(niw)
				if i == m.PsearchCursor {
					style = style.Background(lipgloss.Color(th.NavSelBg)).
						Foreground(lipgloss.Color(th.NavSelFg)).
						Bold(true)
					lines = append(lines, style.Render("▶ "+indent+ui.Truncate(e.Title, niw-len(indent)-2)))
				} else {
					style = style.Foreground(lipgloss.Color(th.NavItemFg))
					lines = append(lines, style.Render("  "+indent+ui.Truncate(e.Title, niw-len(indent)-2)))
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
					lines = append(lines, style.Render("▶ "+prefix+ui.Truncate(title, niw-len(prefix)-2)))
				} else {
					style = style.Foreground(lipgloss.Color(th.NavItemFg))
					lines = append(lines, style.Render("  "+prefix+ui.Truncate(title, niw-len(prefix)-2)))
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
				title := ui.Truncate(strings.ToUpper(e.Title), niw-len(indent))
				style = style.Foreground(lipgloss.Color(th.SectionHdrFg)).Bold(true)
				lines = append(lines, style.Render(indent+title))
			} else {
				title := ui.Truncate(e.Title, niw-len(indent)-2)
				if absIdx == m.Cursor {
					style = style.Background(lipgloss.Color(th.NavSelBg)).
						Foreground(lipgloss.Color(th.NavSelFg)).
						Bold(true)
					lines = append(lines, style.Render("▶ "+indent+title))
				} else {
					style = style.Foreground(lipgloss.Color(th.NavItemFg))
					lines = append(lines, style.Render("  "+indent+title))
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
