# DownloadX Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [x]`) syntax for tracking.

**Goal:** `downloadx` — an NDM-style, multi-connection download manager with a CLI and a native desktop app, in pure Go, supporting HTTP(S), FTP/FTPS, HLS, DASH and BitTorrent/magnet on Windows, macOS and Linux.

**Architecture:** A small `internal/engine` core owns task configuration, destination naming and resume state. `internal/runner` dispatches to protocol packages (`httpdl`, `ftp`, `hls`, `dash`, `bt`) that report progress through one callback. `internal/cli` parses arguments and renders text/JSON progress; `internal/desktop` is a native MyGo + Lobe UI window over a two-worker queue manager. HTTP and FTP share the segmented-download resume model; HLS/DASH share a part-file pipeline in `internal/segdl`; BitTorrent is adapted through one narrow wrapper.

**Tech Stack:** Go 1.27 standard library; `github.com/egoist/mygo` for the native, GPU-drawn desktop UI and `github.com/lobehub/lobe-mygo` for Lobe UI components and tokens (no webview, no HTML, no JavaScript); `github.com/anacrolix/torrent` for BitTorrent; no cgo. Optional `ffmpeg` is used only to mux separate DASH audio/video when present.

**Status:** implemented — Windows CLI + MSI, all five protocols, native GUI, Chrome/Edge extension with store-free auto-update, tests, manual releases; no GitHub Actions.

**Spec:** this document — the Design section below is the spec.

## Design

### Product

- Public repo `neko233-com/DownloadX`, MIT. CLI command `downloadx`, desktop app `downloadx-gui`.
- One static binary per front end, no runtime dependencies, no cgo, no telemetry.
- Windows 10/11 x64 is the only supported release platform for now; shipped as a per-machine MSI.
- Default 16 parallel connections per download (`-x` overrides), NDM-style segmented transfers with resume.
- Supported sources: `http://`, `https://` (including Range-less fallback), `ftp://`, `ftps://` (explicit TLS), HLS `.m3u8` (VOD), DASH `.mpd` (static), `magnet:` links and `.torrent` files.
- Auto-detection by URL scheme, path extension and `Content-Type`; `--type` forces a protocol.
- Resume works across process restarts. State lives beside the output file and is deleted on success.
- Never overwrite an existing file silently; use resume when valid, otherwise pick `name (1).ext`.
- Human output goes to stderr; `--json` emits JSON-lines progress on stdout. Exit codes: `0` success, `1` failure, `2` usage, `130` interrupted.
- Native desktop UI in `downloadx-gui`, drawn by MyGo with Lobe MyGo components and tokens.

### CLI

```
downloadx [flags] <url|magnet|file.torrent>...
downloadx gui
downloadx version

-o, --output PATH        output file (single source) or directory
-d, --dir DIR            destination directory (default ".")
-x, --connections N      parallel connections per download (default 16, max 128)
-i, --input FILE         read one source per line ("#" comments, blank lines skipped)
-H, --header "K: V"      extra HTTP header (repeatable)
    --cookie VALUE       Cookie header
    --referer URL        Referer header
    --user-agent STR     User-Agent (default "DownloadX/<version>")
-u, --user USER:PASS     HTTP basic auth / FTP login (URL credentials win)
    --proxy URL          http, https or socks5 proxy (HTTP/HTTPS/HLS/DASH)
    --retry N            retries per segment or part (default 3)
    --timeout DUR        dial/response-header timeout (default 30s)
    --limit RATE         speed limit per task, e.g. 5M, 800K (default unlimited)
    --type TYPE          auto|http|ftp|hls|dash|bt (default auto)
    --no-resume          discard any resume state and start over
    --insecure           skip TLS certificate verification
    --json               JSON-lines progress on stdout
-q, --quiet              no progress display
    --hls-quality Q      best|worst|N (N = variant index, 0-based; default best)
    --bt-port PORT       BitTorrent listen port (default 0 = random; DHT on)
    --seed               keep seeding after a torrent completes
    --ffmpeg PATH        ffmpeg binary for DASH muxing (default "ffmpeg")
-v, --version            print version
-h, --help               print help
```

