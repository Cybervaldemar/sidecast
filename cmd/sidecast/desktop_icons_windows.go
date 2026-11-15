//go:build windows && wails

package main

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/png"
	"unsafe"

	"github.com/go-mswin/win32"
	"golang.org/x/sys/windows"
)

const sourceIconSize = 32

var (
	shell32GetFileInfo = windows.NewLazySystemDLL("shell32.dll").NewProc("SHGetFileInfoW")
	user32DestroyIcon  = windows.NewLazySystemDLL("user32.dll").NewProc("DestroyIcon")
	user32DrawIconEx   = windows.NewLazySystemDLL("user32.dll").NewProc("DrawIconEx")
	gdi32DIBSection    = windows.NewLazySystemDLL("gdi32.dll").NewProc("CreateDIBSection")
)

type shellFileInfo struct {
	icon        uintptr
	index       int32
	attributes  uint32
	displayName [260]uint16
	typeName    [80]uint16
}

// sourceIcon returns a small PNG data URL so the WebView can display the
// owning application's Windows icon without reading local file paths.
func sourceIcon(exePath string) string {
	if exePath == "" {
		return ""
	}
	path, err := windows.UTF16PtrFromString(exePath)
	if err != nil {
		return ""
	}
	var info shellFileInfo
	const shgfiIcon = 0x100
	if result, _, _ := shell32GetFileInfo.Call(uintptr(unsafe.Pointer(path)), 0, uintptr(unsafe.Pointer(&info)), unsafe.Sizeof(info), shgfiIcon); result == 0 || info.icon == 0 {
		return ""
	}
	defer user32DestroyIcon.Call(info.icon)

	dc, err := win32.CreateCompatibleDC(0)
	if err != nil {
		return ""
	}
	defer win32.DeleteDC(dc)
	bitmapInfo := struct {
		header win32.BitmapInfoHeader
		colors [3]uint32
	}{header: win32.BitmapInfoHeader{BiSize: uint32(unsafe.Sizeof(win32.BitmapInfoHeader{})), BiWidth: sourceIconSize, BiHeight: -sourceIconSize, BiPlanes: 1, BiBitCount: 32}}
	var bits unsafe.Pointer
	bitmap, _, _ := gdi32DIBSection.Call(uintptr(dc), uintptr(unsafe.Pointer(&bitmapInfo)), 0, uintptr(unsafe.Pointer(&bits)), 0, 0)
	if bitmap == 0 || bits == nil {
		return ""
	}
	defer win32.DeleteObject(win32.HANDLE(bitmap))
	old := win32.SelectObject(dc, win32.HANDLE(bitmap))
	if old == 0 {
		return ""
	}
	defer win32.SelectObject(dc, old)
	// DI_NORMAL draws both the image and its transparency mask.
	if ok, _, _ := user32DrawIconEx.Call(uintptr(dc), 0, 0, info.icon, sourceIconSize, sourceIconSize, 0, 0, 3); ok == 0 {
		return ""
	}

	pixels := unsafe.Slice((*byte)(bits), sourceIconSize*sourceIconSize*4)
	icon := image.NewRGBA(image.Rect(0, 0, sourceIconSize, sourceIconSize))
	var hasAlpha bool
	for i := 3; i < len(pixels); i += 4 {
		if pixels[i] != 0 {
			hasAlpha = true
			break
		}
	}
	for i := 0; i < len(pixels); i += 4 {
		icon.Pix[i], icon.Pix[i+1], icon.Pix[i+2] = pixels[i+2], pixels[i+1], pixels[i]
		if hasAlpha {
			icon.Pix[i+3] = pixels[i+3]
		} else if pixels[i]|pixels[i+1]|pixels[i+2] != 0 {
			icon.Pix[i+3] = 255
		}
	}
	var output bytes.Buffer
	if err := png.Encode(&output, icon); err != nil {
		return ""
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(output.Bytes())
}
