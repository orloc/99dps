# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

A terminal DPS meter for classic EverQuest (P99). It tails the active EQ log file, parses combat lines as they appear, and renders live per-dealer damage, accuracy/avoidance, crits, specials, spell estimates, and a damage-bar graph in a **Bubble Tea + Lipgloss** TUI (truecolor, themed). The legacy `gocui` UI has been removed.

Go 1.25. Tests live next to the code; run `go test ./...`.

**Layout** (standard Go): `cmd/99dps/` holds `package main` (`main.go` + `run.go` wiring); all library packages live under `internal/` (`combat` — combat DTOs; `eqclass` — class/category taxonomy; `loader`, `parser`, `session`, `gamestate`, `tts` — shared audio-cue engine, and `tui` — the Bubble Tea UI) so they can't be imported externally. Import paths are `99dps/internal/<pkg>`. `parser` depends on the `Sink` and `SpellObserver` interfaces (not on `session`/`gamestate`); `gamestate` owns the zone-respawn data; `tui` reads lock-free session/tracker snapshots and never imports the parser.

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
- **Log-dir resolution** (`resolveLogDir` in `cmd/99dps/main.go`, identical flow on every OS): `-logdir` flag (saved) → `EQ_LOG_DIR` → saved choice (`os.UserConfigDir()/99dps/logdir.txt`) → platform default if it already holds logs → **auto-detect + prompt** (the chosen dir is saved). Detection (`internal/loader/locate.go` cross-platform core; `scanForEQ`/`eqLogDirFrom`/`logDirFromChoice`) scans candidate roots one level deep for an EQ marker (`eqgame.exe`/`eqclient.ini`/`spells_us.txt` or `eqlog_*.txt`). The platform files supply the roots + prompt UX:
  - `locate_windows.go`: roots = `C:\P99`, Sony/Daybreak dirs, Desktop/Downloads/Games/My Games, the exe folder; prompt = **native Yes/No box** to confirm a find, else a **folder picker** (PowerShell WinForms).
  - `locate_unix.go`: roots = `~/Desktop|Downloads|Games|Documents`, `~/.wine/drive_c`, `/mnt`, `/opt`, the exe folder; prompt = **console** (list+confirm or type a path), since the app launches from a terminal.
  Both persist the pick to the same config file, so it's a one-time question. Saving is unified; only the candidate roots and prompt mechanism differ by OS.
- **`internal/tts/speaker_{unix,windows}.go`** — TTS engine. Unix probes `spd-say`/`espeak`; Windows uses built-in SAPI via `powershell … System.Speech` (hidden window). No engine → cues silently no-op.
The Linux-only log-rotation tooling (`scripts/`, systemd) has no Windows equivalent; on Windows logs just grow (or wire a Task Scheduler + PowerShell job).

## Data flow (three concurrent pieces)

`launchTUI` (`cmd/99dps/run.go`) wires these together and orchestrates graceful shutdown (the Bubble Tea program returns on quit → signal the watcher → `Tail.Stop()` to end the parser):

1. **`loader.LoadFile(dir)`** — picks the most-recently-modified `eqlog_*.txt` under `dir` and follows it (`loader.Latest` finds it, `loader.Follow` opens the tail). Character is parsed from the filename.
2. **`parser.DoParse(tail, sink, character)`** (goroutine) — for each line, `dispatch` runs a cheap `has*` screen, then one `parse*` regex, then forwards a typed event to a `Sink`. The parser depends on the `Sink` interface, **not** on the `session` package — `*session.SessionManager` satisfies it.
3. **`tui.Program.Run()`** (main goroutine, blocks) — the Bubble Tea model self-ticks once per second; each `tickMsg` calls `refresh()` to snapshot all sessions, resolve the selection, and re-render every panel from the snapshot (lock-free). `tui.New`/`NewProgram` build it; the model reads the shared `*session.SessionManager` + `*gamestate.Tracker`.
4. **`logController.watch`** (goroutine, `cmd/99dps/run.go`) — polls `loader.Latest` every few seconds; when a *different* eqlog becomes the most-recently-written (you switched characters in-game), it hot-swaps: opens the new tail from end-of-file, stops the old one (ending its parser goroutine), `Clear`s the manager + tracker, replays the new log into the tracker only (`parser.RebuildTrackerFromFile` — recovers active spell timers / class / zone, since the live tail starts at end-of-file), and calls `Program.SwitchCharacter` (a `switchMsg` that updates the banner + flashes a status). No restart needed.

## Parsing model and the central limitation

EQ logs carry **no entity IDs and no caster on spell damage**. Two consequences shape the whole design:

