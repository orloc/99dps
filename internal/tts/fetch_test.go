package tts

import (
	"archive/tar"
	"bytes"
	"net/http"
	"net/http/httptest"
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

func TestDownloadRejectsTruncated(t *testing.T) {
	// An HTML error page / truncated body served as 200 is the realistic
	// corruption mode the size floor guards against.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("<html>404 not found</html>"))
	}))
	defer srv.Close()

	var buf bytes.Buffer
	err := download(artifact{url: srv.URL, minBytes: 1 << 20}, &buf, nil)
	if err == nil {
		t.Fatal("expected a too-small download to be rejected")
	}
}

func TestDownloadAcceptsAboveFloor(t *testing.T) {
	body := bytes.Repeat([]byte("x"), 2048)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	var buf bytes.Buffer
	if err := download(artifact{url: srv.URL, minBytes: 1024}, &buf, nil); err != nil {
		t.Fatalf("download: %v", err)
	}
	if buf.Len() != len(body) {
		t.Errorf("wrote %d bytes, want %d", buf.Len(), len(body))
	}
}

func TestInstallTarAtomicSuccess(t *testing.T) {
	parent := t.TempDir()
	dest := filepath.Join(parent, "engine")
	src := tarball(t, map[string]string{"pkg/bin": "exe"})
	if err := installTar(src, dest); err != nil {
		t.Fatalf("installTar: %v", err)
	}
	if got, err := os.ReadFile(filepath.Join(dest, "pkg", "bin")); err != nil || string(got) != "exe" {
		t.Fatalf("installed file = %q, err %v", got, err)
	}
	// No staging dir should be left behind.
	entries, _ := os.ReadDir(parent)
	for _, e := range entries {
		if e.Name() != "engine" {
			t.Errorf("leftover entry in parent: %q", e.Name())
		}
	}
}

func TestInstallTarFailureLeavesNoPartialDir(t *testing.T) {
	parent := t.TempDir()
	dest := filepath.Join(parent, "engine")
	// Ordered tar: one good file extracts into the staging dir, THEN a traversal
	// entry makes extractTar fail partway — exactly the interrupted-extract case
	// the atomic install defends against. destDir must still never appear (so a
	// later dirPopulated check won't mistake a partial install for a complete
	// one), and the partially-populated staging dir must be cleaned up.
	src := orderedTar(t,
		tarEntry{name: "pkg/good", body: "ok"}, // written into the stage first
		tarEntry{name: "../escape", body: "x"}, // rejected → extractTar errors here
	)
	if err := installTar(src, dest); err == nil {
		t.Fatal("expected installTar to fail on a traversal entry")
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Errorf("destDir should not exist after a failed install, stat err = %v", err)
	}
	// Parent must be empty: the staging dir (which DID hold the extracted
	// pkg/good) was cleaned up, proving no partial content leaked.
	if entries, _ := os.ReadDir(parent); len(entries) != 0 {
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.Name()
		}
		t.Errorf("parent should be empty after a failed install, has %v", names)
	}
}

type tarEntry struct{ name, body string }

// orderedTar builds a tar stream with entries in the exact order given (unlike
// tarball's map, whose iteration order is random) — needed when a test depends
// on one entry being extracted before a later one fails.
func orderedTar(t *testing.T, entries ...tarEntry) *bytes.Reader {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for _, e := range entries {
		if err := tw.WriteHeader(&tar.Header{Name: e.name, Typeflag: tar.TypeReg, Mode: 0o644, Size: int64(len(e.body))}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(e.body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	return bytes.NewReader(buf.Bytes())
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
