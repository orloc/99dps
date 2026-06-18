package main

import (
	"99dps/internal/eqmap"
	"99dps/internal/prices"
	"99dps/internal/tui"
)

// Adapters that wire the network-bearing eqmap / prices clients to the TUI's
// capability interfaces. They live here, in the composition root, so the UI
// package depends only on the interfaces (tui.MapSource / tui.PriceSource) and
// never imports the network packages — side-effecting infrastructure stays at
// the edge.

// mapAdapter adapts an *eqmap.Loader to tui.MapSource, copying the loader's map
// into the UI's view-model type.
type mapAdapter struct{ l *eqmap.Loader }

func (mapAdapter) ShortName(zone string) string { return eqmap.ShortName(zone) }

func (a mapAdapter) Load(short string) (*tui.MapData, error) {
	m, err := a.l.Get(short)
	if err != nil {
		return nil, err
	}
	out := &tui.MapData{MinX: m.MinX, MinY: m.MinY, MinZ: m.MinZ, MaxX: m.MaxX, MaxY: m.MaxY}
	out.Segments = make([]tui.MapSegment, len(m.Segments))
	for i, s := range m.Segments {
		out.Segments[i] = tui.MapSegment{X1: s.X1, Y1: s.Y1, X2: s.X2, Y2: s.Y2, Z: s.Z, R: s.R, G: s.G, B: s.B}
	}
	out.Points = make([]tui.MapPoint, len(m.Points))
	for i, p := range m.Points {
		out.Points[i] = tui.MapPoint{X: p.X, Y: p.Y, Z: p.Z, R: p.R, G: p.G, B: p.B, Label: p.Label}
	}
	return out, nil
}

// priceAdapter adapts a *prices.Client to tui.PriceSource, binding the server
// (the "which server" policy belongs to the app, not the UI).
type priceAdapter struct{ c *prices.Client }

func (a priceAdapter) Lookup(names []string) (map[string]tui.ItemPrice, error) {
	raw, err := a.c.Lookup(prices.ServerGreen, names)
	if err != nil {
		return nil, err
	}
	out := make(map[string]tui.ItemPrice, len(raw))
	for k, v := range raw {
		out[k] = tui.ItemPrice{Avg: v.Avg, Count: v.Count}
	}
	return out, nil
}
