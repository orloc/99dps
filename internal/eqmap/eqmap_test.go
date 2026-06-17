package eqmap

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
	missHits := hits
	if _, err := l.Get("nowhere"); err == nil { // negatively cached
		t.Error("a zone with no map data should error")
	}
	if hits != missHits {
		t.Errorf("a repeated miss should be negatively cached, made %d new requests", hits-missHits)
	}
}

// A transport/server error (not a clean 404) must NOT be negatively cached, so a
// later call can retry and succeed.
func TestLoaderTransientErrorNotCached(t *testing.T) {
	var hits int
	fail := true
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if fail {
			w.WriteHeader(http.StatusServiceUnavailable) // transient
			return
		}
		if strings.HasSuffix(r.URL.Path, "sebilis.txt") {
			w.Write([]byte("L 0,0,0, 10,10,0, 1,2,3\n"))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	l := &Loader{BaseURL: srv.URL, HTTP: srv.Client(), mem: map[string]*Map{}}
	if _, err := l.Get("sebilis"); err == nil {
		t.Fatal("expected an error while the server is failing")
	}
	hitsAfterFail := hits
	fail = false // server recovers
	m, err := l.Get("sebilis")
	if err != nil || m == nil || len(m.Segments) != 1 {
		t.Fatalf("retry after recovery should succeed; got %v, err %v", m, err)
	}
	if hits == hitsAfterFail {
		t.Error("a transient failure should not be cached — the retry must hit the network")
	}
}

// The disk cache is written atomically on first fetch and read back offline:
// a second loader pointed at the same dir serves the zone with the server down.
func TestLoaderDiskCacheRoundTrip(t *testing.T) {
	dir := t.TempDir()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "sebilis.txt") {
			w.Write([]byte("L 0,0,0, 10,10,0, 1,2,3\n"))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))

	l1 := &Loader{BaseURL: srv.URL, HTTP: srv.Client(), dir: dir, mem: map[string]*Map{}}
	if _, err := l1.Get("sebilis"); err != nil {
		t.Fatalf("first Get: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "sebilis.txt")); err != nil {
		t.Fatalf("base file should be cached on disk: %v", err)
	}
	srv.Close() // network is now gone

	l2 := &Loader{BaseURL: srv.URL, HTTP: srv.Client(), dir: dir, mem: map[string]*Map{}}
	m, err := l2.Get("sebilis")
	if err != nil || m == nil || len(m.Segments) != 1 {
		t.Fatalf("offline read from disk cache failed: %d segs, err %v", len(segsOf(m)), err)
	}
}

func segsOf(m *Map) []Segment {
	if m == nil {
		return nil
	}
	return m.Segments
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
