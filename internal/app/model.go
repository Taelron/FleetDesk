package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Gaetan-Jaminon/fleetdesk/internal/azure"
	"github.com/Gaetan-Jaminon/fleetdesk/internal/config"
	"github.com/Gaetan-Jaminon/fleetdesk/internal/k8s"
	"github.com/Gaetan-Jaminon/fleetdesk/internal/notes"
	"github.com/Gaetan-Jaminon/fleetdesk/internal/probes"
	"github.com/Gaetan-Jaminon/fleetdesk/internal/ssh"
)

// tickMsg triggers a periodic host probe refresh.
type tickMsg time.Time

// azureResourceCountsMsg is sent when Azure resource counts are fetched.
type azureResourceCountsMsg struct {
	counts azure.AzureResourceCounts
	err    error
}

type fetchAzureVMsMsg struct {
	vms []azure.VM
	err error
}

type fetchAzureVMDetailMsg struct {
	detail azure.VMDetail
	err    error
}

type actionResultMsg struct {
	resourceType string
	name         string
	action       string
	err          error
}

// transition tracks an in-flight resource action for optimistic UI.
type transition struct {
	// Data fields — for display, overlay key, logging. NOT used for dispatch.
	ResourceType string // "vm", "aks", "k8s-pod", "k8s-context"
	ResourceName string
	Action       string // "start", "deallocate", "restart", "stop", "delete"
	Display      string // "starting...", "deleting...", or real state from poll
	TargetState  string // "running", "deallocated", "stopped", "gone"
	Confirmed    bool   // true once transitioning state seen (poll strategy only)
	PollCount    int    // number of successful poll cycles
	Strategy     string // "poll" or "oneshot"

	// Callbacks — set by caller, called by engine. Engine never switches on ResourceType.
	ExecFn          tea.Cmd                // execute the action (built at key-press time)
	PollFn          func() (string, error) // poll current state; return ("gone", nil) for not-found
	RefreshFn       func() tea.Cmd         // refresh list after completion
	IsTransitioning func(string) bool      // is this state still transitioning? (nil = no transitioning states)
}

type pollTickMsg time.Time

type pollResultMsg struct {
	states map[string]string
}

type transitionExpireMsg struct {
	key string
}

// logFileStat holds stat metadata for a log file.
type logFileStat struct {
	Size    string
	ModTime string
}

type fetchProcessesMsg struct {
	processes []config.Process
	err       error
}

type fetchLogFileStatMsg struct {
	stats map[int]logFileStat
	err   error
}

// sshStreamBatchMsg and sshStreamDoneMsg are defined in sshstream.go

// startCommandStreamMsg is sent when a user-defined command wizard completes.
type startCommandStreamMsg struct {
	cfg SSHStreamConfig
}

type startProcessActionViewMsg struct {
	action  string
	process string
}

type probeResultBatchMsg struct {
	results    []probes.ProbeResult
	generation int
}

type probesDoneMsg struct {
	generation int
}

type fetchAzureAKSMsg struct {
	clusters []azure.AKSDetail
	err      error
}

type k8sClusterProbeMsg struct {
	index        int
	contextCount int
	k8sVersion   string
	err          error
}

type fetchK8sContextsMsg struct {
	contexts []k8s.K8sContext
	err      error
}

type k8sResourceCountsMsg struct {
	counts k8s.K8sResourceCounts
	errs   []string
}

type fetchK8sNodesMsg struct {
	nodes []k8s.K8sNode
	err   error
}

type fetchK8sNodeDetailMsg struct {
	detail k8s.K8sNodeDetail
	err    error
}

type fetchK8sNodeUsageMsg struct {
	usage k8s.K8sNodeUsage
	err   error
}

type fetchK8sNodePodsMsg struct {
	pods []k8s.K8sNodePod
	err  error
}

type fetchK8sNamespacesMsg struct {
	namespaces []k8s.K8sNamespace
	err        error
}

type fetchK8sNamespaceCountsMsg struct {
	counts map[string][4]int
	err    error
}

type fetchK8sWorkloadsMsg struct {
	workloads []k8s.K8sWorkload
	err       error
}

type fetchK8sPodsMsg struct {
	pods []k8s.K8sPod
	err  error
}

type fetchK8sPodDetailMsg struct {
	detail k8s.K8sPodDetail
	err    error
}

type fetchK8sPodLogsMsg struct {
	entries []k8s.K8sLogEntry
	err     error
}

type k8sPodLogBatchMsg struct {
	entries []k8s.K8sLogEntry
}

type k8sPodLogDoneMsg struct{}

type fetchAzureActivityLogMsg struct {
	entries []azure.ActivityLogEntry
	err     error
}

// sudoTestMsg is sent after silently testing whether the SSH password works for sudo.
// Err is set only when the password could not be delivered safely
// (ssh.ErrSudoDelivery) — never for a wrong password, which still reports
// via Success=false. Carries no credential: on success, the accepted
// password is already resident in the manager under HostIdx and is
// broadcast to other hosts via ShareSudoPassword.
type sudoTestMsg struct {
	Success bool
	Retry   tea.Cmd
	HostIdx int
	Err     error
}

// sudoProbeMsg is sent after testing sudo availability on a host during probe.
type sudoProbeMsg struct {
	hostIdx int
	needsPw bool // true if sudo requires a password
}

