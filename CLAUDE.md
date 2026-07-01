# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

A terminal DPS meter for classic EverQuest (P99). It tails the active EQ log file, parses combat lines as they appear, and renders live per-dealer damage, accuracy/avoidance, crits, specials, spell estimates, and a damage-bar graph in a **Bubble Tea + Lipgloss** TUI (truecolor, themed). The legacy `gocui` UI has been removed.

Go 1.25. Tests live next to the code; run `go test ./...`.

**Layout** (standard Go): `cmd/99dps/` holds `package main` (`main.go` + `run.go` wiring); all library packages live under `internal/` so they can't be imported externally. The 15 packages, grouped by role (the grouping is **directory-only** — every package stays independent; subdirs imply no Go coupling):

**EQ domain model** — `internal/eq/<pkg>` (the EverQuest "vocabulary + live state"; kept as separate leaf/low-dep packages on purpose so the lower layers can share them without pulling in the big tracker):
- **`eq/combat`** — combat DTOs (pure value types, which is what makes session snapshots a trivial deep copy). Zero deps.
- **`eq/eqclass`** — class/category taxonomy (title→class, disciplines, per-class profile). Zero deps.
- **`eq/mobstats`** — app-wide rolling index of how hard each NPC hits in melee (feeds the rune-absorb estimate); fed by the parse goroutine, read concurrently. Zero deps.
- **`eq/gamestate`** — the spell `Book` + live `Tracker` (timers, zones/repops, cooldowns, pet/group, incoming debuffs, …); **owns the zone-respawn data**; depends on `eq/eqclass`.

**Everything else** — `internal/<pkg>`:
- **`parser`** — the line dispatcher; depends on the `Sink` and `SpellObserver` interfaces (`eq/combat` + `eq/eqclass` only — **not** `session`/`eq/gamestate`).
- **`session`** — `SessionManager` + `CombatSession` (segmentation, dealer attribution, the RWMutex snapshot pattern); depends on `eq/combat` + `eq/mobstats`.
- **`loader`** — log-dir resolution, EQ detection, file following/tailing.
- **`cues`** — the audio-cue engine: decides **what** to announce from live game state (spawn/fade/low-buff/charm-break/feign-fail/resist); depends on `eq/eqclass` + `eq/gamestate`.
- **`tts`** — the speech **backend** behind the `cues.Speaker` seam: an `Engine` interface implemented by a Kokoro neural-TTS engine (subprocess) with on-demand asset download and platform playback. Zero internal deps. (`cues` + `tts` together replace what used to be one "tts" cue engine.)
- **`braille`** — pure-Go Unicode-braille drawing surface (used by the TUI zone map). No deps.
- **`eqmap`** — loads/caches EQ zone-map vector files (MQ2/Brewall format); fetches over the network once per zone. No internal deps.
- **`inventory`** — reads the Titanium `/outputfile inventory` dump. No deps.
- **`prices`** — client for the pigparse P99 tunnel-price API (on-demand HTTP). No internal deps.
- **`spellwiki`** — offline P99 spell validation: an **embedded** dataset (`spelldata.json`, scraped from the wiki's Category:Spells) plus a pure `Validate(name, class)` lookup. No deps, no runtime network (the embedded set is the authority — the client's `spells_us.txt` is a superset). Its `gen/` subpackage is a standalone `go generate` tool (not in the app binary) that regenerates the dataset.
- **`tui`** — the Bubble Tea UI; the fan-in hub (imports `braille`, `eq/combat`, `cues`, `eq/eqclass`, `eq/gamestate`, `inventory`, `session`, `tts`). Reads lock-free session/tracker snapshots and **never imports the parser**. It also **never imports the network-bearing `eqmap`/`prices`**: it consumes them through the `MapSource`/`PriceSource` interfaces (`tui/deps.go`), and `cmd/99dps` constructs the concrete clients + adapters (`cmd/99dps/deps.go`) and injects them via `NewProgram`.

The internal dependency graph is acyclic; `cmd/99dps` is the composition root — it wires `eq/gamestate`/`loader`/`parser`/`session`/`tui` together and owns the network-bearing `eqmap`/`prices` (injected into `tui` as interfaces, never imported by it). `eqmap` and `prices` reach the network on demand, off the render loop via a `tea.Cmd`; `tts` also downloads its ~120MB model on first enable.

## Commands

- `make` — list all targets with descriptions (the default goal is `help`).
- `make build` — build, keeping the `99dps` binary (`go build -trimpath -ldflags="-s -w" -o 99dps ./cmd/99dps`).
- `make all` — `build` then `clean` (builds, then deletes the binary; useful as a compile check).
- `go run ./cmd/99dps -logdir <path>` — fastest dev iteration. Log dir also reads `EQ_LOG_DIR`, else `loader.DefaultLogDir`.
- `go test ./...` — all tests. Single test: `go test ./internal/parser -run TestParseCast`. Coverage: `go test ./... -cover`.
- `gofmt -l .` must be empty; `go vet ./...` must be clean before finishing a change.
- `make windows` — cross-compile `99dps.exe` (`GOOS=windows GOARCH=amd64`). Pure Go / no cgo, so it builds from any host. `make release-windows` adds version metadata + a SHA-256; `make lint-windows` lints the build-tagged Windows code. See `docs/windows-release.md` (incl. the deliberate no-code-signing decision).

