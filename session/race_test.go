package session

import (
	"sync"
	"testing"
	"time"

	"99dps/common"
)

// Regression for bug #3: Apply takes the write lock for the duration of the
// session mutation, so concurrent Apply / All / Current must be race-free
// under -race.
func TestApply_RaceFreeWithReaders(t *testing.T) {
	sm := &SessionManager{}

	// Seed a session so readers have something to look at.
	sm.Apply(&common.DamageSet{ActionTime: 1, Dealer: "Foo", Dmg: 1})

	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)

	// Writer: simulates the parser goroutine. Periodically crosses
	// CS_THRESHOLD so a new session is appended.
	go func() {
		defer wg.Done()
		tick := int64(2)
		for {
			select {
			case <-stop:
				return
			default:
				sm.Apply(&common.DamageSet{
					ActionTime: tick,
					Dealer:     "Foo",
					Dmg:        1,
				})
				tick += int64(segGapCeil * 2) // guarantees a roll each tick
			}
		}
	}()

	// Reader: simulates the UI Sync goroutine.
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				_ = sm.All()
				_ = sm.Current()
			}
		}
	}()

	time.Sleep(100 * time.Millisecond)
	close(stop)
	wg.Wait()
}

// Regression for bug #3 variant: Current() now returns a deep snapshot, so a
// reader can iterate its aggressors map while the writer keeps mutating the
// live session. Previously this panicked with "concurrent map iteration and
// map write" even without -race.
func TestCurrent_SnapshotIndependentOfLiveSession(t *testing.T) {
	sm := &SessionManager{}
	sm.Apply(&common.DamageSet{ActionTime: 1, Dealer: "Foo", Dmg: 1})

	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		i := 0
		for {
			select {
			case <-stop:
				return
			default:
				sm.Apply(&common.DamageSet{
					ActionTime: int64(i + 2),
					Dealer:     "d" + string(rune('a'+(i%26))),
					Dmg:        1,
				})
				i++
			}
		}
	}()

	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				snap := sm.Current()
				if snap == nil {
					continue
				}
				for k := range snap.aggressors {
					_ = k
				}
			}
		}
	}()

	time.Sleep(100 * time.Millisecond)
	close(stop)
	wg.Wait()
}
