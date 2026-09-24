package collect

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	typedfake "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"github.com/sujaykumarsuman/landscape/internal/kube"
)

func lhObj(kind, name string, spec, status map[string]any) *unstructured.Unstructured {
	u := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "longhorn.io/v1beta2", "kind": kind,
		"metadata": map[string]any{"name": name, "namespace": "longhorn-system", "creationTimestamp": "2026-09-20T00:00:00Z"},
		"spec":     spec, "status": status,
	}}
	return u
}

func lhCollector(t *testing.T, objs ...runtime.Object) *Collector {
	t.Helper()
	listKinds := map[schema.GroupVersionResource]string{
		gvrLHVolume: "VolumeList", gvrLHEngine: "EngineList", gvrLHNode: "NodeList",
		gvrLHRecurringJob: "RecurringJobList", gvrLHBackupTarget: "BackupTargetList",
		gvrLHEngineImage: "EngineImageList", gvrLHSnapshot: "SnapshotList", gvrLHSetting: "SettingList",
		gvrIngressRoute: "IngressRouteList",
	}
	dyn := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), listKinds, objs...)
	airlift := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "airlift", Namespace: "airlift"},
		Spec: appsv1.DeploymentSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Volumes: []corev1.Volume{{
			Name: "data", VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "airlift"}},
		}}}}},
	}
	return &Collector{c: &kube.Clients{Typed: typedfake.NewClientset(airlift), Dynamic: dyn}, githubOwner: "sujaykumarsuman"}
}

