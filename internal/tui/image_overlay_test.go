package tui

import (
	"strings"
	"testing"
)

func TestImageOverlayInjectKitty(t *testing.T) {
	m := NewModel("", 80, 24)
	m.ImageOverlay = true
	m.ImageOverlaySrc = "mosaic"
	m.ImageOverlayKitty = "\x1b_Gkitty-payload\x1b\\"

	got := m.imageOverlayInject()
	if got == "" {
		t.Fatal("imageOverlayInject() returned empty string for kitty payload")
	}
	if !strings.Contains(got, "\x1b7\x1b[") {
		t.Fatalf("imageOverlayInject() = %q, want cursor positioning prefix", got)
	}
	if !strings.Contains(got, m.ImageOverlayKitty) {
		t.Fatalf("imageOverlayInject() = %q, want kitty payload", got)
	}
	if !strings.HasSuffix(got, "\x1b8") {
		t.Fatalf("imageOverlayInject() = %q, want cursor restore suffix", got)
	}
}

func TestImageOverlayInjectSixel(t *testing.T) {
	m := NewModel("", 80, 24)
	m.ImageOverlay = true
	m.ImageOverlaySrc = "mosaic"
	m.ImageOverlaySixel = "\x1bPqsixel\x1b\\"

	got := m.imageOverlayInject()
	if got == "" {
		t.Fatal("imageOverlayInject() returned empty string for sixel payload")
	}
	if !strings.Contains(got, "\x1b7\x1b[") {
		t.Fatalf("imageOverlayInject() = %q, want cursor positioning prefix", got)
	}
	if !strings.Contains(got, m.ImageOverlaySixel) {
		t.Fatalf("imageOverlayInject() = %q, want sixel payload", got)
	}
	if !strings.HasSuffix(got, "\x1b8") {
		t.Fatalf("imageOverlayInject() = %q, want cursor restore suffix", got)
	}
}
