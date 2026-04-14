// Package wiz1 embeds and provides access to Wizardry 1 game data.
package wiz1

import (
	_ "embed"
	"encoding/json"

	"wizardry/data"
)

//go:embed wiz1_gamedata.json
var gameJSON []byte

//go:embed wiz1_mazes.json
var mazeJSON []byte

//go:embed wiz1_monsters.json
var monsterPicJSON []byte

//go:embed wiz1_title.json
var titleJSON []byte

//go:embed wiz1_title_wt.bin
var titleWTData []byte

//go:embed wiz1_messages.json
var messagesJSON []byte

func Load() (*data.Scenario, error) {
	s, err := data.LoadScenario(gameJSON, mazeJSON, monsterPicJSON, titleJSON)
	if err != nil {
		return nil, err
	}
	s.ScenarioNum = 1
	s.TitleWT = titleWTData
	if len(messagesJSON) > 0 {
		json.Unmarshal(messagesJSON, &s.Messages)
	}
	return s, nil
}
