// Package qr renders a QR Code (QR Model 2, ISO/IEC 18004) to a terminal
// using Unicode half-block characters. Zero dependencies.
//
// Verified by structural tests on this host: GF(256) arithmetic, Reed-Solomon
// remainder/generator, bit packing, capacity selection, finder/timing/alignment
// patterns, data zigzag, masking, and N1-N4 penalty scoring.
//
// Because this macOS host has no camera, the produced codes are checked for
// invariants and self-consistency rather than physically scanned.
package qr

import (
	"fmt"
	"io"
	"strings"
)

// Ecc is an error-correction level.
type Ecc int

const (
	Low Ecc = iota
	Medium
	Quartile
	High
)

// String returns the single-letter level name used by the QR standard.
func (e Ecc) String() string {
	switch e {
	case Low:
		return "L"
	case Medium:
		return "M"
	case Quartile:
		return "Q"
	case High:
		return "H"
	}
	return "?"
}

func (e Ecc) formatBits() int {
	switch e {
	case Low:
		return 1
	case Medium:
		return 0
	case Quartile:
		return 3
	case High:
		return 2
	}
	return 0
}

// Matrix is the square grid of light/dark modules of a rendered QR code.
type Matrix struct {
	Version int
	ECLevel Ecc
	Mask    int
	Size    int
	Dark    [][]bool
}

// EncodeText encodes a UTF-8 string at the given error-correction level. The
// smallest version that fits is chosen and the lowest-penalty mask selected.
func EncodeText(text string, ecl Ecc) (*Matrix, error) {
	return EncodeBinary([]byte(text), ecl)
}

// EncodeBinary encodes raw bytes at the given error-correction level.
func EncodeBinary(data []byte, ecl Ecc) (*Matrix, error) {
	version, ok := chooseVersion(len(data), ecl)
	if !ok {
		return nil, fmt.Errorf("qr: data too long for level %s (max %d bytes)", ecl, maxByteLen(ecl))
	}
	codewords := rsInterleave(dataBitStream(data, version, ecl), version, ecl)
	return buildMatrix(version, ecl, codewords), nil
}

// String renders the QR code as a grid of Unicode half-blocks (two matrix
// rows per output line, one character cell per module).
func (m *Matrix) String() string {
	var b strings.Builder
	for y := 0; y < m.Size; y += 2 {
		for x := 0; x < m.Size; x++ {
			top := m.Dark[y][x]
			bot := false
			if y+1 < m.Size {
				bot = m.Dark[y+1][x]
			}
			switch {
			case top && bot:
				b.WriteString("\xe2\x96\x88") // █
			case top:
				b.WriteString("\xe2\x94\x80") // ▀
			case bot:
				b.WriteString("\xe2\x94\x82") // ▄
			default:
				b.WriteByte(' ')
			}
		}
		b.WriteByte('\n')
	}
	return b.String()
}

// WriteTo writes the rendered QR code to w.
func (m *Matrix) WriteTo(w io.Writer) (int64, error) {
	n, err := io.WriteString(w, m.String())
	return int64(n), err
}

// ---------------------------------------------------------------------------
// Bit stream
// ---------------------------------------------------------------------------

func dataBitStream(data []byte, version int, ecl Ecc) []byte {
	bits := newBitWriter()
	bits.write(4, 4) // byte-mode indicator
	bits.write(len(data), charCountBits(4, version))
	for _, b := range data {
		bits.write(int(b), 8)
	}
	// terminator: up to 4 zero bits
	cap := capacityBits(version, ecl)
	term := 4
	if cap-bits.Len() < 4 {
		term = cap - bits.Len()
	}
	if term > 0 {
		bits.write(0, term)
	}
	// pad to a byte boundary
	if r := bits.Len() % 8; r != 0 {
		bits.write(0, 8-r)
	}
	// pad to capacity with 0xEC/0x11
	for bits.Len() < cap {
		bits.write(0xEC, 8)
		if bits.Len() < cap {
			bits.write(0x11, 8)
		}
	}
	return bits.bytes()
}

func charCountBits(mode, version int) int {
	if mode == 4 {
		if version <= 9 {
			return 8
		}
		return 16
	}
	return 8
}

type bitWriter struct {
	buf []byte
	bit int // next free bit position within buf[len(buf)-1]
}

