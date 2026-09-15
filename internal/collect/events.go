package collect

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/sujaykumarsuman/landscape/internal/model"
)

// eventWindow notes that the API server keeps only recent Events (≈1h default),
// so the browser is a live window, not history.
const eventWindow = "Kubernetes keeps only recent events (≈1h) — this is a live window, not history."

// EventFilter narrows the combined events browser (all fields optional).
type EventFilter struct {
	Namespace string // "" = all namespaces
	Type      string // "", "Normal", "Warning"
	Kind      string // involvedObject kind, "" = any
	Query     string // free-text over reason/message/kind/name
	Limit     int    // cap (default 200, max 1000)
}

// Events lists cluster Events with server-side filters, newest-first, capped.
// It always fetches all namespaces so the namespace dropdown stays complete,
// then applies the namespace filter in memory.
func (co *Collector) Events(ctx context.Context, f EventFilter) (*model.EventList, error) {
	el, err := co.c.Typed.CoreV1().Events("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	limit := f.Limit
	if limit <= 0 {
		limit = 200
	}
	if limit > 1000 {
		limit = 1000
	}
	q := strings.ToLower(strings.TrimSpace(f.Query))

	out := &model.EventList{UpdatedAt: time.Now(), Window: eventWindow, Events: []model.Event{}}
	nsSet := map[string]struct{}{}
	type row struct {
		e    model.Event
		last time.Time
	}
	var matched []row
	for i := range el.Items {
		e := &el.Items[i]
		nsSet[e.Namespace] = struct{}{}
		if f.Namespace != "" && e.Namespace != f.Namespace {
			continue
		}
		if f.Type != "" && !strings.EqualFold(e.Type, f.Type) {
			continue
		}
		if f.Kind != "" && !strings.EqualFold(e.InvolvedObject.Kind, f.Kind) {
			continue
		}
		if q != "" {
			hay := strings.ToLower(e.Reason + " " + e.Message + " " + e.InvolvedObject.Kind + " " + e.InvolvedObject.Name)
			if !strings.Contains(hay, q) {
				continue
			}
		}
		matched = append(matched, row{modelEvent(e), eventLastSeen(e)})
	}
	sort.Slice(matched, func(i, j int) bool { return matched[i].last.After(matched[j].last) })
	out.Total = len(matched)
	if len(matched) > limit {
		matched = matched[:limit]
		out.Capped = true
	}
	for _, m := range matched {
		out.Events = append(out.Events, m.e)
	}
	for n := range nsSet {
		out.Namespaces = append(out.Namespaces, n)
	}
	sort.Strings(out.Namespaces)
	return out, nil
}

// AppEvents lists Events involving one app's Deployment, its ReplicaSet(s),
// Pods, Service and IngressRoute (reusing the same instance-label ownership the
// app-detail graph uses), newest-first.
func (co *Collector) AppEvents(ctx context.Context, name string) (*model.EventList, error) {
	ns, err := co.appNamespace(ctx, name)
	if err != nil {
		return nil, err
	}
	involved := map[string]bool{
		"Deployment/" + name: true,
		"Service/" + name:    true,
	}
	sel := "app.kubernetes.io/instance=" + name
	if rss, err := co.c.Typed.AppsV1().ReplicaSets(ns).List(ctx, metav1.ListOptions{LabelSelector: sel}); err == nil {
		for i := range rss.Items {
			involved["ReplicaSet/"+rss.Items[i].Name] = true
		}
	}
	if pods, err := co.c.Typed.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{LabelSelector: sel}); err == nil {
		for i := range pods.Items {
			involved["Pod/"+pods.Items[i].Name] = true
		}
	}
	if ing := co.ingressDetail(ctx, name); ing != nil {
		involved["IngressRoute/"+ing.Name] = true
	}

	el, err := co.c.Typed.CoreV1().Events(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	out := &model.EventList{UpdatedAt: time.Now(), Window: eventWindow, Events: []model.Event{}}
	type row struct {
		e    model.Event
		last time.Time
	}
	var matched []row
	for i := range el.Items {
		e := &el.Items[i]
		if !involved[e.InvolvedObject.Kind+"/"+e.InvolvedObject.Name] {
			continue
		}
		matched = append(matched, row{modelEvent(e), eventLastSeen(e)})
	}
	sort.Slice(matched, func(i, j int) bool { return matched[i].last.After(matched[j].last) })
	out.Total = len(matched)
	for _, m := range matched {
		out.Events = append(out.Events, m.e)
	}
	return out, nil
}

// LogOptions selects which pod/container to tail and how much.
type LogOptions struct {
	Pod       string // "" = the app's primary pod
	Container string // "" = the pod's first container
	Tail      int64  // lines (default 500, max 5000)
	Since     int64  // seconds; 0 = no since filter
}

// AppLogs tails a pod's stdout for one app. It reads the pod log subresource
// only (the app's own output) — never Secret/ConfigMap contents.
func (co *Collector) AppLogs(ctx context.Context, name string, opts LogOptions) (*model.Logs, error) {
	ns, err := co.appNamespace(ctx, name)
	if err != nil {
		return nil, err
	}
	pl, err := co.c.Typed.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{LabelSelector: "app.kubernetes.io/instance=" + name})
	if err != nil {
		return nil, err
	}
	l := &model.Logs{UpdatedAt: time.Now(), Namespace: ns, Lines: []string{}, Pods: podNames(pl.Items)}
	if len(pl.Items) == 0 {
		l.Note = "no pods for this app"
		return l, nil
	}
	pod := pickPod(pl.Items, opts.Pod)
	l.Pod = pod.Name
	for _, c := range pod.Spec.Containers {
		l.Containers = append(l.Containers, c.Name)
	}
	container := opts.Container
	if container == "" || !containsStr(l.Containers, container) {
		if len(l.Containers) > 0 {
			container = l.Containers[0]
		}
	}
	l.Container = container

	tail := opts.Tail
	if tail <= 0 {
		tail = 500
	}
	if tail > 5000 {
		tail = 5000
	}
	l.Tail = int(tail)
	logOpts := &corev1.PodLogOptions{Container: container, TailLines: &tail}
	if opts.Since > 0 {
		since := opts.Since
		logOpts.SinceSeconds = &since
	}
	raw, err := co.c.Typed.CoreV1().Pods(ns).GetLogs(pod.Name, logOpts).DoRaw(ctx)
	if err != nil {
		// e.g. container still starting, or no logs yet — surface a note, not a 502
		l.Note = "no logs yet (" + shortErr(err.Error()) + ")"
		return l, nil
	}
	l.Lines = splitLines(raw)
	if len(l.Lines) == 0 {
		l.Note = "no log lines in the requested window"
	}
	return l, nil
}

