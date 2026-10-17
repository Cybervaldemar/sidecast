// Package localstudio serves the existing studio locally and proxies its API
// to the chosen LANPlay server. Native capture controls never bind to the LAN.
package localstudio

import (
	"context"
	"encoding/json"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"lanplay/internal/nativecapture"
)

type Capture interface {
	Capabilities() nativecapture.Capabilities
	Sources() ([]nativecapture.Source, error)
	Start(ctx context.Context, nickname, token string, settings nativecapture.Settings) (nativecapture.Status, error)
	Status(string) (nativecapture.Status, error)
	Stop(string) error
}

func New(server *url.URL, capture Capture, assets fs.FS) http.Handler {
	proxy := httputil.NewSingleHostReverseProxy(server)
	original := proxy.Director
	proxy.Director = func(r *http.Request) {
		original(r)
		r.Host = server.Host
		if r.Header.Get("Origin") != "" {
			r.Header.Set("Origin", server.Scheme+"://"+server.Host)
		}
	}
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		write(w, 502, map[string]string{"error": "upstream_unavailable"})
	}
	client := &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/config", func(w http.ResponseWriter, r *http.Request) {
		endpoint := *server
		endpoint.Path = "/api/config"
		request, err := http.NewRequestWithContext(r.Context(), http.MethodGet, endpoint.String(), nil)
		if err != nil {
			http.Error(w, "invalid server", 502)
			return
		}
		response, err := client.Do(request)
		if err != nil {
			write(w, 502, map[string]string{"error": "upstream_unavailable"})
			return
		}
		defer response.Body.Close()
		var config map[string]any
		if response.StatusCode != 200 || json.NewDecoder(io.LimitReader(response.Body, 128*1024)).Decode(&config) != nil {
			write(w, 502, map[string]string{"error": "upstream_unavailable"})
			return
		}
		config["nativeCapture"] = capture.Capabilities()
		if config["publicUrl"] == nil || config["publicUrl"] == "" {
			public := *server
			if server.Hostname() == "localhost" || server.Hostname() == "127.0.0.1" {
				if addresses, ok := config["addresses"].([]any); ok && len(addresses) > 0 {
					if address, ok := addresses[0].(string); ok && net.ParseIP(address) != nil {
						public.Host = address
						if server.Port() != "" {
							public.Host = net.JoinHostPort(address, server.Port())
						}
					}
				}
			}
			config["publicUrl"] = public.Scheme + "://" + public.Host
		}
		write(w, 200, config)
	})
	mux.HandleFunc("GET /api/native/sources", func(w http.ResponseWriter, r *http.Request) { value, err := capture.Sources(); respond(w, value, err) })
	mux.HandleFunc("GET /api/native/status", func(w http.ResponseWriter, r *http.Request) {
		value, err := capture.Status(secret(r))
		respond(w, value, err)
	})
	mux.HandleFunc("POST /api/native/start", func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Nickname string                 `json:"nickname"`
			Settings nativecapture.Settings `json:"settings"`
		}
		r.Body = http.MaxBytesReader(w, r.Body, 16*1024)
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		if decoder.Decode(&request) != nil || decoder.Decode(new(any)) != io.EOF {
			write(w, 400, map[string]string{"error": "bad_request"})
			return
		}
		value, err := capture.Start(r.Context(), request.Nickname, secret(r), request.Settings)
		respond(w, value, err)
	})
	mux.HandleFunc("DELETE /api/native/stop", func(w http.ResponseWriter, r *http.Request) {
		if err := capture.Stop(secret(r)); err != nil {
			respond(w, nil, err)
			return
		}
		w.WriteHeader(204)
	})
	mux.Handle("/api/", proxy)
	files := http.FileServer(http.FS(assets))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" && r.Method != "HEAD" {
			w.WriteHeader(405)
			return
		}
		if r.URL.Path == "/" || r.URL.Path == "/studio" || strings.HasPrefix(r.URL.Path, "/watch/") {
			data, err := fs.ReadFile(assets, "index.html")
			if err != nil {
				http.Error(w, "Build the frontend first", 503)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			if r.Method != "HEAD" {
				_, _ = w.Write(data)
			}
			return
		}
		files.ServeHTTP(w, r)
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.Host)
		if err != nil || (host != "127.0.0.1" && host != "localhost") {
			http.Error(w, "local studio only", 403)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" {
			parsed, err := url.Parse(origin)
			if err != nil || parsed.Scheme != "http" || parsed.Host != r.Host {
				http.Error(w, "origin denied", 403)
				return
			}
		}
		if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
			http.Error(w, "origin denied", 403)
			return
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; media-src 'self' blob:; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		mux.ServeHTTP(w, r)
	})
}
func write(w http.ResponseWriter, code int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(value)
}
func respond(w http.ResponseWriter, value any, err error) {
	if err != nil {
		write(w, 400, map[string]string{"error": "native_capture_failed", "message": err.Error()})
		return
	}
	write(w, 200, value)
}
func secret(r *http.Request) string {
	return strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
}
