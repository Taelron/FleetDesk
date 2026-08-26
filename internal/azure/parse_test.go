package azure

import (
	"testing"
)

func TestParseVMList(t *testing.T) {
	tests := []struct {
		name      string
		input     []byte
		wantCount int
		wantErr   bool
		check     func(t *testing.T, vms []VM)
	}{
		{
			name:      "nil input",
			input:     nil,
			wantCount: 0,
			check: func(t *testing.T, vms []VM) {
				if vms != nil {
					t.Errorf("expected nil slice, got %v", vms)
				}
			},
		},
		{
			name:      "empty array",
			input:     []byte(`[]`),
			wantCount: 0,
		},
		{
			name: "single Linux VM with all fields",
			input: []byte(`[{
				"name": "vm-aap-dev-01",
				"resourceGroup": "RG-AAP-DEV",
				"location": "westeurope",
				"hardwareProfile": {"vmSize": "Standard_D2s_v3"},
				"storageProfile": {
					"osDisk": {"osType": "Linux"},
					"imageReference": {"offer": "RHEL", "publisher": "RedHat", "sku": "9-lvm-gen2"}
				},
				"powerState": "VM running",
				"privateIps": "10.0.1.4",
				"publicIps": "",
				"id": "/subscriptions/xxx/resourceGroups/RG-AAP-DEV/providers/Microsoft.Compute/virtualMachines/vm-aap-dev-01"
			}]`),
			wantCount: 1,
			check: func(t *testing.T, vms []VM) {
				vm := vms[0]
				if vm.Name != "vm-aap-dev-01" {
					t.Errorf("Name = %q, want %q", vm.Name, "vm-aap-dev-01")
				}
				if vm.ResourceGroup != "RG-AAP-DEV" {
					t.Errorf("ResourceGroup = %q, want %q", vm.ResourceGroup, "RG-AAP-DEV")
				}
				if vm.Location != "westeurope" {
					t.Errorf("Location = %q, want %q", vm.Location, "westeurope")
				}
				if vm.VMSize != "Standard_D2s_v3" {
					t.Errorf("VMSize = %q, want %q", vm.VMSize, "Standard_D2s_v3")
				}
				if vm.OSType != "Linux" {
					t.Errorf("OSType = %q, want %q", vm.OSType, "Linux")
				}
				if vm.OSDisk != "RHEL 9-lvm-gen2" {
					t.Errorf("OSDisk = %q, want %q", vm.OSDisk, "RHEL 9-lvm-gen2")
				}
				if vm.PrivateIP != "10.0.1.4" {
					t.Errorf("PrivateIP = %q, want %q", vm.PrivateIP, "10.0.1.4")
				}
				if vm.PublicIP != "" {
					t.Errorf("PublicIP = %q, want empty", vm.PublicIP)
				}
				if vm.PowerState != "running" {
					t.Errorf("PowerState = %q, want %q (should be normalized)", vm.PowerState, "running")
				}
				if vm.ID != "/subscriptions/xxx/resourceGroups/RG-AAP-DEV/providers/Microsoft.Compute/virtualMachines/vm-aap-dev-01" {
					t.Errorf("ID = %q", vm.ID)
				}
			},
		},
		{
			name: "single Windows VM",
			input: []byte(`[{
				"name": "vm-win-dev-01",
				"resourceGroup": "RG-WIN-DEV",
				"location": "westeurope",
				"hardwareProfile": {"vmSize": "Standard_D4s_v3"},
				"storageProfile": {
					"osDisk": {"osType": "Windows"},
					"imageReference": {"offer": "WindowsServer", "publisher": "MicrosoftWindowsServer", "sku": "2022-datacenter-g2"}
				},
				"powerState": "VM running",
				"privateIps": "10.0.2.10",
				"publicIps": "20.1.2.3",
				"id": "/subscriptions/xxx/resourceGroups/RG-WIN-DEV/providers/Microsoft.Compute/virtualMachines/vm-win-dev-01"
			}]`),
			wantCount: 1,
			check: func(t *testing.T, vms []VM) {
				vm := vms[0]
				if vm.OSType != "Windows" {
					t.Errorf("OSType = %q, want %q", vm.OSType, "Windows")
				}
				if vm.PublicIP != "20.1.2.3" {
					t.Errorf("PublicIP = %q, want %q", vm.PublicIP, "20.1.2.3")
				}
				if vm.OSDisk != "WindowsServer 2022-datacenter-g2" {
					t.Errorf("OSDisk = %q, want %q", vm.OSDisk, "WindowsServer 2022-datacenter-g2")
				}
			},
		},
		{
			name: "multiple VMs with mixed OS and power states",
			input: []byte(`[
				{
					"name": "vm-linux-01",
					"resourceGroup": "RG-LINUX",
					"location": "westeurope",
					"hardwareProfile": {"vmSize": "Standard_D2s_v3"},
					"storageProfile": {
						"osDisk": {"osType": "Linux"},
						"imageReference": {"offer": "RHEL", "publisher": "RedHat", "sku": "9-lvm-gen2"}
					},
					"powerState": "VM running",
					"privateIps": "10.0.1.1",
					"publicIps": "",
					"id": "/subscriptions/xxx/resourceGroups/RG-LINUX/providers/Microsoft.Compute/virtualMachines/vm-linux-01"
				},
				{
					"name": "vm-win-01",
					"resourceGroup": "RG-WIN",
					"location": "northeurope",
					"hardwareProfile": {"vmSize": "Standard_D4s_v3"},
					"storageProfile": {
						"osDisk": {"osType": "Windows"},
						"imageReference": {"offer": "WindowsServer", "publisher": "MicrosoftWindowsServer", "sku": "2022-datacenter-g2"}
					},
					"powerState": "VM deallocated",
					"privateIps": "10.0.2.1",
					"publicIps": "",
					"id": "/subscriptions/xxx/resourceGroups/RG-WIN/providers/Microsoft.Compute/virtualMachines/vm-win-01"
				},
				{
					"name": "vm-linux-02",
					"resourceGroup": "RG-LINUX",
					"location": "westeurope",
					"hardwareProfile": {"vmSize": "Standard_B2s"},
					"storageProfile": {
						"osDisk": {"osType": "Linux"},
						"imageReference": {"offer": "0001-com-ubuntu-server-jammy", "publisher": "Canonical", "sku": "22_04-lts-gen2"}
					},
					"powerState": "VM stopped",
					"privateIps": "10.0.1.2",
					"publicIps": "",
					"id": "/subscriptions/xxx/resourceGroups/RG-LINUX/providers/Microsoft.Compute/virtualMachines/vm-linux-02"
				}
			]`),
			wantCount: 3,
			check: func(t *testing.T, vms []VM) {
				if vms[0].PowerState != "running" {
					t.Errorf("vms[0].PowerState = %q, want %q", vms[0].PowerState, "running")
				}
				if vms[1].PowerState != "deallocated" {
					t.Errorf("vms[1].PowerState = %q, want %q", vms[1].PowerState, "deallocated")
				}
				if vms[1].OSType != "Windows" {
					t.Errorf("vms[1].OSType = %q, want %q", vms[1].OSType, "Windows")
				}
				if vms[2].PowerState != "stopped" {
					t.Errorf("vms[2].PowerState = %q, want %q", vms[2].PowerState, "stopped")
				}
			},
		},
		{
			name: "VM with missing optional fields",
			input: []byte(`[{
				"name": "vm-minimal-01",
				"resourceGroup": "RG-MIN",
				"location": "westeurope",
				"hardwareProfile": {"vmSize": "Standard_B1s"},
				"storageProfile": {
					"osDisk": {"osType": "Linux"},
					"imageReference": null
				},
				"powerState": "VM running",
				"privateIps": "10.0.3.1",
				"publicIps": "",
				"id": "/subscriptions/xxx/resourceGroups/RG-MIN/providers/Microsoft.Compute/virtualMachines/vm-minimal-01"
			}]`),
			wantCount: 1,
			check: func(t *testing.T, vms []VM) {
				vm := vms[0]
				if vm.OSDisk != "" {
					t.Errorf("OSDisk = %q, want empty for nil imageReference", vm.OSDisk)
				}
				if vm.PublicIP != "" {
					t.Errorf("PublicIP = %q, want empty", vm.PublicIP)
				}
			},
		},
		{
			name:    "invalid JSON",
			input:   []byte(`{not valid json`),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vms, err := ParseVMList(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(vms) != tt.wantCount {
				t.Fatalf("got %d VMs, want %d", len(vms), tt.wantCount)
			}
			if tt.check != nil {
				tt.check(t, vms)
			}
		})
	}
}

