package media

import (
	"context"
	"testing"
	"time"

	"github.com/pion/webrtc/v4"
)

func TestNativePublisherForwardsVideoAndStops(t *testing.T) {
	api := webrtc.NewAPI()
	hub := New(api, webrtc.Configuration{}, 2)
	defer hub.Close()
	keyframe := make(chan struct{}, 1)
	publisher, err := hub.PublishNative("native", "session", false, func() {
		select {
		case keyframe <- struct{}{}:
		default:
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if !publisher.Active() || !hub.Status("native").Live {
		t.Fatal("native session not live")
	}
	viewer, err := api.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	defer viewer.Close()
	if _, err := viewer.AddTransceiverFromKind(webrtc.RTPCodecTypeVideo, webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionRecvonly}); err != nil {
		t.Fatal(err)
	}
	received := make(chan struct{}, 1)
	viewer.OnTrack(func(remote *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
		if _, _, err := remote.ReadRTP(); err == nil {
			received <- struct{}{}
		}
	})
	answer, _, err := hub.Subscribe(context.Background(), "native", "viewer", testOffer(t, viewer))
	if err != nil {
		t.Fatal(err)
	}
	if err := viewer.SetRemoteDescription(*answer); err != nil {
		t.Fatal(err)
	}
	select {
	case <-keyframe:
	case <-time.After(5 * time.Second):
		t.Fatal("keyframe not requested")
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if err := publisher.WriteVideo([]byte{0x10, 0, 1, 2, 3}, time.Now()); err != nil {
			t.Fatal(err)
		}
		select {
		case <-received:
			publisher.Close()
			if publisher.Active() || hub.Status("native").Live {
				t.Fatal("native session did not stop")
			}
			return
		case <-time.After(20 * time.Millisecond):
		}
	}
	t.Fatal("native RTP was not forwarded")
}

func TestNativeH264ReachesViewer(t *testing.T) {
	api := webrtc.NewAPI()
	hub := New(api, webrtc.Configuration{}, 2)
	defer hub.Close()
	publisher, err := hub.PublishNative("h264", "session", false, nil, webrtc.MimeTypeH264)
	if err != nil {
		t.Fatal(err)
	}
	defer publisher.Close()
	viewer, err := api.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	defer viewer.Close()
	if _, err := viewer.AddTransceiverFromKind(webrtc.RTPCodecTypeVideo, webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionRecvonly}); err != nil {
		t.Fatal(err)
	}
	received := make(chan string, 1)
	viewer.OnTrack(func(remote *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
		if _, _, err := remote.ReadRTP(); err == nil {
			received <- remote.Codec().MimeType
		}
	})
	answer, _, err := hub.Subscribe(context.Background(), "h264", "viewer", testOffer(t, viewer))
	if err != nil {
		t.Fatal(err)
	}
	if err := viewer.SetRemoteDescription(*answer); err != nil {
		t.Fatal(err)
	}
	// One Annex B access unit with SPS, PPS and an IDR slice exercises H.264 RTP packetization.
	frame := []byte{0, 0, 0, 1, 0x67, 0x42, 0xc0, 0x1f, 0, 0, 0, 1, 0x68, 0xce, 0x06, 0xe2, 0, 0, 0, 1, 0x65, 0x88, 0x84}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if err := publisher.WriteVideo(frame, time.Now()); err != nil {
			t.Fatal(err)
		}
		select {
		case codec := <-received:
			if codec != webrtc.MimeTypeH264 {
				t.Fatalf("viewer got %s", codec)
			}
			return
		case <-time.After(20 * time.Millisecond):
		}
	}
	t.Fatal("viewer did not receive H.264 RTP")
}
