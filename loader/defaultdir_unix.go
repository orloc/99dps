//go:build !windows

package loader

// DefaultLogDir on Unix is the author's Wine/P99 install — overridable by the
// -logdir flag or the EQ_LOG_DIR env var.
const DefaultLogDir = "/mnt/storage/p99/drive_c/EQ2Lite/Logs"
