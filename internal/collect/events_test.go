package collect

import (
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestSplitLines(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"", 0},
		{"a", 1},
		{"a\nb\nc", 3},
		{"a\nb\n", 2}, // trailing newline is trimmed, not an empty line
		{"a\n\nb", 3}, // a blank line in the middle is preserved
		{"\n\n", 0},   // only newlines -> empty
		{"one line", 1},
	}
	for _, c := range cases {
		if got := splitLines([]byte(c.in)); len(got) != c.want {
			t.Errorf("splitLines(%q) = %d lines, want %d", c.in, len(got), c.want)
		}
	}
}

func TestEventSeen(t *testing.T) {
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	// classic first/last timestamps
	classic := &corev1.Event{
		FirstTimestamp: metav1.NewTime(base),
		LastTimestamp:  metav1.NewTime(base.Add(time.Hour)),
	}
	if got := eventLastSeen(classic); !got.Equal(base.Add(time.Hour)) {
		t.Errorf("classic lastSeen = %v, want %v", got, base.Add(time.Hour))
	}
	if got := eventFirstSeen(classic); !got.Equal(base) {
		t.Errorf("classic firstSeen = %v, want %v", got, base)
	}

	// series LastObservedTime wins for lastSeen
	series := &corev1.Event{
		FirstTimestamp: metav1.NewTime(base),
		Series:         &corev1.EventSeries{Count: 5, LastObservedTime: metav1.NewMicroTime(base.Add(2 * time.Hour))},
	}
	if got := eventLastSeen(series); !got.Equal(base.Add(2 * time.Hour)) {
		t.Errorf("series lastSeen = %v, want %v", got, base.Add(2*time.Hour))
	}

	// eventTime fallback when the classic timestamps are zero
	newStyle := &corev1.Event{EventTime: metav1.NewMicroTime(base.Add(3 * time.Hour))}
	if got := eventLastSeen(newStyle); !got.Equal(base.Add(3 * time.Hour)) {
		t.Errorf("eventTime lastSeen = %v, want %v", got, base.Add(3*time.Hour))
	}
	if got := eventFirstSeen(newStyle); !got.Equal(base.Add(3 * time.Hour)) {
		t.Errorf("eventTime firstSeen = %v, want %v", got, base.Add(3*time.Hour))
	}
}

func TestModelEvent(t *testing.T) {
	e := &corev1.Event{
		Count:          3,
		Series:         &corev1.EventSeries{Count: 7},
		Type:           "Warning",
		Reason:         "BackOff",
		Message:        "  back-off restarting failed container  ",
		Source:         corev1.EventSource{Component: "kubelet", Host: "node-1"},
		InvolvedObject: corev1.ObjectReference{Kind: "Pod", Name: "airlift-abc"},
	}
	m := modelEvent(e)
	if m.Count != 7 {
		t.Errorf("count = %d, want 7 (series count wins)", m.Count)
	}
	if m.Message != "back-off restarting failed container" {
		t.Errorf("message = %q, want trimmed", m.Message)
	}
	if m.Component != "kubelet · node-1" {
		t.Errorf("component = %q, want kubelet · node-1", m.Component)
	}
	if m.InvolvedKind != "Pod" || m.InvolvedName != "airlift-abc" {
		t.Errorf("involved = %s/%s", m.InvolvedKind, m.InvolvedName)
	}
}

func TestPickPod(t *testing.T) {
	mk := func(name string, phase corev1.PodPhase, age time.Duration) corev1.Pod {
		return corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: name, CreationTimestamp: metav1.NewTime(time.Now().Add(-age))},
			Status:     corev1.PodStatus{Phase: phase},
		}
	}
	running := []corev1.Pod{mk("old", corev1.PodRunning, time.Hour), mk("new", corev1.PodRunning, time.Minute)}
	if got := pickPod(running, "old"); got.Name != "old" {
		t.Errorf("requested pod: got %s, want old", got.Name)
	}
	if got := pickPod(running, ""); got.Name != "new" {
		t.Errorf("default: got %s, want newest running new", got.Name)
	}
	if got := pickPod(running, "ghost"); got.Name != "new" {
		t.Errorf("unknown request falls back to newest running: got %s", got.Name)
	}
	pending := []corev1.Pod{mk("p-old", corev1.PodPending, time.Hour), mk("p-new", corev1.PodPending, time.Minute)}
	if got := pickPod(pending, ""); got.Name != "p-new" {
		t.Errorf("no running -> newest overall: got %s, want p-new", got.Name)
	}
}

func TestContainsStr(t *testing.T) {
	if !containsStr([]string{"a", "b"}, "b") {
		t.Error("containsStr should find b")
	}
	if containsStr([]string{"a"}, "x") {
		t.Error("containsStr should not find x")
	}
	if containsStr(nil, "x") {
		t.Error("containsStr(nil) should be false")
	}
}

func TestEventFilterInMemory(t *testing.T) {
	// relAgeTime is a zero-safe helper shared with relAge
	if relAgeTime(time.Time{}) != "" {
		t.Error("relAgeTime(zero) should be empty")
	}
	if relAgeTime(time.Now().Add(-90*time.Second)) != "1m ago" {
		t.Errorf("relAgeTime(90s) = %q, want 1m ago", relAgeTime(time.Now().Add(-90*time.Second)))
	}
}
