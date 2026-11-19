package app

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/homaaio/hom/pkg/cfg"
	"github.com/homaaio/hom/pkg/dirs"
	"github.com/homaaio/hom/pkg/idx"
	"github.com/homaaio/hom/pkg/web"
)

// repoFiles is a repo served from memory.
type repoFiles struct {
	mu    sync.Mutex
	files map[string][]byte
}

func (r *repoFiles) set(name string, b []byte) {
	r.mu.Lock()
	r.files[name] = b
	r.mu.Unlock()
}

func (r *repoFiles) del(name string) {
	r.mu.Lock()
	delete(r.files, name)
	r.mu.Unlock()
}

func (r *repoFiles) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	r.mu.Lock()
	b, ok := r.files[strings.TrimPrefix(req.URL.Path, "/")]
	r.mu.Unlock()
	if !ok {
		http.NotFound(w, req)
		return
	}
	w.Write(b)
}

type tenv struct {
	*Env
	repo *repoFiles
	srv  *httptest.Server
	out  *bytes.Buffer
	errb *bytes.Buffer
}

func newTestEnv(t *testing.T) *tenv {
	t.Helper()
	rf := &repoFiles{files: map[string][]byte{}}
	srv := httptest.NewTLSServer(rf)
	t.Cleanup(srv.Close)
	root := t.TempDir()
	te := &tenv{repo: rf, srv: srv, out: &bytes.Buffer{}, errb: &bytes.Buffer{}}
	te.Env = &Env{
		Version:  "2.0.0",
		Stdin:    strings.NewReader(""),
		Stdout:   te.out,
		Stderr:   te.errb,
		D:        &dirs.Dirs{Config: filepath.Join(root, "cfg"), Data: filepath.Join(root, "data"), Cache: filepath.Join(root, "cache")},
		HTTP:     web.FromHTTP(srv.Client(), "hom-test"),
		Plat:     idx.Platform{OS: "linux", Arch: "amd64"},
		Progress: "none",
	}
	te.SetRepo(cfg.Repo{Name: "test", URL: srv.URL + "/"})
	te.SetHomrec("") // no HomRec unless a test asks for one
	return te
}

func (te *tenv) runCtx(ctx context.Context, args ...string) int {
	te.out.Reset()
	te.errb.Reset()
	te.Env.Warns = nil
	return te.Run(ctx, args)
}

func (te *tenv) run(args ...string) int { return te.runCtx(context.Background(), args...) }

func (te *tenv) doc(t *testing.T) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(te.out.Bytes(), &m); err != nil {
		t.Fatalf("stdout is not one JSON document: %v\n%s", err, te.out.String())
	}
	return m
}

