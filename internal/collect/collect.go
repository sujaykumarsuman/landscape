// Package collect reads the cluster (read-only) and builds the landscape graph:
// your repos → build → Flux → the k3s workloads, with deep-links and status.
package collect

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/sujaykumarsuman/landscape/internal/kube"
	"github.com/sujaykumarsuman/landscape/internal/model"
)

type Collector struct {
	c           *kube.Clients
	githubOwner string
}

func New(c *kube.Clients, githubOwner string) *Collector {
	return &Collector{c: c, githubOwner: githubOwner}
}

var (
	gvrHelmRelease   = schema.GroupVersionResource{Group: "helm.toolkit.fluxcd.io", Version: "v2", Resource: "helmreleases"}
	gvrKustomization = schema.GroupVersionResource{Group: "kustomize.toolkit.fluxcd.io", Version: "v1", Resource: "kustomizations"}
	gvrGitRepo       = schema.GroupVersionResource{Group: "source.toolkit.fluxcd.io", Version: "v1", Resource: "gitrepositories"}
	gvrImageRepo     = schema.GroupVersionResource{Group: "image.toolkit.fluxcd.io", Version: "v1", Resource: "imagerepositories"}
	gvrIngressRoute  = schema.GroupVersionResource{Group: "traefik.io", Version: "v1alpha1", Resource: "ingressroutes"}
)

// kustNote is a one-line description of what each Flux Kustomization applies, for
// the map hover card; unknown names fall back to a generic line.
var kustNote = map[string]string{
	"flux-system":       "Flux's own components and the sync of the infra repo (bootstrap).",
	"infra-controllers": "Cluster controllers — cert-manager and its Helm repository.",
	"infra-configs":     "Cluster config — Let's Encrypt issuers, the shared TLS store, Traefik config, namespaces.",
	"apps":              "The application HelmReleases (airlift, landscape, projects-hub) via the shared chart.",
}

