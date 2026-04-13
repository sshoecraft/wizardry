package engine

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// UtilStep tracks the current screen within the utilities menu.
type UtilStep int

const (
	UtilMenu     UtilStep = iota // main menu: B/C/I/L
	UtilBackup                    // backup sub-menu: T)O or F)ROM
	UtilBackupTo                  // "backup to" path input
	UtilBackupFrom                // "restore from" path input
	UtilRename                    // rename: select character
	UtilRenameNew                 // rename: enter new name
	UtilImport                    // import: enter .DSK path
	UtilImportResult              // import: showing results
	UtilTransfer                  // transfer: select source scenario
	UtilTransferResult            // transfer: showing results
)

// UtilState holds all state for the utilities phase.
type UtilState struct {
	Step             UtilStep
	InputBuf         string   // text input buffer
	Message          string   // status/result message
	Messages         []string // multi-line results (for import/transfer)
	SelectedChar     int      // index of character being renamed
	TransferSources  []string // available scenario keys for transfer
}

// NewUtilState creates a fresh utilities state at the main menu.
func NewUtilState() *UtilState {
	return &UtilState{Step: UtilMenu}
}

// expandPath expands ~ to home directory and cleans the path.
func expandPath(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			path = filepath.Join(home, path[2:])
		}
	}
	return filepath.Clean(path)
}

// BackupRoster copies the current roster.json to the given path.
func BackupRoster(game *GameState, destPath string) error {
	destPath = expandPath(destPath)
	key := scenarioKey(game.Scenario.Game)
	dir, err := SaveDir(key)
	if err != nil {
		return err
	}
	srcPath := dir + "/roster.json"
	data, err := os.ReadFile(srcPath)
	if err != nil {
		return fmt.Errorf("read roster: %w", err)
	}
	if err := os.WriteFile(destPath, data, 0644); err != nil {
		return fmt.Errorf("write backup: %w", err)
	}
	return nil
}

// RestoreRoster copies a backup file over the current roster.json and reloads.
func RestoreRoster(game *GameState, srcPath string) error {
	srcPath = expandPath(srcPath)
	data, err := os.ReadFile(srcPath)
	if err != nil {
		return fmt.Errorf("read backup: %w", err)
	}
	key := scenarioKey(game.Scenario.Game)
	dir, err := SaveDir(key)
	if err != nil {
		return err
	}
	destPath := dir + "/roster.json"
	if err := os.WriteFile(destPath, data, 0644); err != nil {
		return fmt.Errorf("write roster: %w", err)
	}
	// Reload
	return game.Load()
}

