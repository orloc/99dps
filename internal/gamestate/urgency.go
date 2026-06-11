package gamestate

// Urgency classifies how close a timer is to fading. It is the SINGLE shared
// definition behind both the TUI countdown color and the spell-tracker's
// refresh-vs-new-mob test, so what you SEE (a red, about-to-fade timer) is
// exactly what the tracker treats as refreshable.
//
// The cutoffs are the last fraction of a timer's life, but CAPPED in absolute
// time — otherwise a multi-hour self-buff reads as "low" (orange) with an hour
// still on it, and "red" with many minutes left. The cap makes orange and red
// mean the same wall-clock thing whether the buff lasts 30 seconds or 3 hours.
type Urgency int

const (
	Fresh    Urgency = iota // green — plenty of time
	Low                     // gold/orange — getting low, recast soon
	Expiring                // red — about to fade, recast now
)

const (
	StaleFrac    = 0.2 // expiring (red) cutoff, as a fraction of full duration…
	lowFrac      = 0.5 // low (gold) cutoff, as a fraction of full duration…
	expireCapSec = 60  // …but red never triggers with more than a minute left
	lowCapSec    = 300 // …and gold never triggers with more than five minutes left
)

// TimerUrgency classifies a timer by its remaining vs full duration (seconds).
func TimerUrgency(remaining, total int64) Urgency {
	if total <= 0 {
		return Expiring
	}
	expiring := min(int64(StaleFrac*float64(total)), int64(expireCapSec))
	low := min(int64(lowFrac*float64(total)), int64(lowCapSec))
	switch {
	case remaining <= expiring:
		return Expiring
	case remaining <= low:
		return Low
	default:
		return Fresh
	}
}
