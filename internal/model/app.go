package model

import "time"

// AppDetail is the full k8s component graph for one app (the app-detail page).
type AppDetail struct {
	Name       string            `json:"name"`
	Namespace  string            `json:"namespace"`
	Owner      bool              `json:"owner"`
	Status     string            `json:"status"`
	StatusText string            `json:"statusText,omitempty"`
	Version    string            `json:"version,omitempty"`
	PublicURL  string            `json:"publicUrl,omitempty"`
	UpdatedAt  time.Time         `json:"updatedAt"`
	Image      string            `json:"image,omitempty"`
	SourceRepo string            `json:"sourceRepo,omitempty"`
	Deployment *DeploymentDetail `json:"deployment,omitempty"`
	ReplicaSet *ReplicaSetDetail `json:"replicaSet,omitempty"`
	Pods       []PodDetail       `json:"pods,omitempty"`
	Service    *ServiceDetail    `json:"service,omitempty"`
	Ingress    *IngressDetail    `json:"ingress,omitempty"`
	ConfigMaps []RefName         `json:"configMaps,omitempty"`
	Secrets    []RefName         `json:"secrets,omitempty"`
	PVCs       []RefName         `json:"pvcs,omitempty"`
	GitOps     AppGitOps         `json:"gitops"`
	Links      []Link            `json:"links,omitempty"`
	Warnings   []string          `json:"warnings,omitempty"`
}

type DeploymentDetail struct {
	Name       string `json:"name"`
	Ready      string `json:"ready"` // "1/1"
	Replicas   int32  `json:"replicas"`
	Available  int32  `json:"available"`
	Strategy   string `json:"strategy"`
	Image      string `json:"image"`
	AgeSeconds int64  `json:"ageSeconds"`
	Restarts   int    `json:"restarts"`
	CPUReq     int64  `json:"cpuReqMilli"`
	CPULimit   int64  `json:"cpuLimitMilli"`
	MemReq     int64  `json:"memReqBytes"`
	MemLimit   int64  `json:"memLimitBytes"`
}

type ReplicaSetDetail struct {
	Name     string `json:"name"`
	Revision string `json:"revision,omitempty"`
}

type PodDetail struct {
	Name     string `json:"name"`
	Phase    string `json:"phase"`
	Ready    bool   `json:"ready"`
	Restarts int    `json:"restarts"`
	Node     string `json:"node,omitempty"`
	CPUMilli int64  `json:"cpuMilli"`
	MemBytes int64  `json:"memBytes"`
}

type ServiceDetail struct {
	Name       string `json:"name"`
	Type       string `json:"type"`
	Port       int32  `json:"port"`
	TargetPort string `json:"targetPort,omitempty"`
}

type IngressDetail struct {
	Name        string   `json:"name"`
	Path        string   `json:"path"`
	EntryPoint  string   `json:"entryPoint,omitempty"`
	TLS         bool     `json:"tls"`
	Middlewares []string `json:"middlewares,omitempty"`
}

// RefName is a ConfigMap/Secret/PVC referenced by the Deployment (name + where
// it is used). Names come from the pod spec only — contents are never read.
type RefName struct {
	Name   string `json:"name"`
	Origin string `json:"origin,omitempty"` // envFrom | env:VAR | volume:/path | imagePullSecret
	SOPS   bool   `json:"sops,omitempty"`
	Detail string `json:"detail,omitempty"` // e.g. "1Gi · RWO" for PVC when available
}

type AppGitOps struct {
	HelmRelease     *HRDetail   `json:"helmRelease,omitempty"`
	Kustomization   *KustDetail `json:"kustomization,omitempty"`
	ImageAutomation string      `json:"imageAutomation,omitempty"`
}

type HRDetail struct {
	Name         string `json:"name"`
	Chart        string `json:"chart,omitempty"`
	Ready        string `json:"ready"`
	ReconciledAt string `json:"reconciledAt,omitempty"` // relative, e.g. "2m ago"
}

type KustDetail struct {
	Name         string `json:"name"`
	Ready        string `json:"ready"`
	ReconciledAt string `json:"reconciledAt,omitempty"`
	Path         string `json:"path,omitempty"` // the folder it applies, e.g. "apps"
	URL          string `json:"url,omitempty"`  // deep-link to that folder on GitHub
}

// NsSummary is one namespace's roll-up for the cluster box.
type NsSummary struct {
	Name     string `json:"name"`
	Platform bool   `json:"platform"` // true = infra namespace (no owned app)
	Pods     int    `json:"pods"`
	Note     string `json:"note,omitempty"` // e.g. "traefik · coredns · metrics"
}

// GitOpsInfo drives the GitOps lane box.
type GitOpsInfo struct {
	Version     string       `json:"version,omitempty"`
	Controllers []string     `json:"controllers,omitempty"`
	Kusts       []KustDetail `json:"kustomizations,omitempty"`
	AutoBump    bool         `json:"autoBump"`
}

// TraefikInfo drives the Traefik routing page: the ingress and every path it
// routes to an app.
type TraefikInfo struct {
	UpdatedAt   time.Time      `json:"updatedAt"`
	Version     string         `json:"version,omitempty"`
	EntryPoints []string       `json:"entryPoints,omitempty"`
	TLS         string         `json:"tls,omitempty"`
	Routes      []TraefikRoute `json:"routes"`
}

// TraefikRoute is one IngressRoute path → app mapping.
type TraefikRoute struct {
	Name        string   `json:"name"`
	App         string   `json:"app"`
	Namespace   string   `json:"namespace,omitempty"`
	Owner       bool     `json:"owner"`
	Path        string   `json:"path"`
	EntryPoint  string   `json:"entryPoint,omitempty"`
	TLS         bool     `json:"tls"`
	Middlewares []string `json:"middlewares,omitempty"`
	Service     string   `json:"service,omitempty"`
	Port        int32    `json:"port,omitempty"`
	PortName    string   `json:"portName,omitempty"` // named (string) target port
	PublicURL   string   `json:"publicUrl,omitempty"`
}

// StorageInfo drives the Storage view: the cluster's StorageClasses and PVCs.
// StorageClasses are best-effort — the storage.k8s.io grant may be absent — so
// ClassesForbidden/Warnings report a degraded read while PVCs still populate.
type StorageInfo struct {
	UpdatedAt        time.Time      `json:"updatedAt"`
	Classes          []StorageClass `json:"classes,omitempty"`
	ClassesForbidden bool           `json:"classesForbidden,omitempty"`
	PVCs             []PVCInfo      `json:"pvcs"`
	Warnings         []string       `json:"warnings,omitempty"`
}

// StorageClass is one cluster StorageClass. Default is derived from the
// is-default-class annotation, not a struct field.
type StorageClass struct {
	Name              string `json:"name"`
	Provisioner       string `json:"provisioner"`
	Default           bool   `json:"default"`
	ReclaimPolicy     string `json:"reclaimPolicy,omitempty"`
	VolumeBindingMode string `json:"volumeBindingMode,omitempty"`
}

// PVCInfo is one PersistentVolumeClaim across all namespaces. Only metadata/spec/
// status are read — never the bound data.
type PVCInfo struct {
	Namespace    string `json:"namespace"`
	Name         string `json:"name"`
	StorageClass string `json:"storageClass,omitempty"`
	Capacity     string `json:"capacity,omitempty"` // bound size (Status.Capacity), else request
	Status       string `json:"status"`             // Bound|Pending|Lost
	Volume       string `json:"volume,omitempty"`
	AccessMode   string `json:"accessMode,omitempty"`
}
