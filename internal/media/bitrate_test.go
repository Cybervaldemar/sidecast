package media

import (
	"errors"
	"testing"
	"time"
)

func TestTargetBitrateFollowsSlowestConnectedViewer(t *testing.T) {
	slow := &viewerPacer{started: true, feedbackRate: 8_000_000}
	fast := &viewerPacer{started: true, feedbackRate: 30_000_000}
	session := &Session{ID: "live", viewerPacers: map[string]*viewerPacer{}}
	hub := &Hub{sessions: map[string]*Session{"channel": session}}
	check := func(want int) {
		t.Helper()
		got, err := hub.TargetBitrate("channel", "live")
		if err != nil || got != want {
			t.Fatalf("target = %d, %v; want %d", got, err, want)
		}
	}
	check(MaxVideoBitrate)
	session.viewerPacers["fast"] = fast
	check(MaxVideoBitrate) // Sparse or startup traffic is not congestion.
	fast.congestedSince = time.Now().Add(-2 * time.Second)
	check(27_000_000)
	session.viewerPacers["slow"] = slow
	slow.congestedSince = time.Now().Add(-2 * time.Second)
	check(7_200_000)
	slow.closed = true
	check(27_000_000)
	if _, err := hub.TargetBitrate("channel", "old"); !errors.Is(err, ErrOffline) {
		t.Fatalf("stale publisher session accepted: %v", err)
	}
}
