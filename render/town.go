package render

import (
	"fmt"
	"image/color"
	"strings"

	"github.com/gdamore/tcell/v2"
	"wizardry/engine"
)

var (
	// Monochrome phosphor green (#33FF33) — matches Apple II green monitor
	// ColorMode switches to white text (matches Apple II color display)
	phosphor   = tcell.NewRGBColor(0x33, 0xFF, 0x33)
	base       = tcell.StyleDefault.Background(tcell.ColorBlack).Foreground(phosphor)
	BaseStyle  = base // exported for use by animation goroutine

	styleTitle     = base.Bold(true)
	styleNormal    = base
	styleHighlight = base.Foreground(tcell.ColorBlack).Background(phosphor)
	styleDim       = base
	styleGold      = base
	styleGreen     = base
	styleRed       = base
	styleCyan      = base
	styleBorder    = base
)

// ApplyColorMode switches from green phosphor monochrome to a color palette.
// Base text becomes white (matching Apple II color display), and named styles
// get distinct colors for improved readability.
func ApplyColorMode() {
	white := tcell.NewRGBColor(0xFF, 0xFF, 0xFF)
	base = tcell.StyleDefault.Background(tcell.ColorBlack).Foreground(white)
	BaseStyle = base
	styleTitle = base.Bold(true)
	styleNormal = base
	styleHighlight = base.Foreground(tcell.ColorBlack).Background(white)
	styleDim = base.Foreground(tcell.NewRGBColor(0x88, 0x88, 0x88))
	styleGold = base
	styleGreen = base.Foreground(tcell.NewRGBColor(0x33, 0xFF, 0x33))
	styleRed = base.Foreground(tcell.NewRGBColor(0xFF, 0x44, 0x44))
	styleCyan = base.Foreground(tcell.NewRGBColor(0x00, 0xDD, 0xFF))
	styleBorder = base.Foreground(tcell.NewRGBColor(0xA0, 0xA0, 0xA0))

	// Switch sixel foreground from green to white
	sixelFG = color.RGBA{0xFF, 0xFF, 0xFF, 255}
	sixelDim = color.RGBA{0x88, 0x88, 0x88, 255}
	sixelBright = color.RGBA{0xFF, 0xFF, 0xFF, 255}
	sixelBorder = color.RGBA{0xA0, 0xA0, 0xA0, 255}
}

// Box width matches original Apple II 40-column screen
const boxW = 40

func (s *Screen) hline(x, y int) {
	s.SetCell(x, y, '+', styleBorder)
	for c := 1; c < boxW-1; c++ {
		s.SetCell(x+c, y, '-', styleBorder)
	}
	// Right corner — no extension
	s.tcell.SetContent((x+boxW-1)*s.scale, y, '+', nil, styleBorder)
}

func (s *Screen) hlineText(x, y int, text string) {
	pad := boxW - 4 - len(text)
	left := pad / 2
	right := pad - left
	s.SetCell(x, y, '+', styleBorder)
	for c := 0; c < left; c++ {
		s.SetCell(x+1+c, y, '-', styleBorder)
	}
	s.DrawString(x+1+left, y, styleBorder, " "+text+" ")
	for c := 0; c < right; c++ {
		s.SetCell(x+1+left+2+len(text)+c, y, '-', styleBorder)
	}
	// Right corner — no extension
	s.tcell.SetContent((x+boxW-1)*s.scale, y, '+', nil, styleBorder)
}

func (s *Screen) boxLine(x, y int, text string, st tcell.Style) {
	inner := boxW - 2
	if len(text) > inner {
		text = text[:inner]
	}
	pad := inner - len(text)
	s.SetCell(x, y, '!', styleBorder)
	s.DrawString(x+1, y, st, text+strings.Repeat(" ", pad))
	s.SetCell(x+boxW-1, y, '!', styleBorder)
}

func (s *Screen) boxEmpty(x, y int) {
	s.boxLine(x, y, "", styleNormal)
}

// RenderTown draws the current town screen — layout from p-code CASTLE segment.
//
// From the p-code (offsets 475-776):
//
//	Row 0:  +--------------------------------------+     WRITE + WRITELN
//	Row 1:  | CASTLE                      LOCATION |     CIP 4: GOTOXY(0,1) + WRITE
//	Row 2:  +----------- CURRENT PARTY: -----------+     WRITE + WRITELN
//	Row 3:  (blank)                                       WRITELN
//	Row 4:   # CHARACTER NAME  CLASS AC HITS STATUS       WRITE + WRITELN (39 chars, NO borders)
//	Row 5-10: party slots (CIP 3 or blank, NO borders)
//	Row 11: +--------------------------------------+     WRITE + WRITELN
//	Row 12: (spacing)
//	Row 13+: menu text (varies by location)
func (s *Screen) RenderTown(game *engine.GameState) {
	s.Clear()
	s.ClearSixelTransition()
	town := game.Town

	// Training Grounds is a standalone screen — no party box, no borders
	// (from ROLLER segment p-code)
	if town.Location == engine.Training {
		s.renderTrainingScreen(game)
		s.Show()
		return
	}

	// Inspect screen is standalone (shared between Tavern and Training)
	if town.EditChar != nil {
		switch town.InputMode {
		case engine.InputEquip:
			s.renderEquipScreen(game)
			s.Show()
			return
		case engine.InputInspect, engine.InputDrop,
			engine.InputTrade, engine.InputTradeGold, engine.InputTradeTarget,
			engine.InputCastSpell, engine.InputUseItem:
			s.renderInspectScreen(game)
			s.Show()
			return
		case engine.InputSpellBooks:
			s.renderSpellBooksScreen(game)
			s.Show()
			return
		case engine.InputSpellList:
			s.renderSpellListScreen(game)
			s.Show()
			return
		}
	}

	// Row 0: top border
	s.DrawString(0, 0, styleBorder, "+--------------------------------------+")

	// Row 1: proc 37 header — "! CASTLE" + location:30 + " !"
	// P-code uses '!' for side borders (Apple II vertical bar)
	// Wiz 2 uses "LLYLGAMN" (from CASTLE segment p-code proc 37)
	loc := locationShort[town.Location]
	townName := "CASTLE"
	if game.Scenario.ScenarioNum == 2 {
		townName = "LLYLGAMN"
	}
	locPad := 36 - len(townName) // total 40: "! " (2) + name + pad + " !" (2)
	header := fmt.Sprintf("! %s%*s !", townName, locPad, loc)
	s.DrawString(0, 1, styleTitle, header)

	// Row 2: divider
	s.DrawString(0, 2, styleBorder, "+----------- CURRENT PARTY: -----------+")

	// Row 3: blank (WRITELN)

	// Row 4: column header (39 chars, NO side borders)
	s.DrawString(0, 4, styleDim, " # CHARACTER NAME  CLASS AC HITS STATUS")

	// Rows 5-10: 6 party slots (NO side borders)
	for i := 0; i < 6; i++ {
		if i < len(town.Party.Members) && town.Party.Members[i] != nil {
			m := town.Party.Members[i]
			line := formatPartyLine(i+1, m)
			st := styleNormal
			if m.IsDead() {
				st = styleRed
			}
			s.DrawString(0, 5+i, st, line)
		}
	}

	// Row 11: bottom border
	s.DrawString(0, 11, styleBorder, "+--------------------------------------+")

	// Row 13+: menu text (after spacing)
	y := 13
	switch town.Location {
	case engine.Castle:
		s.renderCastleMenu(0, y, game)
	case engine.Tavern:
		s.renderTavernMenu(0, y, game)
	case engine.Inn:
		s.renderInnMenu(0, y, game)
	case engine.Trading:
		s.renderTradingMenu(0, y, game)
	case engine.Temple:
		s.renderTempleMenu(0, y, game)
	case engine.EdgeOfTown:
		s.renderEdgeMenu(0, y, game)
	}

	s.Show()
}

