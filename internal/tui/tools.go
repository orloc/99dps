package tui

import tea "github.com/charmbracelet/bubbletea"

// The Tools tab is a home for little helper widgets — quick utilities that aren't
// part of the live meter (e.g. a /random winner picker for loot rolls). It starts
// empty; widgets get added here over time.

// updateTools handles input while the Tools tab is focused. No widgets yet, so it
// just consumes keys (tab/number navigation is handled earlier, in Update).
func (m Model) updateTools(msg tea.Msg) (tea.Model, tea.Cmd) {
	return m, nil
}

// toolsView renders the Tools tab body. Placeholder until the first widget lands.
func (m Model) toolsView(th theme, w int) string {
	return lines(
		paint(th, th.accent, "Tools", w),
		"",
		paint(th, th.dim, "A home for little helper widgets.", w),
		paint(th, th.dim, "Coming soon — e.g. a /random winner picker for rolls.", w),
	)
}
