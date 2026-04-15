package engine

import (
	"fmt"
	"math/rand"
	"strings"

	"wizardry/data"
)

// CombatPhase tracks the current step within combat.
type CombatPhase int

const (
	CombatInit        CombatPhase = iota // showing encounter, press key to start
	CombatFriendly                       // friendly encounter: F)ight or L)eave
	CombatChoose                         // selecting actions for each party member
	CombatConfirm                        // "PRESS [RETURN] TO FIGHT, OR GO B)ACK"
	CombatExecute                        // resolving the round (auto-advancing messages)
	CombatChest                          // chest interaction: O)pen C)alfo L)eave I)nspect D)isarm
	CombatChestResult                    // showing result of chest action (trap effect, disarm, etc.)
	CombatVictory                        // combat won — showing rewards
	CombatDefeat                         // party wiped
)

// CombatAction represents what a party member will do this round.
type CombatAction int

const (
	ActionNone   CombatAction = iota
	ActionFight               // attack a monster group
	ActionSpell               // cast a spell
	ActionParry               // defend (+2 AC for round)
	ActionRun                 // attempt to flee
	ActionUse                 // use an item
	ActionDispel              // turn undead
)

// PartyAction holds the chosen action for one party member.
type PartyAction struct {
	Action      CombatAction
	TargetGroup int    // for fight/spell/dispel: which monster group (0-based)
	TargetAlly  int    // for heal spells: which party member (0-based)
	SpellName   string // for spell: the spell to cast
	UseItemIdx  int    // for use: index into character's Items[]
	Initiative  int    // initiative slot 1-10 (lower = acts first)
}

// CombatMonster represents one monster instance in combat.
// Based on p-code data layout: 146-word stride per combatant.
type CombatMonster struct {
	MonsterID  int    // index into Scenario.Monsters
	Name       string // display name (singular)
	NamePlural string
	HP         int
	MaxHP      int
	AC         int
	ACMod      int // combat AC modifier from spells (BATTLERC TEMP04 ARMORCL)
	Unaffect   int // runtime magic resistance (from data.Monster.Unaffect, mutable by HAMMAGIC)
	Status     int // 0=alive, 1=asleep, 2=paralyzed, 3=afraid, 4=silenced, 5=dead
	InaudCnt   int // silence counter — spell casting blocked while > 0, decremented each round
	Identified bool
	Initiative int // initiative slot 1-10 for this round (lower = acts first)
	Victim     int // pre-assigned party target index (Pascal ENATTACK VICTIM)
}

// MonsterGroup is a group of identical monsters in combat.
// From p-code: up to 4 groups, each from monster group table (104-word stride).
type MonsterGroup struct {
	MonsterID  int              // index into Scenario.Monsters
	Members    []*CombatMonster // individual monsters in this group
	AliveCnt   int              // Pascal ALIVECNT — iteration bound for alive members
	Identified bool             // party has identified this group
}

// AliveCount returns the number of living monsters in this group.
// Uses AliveCnt as the iteration bound (only Members[0..AliveCnt-1] are alive).
func (g *MonsterGroup) AliveCount() int {
	count := 0
	for i := 0; i < g.AliveCnt && i < len(g.Members); i++ {
		if g.Members[i].Status < 5 {
			count++
		}
	}
	return count
}

// CompactGroups matches Pascal COMBAT3.TEXT lines 29-73 (CUTIL phase):
// (1) Compact alive members to front of each group, dead stay in array for XP
// (2) Shift groups so empty ones (AliveCount=0) move to end
// Called during action selection (CombatChoose), NOT during ExecuteRound.
func (cs *CombatState) CompactGroups() {
	// (1) Compact alive members to front within each group.
	// Pascal COMBAT3.TEXT lines 29-60: moves alive to front, updates ALIVECNT.
	// Dead members beyond AliveCnt stay in the array for XP counting.
	for _, g := range cs.Groups {
		alive := 0
		for i := 0; i < g.AliveCnt; i++ {
			if g.Members[i].Status < 5 {
				if alive != i {
					g.Members[alive] = g.Members[i]
				}
				alive++
			}
		}
		g.AliveCnt = alive
	}
	// (2) Shift non-empty groups to front
	for i := 0; i < len(cs.Groups)-1; i++ {
		for j := i + 1; j < len(cs.Groups); j++ {
			if cs.Groups[i].AliveCount() == 0 && cs.Groups[j].AliveCount() > 0 {
				cs.Groups[i], cs.Groups[j] = cs.Groups[j], cs.Groups[i]
			}
		}
	}
}

// DisplayName returns the appropriate name for this group.
func (g *MonsterGroup) DisplayName(monsters []data.Monster) string {
	if g.MonsterID < 0 || g.MonsterID >= len(monsters) {
		return "UNKNOWN"
	}
	mon := &monsters[g.MonsterID]
	count := g.AliveCount()
	if g.Identified {
		if count == 1 {
			return mon.Name
		}
		return mon.NamePlural
	}
	if count == 1 {
		return mon.NameUnknown
	}
	return mon.NameUnknownPlural
}

// Trap types from p-code REWARDS proc 31 (IC 873 XJP table).
// The original encodes traps 0-7 with an additional effect category:
//   0-2: basic traps (trapless/poison/gas)
//   3: second-tier trap selector (crossbow/exploding/splinters/blades/stunner)
//   4: teleporter, 5: anti-mage, 6: anti-priest, 7: alarm
const (
	TrapNone       = 0
	TrapPoison     = 1
	TrapGas        = 2
	TrapCrossbow   = 3  // sub-type 0 of Pascal category 3
	TrapExploding  = 4  // sub-type 1 of Pascal category 3
	TrapSplinters  = 5  // sub-type 2 of Pascal category 3
	TrapBlades     = 6  // sub-type 3 of Pascal category 3
	TrapStunner    = 7  // sub-type 4 of Pascal category 3
	TrapTeleporter = 8  // Pascal type 4
	TrapAntiMage   = 9  // Pascal type 5
	TrapAntiPriest = 10 // Pascal type 6
	TrapAlarm      = 11 // Pascal type 7
)

// TrapNames from p-code string literals in REWARDS proc 31.
var TrapNames = [...]string{
	"TRAPLESS CHEST",
	"POISON NEEDLE",
	"GAS BOMB",
	"CROSSBOW BOLT",
	"EXPLODING BOX",
	"SPLINTERS",
	"BLADES",
	"STUNNER",
	"TELEPORTER",
	"ANTI-MAGE",
	"ANTI-PRIEST",
	"ALARM",
}

// ChestSubPhase tracks what the player is doing within the chest interaction.
type ChestSubPhase int

const (
	ChestMenu       ChestSubPhase = iota // showing O)PEN C)ALFO L)EAVE I)NSPECT D)ISARM
	ChestWhoOpen                         // "WHO (#) WILL OPEN?"
	ChestWhoCalfo                        // "WHO (#) WILL CAST CALFO?"
	ChestWhoInspect                      // "WHO (#) WILL INSPECT?"
	ChestWhoDisarm                       // "WHO (#) WILL DISARM?"
)

// PartySnap holds a frozen copy of party member display fields.
// Used during CombatExecute to prevent HP/status changes from showing
// before the corresponding combat message is displayed.
type PartySnap struct {
	HP     int
	MaxHP  int
	Status Status
	AC     int
	PoisonAmt int
}

// CombatState holds all state for one combat encounter.
type CombatState struct {
	Phase        CombatPhase
	Groups       []*MonsterGroup  // up to 4 monster groups
	Actions      []PartyAction    // one per party member
	CurrentActor int              // index of party member choosing action (during CombatChoose)
	Messages     []string         // combat log messages for current round
	MessageIndex int              // current message being displayed
	MessageTime  int64            // unix millis when current messages were shown (for auto-advance)
	Round        int              // current round number
	Surprised     int             // 0=none, 1=party surprised monsters, 2=monsters surprised party
	EncounterType int             // Pascal ATTK012: 0=random, 1=fight-zone cleared, 2=fight-zone/alarm/fixed
	Fled         bool             // party fled successfully
	Friendly     bool             // true if this is a friendly encounter (from CINIT disposition)

	// Spell input state
	SpellInput     string // current spell name being typed
	InputtingSpell bool   // true when typing a spell name

	// Group targeting state — from p-code CUTIL IC 0-200
	// When multiple groups alive, FIGHT and DISPEL prompt "AGAINST GROUP# ?"
	SelectingGroup bool         // true when waiting for group number input
	GroupAction    CombatAction  // ActionFight or ActionDispel
	GroupPrompt    string        // "FIGHT AGAINST GROUP# ?" or "DISPELL WHICH GROUP# ?"

	// Chest pause flag — from Pascal PAUSE2 after inspect/calfo results
	// Ensures message stays visible for 2 ticker cycles (~3s) before returning to menu
	ChestPauseUsed bool

	// Display snapshot — frozen from round start.
	// Pascal CUTIL calls DSPPARTY/DSPENEMY once at the top of each round.
	// During MELEE, neither monster list nor party roster refresh —
	// only the message area (rows 11-14) updates.
	DisplayAliveCounts []int        // [groupIdx] = monster alive count at round start
	DisplayPartySnap   []PartySnap  // [memberIdx] = party member display state at round start

	// Party combat AC modifiers — from BATTLERC[0].A.TEMP04[member].ARMORCL
	// Tracks spell-based AC changes during combat (MOGREF, SOPIC, PORFIC, etc.)
	// Reset each combat. Negative = better AC (harder to hit).
	PartyACMod [6]int

	// Spell targeting state — from p-code CUTIL procs 27/28
	// After entering spell name, attack spells ask "CAST SPELL ON GROUP #?"
	// and heal/buff spells ask " CAST SPELL ON PERSON # ?"
	SelectingSpellGroup  bool   // waiting for group number for attack spell
	SelectingSpellTarget bool   // waiting for person number for heal/target spell
	PendingSpellName     string // spell name pending target selection

	// USE item state — Pascal COMBAT2.TEXT lines 51-173
	SelectingUseItem bool  // waiting for item number input
	UsableItems      []int // indices into character's Items[] that are usable (have SpellPower, equipped)

	// Reward tracking
	TotalXP   int
	TotalGold int
	ItemsWon  []int // item indices found

	// Chest/trap state — from p-code REWARDS proc 16 (IC 3028-3216)
	HasChest       bool          // true if BCHEST flag set on selected reward (Pascal ENMYREWD)
	TrapType       int           // 0=trapless, 1-7=trap types
	ChestOpened    bool          // chest has been opened (proceed to rewards)
	ChestLeft      bool          // player chose to leave the chest
	ChestSubPhase  ChestSubPhase // current sub-phase within chest interaction
	ChestInspected [6]bool       // tracks which party members have already inspected
	CalfoUsed      bool          // CALFO has been cast on this chest
	ChestActor     int           // which party member is acting on the chest

	// Per-party-member drain guard — Pascal WC027: DRAINED[MYVICTIM]
	// Set by HAMAN/MAHAMAN level drain. Prevents monster drain on already-drained members.
	Drained [6]bool

	// Party member silence counters — Pascal BATTLERC[0].A.TEMP04[i].INAUDCNT
	// Set by monster MONTINO, cleared by HAMCURE/HAMHEAL
	PartyInaudCnt [6]int

	// HAMAN/MAHAMAN interactive selection (Wiz 2/3)
	// Pascal Wiz3 COMBAT4.TEXT lines 374-438: player chooses from 3 random boons
	HamanSelecting bool         // true = waiting for player to choose 1/2/3
	HamanOptions   [3]int       // the 3 effect indices offered
	HamanCaster    *Character   // the character who cast HAMAN/MAHAMAN
}

// NewCombat creates a combat encounter from the current maze cell.
// Encounter generation traced from p-code CINIT segment (seg 5).
func NewCombat(game *GameState) *CombatState {
	// Pascal DSPPARTY sorts party by status on every display.
	// Sort at combat start so dead members move to back before first choose phase.
	sortPartyByStatus(game.Town.Party.Members)

	combat := &CombatState{
		Phase:   CombatInit,
		Actions: make([]PartyAction, len(game.Town.Party.Members)),
		Round:   1,
	}

	cell := game.CurrentCell()
	if cell == nil {
		return combat
	}

	monsters := game.Scenario.Monsters
	level := game.CurrentLevel()
	if level == nil || len(monsters) == 0 {
		return combat
	}

	// Determine encounter — from CINIT p-code procs 4/5/8/9.
	// global24 controls group count limit AND group size cap:
	//   Group size cap: min(4 + global24, 9)
	//   TeamMonster chain: only adds companion if groupNum <= global24
	// For random encounters: global24 = 0 → ONE group, max 4 monsters.
	// For encounter squares: global24 from square data → more groups allowed.
	var enemyIdx, enemyRange, global24 int
	switch cell.Type {
	case data.SqEncounter:
		enemyIdx = cell.EnemyIndex
		enemyRange = cell.EnemyRange
		if enemyRange == 0 {
			enemyRange = 5
		}
		global24 = 1 // encounter squares allow 1 companion group
	case data.SqEncounter2:
		enemyIdx = cell.EnemyIndex
		enemyRange = cell.EnemyRange
		if enemyRange == 0 {
			enemyRange = 5
		}
		global24 = 4 // encounter2: p-code sets global24 = -99 (unlimited)
	case data.SqSpclEnctr:
		// AUX0 (Count field) = monster index. B1F ENMYCALC has zero escalation
		// (MultWorse=0), so ENMYCALC can't reach monster 77. AUX0=77=MURPHY'S GHOST.
		// The SpclMonster field is set in checkSquare before any count modification.
		enemyIdx = cell.SpclMonster
		enemyRange = 0
		global24 = 2
	default:
		// Random encounter — global24 = MAZELEV (1-based floor level)
		// Pascal: global24 is set to MAZELEV during dungeon traversal.
		// Affects group size cap (4+MAZELEV) and TeamMonster (nextGroup <= MAZELEV).
		global24 = game.MazeLevel + 1
		if len(level.EnemyCalc) > 0 {
			ec := level.EnemyCalc[0]
			enemyIdx = ec.MinEnemy
			enemyRange = ec.Range0N
			if enemyRange == 0 {
				enemyRange = 5
			}
			// Pascal CINIT proc 5 (IC 408-458): MultWorse rolls are UNCONDITIONAL.
			// No PercWorse gating — always execute all rolls.
			for w := 0; w < ec.MultWorse; w++ {
				if ec.Range0N > 0 {
					enemyIdx += rand.Intn(ec.Range0N) + 1
				}
			}
		}
	}

	// Pick primary monster — from CINIT proc 4 (IC 524-537)
	curMonIdx := enemyIdx
	if enemyRange > 0 {
		curMonIdx += rand.Intn(enemyRange + 1)
	}
	if curMonIdx >= len(monsters) {
		curMonIdx = len(monsters) - 1
	}
	if curMonIdx < 0 {
		curMonIdx = 0
	}

	// Generate groups — from CINIT proc 4 (IC 559-832) + proc 8 (IC 254-408):
	// Group size: min(4 + global24, 9), floor 1.
	// TeamMonster chain: only if groupNum <= global24.
	groupSizeCap := 4 + global24
	if groupSizeCap > 9 {
		groupSizeCap = 9
	}

	for g := 0; g < 4; g++ {
		if curMonIdx < 0 || curMonIdx >= len(monsters) {
			break
		}
		mon := &monsters[curMonIdx]

		count := rollDice(mon.GroupSize.Num, mon.GroupSize.Sides, mon.GroupSize.Bonus)
		if count < 1 {
			count = 1
		}
		if count > groupSizeCap {
			count = groupSizeCap
		}

		group := &MonsterGroup{
			MonsterID: curMonIdx,
			Members:   make([]*CombatMonster, count),
			AliveCnt:  count,
		}
		for i := 0; i < count; i++ {
			hp := rollDice(mon.HP.Num, mon.HP.Sides, mon.HP.Bonus)
			if hp < 1 {
				hp = 1
			}
			group.Members[i] = &CombatMonster{
				MonsterID:  curMonIdx,
				Name:       mon.Name,
				NamePlural: mon.NamePlural,
				HP:         hp,
				MaxHP:      hp,
				AC:         mon.AC,
				Unaffect:   int(mon.Unaffect & 0xFF),
				Status:     0,
			}
		}
		combat.Groups = append(combat.Groups, group)

		// TeamMonster chain — from CINIT proc 8 (IC 335-393):
		// Condition: groupNum < 4 AND groupNum <= global24 AND random% < TeamPercent
		nextGroup := g + 1
		if nextGroup < 4 && nextGroup <= global24 &&
			mon.TeamMonster > 0 && mon.TeamPercent > 0 &&
			rand.Intn(100) < mon.TeamPercent {
			curMonIdx = mon.TeamMonster
		} else {
			break
		}
	}

	// Pascal FRIENDLY (COMBAT.TEXT lines 269-317):
	// 1. Check WEPVSTY3[0] — non-friendable monster flag (WC005)
	// 2. Party must have at least one GOOD-aligned member
	// 3. Roll 0-99, look up class-based threshold, friendly if 50 <= roll <= threshold
	isFixedEncounter := cell.Type == data.SqEncounter || cell.Type == data.SqEncounter2 || cell.Type == data.SqSpclEnctr
	if !isFixedEncounter && len(combat.Groups) > 0 {
		mon := &monsters[combat.Groups[0].MonsterID]
		// WC005: WEPVSTY3[0] set means monster cannot be friendly
		if mon.WepVsType3&1 == 0 {
			// Check for GOOD party member
			hasGood := false
			for _, m := range game.Town.Party.Members {
				if m != nil && m.Alignment == Good {
					hasGood = true
					break
				}
			}
			if hasGood {
				// Class-based threshold lookup
				threshold := 50 // default
				switch mon.Class {
				case 0:
					threshold = 60
				case 1:
					threshold = 55
				case 2:
					threshold = 65
				case 3:
					threshold = 53
				case 4:
					threshold = 80
				case 7:
					threshold = 75
				}
				roll := rand.Intn(100)
				if roll >= 50 && roll <= threshold {
					combat.Friendly = true
					combat.Phase = CombatFriendly
					// Identify all groups on friendly encounter
					for _, g := range combat.Groups {
						g.Identified = true
						for _, m := range g.Members {
							m.Identified = true
						}
					}
					return combat
				}
			}
		}
	}

	// Surprise check — from Pascal source COMBAT.TEXT INITATTK lines 330-335:
	//   IF (RANDOM MOD 100) > 80 THEN SURPRISE := 1
	//   ELSE IF (RANDOM MOD 100) > 80 THEN SURPRISE := 2
	//   ELSE SURPRISE := 0
	if rand.Intn(100) > 80 {
		combat.Surprised = 1 // party surprised monsters
	} else if rand.Intn(100) > 80 {
		combat.Surprised = 2 // monsters surprised party
	}

	// From Pascal source: CINIT falls through to CUTIL with no keypress.
	// On Apple II, disk I/O and picture drawing take ~1-2 seconds naturally.
	// We use CombatInit phase with a timed auto-advance to simulate that.
	combat.Phase = CombatInit

	return combat
}