// Location names for the header (right side) — traced from p-code
// Castle="MARKET" (CASTLE seg offset 5537→CLP 4), Tavern="TAVERN" (1759),
// Inn="INN" (2051→CIP 4), Trading="SHOP" (1985), Temple="TEMPLE" (2017),
// Edge="EXIT" (5231), Training="TRAINING GROUNDS" (ROLLER seg 5427)
var locationShort = [...]string{
	"MARKET", "TAVERN", "INN", "SHOP", "TEMPLE", "EXIT", "TRAINING GROUNDS",
}

func (s *Screen) renderCastleMenu(x, y int, game *engine.GameState) {
	// From p-code proc 3 (IC 5258): WRITECH(' ':13) then "YOU MAY GO TO:"
	// 13 spaces centers on 40-column screen: (40-14)/2 = 13
	s.DrawString(x+13, y, styleNormal, "YOU MAY GO TO:")
	y += 2
	s.DrawString(x, y, styleNormal, "THE A)DVENTURER'S INN, G)ILGAMESH'S")
	y++
	s.DrawString(x, y, styleNormal, "TAVERN, B)OLTAC'S TRADING POST, THE")
	y++
	s.DrawString(x, y, styleNormal, "TEMPLE OF C)ANT, OR THE E)DGE OF TOWN.")

	if game.Town.Message != "" {
		y += 2
		s.DrawString(x, y, styleGold, game.Town.Message)
	}
}

func (s *Screen) renderTavernMenu(x, y int, game *engine.GameState) {
	town := game.Town

	// From p-code proc 33 (IC 848-1097): conditional menu items
	// y starts at 13. "OR PRESS [RETURN]" always ends at row 17
	// regardless of party size (p-code uses cleared lines for padding).
	s.DrawString(x, y, styleNormal, "YOU MAY ")
	if town.Party.Size() < 6 {
		// p-code: comma only when partySize > 0 (IC 909-933)
		if town.Party.Size() > 0 {
			s.DrawString(x+8, y, styleNormal, "A)DD A MEMBER,")
		} else {
			s.DrawString(x+8, y, styleNormal, "A)DD A MEMBER")
		}
		y++
		// p-code: WRITECH(' ':8) indent for next line
	}
	if town.Party.Size() > 0 {
		s.DrawString(x+8, y, styleNormal, "R)EMOVE A MEMBER,")
		y++
		s.DrawString(x+8, y, styleNormal, "#) SEE A MEMBER,")
		y++
	} else {
		// When no party, p-code outputs two cleared lines (CHR(29)+WRITELN x2)
		y += 2
	}
	y++
	s.DrawString(x, y, styleNormal, "OR PRESS [RETURN] TO LEAVE")

	if town.InputMode == engine.InputAddMember {
		// From p-code proc 30 (IC 1144): GOTOXY(0, 19)
		s.DrawString(x, 19, styleNormal, fmt.Sprintf("WHO WILL JOIN ? >%s", town.InputBuf))
		s.DrawString(x+17+len(town.InputBuf), 19, styleGold, "_")
	}

	if town.InputMode == engine.InputRemoveMember {
		// "WHO WILL LEAVE" — from CASTLE p-code byte 1619
		s.DrawString(x, 18, styleNormal, "WHO WILL LEAVE ([RETURN] EXITS) >")
	}

	if town.InputMode == engine.InputTavernPassword {
		// From p-code CASTLE proc 30 (IC 1416): name stays on row 19, password on row 20
		if town.EditChar != nil {
			s.DrawString(x, 19, styleNormal, fmt.Sprintf("WHO WILL JOIN ? >%s", town.EditChar.Name))
		}
		s.DrawString(x, 20, styleNormal, fmt.Sprintf("ENTER PASSWORD  >%s", town.InputBuf))
		s.DrawString(x+17+len(town.InputBuf), 20, styleNormal, "_")
	}

	if town.Message != "" {
		y += 2
		s.DrawString(x, y, styleGold, town.Message)
	}
}

func (s *Screen) renderInnMenu(x, y int, game *engine.GameState) {
	town := game.Town

	switch town.InnStep {
	case engine.InnWho:
		// From p-code proc 26 (IC 2058): GOTOXY(0,13), then WIZARDRY.proc15("WHO WILL STAY")
		s.DrawString(x, y, styleNormal, "WHO WILL STAY ([RETURN] EXITS) >")

	case engine.InnSelectRoom:
		// Room menu for selected character — from p-code bytes 2120-2451
		// "   WELCOME <NAME>. WE HAVE:" then room list
		if town.InnChar != nil {
			s.DrawString(x, y, styleNormal,
				fmt.Sprintf("   WELCOME %s. WE HAVE:", town.InnChar.Name))
		}
		y += 2
		s.DrawString(x, y, styleNormal, "[A] THE STABLES (FREE!)")
		y++
		s.DrawString(x, y, styleNormal, "[B] COTS. 10 GP/WEEK.")
		y++
		s.DrawString(x, y, styleNormal, "[C] ECONOMY ROOMS. 50 GP/WEEK.")
		y++
		s.DrawString(x, y, styleNormal, "[D] MERCHANT SUITES. 200 GP/WEEK.")
		y++
		s.DrawString(x, y, styleNormal, "[E] ROYAL SUITES. 500 GP/WEEK.")
		y++
		s.DrawString(x, y, styleNormal, "    OR [RETURN] TO LEAVE")

	case engine.InnHealing:
		// Animated healing — Pascal HEALHP (CASTLE2.TEXT lines 423-448):
		// Redraws at GOTOXY(0,13) each tick showing current HP and gold.
		c := town.InnChar
		if c != nil {
			if town.InnHealAmt == 0 {
				// Stables: just "IS NAPPING"
				s.DrawString(x, y, styleNormal, fmt.Sprintf("%s IS NAPPING", c.Name))
			} else {
				s.DrawString(x, y, styleNormal, fmt.Sprintf("%s IS HEALING UP", c.Name))
				y += 3
				s.DrawString(x, y, styleNormal,
					fmt.Sprintf("         HIT POINTS (%d/%d)", c.HP, c.MaxHP))
				y += 2
				s.DrawString(x, y, styleNormal,
					fmt.Sprintf("               GOLD  %d", c.Gold))
			}
		}

	case engine.InnLevelUp:
		// Level-up or XP needed — Pascal CHNEWLEV (CASTLE2.TEXT lines 386-412)
		for _, msg := range town.InnMessages {
			if y >= 23 {
				break
			}
			s.DrawString(x, y, styleGold, msg)
			y++
		}
		s.DrawString(x, 23, styleNormal, "PRESS [RETURN] TO LEAVE")
	}

	if town.Message != "" {
		y += 2
		s.DrawString(x, y, styleGold, town.Message)
	}
}

