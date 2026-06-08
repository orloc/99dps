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

## Data flow (three concurrent pieces)

`launchCLI` (`cli.go`) wires these together and orchestrates graceful shutdown (stop the repaint loop → `App.Close()` the gui → `Tail.Stop()` to end the parser):

1. **`loader.LoadFile(dir)`** — picks the most-recently-modified `eqlog_*.txt` under `dir` and follows it (`loader.Latest` finds it, `loader.Follow` opens the tail). Character is parsed from the filename.
2. **`parser.DoParse(tail, sink, character)`** (goroutine) — for each line, `dispatch` runs a cheap `has*` screen, then one `parse*` regex, then forwards a typed event to a `Sink`. The parser depends on the `Sink` interface, **not** on the `session` package — `*session.SessionManager` satisfies it.
3. **`App.Sync(stop)`** (goroutine) — once per second, `refresh()` snapshots all sessions, resolves the selected one, and repaints. `App.Loop()` runs the blocking gocui main loop on the main goroutine.
4. **`logController.watch`** (goroutine, `cli.go`) — polls `loader.Latest` every few seconds; when a *different* eqlog becomes the most-recently-written (you switched characters in-game), it hot-swaps: opens the new tail from end-of-file, stops the old one (ending its parser goroutine), `Clear`s the manager, and calls `App.SetCharacter` (updates the panel title + flashes a banner). No restart needed.

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
- **`cli/app.go`** holds the gui-coupled `App`: lifecycle, input handlers, and thin `update*` wrappers that fetch panel width via `viewInnerWidth`, call a `render*` function, and push the result onto the gocui loop.
- Tables are **width-aware** — optional columns (Hit%, Crit%, the labelled avoidance table) only appear when they fit, because gocui doesn't wrap and would clip the right edge.
- gocui `OutputNormal` only honours ANSI colors **30–37/40–47** plus bold(1)/underline(4)/reverse(7). Bright codes (90–97) are silently ignored — stay in range.

## Spell timers (`spell` package)

A separate subsystem from DPS: tracks durations of spells the player casts (debuffs on mobs, buffs on self/others). `spell.Load` parses the client's `spells_us.txt` (217 `^`-delimited fields; `-spells` flag or `<logdir>/../spells_us.txt`) into a `Book`; `spell.Tracker` holds the live timers. The parser feeds it (via `DmgParser.observeSpells`): `You begin casting X` → pending; a later line that `EndsWith` the spell's `cast_on_other` emote identifies the **target** (the prefix) and starts a timer of `Spell.DurationSeconds(level)` (the EQ buffduration formula). Caster level **and class** come from `/who` self lines (level-ups give level only) via `DmgParser.parseLevel`. EQ `/who` prints a level-based *title* (`[60 Warlord]`), not the class name, so `common.ClassFromTitle` maps the title→class (see `common/class.go`); the `Tracker` stores it (`SetClass`/`Category`). Timers expire on timeout, the `spell_fades` message, a resist, or the target being slain. The tracker is cleared on a character switch. It degrades gracefully (no panel data) if `spells_us.txt` is absent.

## Class-aware bottom-right panel

The `timers` panel is **class-driven** (`App.updatePanel`), keyed on `tracker.Category()` (`common.Category`, derived from the detected class):
- **Caster** → spell timers (the default; also used until a `/who` reveals the class).
- **Pure melee** (War/Monk/Rogue) → `renderSkills`: the player's activated-skill breakdown (Backstab/Bash/Kick, tracked per-skill in `CombatSession.skills` for the player only) plus hit/crit/avoid. Discipline cooldowns are a planned addition here (blocked on log samples of discipline use).
- **Hybrid** (Pal/SK/Ranger/Bard/Beastlord) → spell timers plus a one-line `skillsSummary` digest.

The panel title (`App.panelTitle`) and content both switch on the category.

## Views and input

`cli/view.go` defines five views (`sessions`, `dmg`, `graph`, `timers`, `shortcuts`) in `vp` with fractional coords translated by `GetScreenDims` (both the `ViewProperties` type and `GetScreenDims` live in `cli/view.go`, keeping the shared `common` package free of the gocui dependency). The `sessions` panel is interactive: arrow keys / click select a fight (which drives the other panels), `End` jumps to live, and the mouse wheel scrolls it (selection scrolls into view; autoscroll is off and origin is managed manually). Keybindings live in `cli/keys.go`.

## Gotchas

- **Non-melee must be excluded from `hasDamage`.** Spell lines contain "points of damage", so they would otherwise be parsed by the melee regex into a bogus `"<X> was"` dealer. `hasDamage` filters out `non-melee` and `You have taken`; non-melee is routed to `hasMagic`.
- The player's own crits and casts log under the **character name**, not "You" — the parser remaps the character name to "You" for attribution (hence `DoParse` takes `character`).
- `go.mod` pins gocui 0.4.0 and hpcloud/tail; don't upgrade casually — the gocui API changed in later forks.
