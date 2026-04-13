package render

import (
	"image/color"
	"math/rand"

	"github.com/gdamore/tcell/v2"
	"wizardry/data"
	"wizardry/engine"
)

// Apple II hi-res viewport dimensions from RUNNER.TEXT / CLRPICT assembly.
// "79 LINES X 82 COLUMNS OF PIXELS ON THE HI RES SCREEN."
const (
	mazeW = 82
	mazeH = 79
)

// MazeBitmap is an 82×79 pixel buffer matching the original Apple II hi-res viewport.
// DRAWMAZE renders into this at original resolution; it's then scaled to any output.
type MazeBitmap struct {
	Pix    [mazeH][mazeW]bool
	clipXL int // left clipping boundary (XLOWER)
	clipYL int // top clipping boundary (always 0)
	clipXU int // right clipping boundary (XUPPER)
	clipYU int // bottom clipping boundary (always 79)
}

func (m *MazeBitmap) clear() {
	m.Pix = [mazeH][mazeW]bool{}
	m.clipXL = 0
	m.clipYL = 0
	m.clipXU = mazeW - 1
	m.clipYU = mazeH - 1
}

func (m *MazeBitmap) setClip(xl, yl, xu, yu int) {
	m.clipXL = xl
	m.clipYL = yl
	m.clipXU = xu
	m.clipYU = yu
}

// drawLine is the exact DRAWLINE primitive from the Apple II assembly.
// Draws pixels stepping (dh, dv) per pixel for `length` steps, with clipping.
func (m *MazeBitmap) drawLine(x, y, dh, dv, length int) {
	for i := 0; i < length; i++ {
		if x >= m.clipXL && x <= m.clipXU && y >= m.clipYL && y <= m.clipYU {
			m.Pix[y][x] = true
		}
		x += dh
		y += dv
	}
}

// BlitToCanvas scales the 82×79 bitmap to a Canvas using max-pooling.
// For each destination pixel, checks ALL source pixels in the mapped region.
// This preserves thin wireframe lines that point sampling would miss.
func (m *MazeBitmap) BlitToCanvas(c *Canvas) {
	for cy := 0; cy < c.H; cy++ {
		syStart := cy * mazeH / c.H
		syEnd := (cy + 1) * mazeH / c.H
		if syEnd == syStart {
			syEnd = syStart + 1
		}
		for cx := 0; cx < c.W; cx++ {
			sxStart := cx * mazeW / c.W
			sxEnd := (cx + 1) * mazeW / c.W
			if sxEnd == sxStart {
				sxEnd = sxStart + 1
			}
			hit := false
			for sy := syStart; sy < syEnd && sy < mazeH; sy++ {
				for sx := sxStart; sx < sxEnd && sx < mazeW; sx++ {
					if m.Pix[sy][sx] {
						hit = true
						break
					}
				}
				if hit {
					break
				}
			}
			if hit {
				c.Set(cx, cy)
			}
		}
	}
}

// BlitToSixel scales the 82×79 bitmap to a region of a SixelImage.
func (m *MazeBitmap) BlitToSixel(si *SixelImage, dx, dy, dw, dh int, col color.RGBA) {
	for py := 0; py < dh; py++ {
		sy := py * mazeH / dh
		for px := 0; px < dw; px++ {
			sx := px * mazeW / dw
			if m.Pix[sy][sx] {
				si.SetPixel(dx+px, dy+py, col)
			}
		}
	}
}

// shftPos converts relative (rightShift, fwdShift) offsets to absolute maze coordinates.
// Matches Pascal SHFTPOS exactly — engine uses Pascal Y+=North coordinates.
func shftPos(x, y *int, facing engine.Direction, rightShift, fwdShift int) {
	switch facing {
	case engine.North:
		*x += rightShift
		*y += fwdShift
	case engine.East:
		*x += fwdShift
		*y -= rightShift
	case engine.South:
		*x -= rightShift
		*y -= fwdShift
	case engine.West:
		*x -= fwdShift
		*y += rightShift
	}
	*x = ((*x % 20) + 20) % 20
	*y = ((*y % 20) + 20) % 20
}

