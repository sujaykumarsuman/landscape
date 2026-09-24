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

// sourceInfo describes where an app's image is built from and how the landscape
// Sources lane groups it — resolved from Kubernetes labels, not a hardcoded map:
//   - app.kubernetes.io/part-of → Group: the app all its components belong to
//     (xlearn's gateway/identity/curriculum → "xlearn"); also the Sources-lane
//     dedup key, so a multi-component app collapses to ONE source card.
//   - sujaykumar.dev/source-repo (+ -subdir) → Repo/Subdir: the GitHub repo the
//     image is built from, for the case where it differs from the app name
//     (projects → sujaykumarsuman.github.io/projects). Defaults to the group.
//
// Unlabeled workloads fall back to the image name, preserving prior behaviour.
type sourceInfo struct {
	Group    string // app group (part-of) — the Sources-lane node + card title
	Owner    string // GitHub owner
	Repo     string // source repo (defaults to Group)
	Subdir   string // source subdirectory, if any
	Workflow string // workflow file that builds the image (defaults to deploy.yml)
}

// Kubernetes labels that drive Sources grouping + source links. part-of is the
// standard app.kubernetes.io group label; source-repo/subdir are project-scoped
// (a label value can't hold "owner/repo", so the owner is the image's owner when
// that is one of yours, else githubOwner). source-workflow names the workflow file
// when a repo doesn't build with the shared deploy.yml (kubescope: release.yml).
// The workloads also carry app.kubernetes.io/component, but landscape correlates
// cards to their group by the workload NAME (data-app), so it isn't read here.
const (
	labelPartOf       = "app.kubernetes.io/part-of"
	labelSourceRepo   = "sujaykumar.dev/source-repo"
	labelSourceSubdir = "sujaykumar.dev/source-subdir"
	labelSourceWF     = "sujaykumar.dev/source-workflow"
)

// resolveSource derives an image's source repo + app group from a workload's
// labels, falling back to the image name when the grouping labels are absent.
func (co *Collector) resolveSource(labels map[string]string, img imageRef) sourceInfo {
	owner := img.Owner
	if !co.owns(owner) {
		owner = co.githubOwner
	}
	if owner == "" {
		owner = img.Owner
	}
	group := labels[labelPartOf]
	if group == "" {
		group = img.Repo // unlabeled: keep the image identity, one card per image
	}
	repo := labels[labelSourceRepo]
	if repo == "" {
		repo = group
	}
	return sourceInfo{Group: group, Owner: owner, Repo: repo, Subdir: labels[labelSourceSubdir], Workflow: labels[labelSourceWF]}
}

// sourceLink is the "source" deep-link for a resolved source (repo + optional
// subdir) so images built from a different repo (e.g. projects, built from
// sujaykumarsuman.github.io/projects) link to the right place, not a 404.
func sourceLink(si sourceInfo) model.Link {
	url := "https://github.com/" + si.Owner + "/" + si.Repo
	label := si.Owner + "/" + si.Repo
	if si.Subdir != "" {
		url += "/tree/main/" + si.Subdir
		label += "/" + si.Subdir
	}
	return model.Link{Type: "source", URL: url, Label: label}
}

// ghLinks builds the deep-links for an owned app image plus its GitOps config,
// using the resolved source (si) for the repo-relative links (source, workflow,
// GHCR package path all resolve under the source repo) and the image name for the
// package itself.
func ghLinks(img imageRef, si sourceInfo, infraOwner, infraRepo, infraBranch, app string) []model.Link {
	if infraBranch == "" {
		infraBranch = "main"
	}
	var out []model.Link
	if si.Owner != "" && si.Repo != "" {
		base := "https://github.com/" + si.Owner + "/" + si.Repo
		wf := si.Workflow
		if wf == "" {
			wf = "deploy.yml"
		}
		out = append(out,
			sourceLink(si),
			model.Link{Type: "workflow", URL: base + "/blob/main/.github/workflows/" + wf, Label: wf},
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
