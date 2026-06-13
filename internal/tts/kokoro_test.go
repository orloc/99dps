package tts

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCacheKeyStable(t *testing.T) {
	a := cacheKey(0, "Clarity low")
	if a != cacheKey(0, "Clarity low") {
		t.Error("cacheKey not stable for identical inputs")
	}
	if a == cacheKey(1, "Clarity low") {
		t.Error("cacheKey must differ by voice")
	}
	if a == cacheKey(0, "Snare low") {
		t.Error("cacheKey must differ by text")
	}
}

func TestSetVoiceBounds(t *testing.T) {
	k := &kokoroEngine{sid: defaultVoiceSID}
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
	if got := len(k.Voices()); got != kokoroVoiceCount {
		t.Errorf("Voices() = %d, want %d", got, kokoroVoiceCount)
	}
}

func TestResolveModel(t *testing.T) {
	root := t.TempDir()
	// missing core files → not resolvable
	if _, ok := resolveModel(root); ok {
		t.Fatal("resolveModel should fail without core files")
	}
	for _, f := range []string{"model.onnx", "voices.bin", "tokens.txt"} {
		if err := os.WriteFile(filepath.Join(root, f), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	os.WriteFile(filepath.Join(root, "lexicon-us-en.txt"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(root, "date.fst"), []byte("x"), 0o644)

	mp, ok := resolveModel(root)
	if !ok {
		t.Fatal("resolveModel should succeed with core files present")
	}
	if mp.lexicon == "" {
		t.Error("lexicon glob should have matched lexicon-us-en.txt")
	}
	if mp.fsts == "" {
		t.Error("fst glob should have matched date.fst")
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
