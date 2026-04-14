package engine

import "wizardry/data"

// Race represents a character's race.
type Race int

const (
	Human Race = iota
	Elf
	Dwarf
	Gnome
	Hobbit
)

var RaceNames = [...]string{"HUMAN", "ELF", "DWARF", "GNOME", "HOBBIT"}

func (r Race) String() string { return RaceNames[r] }

// Class represents a character's class.
type Class int

const (
	Fighter Class = iota
	Mage
	Priest
	Thief
	Bishop
	Samurai
	Lord
	Ninja
)

var ClassNames = [...]string{"FIGHTER", "MAGE", "PRIEST", "THIEF", "BISHOP", "SAMURAI", "LORD", "NINJA"}
var ClassAbbrev = [...]string{"FIG", "MAG", "PRI", "THI", "BIS", "SAM", "LOR", "NIN"}

func (c Class) String() string { return ClassNames[c] }

// AlignClass returns the alignment-prefixed class abbreviation (e.g. "G-SAM").
func AlignClass(align Alignment, class Class) string {
	prefix := [...]string{"G", "N", "E"}
	return prefix[align] + "-" + ClassAbbrev[class]
}

// Alignment represents a character's moral alignment.
type Alignment int

const (
	Good Alignment = iota
	Neutral
	Evil
)

var AlignmentNames = [...]string{"GOOD", "NEUTRAL", "EVIL"}

func (a Alignment) String() string { return AlignmentNames[a] }

// Status represents a character's condition.
type Status int

const (
	OK Status = iota
	Asleep
	Afraid
	Paralyzed
	Stoned
	Dead
	Ashed
	Lost
)

var StatusNames = [...]string{"OK", "ASLEEP", "AFRAID", "PARALYZE", "STONED", "DEAD", "ASHED", "LOST"}

func (s Status) String() string { return StatusNames[s] }

// Possession represents a single item held by a character.
// Matches the original's 4-word item record: {equipped, cursed, identified, itemIndex}
type Possession struct {
	ItemIndex  int  `json:"item_index"`
	Equipped   bool `json:"equipped"`
	Cursed     bool `json:"cursed"`
	Identified bool `json:"identified"`
}

// Character represents a player character.
type Character struct {
	Name      string
	Password  string `json:"password,omitempty"` // STRING[15] from TCHAR offset 0x10
	Race      Race
	Class     Class
	Alignment Alignment
	Status    Status
	Level     int
	XP        int
	HP        int
	MaxHP     int
	Gold      int
	Age       int // in weeks

	// Base stats (3-18 range, can be modified by race/class bonuses)
	Strength  int
	IQ        int
	Piety     int
	Vitality  int
	Agility   int
	Luck      int

	// Derived
	AC          int
	MageSpells     [7]int // current spells per level (1-7), 0 = can't cast
	PriestSpells   [7]int
	MaxMageSpells  [7]int // max spells per level (restored at inn)
	MaxPriestSpells [7]int

	// Spell knowledge — Pascal SPELLSKN[50]. Indexed by SpellIndex[spellName].
	// true = character has learned this spell and can cast it.
	SpellKnown [50]bool `json:"spell_known,omitempty"`

	// Dungeon position — from TCHAR offset 0x20 (INMAZE) and 0xC6 (LOSTXYL)
	InMaze    bool `json:"in_maze,omitempty"`    // TCHAR.INMAZE: true if character is in the maze
	MazeLevel int  `json:"maze_level,omitempty"` // LOSTXYL: which dungeon level (0-based)
	MazeX     int  `json:"maze_x,omitempty"`     // LOSTXYL: X position
	MazeY     int  `json:"maze_y,omitempty"`     // LOSTXYL: Y position

	// Poison — from TCHAR LOSTXYL variant offset 0xC6
	PoisonAmt int `json:"poison_amt,omitempty"` // HP lost per step while poisoned

	// Wiz 3 legacy flag — LOSTXYL.AWARDS bit 13. Set by Rite of Passage.
	IsLegacy bool `json:"is_legacy,omitempty"`

	// Pascal MAXLEVAC: level at which HPMAX was last set. Used for drain HPMAX recalculation.
	MaxLevAC int `json:"max_lev_ac,omitempty"`

	// Items — flat array of up to 8 possessions, matching the original's data model.
	// Each has equipped/cursed/identified flags plus an item table index.
	// ItemCount tracks how many slots are in use (0-8).
	Items     [8]Possession `json:"items"`
	ItemCount int           `json:"item_count"`

	// Legacy fields for save migration — not used in new code
	Equipment [8]int `json:"Equipment,omitempty"`
	Inventory []int  `json:"Inventory,omitempty"`
}

// GetHealPts returns the total HealPts from all equipped items.
// Pascal HEALPRTY/UPDATEHP: CHARACTR[T1].HEALPTS is the aggregate of equipped items.
func (c *Character) GetHealPts(items []data.Item) int {
	total := 0
	for i := 0; i < c.ItemCount; i++ {
		if c.Items[i].Equipped && c.Items[i].ItemIndex >= 0 && c.Items[i].ItemIndex < len(items) {
			total += items[c.Items[i].ItemIndex].HealPts
		}
	}
	return total
}

