package collect

import (
	"context"
	"sort"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/sujaykumarsuman/landscape/internal/model"
)

// Metrics reads live usage from metrics-server plus node capacity.
func (co *Collector) Metrics(ctx context.Context) (*model.Metrics, error) {
	m := &model.Metrics{UpdatedAt: time.Now()}
	if co.c.Metrics == nil {
		return m, nil // metrics-server not configured
	}

	// node capacity (from the Node object)
	var cpuCap, memCap int64
	if nl, err := co.c.Typed.CoreV1().Nodes().List(ctx, metav1.ListOptions{}); err == nil && len(nl.Items) > 0 {
		cap0 := nl.Items[0].Status.Capacity
		c := cap0["cpu"]
		mm := cap0["memory"]
		cpuCap, memCap = c.MilliValue(), mm.Value()
		m.Node.Name = nl.Items[0].Name
	}

	// node usage
	if nm, err := co.c.Metrics.MetricsV1beta1().NodeMetricses().List(ctx, metav1.ListOptions{}); err == nil && len(nm.Items) > 0 {
		u := nm.Items[0].Usage
		cpu := u["cpu"]
		mem := u["memory"]
		m.Available = true
		m.Node.CPUMilli = cpu.MilliValue()
		m.Node.MemBytes = mem.Value()
		m.Node.CPUCap = cpuCap
		m.Node.MemCap = memCap
		if cpuCap > 0 {
			m.Node.CPUPct = round1(float64(m.Node.CPUMilli) / float64(cpuCap) * 100)
		}
		if memCap > 0 {
			m.Node.MemPct = round1(float64(m.Node.MemBytes) / float64(memCap) * 100)
		}
	} else if err != nil {
		return m, nil // treat as unavailable rather than failing the page
	}

	// pod usage → per-pod + per-namespace
	nsAgg := map[string]*model.NsMetrics{}
	if pm, err := co.c.Metrics.MetricsV1beta1().PodMetricses("").List(ctx, metav1.ListOptions{}); err == nil {
		for i := range pm.Items {
			p := &pm.Items[i]
			var cpu, mem int64
			for _, cn := range p.Containers {
				cq := cn.Usage["cpu"]
				mq := cn.Usage["memory"]
				cpu += cq.MilliValue()
				mem += mq.Value()
			}
			m.Pods = append(m.Pods, model.PodMetrics{Namespace: p.Namespace, Name: p.Name, CPUMilli: cpu, MemBytes: mem})
			a := nsAgg[p.Namespace]
			if a == nil {
				a = &model.NsMetrics{Name: p.Namespace}
				nsAgg[p.Namespace] = a
			}
			a.CPUMilli += cpu
			a.MemBytes += mem
			a.Pods++
		}
	}
	for _, a := range nsAgg {
		m.Namespaces = append(m.Namespaces, *a)
	}
	sort.Slice(m.Namespaces, func(i, j int) bool { return m.Namespaces[i].MemBytes > m.Namespaces[j].MemBytes })
	sort.Slice(m.Pods, func(i, j int) bool { return m.Pods[i].MemBytes > m.Pods[j].MemBytes })
	// keep a bounded set; the Metrics tab sorts/filters this client-side
	if len(m.Pods) > 50 {
		m.Pods = m.Pods[:50]
	}

	// pod counts (readiness) from the core API
	if pl, err := co.c.Typed.CoreV1().Pods("").List(ctx, metav1.ListOptions{}); err == nil {
		for i := range pl.Items {
			p := &pl.Items[i]
			if p.Status.Phase == "Succeeded" {
				continue // completed Job pods aren't part of the running total
			}
			m.PodsTotal++
			if p.Status.Phase == "Running" {
				for _, c := range p.Status.Conditions {
					if c.Type == "Ready" && c.Status == "True" {
						m.PodsReady++
						break
					}
				}
			}
		}
	}
	return m, nil
}

func round1(f float64) float64 {
	return float64(int64(f*10+0.5)) / 10
}
