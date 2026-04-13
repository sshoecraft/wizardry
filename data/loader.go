package data

import (
	"encoding/json"
	"fmt"
)

// LoadGameData parses a wiz*_gamedata.json byte slice into a GameData struct.
func LoadGameData(raw []byte) (*GameData, error) {
	var gd GameData
	if err := json.Unmarshal(raw, &gd); err != nil {
		return nil, fmt.Errorf("parse gamedata: %w", err)
	}
	return &gd, nil
}

// LoadMazeData parses a wiz*_mazes.json byte slice into a MazeData struct.
func LoadMazeData(raw []byte) (*MazeData, error) {
	var md MazeData
	if err := json.Unmarshal(raw, &md); err != nil {
		return nil, fmt.Errorf("parse mazes: %w", err)
	}
	return &md, nil
}

// LoadMonsterPics parses a wiz*_monsters.json byte slice into a map of MonsterPic.
func LoadMonsterPics(raw []byte) (map[int]*MonsterPic, error) {
	var rawMap map[string]*MonsterPic
	if err := json.Unmarshal(raw, &rawMap); err != nil {
		return nil, fmt.Errorf("parse monster pics: %w", err)
	}
	result := make(map[int]*MonsterPic, len(rawMap))
	for k, v := range rawMap {
		var idx int
		fmt.Sscanf(k, "%d", &idx)
		result[idx] = v
	}
	return result, nil
}

// LoadScenario loads gamedata, maze data, and monster pics into a combined Scenario.
func LoadScenario(gameJSON, mazeJSON, monsterPicJSON, titleJSON []byte) (*Scenario, error) {
	gd, err := LoadGameData(gameJSON)
	if err != nil {
		return nil, err
	}
	md, err := LoadMazeData(mazeJSON)
	if err != nil {
		return nil, err
	}
	var pics map[int]*MonsterPic
	if monsterPicJSON != nil {
		pics, err = LoadMonsterPics(monsterPicJSON)
		if err != nil {
			return nil, err
		}
	}
	var title *TitleBitmap
	if titleJSON != nil {
		var tb TitleBitmap
		if err := json.Unmarshal(titleJSON, &tb); err == nil && tb.Width > 0 && tb.Height > 0 {
			title = &tb
		}
	}
	return &Scenario{
		GameData:    *gd,
		Mazes:       *md,
		MonsterPics: pics,
		Title:       title,
	}, nil
}
