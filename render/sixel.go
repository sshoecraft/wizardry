package render

import (
	"fmt"
	"image"
	"image/color"
	"os"
	"strings"
	"time"

	"golang.org/x/term"
)

// SixelSupported is set on startup after querying the terminal.
var SixelSupported bool

// ColorMode enables Apple II NTSC artifact color rendering for sixel graphics.
var ColorMode bool

// Apple II NTSC artifact colors.
// Each pixel's color depends on its screen position and the byte's palette bit (bit 7).
var (
	ntscBlack  = color.RGBA{0, 0, 0, 255}
	ntscPurple = color.RGBA{255, 68, 253, 255}
	ntscGreen  = color.RGBA{20, 245, 60, 255}
	ntscBlue   = color.RGBA{20, 207, 253, 255}
	ntscOrange = color.RGBA{255, 106, 60, 255}
	ntscWhite  = color.RGBA{255, 255, 255, 255}
)

// HiResToColorPixels converts an 8192-byte Apple II Hi-Res framebuffer to
// a 280x192 color pixel grid using NTSC artifact color simulation.
//
// Algorithm: each byte has 7 data bits (0-6) and a palette bit (7).
// Adjacent set bits produce white. A lone set bit produces a color
// based on its screen X position (even=purple/blue, odd=green/orange)
// and the palette bit.
// hiresLineAddr maps Apple II scanline number to byte offset within the 8K framebuffer.
var hiresLineAddr [192]int

func init() {
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
		hiresLineAddr[i] = addr - 0x2000
	}
}

func HiResToColorPixels(hires []byte) [][]color.RGBA {
	pixels := make([][]color.RGBA, 192)
	for line := 0; line < 192; line++ {
		pixels[line] = make([]color.RGBA, 280)
		base := hiresLineAddr[line]

		// First pass: extract pixel on/off and palette per pixel
		var on [280]bool
		var pal [280]byte
		for byteIdx := 0; byteIdx < 40; byteIdx++ {
			addr := base + byteIdx
			if addr < 0 || addr >= len(hires) {
				continue
			}
			b := hires[addr]
			p := (b >> 7) & 1
			for bit := 0; bit < 7; bit++ {
				px := byteIdx*7 + bit
				if px < 280 {
					on[px] = b&(1<<uint(bit)) != 0
					pal[px] = p
				}
			}
		}

		// Second pass: assign colors with NTSC artifact pattern detection.
		// Adjacent ON pixels → white.
		// Alternating ON/off/ON pattern → solid color (NTSC smears them).
		// Lone ON pixel → colored by position and palette.
		px := 0
		for px < 280 {
			if !on[px] {
				// pixels[line][px] is already zero (black)
				px++
				continue
			}

			nxt := px < 279 && on[px+1]
			if nxt {
				// Adjacent pair → white. Extend through all consecutive ON pixels.
				for px < 280 && on[px] {
					pixels[line][px] = ntscWhite
					px++
				}
				continue
			}

			// This pixel is ON, next is OFF. Scan for alternating pattern.
			end := px
			for end+2 < 280 && !on[end+1] && on[end+2] {
				end += 2
			}

			if end > px {
				// Alternating pattern — NTSC smears into solid color
				var c color.RGBA
				if pal[px] == 0 {
					if px%2 == 0 {
						c = ntscPurple
					} else {
						c = ntscGreen
					}
				} else {
					if px%2 == 0 {
						c = ntscBlue
					} else {
						c = ntscOrange
					}
				}
				// Fill entire region (ON and OFF pixels) with solid color
				for fill := px; fill <= end; fill++ {
					pixels[line][fill] = c
				}
				px = end + 1
			} else {
				// Lone pixel
				if pal[px] == 0 {
					if px%2 == 0 {
						pixels[line][px] = ntscPurple
					} else {
						pixels[line][px] = ntscGreen
					}
				} else {
					if px%2 == 0 {
						pixels[line][px] = ntscBlue
					} else {
						pixels[line][px] = ntscOrange
					}
				}
				px++
			}
		}
	}
	return pixels
}

