package render

import (
	"github.com/gdamore/tcell/v2"
	"wizardry/data"
	"wizardry/engine"
)

// drawCentered draws text centered within the 40-column logical display.
func (s *Screen) drawCentered(y int, style tcell.Style, text string) {
	pad := (40 - len(text)) / 2
	if pad < 0 {
		pad = 0
	}
	s.DrawString(pad, y, style, text)
}

// Title art frames — converted from Apple II Hi-Res boot disk WT bitmap.
// 24 lines × ~70 cols to fit standard 80×24 terminal.

// titleArtFull: complete image — wizard + smoke + "WIZARDRY" text.
var titleArtFull = []string{
	`   ███▀███ ███                    ▄▄█▄                 ▄▄`,
	`  ██▀ ▄██   ██▄████▄▄▄▄▄▄▄▄█▄▄▄▄▄▄  ▄███▄▄▄▄▄▄▄ ▄▄▄▄▄▄▄██`,
	` ███▄▄██▄▄▄▄████▄▄▄█████▄▄██▄██▄█████▄▄██▄██▄▄████▄▄█████▄▄▄▄▄▄▄▄▄▄█▄`,
	` ███ ▀███▀▀▀████▀██▀▀▀██████▀████████████▀███████████████▀▀▀▀▀▀▀▀▀▀▀▀`,
	`  ▀██▄████████ ▀██▀▀▀▀██████▀▀▀  ▀██▄ ▀   ▀▀  ▀███▀▀█████       █ █ █`,
	`                        ▀▀                  █████▄ ▄██▀▀▀`,
	`                       █▄▄▄     ▄▄▄         ▀  ▄▄ ▀▀▀`,
	`                     ▄██▀▀▀ ▄▄▄██████      ▄█████▀▀`,
	`                  ▄▄█▀      █████████▀    ███`,
	`                  ▀█▀█████████████████▄▄▄███▀`,
	`                    ▄██████████████████▀▀▀▀`,
	`                  ▄████▀██████████████▄▄▄▄`,
	`                  ▀████▄ ▀▀████▀████████████`,
	`                    ██████▄▄████▄▄▄██▀▀▀██▀██▄`,
	`                 ▄█▀▀▀ ▀▀▀██▄██▀▀  ▀███▀▀▀████`,
	`                ██▀        ███▀▀█████▄▄▄▄██▀ ▀█▄`,
	`                ██▄         ▀██▄▄   ▀██████▄  ██`,
	`                ▀███▄▄       ███████████████▄██`,
	`                  ▀▀▀█████▄▄  ▀████████████▀▀`,
	`                   ▄▄████████▄▄█▀▀▀████████▄`,
	`                   ▀██████████▀   ▄████████`,
	`                   ████████████▄▄██████████`,
	`                  ▀█████████████▄█████████`,
	`                 ▀▀██▀▀██████████   ▀▀▀▀▀   ▄█████████████▄ █████████▄`,
}

// smokeStartRow: the first row index where the smoke/logo appears (rows 0-9).
// Rows 10+ are the wizard body which is always visible.
const smokeStartRow = 10

