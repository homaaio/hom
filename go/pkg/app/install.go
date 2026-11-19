package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/homaaio/hom/pkg/cfg"
	"github.com/homaaio/hom/pkg/idx"
	"github.com/homaaio/hom/pkg/unz"
	"github.com/homaaio/hom/pkg/web"
)

type installData struct {
	Package string `json:"package"`
	Version string `json:"version"`
	Type    string `json:"type"`
	Path    string `json:"path"`
	Bytes   int64  `json:"bytes"`
	SHA256  string `json:"sha256"`
	Started bool   `json:"started"`
}

func (e *Env) cmdInstall(ctx context.Context, name string, o Opts) (any, error) {
	if err := idx.SafeName(name, true); err != nil {
		return nil, errUsage("bad package name '%s': %v", name, err)
	}
	c, err := e.fetchCatalog(ctx)
	if err != nil {
		return nil, err
	}
	if err := c.needSupported(); err != nil {
		return nil, err
	}
	ent, err := e.pick(c, name)
	if err != nil {
		return nil, err
	}
	d, err := e.installEntry(ctx, c, ent, o)
	if err != nil {
		return nil, err
	}
	return d, nil
}

// targetDir is the folder of a 'file' or an 'archive' package: --to, the HomRec folder for
// "into": "homrec", or under the data folder.
func (e *Env) targetDir(ent *idx.Entry, to string) (string, error) {
	if to != "" {
		abs, err := filepath.Abs(to)
		if err != nil {
			return "", errGeneric("bad --to folder: %v", err)
		}
		return abs, nil
	}
	sub := ent.Name
	if ent.Dest != "" {
		s, err := idx.SafeRel(ent.Dest)
		if err != nil {
			return "", errVerify(ent.Name, "bad dest in the index: %v", err)
		}
		sub = s
	}
	if ent.Into == "homrec" {
		h := e.homrecDir()
		if h == "" {
			return "", errGeneric("couldn't find HomRec, and '%s' installs into its folder; tell hom where it is: hom set homrec <folder>", ent.Name)
		}
		return filepath.Join(h, sub), nil
	}
	base := "files"
	if ent.Type == "archive" {
		base = "apps"
	}
	return filepath.Join(e.D.Data, base, sub), nil
}

// installArchive unpacks a zip. Into a folder that hom manages it unpacks beside it first and then
// swaps, so an upgrade leaves no stale files and a bad archive leaves the old version alone.
// Into --to (the user's folder) it unpacks in place and never deletes anything.
func installArchive(zipPath, dest string, inPlace bool) (int, error) {
	if inPlace {
		return unz.Extract(zipPath, dest)
	}
	stage := dest + ".hom-new"
	old := dest + ".hom-old"
	os.RemoveAll(stage)
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return 0, err
	}
	n, err := unz.Extract(zipPath, stage)
	if err != nil {
		os.RemoveAll(stage)
		return 0, err
	}
	os.RemoveAll(old)
	_, serr := os.Lstat(dest)
	hadOld := serr == nil
	if hadOld {
		if err := os.Rename(dest, old); err != nil {
			os.RemoveAll(stage)
			return 0, fmt.Errorf("can't replace the old version (is it running?): %w", err)
		}
	}
	if err := os.Rename(stage, dest); err != nil {
		if hadOld {
			os.Rename(old, dest)
		}
		os.RemoveAll(stage)
		return 0, err
	}
	os.RemoveAll(old)
	return n, nil
}

