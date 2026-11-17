// Package runner dispatches a task to the right protocol downloader.
package runner

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/neko233-com/DownloadX/internal/bt"
	"github.com/neko233-com/DownloadX/internal/dash"
	"github.com/neko233-com/DownloadX/internal/engine"
	"github.com/neko233-com/DownloadX/internal/ftp"
	"github.com/neko233-com/DownloadX/internal/hls"
	"github.com/neko233-com/DownloadX/internal/httpdl"
	"github.com/neko233-com/DownloadX/internal/httpx"
)

// Run selects a downloader for t and executes it.
func Run(ctx context.Context, t *engine.Task) (*engine.Result, error) {
	typ := t.Type
	if typ == "" {
		typ = engine.TypeAuto
	}
	if typ == engine.TypeAuto {
		detected, err := detect(ctx, t)
		if err != nil {
			return nil, err
		}
		typ = detected
	}

	switch typ {
	case engine.TypeHTTP:
		return httpdl.Download(ctx, t)
	case engine.TypeFTP:
		return ftp.Download(ctx, t)
	case engine.TypeHLS:
		return hls.Download(ctx, t)
	case engine.TypeDASH:
		return dash.Download(ctx, t)
	case engine.TypeBT:
		return bt.Download(ctx, t)
	default:
		return nil, fmt.Errorf("%w: unknown type %q", engine.ErrUnsupported, typ)
	}
}

func detect(ctx context.Context, t *engine.Task) (engine.Type, error) {
	src := strings.TrimSpace(t.URL)
	lower := strings.ToLower(src)

	if strings.HasPrefix(lower, "magnet:") {
		return engine.TypeBT, nil
	}
	if strings.HasPrefix(lower, "ftp://") || strings.HasPrefix(lower, "ftps://") {
		return engine.TypeFTP, nil
	}
	if info, err := os.Stat(src); err == nil && !info.IsDir() {
		if strings.HasSuffix(lower, ".torrent") {
			return engine.TypeBT, nil
		}
		return "", fmt.Errorf("%w: local file is not a .torrent: %s", engine.ErrUnsupported, src)
	}
	if typ := engine.DetectType(src); typ != engine.TypeAuto {
		return typ, nil
	}
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
		switch contentType := probeContentType(ctx, t); {
		case strings.Contains(contentType, "mpegurl"):
			return engine.TypeHLS, nil
		case strings.Contains(contentType, "dash+xml"):
			return engine.TypeDASH, nil
		}
		return engine.TypeHTTP, nil
	}
	return "", fmt.Errorf("%w: %s", engine.ErrUnsupported, src)
}

func probeContentType(ctx context.Context, t *engine.Task) string {
	client, err := httpx.NewClient(t)
	if err != nil {
		return ""
	}
	for _, method := range []string{http.MethodHead, http.MethodGet} {
		req, err := http.NewRequestWithContext(ctx, method, t.URL, nil)
		if err != nil {
			return ""
		}
		httpx.ApplyHeaders(req, t)
		if method == http.MethodGet {
			req.Header.Set("Range", "bytes=0-0")
		}
		resp, err := client.Do(req)
		if err != nil {
			return ""
		}
		ct := resp.Header.Get("Content-Type")
		resp.Body.Close()
		if resp.StatusCode >= 200 && resp.StatusCode < 400 {
			ct = strings.ToLower(ct)
			if i := strings.IndexByte(ct, ';'); i >= 0 {
				ct = strings.TrimSpace(ct[:i])
			}
			return ct
		}
	}
	return ""
}