func (s *Screen) renderTradingMenu(x, y int, game *engine.GameState) {
	town := game.Town
	items := game.Scenario.Items

	switch town.ShopStep {
	case engine.ShopWho:
		// p-code proc 21 (IC 4095): "       WELCOME TO THE TRADING POST"
		// Then one WRITELN, then WIZARDRY.proc15 at row 14
		s.DrawString(x, y, styleNormal, "       WELCOME TO THE TRADING POST")
		y++
		s.DrawString(x, y, styleNormal, "WHO WILL ENTER ([RETURN] EXITS) >")

	case engine.ShopMain:
		// p-code IC 3664: GOTOXY(0,13), IC 3681-3967
		// Row 13: "      WELCOME " + name
		// Row 14: "     YOU HAVE " + gold + " GOLD"
		// Row 15: blank
		// Row 16-19: menu text
		if town.ShopChar != nil {
			s.DrawString(x, y, styleNormal,
				fmt.Sprintf("      WELCOME %s", town.ShopChar.Name))
			y++
			s.DrawString(x, y, styleNormal,
				fmt.Sprintf("     YOU HAVE %d GOLD", town.ShopChar.Gold))
		}
		y += 2
		s.DrawString(x, y, styleNormal, "YOU MAY B)UY  AN ITEM,")
		y++
		s.DrawString(x, y, styleNormal, "        S)ELL AN ITEM, HAVE AN ITEM")
		y++
		s.DrawString(x, y, styleNormal, "        U)NCURSED,  OR HAVE AN ITEM")
		y++
		s.DrawString(x, y, styleNormal, "        I)DENTIFIED, OR L)EAVE")

	case engine.ShopBuy:
		// p-code CLP 12 (IC 2168-2541): buy catalog with 6 items per page
		// CLP 13/14: item display at rows 13-18, GOTOXY(0, 12+N)
		// Per item: WRITEINT(N,1) + ')' + WRITESTR(name,15) + ' ' + price [+ " UNUSABLE"]
		// Row 20: "YOU HAVE " + gold + " GOLD"
		// Row 21-23: menu text
		if town.ShopChar != nil {
			c := town.ShopChar
			for i := 0; i < 6; i++ {
				itemIdx := town.ShopPage[i]
				if itemIdx < 0 || itemIdx >= len(items) {
					continue
				}
				item := items[itemIdx]
				// Skip items with 0 stock
				if item.Stock == 0 {
					continue
				}
				line := fmt.Sprintf("%d)%15s %d", i+1, item.Name, item.Price)
				// Class usability check — IXP 16,1 on ClassUse field
				if item.ClassUse != 0 && (item.ClassUse&(1<<uint(c.Class))) == 0 {
					line += " UNUSABLE"
				}
				s.DrawString(x, y, styleNormal, line)
				y++
			}

			// Row 20: gold display (p-code IC 2245-2293)
			s.DrawString(x, 20, styleNormal,
				fmt.Sprintf("YOU HAVE %d GOLD", c.Gold))

			if town.InputMode == engine.InputShopPurchase {
				// Purchase sub-mode prompt — p-code GOTOXY(0,21), WRITELN, then WRITESTR at row 22
				s.DrawString(x, 22, styleNormal, "PURCHASE WHICH ITEM ([RETURN] EXITS) ? >")
			} else {
				// Browse menu (p-code IC 2313-2415)
				s.DrawString(x, 21, styleNormal, "YOU MAY P)URCHASE, SCROLL")
				s.DrawString(x, 22, styleNormal, "        F)ORWARD OR B)ACK, GO TO THE")
				s.DrawString(x, 23, styleNormal, "        S)TART, OR L)EAVE")
			}
		}

	case engine.ShopSell, engine.ShopUncurse, engine.ShopIdentify:
		// p-code CLP 18 (IC 2558-2792): shared item list for sell/uncurse/identify
		// GOTOXY(0,13): item list starts at row 13
		// Per item: WRITEINT(N,1) + WRITECH(')') + WRITESTR(name,15) + ' ' + price
		// For sell mode: unidentified items show unknown name and price=1 GP
		// p-code IC 3453: GOTOXY(0,22) for prompt
		if town.ShopChar != nil {
			c := town.ShopChar
			for i := 0; i < c.ItemCount; i++ {
				poss := c.Items[i]
				if poss.ItemIndex < 0 || poss.ItemIndex >= len(items) {
					continue
				}
				item := items[poss.ItemIndex]
				// Item name: identified shows real name, unidentified shows unknown name
				// WRITESTR with width=15 right-justifies
				name := item.Name
				if !poss.Identified {
					name = item.NameUnknown
				}
				// Price/fee calculation: CXP seg=1 proc=10 (halves the price)
				// For sell mode with unidentified items: price = 1 GP
				price := item.Price / 2
				if town.ShopStep == engine.ShopSell && !poss.Identified {
					price = 1
				}
				s.DrawString(x, y, styleNormal,
					fmt.Sprintf("%d)%15s %d", i+1, name, price))
				y++
			}
			// Prompt at row 22 (p-code IC 3453: GOTOXY(0,22))
			promptY := 22
			switch town.ShopStep {
			case engine.ShopSell:
				s.DrawString(x, promptY, styleNormal, "WHICH DO YOU WISH TO SELL ? >")
			case engine.ShopUncurse:
				s.DrawString(x, promptY, styleNormal, "WHICH DO YOU WISH UNCURSED ? >")
			case engine.ShopIdentify:
				s.DrawString(x, promptY, styleNormal, "WHICH DO YOU WISH IDENTIFIED ? >")
			}
		}
	}

	if town.Message != "" {
		// p-code CLP 16/20: messages centered on row 23
		msgY := 23
		if town.ShopStep == engine.ShopWho || town.ShopStep == engine.ShopMain {
			msgY = y + 2
		}
		msg := town.Message
		col := (40 - len(msg)) / 2
		if col < 0 {
			col = 0
		}
		s.DrawString(col, msgY, styleGold, msg)
	}
}

