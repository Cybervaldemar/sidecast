package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/pion/webrtc/v4"
	"lanplay/internal/channels"
	"lanplay/internal/media"
)

func TestChannelAPIAndPrivateFiles(t *testing.T) {
	store, _ := channels.Open(t.TempDir())
	hub := media.New(webrtc.NewAPI(), webrtc.Configuration{}, 8)
	defer hub.Close()
	server := httptest.NewServer(New(store, hub, Config{}, fstest.MapFS{"index.html": {Data: []byte("<html>React</html>")}}))
	defer server.Close()
	request := func(method, path, secret, origin, body string) (int, []byte) {
		t.Helper()
		r, _ := http.NewRequest(method, server.URL+path, strings.NewReader(body))
		if secret != "" {
			r.Header.Set("Authorization", "Bearer "+secret)
		}
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		res, err := http.DefaultClient.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		return res.StatusCode, b
	}
	code, b := request("PUT", "/api/channels/cyber", "", "", "")
	if code != 200 {
		t.Fatalf("claim: %d %s", code, b)
	}
	var identity struct{ Token string }
	_ = json.Unmarshal(b, &identity)
	for _, tc := range []struct {
		method, path, secret, origin, body string
		want                               int
	}{
		{"PUT", "/api/channels/cyber", "", "", "", 409},
		{"PUT", "/api/channels/cyber", identity.Token, "", "", 200},
		{"POST", "/api/channels/cyber/publish", "wrong", "", `{}`, 403},
		{"DELETE", "/api/channels/cyber/publish/stale", "wrong", "", "", 403},
		{"DELETE", "/api/channels/cyber/publish/stale", identity.Token, "", "", 204},
		{"PUT", "/api/channels/other", "", "https://evil.example", "", 403},
		{"GET", "/watch/cyber", "", "", "", 200},
		{"HEAD", "/watch/cyber", "", "", "", 200},
		{"GET", "/data/channels.json", "", "", "", 404},
		{"GET", "/stable/server.js", "", "", "", 404},
		{"GET", "/api/nonexistent", "", "", "", 404},
		{"POST", "/api/channels/cyber/subscribe", "", "", strings.Repeat("x", 129*1024), 400},
	} {
		got, b := request(tc.method, tc.path, tc.secret, tc.origin, tc.body)
		if got != tc.want {
			t.Errorf("%s %s: %d want %d: %s", tc.method, tc.path, got, tc.want, b)
		}
	}
	_, b = request("GET", "/api/channels/cyber", "", "", "")
	var state struct{ Exists, Live bool }
	_ = json.Unmarshal(b, &state)
	if !state.Exists || state.Live {
		t.Fatal("channel must exist and wait offline")
	}
}
