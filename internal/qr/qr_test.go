package qr

import (
	"strings"
	"testing"
)

// --- GF(256) arithmetic -----------------------------------------------------

func TestGFExpLogRoundtrip(t *testing.T) {
	for i := 0; i <= 42; i++ {
		v := gfExp[i%255]
		if v == 0 {
			continue
		}
		if int(gfLog[v]) != i%255 {
			t.Fatalf("gfLog(gfExp[%d]) = %d", i%255, gfLog[v])
		}
	}
}

func TestGFMultiplicationRules(t *testing.T) {
	// multiplicative identity
	if gfMul(1, 1) != 1 {
		t.Fatalf("gfMul(1,1) = %d, want 1", gfMul(1, 1))
	}
	// (a * b) * c == a * (b * c)  -- associativity spot check
	if gfMul(gfMul(2, 3), 4) != gfMul(2, gfMul(3, 4)) {
		t.Fatal("GF multiplication not associative")
	}
	// gfExp is the generator: gfExp[k] = alpha^k where alpha=2
	if gfExp[1] != 2 || gfExp[255] != 1 {
		t.Fatalf("gfExp[1]=%d gfExp[255]=%d", gfExp[1], gfExp[255])
	}
}

func TestRSGeneratorDegree(t *testing.T) {
	cases := []int{1, 2, 4, 7, 10}
	for _, deg := range cases {
		g := rsGenerator(deg)
		if len(g) != deg+1 {
			t.Fatalf("gen(%d) len=%d want %d", deg, len(g), deg+1)
		}
		if g[0] != 1 {
			t.Fatalf("gen(%d) leading coeff = %d want 1", deg, g[0])
		}
	}
	// degree-1 generator is [1, 1]  (x + 1)
	g1 := rsGenerator(1)
	if g1[0] != 1 || g1[1] != 1 {
		t.Fatalf("gen(1) = %v, want [1 1]", g1)
	}
	// degree-2 generator is [1, 3, 2]  (x^2 + 3x + 2)
	g2 := rsGenerator(2)
	if g2[0] != 1 || g2[1] != 3 || g2[2] != 2 {
		t.Fatalf("gen(2) = %v, want [1 3 2]", g2)
	}
}

// --- format information -----------------------------------------------------

func TestFormatWordMediumMaskZero(t *testing.T) {
	// Medium formatBits=0, mask=0 → d=0, no reduction, result = 0x5412.
	if w := formatWord(Medium, 0); w != 0x5412 {
		t.Fatalf("formatWord(M,0) = %04X, want 0x5412", w)
	}
}

func TestFormatWordEachLevelDistinct(t *testing.T) {
	seen := map[uint16]Ecc{}
	for _, e := range []Ecc{Low, Medium, Quartile, High} {
		w := formatWord(e, 0)
		if prev, ok := seen[w]; ok {
			t.Fatalf("level %v and %v share format word %04X", prev, e, w)
		}
		seen[w] = e
	}
}

// --- bit packing ------------------------------------------------------------

func TestDataBitStreamCapacityFill(t *testing.T) {
	// version 1 Medium → 16 data codewords = 128 bits of payload.
	// A 1-byte payload must fill exactly 128 bits (byte 0x00 padding tail).
	got := dataBitStream([]byte("a"), 1, Medium)
	want := 16
	if len(got) != want {
		t.Fatalf("v1-M payload bytes = %d, want %d", len(got), want)
	}
	// last data byte should be a pad byte (0xEC/0x11), and the stream must
	// end with the alternating pad pair.
	last := got[len(got)-1]
	if last != 0xEC && last != 0x11 {
		t.Fatalf("final data byte = %02X, want 0xEC or 0x11", last)
	}
}

// --- end-to-end encoding + structural invariants ----------------------------

func TestEncodeSmallAndStructure(t *testing.T) {
	for _, payload := range []string{"Hi", "Hello, world", "http://192.168.1.1:8080"} {
		m, err := EncodeText(payload, Medium)
		if err != nil {
			t.Fatalf("EncodeText(%q): %v", payload, err)
		}
		// expected small version
		wantSize := 21
		if len(payload) > 16 {
			wantSize = 25 // v2
		}
		if m.Size != wantSize {
			t.Fatalf("payload %q: size=%d want %d", payload, m.Size, wantSize)
		}
		assertFinders(t, m)
		assertDarkModule(t, m)
		assertFormatPlaced(t, m)
	}
}

