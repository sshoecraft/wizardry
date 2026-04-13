package engine

import "math/rand"

// CreationStep tracks where we are in character creation.
type CreationStep int

const (
	StepName CreationStep = iota
	StepPassword     // "ENTER A PASSWORD ([RET] FOR NONE)" + confirm
	StepRace
	StepAlignment
	StepStats
	StepClass
	StepConfirm
)

// CreationState holds all state for the character creation flow.
type CreationState struct {
	Step          CreationStep
	Name          string
	Race          Race
	Alignment     Alignment
	BonusPoints   int
	Stats         [6]int // STR, IQ, PIE, VIT, AGI, LCK
	StatCursor    int    // which stat is selected for point allocation
	Class         Class
	ClassCursor   int    // cursor in available classes list
	AvailClasses  []Class
	SelectedIndex int // general cursor for race/alignment selection
	Password      string // password being entered
	PasswordStep  int    // 0=first entry, 1=confirm entry
	PasswordFirst string // first entry stored for comparison
}

var StatNames = [6]string{"STRENGTH", "I.Q.", "PIETY", "VITALITY", "AGILITY", "LUCK"}

// Base stats by race: STR, IQ, PIE, VIT, AGI, LCK
// Verified from p-code: encoded as ASCII strings "885889" etc, subtract 0x30
var RaceBaseStats = [5][6]int{
	{8, 8, 5, 8, 8, 9},   // Human  "885889"
	{7, 10, 10, 6, 9, 6}, // Elf    "7::696"
	{10, 7, 10, 10, 5, 6},// Dwarf  ":7::56"
	{7, 7, 10, 8, 10, 7}, // Gnome  "77:8:7"
	{5, 7, 7, 6, 10, 15}, // Hobbit "5776:?"
}

// NewCreationState starts a fresh character creation.
func NewCreationState() *CreationState {
	return &CreationState{
		Step: StepName,
	}
}

// RollBonusPoints generates bonus points to distribute.
// Verified from p-code: 7 + RANDOM MOD 4 (giving 7-10), then recursive
// 1-in-11 chance to add +10 each time (while <= 20).
func RollBonusPoints() int {
	bonus := 7 + rand.Intn(4) // 7, 8, 9, or 10
	for bonus <= 20 && rand.Intn(11) == 10 {
		bonus += 10
	}
	return bonus
}

// InitStats sets base stats from race and rolls bonus points.
func (cs *CreationState) InitStats() {
	base := RaceBaseStats[cs.Race]
	for i := 0; i < 6; i++ {
		cs.Stats[i] = base[i]
	}
	cs.BonusPoints = RollBonusPoints()
	cs.StatCursor = 0
}

// AddStatPoint adds one bonus point to the currently selected stat (max 18).
func (cs *CreationState) AddStatPoint() {
	if cs.BonusPoints <= 0 {
		return
	}
	if cs.Stats[cs.StatCursor] >= 18 {
		return
	}
	cs.Stats[cs.StatCursor]++
	cs.BonusPoints--
}

// RemoveStatPoint removes one bonus point from the currently selected stat (min = race base).
func (cs *CreationState) RemoveStatPoint() {
	base := RaceBaseStats[cs.Race]
	if cs.Stats[cs.StatCursor] <= base[cs.StatCursor] {
		return
	}
	cs.Stats[cs.StatCursor]--
	cs.BonusPoints++
}

// ClassRequirements defines minimum stats for each class.
// Order: STR, IQ, PIE, VIT, AGI, LCK
// Verified from p-code qualification proc.
var ClassRequirements = [8][6]int{
	{11, 0, 0, 0, 0, 0},      // Fighter: STR 11
	{0, 11, 0, 0, 0, 0},      // Mage: IQ 11
	{0, 0, 11, 0, 0, 0},      // Priest: PIE 11, not Neutral
	{0, 0, 0, 0, 11, 0},      // Thief: AGI 11, not Good
	{0, 12, 12, 0, 0, 0},     // Bishop: IQ 12 PIE 12, not Neutral
	{15, 11, 10, 14, 10, 0},  // Samurai: not Evil
	{15, 12, 12, 15, 14, 15}, // Lord: Good only
	{15, 15, 15, 15, 15, 15}, // Ninja: ALL 15, Evil only (WC030: reduced from 17)
}

