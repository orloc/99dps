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
		"Sebilis":              "sebilis",
		"Lake of Ill Omen":     "lakeofillomen",
		"The Estate of Unrest": "unrest", // override
		"Plane of Fear":        "fear",   // override
		"Kael Drakkel":         "kael",   // override
		"":                     "",
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
			w.Write([]byte("L 0,0,0, 10,10,0, 1,2,3\n"))
		case strings.HasSuffix(r.URL.Path, "sebilis_1.txt"):
			w.Write([]byte("L 10,10,0, 20,5,0, 4,5,6\n"))
		default:
			w.WriteHeader(http.StatusNotFound) // _2 / _3 missing
		}
	}))
	defer srv.Close()

	l := &Loader{BaseURL: srv.URL, HTTP: srv.Client(), mem: map[string]*Map{}} // no disk cache
	m, err := l.Get("Sebilis")
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Segments) != 2 { // base + _1 merged, _2/_3 404 ignored
		t.Fatalf("want 2 merged segments, got %d", len(m.Segments))
	}
	firstHits := hits
	if _, err := l.Get("sebilis"); err != nil { // memory cache → no new requests
		t.Fatal(err)
	}
	if hits != firstHits {
		t.Errorf("second Get should hit the cache, made %d new requests", hits-firstHits)
	}

	if _, err := l.Get("nowhere"); err == nil {
		t.Error("a zone with no map data should error")
	}
}
