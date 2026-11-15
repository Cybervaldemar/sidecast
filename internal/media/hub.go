// Package media forwards encoded RTP without decoding or transcoding it.
package media

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/pion/rtcp"
	"github.com/pion/rtp"
	"github.com/pion/rtp/codecs"
	"github.com/pion/webrtc/v4"
)

var ErrOffline = errors.New("stream_offline")
var ErrBusy = errors.New("stream_busy")
var ErrFull = errors.New("viewer_limit")

const MaxVideoBitrate = 40_000_000

type Session struct {
	ID             string
	PC             *webrtc.PeerConnection
	audio          bool
	tracks         map[webrtc.RTPCodecType]*webrtc.TrackLocalStaticRTP
	videoSSRC      uint32
	viewers        map[string]*webrtc.PeerConnection
	viewerPacers   map[string]*viewerPacer
	keyframes      *keyframeRequester
	nativeKeyframe func()
}

type Status struct {
	Live      bool   `json:"live"`
	Viewers   int    `json:"viewers"`
	SessionID string `json:"sessionId"`
}

type Hub struct {
	mu             sync.Mutex
	api            *webrtc.API
	config         webrtc.Configuration
	sessions       map[string]*Session
	maxViewers     int
	viewerSettings webrtc.SettingEngine
}

type Option func(*Hub)

// WithViewerSettings applies the server's ICE/UDP settings to outgoing peers.
func WithViewerSettings(settings webrtc.SettingEngine) Option {
	return func(h *Hub) { h.viewerSettings = settings }
}

func New(api *webrtc.API, config webrtc.Configuration, maxViewers int, options ...Option) *Hub {
	h := &Hub{api: api, config: config, sessions: map[string]*Session{}, maxViewers: maxViewers}
	for _, option := range options {
		option(h)
	}
	return h
}

func (h *Hub) Status(nick string) Status {
	h.mu.Lock()
	defer h.mu.Unlock()
	s := h.sessions[nick]
	if s == nil {
		return Status{}
	}
	ready := s.tracks[webrtc.RTPCodecTypeVideo] != nil && (!s.audio || s.tracks[webrtc.RTPCodecTypeAudio] != nil)
	return Status{Live: ready, Viewers: len(s.viewers), SessionID: s.ID}
}

func (h *Hub) Stop(nick, id string) {
	h.mu.Lock()
	s := h.sessions[nick]
	if s == nil || s.ID != id {
		h.mu.Unlock()
		return
	}
	delete(h.sessions, nick)
	s.keyframes.close()
	peers := []*webrtc.PeerConnection{}
	if s.PC != nil {
		peers = append(peers, s.PC)
	}
	for _, p := range s.viewers {
		peers = append(peers, p)
	}
	h.mu.Unlock()
	for _, p := range peers {
		_ = p.Close()
	}
}

func (h *Hub) Close() {
	h.mu.Lock()
	ids := map[string]string{}
	for nick, s := range h.sessions {
		ids[nick] = s.ID
	}
	h.mu.Unlock()
	for nick, id := range ids {
		h.Stop(nick, id)
	}
}

func answer(ctx context.Context, pc *webrtc.PeerConnection, offer webrtc.SessionDescription) (*webrtc.SessionDescription, error) {
	if offer.Type != webrtc.SDPTypeOffer {
		return nil, errors.New("expected offer")
	}
	if err := pc.SetRemoteDescription(offer); err != nil {
		return nil, err
	}
	if err := configureAudio(pc); err != nil {
		return nil, err
	}
	sdp, err := pc.CreateAnswer(nil)
	if err != nil {
		return nil, err
	}
	done := webrtc.GatheringCompletePromise(pc)
	if err = pc.SetLocalDescription(sdp); err != nil {
		return nil, err
	}
	select {
	case <-done:
		return pc.LocalDescription(), nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(12 * time.Second):
		return nil, errors.New("ice_timeout")
	}
}

func watchPeer(pc *webrtc.PeerConnection, cleanup func(), connected ...func()) {
	pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		switch state {
		case webrtc.PeerConnectionStateConnected:
			for _, ready := range connected {
				ready()
			}
		case webrtc.PeerConnectionStateFailed, webrtc.PeerConnectionStateClosed:
			go cleanup()
		case webrtc.PeerConnectionStateDisconnected:
			time.AfterFunc(15*time.Second, func() {
				if pc.ConnectionState() == webrtc.PeerConnectionStateDisconnected {
					cleanup()
				}
			})
		}
	})
	// Abandoned negotiations must not reserve a slot forever.
	time.AfterFunc(30*time.Second, func() {
		if pc.ConnectionState() == webrtc.PeerConnectionStateNew || pc.ConnectionState() == webrtc.PeerConnectionStateConnecting {
			cleanup()
		}
	})
}

