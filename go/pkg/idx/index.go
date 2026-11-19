package idx

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
)

// MaxSchema is the newest index schema this client understands.
const MaxSchema = 2

// Entry is one package entry of an index (spec 1.3).
type Entry struct {
	Name, Type, File, URL, SHA256, Dest, Args string
	Into                                      string // "homrec": install relative to the HomRec folder
	Description, Version, Author, License     string
	Homepage, OS, Arch, Key, Sig              string
	Size                                      int64 // -1: the entry has no size
}

// Index is a parsed index.json.
type Index struct {
	Schema   int
	Name     string
	Contact  string
	Entries  []Entry
	Warnings []string // entries that were skipped, and why
}

var (
	knownTypes = map[string]bool{"plugin": true, "file": true, "archive": true, "program": true}
	knownOS    = map[string]bool{"windows": true, "linux": true, "macos": true}
	knownArch  = map[string]bool{"amd64": true, "arm64": true, "386": true, "arm": true}
)

func decode(data []byte) (map[string]any, error) {
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, errors.New("extra data after the JSON document")
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, errors.New("the top level is not a JSON object")
	}
	return m, nil
}

// text returns a string, number or bool member as text; "" for anything else.
func text(m map[string]any, key string) string {
	switch v := m[key].(type) {
	case string:
		return v
	case json.Number:
		return v.String()
	case bool:
		if v {
			return "true"
		}
		return "false"
	}
	return ""
}

// Parse reads an index. Invalid JSON is an error; a bad entry is only skipped (Index.Warnings).
// The old layout (a "plugins" array, no schema) reads as schema 1.
func Parse(data []byte) (*Index, error) {
	root, err := decode(data)
	if err != nil {
		return nil, err
	}
	ix := &Index{Schema: 1, Name: text(root, "name"), Contact: text(root, "contact")}
	if s := text(root, "schema"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 1 {
			return nil, fmt.Errorf("bad schema %q", s)
		}
		ix.Schema = n
	}
	arr, ok := root["packages"].([]any)
	if !ok {
		arr, _ = root["plugins"].([]any)
	}
	for i, raw := range arr {
		m, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		e, why := parseEntry(m, ix.Schema)
		if why != "" {
			label := text(m, "name")
			if label == "" {
				label = fmt.Sprintf("#%d", i+1)
			}
			ix.Warnings = append(ix.Warnings, fmt.Sprintf("skipped index entry %s: %s", label, why))
			continue
		}
		ix.Entries = append(ix.Entries, e)
	}
	return ix, nil
}

func parseEntry(m map[string]any, schema int) (Entry, string) {
	e := Entry{
		Name:        text(m, "name"),
		Type:        strings.ToLower(text(m, "type")),
		File:        text(m, "file"),
		URL:         text(m, "url"),
		SHA256:      strings.ToLower(text(m, "sha256")),
		Dest:        text(m, "dest"),
		Into:        strings.ToLower(text(m, "into")),
		Args:        text(m, "args"),
		Description: text(m, "description"),
		Version:     text(m, "version"),
		Author:      text(m, "author"),
		License:     text(m, "license"),
		Homepage:    text(m, "homepage"),
		OS:          strings.ToLower(text(m, "os")),
		Arch:        strings.ToLower(text(m, "arch")),
		Key:         text(m, "key"),
		Sig:         text(m, "sig"),
		Size:        -1,
	}
	if err := SafeName(e.Name, true); err != nil {
		return e, "bad name: " + err.Error()
	}
	if e.Type == "" {
		e.Type = "plugin"
	}
	if !knownTypes[e.Type] {
		return e, "unknown type '" + e.Type + "'"
	}
	if e.OS == "any" {
		e.OS = ""
	}
	if e.Arch == "any" {
		e.Arch = ""
	}
	if e.OS != "" && !knownOS[e.OS] {
		return e, "unknown os '" + e.OS + "'"
	}
	if e.Arch != "" && !knownArch[e.Arch] {
		return e, "unknown arch '" + e.Arch + "'"
	}
	if e.File == "" {
		if e.Type != "plugin" {
			return e, "no 'file'"
		}
		e.File = e.Name + ".hrp"
	}
	if err := SafeName(e.File, false); err != nil {
		return e, "bad file name: " + err.Error()
	}
	if e.Type == "plugin" {
		ext := strings.ToLower(filepath.Ext(e.File))
		if ext != ".hrp" && ext != ".jar" {
			return e, "a plugin file must be .hrp or .jar"
		}
	}
	if e.URL != "" {
		if err := CheckHTTPS(e.URL); err != nil {
			return e, "bad url: " + err.Error()
		}
	}
	if e.Homepage != "" && CheckHTTPS(e.Homepage) != nil {
		e.Homepage = "" // only shown, so a bad one is just dropped
	}
	if e.Dest != "" {
		if _, err := SafeRel(e.Dest); err != nil {
			return e, "bad dest: " + err.Error()
		}
	}
	if e.Into != "" {
		if e.Into != "homrec" {
			return e, "unknown into '" + e.Into + "' (only 'homrec')"
		}
		if e.Type != "file" && e.Type != "archive" {
			return e, "'into' only works for a file or archive package"
		}
	}
	if e.SHA256 != "" && !isHex64(e.SHA256) {
		return e, "sha256 is not 64 hex characters"
	}
	if e.SHA256 == "" && (schema >= 2 || e.Type == "program" || e.Type == "archive") {
		return e, "no sha256"
	}
	if v, ok := m["size"]; ok && v != nil {
		n, err := parseSize(v)
		if err != nil {
			return e, "bad size"
		}
		e.Size = n
	}
	return e, ""
}