// DetectSixel queries the terminal for sixel support via DA1 (Primary Device Attributes).
// Sends ESC[c and checks if attribute 4 (sixel) is in the response.
func DetectSixel() bool {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return false
	}

	oldState, err := term.MakeRaw(fd)
	if err != nil {
		return false
	}
	defer term.Restore(fd, oldState)

	// Send DA1 query
	os.Stdout.Write([]byte("\x1b[c"))
	os.Stdout.Sync()

	// Read response with timeout using a goroutine
	type readResult struct {
		data []byte
		err  error
	}
	ch := make(chan readResult, 1)
	go func() {
		buf := make([]byte, 128)
		total := 0
		for total < len(buf) {
			n, err := os.Stdin.Read(buf[total:])
			if n > 0 {
				total += n
				if buf[total-1] == 'c' {
					break
				}
			}
			if err != nil {
				break
			}
		}
		ch <- readResult{buf[:total], nil}
	}()

	var resp string
	select {
	case result := <-ch:
		resp = string(result.data)
	case <-time.After(500 * time.Millisecond):
		resp = ""
	}

	// Consume the channel to avoid goroutine leak
	go func() { <-ch }()

	// Parse response: ESC [ ? 62 ; 4 ; 6 ; ... c
	// Attribute 4 = sixel graphics
	if idx := strings.Index(resp, "?"); idx >= 0 {
		params := resp[idx+1:]
		if ci := strings.IndexByte(params, 'c'); ci >= 0 {
			params = params[:ci]
		}
		for _, attr := range strings.Split(params, ";") {
			if strings.TrimSpace(attr) == "4" {
				return true
			}
		}
	}

	return false
}

// CellWidth and CellHeight are the terminal character cell dimensions in pixels.
// Set by DetectCellSize() on startup; defaults assume a typical monospace font.
var (
	CellWidth  = 8
	CellHeight = 17
)

// DetectCellSize returns the terminal cell pixel dimensions.
// Platform-specific implementations in cellsize_unix.go / cellsize_other.go.

// Phosphor green color matching the terminal theme
var (
	sixelBG     = color.RGBA{0, 0, 0, 255}
	sixelFG     = color.RGBA{0x33, 0xFF, 0x33, 255}
	sixelDim    = color.RGBA{0x00, 0x88, 0x00, 255}
	sixelBright = color.RGBA{0x55, 0xFF, 0x55, 255}
	sixelBorder = color.RGBA{0x00, 0xA0, 0x00, 255}
)

// SixelImage holds a pixel buffer for sixel rendering.
type SixelImage struct {
	img *image.RGBA
	W   int
	H   int
}

// NewSixelImage creates a new black image of the given size.
func NewSixelImage(w, h int) *SixelImage {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	// Fill black
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i+3] = 255 // alpha
	}
	return &SixelImage{img: img, W: w, H: h}
}

// SetPixel sets a single pixel.
func (s *SixelImage) SetPixel(x, y int, c color.RGBA) {
	if x >= 0 && x < s.W && y >= 0 && y < s.H {
		s.img.SetRGBA(x, y, c)
	}
}

// DrawLine draws a line using Bresenham's algorithm.
func (s *SixelImage) DrawLine(x0, y0, x1, y1 int, c color.RGBA) {
	dx := x1 - x0
	dy := y1 - y0
	if dx < 0 {
		dx = -dx
	}
	if dy < 0 {
		dy = -dy
	}
	sx := 1
	if x0 > x1 {
		sx = -1
	}
	sy := 1
	if y0 > y1 {
		sy = -1
	}
	err := dx - dy

	for {
		s.SetPixel(x0, y0, c)
		if x0 == x1 && y0 == y1 {
			break
		}
		e2 := 2 * err
		if e2 > -dy {
			err -= dy
			x0 += sx
		}
		if e2 < dx {
			err += dx
			y0 += sy
		}
	}
}

