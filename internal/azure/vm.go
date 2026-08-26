package azure

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
)

// FetchVMs fetches the VM list for a subscription using Azure Resource Graph
// with two parallel goroutines: one for VMs, one for NIC private IPs.
// Falls back to az vm list if graph query fails.
func FetchVMs(m *Manager, subName, subID, tenantID string, logger *slog.Logger) ([]VM, error) {
	start := time.Now()
	logger.Debug("azure fetch vms start", "subscription", subName)

	if subID == "" {
		// No subscription UUID — fall back to az vm list (slower)
		return fetchVMsLegacy(m, subName, tenantID, logger, start)
	}

	var vms []VM
	var nicMap map[string]nicInfo // vmID → NIC info
	var vmErr, nicErr error
	var wg sync.WaitGroup

	wg.Add(2)

	// Goroutine 1: fetch VMs via Resource Graph
	go func() {
		defer wg.Done()
		s := time.Now()
		vmQuery := "Resources | where type =~ 'microsoft.compute/virtualMachines' " +
			"| extend powerState = properties.extended.instanceView.powerState.code " +
			"| project name, resourceGroup, location, id, powerState, " +
			"vmSize = properties.hardwareProfile.vmSize, " +
			"osType = properties.storageProfile.osDisk.osType, " +
			"offer = properties.storageProfile.imageReference.offer, " +
			"sku = properties.storageProfile.imageReference.sku, " +
			"computerName = properties.osProfile.computerName"

		args := []string{"graph", "query", "-q", vmQuery,
			"--subscriptions", subID, "--first", "1000"}
		data, err := m.RunCommand(args...)
		logger.Debug("azure graph vms done", "subscription", subName, "elapsed", time.Since(s))
		if err != nil {
			vmErr = fmt.Errorf("graph vm query: %w", err)
			return
		}
		vms, vmErr = ParseGraphVMs(data)
	}()

	// Goroutine 2: fetch NIC private IPs via Resource Graph
	go func() {
		defer wg.Done()
		s := time.Now()
		nicQuery := "Resources | where type =~ 'microsoft.network/networkinterfaces' " +
			"| where isnotnull(properties.virtualMachine.id) " +
			"| mv-expand ipconfig = properties.ipConfigurations " +
			"| project vmId = tolower(properties.virtualMachine.id), " +
			"privateIp = ipconfig.properties.privateIPAddress, " +
			"subnetId = ipconfig.properties.subnet.id"

		args := []string{"graph", "query", "-q", nicQuery,
			"--subscriptions", subID, "--first", "1000"}
		data, err := m.RunCommand(args...)
		logger.Debug("azure graph nics done", "subscription", subName, "elapsed", time.Since(s))
		if err != nil {
			nicErr = fmt.Errorf("graph nic query: %w", err)
			return
		}
		nicMap, nicErr = ParseGraphNICs(data)
	}()

	wg.Wait()

	if len(vms) == 1000 {
		logger.Warn("resource graph query returned 1000 results, results may be truncated", "query", "vms")
	}
	if len(nicMap) == 1000 {
		logger.Warn("resource graph query returned 1000 results, results may be truncated", "query", "nics")
	}

	if vmErr != nil {
		logger.Error("azure graph vm query failed, falling back", "err", vmErr)
		return fetchVMsLegacy(m, subName, tenantID, logger, start)
	}

	// Join NIC info to VMs (best-effort — nicErr is non-fatal)
	if nicErr != nil {
		logger.Error("azure graph nic query failed", "err", nicErr)
	} else if nicMap != nil {
		for i := range vms {
			if info, ok := nicMap[strings.ToLower(vms[i].ID)]; ok {
				vms[i].PrivateIP = info.PrivateIP
				vms[i].VNet = info.VNet
				vms[i].Subnet = info.Subnet
			}
		}
	}

	logger.Debug("azure fetch vms complete", "subscription", subName, "count", len(vms), "elapsed", time.Since(start))
	return vms, nil
}

