package engine

// TownLocation represents where in town the player currently is.
type TownLocation int

const (
	Castle TownLocation = iota
	Tavern     // Gilgamesh's Tavern — form party, add/remove members
	Inn        // Adventurer's Inn — rest to recover HP and spells
	Trading    // Boltac's Trading Post — buy/sell equipment
	Temple     // Temple of Cant — heal status, resurrect, tithe
	EdgeOfTown // Edge of Town — enter the maze
	Training   // Training Grounds — create characters, level up, change class
)

var TownLocationNames = [...]string{
	"CASTLE",
	"GILGAMESH'S TAVERN",
	"ADVENTURER'S INN",
	"BOLTAC'S TRADING POST",
	"TEMPLE OF CANT",
	"EDGE OF TOWN",
	"TRAINING GROUNDS",
}

func (t TownLocation) String() string { return TownLocationNames[t] }

// InnRoom represents the quality of inn room (affects healing and cost).
type InnRoom int

const (
	Stables InnRoom = iota // free, minimal healing
	Cot                    // cheap
	Economy                // moderate
	Merchant               // good
	Royal                  // full recovery, expensive
)

var InnRoomNames = [...]string{"STABLES", "A COT", "ECONOMY", "MERCHANT", "ROYAL"}
var InnRoomCosts = [...]int{0, 10, 50, 200, 500}

func (r InnRoom) String() string { return InnRoomNames[r] }

// InnStep tracks the sub-state within the Adventurer's Inn.
// Flow traced from CASTLE segment p-code:
//   InnWho → select character → InnRoom → pick room → InnResult → RETURN → InnWho
type InnStep int

const (
	InnWho        InnStep = iota // "WHO WILL STAY" — select party member by number
	InnSelectRoom                // room menu [A]-[E] for selected character
	InnHealing                   // animated healing loop (HEALHP): HP increments, gold decrements
	InnLevelUp                   // level-up or XP needed (CHNEWLEV), RETURN to continue
)

// ShopStep tracks the sub-state within Boltac's Trading Post.
// Flow traced from SHOPS segment (seg 2) p-code:
//   ShopWho → select character → ShopMain → B/S/U/I/L
type ShopStep int

const (
	ShopWho        ShopStep = iota // "WHO WILL ENTER" — select party member
	ShopMain                      // main menu: B)uy, S)ell, U)ncurse, I)dentify, L)eave
	ShopBuy                       // browsing item catalog
	ShopBuyConfirm                // "UNUSABLE ITEM - CONFIRM BUY (Y/N) ?"
	ShopSell                      // selecting item to sell
	ShopUncurse                   // selecting item to uncurse
	ShopIdentify                  // selecting item to identify
)

// TempleStep tracks the sub-state within the Temple of Cant.
// Flow traced from SHOPS segment (seg 2) p-code procs 28/27/23/25:
//   TempleWho → type name → TempleStatus → show cost → TempleTithe → pick payer →
//   TempleRitual → MURMUR/CHANT/PRAY/INVOKE → TempleResult → RETURN → TempleWho
type TempleStep int

const (
	TempleWho    TempleStep = iota // "WHO ARE YOU HELPING ? >" — type character name
	TempleStatus                   // showing character status and donation cost
	TempleTithe                    // "WHO WILL TITHE" — select party member (1-6) to pay
	TempleRitual                   // "MURMUR - CHANT - PRAY - INVOKE!" animation
	TempleResult                   // showing healing result, RETURN to continue
)

// TempleDonation returns the healing cost for a given status and level.
// Pascal SHOPS.TEXT line 88: MULTLONG(PAYAMT, WHO.CHARLEV) — base cost × character level.
// Base costs from p-code proc 27 XJP 3..6: Paralyzed=100, Stoned=200, Dead=250, Ashed=500
func TempleDonation(status Status, level int) int {
	base := 0
	switch status {
	case Paralyzed:
		base = 100
	case Stoned:
		base = 200
	case Dead:
		base = 250
	case Ashed:
		base = 500
	}
	return base * level
}

// InputMode tracks whether we're in a text-input prompt.
type InputMode int

const (
	InputNone InputMode = iota
	InputAddMember    // typing name at "WHO WILL JOIN ? >"
	InputRemoveMember // "WHO WILL LEAVE ([RETURN] EXITS) >"
	InputShopPurchase // "PURCHASE WHICH ITEM ([RETURN] EXITS) ? >"
	InputTrainingName // typing name at Training Grounds "NAME >"
	InputRoster       // viewing *ROSTER list, L to leave
	InputCharEdit     // character edit menu (I/D/R/C/RET)
	InputInspect      // viewing character sheet (E/D/T/R/L)
	InputReorder      // camp reorder: ">>" walks each position, user picks who goes there
	InputEquip        // category-based equip: walks WEAPON→ARMOR→SHIELD→HELMET→GAUNTLETS→MISC
	InputDrop         // "DROP WHICH ITEM?" — number selects item
	InputTrade        // "TRADE WITH" — number selects party member
	InputTradeGold    // "AMT OF GOLD ? >" — typing gold amount
	InputTradeTarget  // "WHAT ITEM ([RET] EXITS) ? >" — number selects item
	InputSpellBooks   // spell book selection (M/P/L)
	InputSpellList    // viewing known spells list (L to leave)
	InputCastSpell    // "WHAT SPELL ? >" — typing spell name at camp inspect
	InputSpellTarget  // "CAST ON WHO ([RETURN] EXITS) >" — select target for camp spell
	InputUseItem      // "USE ITEM (0=EXIT) ? >" — select item to use at camp inspect
	InputPassword     // "PASSWORD >" or "ENTER PASSWORD >" — verifying character password
	InputSetPassword    // "ENTER NEW PASSWORD ([RET] FOR NONE)" — setting password at Training Grounds
	InputTavernPassword // "ENTER PASSWORD  >" — verifying password at Tavern add member
	InputTempleHelp   // typing name at "WHO ARE YOU HELPING ? >"
	InputConfirmCreate // "DO YOU WANT TO CREATE IT? Y/N ? >"
	InputMalor         // MALOR teleport: N/S/E/W/U/D displacement, RETURN to teleport
	InputClassChange   // class change selection: A-H picks class, RET cancels
	InputConfirmReroll // "ARE YOU SURE YOU WANT TO REROLL (Y/N) ?"
	InputConfirmDelete // "ARE YOU SURE YOU WANT TO DELETE (Y/N) ?"
	InputRiteCeremony  // Wiz 3: displaying ceremony text, waiting for RETURN
	InputRiteAlign     // Wiz 3: choosing alignment for descendant (A/B/C)
)