// DrawRect draws a rectangle outline.
func (s *SixelImage) DrawRect(x0, y0, x1, y1 int, c color.RGBA) {
	s.DrawLine(x0, y0, x1, y0, c)
	s.DrawLine(x1, y0, x1, y1, c)
	s.DrawLine(x1, y1, x0, y1, c)
	s.DrawLine(x0, y1, x0, y0, c)
}

// FillRect fills a rectangle.
func (s *SixelImage) FillRect(x0, y0, x1, y1 int, c color.RGBA) {
	for y := y0; y <= y1; y++ {
		for x := x0; x <= x1; x++ {
			s.SetPixel(x, y, c)
		}
	}
}

// DrawText draws a simple bitmap text string (8x8 font).
func (s *SixelImage) DrawText(x, y int, text string, c color.RGBA) {
	for i, ch := range text {
		s.drawChar(x+i*8, y, byte(ch), c)
	}
}

// drawChar draws a single character from the Apple II character ROM.
// Font data: 7 pixels wide, bit 0 = leftmost pixel column.
func (s *SixelImage) drawChar(x, y int, ch byte, c color.RGBA) {
	if ch < 32 || ch > 126 {
		return
	}
	idx := int(ch) - 32
	if idx >= len(font8x8) {
		return
	}
	glyph := font8x8[idx]
	for row := 0; row < 8; row++ {
		bits := glyph[row]
		for col := 0; col < 7; col++ {
			if bits&(1<<uint(col)) != 0 {
				s.SetPixel(x+col, y+row, c)
			}
		}
	}
}

// DrawText2x draws text with 2x-scaled Apple II font characters.
// Each character glyph is doubled (14px wide × 16px tall) with charSpacing pixels between origins.
func (s *SixelImage) DrawText2x(x, y int, text string, c color.RGBA, charSpacing int) {
	for i, ch := range text {
		s.drawChar2x(x+i*charSpacing, y, byte(ch), c)
	}
}

// drawChar2x draws a single 2x-scaled character from the Apple II ROM.
func (s *SixelImage) drawChar2x(x, y int, ch byte, c color.RGBA) {
	if ch < 32 || ch > 126 {
		return
	}
	idx := int(ch) - 32
	if idx >= len(font8x8) {
		return
	}
	glyph := font8x8[idx]
	for row := 0; row < 8; row++ {
		bits := glyph[row]
		for col := 0; col < 7; col++ {
			if bits&(1<<uint(col)) != 0 {
				px := x + col*2
				py := y + row*2
				s.SetPixel(px, py, c)
				s.SetPixel(px+1, py, c)
				s.SetPixel(px, py+1, c)
				s.SetPixel(px+1, py+1, c)
			}
		}
	}
}

// BlitMonster draws a 70x50 monochrome monster bitmap at the given position.
func (s *SixelImage) BlitMonster(x, y int, data [][]int, c color.RGBA) {
	for py, row := range data {
		for px, pixel := range row {
			if pixel != 0 {
				s.SetPixel(x+px, y+py, c)
			}
		}
	}
}