- Two simultaneously-alive same-named mobs (`a saltwater croc`) are textually identical — they cannot be told apart.
- Spell/proc/DoT damage logs in passive voice (`X was hit by non-melee for N points of damage`) — target and amount only, **never who cast it**. So there is no spell-DPS leaderboard, even for the player.

Line categories in `dispatch` (order matters — see the `hasDamage` gotcha below):
`hasDamage`→`Apply` (melee), `hasSwing`→`ApplySwing` (miss/dodge/parry/block avoidance), `hasCrit`→`ApplyCrit`, `hasMagic`→`ApplyMagic` (non-melee), `hasEvent`→`ApplyEvent` (kill/xp/death; party-vs-solo xp distinguished). Combat verbs are a fixed alternation in `COMBAT_VERB_STRING`. Cast lines (`You begin casting X`) are **not** a `Sink` event — `DmgParser.observeSpells` handles them separately, feeding the spell-timer subsystem (see below).

## Session model and attribution

`SessionManager` holds an append-only `[]*CombatSession` and an `activeSession` index.

- **Segmentation is adaptive-cadence** (`activeForLocked`). Combat *exchanges* — melee `Apply`, `ApplyMagic`, and `ApplySwing` — drive it: a fight rolls to a new session when the gap since the last exchange exceeds `segPulseK × pulse`, clamped to `[segGapFloor, segGapCeil]` seconds, where `pulse` is an EWMA of recent inter-exchange gaps. Dense melee pins the threshold to the floor; slow/sparse caster fights widen it toward the ceiling. `ApplyCrit` and the non-death `ApplyEvent` cases are **annotate-only** (kills/xp/crits are punctuation, never boundaries), so a multi-mob pull with several kills stays one encounter; death and zone/camp are the exception — they force a hard boundary. Constants are tuned against real logs — see their comment in `internal/session/sessionManager.go`.
- **Damage is keyed by dealer** in `aggressors map[string]DamageStat` (dealer name with spaces→underscores as the key).
- **The session *name* comes from the target, not the dealer** — `Name()` picks the enemy taking the most damage (the thing being fought), falling back to the heaviest non-player dealer, then "Solo".
- **Offense vs defense are separate axes** (`recordOutcome`): an attacker's accuracy counts only hit/miss; dodge/parry/block/riposte are the *defender's* doing and only land in the defender's tally (the avoidance table). Rune absorbs are tracked but excluded from `Avoided()` (the blow connected).
- **Spell handling**: `magicTotal` is the enemy non-melee total (folded into the encounter total). EQ logs name no caster, so there's no honest per-spell split — but a client only sees its *own* non-melee damage, so the TUI credits it to the player: the Damage meter rolls it up as a dim `↳ spells` child under the You row (falling back to an unattributed `spells (n/a)` line only when there's no You row). Per-spell *duration* tracking lives in the separate spell-timer subsystem (below), not on `CombatSession`.
- **Per-dealer rows keep only aggregates** (`DamageStat`: `Dealer/Total/Hits/FirstTime/LastTime/Special*`), never the individual hits — so a snapshot is a flat value copy and memory stays bounded over a long raid. `Name()` reads a session-level `targets map[string]int` (damage summed per enemy) rather than rescanning hits.

## Concurrency (snapshot pattern)

`SessionManager` owns a single `sync.RWMutex`. Every public method locks at its boundary (writers take `Lock`, `Current`/`All`/`Len` take `RLock`). Readers never touch live state: `Current()`/`All()` return **deep snapshots** (`CombatSession.snapshot()` `maps.Clone`s every map; all map values are pure value types, so a shallow clone is a full copy), so the UI and all `CombatSession` getters/`render*` functions operate lock-free on owned copies. When adding state to `CombatSession`, clone it in `snapshot()` (and keep its map values copy-safe — no pointers/slices that alias live state). `App` guards its own selection/scroll state with a separate `mu`.

## Rendering layer (`internal/tui`, Bubble Tea + Lipgloss)

