package media

import (
	"errors"
	"io"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pion/interceptor"
	"github.com/pion/rtp"
)

func TestSlowViewerDoesNotBlockHealthyViewer(t *testing.T) {
	blocked, release := make(chan struct{}), make(chan struct{})
	defer close(release)
	slow := newViewerPacer(func() {})
	defer slow.Close()
	slow.AddStream(1, interceptor.RTPWriterFunc(func(*rtp.Header, []byte, interceptor.Attributes) (int, error) {
		close(blocked)
		<-release
		return 100, nil
	}))
	slow.start()
	healthy := newViewerPacer(func() { t.Error("healthy viewer was closed") })
	defer healthy.Close()
	delivered := make(chan uint16, 10)
	healthy.AddStream(1, interceptor.RTPWriterFunc(func(h *rtp.Header, payload []byte, _ interceptor.Attributes) (int, error) {
		delivered <- h.SequenceNumber
		return len(payload), nil
	}))
	healthy.start()
	if _, err := slow.Write(&rtp.Header{SSRC: 1}, []byte{1}, nil); err != nil {
		t.Fatal(err)
	}
	select {
	case <-blocked:
	case <-time.After(time.Second):
		t.Fatal("slow writer never started")
	}
	for seq := uint16(1); seq <= 3; seq++ {
		header := &rtp.Header{SSRC: 1, SequenceNumber: seq}
		if _, err := slow.Write(header, []byte{1}, nil); err != nil {
			t.Fatal(err)
		}
		if _, err := healthy.Write(header, []byte{1}, nil); err != nil {
			t.Fatal(err)
		}
		header.SequenceNumber = 999 // queued packets must own their headers
	}
	for seq := uint16(1); seq <= 3; seq++ {
		select {
		case got := <-delivered:
			if got != seq {
				t.Fatalf("corrupted/reordered packet: got %d want %d", got, seq)
			}
		case <-time.After(time.Second):
			t.Fatal("healthy viewer was blocked by slow subscriber")
		}
	}
}

func TestPacerHonorsRateAndRespondsToEstimate(t *testing.T) {
	var sent atomic.Int32
	p := newViewerPacer(func() { t.Error("pacer unexpectedly closed") })
	defer p.Close()
	p.AddStream(1, interceptor.RTPWriterFunc(func(_ *rtp.Header, b []byte, _ interceptor.Attributes) (int, error) { sent.Add(1); return len(b), nil }))
	p.SetTargetBitrate(100_000)
	p.start()
	for range 20 {
		if _, err := p.Write(&rtp.Header{SSRC: 1}, make([]byte, 1000), nil); err != nil {
			t.Fatal(err)
		}
	}
	time.Sleep(80 * time.Millisecond)
	if n := sent.Load(); n > 3 {
		t.Fatalf("low estimate allowed a burst of %d packets", n)
	}
	p.SetTargetBitrate(40_000_000)
	eventually(t, func() bool { return sent.Load() == 20 })
}

func TestPacerAudioDoesNotWaitBehindVideoBurst(t *testing.T) {
	p := newViewerPacer(func() { t.Error("pacer unexpectedly closed") })
	defer p.Close()
	delivered := make(chan rtp.Header, 32)
	writer := interceptor.RTPWriterFunc(func(h *rtp.Header, b []byte, _ interceptor.Attributes) (int, error) {
		delivered <- *h
		return len(b), nil
	})
	p.AddStream(1, writer)
	p.AddStream(2, writer)
	p.markAudio(2)
	p.SetTargetBitrate(100_000)
	p.start()
	// At this estimate the 20 KB video burst needs over one second to
	// drain. A FIFO would delay the audio until the 500 ms queue cutoff.
	for seq := uint16(1); seq <= 20; seq++ {
		if _, err := p.Write(&rtp.Header{SSRC: 1, SequenceNumber: seq}, make([]byte, 1000), nil); err != nil {
			t.Fatal(err)
		}
	}
	for seq := uint16(1); seq <= 2; seq++ {
		if _, err := p.Write(&rtp.Header{SSRC: 2, SequenceNumber: seq}, make([]byte, 160), nil); err != nil {
			t.Fatal(err)
		}
	}
	var videoSequence, audioSequence uint16
	deadline := time.After(300 * time.Millisecond)
	for audioSequence < 2 {
		select {
		case header := <-delivered:
			if header.SSRC == 1 {
				videoSequence++
				if header.SequenceNumber != videoSequence {
					t.Fatal("video packets were reordered")
				}
			} else {
				audioSequence++
				if header.SequenceNumber != audioSequence {
					t.Fatal("audio packets were reordered")
				}
			}
		case <-deadline:
			t.Fatal("audio was held behind the video burst")
		}
	}
	if videoSequence == 20 {
		t.Fatal("audio was sent after the entire video burst")
	}
	p.SetTargetBitrate(40_000_000)
	deadline = time.After(time.Second)
	for videoSequence < 20 {
		select {
		case header := <-delivered:
			videoSequence++
			if header.SSRC != 1 || header.SequenceNumber != videoSequence {
				t.Fatal("video burst was lost or reordered")
			}
		case <-deadline:
			t.Fatal("video burst did not drain")
		}
	}
}

