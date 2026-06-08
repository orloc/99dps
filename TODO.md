# TODO / what's missing

Status of the class-aware skills + cooldown work, and the broader backlog. The
class panel, Mend/Feign cooldowns, feign banners, and bind-wound indicator are
done; most of what's left is blocked on capturing exact log messages.

## Cooldowns — blocked on log samples
Each needs its **exact activation message** + **reuse seconds** (the message text
is client-specific and isn't in the current all-caster logs). Add to
`spell.cooldownRegistry` (message-matched) or via a trigger like `FeignAttempt`.

- [ ] **Disciplines** (War/Monk/Rogue) — Defensive, Evasive, Aggressive, etc. Activation emotes + reuse unknown.
- [ ] **Harm Touch** (Shadow Knight) — long reuse (~72m), distinct message. High value.
- [ ] **Lay on Hands** (Paladin) — long reuse. High value.
- [ ] **Hide / Sneak** (Rogue) — messages + any reuse.
- [ ] Hybrid panel doesn't render cooldowns yet (only the melee panel does). Needed once HT/LoH land — see `updatePanel` CatHybrid branch.

## Verify exact strings (currently best-effort / paraphrased)
- [ ] **Mend** fail/worsen variants in `cooldownRegistry` — success match ("mend your wounds") is solid; the fail/"worsen" strings are paraphrased and unconfirmed (rarely hit once skill ≥100).
- [ ] **Bind wound** completion — using "bandaging is complete" (paraphrased). Confirm the verbatim line; start "You begin to bandage" is confirmed.
- [ ] **Feign Death reuse = 11s** — measured from a real spam sequence (3× consecutive 11s gaps, fails included), but log timestamps are 1s-resolution so the true value may be ~10–11s. Tune `feignReuseSec` if it reads "ready" a beat early.
- [ ] **Feign macro phrase** = "looks dead" (`parser.feignMacroPhrase`) — specific to the current custom macro; update if it changes.

## Known limitations (not fixable from logs)
- Specific damage-skill names aren't recoverable: EQ logs special attacks with a generic verb. Every kick variant → "kick", every monk special strike (Eagle Strike / Tiger Claw / Dragon Punch) → "strike", h2h → "punch", weapon → "crush". `displaySkillName` only best-guesses a 30+ monk's kick as "Flying Kick"; "strike" can't be disambiguated.
- Spell damage has no caster in the log → no per-spell DPS leaderboard (only the unattributed magic total).
- Two same-named mobs are textually identical → can't be told apart (affects debuff/mez attribution).

## Class detection
- [ ] `common.titleToClass` is a conservative subset (base names + well-known + log-validated titles). Mid-level titles for some classes are missing → those default to CatCaster (spell-timer panel) until a known title/base name appears. Extend one line at a time as misdetections show up.

## Zone / repop tracking (done — possible refinements)
- [x] Zone detection + per-kill repop timers (zone defaults from `common/zonetimers.go`); zone/level/class in the bottom bar.
- [ ] Current zone is unknown until the next zone-in (no log line gives it at startup) — could infer from other signals if any exist.
- [ ] Repop times are zone *defaults*; named/PH/exception mobs differ (see the multi-timer zones in `docs/zone-spawn-timers.md`). Per-mob overrides would need a mob→timer table.
- [ ] Only the player's own killing blow ("You have slain X!") starts a timer; group kills where someone else lands the blow aren't tracked.
- [ ] Wiki had no data for: Kelethin, Dagnor's Cauldron, Emerald Jungle, Veeshan's Peak, Western Wastes (not in the map).
- [ ] Zone-name matching is normalized (lowercase / strip "the" / period); some EQ zone-in long names may not match the wiki keys — report misses.

## Not yet built (earlier ideas / offers)
- [ ] **Audio cue on failed feign** (TTS "feign failed" via the `a`-toggle speaker) — offered, not wired.
- [ ] Optional TTS "Mend ready" / cooldown-ready cues.
- [ ] Resist tracking (per-target resist% for debuffs/charm).
- [ ] Paste-able parse summary line (chat-ready, copy to file/clipboard).
- [ ] Mez timers (best-effort per mob name; same-name ambiguity caveat).
- [ ] Session persistence / export (JSON/CSV); history view.
- [ ] XP/hr + time-to-level estimate.
- [ ] Config file for thresholds/colors (TTS low-buff cutoff, etc.).