func TestParseResourceGroupList(t *testing.T) {
	tests := []struct {
		name      string
		input     []byte
		wantCount int
		wantErr   bool
		check     func(t *testing.T, rgs []ResourceGroup)
	}{
		{
			name:      "nil input",
			input:     nil,
			wantCount: 0,
			check: func(t *testing.T, rgs []ResourceGroup) {
				if rgs != nil {
					t.Errorf("expected nil slice, got %v", rgs)
				}
			},
		},
		{
			name:      "empty array",
			input:     []byte(`[]`),
			wantCount: 0,
		},
		{
			name: "single resource group",
			input: []byte(`[{
				"name": "RG-AAP-DEV",
				"location": "westeurope",
				"properties": {"provisioningState": "Succeeded"},
				"id": "/subscriptions/xxx/resourceGroups/RG-AAP-DEV"
			}]`),
			wantCount: 1,
			check: func(t *testing.T, rgs []ResourceGroup) {
				rg := rgs[0]
				if rg.Name != "RG-AAP-DEV" {
					t.Errorf("Name = %q, want %q", rg.Name, "RG-AAP-DEV")
				}
				if rg.Location != "westeurope" {
					t.Errorf("Location = %q, want %q", rg.Location, "westeurope")
				}
				if rg.State != "Succeeded" {
					t.Errorf("State = %q, want %q", rg.State, "Succeeded")
				}
				if rg.ID != "/subscriptions/xxx/resourceGroups/RG-AAP-DEV" {
					t.Errorf("ID = %q", rg.ID)
				}
			},
		},
		{
			name: "multiple resource groups",
			input: []byte(`[
				{
					"name": "RG-AAP-DEV",
					"location": "westeurope",
					"properties": {"provisioningState": "Succeeded"},
					"id": "/subscriptions/xxx/resourceGroups/RG-AAP-DEV"
				},
				{
					"name": "RG-AKS-QUA",
					"location": "northeurope",
					"properties": {"provisioningState": "Succeeded"},
					"id": "/subscriptions/yyy/resourceGroups/RG-AKS-QUA"
				},
				{
					"name": "RG-TEMP",
					"location": "westeurope",
					"properties": {"provisioningState": "Deleting"},
					"id": "/subscriptions/xxx/resourceGroups/RG-TEMP"
				}
			]`),
			wantCount: 3,
			check: func(t *testing.T, rgs []ResourceGroup) {
				if rgs[1].Location != "northeurope" {
					t.Errorf("rgs[1].Location = %q, want %q", rgs[1].Location, "northeurope")
				}
				if rgs[2].State != "Deleting" {
					t.Errorf("rgs[2].State = %q, want %q", rgs[2].State, "Deleting")
				}
			},
		},
		{
			name:    "invalid JSON",
			input:   []byte(`not json`),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rgs, err := ParseResourceGroupList(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(rgs) != tt.wantCount {
				t.Fatalf("got %d RGs, want %d", len(rgs), tt.wantCount)
			}
			if tt.check != nil {
				tt.check(t, rgs)
			}
		})
	}
}

