# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

A terminal DPS meter for classic EverQuest (P99). It tails the active EQ log file, parses combat lines as they appear, and renders live per-dealer damage, accuracy/avoidance, crits, specials, spell estimates, and a damage-bar graph in a `gocui` TUI.

Go 1.25. Tests live next to the code (`parser`, `session`, `common`, `cli`); run `go test ./...`.

## Commands

- `make build` — build, keeping the `99dps` binary (`go build -o 99dps -ldflags="-s -w" .`).
- `make` — `build` then `clean` (builds, then deletes the binary; useful as a compile check).
- `go run . -logdir <path>` — fastest dev iteration. Log dir also reads `EQ_LOG_DIR`, else `loader.DefaultLogDir`.
- `go test ./...` — all tests. Single test: `go test ./parser -run TestParseCast`. Coverage: `go test ./... -cover`.
- `gofmt -l .` must be empty; `go vet ./...` must be clean before finishing a change.
- `make windows` — cross-compile `99dps.exe` (`GOOS=windows GOARCH=amd64`). Pure Go / no cgo, so it builds from any host.

## Cross-platform (Windows)

The app is portable: pure-Go deps (no cgo), `gocui`→`termbox-go` which renders via the Win32 console API (so the ANSI SGR codes the renderer emits are interpreted by gocui, not the terminal — colors/mouse work in `conhost` or Windows Terminal), and CRLF is already stripped (`TrimRight(…, "\r\n")`). Platform-specific pieces are isolated by build tags:
- **`loader/defaultdir_{unix,windows}.go`** — `DefaultLogDir`. The EQ folder layout is identical on every OS, so once the log dir is known everything else (e.g. `spells_us.txt` at `<logdir>/../spells_us.txt`) is located relative to it.
- **Log-dir resolution** (`resolveLogDir` in `99dps.go`): `-logdir` flag (saved) → `EQ_LOG_DIR` → saved choice (`os.UserConfigDir()/99dps/logdir.txt`) → platform default if it already holds logs → **auto-detect + prompt**. Detection (`loader/locate.go` core + `locate_{windows,other}.go`) scans common install spots (`C:\P99`, Sony/Daybreak dirs, Desktop/Downloads/Games/My Games, the exe's own folder) one level deep for an EQ marker (`eqgame.exe`/`eqclient.ini`/`spells_us.txt` or `eqlog_*.txt`). On Windows it then **confirms a find via a native Yes/No box** or **opens a folder picker** (both shelled out to PowerShell WinForms) and **remembers the choice**. Non-Windows hosts skip detection/prompts (no-op stubs) and fall back to the default, preserving prior behavior.
- **`cli/speech_{unix,windows}.go`** — TTS engine. Unix probes `spd-say`/`espeak`; Windows uses built-in SAPI via `powershell … System.Speech` (hidden window). No engine → cues silently no-op.
The Linux-only log-rotation tooling (`scripts/`, systemd) has no Windows equivalent; on Windows logs just grow (or wire a Task Scheduler + PowerShell job).

## Data flow (three concurrent pieces)

`launchCLI` (`cli.go`) wires these together and orchestrates graceful shutdown (stop the repaint loop → `App.Close()` the gui → `Tail.Stop()` to end the parser):

1. **`loader.LoadFile(dir)`** — picks the most-recently-modified `eqlog_*.txt` under `dir` and follows it (`loader.Latest` finds it, `loader.Follow` opens the tail). Character is parsed from the filename.
2. **`parser.DoParse(tail, sink, character)`** (goroutine) — for each line, `dispatch` runs a cheap `has*` screen, then one `parse*` regex, then forwards a typed event to a `Sink`. The parser depends on the `Sink` interface, **not** on the `session` package — `*session.SessionManager` satisfies it.
3. **`App.Sync(stop)`** (goroutine) — once per second, `refresh()` snapshots all sessions, resolves the selected one, and repaints. `App.Loop()` runs the blocking gocui main loop on the main goroutine.
4. **`logController.watch`** (goroutine, `cli.go`) — polls `loader.Latest` every few seconds; when a *different* eqlog becomes the most-recently-written (you switched characters in-game), it hot-swaps: opens the new tail from end-of-file, stops the old one (ending its parser goroutine), `Clear`s the manager + tracker, replays the new log into the tracker only (`parser.RebuildTrackerFromFile` — recovers active spell timers / class / zone, since the live tail starts at end-of-file), and calls `App.SetCharacter` (updates the panel title + flashes a banner). No restart needed.

## Parsing model and the central limitation

EQ logs carry **no entity IDs and no caster on spell damage**. Two consequences shape the whole design:

- Two simultaneously-alive same-named mobs (`a saltwater croc`) are textually identical — they cannot be told apart.
- Spell/proc/DoT damage logs in passive voice (`X was hit by non-melee for N points of damage`) — target and amount only, **never who cast it**. So there is no spell-DPS leaderboard, even for the player.

Line categories in `dispatch` (order matters — see the `hasDamage` gotcha below):
`hasDamage`→`Apply` (melee), `hasSwing`→`ApplySwing` (miss/dodge/parry/block avoidance), `hasCrit`→`ApplyCrit`, `hasMagic`→`ApplyMagic` (non-melee), `hasEvent`→`ApplyEvent` (kill/xp/death; party-vs-solo xp distinguished). Combat verbs are a fixed alternation in `COMBAT_VERB_STRING`. Cast lines (`You begin casting X`) are **not** a `Sink` event — `DmgParser.observeSpells` handles them separately, feeding the spell-timer subsystem (see below).

## Session model and attribution

`SessionManager` holds an append-only `[]*CombatSession` and an `activeSession` index.

- **Segmentation is adaptive-cadence** (`activeForLocked`). Combat *exchanges* — melee `Apply`, `ApplyMagic`, and `ApplySwing` — drive it: a fight rolls to a new session when the gap since the last exchange exceeds `segPulseK × pulse`, clamped to `[segGapFloor, segGapCeil]` seconds, where `pulse` is an EWMA of recent inter-exchange gaps. Dense melee pins the threshold to the floor; slow/sparse caster fights widen it toward the ceiling. `ApplyCrit` and the non-death `ApplyEvent` cases are **annotate-only** (kills/xp/crits are punctuation, never boundaries), so a multi-mob pull with several kills stays one encounter; death and zone/camp are the exception — they force a hard boundary. Constants are tuned against real logs — see their comment in `session/sessionManager.go`.
- **Damage is keyed by dealer** in `aggressors map[string]DamageStat` (dealer name with spaces→underscores as the key).
- **The session *name* comes from the target, not the dealer** — `Name()` picks the enemy taking the most damage (the thing being fought), falling back to the heaviest non-player dealer, then "Solo".
- **Offense vs defense are separate axes** (`recordOutcome`): an attacker's accuracy counts only hit/miss; dodge/parry/block/riposte are the *defender's* doing and only land in the defender's tally (the avoidance table). Rune absorbs are tracked but excluded from `Avoided()` (the blow connected).
- **Spell handling**: `magicTotal` is the unattributed enemy non-melee total (folded into the encounter total) and shown as a single "spells (n/a)" line — EQ logs name no caster, so there is no honest per-spell DPS split. Per-spell *duration* tracking lives in the separate spell-timer subsystem (below), not on `CombatSession`.
- **Per-dealer rows keep only aggregates** (`DamageStat`: `Dealer/Total/Hits/FirstTime/LastTime/Special*`), never the individual hits — so a snapshot is a flat value copy and memory stays bounded over a long raid. `Name()` reads a session-level `targets map[string]int` (damage summed per enemy) rather than rescanning hits.

## Concurrency (snapshot pattern)

`SessionManager` owns a single `sync.RWMutex`. Every public method locks at its boundary (writers take `Lock`, `Current`/`All`/`Len` take `RLock`). Readers never touch live state: `Current()`/`All()` return **deep snapshots** (`CombatSession.snapshot()` `maps.Clone`s every map; all map values are pure value types, so a shallow clone is a full copy), so the UI and all `CombatSession` getters/`render*` functions operate lock-free on owned copies. When adding state to `CombatSession`, clone it in `snapshot()` (and keep its map values copy-safe — no pointers/slices that alias live state). `App` guards its own selection/scroll state with a separate `mu`.

## Rendering layer

- **`cli/render.go`** is the pure layer: free functions `data → string` (`renderDamage`, `renderSessions`, `renderAvoidance`, `renderBars`, formatters). No gui state, so they're unit-tested directly.
- **`cli/app.go`** holds the gui-coupled `App`: lifecycle and the thin `update*` wrappers that fetch panel width via `viewInnerWidth`, call a `render*` function, and push the result onto the gocui loop. **`cli/input.go`** holds the keybinding/mouse handler methods (bound in `keys.go`) and the selection/scroll bookkeeping they drive.
- Tables are **width-aware** — optional columns (Hit%, Crit%, the labelled avoidance table) only appear when they fit, because gocui doesn't wrap and would clip the right edge.
- gocui `OutputNormal` only honours ANSI colors **30–37/40–47** plus bold(1)/underline(4)/reverse(7). Bright codes (90–97) are silently ignored — stay in range.

## Spell timers and live state (`gamestate` package)

A separate subsystem from DPS: tracks durations of spells the player casts (debuffs on mobs, buffs on self/others). `gamestate.Load` parses the client's `spells_us.txt` (217 `^`-delimited fields; `-spells` flag or `<logdir>/../spells_us.txt`) into a `Book`; `gamestate.Tracker` holds the live timers. The `Tracker` is the package's central stateful object — fed one log line at a time via `Observe` (a deliberate *fan-out*: one line can matter to several subsystems, e.g. a kill both expires the victim's debuffs and records a repop) and read lock-free by the UI. Its state is grouped into composed, caller-locked sub-trackers under one mutex: `cooldownTracker` (reuse/feign/bind, `cooldown.go`), `zoneTracker` (zone/repops/kills, `zone.go`), and `canniMeter` (`canni.go`); spell timers + class/level stay on the core (`tracker.go`). Subsystems that detect the class return it for `inferClassLocked` to apply (a `/who` title always wins). The parser feeds it (via `DmgParser.observeSpells`): `You begin casting X` → pending; a later line that `EndsWith` the spell's `cast_on_other` emote identifies the **target** (the prefix) and starts a timer of `Spell.DurationSeconds(level)` (the EQ buffduration formula). Caster level **and class** come from `/who` self lines (level-ups give level only) via `DmgParser.parseLevel`. EQ `/who` prints a level-based *title* (`[60 Warlord]`), not the class name, so `common.ClassFromTitle` maps the title→class (see `common/class.go`); the `Tracker` stores it (`SetClass`/`Category`). Timers expire on timeout, the `spell_fades` message, a resist, or the target being slain. **Crowd control** (mez + charm) is flagged on the spell (`Spell.Mez` from the `mesmeriz`/`enthrall` landing emote; `Spell.Charm`) and rendered in a pinned **CROWD CONTROL** section at the top of the timer panel, apart from buffs/debuffs. A mezzed mob breaks with no log message when it takes damage, so the parser calls `Tracker.BreakMezOnTarget` on every damage line (names normalized — the mez emote drops the article that the damage line keeps). The tracker is cleared on a character switch. It degrades gracefully (no panel data) if `spells_us.txt` is absent.

## Class-aware bottom-right panel

The `timers` panel is **class-driven** (`App.updatePanel`), keyed on `tracker.Category()` (`common.Category`, derived from the detected class):
- **Caster** → spell timers (the default; also used until a `/who` reveals the class).
- **Pure melee** (War/Monk/Rogue) → `renderSkills`: the player's activated-skill breakdown plus hit/crit/avoid. EQ logs special attacks with a *generic* verb, so the bucket is the most we can recover (`CombatSession.skills`, player-only via `playerSkill`): every kick variant logs "kick", every monk special strike (Eagle Strike / Tiger Claw / Dragon Punch) logs "strike", hand-to-hand auto-attack is "punch", weapon is "crush". `displaySkillName`/`skillRelevant` apply class+level best-guesses at render time (a 30+ monk's "kick" → "Flying Kick"; "strike" shown only for monks).
- **Cooldown timers** sit above the skills breakdown. Two trigger paths feed `Tracker.cooldowns` (a name→expiry map rendered by `Cooldowns()`, green-when-ready / blue-while-counting): (1) message-matched abilities in `gamestate.cooldownRegistry` (e.g. Mend, monk, 360s — matched leniently across success/partial/fail variants); (2) macro/event-driven ones like **Feign Death** (11s reuse, measured from logs), started by `FeignAttempt` off the player's custom feign emote. Detecting any class ability also infers the class. **Feign banners** (`FeignStatus`: ✓ feigned by success-by-absence, ⚠ failed on a player-gated "fallen to the ground") and a **bind-wound** progress indicator (`Binding`) sit above the cooldowns. Next registry candidates: disciplines, Harm Touch, Lay on Hands.
- **Hybrid** (Pal/SK/Ranger/Bard/Beastlord) → spell timers plus a one-line `skillsSummary` digest.

The panel title (`App.panelTitle`) and content both switch on the category.

## Zone awareness and repop tracking

The `Tracker` also derives zone state from the log (`gamestate/zone.go`): a "You have
entered X" line sets the current zone and looks up its default respawn in
`common.ZoneRespawn` (data in `common/zonetimers.go`, transcribed from the P99
wiki — see `docs/zone-spawn-timers.md`). Every mob death — the player's own ("You have slain X!") *and* group/others'
("X has been slain by &lt;player&gt;!", with a killer-is-mob heuristic to skip
player deaths) — starts a repop timer at the zone default (`Respawns()`). Each
death is its own entry (a `[]respawnEntry`, not name-keyed), so two same-named
mobs dying close together are tracked as distinct spawns rather than one reset.
Zoning clears the list; entries purge ~2 min after they pop. The Damage panel's
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

`cli/view.go` defines the views (`status`, `sessions`, `dmg`, `graph`, `timers`, `cc`, `repops`, `shortcuts`) — the bottom-row split (Spell Timers | [Crowd Control] | Mob Tracker) is placed dynamically in `Layout`: enchanters get the dedicated `cc` column, everyone else gets two tiles and no `cc` view in `vp` with fractional coords translated by `GetScreenDims` (both the `ViewProperties` type and `GetScreenDims` live in `cli/view.go`, keeping the shared `common` package free of the gocui dependency). The `sessions` panel is interactive: arrow keys / click select a fight (which drives the other panels), `End` jumps to live, and the mouse wheel scrolls it (selection scrolls into view; autoscroll is off and origin is managed manually). Keybindings live in `cli/keys.go`.

## Gotchas

- **Non-melee must be excluded from `hasDamage`.** Spell lines contain "points of damage", so they would otherwise be parsed by the melee regex into a bogus `"<X> was"` dealer. `hasDamage` filters out `non-melee` and `You have taken`; non-melee is routed to `hasMagic`.
- The player's own crits and casts log under the **character name**, not "You" — the parser remaps the character name to "You" for attribution (hence `DoParse` takes `character`).
- `go.mod` pins gocui 0.4.0 and hpcloud/tail; don't upgrade casually — the gocui API changed in later forks.
