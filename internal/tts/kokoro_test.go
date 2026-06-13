package tts

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCacheKeyStable(t *testing.T) {
	a := cacheKey(0, "1.1", "Clarity low")
	if a != cacheKey(0, "1.1", "Clarity low") {
		t.Error("cacheKey not stable for identical inputs")
	}
	if a == cacheKey(1, "1.1", "Clarity low") {
		t.Error("cacheKey must differ by voice")
	}
	if a == cacheKey(0, "1.0", "Clarity low") {
		t.Error("cacheKey must differ by speed/scale")
	}
	if a == cacheKey(0, "1.1", "Snare low") {
		t.Error("cacheKey must differ by text")
	}
}

func TestSetVoiceBounds(t *testing.T) {
	k := &kokoroEngine{sid: defaultSID()}
	if !k.SetVoice("5") || k.Voice() != "5" {
		t.Errorf("valid voice rejected; Voice()=%q", k.Voice())
	}
	for _, bad := range []string{"-1", "99999", "abc", ""} {
		if k.SetVoice(bad) {
			t.Errorf("SetVoice(%q) should fail", bad)
		}
	}
	if k.Voice() != "5" {
		t.Errorf("a rejected SetVoice changed the voice to %q", k.Voice())
	}
}

func TestVoicesCount(t *testing.T) {
	k := &kokoroEngine{}
	if got := len(k.Voices()); got != len(kokoroVoices) {
		t.Errorf("Voices() = %d, want %d", got, len(kokoroVoices))
	}
}

func TestResolveModel(t *testing.T) {
	root := t.TempDir()
	onnx := filepath.Join(root, "model.int8.onnx") // int8 naming
	os.WriteFile(onnx, []byte("x"), 0o644)

	// model present but siblings missing → not resolvable
	if _, ok := resolveModel(onnx); ok {
		t.Fatal("resolveModel should fail without voices/tokens")
	}
	os.WriteFile(filepath.Join(root, "voices.bin"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(root, "tokens.txt"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(root, "lexicon-us-en.txt"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(root, "date.fst"), []byte("x"), 0o644)

	mp, ok := resolveModel(onnx)
	if !ok {
		t.Fatal("resolveModel should succeed with siblings present")
	}
	if mp.onnx != onnx {
		t.Errorf("onnx = %q, want %q", mp.onnx, onnx)
	}
	if mp.lexicon == "" {
		t.Error("lexicon glob should have matched lexicon-us-en.txt")
	}
	if mp.fsts == "" {
		t.Error("fst glob should have matched date.fst")
	}
}

func TestFindModelOnnx(t *testing.T) {
	root := t.TempDir()
	deep := filepath.Join(root, "kokoro-int8-en-v0_19")
	os.MkdirAll(deep, 0o755)
	os.WriteFile(filepath.Join(deep, "model.int8.onnx"), []byte("x"), 0o644)
	if got := findModelOnnx(root); got == "" {
		t.Error("findModelOnnx should locate model.int8.onnx in a nested dir")
	}
	if got := findModelOnnx(t.TempDir()); got != "" {
		t.Errorf("findModelOnnx should return \"\" when no .onnx exists, got %q", got)
	}
}

func TestFindFile(t *testing.T) {
	root := t.TempDir()
	deep := filepath.Join(root, "a", "b")
	os.MkdirAll(deep, 0o755)
	os.WriteFile(filepath.Join(deep, "model.onnx"), []byte("x"), 0o644)
	if got := findFile(root, "model.onnx"); got == "" {
		t.Error("findFile should locate a nested file")
	}
	if got := findFile(root, "nope.bin"); got != "" {
		t.Errorf("findFile should return \"\" for a missing file, got %q", got)
	}
}