Flags may appear before or after positional sources. `--` ends flag parsing.

### Native desktop app (`downloadx-gui`)

- MyGo native UI (`github.com/egoist/mygo/ui`), drawn on the GPU: no webview, no HTML/CSS/JS, no front-end build. Lobe MyGo (`github.com/lobehub/lobe-mygo`) supplies the component kit and design tokens.
- Window: 1180×820, hidden title bar with a view-drawn Lobe-style header, follows the system light/dark appearance, Lobe tokens for color, radii and typography.
- Composer: URL / magnet / `.torrent` input; destination folder with the native folder picker; connection count; protocol override (Auto, HTTP, FTP, HLS, DASH, BitTorrent).
- Queue: two downloads at once, newest first; status tags, progress bar, speed, ETA/segment counts, torrent peer count; cancel, retry, remove, copy path and reveal in the file manager.
- The manager lives in `internal/desktop` (`Manager`, at most two workers, mutex-protected snapshots) and repaints the window from worker goroutines with `Window.Invalidate`.
- Design previews are rendered headlessly by `TestPreviewImage` (`DOWNLOADX_PREVIEW=path.png`) and committed under `docs/`.

### Transfer behavior

**HTTP/HTTPS**
1. Probe with `HEAD`; if that fails, `GET` with `Range: bytes=0-0`.
2. Detect `Content-Length`, `Accept-Ranges`, final URL, `ETag`, `Last-Modified`, `Content-Disposition`, `Content-Type`.
3. If ranges are supported and length ≥ 1 MiB: split into `min(connections, max(1, ceil(size / 512 KiB)))` segments, preallocate the file, run one worker per segment using `WriteAt`, verify each `206 Content-Range` start, retry per segment, checkpoint state every second and on cancellation.
4. If ranges are unsupported, size is unknown, or connections is 1: stream sequentially with the same progress/limit plumbing. A ranged worker that receives `200` aborts the segmented attempt and restarts as a single stream (file truncated first).
5. Auto type: `.m3u8`/`.mpd` extensions or `application/vnd.apple.mpegurl`/`application/dash+xml` content types hand off to HLS/DASH.

**FTP/FTPS**
- Minimal FTP client: greeting, `USER`/`PASS`, `TYPE I`, `FEAT` (optional), `SIZE`, `MDTM`, `EPSV` then `PASV`, `REST` + `RETR`, multi-line replies, explicit TLS (`AUTH TLS`, `PBSZ 0`, `PROT P`).
- Segmented like HTTP when `SIZE` and `REST` work; each worker owns its control + data connection. Fall back to one `RETR` stream when `SIZE`/`REST` fail. Resume state keyed by size and `MDTM`.
- Credentials from URL, `-u`, else anonymous.

**HLS (VOD)**
- Parse master and media playlists: `EXT-X-STREAM-INF`, `EXTINF`, `EXT-X-KEY` (AES-128 only), `EXT-X-MAP`, `EXT-X-BYTERANGE`, quoted attribute lists.
- `--hls-quality` picks a variant (default highest bandwidth). Reject live playlists (no `EXT-X-ENDLIST`) and non-AES-128 encryption.
- Download parts concurrently (default 16), decrypt AES-128-CBC with explicit IV or media-sequence IV, concatenate in order into `.ts` (or `.mp4` when an init map exists).
- Resume via `<dest>.parts` with atomically renamed part files plus a manifest; discard parts when the playlist URL/segment count changes.

**DASH (static)**
- Parse MPD: `SegmentTemplate` (`$Number$`, `$Time$`, `$RepresentationID$`, `$Bandwidth$`, `%0Nd` width), `SegmentTimeline`, `SegmentList`, `SegmentBase`; `BaseURL` inheritance across MPD/Period/AdaptationSet/Representation.
- Pick highest-bandwidth video and audio representations. DRM (`ContentProtection`) is refused with a clear error. Dynamic (live) MPDs are refused.
- Download init + media segments into `<dest>.parts`, then: muxed representation → single output; separate audio/video → mux with `ffmpeg -c copy` if available, otherwise keep `<base>.video.<ext>` and `<base>.audio.<ext>` and tell the user to mux.
- Resume mirrors HLS via the parts manifest.

