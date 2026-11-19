package app

import (
	"context"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/homaaio/hom/pkg/idx"
)

// pkgJSON is a package in the JSON output (spec 5.4). A missing value is null, never "".
type pkgJSON struct {
	Name        string  `json:"name"`
	Type        string  `json:"type"`
	Version     *string `json:"version"`
	Description *string `json:"description"`
	Author      *string `json:"author"`
	License     *string `json:"license"`
	Homepage    *string `json:"homepage"`
	OS          *string `json:"os"`
	Arch        *string `json:"arch"`
	Size        *int64  `json:"size"`
	SHA256      *string `json:"sha256"`
	Into        *string `json:"into"`
	Repo        string  `json:"repo"`
}

func newPkgJSON(ent *idx.Entry, repo string) pkgJSON {
	return pkgJSON{
		Name:        ent.Name,
		Type:        ent.Type,
		Version:     ptr(ent.Version),
		Description: ptr(ent.Description),
		Author:      ptr(ent.Author),
		License:     ptr(ent.License),
		Homepage:    ptr(ent.Homepage),
		OS:          ptr(ent.OS),
		Arch:        ptr(ent.Arch),
		Size:        ptrSize(ent.Size),
		SHA256:      ptr(ent.SHA256),
		Into:        ptr(ent.Into),
		Repo:        repo,
	}
}

func findLocal(ls []local, name string) *local {
	for i := range ls {
		if ls[i].Name == name {
			return &ls[i]
		}
	}
	return nil
}

func isUpgradable(ent *idx.Entry, l *local) bool {
	return l != nil && l.Version != "" && idx.Gt(ent.Version, l.Version)
}

func localType(kind string) string {
	switch kind {
	case "hrp", "jar", "folder":
		return "plugin"
	}
	return kind
}

func orQ(s string) string {
	if s == "" {
		return "?"
	}
	return s
}

func (e *Env) cmdSearch(ctx context.Context, query string) (any, error) {
	c, err := e.fetchCatalog(ctx)
	if err != nil {
		return nil, err
	}
	re, rerr := regexp.Compile("(?i)" + query)
	match := func(s string) bool {
		if rerr == nil {
			return re.MatchString(s)
		}
		return strings.Contains(strings.ToLower(s), strings.ToLower(query))
	}
	hits := []pkgJSON{}
	for _, n := range c.ix.Names() {
		ent, perr := c.ix.Pick(n, e.Plat)
		if perr != nil {
			continue
		}
		if match(ent.Name) || match(ent.Description) {
			hits = append(hits, newPkgJSON(ent, c.repo.Name))
			e.Say("  %-22s %-10s %-8s %s\n", ent.Name, orQ(ent.Version), ent.Type, ent.Description)
		}
	}
	if len(hits) == 0 {
		e.Say("No packages match '%s'.\n", query)
	}
	return map[string]any{"query": query, "packages": hits}, nil
}

type installedJSON struct {
	Version *string `json:"version"`
	Path    string  `json:"path"`
	Time    *string `json:"time"`
}

type showJSON struct {
	pkgJSON
	Installed  *installedJSON `json:"installed"`
	Upgradable bool           `json:"upgradable"`
}

