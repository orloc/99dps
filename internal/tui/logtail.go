package tui

import (
	"sync"

	"99dps/internal/cues"
)

// logTailCap bounds the live-log ring: about this many recent rows (raw lines +
// fired cues) are kept; older ones age out.
const logTailCap = 4000

type logKind uint8

const (
	logRaw logKind = iota // a raw log line
	logCue                // a cue the engine fired
)

// LogEntry is one row of the live-log sheet: a raw log line or a fired cue, in
// arrival order. At is the log timestamp (raw) or wall-clock (cue), unix seconds.
type LogEntry struct {
	At     int64
	Kind   logKind
	Text   string
	Urgent bool // a cue that used the urgent (queue-jumping) voice
}

// LogTail is a thread-safe ring of recent log lines and fired cues, merged in
// arrival order. The parse goroutine appends raw lines (via Tap); the UI
// goroutine appends cues (via the cueRecorder); the renderer reads a snapshot.
// Created by the program and shared with the host so the parser can feed it.
type LogTail struct {
	mu  sync.Mutex
	buf []LogEntry
	seq uint64 // total ever appended — cheap change-detection for the renderer
}

// NewLogTail builds an empty ring.
func NewLogTail() *LogTail { return &LogTail{buf: make([]LogEntry, 0, logTailCap)} }

func (l *LogTail) add(e LogEntry) {
	if l == nil {
		return
	}
	l.mu.Lock()
	if len(l.buf) >= logTailCap {
		// drop the oldest quarter in one shift rather than memmoving every append
		l.buf = append(l.buf[:0], l.buf[logTailCap/4:]...)
	}
	l.buf = append(l.buf, e)
	l.seq++
	l.mu.Unlock()
}

// AddCue records a fired cue (urgent = the queue-jumping voice).
func (l *LogTail) AddCue(text string, at int64, urgent bool) {
	l.add(LogEntry{At: at, Kind: logCue, Text: text, Urgent: urgent})
}

// Tap returns the parser RawTap-shaped callback that records each raw line, or
// nil when the ring is nil (so the parser's nil-tap check disables it).
func (l *LogTail) Tap() func(line string, at int64) {
	if l == nil {
		return nil
	}
	return func(line string, at int64) { l.add(LogEntry{At: at, Kind: logRaw, Text: line}) }
}

// Snapshot returns up to the last n entries (oldest→newest) plus the running
// seq, as an owned copy the renderer can read lock-free.
func (l *LogTail) Snapshot(n int) ([]LogEntry, uint64) {
	if l == nil {
		return nil, 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	start := 0
	if n > 0 && len(l.buf) > n {
		start = len(l.buf) - n
	}
	out := make([]LogEntry, len(l.buf)-start)
	copy(out, l.buf[start:])
	return out, l.seq
}

// cueRecorder is the cues.Speaker the model hands the engine each tick: it
// records every fired cue into the LogTail (for the Live Log tab) and forwards
// to the real speaker so it's still spoken. Flush is forwarded (audio queue
// control), never recorded — it isn't a cue.
type cueRecorder struct {
	out cues.Speaker // the real tts engine (may be nil if TTS is unavailable)
	lt  *LogTail
	at  int64 // this tick's timestamp, stamped on each recorded cue
}

func (r cueRecorder) Say(s string) {
	r.lt.AddCue(s, r.at, false)
	if r.out != nil {
		r.out.Say(s)
	}
}

func (r cueRecorder) SayUrgent(s string) {
	r.lt.AddCue(s, r.at, true)
	if r.out != nil {
		r.out.SayUrgent(s)
	}
}

func (r cueRecorder) Flush() {
	if r.out != nil {
		r.out.Flush()
	}
}
