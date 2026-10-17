//go:build !windows

package nativecapture

import (
	"context"
	"net/url"
	"testing"
)

func TestNonWindowsUsesBrowserFallbackWithoutNativeDependencies(t *testing.T) {
	server, _ := url.Parse("http://127.0.0.1:8080")
	c := New(context.Background(), server, "/does-not-exist/ffmpeg", "/does-not-exist/audio")
	defer c.Close()
	if caps := c.Capabilities(); caps.Available || caps.Audio || caps.ApplicationAudio || caps.Reason == "" {
		t.Fatalf("native capture incorrectly offered: %+v", caps)
	}
	if _, err := c.Sources(); err == nil {
		t.Fatal("unsupported native source enumeration accepted")
	}
}