func (e *Env) cmdShow(ctx context.Context, name string) (any, error) {
	if err := idx.SafeName(name, true); err != nil {
		return nil, errUsage("bad package name '%s': %v", name, err)
	}
	c, err := e.fetchCatalog(ctx)
	if err != nil {
		return nil, err
	}
	ent, err := e.pick(c, name)
	if err != nil {
		return nil, err
	}
	src := ent.URL
	if src == "" {
		src = c.repo.URL + c.prefix + escapePath(ent.File)
	}
	if ent.Size < 0 {
		if h, herr := e.HTTP.Head(ctx, src); herr == nil && h.Status == 200 && h.Length >= 0 {
			ent.Size = h.Length
		}
	}
	loc := findLocal(e.listLocal(), name)

	d := showJSON{pkgJSON: newPkgJSON(ent, c.repo.Name), Upgradable: isUpgradable(ent, loc)}
	e.Say("Name:        %s\n", ent.Name)
	e.Say("Version:     %s\n", orQ(ent.Version))
	e.Say("Type:        %s\n", ent.Type)
	if ent.Description != "" {
		e.Say("About:       %s\n", ent.Description)
	}
	if ent.Author != "" {
		e.Say("Author:      %s\n", ent.Author)
	}
	if ent.License != "" {
		e.Say("License:     %s\n", ent.License)
	}
	if ent.Homepage != "" {
		e.Say("Homepage:    %s\n", ent.Homepage)
	}
	if ent.OS != "" || ent.Arch != "" {
		e.Say("Platform:    %s\n", platformText(ent))
	}
	if ent.Size >= 0 {
		e.Say("Size:        %s\n", FormatBytes(ent.Size))
	}
	if ent.Into == "homrec" {
		sub := ent.Dest
		if sub == "" {
			sub = ent.Name
		}
		e.Say("Into:        HomRec folder (%s)\n", sub)
	}
	if ent.SHA256 != "" {
		e.Say("SHA-256:     %s\n", ent.SHA256)
	}
	e.Say("Repo:        %s\n", c.repo.Name)
	if loc != nil {
		d.Installed = &installedJSON{Version: ptr(loc.Version), Path: loc.Path}
		if st, serr := os.Stat(loc.Path); serr == nil {
			t := st.ModTime().UTC().Format(time.RFC3339)
			d.Installed.Time = &t
		}
		e.Say("Installed:   %s at %s\n", orQ(loc.Version), loc.Path)
		if d.Upgradable {
			e.Say("Upgrade:     %s is available (hom upgrade)\n", ent.Version)
		}
	} else {
		e.Say("Installed:   no\n")
	}
	return map[string]any{"package": d}, nil
}

func platformText(ent *idx.Entry) string {
	o, a := ent.OS, ent.Arch
	if o == "" {
		o = "any"
	}
	if a == "" {
		a = "any"
	}
	return o + "/" + a
}

type listItem struct {
	pkgJSON
	Installed        bool    `json:"installed"`
	InstalledVersion *string `json:"installed_version"`
	Upgradable       bool    `json:"upgradable"`
}

func (e *Env) cmdList(ctx context.Context, o Opts) (any, error) {
	locals := e.listLocal()
	var c *catalog
	if !o.Installed || o.Upgradable { // --installed alone works offline
		var err error
		if c, err = e.fetchCatalog(ctx); err != nil {
			return nil, err
		}
	}
	items := []listItem{}
	inRepo := map[string]bool{}
	if c != nil {
		for _, n := range c.ix.Names() {
			ent, perr := c.ix.Pick(n, e.Plat)
			if perr != nil {
				continue
			}
			inRepo[n] = true
			loc := findLocal(locals, n)
			up := isUpgradable(ent, loc)
			if (o.Installed && loc == nil) || (o.Upgradable && !up) {
				continue
			}
			it := listItem{pkgJSON: newPkgJSON(ent, c.repo.Name), Installed: loc != nil, Upgradable: up}
			if loc != nil {
				it.InstalledVersion = ptr(loc.Version)
			}
			items = append(items, it)
		}
	}
	if o.Installed && !o.Upgradable {
		for _, l := range locals {
			if inRepo[l.Name] {
				continue
			}
			items = append(items, listItem{
				pkgJSON:          pkgJSON{Name: l.Name, Type: localType(l.Kind)},
				Installed:        true,
				InstalledVersion: ptr(l.Version),
			})
		}
	}

	for _, it := range items {
		switch {
		case o.Upgradable:
			e.Say("  %s: %s -> %s\n", it.Name, orQ(deref(it.InstalledVersion)), orQ(deref(it.Version)))
		case o.Installed:
			e.Say("  %-22s %-10s %s\n", it.Name, orQ(deref(it.InstalledVersion)), it.Type)
		default:
			mark := " "
			if it.Installed {
				mark = "*"
			}
			e.Say("%s %-22s %-10s %-8s %s\n", mark, it.Name, orQ(deref(it.Version)), it.Type, deref(it.Description))
		}
	}
	if len(items) == 0 {
		switch {
		case o.Upgradable:
			e.Say("Everything is up to date.\n")
		case o.Installed:
			e.Say("Nothing is installed.\n")
		default:
			e.Say("The repo has no packages for this computer.\n")
		}
	}
	return map[string]any{"packages": items}, nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