// Graph builds the full landscape.
func (co *Collector) Graph(ctx context.Context) (*model.Graph, error) {
	g := &model.Graph{UpdatedAt: time.Now()}
	nodes := map[string]*model.Node{}
	add := func(n model.Node) *model.Node {
		if ex, ok := nodes[n.ID]; ok {
			return ex
		}
		cp := n
		nodes[n.ID] = &cp
		return &cp
	}
	edge := func(from, to, kind, app string) {
		if from == "" || to == "" {
			return
		}
		g.Edges = append(g.Edges, model.Edge{From: from, To: to, Kind: kind, App: app})
	}

	// --- cluster / node header ---
	if nl, err := co.c.Typed.CoreV1().Nodes().List(ctx, metav1.ListOptions{}); err == nil && len(nl.Items) > 0 {
		g.Cluster.Node = nl.Items[0].Name
		g.Cluster.Version = nl.Items[0].Status.NodeInfo.KubeletVersion
	}
	if nsl, err := co.c.Typed.CoreV1().Namespaces().List(ctx, metav1.ListOptions{}); err == nil {
		g.Cluster.Namespaces = len(nsl.Items)
	}

	// --- GitOps source (infra repo) from the flux-system GitRepository ---
	infraOwner, infraRepo, infraBranch := co.infraRepo(ctx)
	if infraOwner != "" {
		add(model.Node{ID: "repo/" + infraRepo, Kind: "repo", Name: infraOwner + "/" + infraRepo, Layer: model.LayerSource, Owner: true,
			Status: "ok", Summary: "GitOps root — Flux reconciles the cluster from here.",
			Links: []model.Link{{Type: "source", URL: "https://github.com/" + infraOwner + "/" + infraRepo, Label: infraOwner + "/" + infraRepo}}})
	}
	// reusable CI repo (.github), by convention
	if co.githubOwner != "" {
		add(model.Node{ID: "repo/.github", Kind: "repo", Name: co.githubOwner + "/.github", Layer: model.LayerSource, Owner: true,
			Status: "ok", Summary: "Reusable build-push workflow shared by every project.",
			Links: []model.Link{{Type: "source", URL: "https://github.com/" + co.githubOwner + "/.github", Label: ".github"},
				{Type: "workflow", URL: "https://github.com/" + co.githubOwner + "/.github/blob/main/.github/workflows/build-push.yml", Label: "build-push.yml"}}})
	}
	// shared build node
	actions := model.Node{ID: "actions", Kind: "actions", Name: "GitHub Actions", Layer: model.LayerBuild, Owner: false, Status: "ok",
		Summary: "Builds each project's image and pushes it to GHCR on a release tag / push, via the reusable build-push workflow."}
	if co.githubOwner != "" {
		actions.Links = []model.Link{{Type: "workflow", URL: "https://github.com/" + co.githubOwner + "/.github/blob/main/.github/workflows/build-push.yml", Label: "build-push.yml"}}
	}
	add(actions)

	// --- Flux objects ---
	fluxReady := true
	add(model.Node{ID: "flux", Kind: "flux", Name: "Flux", Layer: model.LayerGitOps, Owner: false, Status: "ok",
		Summary: "Pulls this repo and reconciles the cluster to match (source, kustomize, helm, image controllers).",
		Links:   []model.Link{infraToolDocs["flux"]}})
	if infraOwner != "" {
		edge("repo/"+infraRepo, "flux", "source", "")
	}
	if ksts, err := co.list(ctx, gvrKustomization); err == nil {
		for _, k := range ksts {
			name := k.GetName()
			st, msg := readReady(k)
			if st != "ok" {
				fluxReady = false
			}
			summary := kustNote[name]
			if summary == "" {
				summary = "Flux Kustomization — applies a folder of the infra repo."
			}
			add(model.Node{ID: "kust/" + name, Kind: "kustomization", Name: name, Namespace: k.GetNamespace(), Layer: model.LayerGitOps,
				Owner: false, Status: st, StatusText: msg, Summary: summary})
			edge("flux", "kust/"+name, "owns", "")
		}
	} else {
		g.Warnings = append(g.Warnings, "flux kustomizations: "+err.Error())
	}
	g.Cluster.FluxReady = fluxReady
	if fluxReady {
		g.Cluster.FluxMsg = "reconciled"
	} else {
		g.Cluster.FluxMsg = "attention"
	}
	// image-automation (the auto-bump loop) — presence of ImageRepositories implies it
	if iras, err := co.list(ctx, gvrImageRepo); err == nil && len(iras) > 0 {
		add(model.Node{ID: "imgauto", Kind: "controller", Name: "image-automation", Namespace: "flux-system", Layer: model.LayerGitOps,
			Owner: false, Status: "ok", Summary: "Watches GHCR for new tags and commits the bump back to the infra repo (auto-deploy)."})
		if infraOwner != "" {
			edge("imgauto", "repo/"+infraRepo, "watch", "") // the write-back loop
		}
	}

	// HelmReleases → keyed by name for linking workloads
	hrByName := map[string]*unstructured.Unstructured{}
	if hrs, err := co.list(ctx, gvrHelmRelease); err == nil {
		for i := range hrs {
			h := hrs[i]
			hrByName[h.GetName()] = &h
		}
	} else {
		g.Warnings = append(g.Warnings, "flux helmreleases: "+err.Error())
	}

	// IngressRoutes → map app (service name) → route path, keyed by target service
	routeForSvc := co.ingressRoutes(ctx)

	// --- workloads (deployments) ---
	deps, err := co.c.Typed.AppsV1().Deployments("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list deployments: %w", err)
	}
	appsSeen := map[string]bool{}
	ownedNS := map[string]bool{}
	for i := range deps.Items {
		d := &deps.Items[i]
		ns := d.Namespace
		name := d.Name
		if len(d.Spec.Template.Spec.Containers) == 0 {
			continue
		}
		img := parseImage(d.Spec.Template.Spec.Containers[0].Image)
		owned := img.Registry == "ghcr.io" && img.Owner == co.githubOwner
		// only surface app-like workloads: owned apps, or Flux-managed ones
		hr := hrByName[name]
		if !owned && hr == nil {
			// still record platform tools that are HelmReleases handled below; skip bare system deployments
			continue
		}
		status := "progressing"
		want := int32(1)
		if d.Spec.Replicas != nil {
			want = *d.Spec.Replicas
		}
		if d.Status.AvailableReplicas >= want && want > 0 {
			status = "ok"
		}
		ready := fmt.Sprintf("%d/%d", d.Status.AvailableReplicas, want)

		appID := "app/" + ns + "/" + name
		if owned {
			appsSeen[name] = true // "apps" counts your applications, not platform tools
		}
		appNode := model.Node{
			ID: appID, Kind: "app", Name: name, Namespace: ns, Layer: model.LayerCluster, App: name, Owner: owned,
			Status: status, StatusText: "Deployment " + ready,
			Meta: map[string]string{"image": img.String(), "replicas": ready, "strategy": string(d.Spec.Strategy.Type)},
		}
		if owned {
			ownedNS[ns] = true
			tag := img.Tag
			si := co.resolveSource(d.Labels, img)
			appNode.Summary = fmt.Sprintf("Deployment %s · image %s · deployed by Flux from %s/apps/%s.yaml.", ready, img.String(), infraRepo, name)
			appNode.Links = ghLinks(img, si, infraOwner, infraRepo, infraBranch, name)
			if r, ok := routeForSvc[name]; ok {
				appNode.Meta["route"] = r
			}
			// Source node keyed off the app group (app.kubernetes.io/part-of), not the image name,
			// so a multi-component app (e.g. xlearn → gateway + identity, both from
			// the xlearn repo) collapses to ONE source card with the correct link;
			// the image + build nodes stay per-component.
			srcID := "repo/" + si.Owner + "/" + si.Group
			srcNode := add(model.Node{ID: srcID, Kind: "repo", Name: si.Owner + "/" + si.Group, Layer: model.LayerSource, Owner: true,
				Status: "ok", Summary: "Application source.",
				Links: []model.Link{sourceLink(si)}})
			// Components are the workload NAMES (xlearn-gateway, …), not the
			// app.kubernetes.io/component label: the frontend correlates each
			// image/app card to its source group by matching these against the
			// card's data-app (the service name), so the vocabularies must match.
			srcNode.Components = append(srcNode.Components, name) // accumulate across components
			add(model.Node{ID: "image/" + img.Repo, Kind: "image", Name: img.Repo + ":" + tag, Namespace: "ghcr.io", Layer: model.LayerBuild, App: name, Owner: true,
				Status: "ok", Summary: "Container image on GHCR (public).",
				Links: []model.Link{{Type: "image", URL: "https://github.com/" + si.Owner + "/" + si.Repo + "/pkgs/container/" + img.Repo, Label: "ghcr · " + img.Repo}}})
			edge(srcID, "actions", "flow", name)
			edge("actions", "image/"+img.Repo, "flow", name)
			edge("image/"+img.Repo, appID, "deploy", name)
			edge("image/"+img.Repo, "imgauto", "watch", name)
			if _, ok := routeForSvc[name]; ok {
				edge("traefik", appID, "route", name)
			}
		} else {
			appNode.Kind = "infraTool"
			appNode.Summary = "Platform component managed by Flux (" + img.String() + ")."
			if doc, ok := infraToolDocs[name]; ok {
				appNode.Links = []model.Link{doc}
			}
		}
		add(appNode)

		// HelmRelease node + edges
		if hr != nil {
			st, msg := readReady(*hr)
			hid := "hr/" + name
			add(model.Node{ID: hid, Kind: "helmRelease", Name: name, Namespace: hr.GetNamespace(), Layer: model.LayerGitOps, App: name, Owner: owned,
				Status: st, StatusText: msg, Summary: "Flux HelmRelease — renders the shared chart and installs it."})
			edge("kust/apps", hid, "deploy", name)
			edge(hid, appID, "deploy", name)
		}
	}

	// Finalize app-source nodes. The Sources lane lists your apps by app name:
	//   - single-component apps show the app/image name (e.g. projects-hub), even
	//     when the source repo differs (projects-hub is built from
	//     sujaykumarsuman.github.io) — the link still resolves to the real repo.
	//   - multi-component apps keep the shared repo name (e.g. xlearn) and list the
	//     components they build.
	for _, n := range nodes {
		if n.Kind != "repo" || !n.Owner || len(n.Components) == 0 {
			continue
		}
		if len(n.Components) <= 1 {
			continue
		}
		sort.Strings(n.Components)
		n.Summary = fmt.Sprintf("Source repo — builds %d components: %s.", len(n.Components), strings.Join(n.Components, ", "))
	}

	// Traefik ingress node (edge of the cluster)
	add(model.Node{ID: "traefik", Kind: "ingress", Name: "Traefik", Namespace: "kube-system", Layer: model.LayerCluster, Owner: false,
		Status: "ok", Summary: "Ingress — terminates TLS (cert-manager) and routes by path to each app.",
		Links: []model.Link{infraToolDocs["traefik"]}})

	// pod readiness totals — completed Job pods (e.g. k3s helm-install-traefik)
	// are terminal, so keep them out of the running total and count them apart.
	if pl, err := co.c.Typed.CoreV1().Pods("").List(ctx, metav1.ListOptions{}); err == nil {
		for i := range pl.Items {
			p := &pl.Items[i]
			if p.Status.Phase == "Succeeded" {
				g.Cluster.PodsCompleted++
				continue
			}
			g.Cluster.PodsTotal++
			if p.Status.Phase == "Running" {
				for _, c := range p.Status.Conditions {
					if c.Type == "Ready" && c.Status == "True" {
						g.Cluster.PodsReady++
						break
					}
				}
			}
		}
	}
	g.Cluster.Apps = len(appsSeen)

	// cluster header extras for the boxed map (namespaces, gitops, capacity)
	co.clusterRollup(ctx, g, ownedNS)

	// flatten + stable order
	for _, n := range nodes {
		g.Nodes = append(g.Nodes, *n)
	}
	sort.Slice(g.Nodes, func(i, j int) bool {
		if g.Nodes[i].Layer != g.Nodes[j].Layer {
			return layerRank(g.Nodes[i].Layer) < layerRank(g.Nodes[j].Layer)
		}
		return g.Nodes[i].ID < g.Nodes[j].ID
	})
	return g, nil
}

