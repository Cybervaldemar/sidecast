package desktop

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/json"
	"net"
	"net/http"
	"net/url"
	"strings"
)

// WithControls exposes process controls only to a browser on this computer.
// A fresh capability for each launch also protects against cross-origin POSTs.
func WithControls(next http.Handler, stop func()) http.Handler {
	secret := rand.Text()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/app", func(w http.ResponseWriter, r *http.Request) {
		token := ""
		if localRequest(r) {
			token = secret
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		_ = json.NewEncoder(w).Encode(map[string]string{"shutdownToken": token})
	})
	mux.HandleFunc("POST /api/app/shutdown", func(w http.ResponseWriter, r *http.Request) {
		if !localRequest(r) || subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+secret)) != 1 {
			http.Error(w, "local application control required", http.StatusForbidden)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusNoContent)
		// Deliver the acknowledgement before shutting down the HTTP listener.
		_ = http.NewResponseController(w).Flush()
		stop()
	})
	mux.Handle("/", next)
	return mux
}

func localRequest(r *http.Request) bool {
	// A loopback connection from a reverse proxy is not a local browser.
	for _, header := range []string{"Forwarded", "X-Forwarded-For", "X-Forwarded-Host", "X-Real-IP"} {
		if r.Header.Get(header) != "" {
			return false
		}
	}
	peer, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil || !net.ParseIP(peer).IsLoopback() {
		return false
	}
	host, err := url.Parse("//" + r.Host)
	if err != nil || host.User != nil || host.Path != "" || host.RawQuery != "" || host.Fragment != "" {
		return false
	}
	if !strings.EqualFold(host.Hostname(), "localhost") && !net.ParseIP(host.Hostname()).IsLoopback() {
		return false
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if origin := r.Header.Get("Origin"); origin != "" && !strings.EqualFold(origin, scheme+"://"+r.Host) {
		return false
	}
	switch r.Header.Get("Sec-Fetch-Site") {
	case "", "none", "same-origin":
		return true
	default:
		return false
	}
}
