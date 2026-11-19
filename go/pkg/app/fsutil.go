package app

import (
	"io"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

func isFile(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}

func isDir(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.IsDir()
}

func fileSize(path string) int64 {
	st, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return st.Size()
}

// dirSize adds up the sizes of the files below path.
func dirSize(path string) int64 {
	var n int64
	filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
		if err == nil && info.Mode().IsRegular() {
			n += info.Size()
		}
		return nil
	})
	return n
}

// isUnder reports whether path is inside base (not base itself). File names are compared without
// regard to case on Windows and macOS.
func isUnder(path, base string) bool {
	if path == "" || base == "" {
		return false
	}
	p, err1 := filepath.Abs(path)
	b, err2 := filepath.Abs(base)
	if err1 != nil || err2 != nil {
		return false
	}
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		p, b = strings.ToLower(p), strings.ToLower(b)
	}
	b = strings.TrimRight(b, string(filepath.Separator)) + string(filepath.Separator)
	return strings.HasPrefix(p, b) && len(p) > len(b)
}

// moveFile puts src at dst, replacing what is there. A rename when both are on one disk,
// otherwise a copy that is renamed into place at the end.
func moveFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := dst + ".hom-tmp"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, dst); err != nil {
		os.Remove(tmp)
		return err
	}
	in.Close()
	os.Remove(src)
	return nil
}

// escapePath percent-encodes a file name for a URL.
func escapePath(name string) string {
	u := url.URL{Path: name}
	return u.EscapedPath()
}

func ptr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func ptrSize(n int64) *int64 {
	if n < 0 {
		return nil
	}
	return &n
}
