package media

import (
	"container/list"
	"io"
	"log"
	"sync"
	"time"

	"github.com/pion/interceptor"
	"github.com/pion/rtp"
)

const (
	viewerQueueBytes   = 8 * 1024 * 1024
	viewerQueuePackets = 8192
	viewerQueueDelay   = 500 * time.Millisecond
	pacingInterval     = 5 * time.Millisecond
)

type pacedPacket struct {
	header     rtp.Header
	payload    []byte
	attributes interceptor.Attributes
	queued     time.Time
	size       int
}

// viewerPacer implements gcc.Pacer without an unbounded packet queue. Network
// writes run on this viewer's worker, never on the publisher's forwarding loop.
// A single encoded stream cannot be thinned by dropping arbitrary RTP packets:
// close a persistently overloaded subscriber and let it reconnect instead of
// accumulating seconds of latency or corrupting its reference frames.
type viewerPacer struct {
	mu             sync.Mutex
	writers        map[uint32]interceptor.RTPWriter
	queue          list.List
	bytes          int
	rate           int
	sourceRate     int
	inputBytes     int
	inputWindow    time.Time
	congestedSince time.Time
	started        bool
	closed         bool
	done           chan struct{}
	failed         func()
}

func newViewerPacer(failed func()) *viewerPacer {
	p := &viewerPacer{writers: map[uint32]interceptor.RTPWriter{}, rate: 48_000_000, done: make(chan struct{}), failed: failed}
	go p.run()
	return p
}

func (p *viewerPacer) start() {
	p.mu.Lock()
	p.started = true
	p.mu.Unlock()
}

func (p *viewerPacer) AddStream(ssrc uint32, writer interceptor.RTPWriter) {
	p.mu.Lock()
	p.writers[ssrc] = writer
	p.mu.Unlock()
}

func (p *viewerPacer) addRTX(primary, retransmission uint32) {
	if retransmission == 0 {
		return
	}
	p.mu.Lock()
	p.writers[retransmission] = p.writers[primary]
	p.mu.Unlock()
}

func (p *viewerPacer) SetTargetBitrate(rate int) {
	p.mu.Lock()
	// Leave headroom for RTP headers, audio and retransmissions.
	p.rate = max(150_000, min(rate, 80_000_000)*3/2)
	p.mu.Unlock()
}

func (p *viewerPacer) observeCongestion(congested bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !congested {
		p.congestedSince = time.Time{}
	} else if p.congestedSince.IsZero() {
		p.congestedSince = time.Now()
	}
}

func (p *viewerPacer) effectiveRateLocked() int {
	// Sparse screen updates make a bandwidth estimate application-limited.
	// Without alternate encoded layers, pacing below the source rate would
	// manufacture a backlog even on a healthy LAN. Follow source bursts until
	// loss or increasing network delay has persisted for one second.
	if !p.congestedSince.IsZero() && time.Since(p.congestedSince) >= time.Second {
		return p.rate
	}
	return max(p.rate, min(p.sourceRate*3/2, 120_000_000))
}

func (p *viewerPacer) Write(header *rtp.Header, payload []byte, attributes interceptor.Attributes) (int, error) {
	size := header.MarshalSize() + len(payload)
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return 0, io.ErrClosedPipe
	}
	// Negotiation/ICE may take several seconds. Do not queue undecodable video
	// before connection; the hub requests a keyframe once transport is ready.
	if !p.started {
		return size, nil
	}
	now := time.Now()
	if p.inputWindow.IsZero() {
		p.inputWindow = now
	}
	p.inputBytes += size
	if elapsed := now.Sub(p.inputWindow); elapsed >= 100*time.Millisecond {
		rate := int(min(80_000_000, float64(p.inputBytes*8)/elapsed.Seconds()))
		p.sourceRate = max(rate, (p.sourceRate*7+rate)/8)
		p.inputWindow = now
		p.inputBytes = 0
	}
	if p.bytes+size > viewerQueueBytes || p.queue.Len() >= viewerQueuePackets {
		p.failLocked("queue capacity exceeded")
		return 0, io.ErrShortBuffer
	}
	var copied interceptor.Attributes
	if attributes != nil {
		copied = make(interceptor.Attributes, len(attributes))
		for key, value := range attributes {
			copied[key] = value
		}
	}
	p.queue.PushBack(pacedPacket{header: header.Clone(), payload: append([]byte(nil), payload...), attributes: copied, queued: time.Now(), size: size})
	p.bytes += size
	return size, nil
}

func (p *viewerPacer) closeLocked() {
	if !p.closed {
		p.closed = true
		p.queue.Init()
		p.bytes = 0
		close(p.done)
	}
}

func (p *viewerPacer) failLocked(reason string) {
	if !p.closed {
		log.Printf("viewer transport stopped: %s (pacing=%d bit/s, queued=%d bytes, packets=%d)", reason, p.effectiveRateLocked(), p.bytes, p.queue.Len())
		p.closeLocked()
		// Closing a PeerConnection also closes its interceptors. Do it outside
		// this worker and its lock to avoid a shutdown deadlock.
		go p.failed()
	}
}

func (p *viewerPacer) Close() error {
	p.mu.Lock()
	p.closeLocked()
	p.mu.Unlock()
	return nil
}

func (p *viewerPacer) run() {
	ticker := time.NewTicker(pacingInterval)
	defer ticker.Stop()
	last := time.Now()
	budget := float64(0)
	for {
		select {
		case <-p.done:
			return
		case now := <-ticker.C:
			p.mu.Lock()
			elapsed := min(now.Sub(last).Seconds(), 0.05)
			last = now
			// Cap idle credit at 10 ms: a keyframe must not burst all at once.
			rate := p.effectiveRateLocked()
			creditLimit := max(1500, float64(rate)/800)
			if front := p.queue.Front(); front != nil {
				creditLimit = max(creditLimit, float64(front.Value.(pacedPacket).size))
			}
			budget = min(budget+elapsed*float64(rate)/8, creditLimit)
			for !p.closed && p.queue.Len() > 0 {
				next := p.queue.Front().Value.(pacedPacket)
				if time.Since(next.queued) > viewerQueueDelay {
					p.failLocked("queue exceeded 500 ms")
					break
				}
				if float64(next.size) > budget {
					break
				}
				p.queue.Remove(p.queue.Front())
				p.bytes -= next.size
				writer := p.writers[next.header.SSRC]
				budget -= float64(next.size)
				p.mu.Unlock()
				var err error
				if writer != nil {
					_, err = writer.Write(&next.header, next.payload, next.attributes)
				} else {
					err = io.ErrClosedPipe
				}
				p.mu.Lock()
				if err != nil {
					p.failLocked(err.Error())
				}
			}
			p.mu.Unlock()
		}
	}
}
