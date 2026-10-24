package media

import (
	"time"

	"github.com/pion/interceptor"
	"github.com/pion/interceptor/pkg/nack"
	"github.com/pion/webrtc/v4"
)

// At 40 Mbit/s the default 1024-packet history can disappear in about 250 ms.
// Keep roughly one second, including packets waiting in the bounded pacer, so
// Wi-Fi jitter and large frames do not prevent retransmission. This is a repair
// cache, not a playout buffer: it does not add latency to the stream.
const recoveryPackets = 4096

func registerMediaInterceptors(engine *webrtc.MediaEngine, registry *interceptor.Registry) error {
	return webrtc.RegisterDefaultInterceptorsWithOptions(engine, registry,
		webrtc.WithNackGeneratorOptions(nack.GeneratorSize(recoveryPackets), nack.GeneratorInterval(20*time.Millisecond)),
		webrtc.WithNackResponderOptions(nack.ResponderSize(recoveryPackets)),
	)
}

// NewPublisherAPI configures loss recovery on the browser-to-server hop too.
func NewPublisherAPI(settings webrtc.SettingEngine) (*webrtc.API, error) {
	engine := &webrtc.MediaEngine{}
	if err := engine.RegisterDefaultCodecs(); err != nil {
		return nil, err
	}
	registry := &interceptor.Registry{}
	if err := registerMediaInterceptors(engine, registry); err != nil {
		return nil, err
	}
	return webrtc.NewAPI(webrtc.WithMediaEngine(engine), webrtc.WithInterceptorRegistry(registry), webrtc.WithSettingEngine(settings)), nil
}
