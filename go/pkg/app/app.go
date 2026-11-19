// Package app is hom itself: every command, as a function that can be tested without a terminal.
// The command line (cmd/hom) only builds an Env and calls Run.
package app

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/homaaio/hom/pkg/cfg"
	"github.com/homaaio/hom/pkg/dirs"
	"github.com/homaaio/hom/pkg/idx"
	"github.com/homaaio/hom/pkg/web"
)

// Exit codes (spec 5.2).
const (
	ExitOK       = 0
	ExitError    = 1
	ExitUsage    = 2
	ExitNotFound = 3
	ExitNetwork  = 4
	ExitVerify   = 5
	ExitDeclined = 6
)

// Error is a failure with the exit code and the JSON error code it ends with.
type Error struct {
	Exit int
	Code string // error | schema | usage | not_found | network | verify | declined
	Pkg  string
	Msg  string
}

func (e *Error) Error() string { return e.Msg }

func newErr(exit int, code, pkg, format string, a ...any) error {
	return &Error{Exit: exit, Code: code, Pkg: pkg, Msg: fmt.Sprintf(format, a...)}
}

func errGeneric(format string, a ...any) error { return newErr(ExitError, "error", "", format, a...) }
func errUsage(format string, a ...any) error   { return newErr(ExitUsage, "usage", "", format, a...) }
func errSchema(format string, a ...any) error  { return newErr(ExitError, "schema", "", format, a...) }
func errNotFound(pkg, format string, a ...any) error {
	return newErr(ExitNotFound, "not_found", pkg, format, a...)
}
func errNet(pkg, format string, a ...any) error { return newErr(ExitNetwork, "network", pkg, format, a...) }
func errVerify(pkg, format string, a ...any) error {
	return newErr(ExitVerify, "verify", pkg, format, a...)
}
func errDeclined(pkg, format string, a ...any) error {
	return newErr(ExitDeclined, "declined", pkg, format, a...)
}

// asErr turns any error into an *Error (a plain one is a generic failure).
func asErr(err error) *Error {
	var he *Error
	if errors.As(err, &he) {
		return he
	}
	return &Error{Exit: ExitError, Code: "error", Msg: err.Error()}
}

// Env is everything a command needs. Tests fill it by hand; cmd/hom fills it from the process.
type Env struct {
	Version string
	Stdin   io.Reader
	Stdout  io.Writer
	Stderr  io.Writer
	TTY     bool // stdin is a terminal
	OutTTY  bool // stdout is a terminal

	JSON     bool
	Yes      bool
	Progress string // bar | lines | none; "" = decide by the terminal

	D      *dirs.Dirs // nil: found by Run
	HTTP   *web.Client
	Plat   idx.Platform
	ExeDir string

	// Tests only: where `hom update` looks for releases, and which file it replaces.
	SelfURL  string
	SelfPath string

	Warns []string

	repo      cfg.Repo
	repoSet   bool
	homrec    string
	homrecSet bool
	rd        *bufio.Reader
}

// Opts are the per-command options.
type Opts struct {
	To                    string
	Force, Run, Recursive bool
	Installed, Upgradable bool
}

// SetRepo fixes the repo in use (instead of reading hom_repos.cfg).
func (e *Env) SetRepo(r cfg.Repo) { e.repo, e.repoSet = r, true }

// SetHomrec fixes the HomRec folder (instead of looking for it).
func (e *Env) SetHomrec(dir string) { e.homrec, e.homrecSet = dir, true }

// Say prints to the user; with --json nothing is printed (stdout holds the JSON document only).
func (e *Env) Say(format string, a ...any) {
	if e.JSON {
		return
	}
	fmt.Fprintf(e.Stdout, format, a...)
}

// Warn prints a warning to stderr and keeps it for the JSON document.
func (e *Env) Warn(format string, a ...any) {
	msg := fmt.Sprintf(format, a...)
	fmt.Fprintln(e.Stderr, "hom: warning: "+msg)
	e.Warns = append(e.Warns, msg)
}