// BlitMonsterColor draws a monster image with NTSC artifact colors.
// hiresBytes: raw Apple II Hi-Res bytes, bytesPerLine: bytes per scanline (10).
func (s *SixelImage) BlitMonsterColor(x, y int, hiresBytes []int, bytesPerLine, pixelH int) {
	pixelW := bytesPerLine * 7
	if pixelW > 70 {
		pixelW = 70
	}

	// Extract pixel on/off and palette per pixel
	for line := 0; line < pixelH; line++ {
		var on [70]bool
		var pal [70]byte
		for bi := 0; bi < bytesPerLine; bi++ {
			idx := line*bytesPerLine + bi
			if idx >= len(hiresBytes) {
				continue
			}
			b := byte(hiresBytes[idx])
			p := (b >> 7) & 1
			for bit := 0; bit < 7; bit++ {
				px := bi*7 + bit
				if px < 70 {
					on[px] = b&(1<<uint(bit)) != 0
					pal[px] = p
				}
			}
		}

		// NTSC color assignment with pattern detection
		px := 0
		for px < pixelW {
			if !on[px] {
				px++
				continue
			}

			nxt := px < pixelW-1 && on[px+1]
			if nxt {
				// Adjacent ON → white
				for px < pixelW && on[px] {
					s.SetPixel(x+px, y+line, ntscWhite)
					px++
				}
				continue
			}

			// Check for alternating pattern
			end := px
			for end+2 < pixelW && !on[end+1] && on[end+2] {
				end += 2
			}

			if end > px {
				var c color.RGBA
				if pal[px] == 0 {
					if px%2 == 0 {
						c = ntscPurple
					} else {
						c = ntscGreen
					}
				} else {
					if px%2 == 0 {
						c = ntscBlue
					} else {
						c = ntscOrange
					}
				}
				for fill := px; fill <= end; fill++ {
					s.SetPixel(x+fill, y+line, c)
				}
				px = end + 1
			} else {
				var c color.RGBA
				if pal[px] == 0 {
					if px%2 == 0 {
						c = ntscPurple
					} else {
						c = ntscGreen
					}
				} else {
					if px%2 == 0 {
						c = ntscBlue
					} else {
						c = ntscOrange
					}
				}
				s.SetPixel(x+px, y+line, c)
				px++
			}
		}
	}
}

// BlitMonsterColorScaled draws a monster with NTSC colors, scaled to fit (dstW x dstH) at (x, y).
func (s *SixelImage) BlitMonsterColorScaled(x, y, dstW, dstH int, hiresBytes []int, bytesPerLine, pixelH int) {
	pixelW := bytesPerLine * 7
	if pixelW > 70 {
		pixelW = 70
	}

	// Build NTSC color pixels for the monster
	colorPixels := make([][]color.RGBA, pixelH)
	for line := 0; line < pixelH; line++ {
		colorPixels[line] = make([]color.RGBA, pixelW)
		var on [70]bool
		var pal [70]byte
		for bi := 0; bi < bytesPerLine; bi++ {
			idx := line*bytesPerLine + bi
			if idx >= len(hiresBytes) {
				continue
			}
			b := byte(hiresBytes[idx])
			p := (b >> 7) & 1
			for bit := 0; bit < 7; bit++ {
				px := bi*7 + bit
				if px < pixelW {
					on[px] = b&(1<<uint(bit)) != 0
					pal[px] = p
				}
			}
		}
		px := 0
		for px < pixelW {
			if !on[px] {
				px++
				continue
			}
			nxt := px < pixelW-1 && on[px+1]
			if nxt {
				for px < pixelW && on[px] {
					colorPixels[line][px] = ntscWhite
					px++
				}
				continue
			}
			end := px
			for end+2 < pixelW && !on[end+1] && on[end+2] {
				end += 2
			}
			var c color.RGBA
			if pal[px] == 0 {
				if px%2 == 0 {
					c = ntscPurple
				} else {
					c = ntscGreen
				}
			} else {
				if px%2 == 0 {
					c = ntscBlue
				} else {
					c = ntscOrange
				}
			}
			if end > px {
				for fill := px; fill <= end; fill++ {
					colorPixels[line][fill] = c
				}
				px = end + 1
			} else {
				colorPixels[line][px] = c
				px++
			}
		}
	}

	// Scale and blit to sixel image
	for py := 0; py < dstH; py++ {
		srcY := py * pixelH / dstH
		if srcY >= pixelH {
			continue
		}
		for px := 0; px < dstW; px++ {
			srcX := px * pixelW / dstW
			if srcX < pixelW {
				c := colorPixels[srcY][srcX]
				if c.R != 0 || c.G != 0 || c.B != 0 {
					s.SetPixel(x+px, y+py, c)
				}
			}
		}
	}
}

