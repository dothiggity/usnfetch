# usnfetch

Web app and downloader for usn:// shares: one program, nothing to install.

    usnfetch serve                      # http://localhost:7979/  (data = the folder the program is in)
    usnfetch serve --data D:\usnfetch   # keep settings, job state and share indexes elsewhere
    usnfetch serve --host 0.0.0.0       # reachable from other computers (set a web password first)
    usnfetch tray                       # Windows: the same, with an icon in the notification area
    usnfetch set-password               # web login password
    usnfetch list  --link usn://… --server … --user … --pass …     # list a share's files (or USNFETCH_SERVER/_PORT/_SSL/_USER/_PASS)
    usnfetch fetch --link usn://… --file NAME --out PATH --verify  # download one file

Builds are in `dist/`: `usnfetch-<version>-windows-amd64.exe`, `-linux-amd64`, `-linux-arm64`
and `-darwin-arm64`. The web UI is built into the program. `--web DIR` serves the UI from a
folder instead, so edits show on a browser refresh.

## First run

Open http://localhost:7979/. A new install starts with a short setup, much like SABnzbd's:

1. **Usenet provider**: server, port, SSL, username, password and connections, with a
   connection test. You can add several providers. If SABnzbd is installed, the setup offers to
   import its servers, passwords and priorities.
2. **Folders**: where downloads go, and where the share indexes are kept (working files, about
   200 MB per TB of share). Each folder is checked for room and write access.
3. **Adding shares**: how to bring in your links (below).

*Set up later* skips it, and **Settings → Run setup again** brings it back.

### Provider priorities

Each provider has a priority, as in SABnzbd: 0 is the highest. Providers with the best priority
download together, each up to its own connection count. A provider with a lower priority (a higher
number) is only asked for articles that the providers above it don't have, such as a block
account. Providers are managed in **Settings → Usenet providers**.

### Adding shares

usnfetch doesn't come with any shares. It opens the share links (`usn://…`) you already have,
the same way the official app does:

- **Catalog → Import list**: choose a file or paste text. Any text with `usn://` links works,
  such as a plain list, a CSV, or a web page you saved. Exported lists (`SOURCE:` sections, titles
  with `[size]`, `SKU:` / `Info:` lines) also give each share its title, listed size and source.
  Importing an updated list again only adds what is new.
- **Catalog → Open link**: paste one link to browse that share, check that it is complete, and
  pick files.
- **Activity → Add links**: queue one or more links in full.

Links are stored only in your data folder (`usnfetch.db`).

## What it does

- **Catalog**: the imported links, with search, source filter and sorting (click a column
  header). It shows each share's real size and availability once the share has been opened.
- **Share page**: the share's file tree. You can search it (every word must appear in the path,
  at least one in the name), sort it by name or size, and show it as a box-art gallery for game
  folders (LaunchBox, RetroBat / EmulationStation). Files already in your download folder are
  marked and can be hidden. *Check availability* samples the share's articles at every account.
  Any single file can be previewed, opened or saved straight from Usenet without being written on
  the server.
- **Selection and review**: ticked files and folders stay selected across folders and shares.
  Every download opens a review first. It shows the file count and size against free space,
  where the files will land, what the archives are, and how much is already on disk. There you
  choose the extraction (none / RAR and multi-part sets / everything) and how to treat files
  already there (skip same-size files / check their SHA-256 / download again). Single `.zip` and
  `.7z` files are kept zipped by default, because ROM and BIOS packs must stay that way.
- **Downloads**: several connections per account, with *backup* accounts asked only for articles
  the others lack. Every segment is decrypted on the fly and every file is checked against its
  SHA-256. Downloads resume from checkpoints. Folders keep the share's layout under
  `<download folder>/<category>/<job>/`.
- **Activity**: the queue with live progress, drag to reorder, and pause / resume. History has
  retry, resume and delete.