// frwdView gets the wall ahead from a position shifted right by deltaR.
func frwdView(level *data.MazeLevel, x, y int, facing engine.Direction, deltaR int) data.WallType {
	sx, sy := x, y
	shftPos(&sx, &sy, facing, deltaR, 0)
	switch facing {
	case engine.North:
		return level.Cells[sy][sx].N
	case engine.East:
		return level.Cells[sy][sx].E
	case engine.South:
		return level.Cells[sy][sx].S
	case engine.West:
		return level.Cells[sy][sx].W
	}
	return data.WallWall
}

// leftView gets the wall to the left from a position shifted right by deltaR.
func leftView(level *data.MazeLevel, x, y int, facing engine.Direction, deltaR int) data.WallType {
	sx, sy := x, y
	shftPos(&sx, &sy, facing, deltaR, 0)
	switch facing {
	case engine.North:
		return level.Cells[sy][sx].W
	case engine.East:
		return level.Cells[sy][sx].N
	case engine.South:
		return level.Cells[sy][sx].E
	case engine.West:
		return level.Cells[sy][sx].S
	}
	return data.WallWall
}

// righView gets the wall to the right from a position shifted right by deltaR.
func righView(level *data.MazeLevel, x, y int, facing engine.Direction, deltaR int) data.WallType {
	sx, sy := x, y
	shftPos(&sx, &sy, facing, deltaR, 0)
	switch facing {
	case engine.North:
		return level.Cells[sy][sx].E
	case engine.East:
		return level.Cells[sy][sx].S
	case engine.South:
		return level.Cells[sy][sx].W
	case engine.West:
		return level.Cells[sy][sx].N
	}
	return data.WallWall
}

// shouldDrawDoor returns true if a door should be drawn for the given wall type.
// From Pascal: door drawn for DOOR always, HIDEDOOR with light or 1/6 chance.
func shouldDrawDoor(wt data.WallType, gotLight bool) bool {
	if wt == data.WallDoor {
		return true
	}
	if wt == data.WallHidden {
		return gotLight || rand.Intn(6) == 3
	}
	return false
}

