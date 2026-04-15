// Package render handles terminal display using tcell.
package render

import (
	"fmt"
	"os"

	"github.com/gdamore/tcell/v2"
	"wizardry/engine"
)

// Screen manages the tcell terminal screen and rendering.
type Screen struct {
	tcell    tcell.Screen
	width    int
	height   int
	scale    int     // horizontal scale factor (1=normal, 2=double-wide)
	VPScale  float64 // viewport scale for maze/combat (1.0=standard, 1.5=50% larger)
	lastSixel bool   // true if the last render emitted sixel graphics
}

// NewScreen initializes the terminal screen.
func NewScreen() (*Screen, error) {
	s, err := tcell.NewScreen()
	if err != nil {
		return nil, fmt.Errorf("tcell init: %w", err)
	}
	if err := s.Init(); err != nil {
		return nil, fmt.Errorf("tcell screen init: %w", err)
	}
	s.SetStyle(tcell.StyleDefault.Background(tcell.ColorBlack).Foreground(phosphor))
	s.Clear()

	// Force the terminal's true background to black (OSC 11).
	// Sixel transparent pixels reveal the terminal's actual background,
	// not tcell's painted background. Without this, terminals with
	// non-black profiles show visible artifacts under sixel images.
	fmt.Fprintf(os.Stdout, "\x1b]11;#000000\x07")
	os.Stdout.Sync()

	w, h := s.Size()
	return &Screen{
		tcell:   s,
		width:   w,
		height:  h,
		scale:   2,
		VPScale: 1.0,
	}, nil
}

// Close shuts down the terminal screen.
func (s *Screen) Close() {
	// Restore the terminal's original background color (OSC 111)
	fmt.Fprintf(os.Stdout, "\x1b]111\x07")
	os.Stdout.Sync()
	s.tcell.Fini()
}

// Clear clears the tcell buffer.
func (s *Screen) Clear() {
	s.tcell.Clear()
}

// MarkSixel records that this frame emitted sixel graphics.
func (s *Screen) MarkSixel() {
	s.lastSixel = true
}

// ClearSixelTransition wipes the terminal graphics layer ONLY when
// transitioning from a sixel frame to a non-sixel frame.
// Called by non-sixel render paths. Sixel paths never call this.
func (s *Screen) ClearSixelTransition() {
	if s.lastSixel {
		fmt.Fprintf(os.Stdout, "\x1b[2J")
		os.Stdout.Sync()
		s.lastSixel = false
	}
}

// Show flushes all pending changes to the terminal.
func (s *Screen) Show() {
	s.tcell.Show()
}

// Size returns the terminal dimensions.
func (s *Screen) Size() (int, int) {
	return s.tcell.Size()
}

// Colors returns the number of colors the terminal supports.
func (s *Screen) Colors() int {
	return s.tcell.Colors()
}

// PollEvent waits for and returns the next terminal event.
func (s *Screen) PollEvent() tcell.Event {
	return s.tcell.PollEvent()
}

// Beep sends an audible bell to the terminal (ASCII BEL).
// Matches original Apple II WRITE(CHR(7)) behavior.
func (s *Screen) Beep() {
	s.tcell.Beep()
}

// DrawString writes a string at the given position.
// When scale > 1, x is in logical coordinates and each character
// occupies `scale` screen columns (char + padding).
func (s *Screen) DrawString(x, y int, style tcell.Style, str string) {
	sx := x * s.scale
	for i, ch := range str {
		col := sx + i*s.scale
		s.tcell.SetContent(col, y, ch, nil, style)
		for d := 1; d < s.scale; d++ {
			s.tcell.SetContent(col+d, y, ' ', nil, style)
		}
	}
}

// DrawStringRaw writes a string at raw screen coordinates (no scaling).
// Use for prose text that should not be character-spaced.
func (s *Screen) DrawStringRaw(x, y int, style tcell.Style, str string) {
	for i, ch := range str {
		s.tcell.SetContent(x+i, y, ch, nil, style)
	}
}

