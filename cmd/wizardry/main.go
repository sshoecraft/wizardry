package main

import (
	"fmt"
	"math/rand"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"wizardry/data"
	"wizardry/engine"
	"wizardry/render"
	"wizardry/scenarios/wiz1"
	"wizardry/scenarios/wiz2"
	"wizardry/scenarios/wiz3"
)

var version = "1.2.1"
var buildDate string // set via ldflags: -ldflags "-X main.buildDate=15-APR-26"

func main() {
	scenarioName := "1"
	vpScale := 1.0 // --vpscale: viewport scale (1.0=100%, 1.5=150%, etc.)
	forceColor := false
	forceGreen := false

	for i := 1; i < len(os.Args); i++ {
		arg := os.Args[i]
		if arg == "--color" {
			forceColor = true
		} else if arg == "--green" {
			forceGreen = true
		} else if strings.HasPrefix(arg, "--vpscale=") {
			val := strings.TrimPrefix(arg, "--vpscale=")
			if v, err := strconv.ParseFloat(val, 64); err == nil {
				if v < 1.0 {
					v = 1.0
				}
				vpScale = v
			}
		} else if arg == "--vpscale" && i+1 < len(os.Args) {
			i++
			if v, err := strconv.ParseFloat(os.Args[i], 64); err == nil {
				if v < 1.0 {
					v = 1.0
				}
				vpScale = v
			}
		} else if strings.HasPrefix(arg, "--scenario=") {
			scenarioName = strings.TrimPrefix(arg, "--scenario=")
		} else if arg == "--scenario" && i+1 < len(os.Args) {
			i++
			scenarioName = os.Args[i]
		}
	}

	scenario, err := loadScenario(scenarioName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading %s: %v\n", scenarioName, err)
		os.Exit(1)
	}

	game := engine.New(scenario)

	// Set version info for title screen display
	// Strip trailing ".0" for display (1.0.0 → 1.0, 1.1.0 → 1.1, 1.1.1 → 1.1.1)
	displayVersion := strings.TrimSuffix(version, ".0")
	game.Version = displayVersion
	if buildDate == "" {
		// Not set via ldflags — use current date
		buildDate = strings.ToUpper(time.Now().Format("02-Jan-06"))
	}
	game.BuildDate = buildDate

	game.Load() // restore roster/party from ~/.config/wizardry/

	// Recalculate AC for all characters from equipped items
	for _, c := range game.Town.Roster.Characters {
		if c != nil {
			recalcAC(c, game.Scenario.Items)
		}
	}

	// Detect sixel support BEFORE tcell takes over the terminal.
	// DetectSixel sends DA1 (ESC[c) and checks for attribute 4.
	render.SixelSupported = render.DetectSixel()

	screen, err := render.NewScreen()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Screen init failed: %v\n", err)
		os.Exit(1)
	}
	defer screen.Close()

	screen.VPScale = vpScale

	// Color mode: --green forces monochrome phosphor, --color forces color,
	// otherwise auto-detect based on terminal color support (256+ colors).
	if forceGreen {
		render.ColorMode = false
	} else if forceColor {
		render.ColorMode = true
	} else {
		render.ColorMode = screen.Colors() >= 256
	}
	if render.ColorMode {
		render.ApplyColorMode()
	}

	// Detect cell pixel size via ioctl TIOCGWINSZ — safe after tcell init,
	// no escape sequences, no stdin reads.
	if render.SixelSupported {
		render.CellWidth, render.CellHeight = render.DetectCellSize()
	}

	// Check terminal size — need at least 80x24 (40 logical cols × 2 scale)
	w, h := screen.Size()
	if w < 80 || h < 24 {
		screen.Close()
		fmt.Fprintf(os.Stderr, "Terminal too small: %dx%d. Minimum required: 80x24.\n", w, h)
		os.Exit(1)
	}

	redraw(screen, game)

	// Run the timed title text + animated art sequence in background (Wiz 1 only)
	// Wiz 2 has static title image (no animation), Wiz 3 has no title at all
	if game.Phase == engine.PhaseTitle && game.Scenario.TitleWT != nil {
		game.Title.Done = make(chan struct{})
		go startTitleSequence(screen, game)
	}

	// Use channel-based events for non-blocking timer support.
	// Combat messages auto-advance per p-code WIZARDRY.proc30 timed wait.
	quit := make(chan struct{})
	defer close(quit)
	eventCh := make(chan tcell.Event, 10)
	go func() {
		for {
			ev := screen.PollEvent()
			if ev == nil {
				return
			}
			eventCh <- ev
		}
	}()

	// Combat auto-advance poll timer.
	// Original Apple II PAUSE1 uses the T)IME delay value (global 7, range 1-5000)
	// as a busy-wait loop count. We map it to milliseconds: delay_ms = MazeDelay * 0.3.
	// Default MazeDelay=0 uses 1500ms. Poll every 100ms, advance when enough time elapsed.
	combatTicker := time.NewTicker(100 * time.Millisecond)
	defer combatTicker.Stop()
	var lastCombatAdvance time.Time

	// Story auto-advance timer: 6 seconds per page (Wiz 3 title sequence)
	// Original Apple II auto-advances with no input required
	storyTicker := time.NewTicker(6 * time.Second)
	defer storyTicker.Stop()

	// Inn healing timer: ~0.7 seconds per heal tick
	// Pascal HEALHP (CASTLE2.TEXT line 445): FOR PAUSEX := 1 TO 500 DO ;
	// On 1 MHz Apple II with p-code interpreter, this is roughly 0.5-1 second.
	innTicker := time.NewTicker(700 * time.Millisecond)
	defer innTicker.Stop()

	for {
		select {
		case ev := <-eventCh:
			switch ev := ev.(type) {
			case *tcell.EventResize:
				redraw(screen, game)

			case *tcell.EventKey:
				if ev.Key() == tcell.KeyCtrlC {
					return
				}

				switch game.Phase {
				case engine.PhaseTitle:
					handleTitleInput(screen, game, ev)
				case engine.PhaseTown:
					if handleTownInput(game, ev) {
						return
					}
				case engine.PhaseCamp:
					handleCampInput(game, ev)
				case engine.PhaseMaze:
					if handleMazeInput(screen, game, ev) {
						return
					}
				case engine.PhaseCombat:
					handleCombatInput(game, ev)
				case engine.PhaseUtilities:
					handleUtilInput(game, ev)
				case engine.PhaseCreation:
					handleCreationInput(game, ev)
				}
				game.Save() // auto-save after every action
				redraw(screen, game)
			}

		case <-combatTicker.C:
			if game.Phase == engine.PhaseCombat && game.Combat != nil {
				combat := game.Combat

				// Compute delay based on combat phase.
				// PAUSE1 (CombatInit/CombatExecute): T)IME-controlled (MazeDelay 1-5000 → ms).
				// PAUSE2 (CombatChestResult/CombatVictory/CombatDefeat): fixed delay —
				//   Pascal PAUSE2 hardcodes 3000, independent of T)IME setting.
				delayMs := 1500 // default (PAUSE2 fixed, and PAUSE1 with no T)IME set)
				if combat.Phase == engine.CombatInit || combat.Phase == engine.CombatExecute {
					// PAUSE1: uses T)IME setting
					if game.MazeDelay > 0 {
						delayMs = game.MazeDelay * 3 / 10 // ~0.3ms per unit
						if delayMs < 50 {
							delayMs = 50 // minimum 50ms
						}
					}
				}
				elapsed := time.Since(lastCombatAdvance)
				if elapsed < time.Duration(delayMs)*time.Millisecond {
					continue // not enough time yet
				}

				if combat.Phase == engine.CombatInit {
					// Auto-advance after delay (simulates Apple II disk I/O time)
					lastCombatAdvance = time.Now()
					if combat.Surprised == 2 {
						combat.ExecuteRound(game)
					} else {
						combat.Phase = engine.CombatChoose
						combat.CurrentActor = findNextActor(game, -1)
					}
					redraw(screen, game)
				} else if combat.Phase == engine.CombatExecute && !combat.HamanSelecting {
					lastCombatAdvance = time.Now()
					advanceCombatMessages(game)
					redraw(screen, game)
				} else if combat.Phase == engine.CombatChestResult {
					lastCombatAdvance = time.Now()
					advanceChestMessages(game)
					redraw(screen, game)
				} else if combat.Phase == engine.CombatVictory {
					lastCombatAdvance = time.Now()
					if !combat.ChestPauseUsed {
						combat.ChestPauseUsed = true
					} else {
						game.Combat = nil
						game.Phase = engine.PhaseMaze
						game.MazeMessage = ""
						game.MazeMessage2 = ""
					}
					redraw(screen, game)
				} else if combat.Phase == engine.CombatDefeat {
					lastCombatAdvance = time.Now()
					if !combat.ChestPauseUsed {
						combat.ChestPauseUsed = true
					} else {
						game.Combat = nil
						game.Phase = engine.PhaseTown
						game.Town.Location = engine.Castle
						game.Town.Message = "YOUR PARTY HAS PERISHED IN THE MAZE..."
					}
					redraw(screen, game)
				}
			}

		case <-storyTicker.C:
			// Auto-advance Wiz 3 story pages every 6 seconds (no input required)
			if game.Phase == engine.PhaseTitle && game.Title != nil &&
				game.Title.Step == engine.TitleStory {
				title := game.Title
				title.StoryFrame++
				totalFrames := len(game.Scenario.TitleFrames)
				if totalFrames == 0 {
					totalFrames = len(game.Scenario.TitleStory)
				}
				if title.StoryFrame >= totalFrames {
					// No keypress through entire sequence → loop back to start
					// (p-code: cycling loop at offsets 855-871 in TITLELOA)
					title.StoryFrame = 0
				}
				redraw(screen, game)
			}

		case <-innTicker.C:
			// Inn healing animation — Pascal HEALHP loop (CASTLE2.TEXT lines 423-448)
			// Each tick: heal HPADD HP, deduct gold, redraw. When done → level-up screen.
			// Pascal delay: FOR PAUSEX := 1 TO 500 DO ; (~0.5-1s on Apple II)
			if game.Phase == engine.PhaseTown && game.Town.Location == engine.Inn &&
				game.Town.InnStep == engine.InnHealing && game.Town.InnChar != nil {
				c := game.Town.InnChar
				town := game.Town
				if town.InnHealAmt == 0 {
					// Stables: no healing animation, go straight to level-up
					innTransitionToLevelUp(game)
					redraw(screen, game)
				} else if c.HP < c.MaxHP && c.Gold >= town.InnHealCost {
					// One heal step per tick
					c.HP += town.InnHealAmt
					if c.HP > c.MaxHP {
						c.HP = c.MaxHP
					}
					c.Gold -= town.InnHealCost
					redraw(screen, game)
				} else {
					// Healing done — transition to level-up
					innTransitionToLevelUp(game)
					redraw(screen, game)
				}
			}
		}
	}
}

func redraw(screen *render.Screen, game *engine.GameState) {
	switch game.Phase {
	case engine.PhaseTitle:
		screen.RenderTitle(game)
	case engine.PhaseTown:
		screen.RenderTown(game)
	case engine.PhaseCamp:
		screen.RenderCamp(game)
	case engine.PhaseMaze:
		if game.ShowMap {
			screen.RenderMap(game)
		} else {
			screen.RenderMaze(game)
		}
	case engine.PhaseCombat:
		screen.RenderCombat(game)
	case engine.PhaseUtilities:
		screen.RenderUtilities(game)
	case engine.PhaseCreation:
		screen.RenderCreation(game.Town.Creation)
	}
}

// startTitleSequence runs the timed text intro then wizard art.
// From p-code TITLELOA (SYSTEM.STARTUP seg 2):
//
//	GOTOXY(12,10) "PREPARE YOURSELF"  → CLP 5 delay(150)
//	GOTOXY(12,12) "FOR THE ULTIMATE"  → CLP 5 delay(150)
//	GOTOXY(12,14) "IN FANTASY GAMES"  → CLP 5 delay(500)
//	→ soft switches to Hi-Res → wizard art animation
//	→ falls through to OPTIONS (seg 3) = menu screen
//
// Any key during text or art skips immediately to the menu.
// Delays scaled up from p-code values (Apple II 1MHz made them feel longer).
func startTitleSequence(screen *render.Screen, game *engine.GameState) {
	if game.Title != nil && game.Title.Done != nil {
		defer close(game.Title.Done)
	}
	skipped := func() bool {
		return game.Title == nil || game.Title.Skipped
	}
	delay := func(d time.Duration) bool {
		time.Sleep(d)
		return skipped()
	}

	// Create WT animation engine if we have the data
	var anim *render.WTAnimation
	if game.Scenario.TitleWT != nil {
		anim = render.NewWTAnimation(game.Scenario.TitleWT)
	}

	// Phase 1: timed text intro
	// From Pascal TITLELOA: GOTOXY/WRITESTR with CHKEYPR delays
	delays := []time.Duration{
		800 * time.Millisecond,
		800 * time.Millisecond,
		1500 * time.Millisecond,
	}
	for i := 0; i < 3; i++ {
		if skipped() {
			return
		}
		game.Title.TextLine = i
		redraw(screen, game)
		if delay(delays[i]) {
			return
		}
	}

	if skipped() {
		return
	}
	game.Title.Step = engine.TitleArt

	// Store animation engine on the title state for the renderer to use
	game.Title.Anim = anim

	// Animation frame rate
	frameDur := 80 * time.Millisecond

	// LZFLAG alternates between XOR toggle and scroll on each loop
	lzflag := 0xFD

	for {
		if skipped() {
			return
		}

		if anim == nil {
			// Fallback: static bitmap reveal (no WT data)
			game.Title.AnimRow = 0
			redraw(screen, game)
			if delay(5 * time.Second) {
				return
			}
			continue
		}

		// ── Phase 2: Clear and draw base scene (section 0) ──
		// Pascal: LZDECOMP(WTBUFF[0], WTBUFF[8]); FILLCHAR($2000, 8192, 0)
		anim.DrawSection(8)
		anim.ClearHires()

		// ── Phase 3: Draw base fire scene + animate fire (20 frames) ──
		// Pascal: LZDECOMP(WTBUFF[0], WTBUFF[0]); FOR MP02:=1 TO 20 DO P070206
		anim.DrawSection(0)
		mp04 := 0
		mp05 := 4

		emitFrame := func() {
			if skipped() {
				return
			}
			if render.SixelSupported {
				anim.EmitSixelFrame()
				screen.MarkSixel()
			} else {
				screen.Clear()
				anim.EmitCanvasFrame(screen, render.BaseStyle)
				screen.Show()
			}
		}

		emitFrame()
		for i := 0; i < 20; i++ {
			if skipped() {
				return
			}
			time.Sleep(frameDur)
			mp04 = 1 + (mp04 % 3) // cycles 1,2,3,1,2,3...
			anim.DrawSection(mp04)
			emitFrame()
		}

		// ── Phase 4: Fire + magic waves (24 rounds of 2 fire + 1 wave) ──
		for i := 0; i < 24; i++ {
			if skipped() {
				return
			}
			for j := 0; j < 2; j++ {
				time.Sleep(frameDur)
				mp04 = 1 + (mp04 % 3)
				anim.DrawSection(mp04)
				emitFrame()
			}
			mp05 = 5 + ((mp05 - 4) % 3) // cycles 5,6,7,5,6,7...
			anim.DrawSection(mp05)
			emitFrame()
		}

		// ── Phase 5: Scene reset + smoke/dragon reveal (sections 9-23) ──
		anim.DrawSection(8) // reset scene
		mp04 = 1 + (mp04 % 3)
		anim.DrawSection(mp04)
		emitFrame()

		for sec := 9; sec <= 23; sec++ {
			if skipped() {
				return
			}
			for j := 0; j < 2; j++ {
				// Increasing delay: 5+sec iterations in original
				time.Sleep(frameDur + time.Duration(sec*4)*time.Millisecond)
				mp04 = 1 + (mp04 % 3)
				anim.DrawSection(mp04)
				emitFrame()
			}
			anim.DrawSection(sec)
			emitFrame()
		}

		// ── Phase 6: Title text reveal (sections 24-32) ──
		for sec := 24; sec <= 32; sec++ {
			if skipped() {
				return
			}
			for j := 0; j < 4; j++ {
				time.Sleep(frameDur)
				mp04 = 1 + (mp04 % 3)
				anim.DrawSection(mp04)
				emitFrame()
			}
			anim.DrawSection(sec)
			emitFrame()
		}

		// ── Phase 7: Hold with fire (40 frames) ──
		for i := 0; i < 40; i++ {
			if skipped() {
				return
			}
			time.Sleep(frameDur)
			mp04 = 1 + (mp04 % 3)
			anim.DrawSection(mp04)
			emitFrame()
		}

		// ── Phase 8: Scroll/flash effect (80 frames) ──
		for i := 0; i < 80; i++ {
			if skipped() {
				return
			}
			if lzflag == 0xFD {
				anim.XorToggle()
			} else {
				anim.ScrollLeft()
			}
			time.Sleep(60 * time.Millisecond)
			mp04 = 1 + (mp04 % 3)
			anim.DrawSection(mp04)
			emitFrame()
		}

		// ── Phase 9: Final hold (40 frames) ──
		for i := 0; i < 40; i++ {
			if skipped() {
				return
			}
			time.Sleep(frameDur)
			mp04 = 1 + (mp04 % 3)
			anim.DrawSection(mp04)
			emitFrame()
		}

		// Toggle LZFLAG for next loop iteration
		if lzflag == 0xFD {
			lzflag = 0xFE
		} else {
			lzflag = 0xFD
		}

		// Loop back: switch to text mode for PREPARE YOURSELF, then re-enter art
		if skipped() {
			return
		}
		game.Title.Step = engine.TitleText
		game.Title.TextLine = -1
		for i := 0; i < 3; i++ {
			if skipped() {
				return
			}
			game.Title.TextLine = i
			redraw(screen, game)
			if delay(delays[i]) {
				return
			}
		}
		if skipped() {
			return
		}
		game.Title.Step = engine.TitleArt
	}
}

// handleTitleInput processes keyboard input during the title screen.
// From p-code: any key during text or art → skip to menu (OPTIONS seg 3).
// At menu: S=start game, U=utilities (not yet), T=show title art again.
func handleTitleInput(screen *render.Screen, game *engine.GameState, ev *tcell.EventKey) {
	title := game.Title
	if title == nil {
		game.Phase = engine.PhaseTown
		return
	}

	switch title.Step {
	case engine.TitleStory:
		// Any keypress exits the story sequence → menu (p-code: CSP 4 XIT in proc 3)
		title.Step = engine.TitleMenu
		if render.SixelSupported {
			fmt.Fprintf(os.Stdout, "\x1b[2J")
			os.Stdout.Sync()
		}

	case engine.TitleText, engine.TitleArt:
		// Any key skips to the menu — from p-code: keypress exits title sequence
		title.Skipped = true
		// Wait for animation goroutine to exit so it can't write more sixel frames
		if title.Done != nil {
			<-title.Done
			title.Done = nil
		}
		title.Step = engine.TitleMenu
		// Force clear sixel layer — animation left residual sixel content
		if render.SixelSupported {
			fmt.Fprintf(os.Stdout, "\x1b[2J")
			os.Stdout.Sync()
		}

	case engine.TitleMenu:
		// From p-code OPTIONS (seg 3): wait for S, U, or T
		if ev.Key() != tcell.KeyRune {
			return
		}
		ch := ev.Rune()
		if ch >= 'a' && ch <= 'z' {
			ch -= 32
		}
		switch ch {
		case 'S': // S)TART GAME → enter castle
			game.Title = nil
			game.Phase = engine.PhaseTown
		case 'T': // T)ITLE PAGE → re-show title art/story
			if len(game.Scenario.TitleFrames) > 0 || len(game.Scenario.TitleStory) > 0 {
				// Wiz 3: restart the story sequence — clear screen first
				// to prevent menu text bleeding through sixel frames
				screen.Clear()
				screen.Show()
				title.Step = engine.TitleStory
				title.StoryFrame = 0
				return
			}
			if game.Scenario.Title == nil {
				return // no title art
			}
			if game.Scenario.TitleWT != nil {
				// Wiz 1: full text + animated art sequence
				title.Step = engine.TitleText
				title.TextLine = -1
				title.Skipped = false
				title.Done = make(chan struct{})
				go startTitleSequence(screen, game)
			} else {
				// Wiz 2: static title image (no animation)
				title.Step = engine.TitleArt
				title.AnimRow = 0
			}
		case 'U': // U)TILITIES — from p-code WIZBOOT offset 228: CXP seg=7 proc=1
			game.Util = engine.NewUtilState()
			game.Phase = engine.PhaseUtilities
		}
	}
}

