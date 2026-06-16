// Package eqmap loads EQ zone maps in the MQ2/Brewall vector format (the same
// "L"/"P" line files the web overlay uses) and exposes them as line segments +
// labeled points in world coordinates. Files are fetched once per zone from a
// public repo and cached on disk, so it works offline after the first load —
// the one on-demand network path here, like internal/prices. Pure Go, no cgo.
package eqmap

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// DefaultBaseURL is the public source of EQ map line-files (world coordinates,
// so segments overlay /loc directly). Overridable for tests.
const DefaultBaseURL = "https://raw.githubusercontent.com/brainiac/eqmap/master/maps/"

// Segment is one line of zone geometry — endpoints in world coords + color. Z is
// the segment's mid-height (for fading far floors in multi-level zones).
type Segment struct {
	X1, Y1, X2, Y2, Z float64
	R, G, B           uint8
}

// Point is a labeled point of interest ("P" line): a position, color, and text.
type Point struct {
	X, Y, Z float64
	R, G, B uint8
	Label   string
}

// Map is a zone's parsed geometry plus its bounding box (for auto-fit). MinZ is
// the lowest segment mid-height — the default floor when there's no player /loc.
type Map struct {
	Zone                   string
	Segments               []Segment
	Points                 []Point
	MinX, MinY, MaxX, MaxY float64
	MinZ                   float64
}

// Empty reports whether the map has no drawable geometry.
func (m *Map) Empty() bool { return m == nil || len(m.Segments) == 0 }

// Parse reads the L (segment) and P (point) lines of a Brewall map file into m.
// Unknown directives are ignored. It appends, so several layer files can be
// parsed into one Map.
func Parse(r io.Reader, m *Map) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if len(line) < 2 {
			continue
		}
		switch line[0] {
		case 'L':
			if f, ok := floats(line[1:], 9); ok {
				m.Segments = append(m.Segments, Segment{
					X1: f[0], Y1: f[1], X2: f[3], Y2: f[4], Z: (f[2] + f[5]) / 2,
					R: clampByte(f[6]), G: clampByte(f[7]), B: clampByte(f[8]),
				})
			}
		case 'P':
			parsePoint(line[1:], m)
		}
	}
}

// parsePoint handles "P x, y, z, r, g, b, size, label" — the label is the tail
// after the 7 numeric fields (underscores → spaces).
func parsePoint(s string, m *Map) {
	parts := strings.SplitN(s, ",", 8)
	if len(parts) < 8 {
		return
	}
	var f [6]float64
	for i := 0; i < 6; i++ {
		v, err := strconv.ParseFloat(strings.TrimSpace(parts[i]), 64)
		if err != nil {
			return
		}
		f[i] = v
	}
	m.Points = append(m.Points, Point{
		X: f[0], Y: f[1], Z: f[2], R: clampByte(f[3]), G: clampByte(f[4]), B: clampByte(f[5]),
		Label: strings.TrimSpace(strings.ReplaceAll(parts[7], "_", " ")),
	})
}

// floats parses the first n comma/space-separated numbers from s.
func floats(s string, n int) ([]float64, bool) {
	fields := strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' })
	if len(fields) < n {
		return nil, false
	}
	out := make([]float64, n)
	for i := 0; i < n; i++ {
		v, err := strconv.ParseFloat(fields[i], 64)
		if err != nil {
			return nil, false
		}
		out[i] = v
	}
	return out, true
}

func clampByte(v float64) uint8 {
	switch {
	case v < 0:
		return 0
	case v > 255:
		return 255
	default:
		return uint8(v)
	}
}

// ComputeBounds fills the Map's bounding box from its segments and points.
func ComputeBounds(m *Map) {
	first := true
	upd := func(x, y float64) {
		if first {
			m.MinX, m.MaxX, m.MinY, m.MaxY, first = x, x, y, y, false
			return
		}
		m.MinX, m.MaxX = min(m.MinX, x), max(m.MaxX, x)
		m.MinY, m.MaxY = min(m.MinY, y), max(m.MaxY, y)
	}
	for i, s := range m.Segments {
		upd(s.X1, s.Y1)
		upd(s.X2, s.Y2)
		if i == 0 || s.Z < m.MinZ {
			m.MinZ = s.Z
		}
	}
	for _, p := range m.Points {
		upd(p.X, p.Y)
	}
}