func (h *Hub) Publish(ctx context.Context, nick, id string, audio bool, offer webrtc.SessionDescription) (*webrtc.SessionDescription, error) {
	pc, err := h.api.NewPeerConnection(h.config)
	if err != nil {
		return nil, err
	}
	s := &Session{ID: id, PC: pc, audio: audio, tracks: map[webrtc.RTPCodecType]*webrtc.TrackLocalStaticRTP{}, viewers: map[string]*webrtc.PeerConnection{}, viewerPacers: map[string]*viewerPacer{}}
	s.keyframes = &keyframeRequester{interval: time.Second, send: func() { h.sendKeyframe(nick, s) }}
	h.mu.Lock()
	if h.sessions[nick] != nil || len(h.sessions) >= 32 {
		h.mu.Unlock()
		_ = pc.Close()
		return nil, ErrBusy
	}
	h.sessions[nick] = s
	h.mu.Unlock()
	watchPeer(pc, func() { h.Stop(nick, id) })
	pc.OnTrack(func(remote *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
		local, trackErr := webrtc.NewTrackLocalStaticRTP(remote.Codec().RTPCodecCapability, remote.Kind().String(), id)
		if trackErr != nil {
			h.Stop(nick, id)
			return
		}
		h.mu.Lock()
		if h.sessions[nick] != s || s.tracks[remote.Kind()] != nil {
			h.mu.Unlock()
			return
		}
		s.tracks[remote.Kind()] = local
		if remote.Kind() == webrtc.RTPCodecTypeVideo {
			s.videoSSRC = uint32(remote.SSRC())
		}
		h.mu.Unlock()
		for {
			packet, _, readErr := remote.ReadRTP()
			if readErr != nil {
				h.Stop(nick, id)
				return
			}
			// RTP extensions belong to the publisher/server hop. Each viewer
			// negotiates its own IDs and gets fresh transport feedback headers.
			packet.Extension = false
			packet.ExtensionProfile = 0
			packet.Extensions = nil
			// Each bound sender enqueues to its own nonblocking, bounded pacer.
			_ = local.WriteRTP(packet)
		}
	})
	sdp, err := answer(ctx, pc, offer)
	if err != nil {
		h.Stop(nick, id)
		return nil, err
	}
	return sdp, nil
}

func (h *Hub) sendKeyframe(nick string, s *Session) {
	h.mu.Lock()
	ssrc := s.videoSSRC
	active := h.sessions[nick] == s && len(s.viewers) > 0
	nativeKeyframe := s.nativeKeyframe
	h.mu.Unlock()
	if active && nativeKeyframe != nil {
		nativeKeyframe()
	}
	if active && s.PC != nil && ssrc != 0 {
		_ = s.PC.WriteRTCP([]rtcp.Packet{&rtcp.PictureLossIndication{MediaSSRC: ssrc}})
	}
}

// NativePublisher sends encoded frames directly from the desktop process.
// Each track has its own RTP clock and sequence number.
type NativePublisher struct {
	hub                *Hub
	nick, id           string
	video, audio       *webrtc.TrackLocalStaticRTP
	videoSeq, audioSeq uint16
	audioTimestamp     uint32
	videoBase          time.Time
	videoPayloader     codecs.VP8Payloader
	h264Payloader      codecs.H264Payloader
	codec              string
}

func (h *Hub) PublishNative(nick, id string, audio bool, keyframe func(), codec ...string) (*NativePublisher, error) {
	videoCodec := webrtc.MimeTypeVP8
	capability := webrtc.RTPCodecCapability{MimeType: videoCodec, ClockRate: 90000}
	if len(codec) != 0 && codec[0] == webrtc.MimeTypeH264 {
		videoCodec = webrtc.MimeTypeH264
		capability = webrtc.RTPCodecCapability{MimeType: videoCodec, ClockRate: 90000, SDPFmtpLine: "level-asymmetry-allowed=1;packetization-mode=1;profile-level-id=42001f"}
	}
	video, err := webrtc.NewTrackLocalStaticRTP(capability, "video", id)
	if err != nil {
		return nil, err
	}
	var audioTrack *webrtc.TrackLocalStaticRTP
	if audio {
		audioTrack, err = webrtc.NewTrackLocalStaticRTP(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus, ClockRate: 48000, Channels: 2, SDPFmtpLine: gameAudioFMTP("")}, "audio", id)
		if err != nil {
			return nil, err
		}
	}
	s := &Session{ID: id, audio: audio, tracks: map[webrtc.RTPCodecType]*webrtc.TrackLocalStaticRTP{webrtc.RTPCodecTypeVideo: video}, viewers: map[string]*webrtc.PeerConnection{}, viewerPacers: map[string]*viewerPacer{}, nativeKeyframe: keyframe}
	if audioTrack != nil {
		s.tracks[webrtc.RTPCodecTypeAudio] = audioTrack
	}
	s.keyframes = &keyframeRequester{interval: time.Second, send: func() { h.sendKeyframe(nick, s) }}
	h.mu.Lock()
	if h.sessions[nick] != nil || len(h.sessions) >= 32 {
		h.mu.Unlock()
		return nil, ErrBusy
	}
	h.sessions[nick] = s
	h.mu.Unlock()
	return &NativePublisher{hub: h, nick: nick, id: id, video: video, audio: audioTrack, codec: videoCodec}, nil
}