- **Speed & schedule**: a speed limit plus weekly rules that pause or slow downloads at set
  times.
- **Appearance** (saved per browser): menu on the left, across the top, or an icon rail; dark,
  light or system theme; comfortable or compact rows.
- **Tray icon** (Windows, `usnfetch tray`): open, status, pause, *Start with Windows* (an HKCU Run
  entry, so no administrator rights) and quit. Its log goes to `usnfetch.log`. Windows 11 first
  puts new icons behind the ^ arrow; drag the icon out to keep it visible.

Other computers can use it once a web password is set (Settings → Web access, or
`usnfetch set-password`) and it is started with `--host 0.0.0.0`. Without a password only the
machine itself is served.

## Data folder

| File | What it is |
|---|---|
| `usnfetch_gui.json` | settings and Usenet providers; passwords are sealed with Windows DPAPI (readable only by your Windows account) |
| `usnfetch.db` (+ `-wal`, `-shm`) | jobs, finished-file records, catalog, share details |
| `usnfetch_gui_state.json.lock` | makes sure only one copy runs on the folder |
| `index_cache/` | share indexes (SQLite, about 200 MB per TB of share), kept while a share is open or a job can resume |
| `usnfetch.log` | tray-mode log |

The first time it starts on a folder that holds the older Python version's state files
(`usnfetch_gui_state.json`, `usnfetch_catalog.json`, `usnfetch_shareinfo.json`), it imports
them into `usnfetch.db`. An older settings file's single account becomes the first provider.

## Code

    cmd/usnfetch/      the program: serve, tray, list, fetch, set-password
    internal/share/    share links, manifests and signatures, index building and lookups, search
    internal/engine/   downloading: connection pool, segment fetch + decrypt, packed files, streaming, speed limit
    internal/nntp/     NNTP client
    internal/yenc/     yEnc decoder
    internal/app/      settings, job queue, schedule, SQLite state
    internal/catalog/  link-list parsing
    internal/extract/  archive detection and extraction (7-Zip / UnRAR)
    internal/web/      HTTP API: shares, gallery, files already on disk
    web/               browser UI (plain JavaScript, no build step)
    assets/            icon sources; make_icons.py builds cmd/usnfetch/icons/*.ico (tray) and web/icon-*.png

The app icon is cyan when idle, red while downloading and grey while paused, in both the tray and
the browser. To change it, replace `assets/icons/source-idle.png` / `source-busy.png` and run
`python assets/make_icons.py` (needs Pillow). The icon on `usnfetch.exe` itself comes from
`cmd/usnfetch/rsrc_windows_*.syso`; after changing the idle icon, regenerate them in `cmd/usnfetch` with
`go run github.com/tc-hib/go-winres@v0.3.3 simply --icon icons\idle.ico --arch amd64,arm64`.

Build with Go 1.27: `go build -o bin/usnfetch.exe ./cmd/usnfetch`. For a release,
`CGO_ENABLED=0 GOOS=… GOARCH=… go build -trimpath -ldflags "-s -w"`.

## Tests

    go test ./...
    pip install -r requirements-test.txt
    python test_go_cli.py      # command line against a fake NNTP server and share publisher (test_rig.py)
    python test_go_web.py      # web app end to end, the same way

The end-to-end scripts test `bin/usnfetch.exe`, or `$USNFETCH_BIN` if it is set. The extract
tests need 7-Zip, and use WinRAR's Rar.exe when it is installed.

## Linux service

`/etc/systemd/system/usnfetch.service`:

    [Unit]
    Description=usnfetch
    After=network-online.target
    [Service]
    User=usnfetch
    ExecStart=/opt/usnfetch/usnfetch serve --data /srv/usnfetch --host 0.0.0.0
    Restart=on-failure
    [Install]
    WantedBy=multi-user.target

## License

MIT; see [LICENSE](LICENSE). `internal/share/gcmstream.go` adapts part of Go's standard library and
carries Go's BSD licence notice.