func sum(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func pkg(name, typ, file, ver string, body []byte, extra map[string]any) map[string]any {
	m := map[string]any{"name": name, "type": typ, "file": file, "version": ver, "size": len(body), "sha256": sum(body)}
	for k, v := range extra {
		m[k] = v
	}
	return m
}

func (te *tenv) publishSchema(schema int, pkgs ...map[string]any) {
	b, _ := json.Marshal(map[string]any{"schema": schema, "name": "test", "packages": pkgs})
	te.repo.set("index.json", b)
}

func (te *tenv) publish(pkgs ...map[string]any) { te.publishSchema(2, pkgs...) }

func mkzip(files map[string]string) []byte {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range files {
		w, _ := zw.Create(name)
		w.Write([]byte(body))
	}
	zw.Close()
	return buf.Bytes()
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("can't read %s: %v", path, err)
	}
	return string(b)
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

func TestInstallFileAndRemove(t *testing.T) {
	te := newTestEnv(t)
	body := []byte("hello world\n")
	te.repo.set("hello.txt", body)
	te.publish(pkg("hello", "file", "hello.txt", "1.0.0", body, nil))

	if code := te.run("install", "hello"); code != 0 {
		t.Fatalf("exit %d: %s", code, te.errb)
	}
	dest := filepath.Join(te.D.Data, "files", "hello", "hello.txt")
	if got := readFile(t, dest); got != string(body) {
		t.Errorf("content %q", got)
	}
	r := cfg.FindRec(cfg.LoadDB(te.dbPath()), "hello")
	if r == nil || r.Path != dest || r.Version != "1.0.0" || r.Type != "file" {
		t.Errorf("db record: %+v", r)
	}
	if entries, _ := os.ReadDir(te.D.Cache); len(entries) != 0 {
		t.Errorf("the cache must be empty after an install, has %d entries", len(entries))
	}

	if code := te.run("remove", "hello"); code != ExitUsage {
		t.Errorf("remove without -r: exit %d, want %d", code, ExitUsage)
	}
	if !exists(dest) {
		t.Error("remove without -r must not delete anything")
	}
	if code := te.run("remove", "hello", "-r"); code != 0 {
		t.Fatalf("remove: exit %d: %s", code, te.errb)
	}
	if exists(dest) || exists(filepath.Dir(dest)) {
		t.Error("the file and its folder must be gone")
	}
	if len(cfg.LoadDB(te.dbPath())) != 0 {
		t.Error("the db must be empty")
	}
	if code := te.run("remove", "hello", "-r"); code != ExitNotFound {
		t.Errorf("remove of something not installed: exit %d, want %d", code, ExitNotFound)
	}
}

func TestInstallIntoHomrec(t *testing.T) {
	te := newTestEnv(t)
	hr := t.TempDir()
	te.SetHomrec(hr)
	body := []byte("MZ fake ffmpeg\n")
	te.repo.set("ffmpeg.exe", body)
	te.publish(pkg("ffmpeg-hr", "file", "ffmpeg.exe", "8.0.1", body,
		map[string]any{"dest": "ffmpeg", "into": "homrec"}))

	if code := te.run("install", "ffmpeg-hr"); code != 0 {
		t.Fatalf("install: exit %d\n%s / %s", code, te.out, te.errb)
	}
	dest := filepath.Join(hr, "ffmpeg", "ffmpeg.exe")
	if readFile(t, dest) != string(body) {
		t.Errorf("%s was not written", dest)
	}
	if te.run("list", "--installed") != 0 || !strings.Contains(te.out.String(), "ffmpeg-hr") {
		t.Errorf("list --installed: %s / %s", te.out, te.errb)
	}
	if code := te.run("remove", "ffmpeg-hr", "-r"); code != 0 {
		t.Fatalf("remove: exit %d\n%s / %s", code, te.out, te.errb)
	}
	if exists(dest) {
		t.Errorf("remove left %s behind", dest)
	}
}

func TestInstallIntoHomrecNeedsHomrec(t *testing.T) {
	te := newTestEnv(t) // SetHomrec("") - no HomRec
	body := []byte("MZ fake ffmpeg\n")
	te.repo.set("ffmpeg.exe", body)
	te.publish(pkg("ffmpeg-hr", "file", "ffmpeg.exe", "8.0.1", body,
		map[string]any{"dest": "ffmpeg", "into": "homrec"}))

	if code := te.run("install", "ffmpeg-hr"); code != ExitError {
		t.Fatalf("install without HomRec: exit %d, want %d\n%s / %s", code, ExitError, te.out, te.errb)
	}
}

func TestInstallJSON(t *testing.T) {
	te := newTestEnv(t)
	body := []byte("data")
	te.repo.set("a.bin", body)
	te.publish(pkg("a", "file", "a.bin", "1.0", body, nil))
	if code := te.run("--json", "install", "a"); code != 0 {
		t.Fatalf("exit %d: %s", code, te.errb)
	}
	d := te.doc(t)
	data, _ := d["data"].(map[string]any)
	if d["ok"] != true || d["cmd"] != "install" || data["package"] != "a" || data["sha256"] != sum(body) ||
		data["started"] != false || data["bytes"].(float64) != 4 {
		t.Errorf("doc: %v", d)
	}
	if w, ok := d["warnings"].([]any); !ok || len(w) != 0 {
		t.Errorf("warnings must be an empty array: %v", d["warnings"])
	}
}

func TestVerifyFailures(t *testing.T) {
	te := newTestEnv(t)
	good := []byte("the real file")
	te.repo.set("f.bin", good)
	wrongHash := pkg("badhash", "file", "f.bin", "1", []byte("something else"), map[string]any{"size": len(good)})
	wrongSize := pkg("badsize", "file", "f.bin", "1", good, map[string]any{"size": len(good) + 7})
	te.publish(wrongHash, wrongSize)
	for _, name := range []string{"badhash", "badsize"} {
		if code := te.run("--json", "install", name); code != ExitVerify {
			t.Errorf("%s: exit %d, want %d (%s)", name, code, ExitVerify, te.errb)
		}
		d := te.doc(t)
		e, _ := d["error"].(map[string]any)
		if d["ok"] != false || e["code"] != "verify" || e["exit"].(float64) != 5 || e["package"] != name {
			t.Errorf("%s: doc %v", name, d)
		}
		if exists(filepath.Join(te.D.Data, "files", name)) {
			t.Errorf("%s: nothing may be installed after a failed check", name)
		}
		if entries, _ := os.ReadDir(te.D.Cache); len(entries) != 0 {
			t.Errorf("%s: no download may be left in the cache", name)
		}
	}
	if len(cfg.LoadDB(te.dbPath())) != 0 {
		t.Error("nothing may be recorded")
	}
}

func TestVerifyKeepsOldVersion(t *testing.T) {
	te := newTestEnv(t)
	v1 := []byte("version one")
	te.repo.set("f.bin", v1)
	te.publish(pkg("f", "file", "f.bin", "1.0", v1, nil))
	if te.run("install", "f") != 0 {
		t.Fatal(te.errb)
	}
	te.repo.set("f.bin", []byte("tampered!!!"))
	te.publish(pkg("f", "file", "f.bin", "2.0", []byte("version two"), nil))
	if code := te.run("upgrade"); code != ExitVerify {
		t.Errorf("upgrade with a bad file: exit %d, want %d", code, ExitVerify)
	}
	if got := readFile(t, filepath.Join(te.D.Data, "files", "f", "f.bin")); got != "version one" {
		t.Errorf("the old file must stay, got %q", got)
	}
}

func TestErrors(t *testing.T) {
	te := newTestEnv(t)
	body := []byte("x")
	te.publish(
		pkg("winonly", "file", "x", "1", body, map[string]any{"os": "windows"}),
		pkg("present", "file", "x", "1", body, nil),
	)
	if code := te.run("install", "nope"); code != ExitNotFound {
		t.Errorf("unknown package: exit %d", code)
	}
	if code := te.run("--json", "install", "winonly"); code != ExitNotFound {
		t.Errorf("no build for this OS: exit %d", code)
	}
	if e, _ := te.doc(t)["error"].(map[string]any); e["code"] != "not_found" {
		t.Errorf("error code: %v", e)
	}
	if code := te.run("install", "../x"); code != ExitUsage {
		t.Errorf("bad name: exit %d", code)
	}
	if code := te.run("show", "nope"); code != ExitNotFound {
		t.Errorf("show of an unknown package: exit %d", code)
	}
}

func TestNetworkDown(t *testing.T) {
	te := newTestEnv(t)
	te.publish()
	te.srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	if code := te.runCtx(ctx, "search", "x"); code != ExitNetwork {
		t.Errorf("exit %d, want %d", code, ExitNetwork)
	}
	if code := te.runCtx(ctx, "--json", "ping"); code != ExitNetwork {
		t.Errorf("ping: exit %d, want %d", code, ExitNetwork)
	}
	d := te.doc(t)
	data, _ := d["data"].(map[string]any)
	if data["reachable"] != false {
		t.Errorf("ping data: %v", d)
	}
}

func TestMissingIndexAndBrokenIndex(t *testing.T) {
	te := newTestEnv(t)
	if code := te.run("search", "x"); code != ExitNetwork {
		t.Errorf("no index.json: exit %d", code)
	}
	te.repo.set("index.json", []byte("{ not json"))
	if code := te.run("search", "x"); code != ExitNetwork {
		t.Errorf("broken index: exit %d", code)
	}
	// the old layout is still read
	body := []byte("old")
	te.repo.del("index.json")
	te.repo.set("plugins/index.json", []byte(`{"plugins":[{"name":"p","type":"file","file":"p.txt","version":"1"}]}`))
	te.repo.set("plugins/p.txt", body)
	if code := te.run("install", "p"); code != 0 {
		t.Fatalf("old layout: exit %d: %s", code, te.errb)
	}
	if got := readFile(t, filepath.Join(te.D.Data, "files", "p", "p.txt")); got != "old" {
		t.Errorf("got %q", got)
	}
}

func TestNewerSchema(t *testing.T) {
	te := newTestEnv(t)
	body := []byte("x")
	te.repo.set("x", body)
	te.publishSchema(3, pkg("x", "file", "x", "1", body, nil))
	if code := te.run("search", "x"); code != 0 {
		t.Errorf("search in a newer index must work, exit %d", code)
	}
	if code := te.run("--json", "install", "x"); code != ExitError {
		t.Errorf("install from a newer index: exit %d, want %d", code, ExitError)
	}
	if e, _ := te.doc(t)["error"].(map[string]any); e["code"] != "schema" {
		t.Errorf("error code: %v", e)
	}
}

func TestSearchShowList(t *testing.T) {
	te := newTestEnv(t)
	a, b := []byte("aaa"), []byte("bbbb")
	te.repo.set("a.txt", a)
	te.repo.set("b.txt", b)
	te.publish(
		pkg("alpha", "file", "a.txt", "1.0.0", a, map[string]any{"description": "First letter", "author": "me"}),
		pkg("beta", "file", "b.txt", "2.5", b, map[string]any{"description": "Second letter"}),
	)
	if te.run("search", "alp") != 0 || !strings.Contains(te.out.String(), "alpha") || strings.Contains(te.out.String(), "beta") {
		t.Errorf("search: %s", te.out)
	}
	if te.run("search", "second", "letter") != 0 || !strings.Contains(te.out.String(), "beta") {
		t.Errorf("search in the description (words joined): %s", te.out)
	}
	if te.run("search", "zzz") != 0 || !strings.Contains(te.out.String(), "No packages match") {
		t.Errorf("no hits is not a failure: %s", te.out)
	}
	if te.run("search", "([") != 0 {
		t.Errorf("a bad regex falls back to plain text, exit with: %s", te.errb)
	}
	if te.run("--json", "search", "^b") != 0 {
		t.Fatal(te.errb)
	}
	if p, _ := te.doc(t)["data"].(map[string]any)["packages"].([]any); len(p) != 1 {
		t.Errorf("json search: %s", te.out)
	}

	if te.run("show", "alpha") != 0 || !strings.Contains(te.out.String(), "Installed:   no") || !strings.Contains(te.out.String(), "First letter") {
		t.Errorf("show: %s", te.out)
	}
	if te.run("--json", "show", "alpha") != 0 {
		t.Fatal(te.errb)
	}
	p, _ := te.doc(t)["data"].(map[string]any)["package"].(map[string]any)
	if p["name"] != "alpha" || p["installed"] != nil || p["upgradable"] != false || p["size"].(float64) != 3 || p["os"] != nil || p["repo"] != "test" {
		t.Errorf("json show: %v", p)
	}

	if te.run("list") != 0 || !strings.Contains(te.out.String(), "alpha") || !strings.Contains(te.out.String(), "beta") {
		t.Errorf("list: %s", te.out)
	}
	if te.run("install", "alpha") != 0 {
		t.Fatal(te.errb)
	}
	te.srv.Close() // --installed needs no network
	if te.run("list", "--installed") != 0 || !strings.Contains(te.out.String(), "alpha") || strings.Contains(te.out.String(), "beta") {
		t.Errorf("list --installed offline: %s / %s", te.out, te.errb)
	}
	if te.run("--json", "list", "--installed") != 0 {
		t.Fatal(te.errb)
	}
	items, _ := te.doc(t)["data"].(map[string]any)["packages"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["installed_version"] != "1.0.0" {
		t.Errorf("json list --installed: %s", te.out)
	}
}

func TestUpgrade(t *testing.T) {
	te := newTestEnv(t)
	v1, v2 := []byte("one"), []byte("two!")
	te.repo.set("h.txt", v1)
	te.publish(pkg("h", "file", "h.txt", "1.0.0", v1, nil))
	if te.run("install", "h") != 0 {
		t.Fatal(te.errb)
	}
	if te.run("list", "--upgradable") != 0 || !strings.Contains(te.out.String(), "up to date") {
		t.Errorf("nothing to upgrade: %s", te.out)
	}

	te.repo.set("h.txt", v2)
	te.publish(pkg("h", "file", "h.txt", "1.10.0", v2, nil))
	if te.run("list", "--upgradable") != 0 || !strings.Contains(te.out.String(), "h: 1.0.0 -> 1.10.0") {
		t.Errorf("list --upgradable: %s", te.out)
	}
	if te.run("--json", "show", "h") != 0 {
		t.Fatal(te.errb)
	}
	if p, _ := te.doc(t)["data"].(map[string]any)["package"].(map[string]any); p["upgradable"] != true {
		t.Errorf("show must say upgradable: %v", p)
	}
	if code := te.run("--json", "upgrade"); code != 0 {
		t.Fatalf("exit %d: %s", code, te.errb)
	}
	data, _ := te.doc(t)["data"].(map[string]any)
	if up, _ := data["upgraded"].([]any); len(up) != 1 || up[0].(map[string]any)["from"] != "1.0.0" || up[0].(map[string]any)["to"] != "1.10.0" {
		t.Errorf("upgrade data: %v", data)
	}
	if got := readFile(t, filepath.Join(te.D.Data, "files", "h", "h.txt")); got != "two!" {
		t.Errorf("content %q", got)
	}
	if r := cfg.FindRec(cfg.LoadDB(te.dbPath()), "h"); r == nil || r.Version != "1.10.0" {
		t.Errorf("db: %+v", r)
	}
	if code := te.run("--json", "upgrade"); code != 0 {
		t.Fatal(te.errb)
	}
	data, _ = te.doc(t)["data"].(map[string]any)
	if up, _ := data["upgraded"].([]any); len(up) != 0 {
		t.Errorf("a second upgrade has nothing to do: %v", data)
	}
	if sk, _ := data["skipped"].([]any); len(sk) != 1 {
		t.Errorf("skipped: %v", data)
	}
}

func TestUpgradeNothingInstalled(t *testing.T) {
	te := newTestEnv(t)
	if code := te.run("upgrade"); code != 0 {
		t.Errorf("exit %d: %s", code, te.errb)
	}
}

func TestArchive(t *testing.T) {
	te := newTestEnv(t)
	v1 := mkzip(map[string]string{"bin/tool.txt": "1", "old.txt": "stale"})
	te.repo.set("tool.zip", v1)
	te.publish(pkg("tool", "archive", "tool.zip", "1.0", v1, nil))
	if te.run("install", "tool") != 0 {
		t.Fatal(te.errb)
	}
	dest := filepath.Join(te.D.Data, "apps", "tool")
	if readFile(t, filepath.Join(dest, "bin", "tool.txt")) != "1" || !exists(filepath.Join(dest, "old.txt")) {
		t.Error("v1 was not unpacked")
	}

	v2 := mkzip(map[string]string{"bin/tool.txt": "2"})
	te.repo.set("tool.zip", v2)
	te.publish(pkg("tool", "archive", "tool.zip", "1.1", v2, nil))
	if te.run("upgrade") != 0 {
		t.Fatal(te.errb)
	}
	if readFile(t, filepath.Join(dest, "bin", "tool.txt")) != "2" {
		t.Error("v2 was not unpacked")
	}
	if exists(filepath.Join(dest, "old.txt")) {
		t.Error("a file of the old version must not survive an upgrade")
	}
	if exists(dest+".hom-new") || exists(dest+".hom-old") {
		t.Error("working folders must be gone")
	}

	if te.run("remove", "tool", "-r") != 0 || exists(dest) {
		t.Errorf("remove: %s", te.errb)
	}
}

func TestArchiveRefusesEscapes(t *testing.T) {
	te := newTestEnv(t)
	evil := mkzip(map[string]string{"fine.txt": "ok", "../evil.txt": "pwned"})
	te.repo.set("evil.zip", evil)
	te.publish(pkg("evil", "archive", "evil.zip", "1", evil, nil))
	if code := te.run("--json", "install", "evil"); code != ExitVerify {
		t.Fatalf("exit %d, want %d", code, ExitVerify)
	}
	if e, _ := te.doc(t)["error"].(map[string]any); e["code"] != "verify" {
		t.Errorf("error: %v", e)
	}
	apps := filepath.Join(te.D.Data, "apps")
	for _, p := range []string{filepath.Join(apps, "evil"), filepath.Join(apps, "evil.txt"), filepath.Join(te.D.Data, "evil.txt")} {
		if exists(p) {
			t.Errorf("%s must not exist", p)
		}
	}
	if len(cfg.LoadDB(te.dbPath())) != 0 {
		t.Error("nothing may be recorded")
	}
}

func TestArchiveToFolder(t *testing.T) {
	te := newTestEnv(t)
	z := mkzip(map[string]string{"new.txt": "n"})
	te.repo.set("p.zip", z)
	te.publish(pkg("p", "archive", "p.zip", "1", z, nil))
	to := filepath.Join(t.TempDir(), "mine")
	os.MkdirAll(to, 0o755)
	os.WriteFile(filepath.Join(to, "keep.txt"), []byte("mine"), 0o644)
	if te.run("install", "p", "--to", to) != 0 {
		t.Fatal(te.errb)
	}
	if readFile(t, filepath.Join(to, "new.txt")) != "n" || readFile(t, filepath.Join(to, "keep.txt")) != "mine" {
		t.Error("--to unpacks in place and keeps what is there")
	}
	if te.run("remove", "p", "-r") != 0 {
		t.Fatal(te.errb)
	}
	if !exists(filepath.Join(to, "new.txt")) || !exists(filepath.Join(to, "keep.txt")) {
		t.Error("hom must leave a folder chosen with --to alone")
	}
	if len(cfg.LoadDB(te.dbPath())) != 0 {
		t.Error("the package must be forgotten")
	}
}

func TestToOnlyForFilesAndArchives(t *testing.T) {
	te := newTestEnv(t)
	body := []byte("x")
	te.repo.set("p.hrp", body)
	te.publish(pkg("p", "plugin", "p.hrp", "1", body, nil))
	if code := te.run("install", "p", "--to", t.TempDir()); code != ExitUsage {
		t.Errorf("exit %d, want %d", code, ExitUsage)
	}
}

func TestProgramNeedsRun(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the .AppImage rules are checked on Linux")
	}
	te := newTestEnv(t)
	app := []byte("#!/bin/sh\nexit 0\n")
	tgz := []byte("not really a tarball")
	te.repo.set("tool.AppImage", app)
	te.repo.set("tool.tar.gz", tgz)
	te.publish(
		pkg("tool", "program", "tool.AppImage", "1.0", app, map[string]any{"author": "someone"}),
		pkg("tgz", "program", "tool.tar.gz", "1.0", tgz, nil),
	)

	// --yes is not the yes that starts an installer
	if code := te.run("--yes", "install", "tool"); code != ExitDeclined {
		t.Fatalf("exit %d, want %d: %s", code, ExitDeclined, te.errb)
	}
	saved := filepath.Join(te.D.Cache, "tool__tool.AppImage")
	if !exists(saved) {
		t.Error("the installer must stay in the cache")
	}
	if len(cfg.LoadDB(te.dbPath())) != 0 {
		t.Error("a program that did not run is not installed")
	}
	if !strings.Contains(te.errb.String(), "--run") {
		t.Errorf("the message must say how to run it: %s", te.errb)
	}

	// --run starts it (here: a script that exits 0)
	if code := te.run("--json", "install", "tool", "--run"); code != 0 {
		t.Fatalf("exit %d: %s", code, te.errb)
	}
	data, _ := te.doc(t)["data"].(map[string]any)
	if data["started"] != true {
		t.Errorf("data: %v", data)
	}
	if r := cfg.FindRec(cfg.LoadDB(te.dbPath()), "tool"); r == nil || r.Type != "program" {
		t.Errorf("db: %+v", r)
	}

	// something hom does not start is only saved
	if code := te.run("--json", "install", "tgz"); code != 0 {
		t.Fatalf("exit %d: %s", code, te.errb)
	}
	data, _ = te.doc(t)["data"].(map[string]any)
	if data["started"] != false || !exists(filepath.Join(te.D.Cache, "tgz__tool.tar.gz")) {
		t.Errorf("data: %v", data)
	}
}

func TestPlugin(t *testing.T) {
	te := newTestEnv(t)
	hr := t.TempDir()
	te.SetHomrec(hr)
	body := []byte("plugin bytes")
	te.repo.set("demo.hrp", body)
	te.publish(pkg("demo", "plugin", "demo.hrp", "1.0", body, nil))

	if te.run("install", "demo") != 0 {
		t.Fatal(te.errb)
	}
	hrp := filepath.Join(hr, "plugins", "demo.hrp")
	if readFile(t, hrp) != "plugin bytes" {
		t.Error("the plugin is not in the plugins folder")
	}

	inst := filepath.Join(hr, "plugins", ".installed", "demo")
	os.MkdirAll(filepath.Join(inst, ".store"), 0o755)
	os.WriteFile(filepath.Join(inst, ".store", "s.txt"), []byte("settings"), 0o644)
	os.WriteFile(filepath.Join(inst, "plugin.json"), []byte(`{"version": "0.9"}`), 0o644)
	if te.run("list", "--installed") != 0 || !strings.Contains(te.out.String(), "demo") || !strings.Contains(te.out.String(), "0.9") {
		t.Errorf("the version comes from plugin.json: %s", te.out)
	}

	if te.run("remove", "demo", "-r") != 0 {
		t.Fatal(te.errb)
	}
	if exists(hrp) || exists(filepath.Join(inst, "plugin.json")) {
		t.Error("remove must delete the plugin")
	}
	if !exists(filepath.Join(inst, ".store", "s.txt")) {
		t.Error("remove must keep .store")
	}
	if te.run("purge", "demo", "-r") != 0 || exists(inst) {
		t.Errorf("purge must delete everything: %s", te.errb)
	}
	if code := te.run("purge", "demo", "-r"); code != ExitNotFound {
		t.Errorf("purge of something not installed: exit %d", code)
	}
}

func TestPluginNeedsHomrec(t *testing.T) {
	te := newTestEnv(t)
	body := []byte("x")
	te.repo.set("demo.hrp", body)
	te.publish(pkg("demo", "plugin", "demo.hrp", "1.0", body, nil))
	if code := te.run("install", "demo"); code != ExitError {
		t.Errorf("exit %d, want %d", code, ExitError)
	}
}

func TestUsageErrors(t *testing.T) {
	te := newTestEnv(t)
	for _, args := range [][]string{
		{}, {"bogus"}, {"install"}, {"install", "a", "b"}, {"--progress=x", "where"}, {"--nope"},
		{"search"}, {"where", "extra"}, {"set"}, {"set", "color", "red"}, {"--root"}, {"--json=1", "where"},
	} {
		if code := te.run(args...); code != ExitUsage {
			t.Errorf("hom %v: exit %d, want %d", args, code, ExitUsage)
		}
	}
	if te.run("--json", "bogus") != ExitUsage {
		t.Error("exit code")
	}
	d := te.doc(t)
	if e, _ := d["error"].(map[string]any); d["ok"] != false || e["code"] != "usage" || e["exit"].(float64) != 2 {
		t.Errorf("doc: %v", d)
	}
	if te.run("help") != 0 || !strings.Contains(te.out.String(), "Usage:") {
		t.Error("help")
	}
	if te.run("--version") != 0 || !strings.Contains(te.out.String(), "hom 2.0.0") {
		t.Errorf("version: %s", te.out)
	}
}

func TestWhereSetClean(t *testing.T) {
	te := newTestEnv(t)
	if te.run("--json", "where") != 0 {
		t.Fatal(te.errb)
	}
	data, _ := te.doc(t)["data"].(map[string]any)
	repo, _ := data["repo"].(map[string]any)
	if data["config"] != te.D.Config || data["data"] != te.D.Data || data["cache"] != te.D.Cache ||
		data["portable"] != false || data["homrec"] != nil || repo["name"] != "test" {
		t.Errorf("where: %v", data)
	}

	hr := t.TempDir()
	if te.run("set", "homrec", hr) != 0 {
		t.Fatal(te.errb)
	}
	if !strings.Contains(te.errb.String(), "saved anyway") {
		t.Errorf("a folder without HomRec is accepted with a warning: %s", te.errb)
	}
	kv := cfg.LoadKv(filepath.Join(te.D.Config, dirs.CfgName))
	if kv["homrec"] != hr {
		t.Errorf("hom.cfg: %v", kv)
	}
	if te.run("set", "homrec", filepath.Join(hr, "missing")) != ExitError {
		t.Error("a folder that does not exist is refused")
	}
	if te.run("set", "data", "auto") != 0 || te.run("set", "homrec", "auto") != 0 {
		t.Fatal(te.errb)
	}
	if kv := cfg.LoadKv(filepath.Join(te.D.Config, dirs.CfgName)); len(kv) != 0 {
		t.Errorf("auto removes the setting: %v", kv)
	}

	os.MkdirAll(te.D.Cache, 0o755)
	os.WriteFile(filepath.Join(te.D.Cache, "junk.bin"), make([]byte, 100), 0o644)
	if te.run("--json", "clean") != 0 {
		t.Fatal(te.errb)
	}
	if data, _ := te.doc(t)["data"].(map[string]any); data["freed"].(float64) != 100 {
		t.Errorf("clean: %v", data)
	}
	if exists(te.D.Cache) {
		t.Error("the cache must be gone")
	}
	if te.run("clean") != 0 {
		t.Error("cleaning an empty cache is fine")
	}
}

func TestRepoCommands(t *testing.T) {
	te := newTestEnv(t)
	if te.run("repo", "add", "mine", "someone/hom") != 0 {
		t.Fatal(te.errb)
	}
	if te.run("repo", "add", "mine", "x/y") != ExitError {
		t.Error("a duplicate name")
	}
	if te.run("repo", "add", "bad!", "x/y") != ExitUsage || te.run("repo", "add", "x", "http://insecure/") != ExitUsage {
		t.Error("a bad name or a non-https URL")
	}
	if te.run("repo", "set", "nope") != ExitNotFound {
		t.Error("an unknown repo")
	}
	if te.run("repo", "set", "mine") != 0 {
		t.Fatal(te.errb)
	}
	if te.run("--json", "repo", "list") != 0 {
		t.Fatal(te.errb)
	}
	data, _ := te.doc(t)["data"].(map[string]any)
	repos, _ := data["repos"].([]any)
	if data["active"] != "mine" || len(repos) != 2 || repos[0].(map[string]any)["official"] != true {
		t.Errorf("repo list: %v", data)
	}
	if te.run("repo", "remove", "official") != ExitError {
		t.Error("official can't be removed")
	}
	if te.run("repo", "remove", "mine") != 0 || !strings.Contains(te.out.String(), "switched back") {
		t.Errorf("remove: %s", te.out)
	}
	if rc := cfg.LoadRepoConfig(filepath.Join(te.D.Config, dirs.ReposName)); rc.Active != cfg.OfficialName || len(rc.Repos) != 0 {
		t.Errorf("repos file: %+v", rc)
	}
}

func TestPing(t *testing.T) {
	te := newTestEnv(t)
	te.publish(pkg("a", "file", "a", "1", []byte("x"), nil))
	if te.run("ping") != 0 || !strings.Contains(te.out.String(), "OK") {
		t.Errorf("ping: %s / %s", te.out, te.errb)
	}
	if te.run("--json", "ping") != 0 {
		t.Fatal(te.errb)
	}
	if data, _ := te.doc(t)["data"].(map[string]any); data["reachable"] != true || data["status"].(float64) != 200 {
		t.Errorf("ping: %v", data)
	}
}

func TestSelfUpdate(t *testing.T) {
	te := newTestEnv(t)
	self := filepath.Join(t.TempDir(), "hom-test-binary")
	os.WriteFile(self, []byte("OLD"), 0o755)
	te.SelfPath = self
	te.SelfURL = te.srv.URL + "/rel/"
	newBin := []byte("NEW BINARY")
	asset := assetName(idx.Here())
	te.repo.set("rel/version.txt", []byte("2.1.0\n"))
	te.repo.set("rel/"+asset, newBin)
	te.repo.set("rel/"+asset+".sha256", []byte(strings.Repeat("0", 64)+"  "+asset+"\n"))

	if code := te.run("update"); code != ExitVerify {
		t.Errorf("a wrong checksum: exit %d, want %d", code, ExitVerify)
	}
	if readFile(t, self) != "OLD" {
		t.Error("the program must stay as it was")
	}

	te.repo.set("rel/"+asset+".sha256", []byte(sum(newBin)+"  "+asset+"\n"))
	if code := te.run("--json", "update"); code != 0 {
		t.Fatalf("exit %d: %s", code, te.errb)
	}
	if data, _ := te.doc(t)["data"].(map[string]any); data["updated"] != true || data["latest"] != "2.1.0" || data["current"] != "2.0.0" {
		t.Errorf("update: %v", data)
	}
	if readFile(t, self) != "NEW BINARY" {
		t.Error("the program was not replaced")
	}

	te.Version = "2.1.0"
	if code := te.run("--json", "update"); code != 0 {
		t.Fatal(te.errb)
	}
	if data, _ := te.doc(t)["data"].(map[string]any); data["updated"] != false {
		t.Errorf("already the newest: %v", data)
	}
}

func TestFormatBytesAndProgress(t *testing.T) {
	for n, want := range map[int64]string{0: "0 B", 1023: "1023 B", 1536: "1.5 KB", 3 << 20: "3.0 MB", 5 << 29: "2.50 GB", -4: "0 B"} {
		if got := FormatBytes(n); got != want {
			t.Errorf("FormatBytes(%d) = %q, want %q", n, got, want)
		}
	}
	if got := FormatProgress(50, 512, 1024, 75); got != "[##########----------]  50%  512 B / 1.0 KB  ~1m 15s" {
		t.Errorf("got %q", got)
	}
	if got := FormatProgress(-1, 2048, -1, -1); got != "downloading...  2.0 KB" {
		t.Errorf("got %q", got)
	}
}

func TestParseArgsOptions(t *testing.T) {
	p, err := parseArgs([]string{"install", "x", "--to=/t", "-y", "--run", "--progress", "lines", "--root", "/r", "-f"})
	if err != nil {
		t.Fatal(err)
	}
	if p.cmd != "install" || len(p.args) != 1 || p.o.To != "/t" || !p.yes || !p.o.Run || p.progress != "lines" || p.root != "/r" || !p.o.Force {
		t.Errorf("%+v", p)
	}
	p, err = parseArgs([]string{"search", "--", "-weird"})
	if err != nil || len(p.args) != 1 || p.args[0] != "-weird" {
		t.Errorf("after --, everything is an argument: %+v %v", p, err)
	}
}
