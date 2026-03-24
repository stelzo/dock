package ssh

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/log/v2"
	"charm.land/wish/v2"
	bm "charm.land/wish/v2/bubbletea"
	wlogging "charm.land/wish/v2/logging"
	"github.com/charmbracelet/ssh"
	"golang.org/x/term"

	"codeberg.org/stelzo/dock/internal/config"
	"codeberg.org/stelzo/dock/internal/graphics"
	"codeberg.org/stelzo/dock/internal/markdown"
	"codeberg.org/stelzo/dock/internal/navigation"
	"codeberg.org/stelzo/dock/internal/search"
	"codeberg.org/stelzo/dock/internal/themes"
	"codeberg.org/stelzo/dock/internal/tui"
)

type themeCtxKey struct{}

func themeIdxForSession(s ssh.Session) int {
	if v := s.Context().Value(themeCtxKey{}); v != nil {
		return v.(int)
	}
	return config.DefaultThemeIdx
}

func CmdList(stdout, _ io.Writer, docs string) int {
	entries := navigation.BuildNav(docs)
	for _, e := range entries {
		if e.FilePath == "" {
			_, _ = fmt.Fprintf(stdout, "%s%s\n", strings.Repeat("  ", e.Depth), strings.ToUpper(e.Title))
		} else {
			rel, _ := filepath.Rel(docs, e.FilePath)
			_, _ = fmt.Fprintf(stdout, "%s%s\t%s\n", strings.Repeat("  ", e.Depth), e.Title, rel)
		}
	}
	return 0
}

func CmdGet(stdout, stderr io.Writer, docs string, args []string, width ...int) int {
	fs := flag.NewFlagSet("get", flag.ContinueOnError)
	fs.SetOutput(stderr)
	raw := fs.Bool("raw", false, "output raw markdown without styling")
	flagArgs, positional := splitInterspersed(fs, args)
	if err := fs.Parse(flagArgs); err != nil {
		return 1
	}
	if len(positional) == 0 {
		_, _ = fmt.Fprintf(stderr, "usage: get [--raw] <title-or-relative-path>\n")
		return 1
	}
	query := positional[0]
	entries := navigation.BuildNav(docs)

	ql := strings.ToLower(query)
	var match *navigation.NavEntry
	for i := range entries {
		e := &entries[i]
		if e.FilePath == "" {
			continue
		}
		rel, _ := filepath.Rel(docs, e.FilePath)
		if rel == query || e.FilePath == query {
			match = e
			break
		}
		if match == nil && strings.Contains(strings.ToLower(e.Title), ql) {
			match = e
		}
	}
	if match == nil {
		_, _ = fmt.Fprintf(stderr, "no doc found matching %q\n", query)
		return 1
	}
	content, err := os.ReadFile(match.FilePath)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "error reading %s: %v\n", match.FilePath, err)
		return 1
	}
	if *raw {
		_, _ = fmt.Fprint(stdout, string(content))
		return 0
	}
	w := 80
	if len(width) > 0 && width[0] > 0 {
		w = width[0]
	} else if tw, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && tw > 0 {
		w = tw
	}
	_, _ = fmt.Fprint(stdout, markdown.RenderMarkdown(markdown.PreprocessMarkdown(string(content)), themes.Themes[config.DefaultThemeIdx], w))
	return 0
}

func CmdSearch(stdout, stderr io.Writer, docs string, args []string) int {
	if len(args) == 0 {
		_, _ = fmt.Fprintf(stderr, "usage: search <query>\n")
		return 1
	}
	query := strings.Join(args, " ")
	entries := navigation.BuildNav(docs)
	words := search.QueryWords(query)

	type hit struct {
		entry    navigation.NavEntry
		hitCount int
	}
	var hits []hit
	for _, e := range entries {
		if e.FilePath == "" {
			continue
		}
		raw, err := os.ReadFile(e.FilePath)
		if err != nil {
			continue
		}
		count := 0
		for _, line := range strings.Split(string(raw), "\n") {
			if search.LineMatchesAll(line, words) {
				count++
			}
		}
		if count > 0 {
			hits = append(hits, hit{e, count})
		}
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].hitCount > hits[j].hitCount })
	if len(hits) == 0 {
		_, _ = fmt.Fprintln(stderr, "no matches")
		return 1
	}
	for _, h := range hits {
		rel, _ := filepath.Rel(docs, h.entry.FilePath)
		_, _ = fmt.Fprintf(stdout, "[%d] %s\t%s\n", h.hitCount, h.entry.Title, rel)
	}
	return 0
}