// RenderTitle draws the title screen in its current phase.
//
// Full flow traced from p-code SYSTEM.STARTUP:
//
//	Seg 2 TITLELOA: text intro (timed) → wizard art animation (loops until keypress)
//	Seg 3 OPTIONS:  copyright → version → "S)TART GAME  U)TILITIES  T)ITLE PAGE"
//
// WIZBOOT main loop (seg 0, offsets 206-252):
//
//	211: CXP seg=8 proc=1  → TITLELOA (full title sequence)
//	214: CXP seg=9 proc=1  → OPTIONS (menu)
//	238: XJP 'S'→start, 'T'→TITLELOA again, 'U'→utilities
func (s *Screen) RenderTitle(game *engine.GameState) {
	title := game.Title
	if title == nil {
		s.Clear()
		s.Show()
		return
	}

	green := base
	white := base

	// TitleArt with WT animation: the animation goroutine drives rendering
	// directly via EmitSixelFrame/EmitCanvasFrame. Don't redraw here.
	if title.Step == engine.TitleArt && title.Anim != nil {
		return
	}

	// All other paths use tcell — clear first.
	s.Clear()
	s.ClearSixelTransition()

	switch title.Step {
	case engine.TitleText:
		if title.TextLine >= 0 {
			s.DrawString(12, 10, white, "PREPARE YOURSELF")
		}
		if title.TextLine >= 1 {
			s.DrawString(12, 12, white, "FOR THE ULTIMATE")
		}
		if title.TextLine >= 2 {
			s.DrawString(12, 14, white, "IN FANTASY GAMES")
		}

	case engine.TitleStory:
		// Multi-frame story sequence (Wiz 3)
		// 11 text pages, 10 image frames: pages 8-9 share image 8, page 10 uses image 9
		idx := title.StoryFrame
		imgIdx := idx
		if imgIdx >= 9 {
			imgIdx = idx - 1 // pages 9→image 8, page 10→image 9
		}
		hasSixelFrame := false
		if SixelSupported && imgIdx >= 0 && imgIdx < len(game.Scenario.TitleFrames) {
			tb := game.Scenario.TitleFrames[imgIdx]
			if tb != nil && len(tb.Pixels) > 0 {
				// Full-screen image, text overlaid at bottom rows
				si := s.buildStorySixel(tb, 24)
				WriteSixel(0, si.Encode())
				s.MarkSixel()
				hasSixelFrame = true
			}
		}
		// Draw story text
		if idx >= 0 && idx < len(game.Scenario.TitleStory) {
			page := game.Scenario.TitleStory[idx]
			if hasSixelFrame {
				// Text overlaid on bottom 4 rows of the image
				startY := 19
				for i, line := range page {
					pad := (40 - len(line)) / 2
					if pad < 0 {
						pad = 0
					}
					s.DrawString(pad, startY+i, white, line)
				}
				s.Show()
				return
			}
			// Non-sixel: centered text only
			startY := (24 - len(page)) / 2
			for i, line := range page {
				pad := (40 - len(line)) / 2
				if pad < 0 {
					pad = 0
				}
				s.DrawString(pad, startY+i, white, line)
			}
		}

	case engine.TitleArt:
		animRow := title.AnimRow
		revealFrac := 1.0
		if animRow > 0 {
			revealFrac = 1.0 - float64(animRow)/24.0
		}

		tb := game.Scenario.Title
		if tb != nil && len(tb.Pixels) > 0 {
			startSrcY := int(float64(tb.Height) * (1.0 - revealFrac))
			if SixelSupported {
				si := s.buildTitleSixel(tb, startSrcY)
				WriteSixel(0, si.Encode())
				s.MarkSixel()
				return
			}
			if ColorMode && len(tb.HiRes) == 8192 {
				s.renderTitleCanvasColor(tb, startSrcY)
			} else {
				s.renderTitleCanvas(tb, startSrcY, green)
			}
			s.Show()
			return
		}

		// Fallback: hardcoded Unicode half-block art (Wiz 1 only).
		for i := 0; i < 24 && i < len(titleArtFull); i++ {
			if i < animRow {
				continue
			}
			col := 0
			for _, ch := range titleArtFull[i] {
				s.tcell.SetContent(col, i, ch, nil, green)
				col++
			}
		}

	case engine.TitleMenu:
		if game.Scenario.Title == nil {
			// Wiz 3: text-only title with game name (no title image)
			y := 2
			name := game.Scenario.Game
			pad := (40 - len(name)) / 2
			if pad < 0 {
				pad = 0
			}
			for i := 0; i < pad; i++ {
				name = " " + name
			}
			s.DrawString(0, y, white, name)
			y += 2
			s.drawCentered(y, white, "NOTICE")
			y += 2
			s.drawCentered(y, white, "THIS SOFTWARE IS A MODERN RECREATION")
			y++
			s.drawCentered(y, white, "OF A CLASSIC DUNGEON ADVENTURE FROM")
			y++
			s.drawCentered(y, white, "THE EARLY DAYS OF PERSONAL COMPUTING.")
			y++
			s.drawCentered(y, white, "IT IS INTENDED FOR ENTERTAINMENT AND")
			y++
			s.drawCentered(y, white, "EDUCATIONAL USE. THE AUTHORS MAKE NO")
			y++
			s.drawCentered(y, white, "CLAIM TO THE ORIGINAL WORK, BUT OFFER")
			y++
			s.drawCentered(y, white, "THIS VERSION AS A TRIBUTE TO ITS")
			y++
			s.drawCentered(y, white, "INFLUENCE AND LEGACY.")
			y += 2
			s.drawCentered(y, white, "ORIGINAL GAME BY ANDREW GREENBERG")
			y++
			s.drawCentered(y, white, "AND ROBERT WOODHEAD")
			y += 2
			s.drawCentered(y, white, "S)TART GAME  U)TILITIES")
		} else {
			// Wiz 1: notice + version + menu
			y := 1
			s.drawCentered(y, white, "NOTICE")
			y += 2
			s.drawCentered(y, white, "THIS SOFTWARE IS A MODERN RECREATION")
			y++
			s.drawCentered(y, white, "OF A CLASSIC DUNGEON ADVENTURE FROM")
			y++
			s.drawCentered(y, white, "THE EARLY DAYS OF PERSONAL COMPUTING.")
			y++
			s.drawCentered(y, white, "IT IS INTENDED FOR ENTERTAINMENT AND")
			y++
			s.drawCentered(y, white, "EDUCATIONAL USE. THE AUTHORS MAKE NO")
			y++
			s.drawCentered(y, white, "CLAIM TO THE ORIGINAL WORK, BUT OFFER")
			y++
			s.drawCentered(y, white, "THIS VERSION AS A TRIBUTE TO ITS")
			y++
			s.drawCentered(y, white, "INFLUENCE AND LEGACY.")
			y += 2
			s.drawCentered(y, white, "ORIGINAL GAME BY ANDREW GREENBERG")
			y++
			s.drawCentered(y, white, "AND ROBERT WOODHEAD")
			y += 2
			versionLine := "VERSION " + game.Version + " OF " + game.BuildDate
			s.drawCentered(y, white, versionLine)
			y += 4
			s.drawCentered(y, white, "S)TART GAME  U)TILITIES  T)ITLE PAGE")
		}
	}

	s.Show()
}

