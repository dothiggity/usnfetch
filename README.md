# usnfetch

usnfetch downloads usn:// shares straight from your own Usenet provider. It's a single
program with a web page you leave open in a tab, a bit like SABnzbd or the *arr apps: you add
share links, look through what's in them, pick what you want, and it lands in a folder on your
disk, decrypted and checked.

I wanted that workflow for `usn://` links. Before this, getting a share into SABnzbd meant a
bridge app that never quite worked, so usnfetch does the whole job itself: it fetches the
articles, decrypts them as they arrive, writes each file once, and checks it against the share's
SHA-256 when it's done.

It's an independent project and isn't affiliated with any other app. It doesn't come with any
shares either. You bring your own links, and what you download is up to you.

## Getting started

There's nothing to install. Grab the latest build from the
[Releases page](https://github.com/dothiggity/usnfetch/releases): the `windows-amd64` zip for most
Windows PCs, `linux-amd64` or `linux-arm64` (a Raspberry Pi 4 or 5 with a 64-bit OS) for Linux.
Building it yourself takes one command too (see further down).

Unzip it wherever you like. On Windows, double-click `usnfetch.exe`: it settles in next to the
clock and opens the web page in your browser. From a terminal it's:

    usnfetch tray        # Windows: the same as double-clicking
    usnfetch serve       # anywhere: runs in the terminal

The web page is at http://localhost:7979/. The first time, a short setup walks you through it:

1. **Your Usenet provider.** Server, port, username, password and how many connections your plan
   allows. It tests the connection for you. If SABnzbd is installed on the same machine, it can
   copy your servers over from there, passwords and all.
2. **Folders.** Where downloads go. It checks the folder exists (or can be made) and how much
   room is left.
3. **Adding shares.** A quick pointer to the next section.

You can skip the setup and come back to it later from Settings.

### More than one provider

Every provider gets a priority, the same idea as in SABnzbd: 0 is the highest. Providers that
share the top priority all download together. One with a lower priority (a bigger number) is only
asked for articles the others are missing, which is what you want for a block account.

### Adding shares

usnfetch opens the share links you already have. There are three ways in:

- **Catalog → Import list.** Pick a file or paste text. Anything with `usn://` links in it works:
  a plain list, a CSV, even a saved web page. If the list is one of those exports with `SOURCE:`
  headings, titles with `[size]` and `SKU:` lines, each share also gets its proper name and listed
  size. Importing a list again adds the new links and fills in anything the catalog didn't know.
  If it lists a share you already have under a different title or size, it shows you the
  difference first and keeps what you have unless you tick otherwise. There's an Undo, too.
- **Catalog → Open link.** Paste a single link to look inside that share before deciding.
- **Activity → Add links.** Paste one or more links to download them whole, no browsing.

The links stay on your machine, in the data folder.

Once the catalog gets big, two menus narrow it down: the launcher a build runs (Batocera,
RetroBat, LaunchBox, RetroPie…) and the device it's for (PC, Steam Deck, ROG Ally, Raspberry Pi 4
or 5, Anbernic…). usnfetch reads those from the titles and labels, so there's nothing to tag by
hand. Click a launcher or device chip on any entry to see only those.

The Opened column keeps track of what you've already looked at. "Cached" means the share's index
is still on disk, so it opens in seconds; after a few days unused that's cleared to save space and
the column just says when you last opened it. Sort by it, or show only the shares you've opened,
the cached ones, or the ones you haven't opened yet.

The Health column says whether a share is still on your provider. Right after an import, usnfetch
asks your provider about each new link, one small request apiece, so dead links show up as
"Gone" straight away and the rest as "Link OK". Opening a share goes further and checks a sample
of its actual files, which turns Health into "Available", "Partial" or "Missing". Links you
imported before adding a provider get checked as soon as you add one, and you can check any again
by ticking them and pressing Check links. Pasted links get the same check in the download review,
which points out any that are gone and leaves them out.

## What it's like to use

Open a share and you get its folders and files, with search, sorting by name or size, and for game
collections (LaunchBox, RetroBat and other EmulationStation setups) a gallery of box art. You can
tick files and folders as you go; the selection sticks while you wander around other folders.
Anything already in your download folder is marked, so it's easy to grab only what's missing.
Videos, pictures and text files can be previewed in the browser, streamed straight from Usenet.

Before anything downloads, you get a review: how big it is next to the free space you have, where
the files will end up, what kind of archives are in there and how much of it you already have.
That's also where you decide whether to unpack archives. By default it unpacks RAR and multi-part
sets but leaves single `.zip` and `.7z` files alone, because ROM and BIOS packs are supposed to stay
zipped.

