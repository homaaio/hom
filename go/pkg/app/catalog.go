package app

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/homaaio/hom/pkg/cfg"
	"github.com/homaaio/hom/pkg/idx"
)

const (
	maxIndex    = 64 << 20 // an index larger than this is refused (spec 1.2)
	maxDownload = 4 << 30
)

// catalog is a fetched index with the repo it came from.
type catalog struct {
	ix     *idx.Index
	repo   cfg.Repo
	prefix string // "plugins/" for a repo that still has the old layout
}

func (e *Env) fetchCatalog(ctx context.Context) (*catalog, error) {
	repo := e.activeRepo()
	if !strings.EqualFold(repo.Name, cfg.OfficialName) {
		e.Say("hom: using repo '%s' (%s)\n", repo.Name, repo.URL)
	}
	r, err := e.HTTP.GetRetry(ctx, repo.URL+"index.json", maxIndex)
	prefix := ""
	if err == nil && r.Status == 404 {
		if r2, err2 := e.HTTP.GetRetry(ctx, repo.URL+"plugins/index.json", maxIndex); err2 == nil && r2.Status == 200 {
			r, prefix = r2, "plugins/"
		}
	}
	if err != nil {
		return nil, errNet("", "couldn't fetch the index of repo '%s': %v", repo.Name, err)
	}
	if r.Status != 200 {
		return nil, errNet("", "couldn't fetch the index of repo '%s' (HTTP %d)", repo.Name, r.Status)
	}
	if len(bytes.TrimSpace(r.Body)) == 0 {
		return nil, errNet("", "the index of repo '%s' is empty", repo.Name)
	}
	ix, perr := idx.Parse(r.Body)
	if perr != nil {
		return nil, errNet("", "the index of repo '%s' is not valid: %v", repo.Name, perr)
	}
	for _, w := range ix.Warnings {
		e.Warn("%s", w)
	}
	return &catalog{ix: ix, repo: repo, prefix: prefix}, nil
}

// needSupported refuses to install from an index newer than this hom understands (spec 1.2).
func (c *catalog) needSupported() error {
	if c.ix.Schema > idx.MaxSchema {
		return errSchema("the index of repo '%s' uses schema %d, newer than this hom understands (%d) -- run 'hom update'",
			c.repo.Name, c.ix.Schema, idx.MaxSchema)
	}
	return nil
}

// pick chooses the entry of a package for this computer.
func (e *Env) pick(c *catalog, name string) (*idx.Entry, error) {
	ent, err := c.ix.Pick(name, e.Plat)
	if err == nil {
		return ent, nil
	}
	if errors.Is(err, idx.ErrNotFound) {
		return nil, errNotFound(name, "no package named '%s' in repo '%s'", name, c.repo.Name)
	}
	return nil, errNotFound(name, "%v", err)
}

// local is something installed on this computer.
type local struct {
	Name    string
	Kind    string // hrp | jar | folder (plugins), or file | archive | program (from the db)
	Version string // "" when unknown
	Path    string
}

func (l local) isPlugin() bool { return l.Kind == "hrp" || l.Kind == "jar" || l.Kind == "folder" }

func (e *Env) pluginsDir() string {
	h := e.homrecDir()
	if h == "" {
		return ""
	}
	return filepath.Join(h, "plugins")
}

// pluginVersion looks for the version of a plugin: its plugin.json (HomRec unpacks one), then the db.
func (e *Env) pluginVersion(name string, db []cfg.Rec) string {
	if pd := e.pluginsDir(); pd != "" {
		for _, dir := range []string{filepath.Join(pd, ".installed", name), filepath.Join(pd, name)} {
			if b, err := os.ReadFile(filepath.Join(dir, "plugin.json")); err == nil {
				if v := idx.ManifestVersion(b); v != "" {
					return v
				}
			}
		}
	}
	if r := cfg.FindRec(db, name); r != nil {
		return r.Version
	}
	return ""
}

// listLocal lists what is installed: the plugins found in the plugins folder, and the rest from the db.
func (e *Env) listLocal() []local {
	db := cfg.LoadDB(e.dbPath())
	var out []local
	if pd := e.pluginsDir(); pd != "" {
		entries, _ := os.ReadDir(pd)
		for _, de := range entries {
			name := de.Name()
			full := filepath.Join(pd, name)
			if de.IsDir() {
				if name == ".installed" || !isFile(filepath.Join(full, "plugin.json")) {
					continue
				}
				out = append(out, local{Name: name, Kind: "folder", Path: full})
				continue
			}
			ext := strings.ToLower(filepath.Ext(name))
			if ext != ".hrp" && ext != ".jar" {
				continue
			}
			out = append(out, local{Name: strings.TrimSuffix(name, filepath.Ext(name)), Kind: ext[1:], Path: full})
		}
	}
	for i := range out {
		out[i].Version = e.pluginVersion(out[i].Name, db)
	}
	for _, r := range db {
		if r.Type != "plugin" {
			out = append(out, local{Name: r.Name, Kind: r.Type, Version: r.Version, Path: r.Path})
		}
	}
	return out
}
