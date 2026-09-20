package collect

import "testing"

// Source grouping is label-driven (app.kubernetes.io/part-of), not a hardcoded
// map: a multi-component app's images all resolve to one source group/repo, so the
// Sources lane collapses them to a single card with the correct link.
func TestSourceGroupingByLabels(t *testing.T) {
	co := &Collector{githubOwner: "sujaykumarsuman"}
	partOf := func(group string) map[string]string { return map[string]string{labelPartOf: group} }

	// All three xlearn components (gateway/identity/curriculum) are part-of xlearn,
	// so they share ONE source group + repo — including curriculum, with no code change.
	g := co.resolveSource(partOf("xlearn"), parseImage("ghcr.io/sujaykumarsuman/xlearn-gateway:0.1.11"))
	i := co.resolveSource(partOf("xlearn"), parseImage("ghcr.io/sujaykumarsuman/xlearn-identity:0.1.11"))
	c := co.resolveSource(partOf("xlearn"), parseImage("ghcr.io/sujaykumarsuman/xlearn-curriculum:0.1.11"))
	for _, si := range []sourceInfo{g, i, c} {
		if si.Group != "xlearn" || si.Owner != "sujaykumarsuman" || si.Repo != "xlearn" || si.Subdir != "" {
			t.Errorf("xlearn component grouped wrong: %+v", si)
		}
	}
	if g != i || i != c {
		t.Errorf("xlearn components must share a source: %+v / %+v / %+v", g, i, c)
	}

	// source-repo/subdir labels override the repo when it differs from the group
	// (projects is built from sujaykumarsuman.github.io/projects).
	proj := co.resolveSource(map[string]string{
		labelPartOf:       "projects",
		labelSourceRepo:   "sujaykumarsuman.github.io",
		labelSourceSubdir: "projects",
	}, parseImage("ghcr.io/sujaykumarsuman/projects-hub:0.1.2"))
	if proj.Group != "projects" || proj.Repo != "sujaykumarsuman.github.io" || proj.Subdir != "projects" {
		t.Errorf("projects source override wrong: %+v", proj)
	}
	if l := sourceLink(proj); l.URL != "https://github.com/sujaykumarsuman/sujaykumarsuman.github.io/tree/main/projects" {
		t.Errorf("projects source link wrong: %q", l.URL)
	}

	// An unlabeled workload falls back to the image identity (prior behaviour).
	fb := co.resolveSource(nil, parseImage("ghcr.io/sujaykumarsuman/airlift:1.0.1"))
	if fb.Group != "airlift" || fb.Repo != "airlift" {
		t.Errorf("unlabeled fallback wrong: %+v", fb)
	}
}

func TestParsePGHost(t *testing.T) {
	cases := []struct{ in, svc, ns, cluster string }{
		{"projects-pgstore-rw.databases.svc.cluster.local", "projects-pgstore-rw", "databases", "projects-pgstore"},
		{"projects-pgstore-ro.databases.svc.cluster.local", "projects-pgstore-ro", "databases", "projects-pgstore"},
		{"projects-pgstore-r.databases", "projects-pgstore-r", "databases", "projects-pgstore"},
		{"pg-rw", "pg-rw", "", "pg"},
		{"plainhost", "plainhost", "", "plainhost"},
	}
	for _, c := range cases {
		svc, ns, cluster := parsePGHost(c.in)
		if svc != c.svc || ns != c.ns || cluster != c.cluster {
			t.Errorf("parsePGHost(%q) = svc=%q ns=%q cluster=%q, want %q/%q/%q", c.in, svc, ns, cluster, c.svc, c.ns, c.cluster)
		}
	}
}
