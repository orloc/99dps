//go:build !windows

package loader

// DetectLogDirs auto-detects EverQuest installs. Non-Windows hosts rely on the
// -logdir flag, EQ_LOG_DIR, or the configured default, so detection is a no-op.
func DetectLogDirs() []string { return nil }

// PromptForLogDir interactively confirms or picks a log dir. It uses native
// dialogs, which are only implemented on Windows; elsewhere it returns "" so the
// caller falls back to its non-interactive default.
func PromptForLogDir(found []string) string { return "" }
