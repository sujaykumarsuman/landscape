package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func testServer(t *testing.T) (*Server, *time.Time) {
	t.Helper()
	now := time.Unix(1_800_000_000, 0)
	s := New(nil, Options{AdminPassword: "hunter2", PublicURL: "https://projects.sujaykumar.dev/landscape",
		Tools: []Tool{{Name: "Kubescope", URL: "/kubescope/"}}})
	s.now = func() time.Time { return now }
	return s, &now
}

func login(t *testing.T, h http.Handler, pw string) *http.Cookie {
	t.Helper()
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/api/login", strings.NewReader(`{"password":"`+pw+`"}`)))
	for _, c := range rr.Result().Cookies() {
		if c.Name == "ls_session" && c.Value != "" {
			return c
		}
	}
	return nil
}

func TestSessionTokenExpiresServerSide(t *testing.T) {
	s, now := testServer(t)
	tok := s.mintToken()
	if !s.validToken(tok) {
		t.Fatal("fresh token must be valid")
	}
	*now = now.Add(sessionTTL - time.Second)
	if !s.validToken(tok) {
		t.Fatal("token must be valid until its expiry")
	}
	*now = now.Add(2 * time.Second)
	if s.validToken(tok) {
		t.Fatal("token must be rejected after its expiry")
	}
}

func TestSessionTokenRejectsForgeries(t *testing.T) {
	s, now := testServer(t)
	tok := s.mintToken()
	parts := strings.Split(tok, ".")
	far := now.Add(365 * 24 * time.Hour).Unix()
	other := New(nil, Options{AdminPassword: "other"})
	other.now = s.now
	for name, bad := range map[string]string{
		"empty":            "",
		"legacy v1 hex":    strings.Repeat("a", 64),
		"extended expiry":  parts[0] + "." + "9999999999" + "." + parts[2],
		"far-future valid": "v2." + itoa(far) + "." + s.sign(far),
		"other password":   other.mintToken(),
		"truncated mac":    parts[0] + "." + parts[1] + "." + parts[2][:10],
	} {
		if s.validToken(bad) {
			t.Errorf("%s: token accepted", name)
		}
	}
}

func itoa(n int64) string { b, _ := json.Marshal(n); return string(b) }

func TestLoginSetsExpiringCookie(t *testing.T) {
	s, _ := testServer(t)
	h := s.Handler()
	if c := login(t, h, "wrong"); c != nil {
		t.Fatal("wrong password must not set a session")
	}
	c := login(t, h, "hunter2")
	if c == nil {
		t.Fatal("login must set ls_session")
	}
	if c.Path != "/" || !c.HttpOnly || !c.Secure || c.MaxAge != int(sessionTTL/time.Second) {
		t.Errorf("cookie attrs = path %q httponly %v secure %v maxage %d", c.Path, c.HttpOnly, c.Secure, c.MaxAge)
	}
	if !s.validToken(c.Value) {
		t.Error("login cookie must carry a valid token")
	}
}

func forwardAuth(h http.Handler, cookie *http.Cookie, hdr map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/api/forward-auth", nil)
	if cookie != nil {
		req.AddCookie(cookie)
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}

func TestForwardAuth(t *testing.T) {
	s, now := testServer(t)
	h := s.Handler()
	nav := map[string]string{"X-Forwarded-Method": "GET", "X-Forwarded-Uri": "/kubescope/overview?x=1",
		"Sec-Fetch-Mode": "navigate", "Accept": "text/html,application/xhtml+xml"}

	// signed in → 2xx lets Traefik through
	c := login(t, h, "hunter2")
	if rr := forwardAuth(h, c, nav); rr.Code != http.StatusNoContent {
		t.Fatalf("authed forward-auth = %d, want 204", rr.Code)
	}

	// no session, page navigation → login with ?next
	rr := forwardAuth(h, nil, nav)
	if rr.Code != http.StatusFound {
		t.Fatalf("anon navigation = %d, want 302", rr.Code)
	}
	if loc := rr.Header().Get("Location"); loc != "/landscape/?next=%2Fkubescope%2Foverview%3Fx%3D1" {
		t.Errorf("redirect = %q", loc)
	}

	// no session, API / WebSocket / mutating calls → plain 401
	for name, hdr := range map[string]map[string]string{
		"fetch":     {"X-Forwarded-Method": "GET", "Sec-Fetch-Mode": "cors", "Accept": "application/json"},
		"websocket": {"X-Forwarded-Method": "GET", "Sec-Fetch-Mode": "websocket", "Upgrade": "websocket"},
		"post":      {"X-Forwarded-Method": "POST", "Accept": "text/html"},
		"no-hints":  {"X-Forwarded-Method": "GET", "Accept": "*/*"},
	} {
		if rr := forwardAuth(h, nil, hdr); rr.Code != http.StatusUnauthorized {
			t.Errorf("%s: anon = %d, want 401", name, rr.Code)
		}
	}

	// an expired session is the same as none
	*now = now.Add(sessionTTL + time.Minute)
	if rr := forwardAuth(h, c, nav); rr.Code != http.StatusFound {
		t.Errorf("expired session = %d, want 302", rr.Code)
	}
}

func TestLoginURLRejectsOffsiteNext(t *testing.T) {
	s, _ := testServer(t)
	for _, bad := range []string{"", "https://evil.example/", "//evil.example/x", "/\\evil.example", "kubescope/", "/x\r\nSet-Cookie: a=b"} {
		if got := s.loginURL(bad); got != "/landscape/" {
			t.Errorf("loginURL(%q) = %q, want the bare login", bad, got)
		}
	}
	s.opt.PublicURL = "" // local dev: mounted at /
	if got := s.loginURL("/longhorn/"); got != "/?next=%2Flonghorn%2F" {
		t.Errorf("loginURL without public URL = %q", got)
	}
}

func TestSessionListsToolsOnlyWhenAuthed(t *testing.T) {
	s, _ := testServer(t)
	h := s.Handler()
	get := func(c *http.Cookie) map[string]any {
		req := httptest.NewRequest(http.MethodGet, "/api/session", nil)
		if c != nil {
			req.AddCookie(c)
		}
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		var m map[string]any
		_ = json.Unmarshal(rr.Body.Bytes(), &m)
		return m
	}
	if m := get(nil); m["authed"] != false || m["tools"] != nil {
		t.Errorf("anon session = %v", m)
	}
	m := get(login(t, h, "hunter2"))
	tools, _ := m["tools"].([]any)
	if m["authed"] != true || len(tools) != 1 {
		t.Errorf("authed session = %v", m)
	}
}

func TestParseTools(t *testing.T) {
	tools, bad := ParseTools(`[{"name":"Longhorn","url":"/longhorn/","desc":"Storage"},
		{"name":"Docs","url":"https://longhorn.io/docs/"},
		{"name":"Evil","url":"//evil.example/"},
		{"name":"","url":"/x/"},
		{"name":"JS","url":"javascript:alert(1)"}]`)
	if len(tools) != 2 || tools[0].Name != "Longhorn" || tools[1].URL != "https://longhorn.io/docs/" {
		t.Errorf("tools = %+v", tools)
	}
	if len(bad) != 3 {
		t.Errorf("bad = %v, want 3 skipped", bad)
	}
	if tools, bad := ParseTools("not json"); tools != nil || len(bad) != 1 {
		t.Errorf("invalid json = %v / %v", tools, bad)
	}
	if tools, bad := ParseTools(""); tools != nil || bad != nil {
		t.Errorf("empty = %v / %v", tools, bad)
	}
}
