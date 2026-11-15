//go:build windows && wails

package main

import (
	"bytes"
	"encoding/base64"
	"image/png"
	"os"
	"strings"
	"testing"
)

func TestExecutableIcon(t *testing.T) {
	path, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dataURL := sourceIcon(path)
	const prefix = "data:image/png;base64,"
	if !strings.HasPrefix(dataURL, prefix) {
		t.Fatal("Windows did not provide an executable icon")
	}
	data, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(dataURL, prefix))
	if err != nil {
		t.Fatal(err)
	}
	icon, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if icon.Bounds().Dx() != sourceIconSize || icon.Bounds().Dy() != sourceIconSize {
		t.Fatalf("icon size: %v", icon.Bounds())
	}
	var visible bool
	for y := 0; y < sourceIconSize && !visible; y++ {
		for x := 0; x < sourceIconSize; x++ {
			_, _, _, alpha := icon.At(x, y).RGBA()
			if alpha != 0 {
				visible = true
				break
			}
		}
	}
	if !visible {
		t.Fatal("executable icon is fully transparent")
	}
}
