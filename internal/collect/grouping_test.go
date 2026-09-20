package collect

import "testing"

// A multi-component app's images all resolve to one source repo, so the Sources
// lane collapses them to a single card with the correct link.
func TestSourceRepoGrouping(t *testing.T) {
	cases := []struct{ image, wantRepo string }{
		{"ghcr.io/sujaykumarsuman/xlearn-gateway:0.1.7", "xlearn"},
		{"ghcr.io/sujaykumarsuman/xlearn-identity:0.1.7", "xlearn"},
		{"ghcr.io/sujaykumarsuman/airlift:1.0.1", "airlift"}, // single-component: unchanged
	}
	for _, c := range cases {
		src := sourceRepo(parseImage(c.image))
		if src.Owner != "sujaykumarsuman" || src.Repo != c.wantRepo {
			t.Errorf("sourceRepo(%q) = %s/%s, want sujaykumarsuman/%s", c.image, src.Owner, src.Repo, c.wantRepo)
		}
	}
	// The two xlearn images must map to the SAME source-node id (so they dedupe).
	g := sourceRepo(parseImage("ghcr.io/sujaykumarsuman/xlearn-gateway:0.1.7"))
	i := sourceRepo(parseImage("ghcr.io/sujaykumarsuman/xlearn-identity:0.1.7"))
	if g != i {
		t.Errorf("xlearn images must share a source repo: %+v != %+v", g, i)
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
