# hom

`hom` is a small package manager for Windows. It started as the plugin manager of
[HomRec](https://github.com/homaaio/HomRec) and now also works **on its own**: it can install HomRec
plugins (`.hrp`, `.jar`), plain files (`.mp4`, `.txt`, ...), archives (`.zip`) and programs
(`.msi`, `.exe`). It is one `hom.exe` with no dependencies; only HomRec *plugins* need HomRec.

```
> hom search overlay
input-overlay (1.0.0) - Keyboard / mouse / gamepad input overlay presets ...

> hom install input-overlay
Fetching plugin 'input-overlay'...
Installed 'input-overlay' -> C:\Users\me\AppData\Local\Programs\HomRec\plugins\input-overlay.hrp (552854 bytes)
```

## Get it

Download `hom.exe` from the [latest release](https://github.com/homaaio/hom/releases/latest), put it in a
folder that is in your `PATH` (or anywhere, and run it by path). HomRec's installer and portable zip
already contain it. Later: `hom update`.

Build it yourself (MSYS2 MINGW64, no wxWidgets or Lua needed):

```bash
cmake -S . -B build -G "MinGW Makefiles"
cmake --build build          # -> build/hom.exe
```

## Commands

```
hom, hom tui                    full-screen package browser (needs a real console, docs/tui.md)
hom search <query>              search names/descriptions (regex or plain text)
hom show <name>                 details: type, version, sha256, size, installed or not
hom install <name> [-y] [-f] [--to <dir>] [--run]
hom remove <name> -r            remove, keep a plugin's saved settings
hom purge  <name> -r            remove everything of it
hom list --installed | --upgradable
hom upgrade [-y] [-f]           update what is installed (full-upgrade: same for now)
hom update [-f]                 update hom itself
hom where                       the folders and the repo hom is using right now
hom set homrec <folder>         tell hom where HomRec is   ('auto' forgets it)
hom set data <folder>           move hom's data folder
hom clean                       empty the download cache (data\dl)
hom ping                        is the repo reachable
hom repo [list|add|set|remove]  use a fork or a mirror as the package repo
--progress=bar|lines|none       any command: how downloads report progress
```

## Package types and where each one goes

Every package has a `type` in the repo index. hom picks the folder from the type:

| type      | what it is                         | where it ends up                                   | removed by `hom remove` |
|-----------|------------------------------------|----------------------------------------------------|--------------------------|
| `plugin`  | HomRec plugin: `.hrp` (or `.jar`)  | `<HomRec>\plugins\<name>.hrp`                      | yes (settings kept)      |
| `file`    | any file: `.mp4`, `.txt`, `.zip`...| `<data>\files\<name>\<file>`, or `--to <dir>`      | the file (and its folder)|
| `archive` | a `.zip` to unpack                 | `<data>\apps\<name>\`, or `--to <dir>`             | the folder, if it is under `<data>\apps` |
| `program` | installer: `.msi`, `.exe`          | saved in `<data>\dl\`, then run (hom asks first)   | the saved installer only; uninstall the program with Windows |

`<data>` is `%LOCALAPPDATA%\hom` when hom is installed on its own, and `<folder of hom.exe>\hom_data` when
`hom.exe` sits next to `hr.exe` (the HomRec layout, fully portable). `hom where` prints what hom picked.

How hom finds HomRec (first hit wins): `hom set homrec <folder>`, the current folder (if it has `hr.exe`),
the folder of `hom.exe`, `%LOCALAPPDATA%\Programs\HomRec` (the installer's default), `%ProgramFiles%\HomRec`.
No HomRec found: everything except `plugin` packages still works.

Safety rules (they apply to every repo, official or not):

- Only `https://`. Downloads are streamed to disk and checked against the index's `sha256` *before* they
  replace anything.
- A `program` or `archive` without a `sha256` in the index is refused.
- A `program` is never started without a question (default: no). `--run` skips the question. When hom runs
  with its input piped (HomRec's console), it never asks and never starts anything unless `--run` is given.
- Package names and file names can't contain `..`, path separators or a drive letter. An index `dest` is a
  relative sub-folder of `<data>`; only the person typing `--to` can send a package somewhere else.
- `.zip` files are unpacked with Windows' own `tar.exe`, which refuses entries that climb out of the folder.
- `hom update` always comes from this repo's releases (`hom.exe` + `hom.exe.sha256`), whichever package repo
  is selected.

## Where everything is stored (this repo)

```
homaaio/hom
  src/hom.cpp, src/common.h     the program
  version.txt                   current hom version (CI checks it against the code and the tag)
  pkgs/                         the OFFICIAL package repo (what 'hom repo' calls "official")
    index.json                  the list of packages (fields: pkgs/README.md)
    <name>.hrp, ...             small packages, stored right here in git
    src/<name>/                 sources of the plugins above (tools/pub.py build)
  tools/pub.py                  add/build/check packages without editing JSON by hand
  Releases                      hom.exe (+ hom.exe.sha256) per version, and BIG package files
```

- **Small packages (< ~10 MB):** the file goes into `pkgs/`, the entry into `pkgs/index.json`. hom downloads
  `https://raw.githubusercontent.com/homaaio/hom/main/pkgs/<file>`.
- **Big packages (videos, installers, zips):** don't commit them (git history keeps every version forever, and
  GitHub rejects files over 100 MB). Attach the file to a GitHub Release of this repo (up to 2 GB per file; use a
  tag like `pkgs`) and put its `https://github.com/homaaio/hom/releases/download/...` address in the entry's
  `url`. The index stays small and in git; the data lives in Releases.
- **hom itself:** each tag `vX.Y.Z` builds `hom.exe`, `hom.exe.sha256` and `version.txt` in CI and attaches
  them to the release. `hom update` reads `releases/latest/download/version.txt`, and if that is newer than
  itself downloads `hom.exe` from the same release. All three appear together, so there is no moment when
  the version is announced but the exe is missing.

### Publishing

```bash
python tools/pub.py add my-video.mp4 --name intro --desc "Intro clip" --url https://github.com/homaaio/hom/releases/download/pkgs/my-video.mp4
python tools/pub.py add setup.msi --name some-tool --version 2.1 --args "/qb" --url https://github.com/.../setup.msi
python tools/pub.py build watermark          # re-zip pkgs/src/watermark -> pkgs/watermark.hrp
python tools/pub.py check                    # run before every commit; CI runs it too
```

`pub.py add` without `--url` copies the file into `pkgs/` instead. After that: commit, push. Users get the
new entry on their next `hom search/install/upgrade`; there is nothing to deploy.

### Releasing hom

1. Bump `k_hom_version` in `src/hom.cpp` and `version.txt` (CI refuses a build where they differ).
2. Add the entry to `CHANGELOG.md`, commit to `main`.
3. `git tag vX.Y.Z && git push origin main --tags`. CI builds and attaches `hom.exe`, `hom.exe.sha256` and
   `version.txt` to the release; users see the update once that release is published.

## Your own repo

A repo is just a folder that serves `index.json` and the files over https. Fork this one (keep `pkgs/`), or
host the folder anywhere:

```
hom repo add friends someone/hom            # GitHub shorthand: owner/repo[@branch] -> .../pkgs/
hom repo add mirror https://example.org/hom/pkgs/
hom repo set friends                        # use it
hom repo set official                       # back to the official one
```

Repos in the old layout (`Hom/plugins/index.json`, `"plugins": [...]`) still work.

## Notes

- hom is Windows-only for now (WinHTTP, CNG for SHA-256, the Win32 console).
- HomRec 3.0.x only loads `.hrp`. hom can install `.jar` plugins into `plugins\`, but nothing in HomRec loads
  them yet.
- Not tested on a real machine yet: everything in this README about `file`, `archive` and `program` packages
  and the new folder detection was written without a build. Please run `hom where`, then an install of each
  type, before trusting it.

License: GPL-3.0, see `LICENSE`.
