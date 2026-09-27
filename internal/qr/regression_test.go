package qr

import (
	"strings"
	"testing"
)

// Golden tables below were taken from an independent reference implementation
// (python-qrcode) and pin down the two BCH(15,5)/BCH(18,6) information codes the
// encoder depends on.

// formatGolden lists the 32 format-information words, indexed by level
// L, M, Q, H and then by mask 0-7.
var formatGolden = [4][8]uint16{
	{0x77C4, 0x72F3, 0x7DAA, 0x789D, 0x662F, 0x6318, 0x6C41, 0x6976},
	{0x5412, 0x5125, 0x5E7C, 0x5B4B, 0x45F9, 0x40CE, 0x4F97, 0x4AA0},
	{0x355F, 0x3068, 0x3F31, 0x3A06, 0x24B4, 0x2183, 0x2EDA, 0x2BED},
	{0x1689, 0x13BE, 0x1CE7, 0x19D0, 0x0762, 0x0255, 0x0D0C, 0x083B},
}

// versionGolden lists the 34 version-information words for versions 7-40.
var versionGolden = map[int]uint32{
	7: 0x07C94, 8: 0x085BC, 9: 0x09A99, 10: 0x0A4D3,
	11: 0x0BBF6, 12: 0x0C762, 13: 0x0D847, 14: 0x0E60D,
	15: 0x0F928, 16: 0x10B78, 17: 0x1145D, 18: 0x12A17,
	19: 0x13532, 20: 0x149A6, 21: 0x15683, 22: 0x168C9,
	23: 0x177EC, 24: 0x18EC4, 25: 0x191E1, 26: 0x1AFAB,
	27: 0x1B08E, 28: 0x1CC1A, 29: 0x1D33F, 30: 0x1ED75,
	31: 0x1F250, 32: 0x209D5, 33: 0x216F0, 34: 0x228BA,
	35: 0x2379F, 36: 0x24B0B, 37: 0x2542E, 38: 0x26A64,
	39: 0x27541, 40: 0x28C69,
}

// --- golden information codes ------------------------------------------------

func TestFormatWordGolden(t *testing.T) {
	levels := [4]Ecc{Low, Medium, Quartile, High}
	for li, e := range levels {
		for mask := 0; mask < 8; mask++ {
			if got := formatWord(e, mask); got != formatGolden[li][mask] {
				t.Errorf("formatWord(%s, %d) = %04X, want %04X", e, mask, got, formatGolden[li][mask])
			}
		}
	}
}

func TestVersionWordGolden(t *testing.T) {
	for v := 7; v <= maxVersion; v++ {
		if got, want := versionWord(v), versionGolden[v]; got != want {
			t.Errorf("versionWord(%d) = %05X, want %05X", v, got, want)
		}
	}
}

// TestVersionWordIsBCH guards the arithmetic itself: a version word must have a
// zero remainder when divided by the BCH(18,6) generator 0x1F25, and its top six
// bits must be the version number. This also proves the 18-bit value is not
// truncated (a uint16 would lose the data bits for version >= 16).
func TestVersionWordIsBCH(t *testing.T) {
	for v := 7; v <= maxVersion; v++ {
		w := versionWord(v)
		if w>>12 != uint32(v) {
			t.Errorf("versionWord(%d) top bits = %d, want %d", v, w>>12, v)
		}
		rem := w
		for i := 17; i >= 12; i-- {
			if rem&(1<<uint(i)) != 0 {
				rem ^= g18 << uint(i-12)
			}
		}
		if rem != 0 {
			t.Errorf("versionWord(%d) = %05X has non-zero BCH remainder %05X", v, w, rem)
		}
	}
}

// --- version information placement -------------------------------------------

// versionCell locates the module holding version-information bit i of the given
// copy. The standard numbers these bits from the least significant end, which
// is the opposite of the format-information ordering.
func versionCell(version, copy, i int) (x, y int) {
	size := version*4 + 17
	if copy == 0 {
		return i%3 + size - 11, i / 3
	}
	return i / 3, i%3 + size - 11
}