// handleTownInput processes keyboard input during town phase.
// Returns true if the game should exit.
func handleTownInput(game *engine.GameState, ev *tcell.EventKey) bool {
	town := game.Town

	// If we're in a text input prompt, handle that first
	if town.InputMode != engine.InputNone {
		return handleTownPrompt(game, ev)
	}

	// [RETURN] = leave (context-dependent)
	if ev.Key() == tcell.KeyEnter || ev.Key() == tcell.KeyEscape {
		if town.Location == engine.Inn {
			handleInnReturn(town)
			return false
		}
		if town.Location == engine.Trading {
			handleShopReturn(town)
			return false
		}
		if town.Location == engine.Temple {
			handleTempleReturn(game, town)
			return false
		}
		if town.Location != engine.Castle {
			goToCastle(town)
			return false
		}
	}

	if ev.Key() != tcell.KeyRune {
		return false
	}

	ch := ev.Rune()
	if ch >= 'a' && ch <= 'z' {
		ch -= 32
	}
	town.Message = ""

	switch town.Location {
	case engine.Castle:
		// From p-code: A=Inn, G=Tavern, B=Boltac's, C=Temple, E=Edge
		// Inn/Boltac's/Temple require a party (from original game flow)
		switch ch {
		case 'G':
			town.Location = engine.Tavern
		case 'A':
			if town.Party.Size() == 0 {
				town.Message = "YOU HAVE NO PARTY!"
			} else {
				town.Location = engine.Inn
				town.InnStep = engine.InnWho
				town.InnChar = nil
				town.InnMessages = nil
			}
		case 'B':
			if town.Party.Size() == 0 {
				town.Message = "YOU HAVE NO PARTY!"
			} else {
				town.Location = engine.Trading
				town.ShopStep = engine.ShopWho
				town.ShopChar = nil
				town.ShopCatalog = 0
			}
		case 'C':
			if town.Party.Size() == 0 {
				town.Message = "YOU HAVE NO PARTY!"
			} else {
				town.Location = engine.Temple
				town.TempleStep = engine.TempleWho
				town.TempleChar = nil
				town.TempleCost = 0
				town.TempleMessages = nil
				town.InputMode = engine.InputTempleHelp
				town.InputBuf = ""
			}
		case 'E':
			town.Location = engine.EdgeOfTown
		case 'Q':
			return true
		}

	case engine.Tavern:
		switch ch {
		case 'A': // Add member
			if town.Party.Size() >= 6 {
				town.Message = "** PARTY IS FULL **"
			} else {
				town.InputMode = engine.InputAddMember
				town.InputBuf = ""
			}
		case 'R': // Remove member — "WHO WILL LEAVE" (CASTLE byte 1619)
			if town.Party.Size() > 0 {
				town.InputMode = engine.InputRemoveMember
				town.Message = ""
			}
		case '1', '2', '3', '4', '5', '6': // #) SEE A MEMBER — full inspect screen
			idx := int(ch-'0') - 1
			if idx < len(town.Party.Members) && town.Party.Members[idx] != nil {
				town.EditChar = town.Party.Members[idx]
				town.InputMode = engine.InputInspect
				town.Message = ""
			}
		}

	case engine.Inn:
		handleInnInput(game, town, ch)

	case engine.Trading:
		handleShopInput(game, town, ch)

	case engine.Temple:
		handleTempleInput(game, town, ch)

	case engine.EdgeOfTown:
		// From p-code: M/T/C/L when party exists, T/C/L when no party
		switch ch {
		case 'M': // Enter maze (only with party)
			if town.Party.Size() > 0 {
				game.Phase = engine.PhaseCamp
				game.MazeLevel = 0
				game.PlayerX = 0
				game.PlayerY = 0
				game.Facing = engine.North
				game.MazeMessage = ""
				game.MazeMessage2 = ""
				game.InitFightMap()
			}
		case 'T': // Training Grounds
			town.Location = engine.Training
		case 'C': // Castle
			goToCastle(town)
		case 'L': // Leave game
			return true
		}

	case engine.Training:
		// Training Grounds is always in name-input mode
		// Any letter starts typing into the name field
		town.InputMode = engine.InputTrainingName
		town.InputBuf = string(ch)
	}
	return false
}

