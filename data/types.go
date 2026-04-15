// Package data defines shared data structures for Wizardry 1-3 game data.
// All three games use identical record formats (TENEMY, TOBJREC, TMAZE, etc.)
// verified from p-code disassembly of the original Apple II UCSD Pascal binaries.
package data

// DiceRoll represents NdS+B (num dice, die size, bonus).
type DiceRoll struct {
	Num   int `json:"num_dice"`
	Sides int `json:"die_size"`
	Bonus int `json:"bonus"`
}

// Attack represents one of up to 7 monster attack types.
type Attack struct {
	Num     int `json:"num_dice"`
	Sides   int `json:"die_size"`
	Special int `json:"special"`
}

// Monster represents a TENEMY record (158 bytes on disk).
type Monster struct {
	Index            int      `json:"index"`
	Name             string   `json:"name"`
	NamePlural       string   `json:"name_plural"`
	NameUnknown      string   `json:"name_unknown"`
	NameUnknownPlural string  `json:"name_unknown_plural"`
	Pic              int      `json:"pic"`
	GroupSize        DiceRoll `json:"calc1"`
	HP               DiceRoll `json:"hp"`
	Class            int      `json:"class"`
	AC               int      `json:"ac"`
	NumAttackTypes   int      `json:"num_attack_types"`
	Attacks          []Attack `json:"attacks"`
	XP               int      `json:"xp"`
	Drain            int      `json:"drain"`
	Heal             int      `json:"heal"`
	Reward1          int      `json:"reward1"`
	Reward2          int      `json:"reward2"`
	TeamMonster      int      `json:"team_monster"`
	TeamPercent      int      `json:"team_percent"`
	MageSpells       uint16   `json:"mage_spells"`
	PriestSpells     uint16   `json:"priest_spells"`
	Unique           int      `json:"unique"`
	Breathe          int      `json:"breathe"`
	Unaffect         uint16   `json:"unaffect"`
	WepVsType3       uint16   `json:"wep_vs_type3"`
	SPPC             uint16   `json:"sppc"`
}

// ItemDamage holds weapon damage dice (only for weapons).
type ItemDamage struct {
	Num   int `json:"num_dice"`
	Sides int `json:"die_size"`
	Bonus int `json:"bonus"`
}

// Item represents a TOBJREC record (78 bytes on disk).
type Item struct {
	Index        int         `json:"index"`
	Name         string      `json:"name"`
	NameUnknown  string      `json:"name_unknown"`
	Type         string      `json:"type"`
	TypeID       int         `json:"type_id"`
	Alignment    int         `json:"alignment"`
	Cursed       bool        `json:"cursed"`
	Special      int         `json:"special"`
	ChangeTo     int         `json:"change_to"`
	ChangeChance int         `json:"change_chance"`
	Price        int         `json:"price"`
	Stock        int         `json:"stock"`
	SpellPower   int         `json:"spell_power"`
	ClassUse     uint16      `json:"class_use"`
	UsableBy     []string    `json:"usable_by"`
	HealPts      int         `json:"heal_pts"`
	WepVsType2   uint16      `json:"wep_vs_type2"`
	WepVsType3   uint16      `json:"wep_vs_type3"`
	ACMod        int         `json:"ac_mod"`
	HitMod       int         `json:"hit_mod"`
	Damage       *ItemDamage `json:"damage"`
	ExtraSwings  int         `json:"extra_swings"`
	CritHit      bool        `json:"crit_hit"`
	WepVsType    uint16      `json:"wep_vs_type"`
}

