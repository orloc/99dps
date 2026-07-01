// Package cues is the audio-cue engine: given the live game state (a
// gamestate.Tracker snapshot), it decides what short phrases to speak this tick,
// dedups them so each event fires once, and phrases them naturally. It is pure
// logic — no rendering — so it lives apart from the tui presentation layer and is
// unit-tested on its own. The tui's model owns an *Engine and calls Announce on
// each tick; a small Speaker interface keeps the tts engine at arm's length.
package cues

import (
	"fmt"
	"strconv"
	"strings"

	"99dps/internal/eq/gamestate"
)

// Speaker is the minimal speech sink the engine needs — the tts engine satisfies
// it. SayUrgent jumps the queue for time-critical alerts; Flush drops queued
// (not-yet-spoken) normal cues when a mob dies so stale fades don't play on.
type Speaker interface {
	Say(text string)
	SayUrgent(text string)
	Flush()
}

// Engine holds the transient "what have we already announced" state across ticks
// (so each event cues once and re-arms when it clears) plus a rotating sequence
// for phrasing variety. The cue prefs are passed in per call (the model owns
// them, for settings + persistence), keeping the engine free of config state.
type Engine struct {
	// announced records the loudest urgency already spoken per spell\x00target, so
	// a buff cues once on entering "low" (gold) and again on "fading" (red),
	// re-arming when refreshed/expired.
	announced map[string]gamestate.Urgency

	// urgent combat-cue dedup (fire once per event, re-arm when it clears).
	charmAnnounced     bool
	resistSpoken       map[string]int64 // spell → last-spoken time (per-spell repeat gate)
	feignFailAnnounced bool
	cdReady            map[string]bool  // cooldown name → was-ready last tick
	cdHalf             map[string]bool  // cooldown name → was-past-halfway last tick
	spawnAnnounced     map[string]bool  // repop key → "spawn soon" heads-up already spoken
	spawnImminent      map[string]bool  // repop key → urgent "spawning now" alert already spoken
	chGapSpoken        map[string]int64 // CH-chain target → last "chain gap" alert time (repeat gate)
	lastSlainClears    int              // tracker.SlainClearCount() last seen → flush queued cues on a kill

	seq int // rotates phrasing for natural variety
}

// NewEngine builds a ready cue engine with its transition maps initialized.
func NewEngine() *Engine {
	return &Engine{
		announced:      map[string]gamestate.Urgency{},
		cdReady:        map[string]bool{},
		cdHalf:         map[string]bool{},
		spawnAnnounced: map[string]bool{},
		spawnImminent:  map[string]bool{},
		resistSpoken:   map[string]int64{},
		chGapSpoken:    map[string]int64{},
	}
}