func TestParseAKSList(t *testing.T) {
	tests := []struct {
		name      string
		input     []byte
		wantCount int
		wantErr   bool
		check     func(t *testing.T, clusters []AKSCluster)
	}{
		{
			name:      "nil input",
			input:     nil,
			wantCount: 0,
			check: func(t *testing.T, clusters []AKSCluster) {
				if clusters != nil {
					t.Errorf("expected nil slice, got %v", clusters)
				}
			},
		},
		{
			name: "single cluster with one agent pool",
			input: []byte(`[{
				"name": "aks-app-dev-blue",
				"resourceGroup": "RG-AKS-DEV",
				"location": "westeurope",
				"kubernetesVersion": "1.30.7",
				"powerState": {"code": "Running"},
				"agentPoolProfiles": [
					{"name": "system", "count": 3}
				],
				"id": "/subscriptions/xxx/resourceGroups/RG-AKS-DEV/providers/Microsoft.ContainerService/managedClusters/aks-app-dev-blue"
			}]`),
			wantCount: 1,
			check: func(t *testing.T, clusters []AKSCluster) {
				c := clusters[0]
				if c.Name != "aks-app-dev-blue" {
					t.Errorf("Name = %q, want %q", c.Name, "aks-app-dev-blue")
				}
				if c.ResourceGroup != "RG-AKS-DEV" {
					t.Errorf("ResourceGroup = %q, want %q", c.ResourceGroup, "RG-AKS-DEV")
				}
				if c.Location != "westeurope" {
					t.Errorf("Location = %q, want %q", c.Location, "westeurope")
				}
				if c.KubernetesVersion != "1.30.7" {
					t.Errorf("KubernetesVersion = %q, want %q", c.KubernetesVersion, "1.30.7")
				}
				if c.PowerState != "Running" {
					t.Errorf("PowerState = %q, want %q", c.PowerState, "Running")
				}
				if c.NodeCount != 3 {
					t.Errorf("NodeCount = %d, want 3", c.NodeCount)
				}
				if c.ID != "/subscriptions/xxx/resourceGroups/RG-AKS-DEV/providers/Microsoft.ContainerService/managedClusters/aks-app-dev-blue" {
					t.Errorf("ID = %q", c.ID)
				}
			},
		},
		{
			name: "cluster with multiple agent pools sums node count",
			input: []byte(`[{
				"name": "aks-app-qua-green",
				"resourceGroup": "RG-AKS-QUA",
				"location": "westeurope",
				"kubernetesVersion": "1.31.2",
				"powerState": {"code": "Running"},
				"agentPoolProfiles": [
					{"name": "system", "count": 3},
					{"name": "user", "count": 5},
					{"name": "gpu", "count": 2}
				],
				"id": "/subscriptions/yyy/resourceGroups/RG-AKS-QUA/providers/Microsoft.ContainerService/managedClusters/aks-app-qua-green"
			}]`),
			wantCount: 1,
			check: func(t *testing.T, clusters []AKSCluster) {
				if clusters[0].NodeCount != 10 {
					t.Errorf("NodeCount = %d, want 10 (3+5+2)", clusters[0].NodeCount)
				}
			},
		},
		{
			name:    "invalid JSON",
			input:   []byte(`[{"broken`),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clusters, err := ParseAKSList(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(clusters) != tt.wantCount {
				t.Fatalf("got %d clusters, want %d", len(clusters), tt.wantCount)
			}
			if tt.check != nil {
				tt.check(t, clusters)
			}
		})
	}
}

