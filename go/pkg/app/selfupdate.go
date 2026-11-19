package app

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/homaaio/hom/pkg/idx"
	"github.com/homaaio/hom/pkg/web"
)

// Where hom's own releases live. The release has version.txt and, for each platform,
// hom-<os>-<arch>[.exe] with a .sha256 file beside it (the line of `sha256sum`).
const selfBase = "https://github.com/homaaio/hom/releases/latest/download/"

func assetName(p idx.Platform) string {
	n := "hom-" + p.OS + "-" + p.Arch
	if p.OS == "windows" {
		n += ".exe"
	}
	return n
}

func isHex(s string, n int) bool {
	if len(s) != n {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

func validVersion(s string) bool {
	if s == "" || len(s) > 40 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '.' || c == '-' || c == '+') {
			return false
		}
	}
	return true
}

// cmdSelfUpdate replaces the running hom with the latest release, after checking its sha256.
func (e *Env) cmdSelfUpdate(ctx context.Context, force bool) (any, error) {
	base := e.SelfURL
	if base == "" {
		base = selfBase
	}
	e.Say("Checking for a new version of hom...\n")
	r, err := e.HTTP.GetRetry(ctx, base+"version.txt", 1024)
	if err != nil {
		return nil, errNet("", "couldn't check for a new version: %v", err)
	}
	if r.Status != 200 {
		return nil, errNet("", "couldn't check for a new version (HTTP %d)", r.Status)
	}
	latest := ""
	if f := strings.Fields(string(r.Body)); len(f) > 0 {
		latest = f[0]
	}
	if !validVersion(latest) {
		return nil, errNet("", "the release has no usable version.txt")
	}
	data := map[string]any{"current": e.Version, "latest": latest, "updated": false}
	if !force && !idx.Gt(latest, e.Version) {
		e.Say("hom %s is the newest version.\n", e.Version)
		return data, nil
	}

	asset := assetName(idx.Here())
	sr, err := e.HTTP.GetRetry(ctx, base+asset+".sha256", 4096)
	if err != nil {
		return nil, errNet("", "couldn't fetch the checksum of %s: %v", asset, err)
	}
	if sr.Status != 200 {
		return nil, errNet("", "the release has no checksum for %s (HTTP %d); not updating", asset, sr.Status)
	}
	want := ""
	if f := strings.Fields(string(sr.Body)); len(f) > 0 {
		want = strings.ToLower(f[0])
	}
	if !isHex(want, 64) {
		return nil, errVerify("", "the checksum file of %s is not a sha256; not updating", asset)
	}

	self := e.SelfPath
	if self == "" {
		var xerr error
		if self, xerr = os.Executable(); xerr != nil {
			return nil, errGeneric("can't find the running hom: %v", xerr)
		}
		if resolved, rerr := filepath.EvalSymlinks(self); rerr == nil {
			self = resolved
		}
	}
	part := self + ".new"
	defer os.Remove(part)
	e.Say("Downloading hom %s (%s)...\n", latest, asset)
	pr := e.newProgress()
	dl, err := e.HTTP.Download(ctx, base+asset, part, 256<<20, pr)
	pr.Finish()
	if err != nil {
		if web.IsNet(err) {
			return nil, errNet("", "the download failed: %v", err)
		}
		return nil, errGeneric("couldn't save the update next to %s: %v (no permission?)", self, err)
	}
	if dl.Status != 200 {
		return nil, errNet("", "the release has no %s (HTTP %d)", asset, dl.Status)
	}
	if dl.SHA256 != want {
		return nil, errVerify("", "the sha256 of the download does not match its checksum file; not updating")
	}
	if err := os.Chmod(part, 0o755); err != nil {
		return nil, errGeneric("couldn't make the new hom executable: %v", err)
	}

	// A running program can't be overwritten on Windows, but it can be renamed: the old one is
	// moved aside and the new one takes its name.
	old := self + ".old"
	os.Remove(old)
	if err := os.Rename(self, old); err != nil {
		return nil, errGeneric("couldn't replace %s: %v (no permission?)", self, err)
	}
	if err := os.Rename(part, self); err != nil {
		os.Rename(old, self)
		return nil, errGeneric("couldn't put the new hom in place: %v", err)
	}
	if runtime.GOOS != "windows" {
		os.Remove(old)
	}
	data["updated"] = true
	e.Say("Updated hom %s -> %s.\n", e.Version, latest)
	return data, nil
}