// AllMonstersDead returns true if all monsters in all groups are dead.
func (cs *CombatState) AllMonstersDead() bool {
	for _, g := range cs.Groups {
		if g.AliveCount() > 0 {
			return false
		}
	}
	return true
}

// FirstAliveGroup returns the index of the first group with living monsters, or -1.
func (cs *CombatState) FirstAliveGroup() int {
	for i, g := range cs.Groups {
		if g.AliveCount() > 0 {
			return i
		}
	}
	return -1
}

// AliveGroupCount returns the number of groups with living monsters.
func (cs *CombatState) AliveGroupCount() int {
	count := 0
	for _, g := range cs.Groups {
		if g.AliveCount() > 0 {
			count++
		}
	}
	return count
}

// rollInitiative computes initiative slot (1-10) for a party member.
// From p-code MELEE: slots 1-10, lower = acts first.
// Party members: agility-based roll — higher agility → lower initiative.
func rollPartyInitiative(member *Character) int {
	// Pascal COMBAT2.TEXT CACTION lines 356-382:
	// Base = RANDOM MOD 10 (0-9), then step-function adjustment by AGI stat.
	init := rand.Intn(10)
	switch {
	case member.Agility <= 3:
		init += 3
	case member.Agility <= 5:
		init += 2
	case member.Agility <= 7:
		init += 1
	case member.Agility == 15:
		init -= 1
	case member.Agility == 16:
		init -= 2
	case member.Agility == 17:
		init -= 3
	case member.Agility >= 18:
		init -= 4
	}
	// WC030: ninja unarmed bonus — if no items equipped, subtract level/3
	if member.Class == Ninja {
		unarmed := true
		for i := 0; i < member.ItemCount; i++ {
			if member.Items[i].Equipped {
				unarmed = false
				break
			}
		}
		if unarmed {
			init -= member.Level / 3
		}
	}
	if init < 1 {
		init = 1
	}
	if init > 10 {
		init = 10
	}
	return init
}

// rollMonsterInitiative computes initiative slot (2-9) for a monster.
// Pascal COMBAT2.TEXT ENATTACK line 801: (RANDOM MOD 8) + 2
func rollMonsterInitiative() int {
	return rand.Intn(8) + 2
}

// ExecuteRound resolves all actions for the current round.
// From p-code MELEE (seg 7, IC 0-170): iterates initiative slots 1-10,
// processing ALL combatants (party and monsters) whose initiative matches
// the current slot. Within each slot, groups are processed in order:
// group 0 (party) first, then monster groups 1-4.
// ExecuteRound resolves all actions for the current round.
// From Pascal MELEE (COMBAT5.TEXT lines 555-576): iterates initiative slots 1-10,
// groups 0-4, members 0-alive. Each action is resolved, then PAUSE1 + clear.
// The display (DSPENEMY/DSPPARTY) is NOT refreshed during MELEE — only the
// message area updates. Monster/party counts stay frozen until the next CUTIL call.
// sortPartyByStatus sorts party members so dead/worse characters move to the back.
// Pascal DSPPARTY (COMBAT3.TEXT lines 267-270): bubble sort on STATUS field.
// Status order: OK(0) < Asleep(1) < Afraid(2) < Paralyzed(3) < Stoned(4) < Dead(5) < Ashed(6) < Lost(7)
func sortPartyByStatus(members []*Character) {
	n := len(members)
	for i := 0; i < n-1; i++ {
		for j := i + 1; j < n; j++ {
			si, sj := Status(99), Status(99)
			if members[i] != nil {
				si = members[i].Status
			}
			if members[j] != nil {
				sj = members[j].Status
			}
			if si > sj {
				members[i], members[j] = members[j], members[i]
			}
		}
	}
}

func (cs *CombatState) ExecuteRound(game *GameState) {
	cs.Messages = nil
	cs.MessageIndex = 0

	// Pascal DSPPARTY (COMBAT3.TEXT lines 267-270): sort party by status.
	// Dead/ashed/lost characters bubble to the back, alive characters move forward.
	// This is how back-row characters naturally enter the front row when front dies.
	sortPartyByStatus(game.Town.Party.Members)

	party := game.Town.Party.Members

	// Snapshot display state — Pascal DSPPARTY/DSPENEMY run once at round start
	// in CUTIL, NOT during MELEE. Freeze both monster counts and party stats.
	cs.DisplayAliveCounts = make([]int, len(cs.Groups))
	for i, g := range cs.Groups {
		cs.DisplayAliveCounts[i] = g.AliveCount()
	}
	cs.DisplayPartySnap = make([]PartySnap, len(party))
	for i, m := range party {
		if m != nil {
			displayAC := m.AC
			if i < len(cs.PartyACMod) {
				displayAC += cs.PartyACMod[i]
			}
			cs.DisplayPartySnap[i] = PartySnap{
				HP: m.HP, MaxHP: m.MaxHP, Status: m.Status,
				AC: displayAC, PoisonAmt: m.PoisonAmt,
			}
		}
	}

	// DECINAUD — decrement silence counters at start of each round
	for _, g := range cs.Groups {
		for _, m := range g.Members {
			if m.InaudCnt > 0 {
				m.InaudCnt--
			}
		}
	}
	for i := range cs.PartyInaudCnt {
		if cs.PartyInaudCnt[i] > 0 {
			cs.PartyInaudCnt[i]--
		}
	}

	// Pascal HEALPRTY (COMBAT3.TEXT lines 92-96): per-round healing/poison
	// Same 25% chance as dungeon: (RANDOM MOD 4) = 2
	// Net change: HPLEFT += HEALPTS - POISNAMT
	if rand.Intn(4) == 2 {
		for _, m := range party {
			if m != nil && m.Status == OK {
				healPts := m.GetHealPts(game.Scenario.Items)
				if m.PoisonAmt > 0 || healPts > 0 {
					m.HP += healPts - m.PoisonAmt
					if m.HP <= 0 {
						m.HP = 0
						m.Status = Dead
					} else if m.HP > m.MaxHP {
						m.HP = m.MaxHP
					}
				}
			}
		}
	}

	// Surprise handling
	if cs.Surprised == 2 && cs.Round == 1 {
		cs.addMessage("THE MONSTERS SURPRISED YOU!")
		cs.assignMonsterInitiative(party)
		for slot := 1; slot <= 10; slot++ {
			cs.executeMonsterSlot(slot, game)
			if cs.allPartyDead(party) {
				break
			}
		}
		cs.Surprised = 0
		cs.checkRoundEnd(game, party)
		return
	}

	if cs.Surprised == 1 && cs.Round == 1 {
		cs.addMessage("YOU SURPRISED THE MONSTERS!")
		cs.assignPartyInitiative(party)
		for slot := 1; slot <= 10; slot++ {
			cs.executePartySlot(slot, party, game)
			if cs.AllMonstersDead() || cs.Fled {
				break
			}
		}
		cs.Surprised = 0
		cs.checkRoundEnd(game, party)
		return
	}

	// Normal round: assign initiative to all combatants
	cs.assignPartyInitiative(party)
	cs.assignMonsterInitiative(party)

	// Process slots 1-10 — from Pascal MELEE outer loop
	for slot := 1; slot <= 10; slot++ {
		cs.executePartySlot(slot, party, game)
		if cs.Fled {
			return
		}
		cs.executeMonsterSlot(slot, game)
		if cs.AllMonstersDead() || cs.allPartyDead(party) {
			break
		}
	}

	cs.checkRoundEnd(game, party)
}

// DisplayAliveCount returns the alive count for rendering during CombatExecute.
// From Pascal: DSPENEMY is called once at round start in CUTIL, NOT during MELEE.
// So the displayed counts are frozen for the entire round execution.
func (cs *CombatState) DisplayAliveCount(groupIdx int) int {
	if cs.Phase == CombatExecute && cs.DisplayAliveCounts != nil && groupIdx < len(cs.DisplayAliveCounts) {
		return cs.DisplayAliveCounts[groupIdx]
	}
	return cs.Groups[groupIdx].AliveCount()
}

// GetPartySnap returns the frozen party display state during CombatExecute.
// Returns nil if not in execute phase or no snapshot exists.
func (cs *CombatState) GetPartySnap(memberIdx int) *PartySnap {
	if cs.Phase == CombatExecute && cs.DisplayPartySnap != nil && memberIdx < len(cs.DisplayPartySnap) {
		return &cs.DisplayPartySnap[memberIdx]
	}
	return nil
}

// Removed executeOneMonsterAction — batch ExecuteRound is correct per Pascal source.

func unused_executeOneMonsterAction(cs *CombatState, cm *CombatMonster, group *MonsterGroup, game *GameState, party []*Character, monsters []data.Monster) {
	if cm.Status >= 5 || cm.Status == 2 {
		return // dead or paralyzed
	}
	if cm.Status == 1 {
		cm.Status = 0 // wake up, skip this round
		return
	}

	mon := &monsters[cm.MonsterID]
	target := pickPartyTarget(party)
	if target == nil {
		return
	}

	// Silenced → spell fails
	if cm.InaudCnt > 0 && (mon.MageSpells > 0 || mon.PriestSpells > 0) && rand.Intn(2) == 0 {
		cs.addMessage(fmt.Sprintf("%s CASTS A SPELL!", displayMonsterName(mon, cm)))
		cs.addMessage("WHICH FAILS TO BECOME AUDIBLE!")
		cs.endAction()
		return
	}
	// Fizzle zone
	if game.Fizzles > 0 && (mon.MageSpells > 0 || mon.PriestSpells > 0) && rand.Intn(2) == 0 {
		cs.addMessage(fmt.Sprintf("%s CASTS A SPELL!", displayMonsterName(mon, cm)))
		cs.addMessage("WHICH FIZZLES OUT")
		cs.endAction()
		return
	}
	// Monster spell casting
	if (mon.MageSpells > 0 || mon.PriestSpells > 0) && rand.Intn(2) == 0 {
		spellLvl := 0
		spellFlags := mon.MageSpells | mon.PriestSpells
		for lvl := 7; lvl >= 1; lvl-- {
			if spellFlags&(1<<uint(lvl-1)) != 0 {
				spellLvl = lvl
				break
			}
		}
		if spellLvl > 0 {
			cs.addMessage(fmt.Sprintf("%s CASTS A SPELL!", displayMonsterName(mon, cm)))
			dmg := rollDice(spellLvl+1, 6, 0)
			if spellLvl >= 5 {
				for _, m := range party {
					if m != nil && m.IsAlive() {
						d := dmg
						if rand.Intn(20) < m.Level {
							d /= 2
						}
						m.HP -= d
						if m.HP <= 0 {
							m.HP = 0
							m.Status = Dead
							cs.addMessage(fmt.Sprintf("%s IS SLAIN!", m.Name))
						}
					}
				}
			} else {
				if rand.Intn(20) < target.Level {
					dmg /= 2
				}
				target.HP -= dmg
				cs.addMessage(fmt.Sprintf("%s TAKES %4d DAMAGE", target.Name, dmg))
				if target.HP <= 0 {
					target.HP = 0
					target.Status = Dead
					cs.addMessage(fmt.Sprintf("%s IS SLAIN!", target.Name))
				}
			}
			cs.endAction()
			return
		}
	}

	// Breath weapon
	if mon.Breathe > 0 && rand.Intn(100) < 60 {
		cs.addMessage(fmt.Sprintf("%s BREATHES!", displayMonsterName(mon, cm)))
		for _, m := range party {
			if m == nil || !m.IsAlive() {
				continue
			}
			breathDmg := cm.HP / 2
			if breathDmg < 1 {
				breathDmg = 1
			}
			if rand.Intn(20) >= m.Level {
				breathDmg = (breathDmg + 1) / 2
			}
			m.HP -= breathDmg
			if m.HP <= 0 {
				m.HP = 0
				m.Status = Dead
				cs.addMessage(fmt.Sprintf("%s IS SLAIN!", m.Name))
			} else {
				cs.addMessage(fmt.Sprintf("%s TAKES %4d DAMAGE", m.Name, breathDmg))
			}
		}
		cs.endAction()
		return
	}

	// YELLHELP
	if (mon.SPPC&(1<<6)) != 0 && group.AliveCount() < 5 && rand.Intn(100) < 75 {
		cs.addMessage(fmt.Sprintf("%s CALLS FOR HELP!", displayMonsterName(mon, cm)))
		if group.AliveCount() >= 9 {
			cs.addMessage("BUT NONE COMES!")
		} else if rand.Intn(200) > 10*mon.HP.Num {
			cs.addMessage("BUT NONE COMES!")
		} else {
			cs.addMessage("AND IS HEARD!")
			newHP := rollDice(mon.HP.Num, mon.HP.Sides, mon.HP.Bonus)
			if newHP < 1 {
				newHP = 1
			}
			newMon := &CombatMonster{
				MonsterID:  cm.MonsterID,
				Name:       cm.Name,
				NamePlural: cm.NamePlural,
				HP:         newHP,
				MaxHP:      newHP,
				AC:         cm.AC,
				Unaffect:   int(mon.Unaffect & 0xFF),
				Status:     0,
				Initiative: -1,
			}
			group.Members = append(group.Members, newMon)
			group.AliveCnt++
		}
		cs.endAction()
		return
	}

	// Normal melee attack
	verbs := []string{"TEARS", "RIPS", "GNAWS", "BITES", "CLAWS"}
	verb := verbs[rand.Intn(len(verbs))]
	numAttacks := mon.NumAttackTypes
	if numAttacks < 1 {
		numAttacks = 1
	}
	if numAttacks > len(mon.Attacks) {
		numAttacks = len(mon.Attacks)
	}
	for a := 0; a < numAttacks; a++ {
		if target == nil || !target.IsAlive() {
			target = pickPartyTarget(party)
			if target == nil {
				return
			}
		}
		var atk data.Attack
		if a < len(mon.Attacks) {
			atk = mon.Attacks[a]
		} else {
			atk = data.Attack{Num: 1, Sides: 2, Special: 0}
		}
		partyIdx := -1
		for pi, pm := range party {
			if pm == target {
				partyIdx = pi
				break
			}
		}
		combatACMod := 0
		if partyIdx >= 0 && partyIdx < 6 {
			combatACMod = cs.PartyACMod[partyIdx]
		}
		// Pascal DAM2ME: + 2 * (ORD(SPELLHSH = 0))
		// Monsters get +2 to hit party members whose combat action is Fight (not casting)
		spellhshBonus := 0
		if partyIdx >= 0 && partyIdx < len(cs.Actions) && cs.Actions[partyIdx].Action == ActionFight {
			spellhshBonus = 2
		}
		monToHit := 20 - target.AC - mon.HP.Num + combatACMod + spellhshBonus
		if monToHit < 1 {
			monToHit = 1
		} else if monToHit > 19 {
			monToHit = 19
		}
		roll := rand.Intn(20)
		if roll >= monToHit {
			damage := rollDice(atk.Num, atk.Sides, 0)
			if damage < 1 {
				damage = 1
			}
			target.HP -= damage
			cs.addMessage(fmt.Sprintf("%s %s AT", displayMonsterName(mon, cm), verb))
			cs.addMessage(target.Name)
			if target.HP <= 0 {
				target.HP = 0
				target.Status = Dead
				cs.addMessage(fmt.Sprintf("AND HITS FOR %4d DAMAGE!", damage))
				cs.addMessage(fmt.Sprintf("%s IS SLAIN!", target.Name))
			} else {
				cs.addMessage(fmt.Sprintf("AND HITS FOR %4d DAMAGE!", damage))
			}
			if atk.Special > 0 && target.IsAlive() {
				cs.applySpecialAttack(atk.Special, target, mon, cm)
			}
			// Pascal DRAINLEV (COMBAT5.TEXT lines 147-176):
			// Guards: DRAINED[victim] (already drained this combat) or WEPVSTY3[4] (drain-immune item)
			if mon.Drain > 0 && target.IsAlive() && partyIdx >= 0 && partyIdx < 6 && !cs.Drained[partyIdx] {
				// Check WEPVSTY3 bit 4 drain immunity from equipped items
				drainImmune := false
				for ei := 0; ei < target.ItemCount; ei++ {
					if !target.Items[ei].Equipped {
						continue
					}
					eqIdx := target.Items[ei].ItemIndex
					if eqIdx >= 0 && eqIdx < len(game.Scenario.Items) {
						if game.Scenario.Items[eqIdx].WepVsType3&(1<<4) != 0 {
							drainImmune = true
							break
						}
					}
				}
				if !drainImmune {
					cs.Drained[partyIdx] = true
					drained := mon.Drain
					target.Level -= drained
					if drained == 1 {
						cs.addMessage(fmt.Sprintf("%d LEVEL IS DRAINED!", drained))
					} else {
						cs.addMessage(fmt.Sprintf("%d LEVELS ARE DRAINED!", drained))
					}
					if target.Level <= 0 {
						target.Level = 0
						target.HP = 0
						target.Status = Lost
						cs.addMessage(fmt.Sprintf("%s IS LOST!", target.Name))
					} else {
						// Pascal: HPMAX = (HPMAX / MAXLEVAC) * newLevel; MAXLEVAC = newLevel
						if target.MaxLevAC > 0 {
							target.MaxHP = (target.MaxHP / target.MaxLevAC) * target.Level
						}
						target.MaxLevAC = target.Level
						if target.MaxHP < 1 {
							target.MaxHP = 1
						}
						if target.HP > target.MaxHP {
							target.HP = target.MaxHP
						}
					}
				}
			}
		}
		cs.endAction()
	}
}

