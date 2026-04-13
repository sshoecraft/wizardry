package engine

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// SaveState is the JSON-serializable game state.
type SaveState struct {
	Roster     []*Character `json:"roster"`
	PartyNames []string     `json:"party"` // character names in party order
}

// RosterPath returns the full path to the roster file for a scenario key.
// e.g. key "1" -> ~/.config/wizardry/roster1.json
// Creates ~/.config/wizardry/ if it doesn't exist.
func RosterPath(key string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("home dir: %w", err)
	}
	dir := filepath.Join(home, ".config", "wizardry")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("mkdir %s: %w", dir, err)
	}
	return filepath.Join(dir, "roster"+key+".json"), nil
}

// legacyRosterPath returns the old-style path for migration.
// e.g. key "1" -> ~/.config/wizardry/wiz1/roster.json
func legacyRosterPath(key string) string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "wizardry", "wiz"+key, "roster.json")
}

// scenarioKey returns "1", "2", or "3" from the game name string.
func scenarioKey(gameName string) string {
	switch {
	case len(gameName) >= 8 && gameName[:8] == "PROVING ":
		return "1"
	case len(gameName) >= 6 && gameName[:6] == "THE KN":
		return "2"
	case len(gameName) >= 6 && gameName[:6] == "THE LE":
		return "3"
	default:
		return "1"
	}
}

// Save writes the current roster and party to disk.
func (g *GameState) Save() error {
	key := scenarioKey(g.Scenario.Game)
	path, err := RosterPath(key)
	if err != nil {
		return err
	}

	state := SaveState{
		Roster: g.Town.Roster.Characters,
	}
	for _, m := range g.Town.Party.Members {
		if m != nil {
			state.PartyNames = append(state.PartyNames, m.Name)
		}
	}

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// Load reads the roster and party from disk. No error if file doesn't exist.
// Checks new flat path first, falls back to legacy subdirectory path for migration.
func (g *GameState) Load() error {
	key := scenarioKey(g.Scenario.Game)
	path, err := RosterPath(key)
	if err != nil {
		return err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			// Try legacy path: ~/.config/wizardry/wiz<key>/roster.json
			legacy := legacyRosterPath(key)
			data, err = os.ReadFile(legacy)
			if err != nil {
				if os.IsNotExist(err) {
					return nil // fresh game, no save yet
				}
				return fmt.Errorf("read %s: %w", legacy, err)
			}
		} else {
			return fmt.Errorf("read %s: %w", path, err)
		}
	}

	var state SaveState
	if err := json.Unmarshal(data, &state); err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}

	g.Town.Roster.Characters = state.Roster

	// Backfill MaxMageSpells/MaxPriestSpells for saves from before these fields existed
	// Also migrate legacy Equipment[8]+Inventory[] to flat Items model
	for _, c := range g.Town.Roster.Characters {
		if c == nil {
			continue
		}
		maxMageZero := c.MaxMageSpells == [7]int{}
		maxPriestZero := c.MaxPriestSpells == [7]int{}
		if maxMageZero {
			c.MaxMageSpells = c.MageSpells
		}
		if maxPriestZero {
			c.MaxPriestSpells = c.PriestSpells
		}
		c.MigrateItems()
		// Force reset SpellKnown — spell table was reordered (MONTINO moved from
		// Mage to Priest, spells reindexed to match Pascal SPELLSKN[1..50]).
		// MigrateSpellKnown will regenerate from current spell slots.
		c.SpellKnown = [50]bool{}
		c.MigrateSpellKnown()
	}

	// Rebuild party from names
	g.Town.Party.Members = nil
	for _, name := range state.PartyNames {
		for _, c := range g.Town.Roster.Characters {
			if c != nil && c.Name == name {
				g.Town.Party.Members = append(g.Town.Party.Members, c)
				break
			}
		}
	}

	return nil
}
