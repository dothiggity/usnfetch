# Working on usnfetch

## Building

You need Go 1.27. No C toolchain, so cross-compiling is just `GOOS`/`GOARCH`.

    go build -o bin/usnfetch.exe ./cmd/usnfetch
    go test ./...

The web page is plain JavaScript built into the program. `--web web` serves it from disk instead,
so a browser refresh shows your changes.

## Where things are

    cmd/usnfetch/      the program: serve, tray, list, fetch, set-password, version
    internal/share/    share links, manifests, the index, games and BIOS files
    internal/engine/   downloading, decrypting, streaming, speed limit, checks
    internal/nntp/     NNTP client and connection limits
    internal/yenc/     yEnc decoding
    internal/app/      settings, the queue, the schedule, saved state, libraries, resets
    internal/catalog/  link lists, launchers and devices
    internal/extract/  unpacking (7-Zip / UnRAR)
    internal/web/      the web API
    web/               the browser UI

## Releases

1. Write `.github/release-notes/v<version>.md`.
2. Set `Version` in `cmd/usnfetch/serve.go` and commit.
3. Tag and push: `git tag -a v1.2.3 -m "usnfetch 1.2.3"`, then `git push origin main v1.2.3`.

GitHub Actions runs the tests (on Linux with Go's race detector too) and publishes the downloads
with GoReleaser (`.goreleaser.yaml`).
