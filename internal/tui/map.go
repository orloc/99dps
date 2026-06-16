package tui

import (
	"fmt"
	"math"
	"sort"

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
	loc := "no /loc yet — type /loc in game"
	if haveLoc {
		loc = fmt.Sprintf("/loc %.0f, %.0f, %.0f", px, py, pz)
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
	return header + "\n" + renderMapCanvas(m.curMap, px, py, pz, haveLoc, w, bodyH)
}

// renderMapCanvas draws a zone map auto-fit to a w×h character area and marks the
// player. The orientation is the one LOCKED + verified in-game in the eqscripts
// web overlay: map segments are drawn in their native file coords (centered, no
// per-axis flip, screen-Y growing down), and the player's /loc position is
// negated — orient(x,y) = (-x,-y) — because the map files are stored negated
// relative to position coords. That negate is the dot↔map fit; without it the
// player doesn't line up.
// Floors are scoped by Z: the reference height is the player's /loc Z (or the
// zone's lowest floor when there's no /loc). Segments far from it in Z fade out,
// and the reference floor draws last so it sits on top — so a multi-level zone
// (HighKeep) shows your floor, not every floor overlaid.
func renderMapCanvas(mp *eqmap.Map, px, py, pz float64, havePlayer bool, wCells, hCells int) string {
	dotW, dotH := max(wCells, 1)*2, max(hCells, 1)*4
	cv := braille.New(dotW, dotH)

	midX, midY := (mp.MinX+mp.MaxX)/2, (mp.MinY+mp.MaxY)/2
	worldW := math.Max(mp.MaxX-mp.MinX, 1)
	worldH := math.Max(mp.MaxY-mp.MinY, 1)
	s := math.Min(float64(dotW-1)/worldW, float64(dotH-1)/worldH) // fit, keep aspect
	cxDot, cyDot := float64(dotW-1)/2, float64(dotH-1)/2
	project := func(x, y float64) (int, int) {
		return int(math.Round(cxDot + (x-midX)*s)), int(math.Round(cyDot + (y-midY)*s))
	}

	refZ := mp.MinZ // no /loc → the bottom floor
	if havePlayer {
		refZ = pz
	}
	// draw farthest-in-Z first so the reference floor lands on top of each cell.
	order := make([]int, len(mp.Segments))
	for i := range order {
		order[i] = i
	}
	sort.Slice(order, func(a, b int) bool {
		return math.Abs(mp.Segments[order[a]].Z-refZ) > math.Abs(mp.Segments[order[b]].Z-refZ)
	})
	for _, i := range order {
		seg := mp.Segments[i]
		a := zAlpha(seg.Z, refZ)
		if a <= 0 {
			continue // a different floor — hidden
		}
		x0, y0 := project(seg.X1, seg.Y1)
		x1, y1 := project(seg.X2, seg.Y2)
		cv.Line(x0, y0, x1, y1, dim(seg.R, a), dim(seg.G, a), dim(seg.B, a))
	}
	if havePlayer {
		dx, dy := project(-px, -py)                                            // orient(): map coords are negated vs /loc
		for _, d := range [][2]int{{0, 0}, {1, 0}, {-1, 0}, {0, 1}, {0, -1}} { // a bright cross
			cv.Set(dx+d[0], dy+d[1], 255, 255, 0)
		}
	}
	return cv.String()
}

// floor-scoping band: a segment within zSameFloor of the reference height is full
// brightness; it fades to nothing by zHideFloor, so floors of a multi-level zone
// (HighKeep: ~50 apart) dim out instead of overlapping.
const (
	zSameFloor = 25.0
	zHideFloor = 60.0
)

// zAlpha is a segment's brightness for floor scoping: 1 on the reference floor,
// fading to 0 beyond zHideFloor.
func zAlpha(z, ref float64) float64 {
	dz := math.Abs(z - ref)
	switch {
	case dz <= zSameFloor:
		return 1
	case dz >= zHideFloor:
		return 0
	default:
		return (zHideFloor - dz) / (zHideFloor - zSameFloor)
	}
}

// dim scales a color channel by alpha (0..1) for faded off-floor segments.
func dim(c uint8, a float64) uint8 { return uint8(float64(c) * a) }