// assignPartyInitiative rolls initiative for each party member's action.
func (cs *CombatState) assignPartyInitiative(party []*Character) {
	for i := range cs.Actions {
		if i < len(party) && party[i] != nil && party[i].IsAlive() {
			cs.Actions[i].Initiative = rollPartyInitiative(party[i])
		}
	}
}

// assignMonsterInitiative rolls initiative and pre-assigns VICTIM for each monster.
// Pascal ENATTACK: assigns initiative + VICTIM once per round, not re-randomized during attacks.
func (cs *CombatState) assignMonsterInitiative(party []*Character) {
	for _, group := range cs.Groups {
		for _, cm := range group.Members {
			if cm.Status < 5 {
				cm.Initiative = rollMonsterInitiative()
				// Pre-assign victim (party target index)
				cm.Victim = pickPartyTargetIndex(party)
			}
		}
	}
}

// executePartySlot processes all party member actions matching the given initiative slot.
func (cs *CombatState) executePartySlot(slot int, party []*Character, game *GameState) {
	monsters := game.Scenario.Monsters
	for i, action := range cs.Actions {
		if action.Initiative != slot {
			continue
		}
		if i >= len(party) || party[i] == nil || !party[i].IsAlive() {
			continue
		}
		member := party[i]

		switch action.Action {
		case ActionFight:
			cs.executeFight(member, action.TargetGroup, monsters, game.Scenario.Items)

		case ActionSpell:
			cs.executeSpell(member, &action, game)
			cs.endAction()

		case ActionUse:
			// USE item — Pascal COMBAT2.TEXT lines 51-173
			// The item's spell is cast via the same executeSpell path
			if action.UseItemIdx >= 0 && action.UseItemIdx < member.ItemCount {
				poss := member.Items[action.UseItemIdx]
				if poss.ItemIndex > 0 && poss.ItemIndex < len(game.Scenario.Items) {
					item := &game.Scenario.Items[poss.ItemIndex]
					if item.SpellPower > 0 && item.SpellPower <= len(SpellTable) {
						sp := SpellTable[item.SpellPower-1]
						cs.addMessage(fmt.Sprintf("%s USES %s!", member.Name, item.Name))
						useAction := PartyAction{
							Action:      ActionSpell,
							SpellName:   sp.Name,
							TargetGroup: action.TargetGroup,
							TargetAlly:  action.TargetAlly,
						}
						cs.executeSpell(member, &useAction, game)
						// Item transformation — Pascal CHGITEM: CHGCHANC% chance
						if item.ChangeChance > 0 && rand.Intn(100) < item.ChangeChance {
							member.Items[action.UseItemIdx].ItemIndex = item.ChangeTo
							member.Items[action.UseItemIdx].Identified = false
						}
					}
				}
			}
			cs.endAction()

		case ActionParry:
			// Parry is silent — no message in the original (confirmed: no PARR string in source)

		case ActionRun:
			runChance := 50 + member.Agility*2
			if rand.Intn(100) < runChance {
				cs.addMessage("THE PARTY FLED!")
				cs.endAction()
				cs.Fled = true
				return
			}
			cs.addMessage(fmt.Sprintf("%s FAILED TO RUN!", member.Name))
			cs.endAction()

		case ActionDispel:
			cs.executeDispel(member, action.TargetGroup, monsters)
			cs.endAction()
		}

		if cs.AllMonstersDead() || cs.Fled {
			return
		}
	}
}

// executeMonsterSlot processes all monster actions matching the given initiative slot.
func (cs *CombatState) executeMonsterSlot(slot int, game *GameState) {
	party := game.Town.Party.Members
	monsters := game.Scenario.Monsters

	for _, group := range cs.Groups {
		if group.AliveCount() == 0 {
			continue
		}

		for _, cm := range group.Members {
			if cm.Initiative != slot {
				continue
			}
			if cm.Status >= 5 || cm.Status == 2 {
				// Dead or paralyzed: skip
				continue
			}
			if cm.Status == 1 {
				// Asleep: wake up, skip this round
				cm.Status = 0
				continue
			}

			mon := &monsters[cm.MonsterID]

			// Use pre-assigned VICTIM from ENATTACK phase
			var target *Character
			if cm.Victim >= 0 && cm.Victim < len(party) && party[cm.Victim] != nil && party[cm.Victim].IsAlive() {
				target = party[cm.Victim]
			} else {
				// Victim dead — pick next alive (Pascal doesn't re-randomize, but falls through)
				target = pickPartyTarget(party)
			}
			if target == nil {
				return
			}

			// Pascal ENEMYSPL (COMBAT2.TEXT lines 524-635):
			// 75% chance to try mage spell, then 75% chance to try priest spell
			spellHash := monsterSelectSpell(mon)

			if spellHash > 0 {
				// Silenced check — Pascal COMBAT4.TEXT line 774-775
				if cm.InaudCnt > 0 {
					cs.addMessage(fmt.Sprintf("%s CASTS A SPELL!", displayMonsterName(mon, cm)))
					cs.addMessage("WHICH FAILS TO BECOME AUDIBLE!")
					cs.endAction()
					continue
				}
				// Fizzle zone
				if game.Fizzles > 0 {
					cs.addMessage(fmt.Sprintf("%s CASTS A SPELL!", displayMonsterName(mon, cm)))
					cs.addMessage("WHICH FIZZLES OUT")
					cs.endAction()
					continue
				}
				// Look up spell and execute via proper spell system
				sp, ok := SpellDB[spellHash]
				if ok && sp != nil {
					cs.addMessage(fmt.Sprintf("%s CASTS %s!", displayMonsterName(mon, cm), sp.Name))
					// Monster casting: CASTGR=0 (target party), CASTI=VICTIM
					cs.executeMonsterSpell(sp, cm, party, game)
					cs.endAction()
					continue
				}
			}

			// Breath weapon — Pascal DOBREATH (COMBAT5.TEXT lines 92-116)
			// 60% chance of choosing breath (COMBAT2.TEXT line 663)
			if mon.Breathe > 0 && rand.Intn(100) < 60 {
				cs.addMessage(fmt.Sprintf("%s BREATHES!", displayMonsterName(mon, cm)))
				for _, m := range party {
					if m == nil || !m.IsAlive() {
						continue
					}
					// Base: monster current HP / 2
					breathDmg := cm.HP / 2
					if breathDmg < 1 {
						breathDmg = 1
					}
					// Save vs breath: (RANDOM MOD 20) >= LUCKSKIL[3]
					// Using Level as approximation for breath save skill
					if rand.Intn(20) >= m.Level {
						breathDmg = (breathDmg + 1) / 2
					}
					m.HP -= breathDmg
					if m.HP <= 0 {
						m.HP = 0
						m.Status = Dead
						cs.addMessage(fmt.Sprintf("%s IS SLAIN!", m.Name))
					} else {
						cs.addMessage(fmt.Sprintf("%s TAKES %4d DAMAGE", m.Name, breathDmg))
					}
				}
				cs.endAction()
				continue
			}

			// YELLHELP — Pascal COMBAT2.TEXT lines 638-645, COMBAT5.TEXT lines 447-481
			// Monster calls for help if: SPPC[6] set, alive < 5, 75% chance
			if (mon.SPPC&(1<<6)) != 0 && group.AliveCount() < 5 && rand.Intn(100) < 75 {
				cs.addMessage(fmt.Sprintf("%s CALLS FOR HELP!", displayMonsterName(mon, cm)))
				if group.AliveCount() >= 9 {
					cs.addMessage("BUT NONE COMES!")
				} else if rand.Intn(200) > 10*mon.HP.Num {
					// resist = 10 * monster_level vs RANDOM MOD 200
					cs.addMessage("BUT NONE COMES!")
				} else {
					cs.addMessage("AND IS HEARD!")
					newHP := rollDice(mon.HP.Num, mon.HP.Sides, mon.HP.Bonus)
					if newHP < 1 {
						newHP = 1
					}
					newMon := &CombatMonster{
						MonsterID:  cm.MonsterID,
						Name:       cm.Name,
						NamePlural: cm.NamePlural,
						HP:         newHP,
						MaxHP:      newHP,
						AC:         cm.AC,
						Unaffect:   int(mon.Unaffect & 0xFF),
						Status:     0,
						Initiative: -1, // won't act this round
					}
					group.Members = append(group.Members, newMon)
					group.AliveCnt++
				}
				cs.endAction()
				continue
			}

			// Normal attack — from p-code SWINGASW
			verbs := []string{"TEARS", "RIPS", "GNAWS", "BITES", "CLAWS"}
			verb := verbs[rand.Intn(len(verbs))]

			numAttacks := mon.NumAttackTypes
			if numAttacks < 1 {
				numAttacks = 1
			}
			if numAttacks > len(mon.Attacks) {
				numAttacks = len(mon.Attacks)
			}

			for a := 0; a < numAttacks; a++ {
				if target == nil || !target.IsAlive() {
					target = pickPartyTarget(party)
					if target == nil {
						return
					}
				}

				var atk data.Attack
				if a < len(mon.Attacks) {
					atk = mon.Attacks[a]
				} else {
					atk = data.Attack{Num: 1, Sides: 2, Special: 0}
				}

				// Monster to-hit formula (game.pas lines 6205-6211):
				//   to_hit = 20 - charAC - monsterHPLevel + combatACmods
				// HPREC.LEVEL = monster HP dice count (higher = more accurate)
				partyIdx := -1
				for pi, pm := range party {
					if pm == target {
						partyIdx = pi
						break
					}
				}
				combatACMod := 0
				if partyIdx >= 0 && partyIdx < 6 {
					combatACMod = cs.PartyACMod[partyIdx]
				}
				monToHit := 20 - target.AC - mon.HP.Num + combatACMod
				if monToHit < 1 {
					monToHit = 1
				} else if monToHit > 19 {
					monToHit = 19
				}
				roll := rand.Intn(20)
				if roll >= monToHit {
					damage := rollDice(atk.Num, atk.Sides, 0)
					if damage < 1 {
						damage = 1
					}

					target.HP -= damage

					cs.addMessage(fmt.Sprintf("%s %s AT", displayMonsterName(mon, cm), verb))
					cs.addMessage(target.Name)

					if target.HP <= 0 {
						target.HP = 0
						target.Status = Dead
						cs.addMessage(fmt.Sprintf("AND HITS FOR %4d DAMAGE!", damage))
						cs.addMessage(fmt.Sprintf("%s IS SLAIN!", target.Name))
					} else {
						cs.addMessage(fmt.Sprintf("AND HITS FOR %4d DAMAGE!", damage))
					}

					if atk.Special > 0 && target.IsAlive() {
						cs.applySpecialAttack(atk.Special, target, mon, cm)
					}

					if mon.Drain > 0 && target.IsAlive() {
						drained := mon.Drain
						target.Level -= drained
						if drained == 1 {
							cs.addMessage(fmt.Sprintf("%d LEVEL IS DRAINED!", drained))
						} else {
							cs.addMessage(fmt.Sprintf("%d LEVELS ARE DRAINED!", drained))
						}
						if target.Level <= 0 {
							target.Level = 0
							target.HP = 0
							target.Status = Lost
							cs.addMessage(fmt.Sprintf("%s IS LOST!", target.Name))
						} else {
							newMax := target.Level * (classHPDie(target.Class) / 2)
							if newMax < 1 {
								newMax = 1
							}
							if target.MaxHP > newMax {
								target.MaxHP = newMax
							}
							if target.HP > target.MaxHP {
								target.HP = target.MaxHP
							}
						}
					}
				}
				cs.endAction()
			}
		}
	}
}

// allPartyDead returns true if all party members are dead.
func (cs *CombatState) allPartyDead(party []*Character) bool {
	for _, m := range party {
		if m != nil && m.IsAlive() {
			return false
		}
	}
	return true
}