// Announce speaks this tick's audio cues into sp, reading live state from tr and
// gating each cue on prefs. Urgent combat alerts (charm break, resist, failed
// feign) use SayUrgent — a snappier voice that jumps the playback queue — and
// each fires once per event. Buff/debuff fades are gentler, combined into one
// varied sentence so simultaneous fades don't overlap. No-op when sp or tr is nil.
func (e *Engine) Announce(sp Speaker, tr *gamestate.Tracker, prefs Prefs, now int64) {
	if sp == nil || tr == nil {
		return
	}
	// lazily ensure the transition-tracking maps exist (a zero-value Engine{}, or
	// a future field, won't have run NewEngine's init).
	if e.cdReady == nil {
		e.cdReady = map[string]bool{}
	}
	if e.cdHalf == nil {
		e.cdHalf = map[string]bool{}
	}
	if e.announced == nil {
		e.announced = map[string]gamestate.Urgency{}
	}
	if e.spawnAnnounced == nil {
		e.spawnAnnounced = map[string]bool{}
	}
	if e.spawnImminent == nil {
		e.spawnImminent = map[string]bool{}
	}
	if e.resistSpoken == nil {
		e.resistSpoken = map[string]int64{}
	}
	if e.chGapSpoken == nil {
		e.chGapSpoken = map[string]int64{}
	}

	// a mob died (a slain line or an inferred xp-only kill) → drop any queued fade
	// cues about its now-gone debuffs, so they don't spam after the kill.
	if n := tr.SlainClearCount(); n != e.lastSlainClears {
		e.lastSlainClears = n
		sp.Flush()
	}

	// charm break — scary and time-critical.
	if tr.CharmBroke(now) {
		if !e.charmAnnounced {
			e.charmAnnounced = true
			if prefs.Enabled(cueCharm, true) {
				sp.SayUrgent(charmBreakPhrase(prefs.Chatter, e.seq))
				e.seq++
			}
		}
	} else {
		e.charmAnnounced = false
	}

	// a failed feign death — you're still being attacked. urgent.
	if tr.FeignStatus(now) == gamestate.FeignFailed {
		if !e.feignFailAnnounced {
			e.feignFailAnnounced = true
			if prefs.Enabled(cueFeignFail, true) {
				sp.SayUrgent(feignFailPhrase(prefs.Chatter, e.seq))
				e.seq++
			}
		}
	} else {
		e.feignFailAnnounced = false
	}

	// a cast that didn't land — alert so you re-cast. Gate per spell on a repeat
	// window: a 2-spell macro grinding a high-resist mob alternates spells, so the
	// resist signal never goes quiet between them. The old single-slot dedup keyed
	// on the spell NAME, so each alternation looked new and re-fired every cast —
	// a per-spell last-spoken time caps it to once per spell per window instead.
	if spell, ok := tr.Resisted(now); ok {
		if now-e.resistSpoken[spell] > resistRepeatSec {
			e.resistSpoken[spell] = now
			if prefs.Enabled(cueResist, true) {
				sp.SayUrgent(resistPhrase(prefs.Chatter, spell, e.seq))
				e.seq++
			}
		}
	} else if len(e.resistSpoken) > 0 {
		e.resistSpoken = map[string]int64{} // resist signal fully cleared → re-arm every spell
	}

	// a Complete Heal chain is slipping — the tank is going unhealed. Fires on the
	// EARLY "gap forming" threshold (a CH is late) so it's immediate, not only once
	// the chain is fully broken. Urgent, and time-gated per target (a chronically-
	// gapping chain re-alerts every chGapRepeatSec, and concurrent tanks each alert).
	activeChains := tr.CHActiveChains(now)
	seenGap := make(map[string]bool, len(activeChains))
	for _, ch := range activeChains {
		// only alert on a SUSTAINED chain's slip — a brief transient run (a couple
		// emergency CHs) petering out isn't a chain to warn about.
		if !ch.GapForming || ch.Casts < chGapCueMinCasts {
			continue
		}
		seenGap[ch.Target] = true
		if now-e.chGapSpoken[ch.Target] > chGapRepeatSec {
			e.chGapSpoken[ch.Target] = now
			if prefs.Enabled(cueCHGap, true) {
				sp.SayUrgent(chGapPhrase(prefs.Chatter, ch.Target, ch.NextCaster, e.seq))
				e.seq++
			}
		}
	}
	for tgt := range e.chGapSpoken { // re-arm a target whose chain recovered or ended
		if !seenGap[tgt] {
			delete(e.chGapSpoken, tgt)
		}
	}

	// cooldowns recovering. Each skill's "ready" and "halfway" cues toggle
	// independently; long reuses (Mend) default on, short ones (Kick/Feign) default
	// off so they aren't chatter — but the user can flip any of them per skill.
	for _, cd := range tr.Cooldowns(now) {
		def := cd.Total >= longCooldownSec
		ready := cd.Remaining <= 0
		// cooldown cues are the gentle (non-critical) tier — Quiet suppresses the
		// spoken alert, but the transition maps still update so flipping back to a
		// chattier mode doesn't replay a backlog.
		if was, seen := e.cdReady[cd.Name]; seen && !was && ready && prefs.Chatter.speaksGentle() {
			if prefs.Enabled(cueCDReady(cd.Name), def) {
				sp.Say(cdReadyPhrase(prefs.Chatter, cd.Name))
				e.seq++
			}
		}
		e.cdReady[cd.Name] = ready // first sight just records; only false→true speaks

		// halfway recovered: fires once as remaining crosses below half the reuse.
		half := cd.Total > 0 && !ready && cd.Remaining <= cd.Total/2
		if was, seen := e.cdHalf[cd.Name]; seen && !was && half && prefs.Chatter.speaksGentle() {
			if prefs.Enabled(cueCDHalf(cd.Name), def) {
				sp.Say(halfCooldownPhrase(prefs.Chatter, cd.Name))
				e.seq++
			}
		}
		e.cdHalf[cd.Name] = half
	}

	// buff/debuff timers crossing into the gold (low) or red (fading) zones — the
	// SAME classifier as the visual flash, so what you hear matches what you see.
	// "Low" and "fading" get distinct verbiage; simultaneous subjects in a zone are
	// combined into one utterance.
	var low, fading []gamestate.Timer
	for _, d := range e.dueAnnouncements(tr.Active(now), prefs, now) {
		if d.fading {
			fading = append(fading, d.Timer)
		} else {
			low = append(low, d.Timer)
		}
	}
	// the gold "running low" cue is gentle (Quiet drops it); the red "fading" cue
	// is critical and always fires.
	if len(low) > 0 && prefs.Chatter.speaksGentle() {
		sp.Say(composeCue(prefs.Chatter, low, false, now, e.seq))
		e.seq++
	}
	if len(fading) > 0 {
		sp.Say(composeCue(prefs.Chatter, fading, true, now, e.seq))
		e.seq++
	}

	// tracked mobs about to repop — a two-stage alert, each firing once per spawn and
	// re-arming when the spawn pops/purges: a calm heads-up ~leadSec out, then an
	// urgent "spawning now" ~immSec out (jumps the queue — it's time-critical). The
	// lead times follow the Timing preset (Relaxed = later, Early = earlier).
	if prefs.Enabled(cueSpawnSoon, true) {
		leadSec, immSec := prefs.Timing.spawnWindows()
		var soon, imminent []string
		liveSpawn := map[string]bool{}
		curZone := tr.Zone()
		for _, r := range tr.Respawns(now) {
			// key on the stable kill time, not the expiry: an override edit rewrites
			// expiry in place (zone.setOverrideLocked), which would otherwise mint a
			// new key and re-announce an already-spoken spawn. A re-killed spawn gets
			// a fresh At, so it still re-arms.
			key := r.Mob + "\x00" + strconv.FormatInt(r.At, 10)
			liveSpawn[key] = true // track every zone's keys so re-arm bookkeeping stays correct
			// only CUE spawns in the zone you're actually in: the list still shows
			// other zones' repops (you can read those at your leisure), but an
			// interrupting audio alert for a mob you can't reach is just noise. A spawn
			// still pending when you zone back in cues then (curZone matches again).
			if r.Zone != curZone {
				continue
			}
			if r.Remaining > 0 && r.Remaining <= leadSec && !e.spawnAnnounced[key] {
				e.spawnAnnounced[key] = true
				soon = append(soon, r.Mob)
			}
			// imminent: a tight window straddling the pop (just before → just after) so
			// it reads as "now". The lower bound rejects a spawn first seen long after it
			// popped (a fresh start / zone-back into the keep-up window) — that's not news.
			if r.Remaining <= immSec && r.Remaining > -immSec && !e.spawnImminent[key] {
				e.spawnImminent[key] = true
				imminent = append(imminent, r.Mob)
			}
		}
		for k := range e.spawnAnnounced {
			if !liveSpawn[k] {
				delete(e.spawnAnnounced, k) // spawn popped / purged → re-arm for next kill
			}
		}
		for k := range e.spawnImminent {
			if !liveSpawn[k] {
				delete(e.spawnImminent, k)
			}
		}
		// the calm heads-up is gentle (Quiet drops it); the at-the-pop alert is
		// urgent and always fires.
		if len(soon) > 0 && prefs.Chatter.speaksGentle() {
			sp.Say(spawnPhrase(prefs.Chatter, soon, e.seq))
			e.seq++
		}
		if len(imminent) > 0 {
			sp.SayUrgent(spawnNowPhrase(prefs.Chatter, imminent, e.seq))
			e.seq++
		}
	}
}