// SetCell draws a single character at logical position x.
// Horizontal borders (─) fill scale columns. Vertical borders (│) draw once.
func (s *Screen) SetCell(x, y int, ch rune, style tcell.Style) {
	sx := x * s.scale
	s.tcell.SetContent(sx, y, ch, nil, style)
	if s.scale > 1 {
		// Horizontal-spanning chars fill the extra column.
		// Right-edge caps (┐ ┤ ┘) and vertical chars (│ !) don't extend.
		switch ch {
		case '─', '┌', '┬', '├', '┼', '└', '┴':
			s.tcell.SetContent(sx+1, y, '─', nil, style)
		case '-', '+':
			s.tcell.SetContent(sx+1, y, '-', nil, style)
		}
	}
}

// DrawBox draws a Unicode box at the given position.
func (s *Screen) DrawBox(x, y, w, h int, style tcell.Style) {
	// Corners
	s.tcell.SetContent(x, y, '┌', nil, style)
	s.tcell.SetContent(x+w-1, y, '┐', nil, style)
	s.tcell.SetContent(x, y+h-1, '└', nil, style)
	s.tcell.SetContent(x+w-1, y+h-1, '┘', nil, style)
	// Horizontal edges
	for i := x + 1; i < x+w-1; i++ {
		s.tcell.SetContent(i, y, '─', nil, style)
		s.tcell.SetContent(i, y+h-1, '─', nil, style)
	}
	// Vertical edges
	for j := y + 1; j < y+h-1; j++ {
		s.tcell.SetContent(x, j, '│', nil, style)
		s.tcell.SetContent(x+w-1, j, '│', nil, style)
	}
}

// RenderCamp draws the camp screen (from p-code CAMP segment).
func (s *Screen) RenderCamp(game *engine.GameState) {
	s.Clear()
	s.ClearSixelTransition()

	// Camp equip member selection (EquipCategory == -1, no EditChar yet)
	if game.Town.InputMode == engine.InputEquip && game.Town.EquipCategory == -1 {
		// Show camp party list with "EQUIP WHO?" prompt
		// Fall through to normal camp rendering — message shows the prompt
	} else if game.Town.EditChar != nil {
		// If inspecting/equipping a character in camp, use shared screens
		switch game.Town.InputMode {
		case engine.InputEquip:
			s.renderEquipScreen(game)
			s.Show()
			return
		case engine.InputInspect, engine.InputDrop,
			engine.InputTrade, engine.InputTradeGold, engine.InputTradeTarget,
			engine.InputCastSpell, engine.InputSpellTarget, engine.InputUseItem:
			s.renderInspectScreen(game)
			s.Show()
			return
		case engine.InputMalor:
			s.renderMalorScreen(game)
			s.Show()
			return
		case engine.InputSpellBooks:
			s.renderSpellBooksScreen(game)
			s.Show()
			return
		case engine.InputSpellList:
			s.renderSpellListScreen(game)
			s.Show()
			return
		}
	}

	// CAMP header — p-code: WRITESTR("CAMP":22) right-justified in 22 chars
	// Places "CAMP" at cols 18-21 (centered on 40-column screen)
	s.DrawString(0, 0, styleTitle, fmt.Sprintf("%22s", "CAMP"))

	// Column header + party (same as town, no borders)
	s.DrawString(0, 2, styleDim, " # CHARACTER NAME  CLASS AC HITS STATUS")
	for i := 0; i < 6; i++ {
		if i < len(game.Town.Party.Members) && game.Town.Party.Members[i] != nil {
			m := game.Town.Party.Members[i]
			line := formatPartyLine(i+1, m)
			st := styleNormal
			if m.IsDead() {
				st = styleRed
			}
			s.DrawString(0, 3+i, st, line)
		}
	}

	if game.Town.InputMode == engine.InputReorder {
		// Reorder mode — from UTILITIE p-code byte 7121
		// "REORDERING" centered at row 11, then numbered slots at rows 13+
		s.DrawString(15, 11, styleNormal, "REORDERING")
		partySize := game.Town.Party.Size()
		for i := 0; i < partySize; i++ {
			row := 13 + i
			if i < len(game.Town.ReorderResult) {
				// Already placed — show "N) NAME"
				s.DrawString(0, row, styleNormal,
					fmt.Sprintf("%d) %s", i+1, game.Town.ReorderResult[i].Name))
			} else if i == game.Town.ReorderPos {
				// Current slot — show "N) >"
				s.DrawString(0, row, styleNormal, fmt.Sprintf("%d) >", i+1))
			} else {
				// Future slot — show "N)"
				s.DrawString(0, row, styleNormal, fmt.Sprintf("%d)", i+1))
			}
		}
	} else {
		// Camp menu — from p-code CAMP segment: GOTOXY(0, 12)
		y := 12
		s.DrawString(0, y, styleNormal, "YOU MAY R)EORDER, E)QUIP, D)ISBAND,")
		y++
		s.DrawString(8, y, styleNormal, "#) TO INSPECT, OR")
		y++
		s.DrawString(8, y, styleNormal, "L)EAVE THE CAMP.")

		if game.Town.Message != "" {
			y += 2
			s.DrawString(0, y, styleGold, game.Town.Message)
		}
	}

	s.Show()
}