func TestParseSubscriptionList(t *testing.T) {
	tests := []struct {
		name      string
		input     []byte
		wantCount int
		wantErr   bool
		check     func(t *testing.T, subs []AzureSubscription)
	}{
		{
			name:      "nil input",
			input:     nil,
			wantCount: 0,
			check: func(t *testing.T, subs []AzureSubscription) {
				if subs != nil {
					t.Errorf("expected nil slice, got %v", subs)
				}
			},
		},
		{
			name: "single subscription",
			input: []byte(`[{
				"name": "APP-DEV",
				"id": "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
				"state": "Enabled",
				"isDefault": true
			}]`),
			wantCount: 1,
			check: func(t *testing.T, subs []AzureSubscription) {
				s := subs[0]
				if s.Name != "APP-DEV" {
					t.Errorf("Name = %q, want %q", s.Name, "APP-DEV")
				}
				if s.ID != "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx" {
					t.Errorf("ID = %q", s.ID)
				}
				if s.State != "Enabled" {
					t.Errorf("State = %q, want %q", s.State, "Enabled")
				}
				if !s.IsDefault {
					t.Error("IsDefault = false, want true")
				}
			},
		},
		{
			name: "multiple subscriptions with different states",
			input: []byte(`[
				{
					"name": "APP-DEV",
					"id": "aaaa-bbbb-cccc-dddd",
					"state": "Enabled",
					"isDefault": true
				},
				{
					"name": "APP-QUA",
					"id": "eeee-ffff-gggg-hhhh",
					"state": "Enabled",
					"isDefault": false
				},
				{
					"name": "OLD-SUB",
					"id": "iiii-jjjj-kkkk-llll",
					"state": "Disabled",
					"isDefault": false
				}
			]`),
			wantCount: 3,
			check: func(t *testing.T, subs []AzureSubscription) {
				if subs[0].IsDefault != true {
					t.Error("subs[0].IsDefault = false, want true")
				}
				if subs[1].IsDefault != false {
					t.Error("subs[1].IsDefault = true, want false")
				}
				if subs[2].State != "Disabled" {
					t.Errorf("subs[2].State = %q, want %q", subs[2].State, "Disabled")
				}
			},
		},
		{
			name:    "invalid JSON",
			input:   []byte(`{"not": "an array"}`),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			subs, err := ParseSubscriptionList(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(subs) != tt.wantCount {
				t.Fatalf("got %d subscriptions, want %d", len(subs), tt.wantCount)
			}
			if tt.check != nil {
				tt.check(t, subs)
			}
		})
	}
}

