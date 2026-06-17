// Package prices is a small client for the pigparse P99 tunnel-price API
// (https://pigparse.azurewebsites.net). It looks up recent want-to-sell averages
// by item name. It's the one part of the app that reaches the network, so it's
// only ever called on demand. Pure Go (net/http), no cgo.
package prices

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// DefaultBaseURL is the public pigparse host. Overridable for tests.
const DefaultBaseURL = "https://pigparse.azurewebsites.net"

// Server identifiers used by the API.
const (
	ServerGreen = 0
	ServerBlue  = 1
	ServerRed   = 2
	ServerQuarm = 3
)

// Price is the tunnel pricing we surface for one item: the most recent
// want-to-sell average (plat) and the sample size + window it came from. A
// Count of 0 means pigparse has no recent sale history (no-drop, vendor junk, or
// simply unsold).
type Price struct {
	Name   string
	ID     int
	Avg    int    // recent WTS average, plat
	Count  int    // sample size behind Avg (0 = no data)
	Window string // which window Avg came from: "30d", "90d", "1y"
}

// apiItem mirrors the pigparse JSON fields we use.
type apiItem struct {
	EQItemID int    `json:"eQitemId"`
	ItemName string `json:"itemName"`
	W30Count int    `json:"totalWTSLast30DaysCount"`
	W30Avg   int    `json:"totalWTSLast30DaysAverage"`
	W90Count int    `json:"totalWTSLast90DaysCount"`
	W90Avg   int    `json:"totalWTSLast90DaysAverage"`
	WYrCount int    `json:"totalWTSLastYearCount"`
	WYrAvg   int    `json:"totalWTSLastYearAverage"`
}

// toPrice picks the freshest window that actually has sales (30d → 90d → year).
func (a apiItem) toPrice() Price {
	p := Price{Name: a.ItemName, ID: a.EQItemID}
	switch {
	case a.W30Count > 0:
		p.Avg, p.Count, p.Window = a.W30Avg, a.W30Count, "30d"
	case a.W90Count > 0:
		p.Avg, p.Count, p.Window = a.W90Avg, a.W90Count, "90d"
	case a.WYrCount > 0:
		p.Avg, p.Count, p.Window = a.WYrAvg, a.WYrCount, "1y"
	}
	return p
}

// Client talks to the pigparse API.
type Client struct {
	BaseURL string
	HTTP    *http.Client
}

// NewClient builds a client against the public host with a sane timeout.
func NewClient() *Client {
	return &Client{BaseURL: DefaultBaseURL, HTTP: &http.Client{Timeout: 15 * time.Second}}
}

// batchSize bounds how many item names go in one request URL (keeps it well
// under any query-length limit; ~25 names is comfortable).
const batchSize = 25

// Lookup fetches tunnel prices for the given item names on a server, returning a
// map keyed by lowercased name (so callers can match case-insensitively). It's
// best-effort and batched: a failed batch is skipped so a partial result still
// returns; an error comes back only when every batch failed.
func (c *Client) Lookup(server int, names []string) (map[string]Price, error) {
	out := make(map[string]Price, len(names))
	var firstErr error
	anyOK := false
	for i := 0; i < len(names); i += batchSize {
		batch := names[i:min(i+batchSize, len(names))]
		items, err := c.fetchBatch(server, batch)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		anyOK = true
		for _, it := range items {
			p := it.toPrice()
			out[strings.ToLower(p.Name)] = p
		}
	}
	if !anyOK && firstErr != nil {
		return out, firstErr
	}
	return out, nil
}

func (c *Client) fetchBatch(server int, names []string) ([]apiItem, error) {
	q := url.Values{}
	for _, n := range names {
		q.Add("itemnames", n)
	}
	u := fmt.Sprintf("%s/api/item/getmultiple/%d?%s", strings.TrimRight(c.BaseURL, "/"), server, q.Encode())
	resp, err := c.HTTP.Get(u)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNoContent {
		return nil, nil // none of these names matched
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("pigparse: %s", resp.Status)
	}
	var items []apiItem
	if err := json.NewDecoder(resp.Body).Decode(&items); err != nil {
		return nil, err
	}
	return items, nil
}
