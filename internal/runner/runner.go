// Package runner dispatches a task to the right protocol downloader.
package runner

import (
	"context"
	"fmt"

	"github.com/neko233-com/DownloadX/internal/engine"
)

// Run selects a downloader for t and executes it.
func Run(ctx context.Context, t *engine.Task) (*engine.Result, error) {
	return nil, fmt.Errorf("%w: protocol dispatch not wired yet", engine.ErrUnsupported)
}