func parseSize(v any) (int64, error) {
	var n int64
	var err error
	switch x := v.(type) {
	case json.Number:
		n, err = x.Int64()
	case string:
		n, err = strconv.ParseInt(x, 10, 64)
	default:
		err = errors.New("not a number")
	}
	if err == nil && n < 0 {
		err = errors.New("negative")
	}
	return n, err
}

func isHex64(s string) bool {
	if len(s) != 64 {
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

// CheckHTTPS accepts an https:// address with a host and no user name.
func CheckHTTPS(s string) error {
	u, err := url.Parse(s)
	if err != nil {
		return err
	}
	if !strings.EqualFold(u.Scheme, "https") {
		return errors.New("must be https://")
	}
	if u.Host == "" || u.User != nil {
		return errors.New("no valid host")
	}
	return nil
}

// ErrNotFound means that no entry has the wanted name.
var ErrNotFound = errors.New("no such package")

// NoBuildError means that the name exists, but not for this platform (spec 1.5, step 4).
type NoBuildError struct {
	Name string
	Want Platform
	Have []string
}

func (e *NoBuildError) Error() string {
	return fmt.Sprintf("'%s' has no build for %s (builds: %s)", e.Name, e.Want, strings.Join(e.Have, ", "))
}

func platformOf(e *Entry) string {
	o, a := e.OS, e.Arch
	if o == "" {
		o = "any"
	}
	if a == "" {
		a = "any"
	}
	return o + "/" + a
}

// Pick chooses the entry of a name for a platform (spec 1.5).
func (ix *Index) Pick(name string, p Platform) (*Entry, error) {
	var best *Entry
	bestRank := 99
	var have []string
	found := false
	for i := range ix.Entries {
		e := &ix.Entries[i]
		if e.Name != name {
			continue
		}
		found = true
		have = append(have, platformOf(e))
		if e.OS != "" && e.OS != p.OS {
			continue
		}
		if e.Arch != "" && e.Arch != p.Arch {
			continue
		}
		rank := 3
		switch {
		case e.OS != "" && e.Arch != "":
			rank = 0
		case e.OS != "":
			rank = 1
		case e.Arch != "":
			rank = 2
		}
		if best == nil || rank < bestRank || (rank == bestRank && Gt(e.Version, best.Version)) {
			best, bestRank = e, rank
		}
	}
	if !found {
		return nil, ErrNotFound
	}
	if best == nil {
		return nil, &NoBuildError{Name: name, Want: p, Have: have}
	}
	return best, nil
}

// Names lists every package name once, in the order of the file.
func (ix *Index) Names() []string {
	seen := map[string]bool{}
	var out []string
	for _, e := range ix.Entries {
		if !seen[e.Name] {
			seen[e.Name] = true
			out = append(out, e.Name)
		}
	}
	return out
}

// ManifestVersion reads "version" from a plugin.json; "" when there is none.
func ManifestVersion(data []byte) string {
	m, err := decode(data)
	if err != nil {
		return ""
	}
	return text(m, "version")
}
