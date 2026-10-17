package media

import (
	"context"
	"testing"
	"time"

	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"
)

func testOffer(t *testing.T, pc *webrtc.PeerConnection) webrtc.SessionDescription {
	t.Helper()
	offer, err := pc.CreateOffer(nil)
	if err != nil {
		t.Fatal(err)
	}
	done := webrtc.GatheringCompletePromise(pc)
	if err = pc.SetLocalDescription(offer); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(8 * time.Second):
		t.Fatal("ICE gathering timeout")
	}
	return *pc.LocalDescription()
}
func eventually(t *testing.T, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(12 * time.Second)
	for time.Now().Before(deadline) {
		if check() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("condition timed out")
}

func TestForwardingRestartAndSessionIsolation(t *testing.T) {
	api := webrtc.NewAPI()
	hub := New(api, webrtc.Configuration{}, 1)
	defer hub.Close()
	for _, session := range []string{"first", "second"} {
		publisher, err := api.NewPeerConnection(webrtc.Configuration{})
		if err != nil {
			t.Fatal(err)
		}
		defer publisher.Close()
		track, err := webrtc.NewTrackLocalStaticRTP(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeVP8, ClockRate: 90000}, "video", "screen")
		if err != nil {
			t.Fatal(err)
		}
		sender, err := publisher.AddTrack(track)
		if err != nil {
			t.Fatal(err)
		}
		go func() {
			for {
				if _, _, err := sender.ReadRTCP(); err != nil {
					return
				}
			}
		}()
		answer, err := hub.Publish(context.Background(), "cyber", session, false, testOffer(t, publisher))
		if err != nil {
			t.Fatal(err)
		}
		if err = publisher.SetRemoteDescription(*answer); err != nil {
			t.Fatal(err)
		}
		done := make(chan struct{})
		go func() {
			ticker := time.NewTicker(20 * time.Millisecond)
			defer ticker.Stop()
			var seq uint16
			for {
				select {
				case <-done:
					return
				case <-ticker.C:
					seq++
					_ = track.WriteRTP(&rtp.Packet{Header: rtp.Header{Version: 2, SequenceNumber: seq, Timestamp: uint32(seq) * 1800, Marker: true}, Payload: []byte{0x10, 0, 1, 2, 3}})
				}
			}
		}()
		eventually(t, func() bool { return hub.Status("cyber").Live })
		viewer, err := api.NewPeerConnection(webrtc.Configuration{})
		if err != nil {
			t.Fatal(err)
		}
		defer viewer.Close()
		if _, err = viewer.AddTransceiverFromKind(webrtc.RTPCodecTypeVideo, webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionRecvonly}); err != nil {
			t.Fatal(err)
		}
		received := make(chan struct{}, 1)
		viewer.OnTrack(func(remote *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
			if _, _, err := remote.ReadRTP(); err == nil {
				received <- struct{}{}
			}
		})
		viewerOffer := testOffer(t, viewer)
		viewerAnswer, id, err := hub.Subscribe(context.Background(), "cyber", "viewer", viewerOffer)
		if err != nil || id != session {
			t.Fatalf("subscribe: %s %v", id, err)
		}
		if err = viewer.SetRemoteDescription(*viewerAnswer); err != nil {
			t.Fatal(err)
		}
		select {
		case <-received:
		case <-time.After(12 * time.Second):
			t.Fatal("RTP was not forwarded")
		}
		if _, _, err = hub.Subscribe(context.Background(), "cyber", "extra", viewerOffer); err != ErrFull {
			t.Fatalf("viewer cap: %v", err)
		}
		hub.Stop("cyber", "stale-session")
		if !hub.Status("cyber").Live {
			t.Fatal("stale stop killed current stream")
		}
		hub.Leave("cyber", session, "viewer")
		if hub.Status("cyber").Viewers != 0 {
			t.Fatal("viewer not released")
		}
		hub.Stop("cyber", session)
		close(done)
		if hub.Status("cyber").Live {
			t.Fatal("stream should be offline")
		}
	}
}