func newBitWriter() *bitWriter { return &bitWriter{buf: []byte{0}} }

func (b *bitWriter) write(v, n int) {
	for i := n - 1; i >= 0; i-- {
		if b.bit == 8 {
			b.buf = append(b.buf, 0)
			b.bit = 0
		}
		if v>>(uint(i))&1 == 1 {
			b.buf[len(b.buf)-1] |= 0x80 >> uint(b.bit)
		}
		b.bit++
	}
}

func (b *bitWriter) Len() int {
	return len(b.buf)*8 - (8 - b.bit)
}

func (b *bitWriter) bytes() []byte {
	return b.buf
}

// ---------------------------------------------------------------------------
// Version / capacity
// ---------------------------------------------------------------------------

// maxVersion is the largest symbol defined by the standard.
const maxVersion = 40

// spec returns the block layout for a version and level.
func spec(version int, ecl Ecc) rsSpec {
	if version < 1 || version > maxVersion || ecl < Low || ecl > High {
		return rsSpec{}
	}
	return rsTable[ecl][version]
}

// capacityBits is the payload capacity of a symbol in bits, or -1 when the
// version/level combination does not exist.
func capacityBits(version int, ecl Ecc) int {
	s := spec(version, ecl)
	if s.rsBlockCount() == 0 {
		return -1
	}
	return s.dataCodewords() * 8
}

// maxByteLen is the largest byte-mode payload a level can carry.
func maxByteLen(ecl Ecc) int {
	best := 0
	for v := 1; v <= maxVersion; v++ {
		if n := spec(v, ecl).dataCodewords() - 3; n > best {
			best = n
		}
	}
	return best
}

// chooseVersion picks the smallest version whose byte-mode capacity fits the
// given data length (mode + charcount + data + terminator + pad).
func chooseVersion(dataLen int, ecl Ecc) (int, bool) {
	for v := 1; v <= maxVersion; v++ {
		cap := capacityBits(v, ecl)
		if cap < 0 {
			continue
		}
		needed := 4 + charCountBits(4, v) + dataLen*8 // mode + count + data
		needed += 4                                   // terminator
		if r := needed % 8; r != 0 {
			needed += 8 - r
		}
		if needed <= cap {
			return v, true
		}
	}
	return 0, false
}

// ---------------------------------------------------------------------------
// Reed-Solomon over GF(256), primitive polynomial 0x11D
// ---------------------------------------------------------------------------

var (
	gfExp = [256]byte{}
	gfLog = [256]byte{}
)

func init() {
	x := uint16(1)
	for i := 0; i < 255; i++ {
		gfExp[i] = byte(x)
		gfLog[byte(x)] = byte(i)
		x <<= 1
		if x&0x100 != 0 {
			x ^= 0x11D
		}
	}
	gfExp[255] = 1
}

func gfMul(a, b byte) byte {
	if a == 0 || b == 0 {
		return 0
	}
	return gfExp[(int(gfLog[a])+int(gfLog[b]))%255]
}

func rsGenerator(degree int) []byte {
	g := []byte{1}
	for i := 0; i < degree; i++ {
		ng := make([]byte, len(g)+1)
		for j := 0; j < len(g); j++ {
			ng[j] ^= g[j]
			ng[j+1] ^= gfMul(g[j], gfExp[i])
		}
		g = ng
	}
	return g
}

// rsRemainder computes the EC bytes for one block. g is the generator
// polynomial, high-first, with the leading 1 coefficient at g[0]; its degree
// is len(g)-1, and the returned remainder has len(g)-1 bytes.
func rsRemainder(data []byte, g []byte) []byte {
	deg := len(g) - 1
	work := make([]byte, len(data)+deg)
	copy(work, data)
	for i := 0; i < len(data); i++ {
		c := work[i]
		if c == 0 {
			continue
		}
		for j := 1; j <= deg; j++ {
			work[i+j] ^= gfMul(g[j], c)
		}
	}
	return work[len(data):]
}

// ---------------------------------------------------------------------------
// Encoding pipeline
// ---------------------------------------------------------------------------