// shortNameOverrides maps an EQ zone *display* name (from "You have entered X",
// lowercased) to its map-file short name, for the many zones whose display name
// doesn't normalize to the file name. Short names are the canonical EQ ones,
// verified against the map repo; a wrong/missing entry just degrades to "no map"
// (the repo also has empty placeholders for some classic zones). Keys are the
// exact display strings as logged.
var shortNameOverrides = map[string]string{
	// --- Classic: Faydwer ---
	"greater faydark": "gfaydark", "lesser faydark": "lfaydark",
	"butcherblock mountains": "butcher", "north kaladim": "kaladima", "south kaladim": "kaladimb",
	"ak'anon": "akanon", "steamfont mountains": "steamfont", "castle mistmoore": "mistmoore",
	"clan runnyeye": "runnyeye",
	// --- Classic: Antonica ---
	"east commonlands": "ecommons", "west commonlands": "commons",
	"north freeport": "freportn", "east freeport": "freporte", "west freeport": "freportw",
	"northern desert of ro": "nro", "southern desert of ro": "sro",
	"oasis of marr": "oasis", "the oasis of marr": "oasis",
	"innothule swamp": "innothule", "the feerrott": "feerrott",
	"lake rathetear": "lakerathe", "rathe mountains": "rathemtn",
	"northern plains of karana": "northkarana", "southern plains of karana": "southkarana",
	"eastern plains of karana": "eastkarana",
	"highpass hold":            "highpasshold", "high keep": "highkeep", "surefall glade": "qrg",
	"misty thicket": "mistythicket", "kithicor woods": "kithicor", "kithicor forest": "kithicor",
	"the nektulos forest": "nektulos", "nektulos forest": "nektulos",
	"lavastorm mountains": "lavastorm", "everfrost peaks": "everfrost",
	"permafrost caverns": "permafrost", "solusek's eye": "soldunga", "nagafen's lair": "soldungb",
	"the estate of unrest": "unrest", "estate of unrest": "unrest",
	"lost temple of cazic-thule": "cazicthule", "cazic-thule": "cazicthule",
	"kerra isle": "kerraridge",
	// --- Classic: Odus ---
	"erudin": "erudnext", "erudin palace": "erudnint", "toxxulia forest": "tox",
	"erud's crossing": "erudsxing", "the hole": "hole", "stonebrunt mountains": "stonebrunt",
	// --- Kunark ---
	"cabilis east": "cabeast", "cabilis west": "cabwest", "field of bone": "fieldofbone",
	"warsliks woods": "warslikswood", "lake of ill omen": "lakeofillomen", "firiona vie": "firiona",
	"the burning wood": "burningwood", "frontier mountains": "frontiermtns",
	"the overthere": "overthere", "skyfire mountains": "skyfire", "timorous deep": "timorous",
	"the emerald jungle": "emeraldjungle", "trakanon's teeth": "trakanon",
	"the city of mist": "citymist", "city of mist": "citymist", "old sebilis": "sebilis",
	"karnor's castle": "karnor", "the howling stones": "charasis", "howling stones": "charasis",
	"mines of nurga": "nurga", "temple of droga": "droga", "the crypt of dalnir": "dalnir",
	"swamp of no hope": "swampofnohope", "warslik's woods": "warslikswood",
	// --- Velious ---
	"great divide": "greatdivide", "the great divide": "greatdivide",
	"eastern wastelands": "eastwastes", "western wastes": "westwastes",
	"iceclad ocean": "iceclad", "cobalt scar": "cobaltscar", "kael drakkel": "kael",
	"the wakening lands": "wakening", "wakening lands": "wakening",
	"temple of veeshan": "templeveeshan", "sleepers tomb": "sleeper", "sleeper's tomb": "sleeper",
	"dragon necropolis": "necropolis", "tower of frozen shadow": "frozenshadow",
	"siren's grotto": "sirens", "sirens grotto": "sirens", "icewell keep": "thurgadinb",
	"crystal caverns": "crystal", "velketor's labyrinth": "velketor",
	// --- Planes ---
	"plane of fear": "fear", "plane of hate": "hate", "plane of sky": "airplane",
	"plane of growth": "growthplane", "plane of mischief": "mischiefplane",
}

// ShortName maps a zone display name to its map-file short name (best-effort: an
// override if known, else lowercase with spaces/punctuation stripped). Returns ""
// for an empty zone.
func ShortName(display string) string {
	d := strings.ToLower(strings.TrimSpace(display))
	if d == "" {
		return ""
	}
	if s, ok := shortNameOverrides[d]; ok {
		return s
	}
	var b strings.Builder
	for _, r := range d {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// Loader fetches and caches zone maps. Safe for concurrent use.
type Loader struct {
	BaseURL string
	HTTP    *http.Client
	dir     string // on-disk cache (empty = memory only)

	mu  sync.Mutex
	mem map[string]*Map
}

// NewLoader builds a loader against the public repo with a disk cache under the
// user cache dir.
func NewLoader() *Loader {
	dir := ""
	if d, err := os.UserCacheDir(); err == nil {
		dir = filepath.Join(d, "99dps", "maps")
		_ = os.MkdirAll(dir, 0o755)
	}
	return &Loader{
		BaseURL: DefaultBaseURL,
		HTTP:    &http.Client{Timeout: 15 * time.Second},
		dir:     dir,
		mem:     map[string]*Map{},
	}
}

// Get returns the parsed map for a zone short name (e.g. "sebilis"), fetching +
// caching on first use. Reads the base file plus its _1.._3 layers.
func (l *Loader) Get(short string) (*Map, error) {
	short = strings.ToLower(strings.TrimSpace(short))
	if short == "" {
		return nil, fmt.Errorf("empty zone")
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.mem == nil {
		l.mem = map[string]*Map{}
	}
	if m, ok := l.mem[short]; ok {
		return m, nil
	}
	m := &Map{Zone: short}
	for _, suffix := range []string{"", "_1", "_2", "_3"} {
		if raw, err := l.fetch(short + suffix + ".txt"); err == nil && len(raw) > 0 {
			Parse(bytes.NewReader(raw), m)
		}
	}
	if m.Empty() {
		return nil, fmt.Errorf("no map data for %q", short)
	}
	ComputeBounds(m)
	l.mem[short] = m
	return m, nil
}

// fetch reads a file from the disk cache, else the network (caching the result).
func (l *Loader) fetch(name string) ([]byte, error) {
	if l.dir != "" {
		if b, err := os.ReadFile(filepath.Join(l.dir, name)); err == nil {
			return b, nil
		}
	}
	resp, err := l.HTTP.Get(strings.TrimRight(l.BaseURL, "/") + "/" + name)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	if l.dir != "" && len(b) > 0 {
		_ = os.WriteFile(filepath.Join(l.dir, name), b, 0o644)
	}
	return b, nil
}