func (s *Screen) renderTempleMenu(x, y int, game *engine.GameState) {
	town := game.Town

	switch town.TempleStep {
	case engine.TempleWho:
		// From p-code proc 28 (IC 78-182): welcome + name prompt
		s.DrawString(x, y, styleNormal, " WELCOME TO THE TEMPLE OF RADIANT CANT!")
		y += 2
		s.DrawString(x, y, styleNormal, "WHO ARE YOU HELPING ? >")
		if town.InputMode == engine.InputTempleHelp {
			s.DrawString(x+23, y, styleNormal, town.InputBuf+"_")
		}

	case engine.TempleTithe:
		// From p-code proc 27 (IC 496-581): donation at GOTOXY(0,17), tithe at row 18
		s.DrawString(x, y, styleNormal, " WELCOME TO THE TEMPLE OF RADIANT CANT!")
		y += 2
		if town.TempleChar != nil {
			s.DrawString(x, y, styleNormal,
				fmt.Sprintf("%s : %s", town.TempleChar.Name, town.TempleChar.Status))
			y += 2
			// Row 17: donation cost — GOTOXY(0,17)
			s.DrawString(x, y, styleNormal,
				fmt.Sprintf("THE DONATION WILL BE %d", town.TempleCost))
			y++
			// Row 18: tithe prompt — one WRITELN after donation, then proc15
			s.DrawString(x, y, styleNormal, "WHO WILL TITHE ([RETURN] EXITS) >")
		}

	case engine.TempleRitual:
		// From p-code proc 23 (IC 798): GOTOXY(0,17) for ritual text
		s.DrawString(x, y, styleNormal, " WELCOME TO THE TEMPLE OF RADIANT CANT!")
		// Row 17: "MURMUR - CHANT - PRAY - INVOKE!" at GOTOXY(0,17)
		s.DrawString(x, 17, styleNormal, "MURMUR - CHANT - PRAY - INVOKE!")

	case engine.TempleResult:
		// Healing result display
		for _, msg := range town.TempleMessages {
			s.DrawString(x, y, styleGold, msg)
			y++
		}
		y++
		s.DrawString(x, y, styleNormal, "PRESS [RETURN] TO CONTINUE")
	}

	if town.Message != "" {
		msgY := 23
		msg := town.Message
		col := (40 - len(msg)) / 2
		if col < 0 {
			col = 0
		}
		s.DrawString(col, msgY, styleGold, msg)
	}
}

func (s *Screen) renderEdgeMenu(x, y int, game *engine.GameState) {
	town := game.Town

	// Exact text from p-code — two versions depending on party
	if town.Party.Size() == 0 {
		s.DrawString(x, y, styleNormal, "YOU MAY GO TO THE T)RAINING GROUNDS,")
		y++
		s.DrawString(x, y, styleNormal, "RETURN TO THE C)ASTLE, OR L)EAVE THE")
		y++
		s.DrawString(x, y, styleNormal, "GAME.")
	} else {
		s.DrawString(x, y, styleNormal, "YOU MAY ENTER THE M)AZE, THE T)RAINING")
		y++
		s.DrawString(x, y, styleNormal, "GROUNDS, C)ASTLE,  OR L)EAVE THE GAME.")
	}

	if town.Message != "" {
		y += 2
		s.DrawString(x, y, styleGold, town.Message)
	}
}

// renderTrainingScreen draws the Training Grounds as a standalone screen.
// From ROLLER segment p-code (offsets 5410-5683):
// No borders, centered title, menu text with indentation.
//
//	Row 0:  (12 spaces) TRAINING GROUNDS     ← centered (ROLLER:5418-5446)
//	Row 1:  (blank)
//	Row 2:  YOU MAY ENTER A CHARACTER NAME TO ADD,
//	Row 3:  (8 spaces) INSPECT OR EDIT,
//	Row 4:  (blank)
//	Row 5:  (8 spaces) "*ROSTER" TO SEE ROSTER,
//	Row 6:  (blank)
//	Row 7:  OR PRESS [RET] FOR CASTLE.
//	Row 8:  (blank)
//	Row 9:  (GOTOXY 13,9) NAME >_
func (s *Screen) renderTrainingScreen(game *engine.GameState) {
	town := game.Town

	// *ROSTER display — from ROLLER segment offsets 3300-3704
	if town.InputMode == engine.InputRoster {
		s.renderRosterScreen(game)
		return
	}

	// Password verification — from ROLLER proc 2 (IC 4826-4874)
	if town.InputMode == engine.InputPassword {
		// Show training grounds with password prompt at GOTOXY(9,10)
		s.DrawString(12, 0, styleTitle, "TRAINING GROUNDS")
		s.DrawString(9, 10, styleNormal, "PASSWORD >")
		s.DrawString(20, 10, styleNormal, town.InputBuf+"_")
		if town.Message != "" {
			s.DrawString(0, 12, styleNormal, town.Message)
		}
		return
	}

	// Set new password — from ROLLER proc 3 (IC 4480-4816)
	if town.InputMode == engine.InputSetPassword {
		if town.PasswordStep == 0 {
			s.DrawString(0, 0, styleNormal, "ENTER NEW PASSWORD ([RET] FOR NONE)")
			s.DrawString(10, 2, styleNormal, town.InputBuf+"_")
		} else {
			s.DrawString(0, 0, styleNormal, "ENTER AGAIN TO BE SURE")
			s.DrawString(10, 2, styleNormal, town.InputBuf+"_")
		}
		return
	}

	// Character inspect/edit screens — from ROLLER segment p-code
	if town.EditChar != nil {
		switch town.InputMode {
		case engine.InputRiteCeremony:
			s.renderRiteCeremony(game)
			return
		case engine.InputRiteAlign:
			s.renderRiteAlign(game)
			return
		case engine.InputCharEdit:
			s.renderCharEditScreen(game)
			return
		case engine.InputConfirmReroll:
			s.renderConfirmScreen(game, "REROLL")
			return
		case engine.InputConfirmDelete:
			s.renderConfirmScreen(game, "DELETE")
			return
		case engine.InputClassChange:
			s.renderClassChangeScreen(game)
			return
		case engine.InputEquip:
			s.renderEquipScreen(game)
			return
		case engine.InputInspect:
			s.renderInspectScreen(game)
			return
		case engine.InputSpellBooks:
			s.renderSpellBooksScreen(game)
			return
		case engine.InputSpellList:
			s.renderSpellListScreen(game)
			return
		}
	}

	// Row 0: centered title (12 spaces + 16 chars = centered on 40 cols)
	s.DrawString(12, 0, styleTitle, "TRAINING GROUNDS")

	if game.Scenario.ScenarioNum >= 2 {
		// Wiz 2/3: no character creation allowed
		// From Wiz 2 ROLLER proc 8 (IC 2818-3277)
		s.DrawString(0, 2, styleNormal, "ENTER THE NAME OF THE CHARACTER YOU WANT")
		s.DrawString(0, 3, styleNormal, "TO INSPECT OR EDIT, OR \"*ROSTER\" TO  SEE")
		s.DrawString(0, 4, styleNormal, "THE ROSTER OF CHARACTERS, OR [RET] TO GO")
		if game.Scenario.ScenarioNum == 2 {
			s.DrawString(0, 5, styleNormal, "BACK TO LLYLGAMN.")
		} else {
			s.DrawString(0, 5, styleNormal, "BACK TO CASTLE.")
		}
		s.DrawString(0, 7, styleNormal, "YOU CANNOT CREATE CHARACTERS HERE.")
		s.DrawString(0, 8, styleNormal, "THEY SHOULD BE CREATED IN THE \"PROVING")
		s.DrawString(0, 9, styleNormal, "GROUNDS\" AND THEN TRANSFERRED HERE WHEN")
		s.DrawString(0, 10, styleNormal, "13TH LEVEL OR SO.")

		// Name prompt at row 13
		s.DrawString(13, 13, styleNormal, "NAME >")
		if town.InputMode == engine.InputTrainingName {
			s.DrawString(19, 13, styleNormal, town.InputBuf)
			s.DrawString(19+len(town.InputBuf), 13, styleGold, "_")
		} else {
			s.DrawString(19, 13, styleGold, "_")
		}
	} else {
		// Wiz 1: original training grounds text
		// Row 2: menu text (from ROLLER segment LSA strings)
		s.DrawString(0, 2, styleNormal, "YOU MAY ENTER A CHARACTER NAME TO ADD,")
		s.DrawString(8, 3, styleNormal, "INSPECT OR EDIT,")

		s.DrawString(8, 5, styleNormal, "\"*ROSTER\" TO SEE ROSTER,")

		// From p-code: no indentation — WRITESTR at cursor col 0
		s.DrawString(0, 7, styleNormal, "OR PRESS [RET] FOR CASTLE.")

		// Row 9: name prompt — GOTOXY(13,9) from p-code byte 5657-5659
		s.DrawString(13, 9, styleNormal, "NAME >")
		if town.InputMode == engine.InputTrainingName {
			s.DrawString(19, 9, styleNormal, town.InputBuf)
			s.DrawString(19+len(town.InputBuf), 9, styleGold, "_")
		} else {
			s.DrawString(19, 9, styleGold, "_")
		}
	}

	if town.Message != "" {
		msgY := 11
		if game.Scenario.ScenarioNum >= 2 {
			msgY = 15
		}
		s.DrawString(0, msgY, styleGold, town.Message)
		if town.Message2 != "" {
			s.DrawString(0, msgY+1, styleGold, town.Message2)
		}
	}
}

