//go:build !windows || !wails

package main

import (
	"context"
	"net/http"

	"sidecast/internal/channels"
	"sidecast/internal/desktop"
	"sidecast/internal/media"
)

func runDesktop(ctx context.Context, studio string, stop context.CancelFunc, openOnStart bool, _ http.Handler, _ *channels.Store, _ *media.Hub, ready func() error) error {
	return desktop.Run(ctx, studio, stop, openOnStart, ready)
}
