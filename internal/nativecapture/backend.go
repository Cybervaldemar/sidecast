// Package nativecapture captures Windows video outside Chromium and publishes
// encoded RTP through the same authenticated WebRTC API as the browser studio.
package nativecapture

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Capabilities struct {
	Available        bool   `json:"available"`
	Platform         string `json:"platform"`
	Reason           string `json:"reason,omitempty"`
	Audio            bool   `json:"audio"`
	ApplicationAudio bool   `json:"applicationAudio"`
}

type Source struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
	ProcessID uint32 `json:"processId,omitempty"`
}

type Settings struct {
	SourceID      string `json:"sourceId"`
	FPS           int    `json:"fps"`
	Bitrate       int    `json:"bitrate"`
	Encoder       string `json:"encoder"`
	Audio         string `json:"audio"`
	AudioSourceID string `json:"audioSourceId"`
}

func (s Settings) validate() error {
	if s.FPS != 30 && s.FPS != 60 && s.FPS != 120 {
		return errors.New("Выберите 30, 60 или 120 FPS.")
	}
	if s.Bitrate < 1 || s.Bitrate > 40 {
		return errors.New("Битрейт должен быть от 1 до 40 Мбит/с.")
	}
	switch s.Encoder {
	case "auto", "h264_nvenc", "h264_amf", "h264_qsv", "libx264":
	default:
		return errors.New("Неизвестный кодировщик.")
	}
	if s.Audio != "off" && s.Audio != "system" && s.Audio != "window" {
		return errors.New("Неизвестный режим звука.")
	}
	return nil
}

func findFFmpeg(explicit string) string {
	if explicit != "" {
		path, _ := filepath.Abs(explicit)
		return path
	}
	if executable, err := os.Executable(); err == nil {
		path := filepath.Join(filepath.Dir(executable), "ffmpeg.exe")
		if _, err = os.Stat(path); err == nil {
			return path
		}
	}
	path, _ := exec.LookPath("ffmpeg")
	return path
}

func probeFFmpeg(ctx context.Context, path string) bool {
	if path == "" {
		return false
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, "-hide_banner", "-h", "filter=gfxcapture")
	hideProcess(cmd)
	output, err := cmd.CombinedOutput()
	return err == nil && strings.Contains(string(output), "max_framerate") && strings.Contains(string(output), "hwnd")
}

func chooseEncoder(ctx context.Context, ffmpeg, requested string) (string, error) {
	choices := []string{requested}
	if requested == "auto" {
		choices = []string{"h264_nvenc", "h264_amf", "h264_qsv", "libx264"}
	}
	for _, encoder := range choices {
		probeCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
		cmd := exec.CommandContext(probeCtx, ffmpeg, "-hide_banner", "-loglevel", "error", "-f", "lavfi", "-i", "color=size=128x128:rate=30", "-frames:v", "1", "-c:v", encoder, "-f", "null", "-")
		hideProcess(cmd)
		err := cmd.Run()
		cancel()
		if err == nil {
			return encoder, nil
		}
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
	}
	return "", errors.New("Кодировщик недоступен. Попробуйте другой или браузерный захват.")
}

func ffmpegArgs(source Source, settings Settings, encoder string, port, audioPort int) ([]string, error) {
	parts := strings.Split(source.ID, ":")
	if len(parts) != 2 || (parts[0] != "window" && parts[0] != "monitor") {
		return nil, errors.New("Некорректный источник.")
	}
	handle, err := strconv.ParseUint(parts[1], 10, 64)
	if err != nil || handle == 0 {
		return nil, errors.New("Некорректный источник.")
	}
	property := "hwnd"
	if parts[0] == "monitor" {
		property = "hmonitor"
	}
	filter := fmt.Sprintf("gfxcapture=%s=%d:max_framerate=%d:width=-2:height=-2", property, handle, settings.FPS)
	rate := strconv.Itoa(settings.Bitrate * 1_000_000)
	args := []string{"-hide_banner", "-loglevel", "warning", "-nostdin", "-thread_queue_size", "32", "-f", "lavfi", "-i", filter}
	if settings.Audio != "off" {
		args = append(args, "-thread_queue_size", "32", "-probesize", "32", "-analyzeduration", "0", "-f", "s16le", "-ar", "48000", "-ac", "2", "-i", "pipe:0")
	}
	args = append(args, "-map", "0:v:0", "-an")
	// NVENC/AMF accept D3D11 textures directly. QSV and software encoding use
	// an explicit download/conversion; never disguise that as a zero-copy path.
	if encoder == "libx264" || encoder == "h264_qsv" {
		args = append(args, "-vf", "hwdownload,format=bgra,format=nv12")
	}
	args = append(args, "-c:v", encoder)
	switch encoder {
	case "h264_nvenc":
		args = append(args, "-preset", "p1", "-tune", "ull", "-rc", "cbr", "-forced-idr", "1")
	case "h264_amf":
		args = append(args, "-usage", "ultralowlatency", "-quality", "speed", "-rc", "cbr")
	case "h264_qsv":
		args = append(args, "-preset", "veryfast", "-look_ahead", "0")
	case "libx264":
		args = append(args, "-preset", "ultrafast", "-tune", "zerolatency", "-x264-params", "repeat-headers=1:scenecut=0")
	default:
		return nil, errors.New("Неизвестный кодировщик.")
	}
	// WGC emits only updated frames. A frame-count GOP alone could postpone the
	// next keyframe for minutes on a quiet desktop; also bound it by source time.
	args = append(args, "-profile:v", "baseline", "-bf", "0", "-g", strconv.Itoa(settings.FPS*2), "-force_key_frames", "expr:if(isnan(prev_forced_t),1,gte(t,prev_forced_t+2))", "-b:v", rate, "-maxrate", rate, "-bufsize", strconv.Itoa(settings.Bitrate*250_000), "-fps_mode", "passthrough", "-bsf:v", "dump_extra=freq=keyframe", "-flush_packets", "1", "-payload_type", "102", "-f", "rtp", fmt.Sprintf("rtp://127.0.0.1:%d?pkt_size=1200", port))
	if settings.Audio != "off" {
		args = append(args, "-map", "1:a:0", "-vn", "-c:a", "libopus", "-b:a", "160k", "-application", "lowdelay", "-frame_duration", "10", "-ar", "48000", "-ac", "2", "-flush_packets", "1", "-payload_type", "111", "-f", "rtp", fmt.Sprintf("rtp://127.0.0.1:%d?pkt_size=1200", audioPort))
	}
	return args, nil
}

// FFmpeg can be verbose on failure. Bound diagnostics for both memory use and
// API error responses; these bytes never contain channel authorization tokens.
type diagnosticBuffer struct {
	mu   sync.Mutex
	text string
}

func (b *diagnosticBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.text += string(p)
	if len(b.text) > 8192 {
		b.text = b.text[len(b.text)-8192:]
	}
	return len(p), nil
}
func (b *diagnosticBuffer) String() string { b.mu.Lock(); defer b.mu.Unlock(); return b.text }
