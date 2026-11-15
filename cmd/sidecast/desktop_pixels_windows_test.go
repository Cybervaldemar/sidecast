//go:build windows && wails

package main

import (
	"testing"
	"time"

	"github.com/go-mswin/screencapture"
	"github.com/thesyncim/govpx"
)

func TestNativeFrameConvertsAndEncodes(t *testing.T) {
	width, height, stride := 16, 16, 16*4+8
	pixels := make([]byte, height*stride)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			i := y*stride + x*4
			pixels[i], pixels[i+1], pixels[i+2] = byte(x*10), byte(y*10), 180
		}
	}
	frame := screencapture.Frame{Pix: pixels, Width: width, Height: height, Stride: stride, At: time.Now()}
	converted := newI420(width, height)
	bgraToI420(frame, &converted)
	if converted.Y[0] == converted.Y[width*height-1] {
		t.Fatal("conversion ignored pixel rows")
	}
	encoder, err := govpx.NewVP8Encoder(govpx.EncoderOptions{Width: width, Height: height, FPS: 60, TargetBitrateKbps: 1000, Deadline: govpx.DeadlineRealtime})
	if err != nil {
		t.Fatal(err)
	}
	defer encoder.Close()
	encoded, err := encoder.EncodeInto(make([]byte, 64*1024), converted, 0, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(encoded.Data) == 0 || !encoded.KeyFrame {
		t.Fatal("first native frame did not encode as a keyframe")
	}
}
