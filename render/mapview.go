package render

import (
	"fmt"

	"github.com/gdamore/tcell/v2"
	"wizardry/data"
	"wizardry/engine"
)

// Box-drawing intersection lookup: (up, right, down, left) → character
var boxChars = map[[4]bool]rune{
	{false, false, false, false}: ' ',
	{true, false, false, false}:  '│',
	{false, true, false, false}:  '─',
	{false, false, true, false}:  '│',
	{false, false, false, true}:  '─',
	{true, true, false, false}:   '└',
	{true, false, true, false}:   '│',
	{true, false, false, true}:   '┘',
	{false, true, true, false}:   '┌',
	{false, true, false, true}:   '─',
	{false, false, true, true}:   '┐',
	{true, true, true, false}:    '├',
	{true, true, false, true}:    '┴',
	{true, false, true, true}:    '┤',
	{false, true, true, true}:    '┬',
	{true, true, true, true}:     '┼',
}

// Square type symbols (from map_viewer.py)
var sqSymbols = map[string]string{
	"":           "   ",
	"stairs":     " S ",
	"encounter":  " ! ",
	"chute":      " C ",
	"pit":        " P ",
	"dark":       " D ",
	"transfer":   " T ",
	"ouchy":      " # ",
	"buttons":    " B ",
	"scnmsg":     " M ",
	"fizzle":     " F ",
	"spclenctr":  " X ",
	"encounter2": " ! ",
}

// Directional arrows for player position
var dirArrows = [4]rune{'↑', '→', '↓', '←'} // N, E, S, W

// mapCell returns the cell for map display position (cx, cy) where cy=0 is the
// top of the screen = highest Y (northernmost). Data uses Pascal coords (Y+=North),
// so we flip: dataY = 19-cy.
func mapCell(level *data.MazeLevel, cx, cy int) *data.MazeCell {
	dy := 19 - cy
	if cx < 0 || cx >= 20 || dy < 0 || dy >= 20 {
		return nil
	}
	return &level.Cells[dy][cx]
}

// mapWall returns a wall for map display. cy=0 is top (north), cy=19 is bottom (south).
func mapWall(level *data.MazeLevel, cx, cy, dir int) data.WallType {
	cell := mapCell(level, cx, cy)
	if cell == nil {
		return data.WallWall
	}
	switch dir {
	case 0: // N on screen = N in data (Y+ direction, toward lower cy)
		return cell.N
	case 1:
		return cell.E
	case 2:
		return cell.S
	case 3:
		return cell.W
	}
	return data.WallWall
}

// hWall returns the horizontal wall between map rows cy-1 and cy at column cx.
// cy=0 is top (north edge), cy=20 is bottom (south edge).
// Row cy-1 is NORTH of row cy. The wall between them:
//   cells at dataY=19-(cy-1)=20-cy (north side, its S wall)
//   cells at dataY=19-cy (south side, its N wall)
func hWall(level *data.MazeLevel, cx, cy int) data.WallType {
	if cy > 0 && cy <= 20 && cx >= 0 && cx < 20 {
		return mapWall(level, cx, cy-1, 2) // S wall of cell above (north side)
	}
	if cy >= 0 && cy < 20 && cx >= 0 && cx < 20 {
		return mapWall(level, cx, cy, 0) // N wall of cell below (south side)
	}
	return data.WallOpen
}

// vWall returns the vertical wall between map cols cx-1 and cx at row cy.
func vWall(level *data.MazeLevel, cx, cy int) data.WallType {
	if cx > 0 && cx <= 20 && cy >= 0 && cy < 20 {
		return mapWall(level, cx-1, cy, 1) // E wall of cell to left
	}
	if cx >= 0 && cx < 20 && cy >= 0 && cy < 20 {
		return mapWall(level, cx, cy, 3) // W wall of cell to right
	}
	return data.WallOpen
}