## Cross-platform (Windows)

The app is portable: pure-Go deps (no cgo); Bubble Tea + Lipgloss render truecolor ANSI that Windows Terminal (and modern `conhost`) interpret, and CRLF is already stripped (`TrimRight(…, "\r\n")`). Platform-specific pieces are isolated by build tags:
- **`internal/loader/defaultdir_{unix,windows}.go`** — `DefaultLogDir`. The EQ folder layout is identical on every OS, so once the log dir is known everything else (e.g. `spells_us.txt` at `<logdir>/../spells_us.txt`) is located relative to it.
- **Log-dir resolution** (`resolveLogDir` in `cmd/99dps/main.go`, identical flow on every OS): `-logdir` flag (saved) → `EQ_LOG_DIR` → saved choice (`os.UserConfigDir()/99dps/logdir.txt`) → platform default if it already holds logs → **auto-detect + prompt** (the chosen dir is saved). A debug `-logfile <path>` flag bypasses all of this: it follows one explicit eqlog from the start (so a captured log replays in full), skips the character hot-swap watcher, and does **not** touch the saved log-dir choice — handy for replaying a friend's log. `defaultSpellsPath` then finds `spells_us.txt` next to the log first, falling back to `<logdir>/../spells_us.txt`. Detection (`internal/loader/locate.go` cross-platform core; `scanForEQ`/`eqLogDirFrom`/`logDirFromChoice`) scans candidate roots one level deep for an EQ marker (`eqgame.exe`/`eqclient.ini`/`spells_us.txt` or `eqlog_*.txt`). The platform files supply the roots + prompt UX:
  - `locate_windows.go`: roots = `C:\P99`, Sony/Daybreak dirs, Desktop/Downloads/Games/My Games, the exe folder; prompt = **native Yes/No box** to confirm a find, else a **folder picker** (PowerShell WinForms).
  - `locate_unix.go`: roots = `~/Desktop|Downloads|Games|Documents`, `~/.wine/drive_c`, `/mnt`, `/opt`, the exe folder; prompt = **console** (list+confirm or type a path), since the app launches from a terminal.
  Both persist the pick to the same config file, so it's a one-time question. Saving is unified; only the candidate roots and prompt mechanism differ by OS.
- **`internal/tts/play_{unix,windows}.go`** — WAV playback for the Kokoro neural engine (`tts/kokoro.go`, which drives the bundled sherpa-onnx offline-TTS CLI as a subprocess; assets fetched on first enable by `tts/fetch.go`). Unix plays the synthesized clip through the first available CLI player (`paplay`/`aplay`/`ffplay`/`sox`); Windows uses the built-in `System.Media.SoundPlayer` via `powershell` (hidden window). Both subprocess calls (synth + playback) are bounded by a timeout so a wedged process can't stall the serial cue worker. No player / no downloaded model → cues silently no-op.
The Linux-only log-rotation tooling (`scripts/`, systemd) has no Windows equivalent; on Windows logs just grow (or wire a Task Scheduler + PowerShell job).

## Data flow (three concurrent pieces)

`launchTUI` (`cmd/99dps/run.go`) wires these together and orchestrates graceful shutdown (the Bubble Tea program returns on quit → signal the watcher → `Tail.Stop()` to end the parser):

1. **`loader.LoadFile(dir)`** — picks the most-recently-modified `eqlog_*.txt` under `dir` and follows it (`loader.Latest` finds it, `loader.Follow` opens the tail). Character is parsed from the filename.
2. **`parser.DoParse(tail, sink, character)`** (goroutine) — for each line, `dispatch` runs a cheap `has*` screen, then one `parse*` regex, then forwards a typed event to a `Sink`. The parser depends on the `Sink` interface, **not** on the `session` package — `*session.SessionManager` satisfies it.
3. **`tui.Program.Run()`** (main goroutine, blocks) — the Bubble Tea model self-ticks once per second; each `tickMsg` calls `refresh()` to snapshot all sessions, resolve the selection, and re-render every panel from the snapshot (lock-free). `tui.NewModel`/`NewProgram` build it; the model reads the shared `*session.SessionManager` + `*gamestate.Tracker`.
4. **`logController.watch`** (goroutine, `cmd/99dps/run.go`) — polls `loader.Latest` every few seconds; when a *different* eqlog becomes the most-recently-written (you switched characters in-game), it hot-swaps: opens the new tail from end-of-file, stops the old one (ending its parser goroutine), `Clear`s the manager + tracker, replays the new log into the tracker only (`parser.RebuildTrackerFromFile` — recovers active spell timers / class / zone, since the live tail starts at end-of-file), and calls `Program.SwitchCharacter` (a `switchMsg` that updates the banner + flashes a status). No restart needed.