// renderRosterScreen draws the *ROSTER display.
// From ROLLER segment p-code proc 11 (IC 3300-3710):
//
//	Row 0:  NAMES IN USE:                         ← col 0 (no centering)
//	Row 1:  ----------------------------------------
//	Row 2+: NAME LEVEL # RACE CLASS (STATUS)      ← GOTOXY(0, count+1)
//	Row 22: ----------------------------------------  ← GOTOXY(0,22)
//	Row 23: YOU MAY L)EAVE WHEN READY
func (s *Screen) renderRosterScreen(game *engine.GameState) {
	// Row 0: header at col 0 (p-code WRITESTR, no indentation)
	s.DrawString(0, 0, styleNormal, "NAMES IN USE:")

	// Row 1: 40 dashes (p-code IC 3343)
	s.DrawString(0, 1, styleBorder, "----------------------------------------")

	// Rows 2+: character listings
	// From p-code: count starts at 0, increments for each non-LOST char,
	// GOTOXY(0, count+1) positions each entry
	count := 0
	for _, c := range game.Town.Roster.Characters {
		if c == nil || c.Status == engine.Lost {
			continue
		}
		count++
		// Format from p-code: name + " LEVEL " + level + " " + race + " " + class + " (" + status + ")"
		line := fmt.Sprintf("%s LEVEL %d %s %s (%s)",
			c.Name, c.Level, c.Race, c.Class, c.Status)
		s.DrawString(0, count+1, styleNormal, line)
	}

	// Row 22: bottom dashes (GOTOXY(0,22) from p-code IC 3598)
	s.DrawString(0, 22, styleBorder, "----------------------------------------")

	// Row 23: leave prompt (p-code IC 3663)
	s.DrawString(0, 23, styleNormal, "YOU MAY L)EAVE WHEN READY")
}

// renderCharEditScreen draws the character edit menu at Training Grounds.
// From ROLLER segment p-code (IC 4905-5269):
// All lines at col 0, written with WRITESTR + WRITELN from cursor position.
//
//	Row 0:  NAME LEVEL N RACE CLASS (STATUS)
//	Row 1:  (blank)
//	Row 2:  YOU MAY I)NSPECT THIS CHARACTER,
//	Row 3:  D)ELETE  THIS CHARACTER,
//	Row 4:  R)EROLL  THIS CHARACTER,
//	Row 5:  C)HANGE  CLASS,
//	Row 6:  S)ET NEW PASSWORD, OR
//	Row 7:    PRESS [RET] TO LEAVE
func (s *Screen) renderCharEditScreen(game *engine.GameState) {
	c := game.Town.EditChar

	// Row 0: character header — NAME LEVEL N RACE CLASS (STATUS)
	header := fmt.Sprintf("%s LEVEL %d %s %s (%s)",
		c.Name, c.Level, c.Race, c.Class, c.Status)
	s.DrawString(0, 0, styleNormal, header)

	// Rows 2-7: menu — WRITESTR widths right-justify to align at col 8
	// "YOU MAY " is 8 chars, so I)NSPECT starts at col 8.
	// Wiz 3 has R)ITE OF PASSAGE instead of R)EROLL, A)LTER instead of S)ET.
	// From Wiz 3 ROLLER.TEXT line 649: R)ITE OF PASSAGE/I)NSPECT/D)ELETE/C)HANGE CLASS/A)LTER PASSWORD/L)EAVE
	if game.Scenario.ScenarioNum == 3 {
		s.DrawString(0, 2, styleNormal, "R)ITE OF PASSAGE,")
		s.DrawString(0, 3, styleNormal, "YOU MAY I)NSPECT THIS CHARACTER,")
		s.DrawString(8, 4, styleNormal, "D)ELETE  THIS CHARACTER,")
		s.DrawString(8, 5, styleNormal, "C)HANGE  CLASS,")
		s.DrawString(8, 6, styleNormal, "S)ET NEW PASSWORD, OR")
		s.DrawString(0, 7, styleNormal, "  PRESS [RET] TO LEAVE")
	} else {
		s.DrawString(0, 2, styleNormal, "YOU MAY I)NSPECT THIS CHARACTER,")
		s.DrawString(8, 3, styleNormal, "D)ELETE  THIS CHARACTER,")
		s.DrawString(8, 4, styleNormal, "R)EROLL  THIS CHARACTER,")
		s.DrawString(8, 5, styleNormal, "C)HANGE  CLASS,")
		s.DrawString(8, 6, styleNormal, "S)ET NEW PASSWORD, OR")
		s.DrawString(0, 7, styleNormal, "  PRESS [RET] TO LEAVE")
	}

	if game.Town.Message != "" {
		s.DrawString(0, 9, styleGold, game.Town.Message)
	}
}

