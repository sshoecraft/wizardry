package render

import (
	"fmt"

	"github.com/gdamore/tcell/v2"
	"wizardry/engine"
)

// overlayMonsterFromCombat shows the appropriate graphic in the viewport:
// - Monster groups alive → first group's monster pic
// - Chest phase → chest graphic (pic 18)
// - Victory phase → gold pile graphic (pic 19)
func overlayMonsterFromCombat(bmp *MazeBitmap, game *engine.GameState, combat *engine.CombatState) {
	// Chest/chest result → show chest graphic (pic 18)
	if combat.Phase == engine.CombatChest || combat.Phase == engine.CombatChestResult {
		if pic, ok := game.Scenario.MonsterPics[18]; ok && len(pic.Art) > 0 {
			bmp.OverlayMonsterArt(pic.Art, pic.Width)
		}
		return
	}

	// Victory → show gold pile (pic 19)
	if combat.Phase == engine.CombatVictory {
		if pic, ok := game.Scenario.MonsterPics[19]; ok && len(pic.Art) > 0 {
			bmp.OverlayMonsterArt(pic.Art, pic.Width)
		}
		return
	}

	// Combat → show first group's monster.
	// During CombatExecute, show regardless of alive count — messages haven't
	// displayed the kills yet, so the player shouldn't see them vanish early.
	for _, group := range combat.Groups {
		if group.AliveCount() == 0 {
			continue
		}
		if group.MonsterID < 0 || group.MonsterID >= len(game.Scenario.Monsters) {
			continue
		}
		mon := &game.Scenario.Monsters[group.MonsterID]
		pic, ok := game.Scenario.MonsterPics[mon.Pic]
		if !ok || len(pic.Art) == 0 {
			continue
		}
		bmp.OverlayMonsterArt(pic.Art, pic.Width)
		return
	}
}

