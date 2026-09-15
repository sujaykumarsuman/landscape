// Package model is the read-only landscape graph the UI renders: nodes (repos,
// images, Flux objects, cluster resources), edges between them, deep-links to the
// places you maintain (source, workflow, config, image), and live metrics.
package model

import "time"

// Layer places a node in one of the four lanes of the landscape map.
type Layer string

const (
	LayerSource  Layer = "source"  // git repos
	LayerBuild   Layer = "build"   // CI + registry
	LayerGitOps  Layer = "gitops"  // Flux
	LayerCluster Layer = "cluster" // k3s workloads
)

// Link is a deep-link to a maintained place for a node.
type Link struct {
	Type  string `json:"type"` // source | workflow | config | image | docs
	URL   string `json:"url"`
	Label string `json:"label"`
}

// Node is one entity in the landscape.
type Node struct {
	ID         string            `json:"id"`
	Kind       string            `json:"kind"` // repo|image|actions|flux|kustomization|helmRelease|controller|namespace|app|deployment|pod|service|ingressRoute|configMap|secret|pvc|ingress|infraTool
	Name       string            `json:"name"`
	Namespace  string            `json:"namespace,omitempty"`
	Layer      Layer             `json:"layer"`
	App        string            `json:"app,omitempty"` // the app this node belongs to (for drill-in + highlight)
	Owner      bool              `json:"owner"`         // true = one of your maintained places; false = upstream infra tool
	Status     string            `json:"status"`        // ok|progressing|failed|unknown
	StatusText string            `json:"statusText,omitempty"`
	Summary    string            `json:"summary,omitempty"` // one-line explanation for the hover card
	Meta       map[string]string `json:"meta,omitempty"`
	Links      []Link            `json:"links,omitempty"`
}

// Edge connects two nodes.
type Edge struct {
	From string `json:"from"`
	To   string `json:"to"`
	Kind string `json:"kind"` // flow|deploy|route|watch|mount|source|owns
	App  string `json:"app,omitempty"`
}

// Graph is the whole landscape at a point in time.
type Graph struct {
	UpdatedAt time.Time `json:"updatedAt"`
	Cluster   Cluster   `json:"cluster"`
	Nodes     []Node    `json:"nodes"`
	Edges     []Edge    `json:"edges"`
	Warnings  []string  `json:"warnings,omitempty"`
}

// Cluster is the node/cluster header info + roll-ups for the ribbon and boxes.
type Cluster struct {
	Node          string      `json:"node"`
	Version       string      `json:"version"`
	Namespaces    int         `json:"namespaces"`
	Apps          int         `json:"apps"`
	PodsReady     int         `json:"podsReady"`
	PodsTotal     int         `json:"podsTotal"`
	PodsCompleted int         `json:"podsCompleted,omitempty"` // terminal Job pods (e.g. k3s helm-install)
	FluxReady     bool        `json:"fluxReady"`
	FluxMsg       string      `json:"fluxMsg,omitempty"`
	FluxAgo       string      `json:"fluxAgo,omitempty"`  // "2m ago"
	NodeCPU       string      `json:"nodeCpu,omitempty"`  // "1 vCPU"
	NodeMem       string      `json:"nodeMem,omitempty"`  // "3.8 GiB"
	DiskFree      string      `json:"diskFree,omitempty"` // "42G free"
	TLS           string      `json:"tls,omitempty"`      // "Let's Encrypt · 78d"
	NsSummary     []NsSummary `json:"nsSummary,omitempty"`
	GitOps        GitOpsInfo  `json:"gitops"`
}

// Metrics is the live-metrics payload.
type Metrics struct {
	UpdatedAt  time.Time    `json:"updatedAt"`
	Available  bool         `json:"available"` // metrics-server reachable
	Node       NodeMetrics  `json:"node"`
	Namespaces []NsMetrics  `json:"namespaces"`
	Pods       []PodMetrics `json:"pods"`
	PodsReady  int          `json:"podsReady"`
	PodsTotal  int          `json:"podsTotal"`
}

type NodeMetrics struct {
	Name     string  `json:"name"`
	CPUMilli int64   `json:"cpuMilli"`
	CPUCap   int64   `json:"cpuCap"`
	CPUPct   float64 `json:"cpuPct"`
	MemBytes int64   `json:"memBytes"`
	MemCap   int64   `json:"memCap"`
	MemPct   float64 `json:"memPct"`
}

type NsMetrics struct {
	Name     string `json:"name"`
	CPUMilli int64  `json:"cpuMilli"`
	MemBytes int64  `json:"memBytes"`
	Pods     int    `json:"pods"`
}

type PodMetrics struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	CPUMilli  int64  `json:"cpuMilli"`
	MemBytes  int64  `json:"memBytes"`
}