// executeMonsterSpell handles a monster casting a spell at the party.
// Pascal CASTASPE (COMBAT4.TEXT lines 18-781): CASTGR=0 (target=party), CASTI=VICTIM.
// Dispatches by spell name for proper bidirectional routing through the original
// DOPRIEST/DOMAGE handlers.
func (cs *CombatState) executeMonsterSpell(sp *Spell, cm *CombatMonster, party []*Character, game *GameState) {
	victim := cm.Victim

	switch sp.Name {
	// --- MONTINO: silence all party members ---
	// Pascal DOSILENC (COMBAT4.TEXT lines 232-251): loop through party,
	// resist = 100 - 5*LUCKSKIL[4] (character Luck stat)
	// Effect: INAUDCNT = rand%4 + 2 (2-5 rounds)
	case "MONTINO":
		for i, m := range party {
			if m == nil || !m.IsAlive() || i >= 6 {
				continue
			}
			resist := 100 - 5*m.Luck
			if resist < 0 {
				resist = 0
			}
			if rand.Intn(100) < resist {
				cs.addMessage(fmt.Sprintf("%s IS NOT SILENCED", m.Name))
			} else {
				cs.PartyInaudCnt[i] = rand.Intn(4) + 2
				cs.addMessage(fmt.Sprintf("%s IS SILENCED", m.Name))
			}
		}

	// --- MANIFO: paralyze all party members ---
	// Pascal DOHOLD (COMBAT4.TEXT): loop through party,
	// resist = 50 + 10*CHARLEV, only affects status <= ASLEEP
	case "MANIFO":
		for _, m := range party {
			if m == nil || !m.IsAlive() {
				continue
			}
			if m.Status > Asleep {
				continue
			}
			resist := 50 + 10*m.Level
			if rand.Intn(100) < resist {
				cs.addMessage(fmt.Sprintf("%s IS NOT HELD", m.Name))
			} else {
				m.Status = Paralyzed
				cs.addMessage(fmt.Sprintf("%s IS HELD", m.Name))
			}
		}

	// --- KATINO: sleep all party members ---
	// Pascal DOSLEPT: loop through party, resist = 20*CHARLEV
	case "KATINO":
		for _, m := range party {
			if m == nil || !m.IsAlive() {
				continue
			}
			if m.Status > Asleep {
				continue
			}
			resist := 20 * m.Level
			if rand.Intn(100) < resist {
				cs.addMessage(fmt.Sprintf("%s IS NOT SLEPT", m.Name))
			} else {
				m.Status = Asleep
				cs.addMessage(fmt.Sprintf("%s IS SLEPT", m.Name))
			}
		}

	// --- MABADI: reduce single party member HP to 1-8 ---
	// Pascal DOPRIEST lines 664-675 (WC013): only if STATUS < DEAD
	case "MABADI":
		if victim >= 0 && victim < len(party) && party[victim] != nil && party[victim].IsAlive() {
			t := party[victim]
			cs.addMessage(fmt.Sprintf("%s IS HIT BY MABADI!", t.Name))
			t.HP = 1 + rand.Intn(8)
			if t.HP > t.MaxHP {
				t.HP = t.MaxHP
			}
		}

	// --- BADI: instant kill single party member ---
	// Pascal DOSLAIN: resist = 10*CHARLEV, on failure STATUS=DEAD, HPLEFT=0
	case "BADI":
		if victim >= 0 && victim < len(party) && party[victim] != nil && party[victim].IsAlive() {
			t := party[victim]
			resist := 10 * t.Level
			if rand.Intn(100) < resist {
				cs.addMessage(fmt.Sprintf("%s RESISTED", t.Name))
			} else {
				t.HP = 0
				t.Status = Dead
				cs.addMessage(fmt.Sprintf("%s IS SLAIN!", t.Name))
			}
		}

	// --- DILTO/MORLIS/MAMORLIS: AC debuff on all party ---
	// Pascal DOMAGE: MODAC(0, -ACMod, 0, PARTYCNT-1)
	case "DILTO", "MORLIS", "MAMORLIS":
		for i, m := range party {
			if m != nil && m.IsAlive() && i < 6 {
				cs.PartyACMod[i] -= sp.ACMod // Go convention: positive ACMod → worse
			}
		}
		cs.addMessage("YOUR AC WORSENED!")

	// --- Damage spells: BADIOS, BADIAL, BADIALMA (single target) ---
	// Pascal DOHITS: single target with UNAFFECT check
	case "BADIOS", "BADIAL", "BADIALMA":
		if victim >= 0 && victim < len(party) && party[victim] != nil && party[victim].IsAlive() {
			t := party[victim]
			dmg := rollDice(sp.DiceNum, sp.DiceSides, sp.DiceBonus)
			if dmg < 1 {
				dmg = 1
			}
			t.HP -= dmg
			cs.addMessage(fmt.Sprintf("%s TAKES %d DAMAGE", t.Name, dmg))
			if t.HP <= 0 {
				t.HP = 0
				t.Status = Dead
				cs.addMessage(fmt.Sprintf("%s IS SLAIN!", t.Name))
			}
		}

	// --- Group/all damage spells hitting party ---
	// TILTOWAIT, MALIKTO, LORTO, MOLITO, MAHALITO, DALTO, LAHALITO, MADALTO, HALITO, LITOKAN
	default:
		switch sp.Effect {
		case EffectDamage:
			// All party members take damage
			for _, m := range party {
				if m == nil || !m.IsAlive() {
					continue
				}
				dmg := rollDice(sp.DiceNum, sp.DiceSides, sp.DiceBonus)
				if dmg < 1 {
					dmg = 1
				}
				m.HP -= dmg
				cs.addMessage(fmt.Sprintf("%s TAKES %d DAMAGE", m.Name, dmg))
				if m.HP <= 0 {
					m.HP = 0
					m.Status = Dead
					cs.addMessage(fmt.Sprintf("%s IS SLAIN!", m.Name))
				}
			}
		case EffectDeath:
			// MAKANITO-style instant kill on all party (rare for monster cast)
			if victim >= 0 && victim < len(party) && party[victim] != nil && party[victim].IsAlive() {
				t := party[victim]
				resist := 10 * t.Level
				if rand.Intn(100) < resist {
					cs.addMessage(fmt.Sprintf("%s RESISTED", t.Name))
				} else {
					t.HP = 0
					t.Status = Dead
					cs.addMessage(fmt.Sprintf("%s IS SLAIN!", t.Name))
				}
			}
		case EffectDebuff:
			for i, m := range party {
				if m != nil && m.IsAlive() && i < 6 {
					cs.PartyACMod[i] -= sp.ACMod
				}
			}
			cs.addMessage("YOUR AC WORSENED!")
		}
	}
}

// checkRoundEnd sets up for message display after a round.
// Phase is ALWAYS set to CombatExecute so messages are shown.
// The actual transition to CombatChest/CombatDefeat/CombatChoose
// happens in advanceCombatMessages (main.go) after all messages are displayed.
func (cs *CombatState) checkRoundEnd(game *GameState, party []*Character) {
	if cs.AllMonstersDead() {
		cs.generateTrap(game)
	}

	if cs.allPartyDead(party) {
		cs.addMessage("** THE PARTY HAS PERISHED **")
	}

	// Always go to CombatExecute to display messages first
	cs.Phase = CombatExecute
}

// executeFight resolves a party member's melee attack against a monster group.
// From p-code SWINGASW proc 1 (real code at IC 2086-2676, labeled "proc 13" in pseudo):
//   - to_hit = 21 - monsterAC - weaponHitMod, clamped [1,19]
//   - Hit roll: random()%20, hit if roll >= to_hit
//   - Swings from TCHAR.SWINGCNT (word91 = 1 + weapon extra swings)
//   - Double damage vs sleeping (status 2) and weapon-vs-type effectiveness
//   - Critical hit: CRITHITM (word90) enables, level*2 % chance (cap 50%)
func (cs *CombatState) executeFight(member *Character, targetGroup int, monsters []data.Monster, items []data.Item) {
	if targetGroup < 0 || targetGroup >= len(cs.Groups) {
		return
	}
	group := cs.Groups[targetGroup]
	if group.AliveCount() == 0 {
		targetGroup = cs.FirstAliveGroup()
		if targetGroup < 0 {
			return
		}
		group = cs.Groups[targetGroup]
	}

	// Find first alive monster in group
	var target *CombatMonster
	for _, m := range group.Members {
		if m.Status < 5 {
			target = m
			break
		}
	}
	if target == nil {
		return
	}

	mon := &monsters[target.MonsterID]

	// Attack verb
	verbs := []string{"SWINGS", "THRUSTS", "STABS", "SLASHES", "CHOPS"}
	verb := verbs[rand.Intn(len(verbs))]

	// Look up equipped weapon for damage dice, extra swings, hit modifier, and
	// weapon-vs-type effectiveness flags.
	dmgDice := 1
	dmgSides := 2
	dmgBonus := 0
	extraSwings := 0
	weaponHitMod := 0
	var weaponWepVsType uint16
	for i := 0; i < member.ItemCount; i++ {
		if !member.Items[i].Equipped {
			continue
		}
		idx := member.Items[i].ItemIndex
		if idx < 0 || idx >= len(items) {
			continue
		}
		item := &items[idx]
		if item.Damage != nil {
			dmgDice = item.Damage.Num
			dmgSides = item.Damage.Sides
			dmgBonus = item.Damage.Bonus
			extraSwings = item.ExtraSwings
			weaponHitMod = item.HitMod
			weaponWepVsType = item.WepVsType
			break
		}
	}

	// SWINGCNT from game.pas lines 1441-1453:
	//   Base = 1
	//   Fighter/Samurai/Lord/Ninja: += level/5 + (1 if Ninja)
	//   Cap at 10, then weapon extra swings added
	swings := 1
	if member.Class == Fighter || member.Class == Samurai ||
		member.Class == Lord || member.Class == Ninja {
		swings += member.Level / 5
		if member.Class == Ninja {
			swings++
		}
	}
	swings += extraSwings
	if swings > 10 {
		swings = 10
	}
	if swings < 1 {
		swings = 1
	}

	// HPCALCMD (attack modifier) from game.pas lines 1417-1435:
	//   Fighter/Priest/Samurai/Lord/Ninja: 2 + level/3
	//   Mage/Thief/Bishop: level/5
	//   STR > 15: += STR - 15
	//   STR < 6:  += STR - 6 (penalty)
	//   Plus weapon hit modifier
	var hpcalcmd int
	if member.Class == Fighter || member.Class == Priest ||
		member.Class == Samurai || member.Class == Lord || member.Class == Ninja {
		hpcalcmd = 2 + member.Level/3
	} else {
		hpcalcmd = member.Level / 5
	}
	if member.Strength > 15 {
		hpcalcmd += member.Strength - 15
	} else if member.Strength < 6 {
		hpcalcmd += member.Strength - 6
	}
	hpcalcmd += weaponHitMod

	// To-hit formula from game.pas lines 6276-6280 and p-code SWINGASW IC 2155-2218:
	//   to_hit = 21 - monsterAC - HPCALCMD + combatACmod + groupDistancePenalty
	// combatACmod (BATTLERC TEMP04 ARMORCL) tracks spell buffs/debuffs on the target;
	// not yet tracked in our combat state.
	// Original p-code has "- 3 * VICTIM" which is a known bug (WC015): makes back
	// groups EASIER to hit. Fixed version from Wizardry v3.1:
	//   + ((3 * VICTIM) - 6) where VICTIM is 1-indexed group number
	// Group 1: -3 (easiest), Group 2: 0, Group 3: +3, Group 4: +6 (hardest)
	groupPenalty := 3*(targetGroup+1) - 6
	toHit := 21 - mon.AC - hpcalcmd - target.ACMod + groupPenalty
	if toHit < 1 {
		toHit = 1
	}
	if toHit > 19 {
		toHit = 19
	}

	// Swing loop: accumulate hits and damage (p-code IC 2220-2288)
	totalHits := 0
	totalDamage := 0
	for s := 0; s < swings; s++ {
		roll := rand.Intn(20)
		if roll >= toHit {
			damage := rollDice(dmgDice, dmgSides, dmgBonus)
			if damage < 1 {
				damage = 1
			}
			totalDamage += damage
			totalHits++
		}
	}

	// Damage multiplier: sleeping/paralyzed targets (p-code IC 2290-2312)
	// P-code checks member.word6 == 2 (paralyzed/sleeping in combat context)
	if (target.Status == 1 || target.Status == 2) && totalDamage > 0 {
		totalDamage *= 2
	}

	// Pascal COMBAT5.TEXT DAM2ENMY line 394:
	//   IF CHARACTR[BATI].WEPVSTYP[BATTLERC[VICTIM].B.CLASS] THEN damage *= 2
	// WEPVSTYP is set from the equipped weapon's WepVsType field.
	if totalDamage > 0 && mon.Class >= 0 && mon.Class < 16 {
		if weaponWepVsType&(1<<uint(mon.Class)) != 0 {
			totalDamage *= 2
		}
	}

	// Display attack message — Apple II format with right-justified numbers
	// "RODON SLASHES AT A" / "SMALL HUMANOID" (wraps at 38 chars)
	cs.addMessage(fmt.Sprintf("%s %s AT A", member.Name, verb))
	cs.addMessage(displayMonsterName(mon, target))
	if totalDamage == 0 {
		cs.addMessage("AND MISSES!")
	} else {
		// Right-justified numbers matching Apple II PRINTNUM format
		cs.addMessage(fmt.Sprintf("AND HITS %4d TIMES FOR %4d DAMAGE!", totalHits, totalDamage))

		// Apply damage (game.pas line 6309-6310)
		target.HP -= totalDamage

		// Critical hit check (game.pas lines 6311-6326)
		hasCrit := member.Class == Ninja
		if hasCrit && totalDamage > 0 {
			critChance := member.Level * 2
			if critChance > 50 {
				critChance = 50
			}
			if rand.Intn(100) < critChance {
				monHPLevel := mon.HP.Num
				if rand.Intn(35) > monHPLevel+10 {
					cs.addMessage("A CRITICAL HIT!")
					target.HP = 0
				}
			}
		}

		// Check for kill (game.pas lines 6328-6335)
		if target.HP <= 0 {
			target.HP = 0
			target.Status = 5
			cs.addMessage(fmt.Sprintf("%s KILLS ONE!", member.Name))
		}
	}
	cs.endAction()
}

func displayMonsterName(mon *data.Monster, cm *CombatMonster) string {
	if cm.Identified {
		return mon.Name
	}
	return mon.NameUnknown
}

