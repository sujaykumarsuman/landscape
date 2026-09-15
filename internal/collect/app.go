package collect

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/sujaykumarsuman/landscape/internal/model"
)

// AppDetail builds the full k8s component graph for one app. ConfigMap/Secret/PVC
// NAMES are derived from the Deployment pod spec only — contents are never read.
func (co *Collector) AppDetail(ctx context.Context, name string) (*model.AppDetail, error) {
	deps, err := co.c.Typed.AppsV1().Deployments("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	di := -1
	for i := range deps.Items {
		if deps.Items[i].Name == name {
			di = i
			break
		}
	}
	if di < 0 {
		return nil, fmt.Errorf("app %q not found", name)
	}
	d := &deps.Items[di]
	ns := d.Namespace
	det := &model.AppDetail{Name: name, Namespace: ns, UpdatedAt: time.Now(), GitOps: model.AppGitOps{}}

	infraOwner, infraRepo, infraBranch := co.infraRepo(ctx)
	var img imageRef
	if len(d.Spec.Template.Spec.Containers) > 0 {
		img = parseImage(d.Spec.Template.Spec.Containers[0].Image)
	}
	det.Image = img.String()
	det.Owner = img.Registry == "ghcr.io" && img.Owner == co.githubOwner
	if det.Owner {
		det.SourceRepo = img.Owner + "/" + img.Repo
		det.Version = img.Tag
		det.PublicURL = "" // filled by server from route if desired
		det.Links = ghLinks(img, infraOwner, infraRepo, infraBranch, name)
	}

	// --- deployment detail ---
	want := int32(1)
	if d.Spec.Replicas != nil {
		want = *d.Spec.Replicas
	}
	dd := &model.DeploymentDetail{
		Name: name, Replicas: want, Available: d.Status.AvailableReplicas,
		Ready:    fmt.Sprintf("%d/%d", d.Status.AvailableReplicas, want),
		Strategy: string(d.Spec.Strategy.Type), Image: img.String(),
		AgeSeconds: int64(time.Since(d.CreationTimestamp.Time).Seconds()),
	}
	if len(d.Spec.Template.Spec.Containers) > 0 {
		res := d.Spec.Template.Spec.Containers[0].Resources
		cr := res.Requests["cpu"]
		mr := res.Requests["memory"]
		cl := res.Limits["cpu"]
		ml := res.Limits["memory"]
		dd.CPUReq, dd.MemReq, dd.CPULimit, dd.MemLimit = cr.MilliValue(), mr.Value(), cl.MilliValue(), ml.Value()
	}
	det.Deployment = dd
	det.Status = "progressing"
	if d.Status.AvailableReplicas >= want && want > 0 {
		det.Status = "ok"
	}
	det.StatusText = "Deployment " + dd.Ready

	// --- config/secret/pvc names from the pod spec (never read contents) ---
	appsUsesSOPS := co.kustomizationUsesSOPS(ctx)
	cms := map[string]string{}
	secs := map[string]string{}
	pvcs := map[string]string{}
	ps := d.Spec.Template.Spec
	for _, c := range ps.Containers {
		for _, ef := range c.EnvFrom {
			if ef.ConfigMapRef != nil {
				cms[ef.ConfigMapRef.Name] = "envFrom"
			}
			if ef.SecretRef != nil {
				secs[ef.SecretRef.Name] = "envFrom"
			}
		}
		for _, e := range c.Env {
			if e.ValueFrom != nil && e.ValueFrom.ConfigMapKeyRef != nil {
				cms[e.ValueFrom.ConfigMapKeyRef.Name] = "env:" + e.Name
			}
			if e.ValueFrom != nil && e.ValueFrom.SecretKeyRef != nil {
				secs[e.ValueFrom.SecretKeyRef.Name] = "env:" + e.Name
			}
		}
	}
	for _, v := range ps.Volumes {
		switch {
		case v.ConfigMap != nil:
			cms[v.ConfigMap.Name] = "volume"
		case v.Secret != nil:
			secs[v.Secret.SecretName] = "volume"
		case v.PersistentVolumeClaim != nil:
			pvcs[v.PersistentVolumeClaim.ClaimName] = "volume"
		}
	}
	for _, ips := range ps.ImagePullSecrets {
		secs[ips.Name] = "imagePullSecret"
	}
	det.ConfigMaps = refList(cms, false)
	det.Secrets = refList(secs, appsUsesSOPS)
	det.PVCs = refList(pvcs, false)
	// PVC size/access enrichment (optional; safe read)
	for i := range det.PVCs {
		if pvc, err := co.c.Typed.CoreV1().PersistentVolumeClaims(ns).Get(ctx, det.PVCs[i].Name, metav1.GetOptions{}); err == nil {
			sz := pvc.Spec.Resources.Requests["storage"]
			am := ""
			if len(pvc.Spec.AccessModes) > 0 {
				am = string(pvc.Spec.AccessModes[0])
			}
			det.PVCs[i].Detail = strings.TrimSpace(sz.String() + " · " + am)
		}
	}

	// --- pods (filter by instance label) + metrics ---
	usage := co.podUsage(ctx, ns)
	restarts := 0
	rsName := ""
	if pl, err := co.c.Typed.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{LabelSelector: "app.kubernetes.io/instance=" + name}); err == nil {
		for i := range pl.Items {
			p := &pl.Items[i]
			ready := false
			for _, c := range p.Status.Conditions {
				if c.Type == "Ready" && c.Status == "True" {
					ready = true
				}
			}
			rc := 0
			for _, cs := range p.Status.ContainerStatuses {
				rc += int(cs.RestartCount)
			}
			restarts += rc
			for _, o := range p.OwnerReferences {
				if o.Kind == "ReplicaSet" {
					rsName = o.Name
				}
			}
			u := usage[p.Name]
			det.Pods = append(det.Pods, model.PodDetail{
				Name: p.Name, Phase: string(p.Status.Phase), Ready: ready, Restarts: rc,
				Node: p.Spec.NodeName, CPUMilli: u.cpu, MemBytes: u.mem,
			})
		}
	}
	dd.Restarts = restarts
	if rsName != "" {
		rsd := &model.ReplicaSetDetail{Name: rsName}
		if rs, err := co.c.Typed.AppsV1().ReplicaSets(ns).Get(ctx, rsName, metav1.GetOptions{}); err == nil {
			rsd.Revision = rs.Annotations["deployment.kubernetes.io/revision"]
		}
		det.ReplicaSet = rsd
	}

	// --- service (named after the app by the shared chart) ---
	if svc, err := co.c.Typed.CoreV1().Services(ns).Get(ctx, name, metav1.GetOptions{}); err == nil {
		sd := &model.ServiceDetail{Name: svc.Name, Type: string(svc.Spec.Type)}
		if len(svc.Spec.Ports) > 0 {
			sd.Port = svc.Spec.Ports[0].Port
			sd.TargetPort = svc.Spec.Ports[0].TargetPort.String()
		}
		det.Service = sd
	}

	// --- ingress route (enriched) ---
	det.Ingress = co.ingressDetail(ctx, name)

	// --- gitops: helmrelease, kustomization apps, image-automation ---
	if hr, err := co.c.Dynamic.Resource(gvrHelmRelease).Namespace(ns).Get(ctx, name, metav1.GetOptions{}); err == nil {
		st, _ := readReady(*hr)
		ago := readyAgo(*hr)
		chart, _, _ := unstructured.NestedString(hr.Object, "spec", "chart", "spec", "chart")
		ver, _, _ := unstructured.NestedString(hr.Object, "spec", "chart", "spec", "version")
		chartLabel := strings.TrimPrefix(chart, "./charts/")
		if ver != "" && ver != "*" {
			chartLabel += "@" + ver
		}
		det.GitOps.HelmRelease = &model.HRDetail{Name: name, Chart: chartLabel, Ready: st, ReconciledAt: ago}
	}
	if k, err := co.c.Dynamic.Resource(gvrKustomization).Namespace("flux-system").Get(ctx, "apps", metav1.GetOptions{}); err == nil {
		st, _ := readReady(*k)
		det.GitOps.Kustomization = &model.KustDetail{Name: "apps", Ready: st, ReconciledAt: readyAgo(*k)}
	}
	if det.Owner && img.Tag != "" {
		if _, err := co.c.Dynamic.Resource(gvrImagePolicy).Namespace("flux-system").Get(ctx, name, metav1.GetOptions{}); err == nil {
			det.GitOps.ImageAutomation = img.Tag + " (auto-bump)"
		}
	}
	return det, nil
}