// renderConfirmScreen draws the "ARE YOU SURE YOU WANT TO <action> (Y/N) ?" prompt.
// From ROLLER segment p-code proc 6 (IC 3800-3889):
// Clears screen, writes prompt at row 0, waits for Y or N.
func (s *Screen) renderConfirmScreen(game *engine.GameState, action string) {
	s.DrawString(0, 0, styleNormal, "ARE YOU SURE YOU WANT TO "+action+" (Y/N) ?")
}

// renderRiteCeremony draws the Rite of Passage ceremony text.
// From Pascal ROLLER.TEXT RITEPASS (lines 363-370):
//
//	THE RITE OF PASSAGE CEREMONY
//	NOW BEGINS.
//	(blank)
//	THE TEMPLE PRIESTS LINK UP THIS
//	ANCESTRAL SPIRIT WITH ITS
//	DESCENDANT...
//	(blank)
//	PRESS (RETURN)
func (s *Screen) renderRiteCeremony(game *engine.GameState) {
	s.DrawString(0, 2, styleNormal, "THE RITE OF PASSAGE CEREMONY")
	s.DrawString(0, 3, styleNormal, "NOW BEGINS.")
	s.DrawString(0, 5, styleNormal, "THE TEMPLE PRIESTS LINK UP THIS")
	s.DrawString(0, 6, styleNormal, "ANCESTRAL SPIRIT WITH ITS")
	s.DrawString(0, 7, styleNormal, "DESCENDANT...")
	s.DrawString(0, 9, styleNormal, "PRESS (RETURN)")
}

// renderRiteAlign draws the alignment selection for the Rite of Passage.
// From Pascal ROLLER.TEXT CHOSALGN (lines 304-332).
func (s *Screen) renderRiteAlign(game *engine.GameState) {
	town := game.Town
	s.DrawString(0, 2, styleNormal, "CHOOSE ALIGNMENT FOR DESCENDANT:")
	row := 4
	if town.RiteAlignGood {
		s.DrawString(0, row, styleNormal, "A)    GOOD")
		row++
	}
	if town.RiteAlignNeut {
		s.DrawString(0, row, styleNormal, "B) NEUTRAL")
		row++
	}
	if town.RiteAlignEvil {
		s.DrawString(0, row, styleNormal, "C)    EVIL")
		row++
	}
	s.DrawString(0, row+1, styleNormal, "SELECT ALIGNMENT >")
}

// renderClassChangeScreen draws the class change selection.
// From Pascal ROLLER.TEXT CHGCLASS (lines 582-598):
//
//	Row 0: (blank — cleared with CHR(11))
//	Row 2+: available classes listed as "A) FIGHTER", "B) MAGE", etc.
//	         excludes current class
//	Then: "PRESS [LETTER] TO CHANGE CLASS"
//	      "         [RET] TO NOT CHANGE CLASS"
func (s *Screen) renderClassChangeScreen(game *engine.GameState) {
	town := game.Town
	row := 2
	for cl := engine.Fighter; cl <= engine.Ninja; cl++ {
		if town.ClassChangeList[cl] && cl != town.EditChar.Class {
			letter := byte('A') + byte(cl)
			s.DrawString(0, row, styleNormal, fmt.Sprintf("%c) %s", letter, engine.ClassNames[cl]))
			row++
		}
	}
	row++
	s.DrawString(0, row, styleNormal, "PRESS [LETTER] TO CHANGE CLASS")
	row++
	s.DrawString(0, row, styleNormal, fmt.Sprintf("%34s", "[RET] TO NOT CHANGE CLASS"))
}

