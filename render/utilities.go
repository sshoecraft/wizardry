package render

import (
	"fmt"

	"github.com/gdamore/tcell/v2"
	"wizardry/engine"
)

// drawRaw writes a string at screen coordinates without scaling.
// Used for utilities screens where file paths need full 80-col width.
func (s *Screen) drawRaw(x, y int, style tcell.Style, str string) {
	for i, ch := range str {
		s.tcell.SetContent(x+i, y, ch, nil, style)
	}
}

// RenderUtilities draws the utilities screen at scale=1 (no double-wide).
// Utilities need full 80-col width for file paths.
// From p-code UTILS (SYSTEM.STARTUP seg 1).
func (s *Screen) RenderUtilities(game *engine.GameState) {
	s.Clear()
	s.ClearSixelTransition()

	util := game.Util
	if util == nil {
		s.Show()
		return
	}

	switch util.Step {
	case engine.UtilMenu:
		s.drawRaw(22, 1, styleTitle, "WIZARDRY UTILITIES")
		s.drawRaw(24, 3, styleNormal, "CHOOSE AN OPTION")
		s.drawRaw(12, 6, styleNormal, "B)ACKUP       C)HANGE NAMES")
		s.drawRaw(12, 8, styleNormal, "I)MPORT       T)RANSFER")
		s.drawRaw(12, 10, styleNormal, "L)EAVE")

	case engine.UtilBackup:
		s.drawRaw(28, 1, styleTitle, "CHAR BACKUP")
		s.drawRaw(20, 5, styleNormal, "T)O OR F)ROM BACKUP")
		s.drawRaw(12, 8, styleDim, "OR [ESC] TO EXIT")

	case engine.UtilBackupTo:
		s.drawRaw(28, 1, styleTitle, "BACKUP TO")
		s.drawRaw(2, 4, styleNormal, "ENTER PATH FOR BACKUP FILE:")
		s.drawRaw(2, 6, styleNormal, "> "+util.InputBuf)
		s.drawRaw(4+len(util.InputBuf), 6, styleGold, "_")
		if util.Message != "" {
			s.drawRaw(2, 9, styleGold, util.Message)
		}

	case engine.UtilBackupFrom:
		s.drawRaw(22, 1, styleTitle, "RESTORE FROM BACKUP")
		s.drawRaw(2, 4, styleNormal, "ENTER PATH OF BACKUP FILE:")
		s.drawRaw(2, 6, styleNormal, "> "+util.InputBuf)
		s.drawRaw(4+len(util.InputBuf), 6, styleGold, "_")
		if util.Message != "" {
			s.drawRaw(2, 9, styleGold, util.Message)
		}

	case engine.UtilRename:
		s.drawRaw(26, 1, styleTitle, "CHANGE NAMES")
		s.drawRaw(2, 3, styleNormal, "SELECT CHARACTER (1-9) OR [ESC]:")
		y := 5
		for i, c := range game.Town.Roster.Characters {
			if c == nil || i >= 9 {
				continue
			}
			line := fmt.Sprintf("%d) %-15s L%d %s", i+1, c.Name, c.Level, c.Class)
			st := styleNormal
			if i == util.SelectedChar {
				st = styleGold
			}
			s.drawRaw(6, y, st, line)
			y++
		}

	case engine.UtilRenameNew:
		s.drawRaw(26, 1, styleTitle, "CHANGE NAMES")
		if util.SelectedChar >= 0 && util.SelectedChar < len(game.Town.Roster.Characters) {
			c := game.Town.Roster.Characters[util.SelectedChar]
			if c != nil {
				s.drawRaw(2, 4, styleNormal, fmt.Sprintf("RENAMING: %s", c.Name))
			}
		}
		s.drawRaw(2, 6, styleNormal, "NEW NAME > "+util.InputBuf)
		s.drawRaw(13+len(util.InputBuf), 6, styleGold, "_")
		if util.Message != "" {
			s.drawRaw(2, 9, styleGold, util.Message)
		}

	case engine.UtilImport:
		s.drawRaw(24, 1, styleTitle, "IMPORT FROM .DSK")
		s.drawRaw(2, 4, styleNormal, "ENTER PATH TO .DSK FILE:")
		s.drawRaw(2, 6, styleNormal, "> "+util.InputBuf)
		s.drawRaw(4+len(util.InputBuf), 6, styleGold, "_")
		if util.Message != "" {
			s.drawRaw(2, 9, styleRed, util.Message)
		}

	case engine.UtilImportResult:
		s.drawRaw(24, 1, styleTitle, "IMPORT RESULTS")
		for i, msg := range util.Messages {
			if i >= 20 {
				break
			}
			st := styleNormal
			if len(msg) > 8 && msg[len(msg)-8:] == "IMPORTED" {
				st = styleGreen
			}
			if len(msg) > 12 && msg[len(msg)-12:] == "NOT IMPORTING" {
				st = styleGold
			}
			s.drawRaw(2, 3+i, st, msg)
		}
		s.drawRaw(2, 23, styleDim, "PRESS ANY KEY TO CONTINUE")
	case engine.UtilTransfer:
		s.drawRaw(20, 1, styleTitle, "TRANSFER FROM SCENARIO")
		if len(util.TransferSources) == 0 {
			s.drawRaw(2, 5, styleNormal, "NO OTHER SCENARIOS WITH SAVED CHARACTERS.")
			s.drawRaw(2, 7, styleDim, "PRESS [ESC] TO RETURN")
		} else {
			s.drawRaw(2, 4, styleNormal, "CHARACTERS MUST BE OK STATUS.")
			s.drawRaw(2, 5, styleNormal, "QUEST ITEMS BLOCK TRANSFER.")
			s.drawRaw(2, 7, styleNormal, "SELECT SOURCE:")
			names := map[string]string{
				"1": "1) PROVING GROUNDS OF THE MAD OVERLORD",
				"2": "2) KNIGHT OF DIAMONDS",
				"3": "3) LEGACY OF LLYLGAMYN",
			}
			for i, key := range util.TransferSources {
				name := names[key]
				if name == "" {
					name = fmt.Sprintf("%d) %s", i+1, key)
				}
				s.drawRaw(6, 9+i, styleNormal, name)
			}
			s.drawRaw(2, 14, styleDim, "OR [ESC] TO RETURN")
		}

	case engine.UtilTransferResult:
		s.drawRaw(22, 1, styleTitle, "TRANSFER RESULTS")
		for i, msg := range util.Messages {
			if i >= 20 {
				break
			}
			st := styleNormal
			if len(msg) > 11 && msg[len(msg)-11:] == "TRANSFERRED" {
				st = styleGreen
			}
			s.drawRaw(2, 3+i, st, msg)
		}
		s.drawRaw(2, 23, styleDim, "PRESS ANY KEY TO CONTINUE")
	}

	s.Show()
}