**BitTorrent**
- `magnet:` and local `.torrent` inputs. Data directory from `-d`/`-o`. DHT on; listen port from `--bt-port` (0 = random); stop after completion unless `--seed`.
- Progress from torrent stats (byte counts); metadata fetch phase reported separately. Resume is handled by the torrent client's piece storage in the data directory.

### Shared pieces

- `engine.Task` carries every option; `engine.Progress` is the single progress shape.
- Destination naming: `Content-Disposition` (incl. `filename*` RFC 5987) → URL path → `download`; sanitize Windows-forbidden characters/reserved names; unique suffix on collision.
- Resume state JSON `<dest>.downloadx`: URL, size, ETag/Last-Modified (HTTP) or MDTM (FTP), per-segment `{start,end,done}`. Atomic write (temp + rename). Invalid/mismatched state is discarded with a message.
- Speed limiter: hand-written token bucket shared by all workers of one task.
- Progress renderer: TTY in-place bar (percent, bytes, speed, ETA, connection count); plain periodic lines when not a TTY; JSON-lines with `--json`; silent with `--quiet`.

### Repository layout

```
cmd/downloadx/main.go          CLI entrypoint, version wiring, signal handling
cmd/downloadx-gui/main.go      native desktop entrypoint (MyGo window)
cmd/downloadx-host/            Chrome/Edge native messaging host
internal/cli/                  argument parsing, run loop
internal/runner/               protocol detection and dispatch
internal/engine/               Task/Progress/Result, naming, resume state, limiter
internal/httpdl/               HTTP(S) segmented downloader
internal/ftp/                  FTP/FTPS client + segmented downloader
internal/hls/                  HLS playlist parser + downloader
internal/dash/                 MPD parser + downloader
internal/segdl/                concurrent part fetch/rename/concat pipeline
internal/bt/                   anacrolix/torrent adapter
internal/progress/             TTY/plain/JSON reporters
internal/desktop/              MyGo + Lobe UI window, queue manager, previews
internal/inbox/                browser-to-desktop request queue
internal/version/              version variables (ldflags)
extensions/downloadx/          Chrome/Edge MV3 extension
extensions/README.md           extension ID, key and update notes
docs/extension/                self-hosted CRX and updates.xml
installer/                     WiX source, native manifest, icon and license RTF
scripts/build-msi.ps1          Windows x64 MSI build
scripts/build-extension.ps1    CRX pack + update manifest
install.ps1, install.sh        one-line installers
RELEASE.md, AGENTS.md          manual release and agent instructions
```

## Global Constraints

- Module `github.com/neko233-com/DownloadX`; repo `DownloadX` (case-sensitive); commands `downloadx` and `downloadx-gui`.
- `go 1.27.3` (MyGo needs it); `CGO_ENABLED=0`; supported release: Windows 10/11 x64.
- Direct dependencies: stdlib plus `github.com/anacrolix/torrent` (BitTorrent), `github.com/egoist/mygo` (native UI) and `github.com/lobehub/lobe-mygo` (Lobe UI kit). No ffmpeg hard dependency.
- No GitHub Actions/CI/CD. Tags and GitHub releases are created manually; see `RELEASE.md`.
- Browser extension: fixed ID `pdlpjmbjgipkbjekncinhchgbahpgbng`, installed by Chrome/Edge policy from jsDelivr `docs/extension/updates.xml`; no Web Store. Signing key stays untracked.
- The MSI install path is fixed at `C:\Program Files\DownloadX` because the native messaging manifest contains that absolute path.
- Default 16 connections; clamp to `[1,128]`. Minimum segment size 512 KiB.
- State artifacts: `<dest>.downloadx` (JSON) and `<dest>.parts/` (directory); removed after success.
- Never overwrite an existing file unless resuming it. Sanitized names must stay inside the destination directory.
- Human-readable output to stderr; JSON progress to stdout. Exit codes `0/1/2/130`.
- Every task ends with `go vet ./...` and `go test ./...` green, and a commit.