## Parsing model and the central limitation

EQ logs carry **no entity IDs and no caster on spell damage**. Two consequences shape the whole design:

- Two simultaneously-alive same-named mobs (`a saltwater croc`) are textually identical — they cannot be told apart. Both the Mob Tracker (one repop entry per kill) and the spell timers (see below) keep *per-instance* state and use heuristics to decide "same mob again" vs "a second same-named mob," since the log itself can't say.
- Spell/proc/DoT damage logs in passive voice (`X was hit by non-melee for N points of damage`) — target and amount only, **never who cast it**. So there is no spell-DPS leaderboard, even for the player.

Line categories in `dispatch` (order matters — see the `hasDamage` gotcha below):
`hasDamage`→`Apply` (melee), `hasSwing`→`ApplySwing` (miss/dodge/parry/block avoidance), `hasCrit`→`ApplyCrit`, `hasMagic`→`ApplyMagic` (non-melee), `hasEvent`→`ApplyEvent` (kill/xp/death; party-vs-solo xp distinguished). Combat verbs are a fixed alternation in `COMBAT_VERB_STRING`. Cast lines (`You begin casting X`) are **not** a `Sink` event — `DmgParser.observeSpells` handles them separately, feeding the spell-timer subsystem (see below).

## Session model and attribution

`SessionManager` holds an append-only `[]*CombatSession` and an `activeSession` index.

- **Segmentation is adaptive-cadence** (`activeForLocked`). Combat *exchanges* — melee `Apply`, `ApplyMagic`, and `ApplySwing` — drive it: a fight rolls to a new session when the gap since the last exchange exceeds `segPulseK × pulse`, clamped to `[segGapFloor, segGapCeil]` seconds, where `pulse` is an EWMA of recent inter-exchange gaps. Dense melee pins the threshold to the floor; slow/sparse caster fights widen it toward the ceiling. **Live-mob rule** (the real boundary is the *kill*, not the clock): a lull past the threshold does *not* split while the engaged mob is still **alive** — i.e. `CombatSession.liveMobs() > 0`: distinct damage-*targets* (mobs) minus credited deaths (`kills`/`xpGains`, "no xp yet ⇒ still up") — *and* this exchange returns to that mob (`reengages`, matched case-insensitively against the session's damage-targets only, so a pet/group-mate dealer can't bridge unrelated fights), within `segLiveCeil` (5 min, the leash/flee bound). So a single pull stays one session through root/med lulls (real example: a drolvarg camp with 29/118/161s gaps, mobs still up); once the mob's **dead** (xp), the next pull — even a same-named respawn — rolls a fresh session, and a *different* mob mid-lull rolls too. `ApplyCrit` and the non-death `ApplyEvent` cases are **annotate-only** (kills/xp/crits are punctuation, never boundaries), so a multi-mob pull with several kills stays one encounter; death and zone/camp are the exception — they force a hard boundary. Constants are tuned against real logs — see their comment in `internal/session/sessionManager.go`.
- **Damage is keyed by dealer** in `aggressors map[string]DamageStat` (dealer name with spaces→underscores as the key).
- **The session *name* comes from the target, not the dealer** — `Name()` picks the enemy taking the most damage (the thing being fought), falling back to the heaviest non-player dealer, then "Solo".
- **Offense vs defense are separate axes** (`recordOutcome`): an attacker's accuracy counts only hit/miss; dodge/parry/block/riposte are the *defender's* doing and only land in the defender's tally (the avoidance table). Rune absorbs are tracked but excluded from `Avoided()` (the blow connected).
- **Spell handling**: `magicTotal` is the enemy non-melee total (folded into the encounter total). EQ logs name no caster, so there's no honest per-spell split — but a client only sees its *own* non-melee damage, so the TUI credits it to the player: the Damage meter rolls it up as a dim `↳ spells` child under the You row (falling back to an unattributed `spells (n/a)` line only when there's no You row). Per-spell *duration* tracking lives in the separate spell-timer subsystem (below), not on `CombatSession`.
- **Per-dealer rows keep only aggregates** (`DamageStat`: `Dealer/Total/Hits/FirstTime/LastTime/Special*`), never the individual hits — so a snapshot is a flat value copy and memory stays bounded over a long raid. `Name()` reads a session-level `targets map[string]int` (damage summed per enemy) rather than rescanning hits.

## Concurrency (snapshot pattern)

`SessionManager` owns a single `sync.RWMutex`. Every public method locks at its boundary (writers take `Lock`, `Current`/`All`/`Len` take `RLock`). Readers never touch live state: `Current()`/`All()` return **deep snapshots** (`CombatSession.snapshot()` `maps.Clone`s every map; all map values are pure value types, so a shallow clone is a full copy), so the UI and all `CombatSession` getters/`render*` functions operate lock-free on owned copies. When adding state to `CombatSession`, clone it in `snapshot()` (and keep its map values copy-safe — no pointers/slices that alias live state). `App` guards its own selection/scroll state with a separate `mu`.