// spawnLeadSec is how far out a repop's calm "about to spawn" heads-up fires.
const spawnLeadSec = 60

// spawnImminentSec sizes the tight window around the actual pop for the urgent
// "spawning now" alert (fires once as the timer crosses ~3s out, into "up").
const spawnImminentSec = 3

// chGapRepeatSec is the minimum gap before a still-broken chain re-alerts on the
// same target (so a chronically-gapping chain warns periodically, not every tick).
const chGapRepeatSec = 30

// chGapCueMinCasts requires a chain be SUSTAINED before its slips are worth an
// audio alert — filters transient emergency-heal bursts that peter out.
const chGapCueMinCasts = 8

// resistRepeatSec is the minimum gap before the same spell's resist re-announces.
// It de-spams a sustained resist macro (each spell at most once per window) while
// still re-alerting if resists keep happening over a long fight.
const resistRepeatSec = 15

// spawnPhrase announces mobs about to repop, varied per seq, collapsing to a
// count when several pop at once. Wording follows the Chatter preset. This is a
// gentle-tier cue (suppressed under Quiet upstream), so it has no terse form.
func spawnPhrase(ch Chatter, mobs []string, seq int) string {
	if len(mobs) > 3 {
		return fmt.Sprintf("%d spawns incoming.", len(mobs))
	}
	subj := joinList(mobs)
	one := len(mobs) == 1
	switch ch {
	case ChatterChatty:
		if one {
			tpl := []string{"Heads up — %s is about to spawn, get into position.", "Get ready, %s is about to pop.", "%s should be spawning any moment now."}
			return fmt.Sprintf(tpl[seq%len(tpl)], subj)
		}
		tpl := []string{"Heads up — %s are about to spawn, get into position.", "Get ready, %s are about to pop.", "%s should be spawning any moment now."}
		return fmt.Sprintf(tpl[seq%len(tpl)], subj)
	default:
		if one {
			tpl := []string{"%s is about to spawn.", "Heads up, %s is about to pop.", "%s spawns soon."}
			return fmt.Sprintf(tpl[seq%len(tpl)], subj)
		}
		tpl := []string{"%s are about to spawn.", "Heads up, %s are about to pop.", "%s spawn soon."}
		return fmt.Sprintf(tpl[seq%len(tpl)], subj)
	}
}

