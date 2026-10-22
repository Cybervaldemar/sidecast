package desktop

import (
	"crypto/tls"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestApplicationControls(t *testing.T) {
	stopped := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(404) })
	handler := WithControls(next, func() { stopped = true })
	request := func(method, path, peer, host, origin, site, token string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, "http://localhost:8080"+path, nil)
		r.RemoteAddr, r.Host = peer, host
		r.Header.Set("Origin", origin)
		r.Header.Set("Sec-Fetch-Site", site)
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	response := request("GET", "/api/app", "127.0.0.1:1234", "localhost:8080", "", "same-origin", "")
	var capability struct{ ShutdownToken string }
	if err := json.Unmarshal(response.Body.Bytes(), &capability); err != nil || len(capability.ShutdownToken) < 26 {
		t.Fatalf("missing local capability: %s (%v)", response.Body, err)
	}
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("capability must not be cached")
	}
	for _, test := range []struct {
		name, peer, host, origin, site string
	}{
		{"LAN viewer", "192.168.1.2:1234", "localhost:8080", "", ""},
		{"LAN origin", "127.0.0.1:1234", "192.168.1.10:8080", "", ""},
		{"DNS rebinding", "127.0.0.1:1234", "attacker.example:8080", "", ""},
		{"foreign origin", "127.0.0.1:1234", "localhost:8080", "https://attacker.example", ""},
		{"different local port", "127.0.0.1:1234", "localhost:8080", "http://localhost:3000", "same-site"},
		{"wrong scheme", "127.0.0.1:1234", "localhost:8080", "https://localhost:8080", ""},
		{"cross-site", "127.0.0.1:1234", "localhost:8080", "", "cross-site"},
		{"unparseable peer", "unknown", "localhost:8080", "", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := request("GET", "/api/app", test.peer, test.host, test.origin, test.site, "")
			var value struct{ ShutdownToken string }
			if err := json.Unmarshal(response.Body.Bytes(), &value); err != nil || value.ShutdownToken != "" {
				t.Fatalf("capability leaked: %s (%v)", response.Body, err)
			}
			response = request("POST", "/api/app/shutdown", test.peer, test.host, test.origin, test.site, capability.ShutdownToken)
			if response.Code != 403 || stopped {
				t.Fatalf("unauthorized shutdown: status=%d stopped=%v", response.Code, stopped)
			}
		})
	}
	for _, token := range []string{"", "wrong", capability.ShutdownToken + "x"} {
		response := request("POST", "/api/app/shutdown", "127.0.0.1:1234", "localhost:8080", "http://localhost:8080", "same-origin", token)
		if response.Code != 403 || stopped {
			t.Fatalf("invalid token accepted: status=%d stopped=%v", response.Code, stopped)
		}
	}
	// Capabilities from a previous process launch cannot stop a new instance.
	newHandler := WithControls(next, func() { t.Error("stale capability stopped a new instance") })
	r := httptest.NewRequest("POST", "http://localhost:8080/api/app/shutdown", nil)
	r.RemoteAddr = "127.0.0.1:1234"
	r.Header.Set("Authorization", "Bearer "+capability.ShutdownToken)
	w := httptest.NewRecorder()
	newHandler.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatalf("stale token: %d", w.Code)
	}
	response = request("POST", "/api/app/shutdown", "[::1]:1234", "[::1]:8080", "http://[::1]:8080", "same-origin", capability.ShutdownToken)
	if response.Code != 204 || !response.Flushed || !stopped {
		t.Fatalf("shutdown: status=%d flushed=%v stopped=%v", response.Code, response.Flushed, stopped)
	}
	response = request("GET", "/api/health", "127.0.0.1:1234", "localhost:8080", "", "", "")
	if response.Code != 404 {
		t.Fatal("unrelated route did not reach the wrapped handler")
	}
}

func TestLocalHTTPSControls(t *testing.T) {
	r := httptest.NewRequest("POST", "https://localhost/api/app/shutdown", nil)
	r.RemoteAddr = "127.0.0.1:1234"
	r.TLS = &tls.ConnectionState{}
	r.Header.Set("Origin", "https://localhost")
	if !localRequest(r) {
		t.Fatal("same-origin HTTPS controls rejected")
	}
}

func TestProxiedControlsDenied(t *testing.T) {
	for _, header := range []string{"Forwarded", "X-Forwarded-For", "X-Forwarded-Host", "X-Real-IP"} {
		t.Run(header, func(t *testing.T) {
			for _, peer := range []string{"127.0.0.1:1234", "192.168.1.2:1234"} {
				r := httptest.NewRequest("GET", "http://localhost:8080/api/app", nil)
				r.RemoteAddr = peer
				r.Header.Set(header, "127.0.0.1")
				if localRequest(r) {
					t.Fatal("forwarding headers must not grant local controls")
				}
			}
		})
	}
}
