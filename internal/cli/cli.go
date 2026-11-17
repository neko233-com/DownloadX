// Package cli parses command-line arguments and drives downloads.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/neko233-com/DownloadX/internal/progress"
	"github.com/neko233-com/DownloadX/internal/runner"
	"github.com/neko233-com/DownloadX/internal/version"
)

// ShortUsage is one line of usage help.
func ShortUsage() string {
	return "usage: downloadx [flags] <url|magnet:...|file.torrent>..."
}

// Usage returns the full help text.
func Usage() string {
	return `downloadx — fast, multi-connection download manager

` + ShortUsage() + `

Protocols: HTTP(S), FTP/FTPS, HLS (VOD), DASH (static), BitTorrent/magnet.

Commands:
  downloadx gui            launch the native desktop app (downloadx-gui)
  downloadx version        print version
  downloadx help           print this help

Flags:
  -o, --output PATH        output file (single source) or directory
  -d, --dir DIR            destination directory (default ".")
  -x, --connections N      parallel connections per download (default 16, max 128)
  -i, --input FILE         read one source per line ("#" comments allowed)
  -H, --header "K: V"      extra HTTP header (repeatable)
      --cookie VALUE       Cookie header
      --referer URL        Referer header
      --user-agent STR     User-Agent (default "DownloadX/<version>")
  -u, --user USER:PASS     HTTP basic auth / FTP login
      --proxy URL          http, https or socks5 proxy
      --retry N            retries per segment (default 3)
      --timeout DUR        connection timeout (default 30s)
      --limit RATE         speed limit per task, e.g. 5M, 800K
      --type TYPE          auto|http|ftp|hls|dash|bt (default auto)
      --no-resume          discard resume state and start over
      --insecure           skip TLS certificate verification
      --json               JSON-lines progress on stdout
  -q, --quiet              no progress display
      --hls-quality Q      best|worst|variant index (default best)
      --bt-port PORT       BitTorrent listen port (default 0 = random)
      --seed               keep seeding after a torrent completes
      --ffmpeg PATH        ffmpeg for DASH muxing (default "ffmpeg")
  -v, --version            print version
  -h, --help               print this help
`
}

// Main runs the CLI and returns a process exit code.
func Main(args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 && (args[0] == "gui" || args[0] == "desktop") {
		if err := launchGUI(args[1:], stdout, stderr); err != nil {
			fmt.Fprintf(stderr, "downloadx: %v\n", err)
			return 1
		}
		return 0
	}

	o, err := Parse(args)
	if err != nil {
		fmt.Fprintf(stderr, "downloadx: %v\n\n%s\n", err, ShortUsage())
		return 2
	}
	if o.ShowHelp {
		fmt.Fprint(stdout, Usage())
		return 0
	}
	if o.ShowVersion {
		fmt.Fprintln(stdout, version.String())
		return 0
	}
	if len(o.Sources) == 1 {
		switch strings.ToLower(o.Sources[0]) {
		case "help":
			fmt.Fprint(stdout, Usage())
			return 0
		case "version":
			fmt.Fprintln(stdout, version.Full())
			return 0
		}
	}
	if len(o.Sources) == 0 {
		fmt.Fprintf(stderr, "downloadx: no source given\n\n%s\n", ShortUsage())
		return 2
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	exit := 0
	for i, src := range o.Sources {
		task := o.Task
		task.URL = src
		if len(o.Sources) > 1 && task.Dest != "" {
			task.Dir = task.Dest
			task.Dest = ""
		}

		mode := progress.ModeAuto
		out := stderr
		switch {
		case o.Quiet:
			mode = progress.ModeQuiet
		case o.JSON:
			mode = progress.ModeJSON
			out = stdout
		}
		rep := progress.New(out, mode)
		rep.SetLabel(src)
		task.Progress = rep.Update

		if !o.Quiet && !o.JSON && len(o.Sources) > 1 {
			fmt.Fprintf(stderr, "downloadx: [%d/%d] %s\n", i+1, len(o.Sources), src)
		}

		start := time.Now()
		res, err := runner.Run(ctx, &task)
		rep.Done(res, err)
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, context.Canceled) {
				fmt.Fprintln(stderr, "downloadx: interrupted; rerun the same command to resume")
				return 130
			}
			fmt.Fprintf(stderr, "downloadx: %v\n", err)
			exit = 1
			continue
		}
		if !o.Quiet && !o.JSON {
			elapsed := time.Since(start).Round(time.Millisecond)
			fmt.Fprintf(stderr, "  %s  %s  %s\n", res.Path, humanBytes(res.Bytes), elapsed)
			for _, f := range res.Files {
				if f != res.Path {
					fmt.Fprintf(stderr, "  related: %s\n", f)
				}
			}
		}
	}
	return exit
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGT"[exp])
}

// launchGUI finds the desktop binary next to this executable or on PATH.
func launchGUI(args []string, stdout, stderr io.Writer) error {
	name := "downloadx-gui"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	path := ""
	if exe, err := os.Executable(); err == nil {
		candidate := filepath.Join(filepath.Dir(exe), name)
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			path = candidate
		}
	}
	if path == "" {
		if found, err := exec.LookPath(name); err == nil {
			path = found
		}
	}
	if path == "" {
		return fmt.Errorf("downloadx-gui is not installed; run: go install github.com/neko233-com/DownloadX/cmd/downloadx-gui@latest")
	}

	cmd := exec.Command(path, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	return cmd.Run()
}
