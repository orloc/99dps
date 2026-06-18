package tui

// External capability seams. The TUI consumes the two network-bearing features
// (zone maps, tunnel prices) through these interfaces so it never imports the
// eqmap / prices packages directly — the concrete clients are constructed and
// adapted in the composition root (cmd/99dps) and injected via NewProgram. Tests
// inject fakes. This keeps the UI's "consume snapshots/interfaces, not side
// effects" discipline: side-effecting infrastructure lives at the edges.

// MapSource loads a zone's map geometry. ShortName maps a full zone name to its
// map's short key ("" when the zone isn't mappable); Load fetches that key.
type MapSource interface {
	ShortName(zone string) string
	Load(short string) (*MapData, error)
}

// MapData is a zone's drawable geometry — a view-model copy of the loader's map,
// so the renderer depends on this type, not on the loader package. Exported so
// the cmd adapter can build it.
type MapData struct {
	MinX, MinY, MinZ, MaxX, MaxY float64
	Segments                     []MapSegment
	Points                       []MapPoint // labeled points of interest (zone lines, camps)
}

// MapSegment is one map line: its endpoints, mid-height (Z, for floor-scoping),
// and color.
type MapSegment struct {
	X1, Y1, X2, Y2, Z float64
	R, G, B           uint8
}

// MapPoint is a labeled point of interest from the map's "P" lines: a position,
// its file color, and a human label ("Zone In", a camp/mob name).
type MapPoint struct {
	X, Y, Z float64
	R, G, B uint8
	Label   string
}

// empty reports whether the map has no drawable geometry (nil-safe).
func (d *MapData) empty() bool { return d == nil || len(d.Segments) == 0 }

// PriceSource looks up tunnel (pigparse) prices by item name. The server choice
// is bound by the adapter, so the UI stays policy-free.
type PriceSource interface {
	Lookup(names []string) (map[string]ItemPrice, error)
}

// ItemPrice is the tunnel-price datum the inventory widget needs for one item.
// Exported so the cmd adapter can build it.
type ItemPrice struct {
	Avg   int // recent WTS average, plat
	Count int // sample size behind Avg (0 = no tunnel data)
}
