package unz

import (
	"archive/zip"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

type ent struct {
	name, body string
	mode       os.FileMode
}

func makeZip(t *testing.T, ents ...ent) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "x.zip")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for _, e := range ents {
		h := &zip.FileHeader{Name: e.name, Method: zip.Deflate}
		if e.mode != 0 {
			h.SetMode(e.mode)
		}
		w, err := zw.CreateHeader(h)
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte(e.body))
	}
	zw.Close()
	f.Close()
	return p
}

func TestExtract(t *testing.T) {
	z := makeZip(t, ent{"a.txt", "A", 0}, ent{"sub/", "", os.ModeDir | 0o755}, ent{"sub/b.txt", "BB", 0},
		ent{"sub/run.sh", "#!/bin/sh", 0o755}, ent{"win\\c.txt", "C", 0})
	dir := filepath.Join(t.TempDir(), "out")
	n, err := Extract(z, dir)
	if err != nil || n != 4 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	for f, want := range map[string]string{"a.txt": "A", "sub/b.txt": "BB", "win/c.txt": "C"} {
		b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(f)))
		if err != nil || string(b) != want {
			t.Errorf("%s: %q %v", f, b, err)
		}
	}
}

func TestRefusesBadEntries(t *testing.T) {
	for _, name := range []string{"../evil.txt", "a/../../evil.txt", "/abs.txt", "\\abs.txt", "C:/x.txt", "C:\\x.txt",
		"a/..\\..\\evil", "file.txt:stream"} {
		parent := t.TempDir()
		dir := filepath.Join(parent, "out")
		z := makeZip(t, ent{"good.txt", "g", 0}, ent{name, "x", 0})
		_, err := Extract(z, dir)
		var ue *UnsafeError
		if !errors.As(err, &ue) {
			t.Errorf("%q: want an UnsafeError, got %v", name, err)
		}
		if _, serr := os.Stat(filepath.Join(dir, "good.txt")); serr == nil {
			t.Errorf("%q: nothing may be written for a bad archive", name)
		}
		if _, serr := os.Stat(filepath.Join(parent, "evil.txt")); serr == nil {
			t.Errorf("%q: a file escaped the folder", name)
		}
	}
}

func TestRefusesLinks(t *testing.T) {
	z := makeZip(t, ent{"l", "target", os.ModeSymlink | 0o777})
	if _, err := Extract(z, filepath.Join(t.TempDir(), "out")); err == nil {
		t.Error("a link must be refused")
	}
}

func TestRefusesNotAZip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x.zip")
	os.WriteFile(p, []byte("this is not a zip"), 0o644)
	if _, err := Extract(p, t.TempDir()); err == nil {
		t.Error("not a zip must fail")
	}
}

func TestTarget(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "d")
	if got, err := Target(dir, "./"); err != nil || got != dir {
		t.Errorf("./ is the folder itself: %q %v", got, err)
	}
	if got, err := Target(dir, "a/./b//c.txt"); err != nil || got != filepath.Join(dir, "a", "b", "c.txt") {
		t.Errorf("got %q %v", got, err)
	}
}
