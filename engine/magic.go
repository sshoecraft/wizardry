package engine

import "math/rand"

// SpellType distinguishes mage and priest spell lists.
type SpellType int

const (
	MageSpell   SpellType = 0
	PriestSpell SpellType = 1
)

// SpellTarget defines what a spell can be cast on.
type SpellTarget int

const (
	TargetSelf        SpellTarget = iota // caster only
	TargetPartyMember                    // one party member
	TargetParty                          // entire party
	TargetSingleMonster                  // one monster in a group
	TargetMonsterGroup                   // one monster group
	TargetAllMonsters                    // all monster groups
)

// SpellEffect defines the category of spell effect.
type SpellEffect int

const (
	EffectDamage   SpellEffect = iota // deal damage (dice roll)
	EffectHeal                        // restore HP (dice roll)
	EffectStatus                      // inflict/cure status
	EffectBuff                        // AC/stat buff
	EffectDebuff                      // AC/stat debuff
	EffectSpecial                     // unique effect (map, light, identify, etc.)
	EffectDispel                      // remove undead
	EffectDeath                       // instant kill (save vs spell)
	EffectResurrect                   // raise dead
)

// Spell defines a single spell's properties.
type Spell struct {
	Name   string
	Level  int // 1-7
	Type   SpellType
	Target SpellTarget
	Effect SpellEffect

	// Damage/heal dice (NdS+B). Zero if not applicable.
	DiceNum   int
	DiceSides int
	DiceBonus int

	// For status effects: which status to inflict/cure
	StatusEffect Status

	// For buff/debuff: AC modifier
	ACMod int

	// DamageType is the WEPVSTY3 bit index for HITGROUP spells (0, 1, 2).
	// -1 means this spell does NOT go through HITGROUP (no resistance halving).
	// Pascal HITGROUP: if monster's WEPVSTY3[TEMP99I] is set, dice count is halved.
	DamageType int

	// Hash for spell name lookup (computed from name)
	Hash int
}

// spellHash computes the spell name hash used by the original game.
// From p-code CUTIL segment (offset 1469-1499):
//
//	hash = len(name)
//	for i, ch in name:
//	    val = ch - 64  (A=1, B=2, ...)
//	    hash += val * val * (i+1)
func spellHash(name string) int {
	hash := len(name)
	for i, ch := range name {
		val := int(ch) - 64
		hash += val * val * (i + 1)
	}
	return hash
}

// SpellDB is the complete spell database, indexed by hash for O(1) lookup.
var SpellDB map[int]*Spell

// SpellsByName allows lookup by spell name.
var SpellsByName map[string]*Spell

// SpellTable is the ordered spell list (0-indexed). Item.SpellPower is 1-indexed into this.
var SpellTable []*Spell

// SpellIndex maps spell name to its SpellTable index (for SpellKnown lookups).
var SpellIndex map[string]int

// MageSpellsByLevel groups mage spells by level (1-7).
var MageSpellsByLevel [7][]*Spell

// PriestSpellsByLevel groups priest spells by level (1-7).
var PriestSpellsByLevel [7][]*Spell

