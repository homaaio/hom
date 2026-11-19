package idx

import (
	"os"
	"strings"
	"testing"
)

func TestSafeName(t *testing.T) {
	for _, n := range []string{"hello", "a.b", "x-1_2", "com10", "console", "tool v2"} {
		if err := SafeName(n, true); err != nil {
			t.Errorf("SafeName(%q) = %v, want ok", n, err)
		}
	}
	for _, n := range []string{"", ".", "..", "a..b", "name.", "name ", "a/b", "a\\b", "a:b", "con", "NUL.hrp",
		"com1", "LPT9", ".hidden", "a*b", "a\tb", strings.Repeat("x", 101)} {
		if SafeName(n, true) == nil {
			t.Errorf("SafeName(%q) = ok, want an error", n)
		}
	}
	if SafeName(".gitignore", false) != nil {
		t.Error("a file name may start with '.'")
	}
	if SafeName(".gitignore", true) == nil {
		t.Error("a package name may not start with '.'")
	}
}

func TestSafeRel(t *testing.T) {
	for _, r := range []string{"tool", "a/b", "a\\b", "my tool/v2"} {
		if _, err := SafeRel(r); err != nil {
			t.Errorf("SafeRel(%q) = %v, want ok", r, err)
		}
	}
	for _, r := range []string{"", "/abs", "a/../b", "..", "C:\\x", "a//b", "a/", "con/x"} {
		if _, err := SafeRel(r); err == nil {
			t.Errorf("SafeRel(%q) = ok, want an error", r)
		}
	}
}

func TestVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.2.3", "1.2.3", 0},
		{"1.10", "1.9", 1},
		{"v1.0", "1.0", 0},
		{"1.0", "1.0.0", 0},
		{"1.0.1", "1.0", 1},
		{"2.0.0-rc1", "2.0.0", 0},
		{"", "1", -1},
		{"0.6.3", "2.0.0", -1},
		{"abc", "0", 0},
	}
	for _, c := range cases {
		if got := Cmp(c.a, c.b); got != c.want {
			t.Errorf("Cmp(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
	if !Gt("1.10", "1.9") || Gt("1.9", "1.10") {
		t.Error("Gt is wrong")
	}
}

const h64 = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func TestParseSpecExample(t *testing.T) {
	b, err := os.ReadFile("../../../spec/examples/index.v2.json")
	if err != nil {
		t.Skip("the spec example is not next to the tests: ", err)
	}
	ix, err := Parse(b)
	if err != nil {
		t.Fatal(err)
	}
	if ix.Schema != 2 || len(ix.Entries) != 6 || len(ix.Warnings) != 0 {
		t.Fatalf("schema %d, %d entries, warnings %v", ix.Schema, len(ix.Entries), ix.Warnings)
	}
	want := map[string]string{ // platform -> file
		"linux/amd64":  "hello-tool-linux-amd64.zip",
		"linux/arm64":  "hello-tool-linux-arm64.zip",
		"macos/arm64":  "hello-tool-macos-arm64.zip",
		"windows/amd64": "hello-tool-windows-amd64.zip",
	}
	for plat, file := range want {
		p := Platform{OS: plat[:strings.IndexByte(plat, '/')], Arch: plat[strings.IndexByte(plat, '/')+1:]}
		e, err := ix.Pick("hello-tool", p)
		if err != nil || e.File != file {
			t.Errorf("Pick(hello-tool, %s) = %v, %v; want %s", plat, e, err, file)
		}
	}
	_, err = ix.Pick("hello-tool", Platform{OS: "linux", Arch: "386"})
	if _, ok := err.(*NoBuildError); !ok {
		t.Errorf("a platform without a build: got %v", err)
	}
	if _, err := ix.Pick("nope", Platform{OS: "linux", Arch: "amd64"}); err != ErrNotFound {
		t.Errorf("an unknown name: got %v", err)
	}
	e, err := ix.Pick("hello", Platform{OS: "macos", Arch: "arm64"})
	if err != nil || e.Type != "file" || e.Size != 13 {
		t.Errorf("hello: %+v, %v", e, err)
	}
}

func TestParseSkipsBadEntries(t *testing.T) {
	doc := `{"schema":2,"packages":[
	  {"name":"ok","type":"file","file":"a.txt","sha256":"` + h64 + `"},
	  {"name":"nohash","type":"file","file":"a.txt"},
	  {"name":"../x","type":"file","file":"a.txt","sha256":"` + h64 + `"},
	  {"name":"http","type":"file","file":"a.txt","url":"http://x/y","sha256":"` + h64 + `"},
	  {"name":"weird","type":"file","file":"a.txt","os":"plan9","sha256":"` + h64 + `"},
	  {"name":"magic","type":"magic","file":"a.txt","sha256":"` + h64 + `"},
	  {"name":"nofile","type":"file","sha256":"` + h64 + `"},
	  {"name":"badsize","type":"file","file":"a.txt","size":-5,"sha256":"` + h64 + `"},
	  {"name":"badplugin","type":"plugin","file":"x.txt","sha256":"` + h64 + `"}
	]}`
	ix, err := Parse([]byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	if len(ix.Entries) != 1 || ix.Entries[0].Name != "ok" {
		t.Fatalf("entries: %+v", ix.Entries)
	}
	if len(ix.Warnings) != 8 {
		t.Errorf("want 8 warnings, got %d: %v", len(ix.Warnings), ix.Warnings)
	}
}

func TestParseInto(t *testing.T) {
	ix, err := Parse([]byte(`{"schema":2,"packages":[
	  {"name":"ok","type":"file","file":"f.exe","into":"homrec","dest":"ffmpeg","sha256":"` + h64 + `"},
	  {"name":"bad","type":"file","file":"f.exe","into":"system","sha256":"` + h64 + `"},
	  {"name":"wrong","type":"plugin","file":"f.hrp","into":"homrec","sha256":"` + h64 + `"}
	]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(ix.Entries) != 1 || ix.Entries[0].Into != "homrec" || ix.Entries[0].Dest != "ffmpeg" {
		t.Fatalf("entries: %+v", ix.Entries)
	}
	if len(ix.Warnings) != 2 {
		t.Errorf("want 2 warnings, got %d: %v", len(ix.Warnings), ix.Warnings)
	}
}

func TestParseOldLayoutAndSchema(t *testing.T) {
	ix, err := Parse([]byte("\xef\xbb\xbf" + `{"plugins":[{"name":"p","version":"1.0"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if ix.Schema != 1 || len(ix.Entries) != 1 || ix.Entries[0].Type != "plugin" || ix.Entries[0].File != "p.hrp" {
		t.Errorf("old layout: %+v", ix)
	}
	// schema 1: a file may come without a hash, a program may not
	ix, _ = Parse([]byte(`{"packages":[{"name":"f","type":"file","file":"a"},{"name":"g","type":"program","file":"a.msi"}]}`))
	if len(ix.Entries) != 1 || ix.Entries[0].Name != "f" {
		t.Errorf("schema 1 hashes: %+v", ix.Entries)
	}
	ix, err = Parse([]byte(`{"schema":3,"packages":[]}`))
	if err != nil || ix.Schema != 3 {
		t.Errorf("newer schema must parse: %v %v", ix, err)
	}
}

func TestParseErrors(t *testing.T) {
	for _, doc := range []string{"", "[]", "{", `{"schema":"x"}`, `{"schema":0}`, `{} {}`, "not json"} {
		if _, err := Parse([]byte(doc)); err == nil {
			t.Errorf("Parse(%q) = ok, want an error", doc)
		}
	}
}

func TestPickPrefersMoreSpecific(t *testing.T) {
	ix := &Index{Entries: []Entry{
		{Name: "x", Version: "1.0", File: "any"},
		{Name: "x", Version: "1.0", File: "os", OS: "linux"},
		{Name: "x", Version: "1.0", File: "both", OS: "linux", Arch: "amd64"},
		{Name: "x", Version: "1.0", File: "arch", Arch: "amd64"},
	}}
	e, _ := ix.Pick("x", Platform{OS: "linux", Arch: "amd64"})
	if e.File != "both" {
		t.Errorf("got %s, want both", e.File)
	}
	e, _ = ix.Pick("x", Platform{OS: "linux", Arch: "arm64"})
	if e.File != "os" {
		t.Errorf("got %s, want os", e.File)
	}
	e, _ = ix.Pick("x", Platform{OS: "macos", Arch: "amd64"})
	if e.File != "arch" {
		t.Errorf("got %s, want arch", e.File)
	}
	ix.Entries = append(ix.Entries, Entry{Name: "y", Version: "1.0", File: "a"}, Entry{Name: "y", Version: "1.10", File: "b"})
	e, _ = ix.Pick("y", Platform{OS: "linux", Arch: "amd64"})
	if e.File != "b" {
		t.Errorf("the newer version must win, got %s", e.File)
	}
}

func TestParseOfficialIndex(t *testing.T) {
	b, err := os.ReadFile("../../../pkgs/index.json")
	if err != nil {
		t.Skip("pkgs/index.json is not next to the tests: ", err)
	}
	ix, err := Parse(b)
	if err != nil {
		t.Fatal(err)
	}
	if len(ix.Warnings) != 0 || len(ix.Entries) < 12 {
		t.Errorf("%d entries, warnings %v", len(ix.Entries), ix.Warnings)
	}
	for _, e := range ix.Entries {
		if e.Type != "plugin" || e.SHA256 == "" {
			t.Errorf("%s: %+v", e.Name, e)
		}
	}
}
