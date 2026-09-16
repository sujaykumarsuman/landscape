package collect

import (
	"context"
	"sort"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/sujaykumarsuman/landscape/internal/model"
)

// annDefaultClass (+ its legacy beta form) marks the cluster's default
// StorageClass; there is no exported client-go constant for it.
const (
	annDefaultClass     = "storageclass.kubernetes.io/is-default-class"
	annDefaultClassBeta = "storageclass.beta.kubernetes.io/is-default-class"
)

// Storage returns the cluster's StorageClasses and PVCs for the Storage view.
// StorageClasses are best-effort: the storage.k8s.io grant may be absent, so a
// Forbidden there degrades to a warning rather than failing the whole view. PVCs
// are already in the read-only ClusterRole. It always returns a nil error so a
// partial read still renders.
func (co *Collector) Storage(ctx context.Context) (*model.StorageInfo, error) {
	si := &model.StorageInfo{UpdatedAt: time.Now()}

	// (a) StorageClasses — best-effort (RBAC may not be granted yet).
	if scl, err := co.c.Typed.StorageV1().StorageClasses().List(ctx, metav1.ListOptions{}); err == nil {
		for i := range scl.Items {
			sc := &scl.Items[i]
			rp := ""
			if sc.ReclaimPolicy != nil {
				rp = string(*sc.ReclaimPolicy)
			}
			bm := ""
			if sc.VolumeBindingMode != nil {
				bm = string(*sc.VolumeBindingMode)
			}
			def := sc.Annotations[annDefaultClass] == "true" || sc.Annotations[annDefaultClassBeta] == "true"
			si.Classes = append(si.Classes, model.StorageClass{
				Name: sc.Name, Provisioner: sc.Provisioner, Default: def,
				ReclaimPolicy: rp, VolumeBindingMode: bm,
			})
		}
		sort.Slice(si.Classes, func(i, j int) bool { return si.Classes[i].Name < si.Classes[j].Name })
	} else {
		if apierrors.IsForbidden(err) {
			si.ClassesForbidden = true
		}
		si.Warnings = append(si.Warnings, "storageclasses: "+err.Error())
	}

	// (b) PVCs cluster-wide — already granted; still tolerant of a read error.
	if pl, err := co.c.Typed.CoreV1().PersistentVolumeClaims("").List(ctx, metav1.ListOptions{}); err == nil {
		for i := range pl.Items {
			p := &pl.Items[i]
			sc := ""
			if p.Spec.StorageClassName != nil {
				sc = *p.Spec.StorageClassName
			}
			capq := p.Status.Capacity["storage"] // bound actual size
			caps := capq.String()
			if caps == "" || caps == "0" {
				rq := p.Spec.Resources.Requests["storage"]
				caps = rq.String()
			}
			if caps == "0" {
				caps = "" // Quantity zero-value stringifies as "0"; blank so the UI shows "—"
			}
			am := ""
			if len(p.Spec.AccessModes) > 0 {
				am = string(p.Spec.AccessModes[0])
			}
			si.PVCs = append(si.PVCs, model.PVCInfo{
				Namespace: p.Namespace, Name: p.Name, StorageClass: sc,
				Capacity: caps, Status: string(p.Status.Phase),
				Volume: p.Spec.VolumeName, AccessMode: am,
			})
		}
		sort.Slice(si.PVCs, func(i, j int) bool {
			if si.PVCs[i].Namespace != si.PVCs[j].Namespace {
				return si.PVCs[i].Namespace < si.PVCs[j].Namespace
			}
			return si.PVCs[i].Name < si.PVCs[j].Name
		})
	} else {
		si.Warnings = append(si.Warnings, "pvcs: "+err.Error())
	}

	return si, nil
}
