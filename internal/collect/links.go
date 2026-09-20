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

// repoRef identifies a GitHub repository, optionally a subdirectory within it.
type repoRef struct {
	Owner  string
	Repo   string
	Subdir string
}

// imageSourceOverrides maps an "owner/repo" image name to the repository its
// image is actually built from, for the cases where the two differ. Most images
// are built from a repo of the same name (ghcr.io/<owner>/<app> ← <owner>/<app>),
// but two cases diverge:
//   - projects-hub is built by the sujaykumarsuman.github.io repo from its
//     projects/ directory — there is no repo named "projects-hub".
//   - multi-component apps ship several images from ONE repo (xlearn →
//     xlearn-gateway + xlearn-identity, both built from the xlearn repo). Mapping
//     each image to the shared repo collapses them to a single source node/link.
var imageSourceOverrides = map[string]repoRef{
	"sujaykumarsuman/projects-hub":    {Owner: "sujaykumarsuman", Repo: "sujaykumarsuman.github.io", Subdir: "projects"},
	"sujaykumarsuman/xlearn-gateway":  {Owner: "sujaykumarsuman", Repo: "xlearn"},
	"sujaykumarsuman/xlearn-identity": {Owner: "sujaykumarsuman", Repo: "xlearn"},
}

// sourceRepo resolves the GitHub source repository (and any subdirectory) for an
// image: an override when one is registered, otherwise the image name itself.
func sourceRepo(img imageRef) repoRef {
	if r, ok := imageSourceOverrides[img.Owner+"/"+img.Repo]; ok {
		return r
	}
	return repoRef{Owner: img.Owner, Repo: img.Repo}
}

// sourceLink is the "source" deep-link for an image, resolved through sourceRepo
// so images built from a different repo (e.g. projects-hub, built from
// sujaykumarsuman.github.io/projects) link to the right place, not a 404.
func sourceLink(img imageRef) model.Link {
	src := sourceRepo(img)
	url := "https://github.com/" + src.Owner + "/" + src.Repo
	label := src.Owner + "/" + src.Repo
	if src.Subdir != "" {
		url += "/tree/main/" + src.Subdir
		label += "/" + src.Subdir
	}
	return model.Link{Type: "source", URL: url, Label: label}
}

// ghLinks builds the deep-links for an owned app image plus its GitOps config.
func ghLinks(img imageRef, infraOwner, infraRepo, infraBranch, app string) []model.Link {
	if infraBranch == "" {
		infraBranch = "main"
	}
	var out []model.Link
	if img.Owner != "" && img.Repo != "" {
		// The source repo is not always the image name (e.g. projects-hub); the
		// GHCR package still resolves under the source repo's path.
		src := sourceRepo(img)
		base := "https://github.com/" + src.Owner + "/" + src.Repo
		out = append(out,
			sourceLink(img),
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
