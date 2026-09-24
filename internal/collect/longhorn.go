package collect

import (
	"context"
	"encoding/json"
	"sort"
	"strconv"
	"strings"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/sujaykumarsuman/landscape/internal/model"
)

// Longhorn CRs (longhorn.io/v1beta2), read with get/list only — metadata, spec
// and status; never volume data. The ClusterRole grant lives in the infra repo.
var (
	gvrLHVolume       = lhGVR("volumes")
	gvrLHEngine       = lhGVR("engines")
	gvrLHNode         = lhGVR("nodes")
	gvrLHRecurringJob = lhGVR("recurringjobs")
	gvrLHBackupTarget = lhGVR("backuptargets")
	gvrLHEngineImage  = lhGVR("engineimages")
	gvrLHSnapshot     = lhGVR("snapshots")
	gvrLHSetting      = lhGVR("settings")
)

func lhGVR(res string) schema.GroupVersionResource {
	return schema.GroupVersionResource{Group: "longhorn.io", Version: "v1beta2", Resource: res}
}

// longhornUIService is the Service the Longhorn chart puts in front of the UI;
// the IngressRoute (in longhornNamespace) that targets it gives the UI's path.
const (
	longhornUIService = "longhorn-frontend"
	longhornNamespace = "longhorn-system"
)

