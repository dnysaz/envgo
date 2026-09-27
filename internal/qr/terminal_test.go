package qr

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// The terminal renderer must be lossless: decoding the half-block text back
// into a module matrix has to reproduce m.Dark exactly. Without this, a wrong
// glyph choice still "looks like a QR code" and only fails when a phone camera
// is pointed at it, which no unit test notices.
func TestTerminalRenderIsLossless(t *testing.T) {
	for _, payload := range []string{
		"http://192.168.101.13:8097",
		"https://example.com",
		"x",
		"http://10.0.0.1:1",
		"HTTP://AVERYLONGLANHOSTNAME.EXAMPLE.COM:65535/SOME/PATH?WITH=QUERY&AND=MORE",
	} {
		m, err := EncodeText(payload, Medium)
		if err != nil {
			t.Fatalf("encode %q: %v", payload, err)
		}
		lines, cells := bodyLines(t, m)

		// Each output line packs two matrix rows, so index by matrix row.
		got := make([][]bool, m.Size)
		for y := range got {
			got[y] = make([]bool, m.Size)
		}
		for y, line := range lines {
			runes := []rune(line)
			for x := 0; x < m.Size; x++ {
				got[y*2][x] = isFilled(runes[x], true)
				if y*2+1 < m.Size {
					got[y*2+1][x] = isFilled(runes[x], false)
				}
			}
		}
		for y := 0; y < m.Size; y++ {
			if len(got[y]) != m.Size {
				t.Fatalf("%q: rendered row %d has %d modules, want %d", payload, y, len(got[y]), m.Size)
			}
			for x := 0; x < m.Size; x++ {
				if got[y][x] != m.Dark[y][x] {
					t.Fatalf("%q: module (%d,%d) rendered light=%v, want %v",
						payload, y, x, got[y][x], m.Dark[y][x])
				}
			}
		}
		if cells != m.Size {
			t.Fatalf("%q: cell width %d, want %d", payload, cells, m.Size)
		}
	}
}

// Regression guard for the glyph mix-up that shipped in v1.0.5: the renderer
// claimed to emit half blocks but wrote U+2500/U+2502 box-drawing strokes.
// Those render as one-pixel hairlines, so real scanners could not read the
// output even though the encoded payload was correct.
func TestTerminalUsesHalfBlocksNotBoxDrawing(t *testing.T) {
	m, err := EncodeText("http://192.168.101.13:8097", Medium)
	if err != nil {
		t.Fatal(err)
	}
	out := m.String()
	for _, bad := range []rune{'\u2500', '\u2502', '\u2501', '\u2503', '\u250c', '\u2510', '\u2514', '\u2518'} {
		if strings.ContainsRune(out, bad) {
			t.Errorf("render contains box-drawing %U; half blocks %U/%U are required for scanners",
				bad, '\u2580', '\u2584')
		}
	}
	for _, r := range out {
		switch r {
		case ' ', '\n', '\u2580', '\u2584', '\u2588':
		default:
			t.Errorf("unexpected rune %U (%q) in terminal output", r, r)
		}
	}
}

// The QR spec requires a four-module light margin; phone scanners rely on it
// to find the symbol's edge, especially when it sits next to other text.
func TestTerminalHasQuietZone(t *testing.T) {
	m, err := EncodeText("http://192.168.101.13:8097", Medium)
	if err != nil {
		t.Fatal(err)
	}
	lines := fullLines(t, m)
	wantWidth := m.Size + 2*quietZoneCols
	for i, line := range lines {
		if n := utf8.RuneCountInString(line); n != wantWidth {
			t.Errorf("line %d width %d, want %d", i, n, wantWidth)
		}
	}
	if want := (m.Size+1)/2 + 2*quietZoneRows; len(lines) != want {
		t.Errorf("got %d output lines, want %d", len(lines), want)
	}
	for i := 0; i < quietZoneRows; i++ {
		if strings.TrimSpace(lines[i]) != "" {
			t.Errorf("line %d should be the top quiet zone but has content", i)
		}
		if strings.TrimSpace(lines[len(lines)-1-i]) != "" {
			t.Errorf("line %d should be the bottom quiet zone but has content", len(lines)-1-i)
		}
	}
	for i, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		for _, x := range []int{0, 1, 2, 3, wantWidth - 4, wantWidth - 3, wantWidth - 2, wantWidth - 1} {
			if isFilled([]rune(line)[x], true) || isFilled([]rune(line)[x], false) {
				t.Errorf("line %d column %d is inside the quiet zone but is filled", i, x)
			}
		}
	}
}

// fullLines splits String() into its output lines, quiet zone included.
func fullLines(t *testing.T, m *Matrix) []string {
	t.Helper()
	raw := strings.TrimSuffix(m.String(), "\n")
	if raw == "" {
		t.Fatal("terminal render is empty")
	}
	return strings.Split(raw, "\n")
}

// bodyLines strips the quiet zone, leaving the QR symbol proper.
func bodyLines(t *testing.T, m *Matrix) (body []string, cells int) {
	t.Helper()
	trimmed := make([]string, 0)
	for _, l := range fullLines(t, m) {
		r := []rune(l)
		if len(r) < 2*quietZoneCols {
			t.Fatalf("render line %q is narrower than the quiet zone", l)
		}
		trimmed = append(trimmed, string(r[quietZoneCols:len(r)-quietZoneCols]))
	}
	for len(trimmed) > 0 && strings.TrimSpace(trimmed[0]) == "" {
		trimmed = trimmed[1:]
	}
	for len(trimmed) > 0 && strings.TrimSpace(trimmed[len(trimmed)-1]) == "" {
		trimmed = trimmed[:len(trimmed)-1]
	}
	if len(trimmed) == 0 {
		t.Fatal("no QR body found in render")
	}
	return trimmed, utf8.RuneCountInString(trimmed[0])
}

// isFilled reports whether the cell is dark in the requested half. Pass upper
// true for the top half of the cell, false for the bottom half.
func isFilled(cell rune, upper bool) bool {
	switch cell {
	case '\u2588': // full block
		return true
	case '\u2580': // upper half block
		return upper
	case '\u2584': // lower half block
		return !upper
	}
	return false
}