func (e *Env) installEntry(ctx context.Context, c *catalog, ent *idx.Entry, o Opts) (*installData, error) {
	name := ent.Name
	if o.To != "" && ent.Type != "file" && ent.Type != "archive" {
		return nil, errUsage("--to only works for packages of type file or archive")
	}

	var dest string // a file (plugin, file, program) or a folder (archive)
	switch ent.Type {
	case "plugin":
		pd := e.pluginsDir()
		if pd == "" {
			return nil, errGeneric("couldn't find HomRec. Plugins go into its plugins folder; tell hom where it is: hom set homrec <folder>")
		}
		dest = filepath.Join(pd, name+strings.ToLower(filepath.Ext(ent.File)))
	case "file", "archive":
		dir, err := e.targetDir(ent, o.To)
		if err != nil {
			return nil, err
		}
		dest = dir
		if ent.Type == "file" {
			dest = filepath.Join(dir, ent.File)
		}
	case "program":
		dest = filepath.Join(e.D.Cache, name+"__"+ent.File)
	default:
		return nil, errGeneric("'%s' has a type this hom does not know: %s", name, ent.Type)
	}

	var existing int64
	if ent.Type == "archive" {
		existing = dirSize(dest)
	} else {
		existing = fileSize(dest)
	}
	src := ent.URL
	if src == "" {
		src = c.repo.URL + c.prefix + escapePath(ent.File)
	}

	// A courtesy question, only on a terminal: this is not a safety gate.
	if e.Interactive() && !e.Yes && !o.Force {
		size := ent.Size
		if size < 0 {
			if h, err := e.HTTP.Head(ctx, src); err == nil && h.Status == 200 {
				size = h.Length
			}
		}
		need := size - existing
		if size >= 0 && need >= 1<<20 {
			q := fmt.Sprintf("'%s' needs about %s of disk space. Continue?", name, FormatBytes(need))
			if !e.ask(q, true) {
				return nil, errDeclined(name, "install of '%s' cancelled", name)
			}
		}
	}

	part := filepath.Join(e.D.Cache, name+"__"+ent.File+".part")
	defer os.Remove(part)
	e.Say("Fetching %s '%s' %s...\n", ent.Type, name, ent.Version)
	pr := e.newProgress()
	dl, err := e.HTTP.Download(ctx, src, part, maxDownload, pr)
	pr.Finish()
	if err != nil {
		if web.IsNet(err) {
			return nil, errNet(name, "the download of '%s' failed: %v", name, err)
		}
		return nil, errGeneric("couldn't save the download of '%s': %v", name, err)
	}
	if dl.Status == 404 {
		return nil, errNet(name, "'%s' is in the index but its file is missing on the server (HTTP 404)", name)
	}
	if dl.Status != 200 {
		return nil, errNet(name, "the download of '%s' failed (HTTP %d)", name, dl.Status)
	}

	// Check before anything is touched (spec section 2, steps 4-5).
	if ent.Size >= 0 && dl.Size != ent.Size {
		return nil, errVerify(name, "'%s' is %d bytes, but the index says %d -- not installed", name, dl.Size, ent.Size)
	}
	if ent.SHA256 != "" {
		if dl.SHA256 != ent.SHA256 {
			return nil, errVerify(name, "the sha256 of '%s' does not match the index (got %s, expected %s) -- not installed",
				name, dl.SHA256, ent.SHA256)
		}
	} else {
		e.Warn("the repo gives no sha256 for '%s'; the download could not be checked", name)
	}

	data := &installData{Package: name, Version: ent.Version, Type: ent.Type, Path: dest, Bytes: dl.Size, SHA256: dl.SHA256}
	switch ent.Type {
	case "plugin", "file":
		if err := moveFile(part, dest); err != nil {
			return nil, errGeneric("couldn't put '%s' in place (%s): %v", name, dest, err)
		}
		if ent.Type == "plugin" {
			e.Say("Installed plugin '%s' -> %s\nRestart HomRec to load it.\n", name, dest)
		} else {
			e.Say("Installed '%s' -> %s (%s)\n", name, dest, FormatBytes(dl.Size))
		}
	case "archive":
		n, err := installArchive(part, dest, o.To != "")
		if err != nil {
			var ue *unz.UnsafeError
			if errors.As(err, &ue) {
				return nil, errVerify(name, "'%s': %v -- nothing was unpacked", name, err)
			}
			return nil, errGeneric("couldn't unpack '%s' into %s: %v", name, dest, err)
		}
		e.Say("Unpacked '%s' (%d files) -> %s\n", name, n, dest)
	case "program":
		if err := e.finishProgram(c, ent, part, dest, o, data); err != nil {
			return nil, err
		}
		if !data.Started {
			return data, nil // saved, nothing to record
		}
	}
	if err := cfg.PutRec(e.dbPath(), cfg.Rec{Name: name, Type: ent.Type, Version: ent.Version, Path: dest}); err != nil {
		e.Warn("couldn't write the list of installed packages: %v", err)
	}
	return data, nil
}

// finishProgram keeps the installer in the cache and, only after a clear yes, starts it.
// "--yes" is not that yes: only --run or a typed "y" is (spec 1.6).
func (e *Env) finishProgram(c *catalog, ent *idx.Entry, part, dest string, o Opts, data *installData) error {
	name := ent.Name
	if err := moveFile(part, dest); err != nil {
		return errGeneric("couldn't save the installer of '%s' (%s): %v", name, dest, err)
	}
	if !canRun(dest) {
		e.Say("Saved '%s' to %s\nhom does not start this kind of file; open or install it yourself.\n", name, dest)
		return nil
	}
	from := ent.URL
	if from == "" {
		from = c.repo.URL + c.prefix + ent.File
	}
	e.Say("\n'%s' %s is a program installer:\n", name, ent.Version)
	if ent.Author != "" {
		e.Say("  author:  %s\n", ent.Author)
	}
	e.Say("  from:    %s\n  size:    %s\n  repo:    %s\n", from, FormatBytes(data.Bytes), c.repo.Name)
	if !strings.EqualFold(c.repo.Name, cfg.OfficialName) {
		e.Say("  This repo is not the official one: hom has not checked who made this program.\n")
	}
	run := o.Run
	if !run && e.Interactive() {
		run = e.ask("Run the installer now?", false)
	}
	if !run {
		return errDeclined(name, "saved '%s' to %s but did not run it; to run it: hom install %s --run", name, dest, name)
	}
	e.Say("Starting the installer...\n")
	code, err := runProgram(dest, ent.Args)
	if err != nil {
		return errGeneric("couldn't start the installer of '%s': %v", name, err)
	}
	if code != 0 && code != 3010 { // 3010: done, a restart is needed
		return errGeneric("the installer of '%s' ended with code %d; it is not recorded as installed", name, code)
	}
	if code == 3010 {
		e.Say("The installer asks for a restart of the computer.\n")
	}
	data.Started = true
	e.Say("Installer of '%s' finished.\n", name)
	return nil
}