// handleTownPrompt handles text input at a "WHO WILL JOIN ? >" style prompt.
func handleTownPrompt(game *engine.GameState, ev *tcell.EventKey) bool {
	town := game.Town

	// Tavern password check — from CASTLE proc 30 (IC 1416-1468)
	if town.InputMode == engine.InputTavernPassword {
		switch ev.Key() {
		case tcell.KeyBackspace, tcell.KeyBackspace2:
			if len(town.InputBuf) > 0 {
				town.InputBuf = town.InputBuf[:len(town.InputBuf)-1]
			}
		case tcell.KeyEnter:
			if town.EditChar != nil && town.InputBuf == town.EditChar.Password {
				town.EditChar.InMaze = true // Pascal line 179: INMAZE := TRUE
				town.Party.Members = append(town.Party.Members, town.EditChar)
				town.Message = fmt.Sprintf("%s JOINS THE PARTY!", town.EditChar.Name)
			} else {
				town.Message = "** THATS NOT IT **"
			}
			town.EditChar = nil
			town.InputMode = engine.InputNone
			town.InputBuf = ""
		case tcell.KeyEscape:
			town.EditChar = nil
			town.InputMode = engine.InputNone
			town.InputBuf = ""
		case tcell.KeyRune:
			ch := ev.Rune()
			if ch >= 'a' && ch <= 'z' {
				ch -= 32
			}
			if len(town.InputBuf) < 15 {
				town.InputBuf += string(ch)
			}
		}
		return false
	}

	// Roster mode: only L exits (from p-code: key == 76 'L')
	if town.InputMode == engine.InputRoster {
		if ev.Key() == tcell.KeyRune {
			ch := ev.Rune()
			if ch == 'l' || ch == 'L' {
				town.InputMode = engine.InputTrainingName
				town.InputBuf = ""
			}
		}
		return false
	}

	// Confirm create Y/N — from p-code ROLLER proc 10:
	// "THAT CHARACTER DOES NOT EXIST. DO YOU WANT TO CREATE IT? Y/N ? >"
	if town.InputMode == engine.InputConfirmCreate {
		if ev.Key() == tcell.KeyRune {
			ch := ev.Rune()
			if ch == 'y' || ch == 'Y' {
				town.Creation = engine.NewCreationState()
				town.Creation.Name = town.PendingCreateName
				town.Creation.Step = engine.StepPassword
				game.Phase = engine.PhaseCreation
				town.Message = ""
				town.Message2 = ""
				town.PendingCreateName = ""
				town.InputMode = engine.InputNone
			} else if ch == 'n' || ch == 'N' || ev.Key() == tcell.KeyEscape {
				town.Message = ""
				town.Message2 = ""
				town.PendingCreateName = ""
				town.InputMode = engine.InputTrainingName
				town.InputBuf = ""
			}
		}
		if ev.Key() == tcell.KeyEscape {
			town.Message = ""
			town.Message2 = ""
			town.PendingCreateName = ""
			town.InputMode = engine.InputTrainingName
			town.InputBuf = ""
		}
		return false
	}

	// Shop purchase sub-mode — "PURCHASE WHICH ITEM ([RETURN] EXITS) ? >"
	// p-code CLP 15 (IC 1627): selection 1-6 maps to ShopPage slot
	if town.InputMode == engine.InputShopPurchase {
		if ev.Key() == tcell.KeyRune {
			ch := ev.Rune()
			if ch >= '1' && ch <= '6' {
				slot := int(ch - '1')
				shopBuyItem(game, town, slot)
				town.InputMode = engine.InputNone
			}
		}
		if ev.Key() == tcell.KeyEnter || ev.Key() == tcell.KeyEscape {
			town.InputMode = engine.InputNone
			town.Message = ""
		}
		return false
	}

	// Remove member — "WHO WILL LEAVE ([RETURN] EXITS) >" (CASTLE byte 1619)
	if town.InputMode == engine.InputRemoveMember {
		if ev.Key() == tcell.KeyRune {
			ch := ev.Rune()
			if ch >= '1' && ch <= '6' {
				idx := int(ch-'0') - 1
				if idx < len(town.Party.Members) && town.Party.Members[idx] != nil {
					removed := town.Party.Members[idx]
					removed.InMaze = false // Pascal line 204: INMAZE := FALSE
					town.Party.Members = append(town.Party.Members[:idx], town.Party.Members[idx+1:]...)
					town.Message = fmt.Sprintf("%s LEAVES THE PARTY.", removed.Name)
					town.InputMode = engine.InputNone
				}
			}
		}
		if ev.Key() == tcell.KeyEnter || ev.Key() == tcell.KeyEscape {
			town.InputMode = engine.InputNone
			town.Message = ""
		}
		return false
	}

	// Temple name input — "WHO ARE YOU HELPING ? >"
	if town.InputMode == engine.InputTempleHelp {
		return handleTempleNameInput(game, ev)
	}

	// Password verification — from ROLLER proc 2 (IC 4826-4874)
	if town.InputMode == engine.InputPassword {
		return handlePasswordCheck(game, ev)
	}

	// Set new password — from ROLLER proc 3 (IC 4480-4816)
	if town.InputMode == engine.InputSetPassword {
		return handleSetPassword(game, ev)
	}

	// Class change selection — from Pascal ROLLER.TEXT CHGCLASS (lines 599-638)
	if town.InputMode == engine.InputClassChange {
		return handleClassChange(game, ev)
	}

	// Rite of Passage ceremony — waiting for RETURN
	if town.InputMode == engine.InputRiteCeremony {
		if ev.Key() == tcell.KeyEnter {
			c := town.EditChar
			good, neut, evil := engine.RiteAlignOptions(c.Class)
			if !good && !neut && !evil {
				// Lord → forced Good, Ninja → forced Evil
				if c.Class == engine.Lord {
					c.Alignment = engine.Good
				} else {
					c.Alignment = engine.Evil
				}
				engine.RiteApply(c)
				town.Message = c.Name + " IS NOW A LEGACY!"
				town.InputMode = engine.InputCharEdit
				game.Save()
			} else {
				town.RiteAlignGood = good
				town.RiteAlignNeut = neut
				town.RiteAlignEvil = evil
				town.InputMode = engine.InputRiteAlign
			}
		}
		return false
	}

	// Rite of Passage alignment choice
	if town.InputMode == engine.InputRiteAlign {
		if ev.Key() == tcell.KeyRune {
			ch := ev.Rune()
			if ch >= 'a' && ch <= 'z' {
				ch -= 32
			}
			c := town.EditChar
			picked := false
			switch ch {
			case 'A':
				if town.RiteAlignGood {
					c.Alignment = engine.Good
					picked = true
				}
			case 'B':
				if town.RiteAlignNeut {
					c.Alignment = engine.Neutral
					picked = true
				}
			case 'C':
				if town.RiteAlignEvil {
					c.Alignment = engine.Evil
					picked = true
				}
			}
			if picked {
				engine.RiteApply(c)
				town.Message = c.Name + " IS NOW A LEGACY!"
				town.InputMode = engine.InputCharEdit
				game.Save()
			}
		}
		return false
	}

	// Character edit mode — from ROLLER segment p-code
	if town.InputMode == engine.InputCharEdit {
		return handleCharEdit(game, ev)
	}

	// Confirm reroll — p-code proc 6: "ARE YOU SURE YOU WANT TO REROLL (Y/N) ?"
	if town.InputMode == engine.InputConfirmReroll {
		if ev.Key() == tcell.KeyRune {
			ch := ev.Rune()
			if ch >= 'a' && ch <= 'z' {
				ch -= 32
			}
			if ch == 'Y' {
				c := town.EditChar
				name := c.Name
				town.Roster.Remove(name)
				town.Creation = engine.NewCreationState()
				town.Creation.Name = name
				town.Creation.Step = engine.StepPassword
				town.Creation.Reroll = true
				game.Phase = engine.PhaseCreation
				town.EditChar = nil
				town.InputMode = engine.InputNone
			} else if ch == 'N' {
				town.InputMode = engine.InputCharEdit
				town.Message = ""
			}
		}
		return false
	}

	// Confirm delete — p-code proc 5: "ARE YOU SURE YOU WANT TO DELETE (Y/N) ?"
	if town.InputMode == engine.InputConfirmDelete {
		if ev.Key() == tcell.KeyRune {
			ch := ev.Rune()
			if ch >= 'a' && ch <= 'z' {
				ch -= 32
			}
			if ch == 'Y' {
				c := town.EditChar
				for i, rc := range town.Roster.Characters {
					if rc == c {
						town.Roster.Characters = append(town.Roster.Characters[:i], town.Roster.Characters[i+1:]...)
						break
					}
				}
				for i, m := range town.Party.Members {
					if m == c {
						town.Party.Members = append(town.Party.Members[:i], town.Party.Members[i+1:]...)
						break
					}
				}
				town.Message = c.Name + " DELETED"
				town.InputMode = engine.InputTrainingName
				town.InputBuf = ""
				town.EditChar = nil
			} else if ch == 'N' {
				town.InputMode = engine.InputCharEdit
				town.Message = ""
			}
		}
		return false
	}

	// Inspect / spell book modes
	if town.InputMode == engine.InputInspect {
		if ev.Key() == tcell.KeyRune {
			ch := ev.Rune()
			if ch >= 'a' && ch <= 'z' {
				ch -= 32
			}
			switch ch {
			case 'R':
				town.InputMode = engine.InputSpellBooks
				town.Message = ""
			case 'L':
				leaveInspect(town)
			case 'E':
				startEquipFlow(game, town)
			case 'D':
				town.InputMode = engine.InputDrop
				town.Message = ""
			case 'T':
				town.InputMode = engine.InputTrade
				town.Message = ""
			case 'S':
				// Cast spell — only available in camp (p-code CAMP IC 1939)
				if game.Phase == engine.PhaseCamp {
					town.InputMode = engine.InputCastSpell
					town.InputBuf = ""
					town.Message = ""
				}
			case 'U':
				// Use item — only available in camp (p-code CAMP IC 2399)
				if game.Phase == engine.PhaseCamp {
					town.InputMode = engine.InputUseItem
					town.Message = ""
				}
			case 'I':
				// Bishop identify — Pascal CAMP2.TEXT IDENTIFY proc (lines 6-28):
				// CAMPCHAR (the character doing camp actions) must be a Bishop.
				// Identifies unidentified items on the inspected character.
				if game.Phase == engine.PhaseCamp {
					c := town.EditChar
					if c == nil {
						break
					}
					// Check if the inspected character is a Bishop
					// (Pascal checks CAMPCHAR which is the active camp character)
					if c.Class != engine.Bishop {
						town.Message = "NOT BISHOP"
					} else {
						// Bishop identifies items on themselves
						// To identify OTHER characters' items, inspect them via the Bishop
						identified := 0
						for i := 0; i < c.ItemCount; i++ {
							if !c.Items[i].Identified {
								c.Items[i].Identified = true
								identified++
							}
						}
						if identified > 0 {
							town.Message = fmt.Sprintf("%d ITEM(S) IDENTIFIED", identified)
						} else {
							town.Message = "NOTHING TO IDENTIFY"
						}
					}
				}
			}
		} else if ev.Key() == tcell.KeyEnter || ev.Key() == tcell.KeyEscape {
			leaveInspect(town)
		}
		return false
	}

	if town.InputMode == engine.InputEquip {
		if ev.Key() == tcell.KeyEnter {
			// RETURN = skip this category (equip nothing for it)
			advanceEquipCategory(game, town)
		} else if ev.Key() == tcell.KeyRune {
			ch := ev.Rune()
			if ch >= '1' && ch <= '9' {
				sel := int(ch - '0')
				handleEquipSelection(game, town, sel)
			}
		}
		return false
	}

	if town.InputMode == engine.InputDrop {
		if ev.Key() == tcell.KeyRune {
			ch := ev.Rune()
			if ch == '0' {
				// 0=EXIT from p-code
				town.InputMode = engine.InputInspect
				town.Message = ""
			} else if ch >= '1' && ch <= '8' {
				handleDropItem(game, town, int(ch-'0'))
			}
		}
		if ev.Key() == tcell.KeyEnter || ev.Key() == tcell.KeyEscape {
			town.InputMode = engine.InputInspect
			town.Message = ""
		}
		return false
	}

	if town.InputMode == engine.InputTrade {
		// "TRADE WITH" — select party member (1-6)
		if ev.Key() == tcell.KeyRune {
			ch := ev.Rune()
			if ch >= '1' && ch <= '6' {
				idx := int(ch-'0') - 1
				if idx < len(town.Party.Members) && town.Party.Members[idx] != nil &&
					town.Party.Members[idx] != town.EditChar {
					town.TradeTarget = town.Party.Members[idx]
					town.InputMode = engine.InputTradeGold
					town.InputBuf = ""
					town.Message = ""
				}
			}
		}
		if ev.Key() == tcell.KeyEnter || ev.Key() == tcell.KeyEscape {
			town.InputMode = engine.InputInspect
			town.Message = ""
		}
		return false
	}

	if town.InputMode == engine.InputTradeGold {
		// "AMT OF GOLD ? >" — type gold amount, RETURN to confirm
		if ev.Key() == tcell.KeyRune {
			ch := ev.Rune()
			if ch >= '0' && ch <= '9' && len(town.InputBuf) < 12 {
				town.InputBuf += string(ch)
			}
		}
		if ev.Key() == tcell.KeyBackspace || ev.Key() == tcell.KeyBackspace2 {
			if len(town.InputBuf) > 0 {
				town.InputBuf = town.InputBuf[:len(town.InputBuf)-1]
			}
		}
		if ev.Key() == tcell.KeyEnter {
			handleTradeGold(game, town)
		}
		if ev.Key() == tcell.KeyEscape {
			town.InputMode = engine.InputInspect
			town.Message = ""
		}
		return false
	}

	if town.InputMode == engine.InputTradeTarget {
		// "WHAT ITEM ([RET] EXITS) ? >" — select item to trade
		if ev.Key() == tcell.KeyRune {
			ch := ev.Rune()
			if ch >= '1' && ch <= '8' {
				town.TradeItemIdx = int(ch - '0')
				handleTradeItem(game, town)
			}
		}
		if ev.Key() == tcell.KeyEnter || ev.Key() == tcell.KeyEscape {
			town.InputMode = engine.InputInspect
			town.Message = ""
		}
		return false
	}

	// Cast spell at camp — "WHAT SPELL ? >" (p-code CAMP IC 1939)
	if town.InputMode == engine.InputSpellBooks {
		if ev.Key() == tcell.KeyRune {
			ch := ev.Rune()
			if ch == 'm' || ch == 'M' {
				town.InspectSpells = engine.MageSpell
				town.InputMode = engine.InputSpellList
			} else if ch == 'p' || ch == 'P' {
				town.InspectSpells = engine.PriestSpell
				town.InputMode = engine.InputSpellList
			} else if ch == 'l' || ch == 'L' {
				town.InputMode = engine.InputInspect
			}
		} else if ev.Key() == tcell.KeyEnter || ev.Key() == tcell.KeyEscape {
			town.InputMode = engine.InputInspect
		}
		return false
	}

	if town.InputMode == engine.InputSpellList {
		if ev.Key() == tcell.KeyRune {
			ch := ev.Rune()
			if ch == 'l' || ch == 'L' {
				town.InputMode = engine.InputSpellBooks
			}
		} else if ev.Key() == tcell.KeyEnter || ev.Key() == tcell.KeyEscape {
			town.InputMode = engine.InputSpellBooks
		}
		return false
	}

	switch ev.Key() {
	case tcell.KeyEscape:
		town.InputMode = engine.InputNone
		town.InputBuf = ""
		town.Message = ""
	case tcell.KeyBackspace, tcell.KeyBackspace2:
		if len(town.InputBuf) > 0 {
			town.InputBuf = town.InputBuf[:len(town.InputBuf)-1]
		}
	case tcell.KeyEnter:
		switch town.InputMode {
		case engine.InputAddMember:
			name := strings.TrimSpace(town.InputBuf)
			if name == "" {
				town.InputMode = engine.InputNone
				return false
			}
			// Pascal ADDPARTY (CASTLE.TEXT lines 148-191):
			// Search roster for name, skip LOST characters.
			// Checks: OUT (InMaze/lost in maze), BAD ALIGNMENT, then password.
			found := false
			for _, c := range town.Roster.Characters {
				if c == nil {
					continue
				}
				// Pascal line 160: skip LOST characters in search
				if c.Status == engine.Lost {
					continue
				}
				if strings.EqualFold(c.Name, name) {
					// Check not already in party
					inParty := false
					for _, m := range town.Party.Members {
						if m == c {
							inParty = true
							break
						}
					}
					if inParty {
						town.Message = fmt.Sprintf("%s IS ALREADY IN THE PARTY!", c.Name)
					} else if c.InMaze || c.MazeLevel != 0 {
						// Pascal line 170-172: INMAZE or LOSTXYL.LOCATION[3] <> 0
						town.Message = "** OUT **"
					} else if game.Scenario.ScenarioNum == 3 && !c.IsLegacy {
						town.Message = "** ONLY A MEMORY **"
					} else {
						// Pascal line 174-177: alignment check
						partyAlign := partyAlignment(town)
						if partyAlign != engine.Neutral && c.Alignment != engine.Neutral && partyAlign != c.Alignment {
							town.Message = "** BAD ALIGNMENT **"
						} else {
							// From p-code CASTLE proc 30 (IC 1416-1468):
							// Prompt "ENTER PASSWORD  >"
							town.EditChar = c
							town.InputMode = engine.InputTavernPassword
							town.InputBuf = ""
							return false
						}
					}
					found = true
					break
				}
			}
			if !found {
				town.Message = "** WHO? **"
			}
			town.InputMode = engine.InputNone
			town.InputBuf = ""
			return false

		case engine.InputTrainingName:
			name := strings.TrimSpace(town.InputBuf)
			if name == "" {
				goToCastle(town)
				town.InputMode = engine.InputNone
				town.InputBuf = ""
				return false
			}
			if strings.EqualFold(name, "*ROSTER") {
				town.InputMode = engine.InputRoster
				town.InputBuf = ""
				return false
			} else {
				found := false
				for _, c := range town.Roster.Characters {
					if c != nil && strings.EqualFold(c.Name, name) {
						found = true
						town.EditChar = c
						// From p-code ROLLER proc 2 (IC 4826-4874):
						// Always prompt "PASSWORD >" — even for empty passwords.
						town.InputMode = engine.InputPassword
						town.InputBuf = ""
						town.Message = ""
						return false
					}
				}
				if !found {
					if game.Scenario.ScenarioNum >= 2 {
						// Wiz 2/3: no character creation — silently re-prompt
						town.InputMode = engine.InputTrainingName
						town.InputBuf = ""
						return false
					}
					if len(town.Roster.Characters) >= 20 {
						town.Message = "THERE IS NO ROOM LEFT - TRY DELETING"
					} else {
						// From p-code ROLLER proc 10 (IC 3164):
						// "THAT CHARACTER DOES NOT EXIST."
						// "DO YOU WANT TO CREATE IT? Y/N ? >"
						town.Message = "THAT CHARACTER DOES NOT EXIST. DO YOU"
						town.Message2 = "WANT TO CREATE IT (Y/N) ?> "
						town.PendingCreateName = strings.ToUpper(name)
						town.InputMode = engine.InputConfirmCreate
						town.InputBuf = ""
						return false
					}
				}
			}
		}
		town.InputMode = engine.InputNone
		town.InputBuf = ""
	case tcell.KeyRune:
		ch := ev.Rune()
		if len(town.InputBuf) < 15 {
			if ch >= 'a' && ch <= 'z' {
				ch -= 32
			}
			if (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == ' ' || ch == '-' || ch == '\'' {
				town.InputBuf += string(ch)
			}
		}
	}
	return false
}

// partyAlignment returns the party's alignment per Pascal GETALIGN:
// scan all members, last non-Neutral alignment wins. Empty/all-Neutral → Neutral.
func partyAlignment(town *engine.TownState) engine.Alignment {
	align := engine.Neutral
	for _, m := range town.Party.Members {
		if m != nil && m.Alignment != engine.Neutral {
			align = m.Alignment
		}
	}
	return align
}

func goToCastle(town *engine.TownState) {
	town.Location = engine.Castle
	town.SelectedIndex = 0
	town.Message = ""
	town.InnMessages = nil
}

// handleInnInput handles key presses at the inn based on current InnStep.
// From CASTLE segment p-code procs 26/24/23/15/14:
//   InnWho: "WHO WILL STAY" → select party member 1-6, RETURN exits to castle
//   InnSelectRoom: "WELCOME [name]. WE HAVE:" → A-E selects room, RETURN → InnWho
//   InnResult: rest results + XP message, RETURN → InnSelectRoom (same character)
func handleInnInput(game *engine.GameState, town *engine.TownState, ch rune) {
	switch town.InnStep {
	case engine.InnWho:
		if ch >= '1' && ch <= '6' {
			idx := int(ch-'0') - 1
			if idx < len(town.Party.Members) && town.Party.Members[idx] != nil {
				c := town.Party.Members[idx]
				if c.Status != engine.OK {
					town.Message = c.Name + " IS NOT OK"
				} else {
					town.InnChar = c
					town.InnStep = engine.InnSelectRoom
					town.Message = ""
				}
			}
		}

	case engine.InnSelectRoom:
		// Pascal TAKENAP (CASTLE2.TEXT lines 415-479):
		// Sets up healing parameters, then enters animated healing loop.
		var healAmt, healCost int
		switch ch {
		case 'A':
			healAmt = 0
			healCost = 0
		case 'B':
			healAmt = 1
			healCost = 10
		case 'C':
			healAmt = 3
			healCost = 50
		case 'D':
			healAmt = 7
			healCost = 200
		case 'E':
			healAmt = 10 // Pascal CASTLE2.TEXT line 500: TAKENAP(10, 500)
			healCost = 500
		default:
			return
		}
		c := town.InnChar

		// Restore spells on any room (Pascal calls SETSPELS in TAKENAP)
		engine.RestoreSpells(c)

		if healAmt == 0 {
			// Stables: no healing, just "IS NAPPING" then straight to level check
			town.InnMessages = nil
			town.InnStep = engine.InnHealing
			town.InnHealAmt = 0
			town.InnHealCost = 0
		} else {
			// Paid room: start animated healing loop
			town.InnHealAmt = healAmt
			town.InnHealCost = healCost
			town.InnStep = engine.InnHealing
		}
		town.Message = ""
	}
}

// innTransitionToLevelUp runs CHNEWLEV (Pascal CASTLE2.TEXT lines 386-412)
// after healing completes. Shows level-up or XP needed.
func innTransitionToLevelUp(game *engine.GameState) {
	town := game.Town
	c := town.InnChar
	prevLevel := c.Level
	// Pascal MADELEV order: level++, MAXLEVAC, SETSPELS, TRYLEARN, GAINLOST, HP calc
	learnedSpells := engine.CheckLevelUp(c, game) // level++, MAXLEVAC, TRYLEARN

	if c.Level > prevLevel {
		town.InnMessages = []string{
			"YOU MADE A LEVEL!",
			"",
		}
		if learnedSpells {
			town.InnMessages = append(town.InnMessages, "YOU LEARNED NEW SPELLS!!!!")
		}
		// GAINLOST — age-based stat changes (must run BEFORE HP recalc)
		statMsgs := engine.InnStatChanges(c)
		town.InnMessages = append(town.InnMessages, statMsgs...)
		// HP recalculation — uses VIT which may have changed in GAINLOST
		engine.RecalcHP(c)
	} else {
		needed := engine.XPForNextLevel(c, game)
		if needed > 0 {
			diff := needed - c.XP
			if diff > 0 {
				town.InnMessages = []string{
					"",
					fmt.Sprintf("YOU NEED %d MORE", diff),
					"EXPERIENCE POINTS TO MAKE LEVEL",
				}
			}
		}
	}
	town.InnStep = engine.InnLevelUp
}

// handleInnReturn handles RETURN/ESC at the inn.
// From p-code: Return at InnResult goes back to InnSelectRoom (same character),
// Return at InnSelectRoom goes back to InnWho, Return at InnWho exits to castle.
func handleInnReturn(town *engine.TownState) {
	switch town.InnStep {
	case engine.InnWho:
		goToCastle(town)
	case engine.InnSelectRoom:
		town.InnStep = engine.InnWho
		town.InnChar = nil
		town.Message = ""
	case engine.InnHealing:
		// Key press during healing skips the animation (Pascal KEYAVAIL check)
		// Transition to level-up immediately — handled by next tick
	case engine.InnLevelUp:
		// Return goes back to room selection for the SAME character
		town.InnStep = engine.InnSelectRoom
		town.InnMessages = nil
		town.Message = ""
	}
}

// handleTempleNameInput handles text input at "WHO ARE YOU HELPING ? >".
// From SHOPS segment p-code proc 28 (IC 76-436):
//   Read name, search roster, check status, display result.
func handleTempleNameInput(game *engine.GameState, ev *tcell.EventKey) bool {
	town := game.Town

	switch ev.Key() {
	case tcell.KeyEscape:
		// ESC exits temple
		town.InputMode = engine.InputNone
		town.InputBuf = ""
		goToCastle(town)
		return false
	case tcell.KeyBackspace, tcell.KeyBackspace2:
		if len(town.InputBuf) > 0 {
			town.InputBuf = town.InputBuf[:len(town.InputBuf)-1]
		}
	case tcell.KeyEnter:
		name := strings.TrimSpace(town.InputBuf)
		if name == "" {
			// Empty name = exit temple (from p-code: empty string → EXIT)
			town.InputMode = engine.InputNone
			town.InputBuf = ""
			goToCastle(town)
			return false
		}

		// Search roster for character by name (case-insensitive)
		// From p-code proc 28 (IC 205-299): loop through roster comparing names
		var found *engine.Character
		for _, c := range town.Roster.Characters {
			if c == nil {
				continue
			}
			if strings.EqualFold(c.Name, name) {
				found = c
				break
			}
		}

		if found == nil {
			// Character not found — silent return to name prompt
			// (p-code just loops back to "WHO ARE YOU HELPING")
			town.InputBuf = ""
			town.Message = ""
			return false
		}

		// Check if character is lost — from p-code proc 28 (IC 374-396)
		if found.Status == engine.Lost {
			town.Message = found.Name + " IS LOST"
			town.InputBuf = ""
			return false
		}

		// Check if character is OK — from p-code proc 28 (IC 397-416)
		if found.Status == engine.OK {
			town.Message = found.Name + " IS OK"
			town.InputBuf = ""
			return false
		}

		// Character needs healing — check if they need temple services
		// Statuses that temple can handle: Paralyzed, Stoned, Dead, Ashed
		cost := engine.TempleDonation(found.Status, found.Level)
		if cost == 0 {
			// Asleep/Afraid — not handled by temple (will be OK after resting)
			town.Message = found.Name + " IS OK"
			town.InputBuf = ""
			return false
		}

		// Set up donation display
		town.TempleChar = found
		town.TempleCost = cost
		town.TempleStep = engine.TempleTithe
		town.InputMode = engine.InputNone
		town.InputBuf = ""
		town.Message = ""
	case tcell.KeyRune:
		ch := ev.Rune()
		if len(town.InputBuf) < 15 {
			town.InputBuf += string(ch)
		}
	}
	return false
}

// handleTempleInput handles key presses at the temple based on current TempleStep.
// From SHOPS segment p-code procs 27/23/25.
func handleTempleInput(game *engine.GameState, town *engine.TownState, ch rune) {
	switch town.TempleStep {
	case engine.TempleWho:
		// Text input mode — handled by handleTempleNameInput via InputTempleHelp
		// If we get here, the user pressed a key while in TempleWho without InputMode set
		town.InputMode = engine.InputTempleHelp
		town.InputBuf = string(ch)

	case engine.TempleTithe:
		// "WHO WILL TITHE" — select party member (1-6)
		// From p-code proc 27 (IC 561-597): WIZARDRY.proc15 reads party member selection
		if ch >= '1' && ch <= '6' {
			idx := int(ch-'0') - 1
			if idx < len(town.Party.Members) && town.Party.Members[idx] != nil {
				payer := town.Party.Members[idx]
				// Check if payer can afford the donation
				// From p-code proc 27 (IC 599-647): WIZARDRY.proc11 subtracts gold,
				// if result > 0 (not enough): "CHEAP APOSTATES! OUT!"
				if payer.Gold < town.TempleCost {
					town.TempleStep = engine.TempleResult
					town.TempleMessages = []string{"CHEAP APOSTATES! OUT!"}
					town.Message = ""
					return
				}

				// Deduct gold from payer
				payer.Gold -= town.TempleCost

				// Advance to ritual display
				// From p-code proc 23 (IC 796-1052): MURMUR-CHANT-PRAY-INVOKE
				town.TempleStep = engine.TempleRitual
				town.Message = ""
			}
		}

	case engine.TempleRitual:
		// During ritual display, any keypress performs the heal and shows result
		templePerformHeal(game, town)
	}
}

// templePerformHeal executes the healing attempt and transitions to result.
func templePerformHeal(game *engine.GameState, town *engine.TownState) {
	msgs, _ := engine.TempleHeal(town.TempleChar)
	town.TempleMessages = msgs
	town.TempleStep = engine.TempleResult
	town.Message = ""
	// Auto-save after healing (original writes character back to disk)
	if game != nil {
		_ = game.Save()
	}
}

// handleTempleReturn handles RETURN/ESC at the temple.
func handleTempleReturn(game *engine.GameState, town *engine.TownState) {
	switch town.TempleStep {
	case engine.TempleWho:
		// RETURN at name prompt without InputMode — exit temple
		goToCastle(town)
	case engine.TempleTithe:
		// RETURN at tithe selection — back to name prompt
		town.TempleStep = engine.TempleWho
		town.TempleChar = nil
		town.TempleCost = 0
		town.InputMode = engine.InputTempleHelp
		town.InputBuf = ""
		town.Message = ""
	case engine.TempleRitual:
		// RETURN during ritual — perform heal and show result
		templePerformHeal(game, town)
	case engine.TempleResult:
		// RETURN at result — back to name prompt
		town.TempleStep = engine.TempleWho
		town.TempleChar = nil
		town.TempleCost = 0
		town.TempleMessages = nil
		town.InputMode = engine.InputTempleHelp
		town.InputBuf = ""
		town.Message = ""
	}
}

// fillShopBuyPage populates town.ShopPage with up to 6 item indices starting
// from town.ShopCatalog, skipping items with stock==0. Wraps around the item
// table (items 1..N, skipping item 0). From p-code CLP 13 (IC 1100-1306).
func fillShopBuyPage(town *engine.TownState, items []data.Item, forward bool) {
	total := len(items)
	if total <= 1 {
		for i := range town.ShopPage {
			town.ShopPage[i] = -1
		}
		return
	}

	idx := town.ShopCatalog
	if forward {
		for slot := 0; slot < 6; slot++ {
			// Find next item with stock != 0
			found := false
			for tries := 0; tries < total; tries++ {
				idx++
				if idx >= total {
					idx = 1 // wrap to item 1 (skip item 0)
				}
				if items[idx].Stock != 0 && !items[idx].Cursed {
					town.ShopPage[slot] = idx
					found = true
					break
				}
			}
			if !found {
				town.ShopPage[slot] = -1
			}
		}
	} else {
		// Fill backward: find 6 items going backward from ShopPage[0]
		idx = town.ShopPage[0]
		for slot := 5; slot >= 0; slot-- {
			found := false
			for tries := 0; tries < total; tries++ {
				idx--
				if idx < 1 {
					idx = total - 1 // wrap to last item
				}
				if items[idx].Stock != 0 && !items[idx].Cursed {
					town.ShopPage[slot] = idx
					found = true
					break
				}
			}
			if !found {
				town.ShopPage[slot] = -1
			}
		}
	}
	// Update ShopCatalog to track the last displayed item (for forward scrolling)
	for i := 5; i >= 0; i-- {
		if town.ShopPage[i] >= 0 {
			town.ShopCatalog = town.ShopPage[i]
			break
		}
	}
}

// handleShopInput handles key presses at Boltac's Trading Post.
// Flow traced from SHOPS segment (seg 2) p-code:
//   - ShopWho: select party member (IC 4143-4188)
//   - ShopMain: B/S/U/I/L dispatch (IC 3664-4057)
//   - ShopBuy: P/F/B/S/L browse (CLP 12, IC 2168-2541)
//   - ShopSell/Uncurse/Identify: CLP 17 (IC 3424-3650)
func handleShopInput(game *engine.GameState, town *engine.TownState, ch rune) {
	items := game.Scenario.Items

	switch town.ShopStep {
	case engine.ShopWho:
		// Select party member by number (1-6) — p-code IC 4161-4183
		if ch >= '1' && ch <= '6' {
			idx := int(ch-'0') - 1
			if idx < len(town.Party.Members) && town.Party.Members[idx] != nil {
				town.ShopChar = town.Party.Members[idx]
				town.ShopStep = engine.ShopMain
				town.Message = ""
			}
		}

	case engine.ShopMain:
		// Main menu dispatch — p-code XJP at IC 4006
		// case 66('B')=Buy, 73('I')=Identify, 76('L')=Leave, 83('S')=Sell, 85('U')=Uncurse
		switch ch {
		case 'B':
			town.ShopStep = engine.ShopBuy
			town.ShopCatalog = 0
			fillShopBuyPage(town, items, true)
			town.Message = ""
		case 'S':
			if town.ShopChar.ItemCount == 0 {
				return // no items → stay on main menu (p-code XIT check)
			}
			town.ShopStep = engine.ShopSell
			town.Message = ""
		case 'U':
			if town.ShopChar.ItemCount == 0 {
				return
			}
			town.ShopStep = engine.ShopUncurse
			town.Message = ""
		case 'I':
			if town.ShopChar.ItemCount == 0 {
				return
			}
			town.ShopStep = engine.ShopIdentify
			town.Message = ""
		case 'L':
			// p-code XIT(8, 11) — back to "WHO WILL ENTER"
			town.ShopStep = engine.ShopWho
			town.ShopChar = nil
			town.Message = ""
		}

	case engine.ShopBuy:
		// Browse catalog — p-code CLP 12 (IC 2168-2541)
		// P=purchase, F=forward, B=back, S=start, L=leave
		switch ch {
		case 'F':
			fillShopBuyPage(town, items, true)
			town.Message = ""
		case 'B':
			fillShopBuyPage(town, items, false)
			town.Message = ""
		case 'S':
			town.ShopCatalog = 0
			fillShopBuyPage(town, items, true)
			town.Message = ""
		case 'P':
			town.InputMode = engine.InputShopPurchase
			town.Message = ""
		case 'L':
			town.ShopStep = engine.ShopMain
			town.InputMode = engine.InputNone
			town.Message = ""
		}

	case engine.ShopBuyConfirm:
		// Y/N confirmation for unusable item purchase — p-code IC 1899
		// Pascal SHOPS.TEXT line 358: "ITS YOUR MONEY"
		if ch == 'Y' {
			itemIdx := town.ShopCatalog
			c := town.ShopChar
			if itemIdx >= 0 && itemIdx < len(items) {
				item := items[itemIdx]
				c.Gold -= item.Price
				c.AddItem(itemIdx, true)
				if item.Stock > 0 {
					game.Scenario.Items[itemIdx].Stock--
				}
				town.Message = "** ITS YOUR MONEY **"
			}
			town.ShopStep = engine.ShopBuy
		} else if ch == 'N' {
			town.Message = ""
			town.ShopStep = engine.ShopBuy
		}

	case engine.ShopSell:
		// p-code CLP 17 mode=0 (IC 2871-3422)
		if ch >= '1' && ch <= '8' {
			idx := int(ch-'0') - 1
			c := town.ShopChar
			if idx < c.ItemCount {
				poss := c.Items[idx]
				itemIdx := poss.ItemIndex
				if itemIdx >= 0 && itemIdx < len(items) {
					item := items[itemIdx]
					// Check cursed — p-code IC 2926-2962
					if poss.Cursed {
						town.Message = "** WE DONT BUY CURSED ITEMS **"
						town.ShopStep = engine.ShopMain // XIT back to main
						return
					}
					// Sell price: half price, or 1 GP if unidentified — p-code IC 2731-2768
					sellPrice := item.Price / 2
					if !poss.Identified {
						sellPrice = 1
					}
					// Check can afford (always true for sell, but fee check is shared)
					// Gold transfer: add sell price — p-code IC 3150-3162 (CXP proc=5)
					c.Gold += sellPrice
					// Remove item — p-code IC 3211-3308
					c.DropItem(idx)
					// Increment stock — p-code IC 3337-3357
					if item.Stock > -1 {
						game.Scenario.Items[itemIdx].Stock++
					}
					town.Message = "** ANYTHING ELSE, SIRE? **"
				}
			}
		}

	case engine.ShopUncurse:
		// p-code CLP 17 mode=1 (IC 2966-3422)
		if ch >= '1' && ch <= '8' {
			idx := int(ch-'0') - 1
			c := town.ShopChar
			if idx < c.ItemCount {
				poss := c.Items[idx]
				itemIdx := poss.ItemIndex
				if itemIdx >= 0 && itemIdx < len(items) {
					item := items[itemIdx]
					// Must be cursed — p-code IC 2966-2990
					if !poss.Cursed {
						town.Message = "** THAT IS NOT A CURSED ITEM **"
						town.ShopStep = engine.ShopMain // XIT back to main
						return
					}
					// Fee = half price — p-code IC 3088-3105
					fee := item.Price / 2
					if c.Gold < fee {
						town.Message = "** YOU CANT AFFORD THE FEE **"
						town.ShopStep = engine.ShopMain
						return
					}
					// Subtract fee — p-code IC 3167-3179 (CXP proc=6)
					c.Gold -= fee
					// Remove item — p-code IC 3211-3308
					c.DropItem(idx)
					// Increment stock — p-code IC 3337-3357
					if item.Stock > -1 {
						game.Scenario.Items[itemIdx].Stock++
					}
					town.Message = "** ANYTHING ELSE, SIRE? **"
				}
			}
		}

	case engine.ShopIdentify:
		// p-code CLP 17 mode=2 (IC 3028-3422)
		if ch >= '1' && ch <= '8' {
			idx := int(ch-'0') - 1
			c := town.ShopChar
			if idx < c.ItemCount {
				poss := c.Items[idx]
				itemIdx := poss.ItemIndex
				if itemIdx >= 0 && itemIdx < len(items) {
					item := items[itemIdx]
					// Already identified — p-code IC 3028-3051
					if poss.Identified {
						town.Message = "** THAT HAS BEEN IDENTIFIED **"
						town.ShopStep = engine.ShopMain // XIT back to main
						return
					}
					// Fee = half price — p-code IC 3088-3105
					fee := item.Price / 2
					if c.Gold < fee {
						town.Message = "** YOU CANT AFFORD THE FEE **"
						town.ShopStep = engine.ShopMain
						return
					}
					// Subtract fee — p-code IC 3167-3179
					c.Gold -= fee
					// Set identified flag — p-code IC 3189-3208
					c.Items[idx].Identified = true
					town.Message = "** ANYTHING ELSE, SIRE? **"
				}
			}
		}
	}
}

// shopBuyItem executes a purchase from the buy catalog.
// From p-code CLP 15 (IC 1589-2166): validates stock, capacity, gold, class.
func shopBuyItem(game *engine.GameState, town *engine.TownState, pageSlot int) {
	items := game.Scenario.Items
	c := town.ShopChar
	itemIdx := town.ShopPage[pageSlot]
	if itemIdx < 0 || itemIdx >= len(items) {
		return
	}
	item := items[itemIdx]
	if item.Stock == 0 {
		town.Message = "** YOU BOUGHT THE LAST ONE **"
		return
	}
	if c.ItemCount >= 8 {
		town.Message = "** YOU CANT CARRY ANYTHING MORE **"
		return
	}
	if c.Gold < item.Price {
		town.Message = "** YOU CANNOT AFFORD IT **"
		return
	}
	// Class usability check — p-code IC 1863-1880
	unusable := item.ClassUse != 0 && (item.ClassUse&(1<<uint(c.Class))) == 0
	if unusable {
		// "UNUSABLE ITEM - CONFIRM BUY (Y/N) ? >" — p-code IC 1899
		town.Message = "UNUSABLE ITEM - CONFIRM BUY (Y/N) ? >"
		town.ShopCatalog = itemIdx
		town.ShopStep = engine.ShopBuyConfirm
		return
	}
	// Execute purchase — p-code IC 1989-2112
	c.Gold -= item.Price
	c.AddItem(itemIdx, true) // purchased items are identified — you know what you bought
	// Decrement stock — p-code IC 2072-2084
	if item.Stock > 0 {
		game.Scenario.Items[itemIdx].Stock--
	}
	town.Message = "** JUST WHAT YOU NEEDED **"
}

func handleShopReturn(town *engine.TownState) {
	switch town.ShopStep {
	case engine.ShopWho:
		goToCastle(town)
	case engine.ShopMain:
		town.ShopStep = engine.ShopWho
		town.ShopChar = nil
		town.Message = ""
	case engine.ShopSell, engine.ShopUncurse, engine.ShopIdentify:
		// RETURN at sell/uncurse/identify prompt — p-code IC 3620-3627: XIT(8,17)
		town.ShopStep = engine.ShopMain
		town.Message = ""
	case engine.ShopBuyConfirm:
		// RETURN at confirm prompt = cancel (treat as 'N')
		town.ShopStep = engine.ShopBuy
		town.Message = ""
	case engine.ShopBuy:
		if town.InputMode == engine.InputShopPurchase {
			// RETURN exits purchase sub-mode — p-code CLP 15 key==13
			town.InputMode = engine.InputNone
			town.Message = ""
		} else {
			town.ShopStep = engine.ShopMain
			town.Message = ""
		}
	}
}

// handleCharEdit handles the character edit menu at Training Grounds.
// From ROLLER segment p-code: I)nspect, D)elete, R)eroll, C)hange class,
// S)et password, or [RET] to leave.
func leaveInspect(town *engine.TownState) {
	if town.Location == engine.Training {
		town.InputMode = engine.InputCharEdit
	} else {
		town.InputMode = engine.InputNone
		town.EditChar = nil
	}
	town.Message = ""
}

// startEquipFlow begins the category-based equip process.
// From UTILITIE segment p-code: walks through WEAPON→ARMOR→SHIELD→HELMET→GAUNTLETS→MISC.
// For each category, shows matching items and lets user pick one.
func startEquipFlow(game *engine.GameState, town *engine.TownState) {
	// First, unequip everything (the original does this at UTILITIE byte 6626-6654)
	c := town.EditChar
	for i := 0; i < c.ItemCount; i++ {
		if c.Items[i].Equipped && !c.Items[i].Cursed {
			c.Items[i].Equipped = false
		}
	}

	town.InputMode = engine.InputEquip
	town.EquipCategory = 0 // start with first category in EquipCategories
	town.Message = ""
	buildEquipChoices(game, town)
}

// buildEquipChoices scans the character's items for ones matching the current equip category.
// Populates town.EquipChoices with 0-based item positions.
func buildEquipChoices(game *engine.GameState, town *engine.TownState) {
	c := town.EditChar
	items := game.Scenario.Items
	catIdx := town.EquipCategory
	if catIdx >= len(engine.EquipCategories) {
		// Done with all categories — recalculate AC from equipped items
		recalcAC(c, items)
		town.EquipChoices = nil

		// Party-wide equip mode: advance to next party member
		if town.EquipPartyMode {
			town.EquipPartyIdx++
			for town.EquipPartyIdx < len(town.Party.Members) {
				m := town.Party.Members[town.EquipPartyIdx]
				if m != nil && m.IsAlive() {
					break
				}
				town.EquipPartyIdx++
			}
			if town.EquipPartyIdx < len(town.Party.Members) {
				town.EditChar = town.Party.Members[town.EquipPartyIdx]
				startEquipFlow(game, town)
				return
			}
			// All party members equipped
			town.EquipPartyMode = false
			town.EditChar = nil
			town.InputMode = engine.InputNone
			return
		}

		town.InputMode = engine.InputInspect
		return
	}
	cat := engine.EquipCategories[catIdx]
	town.EquipChoices = nil

	for i := 0; i < c.ItemCount; i++ {
		poss := c.Items[i]
		if poss.ItemIndex < 0 || poss.ItemIndex >= len(items) {
			continue
		}
		item := items[poss.ItemIndex]
		if item.TypeID != cat {
			continue
		}
		// Class usability check — from p-code byte 5779: IXP 16,1 on ClassUse field
		// Each bit in ClassUse corresponds to a class (bit 0=Fighter, bit 1=Mage, etc.)
		if item.ClassUse != 0 && (item.ClassUse&(1<<uint(c.Class))) == 0 {
			continue
		}
		town.EquipChoices = append(town.EquipChoices, i)
	}

	// If no items match this category, skip to next
	if len(town.EquipChoices) == 0 {
		town.EquipCategory++
		buildEquipChoices(game, town)
	}
}

// handleEquipSelection handles the user picking an item number in the equip category screen.
func handleEquipSelection(game *engine.GameState, town *engine.TownState, sel int) {
	if sel < 1 || sel > len(town.EquipChoices) {
		return
	}
	pos := town.EquipChoices[sel-1]
	c := town.EditChar

	if c.Items[pos].Cursed {
		town.Message = "** CURSED **"
	}

	// Equip the selected item
	c.Items[pos].Equipped = true
	advanceEquipCategory(game, town)
}

// handleReorderPick handles picking a party member for the current reorder slot.
// From UTILITIE p-code byte 7190: user presses a number (1-N) referring to the
// ORIGINAL party position. If that character hasn't been placed yet, they fill
// the current slot. Last remaining character auto-fills.
func handleReorderPick(game *engine.GameState, num int) {
	town := game.Town
	party := town.Party

	idx := num - 1
	if idx < 0 || idx >= len(party.Members) || party.Members[idx] == nil {
		return
	}

	picked := party.Members[idx]

	// Check not already placed — p-code checks local109[idx] == 99 (unassigned)
	for _, c := range town.ReorderResult {
		if c == picked {
			return
		}
	}

	town.ReorderResult = append(town.ReorderResult, picked)
	town.ReorderPos++

	// Auto-fill last remaining character — p-code only loops to party_size - 2
	if town.ReorderPos >= party.Size()-1 {
		for _, m := range party.Members {
			if m == nil {
				continue
			}
			placed := false
			for _, r := range town.ReorderResult {
				if r == m {
					placed = true
					break
				}
			}
			if !placed {
				town.ReorderResult = append(town.ReorderResult, m)
				break
			}
		}
	}

	if len(town.ReorderResult) >= party.Size() {
		party.Members = town.ReorderResult
		town.ReorderResult = nil
		town.InputMode = engine.InputNone
		town.Message = ""
	}
}

// recalcAC recalculates a character's armor class from equipped items.
// Base AC is 10. Each equipped item's ACMod subtracts from it (lower = better).
func recalcAC(c *engine.Character, items []data.Item) {
	ac := 10
	for i := 0; i < c.ItemCount; i++ {
		if !c.Items[i].Equipped {
			continue
		}
		idx := c.Items[i].ItemIndex
		if idx >= 0 && idx < len(items) {
			ac -= items[idx].ACMod
		}
	}
	c.AC = ac
}

// advanceEquipCategory moves to the next equipment category.
func advanceEquipCategory(game *engine.GameState, town *engine.TownState) {
	town.EquipCategory++
	town.Message = ""
	buildEquipChoices(game, town)
}

// handleDropItem removes an item from the character.
// From CAMP segment p-code proc 19: "DROP ITEM (0=EXIT) ? >"
// Warns "CURSED" if cursed+equipped (can't drop). Warns "EQUIPPED" if equipped.
// Then shifts items down, decrements ItemCount.
func handleDropItem(game *engine.GameState, town *engine.TownState, num int) {
	c := town.EditChar
	pos := num - 1

	if pos < 0 || pos >= c.ItemCount {
		return
	}

	// Cursed equipped items can't be dropped
	if c.Items[pos].Equipped && c.Items[pos].Cursed {
		town.Message = "CURSED"
		return
	}

	// Warn about equipped items (but still drop)
	if c.Items[pos].Equipped {
		town.Message = "EQUIPPED"
	}

	c.DropItem(pos)
	if town.Message == "" {
		town.Message = "DROPPED"
	}
}

// handleTradeGold processes the gold amount entry.
// From CAMP segment p-code byte 3029: "AMT OF GOLD ? >"
// Validates amount, checks "NOT ENOUGH $", transfers gold, then moves to item trade.
func handleTradeGold(game *engine.GameState, town *engine.TownState) {
	c := town.EditChar
	target := town.TradeTarget

	if target == nil {
		town.InputMode = engine.InputInspect
		return
	}

	// Parse the gold amount from InputBuf
	amount := 0
	valid := true
	if town.InputBuf == "" {
		amount = 0
	} else {
		for _, ch := range town.InputBuf {
			if ch < '0' || ch > '9' {
				valid = false
				break
			}
			amount = amount*10 + int(ch-'0')
		}
	}

	if !valid || amount < 0 {
		town.Message = "BAD AMT"
		town.InputBuf = ""
		return
	}

	if amount > 0 {
		if c.Gold < amount {
			town.Message = "NOT ENOUGH $"
			town.InputBuf = ""
			return
		}
		c.Gold -= amount
		target.Gold += amount
	}

	// Move to item trading phase
	town.InputMode = engine.InputTradeTarget
	town.InputBuf = ""
	town.Message = ""
}

// handleTradeItem trades an item to another party member.
// From CAMP segment p-code proc 23, byte 3278: "WHAT ITEM ([RET] EXITS) ? >"
// Checks FULL (8 items), CURSED (equipped+cursed), EQUIPPED warning.
func handleTradeItem(game *engine.GameState, town *engine.TownState) {
	c := town.EditChar
	target := town.TradeTarget

	if target == nil {
		town.InputMode = engine.InputInspect
		return
	}

	// Check if target is full (8 items max)
	if target.ItemCount >= 8 {
		town.Message = "FULL"
		town.InputMode = engine.InputInspect
		return
	}

	num := town.TradeItemIdx
	pos := num - 1

	if pos < 0 || pos >= c.ItemCount {
		town.InputMode = engine.InputInspect
		return
	}

	// Equipped items can't be traded — show "EQUIPPED" and block
	if c.Items[pos].Equipped {
		if c.Items[pos].Cursed {
			town.Message = "** CURSED **"
		} else {
			town.Message = "** EQUIPPED **"
		}
		return
	}

	c.TradeItem(pos, target)
	town.Message = "TRADED"
}

// handlePasswordCheck verifies a password typed at Training Grounds or Tavern.
// From p-code ROLLER proc 2 (IC 4826-4874): "PASSWORD >" at GOTOXY(9,10)
func handlePasswordCheck(game *engine.GameState, ev *tcell.EventKey) bool {
	town := game.Town
	switch ev.Key() {
	case tcell.KeyBackspace, tcell.KeyBackspace2:
		if len(town.InputBuf) > 0 {
			town.InputBuf = town.InputBuf[:len(town.InputBuf)-1]
		}
	case tcell.KeyEnter:
		if town.EditChar != nil && town.InputBuf == town.EditChar.Password {
			// Correct password — proceed to char edit
			town.InputMode = engine.InputCharEdit
			town.InputBuf = ""
			town.Message = ""
		} else {
			// Wrong password — back to name prompt
			// From p-code: CSP EXIT returns to training grounds
			town.InputMode = engine.InputTrainingName
			town.InputBuf = ""
			town.EditChar = nil
			town.Message = "** THATS NOT IT **"
		}
	case tcell.KeyEscape:
		town.InputMode = engine.InputTrainingName
		town.InputBuf = ""
		town.EditChar = nil
		town.Message = ""
	case tcell.KeyRune:
		ch := ev.Rune()
		if ch >= 'a' && ch <= 'z' {
			ch -= 32
		}
		if len(town.InputBuf) < 15 {
			town.InputBuf += string(ch)
		}
	}
	return false
}

// handleSetPassword handles the two-step password change at Training Grounds.
// From p-code ROLLER proc 3 (IC 4480-4816):
//   Step 0: "ENTER NEW PASSWORD ([RET] FOR NONE)" → type password
//   Step 1: "ENTER AGAIN TO BE SURE" → confirm; match → save, mismatch → "NOT THE SAME"
func handleSetPassword(game *engine.GameState, ev *tcell.EventKey) bool {
	town := game.Town
	switch ev.Key() {
	case tcell.KeyBackspace, tcell.KeyBackspace2:
		if len(town.InputBuf) > 0 {
			town.InputBuf = town.InputBuf[:len(town.InputBuf)-1]
		}
	case tcell.KeyEnter:
		if town.PasswordStep == 0 {
			if len(town.InputBuf) > 15 {
				town.InputBuf = town.InputBuf[:15]
			}
			town.PasswordFirst = town.InputBuf
			town.PasswordStep = 1
			town.InputBuf = ""
		} else {
			if town.InputBuf == town.PasswordFirst {
				town.EditChar.Password = town.PasswordFirst
				town.Message = "PASSWORD CHANGED - "
			} else {
				town.Message = "THEY ARE NOT THE SAME - YOUR PASSWORD HAS NOT BEEN CHANGED!"
			}
			town.InputMode = engine.InputCharEdit
			town.InputBuf = ""
			town.PasswordStep = 0
			town.PasswordFirst = ""
		}
	case tcell.KeyEscape:
		town.InputMode = engine.InputCharEdit
		town.InputBuf = ""
		town.PasswordStep = 0
		town.PasswordFirst = ""
	case tcell.KeyRune:
		ch := ev.Rune()
		if ch >= 'a' && ch <= 'z' {
			ch -= 32
		}
		if len(town.InputBuf) < 15 {
			town.InputBuf += string(ch)
		}
	}
	return false
}

// handleClassChange handles the class change selection screen.
// From Pascal ROLLER.TEXT CHGCLASS (lines 599-638):
//   REPEAT/UNTIL loop accepts A-H or RET. Validates CHG2LST[class] and class != current.
//   On valid selection: reset stats to race base, set new class, level=1, XP=0,
//   age character, grant starting spell, zero all spell slots, unequip non-cursed items.
func handleClassChange(game *engine.GameState, ev *tcell.EventKey) bool {
	town := game.Town
	c := town.EditChar
	if c == nil {
		town.InputMode = engine.InputTrainingName
		return false
	}

	// RET cancels — Pascal line 606-607: EXIT(CHGCLASS)
	if ev.Key() == tcell.KeyEnter || ev.Key() == tcell.KeyEscape {
		town.InputMode = engine.InputCharEdit
		town.Message = ""
		return false
	}

	if ev.Key() != tcell.KeyRune {
		return false
	}

	ch := ev.Rune()
	if ch >= 'a' && ch <= 'z' {
		ch -= 32
	}

	// Pascal lines 604-605: accept 'A' through 'H'
	if ch < 'A' || ch > 'H' {
		return false
	}

	// Pascal lines 608-613: convert letter to class index
	// A=Fighter(0), B=Mage(1), ..., H=Ninja(7)
	classIdx := engine.Class(ch - 'A')

	// Pascal line 614: UNTIL CHG2LST[CLASSX] AND NOT(CLASSX = CHARREC.CLASS)
	if !town.ClassChangeList[classIdx] || classIdx == c.Class {
		return false
	}

	// Pascal lines 616-618: SETBASE — reset stats to race base values
	base := engine.RaceBaseStats[c.Race]
	c.Strength = base[0]
	c.IQ = base[1]
	c.Piety = base[2]
	c.Vitality = base[3]
	c.Agility = base[4]
	c.Luck = base[5]

	// Pascal line 619: set new class
	c.Class = classIdx
	// Pascal line 620: level = 1
	c.Level = 1
	// Pascal lines 621-623: XP = 0
	c.XP = 0
	// Pascal line 624: AGE + 52*(RANDOM MOD 3) + 252
	// (ages character ~5-7 years — 252 weeks = ~4.8 years base + 0-2 more years)
	c.Age += 52*(rand.Intn(3)) + 252

	// Pascal lines 625-628: grant starting spell
	// SPELLSKN[3] = KATINO for Mage, SPELLSKN[23] = DIOS for Priest
	if classIdx == engine.Mage {
		c.SpellKnown[engine.SpellIndex["KATINO"]] = true
	} else if classIdx == engine.Priest {
		c.SpellKnown[engine.SpellIndex["DIOS"]] = true
	}

	// Pascal lines 629-633: zero all spell slots
	for i := 0; i < 7; i++ {
		c.MageSpells[i] = 0
		c.PriestSpells[i] = 0
		c.MaxMageSpells[i] = 0
		c.MaxPriestSpells[i] = 0
	}

	// Pascal lines 634-636: unequip all non-cursed items
	for i := 0; i < c.ItemCount; i++ {
		if c.Items[i].Equipped && !c.Items[i].Cursed {
			c.Items[i].Equipped = false
		}
	}

	// Recalculate AC (only cursed equipped items remain)
	recalcAC(c, game.Scenario.Items)

	// Recalculate spell slots for new class (SetSpells uses class+level)
	engine.SetSpells(c)

	// Copy current slots to match max (fully rested after class change)
	for i := 0; i < 7; i++ {
		c.MageSpells[i] = c.MaxMageSpells[i]
		c.PriestSpells[i] = c.MaxPriestSpells[i]
	}

	// Pascal line 637: PUTCHARC — save character
	game.Save()

	town.InputMode = engine.InputCharEdit
	town.Message = ""
	return false
}

func handleCharEdit(game *engine.GameState, ev *tcell.EventKey) bool {
	town := game.Town
	c := town.EditChar
	if c == nil {
		town.InputMode = engine.InputTrainingName
		town.InputBuf = ""
		return false
	}

	if ev.Key() == tcell.KeyEnter || ev.Key() == tcell.KeyEscape {
		town.InputMode = engine.InputTrainingName
		town.InputBuf = ""
		town.EditChar = nil
		town.Message = ""
		return false
	}

	if ev.Key() != tcell.KeyRune {
		return false
	}

	ch := ev.Rune()
	if ch >= 'a' && ch <= 'z' {
		ch -= 32
	}

	switch ch {
	case 'I': // Inspect — enter full character sheet
		town.InputMode = engine.InputInspect
		town.Message = ""
		return false
	case 'D': // Delete — confirm first (p-code proc 5: "ARE YOU SURE YOU WANT TO DELETE (Y/N) ?")
		town.InputMode = engine.InputConfirmDelete
		town.Message = ""
	case 'R':
		if game.Scenario.ScenarioNum == 3 {
			// Rite of Passage — Wiz 3 only. From Pascal ROLLER.TEXT RITEPASS.
			msg := engine.RiteCanPerform(c)
			if msg != "" {
				town.Message = msg
			} else {
				town.InputMode = engine.InputRiteCeremony
				town.Message = ""
			}
		} else {
			// Reroll — confirm first (p-code proc 6: "ARE YOU SURE YOU WANT TO REROLL (Y/N) ?")
			town.InputMode = engine.InputConfirmReroll
			town.Message = ""
		}
	case 'C': // Change class — from Pascal ROLLER.TEXT CHGCLASS (lines 574-639)
		avail := engine.CharClassQualifies(c)
		// Exclude current class (Pascal line 590: NOT (CLASSX = CHARREC.CLASS))
		avail[c.Class] = false
		hasAny := false
		for _, v := range avail {
			if v {
				hasAny = true
				break
			}
		}
		if !hasAny {
			town.Message = "NO CLASSES AVAILABLE"
		} else {
			town.ClassChangeList = avail
			town.InputMode = engine.InputClassChange
			town.Message = ""
		}
	case 'S': // Set new password — from p-code ROLLER proc 3 (IC 4480-4816)
		town.InputMode = engine.InputSetPassword
		town.InputBuf = ""
		town.Message = ""
	}
	return false
}

func handleCreationInput(game *engine.GameState, ev *tcell.EventKey) {
	cs := game.Town.Creation
	if cs == nil {
		game.Phase = engine.PhaseTown
		return
	}

	switch cs.Step {
	case engine.StepName:
		handleNameInput(game, cs, ev)
	case engine.StepRace:
		handleRaceInput(game, cs, ev)
	case engine.StepAlignment:
		handleAlignmentInput(game, cs, ev)
	case engine.StepStats:
		handleStatsInput(game, cs, ev)
	case engine.StepClass:
		handleClassInput(game, cs, ev)
	case engine.StepPassword:
		handlePasswordInput(game, cs, ev)
	case engine.StepConfirm:
		handleConfirmInput(game, cs, ev)
	}
}

func handleNameInput(game *engine.GameState, cs *engine.CreationState, ev *tcell.EventKey) {
	switch ev.Key() {
	case tcell.KeyEscape:
		// Cancel creation, return to training
		game.Phase = engine.PhaseTown
		game.Town.Creation = nil
	case tcell.KeyBackspace, tcell.KeyBackspace2:
		if len(cs.Name) > 0 {
			cs.Name = cs.Name[:len(cs.Name)-1]
		}
	case tcell.KeyEnter:
		if len(cs.Name) > 0 {
			cs.Step = engine.StepRace
			cs.SelectedIndex = 0
		}
	case tcell.KeyRune:
		ch := ev.Rune()
		if len(cs.Name) < 15 {
			if ch >= 'a' && ch <= 'z' {
				ch -= 32 // uppercase
			}
			if (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == ' ' || ch == '-' || ch == '\'' {
				cs.Name += string(ch)
			}
		}
	}
}

func handleRaceInput(game *engine.GameState, cs *engine.CreationState, ev *tcell.EventKey) {
	// From p-code ROLLER proc 17: letter A-E selects race
	if ev.Key() == tcell.KeyEscape {
		cs.Step = engine.StepName
		return
	}
	if ev.Key() == tcell.KeyRune {
		ch := ev.Rune()
		if ch >= 'a' && ch <= 'e' {
			ch -= 32
		}
		if ch >= 'A' && ch <= 'E' {
			cs.Race = engine.Race(ch - 'A')
			cs.Step = engine.StepAlignment
			cs.SelectedIndex = 0
		}
	}
}

func handleAlignmentInput(game *engine.GameState, cs *engine.CreationState, ev *tcell.EventKey) {
	// From p-code ROLLER proc 15: letter A-C selects alignment
	if ev.Key() == tcell.KeyEscape {
		cs.Step = engine.StepRace
		return
	}
	if ev.Key() == tcell.KeyRune {
		ch := ev.Rune()
		if ch >= 'a' && ch <= 'c' {
			ch -= 32
		}
		if ch >= 'A' && ch <= 'C' {
			cs.Alignment = engine.Alignment(ch - 'A')
			cs.InitStats()
			cs.Step = engine.StepStats
		}
	}
}

func handleStatsInput(game *engine.GameState, cs *engine.CreationState, ev *tcell.EventKey) {
	// Pascal GIVEPTS (ROLLER.TEXT lines 255-330):
	// +/- adjusts stats, RET cycles to next stat (wraps), ESC exits when points==0 AND a class qualifies
	switch ev.Key() {
	case tcell.KeyEscape:
		// Pascal line 311: UNTIL (INCHAR = CHR(27)) AND CANCHG AND (PTSLEFT = 0)
		if cs.BonusPoints == 0 {
			avail := cs.ClassAvailability()
			canChange := false
			for _, v := range avail {
				if v {
					canChange = true
					break
				}
			}
			if canChange {
				cs.Step = engine.StepClass
			}
		}
	case tcell.KeyEnter:
		// Pascal lines 302-309: RET cycles to next stat, wraps LUCK → STR
		if cs.StatCursor < 5 {
			cs.StatCursor++
		} else {
			cs.StatCursor = 0
		}
	case tcell.KeyRune:
		switch ev.Rune() {
		case '+', ';':
			// p-code IC 1947: '+' and ';' (Apple II shifted +) = add point
			cs.AddStatPoint()
		case '-', '=':
			// p-code IC 1970: '-' and '=' = subtract point
			cs.RemoveStatPoint()
		}
	}
}

func handleClassInput(game *engine.GameState, cs *engine.CreationState, ev *tcell.EventKey) {
	// From p-code ROLLER proc 14 (IC 2269-2309): A-H with fixed indices
	// A=Fighter, B=Mage, ..., H=Ninja. Only accepts if class is available.
	if ev.Key() == tcell.KeyEscape {
		cs.Step = engine.StepStats
		return
	}
	if ev.Key() == tcell.KeyRune {
		ch := ev.Rune()
		if ch >= 'a' && ch <= 'h' {
			ch -= 32
		}
		idx := int(ch - 'A')
		if idx >= 0 && idx < 8 {
			avail := cs.ClassAvailability()
			if avail[idx] {
				cs.Class = engine.Class(idx)
				cs.Step = engine.StepConfirm
			}
		}
	}
}

// handlePasswordInput handles password entry during character creation.
// From p-code ROLLER proc 18 (IC 1249-1431):
//   Step 0: "ENTER A PASSWORD ([RET] FOR NONE)" → type password
//   Step 1: "ENTER IT AGAIN TO BE SURE" → confirm; must match
func handlePasswordInput(game *engine.GameState, cs *engine.CreationState, ev *tcell.EventKey) {
	switch ev.Key() {
	case tcell.KeyBackspace, tcell.KeyBackspace2:
		if cs.PasswordStep == 0 {
			if len(cs.PasswordFirst) > 0 {
				cs.PasswordFirst = cs.PasswordFirst[:len(cs.PasswordFirst)-1]
			}
		} else {
			if len(cs.Password) > 0 {
				cs.Password = cs.Password[:len(cs.Password)-1]
			}
		}
	case tcell.KeyEnter:
		if cs.PasswordStep == 0 {
			// First entry done — move to confirm
			if len(cs.PasswordFirst) > 15 {
				cs.PasswordFirst = cs.PasswordFirst[:15]
			}
			cs.PasswordStep = 1
			cs.Password = ""
		} else {
			// Confirm entry — check match
			if cs.Password == cs.PasswordFirst {
				cs.Password = cs.PasswordFirst
				cs.Step = engine.StepRace
			} else {
				// Mismatch — restart password entry
				cs.PasswordStep = 0
				cs.PasswordFirst = ""
				cs.Password = ""
			}
		}
	case tcell.KeyRune:
		ch := ev.Rune()
		if ch >= 'a' && ch <= 'z' {
			ch -= 32
		}
		if cs.PasswordStep == 0 {
			if len(cs.PasswordFirst) < 15 {
				cs.PasswordFirst += string(ch)
			}
		} else {
			if len(cs.Password) < 15 {
				cs.Password += string(ch)
			}
		}
	}
}

func handleConfirmInput(game *engine.GameState, cs *engine.CreationState, ev *tcell.EventKey) {
	// From p-code ROLLER proc 13 (IC 2611-2617): Y/N only
	if ev.Key() == tcell.KeyRune {
		ch := ev.Rune()
		if ch == 'y' || ch == 'Y' {
			reroll := cs.Reroll
			c := cs.FinalizeCharacter()
			game.Town.Roster.Add(c)
			game.Town.Message = ""
			game.Town.Creation = nil
			game.Phase = engine.PhaseTown
			if reroll {
				// Reroll returns to char edit screen with the new character
				game.Town.EditChar = c
				game.Town.InputMode = engine.InputCharEdit
			}
		} else if ch == 'n' || ch == 'N' {
			// Discard and return to training grounds
			game.Town.Creation = nil
			game.Phase = engine.PhaseTown
		}
	}
}

// handleCampCastSpell handles spell name input during camp inspect.
// From p-code CAMP segment: "WHAT SPELL ? >" then spell hash lookup,
// then "CAST ON WHO" for targeted spells (proc 29, IC 708-772).
func handleCampCastSpell(game *engine.GameState, ev *tcell.EventKey) {
	town := game.Town
	if ev.Key() == tcell.KeyRune {
		ch := ev.Rune()
		if ch >= 'A' && ch <= 'Z' {
			ch += 32
		}
		if (ch >= 'a' && ch <= 'z') && len(town.InputBuf) < 12 {
			town.InputBuf += string(ch)
		}
	}
	if ev.Key() == tcell.KeyBackspace || ev.Key() == tcell.KeyBackspace2 {
		if len(town.InputBuf) > 0 {
			town.InputBuf = town.InputBuf[:len(town.InputBuf)-1]
		}
	}
	if ev.Key() == tcell.KeyEnter {
		sp := engine.LookupSpell(town.InputBuf)
		if sp == nil {
			town.Message = "** WHAT? **"
			town.InputMode = engine.InputInspect
			town.InputBuf = ""
			return
		}
		c := town.EditChar
		if !c.CanCastSpell(sp) {
			town.Message = "** CANT CAST **"
			town.InputMode = engine.InputInspect
			town.InputBuf = ""
			return
		}
		// Pascal CAMP.TEXT: IF FIZZLES > 0 THEN EXITCAST('SPELL HAS NO EFFECT')
		if game.Fizzles > 0 {
			town.Message = "SPELL HAS NO EFFECT"
			town.InputMode = engine.InputInspect
			town.InputBuf = ""
			return
		}
		// MALOR gets its own UI — Pascal UTILITIE.TEXT lines 432-473
		if sp.Name == "MALOR" {
			c.MageSpells[sp.Level-1]--
			town.MalorDeltaEW = 0
			town.MalorDeltaNS = 0
			town.MalorDeltaUD = 0
			town.InputMode = engine.InputMalor
			town.InputBuf = ""
			return
		}
		// Spell is valid and castable — check if it needs a target
		if sp.Target == engine.TargetPartyMember {
			// "CAST ON WHO" — from p-code proc 29 (IC 708)
			town.PendingSpell = sp
			town.InputMode = engine.InputSpellTarget
			town.InputBuf = ""
			return
		}
		// Non-targeted spells: deduct slot and apply immediately
		if sp.Type == engine.MageSpell {
			c.MageSpells[sp.Level-1]--
		} else {
			c.PriestSpells[sp.Level-1]--
		}
		applyCampSpell(game, sp, c)
		// Don't reset input mode if the spell set its own (e.g. DUMAPIC)
		if town.InputMode == engine.InputCastSpell {
			town.InputMode = engine.InputInspect
			town.InputBuf = ""
		}
	}
	if ev.Key() == tcell.KeyEscape {
		town.InputMode = engine.InputInspect
		town.InputBuf = ""
		town.Message = ""
	}
}

// handleCampSpellTarget handles "CAST ON WHO" target selection.
// From p-code CAMP proc 29 (IC 708): WIZARDRY.proc15 selects party member.
func handleCampSpellTarget(game *engine.GameState, ev *tcell.EventKey) {
	town := game.Town
	if ev.Key() == tcell.KeyRune {
		ch := ev.Rune()
		if ch >= '1' && ch <= '6' {
			idx := int(ch - '1')
			if idx < len(town.Party.Members) && town.Party.Members[idx] != nil {
				target := town.Party.Members[idx]
				sp := town.PendingSpell
				caster := town.EditChar
				// Deduct spell slot from caster
				if sp.Type == engine.MageSpell {
					caster.MageSpells[sp.Level-1]--
				} else {
					caster.PriestSpells[sp.Level-1]--
				}
				// Apply to target
				applyCampSpell(game, sp, target)
				town.PendingSpell = nil
				town.InputMode = engine.InputInspect
			}
		}
	}
	if ev.Key() == tcell.KeyEnter || ev.Key() == tcell.KeyEscape {
		town.PendingSpell = nil
		town.InputMode = engine.InputInspect
		town.Message = ""
	}
}

// applyCampSpell applies a camp-castable spell effect to a target character.
func applyCampSpell(game *engine.GameState, sp *engine.Spell, target *engine.Character) {
	town := game.Town
	switch sp.Name {
	case "DIOS":
		target.HP += engine.RollDicePublic(1, 8, 0)
		if target.HP > target.MaxHP {
			target.HP = target.MaxHP
		}
		town.Message = fmt.Sprintf("%s HEALED", target.Name)
	case "DIALMA":
		target.HP += engine.RollDicePublic(3, 8, 0)
		if target.HP > target.MaxHP {
			target.HP = target.MaxHP
		}
		town.Message = fmt.Sprintf("%s HEALED", target.Name)
	case "DIAL":
		target.HP += engine.RollDicePublic(2, 8, 0)
		if target.HP > target.MaxHP {
			target.HP = target.MaxHP
		}
		town.Message = fmt.Sprintf("%s HEALED", target.Name)
	case "MADI":
		target.HP = target.MaxHP
		town.Message = fmt.Sprintf("%s FULLY HEALED", target.Name)
	case "LATUMOFIS":
		target.PoisonAmt = 0
		town.Message = fmt.Sprintf("%s POISON CURED", target.Name)
	case "MILWA":
		// Pascal CAMP.TEXT line 433: LIGHT := 15 + (RANDOM MOD 15)
		game.LightLevel = 15 + rand.Intn(15)
		town.Message = "LIGHT!"
	case "LOMILWA":
		// Pascal CAMP.TEXT line 443: LIGHT := 32000 (effectively permanent)
		game.LightLevel = 32000
		town.Message = "LIGHT!"
	case "DUMAPIC":
		// Pascal UTILITIE.TEXT p-code IC 1872-2314: full-screen location display
		town.InputMode = engine.InputDumapic
		return
	default:
		town.Message = "NOT IN CAMP"
	}
}

// handleCampUseItem handles item use during camp inspect.
// Camp-only — not available from town inspect.
func handleCampUseItem(game *engine.GameState, ev *tcell.EventKey) {
	town := game.Town
	if ev.Key() == tcell.KeyRune {
		ch := ev.Rune()
		if ch == '0' {
			town.InputMode = engine.InputInspect
			town.Message = ""
		} else if ch >= '1' && ch <= '8' {
			idx := int(ch-'0') - 1
			c := town.EditChar
			if idx < c.ItemCount {
				item := game.Scenario.Items[c.Items[idx].ItemIndex]
				if item.SpellPower == 0 {
					town.Message = "** POWERLESS **"
				} else {
					town.Message = "** POWERLESS **"
				}
			}
			town.InputMode = engine.InputInspect
		}
	}
	if ev.Key() == tcell.KeyEnter || ev.Key() == tcell.KeyEscape {
		town.InputMode = engine.InputInspect
		town.Message = ""
	}
}

// handleMalorInput handles the MALOR teleport displacement UI.
// From Pascal UTILITIE.TEXT lines 432-473: N/S/E/W/U/D adjust displacement,
// RETURN teleports, ESC cancels.
func handleMalorInput(game *engine.GameState, ev *tcell.EventKey) {
	town := game.Town

	switch ev.Key() {
	case tcell.KeyEscape:
		// "CHICKEN OUT" — cancel without teleporting (slot already spent)
		town.InputMode = engine.InputInspect
		town.Message = ""
	case tcell.KeyEnter:
		// TELEPORT — Pascal UTILITIE.TEXT lines 403-429
		// Pascal wraps X/Y mod 20 (coordinates are always 0-19)
		newX := ((game.PlayerX + town.MalorDeltaEW) % 20 + 20) % 20
		newY := ((game.PlayerY + town.MalorDeltaNS) % 20 + 20) % 20
		newLevel := game.MazeLevel + town.MalorDeltaUD

		maxLevel := len(game.Scenario.Mazes.Levels)

		// Check ROCK death: beyond max dungeon level
		if newLevel > 0 && newLevel > maxLevel {
			// "YOU LANDED IN SOLID ROCK" — all party LOST
			town.Message = "TELEPORTED INTO ROCK!"
			town.Message2 = "YOU ARE LOST FOREVER!"
			for _, m := range town.Party.Members {
				if m != nil {
					m.InMaze = false
					m.Status = engine.Lost
				}
			}
			game.Phase = engine.PhaseTown
			town.Location = engine.Castle
			town.InputMode = engine.InputNone
			return
		}

		// Check MOAT: level 0 but not at (0,0)
		if newLevel == 0 && !(newX == 0 && newY == 0) {
			town.Message = "TELEPORTED INTO THE MOAT!"
			town.Message2 = "YOU ARE LOST FOREVER!"
			for _, m := range town.Party.Members {
				if m != nil {
					m.InMaze = false
					m.Status = engine.Lost
				}
			}
			game.Phase = engine.PhaseTown
			town.Location = engine.Castle
			town.InputMode = engine.InputNone
			return
		}

		// Check VOLCANO: level < 0
		if newLevel < 0 {
			town.Message = "TELEPORTED INTO VOLCANO!"
			town.Message2 = "YOU ARE LOST FOREVER!"
			for _, m := range town.Party.Members {
				if m != nil {
					m.InMaze = false
					m.Status = engine.Lost
				}
			}
			game.Phase = engine.PhaseTown
			town.Location = engine.Castle
			town.InputMode = engine.InputNone
			return
		}

		// Level 0 at (0,0) = back to castle
		if newLevel == 0 && newX == 0 && newY == 0 {
			town.Message = "TELEPORTED TO CASTLE"
			for _, m := range town.Party.Members {
				if m != nil {
					m.InMaze = false
					m.PoisonAmt = 0
				}
			}
			game.Phase = engine.PhaseTown
			town.Location = engine.Castle
			town.InputMode = engine.InputNone
			return
		}

		// Valid teleport within dungeon
		game.PlayerX = newX
		game.PlayerY = newY
		game.MazeLevel = newLevel
		town.Message = fmt.Sprintf("TELEPORTED TO L%d (%d,%d)", newLevel+1, newX, newY)
		town.InputMode = engine.InputInspect
		game.Phase = engine.PhaseCamp

	case tcell.KeyRune:
		ch := ev.Rune()
		if ch >= 'a' && ch <= 'z' {
			ch -= 32
		}
		switch ch {
		case 'N':
			town.MalorDeltaNS++
		case 'S':
			town.MalorDeltaNS--
		case 'E':
			town.MalorDeltaEW++
		case 'W':
			town.MalorDeltaEW--
		case 'D':
			town.MalorDeltaUD++
		case 'U':
			town.MalorDeltaUD--
		}
	}
}

func handleCampInput(game *engine.GameState, ev *tcell.EventKey) {
	// Camp-only modes: cast spell, spell target, use item, MALOR
	if game.Town.InputMode == engine.InputCastSpell {
		handleCampCastSpell(game, ev)
		return
	}
	if game.Town.InputMode == engine.InputSpellTarget {
		handleCampSpellTarget(game, ev)
		return
	}
	if game.Town.InputMode == engine.InputMalor {
		handleMalorInput(game, ev)
		return
	}
	if game.Town.InputMode == engine.InputDumapic {
		// Pascal p-code IC 2304-2314: wait for 'L' key to leave
		if ev.Key() == tcell.KeyRune && (ev.Rune() == 'L' || ev.Rune() == 'l') {
			game.Town.InputMode = engine.InputInspect
			game.Town.Message = ""
		}
		return
	}
	if game.Town.InputMode == engine.InputUseItem {
		handleCampUseItem(game, ev)
		return
	}

	// Shared inspect/equip/trade modes — route through the shared prompt handler

	if game.Town.InputMode == engine.InputInspect ||
		game.Town.InputMode == engine.InputEquip ||
		game.Town.InputMode == engine.InputDrop ||
		game.Town.InputMode == engine.InputTrade ||
		game.Town.InputMode == engine.InputTradeGold ||
		game.Town.InputMode == engine.InputTradeTarget ||
		game.Town.InputMode == engine.InputSpellBooks ||
		game.Town.InputMode == engine.InputSpellList {
		handleTownPrompt(game, ev)
		return
	}

	// Reorder mode — ">>" walks each slot, user picks who goes there
	if game.Town.InputMode == engine.InputReorder {
		if ev.Key() == tcell.KeyRune {
			ch := ev.Rune()
			if ch >= '1' && ch <= '6' {
				handleReorderPick(game, int(ch-'0'))
			}
		}
		return
	}

	if ev.Key() != tcell.KeyRune {
		return
	}
	ch := ev.Rune()
	if ch >= 'a' && ch <= 'z' {
		ch -= 32
	}
	game.Town.Message = ""

	switch ch {
	case 'L': // Leave camp → enter maze
		game.Phase = engine.PhaseMaze
		// Check if at stairs (0,0) — original shows "STAIRS GOING UP" immediately
		cell := game.CurrentCell()
		if cell != nil && cell.Type == data.SqStairs {
			if cell.DestLevel == 0 {
				game.MazeMessage = "STAIRS GOING UP."
			} else {
				game.MazeMessage = "STAIRS GOING DOWN."
			}
			game.MazeMessage2 = "TAKE THEM (Y/N) ?"
		}
	case 'R': // Reorder — from UTILITIE p-code byte 7121
		if game.Town.Party.Size() > 1 {
			game.Town.InputMode = engine.InputReorder
			game.Town.ReorderPos = 0
			game.Town.ReorderResult = nil
			game.Town.Message = ""
		}
	case 'E': // Equip — equip ALL party members in sequence
		// Pascal CAMP2.TEXT line 501-504: XGOTO := XEQPDSP; LLBASE04 := -1
		// LLBASE04 = -1 means equip all party members, iterating through each
		game.Town.EquipPartyMode = true
		game.Town.EquipPartyIdx = 0
		// Find first alive party member
		for game.Town.EquipPartyIdx < len(game.Town.Party.Members) {
			m := game.Town.Party.Members[game.Town.EquipPartyIdx]
			if m != nil && m.IsAlive() {
				break
			}
			game.Town.EquipPartyIdx++
		}
		if game.Town.EquipPartyIdx < len(game.Town.Party.Members) {
			game.Town.EditChar = game.Town.Party.Members[game.Town.EquipPartyIdx]
			startEquipFlow(game, game.Town)
		}
	case 'D': // Disband — from p-code CAMP proc 2 (IC 5834)
		// Save party members back to roster, age +25 weeks, return to town
		for i, m := range game.Town.Party.Members {
			if m != nil {
				m.Age += 25 // p-code: age increases 25 weeks per disband
				game.Town.Party.Members[i] = nil
			}
		}
		game.Phase = engine.PhaseTown
		goToCastle(game.Town)
	case '1', '2', '3', '4', '5', '6': // Inspect — enter full inspect screen
		idx := int(ch-'0') - 1
		if idx < len(game.Town.Party.Members) && game.Town.Party.Members[idx] != nil {
			game.Town.EditChar = game.Town.Party.Members[idx]
			game.Town.InputMode = engine.InputInspect
			game.Town.Message = ""
		}
	}
}

func handleMazeInput(screen *render.Screen, game *engine.GameState, ev *tcell.EventKey) bool {
	// Map view: arrow keys pan, C re-centers, any other key dismisses
	if game.ShowMap {
		switch ev.Key() {
		case tcell.KeyUp:
			game.MapScrollY--
			return false
		case tcell.KeyDown:
			game.MapScrollY++
			return false
		case tcell.KeyLeft:
			game.MapScrollX--
			return false
		case tcell.KeyRight:
			game.MapScrollX++
			return false
		case tcell.KeyRune:
			ch := ev.Rune()
			if ch == 'c' || ch == 'C' {
				game.MapScrollX = 0
				game.MapScrollY = 0
				return false
			}
		}
		// Any other key: dismiss map and reset scroll
		game.ShowMap = false
		game.MapScrollX = 0
		game.MapScrollY = 0
		return false
	}

	// T)IME delay input — from RUNNER proc 5 (IC 3936):
	// "NEW DELAY (1-5000) >" — reads numeric string, stores in global 7
	if game.MazeDelayInput {
		if ev.Key() == tcell.KeyRune {
			ch := ev.Rune()
			if ch >= '0' && ch <= '9' && len(game.MazeDelayBuf) < 4 {
				game.MazeDelayBuf += string(ch)
			}
		}
		if ev.Key() == tcell.KeyBackspace || ev.Key() == tcell.KeyBackspace2 {
			if len(game.MazeDelayBuf) > 0 {
				game.MazeDelayBuf = game.MazeDelayBuf[:len(game.MazeDelayBuf)-1]
			}
		}
		if ev.Key() == tcell.KeyEnter {
			val := 0
			for _, c := range game.MazeDelayBuf {
				val = val*10 + int(c-'0')
			}
			if val > 0 && val <= 5000 {
				game.MazeDelay = val
			}
			game.MazeDelayInput = false
			game.MazeDelayBuf = ""
			game.MazeMessage = ""
			game.MazeMessage2 = ""
		}
		if ev.Key() == tcell.KeyEscape {
			game.MazeDelayInput = false
			game.MazeDelayBuf = ""
			game.MazeMessage = ""
			game.MazeMessage2 = ""
		}
		return false
	}

	// I)NSPECT dungeon screen — from SPECIALS proc 38:
	// P)ICK UP (if characters found) or L)EAVE
	if game.MazeInspecting {
		if ev.Key() == tcell.KeyRune {
			ch := ev.Rune()
			if ch == 'l' || ch == 'L' {
				game.MazeInspecting = false
				game.MazeInspectFound = nil
			} else if (ch == 'p' || ch == 'P') && len(game.MazeInspectFound) > 0 {
				// P)ICK UP — from p-code SPECIALS CLP 5:
				// Add found characters back to party (if room)
				for _, c := range game.MazeInspectFound {
					c.InMaze = false
					if len(game.Town.Party.Members) < 6 {
						game.Town.Party.Members = append(game.Town.Party.Members, c)
					}
				}
				game.MazeInspecting = false
				game.MazeInspectFound = nil
			}
		}
		return false
	}

	// Handle SCNMSG pagination — only [RET] advances (matches "[RET] FOR MORE" prompt)
	// All other keys are consumed while messages are showing (prevents key repeat bleed)
	if len(game.MazeMessages) > 0 {
		if ev.Key() == tcell.KeyEnter {
			if game.MazeMsgWait {
				// More pages to show — advance scroll
				game.MazeMsgScroll += 4
				if game.MazeMsgScroll >= len(game.MazeMessages) {
					game.MazeMessages = nil
					game.MazeMsgScroll = 0
					game.MazeMsgWait = false
				} else {
					game.MazeMsgWait = game.MazeMsgScroll+4 < len(game.MazeMessages)
				}
			} else {
				// Final page shown — Enter dismisses
				game.MazeMessages = nil
				game.MazeMsgScroll = 0
				// If GETYN pending, show the search prompt now
				if game.MazeSearchYN {
					game.MazeMessage = "SEARCH (Y/N) ?"
				}
				// BCK2SHOP (AUX2=8): warp party back to castle after message.
				// Pascal BCK2SHOP: MAZELEV := 0; XGOTO := XNEWMAZE.
				if game.MazePendingBack2Shop {
					game.MazePendingBack2Shop = false
					for _, m := range game.Town.Party.Members {
						if m != nil {
							m.InMaze = false
						}
					}
					game.MazeMessage = ""
					game.Phase = engine.PhaseTown
					game.Town.Location = engine.Castle
					game.Town.InputMode = engine.InputNone
				}
				// TRYGET (AUX2=2): give item to first eligible party member.
				// Pascal GOTITEM (SPECIALS2.TEXT P010315 w/ WC034 fix): iterate party,
				// per char show "ALREADY HAS ONE" / "IS FULL" / "GOT ITEM" and stop on
				// first successful give. EQINDEX stores the raw item index (0-99).
				if game.MazePendingTryGet {
					itemIdx := game.MazeTryGetItem
					game.MazePendingTryGet = false
					game.MazeTryGetItem = 0
					for _, m := range game.Town.Party.Members {
						if m == nil {
							continue
						}
						alreadyHas := false
						for i := 0; i < m.ItemCount; i++ {
							if m.Items[i].ItemIndex == itemIdx {
								alreadyHas = true
								break
							}
						}
						if alreadyHas {
							game.MazeMessage = m.Name + " ALREADY HAS ONE"
							continue
						}
						if m.ItemCount >= 8 {
							game.MazeMessage = m.Name + " IS FULL"
							continue
						}
						m.Items[m.ItemCount] = engine.Possession{
							ItemIndex:  itemIdx,
							Equipped:   false,
							Identified: false,
						}
						m.ItemCount++
						game.MazeMessage = m.Name + " GOT ITEM"
						break
					}
				}
			}
		}
		return false // consume ALL keys while messages are showing
	}

	// Handle "SEARCH (Y/N) ?" prompt — Pascal GETYN (SPECIALS2.TEXT P010319)
	// AUX2=4: Y branches on sign of AUX0 — positive triggers combat with AUX0
	// as monster index, negative calls TRYGET with ABS(AUX0) as quest item ID.
	// N exits SPECIALS with no effect.
	if game.MazeSearchYN {
		if ev.Key() == tcell.KeyRune {
			ch := ev.Rune()
			if ch == 'y' || ch == 'Y' {
				game.MazeSearchYN = false
				game.MazeMessage = ""
				cell := game.SearchCell
				aux0 := game.SearchMonster
				if cell != nil {
					if aux0 > 0 {
						// Combat with monster AUX0 (e.g., Murphy's Ghost = 77)
						cell.SpclMonster = aux0
						game.MazeMessage = "AN ENCOUNTER"
						game.Combat = engine.NewCombat(game)
						game.Combat.EncounterType = 0
						game.Phase = engine.PhaseCombat
					} else if aux0 < 0 {
						// TRYGET: give ABS(AUX0) as raw item index.
						// SPCMISC pre-processing already applied AUX0 += 1000 for values
						// ≤ -1000, so ABS(aux0) is the raw item index (0-99). Matches
						// Pascal GOTITEM (SPECIALS2.TEXT P010315 w/ WC034 messages).
						itemIdx := -aux0
						for _, m := range game.Town.Party.Members {
							if m == nil {
								continue
							}
							alreadyHas := false
							for i := 0; i < m.ItemCount; i++ {
								if m.Items[i].ItemIndex == itemIdx {
									alreadyHas = true
									break
								}
							}
							if alreadyHas {
								game.MazeMessage = m.Name + " ALREADY HAS ONE"
								continue
							}
							if m.ItemCount >= 8 {
								game.MazeMessage = m.Name + " IS FULL"
								continue
							}
							m.Items[m.ItemCount] = engine.Possession{
								ItemIndex:  itemIdx,
								Equipped:   false,
								Identified: false,
							}
							m.ItemCount++
							game.MazeMessage = m.Name + " GOT ITEM"
							break
						}
					}
				}
				game.SearchCell = nil
			} else if ch == 'n' || ch == 'N' {
				game.MazeSearchYN = false
				game.MazeMessage = ""
				game.SearchCell = nil
			}
		}
		return false
	}

	if ev.Key() != tcell.KeyRune {
		return false
	}
	ch := ev.Rune()
	if ch >= 'a' && ch <= 'z' {
		ch -= 32
	}

	// If waiting for button press — from p-code RUNNER IC 3047
	if game.WaitButton {
		if ev.Key() == tcell.KeyEnter {
			// Leave buttons alone
			game.WaitButton = false
			game.ButtonCount = 0
			game.MazeMessage = ""
			game.MazeMessage2 = ""
		} else if ch >= 'A' && ch <= 'A'+rune(game.ButtonCount-1) {
			buttonIdx := int(ch - 'A')
			game.WaitButton = false
			game.ButtonCount = 0
			// BUTTONS effect — Pascal RUNNER2.TEXT lines 330-354
			// EXITRUN(MINBUT + button_index): teleport to level, keep X,Y
			cell := game.CurrentCell()
			if cell != nil && len(cell.Aux) > 2 {
				minBut := cell.Aux[2] // AUX2 = MINBUT
				if cell.Aux[0] > 0 {
					// AUX0 > 0: randomize position
					game.PlayerX = rand.Intn(20)
					game.PlayerY = rand.Intn(20)
				}
				newLevel := minBut + buttonIdx // 1-based level
				game.MazeLevel = newLevel - 1  // convert to 0-based
				game.MazeMessage = ""
				game.MazeMessage2 = ""
			}
		}
		return false
	}

	// If waiting for Y/N at stairs prompt
	if game.MazeMessage2 == "TAKE THEM (Y/N) ?" {
		if ch == 'Y' {
			cell := game.CurrentCell()
			if cell != nil && cell.Type == data.SqStairs {
				if cell.DestLevel == 0 {
					// Stairs up to castle — Pascal does NOT cure poison on return
					for _, m := range game.Town.Party.Members {
						if m != nil {
							m.InMaze = false
						}
					}
					game.Phase = engine.PhaseTown
					game.Town.Location = engine.Castle
					game.Town.Message = ""
				} else {
					game.MazeLevel = cell.DestLevel - 1 // convert 1-based to 0-based
					game.PlayerX = cell.DestX
					game.PlayerY = cell.DestY
					game.InitFightMap()
				}
			}
			game.MazeMessage = ""
			game.MazeMessage2 = ""
		} else if ch == 'N' {
			game.MazeMessage = ""
			game.MazeMessage2 = ""
		}
		return false
	}

	// TRYGET (AUX2=2): single-line message path — give item on first keypress after message.
	// Multi-line path fires in the MazeMessages dismissal handler above.
	if game.MazePendingTryGet {
		eqIdx := game.MazeTryGetItem + 1000
		game.MazePendingTryGet = false
		game.MazeTryGetItem = 0
		for _, m := range game.Town.Party.Members {
			if m == nil || m.ItemCount >= 8 {
				continue
			}
			alreadyHas := false
			for i := 0; i < m.ItemCount; i++ {
				if m.Items[i].ItemIndex == eqIdx {
					alreadyHas = true
					break
				}
			}
			if !alreadyHas {
				m.Items[m.ItemCount] = engine.Possession{
					ItemIndex:  eqIdx,
					Equipped:   false,
					Identified: false,
				}
				m.ItemCount++
				game.MazeMessage = m.Name + " GOT ITEM"
				return false // consume key — player must press again to dismiss "GOT ITEM"
			}
		}
	}
	game.MazeMessage = ""
	game.MazeMessage2 = ""
	game.MazeMessages = nil
	game.MazeMsgScroll = 0
	game.MazeMsgWait = false
	game.MazeSearchYN = false
	game.SearchCell = nil
	game.SearchMonster = 0
	game.MazePendingBack2Shop = false
	game.MazePendingTryGet = false
	game.MazeTryGetItem = 0
	game.ViewportMsg = ""
	game.ViewportMsg2 = ""
	moved := false // track whether player actually moved to a new square

	switch ch {
	// Original Wizardry maze controls from p-code RUNNER segment
	case 'F', 'W': // Forward
		if game.MoveForward() {
			moved = true
		} else {
			game.ViewportMsg = "OUCH!"
			screen.Beep()
		}
	case 'L', 'A': // Turn left
		game.TurnLeft()
	case 'R', 'D': // Turn right
		game.TurnRight()
	case 'K': // Pascal KICK: moves through anything ≠ WALL
		if game.KickDoor() {
			game.PlayerX = (game.PlayerX + game.Facing.DX() + 20) % 20
			game.PlayerY = (game.PlayerY + game.Facing.DY() + 20) % 20
			moved = true
		} else {
			game.ViewportMsg = "OUCH!"
			screen.Beep()
		}
	case 'C': // Camp — from p-code RUNNER: sets global[10]=12, exits to CAMP segment
		game.Phase = engine.PhaseCamp
	case 'S': // S)TATUS — from p-code RUNNER (CIP 11): redraws party status
		// In the original, this refreshed the party display area.
		// Our renderer redraws every frame, so just clear any stale messages.
		game.MazeMessage = ""
		game.MazeMessage2 = ""
	case 'Q': // Q)UICK — from p-code RUNNER proc 6 (IC 4070): toggle quick plot mode
		game.QuickPlot = !game.QuickPlot
		if game.QuickPlot {
			game.MazeMessage = "QUICK PLOT ON"
		} else {
			game.MazeMessage = "QUICK PLOT OFF"
		}
	case 'T': // T)IME — from p-code RUNNER proc 5 (IC 3936):
		// Prompts "NEW DELAY (1-5000) >" and sets animation delay value.
		// Controls wireframe drawing speed on the Apple II.
		game.MazeMessage = "NEW DELAY (1-5000) >"
		game.MazeMessage2 = ""
		game.MazeDelayInput = true
		game.MazeDelayBuf = ""
	case 'I': // I)NSPECT — from p-code SPECIALS proc 38: search current dungeon square
		// Full separate screen: "LOOKING...", then "FOUND:" + search results
		// Searches for lost characters at current (x,y,level)
		game.MazeInspecting = true
		game.MazeInspectFound = nil
		// Search roster for characters lost at current position
		// From p-code proc 40 (IC 82-292): loops all roster chars,
		// checks INMAZE flag and position match
		for _, c := range game.Town.Roster.Characters {
			if c == nil || c.Status == engine.Lost {
				continue
			}
			// Characters in the party are not "lost" in the dungeon
			inParty := false
			for _, m := range game.Town.Party.Members {
				if m == c {
					inParty = true
					break
				}
			}
			if !inParty && c.InMaze && c.MazeLevel == game.MazeLevel &&
				c.MazeX == game.PlayerX && c.MazeY == game.PlayerY {
				game.MazeInspectFound = append(game.MazeInspectFound, c)
			}
		}
	case 'M': // Map view
		game.ShowMap = true
		game.MapScrollX = 0
		game.MapScrollY = 0
	}

	// Pascal DRAWMAZE: light decrements on every screen redraw (turn, kick, forward)
	if game.LightLevel > 0 {
		game.LightLevel--
	}

	// Only check special squares, apply poison, and roll encounters after actual movement
	if moved {
		checkSquare(game)

		// Pascal UPDATEHP (RUNNER2.TEXT lines 398-436):
		// Fires with 25% probability per step: (RANDOM MOD 4) = 2
		// Net change: HPLEFT += HEALPTS - POISNAMT (regeneration offsets poison)
		if rand.Intn(4) == 2 {
			for _, m := range game.Town.Party.Members {
				if m != nil && m.Status == engine.OK {
					healPts := m.GetHealPts(game.Scenario.Items)
					if m.PoisonAmt > 0 || healPts > 0 {
						m.HP += healPts - m.PoisonAmt
						if m.HP <= 0 {
							m.HP = 0
							m.Status = engine.Dead
						} else if m.HP > m.MaxHP {
							m.HP = m.MaxHP
						}
					}
				}
			}
		}

		// Pascal RUNMAIN (RUNNER2.TEXT lines 616-624): encounter triggers
		// 1. RANDOM MOD 99 = 35 (1/99 chance)
		// 2. CHSTALRM = 1 (alarm trap)
		// 3. FIGHTMAP[x][y] = true (fight zone)
		if game.Phase == engine.PhaseMaze {
			triggerEncounter := rand.Intn(99) == 35
			if game.ChestAlarm == 1 {
				triggerEncounter = true
			}
			if game.FightMap[game.PlayerX][game.PlayerY] {
				triggerEncounter = true
			}
			if triggerEncounter {
				combat := engine.NewCombat(game)
				// Pascal ENCOUNTR (RUNNER2.TEXT lines 134-143): set ATTK012
				level := &game.Scenario.Mazes.Levels[game.MazeLevel]
				cell := level.Cells[game.PlayerY][game.PlayerX]
				if game.ChestAlarm == 1 {
					combat.EncounterType = 2
					combat.Surprised = 2 // alarm: monsters surprise party
					game.ChestAlarm = 0
				} else if cell.Encounter {
					// Fight-zone square: check FIGHTMAP
					if game.FightMap[game.PlayerX][game.PlayerY] {
						combat.EncounterType = 2 // first encounter here
					} else {
						combat.EncounterType = 1 // already cleared
					}
				} else {
					combat.EncounterType = 0 // random encounter
				}
				game.Combat = combat
				game.Phase = engine.PhaseCombat
				// Clear fight room after combat triggers
				game.ClearFightRoom(game.PlayerX, game.PlayerY)
			}
		}
	}

	return false
}

func checkSquare(game *engine.GameState) {
	cell := game.CurrentCell()
	if cell == nil {
		return
	}
	// Pascal SPECSQAR: FIZZLES := 0 at start, set to 1 only for fizzle squares
	game.Fizzles = 0
	// Square effects from RUNNER segment p-code
	switch cell.Type {
	case data.SqStairs:
		if cell.DestLevel < game.MazeLevel+1 {
			game.MazeMessage = "STAIRS GOING UP."
		} else {
			game.MazeMessage = "STAIRS GOING DOWN."
		}
		game.MazeMessage2 = "TAKE THEM (Y/N) ?"
	case data.SqDark:
		// Pascal VERYDARK (RUNNER2.TEXT lines 214-221):
		// GOTOXY(2,5) "IT'S VERY" / GOTOXY(2,6) "DARK HERE"
		game.ViewportMsg = "IT'S VERY"
		game.ViewportMsg2 = "DARK HERE"
		game.LightLevel = 0
	case data.SqChute:
		// From p-code proc 22 (IC 2206): "A CHUTE!" + damage + move down
		game.MazeMessage = "A CHUTE!"
		hazardDamage(game)
		if cell.DestLevel > 0 {
			game.MazeLevel = cell.DestLevel - 1
			game.PlayerX = cell.DestX
			game.PlayerY = cell.DestY
			game.InitFightMap()
		}
	case data.SqPit:
		// From p-code proc 18 (IC 2643): "A PIT!" + damage
		game.MazeMessage = "A PIT!"
		hazardDamage(game)
	case data.SqEncounter, data.SqEncounter2:
		game.Combat = engine.NewCombat(game)
		game.Combat.EncounterType = 2 // fixed encounter → Reward2 (chest)
		game.Phase = engine.PhaseCombat
		return
	case data.SqButtons:
		// From p-code RUNNER IC 2967-3047
		game.MazeMessage = "THERE ARE BUTTONS ON THE WALL"
		if len(cell.Aux) > 1 && cell.Aux[1] > 0 {
			lastBtn := 'A' + rune(cell.Aux[1]-1)
			game.MazeMessage2 = fmt.Sprintf("MARKED A-%c. PRESS ONE OR [RET]", lastBtn)
			game.ButtonCount = cell.Aux[1]
			game.WaitButton = true
		}
	case data.SqSpclEnctr:
		// Pascal CHENCOUN (RUNNER2.TEXT lines 290-319) + SPCMISC (SPECIALS2.TEXT):
		// If FIGHTMAP set and AUX0 != 0: trigger encounter (CHENCOUN).
		// Otherwise: fall through to SPCMISC message/event handler using AUX2.
		if cell.Count == 0 {
			break
		}
		if cell.Encounter && game.FightMap[game.PlayerX][game.PlayerY] {
			// CHENCOUN: trigger combat encounter
			game.MazeMessage = "AN ENCOUNTER"
			cell.SpclMonster = cell.Count
			game.Combat = engine.NewCombat(game)
			game.Combat.EncounterType = 2
			game.Phase = engine.PhaseCombat
			return
		}
		// SPCMISC fallthrough: handle via AUX2 dispatch (messages, events)
		// Pascal SPECIALS2.TEXT lines 657-722
		aux2 := cell.Aux2
		if aux2 == 0 {
			break
		}
		// AUX2=1: message with counter depletion
		// AUX2=8: back-to-shop with counter depletion
		// AUX2=4: GETYN — see AUX0 transform below (Pascal SPCMISC)
		// Pascal SPECIALS2.TEXT lines 626-645: AUX0 transforms only apply when AUX2 in {1,4,8}
		aux0 := cell.Count
		if aux2 == 1 || aux2 == 8 {
			if cell.Count > 0 {
				cell.Count--
				if cell.Count == 0 {
					cell.Type = data.SqNormal // convert to normal when depleted
				}
			}
		} else if aux2 == 4 {
			// Pascal: IF AUX0 < 0 THEN
			//   IF AUX0 > -1000 THEN MAZEFLOR.AUX0 := 0  (one-shot trigger consumed)
			//   ELSE AUX0 := AUX0 + 1000                  (quest-item encoding)
			if aux0 < 0 {
				if aux0 > -1000 {
					cell.Count = 0
				} else {
					aux0 = aux0 + 1000
				}
			}
		}
		// Pascal: IF NOT((AUX2 > 12) OR (AUX2 = 5) OR (AUX2 = 6)) THEN DOMSG(AUX1, ...)
		// AUX2=5 (ITM2PASS) and AUX2=6 (CHKALIGN) skip the main message; any message
		// comes from within BOUNCEBK after the item/align check fails.
		showMsg := aux2 != 5 && aux2 != 6 && aux2 <= 12
		if showMsg {
			// Display message from AUX1 line index (Pascal DOMSG)
			block := game.Scenario.MessageBlock(cell.Aux1)
			if block != nil {
				if len(block) == 1 {
					game.MazeMessage = block[0]
				} else if len(block) > 1 {
					game.MazeMessages = block
					game.MazeMsgScroll = 0
					game.MazeMsgWait = len(block) > 4
				}
			}
		}
		switch aux2 {
		case 2:
			// TRYGET (SPECIALS2.TEXT P010316): after message, give item AUX0 to first
			// party member. Pascal GOTITEM stores EQINDEX := ITEMX directly.
			game.MazePendingTryGet = true
			game.MazeTryGetItem = aux0
		case 4:
			// GETYN (SPECIALS2.TEXT P010319): after message, show "SEARCH (Y/N) ?"
			// On Y: positive AUX0 → combat with monster AUX0; negative AUX0 → TRYGET
			// with ABS(AUX0) as item index. aux0 already has the -1000 transform applied.
			game.MazeSearchYN = true
			game.SearchCell = cell
			game.SearchMonster = aux0
		case 5:
			// ITM2PASS (SPECIALS2.TEXT P01031B): pass if party holds item AUX0, else BOUNCEBK.
			// Pascal checks POSS.EQINDEX == AUX0 directly — item indices are raw (0-99).
			itemTarget := cell.Count
			partyHasItem := false
			for _, m := range game.Town.Party.Members {
				if m == nil {
					continue
				}
				for i := 0; i < m.ItemCount; i++ {
					if m.Items[i].ItemIndex == itemTarget {
						partyHasItem = true
					}
				}
			}
			if !partyHasItem {
				// BOUNCEBK: reverse last movement direction, then show message at AUX1.
				// Pascal BOUNCEBK: CASE DIRECTIO OF 0: MAZEY-=1; 1: MAZEX-=1; ...
				rev := game.Facing.Reverse()
				game.PlayerX = ((game.PlayerX + rev.DX()) % 20 + 20) % 20
				game.PlayerY = ((game.PlayerY + rev.DY()) % 20 + 20) % 20
				// BOUNCEBK shows DOMSG(AUX1, FALSE) if AUX1 >= 0.
				if cell.Aux1 >= 0 {
					block := game.Scenario.MessageBlock(cell.Aux1)
					if block != nil {
						if len(block) == 1 {
							game.MazeMessage = block[0]
						} else if len(block) > 1 {
							game.MazeMessages = block
							game.MazeMsgScroll = 0
							game.MazeMsgWait = len(block) > 4
						}
					}
				}
			}
		case 8:
			// BCK2SHOP: after message, warp party back to castle.
			// Pascal BCK2SHOP: MAZELEV := 0; XGOTO := XNEWMAZE (SPECIALS2 line 353-358).
			game.MazePendingBack2Shop = true
		case 9:
			// LOOKOUT (SPECIALS2.TEXT P01032A): fill FIGHTMAP in radius AUX0 around player.
			// For SqSpclEnctr, AUX0 = cell.Count (radius).
			radius := cell.Count
			for x2 := -radius; x2 <= radius; x2++ {
				for y2 := -radius; y2 <= radius; y2++ {
					fx := ((game.PlayerX + x2) % 20 + 20) % 20
					fy := ((game.PlayerY + y2) % 20 + 20) % 20
					game.FightMap[fx][fy] = true
				}
			}
			game.FightMap[game.PlayerX][game.PlayerY] = false
		}
	case data.SqScnMsg:
		// Pascal SPCMISC (SPECIALS2.TEXT): dispatches on AUX2 sub-type
		aux2 := 0
		if len(cell.Aux) > 2 {
			aux2 = cell.Aux[2]
		}
		switch aux2 {
		case 9:
			// LOOKOUT (SPECIALS2.TEXT P010324): set FIGHTMAP in square radius
			// AUX0 = radius; excludes center square
			radius := 0
			if len(cell.Aux) > 0 {
				radius = cell.Aux[0]
			}
			for x2 := -radius; x2 <= radius; x2++ {
				for y2 := -radius; y2 <= radius; y2++ {
					fx := ((game.PlayerX + x2) % 20 + 20) % 20
					fy := ((game.PlayerY + y2) % 20 + 20) % 20
					game.FightMap[fx][fy] = true
				}
			}
			game.FightMap[game.PlayerX][game.PlayerY] = false
		default:
			// Message display — Pascal DOMSG: AUX1 is a starting LINE number
			msgLine := cell.MsgIndex
			if len(cell.Aux) > 1 && cell.Aux[1] > 0 {
				msgLine = cell.Aux[1]
			}
			block := game.Scenario.MessageBlock(msgLine)
			if block != nil {
				if len(block) == 1 {
					game.MazeMessage = block[0]
				} else if len(block) > 1 {
					game.MazeMessages = block
					game.MazeMsgScroll = 0
					game.MazeMsgWait = len(block) > 4 // need pagination if > 4 lines
				}
			}
		}
	case data.SqFizzle:
		game.Fizzles = 1
		game.MazeMessage = "SPELLS FIZZLE HERE"
	case data.SqTransfer:
		// From p-code proc 23 (IC 3229): teleport to destination
		game.MazeMessage = "TELEPORTER"
		if cell.DestLevel > 0 {
			game.MazeLevel = cell.DestLevel - 1
			game.PlayerX = cell.DestX
			game.PlayerY = cell.DestY
			game.InitFightMap()
		}
	case data.SqOuchy:
		// From p-code proc 17 (IC 2669): "OUCH!" + damage
		game.MazeMessage = "OUCH!"
		ouchyDamage(game, cell)
	case data.SqRockwater:
		// Pascal SPECSQAR ROCKWATE: instant death — MAZELEV := -99
		game.MazeMessage = "SOLID ROCK!"
		for _, m := range game.Town.Party.Members {
			if m != nil && m.IsAlive() {
				m.HP = 0
				m.Status = engine.Dead
			}
		}
		// Return to castle (death sequence)
		game.Phase = engine.PhaseTown
		game.Town.Location = engine.Castle
		return
	case data.SqSpinner:
		// Pascal SPECSQAR SPINNER: randomize direction
		game.Facing = engine.Direction(rand.Intn(4))
	}
}

// hazardDamage applies falling damage to party members (chutes, pits).
// Pascal ROCKWATR (RUNNER2.TEXT lines 232-261):
//   Agility save: (RANDOM MOD 25) + MAZELEV > ATTRIB[AGILITY]
//   Damage: AUX0 (base) + sum(AUX2 dice of AUX1 sides)
// Uses the current cell's damage data.
func hazardDamage(game *engine.GameState) {
	cell := game.CurrentCell()
	if cell == nil {
		return
	}
	level := game.MazeLevel + 1
	for _, m := range game.Town.Party.Members {
		if m == nil || m.IsDead() {
			continue
		}
		// Agility save
		if rand.Intn(25)+level > m.Agility {
			// Roll damage from cell's AUX data
			dmg := cell.BaseDamage
			for i := 0; i < cell.NumDice; i++ {
				if cell.DieSize > 0 {
					dmg += 1 + rand.Intn(cell.DieSize)
				}
			}
			if dmg < 1 {
				dmg = 1
			}
			m.HP -= dmg
			if m.HP <= 0 {
				m.HP = 0
				m.Status = engine.Dead
			}
		}
	}
}

// ouchyDamage applies damage from ouchy squares using the cell's dice.
// From p-code proc 17 (IC 2669).
func ouchyDamage(game *engine.GameState, cell *data.MazeCell) {
	if cell.NumDice <= 0 || cell.DieSize <= 0 {
		return
	}
	for _, m := range game.Town.Party.Members {
		if m == nil || m.IsDead() {
			continue
		}
		dmg := cell.BaseDamage
		for d := 0; d < cell.NumDice; d++ {
			dmg += rand.Intn(cell.DieSize) + 1
		}
		m.HP -= dmg
		if m.HP <= 0 {
			m.HP = 0
			m.Status = engine.Dead
		}
	}
}

// handleCombatInput processes keyboard input during combat phase.
// Combat flow traced from p-code COMBAT (seg 4), CUTIL (seg 6).
func handleCombatInput(game *engine.GameState, ev *tcell.EventKey) {
	combat := game.Combat
	if combat == nil {
		game.Phase = engine.PhaseMaze
		return
	}

	switch combat.Phase {
	case engine.CombatInit:
		// Timed auto-advance — no keypress needed.
		// The ticker handles the transition after ~1.5s.
		return

	case engine.CombatFriendly:
		// From p-code CINIT proc 1: F)ight or L)eave
		if ev.Key() == tcell.KeyRune {
			ch := strings.ToUpper(string(ev.Rune()))
			if ch == "F" {
				// Fight — transition to normal combat
				combat.Friendly = false
				combat.Phase = engine.CombatChoose
				combat.CurrentActor = findNextActor(game, -1)
			} else if ch == "L" {
				// Leave in peace — exit combat
				game.Phase = engine.PhaseMaze
				game.Combat = nil
			}
		}

	case engine.CombatChoose:
		handleCombatChoose(game, ev)

	case engine.CombatConfirm:
		if ev.Key() == tcell.KeyEnter {
			// Execute the round
			combat.ExecuteRound(game)
		}
		if ev.Key() == tcell.KeyRune {
			ch := ev.Rune()
			if ch == 'b' || ch == 'B' {
				// Go back to redo options
				combat.Phase = engine.CombatChoose
				combat.CurrentActor = 0
				for i := range combat.Actions {
					combat.Actions[i] = engine.PartyAction{}
				}
			}
		}

	case engine.CombatExecute:
		// HAMAN/MAHAMAN interactive boon selection (Wiz 2/3)
		if combat.HamanSelecting {
			if ev.Key() == tcell.KeyRune {
				ch := ev.Rune()
				if ch >= '1' && ch <= '3' {
					combat.ExecuteHamanChoice(game, int(ch-'1'))
				}
			}
			return
		}
		// Original game: combat messages auto-advance on timer only (PAUSE1).
		// No keypress skips or speeds them up. Timer handler at combatTicker does the advancing.

	case engine.CombatChest:
		handleChestInput(game, combat, ev)

	case engine.CombatChestResult:
		// Keypress also advances (speed through)
		if ev.Key() == tcell.KeyEnter || ev.Key() == tcell.KeyRune {
			advanceChestMessages(game)
		}

	case engine.CombatVictory:
		// Auto-advances via ticker (PAUSE2 from Pascal source, not keypress).
		// Keypress also advances immediately.
		if ev.Key() == tcell.KeyEnter || ev.Key() == tcell.KeyRune {
			game.Combat = nil
			game.Phase = engine.PhaseMaze
			game.MazeMessage = ""
			game.MazeMessage2 = ""
		}

	case engine.CombatDefeat:
		if ev.Key() == tcell.KeyEnter || ev.Key() == tcell.KeyRune {
			// Return to town (party is dead)
			game.Combat = nil
			game.Phase = engine.PhaseTown
			game.Town.Location = engine.Castle
			game.Town.Message = "YOUR PARTY HAS PERISHED IN THE MAZE..."
		}
	}
}

// advanceCombatMessages auto-advances combat execute messages.
// Called by timer (auto-advance) and by keypress (speed through).
// From Pascal MELEE: each action gets PAUSE1 + CLRRECT. The monster/party
// display is frozen for the entire round (DSPENEMY only called in CUTIL).
func advanceCombatMessages(game *engine.GameState) {
	combat := game.Combat
	if combat == nil {
		return
	}

	if len(combat.Messages) == 0 {
		return
	}

	// Find the next action separator from the current position.
	nextStart := combat.MessageIndex
	for nextStart < len(combat.Messages) {
		if combat.Messages[nextStart] == engine.ActionSeparator {
			nextStart++
			break
		}
		nextStart++
	}

	// If there are more action blocks to show, advance to the next one
	if nextStart < len(combat.Messages) {
		combat.MessageIndex = nextStart
		return
	}

	// All messages shown — clear the frozen display snapshot
	combat.DisplayAliveCounts = nil
	combat.DisplayPartySnap = nil

	// Resolve round end
	if combat.Fled {
		game.Combat = nil
		game.Phase = engine.PhaseMaze
		game.MazeMessage = "THE PARTY FLED!"
		return
	}

	if combat.AllMonstersDead() {
		if combat.HasChest {
			// Pascal CHSTGOLD: BCHEST=true → show chest interaction (traps)
			combat.Phase = engine.CombatChest
			combat.ChestSubPhase = engine.ChestMenu
			combat.Messages = nil
			combat.MessageIndex = 0
		} else {
			// No chest — skip trap interaction, give rewards directly
			// Pascal CHSTGOLD: GETREWRD/GIVEGOLD always run regardless of BCHEST
			combat.ChestOpened = true
			combat.FinalizeChest(game)
		}
		return
	}

	allDead := true
	for _, m := range game.Town.Party.Members {
		if m != nil && m.IsAlive() {
			allDead = false
			break
		}
	}
	if allDead {
		combat.Phase = engine.CombatDefeat
		return
	}

	// Combat continues — compact groups (Pascal CUTIL), then action selection
	combat.CompactGroups()
	combat.Round++
	combat.Phase = engine.CombatChoose
	combat.CurrentActor = findNextActor(game, -1)
	combat.Messages = nil
	combat.MessageIndex = 0
}

// advanceChestMessages auto-advances chest result messages.
func advanceChestMessages(game *engine.GameState) {
	combat := game.Combat
	if combat == nil || len(combat.Messages) == 0 {
		return
	}

	// Find next action separator from current position
	nextStart := combat.MessageIndex
	for nextStart < len(combat.Messages) {
		if combat.Messages[nextStart] == engine.ActionSeparator {
			nextStart++
			break
		}
		nextStart++
	}

	// More message blocks to show
	if nextStart < len(combat.Messages) {
		combat.MessageIndex = nextStart
		return
	}

	// Wait an extra tick before transitioning — PAUSE2 from Pascal source
	// First time we reach the end, just mark it. Second tick actually transitions.
	if !combat.ChestPauseUsed {
		combat.ChestPauseUsed = true
		return
	}
	combat.ChestPauseUsed = false

	// All messages shown — transition
	if combat.ChestOpened || combat.ChestLeft {
		combat.FinalizeChest(game)
	} else {
		// Inspect/Calfo/failed disarm — return to chest menu
		combat.Phase = engine.CombatChest
		combat.ChestSubPhase = engine.ChestMenu
		combat.Messages = nil
		combat.MessageIndex = 0
	}
}

// handleChestInput handles input during the chest interaction phase.
// From p-code REWARDS proc 16 (IC 3028-3216): O)PEN, C)ALFO, L)EAVE, I)NSPECT, D)ISARM.
func handleChestInput(game *engine.GameState, combat *engine.CombatState, ev *tcell.EventKey) {
	party := game.Town.Party.Members

	// Sub-phases that need a member number (1-6)
	if combat.ChestSubPhase != engine.ChestMenu {
		if ev.Key() == tcell.KeyEscape {
			combat.ChestSubPhase = engine.ChestMenu
			return
		}
		if ev.Key() == tcell.KeyRune {
			ch := ev.Rune()
			if ch >= '1' && ch <= '6' {
				idx := int(ch-'0') - 1
				if idx < len(party) && party[idx] != nil && party[idx].IsAlive() {
					switch combat.ChestSubPhase {
					case engine.ChestWhoOpen:
						combat.OpenChest(game, idx)
					case engine.ChestWhoCalfo:
						combat.CalfoChest(game, idx)
					case engine.ChestWhoInspect:
						combat.InspectChest(game, idx)
					case engine.ChestWhoDisarm:
						combat.DisarmChest(game, idx)
					}
				}
			}
		}
		return
	}

	// Main chest menu: O/C/L/I/D
	if ev.Key() != tcell.KeyRune {
		return
	}
	ch := ev.Rune()
	if ch >= 'a' && ch <= 'z' {
		ch -= 32
	}

	switch ch {
	case 'O':
		combat.ChestSubPhase = engine.ChestWhoOpen
	case 'C':
		combat.ChestSubPhase = engine.ChestWhoCalfo
	case 'L':
		combat.LeaveChest()
	case 'I':
		combat.ChestSubPhase = engine.ChestWhoInspect
	case 'D':
		combat.ChestSubPhase = engine.ChestWhoDisarm
	}
}

// handleCombatChoose handles input during action selection for each party member.
// From p-code CUTIL segment: F)IGHT, S)PELL, P)ARRY, R)UN, U)SE, D)ISPELL
func handleCombatChoose(game *engine.GameState, ev *tcell.EventKey) {
	combat := game.Combat
	party := game.Town.Party.Members

	if combat.CurrentActor >= len(party) {
		combat.Phase = engine.CombatConfirm
		return
	}

	member := party[combat.CurrentActor]
	if member == nil || !member.IsAlive() {
		combat.CurrentActor = findNextActor(game, combat.CurrentActor)
		if combat.CurrentActor >= len(party) {
			combat.Phase = engine.CombatConfirm
		}
		return
	}

	// USE item selection — pick item by number (1-8)
	if combat.SelectingUseItem {
		if ev.Key() == tcell.KeyEnter || ev.Key() == tcell.KeyEscape {
			combat.SelectingUseItem = false
			return
		}
		idx := int(ev.Rune()) - '1'
		if idx >= 0 && idx < len(combat.UsableItems) {
			itemIdx := combat.UsableItems[idx]
			member := game.Town.Party.Members[combat.CurrentActor]
			poss := member.Items[itemIdx]
			item := &game.Scenario.Items[poss.ItemIndex]
			sp := engine.SpellTable[item.SpellPower-1]
			combat.SelectingUseItem = false

			// Auto-target: group spells → first alive group, person spells → self
			targetGroup := combat.FirstAliveGroup()
			targetAlly := combat.CurrentActor
			if sp.Target == engine.TargetMonsterGroup || sp.Target == engine.TargetAllMonsters {
				targetAlly = -1
			} else {
				targetGroup = -1
			}
			combat.Actions[combat.CurrentActor] = engine.PartyAction{
				Action:      engine.ActionUse,
				UseItemIdx:  itemIdx,
				TargetGroup: targetGroup,
				TargetAlly:  targetAlly,
			}
			advanceActor(game)
		}
		return
	}

	// Spell input mode
	if combat.InputtingSpell {
		handleSpellInput(game, ev)
		return
	}

	// Spell group selection — "CAST SPELL ON GROUP #?"
	if combat.SelectingSpellGroup {
		if ev.Key() == tcell.KeyEnter || ev.Key() == tcell.KeyEscape {
			combat.SelectingSpellGroup = false
			combat.PendingSpellName = ""
			return
		}
		if ev.Key() == tcell.KeyRune {
			digit := ev.Rune()
			if digit >= '1' && digit <= '4' {
				groupIdx := int(digit - '1')
				if groupIdx < len(combat.Groups) && combat.Groups[groupIdx].AliveCount() > 0 {
					combat.Actions[combat.CurrentActor] = engine.PartyAction{
						Action:      engine.ActionSpell,
						SpellName:   combat.PendingSpellName,
						TargetGroup: groupIdx,
					}
					combat.SelectingSpellGroup = false
					combat.PendingSpellName = ""
					advanceActor(game)
				}
			}
		}
		return
	}

	// Spell person selection — " CAST SPELL ON PERSON # ?"
	if combat.SelectingSpellTarget {
		if ev.Key() == tcell.KeyEnter || ev.Key() == tcell.KeyEscape {
			combat.SelectingSpellTarget = false
			combat.PendingSpellName = ""
			return
		}
		if ev.Key() == tcell.KeyRune {
			digit := ev.Rune()
			if digit >= '1' && digit <= '6' {
				idx := int(digit - '1')
				if idx < len(party) && party[idx] != nil {
					combat.Actions[combat.CurrentActor] = engine.PartyAction{
						Action:      engine.ActionSpell,
						SpellName:   combat.PendingSpellName,
						TargetAlly:  idx,
					}
					combat.SelectingSpellTarget = false
					combat.PendingSpellName = ""
					advanceActor(game)
				}
			}
		}
		return
	}

	// Group selection mode — from p-code CUTIL IC 55-200
	// Accept '1'-'4' for group number, RETURN to cancel
	if combat.SelectingGroup {
		if ev.Key() == tcell.KeyEnter {
			// Cancel — return to options (set -999 = unset, like p-code)
			combat.SelectingGroup = false
			return
		}
		if ev.Key() == tcell.KeyRune {
			digit := ev.Rune()
			if digit >= '1' && digit <= '4' {
				groupIdx := int(digit-'1')
				if groupIdx < len(combat.Groups) && combat.Groups[groupIdx].AliveCount() > 0 {
					combat.Actions[combat.CurrentActor] = engine.PartyAction{
						Action:      combat.GroupAction,
						TargetGroup: groupIdx,
					}
					combat.SelectingGroup = false
					advanceActor(game)
				}
				// Invalid group — ignore, wait for valid input
			}
		}
		return
	}

	if ev.Key() != tcell.KeyRune {
		return
	}

	ch := ev.Rune()
	if ch >= 'a' && ch <= 'z' {
		ch -= 32
	}

	switch ch {
	case 'F': // Fight — only front row (positions 1-3, index 0-2) can melee
		// Dead characters sort to back each round (Pascal DSPPARTY),
		// so back row naturally fills forward positions as front row dies.
		if combat.CurrentActor >= 3 {
			return // back row — can't melee
		}
		targetGroup := combat.FirstAliveGroup()
		if targetGroup < 0 {
			return
		}
		// From p-code CUTIL IC 0-200: if multiple groups alive, ask which group
		if combat.AliveGroupCount() > 1 {
			combat.SelectingGroup = true
			combat.GroupAction = engine.ActionFight
			combat.GroupPrompt = "FIGHT AGAINST GROUP# ?"
		} else {
			combat.Actions[combat.CurrentActor] = engine.PartyAction{
				Action:      engine.ActionFight,
				TargetGroup: targetGroup,
			}
			advanceActor(game)
		}

	case 'S': // Spell
		if member.IsCaster() {
			combat.InputtingSpell = true
			combat.SpellInput = ""
		}

	case 'P': // Parry
		combat.Actions[combat.CurrentActor] = engine.PartyAction{
			Action: engine.ActionParry,
		}
		advanceActor(game)

	case 'R': // Run
		combat.Actions[combat.CurrentActor] = engine.PartyAction{
			Action: engine.ActionRun,
		}
		advanceActor(game)

	case 'U': // Use item — Pascal COMBAT2.TEXT lines 51-173
		member := game.Town.Party.Members[combat.CurrentActor]
		if member == nil {
			return
		}
		// Build list of usable items: SpellPower > 0 AND equipped
		combat.UsableItems = nil
		for i := 0; i < member.ItemCount; i++ {
			poss := member.Items[i]
			if poss.ItemIndex > 0 && poss.ItemIndex < len(game.Scenario.Items) {
				item := &game.Scenario.Items[poss.ItemIndex]
				if item.SpellPower > 0 && poss.Equipped {
					combat.UsableItems = append(combat.UsableItems, i)
				}
			}
		}
		if len(combat.UsableItems) == 0 {
			// No usable items — treat as parry
			combat.Actions[combat.CurrentActor] = engine.PartyAction{
				Action: engine.ActionParry,
			}
			advanceActor(game)
		} else {
			combat.SelectingUseItem = true
		}

	case 'D': // Dispel undead
		// From game.pas lines 4387-4393: Priest always, Lord level>8, Bishop level>3
		canDispel := member.Class == engine.Priest ||
			(member.Class == engine.Lord && member.Level > 8) ||
			(member.Class == engine.Bishop && member.Level > 3)
		if canDispel {
			targetGroup := combat.FirstAliveGroup()
			if targetGroup < 0 {
				return
			}
			if combat.AliveGroupCount() > 1 {
				combat.SelectingGroup = true
				combat.GroupAction = engine.ActionDispel
				combat.GroupPrompt = "DISPELL WHICH GROUP# ?"
			} else {
				combat.Actions[combat.CurrentActor] = engine.PartyAction{
					Action:      engine.ActionDispel,
					TargetGroup: targetGroup,
				}
				advanceActor(game)
			}
		}

	}
}

// handleSpellInput handles typing a spell name during combat.
// From p-code CUTIL: "SPELL NAME ? >" prompt, hash lookup.
func handleSpellInput(game *engine.GameState, ev *tcell.EventKey) {
	combat := game.Combat

	switch ev.Key() {
	case tcell.KeyEscape:
		combat.InputtingSpell = false
		combat.SpellInput = ""
	case tcell.KeyBackspace, tcell.KeyBackspace2:
		if len(combat.SpellInput) > 0 {
			combat.SpellInput = combat.SpellInput[:len(combat.SpellInput)-1]
		}
	case tcell.KeyEnter:
		if len(combat.SpellInput) == 0 {
			combat.InputtingSpell = false
			return
		}
		sp := engine.LookupSpell(combat.SpellInput)
		if sp == nil {
			combat.Messages = []string{"** UNKNOWN SPELL **"}
			combat.MessageIndex = 0
			combat.SpellInput = ""
			return
		}

		member := game.Town.Party.Members[combat.CurrentActor]
		if !member.CanCastSpell(sp) {
			combat.Messages = []string{"** CANT CAST **"}
			combat.MessageIndex = 0
			combat.SpellInput = ""
			return
		}

		combat.InputtingSpell = false

		// Determine target based on spell type — from p-code CUTIL procs 27/28
		switch sp.Target {
		case engine.TargetSingleMonster, engine.TargetMonsterGroup:
			// Attack spell targeting a group — ask which group if multiple
			if combat.AliveGroupCount() > 1 {
				combat.SelectingSpellGroup = true
				combat.PendingSpellName = combat.SpellInput
				combat.SpellInput = ""
				return
			}
			// Only 1 group — auto-select
			combat.Actions[combat.CurrentActor] = engine.PartyAction{
				Action:      engine.ActionSpell,
				SpellName:   combat.SpellInput,
				TargetGroup: combat.FirstAliveGroup(),
			}
		case engine.TargetAllMonsters:
			combat.Actions[combat.CurrentActor] = engine.PartyAction{
				Action:    engine.ActionSpell,
				SpellName: combat.SpellInput,
			}
		case engine.TargetPartyMember:
			// Heal/buff targeting a person — ask who
			combat.SelectingSpellTarget = true
			combat.PendingSpellName = combat.SpellInput
			combat.SpellInput = ""
			return
		case engine.TargetParty, engine.TargetSelf:
			combat.Actions[combat.CurrentActor] = engine.PartyAction{
				Action:    engine.ActionSpell,
				SpellName: combat.SpellInput,
			}
		}

		combat.SpellInput = ""
		advanceActor(game)

	case tcell.KeyRune:
		ch := ev.Rune()
		if len(combat.SpellInput) < 12 {
			if ch >= 'a' && ch <= 'z' {
				ch -= 32
			}
			if ch >= 'A' && ch <= 'Z' {
				combat.SpellInput += string(ch)
			}
		}
	}
}

// findNextActor returns the index of the next alive party member after `from`.
func findNextActor(game *engine.GameState, from int) int {
	party := game.Town.Party.Members
	for i := from + 1; i < len(party); i++ {
		if party[i] != nil && party[i].IsAlive() {
			return i
		}
	}
	return len(party) // past end = all done
}

// advanceActor moves to the next party member or to confirm phase.
func advanceActor(game *engine.GameState) {
	combat := game.Combat
	combat.CurrentActor = findNextActor(game, combat.CurrentActor)
	if combat.CurrentActor >= len(game.Town.Party.Members) {
		combat.Phase = engine.CombatConfirm
	}
}

// handleUtilInput processes keyboard input during the utilities phase.
func handleUtilInput(game *engine.GameState, ev *tcell.EventKey) {
	util := game.Util
	if util == nil {
		game.Phase = engine.PhaseTitle
		return
	}

	switch util.Step {
	case engine.UtilMenu:
		if ev.Key() == tcell.KeyEscape {
			game.Util = nil
			game.Phase = engine.PhaseTitle
			if game.Title == nil {
				game.Title = &engine.TitleState{Step: engine.TitleMenu}
			} else {
				game.Title.Step = engine.TitleMenu
			}
			return
		}
		if ev.Key() != tcell.KeyRune {
			return
		}
		ch := ev.Rune()
		if ch >= 'a' && ch <= 'z' {
			ch -= 32
		}
		switch ch {
		case 'B':
			util.Step = engine.UtilBackup
		case 'C':
			util.Step = engine.UtilRename
			util.SelectedChar = -1
		case 'I':
			util.Step = engine.UtilImport
			util.InputBuf = ""
			util.Message = ""
		case 'T':
			util.TransferSources = engine.AvailableTransferScenarios(game)
			util.Step = engine.UtilTransfer
		case 'L':
			game.Util = nil
			game.Phase = engine.PhaseTitle
			if game.Title == nil {
				game.Title = &engine.TitleState{Step: engine.TitleMenu}
			} else {
				game.Title.Step = engine.TitleMenu
			}
		}

	case engine.UtilBackup:
		if ev.Key() == tcell.KeyEscape {
			util.Step = engine.UtilMenu
			return
		}
		if ev.Key() != tcell.KeyRune {
			return
		}
		ch := ev.Rune()
		if ch == 't' || ch == 'T' {
			util.Step = engine.UtilBackupTo
			util.InputBuf = ""
			util.Message = ""
		} else if ch == 'f' || ch == 'F' {
			util.Step = engine.UtilBackupFrom
			util.InputBuf = ""
			util.Message = ""
		}

	case engine.UtilBackupTo:
		handleUtilTextInput(util, ev, func(path string) {
			if err := engine.BackupRoster(game, path); err != nil {
				util.Message = fmt.Sprintf("ERROR: %s", err)
			} else {
				util.Message = "BACKUP MADE"
			}
		}, func() { util.Step = engine.UtilBackup })

	case engine.UtilBackupFrom:
		handleUtilTextInput(util, ev, func(path string) {
			if err := engine.RestoreRoster(game, path); err != nil {
				util.Message = fmt.Sprintf("ERROR: %s", err)
			} else {
				util.Message = "BACKUP RECOVERED"
			}
		}, func() { util.Step = engine.UtilBackup })

	case engine.UtilRename:
		if ev.Key() == tcell.KeyEscape {
			util.Step = engine.UtilMenu
			return
		}
		if ev.Key() == tcell.KeyRune {
			ch := ev.Rune()
			idx := int(ch-'1')
			if idx >= 0 && idx < len(game.Town.Roster.Characters) && game.Town.Roster.Characters[idx] != nil {
				util.SelectedChar = idx
				util.Step = engine.UtilRenameNew
				util.InputBuf = ""
				util.Message = ""
			}
		}

	case engine.UtilRenameNew:
		handleUtilTextInput(util, ev, func(newName string) {
			newName = strings.ToUpper(strings.TrimSpace(newName))
			if newName == "" {
				util.Message = "NAME CANNOT BE EMPTY"
				return
			}
			// Check for duplicate
			for _, c := range game.Town.Roster.Characters {
				if c != nil && c.Name == newName {
					util.Message = "NAME ALREADY IN USE"
					return
				}
			}
			game.Town.Roster.Characters[util.SelectedChar].Name = newName
			util.Message = "NAME CHANGED!"
			game.Save()
		}, func() { util.Step = engine.UtilRename })

	case engine.UtilImport:
		handleUtilTextInput(util, ev, func(path string) {
			messages, err := engine.ImportCharactersFromDSK(game, path)
			if err != nil {
				util.Message = fmt.Sprintf("ERROR: %s", err)
				return
			}
			util.Messages = messages
			util.Step = engine.UtilImportResult
			game.Save()
		}, func() { util.Step = engine.UtilMenu })

	case engine.UtilImportResult:
		// Any key returns to menu
		if ev.Key() == tcell.KeyEnter || ev.Key() == tcell.KeyRune || ev.Key() == tcell.KeyEscape {
			util.Step = engine.UtilMenu
			util.Messages = nil
		}

	case engine.UtilTransfer:
		if ev.Key() == tcell.KeyEscape {
			util.Step = engine.UtilMenu
			return
		}
		if ev.Key() == tcell.KeyRune {
			idx := int(ev.Rune()) - '1'
			if idx >= 0 && idx < len(util.TransferSources) {
				src := util.TransferSources[idx]
				msgs, err := engine.TransferCharacters(game, src)
				if err != nil {
					util.Messages = []string{err.Error()}
				} else {
					util.Messages = msgs
				}
				util.Step = engine.UtilTransferResult
				game.Save()
			}
		}

	case engine.UtilTransferResult:
		if ev.Key() == tcell.KeyEnter || ev.Key() == tcell.KeyRune || ev.Key() == tcell.KeyEscape {
			util.Step = engine.UtilMenu
			util.Messages = nil
		}
	}
}

// handleUtilTextInput handles text entry for utilities screens.
func handleUtilTextInput(util *engine.UtilState, ev *tcell.EventKey, onEnter func(string), onEsc func()) {
	switch ev.Key() {
	case tcell.KeyEscape:
		onEsc()
	case tcell.KeyBackspace, tcell.KeyBackspace2:
		if len(util.InputBuf) > 0 {
			util.InputBuf = util.InputBuf[:len(util.InputBuf)-1]
		}
	case tcell.KeyEnter:
		input := strings.TrimSpace(util.InputBuf)
		if input == "" {
			onEsc()
			return
		}
		onEnter(input)
	case tcell.KeyRune:
		if len(util.InputBuf) < 60 {
			util.InputBuf += string(ev.Rune())
		}
	}
}

func clamp(val *int, max int) {
	if *val > max {
		*val = max
	}
	if *val < 0 {
		*val = 0
	}
}

func loadScenario(name string) (*data.Scenario, error) {
	switch name {
	case "1":
		return wiz1.Load()
	case "2":
		return wiz2.Load()
	case "3":
		return wiz3.Load()
	default:
		return nil, fmt.Errorf("unknown scenario: %s (use --scenario=1, --scenario=2, or --scenario=3)", name)
	}
}