func TestParseCLIVersion(t *testing.T) {
	tests := []struct {
		name    string
		input   []byte
		want    string
		wantErr bool
	}{
		{
			name:  "valid version",
			input: []byte(`{"azure-cli": "2.67.0"}`),
			want:  "2.67.0",
		},
		{
			name:  "missing field returns empty",
			input: []byte(`{}`),
			want:  "",
		},
		{
			name:    "invalid JSON",
			input:   []byte(`not json`),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseCLIVersion(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("ParseCLIVersion() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNormalizePowerState(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"VM running", "running"},
		{"VM deallocated", "deallocated"},
		{"VM stopped", "stopped"},
		{"PowerState/running", "running"},
		{"PowerState/deallocated", "deallocated"},
		{"running", "running"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := normalizePowerState(tt.input)
			if got != tt.want {
				t.Errorf("normalizePowerState(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseAccountShow(t *testing.T) {
	tests := []struct {
		name    string
		input   []byte
		want    SubscriptionProbeInfo
		wantErr bool
	}{
		{
			name: "full output",
			input: []byte(`{
				"name": "APP-DEV",
				"id": "ac9725d8-a67c-41f1-8e1c-d210756761d2",
				"state": "Enabled",
				"tenantDisplayName": "Fluxys DEV",
				"tenantId": "4aedce13-d621-45e1-823f-840dfaf1aa8e",
				"user": {"name": "gaetan.jaminon@dev.fluxys.com", "type": "user"}
			}`),
			want: SubscriptionProbeInfo{
				ID:     "ac9725d8-a67c-41f1-8e1c-d210756761d2",
				State:  "Enabled",
				Tenant: "Fluxys DEV",
				User:   "gaetan.jaminon@dev.fluxys.com",
			},
		},
		{
			name:  "minimal fields",
			input: []byte(`{"id": "xxx", "state": "Enabled"}`),
			want: SubscriptionProbeInfo{
				ID:    "xxx",
				State: "Enabled",
			},
		},
		{
			name:    "invalid JSON",
			input:   []byte(`not json`),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseAccountShow(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.ID != tt.want.ID {
				t.Errorf("ID = %q, want %q", got.ID, tt.want.ID)
			}
			if got.State != tt.want.State {
				t.Errorf("State = %q, want %q", got.State, tt.want.State)
			}
			if got.Tenant != tt.want.Tenant {
				t.Errorf("Tenant = %q, want %q", got.Tenant, tt.want.Tenant)
			}
			if got.User != tt.want.User {
				t.Errorf("User = %q, want %q", got.User, tt.want.User)
			}
		})
	}
}

func TestParseVMListHostname(t *testing.T) {
	input := []byte(`[{
		"name": "vm-01",
		"resourceGroup": "RG",
		"location": "westeurope",
		"hardwareProfile": {"vmSize": "Standard_D2s_v3"},
		"storageProfile": {
			"osDisk": {"osType": "Linux"},
			"imageReference": {"offer": "RHEL", "publisher": "RedHat", "sku": "9-lvm-gen2"}
		},
		"osProfile": {"computerName": "vm-01.cnp.dev.fluxys.cloud"},
		"powerState": "VM running",
		"privateIps": "10.0.1.4",
		"publicIps": "",
		"id": "/sub/xxx"
	}]`)
	vms, err := ParseVMList(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(vms) != 1 {
		t.Fatalf("got %d VMs, want 1", len(vms))
	}
	if vms[0].Hostname != "vm-01.cnp.dev.fluxys.cloud" {
		t.Errorf("Hostname = %q, want %q", vms[0].Hostname, "vm-01.cnp.dev.fluxys.cloud")
	}
}

func TestParseActivityLog(t *testing.T) {
	tests := []struct {
		name      string
		input     []byte
		wantCount int
		wantErr   bool
		check     func(t *testing.T, entries []ActivityLogEntry)
	}{
		{
			name:      "nil input",
			input:     nil,
			wantCount: 0,
		},
		{
			name:      "empty array",
			input:     []byte("[]"),
			wantCount: 0,
		},
		{
			name: "multiple entries",
			input: []byte(`[
				{
					"t": "2026-04-08T14:30:00.1234567Z",
					"op": "Start Virtual Machine",
					"s": "Succeeded",
					"c": "gaetan@dev.fluxys.com",
					"rg": "rg-app-dev",
					"r": "/subscriptions/xxx/providers/Microsoft.Compute/virtualMachines/vm-01"
				},
				{
					"t": "2026-04-08T12:00:00.0000000Z",
					"op": "Deallocate Virtual Machine",
					"s": "Failed",
					"c": "32f0a514-cfbe-420a-9fc5-dcc98290753b",
					"rg": "rg-app-dev",
					"r": "/subscriptions/xxx/providers/Microsoft.Compute/virtualMachines/vm-02"
				}
			]`),
			wantCount: 2,
			check: func(t *testing.T, entries []ActivityLogEntry) {
				if entries[0].Operation != "Start Virtual Machine" {
					t.Errorf("entries[0].Operation = %q", entries[0].Operation)
				}
				if entries[0].Status != "Succeeded" {
					t.Errorf("entries[0].Status = %q", entries[0].Status)
				}
				if entries[0].Caller != "gaetan@dev.fluxys.com" {
					t.Errorf("entries[0].Caller = %q", entries[0].Caller)
				}
				if entries[1].Status != "Failed" {
					t.Errorf("entries[1].Status = %q", entries[1].Status)
				}
			},
		},
		{
			name:    "invalid JSON",
			input:   []byte("not json"),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entries, err := ParseActivityLog(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(entries) != tt.wantCount {
				t.Fatalf("got %d entries, want %d", len(entries), tt.wantCount)
			}
			if tt.check != nil {
				tt.check(t, entries)
			}
		})
	}
}
