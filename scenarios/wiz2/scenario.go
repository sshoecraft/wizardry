// Package wiz2 embeds and provides access to Wizardry 2 game data.
package wiz2

import (
	_ "embed"
	"encoding/json"

	"wizardry/data"
)

//go:embed wiz2_gamedata.json
var gameJSON []byte

//go:embed wiz2_mazes.json
var mazeJSON []byte

//go:embed wiz2_messages.json
var messagesJSON []byte

//go:embed wiz2_title.json
var titleJSON []byte

func Load() (*data.Scenario, error) {
	s, err := data.LoadScenario(gameJSON, mazeJSON, nil, titleJSON)
	if err != nil {
		return nil, err
	}
	s.ScenarioNum = 2
	if len(messagesJSON) > 0 {
		json.Unmarshal(messagesJSON, &s.Messages)
	}
	s.MessagesByLine = make(map[int]int, len(s.Messages))
	lineNum := 0
	for i, block := range s.Messages {
		s.MessagesByLine[lineNum] = i
		lineNum += len(block)
	}
	return s, nil
}
