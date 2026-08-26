// Package k8s fetches Kubernetes resources (contexts, nodes, pods, workloads)
// through kubectl.
package k8s

import (
	"encoding/json"
	"log/slog"
	"strings"
	"sync"
	"time"
)

// FetchResourceCounts fetches namespace, node, and ArgoCD app counts concurrently.
func FetchResourceCounts(m *Manager, contextName string, logger *slog.Logger) (K8sResourceCounts, []string) {
	start := time.Now()
	logger.Debug("k8s resource counts start", "context", contextName)

	var counts K8sResourceCounts
	var mu sync.Mutex
	var errs []string

	type countTask struct {
		name   string
		args   []string
		target *int
	}

	tasks := []countTask{
		{"namespaces", []string{"get", "namespaces", "--context", contextName, "-o", "json"}, &counts.Namespaces},
		{"nodes", []string{"get", "nodes", "--context", contextName, "-o", "json"}, &counts.Nodes},
		{"argocd", []string{"get", "applications.argoproj.io", "-A", "--context", contextName, "-o", "json"}, &counts.ArgoApps},
	}

	var wg sync.WaitGroup
	wg.Add(len(tasks))

	for _, task := range tasks {
		go func(t countTask) {
			defer wg.Done()
			s := time.Now()
			out, err := m.RunCommand(t.args...)
			elapsed := time.Since(s)
			if err != nil {
				// ArgoCD CRD might not exist — not an error
				if t.name == "argocd" && strings.Contains(err.Error(), "the server doesn't have a resource type") {
					logger.Debug("k8s count skipped", "resource", t.name, "context", contextName, "reason", "CRD not found")
					return
				}
				logger.Error("k8s count failed", "resource", t.name, "context", contextName, "elapsed", elapsed, "err", err)
				mu.Lock()
				errs = append(errs, t.name+": "+err.Error())
				mu.Unlock()
				return
			}
			count := parseK8sListCount(out)
			logger.Debug("k8s count done", "resource", t.name, "context", contextName, "count", count, "elapsed", elapsed)
			mu.Lock()
			*t.target = count
			mu.Unlock()
		}(task)
	}

	wg.Wait()
	logger.Debug("k8s resource counts complete", "context", contextName, "ns", counts.Namespaces, "nodes", counts.Nodes, "argo", counts.ArgoApps, "total_elapsed", time.Since(start))
	return counts, errs
}

// parseK8sListCount extracts the item count from kubectl JSON list output.
func parseK8sListCount(data []byte) int {
	var list struct {
		Items []json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal(data, &list); err != nil {
		return 0
	}
	return len(list.Items)
}
