package gamestate

// Damage-absorb estimation (prototype).
//
// EQ never reports how much a rune absorbed — a soaked blow just logs
// "YOUR magical skin absorbs the blow!" with no amount. So rune health can only
// be ESTIMATED: count the blows the runes have eaten and multiply by the average
// melee hit of the mobs in play (mobstats, supplied by the caller as avgHit),
// then drain the active runes in their stacking order.
//
// Stacked runes have SEPARATE pools: Bedlam sits on top of the Rune line and
// soaks first; only its overflow reaches the rune underneath. So absorption is
// tracked PER rune (runeAbsorbed) and drained incrementally — refreshing one rune
// restores only its own pool and leaves the others' depletion intact. (The naive
// single shared counter wrongly "refilled" the underlying rune whenever the top
// one was recast.)
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

func runePool(name string) (int, bool) {
	for _, r := range runeBook {
		if r.name == name {
			return r.pool, true
		}
	}
	return 0, false
}

func isRune(name string) bool {
	_, ok := runePool(name)
	return ok
}

// RuneShield is one active absorb buff with its total pool and the estimated
// absorb remaining (Pool when avgHit is unknown / nothing soaked yet).
type RuneShield struct {
	Name      string
	Pool      int
	Remaining int
}

// RuneAttacker returns the mob that landed the most recent soaked blow — the one
// whose average melee hit should size the absorb estimate — or "".
func (t *Tracker) RuneAttacker() string {
	if t == nil {
		return ""
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.runeAttacker
}

// RuneShields returns the player's active absorb buffs in depletion order, each
// with its estimated remaining absorb, plus the total blows soaked (for display).
// avgHit is the attacker's average melee hit (0 if unknown → no draining, shields
// read full). Draining is incremental and per-rune: blows soaked since the last
// call eat the top rune first, overflow into the next, and each rune keeps its own
// running depletion, so refreshing one rune (RuneShields is read every tick)
// restores only that rune. With no runes up the estimate resets for the next cast.
func (t *Tracker) RuneShields(now int64, avgHit int) (shields []RuneShield, blows int) {
	if t == nil {
		return nil, 0
	}
	t.mu.Lock()
	defer t.mu.Unlock()

	// active runes in stacking order (a rune is up if any timer on You is unexpired)
	var active []struct {
		name string
		pool int
	}
	for _, r := range runeBook {
		for _, tm := range t.timers {
			if tm.Spell == r.name && tm.Target == "You" && tm.Expiry > now {
				active = append(active, struct {
					name string
					pool int
				}{r.name, r.pool})
				break
			}
		}
	}
	if len(active) == 0 { // stack gone → reset so the next rune cast starts clean
		t.runeAbsorbs, t.runeDrained, t.runeAttacker = 0, 0, ""
		t.runeAbsorbed = nil
		return nil, 0
	}
	if t.runeAbsorbed == nil {
		t.runeAbsorbed = map[string]int{}
	}
	// drop buckets for runes no longer active, so a future recast starts full
	live := make(map[string]bool, len(active))
	for _, a := range active {
		live[a.name] = true
	}
	for name := range t.runeAbsorbed {
		if !live[name] {
			delete(t.runeAbsorbed, name)
		}
	}
	// drain blows soaked since the last read through the stack, top-down
	if avgHit > 0 && t.runeAbsorbs > t.runeDrained {
		dmg := (t.runeAbsorbs - t.runeDrained) * avgHit
		for _, a := range active {
			if dmg <= 0 {
				break
			}
			room := a.pool - t.runeAbsorbed[a.name]
			if room <= 0 {
				continue
			}
			take := dmg
			if take > room {
				take = room
			}
			t.runeAbsorbed[a.name] += take
			dmg -= take
		}
		t.runeDrained = t.runeAbsorbs // overflow past the last rune is dropped
	}

	out := make([]RuneShield, len(active))
	for i, a := range active {
		out[i] = RuneShield{Name: a.name, Pool: a.pool, Remaining: a.pool - t.runeAbsorbed[a.name]}
	}
	return out, t.runeAbsorbs
}