The UI is a single Bubble Tea `Model` (Elm-style `Init`/`Update`/`View`) reading lock-free snapshots:
- **`model.go`** — the root `Model`: state, `layout()` (panel rectangles for the window size), `Update` (keys/mouse/tick/resize/switch), `View` (composes the banner + Sessions sidebar + Damage meter / Specials-Avoidance / class panel / Mob Tracker + footer), and the damage-meter renderer. `NewProgram`/`Program` wrap `*tea.Program` so the host can `SwitchCharacter`.
- **`panels.go`** — pure `data → string` panel renderers (`sessionsList`, `timersBody`, `ccBody`, `mobTracker`, `damageSpecials`, `damageAvoidance`, `card`). No program state, so they're unit-tested directly. **`class.go`** — the class-aware bottom panel (`classPanel`). **`theme.go`** — the 3 themes + gradient/`fg` helpers. **`format.go`** — number/time formatters.
- Each overflowing panel renders into its own `bubbles/viewport` (independent scroll); the mouse wheel routes to whichever panel the cursor is over (`panelAt`), and clicks resolve via line→target maps (`classTargets`/`ccTargets`/`mobTargets`).
- **Lipgloss wraps overlong lines** (it doesn't clip), which reads as "scrunched". So every line must fit its width: tables are **width-aware** (Hit%/Crit% gate on width; the share bar yields before the name; the countdown column is never dropped), free text is clipped with `truncate`/`MaxWidth`, and a degenerate early `WindowSizeMsg` (0×0) is ignored. `TestViewFitsWindow`/`TestDamageNoOverflow` guard this.

## Spell timers and live state (`gamestate` package)

A separate subsystem from DPS: tracks durations of spells the player casts (debuffs on mobs, buffs on self/others). `gamestate.Load` parses the client's `spells_us.txt` (217 `^`-delimited fields; `-spells` flag or `<logdir>/../spells_us.txt`) into a `Book`; `gamestate.Tracker` holds the live timers. The `Tracker` is the package's central stateful object — fed one log line at a time via `Observe` (a deliberate *fan-out*: one line can matter to several subsystems, e.g. a kill both expires the victim's debuffs and records a repop) and read lock-free by the UI. Its state is grouped into composed, caller-locked sub-trackers under one mutex: `cooldownTracker` (reuse/feign/bind, `cooldown.go`), `zoneTracker` (zone/repops/kills, `zone.go`), and `canniMeter` (`canni.go`); spell timers + class/level stay on the core (`tracker.go`). Subsystems that detect the class return it for `inferClassLocked` to apply (a `/who` title always wins). The parser feeds it (via `DmgParser.observeSpells`): `You begin casting X` → pending; a later line that `EndsWith` the spell's `cast_on_other` emote identifies the **target** (the prefix) and starts a timer of `Spell.DurationSeconds(level)` (the EQ buffduration formula). Caster level **and class** come from `/who` self lines (level-ups give level only) via `DmgParser.parseLevel`. EQ `/who` prints a level-based *title* (`[60 Warlord]`), not the class name, so `eqclass.ClassFromTitle` maps the title→class (see `internal/eqclass/class.go`); the `Tracker` stores it (`SetClass`/`Category`). Timers expire on timeout, the target-side `spell_fades` emote, the caster-side `Your <Spell> spell has worn off.` line (the reliable signal for an **early break** — root/mez shaken off, a debuff dropped — `expireSoonestLocked` clears the soonest instance since the line names no target), a resist, or the target being slain. **Crowd control** (mez + charm + pacify) is flagged on the spell (`Spell.Mez` from the `mesmeriz`/`enthrall` landing emote; `Spell.Charm`; `Spell.Pacify` from the `looks less aggressive` lull/calm/pacify emote) and rendered in a pinned **CROWD CONTROL** section at the top of the timer panel (marked `M`/`⊗`/`z`), above separate DEBUFFS (on mobs) and BUFFS (on you/allies) sections, so a debuff never mixes in with a group-mate's buffs. Pacify uses a P99-specific duration (`Spell.PacifyDurationSeconds` — `DurCap × 6`, no cast-tick: Calm 180s, Pacify 210s; Wake of Tranquility is uncapped ≈ 7 min). Mez and pacify break with no log message when the mob takes damage (it re-aggros), so the parser calls `Tracker.BreakCCOnTarget` on every damage line (names normalized — the landing emote drops the article that the damage line keeps). The tracker is cleared on a character switch. It degrades gracefully (no panel data) if `spells_us.txt` is absent.

## Class-aware bottom-right panel

The `timers` panel is **class-driven** (`App.updatePanel`), keyed on `tracker.Category()` (`eqclass.Category`, derived from the detected class):
- **Caster** → spell timers (the default; also used until a `/who` reveals the class).
- **Pure melee** (War/Monk/Rogue) → `renderSkills`: the player's activated-skill breakdown plus hit/crit/avoid. EQ logs special attacks with a *generic* verb, so the bucket is the most we can recover (`CombatSession.skills`, player-only via `playerSkill`): every kick variant logs "kick", every monk special strike (Eagle Strike / Tiger Claw / Dragon Punch) logs "strike", hand-to-hand auto-attack is "punch", weapon is "crush". `displaySkillName`/`skillRelevant` apply class+level best-guesses at render time (a 30+ monk's "kick" → "Flying Kick"; "strike" shown only for monks).
- **Cooldown timers** sit above the skills breakdown. Two trigger paths feed `Tracker.cooldowns` (a name→expiry map rendered by `Cooldowns()`, green-when-ready / blue-while-counting): (1) message-matched abilities in `gamestate.cooldownRegistry` (e.g. Mend, monk, 360s — matched leniently across success/partial/fail variants); (2) macro/event-driven ones like **Feign Death** (11s reuse, measured from logs), started by `FeignAttempt` off the player's custom feign emote. Detecting any class ability also infers the class. **Feign banners** (`FeignStatus`: ✓ feigned by success-by-absence, ⚠ failed on a player-gated "fallen to the ground") and a **bind-wound** progress indicator (`Binding`) sit above the cooldowns. Next registry candidates: disciplines, Harm Touch, Lay on Hands.
- **Hybrid** (Pal/SK/Ranger/Bard/Beastlord) → spell timers plus a one-line `skillsSummary` digest.

The panel title (`App.panelTitle`) and content both switch on the category.

## Zone awareness and repop tracking

The `Tracker` also derives zone state from the log (`internal/gamestate/zone.go`): a "You have
entered X" line sets the current zone and looks up its default respawn in
`gamestate.ZoneRespawn` (data in `internal/gamestate/zonetimers.go`, transcribed from the P99
wiki — see `docs/zone-spawn-timers.md`). Every mob death — the player's own ("You have slain X!") *and* group/others'
("X has been slain by &lt;player&gt;!", with a killer-is-mob heuristic to skip
player deaths) — starts a repop timer at the zone default (`Respawns()`). Each
death is its own entry (a `[]respawnEntry`, not name-keyed), so two same-named
mobs dying close together are tracked as distinct spawns rather than one reset.
The repop list splits **group kills** (yours, plus a group-mate's that you got xp
for — `creditGroupKillLocked` flags the just-killed mob when a "You gain (party)
experience" line lands) from **others'** kills; the killer name still shows for
any blow you didn't land yourself. Zoning clears the list; entries purge ~2 min
after they pop. The Damage panel's
**kills/hr** line is also zone-wide and xp-credited only: `ZoneKillStats` counts
"You gain (party) experience" lines since the zone-in (not per-encounter killing
blows) and rates them over time since the first kill. **Click a repop row**
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

## Views and input

`Model.layout()` (`internal/tui/model.go`) computes panel rectangles for the window: a gold banner header, a full-height **Sessions** sidebar, a top-right **Damage meter** beside a **Specials · Avoidance** column, and a bottom row **class panel | [Enemy] | Mob Tracker** — caster/hybrid classes get a dedicated **Enemy** column (CROWD CONTROL + DEBUFFS, i.e. everything cast on mobs) and the class panel becomes "Buffs" + toolkit; pure melee gets two tiles (Skills | Mob); below `enemyColMinW` the Enemy column drops and CC+debuffs fold back into the class panel. (`timerColumn`'s `wantCC/wantDebuffs/wantBuffs` flags pick which sections each panel shows.) Input (all in `Update`): arrow keys / click select a fight, `End` jumps to live, the wheel scrolls the hovered panel, `t`/`tab` cycles theme, `a` toggles audio cues, `Backspace` clears sessions, clicking a buff target dismisses it (hover shows an ✕), clicking a repop row opens an inline editor (digits/`:`, Enter saves a per-(zone,mob) override). A transient status line in the footer shows switch/clear/edit feedback. `ctrl+c` always quits (even mid-edit).

## Gotchas

- **Non-melee must be excluded from `hasDamage`.** Spell lines contain "points of damage", so they would otherwise be parsed by the melee regex into a bogus `"<X> was"` dealer. `hasDamage` filters out `non-melee` and `You have taken`; non-melee is routed to `hasMagic`.
- The player's own crits and casts log under the **character name**, not "You" — the parser remaps the character name to "You" for attribution (hence `DoParse` takes `character`).
- An AoE/PBAoE lands on several mobs from one cast, so `Tracker` keeps the `pending` cast alive across landing emotes (cleared by the cast window or the next cast) and times each affected mob. A `Your target resisted the X spell.` line is surfaced briefly via `Tracker.Resisted` (a red badge), not a timer.
- **Pet → owner attribution** (`internal/gamestate/pet.go`): the *only* reliable ownership signal is `"<Pet> says 'My leader is <Owner>.'"` (`/pet leader`), so the tracker builds a `petOwners` map from every such line for the **whole group** (`PetOwner(name)`), and flags the player's own pet (`PetName()`, owner == `SetCharacter`). The `"...Master."` command replies are **ignored** — they leak into nearby players' logs, so they can't tell whose pet it is (using them mis-attributed group-mates' pets to you). The Damage meter rolls each pet up under its owner's row (`↳ <pet>`); a pet whose owner dealt no damage is credited as a `<owner>'s pet` row. **Insta-clickies** with no matchable spell message are handled by a class-gated registry (`internal/gamestate/clicky.go`, `RegisterClicky`).
