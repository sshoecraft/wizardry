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

func Load() (*data.Scenario, error) {
	s, err := data.LoadScenario(gameJSON, mazeJSON, nil, nil)
	if err != nil {
		return nil, err
	}
	if len(messagesJSON) > 0 {
		json.Unmarshal(messagesJSON, &s.Messages)
	}
	return s, nil
}
