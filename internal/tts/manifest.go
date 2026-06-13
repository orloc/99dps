package tts

import "runtime"

// This file pins the exact offline-TTS artifacts the neural backend downloads on
// first run (download-on-first-run keeps the shipped zip tiny; the ~150 MB
// Kokoro package is fetched once into the user cache, then everything is local).
//
// Engine:  sherpa-onnx prebuilt offline-TTS CLI (Apache-2.0), called as a
//
//	subprocess so the app stays pure-Go / no cgo.
//
// Model:   Kokoro-82M int8 (Apache-2.0) — one ~80 MB model shared by every
//
//	voice, plus a single ~26 MB voices.bin holding all speaker styles, so
//	all voices ship together and the default is just a speaker index.
//
// TODO(tts): pin sha256 for each artifact before release. They're left empty for
// now (download verifies size only, logs a warning) because the checksums must
// be computed from the real downloads on a networked machine. Integrity still
// rests on HTTPS + GitHub until then.
const sherpaVersion = "v1.13.2"

const sherpaRelease = "https://github.com/k2-fsa/sherpa-onnx/releases/download/" + sherpaVersion + "/"
const modelRelease = "https://github.com/k2-fsa/sherpa-onnx/releases/download/tts-models/"

// artifact is one downloadable .tar.bz2 (URL + optional integrity check).
type artifact struct {
	url    string
	sha256 string // "" = unverified (size-only); pin before release
	size   int64  // expected uncompressed-archive size in bytes, 0 = unknown
}

// sherpaBinary is the prebuilt CLI package per OS. Linux uses the static build
// (self-contained binaries, no loose .so); Windows uses the MT (static CRT)
// shared-release build so no Visual C++ redistributable is required. Both bundle
// the ONNX Runtime and include sherpa-onnx-offline-tts(.exe).
var sherpaBinary = map[string]artifact{
	"linux":   {url: sherpaRelease + "sherpa-onnx-" + sherpaVersion + "-linux-x64-static.tar.bz2"},
	"windows": {url: sherpaRelease + "sherpa-onnx-" + sherpaVersion + "-win-x64-shared-MT-Release.tar.bz2"},
}

// kokoroModel is the int8 Kokoro package (model.onnx, voices.bin, tokens.txt,
// espeak-ng-data/, lexicons, rule FSTs). ~150 MB extracted.
var kokoroModel = artifact{url: modelRelease + "kokoro-int8-multi-lang-v1_1.tar.bz2"}

// kokoroVoiceCount is the number of speakers in voices.bin (sid 0..N-1). The
// default cue voice is an English speaker; richer name labels come later (the
// settings screen). v1_1 ships 103 speakers.
const (
	kokoroVoiceCount = 103
	defaultVoiceSID  = 0 // an English speaker; refine once the sid→name map is confirmed
)

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
