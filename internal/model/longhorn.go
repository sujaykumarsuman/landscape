package model

import "time"

// LonghornInfo drives the Longhorn view: Longhorn's own CRs (volumes, engines,
// nodes/disks, recurring jobs, backup target, engine image, snapshots, settings)
// read with get/list only — metadata/spec/status, never volume data — joined to
// the PVCs and Deployments that use each volume.
type LonghornInfo struct {
	// Installed is false when the longhorn.io API isn't served (no Longhorn).
	Installed bool `json:"installed"`
	// Forbidden means the console's ClusterRole lacks the longhorn.io read grant.
	Forbidden       bool   `json:"forbidden,omitempty"`
	Version         string `json:"version,omitempty"`         // engine image version, e.g. v1.12.1
	DefaultReplicas string `json:"defaultReplicas,omitempty"` // default-replica-count setting
	// UIPath is the path prefix of the IngressRoute serving the Longhorn UI (the
	// longhorn-frontend Service); UIURL is its public URL (filled by the server).
	UIPath        string           `json:"uiPath,omitempty"`
	UIURL         string           `json:"uiURL,omitempty"`
	Summary       LHSummary        `json:"summary"`
	Volumes       []LHVolume       `json:"volumes"`
	Nodes         []LHNode         `json:"nodes"`
	RecurringJobs []LHRecurringJob `json:"recurringJobs"`
	BackupTarget  *LHBackupTarget  `json:"backupTarget,omitempty"`
	Warnings      []string         `json:"warnings,omitempty"`
	UpdatedAt     time.Time        `json:"updatedAt"`
}

// LHSummary rolls the volumes and disks up for the header tiles.
type LHSummary struct {
	Volumes          int   `json:"volumes"`
	Healthy          int   `json:"healthy"`
	Degraded         int   `json:"degraded"`
	Faulted          int   `json:"faulted"`
	Detached         int   `json:"detached"`
	Nodes            int   `json:"nodes"`
	NodesReady       int   `json:"nodesReady"`
	NodesSchedulable int   `json:"nodesSchedulable"`
	StorageMax       int64 `json:"storageMax"`
	StorageAvailable int64 `json:"storageAvailable"`
	StorageScheduled int64 `json:"storageScheduled"`
	StorageReserved  int64 `json:"storageReserved"`
	ActualUsed       int64 `json:"actualUsed"` // sum of volumes' actual (written) size
	Snapshots        int   `json:"snapshots"`
	BackupsEnabled   bool  `json:"backupsEnabled"`
}

// LHVolume is one Longhorn volume and who uses it.
type LHVolume struct {
	Name            string `json:"name"` // the PV name (pvc-…)
	Size            int64  `json:"size"`
	ActualSize      int64  `json:"actualSize"`
	Replicas        int    `json:"replicas"`        // desired
	HealthyReplicas int    `json:"healthyReplicas"` // RW in the engine's replica map
	State           string `json:"state"`           // attached / detached / …
	Robustness      string `json:"robustness"`      // healthy / degraded / faulted / unknown
	Node            string `json:"node,omitempty"`
	DataEngine      string `json:"dataEngine,omitempty"`
	AccessMode      string `json:"accessMode,omitempty"`
	PVCNamespace    string `json:"pvcNamespace,omitempty"`
	PVC             string `json:"pvc,omitempty"`
	Workload        string `json:"workload,omitempty"`     // e.g. nats, projects-pgstore
	WorkloadKind    string `json:"workloadKind,omitempty"` // StatefulSet, Cluster, Deployment, …
	App             string `json:"app,omitempty"`          // landscape app page (a Deployment mounting it)
	LastBackupAt    string `json:"lastBackupAt,omitempty"`
	Snapshots       int    `json:"snapshots"`
	AgeSeconds      int64  `json:"ageSeconds"`
}

// LHNode is a Longhorn node with its disks.
type LHNode struct {
	Name        string   `json:"name"`
	Ready       bool     `json:"ready"`
	Schedulable bool     `json:"schedulable"`
	Disks       []LHDisk `json:"disks"`
}

// LHDisk is one disk on a Longhorn node.
type LHDisk struct {
	Name        string `json:"name"`
	Path        string `json:"path"`
	Type        string `json:"type,omitempty"`
	Max         int64  `json:"max"`
	Available   int64  `json:"available"`
	Scheduled   int64  `json:"scheduled"`
	Reserved    int64  `json:"reserved"`
	Ready       bool   `json:"ready"`
	Schedulable bool   `json:"schedulable"`
	Replicas    int    `json:"replicas"`
}

// LHRecurringJob is a scheduled snapshot/backup job.
type LHRecurringJob struct {
	Name        string   `json:"name"`
	Task        string   `json:"task"`
	Cron        string   `json:"cron"`
	Retain      int      `json:"retain"`
	Concurrency int      `json:"concurrency"`
	Groups      []string `json:"groups,omitempty"`
}

// LHBackupTarget is where backups go (unset URL = backups disabled).
type LHBackupTarget struct {
	Name         string `json:"name"`
	URL          string `json:"url,omitempty"`
	Available    bool   `json:"available"`
	Message      string `json:"message,omitempty"`
	PollInterval string `json:"pollInterval,omitempty"`
	LastSyncedAt string `json:"lastSyncedAt,omitempty"`
}
