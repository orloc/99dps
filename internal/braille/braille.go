// Package braille is a tiny pure-Go drawing surface that renders to Unicode
// braille characters — each cell packs a 2×4 grid of dots, giving ~8× the
// resolution of plain text cells. Output is 24-bit-color ANSI, so it drops
// straight into the truecolor TUI. Used for the zone map. No dependencies.
package braille

import (
	"fmt"
	"strings"
)

// pixmap[y%4][x%2] is the braille dot bit for that sub-cell position (the
// standard Unicode braille dot numbering).
var pixmap = [4][2]byte{
	{0x01, 0x08},
	{0x02, 0x10},
	{0x04, 0x20},
	{0x40, 0x80},
}

type cell struct {
	bits    byte
	r, g, b uint8
}

// Canvas is a braille drawing surface measured in DOTS (2 per char wide, 4 per
// char tall). (0,0) is top-left.
type Canvas struct {
	dotW, dotH int
	cols, rows int
	cells      []cell
}

// New makes a canvas sized in dots; its character size rounds up (Cols/Rows).
func New(dotW, dotH int) *Canvas {
	dotW, dotH = max(dotW, 1), max(dotH, 1)
	cols, rows := (dotW+1)/2, (dotH+3)/4
	return &Canvas{dotW: dotW, dotH: dotH, cols: cols, rows: rows, cells: make([]cell, cols*rows)}
}

// Cols and Rows are the canvas size in character cells.
func (c *Canvas) Cols() int { return c.cols }
func (c *Canvas) Rows() int { return c.rows }

// DotW and DotH are the canvas size in dots.
func (c *Canvas) DotW() int { return c.dotW }
func (c *Canvas) DotH() int { return c.dotH }

// Set lights the dot at (x,y) with a color. Out-of-range coords are ignored.
func (c *Canvas) Set(x, y int, r, g, b uint8) {
	if x < 0 || y < 0 || x >= c.dotW || y >= c.dotH {
		return
	}
	cl := &c.cells[(y/4)*c.cols+(x/2)]
	cl.bits |= pixmap[y&3][x&1]
	cl.r, cl.g, cl.b = r, g, b // last writer colors the cell (fine for map lines)
}

// Line draws a line between two dot coords (Bresenham).
func (c *Canvas) Line(x0, y0, x1, y1 int, r, g, b uint8) {
	dx, dy := abs(x1-x0), -abs(y1-y0)
	sx, sy := 1, 1
	if x0 > x1 {
		sx = -1
	}
	if y0 > y1 {
		sy = -1
	}
	err := dx + dy
	for {
		c.Set(x0, y0, r, g, b)
		if x0 == x1 && y0 == y1 {
			return
		}
		e2 := 2 * err
		if e2 >= dy {
			err += dy
			x0 += sx
		}
		if e2 <= dx {
			err += dx
			y0 += sy
		}
	}
}

// String renders the canvas to 24-bit-color ANSI, one text line per cell row.
// Empty cells render as a space.
func (c *Canvas) String() string { return c.render(-1, -1, "") }

// StringWithMarker renders like String but draws marker (a double-width glyph,
// e.g. an emoji) at cell (markCol, markRow), consuming that cell and the next so
// the line keeps its width. Out-of-range coords render no marker.
func (c *Canvas) StringWithMarker(markCol, markRow int, marker string) string {
	return c.render(markCol, markRow, marker)
}

func (c *Canvas) render(markCol, markRow int, marker string) string {
	var sb strings.Builder
	for row := 0; row < c.rows; row++ {
		for col := 0; col < c.cols; col++ {
			if marker != "" && row == markRow && col == markCol {
				sb.WriteString(marker)
				col++ // the marker is double-width — also consume the next cell's column
				continue
			}
			cl := c.cells[row*c.cols+col]
			if cl.bits == 0 {
				sb.WriteByte(' ')
				continue
			}
			fmt.Fprintf(&sb, "\x1b[38;2;%d;%d;%dm%c\x1b[0m", cl.r, cl.g, cl.b, rune(0x2800+int(cl.bits)))
		}
		if row < c.rows-1 {
			sb.WriteByte('\n')
		}
	}
	return sb.String()
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