// AlignRestriction defines alignment requirements for classes.
type AlignRestriction int

const (
	AlignAny        AlignRestriction = 0
	AlignGoodOnly   AlignRestriction = 1
	AlignEvilOnly   AlignRestriction = 2
	AlignNotEvil    AlignRestriction = 3 // Good or Neutral
	AlignNotNeutral AlignRestriction = 4 // Good or Evil
	AlignNotGood    AlignRestriction = 5 // Neutral or Evil
)

// Verified from p-code: Priest/Bishop not Neutral, Thief not Good
var ClassAlignRestrictions = [8]AlignRestriction{
	AlignAny,        // Fighter
	AlignAny,        // Mage
	AlignNotNeutral, // Priest (Good or Evil, NOT Neutral)
	AlignNotGood,    // Thief (Neutral or Evil, NOT Good)
	AlignNotNeutral, // Bishop (Good or Evil, NOT Neutral)
	AlignNotEvil,    // Samurai (Good or Neutral)
	AlignGoodOnly,   // Lord (Good only)
	AlignEvilOnly,   // Ninja (Evil only)
}

// CalculateAvailableClasses returns which classes the current stats/alignment qualify for.
func (cs *CreationState) CalculateAvailableClasses() []Class {
	var available []Class
	avail := cs.ClassAvailability()
	for c := Fighter; c <= Ninja; c++ {
		if avail[c] {
			available = append(available, c)
		}
	}
	return available
}

// ClassAvailability returns a boolean for each of the 8 classes indicating
// whether the current stats and alignment qualify.
// From p-code ROLLER proc 28 (IC 96-526): checks stat minimums and alignment restrictions.
func (cs *CreationState) ClassAvailability() [8]bool {
	var avail [8]bool
	for c := Fighter; c <= Ninja; c++ {
		reqs := ClassRequirements[c]
		qualified := true
		for i := 0; i < 6; i++ {
			if cs.Stats[i] < reqs[i] {
				qualified = false
				break
			}
		}
		if !qualified {
			continue
		}
		restriction := ClassAlignRestrictions[c]
		switch restriction {
		case AlignGoodOnly:
			if cs.Alignment != Good {
				continue
			}
		case AlignEvilOnly:
			if cs.Alignment != Evil {
				continue
			}
		case AlignNotEvil:
			if cs.Alignment == Evil {
				continue
			}
		case AlignNotNeutral:
			if cs.Alignment == Neutral {
				continue
			}
		case AlignNotGood:
			if cs.Alignment == Good {
				continue
			}
		}
		avail[c] = true
	}
	return avail
}

// CharClassQualifies checks which classes a character qualifies for based on
// current stats and alignment. Used for class change at Training Grounds.
// From Pascal ROLLER.TEXT GTCHGLST (lines 18-67) — same stat/alignment checks
// as character creation but operates on an existing Character's attributes.
func CharClassQualifies(c *Character) [8]bool {
	stats := [6]int{c.Strength, c.IQ, c.Piety, c.Vitality, c.Agility, c.Luck}
	var avail [8]bool
	for cl := Fighter; cl <= Ninja; cl++ {
		reqs := ClassRequirements[cl]
		qualified := true
		for i := 0; i < 6; i++ {
			if stats[i] < reqs[i] {
				qualified = false
				break
			}
		}
		if !qualified {
			continue
		}
		restriction := ClassAlignRestrictions[cl]
		switch restriction {
		case AlignGoodOnly:
			if c.Alignment != Good {
				continue
			}
		case AlignEvilOnly:
			if c.Alignment != Evil {
				continue
			}
		case AlignNotEvil:
			if c.Alignment == Evil {
				continue
			}
		case AlignNotNeutral:
			if c.Alignment == Neutral {
				continue
			}
		case AlignNotGood:
			if c.Alignment == Good {
				continue
			}
		}
		avail[cl] = true
	}
	return avail
}

