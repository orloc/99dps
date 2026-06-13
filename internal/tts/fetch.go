package tts

import (
	"archive/tar"
	"compress/bzip2"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// fetchArtifact downloads a .tar.bz2 to a temp file, verifies it (sha256 when
// pinned, else logs a warning), and extracts it into destDir. progress, if non-
// nil, is called with bytes-downloaded as the transfer proceeds. It is a no-op
// (returns nil) when destDir already looks populated, so first-run downloads are
// idempotent.
func fetchArtifact(a artifact, destDir string, progress func(done int64)) error {
	if dirPopulated(destDir) {
		return nil
	}
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return err
	}

	tmp, err := os.CreateTemp("", "99dps-tts-*.tar.bz2")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()

	if err := download(a, tmp, progress); err != nil {
		return fmt.Errorf("download %s: %w", a.url, err)
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if err := extractTar(bzip2.NewReader(tmp), destDir); err != nil {
		return fmt.Errorf("extract %s: %w", a.url, err)
	}
	return nil
}

// download streams a.url into w, verifying sha256 when a.sha256 is set.
func download(a artifact, w io.Writer, progress func(done int64)) error {
	client := &http.Client{Timeout: 30 * time.Minute}
	resp, err := client.Get(a.url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("http %s", resp.Status)
	}

	h := sha256.New()
	src := io.TeeReader(resp.Body, h)
	if progress != nil {
		src = io.TeeReader(src, &progressWriter{fn: progress})
	}
	if _, err := io.Copy(w, src); err != nil {
		return err
	}

	if a.sha256 == "" {
		// integrity rests on HTTPS until checksums are pinned (see manifest.go).
		return nil
	}
	if got := hex.EncodeToString(h.Sum(nil)); !strings.EqualFold(got, a.sha256) {
		return fmt.Errorf("sha256 mismatch: got %s want %s", got, a.sha256)
	}
	return nil
}

// progressWriter reports cumulative bytes seen to a callback.
type progressWriter struct {
	fn   func(done int64)
	seen int64
}

func (p *progressWriter) Write(b []byte) (int, error) {
	p.seen += int64(len(b))
	p.fn(p.seen)
	return len(b), nil
}

// extractTar writes every entry of a (decompressed) tar stream under destDir.
// Paths are sanitized against traversal ("zip-slip") — an entry that would
// escape destDir is rejected rather than written.
func extractTar(r io.Reader, destDir string) error {
	tr := tar.NewReader(r)
	root, err := filepath.Abs(destDir)
	if err != nil {
		return err
	}
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		target, err := safeJoin(root, hdr.Name)
		if err != nil {
			return err
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			if err := writeFile(target, tr, os.FileMode(hdr.Mode)); err != nil {
				return err
			}
		default:
			// skip symlinks/devices/etc. — these archives are plain dirs+files,
			// and honoring a symlink from a download is an avoidable risk.
		}
	}
}

// safeJoin joins name onto root, rejecting any entry that would escape root
// ("zip-slip"). root must already be absolute/clean.
func safeJoin(root, name string) (string, error) {
	target := filepath.Join(root, strings.ReplaceAll(name, `\`, "/"))
	rel, err := filepath.Rel(root, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("unsafe archive path: %q", name)
	}
	return target, nil
}

func writeFile(path string, r io.Reader, mode os.FileMode) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode|0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, r)
	return err
}

// dirPopulated reports whether dir exists and has at least one entry — used to
// skip a re-download/re-extract.
func dirPopulated(dir string) bool {
	entries, err := os.ReadDir(dir)
	return err == nil && len(entries) > 0
}
