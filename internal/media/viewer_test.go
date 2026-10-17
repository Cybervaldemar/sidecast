package media

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pion/interceptor"
	"github.com/pion/rtcp"
	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"
)

func TestViewerHeadersAreIsolatedAndRTXIsRouted(t *testing.T) {
	p := newViewerPacer(func() { t.Error("viewer closed") })
	defer p.Close()
	delivered := make(chan uint32, 1)
	p.AddStream(1, interceptor.RTPWriterFunc(func(h *rtp.Header, _ []byte, _ interceptor.Attributes) (int, error) {
		delivered <- h.SSRC
		return 1, nil
	}))
	v := &viewerHeaders{pacer: p}
	writer := v.BindLocalStream(&interceptor.StreamInfo{SSRC: 1, SSRCRetransmission: 2}, interceptor.RTPWriterFunc(func(h *rtp.Header, _ []byte, _ interceptor.Attributes) (int, error) {
		if h.Extension || len(h.Extensions) != 0 {
			t.Error("foreign RTP extensions leaked")
		}
		return 1, h.SetExtension(3, []byte{0, 1})
	}))
	header := &rtp.Header{Version: 2, SSRC: 1}
	if err := header.SetExtension(14, []byte{9}); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write(header, []byte{1}, nil); err != nil {
		t.Fatal(err)
	}
	if header.GetExtension(3) != nil || header.GetExtension(14)[0] != 9 {
		t.Fatal("viewer modified shared header")
	}
	p.start()
	if _, err := p.Write(&rtp.Header{SSRC: 2}, []byte{1}, nil); err != nil {
		t.Fatal(err)
	}
	select {
	case ssrc := <-delivered:
		if ssrc != 2 {
			t.Fatal("RTX SSRC changed")
		}
	case <-time.After(time.Second):
		t.Fatal("RTX packet silently dropped")
	}
}

func TestViewerNACKAndDemandDrivenKeyframes(t *testing.T) {
	api := webrtc.NewAPI()
	hub := New(api, webrtc.Configuration{}, 2)
	defer hub.Close()
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
	var plis atomic.Int32
	go func() {
		for {
			packets, _, err := sender.ReadRTCP()
			if err != nil {
				return
			}
			for _, packet := range packets {
				if _, ok := packet.(*rtcp.PictureLossIndication); ok {
					plis.Add(1)
				}
			}
		}
	}()
	answer, err := hub.Publish(context.Background(), "test", "stream", false, testOffer(t, publisher))
	if err != nil {
		t.Fatal(err)
	}
	if err = publisher.SetRemoteDescription(*answer); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	defer close(done)
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
				packet := &rtp.Packet{Header: rtp.Header{Version: 2, SequenceNumber: seq, Timestamp: uint32(seq) * 1800, Marker: true}, Payload: []byte{0x10, 0, 1, 2, 3}}
				_ = packet.SetExtension(14, []byte{9, 9})
				_ = track.WriteRTP(packet)
			}
		}
	}()
	eventually(t, func() bool { return hub.Status("test").Live })
	if plis.Load() != 0 {
		t.Fatal("requested keyframe with no viewers")
	}

	viewers := make([]*webrtc.PeerConnection, 2)
	packets := make([]chan *rtp.Packet, 2)
	for i := range viewers {
		viewer, err := api.NewPeerConnection(webrtc.Configuration{})
		if err != nil {
			t.Fatal(err)
		}
		defer viewer.Close()
		viewers[i] = viewer
		packets[i] = make(chan *rtp.Packet, 256)
		received := packets[i]
		viewer.OnTrack(func(remote *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
			for {
				packet, _, err := remote.ReadRTP()
				if err != nil {
					return
				}
				select {
				case received <- packet:
				default:
				}
			}
		})
		if _, err = viewer.AddTransceiverFromKind(webrtc.RTPCodecTypeVideo, webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionRecvonly}); err != nil {
			t.Fatal(err)
		}
		description, _, err := hub.Subscribe(context.Background(), "test", string(rune('a'+i)), testOffer(t, viewer))
		if err != nil {
			t.Fatal(err)
		}
		if err = viewer.SetRemoteDescription(*description); err != nil {
			t.Fatal(err)
		}
	}
	for i, received := range packets {
		var first *rtp.Packet
		select {
		case first = <-received:
		case <-time.After(5 * time.Second):
			t.Fatal("viewer received no media")
		}
		if first.GetExtension(14) != nil {
			t.Fatal("publisher extension leaked to viewer")
		}
		if len(first.Extensions) == 0 {
			t.Fatal("outgoing TWCC extension missing")
		}
		if err = viewers[i].WriteRTCP([]rtcp.Packet{&rtcp.TransportLayerNack{MediaSSRC: first.SSRC, Nacks: []rtcp.NackPair{{PacketID: first.SequenceNumber}}}}); err != nil {
			t.Fatal(err)
		}
		timer := time.NewTimer(3 * time.Second)
		found := false
		for !found {
			select {
			case packet := <-received:
				found = packet.SequenceNumber == first.SequenceNumber
			case <-timer.C:
				t.Fatal("NACK retransmission not delivered")
			}
		}
		timer.Stop()
		for range 20 {
			_ = viewers[i].WriteRTCP([]rtcp.Packet{&rtcp.PictureLossIndication{MediaSSRC: first.SSRC}})
		}
	}
	eventually(t, func() bool { return plis.Load() >= 2 })
	if n := plis.Load(); n > 2 {
		t.Fatalf("PLI burst was not coalesced: %d", n)
	}
	count := plis.Load()
	time.Sleep(2200 * time.Millisecond)
	if n := plis.Load(); n != count {
		t.Fatalf("unsolicited periodic keyframes: %d -> %d", count, n)
	}
}
