package model

import "time"

// Event is a Kubernetes Event, trimmed for the console. It carries only object
// metadata (kind/name/reason/message) — never Secret or ConfigMap contents.
type Event struct {
	Namespace    string `json:"namespace"`
	InvolvedKind string `json:"involvedKind"`
	InvolvedName string `json:"involvedName"`
	Reason       string `json:"reason"`
	Type         string `json:"type"` // Normal | Warning
	Message      string `json:"message"`
	Count        int32  `json:"count"`
	FirstSeen    string `json:"firstSeen,omitempty"` // relative age, e.g. "2m ago"
	LastSeen     string `json:"lastSeen,omitempty"`  // relative age of the last occurrence
	Component    string `json:"component,omitempty"` // event source (controller / node)
}

// EventList is the events payload (per-app or the combined browser).
type EventList struct {
	UpdatedAt  time.Time `json:"updatedAt"`
	Window     string    `json:"window,omitempty"`     // note on the retention window
	Namespaces []string  `json:"namespaces,omitempty"` // namespaces with events (filter dropdown)
	Events     []Event   `json:"events"`
	Total      int       `json:"total"`  // number matched before the cap
	Capped     bool      `json:"capped"` // true when Total exceeds the returned count
}

// Logs is a tail of a pod's stdout (the app's own output). No Secret/ConfigMap
// contents are read; this is the pod log subresource only.
type Logs struct {
	UpdatedAt  time.Time `json:"updatedAt"`
	Namespace  string    `json:"namespace"`
	Pod        string    `json:"pod"`
	Container  string    `json:"container"`
	Containers []string  `json:"containers,omitempty"` // for the container picker
	Pods       []string  `json:"pods,omitempty"`       // pods matched by the app selector
	Tail       int       `json:"tail"`                 // requested tail line count
	Lines      []string  `json:"lines"`
	Note       string    `json:"note,omitempty"` // e.g. empty / not-yet-started
}
