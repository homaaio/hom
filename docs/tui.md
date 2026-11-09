# hom - extra notes

The commands, package types, folders and the repo layout are in the [README](../README.md); the index
fields are in [pkgs/README.md](../pkgs/README.md). This page only covers the parts that need more room.

### Interactive TUI (`hom tui`)

Run `hom` with no arguments (or `hom tui`) in PowerShell, Windows Terminal or
cmd and hom opens a full-screen package browser instead of printing usage:
a merged list of repo packages and locally installed packages (plugins, files, archives, programs), a details pane,
and one-key actions.

```
Up/Down, PgUp/PgDn, Home/End   move
i or Enter                     install / upgrade the selected package
r                              remove it (saved settings are kept)
p                              purge it (package + extracted files + settings)
u                              upgrade every package that has a newer version
/                              filter by name/description (Enter applies, Esc cancels)
Tab                            switch view: all / installed / upgradable
s or F5                        refresh the repo index
q or Ctrl+C                    quit
```

The status markers are `*` installed, `^` upgrade available, `+` not installed,
`~` local-only package and `?` installed with an unknown local version (HomRec
has not extracted its `plugin.json` yet; after a TUI install the downloaded
repo version is shown for the rest of the session). Destructive actions and
"upgrade all" ask for confirmation first.

The TUI is only enabled when a real console is attached. When hom is piped or
started by HomRec's in-app console it prints the normal line-based output and
`hom tui` exits with an error, so scripts and the console keep working exactly
as before.

A `program` package is only downloaded (and checked) from the TUI, never run: it tells you where the
installer was saved. Use `hom install <name>` in a normal command line to run it.

### Download progress

`install`, `upgrade` and `update` show a bar with percent, sizes and an estimate:

```
  [#########-----------]  45%  1.5 / 3.4 MB  ~4s
```

In a real terminal hom redraws the line itself. HomRec's console starts hom with
`--progress=lines` (hom then prints `@progress <percent> <received> <total> <eta_sec>`
lines, which the console draws as one live bar). When the output is piped into
another program the default is `none`.

### If hom.exe was deleted

HomRec's console (`hom restore`, or any `hom ...` command when `hom.exe` is missing) downloads it again from
this repo's latest release. Without HomRec, download `hom.exe` from the releases page by hand.
