// Package server exposes the read-only landscape API and the embedded UI, behind
// an admin-password gate. The same session also gates other in-cluster UIs
// (Longhorn, kubescope) through Traefik ForwardAuth (GET /api/forward-auth).
package server

import (
	"context"
	"crypto/hmac"
	"crypto/pbkdf2"
	"crypto/sha256"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"html"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
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
	// SessionKey is an optional random secret (LANDSCAPE_SESSION_KEY) mixed into
	// the session-signing key, so a leaked token can't be brute-forced offline
	// for the admin password. Rotating it (or the password) ends every session.
	SessionKey string
}

// sessionTTL bounds a session server-side (and the cookie's browser lifetime).
const sessionTTL = 12 * time.Hour

// Server serves the API + UI.
type Server struct {
	opt     Options
	col     *collect.Collector
	now     func() time.Time
	signKey []byte // session HMAC key, derived once in New (see signingKey)

	mu        sync.Mutex
	graph     *model.Graph
	graphAt   time.Time
	metrics   *model.Metrics
	metricsAt time.Time
	apps      map[string]appEntry
	traefik   *model.TraefikInfo
	traefikAt time.Time
	storage   *model.StorageInfo
	storageAt time.Time
	misc      map[string]cacheEntry // events / per-app events, keyed by query
}

type appEntry struct {
	d  *model.AppDetail
	at time.Time
}

type cacheEntry struct {
	v  any
	at time.Time
}

// New builds a Server.
func New(col *collect.Collector, opt Options) *Server {
	if opt.CacheTTL == 0 {
		opt.CacheTTL = 10 * time.Second
	}
	return &Server{opt: opt, col: col, now: time.Now, signKey: signingKey(opt.AdminPassword, opt.SessionKey),
		apps: map[string]appEntry{}, misc: map[string]cacheEntry{}}
}

// signingKey derives the session HMAC key from the admin password with PBKDF2
// (a guess against a leaked token costs a full derivation, not one HMAC) mixed
// with the optional random LANDSCAPE_SESSION_KEY (which makes offline guessing
// hopeless). Deterministic, so sessions survive restarts; rotating either input
// ends every session. Computed once at startup.
func signingKey(pw, sessionKey string) []byte {
	if pw == "" {
		return nil // no admin password: authed() refuses everything anyway
	}
	dk, err := pbkdf2.Key(sha256.New, pw, []byte("landscape-session/v2"), 600_000, 32)
	if err != nil {
		panic("pbkdf2: " + err.Error()) // only on invalid parameters, which are constant
	}
	mac := hmac.New(sha256.New, []byte(sessionKey))
	mac.Write(dk)
	return mac.Sum(nil)
}

// Session tokens are stateless and expiring: "v2.<expiry unix>.<hmac>", the HMAC
// over "v2.<expiry>" under signKey. They survive restarts and redeploys (no
// server state), expire server-side after sessionTTL even if the cookie is copied
// out of the browser, and all die when the password or session key rotates.
func (s *Server) sign(exp int64) string {
	mac := hmac.New(sha256.New, s.signKey)
	mac.Write([]byte("v2." + strconv.FormatInt(exp, 10)))
	return hex.EncodeToString(mac.Sum(nil))
}

func (s *Server) mintToken() string {
	exp := s.now().Add(sessionTTL).Unix()
	return "v2." + strconv.FormatInt(exp, 10) + "." + s.sign(exp)
}

func (s *Server) validToken(tok string) bool {
	parts := strings.Split(tok, ".")
	if len(parts) != 3 || parts[0] != "v2" {
		return false
	}
	exp, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || parts[1] != strconv.FormatInt(exp, 10) { // canonical form only
		return false
	}
	now := s.now()
	// expired, or implausibly far out (a token is never minted beyond one TTL)
	if now.Unix() >= exp || exp > now.Add(sessionTTL+time.Minute).Unix() {
		return false
	}
	return hmac.Equal([]byte(parts[2]), []byte(s.sign(exp)))
}

// Handler builds the HTTP router.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("/api/info", s.infoH)
	mux.HandleFunc("/api/session", s.sessionH)
	mux.HandleFunc("/api/login", s.loginH)
	mux.HandleFunc("/api/logout", s.logoutH)
	// Traefik ForwardAuth target (unauthenticated by design: it IS the check).
	mux.HandleFunc("/api/forward-auth", s.forwardAuthH)
	mux.HandleFunc("/api/graph", s.requireAuth(s.graphH))
	mux.HandleFunc("/api/metrics", s.requireAuth(s.metricsH))
	mux.HandleFunc("GET /api/app/{name}", s.requireAuth(s.appH))
	mux.HandleFunc("GET /api/app/{name}/events", s.requireAuth(s.appEventsH))
	mux.HandleFunc("GET /api/app/{name}/logs", s.requireAuth(s.appLogsH))
	mux.HandleFunc("GET /api/events", s.requireAuth(s.eventsH))
	mux.HandleFunc("/api/traefik", s.requireAuth(s.traefikH))
	mux.HandleFunc("/api/storage", s.requireAuth(s.storageH))
	mux.HandleFunc("/api/longhorn", s.requireAuth(s.longhornH))

	mux.Handle("/", s.staticHandler())
	return logMW(mux)
}

