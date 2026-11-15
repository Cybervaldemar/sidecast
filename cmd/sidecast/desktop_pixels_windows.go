//go:build windows && wails

package main

import (
	"github.com/go-mswin/screencapture"
	"github.com/thesyncim/govpx"
)

func newI420(width, height int) govpx.Image {
	uvWidth, uvHeight := (width+1)/2, (height+1)/2
	return govpx.Image{Width: width, Height: height, Y: make([]byte, width*height), U: make([]byte, uvWidth*uvHeight), V: make([]byte, uvWidth*uvHeight), YStride: width, UStride: uvWidth, VStride: uvWidth}
}

// Windows delivers BGRA; VP8 expects 4:2:0. Keep the buffers allocated for
// the entire stream and honor the DXGI row pitch on every frame.
func bgraToI420(frame screencapture.Frame, dst *govpx.Image) {
	w, h := dst.Width, dst.Height
	for y := 0; y < h; y += 2 {
		for x := 0; x < w; x += 2 {
			var r, g, b, count int
			for dy := 0; dy < 2 && y+dy < h; dy++ {
				for dx := 0; dx < 2 && x+dx < w; dx++ {
					offset := (y+dy)*frame.Stride + (x+dx)*4
					blue, green, red := int(frame.Pix[offset]), int(frame.Pix[offset+1]), int(frame.Pix[offset+2])
					dst.Y[(y+dy)*dst.YStride+x+dx] = byte((66*red+129*green+25*blue+128)/256 + 16)
					r += red
					g += green
					b += blue
					count++
				}
			}
			r /= count
			g /= count
			b /= count
			uv := (y/2)*dst.UStride + x/2
			dst.U[uv] = byte(clampColor((-38*r-74*g+112*b+128)/256 + 128))
			dst.V[uv] = byte(clampColor((112*r-94*g-18*b+128)/256 + 128))
		}
	}
}

// bgraToNV12 converts the captured pixels to the format accepted by Windows
// hardware H.264 encoders. Y is followed by interleaved U/V chroma samples.
func bgraToNV12(frame screencapture.Frame, dst []byte, width, height int) {
	uv := width * height
	for y := 0; y < height; y += 2 {
		for x := 0; x < width; x += 2 {
			var red, green, blue int
			for dy := 0; dy < 2; dy++ {
				for dx := 0; dx < 2; dx++ {
					offset := (y+dy)*frame.Stride + (x+dx)*4
					b, g, r := int(frame.Pix[offset]), int(frame.Pix[offset+1]), int(frame.Pix[offset+2])
					dst[(y+dy)*width+x+dx] = byte(clampColor((66*r+129*g+25*b+128)/256 + 16))
					red += r
					green += g
					blue += b
				}
			}
			red /= 4
			green /= 4
			blue /= 4
			chroma := uv + (y/2)*width + x
			dst[chroma] = byte(clampColor((-38*red-74*green+112*blue+128)/256 + 128))
			dst[chroma+1] = byte(clampColor((112*red-94*green-18*blue+128)/256 + 128))
		}
	}
}

func clampColor(value int) int {
	if value < 0 {
		return 0
	}
	if value > 255 {
		return 255
	}
	return value
}
