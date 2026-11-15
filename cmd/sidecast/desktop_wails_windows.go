//go:build windows && wails

package main

import (
	"context"
	"net/http"
	"os"
	"path/filepath"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
	"github.com/wailsapp/wails/v2/pkg/runtime"
	"sidecast/internal/channels"
	"sidecast/internal/desktop"
	"sidecast/internal/media"
	"sidecast/web"
)

func runDesktop(ctx context.Context, _ string, stop context.CancelFunc, _ bool, handler http.Handler, store *channels.Store, hub *media.Hub, ready func() error) error {
	capture := newCaptureController(store, hub)
	defer capture.Stop()
	profile := os.Getenv("SIDECAST_WEBVIEW_DATA_DIR")
	if profile == "" {
		config, err := os.UserConfigDir()
		if err != nil {
			return err
		}
		profile = filepath.Join(config, "Sidecast", "WebView2")
	}
	profile, err := filepath.Abs(profile)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(profile, 0700); err != nil {
		return err
	}
	readyResult := make(chan error, 1)
	err = wails.Run(&options.App{
		Title:     "Sidecast",
		Width:     1360,
		Height:    860,
		MinWidth:  980,
		MinHeight: 640,
		AssetServer: &assetserver.Options{
			Assets:  web.Assets(),
			Handler: desktop.EmbeddedWindowHandler(handler),
		},
		Bind:    []interface{}{capture},
		Windows: &windows.Options{Theme: windows.Dark, WebviewUserDataPath: profile},
		OnStartup: func(windowCtx context.Context) {
			go func() {
				result := ready()
				readyResult <- result
				if result != nil {
					stop()
				}
				<-ctx.Done()
				runtime.Quit(windowCtx)
			}()
		},
		OnShutdown: func(context.Context) { stop() },
	})
	if err != nil {
		stop()
		return err
	}
	select {
	case result := <-readyResult:
		return result
	default:
		return nil
	}
}
