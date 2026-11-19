package cfg

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestKvRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "sub", "hom.cfg")
	if err := SaveKv(p, map[string]string{"homrec": `C:\HomRec`, "data": "D:\\x y"}); err != nil {
		t.Fatal(err)
	}
	kv := LoadKv(p)
	if kv["homrec"] != `C:\HomRec` || kv["data"] != `D:\x y` || len(kv) != 2 {
		t.Errorf("got %v", kv)
	}
	if len(LoadKv(filepath.Join(t.TempDir(), "missing"))) != 0 {
		t.Error("a missing file is an empty map")
	}
}

func TestKvIgnoresJunk(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x.cfg")
	os.WriteFile(p, []byte("\ufeff# comment\n\nnovalue\na  b c \n"), 0o644)
	kv := LoadKv(p)
	if kv["a"] != "b c" || len(kv) != 1 {
		t.Errorf("got %v", kv)
	}
}

func TestDB(t *testing.T) {
	p := filepath.Join(t.TempDir(), "db.cfg")
	if err := PutRec(p, Rec{Name: "a", Type: "file", Version: "1", Path: "/x/a"}); err != nil {
		t.Fatal(err)
	}
	PutRec(p, Rec{Name: "b|c", Type: "archive", Version: "2", Path: "/x/b\nc"})
	PutRec(p, Rec{Name: "a", Type: "file", Version: "3", Path: "/x/a"})
	recs := LoadDB(p)
	if len(recs) != 2 || FindRec(recs, "a").Version != "3" {
		t.Fatalf("got %+v", recs)
	}
	if recs[1].Name != "b_c" || recs[1].Path != "/x/b_c" {
		t.Errorf("'|' and line breaks must be cleaned, got %+v", recs[1])
	}
	if ok, _ := DelRec(p, "a"); !ok {
		t.Error("DelRec of a known name must report true")
	}
	if ok, _ := DelRec(p, "a"); ok {
		t.Error("DelRec of an unknown name must report false")
	}
	if len(LoadDB(p)) != 1 {
		t.Error("one record must be left")
	}
	os.WriteFile(p, []byte("bad line\na|b|c\n|x|y|z\nok|file|1|/p\n"), 0o644)
	if r := LoadDB(p); len(r) != 1 || r[0].Name != "ok" {
		t.Errorf("lines that are not name|type|version|path are skipped, got %+v", r)
	}
}

func TestRepoNames(t *testing.T) {
	for _, n := range []string{"mine", "my-repo_2", "a.b"} {
		if err := CheckRepoName(n); err != nil {
			t.Errorf("%q: %v", n, err)
		}
	}
	for _, n := range []string{"", "has space", "official", "OFFICIAL", "a/b", strings.Repeat("x", 33)} {
		if CheckRepoName(n) == nil {
			t.Errorf("%q must be refused", n)
		}
	}
}

func TestNormalizeRepoURL(t *testing.T) {
	good := map[string]string{
		"https://example.org/p":    "https://example.org/p/",
		"https://example.org/p/":   "https://example.org/p/",
		"someone/hom":              "https://raw.githubusercontent.com/someone/hom/main/pkgs/",
		"someone/hom@dev":          "https://raw.githubusercontent.com/someone/hom/dev/pkgs/",
		"  someone/hom@feat/x  ":   "https://raw.githubusercontent.com/someone/hom/feat/x/pkgs/",
		"HTTPS://example.org":      "HTTPS://example.org/",
	}
	for in, want := range good {
		got, err := NormalizeRepoURL(in)
		if err != nil || got != want {
			t.Errorf("%q -> %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"", "http://example.org/p/", "ftp://x/y", "https://exa mple.org/", "https://example.org/../x/",
		"https://user@example.org/", "https:///x", "a/b/c", "/hom", "hom", "x/y@", "x/y z"} {
		if got, err := NormalizeRepoURL(in); err == nil {
			t.Errorf("%q must be refused, got %q", in, got)
		}
	}
}

func TestRepoConfig(t *testing.T) {
	p := filepath.Join(t.TempDir(), "hom_repos.cfg")
	if rc := LoadRepoConfig(p); rc.Active != OfficialName || rc.ActiveRepo().URL != OfficialURL {
		t.Errorf("default: %+v", rc)
	}
	rc := RepoConfig{Active: "Mine", Repos: []Repo{{"mine", "https://example.org/p/"}}}
	if err := SaveRepoConfig(p, rc); err != nil {
		t.Fatal(err)
	}
	got := LoadRepoConfig(p)
	if len(got.Repos) != 1 || got.ActiveRepo().Name != "mine" {
		t.Errorf("got %+v", got)
	}
	os.WriteFile(p, []byte("active gone\nrepo bad name http://x/\nrepo ok https://example.org/p\nrepo OK https://example.org/q\nrepo official https://x/y/\n"), 0o644)
	got = LoadRepoConfig(p)
	if len(got.Repos) != 1 || got.Repos[0].Name != "ok" {
		t.Errorf("junk and duplicates are skipped, got %+v", got.Repos)
	}
	if got.ActiveRepo().Name != OfficialName {
		t.Error("an unknown active repo falls back to official")
	}
}
