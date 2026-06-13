package tts

import (
	"archive/tar"
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// tarball builds an in-memory tar stream from name->content (a trailing "/" in
// the name marks a directory entry).
func tarball(t *testing.T, entries map[string]string) *bytes.Reader {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for name, body := range entries {
		if len(name) > 0 && name[len(name)-1] == '/' {
			if err := tw.WriteHeader(&tar.Header{Name: name, Typeflag: tar.TypeDir, Mode: 0o755}); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := tw.WriteHeader(&tar.Header{Name: name, Typeflag: tar.TypeReg, Mode: 0o644, Size: int64(len(body))}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	return bytes.NewReader(buf.Bytes())
}

func TestExtractTar(t *testing.T) {
	dest := t.TempDir()
	src := tarball(t, map[string]string{
		"pkg/":           "",
		"pkg/model.onnx": "weights",
		"pkg/sub/a.txt":  "hello",
	})
	if err := extractTar(src, dest); err != nil {
		t.Fatalf("extract: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dest, "pkg", "sub", "a.txt"))
	if err != nil {
		t.Fatalf("read extracted: %v", err)
	}
	if string(got) != "hello" {
		t.Errorf("content = %q, want hello", got)
	}
	if _, err := os.Stat(filepath.Join(dest, "pkg", "model.onnx")); err != nil {
		t.Errorf("model.onnx not extracted: %v", err)
	}
}

func TestExtractTarRejectsTraversal(t *testing.T) {
	dest := t.TempDir()
	src := tarball(t, map[string]string{"../escape.txt": "pwned"})
	if err := extractTar(src, dest); err == nil {
		t.Fatal("expected a traversal path to be rejected")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(dest), "escape.txt")); err == nil {
		t.Error("traversal file escaped the destination")
	}
}

func TestSafeJoin(t *testing.T) {
	root := filepath.FromSlash("/tmp/root")
	ok := []string{"a/b.txt", "./c", "pkg/model.onnx"}
	for _, n := range ok {
		if _, err := safeJoin(root, n); err != nil {
			t.Errorf("safeJoin(%q) errored unexpectedly: %v", n, err)
		}
	}
	bad := []string{"../x", "../../etc/passwd", "a/../../x"}
	for _, n := range bad {
		if _, err := safeJoin(root, n); err == nil {
			t.Errorf("safeJoin(%q) should have been rejected", n)
		}
	}
}
