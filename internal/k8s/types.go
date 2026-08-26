package k8s

// ClusterStatus represents the connectivity state of a K8s cluster.
type ClusterStatus int

// ClusterStatus values, in connectivity-state order.
const (
	ClusterChecking ClusterStatus = iota
	ClusterOnline
	ClusterError
)

// K8sClusterItem is the runtime representation of a cluster from config.
type K8sClusterItem struct {
	Name         string
	Status       ClusterStatus
	K8sVersion   string
	Error        string
	ContextCount int // number of matching kubectl contexts
}

// K8sContext represents a kubectl context matching a cluster.
type K8sContext struct {
	Name      string // context name
	Cluster   string // cluster name
	User      string // auth info
	Namespace string // default namespace
	Current   bool   // true if this is the active context
}

// K8sResourceCounts holds resource counts for a cluster context.
type K8sResourceCounts struct {
	Namespaces int
	Nodes      int
	ArgoApps   int
}

// K8sNode represents a Kubernetes node.
type K8sNode struct {
	Name    string
	Pool    string // agentpool label
	Status  string // Ready, NotReady
	Version string // kubeletVersion
	CPU     string // capacity.cpu
	Memory  string // capacity.memory (human-readable)
	Pods    string // capacity.pods
	OS      string // os/arch
	VMSize  string // node.kubernetes.io/instance-type label
	Taints  int    // taint count
	Age     string // human-readable age
	// Top data (merged from kubectl top nodes)
	CPUUsage string // e.g. "321m"
	CPUPct   string // e.g. "8%"
	MemUsage string // e.g. "6211Mi"
	MemPct   string // e.g. "23%"
	CPUA     string // allocatable CPU (e.g. "3860m")
}

// K8sNodeDetail holds extended node properties.
type K8sNodeDetail struct {
	K8sNode
	InternalIP        string
	PodCIDR           string
	Unschedulable     bool
	ContainerRuntime  string
	KernelVersion     string
	OSImage           string
	Created           string
	AllocatableCPU    string
	AllocatableMemory string
	AllocatablePods   string
	ImageCount        int
	Conditions        []K8sCondition
	Taints            []K8sTaint
	Labels            map[string]string
}

// K8sNodeUsage holds CPU/Memory usage from kubectl top node.
type K8sNodeUsage struct {
	CPUUsage   string // e.g. "850m"
	CPUPercent string // e.g. "21%"
	MemUsage   string // e.g. "18Gi"
	MemPercent string // e.g. "58%"
}

// K8sNodePod represents a pod running on a node.
type K8sNodePod struct {
	Namespace string
	Name      string
	Status    string // Running, Pending, Failed, etc.
	Ready     string // "2/2" format
	CPUReq    string // e.g. "60m"
	CPULim    string // e.g. "200m"
	MemReq    string // e.g. "428Mi"
	MemLim    string // e.g. "556Mi"
	Age       string // human-readable age
}

// K8sCondition represents a node condition.
type K8sCondition struct {
	Type   string
	Status string
}

// K8sTaint represents a node taint.
type K8sTaint struct {
	Key    string
	Value  string
	Effect string
}

// K8sNamespace represents a Kubernetes namespace with resource counts.
type K8sNamespace struct {
	Name        string
	Status      string // Active, Terminating
	PodCount    int
	DeployCount int
	STSCount    int
	DSCount     int
	Age         string
}

// K8sWorkload represents a Deployment, StatefulSet, or DaemonSet.
type K8sWorkload struct {
	Kind      string // Deployment, StatefulSet, DaemonSet
	Name      string
	Ready     string // "2/2"
	UpToDate  int    // deployments only
	Available int    // deployments only
	Age       string
}

// K8sPod represents a pod in a namespace.
type K8sPod struct {
	Name      string
	Namespace string
	Status    string // Running, Pending, Failed, etc.
	Ready     string // "1/1"
	Restarts  int
	Node      string
	IP        string
	Age       string
}

// K8sPodDetail holds extended pod properties.
type K8sPodDetail struct {
	K8sPod
	Containers     []K8sContainer
	InitContainers []K8sContainer
	Conditions     []K8sCondition // reuse existing type
	Labels         map[string]string
	Annotations    map[string]string
}

// K8sContainer represents a container in a pod.
type K8sContainer struct {
	Name     string
	Image    string
	State    string // running, waiting, terminated
	Ready    bool
	Restarts int
	CPUReq   string
	CPULim   string
	MemReq   string
	MemLim   string
}

// K8sLogEntry represents a single parsed log line from kubectl logs.
type K8sLogEntry struct {
	Timestamp  string // "10:30:00" short display format
	Pod        string // pod name
	Level      string // "Debug", "Info", "Warning", "Error" (extracted from JSON)
	Message    string // log message content (parsed/extracted)
	RawMessage string // original unparsed message (for detail view)
	RawTime    string // full RFC3339 for sorting
}
