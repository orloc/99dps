package tui

import (
	"fmt"
	"math"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"99dps/internal/braille"
)

// mapLoadedMsg delivers a fetched zone map (or the failure) back to Update.
type mapLoadedMsg struct {
	short  string
	m      *MapData
	status string // non-empty = a message instead of a map (loading failed / no map)
}

// maybeLoadMapCmd issues a fetch when the current zone's map isn't the one we
// already have (or are already fetching). Returns nil when nothing's needed.
func (m *Model) maybeLoadMapCmd() tea.Cmd {
	if m.tracker == nil || m.mapSrc == nil {
		return nil
	}
	short := m.mapSrc.ShortName(m.tracker.Zone())
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
	src := m.mapSrc
	return func() tea.Msg {
		mp, err := src.Load(short)
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
	if m.curMap.empty() {
		msg := m.mapStatus
		if msg == "" {
			msg = "press the Map tab while in a zone to load it"
		}
		return header + "\n" + paint(th, th.dim, truncate(msg, w), w)
	}

	// a POI side panel (selectable with ↑/↓, dropping a pin on the map) when we
	// know where the player is, there are POIs, and the panel fits; otherwise the
	// map takes the full width.
	es := orderedPOIs(m.curMap.Points, px, py)
	panelW := 0
	if haveLoc && len(es) > 0 && w >= mapPanelMinW {
		panelW = min(maxMapPanelW, w/3)
	}
	mapW := w
	var sel *poiEntry
	if panelW > 0 {
		mapW = w - panelW - 1 // a one-column gap
		s := clampInt(m.mapSel, 0, len(es)-1)
		sel = &es[s]
	}
	canvas := renderMapCanvas(m.curMap, px, py, pz, haveLoc, m.mapZoom, m.mapFloors, mapW, bodyH, sel)
	if panelW == 0 {
		return header + "\n" + canvas
	}
	panel := poiPanel(th, es, clampInt(m.mapSel, 0, len(es)-1), panelW, bodyH)
	return header + "\n" + lipgloss.JoinHorizontal(lipgloss.Top, canvas, " ", panel)
}

// map side-panel sizing: only shown when the window is at least mapPanelMinW
// wide, capped at maxMapPanelW so the map keeps most of the room.
const (
	mapPanelMinW = 54
	maxMapPanelW = 30
)

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// zoomIn / zoomOut step the map zoom (clamped to [1, 32]), shared by the +/-
// keys and the mouse wheel.
func (m *Model) zoomIn()  { m.mapZoom = math.Min(math.Max(m.mapZoom, 1)*1.5, 32) }
func (m *Model) zoomOut() { m.mapZoom = math.Max(math.Max(m.mapZoom, 1)/1.5, 1) }

// updateMap handles Map-tab keys: +/- zoom (the wheel zooms too, in Update), f
// toggles floor-scoping, ↑/↓ (or j/k) move the POI selection (which drops a pin).
func (m Model) updateMap(msg tea.Msg) (tea.Model, tea.Cmd) {
	if k, ok := msg.(tea.KeyMsg); ok {
		switch k.String() {
		case "+", "=":
			m.zoomIn()
		case "-", "_":
			m.zoomOut()
		case "f":
			m.mapFloors = !m.mapFloors
		case "up", "k":
			if m.mapSel > 0 {
				m.mapSel--
			}
		case "down", "j":
			if m.curMap != nil && m.mapSel < len(m.curMap.Points)-1 {
				m.mapSel++
			}
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
func renderMapCanvas(mp *MapData, px, py, pz float64, havePlayer bool, zoom float64, floors bool, wCells, hCells int, sel *poiEntry) string {
	dotW, dotH := max(wCells, 1)*2, max(hCells, 1)*4
	cv := braille.NewCanvas(dotW, dotH)
	cv.Background(mapBgR, mapBgG, mapBgB) // a light "paper" backdrop so dark lines show

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
		cv.Line(x0, y0, x1, y1, ink(seg.R), ink(seg.G), ink(seg.B)) // darker ink on the paper bg
	}
	// POI markers on top of the geometry — bright (not darkened like walls) so
	// labeled spots pop; the legend (mapView) names them. Floor-scoped like walls.
	for _, p := range mp.Points {
		if floors && havePlayer && !onFloor(p.Z, refZ) {
			continue
		}
		mx, my := project(p.X, p.Y)
		r, g, b := poiRGB(p.Label)
		drawMarker(cv, mx, my, r, g, b)
	}
	// emoji pins (double-width), drawn over the canvas: the player, plus the
	// selected POI flag. Player is listed first so it wins a shared cell (if a POI
	// sits where you stand, "you are here" shows).
	var markers []braille.Marker
	if havePlayer {
		dx, dy := project(-px, -py) // orient(): map coords are negated vs /loc
		if col, row, ok := cellAt(cv, dx, dy); ok {
			markers = append(markers, braille.Marker{Col: col, Row: row, Glyph: playerMarker})
		}
	}
	if sel != nil {
		// the selected POI is often off-canvas at high zoom, so clamp its pin to the
		// edge in its direction — an always-visible "it's that way" indicator that
		// reads together with the panel's arrow + distance.
		sx, sy := project(sel.x, sel.y)
		col, row := clampCell(cv, sx, sy)
		markers = append(markers, braille.Marker{Col: col, Row: row, Glyph: poiPin})
	}
	if len(markers) > 0 {
		return cv.StringWithMarkers(markers)
	}
	return cv.String()
}

// playerMarker is the "you are here" glyph drawn on the map (double-width emoji).
const playerMarker = "📍"

// the map's "paper" backdrop — a muted parchment so the map's dark line colors
// (a lot of EQ geometry is ~64,64,64 gray) stay legible instead of vanishing
// into a black terminal.
const (
	mapBgR, mapBgG, mapBgB = 198, 190, 170
)

// inkDarken scales map line colors down so they read as darker "ink" against the
// light paper background.
const inkDarken = 0.55

func ink(c uint8) uint8 { return uint8(float64(c) * inkDarken) }

// zFloorBand is the Z half-window for "this floor". Floors in multi-level zones
// sit ~50 apart (HighKeep), so this captures the current floor while excluding
// the next one. A segment outside the band is on another floor and isn't drawn.
const zFloorBand = 30.0

// onFloor reports whether a segment at height z belongs to the reference floor.
func onFloor(z, ref float64) bool { return math.Abs(z-ref) <= zFloorBand }

// poiKind groups a POI by what its label tells us (the map files carry no type
// metadata, so this is a label heuristic): a zone connection ("to <Zone>"), an
// NPC ("Name (Role)" — merchants/GMs/vendors), or a generic point (a named spot,
// mob, or object — the label can't reliably tell those apart).
type poiKind int

const (
	poiZone poiKind = iota
	poiNPC
	poiPoint
)

// poiPin is the glyph dropped on the currently-selected POI — deliberately NOT
// the player's pin, so "where I am" and "where that is" never look the same.
const poiPin = "🚩"

var (
	poiKindHex  = [...]string{poiZone: "#2b6cff", poiNPC: "#37b24d", poiPoint: "#e8730c"}
	poiKindName = [...]string{poiZone: "ZONE LINES", poiNPC: "NPCS", poiPoint: "POINTS"}
)

// classifyPOI buckets a label. Labels arrive with underscores already turned to
// spaces (see eqmap.parsePoint), so "to_Chardok" reads as "to Chardok".
func classifyPOI(label string) poiKind {
	l := strings.ToLower(label)
	switch {
	case strings.HasPrefix(l, "to ") || strings.Contains(l, "zone"):
		return poiZone
	case strings.Contains(label, "("): // "Acet (Merchant)", "Beek (GM_Cleric)", "(Spells)"
		return poiNPC
	default:
		return poiPoint
	}
}

func poiRGB(label string) (uint8, uint8, uint8) {
	r, g, b := hexRGB(poiKindHex[classifyPOI(label)])
	return uint8(r), uint8(g), uint8(b)
}

// drawMarker stamps a small "+" (centre + 4 neighbours) so a POI stands out from
// thin wall lines. Off-canvas dots are clipped by Set.
func drawMarker(cv *braille.Canvas, x, y int, r, g, b uint8) {
	cv.Set(x, y, r, g, b)
	cv.Set(x-1, y, r, g, b)
	cv.Set(x+1, y, r, g, b)
	cv.Set(x, y-1, r, g, b)
	cv.Set(x, y+1, r, g, b)
}

// poiArrows are the 8 screen-relative compass arrows, indexed by 45° sector from
// +x going clockwise (screen +y is down).
var poiArrows = [8]string{"→", "↘", "↓", "↙", "←", "↖", "↑", "↗"}

// compass8 maps a screen-space delta (dx right, dy down) to an 8-way arrow that
// matches where the POI sits on the rendered map.
func compass8(dx, dy float64) string {
	return poiArrows[(int(math.Round(math.Atan2(dy, dx)/(math.Pi/4)))+8)%8]
}

// poiEntry is one POI scored against the player's position for the side panel.
type poiEntry struct {
	kind   poiKind
	label  string
	x, y   float64 // map coords (for the selected pin)
	dist   float64
	dx, dy float64 // screen-space delta from the player (for the arrow)
}

// orderedPOIs scores every POI against the player /loc (the map negates it, so
// the player sits at (-px,-py)) and orders them by kind, then by distance — the
// stable order the side panel lists and the selection indexes into.
func orderedPOIs(pts []MapPoint, px, py float64) []poiEntry {
	es := make([]poiEntry, 0, len(pts))
	for _, p := range pts {
		dx, dy := p.X+px, p.Y+py
		es = append(es, poiEntry{classifyPOI(p.Label), p.Label, p.X, p.Y, math.Hypot(dx, dy), dx, dy})
	}
	sort.SliceStable(es, func(i, j int) bool {
		if es[i].kind != es[j].kind {
			return es[i].kind < es[j].kind
		}
		return es[i].dist < es[j].dist
	})
	return es
}

// poiPanel renders the selectable POI list to width w, grouped under a header per
// kind, each row showing the label · distance · an arrow toward it. The selected
// entry is reverse-highlighted, and the list is windowed to h rows around the
// selection so a long list scrolls with the cursor.
func poiPanel(th theme, es []poiEntry, sel, w, h int) string {
	rows := []string{th.fg(th.accent).Bold(true).Render(truncate(fmt.Sprintf("POIs (%d)", len(es)), w))}
	selLine := 0
	lastKind := poiKind(-1)
	for i, e := range es {
		if e.kind != lastKind {
			rows = append(rows, th.fg(th.dim).Bold(true).Render(truncate(poiKindName[e.kind], w)))
			lastKind = e.kind
		}
		prefix := "  "
		st := th.fg(poiKindHex[e.kind])
		if i == sel {
			prefix, st = "▸ ", st.Reverse(true).Bold(true)
			selLine = len(rows)
		}
		body := fmt.Sprintf("%s%s %.0f%s", prefix, e.label, e.dist, compass8(e.dx, e.dy))
		rows = append(rows, st.Render(truncate(body, w)))
	}
	// window the rows around the selected line so it stays visible.
	start := 0
	if len(rows) > h {
		start = max(0, min(selLine-h/2, len(rows)-h))
	}
	return strings.Join(rows[start:min(start+h, len(rows))], "\n")
}

// cellAt converts a dot coordinate to its character cell, reporting whether it's
// on-canvas. The column is pulled in one cell at the right edge so a double-width
// pin glyph still fits.
func cellAt(cv *braille.Canvas, dotX, dotY int) (col, row int, ok bool) {
	col, row = dotX/2, dotY/4
	if col < 0 || row < 0 || col >= cv.Cols() || row >= cv.Rows() {
		return 0, 0, false
	}
	if col > cv.Cols()-2 {
		col = cv.Cols() - 2
	}
	return col, row, true
}

// clampCell is cellAt without the off-canvas drop: an off-map dot is pulled to
// the nearest edge cell, so a marker for an off-screen target shows on the border
// in its direction (room kept for the double-width glyph at the right edge).
func clampCell(cv *braille.Canvas, dotX, dotY int) (col, row int) {
	return clampInt(dotX/2, 0, max(cv.Cols()-2, 0)), clampInt(dotY/4, 0, max(cv.Rows()-1, 0))
}