While things download, a bar along the bottom of every page shows what's going on: the job, the
file it's on, progress, ETA, speed and free space, with a pause button. The Activity page has the
full queue (drag jobs to reorder them, or hold one back with its own pause) and the history. You can pause for a while, set a speed
limit, or set a weekly schedule, say slower during the day and full speed at night.

On Windows, right-click usnfetch next to the clock to open the app, pause, restart it, or have it
start with Windows. Windows 11 tends to tuck new ones behind the ^ arrow, so drag it out if you
want to keep it in view.

The look is up to you too: menu on the left, across the top or as a slim icon bar, dark or light,
roomy or compact. That's remembered per browser.

## Running it on another machine

By default only the computer it runs on can reach the web page. To use it from elsewhere on your
network, set a password first (Settings → Web access, or `usnfetch set-password`), then start it
with `--host 0.0.0.0` and browse to `http://<that machine>:7979/`.

On Linux you'll probably want it as a service. Something like this in
`/etc/systemd/system/usnfetch.service` does the job:

    [Unit]
    Description=usnfetch
    After=network-online.target
    [Service]
    User=usnfetch
    ExecStart=/opt/usnfetch/usnfetch serve --data /srv/usnfetch --host 0.0.0.0
    Restart=on-failure
    [Install]
    WantedBy=multi-user.target

## Where it keeps things

Settings, the catalog, job history and a cache of share indexes all live in one data folder:
`%LOCALAPPDATA%\usnfetch` on Windows, `~/.local/share/usnfetch` on Linux. It's kept apart from the
program on purpose, so you can replace `usnfetch.exe` with a newer one, or move it, and everything
carries on. `--data <folder>` puts it somewhere else.

If you'd rather keep everything next to the program, say on a USB stick, create an empty file
called `portable` in the same folder as `usnfetch.exe`.

Each share you open leaves its index in the data folder, so opening it again is instant. Shares
never change, so nothing there expires. A full index, the kind downloads, previews and checks
need, is roughly 100 MB per TB of share. The ones you used most recently are kept up to a limit
(20 GB unless you change it in Settings). Past that, the least recently used are cut down to just
their file list, which is zipped while it isn't in use and is usually a few MB. Browsing those
shares is still instant, and the rest of the index is fetched again the first time you preview,
check or download something from them. Box art you've already looked at is kept too, so a gallery
you've seen opens without going back to Usenet. On Windows the full indexes are also compressed by
Windows itself while they're not in use, which roughly halves them again. The System page shows
what all this takes.

Provider passwords are encrypted with Windows' own data protection, so only your Windows account can
read them.

If you're coming from the old Python version of usnfetch, point it at the same folder and it pulls
in your old jobs, catalog and settings the first time it starts.

## A couple of command-line extras

    usnfetch list  --link usn://… --server … --user … --pass …     # what's in a share
    usnfetch fetch --link usn://… --file NAME --out PATH --verify  # grab one file

Instead of the flags, the account can come from `USNFETCH_SERVER`, `USNFETCH_PORT`,
`USNFETCH_SSL`, `USNFETCH_USER` and `USNFETCH_PASS`.

## Building it yourself

You need Go 1.27. From the top of the repository:

    go build -o bin/usnfetch.exe ./cmd/usnfetch

The web page is plain JavaScript with no build step, and it gets baked into the program. While
you're working on it, `--web web` serves the files from disk instead, so a browser refresh is
enough to see a change.

Release builds are plain cross-compiles with no C toolchain needed:

    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "-s -w" ./cmd/usnfetch

A quick map of the code:

    cmd/usnfetch/      the program itself: serve, tray, list, fetch, set-password, version
    internal/share/    share links, manifests and signatures, building and searching the index
    internal/engine/   the downloading: connections, decrypting, packed small files, streaming, speed limit
    internal/nntp/     a small NNTP client
    internal/yenc/     yEnc decoding
    internal/app/      settings, the job queue, the schedule, saved state
    internal/catalog/  reading link lists, and the launcher and device each entry is for
    internal/extract/  finding and unpacking archives (7-Zip / UnRAR)
    internal/web/      the web API: shares, gallery, files already on disk, setup
    web/               the browser UI

## Tests

    go test ./...

## Making a release

Write what changed in `.github/release-notes/v<version>.md`, set `Version` in
`cmd/usnfetch/serve.go` to match, commit, then tag and push:

    git tag -a v0.10.0 -m "usnfetch 0.10.0"
    git push origin main v0.10.0

GitHub Actions runs the tests on Windows and Linux, builds every download with GoReleaser
(`.goreleaser.yaml`) and publishes the release with those notes.

## License

MIT, see [LICENSE](LICENSE). One file, `internal/share/gcmstream.go`, borrows a piece of Go's
standard library and keeps Go's BSD notice.
