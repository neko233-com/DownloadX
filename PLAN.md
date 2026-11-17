# DownloadX Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `downloadx` — an NDM-style, multi-connection download manager CLI in pure Go, supporting HTTP(S), FTP/FTPS, HLS, DASH and BitTorrent/magnet on Windows, macOS and Linux.

**Architecture:** A small `internal/engine` core owns task configuration, destination naming, resume state and protocol dispatch. Each protocol lives in its own package (`httpdl`, `ftp`, `hls`, `dash`, `bt`) and reports progress through one callback. `internal/cli` parses arguments and renders text/JSON progress. HTTP and FTP share the segmented-download resume model; HLS/DASH share a part-file pipeline in `internal/segdl`; BitTorrent is adapted through one narrow wrapper.

**Tech Stack:** Go 1.27.0 standard library. BitTorrent uses `github.com/anacrolix/torrent` (pure Go, no cgo); everything else is stdlib. Optional `ffmpeg` is used only to mux separate DASH audio/video when present.

**Spec:** this document — the Design section below is the spec.

## Design

### Product

- Public repo `neko233-com/DownloadX`, MIT. Binary and command name `downloadx` (lowercase).
- One static binary, no runtime dependencies, no cgo, no telemetry.
- Default 16 parallel connections per download (`-x` overrides), NDM-style segmented transfers with resume.
- Supported sources: `http://`, `https://` (including Range-less fallback), `ftp://`, `ftps://` (explicit TLS), HLS `.m3u8` (VOD), DASH `.mpd` (static), `magnet:` links and `.torrent` files.
- Auto-detection by URL scheme, path extension and `Content-Type`; `--type` forces a protocol.
- Resume works across process restarts. State lives beside the output file and is deleted on success.
- Never overwrite an existing file silently; use resume when valid, otherwise pick `name (1).ext`.
- Human output goes to stderr; `--json` emits JSON-lines progress on stdout. Exit codes: `0` success, `1` failure, `2` usage, `130` interrupted.

### CLI

```
downloadx [flags] <url|magnet|file.torrent>...
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
cmd/downloadx/main.go          entrypoint, version wiring, signal handling
internal/cli/                  argument parsing, run loop, progress rendering
internal/engine/               Task/Progress/Result, dispatch, naming, state, limiter
internal/httpdl/               HTTP(S) segmented downloader
internal/ftp/                  FTP/FTPS client + segmented downloader
internal/hls/                  HLS playlist parser + downloader
internal/dash/                 MPD parser + downloader
internal/segdl/                concurrent part fetch/rename/concat pipeline
internal/bt/                   anacrolix/torrent adapter
internal/version/              version variables (ldflags)
.github/workflows/ci.yml       vet + test + build (linux/windows/macos)
.github/workflows/release.yml  tag → cross-platform binaries + GitHub release
```

## Global Constraints

- Module `github.com/neko233-com/DownloadX`; repo `DownloadX` (case-sensitive); command `downloadx`.
- `go 1.27.0`; `CGO_ENABLED=0`; cross-platform Windows/macOS/Linux (amd64 + arm64 for darwin/linux, amd64 for windows).
- Stdlib only except `github.com/anacrolix/torrent` for BitTorrent. No ffmpeg hard dependency.
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

### Task 1: Module, CLI skeleton, naming, state, limiter, CI

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

- [ ] **Step 1: Write failing parser tests** covering defaults (`Connections == 16`, type auto, retries 3, timeout 30s), flags after URLs, repeated `-H`, `--` terminator, unknown flag error, `-x 0` rejected, input file lines.
- [ ] **Step 2: Run `go test ./internal/cli`; expect failures.**
- [ ] **Step 3: Implement `cli.Parse`** (hand-written parser, help text), `engine` types, naming helpers, state JSON, limiter.
- [ ] **Step 4: Write failing naming/state/limiter tests**: traversal names (`..\..\evil` → inside dir), reserved Windows names, disposition `filename*`, unique collision suffix, state round-trip and mismatch rejection, limiter waits ≈ n/rate.
- [ ] **Step 5: Implement helpers until tests pass.**
- [ ] **Step 6: Create `cmd/downloadx/main.go`** with signal-aware context, `version`, exit codes; CI workflow running vet/test/build.
- [ ] **Step 7: `go vet ./... && go test ./... && go build ./cmd/downloadx`; commit `feat: bootstrap CLI, engine skeleton, naming and resume state`.**

