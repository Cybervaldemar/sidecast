package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"strings"

	"github.com/pion/webrtc/v4"
	"sidecast/internal/channels"
	"sidecast/internal/media"
	"sidecast/internal/network"
)

type Config struct {
	PublicURL  string             `json:"publicUrl"`
	ICEServers []webrtc.ICEServer `json:"iceServers"`
	MaxViewers int                `json:"maxViewers"`
	Addresses  []string           `json:"addresses"`
}

type Server struct {
	Store        *channels.Store
	Hub          *media.Hub
	Config       Config
	Assets       fs.FS
	negotiations chan struct{}
}

func New(store *channels.Store, hub *media.Hub, config Config, assets fs.FS) http.Handler {
	s := &Server{Store: store, Hub: hub, Config: config, Assets: assets, negotiations: make(chan struct{}, 32)}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/config", func(w http.ResponseWriter, r *http.Request) {
		config := s.Config
		config.Addresses = network.Addresses()
		reply(w, 200, config)
	})
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		reply(w, 200, map[string]string{"status": "ok", "version": "2.0.0-alpha.1"})
	})
	mux.HandleFunc("GET /api/channels/{nick}", s.status)
	mux.HandleFunc("PUT /api/channels/{nick}", s.claim)
	mux.HandleFunc("POST /api/channels/{nick}/publish", s.publish)
	mux.HandleFunc("DELETE /api/channels/{nick}/publish/{session}", s.stop)
	mux.HandleFunc("POST /api/channels/{nick}/subscribe", s.subscribe)
	mux.HandleFunc("DELETE /api/channels/{nick}/subscribe/{session}/{viewer}", s.leave)
	mux.HandleFunc("/", s.static)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; media-src 'self' blob:; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		if origin := r.Header.Get("Origin"); origin != "" {
			u, err := url.Parse(origin)
			if err != nil || !strings.EqualFold(u.Host, r.Host) || (u.Scheme != "http" && u.Scheme != "https") {
				fail(w, 403, "origin_denied")
				return
			}
		}
		mux.ServeHTTP(w, r)
	})
}

func reply(w http.ResponseWriter, code int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(value)
}
func fail(w http.ResponseWriter, code int, reason string) {
	reply(w, code, map[string]string{"error": reason})
}
func token(r *http.Request) string {
	return strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
}
func decode(w http.ResponseWriter, r *http.Request, dest any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 128*1024)
	d := json.NewDecoder(r.Body)
	if err := d.Decode(dest); err != nil {
		fail(w, 400, "bad_request")
		return false
	}
	if err := d.Decode(new(any)); err != io.EOF {
		fail(w, 400, "bad_request")
		return false
	}
	return true
}
func (s *Server) valid(w http.ResponseWriter, r *http.Request) bool {
	if !channels.Valid(r.PathValue("nick")) {
		fail(w, 400, "invalid_nickname")
		return false
	}
	return true
}
func (s *Server) authorized(w http.ResponseWriter, r *http.Request) bool {
	if !s.valid(w, r) {
		return false
	}
	if !s.Store.Owns(r.PathValue("nick"), token(r)) {
		fail(w, 403, "owner_required")
		return false
	}
	return true
}
func (s *Server) slot(w http.ResponseWriter) bool {
	select {
	case s.negotiations <- struct{}{}:
		return true
	default:
		fail(w, 503, "server_busy")
		return false
	}
}
func (s *Server) status(w http.ResponseWriter, r *http.Request) {
	if !s.valid(w, r) {
		return
	}
	nick := r.PathValue("nick")
	status := s.Hub.Status(nick)
	reply(w, 200, struct {
		media.Status
		Nickname string `json:"nickname"`
		Exists   bool   `json:"exists"`
	}{status, nick, s.Store.Exists(nick)})
}
func (s *Server) claim(w http.ResponseWriter, r *http.Request) {
	secret, err := s.Store.Claim(r.PathValue("nick"), token(r))
	switch {
	case errors.Is(err, channels.ErrTaken):
		fail(w, 409, err.Error())
	case errors.Is(err, channels.ErrInvalid):
		fail(w, 400, err.Error())
	case errors.Is(err, channels.ErrFull):
		fail(w, 503, err.Error())
	case err != nil:
		fail(w, 500, "storage_error")
	default:
		reply(w, 200, map[string]string{"nickname": r.PathValue("nick"), "token": secret})
	}
}

type negotiation struct {
	Description webrtc.SessionDescription `json:"description"`
	Audio       bool                      `json:"audio"`
}

func (s *Server) publish(w http.ResponseWriter, r *http.Request) {
	if !s.authorized(w, r) || !s.slot(w) {
		return
	}
	defer func() { <-s.negotiations }()
	var request negotiation
	if !decode(w, r, &request) {
		return
	}
	id := channels.Token()
	answer, err := s.Hub.Publish(r.Context(), r.PathValue("nick"), id, request.Audio, request.Description)
	if err != nil {
		if errors.Is(err, media.ErrBusy) {
			fail(w, 409, err.Error())
		} else {
			fail(w, 400, "negotiation_failed")
		}
		return
	}
	reply(w, 200, map[string]any{"description": answer, "sessionId": id})
}
func (s *Server) stop(w http.ResponseWriter, r *http.Request) {
	if !s.authorized(w, r) {
		return
	}
	s.Hub.Stop(r.PathValue("nick"), r.PathValue("session"))
	w.WriteHeader(204)
}
func (s *Server) subscribe(w http.ResponseWriter, r *http.Request) {
	if !s.valid(w, r) || !s.slot(w) {
		return
	}
	defer func() { <-s.negotiations }()
	var request negotiation
	if !decode(w, r, &request) {
		return
	}
	viewer := channels.Token()
	answer, session, err := s.Hub.Subscribe(r.Context(), r.PathValue("nick"), viewer, request.Description)
	if err != nil {
		switch {
		case errors.Is(err, media.ErrOffline):
			fail(w, 409, err.Error())
		case errors.Is(err, media.ErrFull):
			fail(w, 429, err.Error())
		default:
			fail(w, 400, "negotiation_failed")
		}
		return
	}
	reply(w, 200, map[string]any{"description": answer, "sessionId": session, "viewerId": viewer})
}
func (s *Server) leave(w http.ResponseWriter, r *http.Request) {
	if !s.valid(w, r) {
		return
	}
	// Viewer IDs are unguessable capabilities, separate from public stream IDs.
	s.Hub.Leave(r.PathValue("nick"), r.PathValue("session"), r.PathValue("viewer"))
	w.WriteHeader(204)
}
func (s *Server) static(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" && r.Method != "HEAD" {
		fail(w, 405, "method_not_allowed")
		return
	}
	path := r.URL.Path
	if strings.HasPrefix(path, "/api/") {
		fail(w, 404, "not_found")
		return
	}
	if path == "/" || path == "/studio" || (strings.HasPrefix(path, "/watch/") && channels.Valid(strings.TrimPrefix(path, "/watch/"))) {
		b, err := fs.ReadFile(s.Assets, "index.html")
		if err != nil {
			http.Error(w, "Build the frontend first: npm run build", 503)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if r.Method != "HEAD" {
			_, _ = w.Write(b)
		}
		return
	}
	if strings.HasPrefix(path, "/assets/") || path == "/favicon.svg" {
		http.FileServer(http.FS(s.Assets)).ServeHTTP(w, r)
		return
	}
	http.NotFound(w, r)
}
