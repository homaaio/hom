// Package unz unpacks a .zip with hom's own code, and refuses everything that could write outside
// the target folder (spec 1.6). A bad archive is refused before a single byte is written.
package unz

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// Limits against zip bombs.
const (
	MaxEntries = 200_000
	MaxTotal   = 16 << 30
)

// UnsafeError means the archive holds something hom refuses (a path that climbs out, a link).
type UnsafeError struct {
	Entry string
	Why   string
}

func (e *UnsafeError) Error() string {
	if e.Entry == "" {
		return "the archive is refused: " + e.Why
	}
	return fmt.Sprintf("the archive is refused: entry %q %s", e.Entry, e.Why)
}

// Target returns where an entry would be written inside dir, or an *UnsafeError.
// An entry that is the folder itself ("./") returns dir.
func Target(dir, name string) (string, error) {
	bad := func(why string) (string, error) { return "", &UnsafeError{Entry: name, Why: why} }
	n := strings.ReplaceAll(name, "\\", "/")
	if n == "" {
		return bad("has no name")
	}
	if strings.ContainsRune(n, 0) {
		return bad("has a NUL in its name")
	}
	if strings.HasPrefix(n, "/") {
		return bad("is an absolute path")
	}
	if strings.Contains(n, ":") {
		return bad("has a drive letter or a stream name")
	}
	for _, part := range strings.Split(n, "/") {
		if part == ".." {
			return bad("climbs out of the folder (..)")
		}
	}
	clean := path.Clean(n)
	if clean == "." {
		return dir, nil
	}
	target := filepath.Join(dir, filepath.FromSlash(clean))
	rel, err := filepath.Rel(dir, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return bad("climbs out of the folder")
	}
	return target, nil
}

// Extract unpacks zipPath into dir (created if needed) and returns the number of files written.
func Extract(zipPath, dir string) (int, error) {
	zr, err := zip.OpenReader(zipPath)
	if err != nil && !(errors.Is(err, zip.ErrInsecurePath) && zr != nil) {
		return 0, fmt.Errorf("not a valid .zip file: %w", err)
	}
	// With ErrInsecurePath the reader is fine: every name is checked below, by hom's own rules.
	defer zr.Close()
	if len(zr.File) > MaxEntries {
		return 0, &UnsafeError{Why: "has too many entries"}
	}
	var total uint64
	targets := make([]string, len(zr.File))
	for i, f := range zr.File {
		mode := f.Mode()
		if mode&os.ModeSymlink != 0 {
			return 0, &UnsafeError{Entry: f.Name, Why: "is a link"}
		}
		if !mode.IsRegular() && !mode.IsDir() {
			return 0, &UnsafeError{Entry: f.Name, Why: "is not a plain file or folder"}
		}
		t, err := Target(dir, f.Name)
		if err != nil {
			return 0, err
		}
		targets[i] = t
		total += f.UncompressedSize64
	}
	if total > MaxTotal {
		return 0, &UnsafeError{Why: "would unpack to more than 16 GiB"}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return 0, err
	}
	n := 0
	for i, f := range zr.File {
		if f.Mode().IsDir() {
			if err := os.MkdirAll(targets[i], 0o755); err != nil {
				return n, err
			}
			continue
		}
		if err := extractOne(f, targets[i]); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

func extractOne(f *zip.File, target string) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	// never write through a link that was already there (--to into a folder of the user's)
	if fi, err := os.Lstat(target); err == nil && fi.Mode()&os.ModeSymlink != 0 {
		return &UnsafeError{Entry: f.Name, Why: "would be written through an existing link"}
	}
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	perm := os.FileMode(0o644)
	if f.Mode().Perm()&0o111 != 0 {
		perm = 0o755
	}
	out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	want := int64(f.UncompressedSize64)
	n, err := io.Copy(out, io.LimitReader(rc, want+1))
	cerr := out.Close()
	if err != nil {
		return err
	}
	if cerr != nil {
		return cerr
	}
	if n != want {
		return fmt.Errorf("entry %q: its size does not match its header", f.Name)
	}
	return nil
}
