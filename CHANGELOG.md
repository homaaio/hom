# Changelog

## 0.6.4 - 2026-11-19

### Added
- Index key `into` for `file` and `archive` packages: `"into": "homrec"` makes `dest` relative to the
  HomRec program folder instead of hom's data folder, so a package can be installed where HomRec looks
  for it. For example `{"type":"file","file":"ffmpeg.exe","dest":"ffmpeg","into":"homrec"}` installs to
  `<HomRec>\ffmpeg\ffmpeg.exe`, which HomRec already searches. `--to` still wins; an unknown `into`
  value skips the entry; `hom show` prints the folder.
- The first package that uses it: `ffmpeg-hr` (the lean FFmpeg build made for HomRec). After this
  release `inwid hom install ffmpeg-hr` works on a HomRec install.
- `tools/pub.py add --into` (and `--file`, for a download whose asset name differs from the installed
  name), and `pub.py check` validates the field.

### Changed
- Version 0.6.3 -> 0.6.4 in `src/hom.cpp`, `version.txt`.

## 2.0.0-dev - stage 2: the Go client (not released yet)

The new client, in `go/`, written against `spec/index.md`. hom 0.6.x (C++) is untouched and stays the
line for Windows until the Go client is as complete (stages 3-6 of the plan). **Not compiled or run by
its author** (the sandbox had no Go): the first `go vet ./... && go test ./...` is the real review.

### Added
- Commands: `search`, `show`, `list [--installed|--upgradable]`, `install [--to] [--run]`, `remove`/`purge -r`,
  `upgrade`, `update` (self-update with a sha256 check), `where`, `set homrec|data`, `clean`, `ping`,
  `repo list|add|set|remove`, `help`, `version`. Same file formats as 0.6.x (`hom.cfg`, `hom_repos.cfg`,
  `hom_db.cfg`), so an existing install keeps working.
- Index schema 1 and 2 (spec 1): entries that break the rules are skipped with a warning; a newer schema can
  be searched but not installed from (`schema` error); the right entry is picked by `os`/`arch` (1.5).
- Install order of the spec (section 2): download into the cache, check size and sha256 **before** anything
  is touched, then replace; a failed check leaves the old version in place. Zip archives are unpacked with
  hom's own code: `..`, absolute paths, drive letters and links make the whole archive fail before any byte is
  written. Into hom's own folder it unpacks beside the target and swaps, so an upgrade leaves no stale files.
- `program` types are only started after `--run` or a typed "y" (`--yes` is not enough); otherwise the
  installer stays in the cache and the command ends with code 6.
- Paths per OS (spec 6): Windows `%LOCALAPPDATA%\hom`, Linux XDG folders, macOS `~/Library`. Portable mode
  when `hom.cfg` or `hom.portable` is beside the binary (or the 0.6.x layout on Windows), or with `--root`.
- `--json` (one document on stdout, `warnings` array, `error.code`/`error.exit`), exit codes 0-6,
  `--yes`, `--root`, `--progress=bar|lines|none` (`lines` prints `@progress` lines, as before).
- Index key `into` (`file`/`archive`): `"into":"homrec"` installs `dest` under the HomRec program folder,
  for packages that HomRec itself has to find (`ffmpeg-hr`). `--to` wins, `upgrade` keeps the location.
- Tests without a network (a local TLS server): `go/pkg/*/..._test.go`. CI (`.github/workflows/go.yml`):
  vet + tests on Windows, Linux and macOS; on a `v2.*` tag, six binaries (windows/linux/macos x amd64/arm64)
  with `.sha256`, `version.txt` and `hom.exe` (a copy of the Windows amd64 build, so that `hom update` of
  0.6.x finds a file by the name it knows).

### Changed from 0.6.x (on purpose)
- `hom install <name>` of a name that is not in the index is now `not_found` (3). 0.6.x guessed
  `<repo>/<name>.hrp` and installed it without any check.
- Entries without `sha256` are refused when they are `program` or `archive` (schema 1) or any type (schema 2).
- `upgrade` fetches a plugin again only when the repo has a newer version, or when the installed version is
  unknown (0.6.x fetched every plugin each time). A file or archive installed with `--to` is upgraded in place.
- `list` without a flag lists the whole repo (`*` marks what is installed); `list --installed` needs no network.
- `remove` also accepts `--yes` as the confirmation; the courtesy "disk space" question is only asked on a
  terminal.
- Archives with links are refused (the spec said "links that leave the folder"; `b -> .` plus `b/..` shows
  why that is not enough). Spec 1.6 and 5.1 were changed to match; spec 6 now has the full path table.

### Not in this version yet
- Signatures and the blocklist (stage 4: `signed`, `key`, `blocked` fields; `hom keygen/sign/report`).
- Several repos at once, proxy options, `file://` repos (stage 3).
- The text screen (`hom` without arguments; stage 6). Use hom.exe 0.6.x for it.

## 0.6.3 - 2026-11-09

No change in behaviour: hom.exe 0.6.3 works exactly like 0.6.1. This release adds the specification of
the next generation and moves the version number (there was no 0.6.2 release).

### Added
- `spec/index.md`: draft specification 2.0, written before the code. It fixes the language of the new
  client (Go; this C++ client stays the 0.6.x line for Windows), the index format v2 (`schema`, `os`, `arch`,
  `license`, `homepage`, `size`, `key`, `sig`; picking one entry among several of the same name), the
  ed25519 signature rules with a test vector, the `blocklist.txt` format, exit codes 0-6 and the `--json`
  output. A v2 index stays readable by 0.6.x (see section 1.9 of the spec).