var gvrImagePolicy = schema.GroupVersionResource{Group: "image.toolkit.fluxcd.io", Version: "v1", Resource: "imagepolicies"}

var platformNote = map[string]string{
	"kube-system":  "traefik · coredns · metrics-server · local-path",
	"flux-system":  "source · kustomize · helm · image controllers",
	"cert-manager": "cert-manager · webhook · cainjector",
}

// clusterRollup fills the cluster header extras the boxed map renders: node
// capacity, per-namespace summaries, and the GitOps controllers/kustomizations.
func (co *Collector) clusterRollup(ctx context.Context, g *model.Graph, ownedNS map[string]bool) {
	// node capacity
	if nl, err := co.c.Typed.CoreV1().Nodes().List(ctx, metav1.ListOptions{}); err == nil && len(nl.Items) > 0 {
		capc := nl.Items[0].Status.Capacity
		c := capc["cpu"]
		mm := capc["memory"]
		if v := c.MilliValue(); v > 0 {
			g.Cluster.NodeCPU = fmt.Sprintf("%d vCPU", (v+999)/1000)
		}
		if v := mm.Value(); v > 0 {
			g.Cluster.NodeMem = fmt.Sprintf("%.1f GiB", float64(v)/(1<<30))
		}
	}

	// per-namespace pod counts → NsSummary
	podByNS := map[string]int{}
	if pl, err := co.c.Typed.CoreV1().Pods("").List(ctx, metav1.ListOptions{}); err == nil {
		for i := range pl.Items {
			podByNS[pl.Items[i].Namespace]++
		}
	}
	if nsl, err := co.c.Typed.CoreV1().Namespaces().List(ctx, metav1.ListOptions{}); err == nil {
		for i := range nsl.Items {
			n := nsl.Items[i].Name
			platform := !ownedNS[n]
			note := platformNote[n]
			if platform && podByNS[n] == 0 && note == "" {
				continue // drop empty system namespaces
			}
			g.Cluster.NsSummary = append(g.Cluster.NsSummary, model.NsSummary{
				Name: n, Platform: platform, Pods: podByNS[n], Note: note,
			})
		}
		sort.Slice(g.Cluster.NsSummary, func(i, j int) bool {
			a, b := g.Cluster.NsSummary[i], g.Cluster.NsSummary[j]
			if a.Platform != b.Platform {
				return !a.Platform // app namespaces first
			}
			return a.Name < b.Name
		})
	}

	// GitOps controllers + version from flux-system deployments
	if fd, err := co.c.Typed.AppsV1().Deployments("flux-system").List(ctx, metav1.ListOptions{}); err == nil {
		for i := range fd.Items {
			d := &fd.Items[i]
			g.Cluster.GitOps.Controllers = append(g.Cluster.GitOps.Controllers, strings.TrimSuffix(d.Name, "-controller"))
			if g.Cluster.GitOps.Version == "" {
				if v := d.Labels["app.kubernetes.io/version"]; v != "" {
					g.Cluster.GitOps.Version = v
				}
			}
		}
		sort.Strings(g.Cluster.GitOps.Controllers)
	}
	// kustomizations + flux recency
	if ksts, err := co.list(ctx, gvrKustomization); err == nil {
		for _, k := range ksts {
			st, _ := readReady(k)
			g.Cluster.GitOps.Kusts = append(g.Cluster.GitOps.Kusts, model.KustDetail{Name: k.GetName(), Ready: st, ReconciledAt: readyAgo(k)})
			if (k.GetName() == "apps" || k.GetName() == "flux-system") && g.Cluster.FluxAgo == "" {
				g.Cluster.FluxAgo = readyAgo(k)
			}
		}
		sort.Slice(g.Cluster.GitOps.Kusts, func(i, j int) bool { return g.Cluster.GitOps.Kusts[i].Name < g.Cluster.GitOps.Kusts[j].Name })
	}
	if iras, err := co.list(ctx, gvrImageRepo); err == nil && len(iras) > 0 {
		g.Cluster.GitOps.AutoBump = true
	}
	// TLS descriptor (cert-manager present)
	if cm, err := co.c.Typed.AppsV1().Deployments("cert-manager").List(ctx, metav1.ListOptions{}); err == nil && len(cm.Items) > 0 {
		g.Cluster.TLS = "Let's Encrypt · cert-manager"
	}
}