// DrawMaze renders the 3D dungeon view following the exact DRAWMAZE algorithm
// from Pascal source RUNNER.TEXT (lines 16-305).
// Renders into an 82×79 MazeBitmap at original Apple II resolution.
func DrawMaze(m *MazeBitmap, level *data.MazeLevel, px, py int,
	facing engine.Direction, lightLevel int, quickPlot bool) {

	m.clear()

	gotLight := lightLevel > 0
	var lightDis int
	if gotLight {
		if quickPlot {
			lightDis = 3
		} else {
			lightDis = 5
		}
	} else {
		lightDis = 2
	}

	// Initial scaling values — from Pascal DRAWMAZE init (lines 224-231)
	ul := 8
	lr := 72
	walWidth := 32
	doorWidt := 16
	doorFram := 8
	walHeigh := 64

	x4draw := px
	y4draw := py
	xlower := 0
	xupper := 81

	// Main depth loop — from Pascal line 237: WHILE LIGHTDIS > 0 DO
	for lightDis > 0 {
		// Check for DARK square — exit immediately (Pascal line 239-240)
		cell := &level.Cells[y4draw][x4draw]
		if cell.Type == data.SqDark {
			return
		}

		// Set clipping rectangle — Pascal line 253: CLRPICT(XLOWER, 0, XUPPER, 79)
		m.setClip(xlower, 0, xupper, 78)

		// ── LEFT SIDE ── (Pascal lines 256-268)
		wallType := leftView(level, x4draw, y4draw, facing, 0)
		if wallType != data.WallOpen {
			// DRAWLEFT — Pascal P010E07
			xlower = ul

			// Wall panel: 4 edges
			m.drawLine(ul, ul, -1, -1, walWidth)                    // top edge diagonal
			m.drawLine(ul, ul, 0, 1, walHeigh)                      // right edge vertical
			m.drawLine(ul, lr, -1, 1, walWidth)                     // bottom edge diagonal
			m.drawLine(ul-walWidth, ul-walWidth, 0, 1, walHeigh*2)  // left edge vertical

			// Door
			if shouldDrawDoor(wallType, gotLight) {
				m.drawLine(ul-doorFram, ul, -1, -1, doorWidt)
				m.drawLine(ul-doorFram, ul, 0, 1, walHeigh+doorFram)
				m.drawLine(ul-doorFram-doorWidt, ul-doorWidt, 0, 1, walHeigh+walWidth+doorFram)
			}
		} else {
			// Left is open — check forward wall of square to the left
			fwWall := frwdView(level, x4draw, y4draw, facing, -1)
			if fwWall != data.WallOpen {
				// DRAWFRNT with LRCENT = -(2*WALWIDTH)
				lrcent := -(2 * walWidth)
				drawFrnt(m, ul, lr, walWidth, walHeigh, doorWidt, doorFram, lrcent, fwWall, gotLight)
				xlower = ul
			}
		}

		// ── RIGHT SIDE ── (Pascal lines 270-282)
		wallType = righView(level, x4draw, y4draw, facing, 0)
		if wallType != data.WallOpen {
			// DRAWRIGH — Pascal P010E08
			xupper = lr

			// Wall panel: 4 edges
			m.drawLine(lr, ul, 1, -1, walWidth)                     // top edge diagonal
			m.drawLine(lr, ul, 0, 1, walHeigh)                      // left edge vertical
			m.drawLine(lr, lr, 1, 1, walWidth)                      // bottom edge diagonal
			m.drawLine(lr+walWidth, ul-walWidth, 0, 1, walHeigh*2)  // right edge vertical

			// Door
			if shouldDrawDoor(wallType, gotLight) {
				m.drawLine(lr+doorFram, ul, 1, -1, doorWidt)
				m.drawLine(lr+doorFram, ul, 0, 1, walHeigh+doorFram)
				m.drawLine(lr+doorFram+doorWidt, ul-doorWidt, 0, 1, walHeigh+walWidth+doorFram)
			}
		} else {
			// Right is open — check forward wall of square to the right
			fwWall := frwdView(level, x4draw, y4draw, facing, 1)
			if fwWall != data.WallOpen {
				// DRAWFRNT with LRCENT = 2*WALWIDTH
				lrcent := 2 * walWidth
				drawFrnt(m, ul, lr, walWidth, walHeigh, doorWidt, doorFram, lrcent, fwWall, gotLight)
				xupper = lr
			}
		}

		// ── FRONT ── (Pascal lines 284-291)
		wallType = frwdView(level, x4draw, y4draw, facing, 0)
		if wallType != data.WallOpen {
			// DRAWFRNT with LRCENT = 0
			drawFrnt(m, ul, lr, walWidth, walHeigh, doorWidt, doorFram, 0, wallType, gotLight)
			return // EXIT(DRAWMAZE)
		}

		// ── Advance to next depth layer ── (Pascal lines 293-303)
		walWidth = walWidth / 2
		doorWidt = walWidth / 2
		walHeigh = walWidth * 2
		doorFram = walWidth / 4
		ul = ul + walWidth
		lr = lr - walWidth

		shftPos(&x4draw, &y4draw, facing, 0, 1)
		lightDis--
	}
}

// drawFrnt draws a front-facing wall panel — Pascal DRAWFRNT (P010E09).
// lrcent controls horizontal offset: 0=centered, negative=left, positive=right.
func drawFrnt(m *MazeBitmap, ul, lr, walWidth, walHeigh, doorWidt, doorFram, lrcent int,
	wallType data.WallType, gotLight bool) {

	// Wall panel: top, left, right (+1), bottom
	m.drawLine(ul+lrcent, ul, 1, 0, walHeigh)           // top
	m.drawLine(ul+lrcent, ul, 0, 1, walHeigh)           // left
	m.drawLine(ul+lrcent+walHeigh, ul, 0, 1, walHeigh+1) // right (+1 per original)
	m.drawLine(ul+lrcent, ul+walHeigh, 1, 0, walHeigh)  // bottom

	// Door
	if shouldDrawDoor(wallType, gotLight) {
		doorH := walWidth + doorWidt + doorFram
		// Left jamb: from bottom up
		m.drawLine(ul+lrcent+doorFram, lr, 0, -1, doorH)
		// Right jamb: from bottom up
		m.drawLine(ul+lrcent+walWidth+doorWidt+doorFram, lr, 0, -1, doorH)
		// Lintel: horizontal across top of door
		m.drawLine(ul+lrcent+doorFram, lr-doorH, 1, 0, walWidth+doorWidt+1)
	}
}

