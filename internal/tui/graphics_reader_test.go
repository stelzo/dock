package tui

import (
	"image"
	"image/color"
	"strings"
	"testing"

	"codeberg.org/stelzo/dock/internal/graphics"
)

func TestStripGraphicsResponsesDetectsSixelAndKitty(t *testing.T) {
	in := "pre\x1b[?62;4;6;9;15;18;22cmid\x1b_Gi=1;OK\x1b\\post"
	clean, msg, found, pending := stripGraphicsResponses(in)
	if !found {
		t.Fatal("expected probe detection")
	}
	if !msg.Sixel || !msg.Kitty {
		t.Fatalf("expected both protocols, got sixel=%v kitty=%v", msg.Sixel, msg.Kitty)
	}
	if pending != "" {
		t.Fatalf("unexpected pending data: %q", pending)
	}
	if clean != "premidpost" {
		t.Fatalf("clean=%q, want %q", clean, "premidpost")
	}
}

func TestStripGraphicsResponsesHandlesFragmentedSequences(t *testing.T) {
	chunk1 := "abc\x1b_Gi=1;"
	clean1, msg1, found1, pending1 := stripGraphicsResponses(chunk1)
	if found1 || msg1 != (graphics.GraphicsMsg{}) {
		t.Fatalf("chunk1 should not detect yet, found=%v msg=%+v", found1, msg1)
	}
	if clean1 != "abc" {
		t.Fatalf("chunk1 clean=%q, want abc", clean1)
	}
	if pending1 == "" {
		t.Fatal("expected pending kitty fragment")
	}

	chunk2 := pending1 + "OK\x1b\\\x1b[?62;4cdef"
	clean2, msg2, found2, pending2 := stripGraphicsResponses(chunk2)
	if !found2 {
		t.Fatal("expected detection after second chunk")
	}
	if !msg2.Kitty || !msg2.Sixel {
		t.Fatalf("expected kitty+sixel true, got %+v", msg2)
	}
	if pending2 != "" {
		t.Fatalf("unexpected pending data: %q", pending2)
	}
	if clean2 != "def" {
		t.Fatalf("chunk2 clean=%q, want def", clean2)
	}
}

func TestStripGraphicsResponsesPreservesNonGraphicsCSIAndKittyLikeData(t *testing.T) {
	in := "a\x1b[A\x1b[31mred\x1b[0m\x1b_Gi=1;ERR\x1b\\z"
	clean, msg, found, pending := stripGraphicsResponses(in)
	if pending != "" {
		t.Fatalf("unexpected pending data: %q", pending)
	}
	if found {
		t.Fatalf("expected no graphics detection, got msg=%+v", msg)
	}
	if clean != in {
		t.Fatalf("cleaned input changed.\n got: %q\nwant: %q", clean, in)
	}
}

func TestKittyImagePayloadPNGChunksAndTerminates(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 180, 120))
	for y := 0; y < 120; y++ {
		for x := 0; x < 180; x++ {
			img.SetRGBA(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: uint8((x + y) % 255), A: 255})
		}
	}

	payload := kittyImagePayloadPNG(img, 320, 200, 40, 12)
	if payload == "" {
		t.Fatal("expected non-empty kitty payload")
	}

	parts := strings.Split(payload, "\x1b\\")
	parts = parts[:len(parts)-1]
	if len(parts) < 1 {
		t.Fatalf("expected at least one chunk, got %d", len(parts))
	}

	first := parts[0]
	if !strings.Contains(first, "f=100") || !strings.Contains(first, "a=T") {
		t.Fatalf("first chunk missing kitty headers: %q", first)
	}
	if !strings.Contains(first, "q=2") {
		t.Fatalf("first chunk missing quiet mode: %q", first)
	}
	if !strings.Contains(first, "z=1") {
		t.Fatalf("first chunk missing z-index: %q", first)
	}
	if !strings.Contains(first, "s=320") || !strings.Contains(first, "v=200") {
		t.Fatalf("first chunk missing dimensions: %q", first)
	}
	if !strings.Contains(first, "c=40") || !strings.Contains(first, "r=12") {
		t.Fatalf("first chunk missing cell placement: %q", first)
	}
	if !strings.Contains(first, "C=1") {
		t.Fatalf("first chunk missing no-cursor-move flag: %q", first)
	}

	last := parts[len(parts)-1]
	if len(parts) > 1 && !strings.Contains(last, "m=0;") {
		t.Fatalf("last chunk should terminate stream: %q", last)
	}
}

func TestBlankImagePlaceholder(t *testing.T) {
	got := blankImagePlaceholder(3, 2)
	if got != "   \n   " {
		t.Fatalf("blankImagePlaceholder() = %q, want %q", got, "   \n   ")
	}
	if blankImagePlaceholder(0, 2) != "" {
		t.Fatal("expected empty placeholder for zero width")
	}
}
