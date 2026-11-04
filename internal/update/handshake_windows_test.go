//go:build windows

package update

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestHelperHandshake(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "started")
	go func() {
		time.Sleep(30 * time.Millisecond)
		_ = os.WriteFile(marker, []byte("started"), 0600)
	}()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := waitForHelperReady(ctx, os.Getpid(), marker); err != nil {
		t.Fatalf("live helper acknowledgement rejected: %v", err)
	}
	missing, stop := context.WithTimeout(context.Background(), 60*time.Millisecond)
	defer stop()
	if err := waitForHelperReady(missing, os.Getpid(), marker+"-missing"); err == nil {
		t.Fatal("missing helper acknowledgement accepted")
	}
}
