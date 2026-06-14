package gamestate

// Damage-absorb estimation (prototype).
//
// EQ never reports how much a rune absorbed — a soaked blow just logs
// "YOUR magical skin absorbs the blow!" with no amount. So rune health can only
// be ESTIMATED: count the blows the runes have eaten and multiply by the average
// melee hit of the mobs in play (mobstats, supplied by the caller), then drain
// the active runes in their stacking order.
//
// runeBook is the curated set of absorb buffs we recognise, in depletion order
// (the first listed absorbs first). Pools are the level-60 maxima for now; level
// scaling is a later refinement. Bedlam stacks ahead of the Rune line.
var runeBook = []struct {
	name string
	pool int
}{
	{"Bedlam", 350},
	{"Rune V", 700},
	{"Rune IV", 394},
	{"Rune III", 230},
	{"Rune II", 118},
	{"Rune I", 55},
}

// runeAbsorbEmote is the (caster-side) line logged when a rune soaks a melee blow.
const runeAbsorbEmote = "magical skin absorbs the blow"

func isRune(name string) bool {
	for _, r := range runeBook {
		if r.name == name {
			return true
		}
	}
	return false
}

// RuneShield is one active absorb buff and its total pool, for the estimator.
type RuneShield struct {
	Name string
	Pool int
}

// RuneShields returns the player's active absorb buffs (in depletion order) and
// how many blows the runes have soaked since they were last (re)cast. The caller
// turns blows into absorbed damage using the mobs' average hit, then drains the
// shields in order to estimate what's left.
func (t *Tracker) RuneShields(now int64) (shields []RuneShield, absorbedBlows int) {
	if t == nil {
		return nil, 0
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, r := range runeBook {
		for _, tm := range t.timers {
			if tm.Spell == r.name && tm.Target == "You" && tm.Expiry > now {
				shields = append(shields, RuneShield{Name: r.name, Pool: r.pool})
				break
			}
		}
	}
	return shields, t.runeAbsorbs
}
