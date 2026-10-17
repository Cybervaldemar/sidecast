package media

import (
	"github.com/pion/interceptor"
	"github.com/pion/interceptor/pkg/cc"
	"github.com/pion/interceptor/pkg/gcc"
	"github.com/pion/rtcp"
	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"
)

// Each viewer gets its own congestion estimator and bounded pacer. The
// publisher's receive-side transport feedback remains independent of viewers.
func newViewerPeer(settings webrtc.SettingEngine, config webrtc.Configuration) (*webrtc.PeerConnection, *viewerPacer, error) {
	engine := &webrtc.MediaEngine{}
	if err := engine.RegisterDefaultCodecs(); err != nil {
		return nil, nil, err
	}
	var pc *webrtc.PeerConnection
	pacer := newViewerPacer(func() { _ = pc.Close() })
	registry := &interceptor.Registry{}
	controller, err := cc.NewInterceptor(func() (cc.BandwidthEstimator, error) {
		estimator, err := gcc.NewSendSideBWE(
			gcc.SendSideBWEInitialBitrate(40_000_000),
			gcc.SendSideBWEMinBitrate(100_000),
			gcc.SendSideBWEMaxBitrate(80_000_000),
			gcc.SendSideBWEPacer(pacer),
		)
		if err != nil {
			return nil, err
		}
		return &viewerEstimator{BandwidthEstimator: estimator, pacer: pacer}, nil
	})
	if err != nil {
		_ = pacer.Close()
		return nil, nil, err
	}
	registry.Add(controller)
	// Regenerate transport-wide sequence numbers for this hop; never reuse the
	// publisher's extension IDs, sequence numbers, or send timestamps.
	if err = webrtc.ConfigureTWCCHeaderExtensionSender(engine, registry); err != nil {
		_ = pacer.Close()
		return nil, nil, err
	}
	if err = webrtc.RegisterDefaultInterceptors(engine, registry); err != nil {
		_ = pacer.Close()
		return nil, nil, err
	}
	registry.Add(&viewerHeadersFactory{pacer: pacer})
	pc, err = webrtc.NewAPI(webrtc.WithMediaEngine(engine), webrtc.WithInterceptorRegistry(registry), webrtc.WithSettingEngine(settings)).NewPeerConnection(config)
	if err != nil {
		_ = pacer.Close()
		return nil, nil, err
	}
	return pc, pacer, nil
}

type viewerEstimator struct {
	cc.BandwidthEstimator
	pacer *viewerPacer
}

func (v *viewerEstimator) WriteRTCP(packets []rtcp.Packet, attributes interceptor.Attributes) error {
	if err := v.BandwidthEstimator.WriteRTCP(packets, attributes); err != nil {
		return err
	}
	stats := v.GetStats()
	loss, _ := stats["averageLoss"].(float64)
	v.pacer.observeCongestion(stats["usage"] == "overuse" || loss > 0.1)
	return nil
}

type viewerHeadersFactory struct{ pacer *viewerPacer }

func (f *viewerHeadersFactory) NewInterceptor(string) (interceptor.Interceptor, error) {
	return &viewerHeaders{pacer: f.pacer}, nil
}

type viewerHeaders struct {
	interceptor.NoOp
	pacer *viewerPacer
}

func (v *viewerHeaders) BindLocalStream(info *interceptor.StreamInfo, writer interceptor.RTPWriter) interceptor.RTPWriter {
	// Pion's NACK responder sends RTX through the primary stream's writer.
	// GCC registers only the primary SSRC, so explicitly route the RTX SSRC too.
	v.pacer.addRTX(info.SSRC, info.SSRCRetransmission)
	return interceptor.RTPWriterFunc(func(header *rtp.Header, payload []byte, attributes interceptor.Attributes) (int, error) {
		// TrackLocalStaticRTP reuses one header while iterating subscribers.
		// Isolate it before this viewer's interceptors add their extensions.
		isolated := *header
		isolated.Extension = false
		isolated.ExtensionProfile = 0
		isolated.Extensions = nil
		return writer.Write(&isolated, payload, attributes)
	})
}
