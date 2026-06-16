package tui

import (
	"fmt"
	"math"

	tea "github.com/charmbracelet/bubbletea"

	"99dps/internal/braille"
	"99dps/internal/eqmap"
)

// mapLoadedMsg delivers a fetched zone map (or the failure) back to Update.
type mapLoadedMsg struct {
	short  string
	m      *eqmap.Map
	status string // non-empty = a message instead of a map (loading failed / no map)
}

// maybeLoadMapCmd issues a fetch when the current zone's map isn't the one we
// already have (or are already fetching). Returns nil when nothing's needed.
func (m *Model) maybeLoadMapCmd() tea.Cmd {
	if m.tracker == nil || m.mapLoader == nil {
		return nil
	}
	short := eqmap.ShortName(m.tracker.Zone())
	if short == "" {
		m.mapStatus = "zone unknown — zone in to load a map"
		return nil
	}
	if short == m.mapShort { // already loaded or in-flight
		return nil
	}
	m.mapShort = short
	m.curMap = nil
	m.mapStatus = "loading " + short + "…"
	loader := m.mapLoader
	return func() tea.Msg {
		mp, err := loader.Get(short)
		if err != nil {
			return mapLoadedMsg{short: short, status: "no map for " + short}
		}
		return mapLoadedMsg{short: short, m: mp}
	}
}

// mapView renders the Map tab: a header (zone · /loc) over a braille render of
// the zone, with the player marked from the last /loc.
func (m Model) mapView(th theme, w, h int) string {
	zone := ""
	if m.tracker != nil {
		zone = m.tracker.Zone()
	}
	if zone == "" {
		zone = "—"
	}

	px, py, haveLoc := 0.0, 0.0, false
	if m.tracker != nil {
		px, py, haveLoc = m.tracker.Loc()
	}
	loc := "no /loc yet — type /loc in game"
	if haveLoc {
		loc = fmt.Sprintf("/loc %.0f, %.0f", px, py)
	}
	header := paint(th, th.accent, truncate("⊞ "+zone+"   ", w), w) +
		"\n" + paint(th, th.dim, truncate(loc, w), w)

	bodyH := max(h-2, 1) // header is two lines
	if m.curMap == nil || m.curMap.Empty() {
		msg := m.mapStatus
		if msg == "" {
			msg = "press the Map tab while in a zone to load it"
		}
		return header + "\n" + paint(th, th.dim, truncate(msg, w), w)
	}
	return header + "\n" + renderMapCanvas(m.curMap, px, py, haveLoc, w, bodyH)
}

// renderMapCanvas draws a zone map auto-fit to a w×h character area, flipping to
// EQ orientation (north up, west left) and marking the player. (The flip may
// need an in-game tweak once verified against a live /loc.)
func renderMapCanvas(mp *eqmap.Map, px, py float64, havePlayer bool, wCells, hCells int) string {
	dotW, dotH := max(wCells, 1)*2, max(hCells, 1)*4
	cv := braille.New(dotW, dotH)

	worldW := math.Max(mp.MaxX-mp.MinX, 1)
	worldH := math.Max(mp.MaxY-mp.MinY, 1)
	s := math.Min(float64(dotW-1)/worldW, float64(dotH-1)/worldH) // fit, keep aspect
	offX := (float64(dotW-1) - worldW*s) / 2
	offY := (float64(dotH-1) - worldH*s) / 2
	toDot := func(wx, wy float64) (int, int) {
		return int(math.Round(offX + (mp.MaxX-wx)*s)), // flip X → west on the left
			int(math.Round(offY + (mp.MaxY-wy)*s)) // flip Y → north up
	}

	for _, seg := range mp.Segments {
		x0, y0 := toDot(seg.X1, seg.Y1)
		x1, y1 := toDot(seg.X2, seg.Y2)
		cv.Line(x0, y0, x1, y1, seg.R, seg.G, seg.B)
	}
	if havePlayer {
		dx, dy := toDot(px, py)
		for _, d := range [][2]int{{0, 0}, {1, 0}, {-1, 0}, {0, 1}, {0, -1}} { // a bright cross
			cv.Set(dx+d[0], dy+d[1], 255, 255, 0)
		}
	}
	return cv.String()
}