// executeSpell resolves a spell cast in combat.
// From p-code CASTASPE segment (seg 8).
func (cs *CombatState) executeSpell(member *Character, action *PartyAction, game *GameState) {
	sp := LookupSpell(action.SpellName)
	if sp == nil {
		cs.addMessage(fmt.Sprintf("%s: YOU DONT KNOW THAT SPELL", member.Name))
		return
	}

	if !member.CanCastSpell(sp) {
		cs.addMessage(fmt.Sprintf("%s: SPELL POINTS EXHAUSTED", member.Name))
		return
	}

	// Check for disruption — from p-code: status >= 2 (paralyzed) means spell disrupted
	if member.Status == Paralyzed || member.Status == Asleep {
		cs.addMessage(fmt.Sprintf("%s: SPELL DISRUPTED", member.Name))
		return
	}

	// Pascal CASTASPE: INAUDCNT > 0 → "WHICH FAILS TO BECOME AUDIBLE!"
	memberIdx := -1
	for pi, pm := range game.Town.Party.Members {
		if pm == member {
			memberIdx = pi
			break
		}
	}
	if memberIdx >= 0 && memberIdx < 6 && cs.PartyInaudCnt[memberIdx] > 0 {
		cs.addMessage("WHICH FAILS TO BECOME AUDIBLE!")
		return
	}

	// Use the spell slot
	member.UseSpellSlot(sp)

	cs.addMessage(fmt.Sprintf("%s CASTS %s!", member.Name, sp.Name))

	// Pascal COMBAT4.TEXT line 776-777: IF FIZZLES > 0 THEN EXITCAST('WHICH FIZZLES OUT')
	if game.Fizzles > 0 {
		cs.addMessage("WHICH FIZZLES OUT")
		cs.endAction()
		return
	}

	monsters := game.Scenario.Monsters

	switch sp.Effect {
	case EffectDamage:
		cs.resolveSpellDamage(sp, action, monsters)

	case EffectHeal:
		cs.resolveSpellHeal(sp, action, game)

	case EffectStatus:
		cs.resolveSpellStatus(sp, action, monsters)

	case EffectBuff:
		// AC buff applied to caster or party — from p-code CASTASPE CIP 6
		// ACMod is negative (e.g., -2, -4) meaning harder to hit
		if sp.Target == TargetSelf {
			idx := -1
			for pi, pm := range game.Town.Party.Members {
				if pm == member {
					idx = pi
					break
				}
			}
			if idx >= 0 && idx < 6 {
				cs.PartyACMod[idx] += sp.ACMod
			}
			cs.addMessage(fmt.Sprintf("%s'S AC IMPROVED!", member.Name))
		} else if sp.Target == TargetParty {
			for i := range game.Town.Party.Members {
				if i < 6 && game.Town.Party.Members[i] != nil && game.Town.Party.Members[i].IsAlive() {
					cs.PartyACMod[i] += sp.ACMod
				}
			}
			cs.addMessage("PARTY'S AC IMPROVED!")
		}

	case EffectDebuff:
		// AC debuff on monsters — MORLIS (single group), MAMORLIS (all groups)
		// Pascal COMBAT4.TEXT: MORLIS = MODAC(CASTGR, -3, ...), MAMORLIS = all groups
		if sp.Target == TargetAllMonsters {
			for _, group := range cs.Groups {
				for _, m := range group.Members {
					if m.Status < 5 {
						m.ACMod += sp.ACMod
					}
				}
			}
			cs.addMessage("MONSTERS' AC WORSENED!")
		} else if action.TargetGroup >= 0 && action.TargetGroup < len(cs.Groups) {
			group := cs.Groups[action.TargetGroup]
			for _, m := range group.Members {
				if m.Status < 5 {
					m.ACMod += sp.ACMod
				}
			}
			cs.addMessage("MONSTERS' AC WORSENED!")
		}

	case EffectDeath:
		cs.resolveSpellDeath(sp, action, monsters)

	case EffectSpecial:
		// Non-combat effects show a message
		switch sp.Name {
		case "DIALKO":
			// Pascal DOPRIEST lines 609-619: cure single target PLYZE/ASLEEP only
			if action.TargetAlly >= 0 && action.TargetAlly < len(game.Town.Party.Members) {
				target := game.Town.Party.Members[action.TargetAlly]
				if target != nil {
					if target.Status == Paralyzed || target.Status == Asleep {
						target.Status = OK
						cs.addMessage(fmt.Sprintf("%s IS CURED!", target.Name))
					} else {
						cs.addMessage(fmt.Sprintf("%s IS NOT HELPED!", target.Name))
					}
				}
			}
		case "LATUMOFIS":
			// Cure poison — not a combat status in our model, just message
			cs.addMessage("POISON CURED!")
		case "MILWA":
			// Pascal COMBAT4.TEXT line 598: LIGHT := LIGHT + 15 + (RANDOM MOD 15)
			game.LightLevel += 15 + rand.Intn(15)
			cs.addMessage("LIGHT!")
		case "LOMILWA":
			// Pascal COMBAT4.TEXT line 608: LIGHT := 32000
			game.LightLevel = 32000
			cs.addMessage("LIGHT!")
		case "HAMAN":
			cs.hammaham(game, member, 6)
		case "MAHAMAN":
			cs.hammaham(game, member, 8)
		case "LOKTOFEIT":
			// Pascal SLOKTOFE (COMBAT4.TEXT lines 488-511):
			// Success = (RANDOM MOD 100) <= 2 * caster level
			// Strips ALL items and gold from entire party, exits combat
			if rand.Intn(100) > 2*member.Level {
				cs.addMessage("LOKTOFEIT FAILS!")
			} else {
				cs.addMessage("LOKTOFEIT!")
				for _, m := range game.Town.Party.Members {
					if m == nil {
						continue
					}
					m.Items = [8]Possession{}
					m.ItemCount = 0
					m.Gold = 0
				}
				cs.Phase = CombatDefeat // exit combat — party returns to castle
			}
		case "MALOR":
			// Pascal SMALOR (COMBAT4.TEXT lines 557-582):
			// Random x/y, chance to go deeper, exits combat
			game.PlayerX = rand.Intn(20)
			game.PlayerY = rand.Intn(20)
			for rand.Intn(100) < 30 {
				game.MazeLevel++
			}
			for rand.Intn(100) < 10 {
				game.MazeLevel++
			}
			maxLevel := len(game.Scenario.Mazes.Levels)
			if game.MazeLevel >= maxLevel {
				game.MazeLevel = maxLevel - 1
			}
			cs.addMessage("MALOR!")
			cs.Phase = CombatDefeat // exits combat
		case "MABADI":
			// Pascal COMBAT4.TEXT lines 664-675:
			// Reduce single monster HP to 1+rand%8
			if action.TargetGroup >= 0 && action.TargetGroup < len(cs.Groups) {
				group := cs.Groups[action.TargetGroup]
				for _, m := range group.Members {
					if m.Status < 5 {
						mon := &monsters[m.MonsterID]
						m.HP = 1 + rand.Intn(8)
						cs.addMessage(fmt.Sprintf("%s IS HIT BY MABADI!", displayMonsterName(mon, m)))
						break // single target
					}
				}
			}
		case "LAKANITO":
			// Pascal DOMAGE lines 720-724: single group, per-monster ISISNOT
			// Resist chance = 6 * monLevel, DAMTYPE=2 (kill), "SMOTHERED"
			if action.TargetGroup >= 0 && action.TargetGroup < len(cs.Groups) {
				group := cs.Groups[action.TargetGroup]
				for _, m := range group.Members {
					if m.Status >= 5 {
						continue
					}
					mon := &monsters[m.MonsterID]
					resistChance := 6 * mon.HP.Num // HP.Num = HPREC.LEVEL (monster level)
					if rand.Intn(100) < resistChance {
						cs.addMessage(fmt.Sprintf("%s IS NOT SMOTHERED", displayMonsterName(mon, m)))
					} else {
						m.HP = 0
						m.Status = 5
						cs.addMessage(fmt.Sprintf("%s IS SMOTHERED", displayMonsterName(mon, m)))
					}
				}
			}
		case "ZILWAN":
			// Pascal COMBAT4.TEXT lines 725-727: class=10 only, DOHITS 10d200
			if action.TargetGroup >= 0 && action.TargetGroup < len(cs.Groups) {
				group := cs.Groups[action.TargetGroup]
				for _, m := range group.Members {
					if m.Status >= 5 {
						continue
					}
					mon := &monsters[m.MonsterID]
					if mon.Class == 10 {
						dmg := rollDice(10, 200, 0)
						m.HP -= dmg
						if m.HP <= 0 {
							m.HP = 0
							m.Status = 5
							cs.addMessage(fmt.Sprintf("%s IS DISPELLED!", displayMonsterName(mon, m)))
						} else {
							cs.addMessage(fmt.Sprintf("%s TAKES %d DAMAGE!", displayMonsterName(mon, m), dmg))
						}
					} else {
						cs.addMessage(fmt.Sprintf("%s IS UNAFFECTED!", displayMonsterName(mon, m)))
					}
					break // single target (DOHITS uses CASTI)
				}
			}
		case "MAPORFIC":
			// Pascal COMBAT4.TEXT line 640: ACMOD2 := 2
			// Global AC bonus for entire party — add to each member's combat AC mod
			for i := 0; i < len(game.Town.Party.Members); i++ {
				cs.PartyACMod[i] += 2
			}
			cs.addMessage("MAPORFIC!")
		case "LATUMAPIC":
			// Pascal COMBAT4.TEXT lines 621-626: identifies all monster groups
			for _, g := range cs.Groups {
				g.Identified = true
				for _, m := range g.Members {
					m.Identified = true
				}
			}
			cs.addMessage("MONSTERS IDENTIFIED!")
		case "DUMAPIC", "CALFO":
			cs.addMessage(fmt.Sprintf("%s HAS NO EFFECT IN COMBAT.", sp.Name))
		case "KANDI":
			// Pascal DOPRIEST line 647-648: KANDI = DODISRUP in combat
			cs.addMessage("SPELL DISRUPTED")
		default:
			cs.addMessage("SPELL CAST!")
		}

	case EffectResurrect:
		// Pascal DOPRIEST: DI and KADORTO call DODISRUP in combat ("SPELL DISRUPTED")
		if sp.Name == "DI" || sp.Name == "KADORTO" {
			cs.addMessage("SPELL DISRUPTED")
			break
		}
		if action.TargetAlly >= 0 && action.TargetAlly < len(game.Town.Party.Members) {
			target := game.Town.Party.Members[action.TargetAlly]
			if target != nil && target.IsDead() {
				if sp.Name == "KADORTO" || target.Status == Dead {
					target.Status = OK
					target.HP = 1
					cs.addMessage(fmt.Sprintf("%s LIVES AGAIN!", target.Name))
				} else {
					cs.addMessage(fmt.Sprintf("%s CANNOT BE RAISED HERE.", target.Name))
				}
			} else {
				cs.addMessage("NO EFFECT.")
			}
		}
	}
}

// resolveSpellDamage applies damage from a damage spell.
// From p-code CASTASPE proc 4: damage = roll dice, check monster resistance (offset 143),
// if random(0,100) > resist% then damage applied, else "IS UNAFFECTED!"
func (cs *CombatState) resolveSpellDamage(sp *Spell, action *PartyAction, monsters []data.Monster) {
	applyToGroup := func(group *MonsterGroup) {
		for _, m := range group.Members {
			if m.Status >= 5 {
				continue
			}
			mon := &monsters[m.MonsterID]

			// Pascal HITGROUP (COMBAT4.TEXT lines 459-478):
			// If spell has a DamageType and monster's WEPVSTY3 has that bit set,
			// halve the dice count: HITSX DIV 2 + 1
			diceNum := sp.DiceNum
			if sp.DamageType >= 0 && mon.WepVsType3&(1<<uint(sp.DamageType)) != 0 {
				diceNum = sp.DiceNum/2 + 1
			}

			damage := rollDice(diceNum, sp.DiceSides, sp.DiceBonus)
			if damage < 1 {
				damage = 1
			}

			// Pascal DOHITS lines 198-201: UNAFFCT check sets damage to 0 if resisted
			// Uses runtime Unaffect on CombatMonster (mutable by HAMMAGIC)
			if m.Unaffect > 0 && rand.Intn(100) < m.Unaffect {
				damage = 0
			}

			if damage == 0 {
				cs.addMessage(fmt.Sprintf("%s IS UNAFFECTED!", displayMonsterName(mon, m)))
			} else {
				m.HP -= damage
				if m.HP <= 0 {
					m.HP = 0
					m.Status = 5
					cs.addMessage(fmt.Sprintf("%s TAKES %d DAMAGE", displayMonsterName(mon, m), damage))
					cs.addMessage(fmt.Sprintf("%s DIES!", displayMonsterName(mon, m)))
				} else {
					cs.addMessage(fmt.Sprintf("%s TAKES %d DAMAGE", displayMonsterName(mon, m), damage))
				}
			}
		}
	}

	switch sp.Target {
	case TargetSingleMonster:
		// Hit one monster in the group (first alive)
		if action.TargetGroup >= 0 && action.TargetGroup < len(cs.Groups) {
			group := cs.Groups[action.TargetGroup]
			for _, m := range group.Members {
				if m.Status < 5 {
					mon := &monsters[m.MonsterID]
					damage := rollDice(sp.DiceNum, sp.DiceSides, sp.DiceBonus)
					if damage < 1 {
						damage = 1
					}
					// Pascal DOHITS: UNAFFCT check sets damage to 0 if resisted
					// Uses runtime Unaffect on CombatMonster (mutable by HAMMAGIC)
					if m.Unaffect > 0 && rand.Intn(100) < m.Unaffect {
						damage = 0
					}
					if damage == 0 {
						cs.addMessage(fmt.Sprintf("%s IS UNAFFECTED!", displayMonsterName(mon, m)))
					} else {
						m.HP -= damage
						if m.HP <= 0 {
							m.HP = 0
							m.Status = 5
							cs.addMessage(fmt.Sprintf("%s TAKES %d DAMAGE", displayMonsterName(mon, m), damage))
							cs.addMessage(fmt.Sprintf("%s DIES!", displayMonsterName(mon, m)))
						} else {
							cs.addMessage(fmt.Sprintf("%s TAKES %d DAMAGE", displayMonsterName(mon, m), damage))
						}
					}
					break
				}
			}
		}
	case TargetMonsterGroup:
		if action.TargetGroup >= 0 && action.TargetGroup < len(cs.Groups) {
			applyToGroup(cs.Groups[action.TargetGroup])
		}
	case TargetAllMonsters:
		for _, g := range cs.Groups {
			applyToGroup(g)
		}
	}
}

// resolveSpellHeal heals a party member.
// From p-code CASTASPE: heal amount = dice roll, "IS FULLY HEALED" / "IS PARTIALLY HEALED"
func (cs *CombatState) resolveSpellHeal(sp *Spell, action *PartyAction, game *GameState) {
	if action.TargetAlly < 0 || action.TargetAlly >= len(game.Town.Party.Members) {
		return
	}
	target := game.Town.Party.Members[action.TargetAlly]
	if target == nil || target.IsDead() {
		cs.addMessage("NO EFFECT.")
		return
	}

	if sp.Name == "MADI" {
		// Pascal DOPRIEST lines 655-663: full HP, status reset, poison clear, then DOHEAL 1d1
		target.HP = target.MaxHP
		if target.Status < Dead {
			target.Status = OK
		}
		target.PoisonAmt = 0
		cs.addMessage(fmt.Sprintf("%s IS FULLY HEALED", target.Name))
		return
	}

	heal := rollDice(sp.DiceNum, sp.DiceSides, sp.DiceBonus)
	if heal < 1 {
		heal = 1
	}
	target.HP += heal
	if target.HP >= target.MaxHP {
		target.HP = target.MaxHP
		cs.addMessage(fmt.Sprintf("%s IS FULLY HEALED", target.Name))
	} else {
		cs.addMessage(fmt.Sprintf("%s IS PARTIALLY HEALED", target.Name))
	}
}

// resolveSpellStatus inflicts a status on monsters.
// Per-spell resistance formulas from Pascal COMBAT4.TEXT:
//   KATINO (sleep): resist = 20 * monLevel, requires SPPC[4]
//   MANIFO (hold):  resist = 50 + 5 * monLevel (WC014 fix)
//   MONTINO (silence): resist = 10 * monLevel
func (cs *CombatState) resolveSpellStatus(sp *Spell, action *PartyAction, monsters []data.Monster) {
	if action.TargetGroup < 0 || action.TargetGroup >= len(cs.Groups) {
		return
	}
	group := cs.Groups[action.TargetGroup]

	var statusName string
	var statusVal int
	switch sp.StatusEffect {
	case Asleep:
		statusName = "SLEPT"
		statusVal = 1
	case Paralyzed:
		statusName = "HELD"
		statusVal = 2
	case 4: // Silenced
		statusName = "SILENCED"
		statusVal = 4
	default:
		statusName = "AFFECTED"
		statusVal = 1
	}

	mon := &monsters[group.MonsterID]

	for _, m := range group.Members {
		if m.Status >= 5 {
			continue
		}

		// KATINO requires SPPC[4] — monster must be susceptible to sleep
		if sp.Name == "KATINO" && (mon.SPPC&(1<<4)) == 0 {
			continue // immune, no message
		}

		// Per-spell resistance from Pascal COMBAT4.TEXT
		// Monster "level" = HPREC.LEVEL = HP dice count
		monLevel := mon.HP.Num
		var resistChance int
		switch sp.Name {
		case "KATINO":
			// Pascal DOSLEPT line 290: 20 * monster level
			resistChance = 20 * monLevel
		case "MANIFO":
			// Pascal DOHOLD line 224 (WC014): 50 + 5 * monster level
			resistChance = 50 + 5*monLevel
		case "MONTINO":
			// Pascal DOSILENC line 248: 10 * monster level
			resistChance = 10 * monLevel
		default:
			resistChance = 10 * monLevel
		}

		if rand.Intn(100) < resistChance {
			cs.addMessage(fmt.Sprintf("%s IS NOT %s", displayMonsterName(mon, m), statusName))
		} else {
			m.Status = statusVal
			// MONTINO: set silence duration = rand%4 + 2 (2-5 rounds)
			// Pascal COMBAT4.TEXT line 107-108
			if sp.Name == "MONTINO" {
				m.InaudCnt = rand.Intn(4) + 2
			}
			cs.addMessage(fmt.Sprintf("%s IS %s", displayMonsterName(mon, m), statusName))
		}
	}
}

// resolveSpellDeath handles instant-kill spells.
// Each spell has unique mechanics — dispatched by name per Pascal source.
func (cs *CombatState) resolveSpellDeath(sp *Spell, action *PartyAction, monsters []data.Monster) {
	switch sp.Name {
	case "MAKANITO":
		// Pascal SMAKANIT (COMBAT4.TEXT lines 515-554):
		// Iterates all groups. Per group: CLASS=10 → unaffected; level>7 → survive; else perish.
		for _, g := range cs.Groups {
			if g.AliveCount() == 0 {
				continue
			}
			mon := &monsters[g.MonsterID]
			name := mon.NamePlural
			if g.Identified {
				name = mon.NamePlural
			} else {
				name = mon.NameUnknownPlural
			}
			if mon.Class == 10 {
				cs.addMessage(fmt.Sprintf("%s ARE UNAFFECTED!", name))
			} else if mon.HP.Num > 7 { // HPREC.LEVEL > 7
				cs.addMessage(fmt.Sprintf("%s SURVIVE!", name))
			} else {
				cs.addMessage(fmt.Sprintf("%s PERISH!", name))
				for _, m := range g.Members {
					if m.Status < 5 {
						m.HP = 0
						m.Status = 5
					}
				}
			}
		}

	case "BADI":
		// Pascal DOSLAIN (COMBAT4.TEXT lines 262-274): single monster
		// ISISNOT with resist = 10 * monLevel, DAMTYPE=2 (kill), "SLAIN"
		if action.TargetGroup >= 0 && action.TargetGroup < len(cs.Groups) {
			group := cs.Groups[action.TargetGroup]
			for _, m := range group.Members {
				if m.Status >= 5 {
					continue
				}
				mon := &monsters[m.MonsterID]
				resistChance := 10 * mon.HP.Num // 10 * monster level
				if rand.Intn(100) < resistChance {
					cs.addMessage(fmt.Sprintf("%s IS NOT SLAIN", displayMonsterName(mon, m)))
				} else {
					m.HP = 0
					m.Status = 5
					cs.addMessage(fmt.Sprintf("%s IS SLAIN", displayMonsterName(mon, m)))
				}
				break // single target (CASTI)
			}
		}

	default:
		// Generic death spell — Unaffect resistance (runtime, mutable by HAMMAGIC)
		applyToGroup := func(group *MonsterGroup) {
			for _, m := range group.Members {
				if m.Status >= 5 {
					continue
				}
				mon := &monsters[m.MonsterID]
				if m.Unaffect > 0 && rand.Intn(100) < m.Unaffect {
					cs.addMessage(fmt.Sprintf("%s IS UNAFFECTED!", displayMonsterName(mon, m)))
					continue
				}
				m.HP = 0
				m.Status = 5
				cs.addMessage(fmt.Sprintf("%s DIES!", displayMonsterName(mon, m)))
			}
		}
		switch sp.Target {
		case TargetMonsterGroup:
			if action.TargetGroup >= 0 && action.TargetGroup < len(cs.Groups) {
				applyToGroup(cs.Groups[action.TargetGroup])
			}
		case TargetAllMonsters:
			for _, g := range cs.Groups {
				applyToGroup(g)
			}
		}
	}
}