// renderInspectScreen draws the full character sheet.
// Layout traced from CAMP segment (seg 12) p-code bytes 3817-4864.
//
//	Row 0:  NAME RACE A-CLASS
//	Row 2:  (right-justified labels, width 12)(values, width 3)(right-side info)
//	Row 9:  MAGE spell slots (GOTOXY 0,9)
//	Row 10: PRIEST spell slots
//	Row 12: equipment legend (GOTOXY 0,12)
//	Row 14-17: equipment in 2 columns (odd=col 0, even=col 20)
//	Row 18: menu (GOTOXY 0,18)
func (s *Screen) renderInspectScreen(game *engine.GameState) {
	c := game.Town.EditChar
	items := game.Scenario.Items

	// Row 0: header — NAME RACE A-CLASS
	alignPrefix := [...]string{"G", "N", "E"}
	s.DrawString(0, 0, styleNormal, fmt.Sprintf("%s %s %s-%s",
		c.Name, c.Race, alignPrefix[c.Alignment], c.Class))

	// Rows 2-7: stats — WRITESTR(label,12) + WRITEINT(val,3) + right-side
	s.DrawString(0, 2, styleNormal, fmt.Sprintf("%12s%3d%9s%16d",
		"STRENGTH", c.Strength, "GOLD ", c.Gold))
	s.DrawString(0, 3, styleNormal, fmt.Sprintf("%12s%3d%9s%16d",
		"I.Q.", c.IQ, "EXP ", c.XP))
	s.DrawString(0, 4, styleNormal, fmt.Sprintf("%12s%3d",
		"PIETY", c.Piety))
	s.DrawString(0, 5, styleNormal, fmt.Sprintf("%12s%3d%9s%3d%9s%3d",
		"VITALITY", c.Vitality, "LEVEL ", c.Level, "AGE ", c.Age/52))
	s.DrawString(0, 6, styleNormal, fmt.Sprintf("%12s%3d%9s%3d/%3d%4s%4d",
		"AGILITY", c.Agility, "HITS ", c.HP, c.MaxHP, "AC", c.AC))
	s.DrawString(0, 7, styleNormal, fmt.Sprintf("%12s%3d%9s%s",
		"LUCK", c.Luck, "STATUS ", c.Status))

	// Row 9: MAGE — GOTOXY(0,9), WRITECH(' ',7) + " MAGE " + slots
	mageSlots := fmt.Sprintf("%d/%d/%d/%d/%d/%d/%d",
		c.MageSpells[0], c.MageSpells[1], c.MageSpells[2], c.MageSpells[3],
		c.MageSpells[4], c.MageSpells[5], c.MageSpells[6])
	s.DrawString(0, 9, styleNormal, fmt.Sprintf("%7s MAGE %s", "", mageSlots))

	// Row 10: PRIEST — WRITECH(' ',6) + "PRIEST " + slots
	priestSlots := fmt.Sprintf("%d/%d/%d/%d/%d/%d/%d",
		c.PriestSpells[0], c.PriestSpells[1], c.PriestSpells[2], c.PriestSpells[3],
		c.PriestSpells[4], c.PriestSpells[5], c.PriestSpells[6])
	s.DrawString(0, 10, styleNormal, fmt.Sprintf("%6sPRIEST %s", "", priestSlots))

	// Row 12: equipment legend — GOTOXY(0,12)
	s.DrawString(0, 12, styleNormal, "*=EQUIP, -=CURSED, ?=UNKNOWN, #=UNUSABLE")

	// Rows 14-17: items in TWO COLUMNS from flat Items array
	// From p-code: col = 20 - 20*(i%2), row = 14 + (i-1)/2
	// Legend: *=EQUIP, -=CURSED, ?=UNKNOWN, #=UNUSABLE
	for i := 0; i < c.ItemCount; i++ {
		poss := c.Items[i]
		if poss.ItemIndex < 0 || poss.ItemIndex >= len(items) {
			continue
		}
		item := items[poss.ItemIndex]
		marker := byte(' ')
		// #=UNUSABLE: class can't equip this item
		if item.ClassUse != 0 && (item.ClassUse&(1<<uint(c.Class))) == 0 {
			marker = '#'
		}
		if poss.Equipped {
			marker = '*'
		}
		if poss.Cursed && poss.Equipped {
			marker = '-'
		}
		if !poss.Identified {
			marker = '?'
		}
		itemNum := i + 1
		col := 20 - 20*(itemNum%2)
		row := 14 + (itemNum-1)/2
		s.DrawString(col, row, styleNormal, fmt.Sprintf("%d)%c%s", itemNum, marker, item.Name))
	}

	// Row 18: menu/prompt — GOTOXY(0,18)
	switch game.Town.InputMode {
	case engine.InputDrop:
		// From p-code byte 2667
		s.DrawString(0, 18, styleNormal, "DROP ITEM (0=EXIT) ? >")
	case engine.InputTrade:
		// p-code: WIZARDRY proc 15 displays prompt + " ([RETURN] EXITS) >"
		// then shows party members in two-column layout at rows 20-22
		s.DrawString(0, 18, styleNormal, "TRADE WITH ([RETURN] EXITS) >")
		for i, m := range game.Town.Party.Members {
			if m != nil {
				col := 20 * (i % 2)
				row := 20 + i/2
				s.DrawString(col+1, row, styleNormal, fmt.Sprintf("%d) %s", i+1, m.Name))
			}
		}
	case engine.InputCastSpell:
		// p-code CAMP IC 1939: "WHAT SPELL ? >"
		prompt := fmt.Sprintf("WHAT SPELL ? >%s", game.Town.InputBuf)
		s.DrawString(0, 18, styleNormal, prompt)
		s.DrawString(len(prompt), 18, styleGold, "_")
	case engine.InputSpellTarget:
		// p-code CAMP proc 29 (IC 708): "CAST ON WHO" + WIZARDRY.proc15 party list
		s.DrawString(0, 18, styleNormal, "CAST ON WHO ([RETURN] EXITS) >")
		for i, m := range game.Town.Party.Members {
			if m != nil {
				col := 20 * (i % 2)
				row := 20 + i/2
				s.DrawString(col+1, row, styleNormal, fmt.Sprintf("%d) %s", i+1, m.Name))
			}
		}
	case engine.InputUseItem:
		// p-code CAMP IC 2399: "USE ITEM (0=EXIT) ? >"
		s.DrawString(0, 18, styleNormal, "USE ITEM (0=EXIT) ? >")
	case engine.InputTradeGold:
		// From p-code byte 3029: gold amount entry
		s.DrawString(0, 18, styleNormal, fmt.Sprintf("AMT OF GOLD ? >%s", game.Town.InputBuf))
		s.DrawString(15+len(game.Town.InputBuf), 18, styleGold, "_")
	case engine.InputTradeTarget:
		// From p-code byte 3293: then select item
		s.DrawString(0, 18, styleNormal, "WHAT ITEM ([RET] EXITS) ? >")
	default:
		if c.Status >= engine.Dead {
			// p-code IC 4815: dead character — single line
			s.DrawString(0, 18, styleNormal, "YOU MAY R)EAD SPELL BOOKS OR L)EAVE.")
		} else if game.Phase == engine.PhaseCamp {
			// p-code IC 4481-4668: camp/maze inspect — full 4-line menu
			s.DrawString(0, 18, styleNormal, "YOU MAY E)QUIP, D)ROP AN ITEM, T)RADE,")
			s.DrawString(0, 19, styleNormal, "        R)EAD SPELL BOOKS, CAST S)PELLS,")
			s.DrawString(0, 20, styleNormal, "        U)SE AN ITEM, I)DENTIFY AN ITEM,")
			s.DrawString(0, 21, styleNormal, "        OR L)EAVE.")
		} else {
			// p-code IC 4699-4802: town inspect — 2-line menu
			s.DrawString(0, 18, styleNormal, "YOU MAY E)QUIP, D)ROP AN ITEM, T)RADE,")
			s.DrawString(0, 19, styleNormal, "        R)EAD SPELL BOOKS, OR L)EAVE.")
		}
	}

	if game.Town.Message != "" {
		msg := game.Town.Message
		col := (40 - len(msg)) / 2
		if col < 0 {
			col = 0
		}
		s.DrawString(col, 23, styleGold, msg)
	}
}

