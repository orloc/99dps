//go:build windows

package loader

// DefaultLogDir on Windows is the "Logs" folder relative to the working
// directory — drop 99dps.exe in your EverQuest folder and run it, or point it
// anywhere with the -logdir flag / EQ_LOG_DIR env var. EQ installs vary by
// machine, so there's no reliable absolute default; the relative path matches
// the standard <EQ>\Logs layout.
const DefaultLogDir = `Logs`