// executeDispel resolves a priest's Turn Undead action.
func (cs *CombatState) executeDispel(member *Character, targetGroup int, monsters []data.Monster) {
	if targetGroup < 0 || targetGroup >= len(cs.Groups) {
		return
	}
	group := cs.Groups[targetGroup]

	cs.addMessage(fmt.Sprintf("%s CALLS UPON THE GODS!", member.Name))

	for _, m := range group.Members {
		if m.Status >= 5 {
			continue
		}
		mon := &monsters[m.MonsterID]
		// Only works on undead (class 10-12)
		if mon.Class >= 10 && mon.Class <= 12 {
			// Success chance based on priest level vs monster level
			chance := 50 + (member.Level-mon.AC)*5
			if rand.Intn(100) < chance {
				m.HP = 0
				m.Status = 5
				cs.addMessage(fmt.Sprintf("%s IS DISPELLED!", displayMonsterName(mon, m)))
			} else {
				cs.addMessage(fmt.Sprintf("%s RESISTS!", displayMonsterName(mon, m)))
			}
		} else {
			cs.addMessage(fmt.Sprintf("%s IS UNAFFECTED!", displayMonsterName(mon, m)))
		}
	}
}


// applySpecialAttack handles monster special attack abilities.
// From Pascal source COMBAT5.TEXT lines 233-242 (CASEDAMG procedure):
//   SPPC[0] = stone, SPPC[1] = poison, SPPC[2] = paralyze
//   Plus DRAINAMT for level drain.
// The RESULT procedure (lines 189-230) applies the effect with a random check.
func (cs *CombatState) applySpecialAttack(special int, target *Character, mon *data.Monster, cm *CombatMonster) {
	// Pascal COMBAT5.TEXT RESULT (lines 192-248):
	// Each SPPC bit triggers a special effect with a luck save:
	//   if RANDOM MOD 20 > LUCKSKIL[stoneFlag] then exit (save succeeded)
	// LUCKSKIL indices map to character attributes for save purposes.
	sppc := mon.SPPC

	// SPPC bit 0 = Stone (Pascal: SPPC[0], RESULT type 0 = PLYZE via ISISNOT)
	// Luck save uses LUCKSKIL[0] — mapped to Luck attribute
	if sppc&(1<<0) != 0 && target.IsAlive() {
		if rand.Intn(20) <= target.Luck {
			// Save succeeded — no effect
		} else {
			target.Status = Stoned
			cs.addMessage(fmt.Sprintf("%s IS STONED", target.Name))
		}
	}

	// SPPC bit 1 = Poison (Pascal: SPPC[1])
	// Luck save uses LUCKSKIL[1]
	if sppc&(1<<1) != 0 && target.IsAlive() {
		if rand.Intn(20) <= target.Luck {
			// Save succeeded
		} else {
			target.PoisonAmt = 1
			cs.addMessage(fmt.Sprintf("%s IS POISONED", target.Name))
		}
	}

	// SPPC bit 2 = Paralyze (Pascal: SPPC[2], RESULT type 0 = PLYZE)
	// Luck save uses LUCKSKIL[2]
	if sppc&(1<<2) != 0 && target.IsAlive() {
		if rand.Intn(20) <= target.Luck {
			// Save succeeded
		} else {
			target.Status = Paralyzed
			cs.addMessage(fmt.Sprintf("%s IS PARALYZED", target.Name))
		}
	}

	// SPPC bit 3 = Critical Hit (Pascal: SPPC[3], RESULT type 3 = STATUS := DEAD)
	// Luck save uses LUCKSKIL[3] — mapped to Agility
	if sppc&(1<<3) != 0 && target.IsAlive() {
		if rand.Intn(20) <= target.Agility {
			// Save succeeded
		} else {
			target.HP = 0
			target.Status = Dead
			cs.addMessage(fmt.Sprintf("%s IS CRITICALLY HIT", target.Name))
		}
	}

	// Drain handled separately in the monster attack loop (mon.Drain > 0)
}

// pickPartyTarget selects a random living party member with front-rank bias.
func pickPartyTarget(party []*Character) *Character {
	// Monsters melee the front row (party indices 0-2) ONLY.
	// Back row (indices 3-5) can only be hit by spells/breath, not melee.
	// If all front row is dead, monsters can reach back row.
	frontAlive := make([]*Character, 0, 3)
	for i := 0; i < 3 && i < len(party); i++ {
		if party[i] != nil && party[i].IsAlive() {
			frontAlive = append(frontAlive, party[i])
		}
	}
	if len(frontAlive) > 0 {
		return frontAlive[rand.Intn(len(frontAlive))]
	}
	// Front row all dead — back row exposed
	backAlive := make([]*Character, 0, 3)
	for i := 3; i < len(party); i++ {
		if party[i] != nil && party[i].IsAlive() {
			backAlive = append(backAlive, party[i])
		}
	}
	if len(backAlive) > 0 {
		return backAlive[rand.Intn(len(backAlive))]
	}
	return nil
}

// pickPartyTargetIndex returns the index of a random front-row alive party member.
// Used for pre-assigning VICTIM during ENATTACK.
func pickPartyTargetIndex(party []*Character) int {
	frontAlive := make([]int, 0, 3)
	for i := 0; i < 3 && i < len(party); i++ {
		if party[i] != nil && party[i].IsAlive() {
			frontAlive = append(frontAlive, i)
		}
	}
	if len(frontAlive) > 0 {
		return frontAlive[rand.Intn(len(frontAlive))]
	}
	backAlive := make([]int, 0, 3)
	for i := 3; i < len(party); i++ {
		if party[i] != nil && party[i].IsAlive() {
			backAlive = append(backAlive, i)
		}
	}
	if len(backAlive) > 0 {
		return backAlive[rand.Intn(len(backAlive))]
	}
	return -1
}

// pickRandomAlive returns a random alive party member.
func pickRandomAlive(party []*Character) *Character {
	alive := make([]*Character, 0, 6)
	for _, m := range party {
		if m != nil && m.IsAlive() {
			alive = append(alive, m)
		}
	}
	if len(alive) == 0 {
		return nil
	}
	return alive[rand.Intn(len(alive))]
}

// calcMonsterXP computes XP for a single monster from its stats.
// From Pascal REWARDS2.TEXT CALCKILL procedure (lines 69-100).
// The EXPAMT field in TENEMY is always 0; XP is calculated, not stored.
//
// CRITICAL: Pascal MLTADDKX uses DOUBLING, not linear multiplication:
//   MLTADDKX(n, amount) = amount * 2^(n-1)  (for n > 0)
// This was verified against Apple II emulator: Murphy's Ghost = 4450 XP.
func calcMonsterXP(mon *data.Monster) int {
	xp := 0

	// Pascal MLTADDKX: doubles amount (n-1) times, then adds to total.
	// Result: amount * 2^(n-1). If n=0, no contribution.
	mltadd := func(n, amount int) {
		if n <= 0 {
			return
		}
		val := amount
		for i := 1; i < n; i++ {
			val *= 2
		}
		xp += val
	}

	// Base: HP dice level * HP dice factor * (20 or 40 based on breath)
	base := mon.HP.Num * mon.HP.Sides
	if mon.Breathe == 0 {
		base *= 20
	} else {
		base *= 40
	}
	xp += base

	// Mage spells: MLTADDKX(MAGSPELS, 35) — raw integer value, not bit count
	mltadd(int(mon.MageSpells), 35)

	// Priest spells: MLTADDKX(PRISPELS, 35)
	mltadd(int(mon.PriestSpells), 35)

	// Drain: MLTADDKX(DRAINAMT, 200)
	mltadd(mon.Drain, 200)

	// Heal: MLTADDKX(HEALPTS, 90)
	mltadd(mon.Heal, 90)

	// AC factor: 40 * (11 - AC)
	xp += 40 * (11 - mon.AC)

	// Extra attack types: MLTADDKX(RECSN, 30) if RECSN > 1
	if mon.NumAttackTypes > 1 {
		mltadd(mon.NumAttackTypes, 30)
	}

	// Unaffect: MLTADDKX((UNAFFCT DIV 10) + 1, 40) if UNAFFCT > 0
	if mon.Unaffect > 0 {
		mltadd(int(mon.Unaffect)/10+1, 40)
	}

	// WepVsType3: count bits 1-6 only (Pascal: FOR WEPSTY3I := 1 TO 6)
	wepRes := 0
	for b := uint(1); b <= 6; b++ {
		if mon.WepVsType3&(1<<b) != 0 {
			wepRes++
		}
	}
	mltadd(wepRes, 35)

	// SPPC: count bits 0-6 (Pascal: FOR SPPCI := 0 TO 6)
	sppcCount := 0
	for b := uint(0); b <= 6; b++ {
		if mon.SPPC&(1<<b) != 0 {
			sppcCount++
		}
	}
	mltadd(sppcCount, 40)

	if xp < 1 {
		xp = 1
	}
	return xp
}

// calculateRewards computes XP and gold for defeating all monsters.
// From Pascal source REWARDS2.TEXT (lines 102-112): TOTALEXP procedure.
func (cs *CombatState) calculateRewards(game *GameState) {
	monsters := game.Scenario.Monsters

	// Sum XP from all killed monsters — dead members stay in array (not compacted out)
	totalXP := 0
	for _, group := range cs.Groups {
		if group.MonsterID < 0 || group.MonsterID >= len(monsters) {
			continue
		}
		mon := &monsters[group.MonsterID]
		monXP := calcMonsterXP(mon)
		killedCount := 0
		for _, m := range group.Members {
			if m.Status >= 5 {
				killedCount++
			}
		}
		totalXP += monXP * killedCount
	}

	// Pascal REWARDS proc 3 (IC 4700): XP divided by PARTYCNT (total party),
	// awarded only to STATUS == OK members.
	partyCount := 0
	for _, m := range game.Town.Party.Members {
		if m != nil {
			partyCount++
		}
	}
	if partyCount > 0 {
		xpShare := totalXP / partyCount
		cs.TotalXP = xpShare
		for _, m := range game.Town.Party.Members {
			if m != nil && m.IsAlive() {
				m.XP += xpShare
			}
		}
	}

	// Pascal CHSTGOLD: ENMYREWD selects reward based on encounter type,
	// then gold and items come from that ONE reward record.
	// Select reward index using same ENMYREWD logic as generateTrap.
	rewardIdx := -1
	if len(cs.Groups) > 0 {
		primaryMon := &monsters[cs.Groups[0].MonsterID]
		switch cs.EncounterType {
		case 2:
			rewardIdx = primaryMon.Reward2
		default:
			rewardIdx = primaryMon.Reward1
		}
	}

	// Roll for gold — Pascal CALCULAT two-stage calculation
	totalGold := 0
	if rewardIdx >= 0 && rewardIdx < len(game.Scenario.Rewards) {
		reward := &game.Scenario.Rewards[rewardIdx]
		gold := rollDice(reward.Header.GoldDice, reward.Header.GoldSides, reward.Header.GoldBonus)
		if reward.Header.GoldMult > 0 {
			gold *= reward.Header.GoldMult
		}
		mult2 := rollDice(reward.Header.GoldRange, reward.Header.GoldMin, reward.Header.GoldExtra)
		if mult2 > 0 {
			gold *= mult2
		}
		totalGold = gold
	}

	cs.TotalGold = totalGold

	// Distribute gold equally among survivors
	if partyCount > 0 {
		goldShare := totalGold / partyCount
		cs.TotalGold = goldShare // display per-survivor share
		for _, m := range game.Town.Party.Members {
			if m != nil && m.IsAlive() {
				m.Gold += goldShare
			}
		}
	}

	// Roll for item drops — Pascal GETREWRD: from the ENMYREWD-selected reward only
	cs.ItemsWon = nil
	if rewardIdx >= 0 && rewardIdx < len(game.Scenario.Rewards) {
		reward := &game.Scenario.Rewards[rewardIdx]
		for _, slot := range reward.Slots {
			if slot.Chance <= 0 || slot.ItemCount <= 0 {
				continue
			}
			if rand.Intn(100) < slot.Chance {
				itemIdx := slot.ItemStart + rand.Intn(slot.ItemCount)
				if itemIdx >= 0 && itemIdx < len(game.Scenario.Items) {
					cs.ItemsWon = append(cs.ItemsWon, itemIdx)
				}
			}
		}
	}

	// Give won items to random alive party members
	for _, itemIdx := range cs.ItemsWon {
		target := pickRandomAlive(game.Town.Party.Members)
		if target != nil && target.ItemCount < 8 {
			target.Items[target.ItemCount] = Possession{
				ItemIndex: itemIdx,
				Equipped:  false,
			}
			target.ItemCount++
		}
	}

	// Note: level ups happen at the Inn only (CHNEWLEV in CASTLE2.TEXT), not after combat.
	// XP accumulates here but level doesn't change until resting.

	// Build victory messages
	cs.addMessage(fmt.Sprintf("EACH SURVIVOR GETS %d EXPERIENCE POINTS", cs.TotalXP))
	if cs.TotalGold > 0 {
		cs.addMessage(fmt.Sprintf("%d GOLD PIECES FOUND", cs.TotalGold))
	}
	for _, itemIdx := range cs.ItemsWon {
		if itemIdx >= 0 && itemIdx < len(game.Scenario.Items) {
			cs.addMessage(fmt.Sprintf("FOUND: %s", game.Scenario.Items[itemIdx].Name))
		}
	}
}

// XPForNextLevel returns the XP threshold for the character's next level, or 0 if unknown.
func XPForNextLevel(c *Character, game *GameState) int {
	if game.Scenario.ExpTable == nil {
		return 0
	}
	className := strings.ToLower(c.Class.String())
	classXP, ok := game.Scenario.ExpTable[className]
	if !ok {
		return 0
	}
	needed, ok := classXP.Levels[c.Level+1]
	if !ok {
		return 0
	}
	return needed
}

// CheckLevelUp checks if a character has enough XP to level up, and if so,
// levels them up and runs TRYLEARN for spell learning.
// HP recalculation is NOT done here — it must happen AFTER GAINLOST (InnStatChanges)
// because GAINLOST can change VIT, which affects the HP rolls.
// Pascal MADELEV order: level++, MAXLEVAC, SETSPELS, TRYLEARN, GAINLOST, HP calc.
// Returns true if new spells were learned (for "YOU LEARNED NEW SPELLS!!!!" message).
func CheckLevelUp(c *Character, game *GameState) bool {
	if game.Scenario.ExpTable == nil {
		return false
	}
	// XP table uses lowercase keys ("mage"), Class.String() returns uppercase ("MAGE")
	className := strings.ToLower(c.Class.String())
	classXP, ok := game.Scenario.ExpTable[className]
	if !ok {
		return false
	}
	nextLevel := c.Level + 1
	needed, ok := classXP.Levels[nextLevel]
	if !ok {
		return false
	}
	if c.XP >= needed {
		c.Level = nextLevel
		if c.Level > c.MaxLevAC {
			c.MaxLevAC = c.Level
		}

		// TRYLEARN — learn new spells and recalculate slots
		// Pascal CASTLE2.TEXT lines 209-295
		return TryLearn(c)
	}
	return false
}

// RecalcHP re-rolls MaxHP from scratch for the character's current level.
// Pascal MADELEV (CASTLE2.TEXT lines 375-382): NEWHPMAX = sum of Level rolls
// of (1dClassDie + vitMod), each min 1. Samurai gets one extra MOREHP roll.
// If newMax <= oldMax, set newMax = oldMax + 1. HPLEFT is NOT changed.
// Must be called AFTER GAINLOST (InnStatChanges) since VIT may have changed.
func RecalcHP(c *Character) {
	die := classHPDie(c.Class)
	vmod := vitMod(c.Vitality)
	oldMax := c.MaxHP
	newMax := 0
	rolls := c.Level
	if c.Class == Samurai {
		rolls++ // Samurai bonus roll
	}
	for i := 0; i < rolls; i++ {
		hp := rollDice(1, die, 0) + vmod
		if hp < 1 {
			hp = 1
		}
		newMax += hp
	}
	if newMax <= oldMax {
		newMax = oldMax + 1
	}
	c.MaxHP = newMax
}