// spawnNowPhrase is the urgent at-the-pop alert (a few seconds out → now), varied
// per seq, collapsing to a count when several pop together. Wording follows the
// Chatter preset (terse in Quiet, fuller in Chatty).
func spawnNowPhrase(ch Chatter, mobs []string, seq int) string {
	if len(mobs) > 3 {
		return fmt.Sprintf("%d spawns now!", len(mobs))
	}
	subj := joinList(mobs)
	one := len(mobs) == 1
	switch ch {
	case ChatterQuiet:
		return subj + " up!"
	case ChatterChatty:
		if one {
			tpl := []string{"Here comes %s — get ready!", "%s is spawning right now, look alive!", "Incoming, %s is popping now!"}
			return fmt.Sprintf(tpl[seq%len(tpl)], subj)
		}
		tpl := []string{"Here they come — %s, get ready!", "%s are spawning right now, look alive!", "Incoming, %s are popping now!"}
		return fmt.Sprintf(tpl[seq%len(tpl)], subj)
	default:
		if one {
			tpl := []string{"%s is spawning now!", "%s, spawning now!", "Here comes %s!"}
			return fmt.Sprintf(tpl[seq%len(tpl)], subj)
		}
		tpl := []string{"%s are spawning now!", "%s, spawning now!", "Here they come — %s!"}
		return fmt.Sprintf(tpl[seq%len(tpl)], subj)
	}
}

// longCooldownSec gates which cooldowns get a "ready" cue — Mend (360s) and up,
// excluding short skill reuses like feign (11s) or monk specials (5s).
const longCooldownSec = 60

// cueDue is a timer that escalated to a louder urgency this tick, tagged with
// whether it entered the red "fading" zone (vs the gold "low" zone) so the caller
// can pick the right verbiage.
type cueDue struct {
	gamestate.Timer
	fading bool
}

