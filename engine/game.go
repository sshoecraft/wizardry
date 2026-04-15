// Package engine implements the core Wizardry game logic.
package engine

import (
	"math/rand"

	"wizardry/data"
)

// Direction represents a cardinal direction.
type Direction int

const (
	North Direction = iota
	East
	South
	West
)

func (d Direction) String() string {
	return [...]string{"North", "East", "South", "West"}[d]
}

// TurnRight rotates 90 degrees clockwise.
func (d Direction) TurnRight() Direction { return (d + 1) % 4 }

// TurnLeft rotates 90 degrees counter-clockwise.
func (d Direction) TurnLeft() Direction { return (d + 3) % 4 }

// Reverse returns the opposite direction.
func (d Direction) Reverse() Direction { return (d + 2) % 4 }

// DX returns the x offset for moving in this direction.
func (d Direction) DX() int { return [...]int{0, 1, 0, -1}[d] }

// DY returns the y offset for moving in this direction.
// Pascal coordinates: North=Y+1, South=Y-1 (MOVEFRWD in RUNNER2.TEXT)
func (d Direction) DY() int { return [...]int{1, 0, -1, 0}[d] }

// GameState represents the current state of a Wizardry game session.
type GameState struct {
	Scenario  *data.Scenario
	Phase     Phase
	Town      *TownState
	Combat    *CombatState  // active combat encounter (nil when not in combat)
	Title     *TitleState   // title screen state (nil after title dismissed)
	Util      *UtilState    // utilities state (nil when not in utilities)
	Version   string        // display version (e.g. "1.0")
	BuildDate string        // build date in DD-MMM-YY format (e.g. "15-APR-26")

	// Dungeon position — Pascal coords: (0,0) facing North, X=East, Y=North
	MazeLevel    int // 0-based (0 = B1F)
	PlayerX      int
	PlayerY      int
	Facing       Direction
	MazeMessage  string // message area text (stairs, encounters, etc.)
	MazeMessage2 string // second line of message
	MazeMessages []string // multi-line message block (SCNMSG)
	MazeMsgScroll int     // scroll offset into MazeMessages (for "[RET] FOR MORE" pagination)
	MazeMsgWait  bool    // true = waiting for keypress to show more lines
	MazeSearchYN bool    // true = show "SEARCH (Y/N) ?" after message completes (AUX2=4 GETYN)
	SearchCell   *data.MazeCell // cell that triggered the search prompt (for combat on Y)
	SearchMonster int   // original AUX0 value (monster index) before count decrement
	ViewportMsg  string // text shown in the 3D viewport line 1 — clears on next action
	ViewportMsg2 string // viewport line 2 (Pascal rows 5-6 for VERYDARK)
	ShowMap      bool   // true = showing map overlay
	MapScrollX   int    // map pan offset in cells (arrow keys)
	MapScrollY   int
	LightLevel   int    // p-code global15: 0=dark, >0=light (MILWA/LOMILWA)
	Fizzles      int    // p-code FIZZLES: >0 = anti-magic zone, spells fail
	QuickPlot    bool   // p-code global1: QUICK PLOT mode toggle (fast dungeon redraw)
	ProtectLevel int    // p-code global16: 0=none, >0=active (MAPORFIC/BAMATU)
	ButtonCount    int  // number of buttons on current square (0 = not at buttons)
	WaitButton     bool // true = waiting for button input (A-X or Return)
	MazeInspecting   bool         // true = showing I)NSPECT search screen (SPECIALS proc 38)
	MazeInspectFound []*Character // characters found at current square during INSPECT
	MazeDelayInput   bool         // true = waiting for delay value input (T)IME)
	MazeDelayBuf     string       // numeric input buffer for delay
	MazeDelay        int          // animation delay value (1-5000, from global 7)
	ChestAlarm       int          // Pascal CHSTALRM: 1 = alarm trap triggered, forces encounter
	FightMap         [20][20]bool // Pascal FIGHTMAP: per-level fight zone grid
}

// Phase tracks which part of the game the player is in.
type Phase int

const (
	PhaseTitle Phase = iota // title screen (startup sequence)
	PhaseTown
	PhaseCamp // camp screen before/during maze exploration
	PhaseMaze
	PhaseCombat
	PhaseCreation
	PhaseUtilities // utilities menu (backup, rename, import)
)

// TitleStep tracks which part of the title sequence we're in.
type TitleStep int