// SetSpells recalculates max spell slots based on class and level.
// From Pascal SETSPELS (CASTLE2.TEXT lines 52-161).
// First MINSPCNT sets each group's slots to count of known spells,
// then SPLPERLV raises them with: spellcnt = level - levelmod, for each group 1-7:
//   slots = max(current, spellcnt), spellcnt -= levmod2. Cap at 9.
func SetSpells(c *Character) {
	// MINSPCNT — set spell slots to count of known spells per group
	// Pascal CASTLE2.TEXT lines 100-130 (MINMAG/MINPRI)
	minSpCnt := func(slots *[7]int, spellsByLevel [7][]*Spell) {
		for i := 0; i < 7; i++ {
			count := 0
			for _, sp := range spellsByLevel[i] {
				if c.SpellKnown[SpellIndex[sp.Name]] {
					count++
				}
			}
			slots[i] = count
		}
	}

	// Pascal: MINPRI then MINMAG
	minSpCnt(&c.MaxPriestSpells, PriestSpellsByLevel)
	minSpCnt(&c.MaxMageSpells, MageSpellsByLevel)

	// SPLPERLV — raise slots based on level formula
	splPerLv := func(slots *[7]int, levelMod, levMod2 int) {
		spellCnt := c.Level - levelMod
		if spellCnt <= 0 {
			return
		}
		for i := 0; i < 7 && spellCnt > 0; i++ {
			if spellCnt > slots[i] {
				slots[i] = spellCnt
			}
			spellCnt -= levMod2
		}
		for i := 0; i < 7; i++ {
			if slots[i] > 9 {
				slots[i] = 9
			}
		}
	}

	switch c.Class {
	case Priest:
		splPerLv(&c.MaxPriestSpells, 0, 2)
	case Mage:
		splPerLv(&c.MaxMageSpells, 0, 2)
	case Bishop:
		splPerLv(&c.MaxPriestSpells, 3, 4)
		splPerLv(&c.MaxMageSpells, 0, 4)
	case Lord:
		splPerLv(&c.MaxPriestSpells, 3, 2)
	case Samurai:
		splPerLv(&c.MaxMageSpells, 3, 3)
	}
}

// classHPDie returns the HP die for level-up rolls (MOREHP).
// NOTE: This is DIFFERENT from ClassHPDie used for starting HP.
// Pascal MOREHP groups Samurai with Priest at d8, but starting HP
// (KEEPCHYN) gives Samurai 16. Two separate tables in the original.
func classHPDie(c Class) int {
	switch c {
	case Fighter, Lord:
		return 10
	case Priest, Samurai:
		return 8
	case Thief, Bishop, Ninja:
		return 6
	case Mage:
		return 4
	}
	return 6
}

// generateTrap selects the reward record (Pascal ENMYREWD) and rolls the trap type.
//
// Pascal ENMYREWD (REWARDS.TEXT lines 57-83):
//   ATTK012=0 (no surprise):            REWARDI := REWARD1
//   ATTK012=1 (party surprised monsters): REWARDI := REWARD1, ONEORTWO := 2
//   ATTK012=2 (monsters surprised party): REWARDI := REWARD2
//
// Pascal CHSTGOLD (lines 806-823):
//   ENMYREWD; RDREWARD;
//   IF REWARDZ.BCHEST AND (CHSTALRM <> 1) THEN ACHEST;
//
// Pascal GTTRAPTY (lines 176-215):
// 1. Check BTRAPTYP — if no bits set, trapless
// 2. Trapless escape: random()%15 > (4 + MAZELEV)
// 3. Weighted random walk: type 3 has weight 5, all others weight 1
//    Skip types not enabled in bitmask; wrap from 7 back to 1
func (cs *CombatState) generateTrap(game *GameState) {
	level := game.MazeLevel // 0-based

	// Pascal ENMYREWD: select reward based on encounter type (ATTK012)
	// ATTK012=0 (random): Reward1, ATTK012=1 (cleared fight-zone): Reward1,
	// ATTK012=2 (alarm/fixed/first fight-zone): Reward2
	rewardIdx := -1
	monsters := game.Scenario.Monsters
	if len(cs.Groups) > 0 {
		mon := &monsters[cs.Groups[0].MonsterID]
		switch cs.EncounterType {
		case 2: // alarm, fixed encounter, or first fight-zone encounter → Reward2
			rewardIdx = mon.Reward2
		case 1: // fight-zone already cleared → Reward1 (ONEORTWO=2)
			rewardIdx = mon.Reward1
		default: // random encounter → Reward1
			rewardIdx = mon.Reward1
		}
	}

	// Load reward and check BCHEST
	bitmask := 0
	if rewardIdx >= 0 && rewardIdx < len(game.Scenario.Rewards) {
		reward := &game.Scenario.Rewards[rewardIdx]
		if reward.Header.Chest && game.ChestAlarm != 1 {
			cs.HasChest = true
		}
		bitmask = reward.Header.TrapBitmask & 0xFF
	}

	// Check if any trap types are enabled
	trapped := false
	for bit := 0; bit < 8; bit++ {
		if bitmask&(1<<bit) != 0 {
			trapped = true
			break
		}
	}

	// Pascal TRAP3TYP: sub-type for category 3 traps (always rolled)
	trap3typ := rand.Intn(5)

	if !trapped {
		cs.TrapType = TrapNone
		return
	}

	// Pascal: random()%15 > (4 + dungeonLevel) → trapless
	if rand.Intn(15) > 4+level {
		cs.TrapType = TrapNone
		return
	}

	// Pascal weighted random walk through BTRAPTYP bitmask:
	// Roll ZERO99 = random()%100, decrement by weight per enabled type.
	// Type 3 costs 5, all others cost 1. Skip disabled types.
	// When ZERO99 <= 0, current type is selected. Wrap 7→1 (not 0).
	zero99 := rand.Intn(100)
	trapType := 0
	for zero99 > 0 {
		if trapType < 7 {
			if trapType == 3 {
				zero99 -= 5
			} else {
				zero99--
			}
			trapType++
		} else {
			trapType = 1 // wrap back to type 1 (skip 0=trapless)
		}
		// Skip disabled types: keep advancing until we hit an enabled one
		for bitmask&(1<<trapType) == 0 {
			if trapType < 7 {
				trapType++
			} else {
				trapType = 1
			}
		}
	}

	// Map Pascal trap type (0-7) to Go trap constants
	// Pascal type 3 has 5 sub-types selected by TRAP3TYP
	switch trapType {
	case 0:
		cs.TrapType = TrapNone
	case 1:
		cs.TrapType = TrapPoison
	case 2:
		cs.TrapType = TrapGas
	case 3:
		cs.TrapType = TrapCrossbow + trap3typ // sub-types 0-4
	case 4:
		cs.TrapType = TrapTeleporter
	case 5:
		cs.TrapType = TrapAntiMage
	case 6:
		cs.TrapType = TrapAntiPriest
	case 7:
		cs.TrapType = TrapAlarm
	}
}

// OpenChest triggers the trap (if any) and proceeds to rewards.
// From p-code REWARDS proc 25 (IC 1130-1264):
//   - Check character status (must be alive and active)
//   - If trap is TRAPLESS: "THE CHEST WAS NOT TRAPPED" → rewards
//   - Otherwise: "OOPPS! A <trap>" → apply trap effect
func (cs *CombatState) OpenChest(game *GameState, memberIdx int) {
	cs.Messages = nil
	cs.MessageIndex = 0

	if memberIdx < 0 || memberIdx >= len(game.Town.Party.Members) {
		return
	}
	member := game.Town.Party.Members[memberIdx]
	if member == nil || !member.IsAlive() {
		return
	}

	cs.ChestActor = memberIdx

	if cs.TrapType == TrapNone {
		cs.addMessage("THE CHEST WAS NOT TRAPPED")
	} else {
		// Open chest dodge check — game.pas REWARDS proc 18:
		//   random(0,1000) % 1000 <= char.CHARLEV → dodge trap
		if rand.Intn(1000) <= member.Level {
			cs.addMessage("THE CHEST WAS NOT TRAPPED")
		} else {
			cs.addMessage(fmt.Sprintf("OOPPS! A %s!", TrapNames[cs.TrapType]))
			cs.applyTrapEffect(game)
		}
	}

	cs.ChestOpened = true
	cs.Phase = CombatChestResult
}

// applyTrapEffect applies the effect of the current trap to the party.
// From p-code REWARDS proc 24 (IC 1420-1692) and the XJP case table at IC 1651.
func (cs *CombatState) applyTrapEffect(game *GameState) {
	party := game.Town.Party.Members

	switch cs.TrapType {
	case TrapPoison:
		// Pascal p-code case 1: poison needle sets poison flag, no direct HP damage
		if cs.ChestActor >= 0 && cs.ChestActor < len(party) {
			target := party[cs.ChestActor]
			if target != nil && target.IsAlive() {
				target.PoisonAmt = 1
				cs.addMessage(fmt.Sprintf("%s IS POISONED!", target.Name))
			}
		}

	case TrapGas:
		// Gas bomb — from p-code case 2 (IC 1539):
		// Damages all party members, chance of poison
		for _, m := range party {
			if m == nil || !m.IsAlive() {
				continue
			}
			// Random check for each member — from p-code: random()%20 <= agility saves
			if rand.Intn(20) <= m.Agility {
				continue // saved
			}
			damage := rollDice(1, 6, 0)
			m.HP -= damage
			cs.addMessage(fmt.Sprintf("%s TAKES %d DAMAGE", m.Name, damage))
			if m.HP <= 0 {
				m.HP = 0
				m.Status = Dead
				cs.addMessage(fmt.Sprintf("%s DIES!", m.Name))
			}
		}

	case TrapCrossbow:
		// Crossbow bolt — from p-code case 3 (IC 1597):
		// Damages the opener
		if cs.ChestActor >= 0 && cs.ChestActor < len(party) {
			target := party[cs.ChestActor]
			if target != nil && target.IsAlive() {
				damage := rollDice(2, 8, 0)
				target.HP -= damage
				cs.addMessage(fmt.Sprintf("%s TAKES %d DAMAGE", target.Name, damage))
				if target.HP <= 0 {
					target.HP = 0
					target.Status = Dead
					cs.addMessage(fmt.Sprintf("%s DIES!", target.Name))
				}
			}
		}

	case TrapExploding:
		// Exploding box — from p-code case 4 (IC 1601):
		// Damages all party members
		for _, m := range party {
			if m == nil || !m.IsAlive() {
				continue
			}
			damage := rollDice(3, 8, 0)
			m.HP -= damage
			cs.addMessage(fmt.Sprintf("%s TAKES %d DAMAGE", m.Name, damage))
			if m.HP <= 0 {
				m.HP = 0
				m.Status = Dead
				cs.addMessage(fmt.Sprintf("%s DIES!", m.Name))
			}
		}

	case TrapSplinters:
		// Splinters — similar to exploding box, from p-code case 5 (IC 1634)
		for _, m := range party {
			if m == nil || !m.IsAlive() {
				continue
			}
			damage := rollDice(2, 6, 0)
			m.HP -= damage
			cs.addMessage(fmt.Sprintf("%s TAKES %d DAMAGE", m.Name, damage))
			if m.HP <= 0 {
				m.HP = 0
				m.Status = Dead
				cs.addMessage(fmt.Sprintf("%s DIES!", m.Name))
			}
		}

	case TrapBlades:
		// Blades — from p-code case 6 (IC 1639):
		// Similar to splinters with higher damage
		for _, m := range party {
			if m == nil || !m.IsAlive() {
				continue
			}
			damage := rollDice(3, 6, 0)
			m.HP -= damage
			cs.addMessage(fmt.Sprintf("%s TAKES %d DAMAGE", m.Name, damage))
			if m.HP <= 0 {
				m.HP = 0
				m.Status = Dead
				cs.addMessage(fmt.Sprintf("%s DIES!", m.Name))
			}
		}

	case TrapStunner:
		// Stunner — sub-type 4 of Pascal category 3
		// Stuns (paralyzes) the opener
		if cs.ChestActor >= 0 && cs.ChestActor < len(party) {
			target := party[cs.ChestActor]
			if target != nil && target.IsAlive() {
				target.Status = Paralyzed
				cs.addMessage(fmt.Sprintf("%s IS STUNNED!", target.Name))
			}
		}

	case TrapTeleporter:
		// Pascal type 4: MALOR-style random teleport
		game.PlayerX = rand.Intn(20)
		game.PlayerY = rand.Intn(20)
		cs.addMessage("TELEPORTED!")

	case TrapAntiMage:
		// Pascal type 5: zeroes all mage spell slots for opener
		if cs.ChestActor >= 0 && cs.ChestActor < len(party) {
			target := party[cs.ChestActor]
			if target != nil {
				for i := range target.MageSpells {
					target.MageSpells[i] = 0
				}
				cs.addMessage(fmt.Sprintf("%s'S MAGE SPELLS DRAINED!", target.Name))
			}
		}

	case TrapAntiPriest:
		// Pascal type 6: zeroes all priest spell slots for opener
		if cs.ChestActor >= 0 && cs.ChestActor < len(party) {
			target := party[cs.ChestActor]
			if target != nil {
				for i := range target.PriestSpells {
					target.PriestSpells[i] = 0
				}
				cs.addMessage(fmt.Sprintf("%s'S PRIEST SPELLS DRAINED!", target.Name))
			}
		}

	case TrapAlarm:
		// Pascal type 7 (REWARDS.TEXT lines 428-433): CHSTALRM := 1, exit chest
		cs.addMessage("AN ALARM GOES OFF!")
		game.ChestAlarm = 1
	}
}

// DisarmChest attempts to disarm the trap.
// From p-code REWARDS proc 19 (IC 2258-2440):
//   - Check: random()%70 <= (charLevel - dungeonLevel + 50*(isThief||isNinja))
//   - Success: "YOU DISARMED IT!" → chest opens safely
//   - Fail but lucky: "DISARM FAILED!!" → back to chest menu
//   - Fail badly: "YOU SET IT OFF!" → trap triggers
func (cs *CombatState) DisarmChest(game *GameState, memberIdx int) {
	cs.Messages = nil
	cs.MessageIndex = 0

	if memberIdx < 0 || memberIdx >= len(game.Town.Party.Members) {
		return
	}
	member := game.Town.Party.Members[memberIdx]
	if member == nil || !member.IsAlive() {
		return
	}

	cs.ChestActor = memberIdx

	// Pascal REWARDS proc 19 (IC 2270-2315):
	// disarmChance = charLevel - dungeonLevel + 50*(isThief||isNinja)
	// Success if random()%70 <= disarmChance
	bonus := 0
	if member.Class == Thief || member.Class == Ninja {
		bonus = 50
	}
	disarmChance := member.Level - game.MazeLevel + bonus

	roll := rand.Intn(70)
	if roll <= disarmChance {
		// From p-code IC 2319: "YOU DISARMED IT!"
		cs.addMessage("YOU DISARMED IT!")
		cs.TrapType = TrapNone
		cs.ChestOpened = true
		cs.Phase = CombatChestResult
		return
	}

	// Failed — check if trap triggers or just fails
	// Pascal REWARDS proc 19 (IC 2349-2371):
	//   random()%20 <= LUCKSKIL[4] → safe fail ("DISARM FAILED!!")
	//   LUCKSKIL[4] maps to Agility attribute
	failRoll := rand.Intn(20)
	if failRoll <= member.Agility {
		// From p-code IC 2373: "DISARM FAILED!!"
		cs.addMessage("DISARM FAILED!!")
		cs.Phase = CombatChestResult
		// Does NOT open the chest — player returns to chest menu on next key
		return
	}

	// From p-code IC 2403: "YOU SET IT OFF!"
	cs.addMessage("YOU SET IT OFF!")
	cs.addMessage(fmt.Sprintf("IT WAS A %s!", TrapNames[cs.TrapType]))
	cs.applyTrapEffect(game)
	cs.ChestOpened = true
	cs.Phase = CombatChestResult
}

