package prices

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// realPayload is a trimmed pair of pigparse records (one with 30-day sales, one
// with only older sales) shaped exactly like the live API.
const realPayload = `[
 {"eQitemId":17213,"itemName":"Black Sapphire Electrum Earring",
  "totalWTSLast30DaysCount":1367,"totalWTSLast30DaysAverage":615,
  "totalWTSLast90DaysCount":4258,"totalWTSLast90DaysAverage":625,
  "totalWTSLastYearCount":9349,"totalWTSLastYearAverage":635},
 {"eQitemId":1,"itemName":"Dusty Old Thing",
  "totalWTSLast30DaysCount":0,"totalWTSLast30DaysAverage":0,
  "totalWTSLast90DaysCount":0,"totalWTSLast90DaysAverage":0,
  "totalWTSLastYearCount":4,"totalWTSLastYearAverage":120}
]`

func TestLookup(t *testing.T) {
	var gotPaths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPaths = append(gotPaths, r.URL.Path)
		// echo back records for any names asked (the canned two)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(realPayload))
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, HTTP: srv.Client()}
	got, err := c.Lookup(ServerGreen, []string{"Black Sapphire Electrum Earring", "Dusty Old Thing"})
	if err != nil {
		t.Fatal(err)
	}
	// keyed by lowercased name
	bs := got["black sapphire electrum earring"]
	if bs.Avg != 615 || bs.Count != 1367 || bs.Window != "30d" {
		t.Errorf("earring price = %+v, want 615/1367/30d", bs)
	}
	// falls back to the year window when 30d/90d have no sales
	dusty := got["dusty old thing"]
	if dusty.Avg != 120 || dusty.Window != "1y" {
		t.Errorf("dusty fallback = %+v, want 120/1y", dusty)
	}
	if !strings.Contains(gotPaths[0], "/api/item/getmultiple/0") {
		t.Errorf("unexpected request path %q", gotPaths[0])
	}
}

// TestLookupBatches: more than batchSize names produce multiple requests.
func TestLookupBatches(t *testing.T) {
	var reqs int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqs++
		w.WriteHeader(http.StatusNoContent) // none match — fine, just count requests
	}))
	defer srv.Close()

	names := make([]string, batchSize*2+3) // → 3 batches
	for i := range names {
		names[i] = "Item" + string(rune('A'+i%26))
	}
	c := &Client{BaseURL: srv.URL, HTTP: srv.Client()}
	if _, err := c.Lookup(ServerGreen, names); err != nil {
		t.Fatal(err)
	}
	if reqs != 3 {
		t.Errorf("expected 3 batched requests, got %d", reqs)
	}
}

// TestLookupAllFail: when every batch errors, Lookup surfaces the error.
func TestLookupAllFail(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	c := &Client{BaseURL: srv.URL, HTTP: srv.Client()}
	if _, err := c.Lookup(ServerGreen, []string{"x"}); err == nil {
		t.Error("expected an error when all batches fail")
	}
}