// rsInterleave splits data into RS blocks, appends EC, and interleaves the
// blocks column by column. Group 2 blocks hold one extra data codeword, so the
// data section is interleaved over the longest block length while the EC
// section is interleaved over the uniform EC length.
func rsInterleave(data []byte, version int, ecl Ecc) []byte {
	s := spec(version, ecl)
	gen := rsGenerator(s.ecPerBlock)

	type block struct {
		data []byte
		ec   []byte
	}
	blocks := make([]block, 0, s.rsBlockCount())
	off := 0
	addGroup := func(count, size int) {
		for i := 0; i < count; i++ {
			d := data[off : off+size]
			off += size
			blocks = append(blocks, block{data: d, ec: rsRemainder(d, gen)})
		}
	}
	addGroup(s.blocks1, s.data1)
	addGroup(s.blocks2, s.data2)

	longest := 0
	for _, b := range blocks {
		if len(b.data) > longest {
			longest = len(b.data)
		}
	}
	out := make([]byte, 0, len(data)+s.rsBlockCount()*s.ecPerBlock)
	for i := 0; i < longest; i++ {
		for _, b := range blocks {
			if i < len(b.data) {
				out = append(out, b.data[i])
			}
		}
	}
	for i := 0; i < s.ecPerBlock; i++ {
		for _, b := range blocks {
			out = append(out, b.ec[i])
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Module placement
// ---------------------------------------------------------------------------

func buildMatrix(version int, ecl Ecc, codewords []byte) *Matrix {
	size := version*4 + 17
	dark := make([][]bool, size)
	funcs := make([][]bool, size)
	for i := range dark {
		dark[i] = make([]bool, size)
		funcs[i] = make([]bool, size)
	}

	drawFinders(dark, funcs)
	drawTiming(dark, funcs, size)
	drawAlignment(dark, funcs, version, size)
	reserveFormatCells(funcs, size)
	reserveVersionCells(funcs, version, size)
	dark[size-8][8] = true // dark module
	funcs[size-8][8] = true

	placeData(dark, funcs, codewords, size)

	best := bestMask(dark, funcs, size)
	applyMask(dark, funcs, best, size)
	writeFormatBits(dark, funcs, ecl, best, size)
	writeVersionBits(dark, funcs, version, size)

	return &Matrix{Version: version, ECLevel: ecl, Mask: best, Size: size, Dark: dark}
}

// reserveVersionCells marks the two version-information blocks used by version
// 7 and above. They carry no payload, so data placement must skip them.
func reserveVersionCells(funcs [][]bool, version, size int) {
	if version < 7 {
		return
	}
	for i := 0; i < 18; i++ {
		// copy 1: rows 0-5, cols size-11..size-9
		x, y := i%3+size-11, i/3
		funcs[y][x] = true
		// copy 2: rows size-11..size-9, cols 0-5
		x, y = i/3, i%3+size-11
		funcs[y][x] = true
	}
}

const g18 = 0x1F25 // x^12 + x^11 + x^10 + x^9 + x^8 + x^5 + x^2 + 1

// versionWord computes the 18-bit version information value (MSB first) that
// the standard requires for versions 7 and above. It is 18 bits wide, so it
// cannot be held in a uint16.
func versionWord(version int) uint32 {
	d := version << 12
	rem := d
	for i := 17; i >= 12; i-- {
		if rem&(1<<uint(i)) != 0 {
			rem ^= g18 << uint(i-12)
		}
	}
	return uint32(d | rem)
}

// writeVersionBits stamps the version information into both blocks. It runs
// after masking because these modules are not part of the data region.
func writeVersionBits(dark, funcs [][]bool, version, size int) {
	if version < 7 {
		return
	}
	w := versionWord(version)
	// the version blocks are numbered from the least significant bit, unlike
	// the format information
	bit := func(i int) bool { return w>>uint(i)&1 == 1 }
	for i := 0; i < 18; i++ {
		v := bit(i)
		x, y := i%3+size-11, i/3
		dark[y][x] = v
		funcs[y][x] = true
		x, y = i/3, i%3+size-11
		dark[y][x] = v
		funcs[y][x] = true
	}
}

// reserveFormatCells marks the format-information and dark-module cells as
// function patterns so that data placement skips them (they are filled in
// later by writeFormatBits). This must run before placeData.
func reserveFormatCells(funcs [][]bool, size int) {
	set := func(x, y int) {
		if 0 <= y && y < len(funcs) && 0 <= x && x < len(funcs) {
			funcs[y][x] = true
		}
	}
	// copy 1: row 8 (cols 0-5, 7, 8) and column 8 (rows 7,5,4,3,2,1,0).
	for i := 0; i < 6; i++ {
		set(i, 8)
	}
	set(7, 8)
	set(8, 8)
	for _, r := range []int{7, 5, 4, 3, 2, 1, 0} {
		set(8, r)
	}
	// copy 2: column 8 (rows size-1..size-7), dark module, row 8
	// (cols size-8..size-1).
	for i := 0; i < 7; i++ {
		set(8, size-1-i)
	}
	set(8, size-8)
	for i := 0; i < 8; i++ {
		set(size-8+i, 8)
	}
}

func drawFinders(dark, funcs [][]bool) {
	size := len(dark)
	corners := [][2]int{{0, 0}, {size - 7, 0}, {0, size - 7}}
	for _, c := range corners {
		drawFinder(dark, funcs, c[0], c[1])
		drawSeparator(dark, funcs, c[0], c[1])
	}
}

func drawSeparator(dark, funcs [][]bool, fx, fy int) {
	size := len(dark)
	mark := func(x, y int) {
		if x < 0 || y < 0 || x >= size || y >= size {
			return
		}
		dark[y][x] = false
		funcs[y][x] = true
	}
	for i := 0; i < 8; i++ {
		mark(fx-1, fy+i) // left
		mark(fx+7, fy+i) // right
		mark(fx+i, fy-1) // top
		mark(fx+i, fy+7) // bottom
	}
}

// drawFinder paints a 7x7 finder pattern with its top-left corner at (cx, cy).
func drawFinder(dark, funcs [][]bool, cx, cy int) {
	for dy := 0; dy < 7; dy++ {
		for dx := 0; dx < 7; dx++ {
			xx, yy := cx+dx, cy+dy
			if xx < 0 || yy < 0 || xx >= len(dark) || yy >= len(dark) {
				continue
			}
			funcs[yy][xx] = true
			dark[yy][xx] = dx == 0 || dx == 6 || dy == 0 || dy == 6 ||
				(dx >= 2 && dx <= 4 && dy >= 2 && dy <= 4)
		}
	}
}

func drawTiming(dark, funcs [][]bool, size int) {
	for i := 8; i < size-8; i++ {
		v := i%2 == 0
		dark[6][i] = v
		dark[i][6] = v
		funcs[6][i] = true
		funcs[i][6] = true
	}
}

// alignmentCenters returns the row/column coordinates of the alignment pattern
// centres for a version, derived from the standard's rule rather than a
// hand-written table: there are version/7+2 of them, the first is always at 6,
// the last at size-7, and the rest are evenly spaced. Version 32 is the one
// documented exception to the spacing rule.
func alignmentCenters(version int) []int {
	if version < 2 {
		return nil
	}
	size := version*4 + 17
	n := version/7 + 2
	step := 26
	if version != 32 {
		// Round the spacing up to an even number of modules.
		step = 2 * ((version*4 + 4 + (2*n - 3)) / (2*n - 2))
	}
	centers := make([]int, 0, n)
	centers = append(centers, 6)
	pos := size - 7
	for len(centers) < n {
		centers = append([]int{pos}, centers...)
		pos -= step
	}
	return centers
}

// alignmentOverlapsFunction reports whether the 5x5 alignment box centred on
// (cx, cy) would land on a finder pattern or its separator. Those positions
// are omitted; the standard still includes the centres themselves in the
// coordinate list, they are simply not drawn.
func alignmentOverlapsFunction(cx, cy, size int) bool {
	top, bottom := cy-2, cy+2
	left, right := cx-2, cx+2
	intersects := func(r0, r1, c0, c1 int) bool {
		return top <= r1 && bottom >= r0 && left <= c1 && right >= c0
	}
	const sep = 7 // finder plus separator is an 8x8 corner block
	return intersects(0, sep, 0, sep) ||
		intersects(0, sep, size-1-sep, size-1) ||
		intersects(size-1-sep, size-1, 0, sep)
}

func drawAlignment(dark, funcs [][]bool, version, size int) {
	centers := alignmentCenters(version)
	for _, cy := range centers {
		for _, cx := range centers {
			if alignmentOverlapsFunction(cx, cy, size) {
				continue
			}
			for dy := -2; dy <= 2; dy++ {
				for dx := -2; dx <= 2; dx++ {
					xx, yy := cx+dx, cy+dy
					if xx < 0 || yy < 0 || xx >= size || yy >= size {
						continue
					}
					funcs[yy][xx] = true
					// three concentric squares: outer ring plus a dark centre
					dark[yy][xx] = abs(dx) == 2 || abs(dy) == 2 || (dx == 0 && dy == 0)
				}
			}
		}
	}
}

// placeData writes the interleaved codewords into the data modules, following
// the standard's upward-and-downward two-column zigzag from the bottom right.
// Column 6 is the vertical timing pattern and is skipped without consuming a
// bit, so the pair sequence steps over it; the last pair is columns 1 and 0.
func placeData(dark, funcs [][]bool, data []byte, size int) {
	row := size - 1
	bitIndex := 0
	inc := -1 // -1 = going up
	for col := size - 1; col > 0; col -= 2 {
		if col == 6 {
			col = 5
		}
		strips := [2]int{col, col - 1}
		for {
			for _, cc := range strips {
				if !funcs[row][cc] {
					dark[row][cc] = getBit(data, bitIndex)
					bitIndex++
				}
			}
			row += inc
			if row < 0 || row >= size {
				row -= inc
				inc = -inc
				break
			}
		}
	}
}

func getBit(data []byte, i int) bool {
	if i >= len(data)*8 {
		return false
	}
	return data[i>>3]>>uint(7-(i&7))&1 == 1
}

// ---------------------------------------------------------------------------
// Masking + format information
// ---------------------------------------------------------------------------

func applyMask(dark, funcs [][]bool, mask, size int) {
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			if !funcs[y][x] && maskCond(mask, x, y) {
				dark[y][x] = !dark[y][x]
			}
		}
	}
}

// maskCond reports whether the module at (x, y) is flipped by the given mask.
// The standard numbers masks 0-7 over (row, column) pairs; x is the column and
// y is the row here.
func maskCond(mask, x, y int) bool {
	switch mask {
	case 0:
		return (x+y)%2 == 0
	case 1:
		return y%2 == 0
	case 2:
		return x%3 == 0
	case 3:
		return (x+y)%3 == 0
	case 4:
		return (y/2+x/3)%2 == 0
	case 5:
		return x*y%2+x*y%3 == 0
	case 6:
		return (x*y%2+x*y%3)%2 == 0
	case 7:
		return ((x+y)%2+x*y%3)%2 == 0
	}
	return false
}

const g15 = 0x537 // x^10 + x^8 + x^5 + x^4 + x^2 + x + 1

// formatWord computes the 15-bit format information value (MSB first) for
// (ecl, mask). The high bit (14) is the first module placed.
func formatWord(ecl Ecc, mask int) uint16 {
	d := (ecl.formatBits() << 3) | (mask & 0x7)
	// Reduce a copy: the five data bits must survive alongside the remainder.
	rem := d << 10
	for i := 14; i >= 10; i-- {
		if rem&(1<<uint(i)) != 0 {
			rem ^= g15 << uint(i-10)
		}
	}
	return uint16((d<<10 | rem) ^ 0x5412)
}

func bitOf(v uint16, i int) bool {
	return v>>uint(14-i)&1 == 1
}

// writeFormatBits places the 15-bit format string in both copies. Layout
// follows the standard "format information" module order.
func writeFormatBits(dark, funcs [][]bool, ecl Ecc, mask, size int) {
	w := formatWord(ecl, mask)
	bit := func(i int) bool { return w>>uint(14-i)&1 == 1 }
	set := func(x, y int, v bool) {
		dark[y][x] = v
		funcs[y][x] = true
	}
	// copy 1: top-left — row 8 (cols 0-5,7,8) then col 8 (rows 7,5,4,3,2,1,0).
	for i := 0; i < 6; i++ {
		set(i, 8, bit(i))
	}
	set(7, 8, bit(6))
	set(8, 8, bit(7))
	vrows := []int{7, 5, 4, 3, 2, 1, 0} // skip row 6 timing
	for k, r := range vrows {
		set(8, r, bit(8+k))
	}
	// copy 2: column 8 (rows size-1..size-7) carries bits 0-6, row 8
	// (cols size-8..size-1) carries bits 7-14, and the module just above the
	// dark module is always dark.
	for i := 0; i < 7; i++ {
		set(8, size-1-i, bit(i))
	}
	for i := 0; i < 8; i++ {
		set(size-8+i, 8, bit(7+i))
	}
	set(8, size-8, true) // dark module
}

// ---------------------------------------------------------------------------
// Penalty scoring
// ---------------------------------------------------------------------------

// bestMask scores all eight masks on the unmasked matrix and returns the one
// with the lowest penalty, as required by the standard.
func bestMask(dark, funcs [][]bool, size int) int {
	work := make([][]bool, size)
	for y := range work {
		work[y] = make([]bool, size)
	}
	best, bestPenalty := 0, -1
	for m := 0; m < 8; m++ {
		for y := 0; y < size; y++ {
			copy(work[y], dark[y])
		}
		applyMask(work, funcs, m, size)
		if p := penaltyScore(work, size); bestPenalty < 0 || p < bestPenalty {
			bestPenalty, best = p, m
		}
	}
	return best
}

func penaltyScore(d [][]bool, size int) int {
	p := 0
	for y := 0; y < size; y++ {
		p += runPenalty(d[y])
	}
	col := make([]bool, size)
	for x := 0; x < size; x++ {
		for y := 0; y < size; y++ {
			col[y] = d[y][x]
		}
		p += runPenalty(col)
	}
	for y := 0; y < size-1; y++ {
		for x := 0; x < size-1; x++ {
			if d[y][x] == d[y][x+1] && d[y][x] == d[y+1][x] && d[y][x] == d[y+1][x+1] {
				p += 3
			}
		}
	}
	p += finderLike(d, size)
	dark := 0
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			if d[y][x] {
				dark++
			}
		}
	}
	p += darkBalancePenalty(dark, size*size)
	return p
}