## Review Focus

1. Server ignores `Range` mid-transfer → segmented attempt must abort cleanly, truncate, and finish as a single stream with byte-identical output.
2. Ctrl+C or process kill mid-download → state file remains valid and a rerun produces a byte-identical file.
3. Remote file changed between runs (size, ETag, Last-Modified, MDTM) → stale state is discarded; no mixed bytes.
4. Hostile names from `Content-Disposition`/URL (`../`, `C:`, `CON`, trailing dots) → sanitized, unique, inside destination directory.
5. Disk full / permission denied → clear error, bounded retries, partial state saved for later resume.

## Tasks

### Task 1: Module, CLI skeleton, naming, state, limiter, local build checks

**Files:**
- Create: `go.mod`, `cmd/downloadx/main.go`, `internal/version/version.go`, `internal/cli/cli.go`, `internal/cli/parse.go`, `internal/cli/parse_test.go`, `internal/engine/engine.go`, `internal/engine/naming.go`, `internal/engine/naming_test.go`, `internal/engine/state.go`, `internal/engine/state_test.go`, `internal/engine/limit.go`, `internal/engine/limit_test.go`, `.github/workflows/ci.yml`

**Interfaces:**
- Produces:
  - `engine.Task{URL, Dest, Dir string; Connections int; Headers map[string]string; Cookie, Referer, UserAgent, Username, Password, Proxy string; Insecure bool; Timeout time.Duration; Retries int; NoResume bool; Limit int64; Type Type; HLSQuality string; BTPort int; Seed bool; FFmpeg string; Progress func(Progress)}`
  - `engine.Progress{Phase string; Done, Total int64; ItemsDone, ItemsTotal int; Connections int; Message string}`
  - `engine.Result{Path string; Bytes int64; Files []string}`
  - `engine.Run(ctx context.Context, t *Task) (*Result, error)`
  - `engine.SanitizeFilename(string) string`, `engine.FilenameFromDisposition(string) string`, `engine.FilenameFromURL(string) string`, `engine.ResolveDest(*Task, string) (string, error)`
  - `engine.LoadState(dest string) (*State, error)`, `engine.SaveState(dest string, s *State) error`, `engine.RemoveState(dest string) error`
  - `engine.NewLimiter(bytesPerSec int64) *Limiter` with `WaitN(ctx context.Context, n int) error`
  - `cli.Parse(args []string) (*Options, error)`, `cli.Main(args []string, stdout, stderr io.Writer) int`

- [x] **Step 1: Write failing parser tests** covering defaults (`Connections == 16`, type auto, retries 3, timeout 30s), flags after URLs, repeated `-H`, `--` terminator, unknown flag error, `-x 0` rejected, input file lines.
- [x] **Step 2: Run `go test ./internal/cli`; expect failures.**
- [x] **Step 3: Implement `cli.Parse`** (hand-written parser, help text), `engine` types, naming helpers, state JSON, limiter.
- [x] **Step 4: Write failing naming/state/limiter tests**: traversal names (`..\..\evil` → inside dir), reserved Windows names, disposition `filename*`, unique collision suffix, state round-trip and mismatch rejection, limiter waits ≈ n/rate.
- [x] **Step 5: Implement helpers until tests pass.**
- [x] **Step 6: Create `cmd/downloadx/main.go`** with signal-aware context, `version`, exit codes; verify with local `go vet`/`go test`/`go build` (no hosted CI).
- [x] **Step 7: `go vet ./... && go test ./... && go build ./cmd/downloadx`; commit `feat: bootstrap CLI, engine skeleton, naming and resume state`.**

### Task 2: HTTP segmented downloader

**Files:**
- Create: `internal/httpdl/httpdl.go`, `internal/httpdl/segments.go`, `internal/httpdl/httpdl_test.go`, `internal/httpx/client.go`