// appNamespace finds the namespace of the Deployment named after the app.
func (co *Collector) appNamespace(ctx context.Context, name string) (string, error) {
	deps, err := co.c.Typed.AppsV1().Deployments("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return "", err
	}
	for i := range deps.Items {
		if deps.Items[i].Name == name {
			return deps.Items[i].Namespace, nil
		}
	}
	return "", fmt.Errorf("app %q not found", name)
}

// pickPod chooses the requested pod, else the newest Running pod, else the
// newest pod.
func pickPod(items []corev1.Pod, want string) *corev1.Pod {
	if want != "" {
		for i := range items {
			if items[i].Name == want {
				return &items[i]
			}
		}
	}
	best := -1
	for i := range items {
		if items[i].Status.Phase != corev1.PodRunning {
			continue
		}
		if best < 0 || items[i].CreationTimestamp.After(items[best].CreationTimestamp.Time) {
			best = i
		}
	}
	if best < 0 {
		for i := range items {
			if best < 0 || items[i].CreationTimestamp.After(items[best].CreationTimestamp.Time) {
				best = i
			}
		}
	}
	return &items[best]
}

func podNames(items []corev1.Pod) []string {
	out := make([]string, 0, len(items))
	for i := range items {
		out = append(out, items[i].Name)
	}
	sort.Strings(out)
	return out
}

func containsStr(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}

func splitLines(b []byte) []string {
	s := strings.TrimRight(string(b), "\n")
	if s == "" {
		return []string{}
	}
	return strings.Split(s, "\n")
}

func shortErr(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 160 {
		return s[:160] + "…"
	}
	return s
}

// modelEvent maps a core Event to the trimmed console shape.
func modelEvent(e *corev1.Event) model.Event {
	count := e.Count
	if e.Series != nil && e.Series.Count > count {
		count = e.Series.Count
	}
	comp := e.Source.Component
	if comp == "" {
		comp = e.ReportingController
	}
	if comp != "" && e.Source.Host != "" {
		comp += " · " + e.Source.Host
	}
	return model.Event{
		Namespace:    e.Namespace,
		InvolvedKind: e.InvolvedObject.Kind,
		InvolvedName: e.InvolvedObject.Name,
		Reason:       e.Reason,
		Type:         e.Type,
		Message:      strings.TrimSpace(e.Message),
		Count:        count,
		FirstSeen:    relAgeTime(eventFirstSeen(e)),
		LastSeen:     relAgeTime(eventLastSeen(e)),
		Component:    comp,
	}
}

// eventLastSeen / eventFirstSeen tolerate both classic (firstTimestamp/
// lastTimestamp) and new-style (eventTime/series) Events.
func eventLastSeen(e *corev1.Event) time.Time {
	if e.Series != nil && !e.Series.LastObservedTime.IsZero() {
		return e.Series.LastObservedTime.Time
	}
	if !e.LastTimestamp.IsZero() {
		return e.LastTimestamp.Time
	}
	if !e.EventTime.IsZero() {
		return e.EventTime.Time
	}
	return e.FirstTimestamp.Time
}

func eventFirstSeen(e *corev1.Event) time.Time {
	if !e.FirstTimestamp.IsZero() {
		return e.FirstTimestamp.Time
	}
	if !e.EventTime.IsZero() {
		return e.EventTime.Time
	}
	return eventLastSeen(e)
}

// relAgeTime renders a time as a short relative age (shared with relAge).
func relAgeTime(t time.Time) string {
	if t.IsZero() {
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
