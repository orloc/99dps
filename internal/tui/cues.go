package tui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"99dps/internal/gamestate"
)

// announceCues speaks this tick's audio cues. Urgent combat alerts (charm break,
// resist) use SayUrgent — a snappier voice that jumps the playback queue — and
// each fires once per event. Buff/debuff fades are gentler, combined into one
// varied sentence so simultaneous fades don't overlap. No-op when audio is off.
func (m *Model) announceCues() { m.announceCuesAt(time.Now().Unix()) }

// announceCuesAt is the testable core (now is injected). Urgent alerts use
// SayUrgent (snappier, jump the queue); informational ones use Say.
func (m *Model) announceCuesAt(now int64) {
	if !m.ttsOn || m.tracker == nil || m.speaker == nil {
		return
	}
	// lazily ensure the transition-tracking maps exist (a directly-constructed
	// Model in tests, or a future field, won't have run New's init).
	if m.cdReady == nil {
		m.cdReady = map[string]bool{}
	}
	if m.cdHalf == nil {
		m.cdHalf = map[string]bool{}
	}
	if m.announced == nil {
		m.announced = map[string]gamestate.Urgency{}
	}
	if m.spawnAnnounced == nil {
		m.spawnAnnounced = map[string]bool{}
	}

	// a mob died (a slain line or an inferred xp-only kill) → drop any queued fade
	// cues about its now-gone debuffs, so they don't spam after the kill.
	if n := m.tracker.SlainClearCount(); n != m.lastSlainClears {
		m.lastSlainClears = n
		m.speaker.Flush()
	}

	// charm break — scary and time-critical.
	if m.tracker.CharmBroke(now) {
		if !m.charmAnnounced {
			m.charmAnnounced = true
			if m.cues.enabled(cueCharm, true) {
				m.speaker.SayUrgent(charmBreakPhrase(m.cueSeq))
				m.cueSeq++
			}
		}
	} else {
		m.charmAnnounced = false
	}

	// a failed feign death — you're still being attacked. urgent.
	if m.tracker.FeignStatus(now) == gamestate.FeignFailed {
		if !m.feignFailAnnounced {
			m.feignFailAnnounced = true
			if m.cues.enabled(cueFeignFail, true) {
				m.speaker.SayUrgent(feignFailPhrase(m.cueSeq))
				m.cueSeq++
			}
		}
	} else {
		m.feignFailAnnounced = false
	}

	// a cast that didn't land — alert so you re-cast.
	if spell, ok := m.tracker.Resisted(now); ok {
		if spell != m.resistAnnounced {
			m.resistAnnounced = spell
			if m.cues.enabled(cueResist, true) {
				m.speaker.SayUrgent(resistPhrase(spell, m.cueSeq))
				m.cueSeq++
			}
		}
	} else {
		m.resistAnnounced = ""
	}

	// cooldowns recovering. Each skill's "ready" and "halfway" cues toggle
	// independently; long reuses (Mend) default on, short ones (Kick/Feign) default
	// off so they aren't chatter — but the user can flip any of them per skill.
	for _, cd := range m.tracker.Cooldowns(now) {
		def := cd.Total >= longCooldownSec
		ready := cd.Remaining <= 0
		if was, seen := m.cdReady[cd.Name]; seen && !was && ready {
			if m.cues.enabled(cueCDReady(cd.Name), def) {
				m.speaker.Say(cd.Name + " ready.")
				m.cueSeq++
			}
		}
		m.cdReady[cd.Name] = ready // first sight just records; only false→true speaks

		// halfway recovered: fires once as remaining crosses below half the reuse.
		half := cd.Total > 0 && !ready && cd.Remaining <= cd.Total/2
		if was, seen := m.cdHalf[cd.Name]; seen && !was && half {
			if m.cues.enabled(cueCDHalf(cd.Name), def) {
				m.speaker.Say(halfCooldownPhrase(cd.Name))
				m.cueSeq++
			}
		}
		m.cdHalf[cd.Name] = half
	}

	// buff/debuff timers crossing into the gold (low) or red (fading) zones — the
	// SAME classifier as the visual flash, so what you hear matches what you see.
	// "Low" and "fading" get distinct verbiage; simultaneous subjects in a zone are
	// combined into one utterance.
	var low, fading []gamestate.Timer
	for _, d := range m.dueAnnouncements(m.tracker.Active(now), now) {
		if d.fading {
			fading = append(fading, d.Timer)
		} else {
			low = append(low, d.Timer)
		}
	}
	if len(low) > 0 {
		m.speaker.Say(composeCue(low, false, now, m.cueSeq))
		m.cueSeq++
	}
	if len(fading) > 0 {
		m.speaker.Say(composeCue(fading, true, now, m.cueSeq))
		m.cueSeq++
	}

	// tracked mobs about to repop (~spawnLeadSec out) — fires once per spawn, keyed
	// by its stable expiry (now+Remaining), re-arming when the spawn pops/purges.
	if m.cues.enabled(cueSpawnSoon, true) {
		var soon []string
		liveSpawn := map[string]bool{}
		for _, r := range m.tracker.Respawns(now) {
			key := r.Mob + "\x00" + strconv.FormatInt(now+r.Remaining, 10)
			liveSpawn[key] = true
			if r.Remaining > 0 && r.Remaining <= spawnLeadSec && !m.spawnAnnounced[key] {
				m.spawnAnnounced[key] = true
				soon = append(soon, r.Mob)
			}
		}
		for k := range m.spawnAnnounced {
			if !liveSpawn[k] {
				delete(m.spawnAnnounced, k) // spawn popped / purged → re-arm for next kill
			}
		}
		if len(soon) > 0 {
			m.speaker.Say(spawnPhrase(soon, m.cueSeq))
			m.cueSeq++
		}
	}
}