// RenderMaze draws the maze screen — exact 40×24 layout from p-code RUNNER segment.
//
// From CXP 1:30 calls and CXP 1:31 text positions:
//
//	Row 0:  ┌───────────┬──────────────────────────┐  cols 0,12,39
//	Row 1:  │  3D view  │ F)ORWARD  C)AMP  S)TATUS │  menu at col 13
//	Row 2:  │           │ L)EFT     Q)UICK  A<-W>D │
//	Row 3:  │           │ R)IGHT    T)IME  CLUSTER  │
//	Row 4:  │           │ K)ICK     I)NSPECT        │
//	Row 5:  │           ├──────────────────────────┤  right panel divider
//	Row 6:  │           │                           │
//	Row 7:  │           │ SPELLS :  LIGHT           │  spells at col 13
//	Row 8:  │           │           PROTECT         │  col 22
//	Row 9:  │           │                           │
//	Row 10: ├───────────┴──────────────────────────┤  full divider
//	Row 11: │                                       │  message area
//	Row 12: │ STAIRS GOING UP.                      │  (col 1)
//	Row 13: │ TAKE THEM (Y/N) ?                     │  (col 1)
//	Row 14: │                                       │
//	Row 15: ├──────────────────────────────────────┤  party divider
//	Row 16: │ # CHARACTER NAME  CLASS AC HITS STATU │
//	Row 17: │ 1 RODON           G-SAM  2   26   26 │
//	Row 18-22: (party slots)
//	Row 23: └──────────────────────────────────────┘
func (s *Screen) RenderMaze(game *engine.GameState) {
	s.Clear()

	// I)NSPECT dungeon square — full separate screen (no sixel)
	if game.MazeInspecting {
		s.ClearSixelTransition()
		s.DrawString(0, 0, styleNormal, "FOUND:")
		if len(game.MazeInspectFound) == 0 {
			s.DrawString(0, 5, styleNormal, "** NO ONE **")
			s.DrawString(0, 20, styleNormal, "OPTIONS: L)EAVE")
		} else {
			for i, c := range game.MazeInspectFound {
				s.DrawString(0, 5+i, styleNormal, fmt.Sprintf("%s LEVEL %d %s %s (%s)",
					c.Name, c.Level, c.Race, c.Class, c.Status))
			}
			s.DrawString(0, 20, styleNormal, "OPTIONS: P)ICK UP, L)EAVE")
		}
		s.Show()
		return
	}

	white := base
	yellow := base

	// Layout scales with VPScale (--vpscale arg). 1.0 = standard Apple II 40×24.
	var divCol, rightC, viewRows int
	var rpDiv, fullDiv, msgStart, partyDiv, partyStart, bottomRow int

	if s.VPScale > 1.0 {
		baseDivCol := 12
		baseViewRows := 9
		divCol = int(float64(baseDivCol) * s.VPScale)
		viewRows = int(float64(baseViewRows) * s.VPScale)
		extraCols := divCol - baseDivCol
		rightC = 39 + extraCols
		extraRows := viewRows - baseViewRows
		rpDiv = 5 + extraRows/2
		fullDiv = viewRows + 1
		msgStart = fullDiv + 1
		partyDiv = msgStart + 4
		partyStart = partyDiv + 1
		bottomRow = partyStart + 7
	} else {
		divCol = 12
		rightC = 39
		viewRows = 9
		rpDiv = 5
		fullDiv = 10
		msgStart = 11
		partyDiv = 15
		partyStart = 16
		bottomRow = 23
	}

	menuCol := divCol + 1

	// ═══ Sixel path: full screen as pixel image ═══
	if SixelSupported {
		si := s.buildMazeSixel(game, divCol, rightC, viewRows,
			rpDiv, fullDiv, msgStart, partyDiv, partyStart, bottomRow)
		s.Show()
		WriteSixel(0, si.Encode())
		s.MarkSixel()
		return
	}

	// ═══ Unicode half-block path (no sixel) ═══
	s.ClearSixelTransition()
	hbar := func(y, a, b int) {
		for c := a; c <= b; c++ {
			s.SetCell(c, y, '─', white)
		}
	}

	// ── Row 0: top border ──
	s.SetCell(0, 0, '┌', white)
	hbar(0, 1, divCol-1)
	s.SetCell(divCol, 0, '┬', white)
	hbar(0, divCol+1, rightC-1)
	s.SetCell(rightC, 0, '┐', white)

	for r := 1; r < rpDiv; r++ {
		s.SetCell(0, r, '│', white)
		s.SetCell(divCol, r, '│', white)
		s.SetCell(rightC, r, '│', white)
	}

	s.SetCell(0, rpDiv, '│', white)
	s.SetCell(divCol, rpDiv, '├', white)
	hbar(rpDiv, divCol+1, rightC-1)
	s.SetCell(rightC, rpDiv, '┤', white)

	for r := rpDiv + 1; r < fullDiv; r++ {
		s.SetCell(0, r, '│', white)
		s.SetCell(divCol, r, '│', white)
		s.SetCell(rightC, r, '│', white)
	}

	s.SetCell(0, fullDiv, '├', white)
	hbar(fullDiv, 1, divCol-1)
	s.SetCell(divCol, fullDiv, '┴', white)
	hbar(fullDiv, divCol+1, rightC-1)
	s.SetCell(rightC, fullDiv, '┤', white)

	// ── Message area + party borders (shared) ──
	s.renderMazeLower(game, white, yellow, divCol, rightC,
		fullDiv, msgStart, partyDiv, partyStart, bottomRow)

	// ═══ 3D Dungeon View ═══
	vpWidth := (divCol - 1)
	level := game.CurrentLevel()
	if level != nil {
		canvas := NewCanvas(vpWidth*s.scale, viewRows)
		RenderDungeon(canvas, level, game.PlayerX, game.PlayerY, game.Facing,
			game.LightLevel, game.QuickPlot)
		s.DrawCanvas(canvas, 1, 1, white)
	}

	// Viewport message (OUCH!, etc.) — centered in the 3D viewport area
	// Pascal VERYDARK uses GOTOXY(2,5) and GOTOXY(2,6) for two-line messages.
	// Single-line messages (OUCH!) center at row 5 (viewport midpoint).
	if game.ViewportMsg != "" {
		msgX := 1 + (vpWidth-len(game.ViewportMsg))/2
		msgY := 1 + viewRows/2
		s.DrawString(msgX, msgY, styleNormal, game.ViewportMsg)
		if game.ViewportMsg2 != "" {
			msg2X := 1 + (vpWidth-len(game.ViewportMsg2))/2
			s.DrawString(msg2X, msgY+1, styleNormal, game.ViewportMsg2)
		}
	}

	// ═══ Right panel menu ═══
	s.DrawString(menuCol, 1, styleNormal, "F)ORWARD  C)AMP    S)TATUS")
	s.DrawString(menuCol, 2, styleNormal, "L)EFT     Q)UICK   A<-W->D")
	s.DrawString(menuCol, 3, styleNormal, "R)IGHT    T)IME    CLUSTER")
	s.DrawString(menuCol, 4, styleNormal, "K)ICK     I)NSPECT")

	// ═══ Right panel spells ═══
	spellRow := rpDiv + 2
	s.DrawString(menuCol, spellRow, styleNormal, "SPELLS :")
	if game.LightLevel > 0 {
		s.DrawString(menuCol+9, spellRow, styleNormal, "LIGHT")
	}
	if game.ProtectLevel > 0 {
		s.DrawString(menuCol+9, spellRow+1, styleNormal, "PROTECT")
	}

	s.Show()
}

