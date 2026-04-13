package render

import (
	"github.com/gdamore/tcell/v2"
	"wizardry/data"
	"wizardry/engine"
)

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

	case engine.TitleArt:
		// Non-sixel: render from bitmap if available.
		animRow := title.AnimRow
		revealFrac := 1.0
		if animRow > 0 {
			revealFrac = 1.0 - float64(animRow)/24.0
		}

		tb := game.Scenario.Title
		if tb != nil && len(tb.Pixels) > 0 {
			startSrcY := int(float64(tb.Height) * (1.0 - revealFrac))
			s.renderTitleCanvas(tb, startSrcY, green)
			s.Show()
			return
		}

		// Fallback: hardcoded Unicode half-block art.
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
		// Copyright + version + menu.
		// From p-code OPTIONS (seg 3), exact strings at offsets 43-522:
		y := 1
		s.DrawString(0, y, white, "COPYRIGHT (C)1981 ALL RIGHTS RESERVED BY")
		y++
		s.DrawString(0, y, white, "ANDREW GREENBERG, INC & ROBERT WOODHEAD,")
		y++
		s.DrawString(0, y, white, "INC.  THIS PROGRAM  IS  PROTECTED  UNDER")
		y++
		s.DrawString(0, y, white, "THE LAWS OF THE UNITED STATES  AND OTHER")
		y++
		s.DrawString(0, y, white, "COUNTRIES,  AND ILLEGAL DISTRIBUTION MAY")
		y++
		s.DrawString(0, y, white, "RESULT IN CIVIL  LIABILITY  AND CRIMINAL")
		y++
		s.DrawString(0, y, white, "PROSECUTION.")
		y += 2
		s.DrawString(0, y, white, "  VERSION 2.1 OF 22-JAN-82")
		y += 4
		s.DrawString(0, y, white, "  S)TART GAME  U)TILITIES  T)ITLE PAGE")
	}

	s.Show()
}

// buildTitleSixel renders the Apple II title bitmap as a full-screen sixel image.
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