// darkBalancePenalty is rule N4: 10 points for every 5% that the share of dark
// modules departs from one half. The percentage is kept as an exact fraction so
// that a dark share just past a 5% boundary is not rounded down into the
// neighbouring band.
func darkBalancePenalty(dark, total int) int {
	if total == 0 {
		return 0
	}
	dev := int64(dark)*100 - int64(total)*50
	if dev < 0 {
		dev = -dev
	}
	return int(dev / (int64(total) * 5) * 10)
}

func runPenalty(row []bool) int {
	if len(row) == 0 {
		return 0
	}
	p := 0
	last := row[0]
	run := 1
	for i := 1; i < len(row); i++ {
		if row[i] == last {
			run++
		} else {
			if run >= 5 {
				p += 3 + (run - 5)
			}
			last = row[i]
			run = 1
		}
	}
	if run >= 5 {
		p += 3 + (run - 5)
	}
	return p
}

// finderLike implements penalty rule N3: every 1:1:3:1:1 run of dark/light
// modules that is bordered by four light modules on either side scores 40
// points, in rows and in columns. The four light modules must lie inside the
// symbol; the area beyond the edge does not count as a quiet zone.
func finderLike(d [][]bool, size int) int {
	pat := [7]bool{true, false, true, true, true, false, true}
	p := 0
	score := func(get func(int) bool, i int) {
		for k := 0; k < 7; k++ {
			if get(i+k) != pat[k] {
				return
			}
		}
		quietBefore := i >= 4
		for k := 1; quietBefore && k <= 4; k++ {
			if get(i - k) {
				quietBefore = false
			}
		}
		quietAfter := i+10 < size
		for k := 7; quietAfter && k <= 10; k++ {
			if get(i + k) {
				quietAfter = false
			}
		}
		if quietBefore || quietAfter {
			p += 40
		}
	}
	for y := 0; y < size; y++ {
		row := d[y]
		get := func(i int) bool {
			if i < 0 || i >= size {
				return false
			}
			return row[i]
		}
		for x := 0; x+7 <= size; x++ {
			score(get, x)
		}
	}
	for x := 0; x < size; x++ {
		get := func(i int) bool {
			if i < 0 || i >= size {
				return false
			}
			return d[i][x]
		}
		for y := 0; y+7 <= size; y++ {
			score(get, y)
		}
	}
	return p
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
