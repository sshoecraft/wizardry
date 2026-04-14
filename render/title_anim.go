package render

import (
	"encoding/binary"
	"image/color"

	"github.com/gdamore/tcell/v2"
)

// Apple II hi-res page: 8192 bytes, 192 lines × 40 bytes, 280×192 pixels.
const hiresSize = 8192

// WTAnimation holds the state for the Wizardry title screen animation.
// Decodes the WT (Wizardry Title) compressed animation data and renders
// to an Apple II hi-res framebuffer, then converts to pixels for display.
type WTAnimation struct {
	wtData    []byte         // raw WT file data (9216 bytes)
	offsets   [33]int        // byte offsets for each of the 33 sections
	hires     [hiresSize]byte // Apple II hi-res framebuffer
	lineAddr  [192]int       // offset into hires[] for each scan line
}

// NewWTAnimation creates an animation engine from raw WT file data.
func NewWTAnimation(wtData []byte) *WTAnimation {
	a := &WTAnimation{
		wtData: wtData,
	}

	// Parse the 33-entry offset table (little-endian 16-bit integers)
	for i := 0; i < 33; i++ {
		a.offsets[i] = int(binary.LittleEndian.Uint16(wtData[i*2 : i*2+2]))
	}

	// Build scan line address table from SCREENPT assembly data.
	// L04BC (low bytes) and L057C (high bytes) give the Apple II memory
	// address for each of 192 scan lines. We subtract $2000 to get offsets
	// into our 8192-byte buffer.
	l04bc := [192]byte{
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80,
		0x28, 0x28, 0x28, 0x28, 0x28, 0x28, 0x28, 0x28,
		0xA8, 0xA8, 0xA8, 0xA8, 0xA8, 0xA8, 0xA8, 0xA8,
		0x28, 0x28, 0x28, 0x28, 0x28, 0x28, 0x28, 0x28,
		0xA8, 0xA8, 0xA8, 0xA8, 0xA8, 0xA8, 0xA8, 0xA8,
		0x28, 0x28, 0x28, 0x28, 0x28, 0x28, 0x28, 0x28,
		0xA8, 0xA8, 0xA8, 0xA8, 0xA8, 0xA8, 0xA8, 0xA8,
		0x28, 0x28, 0x28, 0x28, 0x28, 0x28, 0x28, 0x28,
		0xA8, 0xA8, 0xA8, 0xA8, 0xA8, 0xA8, 0xA8, 0xA8,
		0x50, 0x50, 0x50, 0x50, 0x50, 0x50, 0x50, 0x50,
		0xD0, 0xD0, 0xD0, 0xD0, 0xD0, 0xD0, 0xD0, 0xD0,
		0x50, 0x50, 0x50, 0x50, 0x50, 0x50, 0x50, 0x50,
		0xD0, 0xD0, 0xD0, 0xD0, 0xD0, 0xD0, 0xD0, 0xD0,
		0x50, 0x50, 0x50, 0x50, 0x50, 0x50, 0x50, 0x50,
		0xD0, 0xD0, 0xD0, 0xD0, 0xD0, 0xD0, 0xD0, 0xD0,
		0x50, 0x50, 0x50, 0x50, 0x50, 0x50, 0x50, 0x50,
		0xD0, 0xD0, 0xD0, 0xD0, 0xD0, 0xD0, 0xD0, 0xD0,
	}
	l057c := [192]byte{
		0x20, 0x24, 0x28, 0x2C, 0x30, 0x34, 0x38, 0x3C,
		0x20, 0x24, 0x28, 0x2C, 0x30, 0x34, 0x38, 0x3C,
		0x21, 0x25, 0x29, 0x2D, 0x31, 0x35, 0x39, 0x3D,
		0x21, 0x25, 0x29, 0x2D, 0x31, 0x35, 0x39, 0x3D,
		0x22, 0x26, 0x2A, 0x2E, 0x32, 0x36, 0x3A, 0x3E,
		0x22, 0x26, 0x2A, 0x2E, 0x32, 0x36, 0x3A, 0x3E,
		0x23, 0x27, 0x2B, 0x2F, 0x33, 0x37, 0x3B, 0x3F,
		0x23, 0x27, 0x2B, 0x2F, 0x33, 0x37, 0x3B, 0x3F,
		0x20, 0x24, 0x28, 0x2C, 0x30, 0x34, 0x38, 0x3C,
		0x20, 0x24, 0x28, 0x2C, 0x30, 0x34, 0x38, 0x3C,
		0x21, 0x25, 0x29, 0x2D, 0x31, 0x35, 0x39, 0x3D,
		0x21, 0x25, 0x29, 0x2D, 0x31, 0x35, 0x39, 0x3D,
		0x22, 0x26, 0x2A, 0x2E, 0x32, 0x36, 0x3A, 0x3E,
		0x22, 0x26, 0x2A, 0x2E, 0x32, 0x36, 0x3A, 0x3E,
		0x23, 0x27, 0x2B, 0x2F, 0x33, 0x37, 0x3B, 0x3F,
		0x23, 0x27, 0x2B, 0x2F, 0x33, 0x37, 0x3B, 0x3F,
		0x20, 0x24, 0x28, 0x2C, 0x30, 0x34, 0x38, 0x3C,
		0x20, 0x24, 0x28, 0x2C, 0x30, 0x34, 0x38, 0x3C,
		0x21, 0x25, 0x29, 0x2D, 0x31, 0x35, 0x39, 0x3D,
		0x21, 0x25, 0x29, 0x2D, 0x31, 0x35, 0x39, 0x3D,
		0x22, 0x26, 0x2A, 0x2E, 0x32, 0x36, 0x3A, 0x3E,
		0x22, 0x26, 0x2A, 0x2E, 0x32, 0x36, 0x3A, 0x3E,
		0x23, 0x27, 0x2B, 0x2F, 0x33, 0x37, 0x3B, 0x3F,
		0x23, 0x27, 0x2B, 0x2F, 0x33, 0x37, 0x3B, 0x3F,
	}
	for i := 0; i < 192; i++ {
		addr := int(l057c[i])<<8 | int(l04bc[i])
		a.lineAddr[i] = addr - 0x2000
	}

	return a
}

