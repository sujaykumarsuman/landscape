package collect

import "testing"

func TestParseImage(t *testing.T) {
	cases := []struct {
		in             string
		reg, o, r, tag string
	}{
		{"ghcr.io/sujaykumarsuman/airlift:1.0.1", "ghcr.io", "sujaykumarsuman", "airlift", "1.0.1"},
		{"ghcr.io/sujaykumarsuman/projects-hub:0.1.1", "ghcr.io", "sujaykumarsuman", "projects-hub", "0.1.1"},
		{"nginxinc/nginx-unprivileged:1.27-alpine", "docker.io", "nginxinc", "nginx-unprivileged", "1.27-alpine"},
		{"ghcr.io/o/r@sha256:abc", "ghcr.io", "o", "r", ""},
	}
	for _, c := range cases {
		got := parseImage(c.in)
		if got.Registry != c.reg || got.Owner != c.o || got.Repo != c.r || got.Tag != c.tag {
			t.Errorf("parseImage(%q) = %+v, want %s/%s/%s:%s", c.in, got, c.reg, c.o, c.r, c.tag)
		}
	}
}

func TestParseGitURL(t *testing.T) {
	cases := []struct{ in, o, r string }{
		{"https://github.com/sujaykumarsuman/infra", "sujaykumarsuman", "infra"},
		{"https://github.com/sujaykumarsuman/infra.git", "sujaykumarsuman", "infra"},
		{"ssh://git@github.com/sujaykumarsuman/infra.git", "sujaykumarsuman", "infra"},
		{"git@github.com:sujaykumarsuman/infra.git", "sujaykumarsuman", "infra"},
	}
	for _, c := range cases {
		o, r, ok := parseGitURL(c.in)
		if !ok || o != c.o || r != c.r {
			t.Errorf("parseGitURL(%q) = %q/%q ok=%v, want %q/%q", c.in, o, r, ok, c.o, c.r)
		}
	}
	if _, _, ok := parseGitURL("https://gitlab.com/x/y"); ok {
		t.Errorf("parseGitURL: non-github should not match")
	}
}

func TestGhLinks(t *testing.T) {
	img := parseImage("ghcr.io/sujaykumarsuman/airlift:1.0.1")
	links := ghLinks(img, "sujaykumarsuman", "infra", "main", "airlift")
	byType := map[string]string{}
	for _, l := range links {
		byType[l.Type] = l.URL
	}
	want := map[string]string{
		"source":   "https://github.com/sujaykumarsuman/airlift",
		"workflow": "https://github.com/sujaykumarsuman/airlift/blob/main/.github/workflows/deploy.yml",
		"image":    "https://github.com/sujaykumarsuman/airlift/pkgs/container/airlift",
		"config":   "https://github.com/sujaykumarsuman/infra/blob/main/apps/airlift.yaml",
	}
	for k, v := range want {
		if byType[k] != v {
			t.Errorf("link %s = %q, want %q", k, byType[k], v)
		}
	}
}

func TestExtractPathPrefix(t *testing.T) {
	got := extractPathPrefix("Host(`projects.sujaykumar.dev`) && PathPrefix(`/airlift`)")
	if got != "/airlift" {
		t.Errorf("extractPathPrefix = %q, want /airlift", got)
	}
	if extractPathPrefix("Host(`x`)") != "/" {
		t.Errorf("extractPathPrefix default should be /")
	}
}