// renderMazeLower draws the lower portion of the maze screen (below fullDiv) via tcell.
// Shared between sixel and non-sixel paths.
func (s *Screen) renderMazeLower(game *engine.GameState, white, yellow tcell.Style,
	divCol, rightC, fullDiv, msgStart, partyDiv, partyStart, bottomRow int) {

	hbar := func(y, a, b int) {
		for c := a; c <= b; c++ {
			s.SetCell(c, y, '─', white)
		}
	}

	// Message area borders
	for r := msgStart; r < partyDiv; r++ {
		s.SetCell(0, r, '│', white)
		s.SetCell(rightC, r, '│', white)
	}

	// Party divider
	s.SetCell(0, partyDiv, '├', white)
	hbar(partyDiv, 1, rightC-1)
	s.SetCell(rightC, partyDiv, '┤', white)

	// Party rows
	for r := partyStart; r < bottomRow; r++ {
		s.SetCell(0, r, '│', white)
		s.SetCell(rightC, r, '│', white)
	}

	// Bottom border
	s.SetCell(0, bottomRow, '└', white)
	hbar(bottomRow, 1, rightC-1)
	s.SetCell(rightC, bottomRow, '┘', white)

	// Messages
	if game.MazeDelayInput {
		s.DrawString(1, msgStart+2, styleNormal, game.MazeMessage+game.MazeDelayBuf+"_")
	} else if len(game.MazeMessages) > 0 {
		// Multi-line SCNMSG display with pagination
		maxLines := 4
		if game.MazeMsgWait {
			maxLines = 3 // reserve last line for "[RET] FOR MORE"
		}
		for i := 0; i < maxLines; i++ {
			idx := game.MazeMsgScroll + i
			if idx < len(game.MazeMessages) {
				s.DrawString(1, msgStart+i, yellow, game.MazeMessages[idx])
			}
		}
		if game.MazeMsgWait {
			s.DrawString(1, msgStart+3, styleNormal, "[RET] FOR MORE")
		}
	} else {
		if game.MazeMessage != "" {
			s.DrawString(1, msgStart, yellow, game.MazeMessage)
		}
		if game.MazeMessage2 != "" {
			s.DrawString(1, msgStart+1, yellow, game.MazeMessage2)
		}
	}

	// Party
	s.DrawString(1, partyStart, styleDim, "# CHARACTER NAME  CLASS AC HITS STATUS")
	for i := 0; i < 6; i++ {
		if i < len(game.Town.Party.Members) && game.Town.Party.Members[i] != nil {
			m := game.Town.Party.Members[i]
			line := formatPartyLine(i+1, game.Town.Party.Members[i])
			st := styleNormal
			if m.IsDead() {
				st = styleRed
			}
			s.DrawString(1, partyStart+1+i, st, line)
		}
	}
}