// ClearHires zeros the entire hi-res framebuffer.
func (a *WTAnimation) ClearHires() {
	a.hires = [hiresSize]byte{}
}

// DrawSection decompresses WT section `idx` into the hi-res framebuffer.
// Ported from 6502 assembly LZDECOMP (LZDECOMP.TEXT).
func (a *WTAnimation) DrawSection(idx int) {
	if idx < 0 || idx >= 33 {
		return
	}
	offset := a.offsets[idx]
	if offset >= len(a.wtData) {
		return
	}
	data := a.wtData
	pos := offset

	for pos < len(data) {
		cmd := data[pos]

		if cmd == 0xFF {
			// End of section
			return
		}

		if cmd == 0xFD {
			// XOR toggle: flip bit 7 of all 40 bytes on each of lines 0-48
			a.XorToggle()
			return
		}

		if cmd == 0xFE {
			// Scroll: rotate bytes left on each of lines 0-48
			a.ScrollLeft()
			return
		}

		if cmd >= 0xC0 {
			// Sparse: single byte write
			// cmd-0xC0 = column offset, next byte = line index, next = value
			colOff := int(cmd - 0xC0)
			if pos+2 >= len(data) {
				return
			}
			lineIdx := int(data[pos+1])
			val := data[pos+2]
			if lineIdx < 192 {
				addr := a.lineAddr[lineIdx] + colOff
				if addr >= 0 && addr < hiresSize {
					a.hires[addr] = val
				}
			}
			pos += 3
		} else {
			// Dense: 5-mask command
			// cmd = line index, next 5 bytes = bitmasks, then data bytes
			lineIdx := int(cmd)
			if pos+5 >= len(data) {
				return
			}
			masks := [5]byte{data[pos+1], data[pos+2], data[pos+3], data[pos+4], data[pos+5]}
			pos += 6

			if lineIdx >= 192 {
				// Skip data bytes for this invalid line
				for _, m := range masks {
					for bit := 0; bit < 8; bit++ {
						if m&(1<<uint(bit)) != 0 {
							pos++
						}
					}
				}
				continue
			}

			baseAddr := a.lineAddr[lineIdx]
			col := 0
			for _, mask := range masks {
				for bit := 0; bit < 8; bit++ {
					if mask&(1<<uint(bit)) != 0 {
						if pos < len(data) {
							addr := baseAddr + col
							if addr >= 0 && addr < hiresSize {
								a.hires[addr] = data[pos]
							}
							pos++
						}
					}
					col++
				}
			}
		}
	}
}