// staticHandler serves the embedded UI and, for unknown non-API paths, falls
// back to index.html so the client-side router can handle deep-links like
// /landscape/<app> or /landscape/metrics (a single-page app served under a path
// prefix). Real assets (app.js, style.css) are served as files; unknown /api/*
// paths 404 rather than returning the shell.
func (s *Server) staticHandler() http.Handler {
	sub, _ := fs.Sub(webFS, "web")
	fileSrv := http.FileServer(http.FS(sub))
	shell := s.shell(sub)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		clean := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if clean == "api" || strings.HasPrefix(clean, "api/") {
			http.NotFound(w, r)
			return
		}
		if clean != "" && clean != "index.html" {
			if f, err := sub.Open(clean); err == nil {
				_ = f.Close()
				fileSrv.ServeHTTP(w, r) // a real embedded asset
				return
			}
		}
		// the root or a client-side route (/app/airlift, /metrics, /longhorn): the shell
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		if r.Method != http.MethodHead {
			_, _ = w.Write(shell)
		}
	})
}

// shell is index.html with a <base href> naming the console's mount path (the
// path of LANDSCAPE_PUBLIC_URL, e.g. /landscape/; "/" when unset). The UI's
// asset and API URLs are relative, so the base keeps them resolving under the
// mount from nested routes like /landscape/app/airlift; the client-side router
// reads it back for its own paths.
func (s *Server) shell(sub fs.FS) []byte {
	index, err := fs.ReadFile(sub, "index.html")
	if err != nil {
		return []byte("landscape UI missing from this build")
	}
	base := "/"
	if u, err := url.Parse(s.opt.PublicURL); err == nil && u.Path != "" && u.Path != "/" {
		base = strings.TrimSuffix(u.Path, "/") + "/"
	}
	tag := `<base href="` + html.EscapeString(base) + `">`
	return []byte(strings.Replace(string(index), "<head>", "<head>\n"+tag, 1))
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
	return s.validToken(c.Value)
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

// forwardAuthH is the Traefik ForwardAuth target that gates other in-cluster UIs
// (Longhorn, kubescope) behind this console's session. Traefik sends the original
// request's headers (so the ls_session cookie, Path=/, comes along) plus
// X-Forwarded-Method/-Uri; a 2xx lets the request through to the gated UI, and
// anything else is returned to the browser as-is. So: a page navigation without a
// session is redirected to the login with ?next=<where it was going>, while
// XHR/fetch/WebSocket/non-GET requests get a plain 401.
func (s *Server) forwardAuthH(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if s.authed(r) {
		if crossOriginWrite(r) {
			writeJSON(w, http.StatusForbidden, map[string]any{"error": "cross-origin request refused"})
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if wantsHTML(r) {
		// Absolute on purpose: Traefik resolves a relative Location against the
		// auth address (the in-cluster service), not the page the user asked for.
		w.Header().Set("Location", s.loginURL(r))
		w.WriteHeader(http.StatusFound)
		return
	}
	writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "unauthorized"})
}

// crossOriginWrite reports a gated request that another origin initiated with a
// state-changing method, or as a WebSocket. The session cookie is SameSite=Lax,
// which still lets sibling subdomains (same-site) ride it — e.g. a form POST that
// restarts a workload in kubescope or acts on a Longhorn volume — so the gate
// checks the origin itself: Sec-Fetch-Site, else Origin vs the forwarded host
// (the rule of Go's http.CrossOriginProtection). Top-level GET navigations from
// elsewhere stay allowed; non-browser clients (no headers) are unaffected.
func crossOriginWrite(r *http.Request) bool {
	switch m := r.Header.Get("X-Forwarded-Method"); {
	case r.Header.Get("Sec-Fetch-Mode") == "websocket", strings.EqualFold(r.Header.Get("Upgrade"), "websocket"):
	case m == "", m == http.MethodGet, m == http.MethodHead, m == http.MethodOptions:
		return false
	}
	switch r.Header.Get("Sec-Fetch-Site") {
	case "same-origin", "none":
		return false
	case "":
		// no Fetch Metadata (older browser / non-browser): fall back to Origin
	default: // same-site, cross-site
		return true
	}
	o := r.Header.Get("Origin")
	if o == "" {
		return false
	}
	u, err := url.Parse(o)
	return err != nil || u.Host == "" || !strings.EqualFold(u.Host, r.Header.Get("X-Forwarded-Host"))
}

// wantsHTML reports whether the gated request is a browser page navigation (worth
// redirecting to the login) rather than an API/asset/WebSocket call.
func wantsHTML(r *http.Request) bool {
	m := r.Header.Get("X-Forwarded-Method")
	if m == "" {
		m = r.Method
	}
	if m != http.MethodGet && m != http.MethodHead {
		return false
	}
	if mode := r.Header.Get("Sec-Fetch-Mode"); mode != "" {
		return mode == "navigate"
	}
	return strings.Contains(r.Header.Get("Accept"), "text/html")
}

// loginURL is the console's absolute login URL — LANDSCAPE_PUBLIC_URL, else the
// scheme/host Traefik forwarded (it overwrites client X-Forwarded-* with
// trustForwardHeader off) — carrying the gated path to return to after sign-in
// (X-Forwarded-Uri) when it is a safe same-host path.
func (s *Server) loginURL(r *http.Request) string {
	origin, base := "", "/"
	if u, err := url.Parse(s.opt.PublicURL); err == nil && u.Host != "" {
		origin = u.Scheme + "://" + u.Host
		base = strings.TrimSuffix(u.Path, "/") + "/"
	} else if h := r.Header.Get("X-Forwarded-Host"); h != "" && safeHost(h) {
		proto := r.Header.Get("X-Forwarded-Proto")
		if proto != "http" {
			proto = "https"
		}
		origin = proto + "://" + h
	}
	if next := r.Header.Get("X-Forwarded-Uri"); safeNext(next) {
		return origin + base + "?next=" + url.QueryEscape(next)
	}
	return origin + base
}

// safeHost accepts a bare host[:port] (no path, userinfo or control characters).
func safeHost(h string) bool {
	u, err := url.Parse("https://" + h)
	return err == nil && u.Host == h && u.User == nil && u.Path == "" && !strings.ContainsAny(h, "\\ \t\r\n")
}

// safeNext accepts only a same-host absolute path ("/x…"): never "//host" or
// "/\host" (browsers read both as another host), nor control characters — so
// ?next can't be turned into an open redirect. The UI re-checks before following.
func safeNext(p string) bool {
	if p == "" || len(p) > 2048 || p[0] != '/' {
		return false
	}
	if len(p) > 1 && (p[1] == '/' || p[1] == '\\') {
		return false
	}
	for i := 0; i < len(p); i++ {
		if p[i] < 0x20 || p[i] == 0x7f {
			return false
		}
	}
	return true
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
	// Path=/ so the cookie also reaches the ForwardAuth-gated UIs on this host.
	http.SetCookie(w, &http.Cookie{
		Name: "ls_session", Value: s.mintToken(), Path: "/", HttpOnly: true, Secure: true,
		SameSite: http.SameSiteLaxMode, MaxAge: int(sessionTTL / time.Second),
	})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) logoutH(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: "ls_session", Value: "", Path: "/", HttpOnly: true, Secure: true,
		SameSite: http.SameSiteLaxMode, MaxAge: -1})
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