// buildStorySixel renders a Wiz 3 story frame as a sixel image.
// 2x scale matching show_picbits.py --sixel output. Text overlaid via tcell.
func (s *Screen) buildStorySixel(tb *data.TitleBitmap, rows int) *SixelImage {
	// 2x scale: 280x192 → 560x384
	dstW := tb.Width * 2
	dstH := tb.Height * 2
	imgH := dstH
	if imgH%6 != 0 {
		imgH += 6 - imgH%6
	}

	si := NewSixelImage(dstW, imgH)

	// NTSC color mode: convert raw framebuffer to color pixels
	if ColorMode && len(tb.HiRes) == 8192 {
		hires := make([]byte, 8192)
		for i, v := range tb.HiRes {
			hires[i] = byte(v)
		}
		colorPixels := HiResToColorPixels(hires)
		for py := 0; py < dstH; py++ {
			srcY := py / 2
			if srcY >= 192 {
				continue
			}
			for px := 0; px < dstW; px++ {
				srcX := px / 2
				if srcX < 280 {
					c := colorPixels[srcY][srcX]
					if c.R != 0 || c.G != 0 || c.B != 0 {
						si.SetPixel(px, py, c)
					}
				}
			}
		}
		return si
	}

	// Monochrome: use sixelFG (green in normal mode, white in color mode)
	white := sixelFG
	for py := 0; py < dstH; py++ {
		srcY := py / 2
		if srcY >= tb.Height {
			continue
		}
		row := tb.Pixels[srcY]
		for px := 0; px < dstW; px++ {
			srcX := px / 2
			if srcX < len(row) && row[srcX] != 0 {
				si.SetPixel(px, py, white)
			}
		}
	}

	return si
}

func (s *Screen) buildTitleSixel(tb *data.TitleBitmap, startSrcY int) *SixelImage {
	cw := CellWidth
	ch := CellHeight
	imgW := 80 * cw
	imgH := 24 * ch
	if imgH%6 != 0 {
		imgH += 6 - imgH%6
	}

	si := NewSixelImage(imgW, imgH)
	fc := sixelFG

	// Scale to fit, preserving aspect ratio, centered
	scaleX := float64(imgW) / float64(tb.Width)
	scaleY := float64(imgH) / float64(tb.Height)
	sc := scaleX
	if scaleY < sc {
		sc = scaleY
	}
	dstW := int(float64(tb.Width) * sc)
	dstH := int(float64(tb.Height) * sc)
	ox := (imgW - dstW) / 2
	oy := (imgH - dstH) / 2

	if ColorMode && len(tb.HiRes) == 8192 {
		hires := make([]byte, 8192)
		for i, v := range tb.HiRes {
			hires[i] = byte(v)
		}
		colorPixels := HiResToColorPixels(hires)
		for py := 0; py < dstH; py++ {
			srcY := py * tb.Height / dstH
			if srcY < startSrcY || srcY >= 192 {
				continue
			}
			for px := 0; px < dstW; px++ {
				srcX := px * tb.Width / dstW
				if srcX < 280 {
					c := colorPixels[srcY][srcX]
					if c.R != 0 || c.G != 0 || c.B != 0 {
						si.SetPixel(ox+px, oy+py, c)
					}
				}
			}
		}
	} else {
		for py := 0; py < dstH; py++ {
			srcY := py * tb.Height / dstH
			if srcY < startSrcY || srcY >= tb.Height {
				continue
			}
			row := tb.Pixels[srcY]
			for px := 0; px < dstW; px++ {
				srcX := px * tb.Width / dstW
				if srcX < len(row) && row[srcX] != 0 {
					si.SetPixel(ox+px, oy+py, fc)
				}
			}
		}
	}

	return si
}