### Task 2: HTTP segmented downloader

**Files:**
- Create: `internal/httpdl/httpdl.go`, `internal/httpdl/segments.go`, `internal/httpdl/httpdl_test.go`, `internal/httpx/client.go`

**Interfaces:**
- Consumes: engine types, state, limiter.
- Produces: `httpdl.Download(ctx context.Context, t *engine.Task, client *http.Client) (*engine.Result, error)`; `httpx.NewClient(t *engine.Task) *http.Client`.

- [ ] **Step 1: Write failing `httptest` tests** for: 16-segment byte-identical download, fewer segments for small files, no-Range fallback, 404 error, redirect, custom headers/cookie/basic auth received, `Content-Disposition` name, cancel→state saved→resume→byte-identical, 200-during-segmented fallback.
- [ ] **Step 2: Run tests; expect compilation failure.**
- [ ] **Step 3: Implement probe, segmentation, worker pool, retries, checkpointing, single-stream fallback, limiter, progress.**
- [ ] **Step 4: Run tests until green; add race run `go test -race ./internal/httpdl`.**
- [ ] **Step 5: Commit `feat: segmented HTTP downloader with resume`.**

### Task 3: FTP/FTPS client and downloader

**Files:**
- Create: `internal/ftp/client.go`, `internal/ftp/reply.go`, `internal/ftp/dl.go`, `internal/ftp/reply_test.go`, `internal/ftp/client_test.go`, `internal/ftp/dl_test.go`

**Interfaces:**
- Consumes: engine types/state/limiter.
- Produces: `ftp.Download(ctx context.Context, t *engine.Task, rawURL *url.URL) (*engine.Result, error)`; internal `Session` with `Login`, `Size`, `ModTime`, `OpenRanged(ctx, path string, offset int64) (io.ReadCloser, error)`, `Close`.

- [ ] **Step 1: Write failing parser tests** for single/multi-line replies, PASV/EPSV parsing, FTP URL parsing (credentials, escaping, defaults).
- [ ] **Step 2: Write an in-process scripted FTP test server** (USER/PASS/TYPE/PASV/SIZE/MDTM/REST/RETR/QUIT) and failing tests for single and 4-connection segmented downloads + resume.
- [ ] **Step 3: Implement minimal client and downloader; explicit TLS path.**
- [ ] **Step 4: Run `go test ./internal/ftp`; commit `feat: FTP/FTPS downloader with segmented transfers`.**

### Task 4: HLS parser and downloader

**Files:**
- Create: `internal/hls/playlist.go`, `internal/hls/playlist_test.go`, `internal/hls/hls.go`, `internal/hls/hls_test.go`, `internal/segdl/segdl.go`, `internal/segdl/segdl_test.go`

**Interfaces:**
- Consumes: engine types, httpx client.
- Produces: `hls.Parse(playlistURL string, data []byte) (*Playlist, error)`; `hls.Download(ctx context.Context, t *engine.Task, client *http.Client) (*engine.Result, error)`; `segdl.Run(ctx, jobs, partsDir, conns, progress) ([]string, error)`, `segdl.Concat(files []string, dest string) error`.

