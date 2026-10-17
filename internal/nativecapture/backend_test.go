package nativecapture

import (
	"strings"
	"testing"
)

func TestCaptureArgumentsPreserveAudioChoice(t *testing.T) {
	for _, mode := range []string{"off", "window", "system"} {
		t.Run(mode, func(t *testing.T) {
			s := Settings{SourceID: "monitor:123", FPS: 120, Bitrate: 40, Encoder: "libx264", Audio: mode}
			if err := s.validate(); err != nil {
				t.Fatal(err)
			}
			args, err := ffmpegArgs(Source{ID: s.SourceID}, s, s.Encoder, 20000, 20002)
			if err != nil {
				t.Fatal(err)
			}
			joined := strings.Join(args, " ")
			if !strings.Contains(joined, "gfxcapture=hmonitor=123:max_framerate=120") {
				t.Fatal("monitor or frame limit lost")
			}
			if !strings.Contains(joined, "-g 240") {
				t.Fatal("expected two-second GOP")
			}
			if !strings.Contains(joined, "prev_forced_t+2") {
				t.Fatal("quiet sources also need a time-based keyframe interval")
			}
			for _, arg := range args {
				if arg == "-r" {
					t.Fatal("must not manufacture frames to reach requested FPS")
				}
			}
			hasAudio := strings.Contains(joined, "-c:a libopus") && strings.Contains(joined, "pipe:0") && strings.Contains(joined, "127.0.0.1:20002")
			if hasAudio != (mode != "off") {
				t.Fatalf("audio pipeline mismatch for %s", mode)
			}
		})
	}
	args, err := ffmpegArgs(Source{ID: "window:456"}, Settings{FPS: 60, Bitrate: 12, Audio: "off"}, "h264_nvenc", 20000, 0)
	if err != nil || !strings.Contains(strings.Join(args, " "), "gfxcapture=hwnd=456") {
		t.Fatal("window capture unavailable", err)
	}
}

func TestCaptureRejectsInvalidSourcesAndSettings(t *testing.T) {
	for _, id := range []string{"window:0", "window:1:2", "window:1,otherfilter", "monitor:-1", "screen:12", "monitor:18446744073709551616"} {
		if _, err := ffmpegArgs(Source{ID: id}, Settings{Audio: "off"}, "libx264", 20000, 0); err == nil {
			t.Fatalf("accepted %q", id)
		}
	}
	valid := Settings{FPS: 60, Bitrate: 12, Encoder: "auto", Audio: "system"}
	for _, change := range []func(*Settings){func(s *Settings) { s.FPS = 144 }, func(s *Settings) { s.Bitrate = 41 }, func(s *Settings) { s.Bitrate = 0 }, func(s *Settings) { s.Encoder = "custom" }, func(s *Settings) { s.Audio = "microphone" }} {
		value := valid
		change(&value)
		if value.validate() == nil {
			t.Fatalf("accepted invalid settings: %+v", value)
		}
	}
}

func TestDiagnosticsBounded(t *testing.T) {
	b := &diagnosticBuffer{}
	_, _ = b.Write([]byte(strings.Repeat("x", 20000)))
	_, _ = b.Write([]byte("last error"))
	if len(b.String()) != 8192 || !strings.HasSuffix(b.String(), "last error") {
		t.Fatal("diagnostics must retain a bounded tail")
	}
}