const (
	TitleText   TitleStep = iota // "PREPARE YOURSELF / FOR THE ULTIMATE / IN FANTASY GAMES"
	TitleArt                     // wizard bitmap with "WIZARDRY" text
	TitleStory                   // multi-frame story (Wiz 3: 10 frames, keypress advances)
	TitleMenu                    // copyright + "S)TART GAME  U)TILITIES  T)ITLE PAGE"
)

// TitleState holds the title screen animation state.
// Flow traced from p-code SYSTEM.STARTUP segments 2 (TITLELOA) and 3 (OPTIONS):
//   TitleText → TitleArt → TitleMenu
//   Any key during TitleText or TitleArt skips straight to TitleMenu.
//   TitleMenu: S=start game, U=utilities, T=show title art again.
type TitleState struct {
	Step       TitleStep
	TextLine   int         // which text line is currently displayed (0-2)
	AnimRow    int         // for TitleArt: smoke reveals from this row up (10→0)
	Skipped    bool        // true if user pressed a key to skip
	Anim       interface{} // *render.WTAnimation (stored as interface to avoid circular import)
	Done       chan struct{} // closed when animation goroutine exits
	StoryFrame int         // current frame index in multi-frame story (Wiz 3)
}

// New creates a new game session with the given scenario data.
func New(scenario *data.Scenario) *GameState {
	// Wiz 1: full title sequence (text intro → animated art → menu)
	// Wiz 2: static title image (no text intro, no animation) → menu
	// Wiz 3: multi-frame story sequence (10 frames, keypress advances) → menu
	titleState := &TitleState{Step: TitleText, TextLine: -1, AnimRow: 10}
	if len(scenario.TitleFrames) > 0 || len(scenario.TitleStory) > 0 {
		// Multi-frame story sequence (Wiz 3)
		titleState = &TitleState{Step: TitleStory, StoryFrame: 0}
	} else if scenario.Title != nil && scenario.TitleWT == nil {
		// Has bitmap but no animation — show static art (Wiz 2)
		titleState = &TitleState{Step: TitleArt, AnimRow: 0}
	} else if scenario.Title == nil {
		// No bitmap at all — straight to menu
		titleState = &TitleState{Step: TitleMenu}
	}
	return &GameState{
		Scenario:  scenario,
		Phase:     PhaseTitle,
		Town:      NewTownState(),
		Title:     titleState,
		MazeLevel: 0,
		PlayerX:   0,
		PlayerY:   0,
		Facing:    North, // Pascal: DIRECTIO=0=NORTH
	}
}

// CurrentLevel returns the maze data for the current dungeon level.
func (g *GameState) CurrentLevel() *data.MazeLevel {
	if g.MazeLevel < 0 || g.MazeLevel >= len(g.Scenario.Mazes.Levels) {
		return nil
	}
	return &g.Scenario.Mazes.Levels[g.MazeLevel]
}

// CurrentCell returns the maze cell at the player's current position.
func (g *GameState) CurrentCell() *data.MazeCell {
	level := g.CurrentLevel()
	if level == nil {
		return nil
	}
	if g.PlayerY < 0 || g.PlayerY >= len(level.Cells) {
		return nil
	}
	if g.PlayerX < 0 || g.PlayerX >= len(level.Cells[g.PlayerY]) {
		return nil
	}
	return &level.Cells[g.PlayerY][g.PlayerX]
}

// WallAhead returns the wall type in the direction the player is facing.
func (g *GameState) WallAhead() data.WallType {
	cell := g.CurrentCell()
	if cell == nil {
		return data.WallWall
	}
	switch g.Facing {
	case North:
		return cell.N
	case South:
		return cell.S
	case East:
		return cell.E
	case West:
		return cell.W
	}
	return data.WallWall
}

// MoveForward attempts to move one step in the current facing direction.
// Returns true if the move was successful (no wall blocking).
// Blocks on solid walls AND doors — doors require K(ick) to open.
func (g *GameState) MoveForward() bool {
	wall := g.WallAhead()
	// Pascal FORWRD: only OPEN walls pass. Door, hidden door, and wall all block.
	if wall != data.WallOpen {
		return false
	}
	g.PlayerX = (g.PlayerX + g.Facing.DX() + 20) % 20
	g.PlayerY = (g.PlayerY + g.Facing.DY() + 20) % 20
	return true
}

// TurnLeft rotates the player 90 degrees counter-clockwise.
func (g *GameState) TurnLeft() {
	g.Facing = g.Facing.TurnLeft()
}

// TurnRight rotates the player 90 degrees clockwise.
func (g *GameState) TurnRight() {
	g.Facing = g.Facing.TurnRight()
}

