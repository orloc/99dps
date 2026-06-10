package loader

import (
	"strings"
	"testing"
)

func TestConfirmFolderScript(t *testing.T) {
	s := confirmFolderScript(`C:\Games\P99`)

	// real line breaks come from the .NET helper, NOT a literal backtick escape
	// (which a single-quoted PS string would print verbatim — the original bug).
	if !strings.Contains(s, "[Environment]::NewLine") {
		t.Errorf("expected [Environment]::NewLine for line breaks:\n%s", s)
	}
	if strings.Contains(s, "`n") {
		t.Errorf("script must not rely on a backtick escape in a single-quoted string:\n%s", s)
	}
	// the path is embedded and the dialog buttons are requested
	if !strings.Contains(s, `C:\Games\P99`) || !strings.Contains(s, "'YesNo'") {
		t.Errorf("script missing path or YesNo buttons:\n%s", s)
	}
}

func TestPsQuoteEscapesSingleQuotes(t *testing.T) {
	// a path with an apostrophe must have its quote doubled so the PS literal
	// can't be broken out of.
	if got := psQuote(`C:\O'Brien\EQ`); got != `C:\O''Brien\EQ` {
		t.Errorf("psQuote = %q, want doubled quote", got)
	}
	// the confirm script carries the escaped form, never a lone quote that would
	// terminate the literal early.
	s := confirmFolderScript(`C:\O'Brien\EQ`)
	if !strings.Contains(s, `C:\O''Brien\EQ`) {
		t.Errorf("confirm script should embed the escaped path:\n%s", s)
	}
}

func TestPickFolderScript(t *testing.T) {
	s := pickFolderScript()
	for _, want := range []string{"FolderBrowserDialog", "ShowDialog", "SelectedPath"} {
		if !strings.Contains(s, want) {
			t.Errorf("pick script missing %q:\n%s", want, s)
		}
	}
}
