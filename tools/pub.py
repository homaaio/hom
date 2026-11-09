#!/usr/bin/env python3
"""pub.py - maintain pkgs/index.json (the package list hom reads). Standard library only.

  python tools/pub.py add FILE [options]   add or update a package (computes the sha256)
  python tools/pub.py build NAME           zip pkgs/src/NAME/ into pkgs/NAME.hrp and refresh its entry
  python tools/pub.py check                verify every entry (file present, sha256, version, https)

add options:
  --name N        package name (default: file name without extension)
  --type T        plugin | file | archive | program (default: from the extension)
  --version V     default 1.0.0 (a plugin: read from its plugin.json when building)
  --desc TEXT     one line for 'hom search'
  --author A
  --url URL       the file lives elsewhere (a GitHub Release asset): the index points there and
                  the file is NOT copied into pkgs/
  --dest D        'file'/'archive': sub-folder under hom's data folder (default: the name)
  --args A        'program': arguments for the installer, e.g. "/qb"
"""
import argparse, hashlib, json, os, shutil, sys, zipfile

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
PKGS = os.path.join(ROOT, "pkgs")
INDEX = os.path.join(PKGS, "index.json")
KEYS = ["name", "type", "file", "url", "dest", "args", "description", "version", "author", "sha256"]


def load():
    with open(INDEX, encoding="utf-8-sig") as f:
        return json.load(f)


def save(idx):
    for p in idx["packages"]:  # stable key order
        ordered = {k: p[k] for k in KEYS if k in p}
        p.clear()
        p.update(ordered)
    with open(INDEX, "w", encoding="utf-8", newline="\n") as f:
        json.dump(idx, f, indent=4, ensure_ascii=False)
        f.write("\n")


def sha256(path):
    h = hashlib.sha256()
    with open(path, "rb") as f:
        for chunk in iter(lambda: f.read(1 << 20), b""):
            h.update(chunk)
    return h.hexdigest()


def guess_type(path):
    ext = os.path.splitext(path)[1].lower()
    if ext in (".hrp", ".jar"):
        return "plugin"
    if ext in (".msi", ".exe"):
        return "program"
    if ext == ".zip":
        return "archive"
    return "file"


def upsert(entry):
    idx = load()
    pk = idx["packages"]
    for i, p in enumerate(pk):
        if p["name"] == entry["name"]:
            pk[i] = {**p, **entry}
            break
    else:
        pk.append(entry)
    save(idx)


def cmd_add(a):
    src = os.path.abspath(a.file)
    if not os.path.isfile(src):
        sys.exit("pub: no such file: " + a.file)
    base = os.path.basename(src)
    e = {
        "name": a.name or os.path.splitext(base)[0],
        "type": a.type or guess_type(src),
        "file": base,
        "version": a.version or "1.0.0",
        "sha256": sha256(src),
    }
    for k in ("desc", "author", "url", "dest", "args"):
        v = getattr(a, k)
        if v:
            e["description" if k == "desc" else k] = v
    if a.url:
        if not a.url.lower().startswith("https://"):
            sys.exit("pub: --url must be https://")
        print("pub: the file is not copied; upload it to", a.url)
    else:
        dst = os.path.join(PKGS, base)
        if os.path.abspath(dst) != src:
            shutil.copyfile(src, dst)
    upsert(e)
    print("pub: %s (%s) %s sha256=%s..." % (e["name"], e["type"], e["version"], e["sha256"][:12]))


def cmd_build(a):
    d = os.path.join(PKGS, "src", a.name)
    pj = os.path.join(d, "plugin.json")
    if not os.path.isfile(pj):
        sys.exit("pub: %s has no plugin.json" % d)
    with open(pj, encoding="utf-8-sig") as f:
        meta = json.load(f)
    out = os.path.join(PKGS, a.name + ".hrp")
    files = []
    for dp, _, fn in os.walk(d):
        for n in fn:
            full = os.path.join(dp, n)
            files.append((os.path.relpath(full, d).replace(os.sep, "/"), full))
    files.sort(key=lambda t: (t[0] != "plugin.json", t[0]))  # plugin.json first, rest sorted
    with zipfile.ZipFile(out, "w", zipfile.ZIP_DEFLATED) as z:
        for rel, full in files:
            zi = zipfile.ZipInfo(rel, (1980, 1, 1, 0, 0, 0))  # fixed date: same input -> same hash
            zi.compress_type = zipfile.ZIP_DEFLATED
            zi.external_attr = 0o644 << 16
            with open(full, "rb") as f:
                z.writestr(zi, f.read())
    e = {"name": a.name, "type": "plugin", "file": a.name + ".hrp",
         "version": meta.get("version", "1.0.0"), "sha256": sha256(out)}
    if meta.get("author"):
        e["author"] = meta["author"]
    upsert(e)
    print("pub: built %s (%s) sha256=%s..." % (out, e["version"], e["sha256"][:12]))


def cmd_check(_):
    bad = 0
    for p in load()["packages"]:
        n = p["name"]
        url = p.get("url", "")
        if url and not url.lower().startswith("https://"):
            print("BAD  %s: url is not https" % n); bad += 1
        if not url:
            path = os.path.join(PKGS, p.get("file", n + ".hrp"))
            if not os.path.isfile(path):
                print("BAD  %s: %s is missing" % (n, os.path.basename(path))); bad += 1
            elif p.get("sha256") and sha256(path) != p["sha256"]:
                print("BAD  %s: sha256 differs from the file" % n); bad += 1
        if p.get("type", "plugin") in ("program", "archive") and not p.get("sha256"):
            print("BAD  %s: %s packages need a sha256" % (n, p["type"])); bad += 1
        pj = os.path.join(PKGS, "src", n, "plugin.json")
        if os.path.isfile(pj):
            with open(pj, encoding="utf-8-sig") as f:
                v = json.load(f).get("version")
            if v != p.get("version"):
                print("BAD  %s: index says %s, plugin.json says %s" % (n, p.get("version"), v)); bad += 1
    print("pub: %d problem(s)" % bad if bad else "pub: index OK")
    sys.exit(1 if bad else 0)


def main():
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    sub = ap.add_subparsers(dest="cmd", required=True)
    p = sub.add_parser("add"); p.add_argument("file")
    for k in ("name", "type", "version", "desc", "author", "url", "dest", "args"):
        p.add_argument("--" + k)
    p.set_defaults(fn=cmd_add)
    p = sub.add_parser("build"); p.add_argument("name"); p.set_defaults(fn=cmd_build)
    p = sub.add_parser("check"); p.set_defaults(fn=cmd_check)
    a = ap.parse_args()
    a.fn(a)


if __name__ == "__main__":
    main()
