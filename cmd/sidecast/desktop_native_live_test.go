//go:build windows && wails

package main

import (
	"context"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/go-mswin/screencapture"
	"github.com/pion/webrtc/v4"
	"sidecast/internal/channels"
	"sidecast/internal/media"
)

// Opt-in smoke test: no captured pixels or samples are written to disk or sent
// over the network. It exercises the actual Windows capture/audio devices.
func TestNativeDevicesLive(t *testing.T) {
	if os.Getenv("SIDECAST_NATIVE_LIVE") != "1" {
		t.Skip("set SIDECAST_NATIVE_LIVE=1")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	displays, err := screencapture.Displays(ctx)
	if err != nil || len(displays) == 0 {
		t.Fatalf("displays: %v (%d)", err, len(displays))
	}
	stream, err := screencapture.CaptureDisplay(ctx, displays[0], screencapture.Options{Width: 640, Height: 360, FPS: 30})
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	defer stream.Close()
	frame, err := stream.WaitFrame(ctx)
	if err != nil || !frame.Valid() {
		t.Fatalf("frame: %v", err)
	}
	ready := make(chan error, 1)
	audioCtx, stopAudio := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- runLoopback(audioCtx, ready, func([]byte) error { return nil }) }()
	if err := <-ready; err != nil {
		stopAudio()
		<-done
		t.Fatalf("audio: %v", err)
	}
	stopAudio()
	if err := <-done; err != nil {
		t.Fatalf("audio shutdown: %v", err)
	}
}

func TestNativeWindowStreamLive(t *testing.T) {
	if os.Getenv("SIDECAST_NATIVE_LIVE") != "1" {
		t.Skip("set SIDECAST_NATIVE_LIVE=1")
	}
	store, err := channels.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	token, err := store.Claim("native-smoke", "")
	if err != nil {
		t.Fatal(err)
	}
	hub := media.New(webrtc.NewAPI(), webrtc.Configuration{}, 2)
	defer hub.Close()
	controller := newCaptureController(store, hub)
	defer controller.Stop()
	sources, err := controller.Sources()
	if err != nil {
		t.Fatal(err)
	}
	var source string
	for _, candidate := range sources {
		if strings.HasPrefix(candidate.ID, "window:") && strings.HasPrefix(candidate.Name, "Sidecast ·") {
			source = candidate.ID
			break
		}
	}
	if source == "" {
		t.Skip("Sidecast test window not open")
	}
	session, err := controller.Start("native-smoke", token, source, false)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(12 * time.Second)
	for time.Now().Before(deadline) {
		status := controller.Status()
		if status.Error != "" {
			t.Fatal(status.Error)
		}
		if status.SessionID == session && status.FPS > 0 {
			time.Sleep(3 * time.Second)
			status = controller.Status()
			t.Logf("native window %dx%d, %s: %.1f encoded FPS, %.1f Mbit/s", status.Width, status.Height, status.Codec, status.FPS, float64(status.Bitrate)/1_000_000)
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("native window stream did not encode a frame")
}

func TestHardwareH264ProbeLive(t *testing.T) {
	if os.Getenv("SIDECAST_NATIVE_LIVE") != "1" {
		t.Skip("set SIDECAST_NATIVE_LIVE=1")
	}
	count, err := countHardwareH264()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Windows reports %d hardware H.264 encoders", count)
}

func TestHardwareH264OpenLive(t *testing.T) {
	if os.Getenv("SIDECAST_NATIVE_LIVE") != "1" {
		t.Skip("set SIDECAST_NATIVE_LIVE=1")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	encoder, err := openHardwareH264(1280, 720, 60, 12_000_000)
	if err != nil {
		t.Fatal(err)
	}
	encoder.Close()
}

func TestHardwareH264EncodeLive(t *testing.T) {
	if os.Getenv("SIDECAST_NATIVE_LIVE") != "1" {
		t.Skip("set SIDECAST_NATIVE_LIVE=1")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	encoder, err := openHardwareH264(640, 360, 60, 4_000_000)
	if err != nil {
		t.Fatal(err)
	}
	defer encoder.Close()
	if err := encoder.SetBitrate(2_000_000); err != nil {
		t.Logf("driver does not expose dynamic H.264 bitrate: %v", err)
	}
	if err := encoder.ForceKeyframe(); err != nil {
		t.Logf("driver does not expose H.264 keyframe request: %v", err)
	}
	nv12 := make([]byte, 640*360*3/2)
	for i := range nv12[:640*360] {
		nv12[i] = 80
	}
	for i := range nv12[640*360:] {
		nv12[640*360+i] = 128
	}
	deadline := time.Now().Add(3 * time.Second)
	var outputs int
	var forced bool
	for time.Now().Before(deadline) {
		if outputs >= 30 && !forced {
			if err := encoder.ForceKeyframe(); err == nil { forced = true }
		}
		frames, err := encoder.Poll(nv12, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		for _, frame := range frames {
			outputs++
			if outputs == 1 {
				t.Logf("first H.264 output: %d bytes, prefix %x", len(frame.Data), frame.Data[:min(16, len(frame.Data))])
			}
			if forced && outputs == 32 {
				t.Logf("H.264 after keyframe request: prefix %x", frame.Data[:min(40, len(frame.Data))])
			}
		}
		time.Sleep(time.Second / 60)
	}
	if outputs == 0 {
		t.Fatal("hardware encoder produced no H.264 frames")
	}
	t.Logf("hardware encoder produced %d frames", outputs)
}