func TestLonghornJoinsVolumesToAppsAndSummarises(t *testing.T) {
	route := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "traefik.io/v1alpha1", "kind": "IngressRoute",
		"metadata": map[string]any{"name": "longhorn-ui", "namespace": "longhorn-system"},
		"spec": map[string]any{"routes": []any{map[string]any{
			"match":    "Host(`projects.sujaykumar.dev`) && PathPrefix(`/longhorn`)",
			"services": []any{map[string]any{"name": "longhorn-frontend", "port": int64(80)}},
		}}},
	}}
	co := lhCollector(t,
		// airlift's volume: mounted by a Deployment → links to its app page
		lhObj("Volume", "pvc-a", map[string]any{"size": "1073741824", "numberOfReplicas": int64(1), "dataEngine": "v1"},
			map[string]any{"state": "attached", "robustness": "healthy", "actualSize": int64(51060736), "currentNodeID": "n1",
				"kubernetesStatus": map[string]any{"namespace": "airlift", "pvcName": "airlift",
					"workloadsStatus": []any{map[string]any{"workloadName": "airlift-6f47bcb8ff", "workloadType": "ReplicaSet"}}}}),
		// NATS: a StatefulSet, no landscape app page; one of two replicas rebuilding
		lhObj("Volume", "pvc-b", map[string]any{"size": "5368709120", "numberOfReplicas": int64(2)},
			map[string]any{"state": "attached", "robustness": "degraded", "actualSize": int64(1000),
				"kubernetesStatus": map[string]any{"namespace": "messaging", "pvcName": "nats-js-nats-0",
					"workloadsStatus": []any{map[string]any{"workloadName": "nats", "workloadType": "StatefulSet"}}}}),
		lhObj("Volume", "pvc-c", map[string]any{"size": "10", "numberOfReplicas": int64(1)},
			map[string]any{"state": "detached", "robustness": "unknown"}),
		lhObj("Engine", "pvc-a-e", map[string]any{"volumeName": "pvc-a"}, map[string]any{"currentState": "running", "replicaModeMap": map[string]any{"pvc-a-r1": "RW"}}),
		// mid-migration: two running engines for one volume — take the best, never the sum
		lhObj("Engine", "pvc-a-e2", map[string]any{"volumeName": "pvc-a"}, map[string]any{"currentState": "running", "replicaModeMap": map[string]any{"pvc-a-r1": "RW"}}),
		lhObj("Engine", "pvc-b-e", map[string]any{"volumeName": "pvc-b"}, map[string]any{"currentState": "running", "replicaModeMap": map[string]any{"pvc-b-r1": "RW", "pvc-b-r2": "WO"}}),
		// pvc-c is detached: its engine is stopped, so its replicas are unknown (not 0/N)
		lhObj("Engine", "pvc-c-e", map[string]any{"volumeName": "pvc-c"}, map[string]any{"currentState": "stopped", "replicaModeMap": map[string]any{}}),
		lhObj("Snapshot", "snap-1", map[string]any{"volume": "pvc-a"}, map[string]any{}),
		lhObj("Node", "n1", map[string]any{"disks": map[string]any{"d1": map[string]any{"path": "/var/lib/longhorn/", "storageReserved": int64(100)}}},
			map[string]any{
				"conditions": []any{map[string]any{"type": "Ready", "status": "True"}, map[string]any{"type": "Schedulable", "status": "True"}},
				"diskStatus": map[string]any{"d1": map[string]any{"storageMaximum": int64(1000), "storageAvailable": int64(700), "storageScheduled": int64(300),
					"conditions":       []any{map[string]any{"type": "Ready", "status": "True"}, map[string]any{"type": "Schedulable", "status": "True"}},
					"scheduledReplica": map[string]any{"pvc-a-r1": int64(1), "pvc-b-r1": int64(1)}}},
			}),
		lhObj("RecurringJob", "nightly", map[string]any{"task": "snapshot", "cron": "0 3 * * *", "retain": int64(7), "concurrency": int64(1), "groups": []any{"default"}}, map[string]any{}),
		lhObj("BackupTarget", "default", map[string]any{"backupTargetURL": "", "pollInterval": "5m0s"},
			map[string]any{"available": false, "conditions": []any{map[string]any{"type": "Unavailable", "status": "True", "message": "backup target URL is empty"}}}),
		lhObj("EngineImage", "ei-1", map[string]any{"image": "longhornio/longhorn-engine:v1.12.1"}, map[string]any{"version": "v1.12.1", "state": "deployed"}),
		&unstructured.Unstructured{Object: map[string]any{"apiVersion": "longhorn.io/v1beta2", "kind": "Setting",
			"metadata": map[string]any{"name": "default-replica-count", "namespace": "longhorn-system"}, "value": `{"v1":"1","v2":"1"}`}},
		&unstructured.Unstructured{Object: map[string]any{"apiVersion": "longhorn.io/v1beta2", "kind": "Setting",
			"metadata": map[string]any{"name": "storage-over-provisioning-percentage", "namespace": "longhorn-system"}, "value": "200"}},
		route,
		// a same-named Service routed from another namespace must not steer the UI link
		&unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "traefik.io/v1alpha1", "kind": "IngressRoute",
			"metadata": map[string]any{"name": "decoy", "namespace": "aaa-tenant"},
			"spec": map[string]any{"routes": []any{map[string]any{
				"match":    "Host(`projects.sujaykumar.dev`) && PathPrefix(`/decoy`)",
				"services": []any{map[string]any{"name": "longhorn-frontend", "port": int64(80)}},
			}}},
		}},
	)

	li, err := co.Longhorn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !li.Installed || li.Forbidden || li.Version != "v1.12.1" || li.DefaultReplicas != "1" || li.UIPath != "/longhorn" {
		t.Fatalf("header wrong: installed=%v forbidden=%v version=%q replicas=%q ui=%q", li.Installed, li.Forbidden, li.Version, li.DefaultReplicas, li.UIPath)
	}
	if len(li.Volumes) != 3 {
		t.Fatalf("volumes = %d", len(li.Volumes))
	}
	byName := map[string]int{}
	for i, v := range li.Volumes {
		byName[v.Name] = i
	}
	a := li.Volumes[byName["pvc-a"]]
	if a.App != "airlift" || a.Workload != "airlift" || a.WorkloadKind != "Deployment" || a.Size != 1073741824 || a.HealthyReplicas != 1 || !a.ReplicasKnown || a.Snapshots != 1 {
		t.Errorf("airlift volume = %+v", a)
	}
	b := li.Volumes[byName["pvc-b"]]
	if b.App != "" || b.Workload != "nats" || b.WorkloadKind != "StatefulSet" || b.Replicas != 2 || b.HealthyReplicas != 1 || !b.ReplicasKnown {
		t.Errorf("nats volume = %+v", b)
	}
	if c := li.Volumes[byName["pvc-c"]]; c.ReplicasKnown {
		t.Errorf("a detached volume's replicas are unknown, got %+v", c)
	}
	s := li.Summary
	if s.Volumes != 3 || s.Healthy != 1 || s.Degraded != 1 || s.Detached != 1 || s.Snapshots != 1 ||
		s.StorageMax != 1000 || s.StorageAvailable != 700 || s.StorageScheduled != 300 || s.StorageReserved != 100 ||
		s.StorageSchedulable != 1800 || // (1000 − 100) × 200%
		s.Nodes != 1 || s.NodesReady != 1 || s.NodesSchedulable != 1 ||
		s.BackupTargetReady || s.BackupJobs != 0 || s.BackupsScheduled {
		t.Errorf("summary = %+v", s)
	}
	if li.OverProvisioningPct != 200 {
		t.Errorf("over-provisioning = %d", li.OverProvisioningPct)
	}
	if n := li.Nodes; len(n) != 1 || len(n[0].Disks) != 1 || n[0].Disks[0].Replicas != 2 || n[0].Disks[0].Path != "/var/lib/longhorn/" || !n[0].Disks[0].Ready ||
		!n[0].Disks[0].Schedulable || n[0].Disks[0].SchedulableMax != 1800 {
		t.Errorf("nodes = %+v", n)
	}
	if j := li.RecurringJobs; len(j) != 1 || j[0].Task != "snapshot" || j[0].Retain != 7 || len(j[0].Groups) != 1 {
		t.Errorf("jobs = %+v", j)
	}
	if bt := li.BackupTarget; bt == nil || bt.Available || bt.Message != "backup target URL is empty" {
		t.Errorf("backup target = %+v", bt)
	}
}

