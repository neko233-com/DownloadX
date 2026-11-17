# DownloadX

**Fast, multi-connection download manager with a native Windows desktop app, written in Go.**

[![Release](https://img.shields.io/github/v/release/neko233-com/DownloadX?label=release)](https://github.com/neko233-com/DownloadX/releases)
[![Platform](https://img.shields.io/badge/platform-Windows%2010%2F11%20x64-0078D4)](#install)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

DownloadX is an NDM-style downloader: HTTP(S) and FTP transfers are split into
16 parallel ranges by default, progress is checkpointed to disk, and interrupted
downloads resume where they stopped. It also handles HLS, DASH, and
BitTorrent — from one CLI or one native window.

> **Platform support:** Windows 10/11 x64 only, for now. Releases ship as an MSI.
> Linux and macOS are not supported yet. This repository does **not** use GitHub
> Actions; releases are built and tagged manually.

![DownloadX native GUI](docs/gui-preview-dark.png)

## Features

- **16 parallel connections** per download (`-x`, up to 128), adaptive segment
  count for small files, and a 512 KiB minimum segment size.
- **Resume anything**: per-segment checkpoints survive crashes, Ctrl+C and
  reboots. Changed remote files are detected through size, ETag,
  Last-Modified or FTP `MDTM`.
- **HTTP/HTTPS** with redirects, custom headers, cookies, basic auth, proxies,
  speed limits, and automatic fallback when a server ignores `Range`.
- **FTP/FTPS** with EPSV/PASV, `SIZE`/`MDTM`, `REST`-based segmented transfers,
  explicit TLS (`ftps://`), and single-stream fallback.
- **HLS VOD** with master-playlist variant selection, AES-128, `EXT-X-MAP`,
  `EXT-X-BYTERANGE`, concurrent segments and part-file resume.
- **DASH static** with SegmentTemplate/Timeline/List/Base, highest-bandwidth
  video + audio, optional `ffmpeg` muxing, and DRM detection.
- **BitTorrent** magnets, `.torrent` files and `.torrent` URLs, DHT, piece-level
  resume and optional seeding.
- **Native desktop UI** built with [MyGo](https://github.com/egoist/mygo) and
  [Lobe MyGo](https://github.com/lobehub/lobe-mygo): GPU-drawn, no webview, no
  HTML, no JavaScript. Light and dark follow the system.
- **Pure Go, no cgo.** The only non-stdlib downloader dependency is
  `anacrolix/torrent`, for BitTorrent.

## Install

### One line (PowerShell)

```powershell
irm https://raw.githubusercontent.com/neko233-com/DownloadX/main/install.ps1 | iex
```

The script downloads the latest MSI, asks Windows for administrator permission,
and installs to `C:\Program Files\DownloadX`. Launch **DownloadX** from the Start
menu, or run the CLI at `C:\Program Files\DownloadX\downloadx.exe`.

### One line (Git Bash, MSYS, Cygwin, WSL with Windows interop)

```sh
curl -fsSL https://raw.githubusercontent.com/neko233-com/DownloadX/main/install.sh | sh
```

### MSI directly

Download **DownloadX-windows-amd64.msi** from
[Releases](https://github.com/neko233-com/DownloadX/releases/latest) and open it.
The installer is per-machine (UAC prompt), installs both executables, and adds a
Start-menu shortcut.

### Build from source

Go **1.27.3 or later** is required because of MyGo.

```powershell
git clone https://github.com/neko233-com/DownloadX
cd DownloadX
go build -o downloadx.exe ./cmd/downloadx
go build -tags=mygo_nowebview -o downloadx-gui.exe ./cmd/downloadx-gui
```

`mygo_nowebview` removes WebView2 code from the Windows build, which also avoids
antivirus heuristics on unsigned executables.

## CLI

```powershell
downloadx [flags] <url|magnet:...|file.torrent>...
downloadx gui
downloadx version
downloadx help
```

```powershell
# 16 connections, inferred file name
downloadx https://releases.ubuntu.com/24.04/ubuntu-24.04-desktop-amd64.iso

# 32 connections, explicit output, speed limit
downloadx -x 32 -o ubuntu.iso --limit 20M https://example.com/ubuntu.iso

# Resume an interrupted download (same command)
downloadx -x 32 -o ubuntu.iso https://example.com/ubuntu.iso

# Batch from a file, one source per line
downloadx -d $HOME\Downloads -i urls.txt

# HLS VOD, best variant
downloadx https://example.com/vod/master.m3u8

# DASH static
downloadx https://example.com/stream/manifest.mpd

# BitTorrent
downloadx "magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567"
downloadx .\debian.torrent

# Machine-readable progress
downloadx --json https://example.com/file.zip
```

### Flags

| Flag | Meaning |
| --- | --- |
| `-o, --output PATH` | Output file (single source) or directory |
| `-d, --dir DIR` | Destination directory (default `.`) |
| `-x, --connections N` | Parallel connections per download (default 16, max 128) |
| `-i, --input FILE` | Read sources from a file (`#` comments allowed) |
| `-H, --header "K: V"` | Extra HTTP header (repeatable) |
| `--cookie VALUE` | Cookie header |
| `--referer URL` | Referer header |
| `--user-agent STR` | User-Agent (default `DownloadX/<version>`) |
| `-u, --user USER:PASS` | HTTP basic auth / FTP login |
| `--proxy URL` | `http`, `https` or `socks5` proxy |
| `--retry N` | Retries per segment or part (default 3) |
| `--timeout DUR` | Dial and response-header timeout (default 30s) |
| `--limit RATE` | Speed limit per task, e.g. `5M`, `800K` |
| `--type TYPE` | `auto`, `http`, `ftp`, `hls`, `dash`, `bt` |
| `--no-resume` | Discard resume state and start over |
| `--insecure` | Skip TLS certificate verification |
| `--json` | JSON-lines progress on stdout |
| `-q, --quiet` | No progress output |
| `--hls-quality Q` | `best`, `worst`, or a 0-based variant index |
| `--bt-port PORT` | BitTorrent listen port (0 = random) |
| `--seed` | Keep seeding after a torrent completes |
| `--ffmpeg PATH` | ffmpeg binary for DASH audio/video muxing |
| `-v, --version` | Print version |
| `-h, --help` | Print help |

Exit codes: `0` success, `1` failure, `2` usage, `130` interrupted.

### Resume state

- HTTP/FTP: `<output>.downloadx` stores the URL, size, validators and per-segment
  offsets. It is deleted after a successful download.
- HLS/DASH: `<output>.parts/` holds atomically written segment files plus a
  manifest; a changed playlist discards the old parts.
- BitTorrent: the data directory keeps piece completion, so re-running the same
  command resumes automatically.

DownloadX never silently overwrites an unrelated file. Auto-named downloads get
a `name (1).ext` suffix; an explicit `-o` that already exists without resume
state is an error (use `--no-resume` to overwrite).

## Desktop app

```powershell
downloadx-gui      # or: downloadx gui
```

The window is drawn natively on the GPU by MyGo, using Lobe UI components and
design tokens. There is no webview and no front-end build.

- Paste a URL, magnet link, `.torrent` path, HLS playlist or DASH manifest.
- Pick a folder with the native folder picker.
- Choose the connection count and force a protocol if auto-detection is not
  what you want.
- Two downloads run at once; the rest wait in the queue.
- Cancel, retry, remove, copy the output path, or reveal it in the file manager.
- Progress updates include speed, ETA, segment counts and torrent peers.

| Dark | Light |
| --- | --- |
| ![Dark](docs/gui-preview-dark.png) | ![Light](docs/gui-preview-light.png) |

## Protocol support

| Source | Status | Notes |
| --- | --- | --- |
| HTTP / HTTPS | Stable | Range segments, resume, redirects, auth, cookies, proxy, speed limit, Range-less fallback |
| FTP / FTPS | Stable | EPSV/PASV, `SIZE`/`MDTM`, `REST` segments, explicit TLS, fallback without `REST` |
| HLS (VOD) | Stable | Master playlists, AES-128, `EXT-X-MAP`, byte ranges, concurrent segments |
| DASH (static) | Stable | SegmentTemplate, SegmentTimeline, SegmentList, SegmentBase; audio/video muxing with ffmpeg |
| BitTorrent | Stable | Magnet, `.torrent` file/URL, DHT, resume, optional seeding |

### Known limitations

- Windows only for now; Linux and macOS builds are not shipped or supported.
- HLS **live** playlists and SAMPLE-AES/DRM streams are refused.
- DASH **dynamic** manifests and DRM (`ContentProtection`) are refused; only
  the first `Period` is downloaded.
- Separate DASH audio and video are muxed with `ffmpeg` when it is available.
  Otherwise two files are kept and the window/CLI tells you so.
- `ftps://` means explicit TLS (`AUTH TLS`); implicit FTPS and SFTP are not
  supported.
- No browser extension yet; browser handoff is on the roadmap.
- BitTorrent v2-only torrents are not guaranteed; v1 and hybrid torrents work.

## Manual releases

There is no CI/CD. Tags and GitHub releases are created by hand. See
[RELEASE.md](RELEASE.md) for the checklist. In short:

```powershell
powershell -ExecutionPolicy Bypass -File scripts\build-msi.ps1 -Version 0.1.0
git tag -a v0.1.0 -m "DownloadX 0.1.0"
git push origin v0.1.0
gh release create v0.1.0 dist\DownloadX-windows-amd64.msi dist\DownloadX-windows-amd64.msi.sha256
```

The build script produces `dist\DownloadX-windows-amd64.msi` with a stable asset
name, so the `releases/latest/download/DownloadX-windows-amd64.msi` URL used by
the install scripts always works. It requires Go 1.27.3+ and WiX Toolset 3.14
(`candle.exe`, `light.exe`).

## Architecture

```
cmd/downloadx              CLI entrypoint
cmd/downloadx-gui          native desktop entrypoint
internal/cli               argument parsing, run loop
internal/runner            protocol detection and dispatch
internal/httpdl            segmented HTTP(S) downloader
internal/ftp               FTP/FTPS client and segmented downloader
internal/hls               HLS playlist parser and downloader
internal/dash              MPD parser and downloader
internal/segdl             concurrent part download + ordered concat
internal/bt                anacrolix/torrent adapter
internal/engine            task model, naming, resume state, rate limiter
internal/progress          TTY / plain / JSON progress reporters
internal/desktop           MyGo + Lobe UI window, queue manager
installer/                 WiX source, license, icon
scripts/build-msi.ps1      Windows MSI build
install.ps1, install.sh    one-line installers
```

## Development

```powershell
go vet ./...
go test ./...          # local HTTP/FTP/HLS/DASH/BitTorrent tests
```

GUI previews can be rendered without opening a window:

```powershell
$env:DOWNLOADX_PREVIEW="$PWD\docs\gui-preview.png"
go test ./internal/desktop -run TestPreviewImage -v
```

## License

[MIT](LICENSE)
