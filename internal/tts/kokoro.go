package tts

import (
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// kokoroEngine is the neural backend. It drives the bundled sherpa-onnx offline
// TTS CLI as a subprocess (keeping the app pure-Go / no cgo) with the Kokoro
// int8 model. Each (voice, text) is synthesized once to a cached WAV, so repeat
// cues — the common case for a small cue vocabulary — replay instantly.
type kokoroEngine struct {
	cli   string     // path to sherpa-onnx-offline-tts(.exe)
	model modelPaths // resolved model file paths
	clips string     // WAV cache dir

	mu  sync.Mutex
	sid int // current speaker index

	once   sync.Once      // lazily starts the playback worker
	queue  chan utterance // normal cues awaiting serialized playback
	urgent chan utterance // urgent cues — drained ahead of the normal queue
}

// utterance is a queued cue: the text plus the length-scale (speed) it's spoken
// at (gentle for buffs, snappier for urgent combat alerts).
type utterance struct{ text, scale string }

type modelPaths struct {
	onnx, voices, tokens, dataDir string
	lexicon, fsts                 string // comma-joined optional extras (may be "")
}

// newKokoro builds the neural engine, resolving the cached assets if present. It
// always returns a non-nil engine; when the assets aren't downloaded yet the
// returned engine is not Available() and cues no-op. Downloading is a separate
// explicit step (EnsureAssets), never done here.
func newKokoro() *kokoroEngine {
	k := &kokoroEngine{sid: defaultSID()}
	engineDir, modelDir, clips, ok := cacheDirs()
	if !ok {
		return k
	}
	cli := findFile(engineDir, ttsCLIName())
	onnx := findModelOnnx(modelDir)
	if cli == "" || onnx == "" {
		return k
	}
	mp, ok := resolveModel(onnx)
	if !ok {
		return k
	}
	k.cli, k.model, k.clips = cli, mp, clips
	return k
}

// Available reports whether the neural voice is downloaded and ready (cli set
// only when both the engine binary and a resolvable model were found).
func (k *kokoroEngine) Available() bool { return k != nil && k.cli != "" }

// Say queues a normal (gentle) cue for playback without blocking the caller.
func (k *kokoroEngine) Say(text string) { k.enqueue(text, kokoroLengthScale, false) }

// SayUrgent queues a snappier cue that jumps ahead of the normal queue.
func (k *kokoroEngine) SayUrgent(text string) { k.enqueue(text, urgentLengthScale, true) }

func (k *kokoroEngine) enqueue(text, scale string, urgent bool) {
	if !k.Available() || text == "" {
		return
	}
	k.once.Do(func() {
		k.queue = make(chan utterance, 16)
		k.urgent = make(chan utterance, 16)
		go k.worker()
	})
	ch := k.queue
	if urgent {
		ch = k.urgent
	}
	select {
	case ch <- utterance{text: text, scale: scale}:
	default: // queue full — drop rather than let stale cues pile up
	}
}

// worker plays queued cues serially (so they never overlap), always draining
// the urgent queue first so a charm-break/resist alert isn't stuck behind a
// gentle buff cue. Each render synthesizes (cached) then blocks on playback.
func (k *kokoroEngine) worker() {
	for {
		select {
		case u := <-k.urgent:
			k.render(u.text, u.scale)
			continue
		default:
		}
		select {
		case u := <-k.urgent:
			k.render(u.text, u.scale)
		case u := <-k.queue:
			k.render(u.text, u.scale)
		}
	}
}

func (k *kokoroEngine) Voices() []Voice { return kokoroVoices }

func (k *kokoroEngine) Voice() string {
	k.mu.Lock()
	defer k.mu.Unlock()
	return strconv.Itoa(k.sid)
}

func (k *kokoroEngine) SetVoice(id string) bool {
	if !validVoice(id) {
		return false
	}
	n, _ := strconv.Atoi(id)
	k.mu.Lock()
	k.sid = n
	k.mu.Unlock()
	return true
}

// defaultSID is the configured default voice's --sid index.
func defaultSID() int { n, _ := strconv.Atoi(defaultVoice); return n }

// validVoice reports whether id is one of the catalog voices.
func validVoice(id string) bool {
	for _, v := range kokoroVoices {
		if v.ID == id {
			return true
		}
	}
	return false
}

// render ensures text is in the clip cache (synthesizing if needed) and plays
// it. Best-effort: a synth/play failure silently no-ops (a cue is never worth
// stalling or crashing for).
func (k *kokoroEngine) render(text, scale string) {
	k.mu.Lock()
	sid := k.sid
	k.mu.Unlock()

	clip := filepath.Join(k.clips, cacheKey(sid, scale, text)+".wav")
	if !fileExists(clip) {
		// synth to a temp file and rename, so a concurrent reader never plays a
		// half-written clip (the cache file appears atomically).
		tmp := clip + ".tmp"
		if err := k.synth(text, sid, scale, tmp); err != nil {
			return
		}
		if err := os.Rename(tmp, clip); err != nil {
			return
		}
	}
	playWav(clip)
}

// synth runs the sherpa CLI to write a WAV for (text, sid, scale).
func (k *kokoroEngine) synth(text string, sid int, scale, out string) error {
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return err
	}
	args := []string{
		"--kokoro-model=" + k.model.onnx,
		"--kokoro-voices=" + k.model.voices,
		"--kokoro-tokens=" + k.model.tokens,
		"--kokoro-data-dir=" + k.model.dataDir,
		"--kokoro-length-scale=" + scale, // gentle for buffs, snappier for alerts
	}
	if k.model.lexicon != "" {
		args = append(args, "--kokoro-lexicon="+k.model.lexicon)
	}
	if k.model.fsts != "" {
		args = append(args, "--kokoro-tts-rule-fsts="+k.model.fsts)
	}
	args = append(args,
		"--sid="+strconv.Itoa(sid),
		"--num-threads=2",
		"--output-filename="+out,
		text, // positional: the text to speak, must come last
	)
	cmd := exec.Command(k.cli, args...)
	hideWindow(cmd) // no-op except on Windows
	return cmd.Run()
}