// dueAnnouncements returns the timers that just escalated this tick and updates
// the per-timer announced urgency. It drives off gamestate.TimerUrgency — the SAME
// classifier as the visual countdown color — so a buff cues once on entering the
// gold "low" zone and again on the red "fading" zone, matching the flash. A debuff
// on a mob (DoT) announces only ONCE per mob (its first escalation), so a re-dot
// doesn't nag. Each timer's type (mez / pacify / debuff / buff) is gated on its cue
// toggle. Speaking is left to the caller so this stays testable. Charm is skipped
// (it breaks before its cap, so a countdown would cry wolf); estimated incoming
// "~" ON-YOU debuffs are skipped too (their duration is a deliberate over-estimate,
// and a debuff ending on you is good news).
func (e *Engine) dueAnnouncements(active []gamestate.Timer, prefs Prefs, now int64) []cueDue {
	// Collapse timers that share a (spell, target) key to a single subject: two
	// same-named mobs (e.g. pacify on each of several "a sand giant") produce
	// duplicate keys, and speech can't tell them apart anyway. The most-urgent
	// (soonest-expiring) instance represents the key. This is also what prevents
	// per-tick repeat spam: with several non-detrimental same-key instances
	// (pacify), the old per-instance loop reset the announced urgency to the
	// LEAST-urgent instance at the end of each tick, so the most-urgent one
	// re-fired its "fading" cue every tick. One representative per key fixes that.
	rep := make(map[string]gamestate.Timer, len(active))
	order := make([]string, 0, len(active))
	for _, tm := range active {
		if tm.Charm || tm.Estimated {
			continue
		}
		k := tm.Spell + "\x00" + tm.Target
		if cur, seen := rep[k]; !seen {
			rep[k] = tm
			order = append(order, k)
		} else if tm.Expiry < cur.Expiry {
			rep[k] = tm // soonest-expiring wins
		}
	}

	var due []cueDue
	for _, k := range order {
		tm := rep[k]
		u := gamestate.TimerUrgency(tm.Expiry-now, tm.Expiry-tm.Start)
		prev := e.announced[k]
		switch {
		case u > prev:
			// a debuff on a mob announces only its first escalation (once per mob); a
			// buff announces each new zone it enters (low, then fading).
			if !(tm.Detrimental && prev > gamestate.Fresh) && prefs.Enabled(cueIDForTimer(tm), true) {
				due = append(due, cueDue{Timer: tm, fading: u == gamestate.Expiring})
			}
			e.announced[k] = u
		case u < prev && !tm.Detrimental:
			e.announced[k] = u // a buff refreshed back to a calmer zone → re-arm
		}
	}
	for k := range e.announced {
		if _, live := rep[k]; !live {
			delete(e.announced, k) // timer gone (expired / mob died) → re-arm
		}
	}
	return due
}

// composeCue turns the timers that just escalated into one terse, natural, varied
// utterance — simultaneous subjects are combined into a single sentence rather
// than overlapping cues. fading picks the register: the urgent, timeless "fading /
// about to drop" (red zone) vs the calmer "running low" (gold zone), which also
// speaks how much time is left. seq rotates the phrasing for variety; now lets the
// low cue compute remaining time. Self-buffs read by name; effects on a mob read
// as "<spell> on <mob>".
func composeCue(ch Chatter, due []gamestate.Timer, fading bool, now int64, seq int) string {
	if len(due) == 0 {
		return ""
	}
	if fading {
		return fadingPhrase(ch, due, seq)
	}
	return lowPhrase(ch, due, now, seq)
}

// subjectName names a timer for a cue: a self/ally buff by spell, an effect on a
// mob as "<spell> on <mob>".
func subjectName(tm gamestate.Timer) string {
	if tm.Target == "" || tm.Target == "You" {
		return tm.Spell
	}
	return tm.Spell + " on " + tm.Target
}

