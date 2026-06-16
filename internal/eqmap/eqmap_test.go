package eqmap

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const sampleMap = `L -100.0, -50.0, 5.0,  100.0, 50.0, 15.0,  64, 64, 64
L 100.0, 50.0, 0,  100.0, -50.0, 0,  255, 0, 0
P 0.0, 0.0, 0,  255, 255, 0,  3, Zone_In
# a comment
garbage line
`

func TestParseAndBounds(t *testing.T) {
	var m Map
	Parse(strings.NewReader(sampleMap), &m)
	if len(m.Segments) != 2 {
		t.Fatalf("want 2 segments, got %d", len(m.Segments))
	}
	if len(m.Points) != 1 || m.Points[0].Label != "Zone In" {
		t.Fatalf("want 1 point labelled 'Zone In', got %+v", m.Points)
	}
	s := m.Segments[0]
	if s.X1 != -100 || s.Y2 != 50 || s.Z != 10 || s.R != 64 {
		t.Errorf("segment[0] = %+v", s)
	}
	ComputeBounds(&m)
	if m.MinX != -100 || m.MaxX != 100 || m.MinY != -50 || m.MaxY != 50 {
		t.Errorf("bounds = (%v,%v)-(%v,%v)", m.MinX, m.MinY, m.MaxX, m.MaxY)
	}
}

func TestShortName(t *testing.T) {
	cases := map[string]string{
		// overrides (display names that don't normalize to the file name)
		"Kithicor Woods":      "kithicor",
		"The Nektulos Forest": "nektulos",
		"Nagafen's Lair":      "soldungb",
		"Old Sebilis":         "sebilis",
		"Erudin":              "erudnext",
		"Greater Faydark":     "gfaydark",
		"East Commonlands":    "ecommons",
		"Kael Drakkel":        "kael",
		"Plane of Fear":       "fear",
		// normalize fallback (display already matches the short name)
		"Rivervale": "rivervale",
		"Oggok":     "oggok",
		"Chardok":   "chardok",
		// unknown zone → best-effort normalize
		"Somewhere New": "somewherenew",
		"":              "",
	}
	for in, want := range cases {
		if got := ShortName(in); got != want {
			t.Errorf("ShortName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLoaderFetchCacheLayers(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		switch {
		case strings.HasSuffix(r.URL.Path, "sebilis.txt"):
			w.Write([]byte("L 0,0,0, 10,10,0, 1,2,3\nL 0,0,0, -5,5,0, 7,8,9\n"))
		default:
			w.WriteHeader(http.StatusNotFound) // only the base layer is loaded
		}
	}))
	defer srv.Close()

	l := &Loader{BaseURL: srv.URL, HTTP: srv.Client(), mem: map[string]*Map{}} // no disk cache
	m, err := l.Get("Sebilis")
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Segments) != 2 { // both base-layer lines; detail layers not requested
		t.Fatalf("want 2 base segments, got %d", len(m.Segments))
	}
	firstHits := hits
	if _, err := l.Get("sebilis"); err != nil { // memory cache → no new requests
		t.Fatal(err)
	}
	if hits != firstHits {
		t.Errorf("second Get should hit the cache, made %d new requests", hits-firstHits)
	}
	if firstHits != 1 { // only the base file is fetched (one request), not 4 layers
		t.Errorf("expected a single base fetch, got %d", firstHits)
	}

	if _, err := l.Get("nowhere"); err == nil {
		t.Error("a zone with no map data should error")
	}
}

// TestLoaderEmptyBaseFallsBackToLayers: some zones (e.g. Skyfire) have an empty
// base file but the real geometry in a numbered layer — load it.
func TestLoaderEmptyBaseFallsBackToLayers(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "skyfire.txt"):
			// empty base (200 OK, no body) — like the real repo
		case strings.HasSuffix(r.URL.Path, "skyfire_1.txt"):
			w.Write([]byte("L 0,0,0, 10,10,0, 1,2,3\n"))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	l := &Loader{BaseURL: srv.URL, HTTP: srv.Client()}
	m, err := l.Get("skyfire")
	if err != nil || len(m.Segments) != 1 {
		t.Fatalf("empty base should fall back to _1; got %d segs, err %v", len(m.Segments), err)
	}
}
