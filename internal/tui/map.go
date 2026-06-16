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

	px, py, pz, haveLoc := 0.0, 0.0, 0.0, false
	if m.tracker != nil {
		px, py, pz, haveLoc = m.tracker.Loc()
	}
	floorState := "off"
	if m.mapFloors {
		floorState = "on"
	}
	loc := "no /loc yet — type /loc in game"
	if haveLoc {
		loc = fmt.Sprintf("/loc %.0f, %.0f, %.0f · %.0f× scroll zoom · f floors:%s", px, py, pz, math.Max(m.mapZoom, 1), floorState)
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
	return header + "\n" + renderMapCanvas(m.curMap, px, py, pz, haveLoc, m.mapZoom, m.mapFloors, w, bodyH)
}

// zoomIn / zoomOut step the map zoom (clamped to [1, 32]), shared by the +/-
// keys and the mouse wheel.
func (m *Model) zoomIn()  { m.mapZoom = math.Min(math.Max(m.mapZoom, 1)*1.5, 32) }
func (m *Model) zoomOut() { m.mapZoom = math.Max(math.Max(m.mapZoom, 1)/1.5, 1) }

// updateMap handles Map-tab keys: +/- zoom (the wheel zooms too, in Update), f
// toggles floor-scoping (off by default — only useful in stacked zones).
func (m Model) updateMap(msg tea.Msg) (tea.Model, tea.Cmd) {
	if k, ok := msg.(tea.KeyMsg); ok {
		switch k.String() {
		case "+", "=":
			m.zoomIn()
		case "-", "_":
			m.zoomOut()
		case "f":
			m.mapFloors = !m.mapFloors
		}
	}
	return m, nil
}

// renderMapCanvas draws a zone map auto-fit to a w×h character area and marks the
// player. The orientation is the one LOCKED + verified in-game in the eqscripts
// web overlay: map segments are drawn in their native file coords (centered, no
// per-axis flip, screen-Y growing down), and the player's /loc position is
// negated — orient(x,y) = (-x,-y) — because the map files are stored negated
// relative to position coords. That negate is the dot↔map fit; without it the
// player doesn't line up.
//
// Floors are scoped by Z: only segments on the reference floor are drawn — other
// levels of a multi-level zone (HighKeep) are hidden entirely, not overlaid. The
// reference height is the player's /loc Z, or the zone's lowest floor when
// there's no /loc.
func renderMapCanvas(mp *eqmap.Map, px, py, pz float64, havePlayer bool, zoom float64, floors bool, wCells, hCells int) string {
	dotW, dotH := max(wCells, 1)*2, max(hCells, 1)*4
	cv := braille.New(dotW, dotH)

	worldW := math.Max(mp.MaxX-mp.MinX, 1)
	worldH := math.Max(mp.MaxY-mp.MinY, 1)
	fit := math.Min(float64(dotW-1)/worldW, float64(dotH-1)/worldH) // whole-zone fit, keep aspect
	// center on the player and zoom in when we have a /loc; otherwise show the
	// whole zone. (Map coords are negated vs /loc, so the player's map point is
	// (-px,-py).) Off-canvas segments are simply clipped by Set/Line.
	centerX, centerY, s := (mp.MinX+mp.MaxX)/2, (mp.MinY+mp.MaxY)/2, fit
	if havePlayer {
		centerX, centerY, s = -px, -py, fit*math.Max(zoom, 1)
	}
	cxDot, cyDot := float64(dotW-1)/2, float64(dotH-1)/2
	project := func(x, y float64) (int, int) {
		return int(math.Round(cxDot + (x-centerX)*s)), int(math.Round(cyDot + (y-centerY)*s))
	}

	refZ := mp.MinZ // no /loc → the bottom floor
	if havePlayer {
		refZ = pz
	}
	for _, seg := range mp.Segments {
		// floor-scope only when asked AND we know the player's Z — outdoor zones
		// span a huge Z range, so scoping there would wrongly hide most of the map.
		if floors && havePlayer && !onFloor(seg.Z, refZ) {
			continue // a different floor — not drawn
		}
		x0, y0 := project(seg.X1, seg.Y1)
		x1, y1 := project(seg.X2, seg.Y2)
		cv.Line(x0, y0, x1, y1, seg.R, seg.G, seg.B)
	}
	if havePlayer {
		// mark the player with an emoji pin (double-width) instead of a colored dot.
		dx, dy := project(-px, -py) // orient(): map coords are negated vs /loc
		col, row := dx/2, dy/4      // dot → cell
		if col >= 0 && row >= 0 && col < cv.Cols() && row < cv.Rows() {
			if col > cv.Cols()-2 {
				col = cv.Cols() - 2 // keep room for the 2-wide glyph at the edge
			}
			return cv.StringWithMarker(col, row, playerMarker)
		}
	}
	return cv.String()
}

// playerMarker is the "you are here" glyph drawn on the map (double-width emoji).
const playerMarker = "📍"

// zFloorBand is the Z half-window for "this floor". Floors in multi-level zones
// sit ~50 apart (HighKeep), so this captures the current floor while excluding
// the next one. A segment outside the band is on another floor and isn't drawn.
const zFloorBand = 30.0

// onFloor reports whether a segment at height z belongs to the reference floor.
func onFloor(z, ref float64) bool { return math.Abs(z-ref) <= zFloorBand }
