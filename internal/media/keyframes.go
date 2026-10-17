package media

import (
	"sync"
	"time"
)

// keyframeRequester coalesces requests from all viewers. A trailing request is
// retained so a newly connected viewer is not stranded by the rate limit.
type keyframeRequester struct {
	mu       sync.Mutex
	interval time.Duration
	last     time.Time
	pending  *time.Timer
	closed   bool
	send     func()
}

func (k *keyframeRequester) request() {
	k.mu.Lock()
	if k.closed || k.pending != nil {
		k.mu.Unlock()
		return
	}
	wait := k.interval - time.Since(k.last)
	if wait > 0 {
		k.pending = time.AfterFunc(wait, func() {
			k.mu.Lock()
			k.pending = nil
			if k.closed {
				k.mu.Unlock()
				return
			}
			k.last = time.Now()
			k.mu.Unlock()
			k.send()
		})
		k.mu.Unlock()
		return
	}
	k.last = time.Now()
	k.mu.Unlock()
	k.send()
}

func (k *keyframeRequester) close() {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.closed = true
	if k.pending != nil {
		k.pending.Stop()
		k.pending = nil
	}
}
