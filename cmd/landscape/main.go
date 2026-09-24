// Command landscape serves a read-only control-plane view of the GitOps landscape
// (repos → build → Flux → k3s) with live metrics, behind an admin-password gate.
package main

import (
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/sujaykumarsuman/landscape/internal/collect"
	"github.com/sujaykumarsuman/landscape/internal/kube"
	"github.com/sujaykumarsuman/landscape/internal/server"
)

// Version is stamped at build time (-ldflags "-X main.Version=vX.Y.Z").
var Version = "dev"

func main() {
	addr := env("LANDSCAPE_LISTEN", "0.0.0.0:8080")
	// comma list: the first owner is primary (shared .github workflows); the rest
	// (e.g. the skriptvalley org) are also "yours" — their images render as apps.
	var owners []string
	for _, o := range strings.Split(env("LANDSCAPE_GITHUB_OWNER", "sujaykumarsuman"), ",") {
		if o = strings.TrimSpace(o); o != "" {
			owners = append(owners, o)
		}
	}
	if len(owners) == 0 {
		owners = []string{"sujaykumarsuman"}
	}
	owner := owners[0]
	pub := os.Getenv("LANDSCAPE_PUBLIC_URL")
	pw := os.Getenv("LANDSCAPE_ADMIN_PASSWORD")
	if pw == "" {
		log.Println("WARNING: LANDSCAPE_ADMIN_PASSWORD is unset — the console will refuse every login")
	}
	if os.Getenv("LANDSCAPE_SESSION_KEY") == "" {
		log.Println("note: LANDSCAPE_SESSION_KEY is unset — sessions are signed from the admin password alone (PBKDF2)")
	}

	clients, err := kube.New()
	if err != nil {
		log.Fatalf("kubernetes clients: %v", err)
	}
	col := collect.New(clients, owners...)
	srv := server.New(col, server.Options{
		Addr: addr, AdminPassword: pw, GithubOwner: owner, PublicURL: pub,
		Version: Version, CacheTTL: 10 * time.Second,
		SessionKey: os.Getenv("LANDSCAPE_SESSION_KEY"),
	})

	hs := &http.Server{Addr: addr, Handler: srv.Handler(), ReadHeaderTimeout: 10 * time.Second}
	log.Printf("landscape %s listening on %s (github owners %q)", Version, addr, owners)
	log.Fatal(hs.ListenAndServe())
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
