package azure

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// normalizePowerState strips common Azure prefixes and lowercases the result.
// "VM running" → "running", "PowerState/deallocated" → "deallocated".
func normalizePowerState(s string) string {
	s = strings.TrimPrefix(s, "VM ")
	s = strings.TrimPrefix(s, "PowerState/")
	return strings.ToLower(s)
}

// ParseVMList parses the JSON output of `az vm list -d --output json`.
func ParseVMList(data []byte) ([]VM, error) {
	if data == nil {
		return nil, nil
	}

	var raw []struct {
		Name            string `json:"name"`
		ResourceGroup   string `json:"resourceGroup"`
		Location        string `json:"location"`
		HardwareProfile struct {
			VMSize string `json:"vmSize"`
		} `json:"hardwareProfile"`
		StorageProfile struct {
			OSDisk struct {
				OSType string `json:"osType"`
			} `json:"osDisk"`
			ImageReference *struct {
				Offer string `json:"offer"`
				SKU   string `json:"sku"`
			} `json:"imageReference"`
		} `json:"storageProfile"`
		PowerState string `json:"powerState"`
		PrivateIPs string `json:"privateIps"`
		PublicIPs  string `json:"publicIps"`
		ID         string `json:"id"`
		OsProfile  struct {
			ComputerName string `json:"computerName"`
		} `json:"osProfile"`
	}

	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parsing VM list: %w", err)
	}

	vms := make([]VM, len(raw))
	for i, r := range raw {
		var osDisk string
		if r.StorageProfile.ImageReference != nil {
			offer := r.StorageProfile.ImageReference.Offer
			sku := r.StorageProfile.ImageReference.SKU
			if offer != "" || sku != "" {
				osDisk = strings.TrimSpace(offer + " " + sku)
			}
		}
		vms[i] = VM{
			Name:          r.Name,
			ResourceGroup: r.ResourceGroup,
			Location:      r.Location,
			VMSize:        r.HardwareProfile.VMSize,
			OSType:        r.StorageProfile.OSDisk.OSType,
			OSDisk:        osDisk,
			PrivateIP:     r.PrivateIPs,
			Hostname:      r.OsProfile.ComputerName,
			PublicIP:      r.PublicIPs,
			PowerState:    normalizePowerState(r.PowerState),
			ID:            r.ID,
		}
	}
	return vms, nil
}

// ParseResourceGroupList parses the JSON output of `az group list --output json`.
func ParseResourceGroupList(data []byte) ([]ResourceGroup, error) {
	if data == nil {
		return nil, nil
	}

	var raw []struct {
		Name       string `json:"name"`
		Location   string `json:"location"`
		Properties struct {
			ProvisioningState string `json:"provisioningState"`
		} `json:"properties"`
		ID string `json:"id"`
	}

	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parsing resource group list: %w", err)
	}

	rgs := make([]ResourceGroup, len(raw))
	for i, r := range raw {
		rgs[i] = ResourceGroup{
			Name:     r.Name,
			Location: r.Location,
			State:    r.Properties.ProvisioningState,
			ID:       r.ID,
		}
	}
	return rgs, nil
}

// ParseAKSList parses the JSON output of `az aks list --output json`.
func ParseAKSList(data []byte) ([]AKSCluster, error) {
	if data == nil {
		return nil, nil
	}

	var raw []struct {
		Name              string `json:"name"`
		ResourceGroup     string `json:"resourceGroup"`
		Location          string `json:"location"`
		KubernetesVersion string `json:"kubernetesVersion"`
		PowerState        struct {
			Code string `json:"code"`
		} `json:"powerState"`
		AgentPoolProfiles []struct {
			Name  string `json:"name"`
			Count int    `json:"count"`
		} `json:"agentPoolProfiles"`
		ID string `json:"id"`
	}

	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parsing AKS list: %w", err)
	}

	clusters := make([]AKSCluster, len(raw))
	for i, r := range raw {
		nodeCount := 0
		for _, pool := range r.AgentPoolProfiles {
			nodeCount += pool.Count
		}
		clusters[i] = AKSCluster{
			Name:              r.Name,
			ResourceGroup:     r.ResourceGroup,
			Location:          r.Location,
			KubernetesVersion: r.KubernetesVersion,
			NodeCount:         nodeCount,
			PowerState:        r.PowerState.Code,
			ID:                r.ID,
		}
	}
	return clusters, nil
}