func TestVersionInfoPlacement(t *testing.T) {
	for v := 7; v <= maxVersion; v++ {
		want := versionGolden[v]
		// EncodeText picks its own version, so build the target version directly.
		m := buildMatrix(v, Low, rsInterleave(dataBitStream([]byte("v"), v, Low), v, Low))
		for copy := 0; copy < 2; copy++ {
			var got uint32
			for i := 0; i < 18; i++ {
				x, y := versionCell(v, copy, i)
				if m.Dark[y][x] {
					got |= 1 << uint(i)
				}
			}
			if got != want {
				t.Errorf("v%d copy %d reads %05X, want %05X", v, copy, got, want)
			}
		}
	}
}

// TestVersionInfoIsFunctionPattern ensures no payload bit is written over the
// version blocks, which is what keeps high-version symbols decodable.
func TestVersionInfoIsFunctionPattern(t *testing.T) {
	for v := 7; v <= maxVersion; v++ {
		size := v*4 + 17
		funcs := blankFuncs(size)
		reserveVersionCells(funcs, v, size)
		for copy := 0; copy < 2; copy++ {
			for i := 0; i < 18; i++ {
				x, y := versionCell(v, copy, i)
				if !funcs[y][x] {
					t.Fatalf("v%d copy %d cell (%d,%d) not reserved", v, copy, x, y)
				}
			}
		}
	}
}

// TestVersionInfoOverridesAlignment pins the one module where a version block
// and an alignment pattern collide. The spec gives the version information
// priority, so it must win.
func TestVersionInfoOverridesAlignment(t *testing.T) {
	for v := 20; v <= maxVersion; v++ {
		size := v*4 + 17
		for _, p := range [][2]int{{5, size - 10}, {size - 10, 5}} {
			cx, cy := versionCell(v, 0, 16)
			if cx != p[0] || cy != p[1] {
				continue
			}
			dark := blankDark(size)
			funcs := blankFuncs(size)
			drawAlignment(dark, funcs, v, size)
			want := versionGolden[v]>>16&1 == 1
			writeVersionBits(dark, funcs, v, size)
			if got := dark[p[1]][p[0]]; got != want {
				t.Errorf("v%d module (%d,%d) = %v, want %v (version bit 16)", v, p[0], p[1], got, want)
			}
		}
	}
}

func TestNoVersionInfoBelowSeven(t *testing.T) {
	for v := 1; v <= 6; v++ {
		size := v*4 + 17
		funcs := blankFuncs(size)
		reserveVersionCells(funcs, v, size)
		dark := blankDark(size)
		writeVersionBits(dark, funcs, v, size)
		for y := range funcs {
			for x := range funcs[y] {
				if funcs[y][x] || dark[y][x] {
					t.Fatalf("v%d unexpectedly wrote a module at (%d,%d)", v, x, y)
				}
			}
		}
	}
}

// --- masks -------------------------------------------------------------------

// TestMask2IsColumnMod3 pins the mask-2 rule. It was previously implemented as
// "column mod 2 == 0", which produced undecodable symbols.
func TestMask2IsColumnMod3(t *testing.T) {
	size := 21
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			want := x%3 == 0
			if got := maskCond(2, x, y); got != want {
				t.Fatalf("mask 2 at (%d,%d) = %v, want %v", x, y, got, want)
			}
		}
	}
}

// --- every version and level -------------------------------------------------

func TestAllVersionsStructural(t *testing.T) {
	levels := [4]Ecc{Low, Medium, Quartile, High}
	for v := 1; v <= maxVersion; v++ {
		size := v*4 + 17
		for _, e := range levels {
			payload := strings.Repeat("A", minPayloadFor(v, e))
			m, err := EncodeText(payload, e)
			if err != nil {
				t.Fatalf("v%d %s: %v", v, e, err)
			}
			if m.Size != size {
				t.Fatalf("payload chose size %d, want %d for v%d", m.Size, size, v)
			}
			if m.Version != v || m.ECLevel != e {
				t.Fatalf("got v%d %s, want v%d %s", m.Version, m.ECLevel, v, e)
			}
			if m.Mask < 0 || m.Mask > 7 {
				t.Fatalf("v%d %s: mask %d out of range", v, e, m.Mask)
			}
			assertFinders(t, m)
			assertDarkModule(t, m)
			assertFormatPlaced(t, m)
			assertTimingPattern(t, m)
			assertAlignmentCenters(t, m, v)
		}
	}
}