func init() {
	// Complete 50-spell table in SPELLSKN[1..50] order.
	// From Pascal COMBAT.TEXT constants (lines 8-71) and CASTLE2.TEXT TRYLEARN groups.
	// 21 mage spells (SPELLSKN 1-21) + 29 priest spells (SPELLSKN 22-50).
	spells := []*Spell{
		// ═══════════════════════════════════════════
		// MAGE SPELLS (21) — SPELLSKN[1..21]
		// ═══════════════════════════════════════════

		// Level 1 (SPELLSKN 1-4)
		{Name: "HALITO", Level: 1, Type: MageSpell, Target: TargetSingleMonster,
			Effect: EffectDamage, DiceNum: 1, DiceSides: 8, DiceBonus: 0, DamageType: -1},
		{Name: "MOGREF", Level: 1, Type: MageSpell, Target: TargetSelf,
			Effect: EffectBuff, ACMod: -2},
		{Name: "KATINO", Level: 1, Type: MageSpell, Target: TargetMonsterGroup,
			Effect: EffectStatus, StatusEffect: Asleep},
		{Name: "DUMAPIC", Level: 1, Type: MageSpell, Target: TargetSelf,
			Effect: EffectSpecial},

		// Level 2 (SPELLSKN 5-6)
		{Name: "DILTO", Level: 2, Type: MageSpell, Target: TargetMonsterGroup,
			Effect: EffectDebuff, ACMod: 2},
		{Name: "SOPIC", Level: 2, Type: MageSpell, Target: TargetSelf,
			Effect: EffectBuff, ACMod: -4},

		// Level 3 (SPELLSKN 7-8)
		{Name: "MAHALITO", Level: 3, Type: MageSpell, Target: TargetMonsterGroup,
			Effect: EffectDamage, DiceNum: 4, DiceSides: 6, DiceBonus: 0, DamageType: 1},
		{Name: "MOLITO", Level: 3, Type: MageSpell, Target: TargetMonsterGroup,
			Effect: EffectDamage, DiceNum: 3, DiceSides: 6, DiceBonus: 0, DamageType: 0},

		// Level 4 (SPELLSKN 9-11)
		{Name: "MORLIS", Level: 4, Type: MageSpell, Target: TargetMonsterGroup,
			Effect: EffectDebuff, ACMod: 3},
		{Name: "DALTO", Level: 4, Type: MageSpell, Target: TargetMonsterGroup,
			Effect: EffectDamage, DiceNum: 6, DiceSides: 6, DiceBonus: 0, DamageType: 2},
		{Name: "LAHALITO", Level: 4, Type: MageSpell, Target: TargetMonsterGroup,
			Effect: EffectDamage, DiceNum: 6, DiceSides: 6, DiceBonus: 0, DamageType: 1},

		// Level 5 (SPELLSKN 12-14)
		{Name: "MAMORLIS", Level: 5, Type: MageSpell, Target: TargetAllMonsters,
			Effect: EffectDebuff, ACMod: 3},
		{Name: "MAKANITO", Level: 5, Type: MageSpell, Target: TargetAllMonsters,
			Effect: EffectDeath},
		{Name: "MADALTO", Level: 5, Type: MageSpell, Target: TargetMonsterGroup,
			Effect: EffectDamage, DiceNum: 8, DiceSides: 8, DiceBonus: 0, DamageType: 2},

		// Level 6 (SPELLSKN 15-18)
		{Name: "LAKANITO", Level: 6, Type: MageSpell, Target: TargetMonsterGroup,
			Effect: EffectSpecial}, // Pascal DOMAGE: single group, ISISNOT resist=6*monLevel, "SMOTHERED"
		{Name: "ZILWAN", Level: 6, Type: MageSpell, Target: TargetSingleMonster,
			Effect: EffectSpecial}, // class=10 only, 10d200 damage
		{Name: "MASOPIC", Level: 6, Type: MageSpell, Target: TargetParty,
			Effect: EffectBuff, ACMod: -4},
		{Name: "HAMAN", Level: 6, Type: MageSpell, Target: TargetSelf,
			Effect: EffectSpecial},

		// Level 7 (SPELLSKN 19-21)
		{Name: "MALOR", Level: 7, Type: MageSpell, Target: TargetParty,
			Effect: EffectSpecial},
		{Name: "MAHAMAN", Level: 7, Type: MageSpell, Target: TargetSelf,
			Effect: EffectSpecial},
		{Name: "TILTOWAIT", Level: 7, Type: MageSpell, Target: TargetAllMonsters,
			Effect: EffectDamage, DiceNum: 10, DiceSides: 15, DiceBonus: 0, DamageType: 0},

		// ═══════════════════════════════════════════
		// PRIEST SPELLS (29) — SPELLSKN[22..50]
		// ═══════════════════════════════════════════

		// Level 1 (SPELLSKN 22-26)
		{Name: "KALKI", Level: 1, Type: PriestSpell, Target: TargetParty,
			Effect: EffectBuff, ACMod: -1},
		{Name: "DIOS", Level: 1, Type: PriestSpell, Target: TargetPartyMember,
			Effect: EffectHeal, DiceNum: 1, DiceSides: 8, DiceBonus: 0},
		{Name: "BADIOS", Level: 1, Type: PriestSpell, Target: TargetSingleMonster,
			Effect: EffectDamage, DiceNum: 1, DiceSides: 8, DiceBonus: 0, DamageType: -1},
		{Name: "MILWA", Level: 1, Type: PriestSpell, Target: TargetParty,
			Effect: EffectSpecial},
		{Name: "PORFIC", Level: 1, Type: PriestSpell, Target: TargetSelf,
			Effect: EffectBuff, ACMod: -4},

		// Level 2 (SPELLSKN 27-30)
		{Name: "MATU", Level: 2, Type: PriestSpell, Target: TargetParty,
			Effect: EffectBuff, ACMod: -2},
		{Name: "CALFO", Level: 2, Type: PriestSpell, Target: TargetSelf,
			Effect: EffectSpecial},
		{Name: "MANIFO", Level: 2, Type: PriestSpell, Target: TargetMonsterGroup,
			Effect: EffectStatus, StatusEffect: Paralyzed},
		{Name: "MONTINO", Level: 2, Type: PriestSpell, Target: TargetMonsterGroup,
			Effect: EffectStatus, StatusEffect: 4}, // silence

		// Level 3 (SPELLSKN 31-34)
		{Name: "LOMILWA", Level: 3, Type: PriestSpell, Target: TargetParty,
			Effect: EffectSpecial},
		{Name: "DIALKO", Level: 3, Type: PriestSpell, Target: TargetPartyMember,
			Effect: EffectSpecial}, // Pascal DOPRIEST: cure single target PLYZE/ASLEEP
		{Name: "LATUMAPIC", Level: 3, Type: PriestSpell, Target: TargetParty,
			Effect: EffectSpecial},
		{Name: "BAMATU", Level: 3, Type: PriestSpell, Target: TargetParty,
			Effect: EffectBuff, ACMod: -4},

		// Level 4 (SPELLSKN 35-38)
		{Name: "DIAL", Level: 4, Type: PriestSpell, Target: TargetPartyMember,
			Effect: EffectHeal, DiceNum: 2, DiceSides: 8, DiceBonus: 0},
		{Name: "BADIAL", Level: 4, Type: PriestSpell, Target: TargetSingleMonster,
			Effect: EffectDamage, DiceNum: 2, DiceSides: 8, DiceBonus: 0, DamageType: -1},
		{Name: "LATUMOFIS", Level: 4, Type: PriestSpell, Target: TargetPartyMember,
			Effect: EffectSpecial}, // cure poison
		{Name: "MAPORFIC", Level: 4, Type: PriestSpell, Target: TargetParty,
			Effect: EffectSpecial}, // sets global ACMOD2=2 (combat-wide party AC bonus)

		// Level 5 (SPELLSKN 39-44)
		{Name: "DIALMA", Level: 5, Type: PriestSpell, Target: TargetPartyMember,
			Effect: EffectHeal, DiceNum: 3, DiceSides: 8, DiceBonus: 0},
		{Name: "BADIALMA", Level: 5, Type: PriestSpell, Target: TargetSingleMonster,
			Effect: EffectDamage, DiceNum: 3, DiceSides: 8, DiceBonus: 0, DamageType: -1},
		{Name: "LITOKAN", Level: 5, Type: PriestSpell, Target: TargetMonsterGroup,
			Effect: EffectDamage, DiceNum: 3, DiceSides: 8, DiceBonus: 0, DamageType: 1},
		{Name: "KANDI", Level: 5, Type: PriestSpell, Target: TargetSelf,
			Effect: EffectSpecial}, // locate
		{Name: "DI", Level: 5, Type: PriestSpell, Target: TargetPartyMember,
			Effect: EffectResurrect},
		{Name: "BADI", Level: 5, Type: PriestSpell, Target: TargetSingleMonster,
			Effect: EffectDeath},

		// Level 6 (SPELLSKN 45-48)
		{Name: "LORTO", Level: 6, Type: PriestSpell, Target: TargetMonsterGroup,
			Effect: EffectDamage, DiceNum: 6, DiceSides: 6, DiceBonus: 0, DamageType: 0},
		{Name: "MADI", Level: 6, Type: PriestSpell, Target: TargetPartyMember,
			Effect: EffectHeal, DiceNum: 0, DiceSides: 0, DiceBonus: 0}, // full heal
		{Name: "MABADI", Level: 6, Type: PriestSpell, Target: TargetSingleMonster,
			Effect: EffectSpecial}, // reduce target HP to 1+rand%8
		{Name: "LOKTOFEIT", Level: 6, Type: PriestSpell, Target: TargetParty,
			Effect: EffectSpecial}, // teleport to castle

		// Level 7 (SPELLSKN 49-50)
		{Name: "MALIKTO", Level: 7, Type: PriestSpell, Target: TargetAllMonsters,
			Effect: EffectDamage, DiceNum: 12, DiceSides: 6, DiceBonus: 0, DamageType: 0},
		{Name: "KADORTO", Level: 7, Type: PriestSpell, Target: TargetPartyMember,
			Effect: EffectResurrect}, // resurrect from ashes
	}

	// Build lookup tables
	SpellTable = spells
	SpellDB = make(map[int]*Spell, len(spells))
	SpellsByName = make(map[string]*Spell, len(spells))
	SpellIndex = make(map[string]int, len(spells))
	for i, sp := range spells {
		sp.Hash = spellHash(sp.Name)
		SpellDB[sp.Hash] = sp
		SpellsByName[sp.Name] = sp
		SpellIndex[sp.Name] = i
		idx := sp.Level - 1
		if sp.Type == MageSpell {
			MageSpellsByLevel[idx] = append(MageSpellsByLevel[idx], sp)
		} else {
			PriestSpellsByLevel[idx] = append(PriestSpellsByLevel[idx], sp)
		}
	}
}

