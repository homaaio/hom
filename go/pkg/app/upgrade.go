package app

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/homaaio/hom/pkg/idx"
)

type upgradedJSON struct {
	Name string `json:"name"`
	From string `json:"from"`
	To   string `json:"to"`
}

type skippedJSON struct {
	Name   string `json:"name"`
	Reason string `json:"reason"`
}

type failedJSON struct {
	Name    string `json:"name"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

type upgradeData struct {
	Upgraded []upgradedJSON `json:"upgraded"`
	Skipped  []skippedJSON  `json:"skipped"`
	Failed   []failedJSON   `json:"failed"`
}

// cmdUpgrade installs the newer version of every installed package that has one. A package whose
// version is not known (a plugin put there by hand) is fetched again.
func (e *Env) cmdUpgrade(ctx context.Context) (any, error) {
	d := &upgradeData{Upgraded: []upgradedJSON{}, Skipped: []skippedJSON{}, Failed: []failedJSON{}}
	locals := e.listLocal()
	if len(locals) == 0 {
		e.Say("Nothing is installed yet, so there is nothing to upgrade.\n")
		return d, nil
	}
	c, err := e.fetchCatalog(ctx)
	if err != nil {
		return nil, err
	}
	if err := c.needSupported(); err != nil {
		return nil, err
	}
	e.Say("Checking %d installed package(s)...\n", len(locals))

	var first *Error
	for _, l := range locals {
		if ctx.Err() != nil {
			break
		}
		ent, perr := c.ix.Pick(l.Name, e.Plat)
		if perr != nil {
			d.Skipped = append(d.Skipped, skippedJSON{Name: l.Name, Reason: "not in the repo (or no build for this computer)"})
			continue
		}
		if localType(l.Kind) != ent.Type {
			d.Skipped = append(d.Skipped, skippedJSON{Name: l.Name, Reason: "the repo's package has a different type"})
			continue
		}
		if l.Version != "" && !idx.Gt(ent.Version, l.Version) {
			d.Skipped = append(d.Skipped, skippedJSON{Name: l.Name, Reason: "up to date"})
			continue
		}
		var o Opts
		switch l.Kind { // keep a package where the user put it with --to
		case "file":
			if ent.Into == "" && !isUnder(l.Path, filepath.Join(e.D.Data, "files")) {
				o.To = filepath.Dir(l.Path)
			}
		case "archive":
			if ent.Into == "" && !isUnder(l.Path, filepath.Join(e.D.Data, "apps")) {
				o.To = l.Path
			}
		}
		from := l.Version
		if from == "" {
			from = "unknown"
		}
		e.Say("Upgrading '%s' %s -> %s...\n", l.Name, from, orQ(ent.Version))
		if _, ierr := e.installEntry(ctx, c, ent, o); ierr != nil {
			he := asErr(ierr)
			if he.Pkg == "" {
				he.Pkg = l.Name
			}
			fmt.Fprintf(e.Stderr, "hom: %s\n", he.Msg)
			d.Failed = append(d.Failed, failedJSON{Name: l.Name, Code: he.Code, Message: he.Msg})
			if first == nil {
				first = he
			}
			continue
		}
		d.Upgraded = append(d.Upgraded, upgradedJSON{Name: l.Name, From: from, To: ent.Version})
	}

	e.Say("Done: %d upgraded, %d skipped, %d failed.\n", len(d.Upgraded), len(d.Skipped), len(d.Failed))
	if first != nil {
		return d, &Error{Exit: first.Exit, Code: first.Code, Pkg: first.Pkg,
			Msg: fmt.Sprintf("%d package(s) could not be upgraded", len(d.Failed))}
	}
	if ctx.Err() != nil {
		return d, errGeneric("interrupted")
	}
	return d, nil
}
