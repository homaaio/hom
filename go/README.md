# hom, the Go client (2.x)

Written against [`spec/index.md`](../spec/index.md). The C++ client in `src/` is the 0.6.x line.

```
go/
  cmd/hom/       main.go: arguments, terminal, Ctrl+C -> package app
  pkg/app/       the commands (every one is a method of Env and can be tested without a terminal)
  pkg/idx/       index parsing, names, versions, choice of the entry for a platform
  pkg/cfg/       hom.cfg, hom_repos.cfg, hom_db.cfg (the formats of 0.6.x)
  pkg/dirs/      where files go: per OS, portable mode, --root
  pkg/web/       https-only client, streaming download with a hash made on the way
  pkg/unz/       zip extraction that refuses paths leaving the folder
```

Only the standard library is used (Go 1.22). The module is called `github.com/homaaio/hom`, so moving `go/`
to the root of the repo later changes no import.

## Build and test

```
cd go
go vet ./...
go test ./...                 # no network: a local TLS server plays the repo
go build -o hom ./cmd/hom     # Windows: -o hom.exe
go build -ldflags "-X main.version=2.0.0" -o hom ./cmd/hom
```

## A short try

```
./hom --root /tmp/homtry ping
./hom --root /tmp/homtry search hello
./hom --root /tmp/homtry --json where
```

`--root <folder>` keeps settings, data and cache under that folder (portable mode), so a trial leaves
nothing in your profile.

## Status

Stage 2 of the plan. It was written without a Go compiler at hand: expect a few compile errors on the first
`go vet`, and please send them back. What is missing is listed at the end of the top entry of
[`CHANGELOG.md`](../CHANGELOG.md).