// Interactive reports whether a question may be asked (spec 5.1: never with --json or piped input).
func (e *Env) Interactive() bool { return e.TTY && !e.JSON }

func (e *Env) reader() *bufio.Reader {
	if e.rd == nil {
		in := e.Stdin
		if in == nil {
			in = strings.NewReader("")
		}
		e.rd = bufio.NewReader(in)
	}
	return e.rd
}

// ask prints a yes/no question and reads the answer; Enter or end of input gives def.
func (e *Env) ask(prompt string, def bool) bool {
	hint := "(y/N) "
	if def {
		hint = "(Y/n) "
	}
	fmt.Fprintf(e.Stdout, "%s %s", prompt, hint)
	line, err := e.reader().ReadString('\n')
	line = strings.TrimSpace(line)
	if line == "" {
		if err != nil {
			fmt.Fprintln(e.Stdout)
		}
		return def
	}
	return line[0] == 'y' || line[0] == 'Y'
}

func (e *Env) cfgPath(name string) string { return filepath.Join(e.D.Config, name) }
func (e *Env) dbPath() string             { return e.cfgPath(dirs.DBName) }

func (e *Env) activeRepo() cfg.Repo {
	if !e.repoSet {
		rc := cfg.LoadRepoConfig(e.cfgPath(dirs.ReposName))
		e.repo = rc.ActiveRepo()
		e.repoSet = true
		if !strings.EqualFold(rc.Active, cfg.OfficialName) && rc.Find(rc.Active) == nil {
			e.Warn("the saved active repo '%s' no longer exists -- using official", rc.Active)
		}
	}
	return e.repo
}

// LooksLikeHomrec tells a HomRec folder by its program file.
func LooksLikeHomrec(dir string) bool {
	if dir == "" {
		return false
	}
	for _, n := range []string{"hr.exe", "homrec.exe", "hr", "homrec"} {
		if st, err := os.Stat(filepath.Join(dir, n)); err == nil && !st.IsDir() {
			return true
		}
	}
	return false
}

// homrecDir is the folder of HomRec ("" when none is found): the "homrec" setting, then the
// current folder, the folder of hom, and the usual places on Windows.
func (e *Env) homrecDir() string {
	if e.homrecSet {
		return e.homrec
	}
	e.homrecSet = true
	if v := cfg.LoadKv(e.cfgPath(dirs.CfgName))["homrec"]; v != "" {
		e.homrec = v
		return e.homrec
	}
	var cands []string
	if wd, err := os.Getwd(); err == nil {
		cands = append(cands, wd)
	}
	if e.ExeDir != "" {
		cands = append(cands, e.ExeDir)
	}
	if runtime.GOOS == "windows" {
		if la := os.Getenv("LOCALAPPDATA"); la != "" {
			cands = append(cands, filepath.Join(la, "Programs", "HomRec"))
		}
		if pf := os.Getenv("ProgramFiles"); pf != "" {
			cands = append(cands, filepath.Join(pf, "HomRec"))
		}
	}
	for _, c := range cands {
		if LooksLikeHomrec(c) {
			e.homrec = c
			break
		}
	}
	return e.homrec
}

const usageText = `hom - a package manager for any OS

Usage: hom <command> [options]

Commands:
  search <text>            search the repo (a regular expression, or plain text)
  show <name>              details of a package
  list [--installed|--upgradable]
                           packages of the repo, or what is installed here
  install <name> [--to <folder>] [--run]
  remove <name> -r         remove an installed package (purge: also its saved data)
  upgrade                  upgrade everything that has a newer version
  update [-f]              update hom itself
  where                    where hom keeps its files
  set <homrec|data> [<folder>|auto]
  clean                    empty the download cache
  ping                     check that the repo answers
  repo list|add <name> <url|owner/repo[@branch]>|set <name>|remove <name>
  help, version

Options:
  --json            machine-readable output (one JSON document on stdout)
  -y, --yes         answer yes to questions (but not to "run this installer?": use --run)
  --root <folder>   keep everything under <folder> (portable mode)
  --progress=bar|lines|none
`