// renderTitleCanvas renders the Apple II title bitmap as half-block Unicode art.
// Same bitmap, same approach as monster images — scale to 80×48 (80 cols × 24 rows × 2 half-blocks).
func (s *Screen) renderTitleCanvas(tb *data.TitleBitmap, startSrcY int, style tcell.Style) {
	// Canvas covers full 80×24 screen (80 wide × 48 half-block pixels)
	canvasW := 80
	canvasH := 48 // 24 rows × 2 pixels per row
	canvas := NewCanvas(canvasW, canvasH)

	// Scale bitmap using max-pooling (like MazeBitmap.BlitToCanvas)
	for cy := 0; cy < canvasH; cy++ {
		syStart := cy * tb.Height / canvasH
		syEnd := (cy + 1) * tb.Height / canvasH
		if syEnd == syStart {
			syEnd = syStart + 1
		}
		for cx := 0; cx < canvasW; cx++ {
			sxStart := cx * tb.Width / canvasW
			sxEnd := (cx + 1) * tb.Width / canvasW
			if sxEnd == sxStart {
				sxEnd = sxStart + 1
			}
			hit := false
			for sy := syStart; sy < syEnd && sy < tb.Height; sy++ {
				if sy < startSrcY {
					continue
				}
				row := tb.Pixels[sy]
				for sx := sxStart; sx < sxEnd && sx < len(row); sx++ {
					if row[sx] != 0 {
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

	// Render canvas to tcell at scale=1 (full screen, no scaling)
	for row := 0; row < 24; row++ {
		pyTop := row * 2
		pyBot := row*2 + 1
		for col := 0; col < canvasW; col++ {
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

// renderTitleCanvasColor renders a TitleBitmap with NTSC artifact colors
// using half-block characters. Each cell gets the dominant NTSC color.
func (s *Screen) renderTitleCanvasColor(tb *data.TitleBitmap, startSrcY int) {
	hires := make([]byte, len(tb.HiRes))
	for i, v := range tb.HiRes {
		hires[i] = byte(v)
	}
	colorPixels := HiResToColorPixels(hires)
	black := tcell.StyleDefault.Background(tcell.ColorBlack)

	for row := 0; row < 24; row++ {
		// Each terminal row covers 8 source rows (192/24)
		srcYTop := row * 8
		srcYBot := row*8 + 4
		if srcYTop < startSrcY {
			continue
		}
		for col := 0; col < 80; col++ {
			srcXStart := col * 280 / 80
			srcXEnd := (col + 1) * 280 / 80
			if srcXEnd <= srcXStart {
				srcXEnd = srcXStart + 1
			}

			topColor := dominantColor(colorPixels, srcXStart, srcXEnd, srcYTop, srcYTop+4)
			botColor := dominantColor(colorPixels, srcXStart, srcXEnd, srcYBot, srcYBot+4)
			topOn := topColor != 0
			botOn := botColor != 0

			if !topOn && !botOn {
				continue
			}

			var ch rune
			var st tcell.Style
			switch {
			case topOn && botOn:
				ch = '\u2588'
				st = tcell.StyleDefault.Foreground(topColor).Background(botColor)
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

// renderTitleBraille renders a TitleBitmap using Unicode Braille characters.
// Each braille char is 2 dots wide × 4 dots tall, giving 160×96 effective
// resolution on an 80×24 terminal — much better than half-block for wireframes.
func (s *Screen) renderTitleBraille(tb *data.TitleBitmap, style tcell.Style) {
	cols := 80
	rows := 24
	dotW := cols * 2  // 160
	dotH := rows * 4  // 96

	// Braille dot bit positions:
	// Dot 1 (0x01) top-left     Dot 4 (0x08) top-right
	// Dot 2 (0x02) mid-left     Dot 5 (0x10) mid-right
	// Dot 3 (0x04) bot-left     Dot 6 (0x20) bot-right
	// Dot 7 (0x40) low-left     Dot 8 (0x80) low-right
	type dotDef struct {
		dy, bit int
		right   bool
	}
	dots := []dotDef{
		{0, 0x01, false}, {1, 0x02, false}, {2, 0x04, false}, {3, 0x40, false},
		{0, 0x08, true}, {1, 0x10, true}, {2, 0x20, true}, {3, 0x80, true},
	}

	for row := 0; row < rows; row++ {
		for col := 0; col < cols; col++ {
			bits := 0
			for _, d := range dots {
				dx := 0
				if d.right {
					dx = 1
				}
				sx := (col*2 + dx) * tb.Width / dotW
				sy := (row*4 + d.dy) * tb.Height / dotH
				if sy >= 0 && sy < tb.Height && sx >= 0 && sx < tb.Width {
					if len(tb.Pixels[sy]) > sx && tb.Pixels[sy][sx] != 0 {
						bits |= d.bit
					}
				}
			}
			s.tcell.SetContent(col, row, rune(0x2800+bits), nil, style)
		}
	}
}