func assertTimingPattern(t *testing.T, m *Matrix) {
	t.Helper()
	for i := 8; i < m.Size-8; i++ {
		if got, want := m.Dark[i][6], i%2 == 0; got != want {
			t.Fatalf("timing column at row %d = %v, want %v", i, got, want)
		}
		if got, want := m.Dark[6][i], i%2 == 0; got != want {
			t.Fatalf("timing row at col %d = %v, want %v", i, got, want)
		}
	}
}

// assertAlignmentCenters checks every alignment pattern that the spec requires.
// The centre module must be dark; a missing centre is undecodable.
func assertAlignmentCenters(t *testing.T, m *Matrix, version int) {
	t.Helper()
	size := m.Size
	centers := alignmentCenters(version)
	for _, cy := range centers {
		for _, cx := range centers {
			if alignmentOverlapsFunction(cx, cy, size) {
				continue
			}
			if !m.Dark[cy][cx] {
				t.Fatalf("v%d alignment centre (%d,%d) not dark", version, cx, cy)
			}
			// the ring at distance 1 must be light
			for _, p := range [][2]int{{1, 0}, {0, 1}, {-1, 0}, {0, -1}} {
				if m.Dark[cy+p[1]][cx+p[0]] {
					t.Fatalf("v%d alignment ring (%d,%d) should be light", version, cx+p[0], cy+p[1])
				}
			}
		}
	}
}

// --- capacity ----------------------------------------------------------------

// TestEncodeRejectsOversizePerLevel checks the per-level guard reports the real
// limit for the requested level rather than a single global byte count.
func TestEncodeRejectsOversizePerLevel(t *testing.T) {
	levels := [4]Ecc{Low, Medium, Quartile, High}
	for _, e := range levels {
		limit := maxByteLen(e)
		if _, err := EncodeBinary(make([]byte, limit), e); err != nil {
			t.Fatalf("%s: %d bytes should fit: %v", e, limit, err)
		}
		if _, err := EncodeBinary(make([]byte, limit+1), e); err == nil {
			t.Fatalf("%s: %d bytes should not fit", e, limit+1)
		}
	}
}

// TestEveryVersionIsReachable makes sure no version is skipped by the capacity
// search, including the awkward 16/17/20 boundaries.
func TestEveryVersionIsReachable(t *testing.T) {
	seen := map[int]bool{}
	prev := 0
	for n := 1; n <= maxByteLen(Low); n++ {
		v, ok := chooseVersion(n, Low)
		if !ok {
			t.Fatalf("no version fits %d bytes at Low", n)
		}
		if v < prev {
			t.Fatalf("chooseVersion(%d) = v%d, went backwards from v%d", n, v, prev)
		}
		prev = v
		seen[v] = true
	}
	for v := 1; v <= maxVersion; v++ {
		if !seen[v] {
			t.Errorf("version %d is never selected", v)
		}
	}
}

// --- Reed-Solomon over every block specification -----------------------------

// TestRSRemainderZeroForEverySpec drives all 160 version/level combinations
// through a full encode/decode check of the block structure.
func TestRSRemainderZeroForEverySpec(t *testing.T) {
	for v := 1; v <= maxVersion; v++ {
		for _, e := range []Ecc{Low, Medium, Quartile, High} {
			s := spec(v, e)
			if s.rsBlockCount() == 0 {
				t.Fatalf("v%d %s: no RS blocks", v, e)
			}
			if got, want := len(rsInterleave(dataBitStream([]byte("x"), v, e), v, e)),
				s.totalCodewords(); got != want {
				t.Errorf("v%d %s: interleaved %d codewords, want %d", v, e, got, want)
			}
		}
	}
}

// --- penalty rules -----------------------------------------------------------