// renderEquipScreen draws the category-based equip selection screen.
// From UTILITIE segment p-code bytes 5466-6194:
// Clears screen, shows "SELECT <CATEGORY> FOR <NAME>", lists matching items.
func (s *Screen) renderEquipScreen(game *engine.GameState) {
	town := game.Town
	c := town.EditChar
	items := game.Scenario.Items

	catIdx := town.EquipCategory
	if catIdx >= len(engine.EquipCategories) {
		return
	}
	cat := engine.EquipCategories[catIdx]
	catName := ""
	if cat < len(engine.EquipCategoryNames) {
		catName = engine.EquipCategoryNames[cat]
	}

	// Row 0: "SELECT <CATEGORY> FOR <NAME>"
	s.DrawString(0, 0, styleNormal, fmt.Sprintf("SELECT %s FOR %s", catName, c.Name))

	// List matching items with selection numbers
	y := 2
	for i, pos := range town.EquipChoices {
		if pos < 0 || pos >= c.ItemCount {
			continue
		}
		poss := c.Items[pos]
		if poss.ItemIndex < 0 || poss.ItemIndex >= len(items) {
			continue
		}
		item := items[poss.ItemIndex]

		// Marker: '-' if cursed, '?' if unidentified, ' ' otherwise
		marker := byte(' ')
		if poss.Cursed {
			marker = '-'
		}
		if !poss.Identified {
			marker = '?'
		}

		// Display: "  1) LONG SWORD" or "  2)-CURSED BLADE"
		displayName := item.Name
		if !poss.Identified {
			displayName = item.NameUnknown
			if displayName == "" {
				displayName = item.Name
			}
		}
		s.DrawString(0, y, styleNormal,
			fmt.Sprintf("          %d)%c%s", i+1, marker, displayName))
		y++
	}

	// Prompt
	y += 1
	s.DrawString(0, y, styleNormal, "WHICH ONE ? ( [RET] FOR NONE )")

	if town.Message != "" {
		s.DrawString(0, y+2, styleGold, town.Message)
	}
}

// renderSpellBooksScreen draws the spell book selection screen.
func (s *Screen) renderSpellBooksScreen(game *engine.GameState) {
	c := game.Town.EditChar

	s.DrawString(0, 0, styleNormal, fmt.Sprintf("MAGE  SPELLS LEFT = %d/%d/%d/%d/%d/%d/%d",
		c.MageSpells[0], c.MageSpells[1], c.MageSpells[2], c.MageSpells[3],
		c.MageSpells[4], c.MageSpells[5], c.MageSpells[6]))
	s.DrawString(0, 1, styleNormal, fmt.Sprintf("PRIEST SPELLS LEFT = %d/%d/%d/%d/%d/%d/%d",
		c.PriestSpells[0], c.PriestSpells[1], c.PriestSpells[2], c.PriestSpells[3],
		c.PriestSpells[4], c.PriestSpells[5], c.PriestSpells[6]))

	s.DrawString(0, 3, styleNormal, "YOU MAY SEE M)AGE OR P)RIEST SPELL BOOKS")
	s.DrawString(12, 4, styleNormal, "OR L)EAVE.")
}

// renderSpellListScreen draws the list of known spells.
// Only shows spells at levels where the character has spell slots.
func (s *Screen) renderSpellListScreen(game *engine.GameState) {
	town := game.Town
	c := town.EditChar

	var title string
	var spellsByLevel [7][]*engine.Spell
	var slots [7]int
	if town.InspectSpells == engine.MageSpell {
		title = "KNOWN MAGE SPELLS"
		spellsByLevel = engine.MageSpellsByLevel
		slots = c.MaxMageSpells
	} else {
		title = "KNOWN PRIEST SPELLS"
		spellsByLevel = engine.PriestSpellsByLevel
		slots = c.MaxPriestSpells
	}

	s.DrawString(0, 0, styleNormal, title)

	row := 2
	for level := 0; level < 7; level++ {
		if slots[level] <= 0 {
			continue
		}
		spells := spellsByLevel[level]
		for _, sp := range spells {
			s.DrawString(0, row, styleNormal, sp.Name)
			row++
		}
		row++ // blank line between levels
	}

	s.DrawString(0, 23, styleNormal, "L)EAVE WHEN READY")
}

func (s *Screen) renderTrainingMenu(x, y int, game *engine.GameState) {
	town := game.Town

	// Exact text from p-code ROLLER segment
	s.DrawString(x, y, styleNormal, "YOU MAY ENTER A CHARACTER NAME TO ADD,")
	y++
	s.DrawString(x, y, styleNormal, "        INSPECT OR EDIT,")
	y++
	s.DrawString(x, y, styleNormal, "        \"*ROSTER\" TO SEE ROSTER,")
	y++
	s.DrawString(x, y, styleNormal, "OR PRESS [RET] FOR CASTLE.")

	y += 2
	s.DrawString(x, y, styleNormal, "NAME >")
	if town.InputMode == engine.InputTrainingName {
		s.DrawString(x+6, y, styleNormal, town.InputBuf)
		s.DrawString(x+6+len(town.InputBuf), y, styleGold, "_")
	} else {
		s.DrawString(x+6, y, styleGold, "_")
	}

	if town.Message != "" {
		y += 2
		s.DrawString(x, y, styleGold, town.Message)
	}
}

// renderMalorScreen draws the MALOR teleport displacement UI.
// From Pascal UTILITIE.TEXT lines 432-473.
func (s *Screen) renderMalorScreen(game *engine.GameState) {
	s.Clear()
	s.ClearSixelTransition()

	town := game.Town
	y := 0
	s.DrawString(0, y, styleTitle, "PARTY TELEPORT:")
	y += 2
	s.DrawString(0, y, styleNormal, "ENTER NSEWU OR D TO  SET DISPLACEMENT,")
	y++
	s.DrawString(0, y, styleNormal, "THEN [RETURN] TO TELEPORT, OR [ESC] TO")
	y++
	s.DrawString(0, y, styleNormal, "CHICKEN OUT!")
	y += 2
	s.DrawString(0, y, styleNormal, fmt.Sprintf("# SQUARES EAST  =%4d", town.MalorDeltaEW))
	y++
	s.DrawString(0, y, styleNormal, fmt.Sprintf("# SQUARES NORTH =%4d", town.MalorDeltaNS))
	y++
	s.DrawString(0, y, styleNormal, fmt.Sprintf("# SQUARES DOWN  =%4d", town.MalorDeltaUD))
}

// renderDumapicScreen draws the DUMAPIC full-screen location display.
// From Pascal UTILITIE.TEXT p-code IC 1872-2314:
// Clear screen, show party location/facing/coordinates, "L)EAVE WHEN READY".
func (s *Screen) renderDumapicScreen(game *engine.GameState) {
	s.Clear()
	s.ClearSixelTransition()

	dirNames := [4]string{"NORTH.", "EAST.", "SOUTH.", "WEST."}

	y := 0
	s.DrawString(0, y, styleTitle, "PARTY LOCATION:")
	y += 2
	s.DrawString(0, y, styleNormal, "THE PARTY IS FACING "+dirNames[game.Facing])
	y += 2
	s.DrawString(0, y, styleNormal, fmt.Sprintf("YOU ARE %d SQUARES EAST AND", game.PlayerX))
	y++
	s.DrawString(0, y, styleNormal, fmt.Sprintf("%d SQUARES NORTH OF THE STAIRS", game.PlayerY))
	y++
	s.DrawString(0, y, styleNormal, fmt.Sprintf("TO THE CASTLE, AND %d LEVELS", game.MazeLevel+1))
	y++
	s.DrawString(0, y, styleNormal, "BELOW IT.")
	y += 2
	s.DrawString(0, y, styleNormal, "L)EAVE WHEN READY")
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
