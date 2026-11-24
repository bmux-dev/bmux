package vt

import (
	"image/color"
	"unicode/utf8"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
)

// PlaceholderRune is the Kitty graphics protocol's Unicode placeholder rune.
const PlaceholderRune = '\U0010EEEE'

// Color is a compact, pointer-free 32-bit packed terminal color.
// The top byte encodes the kind:
//   - 0x00: default (unset)
//   - 0x01: ANSI basic color (0..15) in the low byte
//   - 0x02: 256-color palette index (0..255) in the low byte
//   - 0x03: 24-bit RGB (0x00RRGGBB in the low 24 bits)
type Color uint32

const (
	// ColorDefault represents the unset/default terminal color.
	ColorDefault Color = 0

	colorKindShift   = 24
	colorKindMask    = 0xff << colorKindShift
	colorKindDefault = Color(0) << colorKindShift
	colorKindBasic   = Color(1) << colorKindShift
	colorKindIndexed = Color(2) << colorKindShift
	colorKindRGB     = Color(3) << colorKindShift
)

// BasicColor packs an ANSI basic color index (0..15).
func BasicColor(idx uint8) Color {
	return colorKindBasic | Color(idx)
}

// PaletteColor packs a 256-color palette index (0..255).
func PaletteColor(idx uint8) Color {
	return colorKindIndexed | Color(idx)
}

// RGBColor packs a 24-bit RGB color.
func RGBColor(r, g, b uint8) Color {
	return colorKindRGB | Color(r)<<16 | Color(g)<<8 | Color(b)
}

// ColorFromStd converts a standard Go color.Color into a packed Color.
func ColorFromStd(c color.Color) Color {
	switch v := c.(type) {
	case nil:
		return ColorDefault
	case ansi.BasicColor:
		return BasicColor(uint8(v))
	case ansi.IndexedColor:
		return PaletteColor(uint8(v))
	case ansi.RGBColor:
		return RGBColor(v.R, v.G, v.B)
	case color.RGBA:
		return RGBColor(v.R, v.G, v.B)
	case color.NRGBA:
		return RGBColor(v.R, v.G, v.B)
	default:
		r, g, b, _ := c.RGBA()
		return RGBColor(uint8(r>>8), uint8(g>>8), uint8(b>>8))
	}
}

// IsDefault reports whether c is the default (unset) color.
func (c Color) IsDefault() bool {
	return c == ColorDefault
}

// IsBasic reports whether c is an ANSI basic color (0..15).
func (c Color) IsBasic() bool {
	return c&colorKindMask == colorKindBasic
}

// IsIndexed reports whether c is a palette color (basic 0..15 or 256-color 0..255).
func (c Color) IsIndexed() bool {
	k := c & colorKindMask
	return k == colorKindBasic || k == colorKindIndexed
}

// IsRGB reports whether c is a 24-bit RGB color.
func (c Color) IsRGB() bool {
	return c&colorKindMask == colorKindRGB
}

// Index returns the palette index for basic or indexed colors.
func (c Color) Index() uint8 {
	return uint8(c)
}

// RGB returns the 8-bit red, green, and blue channels for RGB colors.
func (c Color) RGB() (r, g, b uint8) {
	return uint8(c >> 16), uint8(c >> 8), uint8(c)
}

// ToColor converts c back to a color.Color (nil for ColorDefault).
func (c Color) ToColor() color.Color {
	switch c & colorKindMask {
	case colorKindDefault:
		return nil
	case colorKindBasic:
		return ansi.BasicColor(uint8(c))
	case colorKindIndexed:
		return ansi.IndexedColor(uint8(c))
	case colorKindRGB:
		return color.RGBA{R: uint8(c >> 16), G: uint8(c >> 8), B: uint8(c), A: 0xff}
	default:
		return nil
	}
}

// PlaceholderID returns the Kitty graphics image id encoded in a placeholder
// cell's foreground color: the 24-bit RGB value or palette index.
func (c Color) PlaceholderID() uint32 {
	switch c & colorKindMask {
	case colorKindDefault:
		return 0
	case colorKindBasic, colorKindIndexed:
		return uint32(uint8(c))
	case colorKindRGB:
		return uint32(c) & 0xffffff
	default:
		return 0
	}
}

// Style is a compact, pointer-free terminal cell style.
type Style struct {
	Fg             Color
	Bg             Color
	UnderlineColor Color
	Attrs          uint8
	Underline      uv.Underline
}

// IsZero reports whether s is the default plain style.
func (s Style) IsZero() bool {
	return s == (Style{})
}

// StyleFromUV converts a uv.Style into a compact Style.
func StyleFromUV(s uv.Style) Style {
	return Style{
		Fg:             ColorFromStd(s.Fg),
		Bg:             ColorFromStd(s.Bg),
		UnderlineColor: ColorFromStd(s.UnderlineColor),
		Attrs:          s.Attrs,
		Underline:      s.Underline,
	}
}

