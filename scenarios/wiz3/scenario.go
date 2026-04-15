// Package wiz3 embeds and provides access to Wizardry 3 game data.
package wiz3

import (
	_ "embed"
	"encoding/json"

	"wizardry/data"
)

//go:embed wiz3_gamedata.json
var gameJSON []byte

//go:embed wiz3_mazes.json
var mazeJSON []byte

//go:embed wiz3_messages.json
var messagesJSON []byte

//go:embed wiz3_title.json
var titleJSON []byte

// Wiz 3 title story — 10 pages of narrative text from LOLTITLE p-code LSA strings.
// Sixel terminals show the decoded PICTURE.BITS bitmap frames instead.
// Exact 4-line pages from LOLTITLE p-code LSA strings (procs 6-16).
// Each page corresponds to one PICTURE.BITS image frame.
// Text is displayed in Apple II mixed mode: 4 lines at the bottom of the screen.
// Pages 8-9 share image 8, page 10 uses image 9.
var titleStoryPages = [][]string{
	{ // Page 0 — proc 6 (image 0: sword)
		"Andrew Greenberg & Robert Woodhead",
		"proudly present",
		"the third Wizardry Scenario",
		`"Legacy of Llylgamyn"`,
	},
	{ // Page 1 — proc 7 (image 1: adventurers)
		"a generation has  passed  in  the  small",
		"Kingdom of Llylgamyn  since an  intrepid",
		"band of adventurers regained the ancient",
		"and powerful Staff of Gnilda.",
	},
	{ // Page 2 — proc 8 (image 2)
		"Under the protection of the staff and",
		"the  wise  guidance   of  those  same",
		"adventurers,  Llylgamyn  has become a",
		"place of beauty.",
	},
	{ // Page 3 — proc 9 (image 3: face)
		"For years,  however,  strange tales of",
		"freak  disasters  have  been whispered",
		"from ear  to ear.  Only  the  foolish,",
		"and the very wise, paid any attention.",
	},
	{ // Page 4 — proc 10 (image 4)
		"But when the formerly peaceful seas",
		"surrounding  the  island colony  of",
		"Arbithea rose up and swallowed it..",
		"",
	},
	{ // Page 5 — proc 11 (image 5: person in dungeon)
		"..and a massive earthquake damaged the",
		"temple of Gnilda,  all Llylgamyn  knew",
		"that something was very,  very  wrong.",
		"",
	},
	{ // Page 6 — proc 12 (image 6)
		"The Sages of Llylgamyn  all  agree  that",
		"there is but one hope, a mystic orb with",
		"which  they can at least  determine what",
		"is causing these disasters.",
	},
	{ // Page 7 — proc 13 (image 7: dragon in cave)
		"The orb is guarded by the great dragon",
		`L'kbreth.  The  Sages call  upon  you,`,
		"the descendants of  the heroes  of the",
		"Knight of Diamonds, to seek L'kbreth..",
	},
	{ // Page 8 — proc 14 (image 8)
		"..and win from her the orb with which",
		"they can hope to save the world!",
		"",
		"",
	},
	{ // Page 9 — proc 15 (image 8 continued)
		"Now you must go forth into the",
		"unknown to save  your  people.",
		"",
		"This is the Legacy of Llylgamyn!",
	},
	{ // Page 10 — proc 16 (image 9: dragon)
		"Scenario designed by W.A.R.G members",
		"Robert Del Favero, Jr.,  Joshua D.",
		"Mittleman and Samuel Pottle.",
		"(Press <space> at any time to start)",
	},
}

func Load() (*data.Scenario, error) {
	s, err := data.LoadScenario(gameJSON, mazeJSON, nil, nil)
	if err != nil {
		return nil, err
	}
	s.ScenarioNum = 3
	if len(messagesJSON) > 0 {
		json.Unmarshal(messagesJSON, &s.Messages)
	}
	s.MessagesByLine = make(map[int]int, len(s.Messages))
	lineNum := 0
	for i, block := range s.Messages {
		s.MessagesByLine[lineNum] = i
		lineNum += len(block)
	}
	s.TitleStory = titleStoryPages

	// Load decoded PICTURE.BITS title frames (for sixel rendering)
	if len(titleJSON) > 0 {
		var frames []*data.TitleBitmap
		if err := json.Unmarshal(titleJSON, &frames); err == nil {
			s.TitleFrames = frames
		}
	}

	return s, nil
}