func (p *NativePublisher) Active() bool {
	p.hub.mu.Lock()
	defer p.hub.mu.Unlock()
	s := p.hub.sessions[p.nick]
	return s != nil && s.ID == p.id
}

func (p *NativePublisher) Close() { p.hub.Stop(p.nick, p.id) }

func (p *NativePublisher) WriteVideo(encoded []byte, at time.Time) error {
	if p.videoBase.IsZero() {
		p.videoBase = at
	}
	ts := uint32(at.Sub(p.videoBase).Seconds() * 90000)
	var parts [][]byte
	if p.codec == webrtc.MimeTypeH264 {
		parts = p.h264Payloader.Payload(1200, encoded)
	} else {
		parts = p.videoPayloader.Payload(1200, encoded)
	}
	for i, part := range parts {
		packet := &rtp.Packet{Header: rtp.Header{Version: 2, SequenceNumber: p.videoSeq, Timestamp: ts, SSRC: 1, Marker: i == len(parts)-1}, Payload: part}
		p.videoSeq++
		if err := p.video.WriteRTP(packet); err != nil {
			return err
		}
	}
	return nil
}

func (p *NativePublisher) WriteAudio(encoded []byte) error {
	if p.audio == nil {
		return nil
	}
	packet := &rtp.Packet{Header: rtp.Header{Version: 2, SequenceNumber: p.audioSeq, Timestamp: p.audioTimestamp, SSRC: 2}, Payload: encoded}
	p.audioSeq++
	p.audioTimestamp += 960
	return p.audio.WriteRTP(packet)
}

func (h *Hub) Leave(nick, streamID, viewerID string) {
	h.mu.Lock()
	s := h.sessions[nick]
	var pc *webrtc.PeerConnection
	if s != nil && s.ID == streamID {
		pc = s.viewers[viewerID]
		delete(s.viewers, viewerID)
		delete(s.viewerPacers, viewerID)
	}
	h.mu.Unlock()
	if pc != nil {
		_ = pc.Close()
	}
}

// TargetBitrate uses the weakest connected viewer's transport estimate for
// the one encoded stream shared by all viewers. The publisher may additionally
// reduce its bitrate when its own uplink is constrained.
func (h *Hub) TargetBitrate(nick, sessionID string) (int, error) {
	h.mu.Lock()
	s := h.sessions[nick]
	if s == nil || s.ID != sessionID {
		h.mu.Unlock()
		return 0, ErrOffline
	}
	pacers := make([]*viewerPacer, 0, len(s.viewerPacers))
	for _, pacer := range s.viewerPacers {
		pacers = append(pacers, pacer)
	}
	h.mu.Unlock()
	target := MaxVideoBitrate
	for _, pacer := range pacers {
		if estimate, ok := pacer.feedbackTarget(); ok {
			target = min(target, estimate*9/10)
		}
	}
	return max(500_000, target), nil
}

func (h *Hub) Subscribe(ctx context.Context, nick, viewerID string, offer webrtc.SessionDescription) (*webrtc.SessionDescription, string, error) {
	pc, pacer, err := newViewerPeer(h.viewerSettings, h.config)
	if err != nil {
		return nil, "", err
	}
	h.mu.Lock()
	s := h.sessions[nick]
	if s == nil || s.tracks[webrtc.RTPCodecTypeVideo] == nil || (s.audio && s.tracks[webrtc.RTPCodecTypeAudio] == nil) {
		h.mu.Unlock()
		_ = pc.Close()
		return nil, "", ErrOffline
	}
	if len(s.viewers) >= h.maxViewers {
		h.mu.Unlock()
		_ = pc.Close()
		return nil, "", ErrFull
	}
	tracks := []*webrtc.TrackLocalStaticRTP{s.tracks[webrtc.RTPCodecTypeVideo]}
	if audio := s.tracks[webrtc.RTPCodecTypeAudio]; audio != nil {
		tracks = append(tracks, audio)
	}
	s.viewers[viewerID] = pc
	s.viewerPacers[viewerID] = pacer
	h.mu.Unlock()
	cleanup := func() { h.Leave(nick, s.ID, viewerID) }
	watchPeer(pc, cleanup, func() { pacer.start(); s.keyframes.request() })
	for _, track := range tracks {
		sender, addErr := pc.AddTrack(track)
		if addErr != nil {
			cleanup()
			return nil, "", addErr
		}
		go func() {
			for {
				packets, _, readErr := sender.ReadRTCP()
				if readErr != nil {
					return
				}
				for _, packet := range packets {
					switch packet.(type) {
					case *rtcp.PictureLossIndication, *rtcp.FullIntraRequest:
						s.keyframes.request()
					}
				}
			}
		}()
	}
	sdp, err := answer(ctx, pc, offer)
	if err != nil {
		cleanup()
		return nil, "", err
	}
	return sdp, s.ID, nil
}
