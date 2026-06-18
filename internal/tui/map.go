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
	// a one-line legend of the nearest POIs (nearest zone line first) when we know
	// where the player is; it costs the canvas one row.
	legend := ""
	if haveLoc && len(m.curMap.Points) > 0 {
		legend = poiLegend(th, m.curMap.Points, px, py, w)
	}
	canvasH := bodyH
	if legend != "" {
		canvasH = max(bodyH-1, 1)
	}
	out := header + "\n" + renderMapCanvas(m.curMap, px, py, pz, haveLoc, m.mapZoom, m.mapFloors, w, canvasH)
	if legend != "" {
		out += "\n" + legend
	}
	return out
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
func renderMapCanvas(mp *MapData, px, py, pz float64, havePlayer bool, zoom float64, floors bool, wCells, hCells int) string {
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
		r, g, b := poiRGB(p)
		drawMarker(cv, mx, my, r, g, b)
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

// POI marker colors (kept bright — not darkened like walls — so they pop on the
// parchment): zone connections vs other labeled spots. The canvas markers and the
// legend share these so the two read as the same thing.
const (
	poiZoneHex = "#2b6cff" // zone lines / connections
	poiSpotHex = "#e8730c" // camps, named mobs, other points
)

// isZoneLine guesses whether a POI label marks a zone connection (the most
// navigationally useful kind), so it gets the distinct zone color.
func isZoneLine(label string) bool { return strings.Contains(strings.ToLower(label), "zone") }

func poiHex(p MapPoint) string {
	if isZoneLine(p.Label) {
		return poiZoneHex
	}
	return poiSpotHex
}

func poiRGB(p MapPoint) (uint8, uint8, uint8) {
	r, g, b := hexRGB(poiHex(p))
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

// poiLegend renders one width-bounded line of the nearest points of interest —
// the nearest zone line(s) first (most useful for navigation), then the nearest
// other spots — each a colored marker, label, distance and an arrow toward it.
// px,py are the player's /loc world coords (the map negates them, so the player
// sits at (-px,-py) in map space). Greedily packs entries until the width runs
// out, so the line never overflows.
func poiLegend(th theme, pts []MapPoint, px, py float64, w int) string {
	type scored struct {
		p      MapPoint
		d      float64
		dx, dy float64
	}
	var zones, spots []scored
	for _, p := range pts {
		dx, dy := p.X+px, p.Y+py
		e := scored{p, math.Hypot(dx, dy), dx, dy}
		if isZoneLine(p.Label) {
			zones = append(zones, e)
		} else {
			spots = append(spots, e)
		}
	}
	byDist := func(s []scored) { sort.Slice(s, func(i, j int) bool { return s[i].d < s[j].d }) }
	byDist(zones)
	byDist(spots)

	var segs []string
	used := 0
	for _, e := range append(zones, spots...) {
		seg := fmt.Sprintf("◆ %s %.0f%s", truncate(e.p.Label, 12), e.d, compass8(e.dx, e.dy))
		sw := lipgloss.Width(seg)
		if len(segs) > 0 && used+2+sw > w {
			break
		}
		used += 2 + sw
		segs = append(segs, th.fg(poiHex(e.p)).Render(seg))
	}
	return strings.Join(segs, "  ")
}
