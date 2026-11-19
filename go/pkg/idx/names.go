// Package idx reads package indexes (spec section 1): entries, names, versions, platform choice.
package idx

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

// SafeName checks a package name or a file name (spec 1.4), the same way on every OS.
// pkg=true also refuses a leading '.', which only a file name may have.
func SafeName(name string, pkg bool) error {
	switch {
	case name == "":
		return errors.New("name is empty")
	case len(name) > 100:
		return errors.New("name is longer than 100 bytes")
	case strings.Contains(name, ".."):
		return errors.New("contains '..'")
	case name == ".":
		return errors.New("is '.'")
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		if c < 0x20 || c == 0x7f || strings.IndexByte("/\\:\"<>|*?", c) >= 0 {
			return errors.New("contains an illegal character")
		}
	}
	if pkg && name[0] == '.' {
		return errors.New("starts with '.'")
	}
	last := name[len(name)-1]
	if last == '.' || last == ' ' {
		return errors.New("ends with '.' or a space")
	}
	stem := name
	if i := strings.IndexByte(stem, '.'); i >= 0 {
		stem = stem[:i]
	}
	stem = strings.ToUpper(strings.TrimRight(stem, " "))
	switch stem {
	case "CON", "PRN", "AUX", "NUL":
		return errors.New("is a Windows device name")
	}
	if len(stem) == 4 && (stem[:3] == "COM" || stem[:3] == "LPT") && stem[3] >= '1' && stem[3] <= '9' {
		return errors.New("is a Windows device name")
	}
	return nil
}

// SafeRel checks a 'dest' folder (spec 1.3): relative, no '..', no drive letter.
// It returns the folder as a path of this OS.
func SafeRel(rel string) (string, error) {
	if rel == "" || len(rel) > 120 {
		return "", errors.New("empty or longer than 120 bytes")
	}
	r := strings.ReplaceAll(rel, "\\", "/")
	if strings.HasPrefix(r, "/") || strings.HasSuffix(r, "/") {
		return "", errors.New("must be a relative folder")
	}
	parts := strings.Split(r, "/")
	for _, p := range parts {
		if err := SafeName(p, false); err != nil {
			return "", fmt.Errorf("%q: %v", p, err)
		}
	}
	return filepath.Join(parts...), nil
}