// fetchVMsLegacy is the slow fallback using az vm list -d.
func fetchVMsLegacy(m *Manager, subName, tenantID string, logger *slog.Logger, start time.Time) ([]VM, error) {
	logger.Debug("azure fetch vms legacy (az vm list -d)", "subscription", subName)
	args := []string{"vm", "list", "-d", "--subscription", subName}
	if tenantID != "" {
		args = append(args, "--tenant", tenantID)
	}
	data, err := m.RunCommand(args...)
	if err != nil {
		return nil, fmt.Errorf("vm list: %w", err)
	}
	vms, err := ParseVMList(data)
	if err != nil {
		return nil, fmt.Errorf("parse vm list: %w", err)
	}
	logger.Debug("azure fetch vms legacy complete", "subscription", subName, "count", len(vms), "elapsed", time.Since(start))
	return vms, nil
}

// ParseGraphVMs parses the JSON output of az graph query for VMs.
func ParseGraphVMs(data []byte) ([]VM, error) {
	var result struct {
		Data []struct {
			Name          string `json:"name"`
			ResourceGroup string `json:"resourceGroup"`
			Location      string `json:"location"`
			ID            string `json:"id"`
			PowerState    string `json:"powerState"`
			VMSize        string `json:"vmSize"`
			OSType        string `json:"osType"`
			Offer         string `json:"offer"`
			SKU           string `json:"sku"`
			ComputerName  string `json:"computerName"`
		} `json:"data"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("parsing graph vm result: %w", err)
	}

	vms := make([]VM, len(result.Data))
	for i, r := range result.Data {
		osDisk := ""
		if r.Offer != "" {
			osDisk = r.Offer + " " + r.SKU
		}
		vms[i] = VM{
			Name:          r.Name,
			ResourceGroup: r.ResourceGroup,
			Location:      r.Location,
			VMSize:        r.VMSize,
			OSType:        r.OSType,
			OSDisk:        osDisk,
			Hostname:      r.ComputerName,
			PowerState:    normalizePowerState(r.PowerState),
			ID:            r.ID,
		}
	}
	return vms, nil
}

// nicInfo holds network info extracted from a NIC resource.
type nicInfo struct {
	PrivateIP string
	VNet      string
	Subnet    string
}

// parseSubnetID extracts VNet and Subnet names from a subnet resource ID.
// Format: .../virtualNetworks/VNET-NAME/subnets/SUBNET-NAME
func parseSubnetID(subnetID string) (vnet, subnet string) {
	parts := strings.Split(subnetID, "/")
	for i, p := range parts {
		if strings.EqualFold(p, "virtualNetworks") && i+1 < len(parts) {
			vnet = parts[i+1]
		}
		if strings.EqualFold(p, "subnets") && i+1 < len(parts) {
			subnet = parts[i+1]
		}
	}
	return
}

// ParseGraphNICs parses the JSON output of az graph query for NICs.
// Returns a map of lowercase VM resource ID → NIC info (IP, VNet, Subnet).
func ParseGraphNICs(data []byte) (map[string]nicInfo, error) {
	var result struct {
		Data []struct {
			VMID      string `json:"vmId"`
			PrivateIP string `json:"privateIp"`
			SubnetID  string `json:"subnetId"`
		} `json:"data"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("parsing graph nic result: %w", err)
	}

	m := make(map[string]nicInfo, len(result.Data))
	for _, r := range result.Data {
		if r.VMID != "" {
			vnet, subnet := parseSubnetID(r.SubnetID)
			m[strings.ToLower(r.VMID)] = nicInfo{
				PrivateIP: r.PrivateIP,
				VNet:      vnet,
				Subnet:    subnet,
			}
		}
	}
	return m, nil
}

// VMAction executes a VM power action (start, deallocate, restart) with --no-wait.
func VMAction(m *Manager, vmName, rgName, subName, tenantID, action string, logger *slog.Logger) error {
	start := time.Now()
	logger.Debug("azure vm action start", "action", action, "vm", vmName, "rg", rgName)

	args := []string{"vm", action, "--name", vmName, "--resource-group", rgName, "--subscription", subName, "--no-wait"}
	if tenantID != "" {
		args = append(args, "--tenant", tenantID)
	}
	_, err := m.RunCommand(args...)
	if err != nil {
		logger.Error("azure vm action failed", "action", action, "vm", vmName, "elapsed", time.Since(start), "err", err)
		return fmt.Errorf("vm %s: %w", action, err)
	}

	logger.Debug("azure vm action complete", "action", action, "vm", vmName, "elapsed", time.Since(start))
	return nil
}

