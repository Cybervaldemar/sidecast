//go:build windows && wails

package main

import (
	"context"
	"encoding/binary"
	"fmt"
	"runtime"
	"time"
	"unsafe"

	"github.com/go-ole/go-ole"
	"github.com/moutend/go-wca/pkg/wca"
	"github.com/thesyncim/gopus"
)

// runLoopback captures the default Windows output mix with WASAPI and emits
// 20 ms stereo Opus packets. COM objects stay on this one OS thread.
func runLoopback(ctx context.Context, ready chan<- error, send func([]byte) error) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	initErr := ole.CoInitializeEx(0, ole.COINIT_MULTITHREADED)
	if initErr != nil {
		ready <- initErr
		return initErr
	}
	defer ole.CoUninitialize()
	var enumerator *wca.IMMDeviceEnumerator
	if err := wca.CoCreateInstance(wca.CLSID_MMDeviceEnumerator, 0, wca.CLSCTX_ALL, wca.IID_IMMDeviceEnumerator, &enumerator); err != nil {
		ready <- err
		return err
	}
	defer enumerator.Release()
	var device *wca.IMMDevice
	if err := enumerator.GetDefaultAudioEndpoint(wca.ERender, wca.EConsole, &device); err != nil {
		ready <- err
		return err
	}
	defer device.Release()
	var client *wca.IAudioClient
	if err := device.Activate(wca.IID_IAudioClient, wca.CLSCTX_ALL, nil, &client); err != nil {
		ready <- err
		return err
	}
	defer client.Release()
	format := &wca.WAVEFORMATEX{WFormatTag: wca.WAVE_FORMAT_PCM, NChannels: 2, NSamplesPerSec: 48000, NAvgBytesPerSec: 48000 * 4, NBlockAlign: 4, WBitsPerSample: 16}
	flags := uint32(wca.AUDCLNT_STREAMFLAGS_LOOPBACK | wca.AUDCLNT_STREAMFLAGS_AUTOCONVERTPCM | wca.AUDCLNT_STREAMFLAGS_SRC_DEFAULT_QUALITY)
	if err := client.Initialize(wca.AUDCLNT_SHAREMODE_SHARED, flags, 1_000_000, 0, format, nil); err != nil {
		ready <- err
		return err
	}
	var capture *wca.IAudioCaptureClient
	if err := client.GetService(wca.IID_IAudioCaptureClient, &capture); err != nil {
		ready <- err
		return err
	}
	defer capture.Release()
	encoder, err := gopus.NewEncoder(gopus.EncoderConfig{SampleRate: 48000, Channels: 2, Application: gopus.ApplicationAudio})
	if err != nil {
		ready <- err
		return err
	}
	if err := encoder.SetBitrate(256000); err != nil {
		ready <- err
		return err
	}
	if err := client.Start(); err != nil {
		ready <- err
		return err
	}
	defer client.Stop()
	ready <- nil
	pcm := make([]int16, 960*2)
	packet := make([]byte, 4000)
	filled := 0
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
		for {
			var count uint32
			if err := capture.GetNextPacketSize(&count); err != nil {
				return fmt.Errorf("WASAPI GetNextPacketSize: %w", err)
			}
			if count == 0 {
				break
			}
			var data *byte
			var frames, flags uint32
			if err := capture.GetBuffer(&data, &frames, &flags, nil, nil); err != nil {
				return fmt.Errorf("WASAPI GetBuffer: %w", err)
			}
			var bytes []byte
			if flags&wca.AUDCLNT_BUFFERFLAGS_SILENT == 0 && data != nil {
				bytes = unsafe.Slice(data, int(frames)*4)
			}
			for i := uint32(0); i < frames; i++ {
				if len(bytes) > 0 {
					pcm[filled] = int16(binary.LittleEndian.Uint16(bytes[i*4:]))
					pcm[filled+1] = int16(binary.LittleEndian.Uint16(bytes[i*4+2:]))
				} else {
					pcm[filled], pcm[filled+1] = 0, 0
				}
				filled += 2
				if filled == len(pcm) {
					n, err := encoder.EncodeInt16(pcm, packet)
					if err != nil {
						_ = capture.ReleaseBuffer(frames)
						return fmt.Errorf("Opus: %w", err)
					}
					if err := send(packet[:n]); err != nil {
						_ = capture.ReleaseBuffer(frames)
						return err
					}
					filled = 0
				}
			}
			if err := capture.ReleaseBuffer(frames); err != nil {
				return fmt.Errorf("WASAPI ReleaseBuffer: %w", err)
			}
		}
	}
}
