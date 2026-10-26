package media

import (
	"context"
	"strings"
	"testing"

	"github.com/pion/sdp/v3"
	"github.com/pion/webrtc/v4"
)

func TestGameAudioNegotiation(t *testing.T) {
	for _, sending := range []bool{false, true} {
		name := "publisher"
		if sending {
			name = "viewer"
		}
		t.Run(name, func(t *testing.T) {
			engine := &webrtc.MediaEngine{}
			for _, codec := range []webrtc.RTPCodecParameters{
				{
					RTPCodecCapability: webrtc.RTPCodecCapability{
						MimeType: webrtc.MimeTypeOpus, ClockRate: 48000, Channels: 2,
						SDPFmtpLine: "minptime=10;useinbandfec=1",
					},
					PayloadType: 109,
				},
				{RTPCodecCapability: webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypePCMU, ClockRate: 8000}, PayloadType: 0},
			} {
				if err := engine.RegisterCodec(codec, webrtc.RTPCodecTypeAudio); err != nil {
					t.Fatal(err)
				}
			}
			browser, err := webrtc.NewAPI(webrtc.WithMediaEngine(engine)).NewPeerConnection(webrtc.Configuration{})
			if err != nil {
				t.Fatal(err)
			}
			defer browser.Close()
			api, err := NewPublisherAPI(webrtc.SettingEngine{})
			if err != nil {
				t.Fatal(err)
			}
			server, err := api.NewPeerConnection(webrtc.Configuration{})
			if err != nil {
				t.Fatal(err)
			}
			defer server.Close()
			if sending {
				if _, err = browser.AddTransceiverFromKind(webrtc.RTPCodecTypeAudio, webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionRecvonly}); err != nil {
					t.Fatal(err)
				}
				track, trackErr := webrtc.NewTrackLocalStaticRTP(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus, ClockRate: 48000, Channels: 2}, "audio", "game")
				if trackErr != nil {
					t.Fatal(trackErr)
				}
				if _, err = server.AddTrack(track); err != nil {
					t.Fatal(err)
				}
			} else {
				track, trackErr := webrtc.NewTrackLocalStaticRTP(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus, ClockRate: 48000, Channels: 2}, "audio", "game")
				if trackErr != nil {
					t.Fatal(trackErr)
				}
				if _, err = browser.AddTrack(track); err != nil {
					t.Fatal(err)
				}
			}

			description, err := answer(context.Background(), server, testOffer(t, browser))
			if err != nil {
				t.Fatal(err)
			}
			if err = browser.SetRemoteDescription(*description); err != nil {
				t.Fatalf("audio answer was rejected: %v", err)
			}
			parsed := &sdp.SessionDescription{}
			if err = parsed.Unmarshal([]byte(description.SDP)); err != nil {
				t.Fatal(err)
			}
			for _, media := range parsed.MediaDescriptions {
				if media.MediaName.Media != "audio" {
					continue
				}
				if strings.Join(media.MediaName.Formats, " ") != "109 0" {
					t.Fatalf("negotiated payload types or fallback codec changed: %v", media.MediaName.Formats)
				}
				for _, attribute := range media.Attributes {
					if attribute.Key != "fmtp" || !strings.HasPrefix(attribute.Value, "109 ") {
						continue
					}
					parameters := map[string]string{}
					for _, parameter := range strings.Split(strings.TrimPrefix(attribute.Value, "109 "), ";") {
						key, value, _ := strings.Cut(strings.TrimSpace(parameter), "=")
						if _, duplicate := parameters[key]; duplicate {
							t.Fatalf("duplicate Opus parameter %q", key)
						}
						parameters[key] = value
					}
					for key, want := range map[string]string{
						"stereo": "1", "sprop-stereo": "1", "maxaveragebitrate": "256000",
						"maxplaybackrate": "48000", "useinbandfec": "1", "usedtx": "0", "minptime": "10",
					} {
						if parameters[key] != want {
							t.Errorf("%s = %q, want %q", key, parameters[key], want)
						}
					}
					return
				}
			}
			t.Fatal("negotiated Opus parameters missing")
		})
	}
}
