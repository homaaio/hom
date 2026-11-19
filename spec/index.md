# hom specification 2.0 (draft)

Status: **draft**. Written before the code (stage 1 of the plan); nothing here is implemented yet.
hom 0.6.x (C++, Windows) implements only the v1 index and exits with 0 or 1 (see 0.6.x compatibility below).

The key words MUST, SHOULD and MAY are used as in RFC 2119.

This document defines four things: the **index format v2**, the **signature rules**, the **blocklist
format**, and the **exit codes and `--json` output** of the command line. Anything else (repo names like
`repo:name`, `github:` sources, proxies, branded catalogs, dependencies) is listed under "Not in this
spec yet" at the end.

## 0. Decisions

| Question | Decision |
| --- | --- |
| Language of the new client | **Go** (1.22 or newer). One static binary, cross-compiles to windows/linux/macos x amd64/arm64. Standard library plus `golang.org/x/net/proxy` (SOCKS5, stage 3) and nothing else. |
| Rust | Not chosen: harder to maintain for one person; no gain for this program. |
| The C++ client | Stays the 0.6.x line for Windows. Bug fixes only; no new features. It is dropped when the Go client has parity (end of stage 2). |
| Index format | The 0.6.x format stays valid as **schema 1**. Schema 2 only adds (see 1.9), so one catalog can serve both clients. |
| Where the Go code lives | `go/` in this repository (`go/cmd/hom`, `go/pkg/...`) until parity; then it moves to the repository root. |
| Signatures | ed25519 only. No identities, only keys (section 3). |

## 1. Index format v2

### 1.1 File and encoding

- The index is `index.json` in the repo folder (the URL a repo is added with, plus `index.json`).
- UTF-8 JSON, a leading BOM is tolerated. At most **64 MiB** (the limit 0.6.x already has).
- A cut-off or invalid JSON file is a failure ("couldn't fetch the index"), never "0 packages".
- Clients MUST ignore unknown keys, anywhere. Keys starting with `_` are comments.

### 1.2 Root object

| Key | Type | Meaning |
| --- | --- | --- |
| `schema` | integer | `2` for this document. Missing or `1`: the 0.6.x format. |
| `packages` | array | Package entries (1.3). |
| `name` | string | Optional. Human name of the catalog, for `hom repo list`. |
| `contact` | string | Optional. Where to send complaints (an address or an `https://` URL). |

If `schema` is **higher** than the client supports, the client MUST still show `search` / `list` / `show`
results it can read, MUST refuse `install` and `upgrade` from that index, and MUST say
"this repo needs a newer hom" (exit code 1, `error.code` `schema`).
(0.6.x does not know this rule; it ignores `schema`. See 1.9.)

### 1.3 Package entry

