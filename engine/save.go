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

// SaveDir returns ~/.config/wizardry/<scenario>/
func SaveDir(scenario string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("home dir: %w", err)
	}
	dir := filepath.Join(home, ".config", "wizardry", scenario)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("mkdir %s: %w", dir, err)
	}
	return dir, nil
}

// scenarioKey returns a short key like "wiz1" from the game name.
func scenarioKey(gameName string) string {
	switch {
	case len(gameName) >= 8 && gameName[:8] == "PROVING ":
		return "wiz1"
	case len(gameName) >= 6 && gameName[:6] == "THE KN":
		return "wiz2"
	case len(gameName) >= 6 && gameName[:6] == "THE LE":
		return "wiz3"
	default:
		return "wiz1"
	}
}

// Save writes the current roster and party to disk.
func (g *GameState) Save() error {
	key := scenarioKey(g.Scenario.Game)
	dir, err := SaveDir(key)
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

	path := filepath.Join(dir, "roster.json")
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// Load reads the roster and party from disk. No error if file doesn't exist.
func (g *GameState) Load() error {
	key := scenarioKey(g.Scenario.Game)
	dir, err := SaveDir(key)
	if err != nil {
		return err
	}

	path := filepath.Join(dir, "roster.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // fresh game, no save yet
		}
		return fmt.Errorf("read %s: %w", path, err)
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
