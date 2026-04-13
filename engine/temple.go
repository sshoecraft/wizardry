package engine

import "math/rand"

// TempleHeal attempts to heal a character at the Temple of Cant.
// Returns result messages and whether healing succeeded.
//
// From SHOPS segment p-code proc 23 (IC 796-1052):
//   Dead:  rand%100 > 50 + 3*VIT → failure
//   Ashed: rand%100 > 40 + 3*VIT → failure
//   Paralyzed/Stoned: always succeed (no random check in p-code)
//   On success: status=OK, age += rand(0..51)+1
//   On failure: Dead→Ashed (" NEEDS KADORTO NOW"), Ashed→Lost (" WILL BE BURIED")
func TempleHeal(c *Character) ([]string, bool) {
	var msgs []string

	oldStatus := c.Status

	// Paralyzed and Stoned always succeed — the p-code XJP in proc 23
	// only has random failure checks for Dead (status 5) and Ashed (status 6).
	if oldStatus == Paralyzed || oldStatus == Stoned {
		c.Status = OK
		c.Age += rand.Intn(52) + 1 // age 1-52 weeks
		msgs = append(msgs, c.Name+" IS WELL")
		return msgs, true
	}

	// Dead: rand%100 > 50 + 3*VIT → failure
	// From p-code proc 23 lines 904-931:
	//   random(0,0) % 100 > 50 + 3 * ATTRIB[3]
	//   ATTRIB[3] = VIT (IXP per_word=3 width=5, index 3)
	if oldStatus == Dead {
		roll := rand.Intn(100)
		threshold := 50 + 3*c.Vitality
		if roll > threshold {
			// Failure: Dead → Ashed
			c.Status = Ashed
			msgs = append(msgs, c.Name+" NEEDS KADORTO NOW")
			return msgs, false
		}
		// Success — Pascal SHOPS.TEXT line 140: HPLEFT := 1
		c.Status = OK
		c.HP = 1
		c.Age += rand.Intn(52) + 1
		msgs = append(msgs, c.Name+" IS WELL")
		return msgs, true
	}

	// Ashed: rand%100 > 40 + 3*VIT → failure
	// From p-code proc 23 lines 943-972
	if oldStatus == Ashed {
		roll := rand.Intn(100)
		threshold := 40 + 3*c.Vitality
		if roll > threshold {
			// Failure: Ashed → Lost
			c.Status = Lost
			msgs = append(msgs, c.Name+" WILL BE BURIED")
			return msgs, false
		}
		// Success — Pascal SHOPS.TEXT line 148: HPLEFT := HPMAX
		c.Status = OK
		c.HP = c.MaxHP
		c.Age += rand.Intn(52) + 1
		msgs = append(msgs, c.Name+" IS WELL")
		return msgs, true
	}

	return msgs, false
}
