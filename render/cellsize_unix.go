//go:build !windows

package render

import (
	"os"

	"golang.org/x/sys/unix"
)

// DetectCellSize uses ioctl TIOCGWINSZ to get terminal pixel and character
// dimensions, then computes cell size. Safe to call at any time — no escape
// sequences, no stdin reads, no goroutine leaks.
func DetectCellSize() (int, int) {
	fd := int(os.Stdout.Fd())
	ws, err := unix.IoctlGetWinsize(fd, unix.TIOCGWINSZ)
	if err != nil || ws.Col == 0 || ws.Row == 0 || ws.Xpixel == 0 || ws.Ypixel == 0 {
		return CellWidth, CellHeight
	}
	cw := int(ws.Xpixel) / int(ws.Col)
	ch := int(ws.Ypixel) / int(ws.Row)
	if cw < 4 || ch < 8 {
		return CellWidth, CellHeight
	}
	return cw, ch
}