// cacheKey is a stable filename stem for a (voice, speed, text) clip — the
// length scale is folded in so urgent (faster) and normal cues cache separately
// and a speed change invalidates stale clips.
func cacheKey(sid int, scale, text string) string {
	sum := sha1.Sum([]byte(strconv.Itoa(sid) + "\x00" + scale + "\x00" + text))
	return hex.EncodeToString(sum[:])
}

// EnsureAssets downloads the sherpa CLI + Kokoro model into the cache if absent.
// It transfers ~120 MB on first run, so call it off the UI goroutine. progress,
// if non-nil, is called as (label, bytesDone) where label is "engine" or "voice".
func EnsureAssets(progress func(label string, done int64)) error {
	engineDir, modelDir, _, ok := cacheDirs()
	if !ok {
		return errors.New("no user cache directory")
	}
	bin, ok := binaryForOS()
	if !ok {
		return fmt.Errorf("no prebuilt neural TTS engine for %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	if err := fetchArtifact(bin, engineDir, labeled(progress, "engine")); err != nil {
		return err
	}
	return fetchArtifact(kokoroModel, modelDir, labeled(progress, "voice"))
}

func labeled(fn func(string, int64), label string) func(int64) {
	if fn == nil {
		return nil
	}
	return func(done int64) { fn(label, done) }
}

// Setup downloads the neural assets (if missing), synthesizes a test phrase, and
// plays it — the one-shot way to verify the whole runtime path (download → synth
// → audio) on a real machine. Returns the path of the test WAV. Blocking; intended
// for a `-tts-setup` CLI flow, not the UI.
func Setup(progress func(label string, done int64)) (string, error) {
	if err := EnsureAssets(progress); err != nil {
		return "", err
	}
	k := newKokoro()
	if k == nil {
		return "", errors.New("assets missing after download (unexpected)")
	}
	out := filepath.Join(os.TempDir(), "99dps-tts-test.wav")
	if err := k.synth("Audio cues are ready.", k.sid, kokoroLengthScale, out); err != nil {
		return "", fmt.Errorf("synthesis failed: %w", err)
	}
	playWav(out)
	return out, nil
}

// --- path helpers ---

// cacheDirs returns the engine/model/clips dirs under the user cache, creating
// nothing (callers create on demand).
func cacheDirs() (engine, model, clips string, ok bool) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", "", "", false
	}
	root := filepath.Join(base, "99dps", "tts")
	return filepath.Join(root, "engine"), filepath.Join(root, "model"), filepath.Join(root, "clips"), true
}

// resolveModel fills modelPaths given the model .onnx path. The onnx filename
// varies (model.onnx fp32, model.int8.onnx int8) so it's discovered separately;
// the sibling files have stable names. Folds in optional lexicons / rule FSTs.
func resolveModel(onnx string) (modelPaths, bool) {
	root := filepath.Dir(onnx)
	mp := modelPaths{
		onnx:    onnx,
		voices:  filepath.Join(root, "voices.bin"),
		tokens:  filepath.Join(root, "tokens.txt"),
		dataDir: filepath.Join(root, "espeak-ng-data"),
	}
	if !fileExists(mp.voices) || !fileExists(mp.tokens) {
		return mp, false
	}
	mp.lexicon = joinGlob(root, "lexicon*.txt")
	mp.fsts = joinGlob(root, "*.fst")
	return mp, true
}

// findModelOnnx returns the Kokoro model file under root, tolerating the int8
// name (model.int8.onnx) as well as model.onnx — preferring a "model*" match.
func findModelOnnx(root string) string {
	var best string
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(d.Name(), ".onnx") {
			return nil
		}
		if best == "" || strings.HasPrefix(d.Name(), "model") {
			best = path
		}
		return nil
	})
	return best
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// findFile walks root and returns the path of the first entry named base, or "".
func findFile(root, base string) string {
	var found string
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() && d.Name() == base {
			found = path
			return fs.SkipAll
		}
		return nil
	})
	return found
}

// joinGlob returns the matches of pattern in dir as a comma-separated string,
// sorted for determinism, or "" when there are none.
func joinGlob(dir, pattern string) string {
	matches, _ := filepath.Glob(filepath.Join(dir, pattern))
	sort.Strings(matches)
	out := ""
	for i, m := range matches {
		if i > 0 {
			out += ","
		}
		out += m
	}
	return out
}
