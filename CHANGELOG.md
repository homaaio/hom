# Changelog

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

### Not verified
- No build and no run so far (no compiler in the environment that made these changes); a syntax-only check of
  everything except the TUI part against stub headers passed.
- Installing `file`/`archive`/`program` packages, the new folder detection and the CI workflow are untested.