// LookupSpell finds a spell by name hash. Returns nil if not found.
func LookupSpell(name string) *Spell {
	// Uppercase the input
	upper := ""
	for _, ch := range name {
		if ch >= 'a' && ch <= 'z' {
			ch -= 32
		}
		upper += string(ch)
	}
	hash := spellHash(upper)
	return SpellDB[hash]
}

// CanCastMage returns true if the character has mage spell slots at the given level.
func (c *Character) CanCastMage(level int) bool {
	if level < 1 || level > 7 {
		return false
	}
	return c.MageSpells[level-1] > 0
}

// CanCastPriest returns true if the character has priest spell slots at the given level.
func (c *Character) CanCastPriest(level int) bool {
	if level < 1 || level > 7 {
		return false
	}
	return c.PriestSpells[level-1] > 0
}

// UseMageSlot decrements a mage spell slot. Returns false if none available.
func (c *Character) UseMageSlot(level int) bool {
	if level < 1 || level > 7 {
		return false
	}
	if c.MageSpells[level-1] <= 0 {
		return false
	}
	c.MageSpells[level-1]--
	return true
}

// UsePriestSlot decrements a priest spell slot. Returns false if none available.
func (c *Character) UsePriestSlot(level int) bool {
	if level < 1 || level > 7 {
		return false
	}
	if c.PriestSpells[level-1] <= 0 {
		return false
	}
	c.PriestSpells[level-1]--
	return true
}