// XorToggle flips bit 7 of all 40 bytes on scan lines 0-48.
// From assembly at L5264/L0159: EOR #$80 for bytes $00-$27 on each line.
func (a *WTAnimation) XorToggle() {
	for line := 0; line <= 48; line++ {
		base := a.lineAddr[line]
		for col := 0; col < 40; col++ {
			addr := base + col
			if addr >= 0 && addr < hiresSize {
				a.hires[addr] ^= 0x80
			}
		}
	}
}

// ScrollLeft rotates bytes left by 1 on scan lines 0-48.
// From assembly at L5291/L0177: byte[0] saved, shift left, saved → byte[39].
func (a *WTAnimation) ScrollLeft() {
	for line := 0; line <= 48; line++ {
		base := a.lineAddr[line]
		if base < 0 || base+39 >= hiresSize {
			continue
		}
		saved := a.hires[base]
		for col := 0; col < 39; col++ {
			a.hires[base+col] = a.hires[base+col+1]
		}
		a.hires[base+39] = saved
	}
}

// ToPixels converts the hi-res framebuffer to a 280×192 monochrome pixel grid.
// Each byte has 7 data bits (0-6), bit 7 is the color palette bit (ignored for mono).
// Bit 0 = leftmost pixel of the byte.
func (a *WTAnimation) ToPixels() [192][280]bool {
	var pixels [192][280]bool
	for line := 0; line < 192; line++ {
		base := a.lineAddr[line]
		for byteIdx := 0; byteIdx < 40; byteIdx++ {
			addr := base + byteIdx
			if addr < 0 || addr >= hiresSize {
				continue
			}
			b := a.hires[addr]
			for bit := 0; bit < 7; bit++ {
				if b&(1<<uint(bit)) != 0 {
					px := byteIdx*7 + bit
					if px < 280 {
						pixels[line][px] = true
					}
				}
			}
		}
	}
	return pixels
}

// RenderToSixel converts the current framebuffer to a full-screen sixel image.
func (a *WTAnimation) RenderToSixel() *SixelImage {
	cw := CellWidth
	ch := CellHeight
	imgW := 80 * cw
	imgH := 24 * ch
	if imgH%6 != 0 {
		imgH += 6 - imgH%6
	}

	si := NewSixelImage(imgW, imgH)

	// Scale 280×192 to imgW×imgH, centered, preserving aspect ratio
	scaleX := float64(imgW) / 280.0
	scaleY := float64(imgH) / 192.0
	sc := scaleX
	if scaleY < sc {
		sc = scaleY
	}
	dstW := int(280.0 * sc)
	dstH := int(192.0 * sc)
	ox := (imgW - dstW) / 2
	oy := (imgH - dstH) / 2

	if ColorMode {
		// NTSC artifact color from raw framebuffer
		colorPixels := HiResToColorPixels(a.hires[:])
		for py := 0; py < dstH; py++ {
			srcY := py * 192 / dstH
			if srcY >= 192 {
				continue
			}
			for px := 0; px < dstW; px++ {
				srcX := px * 280 / dstW
				if srcX < 280 {
					c := colorPixels[srcY][srcX]
					if c.R != 0 || c.G != 0 || c.B != 0 {
						si.SetPixel(ox+px, oy+py, c)
					}
				}
			}
		}
	} else {
		fc := sixelFG
		pixels := a.ToPixels()
		for py := 0; py < dstH; py++ {
			srcY := py * 192 / dstH
			if srcY >= 192 {
				continue
			}
			for px := 0; px < dstW; px++ {
				srcX := px * 280 / dstW
				if srcX < 280 && pixels[srcY][srcX] {
					si.SetPixel(ox+px, oy+py, fc)
				}
			}
		}
	}

	return si
}

// RenderToCanvas converts the current framebuffer to half-block Unicode art
// on an 80×48 canvas (80 cols × 24 rows × 2 half-block pixels).
func (a *WTAnimation) RenderToCanvas() *Canvas {
	pixels := a.ToPixels()
	canvasW := 80
	canvasH := 48
	canvas := NewCanvas(canvasW, canvasH)

	// Max-pooling scale from 280×192 to 80×48
	for cy := 0; cy < canvasH; cy++ {
		syStart := cy * 192 / canvasH
		syEnd := (cy + 1) * 192 / canvasH
		if syEnd == syStart {
			syEnd = syStart + 1
		}
		for cx := 0; cx < canvasW; cx++ {
			sxStart := cx * 280 / canvasW
			sxEnd := (cx + 1) * 280 / canvasW
			if sxEnd == sxStart {
				sxEnd = sxStart + 1
			}
			hit := false
			for sy := syStart; sy < syEnd && sy < 192; sy++ {
				for sx := sxStart; sx < sxEnd && sx < 280; sx++ {
					if pixels[sy][sx] {
						hit = true
						break
					}
				}
				if hit {
					break
				}
			}
			if hit {
				canvas.Set(cx, cy)
			}
		}
	}

	return canvas
}