// ToUV converts s back into a uv.Style.
func (s Style) ToUV() uv.Style {
	if s.IsZero() {
		return uv.Style{}
	}
	return uv.Style{
		Fg:             s.Fg.ToColor(),
		Bg:             s.Bg.ToColor(),
		UnderlineColor: s.UnderlineColor.ToColor(),
		Attrs:          s.Attrs,
		Underline:      s.Underline,
	}
}

// StyleTable deduplicates unique Style values into compact uint32 IDs.
// ID 0 is always the zero/default Style{}.
type StyleTable struct {
	styles  []Style
	index   map[Style]uint32
	version uint64
}

// Intern returns the compact uint32 ID for s, adding it if new.
func (t *StyleTable) Intern(s Style) uint32 {
	if s.IsZero() {
		return 0
	}
	if t.index == nil {
		t.styles = []Style{{}}
		t.index = make(map[Style]uint32, 16)
		if t.version == 0 {
			t.version = 1
		}
	}
	if id, ok := t.index[s]; ok {
		return id
	}
	id := uint32(len(t.styles))
	t.styles = append(t.styles, s)
	t.index[s] = id
	return id
}

// Get returns the Style for id, or Style{} for 0 or out-of-range IDs.
func (t *StyleTable) Get(id uint32) Style {
	if t == nil || id == 0 || int(id) >= len(t.styles) {
		return Style{}
	}
	return t.styles[id]
}

// Len returns the number of interned styles (including ID 0).
func (t *StyleTable) Len() int {
	if t == nil || len(t.styles) == 0 {
		return 1
	}
	return len(t.styles)
}

// Version returns a monotonic generation counter incremented only if style IDs
// are compacted/renumbered.
func (t *StyleTable) Version() uint64 {
	if t == nil || t.version == 0 {
		return 1
	}
	return t.version
}

// Cell is a compact, pointer-free 12-byte terminal cell stored in screen and
// scrollback backing arrays. Because it contains zero pointers, Go allocates
// []Cell in noscan spans so the GC never scans cell grids or scrollback.
//
// The zero value Cell{} represents a wide-character trailing continuation cell
// (Width() == 0, IsZero() == true). EmptyCell represents a blank space cell
// (Rune == ' ', Width() == 1, StyleID == 0).
type Cell struct {
	// Rune holds the cell's single rune, or the first rune when HasGrapheme()
	// is true (full cluster stored in the row's sparse side-table).
	Rune rune
	// StyleID indexes into the emulator/screen StyleTable (0 = default Style{}).
	StyleID uint32
	// Flags encodes display width in bits 0..2 and side-table presence bits.
	Flags uint16
	// Extra is reserved for future inline metadata (0).
	Extra uint16
}

const (
	cellWidthMask    uint16 = 0x0007
	cellWidth1       uint16 = 1
	cellWidth2       uint16 = 2
	cellFlagGrapheme uint16 = 1 << 3
	cellFlagLink     uint16 = 1 << 4
	cellFlagHasExtra uint16 = cellFlagGrapheme | cellFlagLink
)

// EmptyCell is a blank default-styled space cell.
var EmptyCell = Cell{Rune: ' ', Flags: cellWidth1}

func cellWidthFlags(w int) uint16 {
	if w <= 0 {
		return 0
	}
	if w > int(cellWidthMask) {
		return cellWidthMask
	}
	return uint16(w)
}

// IsZero reports whether c is the zero cell (wide-character continuation).
func (c Cell) IsZero() bool {
	return c == (Cell{})
}

// IsEmpty reports whether c is a plain default-styled blank space cell.
func (c Cell) IsEmpty() bool {
	return c == EmptyCell
}

// Width returns the cell's display column width (0 for wide continuation cells).
func (c Cell) Width() int {
	return int(c.Flags & cellWidthMask)
}

// HasGrapheme reports whether the cell's full multi-rune grapheme cluster is
// stored in the sparse side-table.
func (c Cell) HasGrapheme() bool {
	return c.Flags&cellFlagGrapheme != 0
}

// HasLink reports whether the cell carries an OSC 8 hyperlink in the sparse
// side-table.
func (c Cell) HasLink() bool {
	return c.Flags&cellFlagLink != 0
}

// HasExtra reports whether the cell has an entry in the sparse side-table.
func (c Cell) HasExtra() bool {
	return c.Flags&cellFlagHasExtra != 0
}

// IsPlaceholder reports whether c is a Kitty graphics Unicode placeholder cell.
func (c Cell) IsPlaceholder() bool {
	return c.Rune == PlaceholderRune
}