type podU struct{ cpu, mem int64 }

func (co *Collector) podUsage(ctx context.Context, ns string) map[string]podU {
	out := map[string]podU{}
	if co.c.Metrics == nil {
		return out
	}
	pm, err := co.c.Metrics.MetricsV1beta1().PodMetricses(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		return out
	}
	for i := range pm.Items {
		p := &pm.Items[i]
		var cpu, mem int64
		for _, cn := range p.Containers {
			cq := cn.Usage["cpu"]
			mq := cn.Usage["memory"]
			cpu += cq.MilliValue()
			mem += mq.Value()
		}
		out[p.Name] = podU{cpu, mem}
	}
	return out
}

func (co *Collector) kustomizationUsesSOPS(ctx context.Context) bool {
	k, err := co.c.Dynamic.Resource(gvrKustomization).Namespace("flux-system").Get(ctx, "apps", metav1.GetOptions{})
	if err != nil {
		return false
	}
	prov, _, _ := unstructured.NestedString(k.Object, "spec", "decryption", "provider")
	return prov == "sops"
}

// Traefik returns the ingress and every path it routes to an app (for the
// Traefik routing page).
func (co *Collector) Traefik(ctx context.Context) (*model.TraefikInfo, error) {
	info := &model.TraefikInfo{UpdatedAt: time.Now()}

	// map ns/name → owned (ghcr image of this owner)
	owned := map[string]bool{}
	if deps, err := co.c.Typed.AppsV1().Deployments("").List(ctx, metav1.ListOptions{}); err == nil {
		for i := range deps.Items {
			d := &deps.Items[i]
			if len(d.Spec.Template.Spec.Containers) == 0 {
				continue
			}
			img := parseImage(d.Spec.Template.Spec.Containers[0].Image)
			if img.Registry == "ghcr.io" && img.Owner == co.githubOwner {
				owned[d.Namespace+"/"+d.Name] = true
			}
		}
	}

	irs, err := co.list(ctx, gvrIngressRoute)
	if err != nil {
		return info, err
	}
	eps := map[string]bool{}
	for _, ir := range irs {
		ns := ir.GetNamespace()
		irEps, _, _ := unstructured.NestedStringSlice(ir.Object, "spec", "entryPoints")
		_, hasTLS, _ := unstructured.NestedMap(ir.Object, "spec", "tls")
		routes, _, _ := unstructured.NestedSlice(ir.Object, "spec", "routes")
		for _, r := range routes {
			rm, ok := r.(map[string]any)
			if !ok {
				continue
			}
			// pair name+port+namespace from the same service entry (last named wins)
			var svcName, svcNs, portName string
			var port int32
			svcs, _, _ := unstructured.NestedSlice(rm, "services")
			for _, s := range svcs {
				sm, ok := s.(map[string]any)
				if !ok {
					continue
				}
				n, _ := sm["name"].(string)
				if n == "" {
					continue
				}
				svcName, svcNs, port, portName = n, ns, 0, ""
				if sn, _ := sm["namespace"].(string); sn != "" {
					svcNs = sn // Traefik cross-namespace service ref
				}
				switch p := sm["port"].(type) {
				case int64:
					port = int32(p)
				case float64:
					port = int32(p)
				case string:
					if v, err := strconv.Atoi(p); err == nil {
						port = int32(v)
					} else {
						portName = p // named port
					}
				}
			}
			if svcName == "" {
				continue
			}
			var mws []string
			mwl, _, _ := unstructured.NestedSlice(rm, "middlewares")
			for _, m := range mwl {
				if mm, ok := m.(map[string]any); ok {
					if n, _ := mm["name"].(string); n != "" {
						mws = append(mws, n)
					}
				}
			}
			ep := ""
			if len(irEps) > 0 {
				ep = irEps[0]
			}
			for _, e := range irEps {
				eps[e] = true
			}
			info.Routes = append(info.Routes, model.TraefikRoute{
				Name: ir.GetName(), App: svcName, Namespace: svcNs, Owner: owned[svcNs+"/"+svcName],
				Path: extractPathPrefix(asString(rm["match"])), EntryPoint: ep, TLS: hasTLS,
				Middlewares: mws, Service: svcName, Port: port, PortName: portName,
			})
		}
	}
	for e := range eps {
		info.EntryPoints = append(info.EntryPoints, e)
	}
	sort.Strings(info.EntryPoints)
	sort.Slice(info.Routes, func(i, j int) bool { return info.Routes[i].Path < info.Routes[j].Path })

	if cm, err := co.c.Typed.AppsV1().Deployments("cert-manager").List(ctx, metav1.ListOptions{}); err == nil && len(cm.Items) > 0 {
		info.TLS = "Let's Encrypt · cert-manager"
	}
	if td, err := co.c.Typed.AppsV1().Deployments("kube-system").Get(ctx, "traefik", metav1.GetOptions{}); err == nil {
		if v := td.Labels["app.kubernetes.io/version"]; v != "" {
			info.Version = v
		}
	}
	return info, nil
}

