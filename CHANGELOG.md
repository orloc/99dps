# Changelog

## v0.2.1

First public release since v0.1.0. New live-state tracking (incoming debuffs,
pet/spell damage attribution, melee skills & cooldowns), smarter session
segmentation, and a batch of zone/respawn fixes.

### Added
- **Incoming debuffs ("ON YOU")** — hostile effects landing on you (Slowed,
  Rooted, Snared, Crippled, Malo'd, Tashed, Feared, Blinded, Diseased, Poisoned)
  now show as their own red timer section. Durations are estimated ceilings
  (marked `~`) cleared early on the real wear-off, since the log names no caster.
- **Pet & spell damage folded into your row** — the Damage meter now rolls each
  pet's (and your own spell) damage into the owner's total, so DPS / share /
  ranking are inclusive of everything you put out. Pet and `↳ spells` still show
  as dim breakdown children.
- **Melee toolkit** — a self-buffs column for melee, a left-to-right skill
  cooldown "charge box", monk kick/hand-strike cooldowns named by level, monk
  specials sharing one reuse timer, and monk White Lotus armor clickies.
- **`-logfile <path>`** debug flag — replay/follow one specific eqlog from the
  start (e.g. a friend's captured log) without touching your saved log dir.
- Your own buffs ("You") are pinned to the top of the buff list.
- Specials/Avoidance card titled **"Offense · Defense"**; selected session now
  highlights both of its rows.

### Changed
- **Session segmentation is now kill-driven, not clock-driven** — a single pull
  stays one session through root/med lulls while the mob is still alive, and a
  camp hunt stays one session across medding lulls (re-engage rule). Prevents one
  fight being split into many.
- **kills/hr is a rolling last-hour rate** — idle time decays it toward zero
  instead of leaving a stale lifetime average.
- Timer urgency cutoffs are now **capped in absolute time**, so a multi-hour
  self-buff no longer reads orange/red with an hour left.

### Fixed
- **Zone respawn timers in Permafrost Caverns, The Wakening Lands, and Everfrost**
  — these zone keys had been transcribed as the game's internal short names
  rather than the display names in the "You have entered" line, so kills there
  produced **no repop timers at all**. A regression test now pins the display-name
  forms.
- **Charmed-pet kills** are now credited to you — a charmed pet keeps its
  mob-style name, which the killer-is-mob heuristic previously dropped, costing
  the kill its repop timer.
- A slow clicky's cast is no longer dropped before its landing emote.
- The monk "Disciple" title (levels 51–54) now maps to Monk.
- Dismissing your buff list (clicking the "You" group header) no longer clears
  your incoming ON-YOU debuff timers.
- A hostile detrimental proc can no longer spawn a duplicate ON-YOU timer (its
  cast_on_you emote is no longer mis-indexed as a beneficial self-clicky).

### Windows
Pure-Go single `.exe`, no runtime needed. Build with `make dist-windows` for a
friend-ready zip (exe + plain-language readme). See `docs/windows-release.md`.
SmartScreen will warn on the unsigned binary — "More info → Run anyway" (one
time). Recipient must enable in-game logging with `/log on`.
