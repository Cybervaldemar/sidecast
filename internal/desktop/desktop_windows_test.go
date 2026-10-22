package desktop

import (
	"context"
	"os"
	"testing"
	"time"
	"unsafe"
)

func TestWindowsLayouts(t *testing.T) {
	// A wrong SDK layout corrupts memory across the syscall boundary.
	if unsafe.Sizeof(windowClass{}) != 80 || unsafe.Sizeof(message{}) != 48 || unsafe.Sizeof(notificationIcon{}) != 976 {
		t.Fatalf("unexpected Windows ABI sizes: class=%d message=%d icon=%d", unsafe.Sizeof(windowClass{}), unsafe.Sizeof(message{}), unsafe.Sizeof(notificationIcon{}))
	}
}

func TestApplicationIcon(t *testing.T) {
	icon, err := makeIcon()
	if err != nil {
		t.Fatal(err)
	}
	destroyIcon.Call(icon)
}

func TestTrayExit(t *testing.T) {
	if os.Getenv("SIDECAST_TEST_TRAY") != "1" && os.Getenv("LANPLAY_TEST_TRAY") != "1" {
		t.Skip("set SIDECAST_TEST_TRAY=1 in an interactive Windows session")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	findWindow := user32.NewProc("FindWindowW")
	iconRect := shell32.NewProc("Shell_NotifyIconGetRect")
	checked := make(chan error, 1)
	opened := make(chan string, 1)
	go func() {
		select {
		case address := <-opened:
			if address != "http://localhost:8080/studio" {
				t.Errorf("opened %q instead of the studio", address)
			}
		case <-ctx.Done():
			checked <- ctx.Err()
			return
		}
		for ctx.Err() == nil {
			window, _, _ := findWindow.Call(uintptr(unsafe.Pointer(wide("Sidecast.Tray"))), 0)
			if window != 0 {
				identifier := struct {
					Size   uint32
					Window uintptr
					ID     uint32
					GUID   [16]byte
				}{Window: window, ID: 1}
				identifier.Size = uint32(unsafe.Sizeof(identifier))
				var rect [4]int32
				result, _, _ := iconRect.Call(uintptr(unsafe.Pointer(&identifier)), uintptr(unsafe.Pointer(&rect)))
				if result == 0 {
					postMessage.Call(window, wmCommand, menuExit, 0)
					checked <- nil
					return
				}
			}
			time.Sleep(20 * time.Millisecond)
		}
		checked <- ctx.Err()
	}()
	if err := run(ctx, "http://localhost:8080/studio", cancel, func(address string) { opened <- address }); err != nil {
		t.Fatal(err)
	}
	if err := <-checked; err != nil {
		t.Fatalf("tray icon was not registered: %v", err)
	}
	if ctx.Err() != context.Canceled {
		t.Fatalf("Exit did not cancel the server: %v", ctx.Err())
	}
	window, _, _ := findWindow.Call(uintptr(unsafe.Pointer(wide("Sidecast.Tray"))), 0)
	if window != 0 {
		t.Fatal("tray window survived shutdown")
	}
}
