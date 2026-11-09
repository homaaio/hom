# pkgs/ - the official hom package repo

`index.json` lists the packages; the small ones sit next to it. `hom` reads `index.json`, so nothing else
is needed to publish. Prefer `python tools/pub.py` over editing the JSON by hand.

```json
{
  "packages": [
    {
      "name": "intro",                      // required. letters, digits, - _ .  (no / \ : ..)
      "type": "file",                       // plugin (default) | file | archive | program
      "file": "intro.mp4",                  // file name; default <name>.hrp
      "url": "https://github.com/homaaio/hom/releases/download/pkgs/intro.mp4",
                                            // optional. where to download; default <this folder>/<file>
      "dest": "clips",                      // optional, file/archive: sub-folder of hom's data folder
      "args": "/qb",                        // optional, program: arguments for the installer
      "description": "Intro clip",
      "version": "1.0.0",                   // compared with the installed one by 'hom upgrade'
      "author": "homaaio",
      "sha256": "..."                       // hex. required for program and archive
    }
  ]
}
```

(The comments above are explanation only; real JSON has none.)

- `plugin`: `.hrp` or `.jar`. The `version` must equal the version in the plugin's `plugin.json`.
- `file`: stored as `<data>\files\<dest or name>\<file>`.
- `archive`: a `.zip`, unpacked into `<data>\apps\<dest or name>\`.
- `program`: an `.msi`/`.exe` installer. hom downloads it, checks `sha256`, and runs it only after a yes.

Size guide: under ~10 MB keep the file in this folder; anything bigger goes to a GitHub Release asset with
`url` pointing at it (see the root README).