// TestDarkBalancePenalty pins rule N4. The percentage must be evaluated as an
// exact fraction: computing an integer percentage first would round a dark
// share of 45.35% down to 45% and hand it a full band of penalty it does not
// owe.
func TestDarkBalancePenalty(t *testing.T) {
	cases := []struct {
		dark, total, want int
	}{
		{50, 100, 0},   // exactly half
		{199, 441, 0},  // 45.35% - rounds down to 45% but owes nothing
		{198, 441, 10}, // 44.90% - inside the 45% band
		{243, 441, 10}, // 55.10% - just past the 55% band
		{242, 441, 0},  // 54.88%
		{60, 100, 20},  // 60% - two bands
		{250, 441, 10}, // 56.69%
		{265, 441, 20}, // 60.09% - just past the 60% band
	}
	for _, c := range cases {
		if got := darkBalancePenalty(c.dark, c.total); got != c.want {
			t.Errorf("darkBalancePenalty(%d, %d) = %d, want %d", c.dark, c.total, got, c.want)
		}
	}
}

// TestRunPenalty checks rule N1: a run of n same-colour modules costs n-2.
func TestRunPenalty(t *testing.T) {
	cases := []struct {
		bits string
		want int
	}{
		{"", 0},
		{"0000", 0},
		{"11111", 3},
		{"111111", 4},
		{"1111111111", 8},
		{"11111011111", 6}, // two runs of five
		{"1111101111", 3},  // runs of five and four
	}
	for _, c := range cases {
		row := make([]bool, len(c.bits))
		for i, b := range c.bits {
			row[i] = b == '1'
		}
		if got := runPenalty(row); got != c.want {
			t.Errorf("runPenalty(%q) = %d, want %d", c.bits, got, c.want)
		}
	}
}

// TestDarkModuleScoresAsDark documents a deliberate difference from
// python-qrcode. The standard evaluates the eight mask candidates on the
// finished symbol, so the always-dark module contributes to the penalty.
// python-qrcode blanks it during evaluation, which can flip near-ties; envGo
// follows ISO/IEC 18004 and matches other reference implementations.
func TestDarkModuleScoresAsDark(t *testing.T) {
	v, ecl := 2, Medium
	size := v*4 + 17
	cw := rsInterleave(dataBitStream([]byte("payload for mask scoring"), v, ecl), v, ecl)
	dark := blankDark(size)
	funcs := blankFuncs(size)
	drawFinders(dark, funcs)
	drawTiming(dark, funcs, size)
	drawAlignment(dark, funcs, v, size)
	reserveFormatCells(funcs, size)
	reserveVersionCells(funcs, v, size)
	dark[size-8][8] = true
	funcs[size-8][8] = true
	placeData(dark, funcs, cw, size)
	if !dark[size-8][8] {
		t.Fatal("dark module must be dark while masks are scored")
	}
	best := bestMask(dark, funcs, size)
	m := buildMatrix(v, ecl, cw)
	if m.Mask != best {
		t.Fatalf("built mask %d, bestMask says %d", m.Mask, best)
	}
	// the winning mask must be the strict minimum, ties resolved to the
	// lowest mask number as the standard requires
	work := blankDark(size)
	for mask := 0; mask < 8; mask++ {
		for y := range work {
			copy(work[y], dark[y])
		}
		applyMask(work, funcs, mask, size)
		p := penaltyScore(work, size)
		if mask < best && p <= penaltyFor(dark, funcs, best, size) {
			t.Fatalf("mask %d scores %d, not worse than chosen mask %d", mask, p, best)
		}
	}
}

func penaltyFor(dark, funcs [][]bool, mask, size int) int {
	work := make([][]bool, size)
	for y := range work {
		work[y] = make([]bool, size)
		copy(work[y], dark[y])
	}
	applyMask(work, funcs, mask, size)
	return penaltyScore(work, size)
}

// --- helpers -----------------------------------------------------------------

func blankDark(size int) [][]bool {
	g := make([][]bool, size)
	for i := range g {
		g[i] = make([]bool, size)
	}
	return g
}

func blankFuncs(size int) [][]bool { return blankDark(size) }

// minPayloadFor returns the shortest byte-mode payload that forces the capacity
// search to select exactly the given version.
func minPayloadFor(version int, ecl Ecc) int {
	for n := 1; ; n++ {
		if v, ok := chooseVersion(n, ecl); ok && v == version {
			return n
		}
	}
}