// NewCharacter creates a character with default stats.
func NewCharacter(name string, race Race, class Class, align Alignment) *Character {
	c := &Character{
		Name:      name,
		Race:      race,
		Class:     class,
		Alignment: align,
		Status:    OK,
		Level:     1,
		HP:        0, // rolled during creation
		Gold:      0, // rolled during creation
		Age:       0, // set by FinalizeCharacter or DSK import
	}
	for i := range c.Equipment {
		c.Equipment[i] = -1
	}
	return c
}

// MigrateItems converts legacy Equipment[8]/Inventory[] to the flat Items model.
// Called during save load for backwards compatibility.
func (c *Character) MigrateItems() {
	hasLegacy := false
	for _, eq := range c.Equipment {
		if eq >= 0 {
			hasLegacy = true
			break
		}
	}
	if !hasLegacy && len(c.Inventory) == 0 {
		return
	}
	// Already migrated if ItemCount > 0
	if c.ItemCount > 0 {
		return
	}

	idx := 0
	for _, eq := range c.Equipment {
		if eq >= 0 && idx < 8 {
			c.Items[idx] = Possession{ItemIndex: eq, Equipped: true, Identified: true}
			idx++
		}
	}
	for _, inv := range c.Inventory {
		if idx < 8 {
			c.Items[idx] = Possession{ItemIndex: inv, Equipped: false, Identified: true}
			idx++
		}
	}
	c.ItemCount = idx

	// Clear legacy fields
	for i := range c.Equipment {
		c.Equipment[i] = -1
	}
	c.Inventory = nil
}

// EquipItem sets the equipped flag on item at position pos (0-based).
// Returns false if the item is already equipped.
func (c *Character) EquipItem(pos int) bool {
	if pos < 0 || pos >= c.ItemCount {
		return false
	}
	if c.Items[pos].Equipped {
		return false // already equipped
	}
	c.Items[pos].Equipped = true
	return true
}

// UnequipItem clears the equipped flag on item at position pos (0-based).
// Returns false if the item is cursed (can't unequip).
func (c *Character) UnequipItem(pos int) bool {
	if pos < 0 || pos >= c.ItemCount {
		return false
	}
	if !c.Items[pos].Equipped {
		return false // not equipped
	}
	if c.Items[pos].Cursed {
		return false // cursed items can't be unequipped
	}
	c.Items[pos].Equipped = false
	return true
}

// DropItem removes the item at position pos (0-based), shifting remaining items down.
// Returns false if the item is cursed+equipped.
func (c *Character) DropItem(pos int) bool {
	if pos < 0 || pos >= c.ItemCount {
		return false
	}
	if c.Items[pos].Equipped && c.Items[pos].Cursed {
		return false
	}
	// Shift items down
	for i := pos; i < c.ItemCount-1; i++ {
		c.Items[i] = c.Items[i+1]
	}
	c.Items[c.ItemCount-1] = Possession{}
	c.ItemCount--
	return true
}

// TradeItem moves the item at position pos (0-based) from this character to target.
// Returns false if the item can't be traded or target is full.
func (c *Character) TradeItem(pos int, target *Character) bool {
	if pos < 0 || pos >= c.ItemCount {
		return false
	}
	if target.ItemCount >= 8 {
		return false
	}
	if c.Items[pos].Equipped && c.Items[pos].Cursed {
		return false
	}
	// Copy item to target
	item := c.Items[pos]
	item.Equipped = false // unequip when trading
	target.Items[target.ItemCount] = item
	target.ItemCount++

	// Remove from source (shift down)
	for i := pos; i < c.ItemCount-1; i++ {
		c.Items[i] = c.Items[i+1]
	}
	c.Items[c.ItemCount-1] = Possession{}
	c.ItemCount--
	return true
}

// AddItem adds an item to the character's possession list.
// Returns false if already holding 8 items.
func (c *Character) AddItem(itemIndex int, identified bool) bool {
	if c.ItemCount >= 8 {
		return false
	}
	c.Items[c.ItemCount] = Possession{
		ItemIndex:  itemIndex,
		Equipped:   false,
		Identified: identified,
	}
	c.ItemCount++
	return true
}

// IsAlive returns true if the character can act.
func (c *Character) IsAlive() bool {
	return c.Status == OK || c.Status == Asleep || c.Status == Afraid
}

// IsDead returns true if the character is dead, ashed, or lost.
func (c *Character) IsDead() bool {
	return c.Status >= Dead
}

// Party represents the active adventuring party (up to 6 members).
type Party struct {
	Members []*Character // 0-5, front to back
}

// Size returns the number of characters in the party.
func (p *Party) Size() int {
	count := 0
	for _, m := range p.Members {
		if m != nil {
			count++
		}
	}
	return count
}

// ActiveCount returns the number of alive, conscious members.
func (p *Party) ActiveCount() int {
	count := 0
	for _, m := range p.Members {
		if m != nil && m.IsAlive() {
			count++
		}
	}
	return count
}

// Roster holds all created characters (up to 20 in original).
type Roster struct {
	Characters []*Character
}

// Add adds a character to the roster. Returns false if roster is full.
func (r *Roster) Add(c *Character) bool {
	if len(r.Characters) >= 20 {
		return false
	}
	r.Characters = append(r.Characters, c)
	return true
}

// Remove removes a character by name from the roster.
func (r *Roster) Remove(name string) {
	for i, c := range r.Characters {
		if c != nil && c.Name == name {
			r.Characters = append(r.Characters[:i], r.Characters[i+1:]...)
			return
		}
	}
}