// IsCaster returns true if the character's class can cast spells.
func (c *Character) IsCaster() bool {
	switch c.Class {
	case Mage, Bishop, Samurai:
		return true
	case Priest, Lord:
		return true
	}
	return false
}

// CanCastSpell returns true if the character can cast the given spell.
// Requires: correct class for spell type, spell slots at that level, AND spell is known.
func (c *Character) CanCastSpell(sp *Spell) bool {
	if sp == nil {
		return false
	}
	// Must know the spell
	if idx, ok := SpellIndex[sp.Name]; ok {
		if !c.SpellKnown[idx] {
			return false
		}
	}
	switch sp.Type {
	case MageSpell:
		// Mage, Bishop, Samurai can cast mage spells
		switch c.Class {
		case Mage, Bishop, Samurai:
			return c.CanCastMage(sp.Level)
		}
	case PriestSpell:
		// Priest, Bishop, Lord can cast priest spells
		switch c.Class {
		case Priest, Bishop, Lord:
			return c.CanCastPriest(sp.Level)
		}
	}
	return false
}

// UseSpellSlot decrements the appropriate spell slot.
func (c *Character) UseSpellSlot(sp *Spell) bool {
	if sp == nil {
		return false
	}
	switch sp.Type {
	case MageSpell:
		return c.UseMageSlot(sp.Level)
	case PriestSpell:
		return c.UsePriestSlot(sp.Level)
	}
	return false
}