// InspectChest has a thief/ninja examine the trap.
// From p-code REWARDS proc 21 (IC 1814-2066):
//   - Only thief (class 3) or ninja (class 7) can inspect effectively
//   - Chance to identify = charAgility * (6 for thief, 4 for ninja), capped at 95
//   - On success: shows the trap name
//   - On fail: shows a random trap name (deception)
//   - Each member can only inspect once per chest
func (cs *CombatState) InspectChest(game *GameState, memberIdx int) {
	cs.Messages = nil
	cs.MessageIndex = 0

	if memberIdx < 0 || memberIdx >= len(game.Town.Party.Members) {
		return
	}
	member := game.Town.Party.Members[memberIdx]
	if member == nil || !member.IsAlive() {
		return
	}

	// Check if already inspected — from p-code IC 1898
	if cs.ChestInspected[memberIdx] {
		cs.addMessage("YOU ALREADY LOOKED!")
		cs.Phase = CombatChestResult
		return
	}
	cs.ChestInspected[memberIdx] = true

	// From Pascal INSPCHST (REWARDS.TEXT lines 499-514):
	//   CHNCGOOD = ATTRIB[AGILITY], *6 for Thief, *4 for Ninja, cap 95.
	//   On fail: second check (RANDOM MOD 20) > AGILITY → trap fires, else random wrong name.
	inspectChance := member.Agility
	switch member.Class {
	case Thief:
		inspectChance *= 6
	case Ninja:
		inspectChance *= 4
	}
	if inspectChance > 95 {
		inspectChance = 95
	}

	if rand.Intn(100) < inspectChance {
		// Correct identification
		cs.addMessage(fmt.Sprintf("IT'S A %s", TrapNames[cs.TrapType]))
	} else if rand.Intn(20) > member.Agility {
		// Fumble — trap fires (DOTRAPDM in Pascal)
		cs.addMessage(fmt.Sprintf("OOPPS! A %s!", TrapNames[cs.TrapType]))
		cs.applyTrapEffect(game)
		cs.ChestOpened = true
	} else {
		// Wrong trap name shown (PRRNDTRP in Pascal)
		fakeTrap := rand.Intn(len(TrapNames))
		cs.addMessage(fmt.Sprintf("IT'S A %s", TrapNames[fakeTrap]))
	}
	cs.Phase = CombatChestResult
}

// CalfoChest casts CALFO to identify the trap.
// From p-code REWARDS proc 20 (IC 2068-2256):
//   - Requires a priest spell slot level 2
//   - Must know CALFO spell (bit 28 in spell known bitmask)
//   - 95% chance of correct identification, 5% chance of random result
func (cs *CombatState) CalfoChest(game *GameState, memberIdx int) {
	cs.Messages = nil
	cs.MessageIndex = 0

	if memberIdx < 0 || memberIdx >= len(game.Town.Party.Members) {
		return
	}
	member := game.Town.Party.Members[memberIdx]
	if member == nil || !member.IsAlive() {
		return
	}

	// Check if member knows CALFO and has spell slots
	sp := LookupSpell("CALFO")
	if sp == nil || !member.CanCastSpell(sp) {
		cs.addMessage("CAN'T CAST CALFO!")
		cs.Phase = CombatChestResult
		return
	}

	// Use the spell slot
	member.UseSpellSlot(sp)

	// Pascal p-code proc 20 (IC 2228-2243): 95% correct, 5% → re-enter CALFO selection
	if rand.Intn(100) < 95 {
		if cs.TrapType >= 0 && cs.TrapType < len(TrapNames) {
			cs.addMessage(fmt.Sprintf("IT'S A %s", TrapNames[cs.TrapType]))
		} else {
			cs.addMessage("TRAPLESS CHEST")
		}
		cs.CalfoUsed = true
		cs.Phase = CombatChestResult
	} else {
		// 5% fail — Pascal re-enters CALFO prompt (CIP 20 = CalfoChest itself)
		// Return to chest menu without showing any trap name
		cs.addMessage("CALFO REVEALS NOTHING...")
		cs.Phase = CombatChestResult
	}
}

// LeaveChest skips the chest and proceeds without rewards.
// From p-code REWARDS proc 10 (IC 4888-4952).
func (cs *CombatState) LeaveChest() {
	cs.Messages = nil
	cs.MessageIndex = 0
	cs.addMessage("YOU LEAVE THE CHEST.")
	cs.ChestLeft = true
	cs.ChestOpened = false
	cs.Phase = CombatChestResult
}

// FinalizeChest distributes rewards after chest interaction completes.
// Called when transitioning from CombatChestResult to CombatVictory.
func (cs *CombatState) FinalizeChest(game *GameState) {
	if cs.ChestOpened {
		cs.calculateRewards(game)
	} else {
		// Left chest or chest not opened — no gold/items
		// Still distribute XP for killing the monsters
		cs.distributeXPOnly(game)
	}
	cs.Phase = CombatVictory
}

// distributeXPOnly gives XP for kills but no gold/items (chest was left).
func (cs *CombatState) distributeXPOnly(game *GameState) {
	monsters := game.Scenario.Monsters

	totalXP := 0
	for _, group := range cs.Groups {
		if group.MonsterID < 0 || group.MonsterID >= len(monsters) {
			continue
		}
		mon := &monsters[group.MonsterID]
		monXP := calcMonsterXP(mon)
		killedCount := 0
		for _, m := range group.Members {
			if m.Status >= 5 {
				killedCount++
			}
		}
		totalXP += monXP * killedCount
	}
	cs.TotalGold = 0

	// Pascal: divide by PARTYCNT, award to alive only
	partyCount := 0
	for _, m := range game.Town.Party.Members {
		if m != nil {
			partyCount++
		}
	}
	if partyCount > 0 {
		xpShare := totalXP / partyCount
		cs.TotalXP = xpShare
		for _, m := range game.Town.Party.Members {
			if m != nil && m.IsAlive() {
				m.XP += xpShare
			}
		}
	}

	cs.Messages = nil
	cs.MessageIndex = 0
	cs.addMessage(fmt.Sprintf("EACH SURVIVOR GETS %d EXPERIENCE POINTS", cs.TotalXP))
}

func (cs *CombatState) addMessage(msg string) {
	cs.Messages = append(cs.Messages, msg)
}

// endAction inserts an action separator so the display pauses between actions.
// Each action block (attack, spell, parry, etc.) should call this after its messages.
const ActionSeparator = "\x00"

func (cs *CombatState) endAction() {
	cs.Messages = append(cs.Messages, ActionSeparator)
}


// HAMAN/MAHAMAN effect names for interactive selection (Wiz 2/3)
var hamanEffectNames = [7]string{
	"CURE THE PARTY",
	"SILENCE THE MONSTERS",
	"MAKE MAGIC MORE EFFECTIVE",
	"TELEPORT THE MONSTERS",
	"HEAL THE PARTY",
	"PROTECT THE PARTY",
	"REANIMATE CORPSES!",
}

// hamCure implements HAMCURE: cure + heal 9d8 per member
func (cs *CombatState) hamCure(game *GameState) {
	for _, m := range game.Town.Party.Members {
		if m == nil || m.IsDead() {
			continue
		}
		m.Status = OK
		for i, pm := range game.Town.Party.Members {
			if pm == m && i < 6 {
				cs.PartyInaudCnt[i] = 0
				break
			}
		}
		heal := rollDice(9, 8, 0)
		m.HP += heal
		if m.HP > m.MaxHP {
			m.HP = m.MaxHP
		}
	}
}

// hamSilen implements HAMSILEN: silence monster groups 1-3
func (cs *CombatState) hamSilen() {
	for gi, g := range cs.Groups {
		if gi >= 3 {
			break
		}
		for _, m := range g.Members {
			m.InaudCnt = 5 + rand.Intn(5)
		}
	}
}

// hamMagic implements HAMMAGIC: zero Unaffect on groups 1-3
func (cs *CombatState) hamMagic() {
	for gi, g := range cs.Groups {
		if gi >= 3 {
			break
		}
		for _, m := range g.Members {
			m.Unaffect = 0
		}
	}
}

// hamTelep implements HAMTELEP: destroy all monsters
func (cs *CombatState) hamTelep() {
	for _, g := range cs.Groups {
		for _, m := range g.Members {
			m.HP = 0
			m.Status = 5
		}
	}
}

// hamHeal implements HAMHEAL: full heal + STATUS=OK + clear INAUDCNT
func (cs *CombatState) hamHeal(game *GameState) {
	for _, m := range game.Town.Party.Members {
		if m == nil || m.IsDead() {
			continue
		}
		m.Status = OK
		m.HP = m.MaxHP
		for i, pm := range game.Town.Party.Members {
			if pm == m && i < 6 {
				cs.PartyInaudCnt[i] = 0
				break
			}
		}
	}
}

// hamProt implements HAMPROT: set permanent AC = -10
func (cs *CombatState) hamProt(game *GameState) {
	for _, m := range game.Town.Party.Members {
		if m != nil && m.AC > -10 {
			m.AC = -10
		}
	}
}

// hamAlive implements HAMALIVE: all non-LOST → OK, then full heal
func (cs *CombatState) hamAlive(game *GameState) {
	for _, m := range game.Town.Party.Members {
		if m == nil {
			continue
		}
		if m.Status != Lost {
			m.Status = OK
		}
		m.HP = m.MaxHP
		for i, pm := range game.Town.Party.Members {
			if pm == m && i < 6 {
				cs.PartyInaudCnt[i] = 0
				break
			}
		}
	}
}

// executeHamanEffect dispatches effect index 0-6 to the appropriate method.
// Prints the effect name message before executing.
func (cs *CombatState) executeHamanEffect(game *GameState, effectIdx int) {
	cs.addMessage(hamanEffectNames[effectIdx])
	switch effectIdx {
	case 0:
		cs.hamCure(game)
	case 1:
		cs.hamSilen()
	case 2:
		cs.hamMagic()
	case 3:
		cs.hamTelep()
	case 4:
		cs.hamHeal(game)
	case 5:
		cs.hamProt(game)
	case 6:
		cs.hamAlive(game)
	}
}

// ExecuteHamanChoice completes a Wiz 2/3 HAMAN/MAHAMAN interactive selection.
// Called when the player presses 1, 2, or 3 during HamanSelecting.
func (cs *CombatState) ExecuteHamanChoice(game *GameState, choice int) {
	if choice < 0 || choice > 2 {
		return
	}
	cs.HamanSelecting = false
	effectIdx := cs.HamanOptions[choice]
	cs.executeHamanEffect(game, effectIdx)

	// HAMMANGL: (RANDOM MOD CHARLEV) == 5 → mangle spells
	member := cs.HamanCaster
	if member != nil && member.Level > 0 && rand.Intn(member.Level) == 5 {
		cs.addMessage("BUT HIS SPELL BOOKS ARE MANGLED!")
		for i := range member.SpellKnown {
			if rand.Intn(100) > 50 {
				member.SpellKnown[i] = false
			}
		}
	}
	cs.HamanCaster = nil
}

// hammaham implements the HAMAN/MAHAMAN sacrifice spells.
// Wiz 1: random effect (COMBAT4.TEXT lines 303-456, WC006 fix)
// Wiz 2/3: interactive selection from 3 random boons (COMBAT4.TEXT lines 374-438)
func (cs *CombatState) hammaham(game *GameState, member *Character, mahamFlg int) {
	prefix := ""
	isMahaman := mahamFlg == 8 || mahamFlg == 7
	if isMahaman {
		prefix = "MA"
	}
	cs.addMessage(fmt.Sprintf("%sHAMAN IS INTONED AND...", prefix))

	if member.Level < 13 {
		cs.addMessage("FAILS!")
		return
	}

	member.Level--
	// Set DRAINED flag for this party member
	for i, m := range game.Town.Party.Members {
		if m == member && i < 6 {
			cs.Drained[i] = true
			break
		}
	}

	if game.Scenario.ScenarioNum >= 2 {
		// Wiz 2/3: interactive boon selection
		// Pick 3 unique random effects from available pool
		poolSize := 5 // HAMAN: effects 0-4
		if isMahaman {
			poolSize = 7 // MAHAMAN: effects 0-6
		}
		var picked [7]bool
		for i := 0; i < 3; i++ {
			idx := rand.Intn(poolSize)
			for picked[idx] {
				idx = (idx + 1) % 7
			}
			picked[idx] = true
			cs.HamanOptions[i] = idx
		}
		cs.HamanSelecting = true
		cs.HamanCaster = member
		cs.addMessage("WHICH BOON WILL YOU INVOKE ?")
		for i := 0; i < 3; i++ {
			cs.addMessage(fmt.Sprintf("%d) %s", i+1, hamanEffectNames[cs.HamanOptions[i]]))
		}
		return // wait for player input
	}

	// Wiz 1: random effect selection
	roll := rand.Intn(3 * mahamFlg)
	switch {
	case roll <= 5:
		cs.addMessage("DIALKO'S PARTY 3 TIMES")
		cs.hamCure(game)
	case roll >= 7 && roll <= 11:
		cs.addMessage("SILENCES MONSTERS!")
		cs.hamSilen()
	case roll == 12 || roll == 13 || roll == 22 || roll == 23:
		cs.addMessage("ZAPS MONSTER MAGIC RESISTANCE!")
		cs.hamMagic()
	case roll == 14 || roll == 20 || roll == 21:
		cs.addMessage("DESTROYS MONSTERS!")
		cs.hamTelep()
	case roll == 6 || roll == 15 || roll == 19:
		cs.addMessage("HEALS PARTY!")
		cs.hamHeal(game)
	case roll == 17:
		cs.addMessage("SHIELDS PARTY")
		cs.hamProt(game)
	case roll == 16 || roll == 18:
		cs.addMessage("RESURRECTS AND HEALS PARTY!")
		cs.hamAlive(game)
	}

	// HAMMANGL (COMBAT4.TEXT lines 412-425): (RANDOM MOD CHARLEV) == 5 → mangle spells
	// Each of SPELLSKN[1..50]: (RANDOM MOD 100) > 50 → lose that spell (49% chance)
	if member.Level > 0 && rand.Intn(member.Level) == 5 {
		cs.addMessage("BUT HIS SPELL BOOKS ARE MANGLED!")
		for i := range member.SpellKnown {
			if rand.Intn(100) > 50 {
				member.SpellKnown[i] = false
			}
		}
	}
}

// monsterSelectSpell implements Pascal ENEMYSPL (COMBAT2.TEXT lines 524-635).
// Returns the spell hash, or 0 if no spell selected.
func monsterSelectSpell(mon *data.Monster) int {
	// 75% chance to try mage spell
	if mon.MageSpells > 0 && rand.Intn(100) < 75 {
		// Find highest mage spell level (bit position in MAGSPELS)
		spellLvl := 0
		for lvl := 7; lvl >= 1; lvl-- {
			if mon.MageSpells&(1<<uint(lvl-1)) != 0 {
				spellLvl = lvl
				break
			}
		}
		// Random down-leveling: 30% chance per level to go lower
		for spellLvl > 1 && rand.Intn(100) > 70 {
			spellLvl--
		}
		// Pascal GETMAGSP spell selection table
		twoThird := rand.Intn(100) > 33
		var spellName string
		switch spellLvl {
		case 1:
			if twoThird { spellName = "KATINO" } else { spellName = "HALITO" }
		case 2:
			if twoThird { spellName = "DILTO" } else { spellName = "HALITO" }
		case 3:
			if twoThird { spellName = "MOLITO" } else { spellName = "MAHALITO" }
		case 4:
			if twoThird { spellName = "DALTO" } else { spellName = "LAHALITO" }
		case 5:
			if twoThird { spellName = "LAHALITO" } else { spellName = "MADALTO" }
		case 6:
			spellName = "MADALTO"
		case 7:
			spellName = "TILTOWAIT"
		}
		if sp, ok := SpellsByName[spellName]; ok {
			return sp.Hash
		}
	}

	// 75% chance to try priest spell
	if mon.PriestSpells > 0 && rand.Intn(100) < 75 {
		spellLvl := 0
		for lvl := 7; lvl >= 1; lvl-- {
			if mon.PriestSpells&(1<<uint(lvl-1)) != 0 {
				spellLvl = lvl
				break
			}
		}
		twoThird := rand.Intn(100) > 33
		var spellName string
		switch spellLvl {
		case 1:
			spellName = "BADIOS"
		case 2:
			spellName = "MONTINO"
		case 3:
			if twoThird { spellName = "BADIOS" } else { spellName = "BADIAL" }
		case 4:
			spellName = "BADIAL"
		case 5:
			if twoThird { spellName = "BADIALMA" } else { spellName = "BADI" }
		case 6:
			if twoThird { spellName = "LORTO" } else { spellName = "MABADI" }
		case 7:
			spellName = "MABADI"
		}
		if sp, ok := SpellsByName[spellName]; ok {
			return sp.Hash
		}
	}

	return 0
}

// rollDice rolls NdS+B.
func rollDice(num, sides, bonus int) int {
	total := bonus
	for i := 0; i < num; i++ {
		if sides > 0 {
			total += 1 + rand.Intn(sides)
		}
	}
	return total
}

// RollDicePublic is the exported version of rollDice for use outside the engine package.
func RollDicePublic(num, sides, bonus int) int {
	return rollDice(num, sides, bonus)
}
