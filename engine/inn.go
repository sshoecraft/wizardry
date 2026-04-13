package engine

import (
	"fmt"
	"math/rand"
)

// InnRoomQuality is the HP healed per week for each room type.
// Traced from CASTLE segment p-code (byte 5141-5171):
//   Stables=0, Cot=1, Economy=3, Merchant=7, Royal=10
var InnRoomQuality = [...]int{0, 1, 3, 7, 10}

// RestAtInn processes one character staying at the inn.
// Returns a slice of messages describing what happened.
//
// From CASTLE segment p-code proc 4 (IC 4886):
//   - Only STATUS=OK characters can stay (byte 5119-5123)
//   - Stables (quality=0): "IS NAPPING", no healing, no stat changes
//   - Paid rooms: heal loop, restore spells silently, stat changes
//   - Original animates HP/gold each tick; we show the final state
func RestAtInn(c *Character, room InnRoom) []string {
	var msgs []string

	// Only STATUS=OK characters can use the inn
	if c.Status != OK {
		return nil
	}

	cost := InnRoomCosts[room]
	quality := InnRoomQuality[room]

	if quality == 0 {
		// Stables: free, no healing — from p-code byte 4985: " IS NAPPING"
		msgs = append(msgs, fmt.Sprintf("%s IS NAPPING", c.Name))
		return msgs
	}

	// Paid rooms: heal loop
	// From p-code proc 4 (IC 4910-4964):
	//   WHILE gold >= cost AND HP < MaxHP: heal, deduct gold
	startHP := c.HP
	for c.Gold >= cost && c.HP < c.MaxHP {
		c.HP += quality
		if c.HP > c.MaxHP {
			c.HP = c.MaxHP
		}
		c.Gold -= cost
	}

	// Display result — from p-code proc 15 (IC 4568):
	//   "<name> IS HEALING UP"    (row 13)
	//   blank                     (row 14)
	//   blank                     (row 15)
	//   "         HIT POINTS (<HP>/<maxHP>)"  (row 16)
	//   blank                     (row 17)
	//   "               GOLD  <gold>"         (row 18)
	if c.HP > startHP {
		msgs = append(msgs, fmt.Sprintf("%s IS HEALING UP", c.Name))
		msgs = append(msgs, "")
		msgs = append(msgs, "")
		msgs = append(msgs, fmt.Sprintf("         HIT POINTS (%d/%d)", c.HP, c.MaxHP))
		msgs = append(msgs, "")
		msgs = append(msgs, fmt.Sprintf("               GOLD  %d", c.Gold))
	}

	// Restore spells silently — original does not display a message
	RestoreSpells(c)

	// Stat changes only happen on LEVEL UP, not every inn rest.
	// From p-code: proc 14 calls stat changes only when enough XP.
	// The caller (handleInnInput) handles level-up + stat changes.

	return msgs
}

// restoreSpells sets current spell slots to max based on character class.
// From p-code proc 18/17 (IC 2700/2778) — CIP 22 dispatches by class:
//   Fighter/Thief/Ninja: no spells
//   Mage: mage spells
//   Priest: priest spells
//   Bishop: both
//   Samurai: mage only
//   Lord: priest only
// The original restores spells silently (no display message).
func RestoreSpells(c *Character) {
	switch c.Class {
	case Mage, Bishop, Samurai:
		for i := 0; i < 7; i++ {
			c.MageSpells[i] = c.MaxMageSpells[i]
		}
	}

	switch c.Class {
	case Priest, Bishop, Lord:
		for i := 0; i < 7; i++ {
			c.PriestSpells[i] = c.MaxPriestSpells[i]
		}
	}
}

// innStatChanges applies age-dependent stat modifications.
// From p-code proc 34 (byte 3900, lex 6):
//   For each of 6 stats (STR, IQ, PIE, VIT, AGI, LCK):
//     if random()%130 < AGE/52: stat may decrease
//     else: stat may increase
//   Decrease from 18: only 1/6 chance
//   If VIT drops to 2: old age death
func InnStatChanges(c *Character) []string {
	var msgs []string
	statNames := [6]string{"STRENGTH", "I.Q.", "PIETY", "VITALITY", "AGILITY", "LUCK"}
	stats := [6]*int{&c.Strength, &c.IQ, &c.Piety, &c.Vitality, &c.Agility, &c.Luck}

	ageYears := c.Age / 52

	for i := 0; i < 6; i++ {
		// From p-code proc 6 (IC 3911-3920): random()%4 == 0 → skip this stat
		// (backward jump target in .pseudo is wrong; must be forward skip)
		if rand.Intn(4) == 0 {
			continue
		}
		// From p-code: random()%130 < ageYears → decrease path
		if rand.Intn(130) < ageYears {
			// Decrease path (bytes 3961-4016)
			if *stats[i] == 18 {
				// At max: only 1/6 chance of decrease
				if rand.Intn(6) != 4 {
					continue
				}
			}
			if *stats[i] > 1 {
				*stats[i]--
				msgs = append(msgs, fmt.Sprintf("YOU LOST %s", statNames[i]))

				// VIT drop to 2 = old age death (proc 7, IC 3816)
				// P-code IC 4009: EQUI (==), not LEQI (<=)
				if i == 3 && c.Vitality == 2 {
					msgs = append(msgs, "** YOU HAVE DIED OF OLD AGE **")
					c.Status = Lost
					c.HP = 0
					return msgs
				}
			}
		} else {
			// Increase path (bytes 4018-4049)
			if *stats[i] < 18 {
				*stats[i]++
				msgs = append(msgs, fmt.Sprintf("YOU GAINED %s", statNames[i]))
			}
		}
	}

	return msgs
}