// Run executes one command line (without the program name) and returns the exit code.
func (e *Env) Run(ctx context.Context, args []string) int {
	e.JSON = false
	for _, a := range args {
		if a == "--" {
			break
		}
		if a == "--json" {
			e.JSON = true
		}
	}
	p, err := parseArgs(args)
	if err != nil {
		return e.finish("", nil, err)
	}
	e.Yes = p.yes
	if p.progress != "" {
		e.Progress = p.progress
	}
	if p.help || p.cmd == "help" {
		fmt.Fprint(e.Stdout, usageText)
		return ExitOK
	}
	if p.version || p.cmd == "version" {
		line := fmt.Sprintf("hom %s (%s, %s)", e.Version, runtime.Version(), idx.Here())
		if !e.JSON {
			fmt.Fprintln(e.Stdout, line)
		}
		return e.finish("version", map[string]any{"version": e.Version}, nil)
	}
	if p.cmd == "" {
		return e.finish("", nil, errUsage("no command given (try 'hom help')"))
	}
	if e.HTTP == nil {
		e.HTTP = web.New("hom/" + e.Version)
	}
	if e.Plat.OS == "" {
		e.Plat = idx.Here()
	}
	if e.D == nil {
		d, derr := dirs.Resolve(p.root, e.ExeDir)
		if derr != nil {
			return e.finish(p.cmd, nil, errGeneric("can't decide where to keep files: %v", derr))
		}
		e.D = d
		if v := cfg.LoadKv(e.cfgPath(dirs.CfgName))["data"]; v != "" {
			e.D.SetData(v)
		}
	}
	data, err := e.dispatch(ctx, p)
	return e.finish(p.cmd, data, err)
}

func (e *Env) dispatch(ctx context.Context, p *parsed) (any, error) {
	one := func() (string, error) {
		if len(p.args) != 1 {
			return "", errUsage("'%s' needs exactly one name (try 'hom help')", p.cmd)
		}
		return p.args[0], nil
	}
	none := func() error {
		if len(p.args) != 0 {
			return errUsage("'%s' takes no arguments", p.cmd)
		}
		return nil
	}
	switch p.cmd {
	case "search":
		if len(p.args) == 0 {
			return nil, errUsage("search needs some text")
		}
		return e.cmdSearch(ctx, strings.Join(p.args, " "))
	case "show":
		n, err := one()
		if err != nil {
			return nil, err
		}
		return e.cmdShow(ctx, n)
	case "list":
		if err := none(); err != nil {
			return nil, err
		}
		return e.cmdList(ctx, p.o)
	case "install":
		n, err := one()
		if err != nil {
			return nil, err
		}
		if n == "update-hrp" { // the old spelling HomRec's console knows
			return e.cmdUpgrade(ctx)
		}
		return e.cmdInstall(ctx, n, p.o)
	case "remove", "uninstall":
		n, err := one()
		if err != nil {
			return nil, err
		}
		return e.cmdRemove(n, false, p.o.Recursive || e.Yes)
	case "purge":
		n, err := one()
		if err != nil {
			return nil, err
		}
		return e.cmdRemove(n, true, p.o.Recursive || e.Yes)
	case "upgrade", "full-upgrade":
		if err := none(); err != nil {
			return nil, err
		}
		return e.cmdUpgrade(ctx)
	case "autoremove":
		e.Say("hom has no dependencies yet, so there is nothing to autoremove.\n")
		return struct{}{}, nil
	case "update":
		if err := none(); err != nil {
			return nil, err
		}
		return e.cmdSelfUpdate(ctx, p.o.Force)
	case "where":
		if err := none(); err != nil {
			return nil, err
		}
		return e.cmdWhere()
	case "set":
		return e.cmdSet(p.args)
	case "clean":
		if err := none(); err != nil {
			return nil, err
		}
		return e.cmdClean()
	case "ping":
		if err := none(); err != nil {
			return nil, err
		}
		return e.cmdPing(ctx)
	case "repo":
		return e.cmdRepo(p.args)
	case "tui", "ui":
		return nil, errUsage("the interactive screen is not in this version of hom yet (it is still in hom 0.6.x)")
	}
	return nil, errUsage("unknown command '%s' (try 'hom help')", p.cmd)
}