// RenderMap draws the maze map centered on the player, filling 80x24.
func (s *Screen) RenderMap(game *engine.GameState) {
	s.Clear()
	s.ClearSixelTransition()
	savedScale := s.scale
	s.scale = 1 // map renders at 1:1

	level := game.CurrentLevel()
	if level == nil {
		s.DrawString(0, 0, styleNormal, "No maze data")
		s.scale = savedScale
		s.Show()
		return
	}

	white := base
	dim := base
	bold := base.Bold(true)
	playerStyle := tcell.StyleDefault.Foreground(tcell.ColorBlack).Background(tcell.ColorWhite)
	doorStyle := base

	// Map dimensions: 81 wide × 41 tall (20 cells × (3+1) + 1)
	mapW := 81
	mapH := 41
	viewW := 80
	viewH := 22 // leave 2 rows for status

	// Map display: X=East (left to right), Y=North (bottom to top).
	// Screen row 0 = top = highest Y = northernmost. Flip: screenY = 19 - PlayerY.
	mapPX := game.PlayerX
	mapPY := 19 - game.PlayerY

	// Scroll to center on player + pan offset from arrow keys
	centerX := (mapPX + game.MapScrollX) * 4 + 2
	centerY := (mapPY + game.MapScrollY) * 2 + 1

	scrollX := centerX - viewW/2
	scrollY := centerY - viewH/2
	if scrollX < 0 {
		scrollX = 0
	}
	if scrollY < 0 {
		scrollY = 0
	}
	if scrollX > mapW-viewW {
		scrollX = mapW - viewW
	}
	if scrollY > mapH-viewH {
		scrollY = mapH - viewH
	}
	if scrollX < 0 {
		scrollX = 0
	}
	if scrollY < 0 {
		scrollY = 0
	}

	// Build teleport pair map — assign each teleport a label and mark destinations
	type teleportInfo struct {
		label rune         // '1'-'9', 'a'-'z'
		color tcell.Style
	}
	type teleportDstInfo struct {
		count int
		color tcell.Style
	}
	teleportSrc := map[[2]int]teleportInfo{}    // [y][x] → info for source cells
	teleportDst := map[[2]int]teleportDstInfo{} // [y][x] → info for destination cells
	teleColors := []tcell.Style{
		base.Foreground(tcell.ColorRed),
		base.Foreground(tcell.ColorGreen),
		base.Foreground(tcell.ColorYellow),
		base.Foreground(tcell.ColorBlue),
		base.Foreground(tcell.ColorDarkCyan),
		base.Foreground(tcell.ColorPurple),
		base.Foreground(tcell.ColorOrange),
		base.Foreground(tcell.ColorWhite),
	}
	teleIdx := 0
	for cy := 0; cy < 20; cy++ {
		for cx := 0; cx < 20; cx++ {
			cell := mapCell(level, cx, cy)
			if cell == nil {
				continue
			}
			if cell.Type == data.SqTransfer || cell.Type == data.SqChute {
				label := rune('1' + teleIdx)
				if teleIdx >= 9 {
					label = rune('a' + teleIdx - 9)
				}
				color := teleColors[teleIdx%len(teleColors)]
				teleportSrc[[2]int{cy, cx}] = teleportInfo{label, color}
				// Mark destination if same level — convert dest to map coords
				destLevel := cell.DestLevel - 1 // DestLevel is 1-based
				if destLevel == level.Level-1 {
					mapDestX := cell.DestX
					mapDestY := 19 - cell.DestY // flip Y for map display
					destKey := [2]int{mapDestY, mapDestX}
					if _, isSrc := teleportSrc[destKey]; !isSrc {
						di := teleportDst[destKey]
						di.count++
						di.color = color
						teleportDst[destKey] = di
					}
				}
				teleIdx++
			}
		}
	}

	// Draw the map — apply both scrollX and scrollY
	for cy := 0; cy <= 20; cy++ {
		// Intersection + horizontal wall row
		mapRow := cy * 2
		screenRow := mapRow - scrollY
		if screenRow >= 0 && screenRow < viewH {
			col := 0
			for cx := 0; cx <= 20; cx++ {
				up := cy > 0 && vWall(level, cx, cy-1) != data.WallOpen
				right := cx < 20 && hWall(level, cx, cy) != data.WallOpen
				down := cy < 20 && vWall(level, cx, cy) != data.WallOpen
				left := cx > 0 && hWall(level, cx-1, cy) != data.WallOpen

				sc := col - scrollX
				if sc >= 0 && sc < viewW {
					ch := boxChars[[4]bool{up, right, down, left}]
					s.tcell.SetContent(sc, screenRow, ch, nil, white)
				}
				col++

				if cx < 20 {
					wt := hWall(level, cx, cy)
					// Detect one-way: passability differs between sides
					// passable = open, door, hidden; blocked = wall
					var seg [3]rune
					var st tcell.Style
					oneWayH := false
					if cy > 0 && cy < 20 {
						abovePass := mapWall(level, cx, cy-1, 2) != data.WallWall // S wall of north cell
						belowPass := mapWall(level, cx, cy, 0) != data.WallWall   // N wall of south cell
						if abovePass != belowPass {
							oneWayH = true
							if abovePass {
								seg = [3]rune{'─', '▼', '─'} // can pass from above going south
							} else {
								seg = [3]rune{'─', '▲', '─'} // can pass from below going north
							}
							st = base.Foreground(tcell.ColorYellow).Bold(true)
						}
					}
					if !oneWayH {
						switch wt {
						case data.WallWall:
							seg = [3]rune{'─', '─', '─'}
							st = white
						case data.WallDoor:
							seg = [3]rune{'╌', '╌', '╌'}
							st = doorStyle
						case data.WallHidden:
							seg = [3]rune{'┄', '┄', '┄'}
							st = dim
						default:
							seg = [3]rune{' ', ' ', ' '}
							st = white
						}
					}
					for i := 0; i < 3; i++ {
						sc := col + i - scrollX
						if sc >= 0 && sc < viewW {
							s.tcell.SetContent(sc, screenRow, seg[i], nil, st)
						}
					}
					col += 3
				}
			}
		}

		// Cell interior row
		interiorRow := mapRow + 1
		screenRow = interiorRow - scrollY
		if cy < 20 && screenRow >= 0 && screenRow < viewH {
			col := 0
			for cx := 0; cx <= 20; cx++ {
				// Vertical wall
				wt := vWall(level, cx, cy)
				// Detect one-way: passability differs between sides
				sc := col - scrollX
				if sc >= 0 && sc < viewW {
					var ch rune
					var st tcell.Style
					oneWayV := false
					if cx > 0 && cx < 20 && cy < 20 {
						leftPass := mapWall(level, cx-1, cy, 1) != data.WallWall // E wall of left cell
						rightPass := mapWall(level, cx, cy, 3) != data.WallWall  // W wall of right cell
						if leftPass != rightPass {
							oneWayV = true
							if leftPass {
								ch = '►' // can pass from left going east
							} else {
								ch = '◄' // can pass from right going west
							}
							st = base.Foreground(tcell.ColorYellow).Bold(true)
						}
					}
					if !oneWayV {
						switch wt {
						case data.WallWall:
							ch = '│'
							st = white
						case data.WallDoor:
							ch = '╎'
							st = doorStyle
						case data.WallHidden:
							ch = '┆'
							st = dim
						default:
							ch = ' '
							st = white
						}
					}
					s.tcell.SetContent(sc, screenRow, ch, nil, st)
				}
				col++

				// Cell interior (3 chars)
				if cx < 20 {
					isPlayer := cx == mapPX && cy == mapPY
					cell := mapCell(level, cx, cy)

					var interior string
					var st tcell.Style
					if isPlayer {
						arrow := dirArrows[game.Facing]
						interior = " " + string(arrow) + " "
						st = playerStyle
					} else if cell == nil {
						interior = "   "
						st = white
					} else if ti, ok := teleportSrc[[2]int{cy, cx}]; ok {
						// Teleport source: show type + label
						prefix := "T"
						if cell.Type == data.SqChute {
							prefix = "C"
						}
						interior = prefix + string(ti.label) + " "
						st = ti.color.Bold(true)
					} else if di, ok := teleportDst[[2]int{cy, cx}]; ok {
						// Teleport destination — show count of sources pointing here
						countCh := rune('0' + di.count)
						if di.count > 9 {
							countCh = '+'
						}
						interior = ">" + string(countCh) + " "
						st = di.color
					} else if cell.Type == data.SqStairs {
						if cell.DestLevel < level.Level {
							interior = "UP "
						} else {
							interior = "DN "
						}
						st = bold
					} else if cell.Type != "" {
						sym, ok := sqSymbols[string(cell.Type)]
						if !ok {
							sym = " ? "
						}
						interior = sym
						st = bold
					} else if cell != nil && cell.Encounter {
						interior = " . "
						st = dim
					} else {
						interior = "   "
						st = white
					}

					for i, ch := range interior {
						sc := col + i - scrollX
						if sc >= 0 && sc < viewW {
							s.tcell.SetContent(sc, screenRow, ch, nil, st)
						}
					}
					col += 3
				}
			}
		}
	}

	// Status bar at bottom
	statusY := viewH
	cyan := base
	s.tcell.SetContent(0, statusY, ' ', nil, white)
	statusStr := fmt.Sprintf(" B%dF  (%d,%d) Facing %s",
		level.Level, game.PlayerX, game.PlayerY, game.Facing)
	for i, ch := range statusStr {
		s.tcell.SetContent(i, statusY, ch, nil, cyan)
	}

	helpStr := " PRESS ANY KEY TO RETURN"
	for i, ch := range helpStr {
		s.tcell.SetContent(i, statusY+1, ch, nil, dim)
	}

	s.scale = savedScale
	s.Show()
}