// TryLearn attempts to learn new spells on level-up.
// From Pascal TRYLEARN (CASTLE2.TEXT lines 209-295).
// Calls SetSpells after learning. Returns true if any new spells were learned.
func TryLearn(c *Character) bool {
	learned := false

	// TRYMAGE — try mage spells using IQ
	for lvl := 0; lvl < 7; lvl++ {
		if c.MaxMageSpells[lvl] > 0 {
			try2Learn(c, MageSpellsByLevel[lvl], c.IQ, &learned)
		}
	}

	// TRYPRI — try priest spells using Piety
	for lvl := 0; lvl < 7; lvl++ {
		if c.MaxPriestSpells[lvl] > 0 {
			try2Learn(c, PriestSpellsByLevel[lvl], c.Piety, &learned)
		}
	}

	SetSpells(c)
	return learned
}

// try2Learn is the inner loop of TRYLEARN (Pascal TRY2LRN, CASTLE2.TEXT lines 220-237).
// For each unknown spell in the group: if random%30 < stat OR no spell known yet
// in this group, learn it.
func try2Learn(c *Character, spells []*Spell, stat int, learned *bool) {
	splKnown := false
	for _, sp := range spells {
		idx := SpellIndex[sp.Name]
		splKnown = splKnown || c.SpellKnown[idx]
	}
	for _, sp := range spells {
		idx := SpellIndex[sp.Name]
		if !c.SpellKnown[idx] {
			if rand.Intn(30) < stat || !splKnown {
				*learned = true
				splKnown = true
				c.SpellKnown[idx] = true
			}
		}
	}
}

// MigrateSpellKnown sets SpellKnown for characters loaded from pre-TRYLEARN saves.
// If no spells are marked known but the character has spell slots, assume all spells
// known at levels where they have max slots (preserves pre-TRYLEARN behavior).
func (c *Character) MigrateSpellKnown() {
	for _, k := range c.SpellKnown {
		if k {
			return // already has spell knowledge
		}
	}
	for lvl := 0; lvl < 7; lvl++ {
		if c.MaxMageSpells[lvl] > 0 {
			for _, sp := range MageSpellsByLevel[lvl] {
				c.SpellKnown[SpellIndex[sp.Name]] = true
			}
		}
		if c.MaxPriestSpells[lvl] > 0 {
			for _, sp := range PriestSpellsByLevel[lvl] {
				c.SpellKnown[SpellIndex[sp.Name]] = true
			}
		}
	}
}