// TransferCharacters imports characters from another scenario's roster.
// Characters are stripped of items and gold (as in the original disk transfer).
// Dead/Lost characters are skipped.
func TransferCharacters(game *GameState, sourceScenario string) ([]string, error) {
	dir, err := SaveDir(sourceScenario)
	if err != nil {
		return nil, fmt.Errorf("source save dir: %w", err)
	}
	rosterPath := filepath.Join(dir, "roster.json")
	data, err := os.ReadFile(rosterPath)
	if err != nil {
		return nil, fmt.Errorf("no roster found for %s", sourceScenario)
	}

	// Save format: {"roster": [characters], "party": [...]}
	var saveFile struct {
		Roster []*Character `json:"roster"`
	}
	if err := json.Unmarshal(data, &saveFile); err != nil {
		return nil, fmt.Errorf("corrupt roster: %w", err)
	}
	characters := saveFile.Roster

	scenarioNames := map[string]string{
		"wiz1": "PROVING GROUNDS",
		"wiz2": "KNIGHT OF DIAMONDS",
		"wiz3": "LEGACY OF LLYLGAMYN",
	}
	srcName := scenarioNames[sourceScenario]
	if srcName == "" {
		srcName = sourceScenario
	}

	var messages []string
	messages = append(messages, fmt.Sprintf("FROM: %s", srcName))
	messages = append(messages, fmt.Sprintf("FOUND %d CHARACTERS", len(characters)))
	messages = append(messages, "")

	imported := 0
	for _, c := range characters {
		if c == nil {
			continue
		}
		// Must be STATUS=OK to transfer (Pascal REMOVCHR line 181/203)
		if c.Status != OK {
			messages = append(messages, fmt.Sprintf("%s IS %s, SKIPPED", c.Name, c.Status))
			continue
		}
		// Quest item check: EQINDEX > 93 blocks transfer (Pascal line 213-215)
		hasQuestItem := false
		for i := 0; i < c.ItemCount; i++ {
			if c.Items[i].ItemIndex > 93 {
				hasQuestItem = true
				break
			}
		}
		if hasQuestItem {
			messages = append(messages, fmt.Sprintf("%s HAS NON-TRANSFERABLE ITEMS", c.Name))
			continue
		}
		// Check name collision
		exists := false
		for _, existing := range game.Town.Roster.Characters {
			if existing != nil && existing.Name == c.Name {
				exists = true
				break
			}
		}
		if exists {
			messages = append(messages, fmt.Sprintf("%s ALREADY EXISTS, SKIPPED", c.Name))
			continue
		}
		if len(game.Town.Roster.Characters) >= 20 {
			messages = append(messages, "ROSTER FULL")
			break
		}

		// Raw character copy — everything transfers as-is (Pascal TRANGOOD line 253)
		// Items, gold, level, stats, spells all kept intact.
		c.MigrateItems()
		c.MigrateSpellKnown()
		game.Town.Roster.Characters = append(game.Town.Roster.Characters, c)
		messages = append(messages, fmt.Sprintf("%s L%d %s TRANSFERRED", c.Name, c.Level, c.Class))
		imported++
	}

	messages = append(messages, "")
	messages = append(messages, fmt.Sprintf("%d CHARACTERS TRANSFERRED", imported))
	return messages, nil
}

// AvailableTransferScenarios returns scenario keys that have save data,
// excluding the current scenario.
func AvailableTransferScenarios(currentGame *GameState) []string {
	current := scenarioKey(currentGame.Scenario.Game)
	all := []string{"wiz1", "wiz2", "wiz3"}
	var available []string
	for _, key := range all {
		if key == current {
			continue
		}
		dir, err := SaveDir(key)
		if err != nil {
			continue
		}
		rosterPath := filepath.Join(dir, "roster.json")
		if _, err := os.Stat(rosterPath); err == nil {
			available = append(available, key)
		}
	}
	return available
}

// ImportCharactersFromDSK reads characters from an Apple II .DSK file
// and adds them to the current roster. Skips characters whose names
// already exist in the roster.
func ImportCharactersFromDSK(game *GameState, dskPath string) ([]string, error) {
	gameName, chars, err := ImportFromDSK(dskPath)
	if err != nil {
		return nil, err
	}

	var messages []string
	messages = append(messages, fmt.Sprintf("SCENARIO: %s", gameName))
	messages = append(messages, fmt.Sprintf("FOUND %d CHARACTERS", len(chars)))
	messages = append(messages, "")

	imported := 0
	for _, c := range chars {
		// Check for name collision
		exists := false
		for _, existing := range game.Town.Roster.Characters {
			if existing != nil && existing.Name == c.Name {
				exists = true
				break
			}
		}
		if exists {
			messages = append(messages, fmt.Sprintf("%s ALREADY EXISTS, NOT IMPORTING", c.Name))
			continue
		}
		if len(game.Town.Roster.Characters) >= 20 {
			messages = append(messages, "ROSTER FULL, CANNOT IMPORT MORE")
			break
		}
		game.Town.Roster.Characters = append(game.Town.Roster.Characters, c)
		messages = append(messages, fmt.Sprintf("%s L%d %s IMPORTED", c.Name, c.Level, c.Class))
		imported++
	}

	messages = append(messages, "")
	messages = append(messages, fmt.Sprintf("%d CHARACTERS IMPORTED", imported))
	return messages, nil
}