// buildMazeSixel builds a full-screen SixelImage for the maze screen.
// Renders the entire frame: borders, 3D dungeon, menu, spells, messages, party.
func (s *Screen) buildMazeSixel(game *engine.GameState,
	divCol, rightC, viewRows, rpDiv, fullDiv, msgStart, partyDiv, partyStart, bottomRow int) *SixelImage {

	cw := CellWidth
	ch := CellHeight
	sc := s.scale

	imgW := (rightC + 1) * sc * cw
	imgH := (bottomRow + 1) * ch
	if imgH%6 != 0 {
		imgH += 6 - imgH%6
	}

	si := NewSixelImage(imgW, imgH)

	pxMid := func(logCol int) int { return logCol*sc*cw + sc*cw/2 }
	pyMid := func(row int) int { return row*ch + ch/2 }

	fc := sixelFG
	charSp := sc * cw
	textYOff := (ch - 16) / 2
	textX := func(logCol int) int { return logCol * sc * cw }
	textY := func(row int) int { return row*ch + textYOff }

	// ── Full frame borders ──
	// Top border
	si.DrawLine(pxMid(0), pyMid(0), pxMid(rightC), pyMid(0), fc)
	// Left vertical (full height)
	si.DrawLine(pxMid(0), pyMid(0), pxMid(0), pyMid(bottomRow), fc)
	// Right vertical (full height)
	si.DrawLine(pxMid(rightC), pyMid(0), pxMid(rightC), pyMid(bottomRow), fc)
	// DivCol vertical (upper area only)
	si.DrawLine(pxMid(divCol), pyMid(0), pxMid(divCol), pyMid(fullDiv), fc)
	// Right panel horizontal divider
	si.DrawLine(pxMid(divCol), pyMid(rpDiv), pxMid(rightC), pyMid(rpDiv), fc)
	// Full divider (upper/lower boundary)
	si.DrawLine(pxMid(0), pyMid(fullDiv), pxMid(rightC), pyMid(fullDiv), fc)
	// Party divider
	si.DrawLine(pxMid(0), pyMid(partyDiv), pxMid(rightC), pyMid(partyDiv), fc)
	// Bottom border
	si.DrawLine(pxMid(0), pyMid(bottomRow), pxMid(rightC), pyMid(bottomRow), fc)

	// ── 3D Dungeon View ──
	level := game.CurrentLevel()
	if level != nil {
		vpX := pxMid(0) + 2
		vpY := pyMid(0) + 2
		vpW := pxMid(divCol) - vpX - 2
		vpH := pyMid(fullDiv) - vpY - 2
		RenderDungeonSixel(si, vpX, vpY, vpW, vpH,
			level, game.PlayerX, game.PlayerY, game.Facing, fc,
			game.LightLevel, game.QuickPlot)
	}

	// Viewport message (OUCH!, etc.) — centered in 3D viewport
	if game.ViewportMsg != "" {
		vpMsgX := textX(1 + (divCol-1-len(game.ViewportMsg))/2)
		vpMsgY := textY(1 + viewRows/2)
		si.DrawText2x(vpMsgX, vpMsgY, game.ViewportMsg, fc, charSp)
		if game.ViewportMsg2 != "" {
			vpMsg2X := textX(1 + (divCol-1-len(game.ViewportMsg2))/2)
			vpMsg2Y := textY(1 + viewRows/2 + 1)
			si.DrawText2x(vpMsg2X, vpMsg2Y, game.ViewportMsg2, fc, charSp)
		}
	}

	menuX := textX(divCol + 1)

	// ── Right panel menu (rows 1-4) ──
	si.DrawText2x(menuX, textY(1), "F)ORWARD  C)AMP    S)TATUS", fc, charSp)
	si.DrawText2x(menuX, textY(2), "L)EFT     Q)UICK   A<-W->D", fc, charSp)
	si.DrawText2x(menuX, textY(3), "R)IGHT    T)IME    CLUSTER", fc, charSp)
	si.DrawText2x(menuX, textY(4), "K)ICK     I)NSPECT", fc, charSp)

	// ── Spells ──
	spellRow := rpDiv + 2
	si.DrawText2x(menuX, textY(spellRow), "SPELLS :", fc, charSp)
	if game.LightLevel > 0 {
		si.DrawText2x(textX(divCol+1+9), textY(spellRow), "LIGHT", fc, charSp)
	}
	if game.ProtectLevel > 0 {
		si.DrawText2x(textX(divCol+1+9), textY(spellRow+1), "PROTECT", fc, charSp)
	}

	// ── Message area ──
	if game.MazeDelayInput {
		si.DrawText2x(textX(1), textY(msgStart+2), game.MazeMessage+game.MazeDelayBuf+"_", fc, charSp)
	} else if len(game.MazeMessages) > 0 {
		maxLines := 4
		if game.MazeMsgWait {
			maxLines = 3
		}
		for i := 0; i < maxLines; i++ {
			idx := game.MazeMsgScroll + i
			if idx < len(game.MazeMessages) {
				si.DrawText2x(textX(1), textY(msgStart+i), game.MazeMessages[idx], fc, charSp)
			}
		}
		if game.MazeMsgWait {
			si.DrawText2x(textX(1), textY(msgStart+3), "[RET] FOR MORE", sixelDim, charSp)
		}
	} else {
		if game.MazeMessage != "" {
			si.DrawText2x(textX(1), textY(msgStart), game.MazeMessage, fc, charSp)
		}
		if game.MazeMessage2 != "" {
			si.DrawText2x(textX(1), textY(msgStart+1), game.MazeMessage2, fc, charSp)
		}
	}

	// ── Party roster ──
	dc := sixelDim
	si.DrawText2x(textX(1), textY(partyStart), "# CHARACTER NAME  CLASS AC HITS STATUS", dc, charSp)
	for i := 0; i < 6; i++ {
		if i < len(game.Town.Party.Members) && game.Town.Party.Members[i] != nil {
			m := game.Town.Party.Members[i]
			line := formatPartyLine(i+1, m)
			si.DrawText2x(textX(1), textY(partyStart+1+i), line, fc, charSp)
		}
	}

	return si
}