// ingressDetail returns the enriched route targeting the app's service.
func (co *Collector) ingressDetail(ctx context.Context, svcName string) *model.IngressDetail {
	irs, err := co.list(ctx, gvrIngressRoute)
	if err != nil {
		return nil
	}
	for _, ir := range irs {
		eps, _, _ := unstructured.NestedStringSlice(ir.Object, "spec", "entryPoints")
		_, hasTLS, _ := unstructured.NestedMap(ir.Object, "spec", "tls")
		routes, _, _ := unstructured.NestedSlice(ir.Object, "spec", "routes")
		for _, r := range routes {
			rm, ok := r.(map[string]any)
			if !ok {
				continue
			}
			svcs, _, _ := unstructured.NestedSlice(rm, "services")
			hit := false
			for _, s := range svcs {
				if sm, ok := s.(map[string]any); ok {
					if n, _ := sm["name"].(string); n == svcName {
						hit = true
					}
				}
			}
			if !hit {
				continue
			}
			d := &model.IngressDetail{Name: ir.GetName(), Path: extractPathPrefix(asString(rm["match"])), TLS: hasTLS}
			if len(eps) > 0 {
				d.EntryPoint = eps[0]
			}
			mws, _, _ := unstructured.NestedSlice(rm, "middlewares")
			for _, m := range mws {
				if mm, ok := m.(map[string]any); ok {
					if n, _ := mm["name"].(string); n != "" {
						d.Middlewares = append(d.Middlewares, n)
					}
				}
			}
			return d
		}
	}
	return nil
}

func asString(v any) string { s, _ := v.(string); return s }

func refList(m map[string]string, sops bool) []model.RefName {
	var out []model.RefName
	for n, origin := range m {
		if n == "" {
			continue
		}
		out = append(out, model.RefName{Name: n, Origin: origin, SOPS: sops})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// readyAgo returns the Ready condition's lastTransitionTime as a relative string.
func readyAgo(obj unstructured.Unstructured) string {
	conds, _, _ := unstructured.NestedSlice(obj.Object, "status", "conditions")
	for _, c := range conds {
		if cm, ok := c.(map[string]any); ok {
			if t, _ := cm["type"].(string); t == "Ready" {
				return relAge(asString(cm["lastTransitionTime"]))
			}
		}
	}
	return ""
}

func relAge(ts string) string {
	if ts == "" {
		return ""
	}
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return ""
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}