**Interfaces:**
- Consumes: engine types, state, limiter.
- Produces: `httpdl.Download(ctx context.Context, t *engine.Task, client *http.Client) (*engine.Result, error)`; `httpx.NewClient(t *engine.Task) *http.Client`.

- [x] **Step 1: Write failing `httptest` tests** for: 16-segment byte-identical download, fewer segments for small files, no-Range fallback, 404 error, redirect, custom headers/cookie/basic auth received, `Content-Disposition` name, cancel→state saved→resume→byte-identical, 200-during-segmented fallback.
- [x] **Step 2: Run tests; expect compilation failure.**
- [x] **Step 3: Implement probe, segmentation, worker pool, retries, checkpointing, single-stream fallback, limiter, progress.**
- [x] **Step 4: Run tests until green; add race run `go test -race ./internal/httpdl`.**
- [x] **Step 5: Commit `feat: segmented HTTP downloader with resume`.**

### Task 3: FTP/FTPS client and downloader

**Files:**
- Create: `internal/ftp/client.go`, `internal/ftp/reply.go`, `internal/ftp/dl.go`, `internal/ftp/reply_test.go`, `internal/ftp/client_test.go`, `internal/ftp/dl_test.go`

**Interfaces:**
- Consumes: engine types/state/limiter.
- Produces: `ftp.Download(ctx context.Context, t *engine.Task, rawURL *url.URL) (*engine.Result, error)`; internal `Session` with `Login`, `Size`, `ModTime`, `OpenRanged(ctx, path string, offset int64) (io.ReadCloser, error)`, `Close`.

- [x] **Step 1: Write failing parser tests** for single/multi-line replies, PASV/EPSV parsing, FTP URL parsing (credentials, escaping, defaults).
- [x] **Step 2: Write an in-process scripted FTP test server** (USER/PASS/TYPE/PASV/SIZE/MDTM/REST/RETR/QUIT) and failing tests for single and 4-connection segmented downloads + resume.
- [x] **Step 3: Implement minimal client and downloader; explicit TLS path.**
- [x] **Step 4: Run `go test ./internal/ftp`; commit `feat: FTP/FTPS downloader with segmented transfers`.**

### Task 4: HLS parser and downloader

**Files:**
- Create: `internal/hls/playlist.go`, `internal/hls/playlist_test.go`, `internal/hls/hls.go`, `internal/hls/hls_test.go`, `internal/segdl/segdl.go`, `internal/segdl/segdl_test.go`

**Interfaces:**
- Consumes: engine types, httpx client.
- Produces: `hls.Parse(playlistURL string, data []byte) (*Playlist, error)`; `hls.Download(ctx context.Context, t *engine.Task, client *http.Client) (*engine.Result, error)`; `segdl.Run(ctx, jobs, partsDir, conns, progress) ([]string, error)`, `segdl.Concat(files []string, dest string) error`.

- [x] **Step 1: Write failing parser tests** for master variant selection, quoted attributes, key/map/byterange, relative URI resolution, live rejection.
- [x] **Step 2: Write failing `httptest` downloader tests**: 10-segment VOD → exact concatenation, AES-128 decryption, byterange requests, best/worst quality, resume after partial parts.
- [x] **Step 3: Implement parser, segdl pipeline, AES decryption, concatenation, manifest resume.**
- [x] **Step 4: Run `go test ./internal/hls ./internal/segdl`; commit `feat: HLS VOD downloader`.**

### Task 5: DASH parser and downloader

**Files:**
- Create: `internal/dash/mpd.go`, `internal/dash/mpd_test.go`, `internal/dash/dash.go`, `internal/dash/dash_test.go`

**Interfaces:**
- Consumes: engine types, httpx, segdl.
- Produces: `dash.Parse(mpdURL string, data []byte) (*MPD, error)`; `dash.PickRepresentations(*MPD) (video, audio *Representation, err error)`; `dash.Download(ctx, t, client) (*engine.Result, error)`.

