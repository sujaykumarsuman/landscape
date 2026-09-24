package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
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

	// no session, page navigation → login with ?next, as an ABSOLUTE URL: Traefik
	// resolves a relative Location against the auth address (the in-cluster
	// service), so resolve it the same way and require the public host.
	rr := forwardAuth(h, nil, nav)
	if rr.Code != http.StatusFound {
		t.Fatalf("anon navigation = %d, want 302", rr.Code)
	}
	if loc := rr.Header().Get("Location"); loc != "https://projects.sujaykumar.dev/landscape/?next=%2Fkubescope%2Foverview%3Fx%3D1" {
		t.Errorf("redirect = %q", loc)
	}
	authAddr, _ := url.Parse("http://landscape.landscape.svc.cluster.local:8080/api/forward-auth")
	if got, _ := authAddr.Parse(rr.Header().Get("Location")); got.Host != "projects.sujaykumar.dev" || got.Scheme != "https" {
		t.Errorf("Location resolved against the auth address = %s, want the public host", got)
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

func gatedReq(hdr map[string]string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/api/forward-auth", nil)
	for k, v := range hdr {
		r.Header.Set(k, v)
	}
	return r
}

func TestLoginURLRejectsOffsiteNext(t *testing.T) {
	s, _ := testServer(t)
	for _, bad := range []string{"", "https://evil.example/", "//evil.example/x", "/\\evil.example", "kubescope/", "/x\r\nSet-Cookie: a=b"} {
		if got := s.loginURL(gatedReq(map[string]string{"X-Forwarded-Uri": bad})); got != "https://projects.sujaykumar.dev/landscape/" {
			t.Errorf("loginURL(next=%q) = %q, want the bare login", bad, got)
		}
	}
}

func TestLoginURLWithoutPublicURL(t *testing.T) {
	s, _ := testServer(t)
	s.opt.PublicURL = "" // local dev: mounted at /, origin from Traefik's forwarded headers
	fwd := map[string]string{"X-Forwarded-Uri": "/longhorn/", "X-Forwarded-Host": "projects.sujaykumar.dev", "X-Forwarded-Proto": "https"}
	if got := s.loginURL(gatedReq(fwd)); got != "https://projects.sujaykumar.dev/?next=%2Flonghorn%2F" {
		t.Errorf("forwarded origin = %q", got)
	}
	for _, host := range []string{"evil.example/x", "a@evil.example", "evil example"} {
		fwd["X-Forwarded-Host"] = host
		if got := s.loginURL(gatedReq(fwd)); got != "/?next=%2Flonghorn%2F" {
			t.Errorf("unsafe X-Forwarded-Host %q → %q, want a relative login", host, got)
		}
	}
}

func TestSessionTokenCanonicalExpiry(t *testing.T) {
	s, _ := testServer(t)
	parts := strings.Split(s.mintToken(), ".")
	for _, exp := range []string{"+" + parts[1], "0" + parts[1], parts[1] + " "} {
		if s.validToken("v2." + exp + "." + parts[2]) {
			t.Errorf("non-canonical expiry %q accepted", exp)
		}
	}
}

func TestSessionKeyChangesSignatures(t *testing.T) {
	a := New(nil, Options{AdminPassword: "hunter2", SessionKey: "key-a"})
	b := New(nil, Options{AdminPassword: "hunter2", SessionKey: "key-b"})
	noKey := New(nil, Options{AdminPassword: "hunter2"})
	tok := a.mintToken()
	if !a.validToken(tok) || b.validToken(tok) || noKey.validToken(tok) {
		t.Error("a token must only validate under the session key it was minted with")
	}
	// deterministic: a restart with the same inputs keeps sessions valid
	if again := New(nil, Options{AdminPassword: "hunter2", SessionKey: "key-a"}); !again.validToken(tok) {
		t.Error("same password + session key must accept existing tokens (restart-safe)")
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

// With a valid session, the gate still refuses writes and WebSockets that another
// origin started (a sibling subdomain is same-site, so SameSite=Lax lets the
// cookie through); same-origin use and plain navigations pass.
func TestForwardAuthRefusesCrossOriginWrites(t *testing.T) {
	s, _ := testServer(t)
	h := s.Handler()
	c := login(t, h, "hunter2")
	host := map[string]string{"X-Forwarded-Host": "projects.sujaykumar.dev"}
	with := func(kv ...string) map[string]string {
		m := map[string]string{}
		for k, v := range host {
			m[k] = v
		}
		for i := 0; i+1 < len(kv); i += 2 {
			m[kv[i]] = kv[i+1]
		}
		return m
	}
	for name, tc := range map[string]struct {
		hdr  map[string]string
		want int
	}{
		"same-origin POST":           {with("X-Forwarded-Method", "POST", "Sec-Fetch-Site", "same-origin"), http.StatusNoContent},
		"same-site POST (subdomain)": {with("X-Forwarded-Method", "POST", "Sec-Fetch-Site", "same-site"), http.StatusForbidden},
		"cross-site DELETE":          {with("X-Forwarded-Method", "DELETE", "Sec-Fetch-Site", "cross-site"), http.StatusForbidden},
		"cross-site websocket":       {with("X-Forwarded-Method", "GET", "Sec-Fetch-Mode", "websocket", "Sec-Fetch-Site", "cross-site"), http.StatusForbidden},
		"same-origin websocket":      {with("X-Forwarded-Method", "GET", "Sec-Fetch-Mode", "websocket", "Sec-Fetch-Site", "same-origin"), http.StatusNoContent},
		"cross-site GET navigation":  {with("X-Forwarded-Method", "GET", "Sec-Fetch-Mode", "navigate", "Sec-Fetch-Site", "cross-site"), http.StatusNoContent},
		"POST, foreign Origin only":  {with("X-Forwarded-Method", "POST", "Origin", "https://blog.sujaykumar.dev"), http.StatusForbidden},
		"POST, own Origin only":      {with("X-Forwarded-Method", "POST", "Origin", "https://projects.sujaykumar.dev"), http.StatusNoContent},
		"POST, non-browser":          {with("X-Forwarded-Method", "POST"), http.StatusNoContent},
	} {
		if rr := forwardAuth(h, c, tc.hdr); rr.Code != tc.want {
			t.Errorf("%s = %d, want %d", name, rr.Code, tc.want)
		}
	}
}
