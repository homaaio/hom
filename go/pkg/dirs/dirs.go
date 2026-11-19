// Package dirs decides where hom keeps its settings, data and downloads (spec section 6).
package dirs

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
)

// File names inside the settings folder.
const (
	CfgName   = "hom.cfg"
	ReposName = "hom_repos.cfg"
	DBName    = "hom_db.cfg"
)

// Dirs is the set of folders hom works in.
type Dirs struct {
	Config   string // hom.cfg, hom_repos.cfg, hom_db.cfg
	Data     string // files/, apps/
	Cache    string // downloads
	Portable bool
	Root     string // the portable folder, "" when not portable

	cacheInData bool // the cache is <data>/dl and follows the data folder
}

// Resolve picks the folders. root is the --root option ("" = none), exeDir is the folder of the
// running binary (used to find the portable mode).
func Resolve(root, exeDir string) (*Dirs, error) {
	if root != "" {
		abs, err := filepath.Abs(root)
		if err != nil {
			return nil, err
		}
		return portable(abs), nil
	}
	if exeDir != "" && isPortable(exeDir) {
		return portable(exeDir), nil
	}
	return system()
}

// SetData moves the data folder (the "data" setting). A cache that lives inside it moves too.
func (d *Dirs) SetData(dir string) {
	d.Data = dir
	if d.cacheInData {
		d.Cache = filepath.Join(dir, "dl")
	}
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func isPortable(dir string) bool {
	names := []string{CfgName, "hom.portable"}
	if runtime.GOOS == "windows" {
		names = append(names, "cfg", "hr.exe", "homrec.exe") // the 0.6.x layout
	}
	for _, n := range names {
		if exists(filepath.Join(dir, n)) {
			return true
		}
	}
	return false
}

func portable(root string) *Dirs {
	cfgDir := root
	if st, err := os.Stat(filepath.Join(root, "cfg")); err == nil && st.IsDir() {
		cfgDir = filepath.Join(root, "cfg")
	}
	data := filepath.Join(root, "hom_data")
	return &Dirs{
		Config:      cfgDir,
		Data:        data,
		Cache:       filepath.Join(data, "dl"),
		Portable:    true,
		Root:        root,
		cacheInData: true,
	}
}

func system() (*Dirs, error) {
	home, herr := os.UserHomeDir()
	switch runtime.GOOS {
	case "windows":
		la := os.Getenv("LOCALAPPDATA")
		if la == "" {
			if herr != nil {
				return nil, herr
			}
			la = filepath.Join(home, "AppData", "Local")
		}
		d := filepath.Join(la, "hom")
		return &Dirs{Config: d, Data: d, Cache: filepath.Join(d, "dl"), cacheInData: true}, nil
	case "darwin":
		if herr != nil {
			return nil, herr
		}
		lib := filepath.Join(home, "Library")
		d := filepath.Join(lib, "Application Support", "hom")
		return &Dirs{Config: d, Data: d, Cache: filepath.Join(lib, "Caches", "hom")}, nil
	}
	pick := func(env, rel string) (string, error) {
		if v := os.Getenv(env); v != "" && filepath.IsAbs(v) {
			return filepath.Join(v, "hom"), nil
		}
		if herr != nil {
			return "", errors.New("can't find the home folder: " + herr.Error())
		}
		return filepath.Join(home, rel, "hom"), nil
	}
	cfgDir, err := pick("XDG_CONFIG_HOME", ".config")
	if err != nil {
		return nil, err
	}
	dataDir, err := pick("XDG_DATA_HOME", filepath.Join(".local", "share"))
	if err != nil {
		return nil, err
	}
	cacheDir, err := pick("XDG_CACHE_HOME", ".cache")
	if err != nil {
		return nil, err
	}
	return &Dirs{Config: cfgDir, Data: dataDir, Cache: cacheDir}, nil
}