## Rendering layer (`internal/tui`, Bubble Tea + Lipgloss)

The UI is a single Bubble Tea `Model` (Elm-style `Init`/`Update`/`View`) reading lock-free snapshots. `model.go` (the root `Model`: state, tick, `refresh`) has been **decomposed by concern** into per-file pieces:
- **`model.go`** — the root `Model` struct, `NewModel`, the 1s self-`tick`, `refresh()`/`rebuildInteractive` (snapshot + re-render), and `announceCues`. **`program.go`** — `NewProgram`/`Program` wrap `*tea.Program` so the host can `SwitchCharacter`.
- **`layout.go`** — `layout()` computes panel rectangles for the window. **`view.go`** — `View` composes the active screen (banner + Sessions sidebar + Damage meter / Offense·Defense / class panel / Mob Tracker + footer). **`update.go`** — `Update` (keys/mouse/tick/resize/switch) plus input routing: `panelAt` (wheel → hovered panel), `hoverTargetAt`/`mobAt` (clicks → targets), `editKey`.
- **`damage.go`** — the damage-meter renderer (`damageMeter`, pure; `damageContent` a thin forwarder). **`sessions.go`** — the Sessions sidebar/table + its selection/scroll. **`panels.go`** — pure `data → string` renderers (`card`, the timer-column split, mob tracker, specials/avoidance). **`class.go`** — the class-aware bottom panel (`classPanel`, cooldown/discipline/canni rows). No program state in the pure renderers, so they're unit-tested directly.
- **Tabbed screens** (`settings.go`: `tabBar`/`cycleTab`/`gotoScreen`) — beyond the main meter there are **Map** (`map.go`: braille zone map via the `MapSource` seam, player pinned from `/loc`; a right-hand **POI side panel** lists the map's `P`-line points grouped by label heuristic — ZONE LINES (`to <zone>`) / NPCS (`Name (Role)`) / POINTS — each with distance + an 8-way screen-arrow, `↑/↓` select; the selected POI drops a distinct **🚩 pin** (≠ the player's 📍, edge-clamped when off-screen at zoom). All POIs also show as faint colored `+` markers. The braille `Canvas` supports multiple overlay `Marker`s — player + selected pin), **Tools** (`tools.go`: inventory board from `internal/inventory` + `prices` lookups), **Live Log** (`logview.go`: a merged auto-tailing sheet — `time │ raw log line │ fired cue` — that interleaves the verbatim log with the cues the engine fired; raw lines are fed by an optional `parser.RawTap` the composition root wires into a thread-safe `LogTail` ring (`logtail.go`), and fired cues are captured by a `cueRecorder` wrapping the cue `Speaker`, so the tui still never imports the parser), **Raid** (`chchain.go`: the CH-chain monitor + raid-debuff board — two side-by-side scrollable viewports `vpChain`/`vpRaid`; see the dedicated section below), **Setup** (`setup.go`: first-run onboarding + the neural-voice download), and **Settings** screens. **`store.go`** persists per-character UI prefs; **`layoutprefs.go`** the panel-size prefs.
- **`theme.go`** — the themes + gradient/`fg` helpers. **`format.go`** — number/time/name formatters (`humanize`, `truncate`, `mmss`, …).
- Each overflowing panel renders into its own `bubbles/viewport` (independent scroll); the mouse wheel routes to whichever panel the cursor is over (`panelAt`), and clicks resolve via line→target maps built by the same code that renders them (the anti-drift pattern).
- **Lipgloss wraps overlong lines** (it doesn't clip), which reads as "scrunched". So every line must fit its width: tables are **width-aware** (Hit%/Crit% gate on width; the share bar yields before the name; the countdown column is never dropped), free text is clipped with `truncate`/`MaxWidth`, and a degenerate early `WindowSizeMsg` (0×0) is ignored. `TestViewFitsWindow`/`TestDamageNoOverflow` guard this.

## Spell timers and live state (`gamestate` package)

A separate subsystem from DPS: tracks durations of spells the player casts (debuffs on mobs, buffs on self/others). `gamestate.Load` parses the client's `spells_us.txt` (217 `^`-delimited fields; `-spells` flag or `<logdir>/../spells_us.txt`) into a `Book`; `gamestate.Tracker` holds the live timers. The `Tracker` is the package's central stateful object — fed one log line at a time via `Observe` (a deliberate *fan-out*: one line can matter to several subsystems, e.g. a kill both expires the victim's debuffs and records a repop) and read lock-free by the UI. Its state is grouped into composed, caller-locked sub-trackers under one mutex: `cooldownTracker` (reuse/feign/bind, `cooldown.go`), `zoneTracker` (zone/repops/kills, `zone.go`), and `canniMeter` (`canni.go`); spell timers + class/level stay on the core (`tracker.go`). Subsystems that detect the class return it for `inferClassLocked` to apply (a `/who` title always wins). The parser feeds it (via `DmgParser.observeSpells`): `You begin casting X` → pending; a later line that `EndsWith` the spell's `cast_on_other` emote identifies the **target** (the prefix) and starts a timer of `Spell.DurationSeconds(level)` (the EQ buffduration formula). Caster level **and class** come from `/who` self lines (level-ups give level only) via `DmgParser.parseLevel`. EQ `/who` prints a level-based *title* (`[60 Warlord]`), not the class name, so `eqclass.ClassFromTitle` maps the title→class (see `internal/eq/eqclass/class.go`); the `Tracker` stores it (`SetClass`/`Category`). Timers expire on timeout, the target-side `spell_fades` emote, the caster-side `Your <Spell> spell has worn off.` line (the reliable signal for an **early break** — root/mez shaken off, a debuff dropped — `expireSoonestLocked` clears the soonest instance since the line names no target), a resist, or the target being slain. **Same-named mobs are tracked as distinct timers** (mirroring the Mob Tracker's one-entry-per-kill): a landing isn't a blind overwrite of `(spell, target)` — `addOrRefreshTimerLocked` only refreshes an existing same-named copy when it's within `reapplyWindowSec` (30s) of expiring (you're re-applying to the *same* mob as it wakes); cast earlier it's a different same-named mob and gets its own instance. Any **mob-landed** spell can spawn a second instance — a debuff **or CC (pacify/mez)**. Note pacify/calm/lull are flagged **beneficial** in `spells_us.txt` (GoodEffect 1) yet land on mobs, so the test is `Detrimental || Pacify || Mez`, *not* just `Detrimental` (the old bug: pacify always overwrote). A genuine beneficial **buff** lands on a unique target (you or a named ally), so it always refreshes, never duplicates. (The TUI countdown color is a *separate* classifier — `gamestate.TimerUrgency`, `internal/eq/gamestate/urgency.go`: green → gold (`lowFrac` 0.5) → red (`StaleFrac` 0.2), each cutoff **capped in absolute time** (`expireCapSec` 60s, `lowCapSec` 300s) so a multi-hour self-buff doesn't read orange with an hour left. It no longer gates the refresh decision — that's the flat 30s window above.) A second instance gets a unique map key (same `Target`, so grouping/break/dismiss are unaffected). Symmetrically, **one ending event clears one instance**: a fade line drops the soonest same-named copy (not every copy), and a slay drops one instance per spell — so killing or fading one of two `a sand giant` leaves the other's timer running. A re-cast after a copy falls off naturally re-adds it (the freed slot is reused). **Crowd control** (mez + charm + pacify) is flagged on the spell (`Spell.Mez` from the `mesmeriz`/`enthrall` landing emote; `Spell.Charm`; `Spell.Pacify` from the `looks less aggressive` lull/calm/pacify emote) and rendered in a pinned **CROWD CONTROL** section at the top of the timer panel (marked `M`/`⊗`/`z`), above separate DEBUFFS (on mobs) and BUFFS (on you/allies) sections, so a debuff never mixes in with a group-mate's buffs. Pacify uses a P99-specific duration (`Spell.PacifyDurationSeconds` — `DurCap × 6`, no cast-tick: Calm 180s, Pacify 210s; Wake of Tranquility is uncapped ≈ 7 min). Mez and pacify break with no log message when the mob takes damage (it re-aggros), so the parser calls `Tracker.BreakCCOnTarget` on every damage line (names normalized — the landing emote drops the article that the damage line keeps). The tracker is cleared on a character switch. It degrades gracefully (no panel data) if `spells_us.txt` is absent.

**Incoming debuffs (effects on the player)** are a separate, *registry-driven* path (`internal/eq/gamestate/incoming.go`), not derived from the spell book — because an incoming hostile spell logs only its `cast_on_you` emote with **no caster and no caster level**. Two limits follow and shape the design: (1) within an effect family the `cast_on_you` emote is shared (every classic slow → "You feel drowsy."; every root → "Your feet adhere to the ground."), so the emote tells us the **category**, not the spell — we show the category (`Slowed`/`Rooted`/`Snared`/`Crippled`/`Malo'd`/`Tashed`/`Feared`/`Blinded`/`Diseased`/`Poisoned`); (2) the real duration scales with the mob's (unknown) level, so we time the family's **ceiling** (an over-estimate, marked `Timer.Estimated` → a leading `~` in the UI) and clear it early on the authoritative wear-off (`fade`) emote — over-estimate + reliable clear means a timer can linger a beat past the true end but never vanish while you're still debuffed. `incomingDebuffs` is the curated **common melee-relevant** set (each landing emote maps to exactly one category, so detection is unambiguous); `init` builds `incomingByLand`/`incomingByFade`. `matchIncomingDebuffLocked` runs on every line (the detrimental phrasing can't collide with a self-cast, so it's matched regardless of any pending cast) and starts a single per-category timer on `"You"` (re-application refreshes — no same-named instances, since the target is always you); `expireIncomingByFadeLocked` clears it on the fade line, and the existing slay path drops it when you die. These render in their own red **ON YOU** section, pinned above BUFFS in the player-self column (never the Enemy/mob column), and aren't click-dismissible (a debuff on you carries no target to dismiss).

**Offline spell validation (`internal/spellwiki`)** is a cross-check layered on top of the timers, not part of the `Book`. The client's `spells_us.txt` is a *superset* — the server hides spells, and a cast emote can resolve to a wrong/non-existent entry — so the P99 wiki is the authority. The package bundles a dataset (`spelldata.json`, ~1500 spells scraped from the wiki's Category:Spells) **embedded at build time** via `go:embed`, and exposes a pure synchronous `Validate(name, class) (found, classOK)` map lookup — no per-cast network, no `tea.Cmd`. The TUI (`tui/spellvalidate.go`) runs every active-timer spell through it (`suspectActiveSpells`) and **flags, never silently changes,** any spell the bundled set can't confirm for the detected class — rendered as a dim-red `⚠ unconfirmed on P99 wiki` block under the class panel, with a trailing `P99 set: N spells · <date>` provenance line (`spellwiki.Count`/`Generated`) so the dataset's currency is visible. Flagging is suppressed when the class is unknown (castability can't be judged) and for `Estimated` timers (incoming debuffs are categories, not spells we cast). The dataset is regenerated offline by a maintainer tool (`internal/spellwiki/gen`, run via `go generate ./internal/spellwiki`) that scrapes the wiki via the MediaWiki API, parses pages with `spellwiki.Parse`, and validates anchor spells before writing; the tool is **not** part of the app binary, and the runtime imports only the pure `spellwiki` package.

## Class-aware bottom-right panel

The `timers` panel is **class-driven** (`App.updatePanel`), keyed on `tracker.Category()` (`eqclass.Category`, derived from the detected class):
- **Caster** → spell timers (the default; also used until a `/who` reveals the class).
- **Pure melee** (War/Monk/Rogue) → `renderSkills`: the player's activated-skill breakdown plus hit/crit/avoid. EQ logs special attacks with a *generic* verb, so the bucket is the most we can recover (`CombatSession.skills`, player-only via `playerSkill`): every kick variant logs "kick", every monk special strike (Eagle Strike / Tiger Claw / Dragon Punch) logs "strike", hand-to-hand auto-attack is "punch", weapon is "crush". `displaySkillName`/`skillRelevant` apply class+level best-guesses at render time (a 30+ monk's "kick" → "Flying Kick"; "strike" shown only for monks).
- **Cooldown timers** sit above the skills breakdown. Two trigger paths feed `Tracker.cooldowns` (a name→expiry map rendered by `Cooldowns()`, green-when-ready / blue-while-counting): (1) message-matched abilities in `gamestate.cooldownRegistry` (e.g. Mend, monk, 360s — matched leniently across success/partial/fail variants); (2) macro/event-driven ones like **Feign Death** (11s reuse, measured from logs), started by `FeignAttempt` off the player's custom feign emote. Detecting any class ability also infers the class. **Feign banners** (`FeignStatus`: ✓ feigned by success-by-absence, ⚠ failed on a player-gated "fallen to the ground") and a **bind-wound** progress indicator (`Binding`) sit above the cooldowns. Next registry candidates: disciplines, Harm Touch, Lay on Hands.
- **Hybrid** (Pal/SK/Ranger/Bard/Beastlord) → spell timers plus a one-line `skillsSummary` digest.

The panel title (`App.panelTitle`) and content both switch on the category.

**Disciplines** are a separate widget (`eq/eqclass/discipline.go` data + `eq/gamestate/discipline.go` state + `tui/class.go` `disciplineRows`), gated to melee/hybrid classes and the player's level, detected from each disc's self-activation emote (`disciplineActivations` → infers the class). The key P99 rule: **all combat disciplines share ONE reuse timer** — activating any discipline locks out *every* discipline until that one's reuse expires (a 60-min-reuse disc like Fearless blocks everything for an hour). So `disciplineTimersLocked` computes phases off the *most-recently-used* disc (`sharedDiscLockLocked`): it shows **Active** for its duration then counts its reuse down, and every other disc shows the **same** lockout countdown (`DiscCooling`, `LockedBy`/`LockTotal` set). `Tracker.DisciplineLock` exposes the shared state for the widget header (`⊘ M:SS · <holder>`); collaterally-locked discs render muted to distinguish them from the one you actually used.

## Zone awareness and repop tracking

The `Tracker` also derives zone state from the log (`internal/eq/gamestate/zone.go`): a "You have
entered X" line sets the current zone and looks up its default respawn in
`gamestate.ZoneRespawn` (data in `internal/eq/gamestate/zonetimers.go`, transcribed from the P99
wiki — see `docs/zone-spawn-timers.md`). Every mob death — the player's own ("You have slain X!") *and* group/others'
("X has been slain by &lt;player&gt;!", with a killer-is-mob heuristic to skip
player deaths) — starts a repop timer at the zone default (`Respawns()`). Each
death is its own entry (a `[]respawnEntry`, not name-keyed), so two same-named
mobs dying close together are tracked as distinct spawns rather than one reset.
A kill landed by **your own pet** (`<mob> has been slain by <petName>!`, where
`petName` is the pet whose `/pet leader` line named you — passed into
`observeLocked`) is credited to **you** (`mine`, killer "You"), not parked under
others' kills; a group-mate's pet stays theirs. The repop list splits **group
kills** (yours, plus a group-mate's that you got xp
for — `creditGroupKillLocked` flags the just-killed mob when a "You gain (party)
experience" line lands) from **others'** kills; the killer name still shows for
any blow you didn't land yourself. Repops **persist across zones** (each
`respawnEntry` carries its `zone`): zoning only resets the kill tallies, not the
list — the current zone's timers lead, then each other zone renders under its own
zone-name header (`mobTracker`), ordered by its soonest timer; a zone whose
entries have all purged drops off. Entries purge ~2 min after they pop. Users can
also drop **manual** timers (double-click empty space in the Mob Tracker →
`AddManualRespawn`, seeded from the zone default or a typed time, live-only,
flagged `✎`). The Damage panel's
**kills/hr** line is also zone-wide and xp-credited only: `ZoneKillStats` counts
"You gain (party) experience" lines since the zone-in (not per-encounter killing
blows). The total is the count; the *rate* is a **rolling last-hour** figure
(`killRateWindowSec`) — recent kills over the window's actual length, not a flat
average since the first kill — so idle time drops it toward zero rather than
leaving a stale lifetime average. **Click a repop row**
to edit that mob's respawn: an inline editor (digits/`:` typed into the bottom
bar, Enter saves) writes a per-`(zone, mob)` override via `gamestate.Overrides` (JSON
at `<logdir>/99dps-overrides.json`), retroactively fixes that mob's live timers,
and is used for all future kills (override → zone default fallback in
`recordKillLocked`). The repop list is its own panel (`viewRepops` / "Mob
Tracker", `App.updateRepops`, `renderRespawns`), below the spell-timer panel in
the right column; its title shows the current zone. The bottom bar
(`updateShortcuts`) shows `character · L<level> <class> · Zone: <zone>` from the
tracker. Caveat: zone is only known after the next zone-in (no log line gives the
current zone at startup), and repop times are zone *defaults* — named/PH mobs
differ.

## CH-chain + raid debuffs (the Raid tab)

Two `gamestate` sub-trackers fed off `Tracker.Observe` (same fan-out as the spell
timers), read lock-free by the **Raid** tab. Both parse guild/group *macro
call-outs*, not spell landings.

**CH-chain analyzer (`internal/eq/gamestate/chchain.go`).** Clerics running a
Complete Heal chain announce each cast: `"<Caster> tells the guild, 'KCH -
<Target> - <Slot>'"`, so one guild member's log carries the whole chain. Parsed by
`kchRE` (tolerates `-`/`---` separators, trailing space; slot is a number *or* a
label — `GAP`/`R11`/`SPOT`/`RAMP` are real call-outs, counted; `GAP` is also
surfaced as the clerics' own gap signal). Real raids taught the model three
non-obvious things, each with a regression test: (1) **concurrent chains** — 2–4
tanks are chained at once with call-outs *interleaved*, so chains are keyed by
**target** in `chchainTracker.active` and tracked simultaneously (a single
"current chain" would shatter each at every interleaved call); (2) **cadence is
per-chain and derived** — median of the "tight" intervals (within 1.5× the
overall median), which scales with stack size (~3s @16 clerics, ~6s @7); a **gap**
is an interval past `max(2.5×cadence, cadence+3s)`, judged against cadence from
*prior* intervals so it can't inflate its own baseline; (3) **slots are positions,
clerics get subbed** — the live roster is keyed by **slot** (`chSlot`), the holder
is whoever's currently calling that number, and a holder silent >`chDropSec`(60s)
is dropped and replaced by the next caller (counted as a "sub"). `statsLocked`
also predicts **NextCaster** (next numbered slot after the last, wrapping) and,
per cleric, **BeatPct** (% of their *recent* laps — capped to `chLapWindow` so a
stack-size change can't retro-misjudge old laps — within 1.5× the rotation period
`cadence×activeClerics`) and **LagSec** (seconds overdue now). `CHActiveChains` /
`CHChainHistory` return fresh value-typed snapshots (no aliasing). A quiet chain
(>`chChainEndSec` 90s) finalizes into `done` for the post-raid summary.

**Raid-debuff board (`internal/eq/gamestate/raid.go`).** Reuses the existing
macro-debuff vocabulary/durations (`enemydebuff.go` `macroDebuffs` — SLOWED /
TASHED / SIEVED / MALO / CRIPPLE / OOS…). `matchMacroDebuffLocked` already timed
these for the player's Enemy column; it now *also* calls `raid.recordLocked` with
the **caller** (`speakerOf`). Keyed by target → debuff label → `{caller, expiry}`;
`RaidDebuffTargets(now)` returns each active mob (called within
`raidTargetTTLSec`) and its debuffs with **seconds remaining** (≤0 = lapsed →
`OUT`) and who called each. Bounded by `raidTargetCap` (evict stalest).

**TUI (`internal/tui/chchain.go` + `view.go`/`update.go`).** The Raid tab is two
side-by-side scrollable `viewport`s (`vpChain`/`vpRaid`) — `chchainBody` /
`raidDebuffBody` render into them via `refreshRaid` (each tick + on resize/enter).
Column widths come from the single-source `chainCardW`/`raidCardW`/`raidSplit`
helpers (shared by `View`, `resizeViewports`, and wheel routing, so geometry can't
drift); below 64 cols it collapses to the chain column only. The roster's
**NextCaster** row is a full-width green highlight (`◀ NEXT`); a lagging cleric's
row goes red; every row is `truncate`d to width (Lipgloss wraps overlong lines).

## Views and input

`Model.layout()` (`internal/tui/model.go`) computes panel rectangles for the window: a gold banner header, a full-height **Sessions** sidebar, a top-right **Damage meter** beside a **Specials · Avoidance** column (card title **"Offense · Defense"**, which hosts its scroll chevron; the two stacked tables keep their own inner section heads), and a bottom row **class panel | [Enemy] | Mob Tracker** — caster/hybrid classes get a dedicated **Enemy** column (CROWD CONTROL + DEBUFFS, i.e. everything cast on mobs) and the class panel becomes "Buffs" + toolkit; pure melee gets two tiles (Skills | Mob); below `enemyColMinW` the Enemy column drops and CC+debuffs fold back into the class panel. (`timerColumn`'s `wantCC/wantDebuffs/wantBuffs` flags pick which sections each panel shows.) Input (all in `Update`): arrow keys / click select a fight, `End` jumps to live, the wheel scrolls the hovered panel, `t`/`tab` cycles theme, `a` toggles audio cues, `Backspace` clears sessions, clicking a buff target dismisses it (hover shows an ✕), clicking a repop row opens an inline editor (digits/`:`, Enter saves a per-(zone,mob) override). A transient status line in the footer shows switch/clear/edit feedback. `ctrl+c` always quits (even mid-edit).

## Gotchas

- **Non-melee must be excluded from `hasDamage`.** Spell lines contain "points of damage", so they would otherwise be parsed by the melee regex into a bogus `"<X> was"` dealer. `hasDamage` filters out `non-melee` and `You have taken`; non-melee is routed to `hasMagic`.
- The player's own crits and casts log under the **character name**, not "You" — the parser remaps the character name to "You" for attribution (hence `DoParse` takes `character`).
- An AoE/PBAoE lands on several mobs from one cast, so `Tracker` keeps the `pending` cast alive across landing emotes (cleared by the cast window or the next cast) and times each affected mob. A `Your target resisted the X spell.` line is surfaced briefly via `Tracker.Resisted` (a red badge), not a timer.
- **Pet → owner attribution** (`internal/eq/gamestate/pet.go`): the *only* reliable ownership signal is `"<Pet> says 'My leader is <Owner>.'"` (`/pet leader`), so the tracker builds a `petOwners` map from every such line for the **whole group** (`PetOwner(name)`), and flags the player's own pet (`PetName()`, owner == `SetCharacter`). The `"...Master."` command replies (both the `says '…'` and `tells you, '…'` forms) leak into nearby players' logs, so they're a leak-prone **charm-only fallback**: a charmed mob keeps a generic mob name and rarely gets a leader line, so a `"...Master."` speaker is adopted as **your** pet **only while you have an active charm running** (`hasActiveCharmLocked`) and **never over an owner a leader line already set** — bridging the gap before the first `/pet leader` for charm-killers (e.g. an Enchanter charming a "greater war boned skeleton" to kill, where the pet's kills/damage would otherwise go uncredited). The Damage meter rolls each pet up under its owner's row (`↳ <pet>`) **and folds the pet's damage into that row's total** — so an owner's Total/DPS/share is inclusive of everything they put out (for the player: melee + spells + pet), with the pet (and `↳ spells`) shown only as dim breakdown children. Ranking and bar-scaling run on these inclusive totals. A pet whose owner dealt no damage is credited as a `<owner>'s pet` row. **Insta-clickies** with no matchable spell message are handled by a class-gated registry (`internal/eq/gamestate/clicky.go`, `RegisterClicky`).