// FetchVMDetail fetches extended VM properties using az vm show.
func FetchVMDetail(m *Manager, vmName, rgName, subName, tenantID string, logger *slog.Logger) (VMDetail, error) {
	start := time.Now()
	logger.Debug("azure fetch vm detail start", "vm", vmName, "rg", rgName)

	args := []string{"vm", "show", "--name", vmName, "--resource-group", rgName, "--subscription", subName}
	if tenantID != "" {
		args = append(args, "--tenant", tenantID)
	}
	data, err := m.RunCommand(args...)
	if err != nil {
		return VMDetail{}, fmt.Errorf("vm show: %w", err)
	}
	detail, err := ParseVMDetail(data)
	if err != nil {
		return VMDetail{}, err
	}

	logger.Debug("azure fetch vm detail complete", "vm", vmName, "elapsed", time.Since(start))
	return detail, nil
}

// ParseVMDetail parses the JSON output of `az vm show`.
func ParseVMDetail(data []byte) (VMDetail, error) {
	var raw struct {
		Name            string `json:"name"`
		ResourceGroup   string `json:"resourceGroup"`
		Location        string `json:"location"`
		HardwareProfile struct {
			VMSize string `json:"vmSize"`
		} `json:"hardwareProfile"`
		StorageProfile struct {
			OsDisk struct {
				OsType     string `json:"osType"`
				Name       string `json:"name"`
				DiskSizeGB int    `json:"diskSizeGb"`
			} `json:"osDisk"`
			ImageReference *struct {
				Offer     string `json:"offer"`
				Publisher string `json:"publisher"`
				Sku       string `json:"sku"`
			} `json:"imageReference"`
		} `json:"storageProfile"`
		PowerState     string            `json:"powerState"`
		PrivateIps     string            `json:"privateIps"`
		PublicIps      string            `json:"publicIps"`
		ID             string            `json:"id"`
		Tags           map[string]string `json:"tags"`
		NetworkProfile struct {
			NetworkInterfaces []struct {
				ID string `json:"id"`
			} `json:"networkInterfaces"`
		} `json:"networkProfile"`
		OsProfile struct {
			ComputerName string `json:"computerName"`
		} `json:"osProfile"`
		TimeCreated string `json:"timeCreated"`
	}

	if err := json.Unmarshal(data, &raw); err != nil {
		return VMDetail{}, fmt.Errorf("parsing vm detail: %w", err)
	}

	osDisk := ""
	if raw.StorageProfile.ImageReference != nil {
		osDisk = raw.StorageProfile.ImageReference.Offer + " " + raw.StorageProfile.ImageReference.Sku
	}

	nicName := ""
	if len(raw.NetworkProfile.NetworkInterfaces) > 0 {
		nicID := raw.NetworkProfile.NetworkInterfaces[0].ID
		parts := splitLast(nicID, "/")
		if parts != "" {
			nicName = parts
		}
	}

	return VMDetail{
		VM: VM{
			Name:          raw.Name,
			ResourceGroup: raw.ResourceGroup,
			Location:      raw.Location,
			VMSize:        raw.HardwareProfile.VMSize,
			OSType:        raw.StorageProfile.OsDisk.OsType,
			OSDisk:        osDisk,
			PrivateIP:     raw.PrivateIps,
			Hostname:      raw.OsProfile.ComputerName,
			PublicIP:      raw.PublicIps,
			PowerState:    normalizePowerState(raw.PowerState),
			ID:            raw.ID,
		},
		Tags:         raw.Tags,
		OSDiskName:   raw.StorageProfile.OsDisk.Name,
		OSDiskSizeGB: raw.StorageProfile.OsDisk.DiskSizeGB,
		CreatedTime:  raw.TimeCreated,
		NICName:      nicName,
	}, nil
}

