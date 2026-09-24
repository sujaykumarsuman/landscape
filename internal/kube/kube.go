// Package kube builds the read-only Kubernetes clients the collector uses:
// a typed clientset (core/apps/nodes), a dynamic client (Flux + Traefik CRDs),
// and a metrics clientset (metrics-server). In-cluster by default; falls back to
// KUBECONFIG / ~/.kube/config for local runs.
package kube

import (
	"fmt"
	"os"
	"path/filepath"

	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	metricsv "k8s.io/metrics/pkg/client/clientset/versioned"
)

// Clients bundles everything the collector needs.
type Clients struct {
	Typed   kubernetes.Interface
	Dynamic dynamic.Interface
	Metrics *metricsv.Clientset
}

// New builds the clients, in-cluster first, then a kubeconfig fallback.
func New() (*Clients, error) {
	cfg, err := restConfig()
	if err != nil {
		return nil, err
	}
	cfg.QPS, cfg.Burst = 50, 100

	typed, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("typed client: %w", err)
	}
	dyn, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("dynamic client: %w", err)
	}
	// Metrics is optional; a nil clientset means "metrics-server unavailable".
	met, _ := metricsv.NewForConfig(cfg)

	return &Clients{Typed: typed, Dynamic: dyn, Metrics: met}, nil
}

func restConfig() (*rest.Config, error) {
	if cfg, err := rest.InClusterConfig(); err == nil {
		return cfg, nil
	}
	kc := os.Getenv("KUBECONFIG")
	if kc == "" {
		if home, err := os.UserHomeDir(); err == nil {
			kc = filepath.Join(home, ".kube", "config")
		}
	}
	cfg, err := clientcmd.BuildConfigFromFlags("", kc)
	if err != nil {
		return nil, fmt.Errorf("no in-cluster config and no usable kubeconfig (%s): %w", kc, err)
	}
	return cfg, nil
}