// Longhorn builds the Longhorn view. Volumes are the anchor read: without it
// there is nothing to show, so its errors decide the outcome (absent API →
// not installed; Forbidden → the grant is missing). The other reads are
// best-effort and degrade to warnings.
func (co *Collector) Longhorn(ctx context.Context) (*model.LonghornInfo, error) {
	li := &model.LonghornInfo{UpdatedAt: time.Now(), Volumes: []model.LHVolume{}, Nodes: []model.LHNode{}, RecurringJobs: []model.LHRecurringJob{}}
	vols, err := co.list(ctx, gvrLHVolume)
	switch {
	case apierrors.IsNotFound(err):
		return li, nil // longhorn.io not served: Longhorn isn't installed
	case apierrors.IsForbidden(err):
		li.Installed, li.Forbidden = true, true
		return li, nil
	case err != nil:
		return nil, err
	}
	li.Installed = true
	warn := func(what string, err error) { li.Warnings = append(li.Warnings, what+": "+err.Error()) }

	// healthy replicas per volume: RW entries in a *running* engine's replica
	// mode map. A detached volume has no running engine (unknown, not 0), and a
	// volume mid-migration can have two — take the best, never the sum.
	healthy := map[string]int{}
	if engines, err := co.list(ctx, gvrLHEngine); err == nil {
		for _, e := range engines {
			if nestedStr(e.Object, "status", "currentState") != "running" {
				continue
			}
			vol := nestedStr(e.Object, "spec", "volumeName")
			modes, _, _ := unstructured.NestedMap(e.Object, "status", "replicaModeMap")
			rw := 0
			for _, m := range modes {
				if m == "RW" {
					rw++
				}
			}
			if cur, seen := healthy[vol]; !seen || rw > cur {
				healthy[vol] = rw
			}
		}
	} else {
		warn("engines", err)
	}

	snapshots := map[string]int{}
	if snaps, err := co.list(ctx, gvrLHSnapshot); err == nil {
		for _, sn := range snaps {
			vol, _, _ := unstructured.NestedString(sn.Object, "spec", "volume")
			snapshots[vol]++
			li.Summary.Snapshots++
		}
	} else {
		warn("snapshots", err)
	}

	claimApp := co.claimApps(ctx) // "ns/claim" → Deployment (landscape app page)

	for _, v := range vols {
		o := v.Object
		vol := model.LHVolume{
			Name:         v.GetName(),
			Size:         nestedInt(o, "spec", "size"),
			ActualSize:   nestedInt(o, "status", "actualSize"),
			Replicas:     int(nestedInt(o, "spec", "numberOfReplicas")),
			State:        nestedStr(o, "status", "state"),
			Robustness:   nestedStr(o, "status", "robustness"),
			Node:         nestedStr(o, "status", "currentNodeID"),
			DataEngine:   nestedStr(o, "spec", "dataEngine"),
			AccessMode:   nestedStr(o, "spec", "accessMode"),
			PVCNamespace: nestedStr(o, "status", "kubernetesStatus", "namespace"),
			PVC:          nestedStr(o, "status", "kubernetesStatus", "pvcName"),
			LastBackupAt: nestedStr(o, "status", "lastBackupAt"),
			Snapshots:    snapshots[v.GetName()],
			AgeSeconds:   int64(time.Since(v.GetCreationTimestamp().Time).Seconds()),
		}
		vol.HealthyReplicas, vol.ReplicasKnown = healthy[v.GetName()]
		if wls, _, _ := unstructured.NestedSlice(o, "status", "kubernetesStatus", "workloadsStatus"); len(wls) > 0 {
			if wm, ok := wls[0].(map[string]any); ok {
				vol.Workload, vol.WorkloadKind = asString(wm["workloadName"]), asString(wm["workloadType"])
			}
		}
		if app, ok := claimApp[vol.PVCNamespace+"/"+vol.PVC]; ok && vol.PVC != "" {
			vol.App = app
			vol.Workload, vol.WorkloadKind = app, "Deployment" // not the ReplicaSet hash name
		}
		li.Volumes = append(li.Volumes, vol)

		li.Summary.Volumes++
		li.Summary.ActualUsed += vol.ActualSize
		switch vol.Robustness {
		case "healthy":
			li.Summary.Healthy++
		case "degraded":
			li.Summary.Degraded++
		case "faulted":
			li.Summary.Faulted++
		}
		if vol.State == "detached" {
			li.Summary.Detached++
		}
	}
	sort.Slice(li.Volumes, func(i, j int) bool {
		a, b := li.Volumes[i], li.Volumes[j]
		if a.PVCNamespace != b.PVCNamespace {
			return a.PVCNamespace < b.PVCNamespace
		}
		if a.PVC != b.PVC {
			return a.PVC < b.PVC
		}
		return a.Name < b.Name
	})

	li.OverProvisioningPct = 100 // Longhorn's default
	if st, err := co.c.Dynamic.Resource(gvrLHSetting).Namespace(longhornNamespace).Get(ctx, "storage-over-provisioning-percentage", metav1.GetOptions{}); err == nil {
		if pct, err := strconv.Atoi(settingV1(nestedStr(st.Object, "value"))); err == nil && pct > 0 {
			li.OverProvisioningPct = pct
		}
	}
	if nodes, err := co.list(ctx, gvrLHNode); err == nil {
		for _, n := range nodes {
			li.Nodes = append(li.Nodes, lhNode(n, &li.Summary, li.OverProvisioningPct))
		}
		sort.Slice(li.Nodes, func(i, j int) bool { return li.Nodes[i].Name < li.Nodes[j].Name })
	} else {
		warn("nodes", err)
	}

	if jobs, err := co.list(ctx, gvrLHRecurringJob); err == nil {
		for _, j := range jobs {
			o := j.Object
			groups, _, _ := unstructured.NestedStringSlice(o, "spec", "groups")
			task := nestedStr(o, "spec", "task")
			li.RecurringJobs = append(li.RecurringJobs, model.LHRecurringJob{
				Name: j.GetName(), Task: task, Cron: nestedStr(o, "spec", "cron"),
				Retain: int(nestedInt(o, "spec", "retain")), Concurrency: int(nestedInt(o, "spec", "concurrency")),
				Groups: groups,
			})
			if strings.HasPrefix(task, "backup") { // backup, backup-force-create
				li.Summary.BackupJobs++
			}
		}
		sort.Slice(li.RecurringJobs, func(i, j int) bool { return li.RecurringJobs[i].Name < li.RecurringJobs[j].Name })
	} else {
		warn("recurringjobs", err)
	}

	if bts, err := co.list(ctx, gvrLHBackupTarget); err == nil && len(bts) > 0 {
		bt := bts[0]
		for _, b := range bts {
			if b.GetName() == "default" {
				bt = b
			}
		}
		t := &model.LHBackupTarget{
			Name:         bt.GetName(),
			URL:          redactURL(nestedStr(bt.Object, "spec", "backupTargetURL")),
			PollInterval: nestedStr(bt.Object, "spec", "pollInterval"),
			LastSyncedAt: nestedStr(bt.Object, "status", "lastSyncedAt"),
		}
		t.Available, _, _ = unstructured.NestedBool(bt.Object, "status", "available")
		if msg := condMessage(bt.Object, "Unavailable"); msg != "" && !t.Available {
			t.Message = msg
		}
		li.BackupTarget = t
		li.Summary.BackupTargetReady = t.URL != "" && t.Available
	} else if err != nil {
		warn("backuptargets", err)
	}

	if eis, err := co.list(ctx, gvrLHEngineImage); err == nil {
		for _, ei := range eis {
			if v := nestedStr(ei.Object, "status", "version"); v != "" && (li.Version == "" || nestedStr(ei.Object, "status", "state") == "deployed") {
				li.Version = v
			}
		}
	} else {
		warn("engineimages", err)
	}

	li.Summary.BackupsScheduled = li.Summary.BackupTargetReady && li.Summary.BackupJobs > 0

	if st, err := co.c.Dynamic.Resource(gvrLHSetting).Namespace(longhornNamespace).Get(ctx, "default-replica-count", metav1.GetOptions{}); err == nil {
		li.DefaultReplicas = settingV1(nestedStr(st.Object, "value"))
	}

	li.UIPath = co.longhornUIPath(ctx)
	return li, nil
}

