package collect

import (
	"fmt"
	"strings"

	"github.com/sujaykumarsuman/landscape/internal/model"
)

// imageRef is a parsed container image reference.
type imageRef struct {
	Registry string
	Owner    string
	Repo     string
	Tag      string
}

func (r imageRef) String() string {
	s := r.Registry + "/" + r.Owner + "/" + r.Repo
	if r.Tag != "" {
		s += ":" + r.Tag
	}
	return s
}

// parseImage splits "ghcr.io/owner/repo:tag" (digest stripped) into its parts.
func parseImage(image string) imageRef {
	ref := image
	if i := strings.IndexByte(ref, '@'); i >= 0 { // strip digest
		ref = ref[:i]
	}
	tag := ""
	slash := strings.LastIndexByte(ref, '/')
	if c := strings.LastIndexByte(ref, ':'); c > slash { // tag, not a port
		tag = ref[c+1:]
		ref = ref[:c]
	}
	parts := strings.Split(ref, "/")
	out := imageRef{Tag: tag}
	switch {
	case len(parts) >= 3:
		out.Registry, out.Owner, out.Repo = parts[0], parts[1], strings.Join(parts[2:], "/")
	case len(parts) == 2:
		out.Registry, out.Owner, out.Repo = "docker.io", parts[0], parts[1]
	default:
		out.Registry, out.Repo = "docker.io", parts[0]
	}
	return out
}

// parseGitURL turns a Flux GitRepository URL (https or ssh) into owner/repo.
func parseGitURL(u string) (owner, repo string, ok bool) {
	s := strings.TrimSuffix(strings.TrimSpace(u), ".git")
	for _, p := range []string{"https://github.com/", "http://github.com/", "ssh://git@github.com/", "git@github.com:"} {
		if strings.HasPrefix(s, p) {
			rest := strings.TrimPrefix(s, p)
			parts := strings.SplitN(rest, "/", 3)
			if len(parts) >= 2 && parts[0] != "" && parts[1] != "" {
				return parts[0], parts[1], true
			}
		}
	}
	return "", "", false
}

// ghLinks builds the deep-links for an owned app image plus its GitOps config.
func ghLinks(img imageRef, infraOwner, infraRepo, infraBranch, app string) []model.Link {
	if infraBranch == "" {
		infraBranch = "main"
	}
	var out []model.Link
	if img.Owner != "" && img.Repo != "" {
		base := "https://github.com/" + img.Owner + "/" + img.Repo
		out = append(out,
			model.Link{Type: "source", URL: base, Label: img.Owner + "/" + img.Repo},
			model.Link{Type: "workflow", URL: base + "/blob/main/.github/workflows/deploy.yml", Label: "deploy.yml"},
			model.Link{Type: "image", URL: base + "/pkgs/container/" + img.Repo, Label: "ghcr · " + img.Repo},
		)
	}
	if infraOwner != "" && infraRepo != "" && app != "" {
		out = append(out, model.Link{
			Type:  "config",
			URL:   fmt.Sprintf("https://github.com/%s/%s/blob/%s/apps/%s.yaml", infraOwner, infraRepo, infraBranch, app),
			Label: infraRepo + "/apps/" + app + ".yaml",
		})
	}
	return out
}

// infraToolDocs maps an upstream tool to its documentation.
var infraToolDocs = map[string]model.Link{
	"cert-manager":   {Type: "docs", URL: "https://cert-manager.io/docs/", Label: "cert-manager.io"},
	"traefik":        {Type: "docs", URL: "https://doc.traefik.io/traefik/", Label: "traefik docs"},
	"flux":           {Type: "docs", URL: "https://fluxcd.io/flux/", Label: "fluxcd.io"},
	"k3s":            {Type: "docs", URL: "https://docs.k3s.io/", Label: "k3s docs"},
	"metrics-server": {Type: "docs", URL: "https://github.com/kubernetes-sigs/metrics-server", Label: "metrics-server"},
}
