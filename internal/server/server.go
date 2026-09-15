// Package server exposes the read-only landscape API and the embedded UI, behind
// an admin-password gate.
package server

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/sujaykumarsuman/landscape/internal/collect"
	"github.com/sujaykumarsuman/landscape/internal/model"
)

//go:embed web
var webFS embed.FS

// Options configures the server.
type Options struct {
	Addr          string
	AdminPassword string
	GithubOwner   string
	PublicURL     string
	Version       string
	CacheTTL      time.Duration
}

// Server serves the API + UI.
type Server struct {
	opt   Options
	col   *collect.Collector
	token string

	mu        sync.Mutex
	graph     *model.Graph
	graphAt   time.Time
	metrics   *model.Metrics
	metricsAt time.Time
}

// New builds a Server.
func New(col *collect.Collector, opt Options) *Server {
	if opt.CacheTTL == 0 {
		opt.CacheTTL = 10 * time.Second
	}
	return &Server{opt: opt, col: col, token: sessionToken(opt.AdminPassword)}
}

func sessionToken(pw string) string {
	mac := hmac.New(sha256.New, []byte("landscape-session/"+pw))
	mac.Write([]byte("v1"))
	return hex.EncodeToString(mac.Sum(nil))
}

// Handler builds the HTTP router.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("/api/info", s.infoH)
	mux.HandleFunc("/api/session", s.sessionH)
	mux.HandleFunc("/api/login", s.loginH)
	mux.HandleFunc("/api/logout", s.logoutH)
	mux.HandleFunc("/api/graph", s.requireAuth(s.graphH))
	mux.HandleFunc("/api/metrics", s.requireAuth(s.metricsH))

	sub, _ := fs.Sub(webFS, "web")
	mux.Handle("/", http.FileServer(http.FS(sub)))
	return logMW(mux)
}

func (s *Server) adminEnabled() bool { return s.opt.AdminPassword != "" }

func (s *Server) authed(r *http.Request) bool {
	if !s.adminEnabled() {
		return false
	}
	c, err := r.Cookie("ls_session")
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(c.Value), []byte(s.token)) == 1
}

func (s *Server) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.authed(r) {
			writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "unauthorized"})
			return
		}
		next(w, r)
	}
}

func (s *Server) infoH(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"version": s.opt.Version, "public_url": s.opt.PublicURL, "admin_enabled": s.adminEnabled(),
	})
}

func (s *Server) sessionH(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"authed": s.authed(r)})
}

func (s *Server) loginH(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Password string `json:"password"`
	}
	_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body)
	if !s.adminEnabled() || subtle.ConstantTimeCompare([]byte(body.Password), []byte(s.opt.AdminPassword)) != 1 {
		time.Sleep(400 * time.Millisecond) // gentle brake on guessing
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "wrong password"})
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: "ls_session", Value: s.token, Path: "/", HttpOnly: true, Secure: true,
		SameSite: http.SameSiteLaxMode, MaxAge: 12 * 3600,
	})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) logoutH(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: "ls_session", Value: "", Path: "/", HttpOnly: true, MaxAge: -1})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) graphH(w http.ResponseWriter, r *http.Request) {
	g, err := s.getGraph(r.Context())
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, g)
}

func (s *Server) metricsH(w http.ResponseWriter, r *http.Request) {
	m, err := s.getMetrics(r.Context())
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, m)
}

func (s *Server) getGraph(ctx context.Context) (*model.Graph, error) {
	s.mu.Lock()
	if s.graph != nil && time.Since(s.graphAt) < s.opt.CacheTTL {
		g := s.graph
		s.mu.Unlock()
		return g, nil
	}
	s.mu.Unlock()
	g, err := s.col.Graph(ctx)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.graph, s.graphAt = g, time.Now()
	s.mu.Unlock()
	return g, nil
}

func (s *Server) getMetrics(ctx context.Context) (*model.Metrics, error) {
	s.mu.Lock()
	if s.metrics != nil && time.Since(s.metricsAt) < s.opt.CacheTTL {
		m := s.metrics
		s.mu.Unlock()
		return m, nil
	}
	s.mu.Unlock()
	m, err := s.col.Metrics(ctx)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.metrics, s.metricsAt = m, time.Now()
	s.mu.Unlock()
	return m, nil
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func logMW(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		if r.URL.Path != "/healthz" {
			log.Printf("%s %s %s", r.Method, r.URL.Path, time.Since(start).Round(time.Millisecond))
		}
	})
}