func (s *Server) appH(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	d, err := s.getApp(r.Context(), name)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, d)
}

func (s *Server) getApp(ctx context.Context, name string) (*model.AppDetail, error) {
	s.mu.Lock()
	if e, ok := s.apps[name]; ok && time.Since(e.at) < s.opt.CacheTTL {
		s.mu.Unlock()
		return e.d, nil
	}
	s.mu.Unlock()
	d, err := s.col.AppDetail(ctx, name)
	if err != nil {
		return nil, err
	}
	// public URL = host of PublicURL + the app's ingress path
	if d.Owner && d.Ingress != nil && d.Ingress.Path != "" {
		if u, err := url.Parse(s.opt.PublicURL); err == nil && u.Host != "" {
			d.PublicURL = u.Scheme + "://" + u.Host + d.Ingress.Path
		}
	}
	s.mu.Lock()
	s.apps[name] = appEntry{d: d, at: time.Now()}
	s.mu.Unlock()
	return d, nil
}

func (s *Server) traefikH(w http.ResponseWriter, r *http.Request) {
	t, err := s.getTraefik(r.Context())
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, t)
}

func (s *Server) getTraefik(ctx context.Context) (*model.TraefikInfo, error) {
	s.mu.Lock()
	if s.traefik != nil && time.Since(s.traefikAt) < s.opt.CacheTTL {
		t := s.traefik
		s.mu.Unlock()
		return t, nil
	}
	s.mu.Unlock()
	t, err := s.col.Traefik(ctx)
	if err != nil {
		return nil, err
	}
	if u, err := url.Parse(s.opt.PublicURL); err == nil && u.Host != "" {
		for i := range t.Routes {
			t.Routes[i].PublicURL = u.Scheme + "://" + u.Host + t.Routes[i].Path
		}
	}
	s.mu.Lock()
	s.traefik, s.traefikAt = t, time.Now()
	s.mu.Unlock()
	return t, nil
}

