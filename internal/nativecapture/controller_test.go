package nativecapture

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/pion/webrtc/v4"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestStopRequiresOwnerAndCleansUpOnce(t *testing.T) {
	pc, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	video, err := listenRTP()
	if err != nil {
		t.Fatal(err)
	}
	defer video.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a := &publication{ctx: ctx, cancel: cancel, pc: pc, video: video, nickname: "test-channel", token: strings.Repeat("a", 64), session: "session-1", status: Status{Phase: "live"}}
	target, _ := url.Parse("http://server.example:8080")
	var deletions atomic.Int32
	c := &Controller{ctx: context.Background(), server: target, current: a, client: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.Method != "DELETE" || r.URL.Path != "/api/channels/test-channel/publish/session-1" || r.Header.Get("Authorization") != "Bearer "+a.token {
			t.Error("incorrect upstream cleanup")
		}
		deletions.Add(1)
		return &http.Response{StatusCode: 204, Body: io.NopCloser(strings.NewReader(""))}, nil
	})}}
	if _, err = c.Status("wrong"); err == nil {
		t.Fatal("foreign owner read active capture")
	}
	if err = c.Stop("wrong"); err == nil || ctx.Err() != nil {
		t.Fatal("foreign owner stopped capture")
	}
	if err = c.Stop(a.token); err != nil {
		t.Fatal(err)
	}
	c.finish(a, "late failure")
	c.Close()
	if deletions.Load() != 1 || ctx.Err() == nil || pc.ConnectionState() != webrtc.PeerConnectionStateClosed {
		t.Fatal("capture not cleaned up exactly once")
	}
	status, err := c.Status(a.token)
	if err != nil || status.Phase != "idle" || status.Error != "" {
		t.Fatalf("unexpected idle status: %+v %v", status, err)
	}
}
