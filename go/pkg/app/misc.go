package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/homaaio/hom/pkg/cfg"
	"github.com/homaaio/hom/pkg/dirs"
)

func (e *Env) cmdWhere() (any, error) {
	repo := e.activeRepo()
	hr := e.homrecDir()
	mode := "installed (per-user folders)"
	if e.D.Portable {
		mode = "portable (everything beside hom)"
	}
	e.Say("hom %s\n", e.Version)
	e.Say("Mode:      %s\n", mode)
	e.Say("Settings:  %s\n", e.D.Config)
	e.Say("Data:      %s\n", e.D.Data)
	e.Say("Cache:     %s\n", e.D.Cache)
	e.Say("Repo:      %s (%s)\n", repo.Name, repo.URL)
	if hr == "" {
		e.Say("HomRec:    not found (tell hom with: hom set homrec <folder>)\n")
	} else {
		e.Say("HomRec:    %s\n", hr)
		e.Say("Plugins:   %s\n", filepath.Join(hr, "plugins"))
	}
	return map[string]any{
		"data":     e.D.Data,
		"config":   e.D.Config,
		"cache":    e.D.Cache,
		"portable": e.D.Portable,
		"repo":     map[string]string{"name": repo.Name, "url": repo.URL},
		"homrec":   ptr(hr),
		"version":  e.Version,
	}, nil
}

// cmdSet reads or writes a setting of hom.cfg: homrec (where HomRec is) or data (where files go).
func (e *Env) cmdSet(args []string) (any, error) {
	usage := errUsage("usage: hom set <homrec|data> [<folder>|auto]")
	if len(args) < 1 || len(args) > 2 {
		return nil, usage
	}
	key := args[0]
	if key != "homrec" && key != "data" {
		return nil, usage
	}
	path := e.cfgPath(dirs.CfgName)
	kv := cfg.LoadKv(path)
	if len(args) == 1 {
		cur := kv[key]
		if cur == "" {
			cur = "(automatic)"
		}
		e.Say("%s: %s\n", key, cur)
		return struct{}{}, nil
	}
	val := args[1]
	if val == "auto" {
		delete(kv, key)
		if err := cfg.SaveKv(path, kv); err != nil {
			return nil, errGeneric("couldn't write %s: %v", path, err)
		}
		e.Say("%s: back to automatic detection.\n", key)
		return struct{}{}, nil
	}
	abs, err := filepath.Abs(val)
	if err != nil {
		return nil, errGeneric("bad folder '%s': %v", val, err)
	}
	if key == "homrec" {
		if !isDir(abs) {
			return nil, errGeneric("'%s' is not a folder", abs)
		}
		if !LooksLikeHomrec(abs) {
			e.Warn("there is no hr.exe or homrec.exe in %s; saved anyway", abs)
		}
	}
	kv[key] = abs
	if err := cfg.SaveKv(path, kv); err != nil {
		return nil, errGeneric("couldn't write %s: %v", path, err)
	}
	e.Say("%s: %s\n", key, abs)
	if key == "data" {
		e.Say("New downloads go there; what is already installed stays where it is.\n")
	}
	return struct{}{}, nil
}

func (e *Env) cmdClean() (any, error) {
	cache := e.D.Cache
	if cache == "" || filepath.Dir(cache) == cache {
		return nil, errGeneric("the cache folder is not set up")
	}
	if !isDir(cache) {
		e.Say("Nothing to clean (%s does not exist).\n", cache)
		return map[string]any{"freed": int64(0)}, nil
	}
	freed := dirSize(cache)
	if err := os.RemoveAll(cache); err != nil {
		return nil, errGeneric("couldn't fully clean %s: %v", cache, err)
	}
	e.Say("Cleaned %s (freed %s).\n", cache, FormatBytes(freed))
	return map[string]any{"freed": freed}, nil
}

func (e *Env) cmdPing(ctx context.Context) (any, error) {
	repo := e.activeRepo()
	start := time.Now()
	c, err := e.fetchCatalog(ctx)
	ms := time.Since(start).Milliseconds()
	if err != nil {
		return map[string]any{"repo": repo.Name, "url": repo.URL, "reachable": false}, err
	}
	e.Say("OK: repo '%s' answered in %d ms (%d packages).\n", repo.Name, ms, len(c.ix.Entries))
	return map[string]any{"repo": repo.Name, "url": repo.URL, "reachable": true, "status": 200, "ms": ms}, nil
}