// EmitSixelFrame renders the current framebuffer as sixel and writes it.
func (a *WTAnimation) EmitSixelFrame() {
	si := a.RenderToSixel()
	WriteSixel(0, si.Encode())
}

// EmitCanvasFrame renders the current framebuffer as half-block art to a tcell screen.
// In color mode, uses NTSC artifact colors from the raw Hi-Res framebuffer.
func (a *WTAnimation) EmitCanvasFrame(s *Screen, style tcell.Style) {
	if ColorMode {
		a.emitCanvasFrameColor(s)
		return
	}
	canvas := a.RenderToCanvas()
	for row := 0; row < 24; row++ {
		pyTop := row * 2
		pyBot := row*2 + 1
		for col := 0; col < 80; col++ {
			top := canvas.Get(col, pyTop)
			bot := canvas.Get(col, pyBot)
			var ch rune
			switch {
			case top && bot:
				ch = '\u2588'
			case top:
				ch = '\u2580'
			case bot:
				ch = '\u2584'
			default:
				continue
			}
			s.tcell.SetContent(col, row, ch, nil, style)
		}
	}
}

// emitCanvasFrameColor renders the framebuffer with NTSC artifact colors.
// Each terminal cell covers a 3.5x8 pixel region of the 280x192 source.
// Top/bottom half-block halves each get the dominant color from their 4 source rows.
func (a *WTAnimation) emitCanvasFrameColor(s *Screen) {
	colorPixels := HiResToColorPixels(a.hires[:])
	black := tcell.StyleDefault.Background(tcell.ColorBlack)

	for row := 0; row < 24; row++ {
		// Each terminal row covers 8 source rows (192/24), split into top 4 and bottom 4
		srcYTop := row * 8
		srcYBot := row*8 + 4
		for col := 0; col < 80; col++ {
			// Each terminal col covers 3.5 source pixels (280/80)
			srcXStart := col * 280 / 80
			srcXEnd := (col + 1) * 280 / 80
			if srcXEnd <= srcXStart {
				srcXEnd = srcXStart + 1
			}

			topColor := dominantColor(colorPixels, srcXStart, srcXEnd, srcYTop, srcYTop+4)
			botColor := dominantColor(colorPixels, srcXStart, srcXEnd, srcYBot, srcYBot+4)

			topOn := topColor != (tcell.Color(0))
			botOn := botColor != (tcell.Color(0))

			if !topOn && !botOn {
				continue
			}

			var ch rune
			var st tcell.Style
			switch {
			case topOn && botOn:
				ch = '\u2588'
				if topColor == botColor {
					st = black.Foreground(topColor)
				} else {
					// Upper half-block with fg=top, bg=bottom
					st = tcell.StyleDefault.Foreground(topColor).Background(botColor)
				}
			case topOn:
				ch = '\u2580'
				st = black.Foreground(topColor)
			case botOn:
				ch = '\u2584'
				st = black.Foreground(botColor)
			}
			s.tcell.SetContent(col, row, ch, nil, st)
		}
	}
}

// dominantColor finds the most common non-black NTSC color in a pixel region.
// Returns a tcell.Color, or 0 if the region is all black.
func dominantColor(pixels [][]color.RGBA, x0, x1, y0, y1 int) tcell.Color {
	type rgb struct{ r, g, b uint8 }
	counts := make(map[rgb]int)
	for y := y0; y < y1 && y < len(pixels); y++ {
		for x := x0; x < x1 && x < len(pixels[y]); x++ {
			c := pixels[y][x]
			if c.R == 0 && c.G == 0 && c.B == 0 {
				continue
			}
			counts[rgb{c.R, c.G, c.B}]++
		}
	}
	if len(counts) == 0 {
		return 0
	}
	var best rgb
	bestN := 0
	for c, n := range counts {
		if n > bestN {
			best = c
			bestN = n
		}
	}
	return tcell.NewRGBColor(int32(best.r), int32(best.g), int32(best.b))
}

// dummy use to keep import
var _ tcell.Style
