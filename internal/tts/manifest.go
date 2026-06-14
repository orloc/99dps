package tts

import "runtime"

// This file pins the exact offline-TTS artifacts the neural backend downloads on
// first run (download-on-first-run keeps the shipped zip tiny; ~120 MB is fetched
// once into the user cache, then everything is local).
//
//   - Engine: sherpa-onnx prebuilt offline-TTS CLI (Apache-2.0), called as a
//     subprocess so the app stays pure-Go / no cgo. ~25 MB (shared build).
//   - Model:  Kokoro int8 English (v0.19, Apache-2.0) — ~98 MB, 11 English voices
//     (see kokoroVoices). English-only keeps it small and the picker describable.
//
// TODO(tts): pin sha256 for each artifact before release. They're left empty for
// now (no integrity check is performed) because the checksums must be computed
// from the real downloads on a networked machine. Integrity rests on HTTPS +
// GitHub until then.
const sherpaVersion = "v1.13.2"

const sherpaRelease = "https://github.com/k2-fsa/sherpa-onnx/releases/download/" + sherpaVersion + "/"
const modelRelease = "https://github.com/k2-fsa/sherpa-onnx/releases/download/tts-models/"

// artifact is one downloadable .tar.bz2 (URL + optional sha256 integrity check).
type artifact struct {
	url    string
	sha256 string // "" = no integrity check (HTTPS only); pin before release
}

// sherpaBinary is the prebuilt CLI package per OS — the shared build on both
// (binary + ONNX Runtime shared lib alongside). The Linux static build is
// avoided: it's ~336 MB vs ~25 MB shared. Windows uses the MT (static CRT)
// build so no Visual C++ redistributable is required. Both include
// sherpa-onnx-offline-tts(.exe). Download totals: ~123 MB (engine + model).
var sherpaBinary = map[string]artifact{
	"linux":   {url: sherpaRelease + "sherpa-onnx-" + sherpaVersion + "-linux-x64-shared.tar.bz2"},
	"windows": {url: sherpaRelease + "sherpa-onnx-" + sherpaVersion + "-win-x64-shared-MT-Release.tar.bz2"},
}

// kokoroModel is the int8 English Kokoro package (~98 MB: model.onnx, voices.bin,
// tokens.txt, espeak-ng-data/, lexicons). English-only (v0_19) keeps the
// download small and the voice list describable — see kokoroVoices.
var kokoroModel = artifact{url: modelRelease + "kokoro-int8-en-v0_19.tar.bz2"}

// kokoroVoices is the curated, described English voice catalog (Kokoro v0.19).
// ID is the sherpa --sid index. NOTE: the sid order follows the documented v0.19
// voicepack order; if a preview doesn't match its label, the index needs a swap
// (the names/descriptions are correct regardless). Default is af_bella.
var kokoroVoices = []Voice{
	{ID: "0", Name: "af", Desc: "American · female · neutral (default blend)"},
	{ID: "1", Name: "af_bella", Desc: "American · female · warm, expressive (recommended)"},
	{ID: "2", Name: "af_sarah", Desc: "American · female · clear"},
	{ID: "3", Name: "am_adam", Desc: "American · male"},
	{ID: "4", Name: "am_michael", Desc: "American · male · steady"},
	{ID: "5", Name: "bf_emma", Desc: "British · female · warm"},
	{ID: "6", Name: "bf_isabella", Desc: "British · female"},
	{ID: "7", Name: "bm_george", Desc: "British · male"},
	{ID: "8", Name: "bm_lewis", Desc: "British · male · deep"},
	{ID: "9", Name: "af_nicole", Desc: "American · female · soft (headphones)"},
	{ID: "10", Name: "af_sky", Desc: "American · female · bright"},
}

// defaultVoice is the sid used until the user picks one (af_bella).
const defaultVoice = "1"

// kokoroLengthScale slows the voice slightly (larger = slower) for a gentler,
// sweeter delivery than the default 1.0. It's part of the clip cache key, so
// changing it re-synthesizes cached cues.
const kokoroLengthScale = "1.1"

// urgentLengthScale is the snappier delivery for time-critical combat alerts
// (charm break, resist) — at normal speed, it reads as more urgent than the
// gentle buff cadence.
const urgentLengthScale = "1.0"

// binaryForOS reports the sherpa CLI package for the current OS, and whether one
// is defined (only linux/windows amd64 are supported today).
func binaryForOS() (artifact, bool) {
	a, ok := sherpaBinary[runtime.GOOS]
	return a, ok
}

// ttsCLIName is the offline-TTS executable inside the sherpa package.
func ttsCLIName() string {
	if runtime.GOOS == "windows" {
		return "sherpa-onnx-offline-tts.exe"
	}
	return "sherpa-onnx-offline-tts"
}