- [x] **Step 1: Write failing MPD tests**: SegmentTemplate number/time expansion with `%05d`, SegmentTimeline with `r`, SegmentList, BaseURL inheritance, DRM detection, dynamic rejection, best video+audio selection.
- [x] **Step 2: Write failing integration test** with `httptest` serving init + media segments; assert muxed single-file output and separate audio/video naming when ffmpeg is absent.
- [x] **Step 3: Implement parser, URL resolution, segment planning, download, optional ffmpeg mux.**
- [x] **Step 4: Run `go test ./internal/dash`; commit `feat: DASH static downloader`.**

### Task 6: BitTorrent support

**Files:**
- Create: `internal/bt/bt.go`, `internal/bt/bt_test.go`; Modify: `go.mod`, `go.sum`

**Interfaces:**
- Produces: `bt.Download(ctx context.Context, t *engine.Task) (*engine.Result, error)`.

- [x] **Step 1: Add `github.com/anacrolix/torrent`; verify API with `go doc`.**
- [x] **Step 2: Write failing tests** for input classification (magnet vs `.torrent` path) and, guarded by `testing.Short`, a local seeder/leecher round trip.
- [x] **Step 3: Implement client config (data dir, port, seed), metadata wait, progress polling, completion.**
- [x] **Step 4: Run `go test ./internal/bt`; commit `feat: BitTorrent and magnet downloads`.**

### Task 7: Dispatcher, CLI run loop, progress rendering

**Files:**
- Modify: `internal/engine/engine.go` (dispatch), `internal/cli/cli.go`, `cmd/downloadx/main.go`
- Create: `internal/progress/progress.go`, `internal/progress/progress_test.go`

**Interfaces:**
- Consumes: all protocol downloaders.
- Produces: working `downloadx` end-to-end.

- [x] **Step 1: Write failing tests** for dispatch (magnet→bt, ftp→ftp, `.m3u8`→hls, `--type` override), progress JSON line schema, TTY/plain selection.
- [x] **Step 2: Implement dispatch, sequential batch run, reporter lifecycle, interrupted exit code 130, per-task summary.**
- [x] **Step 3: `go test ./... && go build ./cmd/downloadx`; commit `feat: wire CLI, dispatcher and progress output`.**

### Task 8: Docs, manual releases, end-to-end verification

**Files:**
- Modify: `README.md`, `PLAN.md`
- Create: `RELEASE.md`

- [x] **Step 1: Write README** (Windows install, MSI, one-line scripts, usage table, protocol matrix, architecture, limitations).
- [x] **Step 2: Document the manual release process** in `RELEASE.md`: build the MSI, test, commit, tag, `gh release create`. No GitHub Actions.
- [x] **Step 3: Verify against live endpoints** (`https://speed.cloudflare.com/__down?bytes=...`) plus `go test ./...`, `go vet ./...`, `go build`.
- [x] **Step 4: Commit and push.**

### Task 9: Native GUI with MyGo + Lobe UI

**Files:**
- Create: `cmd/downloadx-gui/main.go`, `internal/desktop/manager.go`, `internal/desktop/view.go`, `internal/desktop/manager_test.go`, `internal/desktop/view_test.go`, `internal/desktop/preview_test.go`, `mygo.json`, `docs/gui-preview-dark.png`, `docs/gui-preview-light.png`

**Interfaces:**
- `desktop.New() *App`; `App.Attach(*mygo.Window)`; `App.View(*ui.Context)`; `App.Stop()`
- `desktop.Manager` with `Add(Request) (*Job, error)`, `Cancel(id)`, `Retry(id) error`, `Remove(id)`, `ClearFinished()`, `Snapshot() []Job`, `SetOnUpdate(func())`
- `Request{URL, Dir string; Connections int; Type engine.Type}`

- [x] **Step 1: Add MyGo and Lobe MyGo; inspect component APIs and tokens.**
- [x] **Step 2: Implement the two-worker queue manager on top of `internal/runner`, with progress, speed, cancel, retry and cleanup.**
- [x] **Step 3: Build the Lobe-style window: hidden title bar, hero, composer (URL, folder picker, connections, protocol), queue cards, empty state, toasts.**
- [x] **Step 4: Add a headless `ui.Tester` build test and a preview renderer that writes `docs/gui-preview-{dark,light}.png` from fake jobs.**
- [x] **Step 5: `go test ./...`, `go vet ./...`, build both commands; commit.**