// KickDoor attempts to kick/force through the wall ahead.
// Pascal KICK: moves through anything ≠ WALL (OPEN, Door, Hidden all pass).
func (g *GameState) KickDoor() bool {
	wall := g.WallAhead()
	return wall != data.WallWall
}

// InitFightMap initializes the FIGHTMAP for the current maze level.
// Pascal UTILITIE.TEXT FIGHTS procedure:
// 1. Clear all to FALSE
// 2. Find 9 random squares where MAZE.FIGHTS=1 and not already in FIGHTMAP
// 3. Flood-fill each through OPEN walls
// 4. Also flood-fill all ENCOUNTER-type squares
func (g *GameState) InitFightMap() {
	g.FightMap = [20][20]bool{}

	if g.MazeLevel < 0 || g.MazeLevel >= len(g.Scenario.Mazes.Levels) {
		return
	}
	level := &g.Scenario.Mazes.Levels[g.MazeLevel]
	cells := level.Cells

	// Collect all squares with FIGHTS flag set (cell.Encounter == true)
	type coord struct{ x, y int }
	fightSquares := []coord{}
	for y := 0; y < 20; y++ {
		for x := 0; x < 20; x++ {
			if y < len(cells) && x < len(cells[y]) && cells[y][x].Encounter {
				fightSquares = append(fightSquares, coord{x, y})
			}
		}
	}

	// Pick 9 random fight spots and flood-fill
	for i := 0; i < 9 && len(fightSquares) > 0; i++ {
		// FINDSPOT: find a fight square not already in FIGHTMAP
		found := false
		for attempts := 0; attempts < 100; attempts++ {
			idx := rand.Intn(len(fightSquares))
			c := fightSquares[idx]
			if !g.FightMap[c.x][c.y] {
				g.fillRoom(cells, c.x, c.y)
				found = true
				break
			}
		}
		if !found {
			break
		}
	}

	// Also flood-fill all ENCOUNTER-type squares
	for y := 0; y < 20; y++ {
		for x := 0; x < 20; x++ {
			if y < len(cells) && x < len(cells[y]) {
				if cells[y][x].Type == data.SqEncounter || cells[y][x].Type == data.SqEncounter2 {
					g.fillRoom(cells, x, y)
				}
			}
		}
	}
}

// fillRoom is a recursive flood-fill that sets FIGHTMAP entries to true.
// Pascal FILLROOM (UTILITIE.TEXT lines 522-548):
// Wraps coords mod 20. Stops if FIGHTS=0 or already marked. Recurses through OPEN walls.
func (g *GameState) fillRoom(cells [][]data.MazeCell, x, y int) {
	x = ((x % 20) + 20) % 20
	y = ((y % 20) + 20) % 20

	// Stop if no fight flag or already in FIGHTMAP
	if y >= len(cells) || x >= len(cells[y]) {
		return
	}
	if !cells[y][x].Encounter || g.FightMap[x][y] {
		return
	}

	g.FightMap[x][y] = true

	// Recurse through OPEN walls
	if cells[y][x].N == data.WallOpen {
		g.fillRoom(cells, x, y+1)
	}
	if cells[y][x].E == data.WallOpen {
		g.fillRoom(cells, x+1, y)
	}
	if cells[y][x].S == data.WallOpen {
		g.fillRoom(cells, x, y-1)
	}
	if cells[y][x].W == data.WallOpen {
		g.fillRoom(cells, x-1, y)
	}
}

// ClearFightRoom clears FIGHTMAP entries for the room at (x,y) via flood-fill.
// Pascal CLROOMFG (RUNNER2.TEXT lines 670-683): called after combat in a fight zone.
func (g *GameState) ClearFightRoom(x, y int) {
	x = ((x % 20) + 20) % 20
	y = ((y % 20) + 20) % 20

	if !g.FightMap[x][y] {
		return
	}

	g.FightMap[x][y] = false

	if g.MazeLevel < 0 || g.MazeLevel >= len(g.Scenario.Mazes.Levels) {
		return
	}
	cells := g.Scenario.Mazes.Levels[g.MazeLevel].Cells
	if y >= len(cells) || x >= len(cells[y]) {
		return
	}

	if cells[y][x].N == data.WallOpen {
		g.ClearFightRoom(x, y+1)
	}
	if cells[y][x].E == data.WallOpen {
		g.ClearFightRoom(x+1, y)
	}
	if cells[y][x].S == data.WallOpen {
		g.ClearFightRoom(x, y-1)
	}
	if cells[y][x].W == data.WallOpen {
		g.ClearFightRoom(x-1, y)
	}
}