func CliMiddleware(docs string) wish.Middleware {
	return func(next ssh.Handler) ssh.Handler {
		return func(s ssh.Session) {
			if i, ok := themes.IdxFromName(s.User()); ok {
				s.Context().SetValue(themeCtxKey{}, i)
			}

			pty, _, active := s.Pty()
			if active {
				sixel := graphics.IsSixelTerminal(pty.Term)
				kitty := graphics.IsKittyTerminal(pty.Term)
				s.Context().SetValue(graphicsCtxKey{}, graphicsInfo{
					sixel: sixel,
					kitty: kitty,
				})
			}

			cmd := s.Command()
			if len(cmd) == 0 {
				next(s)
				return
			}
			var code int
			w := pty.Window.Width
			if w == 0 {
				w = 80
			}
			switch cmd[0] {
			case "list":
				code = CmdList(s, s.Stderr(), docs)
			case "get":
				code = CmdGet(s, s.Stderr(), docs, cmd[1:], w)
			case "search":
				code = CmdSearch(s, s.Stderr(), docs, cmd[1:])
			default:
				_, _ = fmt.Fprintf(s.Stderr(), "unknown command %q — available: list, get [--raw] <title>, search <query>\n", cmd[0])
				code = 1
			}
			s.Exit(code) //nolint:errcheck
		}
	}
}

type graphicsCtxKey struct{}

type graphicsInfo struct {
	sixel bool
	kitty bool
}

func SessionMiddleware() wish.Middleware {
	return func(next ssh.Handler) ssh.Handler {
		return func(s ssh.Session) {
			start := time.Now()
			pty, _, _ := s.Pty()
			next(s)
			duration := time.Since(start)

			info := graphicsInfo{}
			if v := s.Context().Value(graphicsCtxKey{}); v != nil {
				info = v.(graphicsInfo)
			}

			imgAPI := "none"
			if info.kitty {
				imgAPI = "kitty"
			} else if info.sixel {
				imgAPI = "sixel"
			}

			log.Info("session ended",
				"user", s.User(),
				"addr", s.RemoteAddr().String(),
				"duration", duration.String(),
				"term", pty.Term,
				"image_api", imgAPI,
			)
		}
	}
}

func RunServe(docs string, port string) {
	s, err := wish.NewServer(
		wish.WithAddress(net.JoinHostPort("0.0.0.0", port)),
		wish.WithHostKeyPath(".ssh/id_ed25519"),
		wish.WithMiddleware(
			SessionMiddleware(),
			bm.Middleware(func(s ssh.Session) (tea.Model, []tea.ProgramOption) {
				pty, _, active := s.Pty()
				if !active {
					wish.Fatalln(s, "no active terminal, use: ssh host list|get|search")
					return nil, nil
				}
				w, h := pty.Window.Width, pty.Window.Height
				if w == 0 {
					w = 80
				}
				if h == 0 {
					h = 24
				}
				m := tui.NewModel(docs, w, h)
				m.SshSession = s
				m.ThemeIdx = themeIdxForSession(s)
				m.IsTmux = strings.HasPrefix(pty.Term, "tmux")
				m.IsScreen = strings.HasPrefix(pty.Term, "screen")

				if !m.SixelSupported {
					m.SixelSupported = graphics.IsSixelTerminal(pty.Term)
				}
				if !m.KittySupported {
					m.KittySupported = graphics.IsKittyTerminal(pty.Term)
				}
				m.UpdateCellPix()

				s.Context().SetValue(graphicsCtxKey{}, graphicsInfo{
					sixel: m.SixelSupported,
					kitty: m.KittySupported,
				})

				gr := &tui.GraphicsReader{R: s}
				return m,
					[]tea.ProgramOption{
						tea.WithInput(gr),
						func(p *tea.Program) {
							gr.P = p
						},
					}
			}),
			wlogging.Middleware(),
			CliMiddleware(docs),
		),
	)
	if err != nil {
		log.Fatal("could not create server", "err", err)
	}

	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)

	log.Info("SSH docs server listening", "addr", s.Addr)
	go func() {
		if err := s.ListenAndServe(); err != nil && !errors.Is(err, ssh.ErrServerClosed) {
			log.Error("server error", "err", err)
			done <- nil
		}
	}()

	<-done
	log.Info("Shutting down…")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := s.Shutdown(ctx); err != nil && !errors.Is(err, ssh.ErrServerClosed) {
		log.Error("shutdown error", "err", err)
	}
}

func splitInterspersed(fs *flag.FlagSet, args []string) (flagArgs, positional []string) {
	i := 0
	for i < len(args) {
		arg := args[i]
		if arg == "--" {
			positional = append(positional, args[i+1:]...)
			break
		}
		if !strings.HasPrefix(arg, "-") {
			positional = append(positional, arg)
			i++
			continue
		}
		name := strings.TrimLeft(arg, "-")
		if idx := strings.IndexByte(name, '='); idx != -1 {
			flagArgs = append(flagArgs, arg)
			i++
			continue
		}
		flagArgs = append(flagArgs, arg)
		i++
		f := fs.Lookup(name)
		if f == nil {
			continue
		}
		if bf, ok := f.Value.(interface{ IsBoolFlag() bool }); ok && bf.IsBoolFlag() {
			continue
		}
		if i < len(args) {
			flagArgs = append(flagArgs, args[i])
			i++
		}
	}
	return
}