func TestPacerAudioSharesRateAndQueueLimits(t *testing.T) {
	var sent atomic.Int32
	failed := make(chan struct{}, 1)
	p := newViewerPacer(func() { failed <- struct{}{} })
	defer p.Close()
	p.AddStream(2, interceptor.RTPWriterFunc(func(_ *rtp.Header, b []byte, _ interceptor.Attributes) (int, error) {
		sent.Add(1)
		return len(b), nil
	}))
	p.markAudio(2)
	p.SetTargetBitrate(100_000)
	p.start()
	for range 20 {
		if _, err := p.Write(&rtp.Header{SSRC: 2}, make([]byte, 1000), nil); err != nil {
			t.Fatal(err)
		}
	}
	time.Sleep(80 * time.Millisecond)
	if n := sent.Load(); n > 3 {
		t.Fatalf("audio bypassed the pacing budget: %d packets", n)
	}
	if _, err := p.Write(&rtp.Header{SSRC: 2}, make([]byte, viewerQueueBytes), nil); !errors.Is(err, io.ErrShortBuffer) {
		t.Fatalf("audio bypassed the queue capacity: %v", err)
	}
	select {
	case <-failed:
	case <-time.After(time.Second):
		t.Fatal("audio overflow did not close viewer")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.bytes != 0 || p.queue.Len()+p.audioQueue.Len() != 0 {
		t.Fatal("closed pacer retained audio payloads")
	}
}

func TestPacerAudioPriorityDoesNotHideStaleVideo(t *testing.T) {
	failed := make(chan struct{}, 1)
	audioSent := make(chan struct{}, 1)
	p := newViewerPacer(func() { failed <- struct{}{} })
	defer p.Close()
	p.AddStream(2, interceptor.RTPWriterFunc(func(_ *rtp.Header, b []byte, _ interceptor.Attributes) (int, error) {
		audioSent <- struct{}{}
		return len(b), nil
	}))
	p.mu.Lock()
	p.started = true
	p.queue.PushBack(pacedPacket{queued: time.Now().Add(-viewerQueueDelay - time.Second), size: 100})
	p.audioQueue.PushBack(pacedPacket{header: rtp.Header{SSRC: 2}, queued: time.Now(), size: 100})
	p.bytes = 200
	p.mu.Unlock()
	select {
	case <-failed:
	case <-time.After(time.Second):
		t.Fatal("audio priority concealed stale video")
	}
	select {
	case <-audioSent:
		t.Fatal("pacer continued sending while video exceeded the queue deadline")
	default:
	}
}

func TestPacerBoundsMemoryAndClosesOnlyOnce(t *testing.T) {
	failed := make(chan struct{}, 2)
	p := newViewerPacer(func() { failed <- struct{}{} })
	defer p.Close()
	p.start()
	if _, err := p.Write(&rtp.Header{}, make([]byte, viewerQueueBytes), nil); !errors.Is(err, io.ErrShortBuffer) {
		t.Fatalf("oversized queue accepted: %v", err)
	}
	select {
	case <-failed:
	case <-time.After(time.Second):
		t.Fatal("overflow did not close viewer")
	}
	if _, err := p.Write(&rtp.Header{}, []byte{1}, nil); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("closed pacer accepted packet: %v", err)
	}
	p.Close()
	select {
	case <-failed:
		t.Fatal("duplicate failure callback")
	default:
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.bytes != 0 || p.queue.Len() != 0 {
		t.Fatal("closed pacer retained payloads")
	}
}

func TestPacerDiscardsBeforeConnectionAndExpiresBacklog(t *testing.T) {
	failed := make(chan struct{}, 1)
	p := newViewerPacer(func() { failed <- struct{}{} })
	defer p.Close()
	if _, err := p.Write(&rtp.Header{}, []byte{1}, nil); err != nil {
		t.Fatal(err)
	}
	p.mu.Lock()
	queuedBeforeConnection := p.queue.Len()
	p.started = true
	p.queue.PushBack(pacedPacket{queued: time.Now().Add(-viewerQueueDelay - time.Second), size: 100})
	p.bytes = 100
	p.mu.Unlock()
	if queuedBeforeConnection != 0 {
		t.Fatal("queued media before connection")
	}
	select {
	case <-failed:
	case <-time.After(time.Second):
		t.Fatal("stale backlog did not close viewer")
	}
}

func TestSparseSourceBurstsDoNotManufactureCongestion(t *testing.T) {
	p := newViewerPacer(func() { t.Error("healthy source burst closed viewer") })
	defer p.Close()
	p.SetTargetBitrate(100_000)
	p.start()
	p.AddStream(1, interceptor.RTPWriterFunc(func(_ *rtp.Header, b []byte, _ interceptor.Attributes) (int, error) { return len(b), nil }))
	p.mu.Lock()
	p.inputWindow = time.Now().Add(-120 * time.Millisecond)
	p.inputBytes = 120_000
	p.mu.Unlock()
	if _, err := p.Write(&rtp.Header{SSRC: 1}, []byte{1}, nil); err != nil {
		t.Fatal(err)
	}
	p.mu.Lock()
	burstRate := p.effectiveRateLocked()
	p.mu.Unlock()
	if burstRate <= 5_000_000 {
		t.Fatal("sparse-source estimate throttled a healthy source burst")
	}
	p.observeCongestion(true)
	p.mu.Lock()
	p.congestedSince = time.Now().Add(-2 * time.Second)
	congestedRate, estimate := p.effectiveRateLocked(), p.rate
	p.mu.Unlock()
	if congestedRate != estimate {
		t.Fatal("persistent network congestion did not enforce estimate")
	}
	p.observeCongestion(false)
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.effectiveRateLocked() <= p.rate {
		t.Fatal("source pacing did not recover after congestion cleared")
	}
}