// cellExtra holds rare per-cell heap metadata (multi-rune grapheme clusters
// and OSC 8 hyperlinks) outside the pointer-free Cell grid.
type cellExtra struct {
	grapheme string
	link     uv.Link
}

// rowExtra maps column index -> cellExtra for the rare cells in a row that
// have a multi-rune grapheme cluster or hyperlink.
type rowExtra map[int]cellExtra

// cloneRowExtra copies entries with column < limit into a new rowExtra, or
// returns nil if none match.
func cloneRowExtra(src rowExtra, limit int) rowExtra {
	if len(src) == 0 {
		return nil
	}
	var dst rowExtra
	for x, ex := range src {
		if x >= 0 && x < limit {
			if dst == nil {
				dst = make(rowExtra, min(len(src), limit))
			}
			dst[x] = ex
		}
	}
	return dst
}

// compactFromUV converts a *uv.Cell into a compact Cell plus optional extra.
// A nil *uv.Cell maps to EmptyCell.
func compactFromUV(c *uv.Cell, styles *StyleTable) (Cell, cellExtra, bool) {
	if c == nil {
		return EmptyCell, cellExtra{}, false
	}
	if c.IsZero() {
		return Cell{}, cellExtra{}, false
	}
	var styleID uint32
	if !c.Style.IsZero() && styles != nil {
		styleID = styles.Intern(StyleFromUV(c.Style))
	}
	flags := cellWidthFlags(c.Width)
	var r rune
	var ex cellExtra
	hasExtra := false
	if c.Content != "" {
		var size int
		r, size = utf8.DecodeRuneInString(c.Content)
		if size < len(c.Content) {
			flags |= cellFlagGrapheme
			ex.grapheme = c.Content
			hasExtra = true
		}
	}
	if !c.Link.IsZero() {
		flags |= cellFlagLink
		ex.link = c.Link
		hasExtra = true
	}
	return Cell{Rune: r, StyleID: styleID, Flags: flags}, ex, hasExtra
}

// uvFromCompact reconstructs a uv.Cell value from a compact Cell, StyleTable,
// and optional rowExtra map.
func uvFromCompact(c Cell, x int, ex rowExtra, styles *StyleTable) uv.Cell {
	if c.IsZero() {
		return uv.Cell{}
	}
	out := uv.Cell{
		Width: c.Width(),
	}
	if c.StyleID != 0 {
		out.Style = styles.Get(c.StyleID).ToUV()
	}
	if c.HasExtra() && ex != nil {
		ce := ex[x]
		if c.HasGrapheme() && ce.grapheme != "" {
			out.Content = ce.grapheme
		} else if c.Rune != 0 {
			out.Content = runeString(c.Rune)
		}
		if c.HasLink() {
			out.Link = ce.link
		}
		return out
	}
	if c.Rune != 0 {
		out.Content = runeString(c.Rune)
	}
	return out
}

// asciiStrings avoids allocating a 1-byte string when reconstructing uv.Cell
// for ASCII runes in legacy CellAt callers.
var asciiStrings [128]string

func init() {
	for i := range asciiStrings {
		asciiStrings[i] = string(byte(i))
	}
}

func runeString(r rune) string {
	if r >= 0 && r < 128 {
		return asciiStrings[r]
	}
	return string(r)
}

// isWideContinuation reports whether row[idx] is a zero continuation cell
// belonging to a preceding wide character.
func isWideContinuation(row []Cell, idx int) bool {
	if idx <= 0 || !row[idx].IsZero() {
		return false
	}
	for j := 1; idx-j >= 0 && j < int(cellWidthMask); j++ {
		if w := row[idx-j].Width(); w > 1 {
			return j < w
		}
		if !row[idx-j].IsZero() {
			return false
		}
	}
	return false
}

// trimCompactRow drops trailing empty or orphan zero cells from a row, while
// preserving the trailing continuation cell of a wide character at the end.
func trimCompactRow(row []Cell) []Cell {
	end := len(row)
	for end > 0 {
		c := row[end-1]
		if c.IsEmpty() || (c.IsZero() && !isWideContinuation(row, end-1)) {
			end--
			continue
		}
		break
	}
	return row[:end]
}

// isCompactRowEmpty reports whether every cell in row is blank/space or zero.
func isCompactRowEmpty(row []Cell) bool {
	for _, c := range row {
		if c.HasGrapheme() {
			return false
		}
		if c.Rune != 0 && c.Rune != ' ' {
			return false
		}
	}
	return true
}

// newCompactRow allocates a row of width EmptyCells.
func newCompactRow(width int) []Cell {
	if width <= 0 {
		return nil
	}
	row := make([]Cell, width)
	for i := range row {
		row[i] = EmptyCell
	}
	return row
}