// TownState tracks the current town UI state.
type TownState struct {
	Location TownLocation
	Roster   *Roster
	Party    *Party
	Creation *CreationState // non-nil during character creation

	// Sub-menu state
	SelectedIndex int       // cursor position in current menu
	Message       string    // feedback text shown to player
	Message2      string    // second line of feedback
	Prompt        string    // current prompt text (e.g. "WHO WILL JOIN ? >")
	InputBuf      string    // text being typed at prompt
	InputMode     InputMode // what kind of input we're waiting for
	PendingCreateName string // name for Y/N create confirmation

	// Inn state machine — from CASTLE segment p-code
	InnStep     InnStep    // which phase of the inn we're in
	InnChar     *Character // character selected for inn rest
	InnMessages []string   // level-up/XP result messages
	InnHealAmt  int        // HP healed per tick (room quality)
	InnHealCost int        // gold cost per tick

	// Boltac's Trading Post state
	ShopStep    ShopStep   // which phase of the shop
	ShopChar    *Character // character who entered the shop
	ShopCatalog int        // current position in buy catalog (item index)
	ShopPage    [6]int     // item indices displayed on current buy page (p-code pageArray)

	// Temple of Cant state — from SHOPS segment p-code procs 28/27/23/25
	TempleStep     TempleStep   // which phase of the temple
	TempleChar     *Character   // character selected for healing (from roster, not necessarily in party)
	TempleCost     int          // donation amount (depends on status)
	TempleMessages []string     // ritual result messages

	// Character edit — which character is selected at Training Grounds
	EditChar      *Character
	InspectSpells SpellType // 0=Mage, 1=Priest for spell list view
	TradeTarget   *Character // who we're trading with
	TradeItemIdx  int        // item number being traded (for InputTradeTarget)
	PendingSpell    *Spell  // spell waiting for target selection ("CAST ON WHO")
	PasswordStep    int     // 0=first entry, 1=confirm (for InputSetPassword)
	PasswordFirst   string  // first password entry (for confirmation comparison)

	// Rite of Passage state (Wiz 3) — from ROLLER.TEXT RITEPASS
	RiteAlignGood bool // can choose Good
	RiteAlignNeut bool // can choose Neutral
	RiteAlignEvil bool // can choose Evil

	// MALOR teleport displacement — from Pascal UTILITIE.TEXT lines 432-473
	MalorDeltaEW int // east(+)/west(-) displacement
	MalorDeltaNS int // north(+)/south(-) displacement
	MalorDeltaUD int // down(+)/up(-) displacement

	// Reorder state — from UTILITIE segment p-code byte 7121
	ReorderPos     int            // current position being assigned (0-based)
	ReorderResult  []*Character   // new order being built

	// Equip state — category walk from UTILITIE segment p-code
	// Categories: 0=WEAPON, 1=ARMOR, 2=SHIELD, 3=HELMET, 4=GAUNTLETS, 6=MISC
	// (5=SPECIAL is skipped during equip)
	EquipCategory  int   // current category being equipped (0-6)
	EquipChoices   []int // item positions (0-based) matching current category
	EquipPartyMode bool  // true = equipping all party members in sequence (camp top-level E)
	EquipPartyIdx  int   // current party member index in party-wide equip mode

	// Class change — from ROLLER CHGCLASS (Pascal lines 574-639)
	ClassChangeList [8]bool // which classes the character qualifies for (excluding current)
}

// Item type categories for the equip flow (matches TOBJREC.OBJTYPE field).
// From UTILITIE segment p-code XJP at byte 5610.
var EquipCategoryNames = [...]string{
	"WEAPON", "ARMOR", "SHIELD", "HELMET", "GAUNTLETS", "SPECIAL", "MISC. ITEM",
}

// EquipCategories is the order in which categories are presented during equip.
// Type 5 (SPECIAL) is skipped — potions/scrolls can't be "equipped".
var EquipCategories = []int{0, 1, 2, 3, 4, 6}

// NewTownState creates initial town state.
func NewTownState() *TownState {
	return &TownState{
		Location: Castle,
		Roster:   &Roster{},
		Party:    &Party{},
	}
}

// CastleMenuItems returns the main castle menu options.
func CastleMenuItems() []string {
	return []string{
		"GILGAMESH'S TAVERN",
		"ADVENTURER'S INN",
		"BOLTAC'S TRADING POST",
		"TEMPLE OF CANT",
		"EDGE OF TOWN",
		"TRAINING GROUNDS",
	}
}

// CastleMenuLocations maps menu indices to town locations.
func CastleMenuLocations() []TownLocation {
	return []TownLocation{Tavern, Inn, Trading, Temple, EdgeOfTown, Training}
}
