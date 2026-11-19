package idx

import (
	"runtime"
	"strings"
)

// Cmp compares two versions as in spec 1.7: parts split by '.', compared as numbers, a missing
// part is 0, a leading 'v' is ignored, only the leading digits of a part count.
// It returns -1, 0 or 1.
func Cmp(a, b string) int {
	va, vb := parseVer(a), parseVer(b)
	n := len(va)
	if len(vb) > n {
		n = len(vb)
	}
	for i := 0; i < n; i++ {
		var x, y int64
		if i < len(va) {
			x = va[i]
		}
		if i < len(vb) {
			y = vb[i]
		}
		if x > y {
			return 1
		}
		if x < y {
			return -1
		}
	}
	return 0
}

// Gt reports whether version a is newer than b.
func Gt(a, b string) bool { return Cmp(a, b) > 0 }

func parseVer(s string) []int64 {
	s = strings.TrimSpace(s)
	if s != "" && (s[0] == 'v' || s[0] == 'V') {
		s = s[1:]
	}
	parts := strings.Split(s, ".")
	out := make([]int64, len(parts))
	for i, p := range parts {
		var n int64
		for j := 0; j < len(p) && p[j] >= '0' && p[j] <= '9'; j++ {
			n = n*10 + int64(p[j]-'0')
			if n > 1_000_000_000_000 {
				break
			}
		}
		out[i] = n
	}
	return out
}

// Platform is an OS and a CPU in the names of the spec (windows|linux|macos, amd64|arm64|386|arm).
type Platform struct{ OS, Arch string }

// Here is the platform this program runs on.
func Here() Platform {
	os := runtime.GOOS
	if os == "darwin" {
		os = "macos"
	}
	return Platform{OS: os, Arch: runtime.GOARCH}
}

func (p Platform) String() string { return p.OS + "/" + p.Arch }
