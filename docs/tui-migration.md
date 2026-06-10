# Porting 99dps to Bubble Tea + Lipgloss

A concrete, phased plan to replace the gocui UI with a Charm (Bubble Tea +
Lipgloss + Bubbles) UI. Proven by the spike in `spike/bubbletea-damage/`
(true-color themes, gradient bars, free-scrolling viewports).

## Why

gocui (on `termbox`) caps us at: **8 colors** (the "gold" is ANSI yellow), no
styling primitives (we hand-pad with `headerBar`/`padTo`), manual layout +
origins (the resize geometry race, the scroll off-by-ones we kept fixing), and
character-only bars. Bubble Tea + Lipgloss gives: 24-bit themeable color,
declarative styled panels, a `viewport` that scrolls for free, and a framework
that owns geometry (the race becomes structurally impossible).

## What stays vs. what changes

**Untouched — the entire data core** (this is the payoff of the earlier refactor):
`internal/combat`, `internal/eqclass`, `internal/gamestate`, `internal/loader`,
`internal/parser`, `internal/session`. The UI only *reads* their snapshots and
getters. Zero changes, zero risk to parsing/correctness.

**Rewritten** — `internal/cli` (gocui) → `internal/tui` (Bubble Tea). The
*rendering logic* (ranking, width-awareness, number formatting) ports; the gocui
*mechanics* are **deleted** because the framework does them: `SetOrigin`,
`clampScroll`, `lineCount`, `GetScreenDims`, `ViewProperties`, `enchanterCols`,
the single-`g.Update` race fix, the `shuttingDown` guard, the manual
`viewInnerWidth` actual-vs-static logic.

**Deps:** add `bubbletea`, `lipgloss`, `bubbles` (+ `termenv` transitively);
drop `gocui` + `termbox-go` at cutover.

## Target architecture (`internal/tui/`)

- `model.go` — the root `tea.Model`: read-only `*session.SessionManager` +
  `*gamestate.Tracker`, a `viewport` per scrollable panel, selection index,
  active theme, window size, the TTS `speaker`.
- `update.go` — `Update`: `tickMsg` (re-snapshot once/sec), key/mouse,
  `tea.WindowSizeMsg`, `characterSwitchedMsg`.
- `view.go` — `View`: compose the layout with lipgloss (`JoinHorizontal` /
  `JoinVertical`) — Now box + Sessions sidebar on the left, Damage | Graph top,
  Timers | [CC] | Mob-Tracker bottom, shortcuts bar.
- `panels.go` (or one file per panel) — `snapshot → styled string` functions,
  porting the `render*` logic to lipgloss styles.
- `theme.go` — `Theme` struct + registry (Kunark Gold, Velious Ice, …) + persist
  the choice via `os.UserConfigDir()` (same pattern as the saved log dir).
- `bars.go` — the gradient / sub-cell bar helper from the spike.
- `msgs.go` — `tickMsg`, `characterSwitchedMsg`, etc.

## Data flow (Bubble Tea)

- The parser goroutine → `SessionManager` is **unchanged**.
- `Model.Init` returns `tea.Tick(1s)` → `tickMsg` → `Update` re-reads
  `sm.All()` + tracker getters and refreshes the viewports → `View` renders.
  (Mirrors today's 1/sec `App.Sync` repaint — but no manual loop, no race.)
- `logController.switchTo` → `program.Send(characterSwitchedMsg{...})` →
  `Update` clears + retitles (replaces `App.SetCharacter`).
- `q`/`ctrl+c` → `tea.Quit`; the launcher then `Tail.Stop()`s etc. (graceful
  shutdown; Bubble Tea restores the terminal on exit).

## Phases (each shippable; gocui stays the default until cutover)

A `-ui gocui|tui` flag selects the UI during transition, so we can run them
side-by-side and only cut over at parity.

- **Phase 0 — skeleton + live Damage panel** (behind `-ui tui`): the `tui`
  package, `Model`, `tick → snapshot`, the Damage panel rendered from **real**
  session data in the themed style, with viewport scroll. Deliverable:
  `99dps -ui tui` shows a live, themed, scrollable Damage panel.
- **Phase 1 — all panels as lipgloss views:** Graph (gradient bars), Sessions
  sidebar, Now box, Spell Timers (countdown bars + pinned crowd-control), Mob
  Tracker, shortcuts bar.
- **Phase 2 — interaction:** session select (↑/↓/click/End), per-panel wheel
  scroll (viewport), click-to-dismiss a buff, click-to-edit a repop timer, TTS
  toggle.
- **Phase 3 — responsive layout:** the 2×2 grid + the enchanter 3-column bottom
  row, adaptive to window size via lipgloss min-widths (replaces `view.go`'s
  fractional math *and* the `enchanterCols` floor logic).
- **Phase 4 — themes:** registry + a cycle key + persistence; ship Kunark Gold /
  Velious Ice / Spirit Crimson (+ whatever else).
- **Phase 5 — lifecycle parity:** character hot-swap messages, graceful
  shutdown, and a side-by-side parity check against gocui.
- **Phase 6 — cutover:** make `tui` the default, delete `internal/cli` (gocui),
  drop the `gocui` + `termbox-go` deps, update `makefile` / `CLAUDE.md` /
  `docs`. Tag a rollback point first.

## Reuse map (file → fate)

| gocui file | fate |
|---|---|
| `internal/cli/render.go` | logic ports to `tui` panel funcs in lipgloss; manual ANSI/`padTo`/`headerBar`/`sectionHeader`/`lineCount` deleted |
| `internal/cli/app.go` (refresh/update*/scroll/select) | → `model.go`/`update.go`; viewports replace scroll math; the `g.Update` race fix + `shuttingDown` guard become moot |
| `internal/cli/view.go` (layout, `GetScreenDims`, `enchanterCols`) | → `view.go` lipgloss layout; fractional math deleted |
| `internal/cli/input.go` (handlers, `clampScroll`, `ensureVisible`) | → `update.go` key/mouse cases; clamp/ensure deleted |
| `internal/cli/keys.go` | → `update.go` key map |
| `internal/cli/speech*.go` | **reused as-is** (TTS is independent of the UI) |
| `cmd/99dps/cli.go` | adapted: launch the chosen UI, feed switches via `program.Send`, drive refresh by tick |

## Effort & risk

- **Effort: medium.** The core is untouched; `internal/cli` (~2.5k LOC) becomes
  `internal/tui` and likely **shrinks** — the framework deletes the manual
  layout/scroll/geometry code we kept patching. Several focused sessions.
- **Risk: low–medium.** The Elm-style Model/Update/View is the learning curve.
  Bubble Tea has solid Windows console support (good for the friend build). The
  `-ui` flag de-risks (compare at parity, cut over only then). No risk to the
  parsing/session/gamestate core.
- **Mitigations:** keep gocui until parity; keep every phase build+lint+test
  green; the spike already validated rendering, scrolling, and themes.
