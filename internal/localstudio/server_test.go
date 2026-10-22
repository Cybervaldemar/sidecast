package localstudio

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"testing/fstest"

	"sidecast/internal/nativecapture"
)

type fakeCapture struct {
	calls           int
	nickname, token string
	settings        nativecapture.Settings
}

func (*fakeCapture) Capabilities() nativecapture.Capabilities {
	return nativecapture.Capabilities{Available: true, Audio: true, ApplicationAudio: true, Platform: "windows"}
}
func (*fakeCapture) Sources() ([]nativecapture.Source, error) {
	return []nativecapture.Source{{ID: "monitor:1", Kind: "monitor"}, {ID: "window:2", Kind: "window", ProcessID: 42}}, nil
}
func (*fakeCapture) Status(string) (nativecapture.Status, error) {
	return nativecapture.Status{Phase: "idle"}, nil
}
func (c *fakeCapture) Start(_ context.Context, nickname, token string, s nativecapture.Settings) (nativecapture.Status, error) {
	c.calls++
	c.nickname = nickname
	c.token = token
	c.settings = s
	return nativecapture.Status{Phase: "live"}, nil
}
func (c *fakeCapture) Stop(token string) error { c.calls++; c.token = token; return nil }
func testHandler(c *fakeCapture) http.Handler {
	server, _ := url.Parse("http://127.0.0.1:8080")
	return New(server, c, fstest.MapFS{"index.html": {Data: []byte("studio")}})
}
func request(h http.Handler, method, path, host, origin, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "http://127.0.0.1:8765"+path, strings.NewReader(body))
	r.Host = host
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	r.Header.Set("Authorization", "Bearer owner-key")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestCaptureRejectsForeignOrigins(t *testing.T) {
	c := &fakeCapture{}
	h := testHandler(c)
	for _, tc := range []struct{ host, origin string }{{"evil.example:8765", ""}, {"192.168.1.2:8765", ""}, {"127.0.0.1:8765", "https://evil.example"}, {"127.0.0.1:8765", "null"}, {"127.0.0.1:8765", "http://localhost:8765"}} {
		if w := request(h, "POST", "/api/native/start", tc.host, tc.origin, `{}`); w.Code != 403 {
			t.Fatalf("accepted origin %q host %q: %d", tc.origin, tc.host, w.Code)
		}
	}
	r := httptest.NewRequest("DELETE", "http://127.0.0.1:8765/api/native/stop", nil)
	r.Header.Set("Sec-Fetch-Site", "cross-site")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 403 || c.calls != 0 {
		t.Fatal("foreign request reached capture")
	}
}

func TestMonitorAndSystemAudioReachCaptureUnchanged(t *testing.T) {
	c := &fakeCapture{}
	h := testHandler(c)
	for _, mode := range []string{"system", "window", "off"} {
		body := `{"nickname":"test-channel","settings":{"sourceId":"monitor:1","fps":120,"bitrate":40,"encoder":"auto","audio":"` + mode + `","audioSourceId":"window:2"}}`
		w := request(h, "POST", "/api/native/start", "127.0.0.1:8765", "http://127.0.0.1:8765", body)
		if w.Code != 200 || c.nickname != "test-channel" || c.token != "owner-key" || c.settings.Audio != mode || c.settings.SourceID != "monitor:1" || c.settings.AudioSourceID != "window:2" {
			t.Fatalf("capture mode changed: %d %+v", w.Code, c)
		}
	}
	before := c.calls
	for _, body := range []string{`{"unknown":true}`, `{} {}`, strings.Repeat("x", 17000)} {
		w := request(h, "POST", "/api/native/start", "127.0.0.1:8765", "", body)
		if w.Code != 400 || c.calls != before {
			t.Fatal("invalid body reached capture")
		}
	}
	w := request(h, "DELETE", "/api/native/stop", "localhost:8765", "http://localhost:8765", "")
	if w.Code != 204 || c.token != "owner-key" || c.calls != before+1 {
		t.Fatal("stop not forwarded")
	}
}

func TestSourcesAndStaticStudio(t *testing.T) {
	h := testHandler(&fakeCapture{})
	w := request(h, "GET", "/api/native/sources", "127.0.0.1:8765", "", "")
	var sources []nativecapture.Source
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &sources) != nil || len(sources) != 2 {
		t.Fatal("source listing unavailable")
	}
	w = request(h, "GET", "/studio", "127.0.0.1:8765", "", "")
	if w.Code != 200 || w.Body.String() != "studio" || !strings.Contains(w.Header().Get("Content-Security-Policy"), "frame-ancestors 'none'") {
		t.Fatal("local UI unavailable or frame protection absent")
	}
}