// HP die size by class. Verified from p-code XJP table.
var ClassHPDie = [8]int{
	10, // Fighter
	4,  // Mage
	8,  // Priest
	6,  // Thief
	6,  // Bishop (NOT 4 — verified from p-code)
	16, // Samurai (NOT 10 — verified from p-code)
	10, // Lord
	6,  // Ninja
}

// VIT modifier table for HP. Verified from p-code.
func vitMod(vit int) int {
	switch {
	case vit <= 3:
		return -2
	case vit <= 5:
		return -1
	case vit >= 18:
		return 3
	case vit >= 17:
		return 2
	case vit >= 16:
		return 1
	default:
		return 0
	}
}

// RollHP rolls starting HP for the given class and vitality.
// From p-code proc 13 (IC 2838-2877): die roll + VIT modifier, then
// TWO decay passes (counter 1..2), each 50% chance of ×9/10. Min 2.
func RollHP(class Class, vitality int) int {
	die := ClassHPDie[class]
	hp := rand.Intn(die) + 1 + vitMod(vitality)
	// Two decay passes — p-code loops counter 1..2
	for pass := 0; pass < 2; pass++ {
		if rand.Intn(2) == 1 {
			hp = hp * 9 / 10
		}
	}
	if hp < 2 {
		hp = 2
	}
	return hp
}

// RollGold rolls starting gold. Verified from p-code: 90 + RANDOM MOD 100 for all classes.
func RollGold() int {
	return 90 + rand.Intn(100)
}

// FinalizeCharacter creates the Character from creation state.
// From p-code ROLLER proc 13 (IC 2629-2895):
//   Mage/Bishop: SPELLSKN bits 1,3 set, MAGESP[0]=2
//   Priest: SPELLSKN bits 23,24 set, PRIESTSP[0]=2
//   HP: class die + VIT mod, 2 decay passes, min 2
//   Gold: 90 + random()%100
//   Age: 18*52 + random()%300
func (cs *CreationState) FinalizeCharacter() *Character {
	c := NewCharacter(cs.Name, cs.Race, cs.Class, cs.Alignment)
	c.Password = cs.Password
	c.Strength = cs.Stats[0]
	c.IQ = cs.Stats[1]
	c.Piety = cs.Stats[2]
	c.Vitality = cs.Stats[3]
	c.Agility = cs.Stats[4]
	c.Luck = cs.Stats[5]

	hp := RollHP(cs.Class, cs.Stats[3])
	c.HP = hp
	c.MaxHP = hp
	c.MaxLevAC = 1 // starting level
	c.Gold = RollGold()
	c.AC = 10

	// Starting age — p-code proc 22 (IC 810): 18*52 + random()%300
	c.Age = 18*52 + rand.Intn(300)

	// Starting spells — p-code proc 13 (IC 2629-2704)
	// SPELLSKN bits set at creation determine which spells the character knows
	switch cs.Class {
	case Mage, Bishop:
		// 2 level-1 mage spell slots, knows HALITO + KATINO (SPELLSKN bits 1,3)
		c.MageSpells[0] = 2
		c.MaxMageSpells[0] = 2
		c.SpellKnown[SpellIndex["HALITO"]] = true
		c.SpellKnown[SpellIndex["KATINO"]] = true
	case Priest:
		// Pascal ROLLER.TEXT KEEPCHYN lines 383-388: SPELLSKN[23]=DIOS, SPELLSKN[24]=BADIOS
		c.PriestSpells[0] = 2
		c.MaxPriestSpells[0] = 2
		c.SpellKnown[SpellIndex["DIOS"]] = true
		c.SpellKnown[SpellIndex["BADIOS"]] = true
	}

	return c
}