// ParseSubscriptionList parses the JSON output of `az account list --output json`.
// Input must be a JSON array; a JSON object returns an error.
func ParseSubscriptionList(data []byte) ([]AzureSubscription, error) {
	if data == nil {
		return nil, nil
	}

	// Detect non-array JSON (object).
	trimmed := strings.TrimSpace(string(data))
	if len(trimmed) > 0 && trimmed[0] != '[' {
		return nil, fmt.Errorf("parsing subscription list: expected JSON array, got object")
	}

	var raw []struct {
		Name      string `json:"name"`
		ID        string `json:"id"`
		State     string `json:"state"`
		IsDefault bool   `json:"isDefault"`
	}

	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parsing subscription list: %w", err)
	}

	subs := make([]AzureSubscription, len(raw))
	for i, r := range raw {
		subs[i] = AzureSubscription{
			Name:      r.Name,
			ID:        r.ID,
			State:     r.State,
			IsDefault: r.IsDefault,
		}
	}
	return subs, nil
}

// ParseCLIVersion parses the JSON output of `az version --output json` and
// returns the "azure-cli" version string. Returns ("", nil) if the key is missing.
func ParseCLIVersion(data []byte) (string, error) {
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return "", fmt.Errorf("parsing CLI version: %w", err)
	}

	v, ok := raw["azure-cli"]
	if !ok {
		return "", nil
	}

	s, ok := v.(string)
	if !ok {
		return "", nil
	}
	return s, nil
}

// ParseAccountShow parses the JSON output of `az account show --subscription <name>`.
// Returns subscription info including tenant and user details.
func ParseAccountShow(data []byte) (SubscriptionProbeInfo, error) {
	var raw struct {
		Name              string `json:"name"`
		ID                string `json:"id"`
		State             string `json:"state"`
		TenantDisplayName string `json:"tenantDisplayName"`
		User              struct {
			Name string `json:"name"`
		} `json:"user"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return SubscriptionProbeInfo{}, fmt.Errorf("parsing account show: %w", err)
	}
	return SubscriptionProbeInfo{
		ID:     raw.ID,
		State:  raw.State,
		Tenant: raw.TenantDisplayName,
		User:   raw.User.Name,
	}, nil
}

// ParseActivityLog parses JSON output from `az monitor activity-log list`.
func ParseActivityLog(data []byte) ([]ActivityLogEntry, error) {
	if len(data) == 0 {
		return nil, nil
	}
	var raw []struct {
		Timestamp     string `json:"t"`
		Operation     string `json:"op"`
		Status        string `json:"s"`
		Caller        string `json:"c"`
		ResourceGroup string `json:"rg"`
		ResourceID    string `json:"r"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parsing activity log: %w", err)
	}

	entries := make([]ActivityLogEntry, 0, len(raw))
	for _, r := range raw {
		// Extract resource name from last segment of resourceId
		resource := r.ResourceID
		if idx := strings.LastIndex(resource, "/"); idx >= 0 {
			resource = resource[idx+1:]
		}
		entries = append(entries, ActivityLogEntry{
			Timestamp:     formatActivityTime(r.Timestamp),
			ResourceGroup: r.ResourceGroup,
			Operation:     r.Operation,
			Resource:      resource,
			Status:        r.Status,
			Caller:        r.Caller,
		})
	}
	return entries, nil
}

// formatActivityTime converts ISO-8601 timestamp to "02/01 15:04" format.
func formatActivityTime(s string) string {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		t, err = time.Parse("2006-01-02T15:04:05.9999999Z", s)
		if err != nil {
			return s
		}
	}
	return t.Local().Format("02/01 15:04")
}