// RenderCombat draws the combat screen.
//
// The combat screen uses the SAME bordered frame as the maze screen.
// The dungeon wireframe stays visible in the left panel (cols 1-11).
// Monster list replaces the maze menu in the right panel (col 13, rows 1-3).
// Action menu replaces the spell info (col 13, rows 6-9).
// Message area (rows 11-14) and party (rows 16-22) are identical to maze.
//
// From the Apple II original (verified by user):
//
//	Row 0:  +----------+---------------------------+
//	Row 1:  | 3D view  | 1) 2 SCRUFFY MEN (2)      |
//	Row 2:  |          |                            |
//	Row 3:  |          |                            |
//	Row 4:  |          |                            |
//	Row 5:  |          +---------------------------+
//	Row 6:  |          | RODON'S OPTIONS            |
//	Row 7:  |          |                            |
//	Row 8:  |          | F)IGHT  S)PELL  P)ARRY     |
//	Row 9:  |          | R)UN    U)SE               |
//	Row 10: +----------+---------------------------+
//	Row 11-14: message area
//	Row 15: +--------------------------------------+
//	Row 16: | # CHARACTER NAME  CLASS AC HITS STATUS|
//	Row 17-22: party members
//	Row 23: +--------------------------------------+
func (s *Screen) RenderCombat(game *engine.GameState) {
	s.Clear()

	combat := game.Combat
	if combat == nil {
		s.Show()
		return
	}

	white := base

	// Same layout as maze — uses VPScale
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
	actRow := rpDiv + 1

	// ═══ Sixel path: full screen as pixel image ═══
	if SixelSupported {
		si := s.buildCombatSixel(game, combat, divCol, rightC, viewRows,
			rpDiv, fullDiv, msgStart, partyDiv, partyStart, bottomRow, menuCol, actRow)
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

	// Frame borders
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

	// Lower area
	s.renderCombatLower(game, combat, white, divCol, rightC,
		fullDiv, msgStart, partyDiv, partyStart, bottomRow, menuCol)

	// 3D Dungeon View + Monster overlay
	vpWidth := divCol - 1
	level := game.CurrentLevel()
	if level != nil {
		canvas := NewCanvas(vpWidth*s.scale, viewRows)
		var bmp MazeBitmap
		DrawMaze(&bmp, level, game.PlayerX, game.PlayerY, game.Facing,
			game.LightLevel, game.QuickPlot)
		overlayMonsterFromCombat(&bmp, game, combat)
		bmp.BlitToCanvas(canvas)
		s.DrawCanvas(canvas, 1, 1, white)
	}

	// Monster Groups (tcell) — use DisplayAliveCount during CombatExecute
	// to prevent kills from showing before their message is displayed
	for i, group := range combat.Groups {
		if i >= rpDiv-1 {
			break
		}
		alive := combat.DisplayAliveCount(i)
		if alive == 0 {
			continue
		}
		name := group.DisplayName(game.Scenario.Monsters)
		line := fmt.Sprintf("%d) %d %s (%d)", i+1, alive, name, alive)
		maxLen := rightC - menuCol - 1
		if len(line) > maxLen {
			line = line[:maxLen]
		}
		s.DrawString(menuCol, 1+i, styleNormal, line)
	}

	// Action Area (tcell)
	s.renderCombatActionTcell(game, combat, menuCol, actRow, rightC, msgStart)

	s.Show()
}

// renderCombatActionTcell draws the action area text via tcell (non-sixel path).
func (s *Screen) renderCombatActionTcell(game *engine.GameState, combat *engine.CombatState,
	menuCol, actRow, rightC, msgStart int) {

	switch combat.Phase {
	case engine.CombatInit:
		msg := "AN ENCOUNTER"
		col := (rightC + 1 - len(msg)) / 2
		s.DrawString(col, msgStart+1, styleNormal, msg)
		if combat.Surprised == 1 {
			s.DrawString(1, msgStart+2, styleNormal, "YOU SURPRISED THE MONSTERS!")
		} else if combat.Surprised == 2 {
			s.DrawString(1, msgStart+2, styleNormal, "THE MONSTERS SURPRISED YOU!")
		}

	case engine.CombatFriendly:
		s.DrawString(menuCol, actRow, styleNormal, "A FRIENDLY GROUP OF")
		if len(combat.Groups) > 0 {
			groupName := combat.Groups[0].DisplayName(game.Scenario.Monsters)
			s.DrawString(menuCol, actRow+1, styleNormal, groupName)
		}
		s.DrawString(1, msgStart+1, styleNormal, "THEY HAIL YOU IN WELCOME!")
		s.DrawString(1, msgStart+2, styleNormal, "F)IGHT OR L)EAVE IN PEACE.")

	case engine.CombatChoose:
		if combat.CurrentActor < len(game.Town.Party.Members) {
			member := game.Town.Party.Members[combat.CurrentActor]
			if member != nil {
				if combat.InputtingSpell {
					s.DrawString(menuCol, actRow, styleNormal, fmt.Sprintf("%s:", member.Name))
					prompt := fmt.Sprintf("SPELL NAME ? >%s", combat.SpellInput)
					s.DrawString(menuCol, actRow+2, styleNormal, prompt)
					s.DrawString(menuCol+len(prompt), actRow+2, styleNormal, "_")
				} else if combat.SelectingSpellGroup {
					s.DrawString(menuCol, actRow+2, styleNormal, "CAST SPELL ON GROUP #?")
				} else if combat.SelectingSpellTarget {
					s.DrawString(menuCol, actRow+2, styleNormal, " CAST SPELL ON PERSON # ?")
				} else if combat.SelectingUseItem {
					s.DrawString(menuCol, actRow, styleNormal, "USE WHICH ITEM?")
					for i, idx := range combat.UsableItems {
						if idx < member.ItemCount {
							poss := member.Items[idx]
							if poss.ItemIndex > 0 && poss.ItemIndex < len(game.Scenario.Items) {
								item := &game.Scenario.Items[poss.ItemIndex]
								s.DrawString(menuCol, actRow+1+i, styleNormal,
									fmt.Sprintf("%d) %s", i+1, item.Name))
							}
						}
					}
				} else if combat.SelectingGroup {
					s.DrawString(menuCol, actRow+2, styleNormal, combat.GroupPrompt)
				} else {
					s.DrawString(menuCol, actRow, styleNormal, fmt.Sprintf("%s'S OPTIONS", member.Name))
					if combat.CurrentActor < 3 {
						s.DrawString(menuCol, actRow+2, styleNormal, "F)IGHT  S)PELL  P)ARRY")
					} else {
						s.DrawString(menuCol, actRow+2, styleNormal, "S)PELL  P)ARRY")
					}
					s.DrawString(menuCol, actRow+3, styleNormal, "R)UN    U)SE    D)ISPELL")
				}
			}
		}

	case engine.CombatConfirm:
		s.DrawString(menuCol, actRow, styleNormal, "PRESS [RETURN] TO FIGHT,")
		s.DrawString(menuCol, actRow+1, styleNormal, "OR")
		s.DrawString(menuCol, actRow+2, styleNormal, "GO B)ACK TO REDO OPTIONS")

	case engine.CombatExecute:
		// Messages shown below

	case engine.CombatChest:
		switch combat.ChestSubPhase {
		case engine.ChestMenu:
			s.DrawString(menuCol, actRow, styleNormal, "A CHEST! YOU MAY:")
			s.DrawString(menuCol, actRow+2, styleNormal, "O)PEN     C)ALFO   L)EAVE")
			s.DrawString(menuCol, actRow+3, styleNormal, "I)NSPECT  D)ISARM")
		case engine.ChestWhoOpen:
			s.DrawString(menuCol, actRow+2, styleNormal, "WHO (#) WILL OPEN?")
		case engine.ChestWhoCalfo:
			s.DrawString(menuCol, actRow+2, styleNormal, "WHO (#) WILL CAST CALFO?")
		case engine.ChestWhoInspect:
			s.DrawString(menuCol, actRow+2, styleNormal, "WHO (#) WILL INSPECT?")
		case engine.ChestWhoDisarm:
			s.DrawString(menuCol, actRow+2, styleNormal, "WHO (#) WILL DISARM?")
		}

	case engine.CombatChestResult:
		// Result in message area

	case engine.CombatVictory:
		s.DrawString(menuCol, 1, styleNormal, "FOR KILLING THE MONSTERS")
		s.DrawString(menuCol, 2, styleNormal, fmt.Sprintf("EACH SURVIVOR GETS %d", combat.TotalXP))
		s.DrawString(menuCol, 3, styleNormal, "EXPERIENCE POINTS")
		s.DrawString(1, msgStart+1, styleNormal, fmt.Sprintf("EACH SHARE IS WORTH %d GP!", combat.TotalGold))

	case engine.CombatDefeat:
		s.DrawString(1, msgStart, styleNormal, "TOTAL PARTY KILL")
		s.DrawString(1, msgStart+1, styleNormal, "THE PARTY HAS PERISHED")
	}
}

// renderCombatLower draws the lower portion of the combat screen (messages + party) via tcell.
func (s *Screen) renderCombatLower(game *engine.GameState, combat *engine.CombatState,
	white tcell.Style, divCol, rightC, fullDiv, msgStart, partyDiv, partyStart, bottomRow, menuCol int) {

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

	// Party rows + bottom
	for r := partyStart; r < bottomRow; r++ {
		s.SetCell(0, r, '│', white)
		s.SetCell(rightC, r, '│', white)
	}
	s.SetCell(0, bottomRow, '└', white)
	hbar(bottomRow, 1, rightC-1)
	s.SetCell(rightC, bottomRow, '┘', white)

	// Combat messages (some phases put text in message area)
	switch combat.Phase {
	case engine.CombatInit:
		msg := "AN ENCOUNTER"
		col := (rightC + 1 - len(msg)) / 2
		s.DrawString(col, msgStart+1, styleNormal, msg)
		if combat.Surprised == 1 {
			s.DrawString(1, msgStart+2, styleNormal, "YOU SURPRISED THE MONSTERS!")
		} else if combat.Surprised == 2 {
			s.DrawString(1, msgStart+2, styleNormal, "THE MONSTERS SURPRISED YOU!")
		}
	case engine.CombatFriendly:
		s.DrawString(1, msgStart+1, styleNormal, "THEY HAIL YOU IN WELCOME!")
		s.DrawString(1, msgStart+2, styleNormal, "F)IGHT OR L)EAVE IN PEACE.")
	case engine.CombatVictory:
		s.DrawString(1, msgStart+1, styleNormal, fmt.Sprintf("EACH SHARE IS WORTH %d GP!", combat.TotalGold))
	case engine.CombatDefeat:
		s.DrawString(1, msgStart, styleNormal, "TOTAL PARTY KILL")
		s.DrawString(1, msgStart+1, styleNormal, "THE PARTY HAS PERISHED")
	}

	// Combat execute/chest messages
	msgRows := partyDiv - msgStart
	if combat.Phase == engine.CombatExecute || combat.Phase == engine.CombatChestResult ||
		(combat.Phase == engine.CombatChoose && len(combat.Messages) > 0) {
		msgs := combat.Messages
		startIdx := combat.MessageIndex
		if startIdx > len(msgs) {
			startIdx = len(msgs)
		}
		row := 0
		for idx := startIdx; idx < len(msgs) && row < msgRows; idx++ {
			msg := msgs[idx]
			if msg == engine.ActionSeparator {
				break
			}
			maxLen := rightC - 2
			if len(msg) > maxLen {
				msg = msg[:maxLen]
			}
			s.DrawString(1, msgStart+row, styleNormal, msg)
			row++
		}
	}

	// Party
	s.DrawString(1, partyStart, styleDim, "# CHARACTER NAME  CLASS AC HITS STATUS")
	for i := 0; i < 6; i++ {
		if i < len(game.Town.Party.Members) && game.Town.Party.Members[i] != nil {
			m := game.Town.Party.Members[i]
			// During CombatExecute, use frozen snapshot — Pascal DSPPARTY
			// only runs at round start, not during MELEE
			dispM := m
			snap := combat.GetPartySnap(i)
			if snap != nil {
				tmp := *m
				tmp.HP = snap.HP
				tmp.MaxHP = snap.MaxHP
				tmp.Status = snap.Status
				tmp.AC = snap.AC
				tmp.PoisonAmt = snap.PoisonAmt
				dispM = &tmp
			} else if i < 6 {
				// Show combat-adjusted AC outside of execute phase
				tmp := *m
				tmp.AC = m.AC + combat.PartyACMod[i]
				dispM = &tmp
			}
			line := formatPartyLine(i+1, dispM)
			st := styleNormal
			if dispM.IsDead() {
				st = styleRed
			}
			if combat.Phase == engine.CombatChoose && i == combat.CurrentActor {
				st = base
			}
			s.DrawString(1, partyStart+1+i, st, line)
		}
	}
}

// buildCombatSixel builds a full-screen SixelImage for the combat screen.
func (s *Screen) buildCombatSixel(game *engine.GameState, combat *engine.CombatState,
	divCol, rightC, viewRows, rpDiv, fullDiv, msgStart, partyDiv, partyStart, bottomRow, menuCol, actRow int) *SixelImage {

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
	menuX := textX(menuCol)

	// ── Full frame borders ──
	si.DrawLine(pxMid(0), pyMid(0), pxMid(rightC), pyMid(0), fc)
	si.DrawLine(pxMid(0), pyMid(0), pxMid(0), pyMid(bottomRow), fc)
	si.DrawLine(pxMid(rightC), pyMid(0), pxMid(rightC), pyMid(bottomRow), fc)
	si.DrawLine(pxMid(divCol), pyMid(0), pxMid(divCol), pyMid(fullDiv), fc)
	si.DrawLine(pxMid(divCol), pyMid(rpDiv), pxMid(rightC), pyMid(rpDiv), fc)
	si.DrawLine(pxMid(0), pyMid(fullDiv), pxMid(rightC), pyMid(fullDiv), fc)
	si.DrawLine(pxMid(0), pyMid(partyDiv), pxMid(rightC), pyMid(partyDiv), fc)
	si.DrawLine(pxMid(0), pyMid(bottomRow), pxMid(rightC), pyMid(bottomRow), fc)

	// ── 3D Dungeon View + Monster overlay ──
	level := game.CurrentLevel()
	if level != nil {
		vpX := pxMid(0) + 2
		vpY := pyMid(0) + 2
		vpW := pxMid(divCol) - vpX - 2
		vpH := pyMid(fullDiv) - vpY - 2
		var bmp MazeBitmap
		DrawMaze(&bmp, level, game.PlayerX, game.PlayerY, game.Facing,
			game.LightLevel, game.QuickPlot)
		overlayMonsterFromCombat(&bmp, game, combat)
		bmp.BlitToSixel(si, vpX, vpY, vpW, vpH, fc)
	}

	// ── Monster groups (rows 1 to rpDiv-1) ──
	for i, group := range combat.Groups {
		if i >= rpDiv-1 {
			break
		}
		alive := combat.DisplayAliveCount(i)
		if alive == 0 {
			continue
		}
		name := group.DisplayName(game.Scenario.Monsters)
		line := fmt.Sprintf("%d) %d %s (%d)", i+1, alive, name, alive)
		maxLen := rightC - menuCol - 1
		if len(line) > maxLen {
			line = line[:maxLen]
		}
		si.DrawText2x(menuX, textY(1+i), line, fc, charSp)
	}

	// ── Action area ──
	switch combat.Phase {
	case engine.CombatFriendly:
		si.DrawText2x(menuX, textY(actRow), "A FRIENDLY GROUP OF", fc, charSp)
		if len(combat.Groups) > 0 {
			groupName := combat.Groups[0].DisplayName(game.Scenario.Monsters)
			si.DrawText2x(menuX, textY(actRow+1), groupName, fc, charSp)
		}
	case engine.CombatChoose:
		if combat.CurrentActor < len(game.Town.Party.Members) {
			member := game.Town.Party.Members[combat.CurrentActor]
			if member != nil {
				if combat.InputtingSpell {
					si.DrawText2x(menuX, textY(actRow), fmt.Sprintf("%s:", member.Name), fc, charSp)
					prompt := fmt.Sprintf("SPELL NAME ? >%s_", combat.SpellInput)
					si.DrawText2x(menuX, textY(actRow+2), prompt, fc, charSp)
				} else if combat.SelectingSpellGroup {
					si.DrawText2x(menuX, textY(actRow+2), "CAST SPELL ON GROUP #?", fc, charSp)
				} else if combat.SelectingSpellTarget {
					si.DrawText2x(menuX, textY(actRow+2), " CAST SPELL ON PERSON # ?", fc, charSp)
				} else if combat.SelectingUseItem {
					si.DrawText2x(menuX, textY(actRow), "USE WHICH ITEM?", fc, charSp)
					for i, idx := range combat.UsableItems {
						if idx < member.ItemCount {
							poss := member.Items[idx]
							if poss.ItemIndex > 0 && poss.ItemIndex < len(game.Scenario.Items) {
								item := &game.Scenario.Items[poss.ItemIndex]
								si.DrawText2x(menuX, textY(actRow+1+i),
									fmt.Sprintf("%d) %s", i+1, item.Name), fc, charSp)
							}
						}
					}
				} else if combat.SelectingGroup {
					si.DrawText2x(menuX, textY(actRow+2), combat.GroupPrompt, fc, charSp)
				} else {
					si.DrawText2x(menuX, textY(actRow), fmt.Sprintf("%s'S OPTIONS", member.Name), fc, charSp)
					if combat.CurrentActor < 3 {
						si.DrawText2x(menuX, textY(actRow+2), "F)IGHT  S)PELL  P)ARRY", fc, charSp)
					} else {
						si.DrawText2x(menuX, textY(actRow+2), "S)PELL  P)ARRY", fc, charSp)
					}
					si.DrawText2x(menuX, textY(actRow+3), "R)UN    U)SE    D)ISPELL", fc, charSp)
				}
			}
		}
	case engine.CombatConfirm:
		si.DrawText2x(menuX, textY(actRow), "PRESS [RETURN] TO FIGHT,", fc, charSp)
		si.DrawText2x(menuX, textY(actRow+1), "OR", fc, charSp)
		si.DrawText2x(menuX, textY(actRow+2), "GO B)ACK TO REDO OPTIONS", fc, charSp)
	case engine.CombatChest:
		switch combat.ChestSubPhase {
		case engine.ChestMenu:
			si.DrawText2x(menuX, textY(actRow), "A CHEST! YOU MAY:", fc, charSp)
			si.DrawText2x(menuX, textY(actRow+2), "O)PEN     C)ALFO   L)EAVE", fc, charSp)
			si.DrawText2x(menuX, textY(actRow+3), "I)NSPECT  D)ISARM", fc, charSp)
		case engine.ChestWhoOpen:
			si.DrawText2x(menuX, textY(actRow+2), "WHO (#) WILL OPEN?", fc, charSp)
		case engine.ChestWhoCalfo:
			si.DrawText2x(menuX, textY(actRow+2), "WHO (#) WILL CAST CALFO?", fc, charSp)
		case engine.ChestWhoInspect:
			si.DrawText2x(menuX, textY(actRow+2), "WHO (#) WILL INSPECT?", fc, charSp)
		case engine.ChestWhoDisarm:
			si.DrawText2x(menuX, textY(actRow+2), "WHO (#) WILL DISARM?", fc, charSp)
		}
	case engine.CombatVictory:
		si.DrawText2x(menuX, textY(1), "FOR KILLING THE MONSTERS", fc, charSp)
		si.DrawText2x(menuX, textY(2), fmt.Sprintf("EACH SURVIVOR GETS %d", combat.TotalXP), fc, charSp)
		si.DrawText2x(menuX, textY(3), "EXPERIENCE POINTS", fc, charSp)
	}

	// ── Message area ──
	switch combat.Phase {
	case engine.CombatInit:
		msg := "AN ENCOUNTER"
		col := (rightC + 1 - len(msg)) / 2
		si.DrawText2x(textX(col), textY(msgStart+1), msg, fc, charSp)
		if combat.Surprised == 1 {
			si.DrawText2x(textX(1), textY(msgStart+2), "YOU SURPRISED THE MONSTERS!", fc, charSp)
		} else if combat.Surprised == 2 {
			si.DrawText2x(textX(1), textY(msgStart+2), "THE MONSTERS SURPRISED YOU!", fc, charSp)
		}
	case engine.CombatFriendly:
		si.DrawText2x(textX(1), textY(msgStart+1), "THEY HAIL YOU IN WELCOME!", fc, charSp)
		si.DrawText2x(textX(1), textY(msgStart+2), "F)IGHT OR L)EAVE IN PEACE.", fc, charSp)
	case engine.CombatVictory:
		si.DrawText2x(textX(1), textY(msgStart+1), fmt.Sprintf("EACH SHARE IS WORTH %d GP!", combat.TotalGold), fc, charSp)
	case engine.CombatDefeat:
		si.DrawText2x(textX(1), textY(msgStart), "TOTAL PARTY KILL", fc, charSp)
		si.DrawText2x(textX(1), textY(msgStart+1), "THE PARTY HAS PERISHED", fc, charSp)
	}

	// Combat execute/chest messages
	msgRows := partyDiv - msgStart
	if combat.Phase == engine.CombatExecute || combat.Phase == engine.CombatChestResult ||
		(combat.Phase == engine.CombatChoose && len(combat.Messages) > 0) {
		msgs := combat.Messages
		startIdx := combat.MessageIndex
		if startIdx > len(msgs) {
			startIdx = len(msgs)
		}
		row := 0
		for idx := startIdx; idx < len(msgs) && row < msgRows; idx++ {
			msg := msgs[idx]
			if msg == engine.ActionSeparator {
				break
			}
			maxLen := rightC - 2
			if len(msg) > maxLen {
				msg = msg[:maxLen]
			}
			si.DrawText2x(textX(1), textY(msgStart+row), msg, fc, charSp)
			row++
		}
	}

	// ── Party roster ──
	dc := sixelDim
	si.DrawText2x(textX(1), textY(partyStart), "# CHARACTER NAME  CLASS AC HITS STATUS", dc, charSp)
	for i := 0; i < 6; i++ {
		if i < len(game.Town.Party.Members) && game.Town.Party.Members[i] != nil {
			m := game.Town.Party.Members[i]
			dispM := m
			snap := combat.GetPartySnap(i)
			if snap != nil {
				tmp := *m
				tmp.HP = snap.HP
				tmp.MaxHP = snap.MaxHP
				tmp.Status = snap.Status
				tmp.AC = snap.AC
				tmp.PoisonAmt = snap.PoisonAmt
				dispM = &tmp
			} else if i < 6 {
				tmp := *m
				tmp.AC = m.AC + combat.PartyACMod[i]
				dispM = &tmp
			}
			line := formatPartyLine(i+1, dispM)
			si.DrawText2x(textX(1), textY(partyStart+1+i), line, fc, charSp)
		}
	}

	return si
}
