package media

import (
	"context"
	"testing"
	"time"

	"github.com/pion/interceptor"
	"github.com/pion/rtcp"
	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"
)

func TestPublisherRequestsLostPacketAfterReceiveBurst(t *testing.T) {
	engine := &webrtc.MediaEngine{}
	if err := engine.RegisterDefaultCodecs(); err != nil {
		t.Fatal(err)
	}
	registry := &interceptor.Registry{}
	if err := registerMediaInterceptors(engine, registry); err != nil {
		t.Fatal(err)
	}
	chain, err := registry.Build("publisher-burst")
	if err != nil {
		t.Fatal(err)
	}
	defer chain.Close()
	var sequence uint16
	reader := chain.BindRemoteStream(&interceptor.StreamInfo{SSRC: 1, MimeType: webrtc.MimeTypeVP8, ClockRate: 90000, RTCPFeedback: []interceptor.RTCPFeedback{{Type: "nack"}}},
		interceptor.RTPReaderFunc(func(buffer []byte, attributes interceptor.Attributes) (int, interceptor.Attributes, error) {
			packet := rtp.Packet{Header: rtp.Header{Version: 2, SSRC: 1, SequenceNumber: sequence}, Payload: []byte{0x10, 0}}
			n, err := packet.MarshalTo(buffer)
			return n, attributes, err
		}),
	)
	buffer := make([]byte, 1500)
	// Model a single receive burst before the first NACK timer fires. The
	// original 512-entry receive log forgets the loss at the start of the frame.
	for sequence = 1; sequence < 1600; sequence++ {
		if sequence == 2 {
			continue
		}
		if _, _, err := reader.Read(buffer, nil); err != nil {
			t.Fatal(err)
		}
	}
	requested := make(chan struct{}, 1)
	chain.BindRTCPWriter(interceptor.RTCPWriterFunc(func(packets []rtcp.Packet, _ interceptor.Attributes) (int, error) {
		for _, packet := range packets {
			if nack, ok := packet.(*rtcp.TransportLayerNack); ok {
				for _, pair := range nack.Nacks {
					pair.Range(func(sequence uint16) bool {
						if sequence == 2 {
							select {
							case requested <- struct{}{}:
							default:
							}
						}
						return true
					})
				}
			}
		}
		return len(packets), nil
	}))
	select {
	case <-requested:
	case <-time.After(time.Second):
		t.Fatal("receive burst erased a missing packet before it could be NACKed")
	}
}

func TestViewerRepairsPacketAfterLargeFrameBurst(t *testing.T) {
	api := webrtc.NewAPI()
	hub := New(api, webrtc.Configuration{}, 1)
	defer hub.Close()
	publisher, err := api.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	defer publisher.Close()
	track, err := webrtc.NewTrackLocalStaticRTP(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeVP8, ClockRate: 90000}, "video", "burst")
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
	answer, err := hub.Publish(context.Background(), "burst", "session", false, testOffer(t, publisher))
	if err != nil {
		t.Fatal(err)
	}
	if err = publisher.SetRemoteDescription(*answer); err != nil {
		t.Fatal(err)
	}
	var sequence uint16
	write := func() {
		t.Helper()
		sequence++
		payload := make([]byte, 1000)
		payload[0] = 0x10
		if err := track.WriteRTP(&rtp.Packet{Header: rtp.Header{Version: 2, SequenceNumber: sequence, Timestamp: uint32(sequence) * 90, Marker: true}, Payload: payload}); err != nil {
			t.Fatal(err)
		}
	}
	eventually(t, func() bool { write(); return hub.Status("burst").Live })
	viewer, err := api.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	defer viewer.Close()
	received := make(chan *rtp.Packet, 4096)
	viewer.OnTrack(func(remote *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
		for {
			packet, _, err := remote.ReadRTP()
			if err != nil {
				return
			}
			received <- packet
		}
	})
	if _, err = viewer.AddTransceiverFromKind(webrtc.RTPCodecTypeVideo, webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionRecvonly}); err != nil {
		t.Fatal(err)
	}
	answer, _, err = hub.Subscribe(context.Background(), "burst", "viewer", testOffer(t, viewer))
	if err != nil {
		t.Fatal(err)
	}
	if err = viewer.SetRemoteDescription(*answer); err != nil {
		t.Fatal(err)
	}
	var first *rtp.Packet
	eventually(t, func() bool {
		write()
		select {
		case first = <-received:
			return true
		default:
			return false
		}
	})
	// A 1.6 MB frame/burst at about 40 Mbit/s replaces the default 1024-packet
	// retransmission history before a delayed Wi-Fi NACK can reach the sender.
	for range 80 {
		for range 20 {
			write()
		}
		time.Sleep(4 * time.Millisecond)
	}
	deadline := time.After(5 * time.Second)
	for {
		select {
		case packet := <-received:
			if packet.SequenceNumber == sequence {
				goto drained
			}
		case <-deadline:
			t.Fatal("viewer did not receive the burst")
		}
	}
drained:
	if err = viewer.WriteRTCP([]rtcp.Packet{&rtcp.TransportLayerNack{MediaSSRC: first.SSRC, Nacks: []rtcp.NackPair{{PacketID: first.SequenceNumber}}}}); err != nil {
		t.Fatal(err)
	}
	deadline = time.After(time.Second)
	traffic := time.NewTicker(20 * time.Millisecond)
	defer traffic.Stop()
	for {
		select {
		case packet := <-received:
			if packet.SequenceNumber == first.SequenceNumber {
				return
			}
		case <-traffic.C:
			write()
		case <-deadline:
			t.Fatal("lost packet expired before a delayed NACK could repair it")
		}
	}
}
