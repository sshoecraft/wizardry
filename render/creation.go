package render

import (
	"fmt"

	"wizardry/engine"
)

// RenderCreation draws the character creation screen.
//
// From ROLLER p-code proc 18 (IC 959-1246): ONE persistent form with labels
// that fill in progressively. The screen is NOT cleared between steps — only
// the prompt area (rows 15-23) is redrawn. Values appear at column 10 as
// they are selected.
//
// Form layout (40×24, traced from GOTOXY calls):
//
//	Row 0:       NAME [name]                 "NAME ":10 + name at col 10
//	Row 1:  PASSWORD                         label only (password entered later)
//	Row 2:  RACE      [race name]            value at GOTOXY(10,2)
//	Row 3:  POINTS    [bonus pts]            value at GOTOXY(10,3)
//	Row 4:  (blank)
//	Row 5:  STRENGTH  [val] <--  A) FIGHTER  stat at GOTOXY(10,5+i), ptr at GOTOXY(13,5+i), class at GOTOXY(20,5+i)
//	Row 6:  I.Q.      [val]      B) MAGE
//	Row 7:      PIETY [val]      C) PRIEST   "PIETY":9 right-justified
//	Row 8:   VITALITY [val]      D) THIEF    "VITALITY":9 right-justified
//	Row 9:    AGILITY [val]      E) BISHOP   "AGILITY":9 right-justified
//	Row 10: LUCK      [val]      F) SAMURAI
//	Row 11: (blank)               G) LORD
//	Row 12: ALIGNMENT [align]     H) NINJA   value at GOTOXY(10,12)
//	Row 13:     CLASS [class]                "CLASS":9, value at GOTOXY(10,13)
//	Row 14: (blank)
//	Row 15+: prompts (cleared between steps)
//
// Prompt area by step:
//   - StepRace:      rows 17-21 race list, row 23 "CHOOSE A RACE >"
//   - StepAlignment: rows 17-19 alignment list, row 21 "CHOOSE AN ALIGNMENT >"
//   - StepStats:     rows 15-17 instructions
//   - StepClass:     row 15 "CHOOSE A CLASS >"
//   - StepConfirm:   row 15 "KEEP THIS CHARACTER (Y/N)? >"
func (s *Screen) RenderCreation(cs *engine.CreationState) {
	s.Clear()
	s.ClearSixelTransition()

	// === FORM LABELS (rows 0-14) — always drawn ===
	// All labels right-justified in 9-char field (ending at col 8),
	// col 9 is a gap, values start at col 10.

	// Row 0: NAME + character name
	s.DrawString(0, 0, styleNormal, fmt.Sprintf("%9s", "NAME"))
	s.DrawString(10, 0, styleNormal, cs.Name)
	if cs.Step == engine.StepName {
		s.DrawString(10+len(cs.Name), 0, styleNormal, "_")
	}

	// Row 1: PASSWORD
	s.DrawString(0, 1, styleNormal, fmt.Sprintf("%9s", "PASSWORD"))

	// Row 2: RACE + value at col 10
	s.DrawString(0, 2, styleNormal, fmt.Sprintf("%9s", "RACE"))
	if cs.Step >= engine.StepAlignment {
		s.DrawString(10, 2, styleNormal, cs.Race.String())
	}

	// Row 3: POINTS + bonus points at col 10
	s.DrawString(0, 3, styleNormal, fmt.Sprintf("%9s", "POINTS"))
	if cs.Step >= engine.StepStats {
		s.DrawString(10, 3, styleNormal, fmt.Sprintf("%2d", cs.BonusPoints))
	}

	// Row 4: blank

	// Rows 5-10: Stat labels (all right-justified in 9) + values + pointer
	statNames := [6]string{"STRENGTH", "I.Q.", "PIETY", "VITALITY", "AGILITY", "LUCK"}
	for i := 0; i < 6; i++ {
		row := 5 + i
		s.DrawString(0, row, styleNormal, fmt.Sprintf("%9s", statNames[i]))
		// Stat value at GOTOXY(10, 5+i) — WRITEINT width 2
		if cs.Step >= engine.StepStats {
			s.DrawString(10, row, styleNormal, fmt.Sprintf("%2d", cs.Stats[i]))
		}
		// "<--" pointer at GOTOXY(13, 5+cursor) — only during StepStats
		if cs.Step == engine.StepStats && i == cs.StatCursor {
			s.DrawString(13, row, styleNormal, "<--")
		}
	}

	// Row 11: blank

	// Row 12: ALIGNMENT + alignment name at GOTOXY(10,12)
	s.DrawString(0, 12, styleNormal, fmt.Sprintf("%9s", "ALIGNMENT"))
	if cs.Step >= engine.StepStats {
		s.DrawString(10, 12, styleNormal, cs.Alignment.String())
	}

	// Row 13: CLASS + class name at GOTOXY(10,13)
	s.DrawString(0, 13, styleNormal, fmt.Sprintf("%9s", "CLASS"))
	if cs.Step >= engine.StepPassword {
		s.DrawString(10, 13, styleNormal, cs.Class.String())
	}

	// Row 14: blank

	// === AVAILABLE CLASSES at GOTOXY(20, 5+i) for i=0..7 ===
	// From p-code proc 14 (IC 2093-2179): shown during stat allocation
	// and class selection. Fixed letters A-H for all 8 classes.
	// Available: "A) FIGHTER", unavailable: blank.
	if cs.Step == engine.StepStats || cs.Step == engine.StepClass {
		avail := cs.ClassAvailability()
		for i := 0; i < 8; i++ {
			if avail[i] {
				s.DrawString(20, 5+i, styleNormal,
					fmt.Sprintf("%c) %s", 'A'+i, engine.ClassNames[i]))
			}
		}
	}

	// === PROMPT AREA (rows 15-23) ===
	switch cs.Step {
	case engine.StepName:
		// Name entry (not in original form — Training Grounds handles this)
		// But needed for ESC-back-from-race flow
		s.DrawString(0, 17, styleNormal, "ENTER THY NAME:")

	case engine.StepRace:
		// From p-code proc 17: GOTOXY(0,17), loop 1..5 race list
		for i, name := range engine.RaceNames {
			s.DrawString(0, 17+i, styleNormal,
				fmt.Sprintf("%c) %s", 'A'+i, name))
		}
		// "CHOOSE A RACE >" after the list
		s.DrawString(0, 23, styleNormal, "CHOOSE A RACE >")

	case engine.StepAlignment:
		// From p-code proc 15: GOTOXY(0,17), loop 1..3 alignment list
		for i, name := range engine.AlignmentNames {
			s.DrawString(0, 17+i, styleNormal,
				fmt.Sprintf("%c) %s", 'A'+i, name))
		}
		// "CHOOSE AN ALIGNMENT >" after the list
		s.DrawString(0, 21, styleNormal, "CHOOSE AN ALIGNMENT >")

	case engine.StepStats:
		// From p-code proc 16 (IC 1618-1775): instructions at GOTOXY(0,15)
		s.DrawString(0, 15, styleNormal, "ENTER [+,-] TO ALTER A SCORE,")
		s.DrawString(0, 16, styleNormal, "      [RET] TO GO TO NEXT SCORE,")
		s.DrawString(0, 17, styleNormal, "      [ESC] TO GO ON WHEN POINTS USED UP")

	case engine.StepClass:
		// From p-code proc 14 (IC 2241-2266): just the prompt
		// Classes are already visible at col 20 (drawn above)
		s.DrawString(0, 15, styleNormal, "CHOOSE A CLASS >")

	case engine.StepPassword:
		// From p-code ROLLER proc 18 (IC 1249-1431):
		// Password entry at GOTOXY(10, 1) on the PASSWORD row of the form.
		// Prompt text in bottom area.
		if cs.PasswordStep == 0 {
			s.DrawString(0, 15, styleNormal, "ENTER A PASSWORD ([RET] FOR NONE)")
			s.DrawString(10, 1, styleNormal, cs.PasswordFirst+"_")
		} else {
			s.DrawString(0, 15, styleNormal, "ENTER IT AGAIN TO BE SURE")
			s.DrawString(10, 1, styleNormal, cs.Password+"_")
		}

	case engine.StepConfirm:
		// From p-code proc 13 (IC 2573): confirmation prompt
		s.DrawString(0, 15, styleNormal, "KEEP THIS CHARACTER (Y/N)? >")
	}

	s.Show()
}