// OverlayMonsterArt clears the viewport, then draws the half-block Unicode art
// centered in the 82×79 bitmap. Art uses ▀ (top), ▄ (bottom), █ (both).
func (m *MazeBitmap) OverlayMonsterArt(art []string, artWidth int) {
	// Clear the bitmap — monster replaces the dungeon wireframe
	m.Pix = [mazeH][mazeW]bool{}
	// Convert art to pixel rows (2 pixel rows per art line)
	artH := len(art) * 2
	// Center in the 82×79 viewport
	ox := (mazeW - artWidth) / 2
	oy := (mazeH - artH) / 2
	if ox < 0 {
		ox = 0
	}
	if oy < 0 {
		oy = 0
	}

	for lineIdx, line := range art {
		runes := []rune(line)
		for col, ch := range runes {
			px := ox + col
			topY := oy + lineIdx*2
			botY := topY + 1
			if px >= mazeW {
				break
			}
			switch ch {
			case '▀':
				if topY >= 0 && topY < mazeH {
					m.Pix[topY][px] = true
				}
			case '▄':
				if botY >= 0 && botY < mazeH {
					m.Pix[botY][px] = true
				}
			case '█':
				if topY >= 0 && topY < mazeH {
					m.Pix[topY][px] = true
				}
				if botY >= 0 && botY < mazeH {
					m.Pix[botY][px] = true
				}
			}
		}
	}
}

// Canvas is a pixel buffer rendered with Unicode half-block characters.
type Canvas struct {
	W      int
	H      int
	pixels []bool
}

func NewCanvas(termW, termH int) *Canvas {
	pw := termW
	ph := termH * 2
	return &Canvas{W: pw, H: ph, pixels: make([]bool, pw*ph)}
}

func (c *Canvas) Clear() {
	for i := range c.pixels {
		c.pixels[i] = false
	}
}

func (c *Canvas) Set(x, y int) {
	if x >= 0 && x < c.W && y >= 0 && y < c.H {
		c.pixels[y*c.W+x] = true
	}
}

func (c *Canvas) Get(x, y int) bool {
	if x >= 0 && x < c.W && y >= 0 && y < c.H {
		return c.pixels[y*c.W+x]
	}
	return false
}

// RenderDungeon draws the 3D wireframe dungeon view onto a Canvas.
// Uses DrawMaze at 82×79 original resolution, then scales to the Canvas.
func RenderDungeon(c *Canvas, level *data.MazeLevel, px, py int, facing engine.Direction,
	lightLevel int, quickPlot bool) {
	c.Clear()
	var bmp MazeBitmap
	DrawMaze(&bmp, level, px, py, facing, lightLevel, quickPlot)
	bmp.BlitToCanvas(c)
}

// RenderDungeonSixel draws the 3D wireframe dungeon into a SixelImage region.
// Uses DrawMaze at 82×79 original resolution, then scales to the target area.
func RenderDungeonSixel(si *SixelImage, x0, y0, w, h int, level *data.MazeLevel, px, py int,
	facing engine.Direction, col color.RGBA, lightLevel int, quickPlot bool) {
	var bmp MazeBitmap
	DrawMaze(&bmp, level, px, py, facing, lightLevel, quickPlot)
	bmp.BlitToSixel(si, x0, y0, w, h, col)
}

// DrawCanvas renders the pixel canvas to the tcell screen using half-block characters.
func (s *Screen) DrawCanvas(c *Canvas, logX, sy int, style tcell.Style) {
	sx := logX * s.scale
	termH := c.H / 2
	for row := 0; row < termH; row++ {
		pyTop := row * 2
		pyBot := row*2 + 1
		for col := 0; col < c.W; col++ {
			top := c.Get(col, pyTop)
			bot := c.Get(col, pyBot)
			var ch rune
			switch {
			case top && bot:
				ch = '\u2588'
			case top:
				ch = '\u2580'
			case bot:
				ch = '\u2584'
			default:
				ch = ' '
			}
			s.tcell.SetContent(sx+col, sy+row, ch, nil, style)
		}
	}
}