// longhornUIPath finds the public path of the Longhorn UI: an IngressRoute in
// longhorn-system that routes a PathPrefix to the longhorn-frontend Service.
// Only that namespace counts (a same-named Service elsewhere must not steer the
// console's links), and the path must be a plain absolute path — it becomes a
// link, so nothing that could turn into another host ("//x", "@x").
func (co *Collector) longhornUIPath(ctx context.Context) string {
	irs, err := co.c.Dynamic.Resource(gvrIngressRoute).Namespace(longhornNamespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return ""
	}
	for _, ir := range irs.Items {
		routes, _, _ := unstructured.NestedSlice(ir.Object, "spec", "routes")
		for _, r := range routes {
			rm, ok := r.(map[string]any)
			if !ok || !strings.Contains(asString(rm["match"]), "PathPrefix(`") {
				continue
			}
			svcs, _, _ := unstructured.NestedSlice(rm, "services")
			for _, sv := range svcs {
				sm, _ := sv.(map[string]any)
				if asString(sm["name"]) != longhornUIService {
					continue
				}
				if p := extractPathPrefix(asString(rm["match"])); safeUIPath(p) {
					return strings.TrimSuffix(p, "/")
				}
			}
		}
	}
	return ""
}

// safeUIPath accepts "/segment[/segment…]" of URL-safe characters only.
func safeUIPath(p string) bool {
	if len(p) < 2 || p[0] != '/' || p[1] == '/' {
		return false
	}
	for _, c := range p {
		ok := c == '/' || c == '-' || c == '_' || c == '.' || c == '~' ||
			(c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
		if !ok {
			return false
		}
	}
	return true
}

// lhNode maps a Longhorn node CR (spec.disks + status.diskStatus) and adds its
// capacity to the summary; a schedulable disk contributes (max − reserved) ×
// over-provisioning % to the scheduling budget.
func lhNode(n unstructured.Unstructured, sum *model.LHSummary, overPct int) model.LHNode {
	o := n.Object
	node := model.LHNode{Name: n.GetName(), Ready: condTrue(o, "Ready"), Schedulable: condTrue(o, "Schedulable")}
	sum.Nodes++
	if node.Ready {
		sum.NodesReady++
	}
	if node.Schedulable {
		sum.NodesSchedulable++
	}
	specDisks, _, _ := unstructured.NestedMap(o, "spec", "disks")
	statusDisks, _, _ := unstructured.NestedMap(o, "status", "diskStatus")
	for name, raw := range statusDisks {
		ds, _ := raw.(map[string]any)
		sd, _ := specDisks[name].(map[string]any)
		d := model.LHDisk{
			Name:      name,
			Path:      asString(sd["path"]),
			Type:      asString(ds["diskType"]),
			Max:       numInt(ds["storageMaximum"]),
			Available: numInt(ds["storageAvailable"]),
			Scheduled: numInt(ds["storageScheduled"]),
			Reserved:  numInt(sd["storageReserved"]),
		}
		if reps, ok := ds["scheduledReplica"].(map[string]any); ok {
			d.Replicas = len(reps)
		}
		d.Ready = condTrue(ds, "Ready")
		d.Schedulable = condTrue(ds, "Schedulable") && asBoolDefault(sd["allowScheduling"], true)
		if d.Schedulable && node.Schedulable {
			d.SchedulableMax = max(0, d.Max-d.Reserved) * int64(overPct) / 100
		}
		node.Disks = append(node.Disks, d) // after every field is set: d is a value
		sum.StorageMax += d.Max
		sum.StorageAvailable += d.Available
		sum.StorageScheduled += d.Scheduled
		sum.StorageReserved += d.Reserved
		sum.StorageSchedulable += d.SchedulableMax
	}
	sort.Slice(node.Disks, func(i, j int) bool { return node.Disks[i].Name < node.Disks[j].Name })
	return node
}

// claimApps maps "namespace/claimName" → the Deployment whose pod template mounts
// that PVC (the landscape app page), from the typed Deployment list.
func (co *Collector) claimApps(ctx context.Context) map[string]string {
	out := map[string]string{}
	deps, err := co.c.Typed.AppsV1().Deployments("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return out
	}
	for i := range deps.Items {
		d := &deps.Items[i]
		for _, v := range d.Spec.Template.Spec.Volumes {
			if v.PersistentVolumeClaim != nil && v.PersistentVolumeClaim.ClaimName != "" {
				out[d.Namespace+"/"+v.PersistentVolumeClaim.ClaimName] = d.Name
			}
		}
	}
	return out
}

func nestedStr(o map[string]any, path ...string) string {
	v, _, _ := unstructured.NestedFieldNoCopy(o, path...)
	switch t := v.(type) {
	case string:
		return t
	case nil:
		return ""
	default:
		b, _ := json.Marshal(t)
		return string(b)
	}
}

func nestedInt(o map[string]any, path ...string) int64 {
	v, _, _ := unstructured.NestedFieldNoCopy(o, path...)
	return numInt(v)
}

// numInt reads an API number that may decode as int64, float64 or a decimal
// string (Longhorn serialises volume sizes as strings).
func numInt(v any) int64 {
	switch t := v.(type) {
	case int64:
		return t
	case int:
		return int64(t)
	case float64:
		return int64(t)
	case string:
		n, _ := strconv.ParseInt(strings.TrimSpace(t), 10, 64)
		return n
	}
	return 0
}

// condTrue reports a status condition of the given type is "True"; o is either
// an object (status.conditions) or a disk status (conditions).
func condTrue(o map[string]any, typ string) bool {
	return condField(o, typ, "status") == "True"
}

func condMessage(o map[string]any, typ string) string { return condField(o, typ, "message") }

func condField(o map[string]any, typ, field string) string {
	conds, ok, _ := unstructured.NestedSlice(o, "status", "conditions")
	if !ok {
		conds, _, _ = unstructured.NestedSlice(o, "conditions")
	}
	for _, c := range conds {
		if cm, ok := c.(map[string]any); ok && asString(cm["type"]) == typ {
			return asString(cm[field])
		}
	}
	return ""
}

// settingV1 reads a Longhorn setting value that may be data-engine keyed
// (`{"v1":"1","v2":"1"}` since 1.8) or a plain string.
func settingV1(v string) string {
	var m map[string]string
	if json.Unmarshal([]byte(v), &m) == nil {
		if s, ok := m["v1"]; ok {
			return s
		}
	}
	return v
}

// redactURL masks inline credentials in a backup-target URL without trusting a
// URL parser (a password may hold "/", "#", "?" or "@"): whatever sits between
// "scheme://" and the LAST "@" is masked when it contains a ":" (user:secret).
// Longhorn's own forms keep their meaning — s3://bucket@region/…,
// azblob://container@endpoint/…, nfs://host:/path — since credentials belong in
// the referenced Secret.
func redactURL(raw string) string {
	i := strings.Index(raw, "://")
	if i < 0 {
		return raw
	}
	rest := raw[i+3:]
	at := strings.LastIndex(rest, "@")
	if at < 0 || !strings.Contains(rest[:at], ":") {
		return raw
	}
	return raw[:i+3] + "***" + rest[at:]
}

func asBoolDefault(v any, def bool) bool {
	if b, ok := v.(bool); ok {
		return b
	}
	return def
}