| Key | Type | Required | Meaning |
| --- | --- | --- | --- |
| `name` | string | yes | Package name, see 1.4. |
| `type` | string | yes (v1: default `plugin`) | `plugin`, `file`, `archive` or `program`, see 1.6. A type the client does not know: the entry is skipped with a warning. |
| `file` | string | yes, except `plugin` (default `<name>.hrp`) | File name. Checked like a name (1.4) but may start with `.`. |
| `url` | string | no | `https://` address of the file. Default `<repo folder>/<file>`. Redirects are followed, but only to `https://`. |
| `sha256` | string | **yes** (v1: only `program`, `archive`) | 64 hex characters (case-insensitive) of the downloaded file. |
| `size` | integer | no | Bytes. If present: shown before the download, checked after it; a mismatch is a verification failure. |
| `version` | string | yes | Compared as in 1.7. |
| `os` | string | no | `windows`, `linux`, `macos`. Missing or `any` = every OS. |
| `arch` | string | no | `amd64`, `arm64`, `386`, `arm` (Go names). Missing or `any` = every CPU. |
| `dest` | string | no | `file` / `archive`: relative sub-folder of hom's data folder. No `..`, no drive letter, no leading `/` or `\`. |
| `into` | string | no | `file` / `archive`: `homrec` makes `dest` relative to the **HomRec program folder** instead (`{"into":"homrec","dest":"ffmpeg"}` puts the file in `<HomRec>\ffmpeg\`). Other values: the entry is skipped. `--to` still wins. |
| `args` | string | no | `program` on Windows: arguments for the installer. Shown in the confirmation. Ignored on other OSes. |
| `description` | string | no | One line for `hom search`. |
| `author` | string | no | Free text. |
| `license` | string | no | An SPDX identifier (`MIT`, `GPL-3.0-only`) or `Proprietary`. |
| `homepage` | string | no | `https://` address. |
| `key` | string | no | Public key of the signer, see 3.1. |
| `sig` | string | no | Signature of the entry, see 3.3. |
| `depends` | array | no | **Reserved.** Clients MUST ignore it in 2.0. |

An entry that breaks a rule above (bad name, missing `sha256` in schema 2, `url` not `https`, unknown
`os`) is skipped with a warning; it never makes the whole index fail.

### 1.4 Names

A package name and a file name (`name`, `file`) MUST be accepted only if all of this holds. The rules are
the 0.6.x rules, checked on **every** OS so that one catalog works everywhere:

- 1 to 100 bytes;
- none of `/ \ : " < > | * ?`, no control characters, no `..`;
- a name does not start with `.`; neither ends with `.` or a space;
- not a Windows device name (`con`, `prn`, `aux`, `nul`, `com1`-`com9`, `lpt1`-`lpt9`, with or without an
  extension, any case).

`:` is forbidden on purpose: stage 3 uses `repo:name`.

The official catalog (lint, stage 4) is stricter for new names: `^[a-z0-9][a-z0-9._-]{0,63}$`.

### 1.5 Several entries with the same name

One name can have several entries, one per platform. The client picks one like this:

1. Drop entries whose `os` is set and is not the current OS, or whose `arch` is set and is not the current CPU.
2. Rank the rest: `os` and `arch` both match, then only `os`, then only `arch`, then neither.
3. Take the best rank. Within a rank, the highest `version` (1.7); if equal, the first in the file.
4. Nothing left: error `not_found`, with the message "'name' has no build for linux/arm64"
   (the list of platforms that do exist is printed after it).

`hom upgrade` compares the installed version with the entry picked by these rules, not with any other
entry of the same name.

Lint rule (stage 4): two entries must not have the same `name`, `os` and `arch` (a missing value counts as
`any`).

### 1.6 Types

| Type | What the client does | Platforms |
| --- | --- | --- |
| `plugin` | `.hrp` / `.jar`: the file goes into the plugins folder of the host program. | As in 0.6.x (HomRec). |
| `file` | The file is stored under `<data>/files/<dest or name>/<file>`, or in `--to`. With `into: homrec` it goes under `<HomRec>/<dest or name>/<file>` instead. | all |
| `archive` | A `.zip`, unpacked with the client's own code (no `tar` needed) into `<data>/apps/<dest or name>/`, or `--to`. `into: homrec` unpacks under `<HomRec>/<dest or name>/` instead. Entries that climb out of the folder (`..`, absolute paths, drive letters) and any link (symlink) make the install fail; nothing is written for such an archive. | all |
| `program` | Saved to the download cache (`<cache>`, see 6); opened with the system **only after a yes**: `--run`, or a typed "y" at the prompt; `--yes` does not count, as in 0.6.x. Windows: `.msi`, `.exe`. Linux: `.AppImage` (made executable and started), `.tar.gz` (saved, nothing started). macOS: `.dmg`, `.pkg` (opened). `.deb` / `.rpm` are never installed automatically: the file is saved and the path printed. | per OS |

The confirmation for `program` always shows: name, version, author (or "unknown"), URL, size, and the
repo name; for a repo that is not the official one it also shows "source not verified".

### 1.7 Versions

Same as 0.6.x, written down:

- Split by `.`; compare the parts as numbers, left to right; a missing part is `0`.
- A leading `v` is ignored.
- Per part only the leading digits count; the rest is ignored. So `1.0-beta` equals `1.0`.
  Pre-release ordering is **not defined** in 2.0; catalogs SHOULD NOT publish pre-releases under the same
  name.

### 1.8 Full example

`spec/examples/index.v2.json` is a complete index with a cross-platform `file`, a tool with four platform
builds and a Windows `program`; two of its entries are signed with the test key from 3.6. The URLs and hashes
in it are made up.

### 1.9 Compatibility with hom 0.6.x

0.6.x reads `packages`, ignores every key it does not know (`schema`, `os`, `arch`, `license`, ...) and
`FindEntry` takes the **first** entry with the wanted name. So, while 0.6.x clients exist, a schema-2
catalog MUST list, for each name, the entry that suits a Windows amd64 machine (or the one without `os`)
**first**. 0.6.x will install that one and never see the other platforms. A name that has no Windows build
at all will be installed by 0.6.x anyway (it cannot tell); the catalog SHOULD keep such names out of the
index until 0.6.x is retired, or accept that.

## 2. Verification of a download (all clients)

In this order; any failure removes the partial file, changes nothing on disk and exits with code 5
(`verify`), except the first two:

1. The URL is `https://`. (Otherwise the entry was skipped already.)
2. The body is streamed to `<file>.part`. A dropped connection or a non-200 answer is a `network` failure (4).
3. If `size` is present: it matches.
4. The SHA-256 of the bytes equals `sha256`.
5. The hash and the entry's `key` are not on the blocklist (section 4).
6. If the entry has `sig`: it verifies (3.3). A bad `sig` is always fatal, even for an unknown key.
7. Key continuity (3.4).

Only then is the old file replaced (rename) or the installer offered.

## 3. Signatures

### 3.1 Keys

- Algorithm: **ed25519** (RFC 8032, "pure" mode, no pre-hash).
- Text form of a public key: `ed25519:` followed by the 32 raw bytes in **base64url without padding**
  (43 characters).
- Fingerprint (what the client shows): the first 16 hex characters of the SHA-256 of the 32 raw bytes.
- The private key never appears in a repo, a CI secret, or an index. `hom keygen` (stage 4) writes it to a
  file with owner-only permissions.

### 3.2 Signature text form

64 raw bytes in base64url without padding (86 characters). The same form is used for `sig` and for the
content of `.sig` files.

### 3.3 What is signed

**An entry** (`key` + `sig` fields). The signed message is these lines, UTF-8, each ended with LF
(also the last), in exactly this order:

```
hom-entry-v1
name=<name>
version=<version>
type=<type>
os=<os>
arch=<arch>
sha256=<sha256, lowercase>
size=<size>
```

`type` is the effective type (`plugin` when the entry has none). A missing `os`, `arch` or `size` is an
empty value (`os=` and nothing after it); `any` is written as an empty value too. If any value contains CR
or LF the signature is invalid.

The signature binds the *content* (name, version, platform, hash, size), not the location: `url`, `file`,
`description` and the rest are not signed, so a file can move to another host without a new signature.

**A file** (detached, `<file>.sig`, used for `index.json` and `blocklist.txt`, see 3.5). The message is the
ASCII line `hom-file-v1` and LF, followed by the exact bytes of the file.

The first line of each message is a domain separator: an entry signature can never be valid as a file
signature, and the other way round.

### 3.4 Trust rules in the client

- **No identity, only continuity.** On the first install of a package the client records the entry's `key`
  (or "unsigned") in its database. Later:
  - the same key, or both unsigned: fine;
  - a **different** key: refused (exit 5, "the signing key of 'x' changed from A to B"). The user can
    `hom remove` and install again, which is an explicit decision;
  - unsigned after signed: refused as well (a downgrade).
- `hom show` prints the key's fingerprint and "signed" / "unsigned". `hom install` prints it too when the
  package is signed.
- A repo can **require** signatures (`require_sig`, set when the repo is added; the official catalog is
  built that way from stage 4). Unsigned entries from such a repo are refused.
- Signed does **not** mean safe or reviewed. It means: same signer as before. Review is the job of the
  catalog (the pull request, lint), not of the signature.

### 3.5 Catalog signature (optional)

A repo MAY publish `index.json.sig` and `blocklist.txt.sig` (detached signatures, 3.3) made with a *catalog
key*. A client that was given the key (`hom repo add ... --key ed25519:...`) then:

- MUST fetch the `.sig`, MUST verify it, MUST treat a missing or bad one as a failure of the whole index;
- MUST NOT use the file if it fails.

A repo without a pinned key is not checked this way. The official catalog does **not** use catalog
signatures in 2.0: its key is kept offline, and merges happen without the owner at the keyboard (see the
open questions).

### 3.6 Test vector

All of this is deterministic; an implementation MUST reproduce it.

```
seed (32 bytes, hex)  1817a3d1fecbf50dace5a618dc7f0bcc9493102643355de8cdc51e012e1829da
                      = SHA-256 of the ASCII text "hom spec test key - never trust"
public key            ed25519:g3mwF_WVsggQM4ioJDvwnD9t1ylWHIXHGf_r21nsJvM
fingerprint           891229d8b09b89ff
```

Entry message for `hello` (the first entry of `spec/examples/index.v2.json`):

```
hom-entry-v1
name=hello
version=1.0.0
type=file
os=
arch=
sha256=99a62baacd3f7958c0c7c5654e8f9c4bf0bcf89053b80724f2b60ee35cc93c95
size=13
```

```
sig  EYXoQWv61DFT4diLlyFbsYgzQwWbg4m06_Wziym_NDZGvKxp1gKv6Xc7gWuTmVrn5TXVzcgazuENxSvyW9GCCA
```

File signature of the 6 bytes `hello` + LF (message = `hom-file-v1` LF `hello` LF):

```
sig  6a89zpUjDy5xs_OwYwFWlaFrz1QCoagtDs_LT7gg8KDnj754A67Td2K0iD1U_byem7HuSA1e8nE-oGyzc0BVAw
```

This key is public. Never trust it for anything.

## 4. Blocklist

### 4.1 File

`blocklist.txt`, UTF-8, LF line ends (a CR before LF is tolerated). One rule per line:

```
<kind>:<value> [reason, free text]
```

- Empty lines and lines starting with `#` are ignored.
- The first token is the rule; everything after the first whitespace is a reason (not interpreted; shown by
  `hom show` for a blocked package).
- Kinds:

| Kind | Value | Blocks |
| --- | --- | --- |
| `sha256` | 64 lowercase hex characters | any file with that hash, in any repo |
| `key` | a public key (3.1) | any entry signed with that key, in any repo |

- A line of an unknown kind or with a malformed value is ignored (and counted in a warning), so a newer
  blocklist works with an older client.

`spec/examples/blocklist.txt` is an example.

### 4.2 Where and when

- Each repo MAY have `blocklist.txt` next to its `index.json`. A missing file (404) is an empty list.
- The client always uses the **official** blocklist, and, additionally, the blocklist of the repo it
  installs from. (A hash removed from the official catalog is blocked for every repo; that is the point.)
- Used by: `install`, `upgrade` (before the download is kept, check 5 in section 2), and `list`, which marks
  an installed package whose hash or key is now blocked ("blocked: do not use, remove it").
- Cached in the cache folder. Fetched at most once an hour. If it cannot be fetched: a cached copy of any
  age is used (with a warning when it is older than 7 days); no cached copy at all is a `network` failure
  (4) for `install` and `upgrade`.
- The blocklist only stops files that are already known. It is not a scanner.

## 5. Command line

### 5.1 Global options

| Option | Meaning |
| --- | --- |
| `--json` | Machine output (5.4). Implies non-interactive. |
| `-y`, `--yes` | Answer yes to every question (not to the question "run this installer?"; that one needs `--run`, as in 0.6.x). |
| `--root <dir>` | Everything (data, settings, cache) lives under `<dir>`, as in portable mode. For embedding. |
| `--progress=bar\|lines\|none` | As in 0.6.x. `--json` turns it to `none` unless given. |

**Non-interactive** means: no prompt is ever printed or waited for. A question that would have been asked
is answered "no" (or "yes" when `--yes`). Courtesy questions (such as "enough disk space?") are only
asked on a terminal and are simply skipped when non-interactive. A command that needs a "yes" it did not
get (running an installer) ends with code 6; the downloaded file stays in the cache.

### 5.2 Exit codes

| Code | Name (`error.code` in JSON) | Meaning |
| --- | --- | --- |
| 0 | - | Done. Includes: `search` with no hits, `upgrade` with nothing to upgrade, `list` with nothing in it. |
| 1 | `error`, `schema` | Anything else (disk, permissions, bad data folder). `schema`: the index is newer than the client (1.2). |
| 2 | `usage` | Unknown command or option, missing argument. |
| 3 | `not_found` | No such package, or no build of it for this OS and CPU; `remove` of something not installed. |
| 4 | `network` | The index, the blocklist or a download could not be fetched (no connection, HTTP error, cut-off). |
| 5 | `verify` | Size, hash, signature, key continuity, blocklist, or a refused path in an archive. |
| 6 | `declined` | A "yes" was needed and not given: the user said no, or `--json` / piped input without `--yes` / `--run`. |

Rules:

- Anything except 0 is a failure; a script that only checks "not 0" is always right.
- `upgrade` that fails for some packages and not for others: it carries on, and ends with the code of the
  **first** failure (the full list is in the JSON `failed`).
- hom never uses codes above 6 for its own purposes. The operating system's codes (a crash, Ctrl+C) can
  show up as they are.

**0.6.x compatibility:** 0.6.x returns only 0 and 1. The numbers 2 to 6 start with the Go client.

### 5.3 stdout and stderr

- Human output goes to stdout, warnings and errors to stderr, as in 0.6.x.
- With `--json`: stdout holds **one** JSON document (5.4) and nothing else, even on failure; stderr may hold
  human-readable messages (warnings, errors) for logs.

### 5.4 `--json`

One UTF-8 JSON object per run, ended with LF:

```json
{ "json": 1, "ok": true, "cmd": "install", "data": { }, "warnings": [] }
```

```json
{ "json": 1, "ok": false, "cmd": "install",
  "error": { "code": "verify", "exit": 5, "message": "sha256 of 'x' does not match the index", "package": "x" },
  "warnings": [] }
```

- `json` is the version of this output format (`1`). New keys can appear in a later `1`; clients of the
  output MUST ignore keys they do not know. A removed or renamed key means `2`.
- `ok` is `true` exactly when the exit code is 0.
- `error.code` is one of the names in 5.2; `error.exit` is the exit code; `error.package` appears when one
  package is to blame.
- `warnings` is an array of strings (may be empty or missing).
- Absent values are `null`, not empty strings. Sizes are bytes, times are RFC 3339 UTC.
- Package objects, used below (`pkg`): `name`, `type`, `version`, `description`, `author`, `license`,
  `homepage`, `os`, `arch`, `size`, `sha256`, `into`, `repo`, `signed` (bool), `key` (fingerprint or `null`),
  `blocked` (bool).

`data` per command:

| Command | `data` |
| --- | --- |
| `search <q>` | `{ "query": "...", "packages": [pkg, ...] }` |
| `show <name>` | `{ "package": pkg + { "installed": { "version", "path", "time" } or null, "upgradable": bool } }` |
| `list` | `{ "packages": [ pkg + { "installed": bool, "installed_version": str or null, "upgradable": bool } ] }` (`--installed`, `--upgradable` filter it) |
| `install <name>` | `{ "package": "x", "version": "1.0.0", "type": "plugin", "path": "...", "bytes": 123, "sha256": "...", "signed": bool, "key": fp or null, "started": bool }` (`started`: an installer was run) |
| `remove`, `purge` | `{ "removed": [ { "name": "x", "paths": ["..."] } ] }` |
| `upgrade` | `{ "upgraded": [ { "name", "from", "to" } ], "skipped": [ { "name", "reason" } ], "failed": [ { "name", "code", "message" } ] }` |
| `update` | `{ "current": "2.0.0", "latest": "2.0.1", "updated": bool }` |
| `where` | `{ "data": "...", "config": "...", "cache": "...", "portable": bool, "repo": { "name", "url" } }` |
| `ping` | `{ "repo": "official", "url": "...", "reachable": bool, "status": 200 }` |
| `clean` | `{ "freed": 123 }` |
| `repo list` | `{ "active": "official", "repos": [ { "name", "url", "official": bool } ] }` |
| `repo add`, `set`, `remove`, `set data` | `{ }` |

## 6. Paths and portable mode

| | Settings (`hom.cfg`, `hom_repos.cfg`, `hom_db.cfg`) | Data (`files/`, `apps/`) | Cache (downloads) |
| --- | --- | --- | --- |
| Windows | `%LOCALAPPDATA%\hom` | same | `<data>\dl` |
| Linux | `$XDG_CONFIG_HOME/hom` (`~/.config/hom`) | `$XDG_DATA_HOME/hom` (`~/.local/share/hom`) | `$XDG_CACHE_HOME/hom` (`~/.cache/hom`) |
| macOS | `~/Library/Application Support/hom` | same | `~/Library/Caches/hom` |
| Portable / `--root <dir>` | `<dir>` (or `<dir>/cfg` if that folder exists) | `<dir>/hom_data` | `<dir>/hom_data/dl` |

Portable mode is on when the folder of the binary has `hom.cfg` or `hom.portable` (on Windows also `cfg\`,
`hr.exe` or `homrec.exe`, the 0.6.x layout). `--root` (5.1) is the same thing chosen on the command line.
`hom set data <dir>` moves the data folder (and, where the cache lives in it, the cache). `hom where --json`
is how a program asks hom where it put things. `hom clean` empties the cache.

## Not in this spec yet

Decided later, each with its own stage:

- Several repos, names `repo:name`, `github:owner/project` sources (stage 3).
- `--proxy`, `HTTPS_PROXY`, Tor (stage 3).
- `hom keygen`, `hom sign`, `hom report`, `hom lint` (their commands and output; stage 4). The formats they
  work with are in sections 3 and 4.
- Branded catalogs through `hom.cfg` (stage 5).
- Self-update signature (stage 6).
- `depends` and post-install scripts (later).

## Open questions

1. **Who may sign for the official catalog.** Entry signatures prove "the same signer as before", not "an
   approved signer". A list of approved author keys (`keys.txt`), signed once, offline, by the owner key and
   changed rarely, would close that without the owner signing every merge. To be decided in stage 4.
2. **`size` is required?** It is optional here because 0.6.x entries lack it. It could become required in
   the official catalog by lint.
3. **Linux `program` types.** Only `.AppImage` is started; the rest is saved. Is that enough, or is
   `.AppImage` alone too narrow? To be seen when stage 2 is tested on Linux.
4. **Directory of `go/`.** `go/` is where stage 2 put the client. The module is named `github.com/homaaio/hom`,
   so moving the folder to the repo root later changes no import.
