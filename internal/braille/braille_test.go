package braille

import (
	"strings"
	"testing"
)

func TestDimensions(t *testing.T) {
	c := New(10, 8)
	if c.Cols() != 5 || c.Rows() != 2 {
		t.Errorf("New(10,8) → %dx%d cells, want 5x2", c.Cols(), c.Rows())
	}
	// rounds up: 3 dots wide → 2 cols, 5 dots tall → 2 rows
	if c2 := New(3, 5); c2.Cols() != 2 || c2.Rows() != 2 {
		t.Errorf("New(3,5) → %dx%d, want 2x2", c2.Cols(), c2.Rows())
	}
}

func TestSetBitsAndRune(t *testing.T) {
	c := New(2, 4) // one cell
	c.Set(0, 0, 255, 0, 0)
	c.Set(1, 3, 255, 0, 0) // top-left dot (0x01) + bottom-right dot (0x80) → 0x81
	out := c.String()
	if !strings.ContainsRune(out, rune(0x2800+0x81)) {
		t.Errorf("expected braille rune for bits 0x81, got %q", out)
	}
	if !strings.Contains(out, "\x1b[38;2;255;0;0m") {
		t.Error("expected a truecolor escape for the set color")
	}
}

func TestSetOutOfRangeIgnored(t *testing.T) {
	c := New(2, 4)
	c.Set(-1, 0, 1, 1, 1)
	c.Set(0, 99, 1, 1, 1)
	c.Set(5, 0, 1, 1, 1)
	if strings.TrimSpace(c.String()) != "" {
		t.Errorf("out-of-range sets should leave the canvas blank, got %q", c.String())
	}
}

// TestBackground: a set background colors every cell (lit and empty) so lines
// stay visible against it.
func TestBackground(t *testing.T) {
	c := New(4, 4)
	c.Background(198, 190, 170)
	c.Set(0, 0, 0, 0, 0) // a dark line dot
	out := c.String()
	if !strings.Contains(out, "\x1b[48;2;198;190;170m") {
		t.Error("background color escape should be emitted")
	}
	// empty cells carry the background too (so the whole map area is filled)
	if strings.Count(out, "\x1b[48;2;198;190;170m") < 2 {
		t.Error("empty cells should also get the background")
	}
}

func TestLine(t *testing.T) {
	c := New(8, 4)
	c.Line(0, 0, 7, 0, 0, 255, 0) // a horizontal line across the top
	// every cell in the top dot-row should be lit (4 cells wide)
	lit := strings.Count(c.String(), "\x1b[38;2;0;255;0m")
	if lit != c.Cols() {
		t.Errorf("a full-width line should light all %d cells, lit %d", c.Cols(), lit)
	}
}