- `spec/examples/index.v2.json` (two entries signed with a public test key) and `spec/examples/blocklist.txt`.
- CI job `index` also checks that `spec/examples/index.v2.json` is valid JSON.

### Changed
- Version 0.6.1 -> 0.6.3 in `src/hom.cpp`, `version.txt`, the workflow comment.
- `README.md` points to the spec.

### Not verified
- Same as 0.6.1: still no Windows build of hom.exe, CI workflow not run yet. The spec is a draft and no code
  implements it. The signatures in the examples were checked with Python's `cryptography` (ed25519), not with Go.

## 0.6.1 - 2026-11-08

hom moved out of the HomRec repository into its own (`homaaio/hom`) and now works without HomRec.

### Added
- Package types: `plugin` (`.hrp`, `.jar`), `file`, `archive` (`.zip`), `program` (`.msi`/`.exe`). The index
  entry's `type`, `url`, `sha256`, `dest`, `args` fields are optional; an old index means "plugin".
- Standalone mode: hom keeps its settings and data in `%LOCALAPPDATA%\hom` (or in `cfg\` / `hom_data\` when it
  sits next to `hr.exe`) and looks for HomRec on its own; only plugin packages need it.
- `hom where`, `hom set homrec|data <folder>`, `hom clean`; `install --to <dir>` and `install --run`.
- SHA-256 check of every download against the index (CNG); `program` and `archive` packages without a hash are
  refused. `hom update` checks `hom.exe.sha256` from the release.
- Downloads are streamed to a file instead of held in memory (the old 64 MB limit stays only for the index and
  hom's own update). Any https URL works, GitHub Release links included.
- `hom_db.cfg` records what hom installed: needed to remove non-plugin packages, and it gives a plugin a known
  version right after install (before HomRec has unpacked it).
- `tools/pub.py` (add / build / check), `pkgs/` (the official package repo), a CI workflow that builds hom.exe
  and attaches it and its checksum to each `v*` release. Not run yet.

### Changed
- Official repo is now `https://raw.githubusercontent.com/homaaio/hom/main/pkgs/` (index.json and files
  directly in that folder). A repo in the old layout (`plugins/index.json`, `"plugins"` array) is still read.
- `hom update` always reads `version.txt` and downloads `hom.exe` from this repo's latest release (CI attaches
  both), not from the selected package repo.
- `owner/repo` shorthand of `hom repo add` now means `.../pkgs/` (was `.../Hom/`).
- Plugin install no longer creates a `plugins` folder in whatever folder hom was started from: it needs a
  found HomRec folder (see `hom where`).
- Repo settings moved to hom's settings folder (same place as before when hom sits next to `hr.exe`).
- Package names and file names reject control characters and `" < > | * ?` as well.
- `hom upgrade` re-installs every installed plugin as before, but other package types only when the repo has a
  newer version than the recorded one.

### Fixed (code review of the first, unbuilt 0.6.1)
- `index.json` / `plugin.json` are read by a real JSON parser (`common.h`). The old text scanner broke on a
  `{`/`}` inside a description and could take the *value* `"file"` of `"type"` for the key `file`. A cut-off or
  garbled index is now a failure ("couldn't fetch the index"), not "0 packages".
- `hom_db.cfg`: a `|` or line break in a version string from an index could shift the fields and turn into the
  "path" that `hom remove` deletes. Fields are cleaned on save; lines that don't have exactly 4 fields are ignored.
- Package names can't start with `.` (`hom purge . -r` would have wiped the whole plugins folder, `.installed` its
  cache), can't end with `.`/space and can't be Windows device names (`nul`, `com1`...). File names get the same
  checks except the leading dot.
- Recursive delete no longer follows junctions/symlinks (it removes the link only).
- Downloads: a connection dropped mid-body is a failure (was: a silently shorter file/index); a body over the
  64 MB in-memory cap is a failure (was: silently cut).
- `--to D:\clips\` (trailing backslash) broke the `tar.exe` command line of an `archive` install.
- Piped stdin (HomRec's console, scripts): hom never waits for an answer any more (`Y/n` = yes, "run installer?" =
  no unless `--run`), as the README always claimed. Before, it could hang on an open pipe.
- `hom update`: refuses to install when `hom.exe.sha256` can't be fetched (was: skipped the check); retries
  HTTP 5xx twice; says "HTTP 504 from github.com" or "no release yet (404)" instead of "check your connection".
- `hom upgrade` stops when the index can't be fetched (was: tried every plugin blind and failed N times).
- `ShellExecuteEx` (running an installer) now initialises COM first.
- Unknown options (`hom install --yes x`) are reported instead of being taken for the package name.
- TUI: keys work on any keyboard layout (on a Russian layout `q` typed a Cyrillic letter and nothing happened);
  `/` also on the key that is `/` on a US layout; the filter accepts and lower-cases any script (Cyrillic).
- Crash handler: no more `localtime_s`/`_snprintf_s` (not reliably in MinGW headers); `_countof` has a fallback.
- Version compare: leading `v` ignored.
- `tests/test_common.cpp` (runs on Linux, CI job `unit`): names, versions, JSON, repo URLs, config/db files.

### Not verified
- Still no Windows build. `hom.cpp` was syntax-checked against hand-written stub headers (no errors, no `-Wall
  -Wextra` warnings); `common.h` is really compiled and tested on Linux. WinHTTP/CNG/console behaviour, installing
  `file`/`archive`/`program` packages, the new folder detection and the CI workflow are untested.
- Non-ASCII paths: hom uses the ANSI (`*A`) Win32 calls with UTF-8/ACP strings. A user name or package file name
  with Cyrillic letters may break. Test `hom where` / an install under such a profile.
