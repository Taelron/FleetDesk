package azure

// VM represents an Azure virtual machine.
type VM struct {
	Name          string
	ResourceGroup string
	Location      string
	VMSize        string
	OSType        string // Linux, Windows
	OSDisk        string // OS distro info (e.g. "RHEL 9", "WindowsServer 2022")
	PrivateIP     string
	Hostname      string
	PublicIP      string
	PowerState    string // running, deallocated, stopped
	ID            string // full Azure resource ID
	VNet          string // virtual network name
	Subnet        string // subnet name
}

// ResourceGroup represents an Azure resource group from `az group list`.
type ResourceGroup struct {
	Name     string
	Location string
	State    string // Succeeded, etc.
	ID       string
}

// AKSCluster represents an Azure Kubernetes cluster from `az aks list`.
type AKSCluster struct {
	Name              string
	ResourceGroup     string
	Location          string
	KubernetesVersion string
	NodeCount         int
	PowerState        string // Running, Stopped
	ProvisioningState string // Succeeded, Starting, Stopping
	CreatedDate       string // from tags.created_Date
	Tags              map[string]string
	ID                string
}

// AzureSubscription represents an Azure subscription from `az account list`.
// Named AzureSubscription to avoid collision with config.Subscription (RHEL subscription-manager).
type AzureSubscription struct {
	Name      string
	ID        string
	State     string // Enabled, Disabled
	IsDefault bool
}

// SubscriptionStatus represents the probe state of an Azure subscription.
type SubscriptionStatus int

// SubscriptionStatus values, in probe-state order.
const (
	SubConnecting SubscriptionStatus = iota
	SubOnline
	SubError
)

// SubscriptionProbeInfo holds the result of checking access to an Azure subscription.
type SubscriptionProbeInfo struct {
	ID     string // subscription UUID
	State  string // Enabled, Disabled
	Tenant string // tenantDisplayName
	User   string // user.name
}

// AzureSubscriptionItem is the runtime representation of a subscription with access state.
type AzureSubscriptionItem struct {
	Name     string
	TenantID string
	Status   SubscriptionStatus
	Error    string
	ID       string // subscription UUID
	State    string // Enabled, Disabled
	Tenant   string // tenantDisplayName
	User     string // user.name
}

// AzureResourceCounts holds resource counts for a subscription.
type AzureResourceCounts struct {
	VMs int
	RGs int
	AKS int
}

// VMDetail holds extended VM properties from `az vm show -d`.
type VMDetail struct {
	VM           // embedded, all list fields
	Tags         map[string]string
	OSDiskName   string
	OSDiskSizeGB int
	CreatedTime  string
	NICName      string
}

// ActivityLogEntry represents an Azure activity log entry.
type ActivityLogEntry struct {
	Timestamp     string // eventTimestamp (ISO-8601)
	ResourceGroup string // resourceGroupName
	Operation     string // operationName.localizedValue
	Resource      string // resource name extracted from resourceId
	Status        string // status.localizedValue (Succeeded/Failed/Started)
	Caller        string // caller (email or service principal UUID)
}

// AKSNodePool represents a node pool in an AKS cluster.
type AKSNodePool struct {
	Name      string
	Mode      string // System, User
	VMSize    string
	Count     int
	MinCount  int
	MaxCount  int
	Version   string // currentOrchestratorVersion
	AutoScale bool
}

// AKSDetail holds extended AKS cluster properties including node pools.
type AKSDetail struct {
	AKSCluster    // embedded
	NetworkPlugin string
	Pools         []AKSNodePool
}