// RewardHeader is the 12-word header of a ZREWARD record.
type RewardHeader struct {
	Chest       bool `json:"chest"`
	TrapBitmask int  `json:"trap_level"` // Pascal BTRAPTYP: PACKED ARRAY[0..7] OF BOOLEAN (low byte of word[1])
	Tier      int  `json:"tier"`
	BaseProb  int  `json:"base_prob"`
	Special   bool `json:"special"`
	GoldDice  int  `json:"gold_dice"`
	GoldSides int  `json:"gold_sides"`
	GoldBonus int  `json:"gold_bonus"`
	GoldRange int  `json:"gold_range"`
	GoldMin   int  `json:"gold_min"`
	GoldMult  int  `json:"gold_mult"`
	GoldExtra int  `json:"gold_extra"`
}

// RewardSlot is one of up to 8 item/gold slots in a reward record.
type RewardSlot struct {
	Chance     int    `json:"chance"`
	Type       string `json:"type"`
	ItemStart  int    `json:"item_start,omitempty"`
	ItemCount  int    `json:"item_count,omitempty"`
	BonusStart int    `json:"bonus_start,omitempty"`
	BonusParam int    `json:"bonus_param,omitempty"`
	BonusCount int    `json:"bonus_count,omitempty"`
	GoldAmount int    `json:"gold_amount,omitempty"`
	Params     []int  `json:"params,omitempty"`
}

// Reward represents a ZREWARD record (168 bytes on disk).
type Reward struct {
	Index  int          `json:"index"`
	Header RewardHeader `json:"header"`
	Slots  []RewardSlot `json:"slots"`
}

// ClassXP holds XP thresholds for one character class.
type ClassXP struct {
	Cap    int         `json:"cap"`
	Levels map[int]int `json:"levels"` // level number -> cumulative XP
}

// WallType represents a wall state on a maze cell edge.
type WallType string

const (
	WallOpen   WallType = "open"
	WallWall   WallType = "wall"
	WallDoor   WallType = "door"
	WallHidden WallType = "hidden"
)

// SquareType represents a special square type in the maze.
type SquareType string

const (
	SqNormal     SquareType = ""
	SqStairs     SquareType = "stairs"
	SqEncounter  SquareType = "encounter"
	SqChute      SquareType = "chute"
	SqPit        SquareType = "pit"
	SqDark       SquareType = "dark"
	SqTransfer   SquareType = "transfer"
	SqOuchy      SquareType = "ouchy"
	SqButtons    SquareType = "buttons"
	SqScnMsg     SquareType = "scnmsg"
	SqFizzle     SquareType = "fizzle"
	SqSpclEnctr  SquareType = "spclenctr"
	SqEncounter2 SquareType = "encounter2"
	SqRockwater  SquareType = "rockwater"
	SqSpinner    SquareType = "spinner"
)

// MazeCell represents one 1x1 cell of a maze level.
type MazeCell struct {
	N         WallType   `json:"n"`
	S         WallType   `json:"s"`
	E         WallType   `json:"e"`
	W         WallType   `json:"w"`
	Type      SquareType `json:"type,omitempty"`
	Encounter bool       `json:"encounter,omitempty"`
	// Stairs/Transfer/Chute/Pit destinations
	DestLevel int `json:"dest_level,omitempty"`
	DestY     int `json:"dest_y,omitempty"`
	DestX     int `json:"dest_x,omitempty"`
	// Ouchy damage
	BaseDamage int `json:"base_damage,omitempty"`
	DieSize    int `json:"die_size,omitempty"`
	NumDice    int `json:"num_dice,omitempty"`
	// Encounter squares
	EnemyIndex int `json:"enemy_index,omitempty"`
	EnemyRange int `json:"enemy_range,omitempty"`
	MinEnemy   int `json:"min_enemy,omitempty"`
	// Message
	MsgIndex int `json:"msg_index,omitempty"`
	// Buttons
	Aux []int `json:"aux,omitempty"`
	// Special encounter
	Count        int `json:"count,omitempty"`
	Aux1         int `json:"aux1,omitempty"`
	Aux2         int `json:"aux2,omitempty"`
	SpclMonster  int `json:"-"` // runtime: monster index saved before count decrement
}

