package spell

import (
	"encoding/json"
	"os"
	"sync"
)

// Overrides holds user-set per-zone, per-mob respawn overrides (in seconds),
// persisted to a JSON file. They layer on top of the wiki zone defaults: a kill
// uses the override for (zone, mob) if present, else the zone default.
type Overrides struct {
	mu   sync.RWMutex
	path string
	data map[string]map[string]int // zone -> mob -> seconds
}

// LoadOverrides reads overrides from path. A missing/unreadable file yields an
// empty (but usable) set — not an error.
func LoadOverrides(path string) *Overrides {
	o := &Overrides{path: path, data: map[string]map[string]int{}}
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, &o.data)
		if o.data == nil {
			o.data = map[string]map[string]int{}
		}
	}
	return o
}

// Get returns the override seconds for (zone, mob), if one is set.
func (o *Overrides) Get(zone, mob string) (int, bool) {
	if o == nil {
		return 0, false
	}
	o.mu.RLock()
	defer o.mu.RUnlock()
	if m, ok := o.data[zone]; ok {
		s, ok := m[mob]
		return s, ok
	}
	return 0, false
}

// Set records an override for (zone, mob) and persists the file. A sec <= 0
// removes the override (reverting to the zone default).
func (o *Overrides) Set(zone, mob string, sec int) error {
	if o == nil {
		return nil
	}
	o.mu.Lock()
	if sec <= 0 {
		delete(o.data[zone], mob)
	} else {
		if o.data[zone] == nil {
			o.data[zone] = map[string]int{}
		}
		o.data[zone][mob] = sec
	}
	b, _ := json.MarshalIndent(o.data, "", "  ")
	path := o.path
	o.mu.Unlock()

	if path == "" {
		return nil
	}
	return os.WriteFile(path, b, 0o644)
}