// fadingPhrase is the urgent red-zone cue: no time (it's about to drop), varied
// per seq, with a count summary when more than three drop at once. Wording
// follows the Chatter preset (terse in Quiet, fuller in Chatty).
func fadingPhrase(ch Chatter, due []gamestate.Timer, seq int) string {
	names := make([]string, len(due))
	for i := range due {
		names[i] = subjectName(due[i])
	}
	if len(names) > 3 {
		return fmt.Sprintf("%d effects are fading.", len(names))
	}
	subj := joinList(names)
	one := len(names) == 1
	switch ch {
	case ChatterQuiet:
		return subj + " fading."
	case ChatterChatty:
		if one {
			tpl := []string{"Quick — %s is fading, refresh it!", "%s is about to drop, recast now.", "Watch out, %s is wearing off."}
			return fmt.Sprintf(tpl[seq%len(tpl)], subj)
		}
		tpl := []string{"Quick — %s are fading, refresh them!", "%s are about to drop, recast now.", "Watch out, %s are wearing off."}
		return fmt.Sprintf(tpl[seq%len(tpl)], subj)
	default:
		if one {
			tpl := []string{"%s is fading.", "%s is wearing off.", "%s is about to drop.", "Quick, %s is fading."}
			return fmt.Sprintf(tpl[seq%len(tpl)], subj)
		}
		tpl := []string{"%s are fading.", "%s are wearing off.", "%s are about to drop.", "Heads up, %s are fading."}
		return fmt.Sprintf(tpl[seq%len(tpl)], subj)
	}
}

// lowPhrase is the calmer gold-zone cue: it speaks the time left, varied per seq.
// One subject reads "<name> … <time> left"; a few combine name+time into a list;
// many collapse to a count (per-item times would be a mouthful).
func lowPhrase(ch Chatter, due []gamestate.Timer, now int64, seq int) string {
	if len(due) == 1 {
		name, left := subjectName(due[0]), spokenDuration(due[0].Expiry-now)
		switch ch {
		case ChatterChatty:
			tpl := []string{
				"Heads up, %s is running low — about %s left, think about refreshing it.",
				"%s is getting low, roughly %s to go before it drops.",
				"You'll want to recast %s soon, only %s left.",
			}
			return fmt.Sprintf(tpl[seq%len(tpl)], name, left)
		default:
			tpl := []string{
				"%s is running low, %s left.",
				"%s, %s remaining.",
				"Heads up, %s has about %s left.",
				"%s is getting low — %s to go.",
			}
			return fmt.Sprintf(tpl[seq%len(tpl)], name, left)
		}
	}
	if len(due) > 3 {
		return fmt.Sprintf("%d effects are running low.", len(due))
	}
	items := make([]string, len(due))
	for i := range due {
		items[i] = subjectName(due[i]) + ", " + spokenDuration(due[i].Expiry-now)
	}
	switch ch {
	case ChatterChatty:
		tpl := []string{"Heads up, these are running low: %s.", "Getting low and worth a refresh — %s.", "You'll want to recast soon: %s."}
		return fmt.Sprintf(tpl[seq%len(tpl)], joinList(items))
	default:
		tpl := []string{"Running low: %s.", "Getting low — %s.", "%s are running low."}
		return fmt.Sprintf(tpl[seq%len(tpl)], joinList(items))
	}
}

// spokenDuration renders a remaining time for speech precisely, flooring so it
// never claims MORE time than is left (a "time left" cue that overstates is worse
// than one that's exact): "5 minutes", "1 minute 40 seconds", "45 seconds".
func spokenDuration(sec int64) string {
	if sec < 0 {
		sec = 0
	}
	m, s := sec/60, sec%60
	switch {
	case m == 0:
		return plural(s, "second")
	case s == 0:
		return plural(m, "minute")
	default:
		return plural(m, "minute") + " " + plural(s, "second")
	}
}

// plural renders a count with its unit, pluralized: "1 minute", "2 minutes".
func plural(n int64, unit string) string {
	if n == 1 {
		return "1 " + unit
	}
	return fmt.Sprintf("%d %ss", n, unit)
}

// joinList renders names with natural list grammar: "X" / "X and Y" /
// "X, Y, and Z".
func joinList(s []string) string {
	switch len(s) {
	case 0:
		return ""
	case 1:
		return s[0]
	case 2:
		return s[0] + " and " + s[1]
	default:
		return strings.Join(s[:len(s)-1], ", ") + ", and " + s[len(s)-1]
	}
}