// Encode converts the image to a sixel string.
func (s *SixelImage) Encode() string {
	// Quantize to 16 colors
	palette := s.buildPalette(16)
	indexed := s.quantize(palette)

	var out strings.Builder

	// DCS introducer
	out.WriteString("\x1bP0;0;0q")
	// Raster attributes
	fmt.Fprintf(&out, "\"1;1;%d;%d", s.W, s.H)

	// Define colors
	for i, c := range palette {
		r := int(c.R) * 100 / 255
		g := int(c.G) * 100 / 255
		b := int(c.B) * 100 / 255
		fmt.Fprintf(&out, "#%d;2;%d;%d;%d", i, r, g, b)
	}

	// Encode sixel data band by band (6 rows per band)
	for bandTop := 0; bandTop < s.H; bandTop += 6 {
		firstColor := true
		for ci, _ := range palette {

			sixels := make([]byte, s.W)
			hasPixel := false
			for x := 0; x < s.W; x++ {
				var bits byte
				for dy := 0; dy < 6; dy++ {
					y := bandTop + dy
					if y < s.H && indexed[y*s.W+x] == ci {
						bits |= 1 << uint(dy)
					}
				}
				sixels[x] = bits
				if bits != 0 {
					hasPixel = true
				}
			}

			if !hasPixel {
				continue
			}

			// Trim trailing zeros
			end := s.W
			for end > 0 && sixels[end-1] == 0 {
				end--
			}
			if end == 0 {
				continue
			}

			if firstColor {
				firstColor = false
			} else {
				out.WriteByte('$')
			}

			fmt.Fprintf(&out, "#%d", ci)

			// RLE encode
			i := 0
			for i < end {
				val := sixels[i]
				run := 1
				for i+run < end && sixels[i+run] == val {
					run++
				}
				ch := val + 63
				if run >= 4 {
					fmt.Fprintf(&out, "!%d%c", run, ch)
				} else {
					for r := 0; r < run; r++ {
						out.WriteByte(ch)
					}
				}
				i += run
			}
		}
		out.WriteByte('-')
	}

	out.WriteString("\x1b\\") // ST
	return out.String()
}

// buildPalette extracts the most-used colors from the image.
func (s *SixelImage) buildPalette(maxColors int) []color.RGBA {
	counts := make(map[color.RGBA]int)
	for y := 0; y < s.H; y++ {
		for x := 0; x < s.W; x++ {
			r, g, b, a := s.img.At(x, y).RGBA()
			c := color.RGBA{uint8(r >> 8), uint8(g >> 8), uint8(b >> 8), uint8(a >> 8)}
			counts[c]++
		}
	}

	// Sort by frequency, keep top N
	type colorCount struct {
		c     color.RGBA
		count int
	}
	sorted := make([]colorCount, 0, len(counts))
	for c, n := range counts {
		sorted = append(sorted, colorCount{c, n})
	}
	for i := 0; i < len(sorted); i++ {
		for j := i + 1; j < len(sorted); j++ {
			if sorted[j].count > sorted[i].count {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}

	palette := make([]color.RGBA, 0, maxColors)
	for i := 0; i < len(sorted) && i < maxColors; i++ {
		palette = append(palette, sorted[i].c)
	}
	return palette
}

// quantize maps each pixel to the nearest palette index.
func (s *SixelImage) quantize(palette []color.RGBA) []int {
	result := make([]int, s.W*s.H)
	for y := 0; y < s.H; y++ {
		for x := 0; x < s.W; x++ {
			r, g, b, _ := s.img.At(x, y).RGBA()
			pr, pg, pb := int(r>>8), int(g>>8), int(b>>8)

			best := 0
			bestDist := 1<<30
			for i, c := range palette {
				dr := pr - int(c.R)
				dg := pg - int(c.G)
				db := pb - int(c.B)
				dist := dr*dr + dg*dg + db*db
				if dist < bestDist {
					bestDist = dist
					best = i
				}
			}
			result[y*s.W+x] = best
		}
	}
	return result
}

// WriteSixel writes the sixel data to stdout at the given terminal row.
func WriteSixel(row int, data string) {
	// Move cursor to position, write sixel, then restore cursor
	fmt.Fprintf(os.Stdout, "\x1b[%d;1H%s", row+1, data)
	os.Stdout.Sync()
}