func (s *Server) storageH(w http.ResponseWriter, r *http.Request) {
	st, err := s.getStorage(r.Context())
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) getStorage(ctx context.Context) (*model.StorageInfo, error) {
	s.mu.Lock()
	if s.storage != nil && time.Since(s.storageAt) < s.opt.CacheTTL {
		st := s.storage
		s.mu.Unlock()
		return st, nil
	}
	s.mu.Unlock()
	st, err := s.col.Storage(ctx)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.storage, s.storageAt = st, time.Now()
	s.mu.Unlock()
	return st, nil
}

// longhornH serves the Longhorn view (volumes ↔ PVCs ↔ apps, node disks,
// recurring jobs, backup target) plus the public URL of the Longhorn UI route.
func (s *Server) longhornH(w http.ResponseWriter, r *http.Request) {
	s.cachedJSON(w, r, "longhorn", func(ctx context.Context) (any, error) {
		li, err := s.col.Longhorn(ctx)
		if err != nil {
			return nil, err
		}
		if li.UIPath != "" {
			li.UIURL = strings.TrimSuffix(li.UIPath, "/") + "/"
			if u, err := url.Parse(s.opt.PublicURL); err == nil && u.Host != "" {
				li.UIURL = u.Scheme + "://" + u.Host + li.UIURL
			}
		}
		return li, nil
	})
}

// eventsH serves the combined events browser with ns/type/kind/text filters.
func (s *Server) eventsH(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := collect.EventFilter{
		Namespace: q.Get("ns"),
		Type:      q.Get("type"),
		Kind:      q.Get("kind"),
		Query:     q.Get("q"),
		Limit:     atoiDefault(q.Get("limit"), 200),
	}
	s.cachedJSON(w, r, "events?"+r.URL.RawQuery, func(ctx context.Context) (any, error) {
		return s.col.Events(ctx, f)
	})
}

// appEventsH serves the events involving one app's workloads.
func (s *Server) appEventsH(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	s.cachedJSON(w, r, "appevents:"+name, func(ctx context.Context) (any, error) {
		return s.col.AppEvents(ctx, name)
	})
}

// appLogsH tails a pod's stdout (never cached — always a fresh tail).
func (s *Server) appLogsH(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	q := r.URL.Query()
	opts := collect.LogOptions{
		Pod:       q.Get("pod"),
		Container: q.Get("container"),
		Tail:      int64(atoiDefault(q.Get("tail"), 500)),
		Since:     int64(atoiDefault(q.Get("since"), 0)),
	}
	l, err := s.col.AppLogs(r.Context(), name, opts)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, l)
}

// cachedJSON answers from a small per-key cache within CacheTTL, else builds and
// stores. Used for the (cheap but pollable) events reads.
func (s *Server) cachedJSON(w http.ResponseWriter, r *http.Request, key string, build func(context.Context) (any, error)) {
	s.mu.Lock()
	if e, ok := s.misc[key]; ok && time.Since(e.at) < s.opt.CacheTTL {
		v := e.v
		s.mu.Unlock()
		writeJSON(w, http.StatusOK, v)
		return
	}
	s.mu.Unlock()
	v, err := build(r.Context())
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
		return
	}
	s.mu.Lock()
	// bound the cache: free-text search makes keys unbounded, so evict expired
	// entries (and, if still full of fresh ones, reset) before inserting.
	if len(s.misc) >= 512 {
		now := time.Now()
		for k, e := range s.misc {
			if now.Sub(e.at) >= s.opt.CacheTTL {
				delete(s.misc, k)
			}
		}
		if len(s.misc) >= 512 {
			s.misc = map[string]cacheEntry{}
		}
	}
	s.misc[key] = cacheEntry{v: v, at: time.Now()}
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, v)
}

func atoiDefault(s string, def int) int {
	if s == "" {
		return def
	}
	if n, err := strconv.Atoi(s); err == nil {
		return n
	}
	return def
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
		// /api/forward-auth runs on every request to the gated UIs (assets, API
		// polls) — too chatty to log; its denials are visible at the browser.
		if r.URL.Path != "/healthz" && r.URL.Path != "/api/forward-auth" {
			log.Printf("%s %s %s", r.Method, r.URL.Path, time.Since(start).Round(time.Millisecond))
		}
	})
}