// cdReadyPhrase announces a cooldown has finished recovering, worded per Chatter.
// Gentle-tier (suppressed under Quiet upstream), so it carries no terse form.
func cdReadyPhrase(ch Chatter, skill string) string {
	if ch == ChatterChatty {
		return skill + " is ready to use again."
	}
	return skill + " ready."
}

// halfCooldownPhrase announces a cooldown is halfway recovered (a heads-up to
// start positioning for the next use), worded per Chatter. Gentle-tier
// (suppressed under Quiet upstream), so it carries no terse form.
func halfCooldownPhrase(ch Chatter, skill string) string {
	if ch == ChatterChatty {
		return skill + " is about halfway recovered."
	}
	return skill + " halfway back."
}

// cueIDForTimer maps a fading timer to its cue category ID, so each type
// (mez / pacify / debuff / buff) can be toggled independently. Charm and the
// estimated incoming "ON YOU" timers are filtered out before this is reached.
func cueIDForTimer(tm gamestate.Timer) string {
	switch {
	case tm.Mez:
		return cueMez
	case tm.Pacify:
		return cuePacify
	case tm.Detrimental:
		return cueDebuffFade
	default:
		return cueBuffFade
	}
}

// charmBreakPhrase is an urgent, varied alert that the player's charm broke.
func charmBreakPhrase(ch Chatter, seq int) string {
	switch ch {
	case ChatterQuiet:
		return "Charm broke!"
	case ChatterChatty:
		phrases := []string{"Your charm just broke — watch out, it's loose!", "Charm broke, get control back fast!", "Careful, your charmed pet broke free!"}
		return phrases[seq%len(phrases)]
	default:
		phrases := []string{"Charm broke!", "Your charm broke — careful!", "Charm just broke!"}
		return phrases[seq%len(phrases)]
	}
}

// feignFailPhrase is an urgent, varied alert that a feign death failed.
func feignFailPhrase(ch Chatter, seq int) string {
	switch ch {
	case ChatterQuiet:
		return "Feign failed!"
	case ChatterChatty:
		phrases := []string{"Your feign death failed — get up and move!", "Feign didn't take, you're still aggro!", "Feign Death failed, run!"}
		return phrases[seq%len(phrases)]
	default:
		phrases := []string{"Feign failed!", "Your feign failed — get up!", "Feign Death failed!"}
		return phrases[seq%len(phrases)]
	}
}

// chGapPhrase is an urgent, varied alert that a CH chain broke. It names the tank
// (so concurrent chains are distinguishable) and, when known, the cleric who's up.
func chGapPhrase(ch Chatter, target, next string, seq int) string {
	switch ch {
	case ChatterQuiet:
		return "Chain gap!"
	case ChatterChatty:
		if next != "" {
			phrases := []string{
				fmt.Sprintf("Chain gap on %s — %s is up!", target, next),
				fmt.Sprintf("The heal chain broke on %s, %s cast now!", target, next),
				fmt.Sprintf("Gap on %s — %s, you're up!", target, next),
			}
			return phrases[seq%len(phrases)]
		}
		phrases := []string{
			fmt.Sprintf("Chain gap on %s!", target),
			fmt.Sprintf("The heal chain broke on %s!", target),
		}
		return phrases[seq%len(phrases)]
	default:
		if next != "" {
			return fmt.Sprintf("Chain gap on %s, %s up!", target, next)
		}
		return fmt.Sprintf("Chain gap on %s!", target)
	}
}

// resistPhrase is an urgent, varied alert that a cast was resisted.
func resistPhrase(ch Chatter, spell string, seq int) string {
	switch ch {
	case ChatterQuiet:
		return spell + " resisted!"
	case ChatterChatty:
		phrases := []string{"Heads up — your " + spell + " was resisted, recast it.", spell + " got resisted, try again.", "Resisted: " + spell + " — land it again."}
		return phrases[seq%len(phrases)]
	default:
		switch seq % 3 {
		case 0:
			return spell + " resisted!"
		case 1:
			return spell + " was resisted."
		default:
			return "Resisted: " + spell + "."
		}
	}
}
