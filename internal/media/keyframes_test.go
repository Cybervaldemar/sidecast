package media

import (
	"sync"
	"testing"
	"time"
)

func TestKeyframesCoalesceAndRetainTrailingRequest(t *testing.T) {
	sent := make(chan time.Time, 100)
	k := &keyframeRequester{interval: 80 * time.Millisecond, send: func() { sent <- time.Now() }}
	defer k.close()
	k.request()
	first := <-sent
	var callers sync.WaitGroup
	for range 100 {
		callers.Add(1)
		go func() { defer callers.Done(); k.request() }()
	}
	callers.Wait()
	select {
	case trailing := <-sent:
		if trailing.Sub(first) < k.interval {
			t.Fatal("keyframe rate limit exceeded")
		}
	case <-time.After(time.Second):
		t.Fatal("trailing request was lost")
	}
	select {
	case <-sent:
		t.Fatal("unsolicited periodic keyframe request")
	case <-time.After(2 * k.interval):
	}
}

func TestKeyframesStopCancelsPendingRequest(t *testing.T) {
	sent := make(chan struct{}, 10)
	k := &keyframeRequester{interval: 40 * time.Millisecond, send: func() { sent <- struct{}{} }}
	k.request()
	<-sent
	k.request()
	k.close()
	k.close()
	k.request()
	select {
	case <-sent:
		t.Fatal("closed session requested a keyframe")
	case <-time.After(3 * k.interval):
	}
}