// spawnLeadSec is how far out a repop's "about to spawn" cue fires.
const spawnLeadSec = 30

// spawnPhrase announces mobs about to repop, varied per seq, collapsing to a
// count when several pop at once.
func spawnPhrase(mobs []string, seq int) string {
	if len(mobs) > 3 {
		return fmt.Sprintf("%d spawns incoming.", len(mobs))
	}
	subj := joinList(mobs)
	if len(mobs) == 1 {
		tpl := []string{"%s is about to spawn.", "Heads up, %s is about to pop.", "%s spawns soon."}
		return fmt.Sprintf(tpl[seq%len(tpl)], subj)
	}
	tpl := []string{"%s are about to spawn.", "Heads up, %s are about to pop.", "%s spawn soon."}
	return fmt.Sprintf(tpl[seq%len(tpl)], subj)
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
func (m *Model) dueAnnouncements(active []gamestate.Timer, now int64) []cueDue {
	var due []cueDue
	live := make(map[string]bool, len(active))
	for _, tm := range active {
		if tm.Charm || tm.Estimated {
			continue
		}
		k := tm.Spell + "\x00" + tm.Target
		live[k] = true
		u := gamestate.TimerUrgency(tm.Expiry-now, tm.Expiry-tm.Start)
		prev := m.announced[k]
		switch {
		case u > prev:
			// a debuff on a mob announces only its first escalation (once per mob); a
			// buff announces each new zone it enters (low, then fading).
			if !(tm.Detrimental && prev > gamestate.Fresh) && m.cues.enabled(cueIDForTimer(tm), true) {
				due = append(due, cueDue{Timer: tm, fading: u == gamestate.Expiring})
			}
			m.announced[k] = u
		case u < prev && !tm.Detrimental:
			m.announced[k] = u // a buff refreshed back to a calmer zone → re-arm
		}
	}
	for k := range m.announced {
		if !live[k] {
			delete(m.announced, k) // timer gone (expired / mob died) → re-arm
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
func composeCue(due []gamestate.Timer, fading bool, now int64, seq int) string {
	if len(due) == 0 {
		return ""
	}
	if fading {
		return fadingPhrase(due, seq)
	}
	return lowPhrase(due, now, seq)
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
// per seq, with a count summary when more than three drop at once.
func fadingPhrase(due []gamestate.Timer, seq int) string {
	names := make([]string, len(due))
	for i := range due {
		names[i] = subjectName(due[i])
	}
	if len(names) > 3 {
		return fmt.Sprintf("%d effects are fading.", len(names))
	}
	subj := joinList(names)
	if len(names) == 1 {
		tpl := []string{"%s is fading.", "%s is wearing off.", "%s is about to drop.", "Quick, %s is fading."}
		return fmt.Sprintf(tpl[seq%len(tpl)], subj)
	}
	tpl := []string{"%s are fading.", "%s are wearing off.", "%s are about to drop.", "Heads up, %s are fading."}
	return fmt.Sprintf(tpl[seq%len(tpl)], subj)
}

// lowPhrase is the calmer gold-zone cue: it speaks the time left, varied per seq.
// One subject reads "<name> … <time> left"; a few combine name+time into a list;
// many collapse to a count (per-item times would be a mouthful).
func lowPhrase(due []gamestate.Timer, now int64, seq int) string {
	if len(due) == 1 {
		name, left := subjectName(due[0]), spokenDuration(due[0].Expiry-now)
		tpl := []string{
			"%s is running low, %s left.",
			"%s, %s remaining.",
			"Heads up, %s has about %s left.",
			"%s is getting low — %s to go.",
		}
		return fmt.Sprintf(tpl[seq%len(tpl)], name, left)
	}
	if len(due) > 3 {
		return fmt.Sprintf("%d effects are running low.", len(due))
	}
	items := make([]string, len(due))
	for i := range due {
		items[i] = subjectName(due[i]) + ", " + spokenDuration(due[i].Expiry-now)
	}
	tpl := []string{"Running low: %s.", "Getting low — %s.", "%s are running low."}
	return fmt.Sprintf(tpl[seq%len(tpl)], joinList(items))
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

// halfCooldownPhrase announces a cooldown is halfway recovered (a heads-up to
// start positioning for the next use).
func halfCooldownPhrase(skill string) string { return skill + " halfway back." }

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
func charmBreakPhrase(seq int) string {
	phrases := []string{"Charm broke!", "Your charm broke — careful!", "Charm just broke!"}
	return phrases[seq%len(phrases)]
}

// feignFailPhrase is an urgent, varied alert that a feign death failed.
func feignFailPhrase(seq int) string {
	phrases := []string{"Feign failed!", "Your feign failed — get up!", "Feign Death failed!"}
	return phrases[seq%len(phrases)]
}

// resistPhrase is an urgent, varied alert that a cast was resisted.
func resistPhrase(spell string, seq int) string {
	switch seq % 3 {
	case 0:
		return spell + " resisted!"
	case 1:
		return spell + " was resisted."
	default:
		return "Resisted: " + spell + "."
	}
}