### Task 10: Windows MSI and one-line installers

**Files:**
- Create: `installer/DownloadX.wxs`, `installer/License.rtf`, `assets/downloadx.ico`, `scripts/build-msi.ps1`, `install.ps1`, `install.sh`, `RELEASE.md`
- Remove: `.github/workflows/`

**Interfaces:**
- `scripts/build-msi.ps1 -Version X.Y.Z` → `dist\DownloadX-windows-amd64.msi` and `.sha256`.
- Stable asset name `DownloadX-windows-amd64.msi`, so `releases/latest/download/DownloadX-windows-amd64.msi` always resolves.
- MSI: per-machine, fixed `C:\Program Files\DownloadX`, CLI + GUI + native host, Start-menu shortcut, ARP entry, icon, Chrome/Edge native-messaging registration and force-install policies; WiX `WixUI_Minimal` wizard.

- [x] **Step 1: Write the WiX source** (per-machine Program Files install, CLI + GUI + host components, HKLM keypaths, Common Programs shortcut, license and icon) and build it with `candle`/`light`.
- [x] **Step 2: Add `scripts/build-msi.ps1`** producing the stable MSI name and SHA256; verify with an administrative install (`msiexec /a`) that all executables are inside.
- [x] **Step 3: Add `install.ps1` and `install.sh`** that download the latest MSI and run it with UAC elevation; sh works in Git Bash/MSYS/Cygwin/WSL and refuses non-Windows hosts.
- [x] **Step 4: Delete GitHub workflows and disable repository Actions; document manual tags/releases in `RELEASE.md`, `AGENTS.md` and README.**
- [x] **Step 5: Update `go vet`/`go test`/builds and commit; tag and publish the release manually.**

### Task 11: Chrome/Edge extension with store-free auto-update

**Files:**
- Create: `extensions/downloadx/` (MV3 manifest, service worker, popup, icons), `extensions/id.txt`, `extensions/public-key.txt`, `extensions/README.md`, `cmd/downloadx-host/`, `internal/inbox/`, `scripts/build-extension.ps1`, `docs/extension/`, `installer/com.neko233.downloadx.json`
- Modify: `internal/desktop/manager.go`, `internal/desktop/view.go`, `installer/DownloadX.wxs`, `scripts/build-msi.ps1`, `README.md`, `RELEASE.md`, `AGENTS.md`

**Interfaces:**
- Fixed extension ID `pdlpjmbjgipkbjekncinhchgbahpgbng`, derived from the RSA public key; signing key `extensions/downloadx.pem` stays untracked.
- Native messaging host `com.neko233.downloadx` → `C:\Program Files\DownloadX\downloadx-host.exe` → `%LOCALAPPDATA%\DownloadX\inbox`.
- `downloadx-host` speaks the Chrome native-messaging length-prefixed JSON protocol; actions `ping`, `open`, `download`.
- Auto-update: `ExtensionInstallForcelist` policy → jsDelivr `docs/extension/updates.xml` → `docs/extension/downloadx.crx` with `application/x-chrome-extension`.

- [x] **Step 1: Generate the RSA key, fixed extension ID, public key, PNG icons and the MV3 extension** that cancels browser downloads and hands URLs to native messaging.
- [x] **Step 2: Implement the native host and inbox queue, launch the GUI detached, and make the desktop manager watch the inbox and enqueue with 16 connections.**
- [x] **Step 3: Add the host, native-messaging registration and Chrome/Edge force-install policies to the MSI; lock the install directory so the native manifest path stays valid.**
- [x] **Step 4: Add `scripts/build-extension.ps1`** (Chrome pack, CRX/SHA256, refreshed `updates.xml` and `docs/extension/`) and verify the native host round-trip.
- [x] **Step 5: Update docs for one-line install, browser restart, self-hosted updates, manual release; run tests, build the MSI and validate it.**
