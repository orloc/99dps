# Changelog

## v0.3.0

A big release: realistic neural voice cues, a tabbed UI, a dedicated Sessions
screen, a configurable meter layout, and a batch of tracking fixes.

### Added
- **Neural voice audio cues (Kokoro)** — replaces the robotic espeak/SAPI voice
  with a realistic, fully-offline neural voice. It downloads once on first run
  (~120 MB) into a local cache, then needs no network. A **first-run setup
  screen** lets you enable cues and pick a voice (11 described English voices) or
  skip; `-tts-setup` verifies the whole path from the command line.
- **Tabbed UI** — a clickable tab bar: **Meter | Sessions | Settings** (click, or
  `tab`/`shift+tab`, or `1`/`2`/`3`).
- **Sessions tab** — a scrollable per-session stats table (#, fight, duration,
  total, DPS, kills, top dealer) with a sticky header, beside a DPS breakdown of
  the highlighted fight.
- **Settings tab** — toggle audio cues + pick a voice; and a **Meter boxes**
  section where the **Damage meter** and **Offense · Defense** each cycle
  **Full / Compact / Off**. Compact slims the columns *and* shrinks the top row so
  the bottom panels grow; Off hides the box and the layout reflows. Choices
  persist across launches.
- **More audio cues**, with natural varied phrasing: charm break and resist
  (urgent), **failed feign death** (urgent), and **long cooldown ready** (gentle,
  e.g. Mend). Simultaneous fades are combined into one sentence, cues no longer
  talk over each other (serialized playback), and the warning lead scales with a
  buff's length (≈3 min for a ~100-min buff instead of a flat 15s).

### Changed
- The Meter is now **full-width** — the Sessions list moved to its own tab.

### Fixed
- **Zone respawn timers** for zones whose table key didn't match the in-game
  name, so they produced no repop timers: **Permafrost Caverns, The Wakening
  Lands, Everfrost, Kael Drakkel, Western Wastes, Freeport (East/West/North)**.
- **kills/hr** no longer counts **quest turn-ins** as kills (e.g. the Chardok
  Green Goblin Skin grind was inflating the count massively).
- **Charmed-pet kills** are now credited to you (so they get repop timers).

### Windows
Still a single `.exe`; the neural voice downloads on first run, so the shipped
zip stays small. SmartScreen warns on the unsigned binary — "More info → Run
anyway" (one time). Enable in-game logging with `/log on`.

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