// formatPartyLine builds the formatted party member string.
// From p-code CASTLE proc 35 (IC 788-848):
//   STATUS column: OK chars show maxHP (WRITEINT width 4),
//   poisoned chars show "POISON", others show status text.
//   HP uses WRITEINT width 5.
func formatPartyLine(slot int, m *engine.Character) string {
	ac := engine.AlignClass(m.Alignment, m.Class)
	name := m.Name
	if len(name) > 15 {
		name = name[:15]
	}
	acStr := fmt.Sprintf("%2d", m.AC)
	if m.AC <= -10 {
		acStr = "LO"
	}
	// Status column from Apple II original:
	// OK: "  HP  maxHP"
	// Poisoned: "  HP-POISON" (hyphen, no space)
	// Dead/other: "  HP  STATUS"
	if m.Status == engine.OK && m.PoisonAmt > 0 {
		return fmt.Sprintf(" %d %-15s %-5s %s%4d-POISON",
			slot, name, ac, acStr, m.HP)
	} else if m.Status == engine.OK {
		return fmt.Sprintf(" %d %-15s %-5s %s%4d %4d",
			slot, name, ac, acStr, m.HP, m.MaxHP)
	}
	status := m.Status.String()
	if len(status) > 6 {
		status = status[:6]
	}
	return fmt.Sprintf(" %d %-15s %-5s %s%4d %s",
		slot, name, ac, acStr, m.HP, status)
}

// drawPartyMemberLine draws a single party member line at the given position.
func (s *Screen) drawPartyMemberLine(x, y, slot int, m *engine.Character) {
	st := styleNormal
	if m.IsDead() {
		st = styleRed
	}
	s.DrawString(x, y, st, formatPartyLine(slot, m))
}