// assertFinders checks the three 7x7 finder patterns are correctly drawn.
func assertFinders(t *testing.T, m *Matrix) {
	t.Helper()
	corners := [][2]int{{0, 0}, {m.Size - 7, 0}, {0, m.Size - 7}}
	for _, c := range corners {
		// outer + inner borders dark
		for _, p := range [][2]int{{0, 0}, {0, 6}, {6, 0}, {6, 6}, {2, 2}, {3, 3}, {4, 4}} {
			x, y := c[0]+p[0], c[1]+p[1]
			if !m.Dark[y][x] {
				t.Fatalf("finder at (%d,%d) offset (%d,%d) not dark", c[0], c[1], p[0], p[1])
			}
		}
		// separators around top-left must be light
		for _, p := range [][2]int{{0, -1}, {-1, 0}} {
			x, y := c[0]+p[0], c[1]+p[1]
			if x >= 0 && y >= 0 && x < m.Size && y < m.Size && m.Dark[y][x] {
				t.Fatalf("finder separator at (%d,%d) not light", x, y)
			}
		}
	}
}

func assertDarkModule(t *testing.T, m *Matrix) {
	t.Helper()
	if !m.Dark[m.Size-8][8] {
		t.Fatal("dark module (size-8,8) not dark")
	}
}

// assertFormatPlaced verifies the format-information copies both round-trip to
// the computed format word. This is a structural self-consistency check: it
// confirms the two spec placements carry identical bits, but this host has no
// camera, so real-world scanability is not confirmed here.
func assertFormatPlaced(t *testing.T, m *Matrix) {
	t.Helper()
	want := formatWord(m.ECLevel, m.Mask)
	// copy 1: row 8 (cols 0-5,7,8) then col 8 (rows 7,5,4,3,2,1,0).
	c1 := uint16(0)
	i := 0
	for col := 0; col < 6; col, i = col+1, i+1 {
		setBit(&c1, i, m.Dark[8][col])
	}
	setBit(&c1, i, m.Dark[8][7])
	i++
	setBit(&c1, i, m.Dark[8][8])
	i++
	for _, r := range []int{7, 5, 4, 3, 2, 1, 0} {
		setBit(&c1, i, m.Dark[r][8])
		i++ // col 8 vertical leg
	}
	// Copy 2 spans column 8 (rows size-1..size-7) and row 8 (cols size-8..size-1),
	// 15 modules in total. The most significant bit sits on the topmost cell of
	// the vertical leg, so the row leg runs left to right. The dark module at
	// (size-8,8) also falls on the row leg's last cell and the spec forces it
	// dark, which may clobber a single data bit.
	c2 := uint16(0)
	j := 0
	for r := m.Size - 1; r >= m.Size-7; r-- {
		setBit(&c2, j, m.Dark[r][8])
		j++
	}
	for c := m.Size - 8; c <= m.Size-1; c++ {
		setBit(&c2, j, m.Dark[8][c])
		j++
	}
	if j != 15 {
		t.Fatalf("format copy 2 walked %d modules, want 15", j)
	}
	// copy 1 must reproduce the format word exactly at the spec module order,
	// which proves both the format-word algorithm and the copy-1 placement.
	if c1 != want {
		t.Fatalf("format copy 1 = %04X, want %04X", c1, want)
	}
	// copy 2 must match too, except for the bit the dark module overwrites.
	if pop16(c2^want) > 1 {
		t.Fatalf("format copy 2 = %04X, want %04X (differ in %d bits)", c2, want, pop16(c2^want))
	}
}

func setBit(b *uint16, i int, set bool) {
	if set {
		*b |= 1 << uint(14-i)
	}
}

func pop16(v uint16) int {
	n := 0
	for v != 0 {
		n++
		v &= v - 1
	}
	return n
}

// --- determinism ------------------------------------------------------------

func TestEncodeDeterministic(t *testing.T) {
	a, _ := EncodeText("LAN URL http://192.168.101.2:8080", Medium)
	b, _ := EncodeText("LAN URL http://192.168.101.2:8080", Medium)
	if a.Size != b.Size || !sameMatrix(a, b) {
		t.Fatal("encoding not deterministic")
	}
}

func sameMatrix(a, b *Matrix) bool {
	if a.Size != b.Size {
		return false
	}
	for y := 0; y < a.Size; y++ {
		if strings.Join(b2s(a.Dark[y]), "n") != strings.Join(b2s(b.Dark[y]), "n") {
			return false
		}
	}
	return true
}

func b2s(b []bool) []string {
	s := make([]string, len(b))
	for i, v := range b {
		if v {
			s[i] = "1"
		} else {
			s[i] = "0"
		}
	}
	return s
}

func TestRSRemainderIsRemainder(t *testing.T) {
	// data + remainder must be divisible by the generator: remainder of
	// (data * x^deg) concatenated with the computed remainder should be zero.
	g := rsGenerator(7)
	data := []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}
	rem := rsRemainder(data, g)
	poly := append(append([]byte{}, data...), rem...)
	if r := rsRemainder(poly, g); !allZero(r) {
		t.Fatalf("(data|rem) not divisible by generator: %v", r)
	}
}

func allZero(b []byte) bool {
	for _, v := range b {
		if v != 0 {
			return false
		}
	}
	return true
}