func (e *Env) cmdRepo(args []string) (any, error) {
	sub := "list"
	if len(args) > 0 {
		sub, args = args[0], args[1:]
	}
	path := e.cfgPath(dirs.ReposName)
	rc := cfg.LoadRepoConfig(path)
	save := func() error {
		if err := cfg.SaveRepoConfig(path, rc); err != nil {
			return errGeneric("couldn't write %s: %v", path, err)
		}
		return nil
	}
	switch sub {
	case "list":
		active := rc.ActiveRepo()
		type row struct {
			Name     string `json:"name"`
			URL      string `json:"url"`
			Official bool   `json:"official"`
		}
		rows := []row{{cfg.OfficialName, cfg.OfficialURL, true}}
		for _, r := range rc.Repos {
			rows = append(rows, row{r.Name, r.URL, false})
		}
		for _, r := range rows {
			mark := " "
			if strings.EqualFold(r.Name, active.Name) {
				mark = "*"
			}
			e.Say("%s %-14s %s\n", mark, r.Name, r.URL)
		}
		if !strings.EqualFold(rc.Active, cfg.OfficialName) && rc.Find(rc.Active) == nil {
			e.Say("The saved active repo '%s' no longer exists -- using official.\n", rc.Active)
		}
		return map[string]any{"active": active.Name, "repos": rows}, nil

	case "add":
		if len(args) != 2 {
			return nil, errUsage("usage: hom repo add <name> <https-url | owner/repo[@branch]>")
		}
		name := args[0]
		if err := cfg.CheckRepoName(name); err != nil {
			return nil, errUsage("bad repo name '%s' (%v)", name, err)
		}
		u, err := cfg.NormalizeRepoURL(args[1])
		if err != nil {
			return nil, errUsage("bad repo URL (%v)", err)
		}
		if rc.Find(name) != nil {
			return nil, errGeneric("a repo named '%s' already exists (hom repo remove %s, or pick another name)", name, name)
		}
		rc.Repos = append(rc.Repos, cfg.Repo{Name: name, URL: u})
		if err := save(); err != nil {
			return nil, err
		}
		e.Say("Added repo '%s' -> %s\nUse 'hom repo set %s' to download from it.\n", name, u, name)
		return struct{}{}, nil

	case "set":
		if len(args) != 1 {
			return nil, errUsage("usage: hom repo set <name>   (see 'hom repo list')")
		}
		var target, u string
		if strings.EqualFold(args[0], cfg.OfficialName) {
			target, u = cfg.OfficialName, cfg.OfficialURL
		} else if r := rc.Find(args[0]); r != nil {
			target, u = r.Name, r.URL
		} else {
			return nil, errNotFound("", "no repo named '%s' (hom repo list; add it with hom repo add)", args[0])
		}
		rc.Active = target
		if err := save(); err != nil {
			return nil, err
		}
		e.Say("Now using repo '%s' (%s)\nInstalled packages stay as they are; install and upgrade now download from this repo.\n", target, u)
		return struct{}{}, nil

	case "remove":
		if len(args) != 1 {
			return nil, errUsage("usage: hom repo remove <name>")
		}
		if strings.EqualFold(args[0], cfg.OfficialName) {
			return nil, errGeneric("the official repo is built in and can't be removed")
		}
		at := -1
		for i, r := range rc.Repos {
			if strings.EqualFold(r.Name, args[0]) {
				at = i
			}
		}
		if at < 0 {
			return nil, errNotFound("", "no repo named '%s'", args[0])
		}
		removed := rc.Repos[at].Name
		rc.Repos = append(rc.Repos[:at], rc.Repos[at+1:]...)
		wasActive := strings.EqualFold(rc.Active, removed)
		if wasActive {
			rc.Active = cfg.OfficialName
		}
		if err := save(); err != nil {
			return nil, err
		}
		e.Say("Removed repo '%s'.\n", removed)
		if wasActive {
			e.Say("It was the one in use -- switched back to 'official'.\n")
		}
		return struct{}{}, nil
	}
	return nil, errUsage("usage: hom repo list | add <name> <url> | set <name> | remove <name>")
}
