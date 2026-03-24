package graphics

import (
	"fmt"
	"io"
	"os"
	"strings"
	"syscall"
	"unsafe"

	"github.com/charmbracelet/ssh"
)

const (
	TIOCGWINSZ = 0x40087468
)

type Winsize struct {
	Row    uint16
	Col    uint16
	Xpixel uint16
	Ypixel uint16
}

type GraphicsMsg struct {
	Sixel bool
	Kitty bool
}

func GetCellPixelSize(sshSession ssh.Session) (int, int) {
	if sshSession != nil {
		if pty, _, ok := sshSession.Pty(); ok {
			var w, h int
			if pty.Window.Width > 0 && pty.Window.WidthPixels > 0 {
				w = pty.Window.WidthPixels / pty.Window.Width
			}
			if pty.Window.Height > 0 && pty.Window.HeightPixels > 0 {
				h = pty.Window.HeightPixels / pty.Window.Height
			}
			return w, h
		}
		return 0, 0
	}

	var ws Winsize
	_, _, err := syscall.Syscall(syscall.SYS_IOCTL, uintptr(os.Stdout.Fd()), uintptr(TIOCGWINSZ), uintptr(unsafe.Pointer(&ws)))
	if err == 0 {
		var w, h int
		if ws.Col > 0 && ws.Xpixel > 0 {
			w = int(ws.Xpixel) / int(ws.Col)
		}
		if ws.Row > 0 && ws.Ypixel > 0 {
			h = int(ws.Ypixel) / int(ws.Row)
		}
		return w, h
	}
	return 0, 0
}

func SendProbe(w io.Writer, isTmux bool) {
	probe := "\x1b[c\x1b_Gi=1,a=q\x1b\\"
	if isTmux {
		probe = "\x1bPtmux;\x1b" + strings.ReplaceAll(probe, "\x1b", "\x1b\x1b") + "\x1b\\"
	}
	_, _ = fmt.Fprint(w, probe)
}

func IsSixelTerminal(term string) bool {
	t := strings.ToLower(term)
	for _, k := range []string{"foot", "mlterm", "contour", "yaft"} {
		if strings.Contains(t, k) {
			return true
		}
	}
	return false
}

func IsKittyTerminal(term string) bool {
	t := strings.ToLower(term)
	for _, k := range []string{"kitty", "ghostty", "wezterm"} {
		if strings.Contains(t, k) {
			return true
		}
	}
	return false
}
