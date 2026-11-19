package app

import (
	"os"
	"path/filepath"

	"github.com/homaaio/hom/pkg/cfg"
	"github.com/homaaio/hom/pkg/idx"
)

// removal collects the paths a remove deleted.
type removal struct{ Paths []string }

// data is the JSON of remove and purge (spec 5.4).
func (r *removal) data(name string) any {
	return map[string]any{"removed": []map[string]any{{"name": name, "paths": r.Paths}}}
}

// cmdRemove removes an installed package. purge also deletes the saved settings of a plugin
// (its .store folder). confirmed is -r (or --yes): removing needs it, as in hom 0.6.x.
func (e *Env) cmdRemove(name string, purge, confirmed bool) (any, error) {
	verb := "remove"
	if purge {
		verb = "purge"
	}
	if err := idx.SafeName(name, true); err != nil {
		return nil, errUsage("refusing the package name '%s': %v", name, err)
	}
	if !confirmed {
		return nil, errUsage("'%s' deletes files; add -r to confirm: hom %s %s -r", verb, verb, name)
	}
	d := &removal{Paths: []string{}}

	db := cfg.LoadDB(e.dbPath())
	if r := cfg.FindRec(db, name); r != nil && r.Type != "plugin" {
		return e.removeRecorded(*r, d)
	}

	pd := e.pluginsDir()
	var hrp string
	if pd != "" {
		for _, ext := range []string{".hrp", ".jar"} {
			if p := filepath.Join(pd, name+ext); isFile(p) {
				hrp = p
				break
			}
		}
	}
	var dirs []string
	if pd != "" {
		for _, dir := range []string{filepath.Join(pd, ".installed", name), filepath.Join(pd, name)} {
			if isDir(dir) {
				dirs = append(dirs, dir)
			}
		}
	}
	if hrp == "" && len(dirs) == 0 {
		hint := ""
		if e.homrecDir() == "" {
			hint = " (No HomRec folder was found, so plugins were not checked -- see 'hom where'.)"
		}
		return nil, errNotFound(name, "no package named '%s' is installed here.%s", name, hint)
	}

	if hrp != "" {
		if err := os.Remove(hrp); err != nil {
			return nil, errGeneric("couldn't delete '%s': %v", hrp, err)
		}
		d.Paths = append(d.Paths, hrp)
		e.Say("Removed %s\n", hrp)
	}
	for _, dir := range dirs {
		kept := false
		if purge {
			if err := os.RemoveAll(dir); err != nil {
				return nil, errGeneric("couldn't fully clean up '%s': %v", dir, err)
			}
		} else {
			var err error
			if kept, err = removeContentsExcept(dir, ".store"); err != nil {
				return nil, errGeneric("couldn't fully clean up '%s': %v", dir, err)
			}
		}
		d.Paths = append(d.Paths, dir)
		if kept {
			e.Say("Removed %s (kept its .store)\n", dir)
		} else {
			e.Say("Removed %s\n", dir)
		}
	}
	if _, err := cfg.DelRec(e.dbPath(), name); err != nil {
		e.Warn("couldn't update the list of installed packages: %v", err)
	}
	if !purge {
		e.Say("Plugin removed. Its saved settings (if any) were kept -- use 'hom purge %s -r' to delete those too.\n", name)
	}
	return d.data(name), nil
}

// removeContentsExcept empties a folder but keeps the entry called keep. It reports whether it was there.
func removeContentsExcept(dir, keep string) (bool, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false, err
	}
	kept := false
	for _, de := range entries {
		if de.Name() == keep {
			kept = true
			continue
		}
		if err := os.RemoveAll(filepath.Join(dir, de.Name())); err != nil {
			return kept, err
		}
	}
	return kept, nil
}

// removeRecorded removes a file, an archive or a saved program installer that the db knows.
func (e *Env) removeRecorded(r cfg.Rec, d *removal) (any, error) {
	switch r.Type {
	case "file":
		if err := os.Remove(r.Path); err != nil && !os.IsNotExist(err) {
			return nil, errGeneric("couldn't delete '%s': %v", r.Path, err)
		}
		d.Paths = append(d.Paths, r.Path)
		// only hom's own per-package folder goes; a folder chosen with --to stays
		dir := filepath.Dir(r.Path)
		if isUnder(dir, filepath.Join(e.D.Data, "files")) {
			os.Remove(dir)
		}
		e.Say("Removed %s\n", r.Path)
	case "archive":
		if isUnder(r.Path, filepath.Join(e.D.Data, "apps")) {
			if err := os.RemoveAll(r.Path); err != nil {
				return nil, errGeneric("couldn't fully remove '%s': %v", r.Path, err)
			}
			d.Paths = append(d.Paths, r.Path)
			e.Say("Removed %s\n", r.Path)
		} else {
			e.Say("'%s' was unpacked into %s, outside hom's data folder. hom leaves that folder alone; "+
				"delete it yourself if you want it gone. The package is forgotten.\n", r.Name, r.Path)
		}
	default: // program
		os.Remove(r.Path)
		if r.Path != "" {
			d.Paths = append(d.Paths, r.Path)
		}
		e.Say("Forgot '%s' and removed its saved installer. To uninstall the program itself, use the system's own tools.\n", r.Name)
	}
	if _, err := cfg.DelRec(e.dbPath(), r.Name); err != nil {
		e.Warn("couldn't update the list of installed packages: %v", err)
	}
	return d.data(r.Name), nil
}