type parsed struct {
	cmd      string
	args     []string
	o        Opts
	root     string
	progress string
	yes      bool
	help     bool
	version  bool
}

func parseArgs(args []string) (*parsed, error) {
	p := &parsed{}
	var pos []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			pos = append(pos, args[i+1:]...)
			break
		}
		if len(a) < 2 || a[0] != '-' {
			pos = append(pos, a)
			continue
		}
		name, val, hasVal := a, "", false
		if strings.HasPrefix(a, "--") {
			if k := strings.IndexByte(a, '='); k >= 0 {
				name, val, hasVal = a[:k], a[k+1:], true
			}
		}
		valued := name == "--root" || name == "--to" || name == "--progress"
		if hasVal && !valued {
			return nil, errUsage("option %s takes no value", name)
		}
		if valued && !hasVal {
			if i+1 >= len(args) {
				return nil, errUsage("option %s needs a value", name)
			}
			i++
			val = args[i]
		}
		switch name {
		case "--json":
			// already noticed by Run
		case "-y", "--yes":
			p.yes = true
		case "-f", "--force":
			p.o.Force = true
		case "-r":
			p.o.Recursive = true
		case "--run":
			p.o.Run = true
		case "--installed":
			p.o.Installed = true
		case "--upgradable":
			p.o.Upgradable = true
		case "-h", "--help":
			p.help = true
		case "-v", "-V", "--version":
			p.version = true
		case "--root":
			p.root = val
		case "--to":
			p.o.To = val
		case "--progress":
			if val != "bar" && val != "lines" && val != "none" {
				return nil, errUsage("--progress must be bar, lines or none")
			}
			p.progress = val
		default:
			return nil, errUsage("unknown option '%s' (try 'hom help')", a)
		}
	}
	if len(pos) > 0 {
		p.cmd = pos[0]
		p.args = pos[1:]
	}
	return p, nil
}

// jsonDoc is the document of --json (spec 5.4).
type jsonDoc struct {
	JSON     int        `json:"json"`
	OK       bool       `json:"ok"`
	Cmd      string     `json:"cmd"`
	Data     any        `json:"data,omitempty"`
	Error    *jsonError `json:"error,omitempty"`
	Warnings []string   `json:"warnings"`
}

type jsonError struct {
	Code    string `json:"code"`
	Exit    int    `json:"exit"`
	Message string `json:"message"`
	Package string `json:"package,omitempty"`
}

func (e *Env) writeDoc(cmd string, data any, he *Error) {
	d := jsonDoc{JSON: 1, OK: he == nil, Cmd: cmd, Data: data, Warnings: e.Warns}
	if d.Warnings == nil {
		d.Warnings = []string{}
	}
	if he != nil {
		d.Error = &jsonError{Code: he.Code, Exit: he.Exit, Message: he.Msg, Package: he.Pkg}
	}
	enc := json.NewEncoder(e.Stdout)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	enc.Encode(d)
}

// finish prints the error (or the JSON document) and gives the exit code.
func (e *Env) finish(cmd string, data any, err error) int {
	if err == nil {
		if e.JSON {
			e.writeDoc(cmd, data, nil)
		}
		return ExitOK
	}
	he := asErr(err)
	fmt.Fprintf(e.Stderr, "hom: %s\n", he.Msg)
	if e.JSON {
		e.writeDoc(cmd, data, he)
	}
	return he.Exit
}