// EnemyCalc holds encounter difficulty parameters for a maze level.
type EnemyCalc struct {
	MinEnemy  int `json:"min_enemy"`
	MultWorse int `json:"mult_worse"`
	Worse01   int `json:"worse01"`
	Range0N   int `json:"range0n"`
	PercWorse int `json:"perc_worse"`
}

// MazeLevel holds all data for one dungeon level (20x20 grid).
type MazeLevel struct {
	Level     int          `json:"level"`
	Cells     [][]MazeCell `json:"cells"`
	EnemyCalc []EnemyCalc  `json:"enmycalc"`
}

// GameData holds all extracted data for one Wizardry game.
type GameData struct {
	Game     string              `json:"game"`
	Source   string              `json:"source"`
	Monsters []Monster           `json:"monsters"`
	Items    []Item              `json:"items"`
	Rewards  []Reward            `json:"rewards"`
	ExpTable map[string]*ClassXP `json:"exp_table"`
}

// MazeData holds all maze levels for one Wizardry game.
type MazeData struct {
	Game        string      `json:"game"`
	Source      string      `json:"source"`
	GridSize    int         `json:"grid_size"`
	WallTypes   []string    `json:"wall_types"`
	SquareTypes []string    `json:"square_types"`
	Levels      []MazeLevel `json:"levels"`
}

// MonsterPic holds a single monster image as Unicode half-block art lines.
type MonsterPic struct {
	Monsters []string `json:"monsters"`          // monster names using this pic
	Width    int      `json:"width"`             // pixel width (70)
	Height   int      `json:"height"`            // terminal char height (25)
	Art      []string `json:"art"`               // lines of Unicode half-block art
	HiRes    []int    `json:"hires,omitempty"`   // raw Apple II Hi-Res bytes for NTSC color
	HiResW   int      `json:"hires_w,omitempty"` // bytes per line (10)
	HiResH   int      `json:"hires_h,omitempty"` // pixel height (50)
}

// TitleBitmap holds the title screen image as a monochrome pixel grid.
type TitleBitmap struct {
	Width  int     `json:"width"`
	Height int     `json:"height"`
	Pixels [][]int `json:"pixels"`          // [height][width], 0 or 1
	HiRes  []int   `json:"hires,omitempty"` // raw Apple II Hi-Res framebuffer (8192 bytes) for NTSC color
}

// Scenario holds the complete data set for one game (gamedata + mazes + images).
type Scenario struct {
	GameData
	ScenarioNum int // 1=Proving Grounds, 2=Knight of Diamonds, 3=Legacy of Llylgamyn
	Mazes       MazeData
	MonsterPics map[int]*MonsterPic // keyed by PIC index (0-19)
	Title       *TitleBitmap   // title screen bitmap (nil if not available)
	TitleWT     []byte        // raw WT animation data (Wiz 1 only, nil otherwise)
	TitleStory  [][]string    // multi-page text story (fallback if no TitleFrames)
	TitleFrames []*TitleBitmap // multi-frame title story bitmaps (Wiz 3)
	Messages       [][]string    // SCENARIO.MESGS — message blocks indexed by block number
	MessagesByLine map[int]int   // maps starting line number → Messages block index (for DOMSG)
}

// MessageBlock returns the message block for a DOMSG line index.
// Pascal DOMSG takes a starting line number, not a block index.
// Returns nil if the line index doesn't map to a valid block.
func (s *Scenario) MessageBlock(lineIdx int) []string {
	if s.MessagesByLine == nil {
		// Fallback: treat as block index (pre-fix compatibility)
		if lineIdx >= 0 && lineIdx < len(s.Messages) {
			return s.Messages[lineIdx]
		}
		return nil
	}
	blockIdx, ok := s.MessagesByLine[lineIdx]
	if !ok || blockIdx < 0 || blockIdx >= len(s.Messages) {
		return nil
	}
	return s.Messages[blockIdx]
}