// The volumes list decides the outcome: an apiserver without the CRDs answers
// NotFound (not installed), a missing grant answers Forbidden (shown as such).
func TestLonghornNotInstalledOrForbidden(t *testing.T) {
	for name, tc := range map[string]struct {
		err                  error
		installed, forbidden bool
	}{
		"not installed": {apierrors.NewNotFound(gvrLHVolume.GroupResource(), ""), false, false},
		"forbidden":     {apierrors.NewForbidden(gvrLHVolume.GroupResource(), "", nil), true, true},
	} {
		co := lhCollector(t)
		fake := co.c.Dynamic.(*dynamicfake.FakeDynamicClient)
		fake.PrependReactor("list", "volumes", func(k8stesting.Action) (bool, runtime.Object, error) { return true, nil, tc.err })
		li, err := co.Longhorn(context.Background())
		if err != nil {
			t.Fatalf("%s: not an error for the view: %v", name, err)
		}
		if li.Installed != tc.installed || li.Forbidden != tc.forbidden {
			t.Errorf("%s: installed=%v forbidden=%v", name, li.Installed, li.Forbidden)
		}
	}
}

// Backups are "scheduled" only with a ready target AND a recurring backup job;
// a configured target alone isn't a backup.
func TestLonghornBackupsScheduled(t *testing.T) {
	target := lhObj("BackupTarget", "default", map[string]any{"backupTargetURL": "s3://bucket@us-east-1/"}, map[string]any{"available": true})
	vol := lhObj("Volume", "pvc-a", map[string]any{"size": "1"}, map[string]any{"state": "attached", "robustness": "healthy"})
	snapJob := lhObj("RecurringJob", "snap", map[string]any{"task": "snapshot", "cron": "0 * * * *"}, map[string]any{})
	backupJob := lhObj("RecurringJob", "nightly-backup", map[string]any{"task": "backup", "cron": "0 3 * * *"}, map[string]any{})

	li, err := lhCollector(t, vol, target, snapJob).Longhorn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if s := li.Summary; !s.BackupTargetReady || s.BackupJobs != 0 || s.BackupsScheduled {
		t.Errorf("target without a backup job: %+v", s)
	}
	li, _ = lhCollector(t, vol, target, snapJob, backupJob).Longhorn(context.Background())
	if s := li.Summary; !s.BackupTargetReady || s.BackupJobs != 1 || !s.BackupsScheduled {
		t.Errorf("target + backup job: %+v", s)
	}
	if li.BackupTarget.URL != "s3://bucket@us-east-1/" {
		t.Errorf("s3 bucket@region must survive redaction: %q", li.BackupTarget.URL)
	}
}

func TestLonghornHelpers(t *testing.T) {
	if numInt("1073741824") != 1073741824 || numInt(int64(5)) != 5 || numInt(float64(7)) != 7 || numInt(nil) != 0 {
		t.Error("numInt")
	}
	if settingV1(`{"v1":"2","v2":"1"}`) != "2" || settingV1("3") != "3" {
		t.Error("settingV1")
	}
	for p, want := range map[string]bool{"/longhorn": true, "/a/b-c_d.e~f": true, "/": false, "//evil.example": false, "/x@evil": false, "/a b": false, "longhorn": false, "": false} {
		if got := safeUIPath(p); got != want {
			t.Errorf("safeUIPath(%q) = %v, want %v", p, got, want)
		}
	}
	for in, want := range map[string]string{
		"s3://bucket@us-east-1/":             "s3://bucket@us-east-1/",
		"azblob://container@blob.example/x/": "azblob://container@blob.example/x/",
		"nfs://nas:/export":                  "nfs://nas:/export",
		"cifs://user@nas/share":              "cifs://user@nas/share",
		"cifs://user:hunter2@nas/share":      "cifs://***@nas/share",
		"cifs://user:1234/x@nas/share":       "cifs://***@nas/share",
		"cifs://user:p@ss#w?rd%zz@nas/share": "cifs://***@nas/share",
		"cifs://user:pa ss@nas":              "cifs://***@nas",
		"":                                   "",
	} {
		if got := redactURL(in); got != want {
			t.Errorf("redactURL(%q) = %q, want %q", in, got, want)
		}
	}
}