- [ ] **Step 1: Write failing parser tests** for master variant selection, quoted attributes, key/map/byterange, relative URI resolution, live rejection.
- [ ] **Step 2: Write failing `httptest` downloader tests**: 10-segment VOD → exact concatenation, AES-128 decryption, byterange requests, best/worst quality, resume after partial parts.
- [ ] **Step 3: Implement parser, segdl pipeline, AES decryption, concatenation, manifest resume.**
- [ ] **Step 4: Run `go test ./internal/hls ./internal/segdl`; commit `feat: HLS VOD downloader`.**

### Task 5: DASH parser and downloader

**Files:**
- Create: `internal/dash/mpd.go`, `internal/dash/mpd_test.go`, `internal/dash/dash.go`, `internal/dash/dash_test.go`

**Interfaces:**
- Consumes: engine types, httpx, segdl.
- Produces: `dash.Parse(mpdURL string, data []byte) (*MPD, error)`; `dash.PickRepresentations(*MPD) (video, audio *Representation, err error)`; `dash.Download(ctx, t, client) (*engine.Result, error)`.

- [ ] **Step 1: Write failing MPD tests**: SegmentTemplate number/time expansion with `%05d`, SegmentTimeline with `r`, SegmentList, BaseURL inheritance, DRM detection, dynamic rejection, best video+audio selection.
- [ ] **Step 2: Write failing integration test** with `httptest` serving init + media segments; assert muxed single-file output and separate audio/video naming when ffmpeg is absent.
- [ ] **Step 3: Implement parser, URL resolution, segment planning, download, optional ffmpeg mux.**
- [ ] **Step 4: Run `go test ./internal/dash`; commit `feat: DASH static downloader`.**

### Task 6: BitTorrent support

**Files:**
- Create: `internal/bt/bt.go`, `internal/bt/bt_test.go`; Modify: `go.mod`, `go.sum`

**Interfaces:**
- Produces: `bt.Download(ctx context.Context, t *engine.Task) (*engine.Result, error)`.

- [ ] **Step 1: Add `github.com/anacrolix/torrent`; verify API with `go doc`.**
- [ ] **Step 2: Write failing tests** for input classification (magnet vs `.torrent` path) and, guarded by `testing.Short`, a local seeder/leecher round trip.
- [ ] **Step 3: Implement client config (data dir, port, seed), metadata wait, progress polling, completion.**
- [ ] **Step 4: Run `go test ./internal/bt`; commit `feat: BitTorrent and magnet downloads`.**

### Task 7: Dispatcher, CLI run loop, progress rendering

**Files:**
- Modify: `internal/engine/engine.go` (dispatch), `internal/cli/cli.go`, `cmd/downloadx/main.go`
- Create: `internal/progress/progress.go`, `internal/progress/progress_test.go`

**Interfaces:**
- Consumes: all protocol downloaders.
- Produces: working `downloadx` end-to-end.

- [ ] **Step 1: Write failing tests** for dispatch (magnet→bt, ftp→ftp, `.m3u8`→hls, `--type` override), progress JSON line schema, TTY/plain selection.
- [ ] **Step 2: Implement dispatch, sequential batch run, reporter lifecycle, interrupted exit code 130, per-task summary.**
- [ ] **Step 3: `go test ./... && go build ./cmd/downloadx`; commit `feat: wire CLI, dispatcher and progress output`.**

### Task 8: Docs, release pipeline, end-to-end verification

**Files:**
- Modify: `README.md`, `PLAN.md`
- Create: `.github/workflows/release.yml`

- [ ] **Step 1: Write README** (install, usage table, protocol matrix, examples, architecture, limitations, roadmap).
- [ ] **Step 2: Write release workflow** building windows/amd64, linux/amd64+arm64, darwin/amd64+arm64 with `CGO_ENABLED=0` and uploading archives on `v*` tags.
- [ ] **Step 3: Verify against live endpoints** (`https://speed.cloudflare.com/__down?bytes=...`, a public HLS test stream) plus `go test ./...`, `go vet ./...`, `go build`.
- [ ] **Step 4: Tick completed boxes in this plan; commit and push.**
