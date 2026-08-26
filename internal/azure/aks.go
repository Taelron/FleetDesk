// Package azure fetches Azure resources (subscriptions, VMs, AKS clusters)
// through the az CLI.
package azure

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

// FetchAKSClusters fetches AKS clusters for a subscription using Resource Graph.
func FetchAKSClusters(m *Manager, subName, subID, tenantID string, logger *slog.Logger) ([]AKSDetail, error) {
	start := time.Now()
	logger.Debug("azure fetch aks start", "subscription", subName)

	if subID == "" {
		return nil, fmt.Errorf("subscription ID required for graph query")
	}

	query := "Resources | where type =~ 'microsoft.containerservice/managedclusters' " +
		"| project name, resourceGroup, location, id, " +
		"k8sVersion = properties.kubernetesVersion, " +
		"powerState = properties.powerState.code, " +
		"provisioningState = properties.provisioningState, " +
		"networkPlugin = properties.networkProfile.networkPlugin, " +
		"pools = properties.agentPoolProfiles, " +
		"createdDate = tostring(tags.created_Date), tags " +
		"| order by createdDate asc"

	args := []string{"graph", "query", "-q", query, "--subscriptions", subID, "--first", "1000"}
	data, err := m.RunCommand(args...)
	if err != nil {
		return nil, fmt.Errorf("graph aks query: %w", err)
	}

	clusters, err := ParseGraphAKS(data)
	if err != nil {
		return nil, err
	}

	if len(clusters) == 1000 {
		logger.Warn("resource graph query returned 1000 results, results may be truncated", "query", "aks")
	}

	logger.Debug("azure fetch aks complete", "subscription", subName, "count", len(clusters), "elapsed", time.Since(start))
	return clusters, nil
}

// ParseGraphAKS parses Resource Graph output for AKS clusters with embedded node pools.
func ParseGraphAKS(data []byte) ([]AKSDetail, error) {
	var result struct {
		Data []struct {
			Name              string                 `json:"name"`
			ResourceGroup     string                 `json:"resourceGroup"`
			Location          string                 `json:"location"`
			ID                string                 `json:"id"`
			K8sVersion        string                 `json:"k8sVersion"`
			PowerState        string                 `json:"powerState"`
			ProvisioningState string                 `json:"provisioningState"`
			NetworkPlugin     string                 `json:"networkPlugin"`
			CreatedDate       string                 `json:"createdDate"`
			Tags              map[string]interface{} `json:"tags"`
			Pools             []struct {
				Name                       string `json:"name"`
				Mode                       string `json:"mode"`
				VMSize                     string `json:"vmSize"`
				Count                      int    `json:"count"`
				MinCount                   int    `json:"minCount"`
				MaxCount                   int    `json:"maxCount"`
				CurrentOrchestratorVersion string `json:"currentOrchestratorVersion"`
				EnableAutoScaling          bool   `json:"enableAutoScaling"`
			} `json:"pools"`
		} `json:"data"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("parsing graph aks result: %w", err)
	}

	clusters := make([]AKSDetail, len(result.Data))
	for i, r := range result.Data {
		totalNodes := 0
		pools := make([]AKSNodePool, len(r.Pools))
		for j, p := range r.Pools {
			totalNodes += p.Count
			pools[j] = AKSNodePool{
				Name:      p.Name,
				Mode:      p.Mode,
				VMSize:    p.VMSize,
				Count:     p.Count,
				MinCount:  p.MinCount,
				MaxCount:  p.MaxCount,
				Version:   p.CurrentOrchestratorVersion,
				AutoScale: p.EnableAutoScaling,
			}
		}
		tags := make(map[string]string)
		for k, v := range r.Tags {
			if v != nil {
				tags[k] = fmt.Sprintf("%v", v)
			}
		}
		clusters[i] = AKSDetail{
			AKSCluster: AKSCluster{
				Name:              r.Name,
				ResourceGroup:     r.ResourceGroup,
				Location:          r.Location,
				KubernetesVersion: r.K8sVersion,
				NodeCount:         totalNodes,
				PowerState:        r.PowerState,
				ProvisioningState: r.ProvisioningState,
				CreatedDate:       r.CreatedDate,
				Tags:              tags,
				ID:                r.ID,
			},
			NetworkPlugin: r.NetworkPlugin,
			Pools:         pools,
		}
	}
	return clusters, nil
}

// AKSAction performs an AKS action (start, stop, delete) asynchronously.
func AKSAction(m *Manager, clusterName, rgName, subName, tenantID, action string, logger *slog.Logger) error {
	start := time.Now()
	logger.Debug("azure aks action start", "action", action, "cluster", clusterName, "rg", rgName)

	args := []string{"aks", action, "--name", clusterName, "--resource-group", rgName, "--subscription", subName, "--no-wait"}
	if action == "delete" {
		args = append(args, "--yes")
	}
	if tenantID != "" {
		args = append(args, "--tenant", tenantID)
	}
	_, err := m.RunCommand(args...)
	if err != nil {
		logger.Error("azure aks action failed", "action", action, "cluster", clusterName, "elapsed", time.Since(start), "err", err)
		return fmt.Errorf("aks %s: %w", action, err)
	}

	logger.Debug("azure aks action complete", "action", action, "cluster", clusterName, "elapsed", time.Since(start))
	return nil
}

// FetchAKSPowerStates queries Resource Graph for power states of specific AKS clusters.
// Returns a map of cluster name (lowercased) → power state.
func FetchAKSPowerStates(m *Manager, subID string, clusterNames []string, logger *slog.Logger) (map[string]string, error) {
	start := time.Now()
	logger.Debug("azure poll aks states start", "count", len(clusterNames))

	// Build name filter: name in~ ('c1','c2')
	// Skip names containing single quotes to prevent KQL injection.
	var safe []string
	for _, n := range clusterNames {
		if !strings.ContainsRune(n, '\'') {
			safe = append(safe, "'"+n+"'")
		}
	}
	if len(safe) == 0 {
		return nil, nil
	}
	nameFilter := strings.Join(safe, ",")

	query := fmt.Sprintf("Resources | where type =~ 'microsoft.containerservice/managedclusters' "+
		"| where name in~ (%s) "+
		"| project name, provisioningState = properties.provisioningState, powerState = properties.powerState.code", nameFilter)

	args := []string{"graph", "query", "-q", query, "--subscriptions", subID, "--first", "1000"}
	data, err := m.RunCommand(args...)
	if err != nil {
		return nil, fmt.Errorf("graph aks poll query: %w", err)
	}

	states, err := ParseAKSPowerStates(data)
	if err != nil {
		return nil, err
	}

	logger.Debug("azure poll aks states complete", "count", len(states), "elapsed", time.Since(start))
	return states, nil
}

// ParseAKSPowerStates parses Resource Graph output into a cluster name → power state map.
// Uses powerState (Running/Stopped) for start/stop tracking, falls back to provisioningState.
func ParseAKSPowerStates(data []byte) (map[string]string, error) {
	var result struct {
		Data []struct {
			Name              string `json:"name"`
			ProvisioningState string `json:"provisioningState"`
			PowerState        string `json:"powerState"`
		} `json:"data"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("parsing aks power states: %w", err)
	}

	states := make(map[string]string, len(result.Data))
	for _, r := range result.Data {
		state := r.PowerState
		if state == "" {
			state = r.ProvisioningState
		}
		states[strings.ToLower(r.Name)] = strings.ToLower(state)
	}
	return states, nil
}