// FetchActivityLog fetches recent activity log entries for a resource group.
// Note: --resource-group filter is case-sensitive in Azure CLI but Resource Graph
// returns lowercase RG names. We query at subscription level and filter client-side.
func FetchActivityLog(m *Manager, rgName, subName, tenantID string, hours int, logger *slog.Logger) ([]ActivityLogEntry, error) {
	start := time.Now()
	logger.Debug("azure fetch activity log start", "rg", rgName, "subscription", subName, "hours", hours)

	if hours <= 0 {
		hours = 3
	}
	startTime := time.Now().UTC().Add(-time.Duration(hours) * time.Hour).Format(time.RFC3339)

	// Resolve original RG casing (activity log API is case-sensitive)
	rgData, err := m.RunCommand("group", "show", "--name", rgName, "--subscription", subName, "--query", "name", "-o", "tsv")
	if err != nil {
		logger.Debug("azure rg casing resolve failed, using as-is", "rg", rgName, "err", err)
	} else {
		resolved := strings.TrimSpace(string(rgData))
		if resolved != "" {
			rgName = resolved
		}
	}

	args := []string{"monitor", "activity-log", "list",
		"--resource-group", rgName,
		"--subscription", subName,
		"--start-time", startTime,
		"--query", "[].{t:eventTimestamp,op:operationName.localizedValue,s:status.localizedValue,c:caller,rg:resourceGroupName,r:resourceId}",
	}
	if tenantID != "" {
		args = append(args, "--tenant", tenantID)
	}
	data, err := m.RunCommand(args...)
	if err != nil {
		return nil, fmt.Errorf("activity log: %w", err)
	}
	entries, err := ParseActivityLog(data)
	if err != nil {
		return nil, err
	}

	logger.Debug("azure fetch activity log complete", "rg", rgName, "count", len(entries), "elapsed", time.Since(start))
	return entries, nil
}

// FetchVMPowerStates queries Resource Graph for power states of specific VMs.
// Returns a map of VM name (lowercased) → normalized power state.
func FetchVMPowerStates(m *Manager, subID string, vmNames []string, logger *slog.Logger) (map[string]string, error) {
	start := time.Now()
	logger.Debug("azure poll vm states start", "count", len(vmNames))

	// Build name filter: name in~ ('vm-01','vm-02')
	// Skip names containing single quotes to prevent KQL injection.
	var safe []string
	for _, n := range vmNames {
		if !strings.ContainsRune(n, '\'') {
			safe = append(safe, "'"+n+"'")
		}
	}
	if len(safe) == 0 {
		return nil, nil
	}
	nameFilter := strings.Join(safe, ",")

	query := fmt.Sprintf("Resources | where type =~ 'microsoft.compute/virtualMachines' "+
		"| where name in~ (%s) "+
		"| extend powerState = properties.extended.instanceView.powerState.code "+
		"| project name, powerState", nameFilter)

	args := []string{"graph", "query", "-q", query, "--subscriptions", subID, "--first", "1000"}
	data, err := m.RunCommand(args...)
	if err != nil {
		return nil, fmt.Errorf("graph poll query: %w", err)
	}

	states, err := ParseVMPowerStates(data)
	if err != nil {
		return nil, err
	}

	logger.Debug("azure poll vm states complete", "count", len(states), "elapsed", time.Since(start))
	return states, nil
}

// ParseVMPowerStates parses Resource Graph output into a name → power state map.
func ParseVMPowerStates(data []byte) (map[string]string, error) {
	var result struct {
		Data []struct {
			Name       string `json:"name"`
			PowerState string `json:"powerState"`
		} `json:"data"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("parsing power states: %w", err)
	}

	states := make(map[string]string, len(result.Data))
	for _, r := range result.Data {
		states[strings.ToLower(r.Name)] = normalizePowerState(r.PowerState)
	}
	return states, nil
}

// splitLast returns the part after the last occurrence of sep in s.
func splitLast(s, sep string) string {
	if i := strings.LastIndex(s, sep); i >= 0 {
		return s[i+len(sep):]
	}
	return s
}