func layerRank(l model.Layer) int {
	switch l {
	case model.LayerSource:
		return 0
	case model.LayerBuild:
		return 1
	case model.LayerGitOps:
		return 2
	default:
		return 3
	}
}

// infraRepo reads the flux-system GitRepository to learn the infra repo.
func (co *Collector) infraRepo(ctx context.Context) (owner, repo, branch string) {
	owner, repo, branch = co.githubOwner, "infra", "main"
	grs, err := co.list(ctx, gvrGitRepo)
	if err != nil {
		return
	}
	for _, gr := range grs {
		url, _, _ := unstructured.NestedString(gr.Object, "spec", "url")
		if o, r, ok := parseGitURL(url); ok {
			owner, repo = o, r
			if b, _, _ := unstructured.NestedString(gr.Object, "spec", "ref", "branch"); b != "" {
				branch = b
			}
			if gr.GetName() == "flux-system" {
				return // prefer the bootstrap source
			}
		}
	}
	return
}

// ingressRoutes maps a target service name → the first path prefix routing to it.
func (co *Collector) ingressRoutes(ctx context.Context) map[string]string {
	out := map[string]string{}
	irs, err := co.list(ctx, gvrIngressRoute)
	if err != nil {
		return out
	}
	for _, ir := range irs {
		routes, _, _ := unstructured.NestedSlice(ir.Object, "spec", "routes")
		for _, r := range routes {
			rm, ok := r.(map[string]any)
			if !ok {
				continue
			}
			match, _ := rm["match"].(string)
			path := extractPathPrefix(match)
			svcs, _, _ := unstructured.NestedSlice(rm, "services")
			for _, s := range svcs {
				if sm, ok := s.(map[string]any); ok {
					if nm, _ := sm["name"].(string); nm != "" {
						if _, exists := out[nm]; !exists {
							out[nm] = path
						}
					}
				}
			}
		}
	}
	return out
}

func extractPathPrefix(match string) string {
	// match like: Host(`h`) && PathPrefix(`/airlift`)
	i := strings.Index(match, "PathPrefix(`")
	if i < 0 {
		return "/"
	}
	rest := match[i+len("PathPrefix(`"):]
	if j := strings.IndexByte(rest, '`'); j >= 0 {
		return rest[:j]
	}
	return "/"
}

func (co *Collector) list(ctx context.Context, gvr schema.GroupVersionResource) ([]unstructured.Unstructured, error) {
	l, err := co.c.Dynamic.Resource(gvr).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	return l.Items, nil
}

func readReady(obj unstructured.Unstructured) (status, msg string) {
	conds, _, _ := unstructured.NestedSlice(obj.Object, "status", "conditions")
	for _, c := range conds {
		cm, ok := c.(map[string]any)
		if !ok {
			continue
		}
		if t, _ := cm["type"].(string); t == "Ready" {
			s, _ := cm["status"].(string)
			m, _ := cm["message"].(string)
			switch s {
			case "True":
				return "ok", m
			case "False":
				return "failed", m
			default:
				return "progressing", m
			}
		}
	}
	return "unknown", ""
}