func (m Model) tickCmd() tea.Cmd {
	interval, err := time.ParseDuration(m.fleets[m.selectedFleet].Defaults.RefreshInterval)
	if err != nil {
		return nil
	}
	return tea.Tick(interval, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

type view int

const (
	viewFleetPicker view = iota
	viewHostList
	viewMetrics
	viewResourcePicker
	viewServiceList
	viewContainerList
	viewCronList
	viewLogLevelPicker
	viewErrorLogList
	viewUpdateList
	viewDiskList
	viewSubscription
	viewAccountList
	viewNetworkPicker
	viewNetworkInterfaces
	viewNetworkPorts
	viewNetworkRoutes
	viewNetworkFirewall
	viewSecurityFailedLogins
	viewSecuritySudo
	viewSecuritySELinux
	viewSecurityAudit
	viewAzureSubList
	viewAzureResourcePicker
	viewAzureVMList
	viewAzureVMDetail
	viewAzureAKSList
	viewAzureAKSDetail
	viewK8sClusterList
	viewK8sContextList
	viewK8sResourcePicker
	viewK8sNodeList
	viewK8sNodeDetail
	viewK8sNamespaceList
	viewK8sWorkloadList
	viewK8sPodList
	viewK8sPodDetail
	viewK8sPodLogs
	viewProcessList
	viewLogFileList
	viewSSHStream
	viewProbeList
	viewProbeDetail
	viewConfig
	viewNoteList
	viewNoteRead
)

// resourceCount removed — replaced by dynamic visibleResourceRows()

// networkSubViewCount is the number of sub-views in the network picker.
const networkSubViewCount = 4

// Model is the Bubble Tea application model holding all TUI state.
type Model struct {
	view view

	// fleet picker
	fleets      []config.Fleet
	fleetCursor int

	// host list
	selectedFleet int
	hosts         []config.Host
	hostCursor    int

	// azure subscription list
	azureSubs      []azure.AzureSubscriptionItem
	azureSubCursor int

	// azure resource picker
	selectedAzureSub    int
	azureResourceCursor int
	azureResourceCounts azure.AzureResourceCounts
	azureResourceErr    error
	azureCountsLoaded   bool

	// azure VM list
	azureVMs            []azure.VM
	azureVMCursor       int
	azureVMDetail       azure.VMDetail
	showAzureVMDetail   bool
	azureActivityLog    []azure.ActivityLogEntry
	azureActivityCursor int
	azureVMDetailScroll int
	transitions         map[string]transition // overlay: "type/name" → in-flight action
	polling             bool                  // true if a poll chain is active

	// azure AKS list
	azureAKSClusters []azure.AKSDetail
	azureAKSCursor   int
	azureAKSDetail   azure.AKSDetail

	// kubernetes
	k8s                *k8s.Manager
	k8sClusters        []k8s.K8sClusterItem
	k8sClusterCursor   int
	selectedK8sCluster int
	k8sContexts        []k8s.K8sContext
	k8sContextCursor   int
	selectedK8sContext string
	k8sResourceCursor  int
	k8sResourceCounts  k8s.K8sResourceCounts
	k8sResourceErrors  []string
	k8sCountsLoaded    bool
	k8sNodes           []k8s.K8sNode
	k8sNodeCursor      int
	k8sNodeDetail      k8s.K8sNodeDetail
	k8sNodeUsage       *k8s.K8sNodeUsage // nil = loading
	k8sNodePods        []k8s.K8sNodePod  // nil = loading
	k8sNodePodCursor   int

	// k8s workloads
	k8sNamespaces         []k8s.K8sNamespace
	k8sNamespaceCursor    int
	selectedK8sNamespace  int
	k8sWorkloads          []k8s.K8sWorkload
	k8sWorkloadCursor     int
	selectedK8sWorkload   int
	k8sPodList            []k8s.K8sPod
	k8sPodCursor          int
	k8sPodDetail          k8s.K8sPodDetail
	k8sPodContainerCursor int

	// k8s pod logs
	k8sPodLogs               []k8s.K8sLogEntry
	k8sPodLogCursor          int
	k8sPodLogCancel          context.CancelFunc
	k8sPodLogChan            chan string
	k8sPodLogStreaming       bool
	k8sPodLogAllContainers   bool // true = show sidecar logs too
	k8sPodLogMinLevel        int  // 0=all, 1=Info+, 2=Warn+, 3=Error+
	showK8sLogDetail         bool
	k8sLogDetailScroll       int  // scroll offset for log detail view
	k8sLogDetailWasStreaming bool // resume streaming after closing detail
	k8sPodLogFromDetail      bool // true when logs opened from pod detail (Esc returns to detail)

	// supervisord processes
	processes     []config.Process
	processCursor int

	// log files
	logFileEntries  []config.LogEntry
	logFileCursor   int
	logFileStats    map[int]logFileStat // index → {size, modtime}
	logFileStatDone bool

	// generic SSH stream view (used by log tail, process actions, etc.)
	streamLines       []string
	streamCursor      int
	streamCancel      context.CancelFunc
	streamChan        chan string
	streamStreaming   bool
	streamTitle       string // display title in breadcrumb
	streamSourceName  string // clean name for file save (no special chars)
	streamReturnView  view
	streamNewestFirst bool             // true = newest on top (log tail), false = append (command output)
	streamAutoDone    bool             // true = show "Done" when command finishes
	streamGeneration  int              // incremented on each start, used to discard stale messages
	streamExitCode    *int             // shared with goroutine; written before channel close
	streamLastConfig  *SSHStreamConfig // saved for pause/resume

	// probes fleet
	probesManager     *probes.Manager
	probeItems        []ProbeItem
	probeCursor       int
	selectedProbe     int
	probeDetailScroll int
	probesCancel      context.CancelFunc
	probesChan        chan probes.ProbeResult
	probesStreaming   bool
	probesGeneration  int // incremented on each startProbing, used to discard stale messages

	// metrics dashboard
	metrics          map[int]config.HostMetrics
	metricsCursor    int
	metricsSortedIdx []int        // sorted host indices for metrics view
	metricErrors     map[int]bool // hosts where metrics fetch failed

	// resource picker
	selectedHost   int
	resourceCursor int

	// service list
	services          []config.Service
	serviceCursor     int
	showServiceDetail bool
	serviceDetailUnit string // unit name being viewed
	serviceStatus     config.ServiceStatus
	serviceLogLines   []string
	serviceLogCursor  int

	// container list
	containers            []config.Container
	containerCursor       int
	showContainerDetail   bool
	containerDetail       config.ContainerDetail
	containerDetailCursor int

	// cron jobs
	cronJobs   []config.CronJob
	cronCursor int

	// log level picker
	logLevels      []config.LogLevelEntry
	logLevelCursor int

	// error logs
	errorLogs        []config.ErrorLog
	errorCursor      int
	selectedLogLevel string

	// updates
	updates            []config.Update
	updateCursor       int
	showUpdateDetail   bool
	updateDetailLines  []string
	updateDetailCursor int

	// disk
	disks            []config.Disk
	diskCursor       int
	showDiskDetail   bool
	diskDetailLines  []string
	diskDetailCursor int

	// subscription
	subscriptions      []config.Subscription
	subscriptionCursor int

	// network
	networkCursor   int
	routeCount      int
	firewallType    string
	firewallCount   int
	interfaces      []config.NetInterface
	interfaceCursor int
	ports           []config.ListeningPort
	portCursor      int
	routes          []config.Route
	routeCursor     int
	dnsNameservers  []string
	dnsSearch       string
	firewallRules   []config.FirewallRule
	firewallCursor  int
	firewallBackend string // "firewalld", "nftables", "iptables", ""

	// security / audit
	failedLogins      []config.FailedLogin
	failedLoginCursor int
	sudoEntries       []config.SudoEntry
	sudoCursor        int
	selinuxDenials    []config.SELinuxDenial
	selinuxCursor     int
	auditEvents       []config.AuditEvent
	auditCursor       int

	// accounts
	accounts              []config.Account
	accountCursor         int
	showAccountDetail     bool
	accountDetailSections []accountDetailSection

	// filter / search
	filterActive bool
	filterText   string

	// column sort
	sortColumn int // 0 = default sort, 1+ = user-selected column
	sortAsc    bool

	// log detail
	showLogDetail bool

	// SSH
	ssh    *ssh.Manager
	azure  *azure.Manager
	logger *slog.Logger

	// flash message
	flash      string
	flashError bool

	// terminal size
	width  int
	height int

	// app info
	version string
	commit  string

	// config
	appCfg config.AppConfig

	// modal overlay
	modal           *ModalOverlay
	wizardCancelled bool
	wizardExitError error

	// notes (FLE-77/78/79)
	noteEngine      *notes.Engine
	noteRef         notes.ResourceRef // resource whose notes are being viewed
	noteList        []notes.Note      // loaded notes, newest first
	noteCursor      int
	noteReadLines   []string // split contents for scrollable reader
	noteReadOffset  int
	noteCounts      map[string]int // ref.Key() → count, for FLE-79 indicators
	previousView    view           // for Esc-back from Note List to the originating view
	noteLastCreated string         // path of just-created note; delete on exit if still empty
}

// NewModel builds the initial Model from loaded fleets and app
// configuration. sm is constructed by the caller (main, so it can call
// SetCredentialTimeout and defer Close before the model exists) rather
// than here.
func NewModel(fleets []config.Fleet, appCfg config.AppConfig, logger *slog.Logger, sm *ssh.Manager, version, commit string) Model {
	m := Model{
		view:          viewFleetPicker,
		fleets:        fleets,
		appCfg:        appCfg,
		ssh:           sm,
		azure:         azure.NewManager(logger),
		k8s:           k8s.NewManager(logger),
		probesManager: probes.NewManager(logger),
		logger:        logger,
		version:       version,
		commit:        commit,
		noteCounts:    make(map[string]int),
	}
	if appCfg.FleetDir != "" {
		m.noteEngine = notes.New(appCfg.FleetDir)
	}
	if appCfg.FleetDir == "" {
		m.modal = NewFirstRunWizard()
	}
	return m
}

// Init implements tea.Model.
func (m Model) Init() tea.Cmd {
	// On startup, the initial view is Fleet Picker (or first-run wizard). If
	// it supports notes, fire an initial note-count load so indicators appear
	// without waiting for the first tick.
	return m.refreshNoteCountsCmd()
}

// WizardCancelled returns true if the first-run wizard was cancelled.
func (m Model) WizardCancelled() bool {
	return m.wizardCancelled
}

// WizardError returns the error that caused the wizard to fail, or nil.
func (m Model) WizardError() error {
	return m.wizardExitError
}

// sudoDeliveryErr returns err when it wraps ssh.ErrSudoDelivery (the
// password could not be delivered safely), else nil — distinguishing that
// case from a plain wrong-password failure, which msg handlers must keep
// reporting as "Wrong sudo password".
func sudoDeliveryErr(err error) error {
	if errors.Is(err, ssh.ErrSudoDelivery) {
		return err
	}
	return nil
}

// handleSudoOrFlash checks if err is a sudo password error.
// If yes: stores the retry closure, then either silently tests the SSH cached password
// or shows the sudo prompt. Returns the updated model, an optional tea.Cmd, and true.
// If no: returns the original model, nil, and false — caller should set flash.
func (m Model) handleSudoOrFlash(err error, retry tea.Cmd) (Model, tea.Cmd, bool) {
	if !ssh.IsSudoError(err) {
		return m, nil, false
	}
	idx := m.selectedHost
	user := ""
	host := ""
	if idx < len(m.hosts) {
		user = m.hosts[idx].Entry.User
		host = m.hosts[idx].Entry.Name
	}
	m.logger.Debug("sudo required", "host_idx", idx, "host", host, "user", user)

	// Wrong cached password: it was tried and failed — clear and re-prompt.
	if m.ssh.GetSudoPassword(idx) {
		m.logger.Debug("sudo cached password failed, re-prompting", "host_idx", idx)
		m.ssh.ClearSudoPassword(idx)
		m.replaceModal(NewSudoModal(user, host, idx, retry))
		return m, nil, true
	}

	// Try the SSH connection password silently if available. The user never
	// entered it *as* a sudo password, so one sudo -S can't accept (e.g. a
	// multi-line paste) must not surface as a flash here — SeedSudoFromCachedPassword
	// refuses an undeliverable password instead of silently testing it.
	if m.ssh.SeedSudoFromCachedPassword(idx) {
		m.logger.Debug("sudo trying SSH password silently", "host_idx", idx)
		sm := m.ssh
		r := retry
		testCmd := func() tea.Msg {
			out, runErr := sm.RunSudoCommand(idx, "sudo true")
			success := runErr == nil && !ssh.IsSudoOutput(out)
			if !success {
				sm.ClearSudoPassword(idx)
			}
			return sudoTestMsg{Success: success, Retry: r, HostIdx: idx, Err: sudoDeliveryErr(runErr)}
		}
		return m, testCmd, true
	}

	// No SSH password cached (key auth): show prompt directly.
	m.logger.Debug("sudo no SSH password cached, showing prompt", "host_idx", idx)
	m.replaceModal(NewSudoModal(user, host, idx, retry))
	return m, nil, true
}

// testSudo fires a goroutine to test if sudo works on the given host.
func (m Model) testSudo(idx int) tea.Cmd {
	sm := m.ssh
	logger := m.logger
	return func() tea.Msg {
		out, err := sm.RunCommand(idx, "sudo -n true 2>&1")
		needsPw := err != nil || ssh.IsSudoOutput(out)
		logger.Debug("sudo test", "host_idx", idx, "needs_password", needsPw)
		return sudoProbeMsg{hostIdx: idx, needsPw: needsPw}
	}
}

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case wizardCompleteMsg:
		m.replaceModal(nil)
		m.appCfg = msg.appCfg
		m.fleets = msg.fleets
		m.view = viewFleetPicker
		m.flash = "Setup complete"
		return m, nil

	case wizardCancelMsg:
		m.replaceModal(nil)
		m.wizardCancelled = true
		m.ssh.EraseCredentials()
		return m, tea.Quit

	case wizardErrorMsg:
		m.replaceModal(nil)
		m.wizardCancelled = true
		m.wizardExitError = msg.err
		m.ssh.EraseCredentials()
		return m, tea.Quit

	case wizardNeedCustomEditorMsg:
		m.replaceModal(newCustomEditorWizard(msg.fleetDir))
		return m, nil

	case aboutFieldMsg:
		if m.modal != nil && !m.modal.Done() && m.modal.current < len(m.modal.steps) {
			if ac, ok := m.modal.steps[m.modal.current].Content.(*AboutContent); ok {
				ac.UpdateField(msg.field, msg.value)
			}
		}
		return m, nil

	case ssh.HostProbeResult:
		if msg.Index < len(m.hosts) {
			if msg.Err != nil {
				if ssh.IsAuthError(msg.Err) {
					// mark as needing password
					m.hosts[msg.Index].Status = config.HostUnreachable
					m.hosts[msg.Index].Error = "password required"
					m.hosts[msg.Index].NeedsPassword = true
					// show prompt — SSH password takes priority over other modals
					// (sudo, loading, etc.) but don't replace another password modal
					if m.modal == nil || m.modal.Done() || !isPasswordModal(m.modal) {
						h := m.hosts[msg.Index]
						m.replaceModal(NewPasswordModal(h.Entry.User, h.Entry.Name, msg.Index))
					}
					return m, nil
				}
				m.hosts[msg.Index].Status = config.HostUnreachable
				m.hosts[msg.Index].Error = msg.Err.Error()
			} else {
				m.applyProbeInfo(msg.Index, msg.Info)
			}
		}
		return m, nil

	case sudoProbeMsg:
		if !msg.needsPw || m.ssh.GetSudoPassword(msg.hostIdx) {
			// Sudo ready — navigate to resource picker
			m.logger.Debug("sudo ready, navigating", "host_idx", msg.hostIdx)
			if msg.hostIdx < len(m.hosts) {
				m.hosts[msg.hostIdx].SudoReady = true
			}
			m.selectedHost = msg.hostIdx
			m.resourceCursor = 0
			m.services = nil
			m.containers = nil
			m.updates = nil
			m.cronJobs = nil
			m.logLevels = nil
			m.errorLogs = nil
			m.disks = nil
			m.subscriptions = nil
			m.accounts = nil
			m.interfaces = nil
			m.ports = nil
			m.routes = nil
			m.firewallRules = nil
			m.failedLogins = nil
			m.sudoEntries = nil
			m.selinuxDenials = nil
			m.auditEvents = nil
			m.view = viewResourcePicker
			return m, tea.Batch(m.fetchServices(), m.fetchContainers(), m.fetchUpdates())
		}

		// Try SSH password silently first. The user never entered it *as* a
		// sudo password, so one sudo -S can't accept must not surface as a
		// flash here — SeedSudoFromCachedPassword refuses an undeliverable
		// password instead of silently testing it.
		idx := msg.hostIdx
		if m.ssh.SeedSudoFromCachedPassword(idx) {
			m.logger.Debug("sudo trying SSH password silently", "host_idx", idx)
			sm := m.ssh
			return m, func() tea.Msg {
				out, runErr := sm.RunSudoCommand(idx, "sudo true")
				if runErr == nil && !ssh.IsSudoOutput(out) {
					// Leave password cached — sudoWizardResultMsg handler will propagate
					return sudoWizardResultMsg{hostIdx: idx, success: true}
				}
				sm.ClearSudoPassword(idx) // wrong — clear
				return sudoWizardResultMsg{hostIdx: idx, success: false, err: sudoDeliveryErr(runErr)}
			}
		}

		// No SSH password — show sudo wizard
		user := ""
		host := ""
		if idx < len(m.hosts) {
			user = m.hosts[idx].Entry.User
			host = m.hosts[idx].Entry.Name
		}
		m.logger.Debug("sudo wizard opening", "host_idx", idx)
		m.replaceModal(NewSudoWizard(user, host, idx, m.ssh))
		return m, nil

	case sudoWizardTestMsg:
		// Step 2: show loading modal while testing sudo
		showLoading(&m, "sudotest", "Testing sudo...")
		idx := msg.hostIdx
		pw := msg.password
		sm := m.ssh
		m.logger.Debug("sudo wizard testing password", "host_idx", idx)
		return m, func() tea.Msg {
			sm.SetSudoPassword(idx, pw)
			out, err := sm.RunSudoCommand(idx, "sudo true")
			success := err == nil && !ssh.IsSudoOutput(out)
			if !success {
				sm.ClearSudoPassword(idx) // rejected candidate — erase before the next attempt
			}
			return sudoWizardResultMsg{hostIdx: idx, tested: true, success: success, err: sudoDeliveryErr(err)}
		}

	case sudoWizardResultMsg:
		if errors.Is(msg.err, ssh.ErrSudoDelivery) {
			m.flash = msg.err.Error()
			m.flashError = true
			m.replaceModal(nil)
			return m, nil
		}
		if msg.success {
			m.replaceModal(nil)
			m.logger.Debug("sudo wizard success", "host_idx", msg.hostIdx)
			// Share the accepted password (already stored under hostIdx) with
			// every other host — no result message carries the credential.
			for i := range m.hosts {
				if i != msg.hostIdx {
					m.ssh.ShareSudoPassword(msg.hostIdx, i)
				}
				if m.hosts[i].Status == config.HostOnline {
					m.hosts[i].SudoReady = true
				}
			}
			// Navigate
			return m, func() tea.Msg {
				return sudoProbeMsg{hostIdx: msg.hostIdx, needsPw: false}
			}
		}
		if msg.tested {
			// Password was tested and failed — re-show wizard
			m.logger.Debug("sudo wizard failed, re-prompting", "host_idx", msg.hostIdx)
			idx := msg.hostIdx
			user := ""
			host := ""
			if idx < len(m.hosts) {
				user = m.hosts[idx].Entry.User
				host = m.hosts[idx].Entry.Name
			}
			m.flash = "Wrong sudo password"
			m.flashError = true
			m.replaceModal(NewSudoWizard(user, host, idx, m.ssh))
			return m, nil
		}
		// Cancelled
		m.replaceModal(nil)
		return m, nil

	case azure.SubscriptionProbeResult:
		if msg.Index < len(m.azureSubs) {
			if msg.Err != nil {
				m.azureSubs[msg.Index].Status = azure.SubError
				m.azureSubs[msg.Index].Error = msg.Err.Error()
			} else {
				m.applyAzureProbeInfo(msg.Index, msg.Info)
			}
		}
		return m, nil

	case azureResourceCountsMsg:
		dismissLoadingFor(&m, "resourcecounts")
		m.azureResourceCounts = msg.counts
		m.azureResourceErr = msg.err
		m.azureCountsLoaded = true
		return m, nil

	case fetchAzureVMsMsg:
		dismissLoadingFor(&m, "vms")
		if msg.err != nil {
			m.flash = fmt.Sprintf("Failed: %v", msg.err)
			m.flashError = true
		} else {
			m.azureVMs = msg.vms
			m.flash = ""
		}
		return m, nil

	case fetchAzureVMDetailMsg:
		if msg.err != nil {
			m.flash = fmt.Sprintf("Failed: %v", msg.err)
			m.flashError = true
		} else {
			m.azureVMDetail = msg.detail
			// Copy network info from list VM (populated by graph, not az vm show).
			// Match by resource ID instead of cursor position to avoid race
			// when user scrolls while the detail fetch is in flight.
			for _, listVM := range m.azureVMs {
				if strings.EqualFold(listVM.ID, msg.detail.ID) {
					m.azureVMDetail.VNet = listVM.VNet
					m.azureVMDetail.Subnet = listVM.Subnet
					if m.azureVMDetail.PrivateIP == "" {
						m.azureVMDetail.PrivateIP = listVM.PrivateIP
					}
					if m.azureVMDetail.PowerState == "" {
						m.azureVMDetail.PowerState = listVM.PowerState
					}
					break
				}
			}
			m.showAzureVMDetail = true
			m.view = viewAzureVMDetail
			m.flash = ""
		}
		return m, nil

	case actionResultMsg:
		if m.transitions == nil {
			m.transitions = make(map[string]transition)
		}
		key := msg.resourceType + "/" + msg.name
		t, exists := m.transitions[key]

		if msg.err != nil {
			// Action failed — show error overlay, schedule removal
			m.transitions[key] = transition{
				ResourceType: msg.resourceType,
				ResourceName: msg.name,
				Action:       msg.action,
				Display:      msg.action + " failed",
				Strategy:     t.Strategy,
			}
			return m, expireTransition(key)
		}

		// For oneshot: action succeeded, remove transition immediately + refresh
		if exists && t.Strategy == "oneshot" {
			delete(m.transitions, key)
			if t.RefreshFn != nil {
				return m, t.RefreshFn()
			}
			return m, nil
		}

		// For poll: overlay already set, poll already started — nothing to do
		return m, nil

	case pollTickMsg:
		if len(m.transitions) == 0 {
			return m, nil // no transitions to track
		}
		return m, m.pollStates()

	case pollResultMsg:
		if m.transitions == nil {
			return m, nil
		}
		var cmds []tea.Cmd
		for tKey, t := range m.transitions {
			state, ok := msg.states[tKey]
			if !ok {
				continue // PollFn errored — skip, try next cycle
			}
			t.PollCount++
			if t.IsTransitioning != nil && t.IsTransitioning(state) {
				t.Confirmed = true
				t.Display = state
				m.transitions[tKey] = t
			} else if state == t.TargetState && (t.Confirmed || t.PollCount >= 3) {
				if t.RefreshFn != nil {
					cmds = append(cmds, t.RefreshFn())
				}
				delete(m.transitions, tKey)
			} else {
				m.transitions[tKey] = t // persist PollCount
			}
		}
		// Continue polling if transitions remain, otherwise stop
		if len(m.transitions) > 0 {
			cmds = append(cmds, m.startPoll())
		} else {
			m.polling = false
		}
		if len(cmds) > 0 {
			return m, tea.Batch(cmds...)
		}
		return m, nil

	case transitionExpireMsg:
		if m.transitions != nil {
			delete(m.transitions, msg.key)
			if len(m.transitions) == 0 {
				m.polling = false
			}
		}
		return m, nil

	case k8sClusterProbeMsg:
		if msg.index < len(m.k8sClusters) {
			if msg.err != nil {
				m.k8sClusters[msg.index].Status = k8s.ClusterError
				m.k8sClusters[msg.index].Error = msg.err.Error()
			} else {
				m.k8sClusters[msg.index].Status = k8s.ClusterOnline
				m.k8sClusters[msg.index].ContextCount = msg.contextCount
				m.k8sClusters[msg.index].K8sVersion = msg.k8sVersion
			}
		}
		return m, nil

	case fetchK8sContextsMsg:
		dismissLoadingFor(&m, "contexts")
		if msg.err != nil {
			m.flash = fmt.Sprintf("Failed: %v", msg.err)
			m.flashError = true
		} else {
			m.k8sContexts = msg.contexts
			m.flash = ""
			// Auto-select if only one context
			if len(msg.contexts) == 1 {
				m.selectedK8sContext = msg.contexts[0].Name
				m.k8sResourceCursor = 0
				m.k8sCountsLoaded = false
				m.view = viewK8sResourcePicker
				return m, m.fetchK8sResourceCounts()
			}
		}
		return m, nil

	case k8sResourceCountsMsg:
		dismissLoadingFor(&m, "resourcecounts")
		m.k8sResourceCounts = msg.counts
		m.k8sResourceErrors = msg.errs
		m.k8sCountsLoaded = true
		return m, nil

	case fetchK8sNodesMsg:
		dismissLoadingFor(&m, "nodes")
		if msg.err != nil {
			m.flash = fmt.Sprintf("Failed: %v", msg.err)
			m.flashError = true
		} else {
			m.k8sNodes = msg.nodes
			m.flash = ""
		}
		return m, nil

	case fetchK8sNodeDetailMsg:
		if msg.err != nil {
			m.flash = fmt.Sprintf("Failed: %v", msg.err)
			m.flashError = true
		} else {
			m.k8sNodeDetail = msg.detail
			m.view = viewK8sNodeDetail
			m.flash = ""
		}
		return m, nil

	case fetchK8sNodeUsageMsg:
		if msg.err == nil {
			m.k8sNodeUsage = &msg.usage
		}
		return m, nil

	case fetchK8sNodePodsMsg:
		if msg.err == nil {
			m.k8sNodePods = msg.pods
		}
		return m, nil

	case fetchK8sNamespacesMsg:
		dismissLoadingFor(&m, "namespaces")
		if msg.err != nil {
			m.flash = fmt.Sprintf("Failed: %v", msg.err)
			m.flashError = true
		} else {
			m.k8sNamespaces = msg.namespaces
			m.flash = ""
		}
		// Start background count fetch
		return m, m.fetchK8sNamespaceCounts()

	case fetchK8sNamespaceCountsMsg:
		if msg.err == nil && msg.counts != nil {
			for i := range m.k8sNamespaces {
				if c, ok := msg.counts[m.k8sNamespaces[i].Name]; ok {
					m.k8sNamespaces[i].PodCount = c[0]
					m.k8sNamespaces[i].DeployCount = c[1]
					m.k8sNamespaces[i].STSCount = c[2]
					m.k8sNamespaces[i].DSCount = c[3]
				}
			}
		}
		return m, nil

	case fetchK8sWorkloadsMsg:
		dismissLoadingFor(&m, "workloads")
		if msg.err != nil {
			m.flash = fmt.Sprintf("Failed: %v", msg.err)
			m.flashError = true
		} else {
			m.k8sWorkloads = msg.workloads
			m.flash = ""
		}
		return m, nil

	case fetchK8sPodsMsg:
		dismissLoadingFor(&m, "pods")
		if msg.err != nil {
			m.flash = fmt.Sprintf("Failed: %v", msg.err)
			m.flashError = true
		} else {
			m.k8sPodList = msg.pods
			// Sort pods by name for stable ordering
			sort.Slice(m.k8sPodList, func(i, j int) bool {
				return m.k8sPodList[i].Name < m.k8sPodList[j].Name
			})
			m.flash = ""
		}
		return m, nil

	case fetchK8sPodDetailMsg:
		if msg.err != nil {
			m.flash = fmt.Sprintf("Failed: %v", msg.err)
			m.flashError = true
		} else {
			m.k8sPodDetail = msg.detail
			m.view = viewK8sPodDetail
			m.flash = ""
		}
		return m, nil

	case fetchK8sPodLogsMsg:
		dismissLoadingFor(&m, "podlogs")
		if msg.err != nil {
			m.flash = fmt.Sprintf("Failed: %v", msg.err)
			m.flashError = true
		} else {
			m.k8sPodLogs = msg.entries
			m.k8sPodLogCursor = 0
			m.view = viewK8sPodLogs
			m.flash = ""
			// Start streaming
			pods := m.k8sPodList
			var podNames []string
			for _, p := range pods {
				podNames = append(podNames, p.Name)
			}
			ns := m.k8sNamespaces[m.selectedK8sNamespace].Name
			return m, m.streamK8sPodLogs(ns, podNames)
		}
		return m, nil

	case k8sPodLogBatchMsg:
		// Prepend new entries (newest first)
		wasAtTop := m.k8sPodLogCursor == 0
		m.k8sPodLogs = append(msg.entries, m.k8sPodLogs...)
		// Cap at 500 lines (trim oldest from end)
		if len(m.k8sPodLogs) > 500 {
			m.k8sPodLogs = m.k8sPodLogs[:500]
		}
		// Auto-scroll: keep cursor at top (newest) if user was there
		if wasAtTop {
			m.k8sPodLogCursor = 0
		} else {
			// Shift cursor down to keep the same entry selected
			m.k8sPodLogCursor += len(msg.entries)
		}
		// Re-subscribe
		if m.k8sPodLogChan != nil {
			return m, m.listenForLogLines()
		}
		return m, nil

	case k8sPodLogDoneMsg:
		m.k8sPodLogChan = nil
		m.k8sPodLogStreaming = false
		return m, nil

	case probeResultBatchMsg:
		if msg.generation != m.probesGeneration {
			return m, nil // stale message from previous session
		}
		for _, r := range msg.results {
			// Look up by ProbeIndex identity, not by slice position (sort reorders the slice).
			for i := range m.probeItems {
				if m.probeItems[i].Result.ProbeIndex == r.ProbeIndex {
					m.probeItems[i].Result = r
					break
				}
			}
		}
		if m.probesChan != nil {
			return m, m.listenForProbeResults()
		}
		return m, nil

	case probesDoneMsg:
		if msg.generation != m.probesGeneration {
			return m, nil // stale done from previous session
		}
		m.probesChan = nil
		m.probesStreaming = false
		return m, nil

	case fetchProcessesMsg:
		dismissLoadingFor(&m, "processes")
		if msg.err != nil {
			mm, cmd, handled := m.handleSudoOrFlash(msg.err, m.fetchProcesses())
			if handled {
				return mm, cmd
			}
			m.flash = fmt.Sprintf("Failed: %v", msg.err)
			m.flashError = true
			return m, nil
		}
		m.processes = msg.processes
		return m, nil

	case sshStreamBatchMsg:
		if msg.generation != m.streamGeneration {
			return m, nil // stale message from previous stream
		}
		if m.streamNewestFirst {
			wasAtTop := m.streamCursor == 0
			m.streamLines = append(msg.lines, m.streamLines...)
			if len(m.streamLines) > 1000 {
				m.streamLines = m.streamLines[:1000]
			}
			if wasAtTop {
				m.streamCursor = 0
			} else {
				m.streamCursor += len(msg.lines)
			}
		} else {
			m.streamLines = append(m.streamLines, msg.lines...)
			const streamCommandMaxLines = 5000
			if len(m.streamLines) > streamCommandMaxLines {
				m.streamLines = m.streamLines[len(m.streamLines)-streamCommandMaxLines:]
			}
		}
		if m.streamChan != nil {
			return m, m.listenForStreamLines()
		}
		return m, nil

	case sshStreamDoneMsg:
		if msg.generation != m.streamGeneration {
			return m, nil // stale done from previous stream
		}
		m.streamChan = nil
		m.streamStreaming = false
		if m.streamAutoDone {
			if msg.exitCode == 0 {
				m.streamLines = append(m.streamLines, "", ansiColor("✓ Done — press Esc to return", "32"))
			} else {
				m.streamLines = append(m.streamLines, "",
					ansiColor(fmt.Sprintf("✗ Exited with code %d — press Esc to return", msg.exitCode), "31"))
			}
		}
		return m, nil

	case fetchLogFileStatMsg:
		dismissLoadingFor(&m, "logfiles")
		if msg.err != nil {
			m.flash = fmt.Sprintf("Failed: %v", msg.err)
			m.flashError = true
			return m, nil
		}
		m.logFileStats = msg.stats
		m.logFileStatDone = true
		return m, nil

	case startCommandStreamMsg:
		m.replaceModal(nil)
		// If the command contains sudo and no password is cached, prompt first
		if strings.Contains(msg.cfg.Command, "sudo") && !m.ssh.GetSudoPassword(msg.cfg.HostIdx) {
			cfg := msg.cfg
			retry := func() tea.Msg { return startCommandStreamMsg{cfg: cfg} }
			user := ""
			host := ""
			if cfg.HostIdx < len(m.hosts) {
				user = m.hosts[cfg.HostIdx].Entry.User
				host = m.hosts[cfg.HostIdx].Entry.Name
			}
			m.replaceModal(NewSudoModal(user, host, cfg.HostIdx, retry))
			return m, nil
		}
		return m, m.startSSHStream(msg.cfg)

	case startProcessActionViewMsg:
		// Validate action against allowlist
		action := msg.action
		switch action {
		case "start", "stop", "restart":
			// valid
		default:
			m.flash = fmt.Sprintf("Invalid action: %s", action)
			m.flashError = true
			return m, nil
		}
		cmd := fmt.Sprintf("sudo supervisorctl %s '%s'; echo ''; echo '--- Current Status ---'; sudo supervisorctl status",
			action, shellQuote(msg.process))
		return m, m.startSSHStream(SSHStreamConfig{
			Command:     cmd,
			Title:       "Processes › " + msg.action + " " + msg.process,
			SourceName:  msg.action + "-" + msg.process,
			ReturnView:  viewProcessList,
			HostIdx:     m.selectedHost,
			NewestFirst: false,
			AutoDone:    true,
		})

	case fetchAzureAKSMsg:
		dismissLoadingFor(&m, "aks")
		if msg.err != nil {
			m.flash = fmt.Sprintf("Failed: %v", msg.err)
			m.flashError = true
		} else {
			m.azureAKSClusters = msg.clusters
			m.flash = ""
			// Auto-detect transitioning clusters and start polling
			for _, c := range msg.clusters {
				if strings.ToLower(c.ProvisioningState) != "succeeded" {
					key := "aks/" + c.Name
					if _, exists := m.transitions[key]; !exists {
						if m.transitions == nil {
							m.transitions = make(map[string]transition)
						}
						am := m.azure
						subID := m.azureSubs[m.selectedAzureSub].ID
						logger := m.logger
						clusterName := c.Name
						// Determine target based on the transitioning action
						target := "running"
						action := strings.ToLower(c.ProvisioningState)
						switch action {
						case "stopping":
							target = "stopped"
						case "deleting":
							target = "gone"
						}
						m.transitions[key] = transition{
							ResourceType: "aks",
							ResourceName: clusterName,
							Action:       action,
							Display:      c.ProvisioningState,
							TargetState:  target,
							Confirmed:    true,
							Strategy:     "poll",
							PollFn: func() (string, error) {
								states, err := azure.FetchAKSPowerStates(am, subID, []string{clusterName}, logger)
								if err != nil {
									return "", err
								}
								if s, ok := states[strings.ToLower(clusterName)]; ok {
									return s, nil
								}
								return "gone", nil
							},
							RefreshFn: func() tea.Cmd {
								return m.fetchAzureAKSClusters()
							},
							IsTransitioning: isAzureTransitioningState,
						}
					}
				}
			}
			if len(m.transitions) > 0 && !m.polling {
				m.polling = true
				return m, m.startPoll()
			}
		}
		return m, nil

	case fetchAzureActivityLogMsg:
		if msg.err != nil {
			m.flash = fmt.Sprintf("Activity log: %v", msg.err)
			m.flashError = true
		} else {
			m.azureActivityLog = msg.entries
			m.flash = ""
		}
		return m, nil

	case ssh.PasswordRetryResult:
		if msg.Index < len(m.hosts) {
			if msg.Err != nil {
				m.hosts[msg.Index].Status = config.HostUnreachable
				m.hosts[msg.Index].Error = msg.Err.Error()
			} else {
				m.hosts[msg.Index].NeedsPassword = false
				m.applyProbeInfo(msg.Index, msg.Info)
			}
		}
		return m, nil

	case sudoTestMsg:
		m.logger.Debug("sudo test result", "success", msg.Success, "host_idx", msg.HostIdx)
		if errors.Is(msg.Err, ssh.ErrSudoDelivery) {
			m.flash = msg.Err.Error()
			m.flashError = true
			m.replaceModal(nil)
			return m, nil
		}
		if msg.Success {
			// Share the accepted password (already stored under HostIdx) with
			// every other host — no result message carries the credential.
			for i := range m.hosts {
				if i != msg.HostIdx {
					m.ssh.ShareSudoPassword(msg.HostIdx, i)
				}
				if m.hosts[i].Status == config.HostOnline {
					m.hosts[i].SudoReady = true
				}
			}
			if msg.Retry != nil {
				return m, msg.Retry
			}
			// From probe flow — trigger navigation
			idx := msg.HostIdx
			return m, func() tea.Msg { return sudoProbeMsg{hostIdx: idx, needsPw: false} }
		}
		// SSH password didn't work as sudo password — show prompt.
		idx := msg.HostIdx
		user := ""
		host := ""
		if idx < len(m.hosts) {
			user = m.hosts[idx].Entry.User
			host = m.hosts[idx].Entry.Name
		}
		// Retry: re-test sudo after password entry, which will trigger navigation
		retryNavigate := func() tea.Msg { return sudoProbeMsg{hostIdx: idx, needsPw: false} }
		m.replaceModal(NewSudoModal(user, host, idx, retryNavigate))
		return m, nil

	case passwordEnteredMsg:
		m.replaceModal(nil)
		m.ssh.SetCachedPassword(msg.password)
		var cmds []tea.Cmd
		for i, h := range m.hosts {
			if h.NeedsPassword && (h.Status == config.HostUnreachable || i == msg.hostIdx) {
				m.hosts[i].Status = config.HostConnecting
				ii := i
				hh := h
				sm := m.ssh
				cmds = append(cmds, func() tea.Msg {
					return sm.ConnectWithCachedPassword(ii, hh)
				})
			}
		}
		return m, tea.Batch(cmds...)

	case passwordCancelledMsg:
		m.replaceModal(nil)
		if msg.hostIdx < len(m.hosts) {
			m.hosts[msg.hostIdx].Status = config.HostUnreachable
			m.hosts[msg.hostIdx].Error = "password prompt cancelled"
		}
		return m, nil

	case sudoEnteredMsg:
		m.replaceModal(nil)
		// Test the password before caching
		m.logger.Debug("sudo password entered, testing", "host_idx", msg.hostIdx)
		idx := msg.hostIdx
		pw := msg.password
		retry := msg.retry
		sm := m.ssh
		sm.SetSudoPassword(idx, pw)
		return m, func() tea.Msg {
			out, runErr := sm.RunSudoCommand(idx, "sudo true")
			success := runErr == nil && !ssh.IsSudoOutput(out)
			if !success {
				sm.ClearSudoPassword(idx) // rejected candidate — erase before the next attempt
			}
			return sudoTestMsg{Success: success, Retry: retry, HostIdx: idx, Err: sudoDeliveryErr(runErr)}
		}

	case sudoCancelledMsg:
		m.replaceModal(nil)
		m.flash = "Sudo password prompt cancelled"
		m.flashError = true
		return m, nil

	case transitionConfirmedMsg:
		m.replaceModal(nil)
		t := msg.t
		key := t.ResourceType + "/" + t.ResourceName
		if m.transitions == nil {
			m.transitions = make(map[string]transition)
		}
		m.transitions[key] = t
		m.flash = ""
		execCmd := t.ExecFn
		if t.Strategy == "poll" && !m.polling {
			m.polling = true
			return m, tea.Batch(execCmd, m.startPoll())
		}
		return m, execCmd

	case confirmCancelledMsg:
		m.replaceModal(nil)
		m.flash = "Cancelled"
		return m, nil

	case fetchMetricsMsg:
		if msg.err != nil {
			if m.metricErrors == nil {
				m.metricErrors = make(map[int]bool)
			}
			m.metricErrors[msg.index] = true
		} else {
			if m.metrics == nil {
				m.metrics = make(map[int]config.HostMetrics)
			}
			m.metrics[msg.index] = msg.metrics
		}
		m.flash = ""
		return m, nil

	case fetchServicesMsg:
		dismissLoadingFor(&m, "services")
		if msg.err != nil {
			m.flash = fmt.Sprintf("Failed: %v", msg.err)
			m.flashError = true
		} else {
			if msg.services == nil {
				m.services = []config.Service{}
			} else {
				m.services = msg.services
			}
		}
		return m, nil

	case fetchServiceDetailMsg:
		if msg.err != nil {
			if m2, cmd, ok := m.handleSudoOrFlash(msg.err, m.fetchServiceDetail(m.serviceDetailUnit)); ok {
				return m2, cmd
			}
			m.flash = fmt.Sprintf("Failed: %v", msg.err)
			m.flashError = true
		} else {
			m.serviceStatus = msg.status
			m.serviceLogLines = msg.logLines
			m.showServiceDetail = true
			m.flash = ""
		}
		return m, nil

	case fetchContainersMsg:
		dismissLoadingFor(&m, "containers")
		if msg.err != nil {
			m.flash = fmt.Sprintf("Failed: %v", msg.err)
			m.flashError = true
		} else {
			m.containers = msg.containers
		}
		return m, nil

	case fetchContainerDetailMsg:
		if msg.err != nil {
			m.flash = fmt.Sprintf("Failed: %v", msg.err)
			m.flashError = true
		} else {
			m.containerDetail = msg.detail
			m.showContainerDetail = true
			m.containerDetailCursor = 0
		}
		return m, nil

	case fetchCronMsg:
		dismissLoadingFor(&m, "cron")
		if msg.err != nil {
			m.flash = fmt.Sprintf("Failed: %v", msg.err)
			m.flashError = true
		} else {
			m.cronJobs = msg.jobs
		}
		return m, nil

	case fetchLogLevelsMsg:
		dismissLoadingFor(&m, "loglevels")
		if msg.err != nil {
			if m2, cmd, ok := m.handleSudoOrFlash(msg.err, m.fetchLogLevels()); ok {
				return m2, cmd
			}
			m.flash = fmt.Sprintf("Failed: %v", msg.err)
			m.flashError = true
		} else {
			m.logLevels = msg.levels
		}
		return m, nil

	case fetchErrorLogsMsg:
		dismissLoadingFor(&m, "errorlogs")
		if msg.err != nil {
			if m2, cmd, ok := m.handleSudoOrFlash(msg.err, m.fetchErrorLogs()); ok {
				return m2, cmd
			}
			m.flash = fmt.Sprintf("Failed: %v", msg.err)
			m.flashError = true
		} else {
			m.errorLogs = msg.logs
		}
		return m, nil

	case fetchUpdatesMsg:
		dismissLoadingFor(&m, "updates")
		if msg.err != nil {
			m.logger.Debug("fetchUpdates error", "err", msg.err, "view", m.view, "host_idx", m.selectedHost)
			if m2, cmd, ok := m.handleSudoOrFlash(msg.err, m.fetchUpdates()); ok {
				return m2, cmd
			}
			m.flash = fmt.Sprintf("Failed: %v", msg.err)
			m.flashError = true
		} else {
			if msg.updates == nil {
				m.updates = []config.Update{}
			} else {
				m.updates = msg.updates
			}
		}
		return m, nil

	case fetchUpdateDetailMsg:
		if msg.err != nil {
			m.flash = fmt.Sprintf("Failed: %v", msg.err)
			m.flashError = true
		} else {
			m.updateDetailLines = msg.lines
			m.showUpdateDetail = true
			m.updateDetailCursor = 0
		}
		return m, nil

	case fetchSubscriptionMsg:
		dismissLoadingFor(&m, "subscription")
		if msg.err != nil {
			if m2, cmd, ok := m.handleSudoOrFlash(msg.err, m.fetchSubscription()); ok {
				return m2, cmd
			}
			m.flash = fmt.Sprintf("Failed: %v", msg.err)
			m.flashError = true
		} else {
			m.subscriptions = msg.subs
		}
		return m, nil

	case fetchAccountDetailMsg:
		if msg.err != nil {
			user := ""
			if m.accountCursor < len(m.accounts) {
				user = m.accounts[m.accountCursor].User
			}
			if m2, cmd, ok := m.handleSudoOrFlash(msg.err, m.fetchAccountDetail(user)); ok {
				return m2, cmd
			}
			m.flash = fmt.Sprintf("Failed: %v", msg.err)
			m.flashError = true
		} else {
			m.accountDetailSections = msg.sections
			m.showAccountDetail = true
		}
		return m, nil

	case fetchAccountsMsg:
		dismissLoadingFor(&m, "accounts")
		if msg.err != nil {
			m.flash = fmt.Sprintf("Failed: %v", msg.err)
			m.flashError = true
		} else {
			if msg.accounts == nil {
				m.accounts = []config.Account{}
			} else {
				m.accounts = msg.accounts
			}
		}
		return m, nil

	case fetchPortsMsg:
		dismissLoadingFor(&m, "ports")
		if msg.err != nil {
			m.flash = fmt.Sprintf("Failed: %v", msg.err)
			m.flashError = true
		} else {
			if msg.ports == nil {
				m.ports = []config.ListeningPort{}
			} else {
				m.ports = msg.ports
			}
		}
		return m, nil

	case fetchRoutesMsg:
		dismissLoadingFor(&m, "routes")
		if msg.err != nil {
			m.flash = fmt.Sprintf("Failed: %v", msg.err)
			m.flashError = true
		} else {
			if msg.routes == nil {
				m.routes = []config.Route{}
			} else {
				m.routes = msg.routes
			}
			m.dnsNameservers = msg.nameservers
			m.dnsSearch = msg.search
		}
		return m, nil

	case fetchInterfacesMsg:
		dismissLoadingFor(&m, "interfaces")
		if msg.err != nil {
			m.flash = fmt.Sprintf("Failed: %v", msg.err)
			m.flashError = true
		} else {
			if msg.interfaces == nil {
				m.interfaces = []config.NetInterface{}
			} else {
				m.interfaces = msg.interfaces
			}
		}
		return m, nil

	case fetchNetworkInfoMsg:
		dismissLoadingFor(&m, "network")
		if msg.err != nil {
			if m2, cmd, ok := m.handleSudoOrFlash(msg.err, m.fetchNetworkInfo()); ok {
				return m2, cmd
			}
			m.flash = fmt.Sprintf("Failed: %v", msg.err)
			m.flashError = true
		} else {
			m.routeCount = msg.routeCount
			m.firewallType = msg.firewallType
			m.firewallCount = msg.firewallCount
		}
		return m, nil

	case fetchFirewallMsg:
		dismissLoadingFor(&m, "firewall")
		if msg.err != nil {
			if m2, cmd, ok := m.handleSudoOrFlash(msg.err, m.fetchFirewall()); ok {
				return m2, cmd
			}
			m.flash = fmt.Sprintf("Failed: %v", msg.err)
			m.flashError = true
		} else {
			m.firewallRules = msg.rules
			m.firewallBackend = msg.backend
		}
		return m, nil

	case fetchFailedLoginsMsg:
		dismissLoadingFor(&m, "failedlogins")
		if msg.err != nil {
			if m2, cmd, ok := m.handleSudoOrFlash(msg.err, m.fetchFailedLogins()); ok {
				return m2, cmd
			}
			m.flash = fmt.Sprintf("Failed: %v", msg.err)
			m.flashError = true
		} else {
			if msg.logins == nil {
				m.failedLogins = []config.FailedLogin{}
			} else {
				m.failedLogins = msg.logins
			}
		}
		return m, nil

	case fetchSudoActivityMsg:
		dismissLoadingFor(&m, "sudo")
		if msg.err != nil {
			if m2, cmd, ok := m.handleSudoOrFlash(msg.err, m.fetchSudoActivity()); ok {
				return m2, cmd
			}
			m.flash = fmt.Sprintf("Failed: %v", msg.err)
			m.flashError = true
		} else {
			if msg.entries == nil {
				m.sudoEntries = []config.SudoEntry{}
			} else {
				m.sudoEntries = msg.entries
			}
		}
		return m, nil

	case fetchSELinuxMsg:
		dismissLoadingFor(&m, "selinux")
		if msg.err != nil {
			if m2, cmd, ok := m.handleSudoOrFlash(msg.err, m.fetchSELinuxDenials()); ok {
				return m2, cmd
			}
			m.flash = fmt.Sprintf("Failed: %v", msg.err)
			m.flashError = true
		} else {
			if msg.denials == nil {
				m.selinuxDenials = []config.SELinuxDenial{}
			} else {
				m.selinuxDenials = msg.denials
			}
		}
		return m, nil

	case fetchAuditSummaryMsg:
		dismissLoadingFor(&m, "audit")
		if msg.err != nil {
			if m2, cmd, ok := m.handleSudoOrFlash(msg.err, m.fetchAuditSummary()); ok {
				return m2, cmd
			}
			m.flash = fmt.Sprintf("Failed: %v", msg.err)
			m.flashError = true
		} else {
			if msg.events == nil {
				m.auditEvents = []config.AuditEvent{}
			} else {
				m.auditEvents = msg.events
			}
		}
		return m, nil

	case fetchDiskMsg:
		dismissLoadingFor(&m, "disk")
		if msg.err != nil {
			m.flash = fmt.Sprintf("Failed: %v", msg.err)
			m.flashError = true
		} else {
			m.disks = msg.disks
		}
		return m, nil

	case fetchDiskDetailMsg:
		if msg.err != nil {
			mount := ""
			if m.diskCursor < len(m.disks) {
				mount = m.disks[m.diskCursor].Mount
			}
			if m2, cmd, ok := m.handleSudoOrFlash(msg.err, m.fetchDiskDetail(mount)); ok {
				return m2, cmd
			}
			m.flash = fmt.Sprintf("Failed: %v", msg.err)
			m.flashError = true
		} else {
			m.diskDetailLines = msg.lines
			m.showDiskDetail = true
			m.diskDetailCursor = 0
		}
		return m, nil

	case serviceActionMsg:
		if msg.err != nil {
			m.flash = fmt.Sprintf("%s %s failed: %v", msg.action, msg.unit, msg.err)
			m.flashError = true
		} else {
			m.flash = fmt.Sprintf("%s %s: ok", msg.action, msg.unit)
		}
		// refresh service list after action
		return m, m.fetchServices()

	case editFinishedMsg:
		// reload config + fleets after editor returns
		if msg.configEdit {
			newCfg, cfgErr := config.LoadAppConfig(config.ConfigPath())
			if cfgErr != nil {
				m.flash = fmt.Sprintf("Config reload failed: %v", cfgErr)
				m.flashError = true
				return m, tea.EnterAltScreen
			}
			m.appCfg = newCfg
		}
		fleets, err := config.ScanFleets(m.appCfg.FleetDir)
		if err != nil {
			m.flash = fmt.Sprintf("Reload failed: %v", err)
			m.flashError = true
		} else {
			m.fleets = fleets
			if m.fleetCursor >= len(m.fleets) {
				m.fleetCursor = max(0, len(m.fleets)-1)
			}
			m.flash = "Reloaded"
		}
		return m, tea.EnterAltScreen

	case noteListLoadedMsg:
		if msg.err != nil {
			m.flash = fmt.Sprintf("Notes load failed: %v", msg.err)
			m.flashError = true
			return m, nil
		}
		m.noteList = msg.notes
		if m.noteCursor >= len(m.noteList) {
			m.noteCursor = max(0, len(m.noteList)-1)
		}
		m.view = viewNoteList
		return m, nil

	case noteReadLoadedMsg:
		if msg.err != nil {
			m.flash = fmt.Sprintf("Read failed: %v", msg.err)
			m.flashError = true
			return m, nil
		}
		m.noteReadLines = strings.Split(msg.content, "\n")
		m.noteReadOffset = 0
		m.view = viewNoteRead
		return m, nil

	case noteCreatedMsg:
		if msg.err != nil {
			m.flash = fmt.Sprintf("Create failed: %v", msg.err)
			m.flashError = true
			return m, nil
		}
		m.noteLastCreated = msg.path
		return m, m.editNoteCmd(msg.path, true)

	case noteEditFinishedMsg:
		// If this was a post-create flow and the file is still empty, delete it.
		if msg.createdAt && fileIsEmpty(msg.path) {
			_ = m.noteEngine.Delete(msg.path)
		}
		m.noteLastCreated = ""
		// Invalidate count cache for this ref so indicators refresh on re-entry.
		delete(m.noteCounts, m.noteRef.Key())
		return m, tea.Batch(
			tea.EnterAltScreen,
			m.loadNoteListCmd(m.noteRef),
		)

	case noteDeleteConfirmedMsg:
		m.replaceModal(nil)
		return m, m.deleteNoteCmd(msg.path)

	case noteDeletedMsg:
		if msg.err != nil {
			m.flash = fmt.Sprintf("Delete failed: %v", msg.err)
			m.flashError = true
			return m, nil
		}
		m.flash = "Note deleted"
		delete(m.noteCounts, m.noteRef.Key())
		return m, m.loadNoteListCmd(m.noteRef)

	case noteCountsLoadedMsg:
		for k, v := range msg.counts {
			m.noteCounts[k] = v
		}
		return m, nil

	case sshHandoverFinishedMsg:
		m.replaceModal(nil)
		// refresh list after terminal handover returns
		switch m.view {
		case viewServiceList:
			if m.showServiceDetail && m.serviceDetailUnit != "" {
				return m, tea.Batch(tea.EnterAltScreen, m.fetchServiceDetail(m.serviceDetailUnit))
			}
			m.serviceCursor = 0
			m.services = nil
			return m, tea.Batch(tea.EnterAltScreen, m.fetchServices())
		case viewContainerList:
			return m, tea.Batch(tea.EnterAltScreen, m.fetchContainers())
		case viewUpdateList:
			m.updates = nil
			return m, tea.Batch(tea.EnterAltScreen, m.fetchUpdates())
		case viewSubscription:
			m.subscriptions = nil
			return m, tea.Batch(tea.EnterAltScreen, m.fetchSubscription())
		case viewAccountList:
			m.accounts = nil
			return m, tea.Batch(tea.EnterAltScreen, m.fetchAccounts())
		case viewNetworkPicker:
			return m, tea.Batch(tea.EnterAltScreen, m.fetchNetworkInfo())
		case viewProcessList:
			return m, tea.Batch(tea.EnterAltScreen, m.fetchProcesses())
		case viewLogFileList:
			return m, tea.Batch(tea.EnterAltScreen, m.fetchLogFileStats())
		case viewSSHStream:
			return m, tea.EnterAltScreen
		}
		return m, tea.EnterAltScreen

	case tickMsg:
		countsCmd := m.refreshNoteCountsCmd()
		if m.view == viewHostList {
			return m, tea.Batch(m.startProbe(), m.tickCmd(), countsCmd)
		}
		if m.view == viewAzureSubList {
			return m, tea.Batch(m.startAzureProbe(), m.tickCmd(), countsCmd)
		}
		if m.view == viewAzureAKSList {
			return m, tea.Batch(m.fetchAzureAKSClusters(), m.tickCmd(), countsCmd)
		}
		if m.view == viewK8sPodList {
			ns := m.k8sNamespaces[m.selectedK8sNamespace].Name
			w := m.k8sWorkloads[m.selectedK8sWorkload]
			return m, tea.Batch(m.fetchK8sPods(ns, w.Name), m.tickCmd(), countsCmd)
		}
		return m, countsCmd

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	return m, nil
}

// View implements tea.Model.
func (m Model) View() string {
	// If modal is active, render it on top of the current view
	if m.modal != nil && !m.modal.Done() {
		bgView := m.renderCurrentView()
		return m.modal.View(bgView, m.width, m.height)
	}
	return m.renderCurrentView()
}

func (m Model) renderCurrentView() string {
	switch m.view {
	case viewConfig:
		return m.renderConfig()
	case viewFleetPicker:
		return m.renderFleetPicker()
	case viewHostList:
		return m.renderHostList()
	case viewMetrics:
		return m.renderMetrics()
	case viewResourcePicker:
		return m.renderResourcePicker()
	case viewServiceList:
		return m.renderServiceList()
	case viewContainerList:
		return m.renderContainerList()
	case viewCronList:
		return m.renderCronList()
	case viewLogLevelPicker:
		return m.renderLogLevelPicker()
	case viewErrorLogList:
		return m.renderErrorLogList()
	case viewUpdateList:
		return m.renderUpdateList()
	case viewDiskList:
		return m.renderDiskList()
	case viewAccountList:
		return m.renderAccountList()
	case viewNetworkPicker:
		return m.renderNetworkPicker()
	case viewNetworkInterfaces:
		return m.renderNetworkInterfaces()
	case viewNetworkPorts:
		return m.renderNetworkPorts()
	case viewNetworkRoutes:
		return m.renderNetworkRoutes()
	case viewNetworkFirewall:
		return m.renderNetworkFirewall()
	case viewSubscription:
		return m.renderSubscription()
	case viewSecurityFailedLogins:
		return m.renderFailedLogins()
	case viewSecuritySudo:
		return m.renderSudoActivity()
	case viewSecuritySELinux:
		return m.renderSELinuxDenials()
	case viewSecurityAudit:
		return m.renderAuditSummary()
	case viewAzureSubList:
		return m.renderAzureSubList()
	case viewAzureResourcePicker:
		return m.renderAzureResourcePicker()
	case viewAzureVMList:
		return m.renderAzureVMList()
	case viewAzureVMDetail:
		return m.renderAzureVMDetail()
	case viewAzureAKSList:
		return m.renderAzureAKSList()
	case viewAzureAKSDetail:
		return m.renderAzureAKSDetail()
	case viewK8sClusterList:
		return m.renderK8sClusterList()
	case viewK8sContextList:
		return m.renderK8sContextList()
	case viewK8sResourcePicker:
		return m.renderK8sResourcePicker()
	case viewK8sNodeList:
		return m.renderK8sNodeList()
	case viewK8sNodeDetail:
		return m.renderK8sNodeDetail()
	case viewK8sNamespaceList:
		return m.renderK8sNamespaceList()
	case viewK8sWorkloadList:
		return m.renderK8sWorkloadList()
	case viewK8sPodList:
		return m.renderK8sPodList()
	case viewK8sPodDetail:
		return m.renderK8sPodDetail()
	case viewK8sPodLogs:
		return m.renderK8sPodLogs()
	case viewProcessList:
		return m.renderProcessList()
	case viewLogFileList:
		return m.renderLogFileList()
	case viewSSHStream:
		return m.renderSSHStream()
	case viewProbeList:
		return m.renderProbeList()
	case viewProbeDetail:
		return m.renderProbeDetail()
	case viewNoteList:
		return m.renderNoteList()
	case viewNoteRead:
		return m.renderNoteRead()
	}
	return ""
}
