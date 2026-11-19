package cfg

import (
	"errors"
	"strings"
)

// The built-in official repo.
const (
	OfficialName = "official"
	OfficialURL  = "https://raw.githubusercontent.com/homaaio/hom/main/pkgs/"
)

// Repo is a package repository: the folder that holds index.json and the package files.
type Repo struct {
	Name string
	URL  string // https://..., ends with '/'
}

// RepoConfig is hom_repos.cfg: the repos the user added and the one in use.
type RepoConfig struct {
	Active string
	Repos  []Repo
}

func isAlnum(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

// CheckRepoName keeps repo names boring: letters, digits, '.', '-', '_'.
func CheckRepoName(name string) error {
	if name == "" {
		return errors.New("name is empty")
	}
	if len(name) > 32 {
		return errors.New("name is longer than 32 characters")
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		if !(isAlnum(c) || c == '.' || c == '-' || c == '_') {
			return errors.New("only letters, digits, '.', '-' and '_' are allowed")
		}
	}
	if strings.EqualFold(name, OfficialName) {
		return errors.New("'official' is reserved")
	}
	return nil
}

// NormalizeRepoURL turns what the user typed into the canonical base URL of a repo.
// It accepts an https:// URL or the GitHub shorthand owner/repo[@branch].
func NormalizeRepoURL(input string) (string, error) {
	s := strings.TrimSpace(input)
	if s == "" {
		return "", errors.New("URL is empty")
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c <= 0x20 || c == 0x7f || strings.IndexByte("\"'\\<>", c) >= 0 {
			return "", errors.New("URL contains a space or an illegal character")
		}
	}
	if strings.Contains(s, "..") {
		return "", errors.New("URL contains '..'")
	}
	lower := strings.ToLower(s)
	if strings.HasPrefix(lower, "http://") {
		return "", errors.New("only https:// repositories are allowed (hom downloads executables from them)")
	}
	if strings.HasPrefix(lower, "https://") {
		rest := s[len("https://"):]
		host := rest
		if i := strings.IndexByte(rest, '/'); i >= 0 {
			host = rest[:i]
		}
		if host == "" || strings.Contains(host, "@") {
			return "", errors.New("URL has no valid host")
		}
		if !strings.HasSuffix(s, "/") {
			s += "/"
		}
		return s, nil
	}
	if strings.Contains(s, "://") {
		return "", errors.New("only https:// repositories are allowed")
	}

	// owner/repo[@branch]
	branch := "main"
	repoPart := s
	if at := strings.IndexByte(s, '@'); at >= 0 {
		repoPart = s[:at]
		branch = s[at+1:]
		if branch == "" {
			return "", errors.New("branch after '@' is empty")
		}
	}
	slash := strings.IndexByte(repoPart, '/')
	if slash <= 0 || slash+1 >= len(repoPart) || strings.IndexByte(repoPart[slash+1:], '/') >= 0 {
		return "", errors.New("expected a https:// URL or GitHub shorthand 'owner/repo' (or 'owner/repo@branch')")
	}
	for i := 0; i < len(repoPart); i++ {
		c := repoPart[i]
		if !(isAlnum(c) || c == '.' || c == '-' || c == '_' || c == '/') {
			return "", errors.New("owner/repo contains an illegal character")
		}
	}
	for i := 0; i < len(branch); i++ {
		c := branch[i]
		if !(isAlnum(c) || c == '.' || c == '-' || c == '_' || c == '/') {
			return "", errors.New("branch contains an illegal character")
		}
	}
	return "https://raw.githubusercontent.com/" + repoPart + "/" + branch + "/pkgs/", nil
}

// LoadRepoConfig reads hom_repos.cfg; a missing file is the default (official).
// Hand-edited junk is skipped, not trusted.
func LoadRepoConfig(path string) RepoConfig {
	rc := RepoConfig{Active: OfficialName}
	s, ok := readText(path)
	if !ok {
		return rc
	}
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || line[0] == '#' {
			continue
		}
		key, rest := line, ""
		if i := strings.IndexAny(line, " \t"); i >= 0 {
			key, rest = line[:i], strings.TrimSpace(line[i+1:])
		}
		switch key {
		case "active":
			if rest != "" {
				rc.Active = rest
			}
		case "repo":
			i := strings.IndexAny(rest, " \t")
			if i < 0 {
				continue
			}
			name := rest[:i]
			if CheckRepoName(name) != nil {
				continue
			}
			u, err := NormalizeRepoURL(rest[i+1:])
			if err != nil {
				continue
			}
			if rc.Find(name) == nil {
				rc.Repos = append(rc.Repos, Repo{Name: name, URL: u})
			}
		}
	}
	return rc
}

// SaveRepoConfig writes hom_repos.cfg.
func SaveRepoConfig(path string, rc RepoConfig) error {
	var b strings.Builder
	b.WriteString("# hom repositories - edit with `hom repo add/set/remove`, or by hand.\n")
	b.WriteString("# \"official\" is built in and is used when 'active' is missing or unknown.\n")
	b.WriteString("active " + rc.Active + "\n")
	for _, r := range rc.Repos {
		b.WriteString("repo " + r.Name + " " + r.URL + "\n")
	}
	return WriteFile(path, []byte(b.String()))
}

// Find returns the repo with a name (ignoring case), or nil.
func (rc *RepoConfig) Find(name string) *Repo {
	for i := range rc.Repos {
		if strings.EqualFold(rc.Repos[i].Name, name) {
			return &rc.Repos[i]
		}
	}
	return nil
}

// ActiveRepo is the repo in use; official when 'active' is missing or unknown.
func (rc *RepoConfig) ActiveRepo() Repo {
	if !strings.EqualFold(rc.Active, OfficialName) {
		if r := rc.Find(rc.Active); r != nil {
			return *r
		}
	}
	return Repo{Name: OfficialName, URL: OfficialURL}
}
