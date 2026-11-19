// Package cfg reads and writes hom's small files: settings (hom.cfg), repos (hom_repos.cfg)
// and the list of installed packages (hom_db.cfg). The formats are those of hom 0.6.x.
package cfg

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// WriteFile writes a file so that a reader never sees half of it: the folder is created,
// the data goes to a temporary file, which then replaces the target.
func WriteFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

func readText(path string) (string, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	return strings.TrimPrefix(string(b), "\ufeff"), true
}

// LoadKv reads "key value" lines; empty lines and lines starting with '#' are ignored.
func LoadKv(path string) map[string]string {
	kv := map[string]string{}
	s, ok := readText(path)
	if !ok {
		return kv
	}
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || line[0] == '#' {
			continue
		}
		i := strings.IndexAny(line, " \t")
		if i < 0 {
			continue
		}
		kv[line[:i]] = strings.TrimSpace(line[i+1:])
	}
	return kv
}

// SaveKv writes "key value" lines, sorted by key. A value with a line break is cleaned.
func SaveKv(path string, kv map[string]string) error {
	keys := make([]string, 0, len(kv))
	for k := range kv {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		b.WriteString(k + " " + strings.NewReplacer("\r", " ", "\n", " ").Replace(kv[k]) + "\n")
	}
	return WriteFile(path, []byte(b.String()))
}

// Rec is one installed package: name|type|version|path.
type Rec struct {
	Name, Type, Version, Path string
}

func cleanField(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '|' || r == '\r' || r == '\n' {
			return '_'
		}
		return r
	}, s)
}

// LoadDB reads the list of installed packages. A missing file is an empty list.
func LoadDB(path string) []Rec {
	s, ok := readText(path)
	if !ok {
		return nil
	}
	var out []Rec
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || line[0] == '#' {
			continue
		}
		p := strings.Split(line, "|")
		if len(p) != 4 || p[0] == "" {
			continue
		}
		out = append(out, Rec{Name: p[0], Type: p[1], Version: p[2], Path: p[3]})
	}
	return out
}

// SaveDB writes the list of installed packages.
func SaveDB(path string, recs []Rec) error {
	var b strings.Builder
	for _, r := range recs {
		b.WriteString(cleanField(r.Name) + "|" + cleanField(r.Type) + "|" + cleanField(r.Version) + "|" + cleanField(r.Path) + "\n")
	}
	return WriteFile(path, []byte(b.String()))
}

// FindRec returns the record of a name (nil if there is none).
func FindRec(recs []Rec, name string) *Rec {
	for i := range recs {
		if recs[i].Name == name {
			return &recs[i]
		}
	}
	return nil
}

// PutRec adds a record or replaces the one with the same name.
func PutRec(path string, r Rec) error {
	recs := LoadDB(path)
	replaced := false
	for i := range recs {
		if recs[i].Name == r.Name {
			recs[i] = r
			replaced = true
		}
	}
	if !replaced {
		recs = append(recs, r)
	}
	return SaveDB(path, recs)
}

// DelRec removes the record of a name. It reports whether there was one.
func DelRec(path, name string) (bool, error) {
	recs := LoadDB(path)
	out := recs[:0:0]
	for _, r := range recs {
		if r.Name != name {
			out = append(out, r)
		}
	}
	if len(out) == len(recs) {
		return false, nil
	}
	return true, SaveDB(path, out)
}
