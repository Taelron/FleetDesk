package app

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Gaetan-Jaminon/fleetdesk/internal/azure"
	"github.com/Gaetan-Jaminon/fleetdesk/internal/config"
	"github.com/Gaetan-Jaminon/fleetdesk/internal/k8s"
)

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// modal overlay -- intercept all keys
	if m.modal != nil && !m.modal.Done() {
		cmd := m.modal.HandleKey(msg)
		return m, cmd
	}

	// log detail mode -- any key goes back
	if m.showLogDetail {
		m.showLogDetail = false
		return m, nil
	}

	// filter mode -- capture search input
	if m.filterActive {
		switch msg.Type {
		case tea.KeyEnter:
			m.filterActive = false
			m.serviceCursor = 0
			m.errorCursor = 0
			m.accountCursor = 0
			m.portCursor = 0
			m.firewallCursor = 0
			m.serviceLogCursor = 0
			m.failedLoginCursor = 0
			m.sudoCursor = 0
			m.selinuxCursor = 0
			m.auditCursor = 0
			m.containerCursor = 0
			m.updateCursor = 0
			m.cronCursor = 0
			m.diskCursor = 0
			m.interfaceCursor = 0
			m.routeCursor = 0
			m.azureSubCursor = 0
			m.azureVMCursor = 0
			m.azureAKSCursor = 0
			m.k8sClusterCursor = 0
			m.k8sContextCursor = 0
			m.k8sNodeCursor = 0
			m.k8sNamespaceCursor = 0
			m.k8sWorkloadCursor = 0
			m.k8sPodCursor = 0
			m.k8sPodContainerCursor = 0
		case tea.KeyEsc:
			m.filterActive = false
			m.filterText = ""
			m.serviceCursor = 0
			m.errorCursor = 0
			m.accountCursor = 0
			m.portCursor = 0
			m.firewallCursor = 0
			m.serviceLogCursor = 0
			m.failedLoginCursor = 0
			m.sudoCursor = 0
			m.selinuxCursor = 0
			m.auditCursor = 0
			m.containerCursor = 0
			m.updateCursor = 0
			m.cronCursor = 0
			m.diskCursor = 0
			m.interfaceCursor = 0
			m.routeCursor = 0
			m.azureSubCursor = 0
			m.azureVMCursor = 0
			m.azureAKSCursor = 0
			m.k8sClusterCursor = 0
			m.k8sContextCursor = 0
			m.k8sNodeCursor = 0
			m.k8sNamespaceCursor = 0
			m.k8sWorkloadCursor = 0
			m.k8sPodCursor = 0
			m.k8sPodContainerCursor = 0
		case tea.KeyBackspace:
			if len(m.filterText) > 0 {
				m.filterText = m.filterText[:len(m.filterText)-1]
				m.serviceCursor = 0
				m.errorCursor = 0
				m.accountCursor = 0
				m.portCursor = 0
				m.firewallCursor = 0
				m.serviceLogCursor = 0
				m.failedLoginCursor = 0
				m.sudoCursor = 0
				m.selinuxCursor = 0
				m.auditCursor = 0
				m.containerCursor = 0
				m.updateCursor = 0
				m.cronCursor = 0
				m.diskCursor = 0
				m.interfaceCursor = 0
				m.routeCursor = 0
				m.azureSubCursor = 0
				m.azureVMCursor = 0
				m.azureAKSCursor = 0
				m.k8sClusterCursor = 0
				m.k8sContextCursor = 0
				m.k8sNodeCursor = 0
				m.k8sNamespaceCursor = 0
				m.k8sWorkloadCursor = 0
				m.k8sPodCursor = 0
				m.k8sPodContainerCursor = 0
				m.noteCursor = 0
			}
		default:
			if msg.Type == tea.KeyRunes {
				m.filterText += string(msg.Runes)
				m.serviceCursor = 0
				m.errorCursor = 0
				m.accountCursor = 0
				m.portCursor = 0
				m.serviceLogCursor = 0
				m.failedLoginCursor = 0
				m.sudoCursor = 0
				m.selinuxCursor = 0
				m.auditCursor = 0
				m.containerCursor = 0
				m.updateCursor = 0
				m.cronCursor = 0
				m.diskCursor = 0
				m.interfaceCursor = 0
				m.routeCursor = 0
				m.azureSubCursor = 0
				m.azureVMCursor = 0
				m.azureAKSCursor = 0
				m.k8sClusterCursor = 0
				m.k8sContextCursor = 0
				m.k8sNodeCursor = 0
				m.k8sNamespaceCursor = 0
				m.k8sWorkloadCursor = 0
				m.k8sPodCursor = 0
				m.k8sPodContainerCursor = 0
				m.noteCursor = 0
			}
		}
		return m, nil
	}

	m.flash = ""
	m.flashError = false

	// Global `n` key intercept (FLE-78): open Note List for the currently
	// selected resource. Only fires on views that support notes; individual
	// view handlers never see the `n` key when their view supports notes.
	if msg.String() == "n" && isNoteableView(m.view) && m.noteEngine != nil {
		ref, ok := m.currentNoteRef()
		if !ok {
			return m, nil
		}
		m.noteRef = ref
		m.previousView = m.view
		m.noteCursor = 0
		m.filterText = ""
		m.filterActive = false
		return m, m.loadNoteListCmd(ref)
	}

	switch msg.String() {
	case "q", "ctrl+c":
		m.azure.Close()
		return m, tea.Quit
	case "?":
		text := helpForView(m.view)
		m.modal = NewModalOverlay("Keybindings", []ModalStep{
			{Title: "", Content: NewStaticContent(text)},
		}, func(_ []any) tea.Cmd { return nil },
			func() tea.Cmd { return nil })
		m.modal.FooterFn = func() string {
			return modalKeyStyle.Render("?/Esc") + " " + modalDimStyle.Render("close") +
				"  " + modalKeyStyle.Render("↑↓") + " " + modalDimStyle.Render("scroll")
		}
		return m, nil
	}

	switch m.view {
	case viewFleetPicker:
		return m.handleFleetPickerKeys(msg)
	case viewHostList:
		return m.handleHostListKeys(msg)
	case viewMetrics:
		return m.handleMetricsKeys(msg)
	case viewResourcePicker:
		return m.handleResourcePickerKeys(msg)
	case viewServiceList:
		return m.handleServiceListKeys(msg)
	case viewContainerList:
		return m.handleContainerListKeys(msg)
	case viewCronList:
		return m.handleCronListKeys(msg)
	case viewLogLevelPicker:
		return m.handleLogLevelPickerKeys(msg)
	case viewErrorLogList:
		return m.handleErrorLogListKeys(msg)
	case viewUpdateList:
		return m.handleUpdateListKeys(msg)
	case viewDiskList:
		return m.handleDiskListKeys(msg)
	case viewAccountList:
		return m.handleAccountListKeys(msg)
	case viewNetworkPicker:
		return m.handleNetworkPickerKeys(msg)
	case viewNetworkInterfaces:
		return m.handleInterfaceListKeys(msg)
	case viewNetworkPorts:
		return m.handlePortListKeys(msg)
	case viewNetworkRoutes:
		return m.handleRouteListKeys(msg)
	case viewNetworkFirewall:
		return m.handleFirewallListKeys(msg)
	case viewSubscription:
		return m.handleSubscriptionKeys(msg)
	case viewSecurityFailedLogins:
		return m.handleFailedLoginKeys(msg)
	case viewSecuritySudo:
		return m.handleSudoKeys(msg)
	case viewSecuritySELinux:
		return m.handleSELinuxKeys(msg)
	case viewSecurityAudit:
		return m.handleAuditKeys(msg)
	case viewAzureSubList:
		return m.handleAzureSubListKeys(msg)
	case viewAzureResourcePicker:
		return m.handleAzureResourcePickerKeys(msg)
	case viewAzureVMList:
		return m.handleAzureVMListKeys(msg)
	case viewAzureVMDetail:
		return m.handleAzureVMDetailKeys(msg)
	case viewAzureAKSList:
		return m.handleAzureAKSListKeys(msg)
	case viewAzureAKSDetail:
		return m.handleAzureAKSDetailKeys(msg)
	case viewK8sClusterList:
		return m.handleK8sClusterListKeys(msg)
	case viewK8sContextList:
		return m.handleK8sContextListKeys(msg)
	case viewK8sResourcePicker:
		return m.handleK8sResourcePickerKeys(msg)
	case viewK8sNodeList:
		return m.handleK8sNodeListKeys(msg)
	case viewK8sNodeDetail:
		return m.handleK8sNodeDetailKeys(msg)
	case viewK8sNamespaceList:
		return m.handleK8sNamespaceListKeys(msg)
	case viewK8sWorkloadList:
		return m.handleK8sWorkloadListKeys(msg)
	case viewK8sPodList:
		return m.handleK8sPodListKeys(msg)
	case viewK8sPodDetail:
		return m.handleK8sPodDetailKeys(msg)
	case viewK8sPodLogs:
		return m.handleK8sPodLogKeys(msg)
	case viewProcessList:
		return m.handleProcessListKeys(msg)
	case viewLogFileList:
		return m.handleLogFileListKeys(msg)
	case viewSSHStream:
		return m.handleSSHStreamKeys(msg)
	case viewProbeList:
		return m.handleProbeListKeys(msg)
	case viewProbeDetail:
		return m.handleProbeDetailKeys(msg)
	case viewConfig:
		return m.handleConfigKeys(msg)
	case viewNoteList:
		return m.handleNoteListKeys(msg)
	case viewNoteRead:
		return m.handleNoteReadKeys(msg)
	}
	return m, nil
}

func (m Model) handleFleetPickerKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.fleetCursor > 0 {
			m.fleetCursor--
		}
	case "down", "j":
		if m.fleetCursor < len(m.fleets)-1 {
			m.fleetCursor++
		}
	case "c":
		m.view = viewConfig
		return m, nil
	case "a":
		modal, cmd := NewAboutModal(m.version, m.commit)
		m.modal = modal
		return m, cmd
	case "e":
		if len(m.fleets) > 0 {
			return m, m.editFleetFile()
		}
	case "r":
		fleets, err := config.ScanFleets(m.appCfg.FleetDir)
		if err != nil {
			m.flash = fmt.Sprintf("Reload failed: %v", err)
			m.flashError = true
			return m, nil
		}
		m.fleets = fleets
		if m.fleetCursor >= len(m.fleets) {
			m.fleetCursor = max(0, len(m.fleets)-1)
		}
		m.flash = "Reloaded"
	case "enter":
		if len(m.fleets) > 0 {
			f := m.fleets[m.fleetCursor]
			switch f.Type {
			case "vm":
				m.selectedFleet = m.fleetCursor
				m.ssh.Close()
				m.hosts = buildHostList(f)
				m.hostCursor = 0
				m.view = viewHostList
				return m, tea.Batch(m.startProbe(), m.tickCmd())
			case "azure":
				if err := m.azure.CheckPrerequisites(); err != nil {
					if azure.IsNotInstalled(err) {
						m.flash = "az CLI not found — install Azure CLI to use Azure fleets"
					} else if azure.IsNotLoggedIn(err) {
						m.flash = "Not logged in to Azure — run 'az login' first"
					} else {
						m.flash = fmt.Sprintf("Azure error: %v", err)
					}
					m.flashError = true
					return m, nil
				}
				m.selectedFleet = m.fleetCursor
				m.azureSubs = buildAzureSubList(f)
				m.azureSubCursor = 0
				m.sortColumn = 0
				m.filterText = ""
				m.filterActive = false
				m.view = viewAzureSubList
				return m, tea.Batch(m.startAzureProbe(), m.tickCmd())
			case "kubernetes":
				if err := m.k8s.CheckPrerequisites(); err != nil {
					m.flash = "kubectl not found — install kubectl to use Kubernetes fleets"
					m.flashError = true
					return m, nil
				}
				m.selectedFleet = m.fleetCursor
				m.k8sClusters = buildK8sClusterList(f)
				m.k8sClusterCursor = 0
				m.view = viewK8sClusterList
				return m, m.startK8sProbe()
			case "probes":
				m.selectedFleet = m.fleetCursor
				m.probeItems = buildProbeList(f.ProbeFleet)
				m.probeCursor = 0
				m.sortColumn = 0
				m.filterText = ""
				m.filterActive = false
				m.view = viewProbeList
				return m, m.startProbing()
			default:
				m.flash = "Unsupported fleet type"
				m.flashError = false
				return m, nil
			}
		}
	}
	return m, nil
}

func (m Model) handleHostListKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.hostCursor > 0 {
			m.hostCursor--
		}
	case "down", "j":
		if m.hostCursor < len(m.hosts)-1 {
			m.hostCursor++
		}
	case "x":
		if len(m.hosts) > 0 {
			h := m.hosts[m.hostCursor]
			if h.Status != config.HostOnline {
				m.flash = "Host is not reachable"
				m.flashError = true
				return m, nil
			}
			return m, sshHandover(h, []string{}, fmt.Sprintf("shell %s@%s", h.Entry.User, h.Entry.Name))
		}
	case "K":
		if len(m.hosts) > 0 {
			h := m.hosts[m.hostCursor]
			if h.Status != config.HostOnline {
				m.flash = "Host is not reachable"
				m.flashError = true
				return m, nil
			}
			m.selectedHost = m.hostCursor
			sshTarget := shellQuote(h.Entry.User) + "@" + shellQuote(h.Entry.Hostname)
			portStr := fmt.Sprintf("%d", h.Entry.Port)
			script := fmt.Sprintf(
				"ssh -t -o StrictHostKeyChecking=no -p %s '%s' 'sudo mkdir -p $HOME/.ssh && sudo chown -R $(id -u):$(id -g) $HOME && chmod 700 $HOME/.ssh' && ssh-copy-id -o StrictHostKeyChecking=no -p %s '%s'",
				portStr, sshTarget, portStr, sshTarget)
			handover := cmdHandover("bash",
				[]string{"-c", script},
				fmt.Sprintf("deploy key to %s@%s", h.Entry.User, h.Entry.Name))
			m.modal = NewConfirmModal("Confirm",
				fmt.Sprintf("Deploy SSH key to %s@%s? [Y/n]", h.Entry.User, h.Entry.Name),
				handover)
		}
	case "R":
		if len(m.hosts) > 0 {
			h := m.hosts[m.hostCursor]
			if h.Status != config.HostOnline {
				m.flash = "Host is not reachable"
				m.flashError = true
				return m, nil
			}
			m.selectedHost = m.hostCursor
			hh := h
			m.modal = NewConfirmModal("Confirm",
				fmt.Sprintf("REBOOT %s? [Y/n]", h.Entry.Name),
				sshHandover(hh, []string{`sudo reboot; echo 'Reboot initiated'`}, fmt.Sprintf("reboot %s", h.Entry.Name)))
		}
	case "c":
		f := m.fleets[m.selectedFleet]
		if f.Type != "vm" {
			return m, nil
		}
		if len(m.hosts) == 0 {
			return m, nil
		}
		h := m.hosts[m.hostCursor]
		if h.Status != config.HostOnline {
			m.flash = "Host is not reachable"
			m.flashError = true
			return m, nil
		}
		if len(h.Entry.Commands) == 0 {
			m.flash = "No commands defined for this host"
			m.flashError = true
			return m, nil
		}
		m.selectedHost = m.hostCursor
		openCommandWizard(&m)
	case "d":
		m.metrics = make(map[int]config.HostMetrics)
		m.metricErrors = make(map[int]bool)
		m.metricsCursor = 0
		m.sortColumn = 0
		m.metricsSortedIdx = nil
		m.view = viewMetrics
		m.flash = "Fetching metrics..."
		return m, m.fetchAllMetrics()
	case "r":
		f := m.fleets[m.selectedFleet]
		m.ssh.Close()
		m.hosts = buildHostList(f)
		m.flash = "Refreshing..."
		return m, m.startProbe()
	case "enter":
		if len(m.hosts) > 0 {
			h := m.hosts[m.hostCursor]
			if h.Status != config.HostOnline {
				m.flash = "Host is not reachable"
				m.flashError = true
				return m, nil
			}
			if !h.SudoReady {
				// Test sudo on this host — prompt if needed, then navigate
				m.selectedHost = m.hostCursor
				return m, m.testSudo(m.hostCursor)
			}
			m.selectedHost = m.hostCursor
			m.resourceCursor = 0
			// Clear stale data from previous host
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
			// pre-fetch for accurate counts
			return m, tea.Batch(m.fetchServices(), m.fetchContainers(), m.fetchUpdates())
		}
	case "esc":
		m.ssh.Close()
		m.view = viewFleetPicker
	}
	return m, nil
}

func (m Model) handleResourcePickerKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	rows := m.visibleResourceRows()
	maxIdx := len(rows) - 1
	if m.resourceCursor > maxIdx {
		m.resourceCursor = maxIdx
	}

	switch msg.String() {
	case "up", "k":
		if m.resourceCursor > 0 {
			m.resourceCursor--
		}
	case "down", "j":
		if m.resourceCursor < maxIdx {
			m.resourceCursor++
		}
	case "enter":
		targetView, fetchFn, loadKey := m.resourcePickerEnter()
		if fetchFn == nil {
			return m, nil
		}
		m.sortColumn = 0
		m.filterText = ""
		m.filterActive = false
		// Reset view-specific cursors
		switch targetView {
		case viewProcessList:
			m.processCursor = 0
		case viewLogFileList:
			m.logFileCursor = 0
			m.logFileEntries = m.hosts[m.selectedHost].Entry.Logs
			m.logFileStats = nil
			m.logFileStatDone = false
		}
		m.view = targetView
		showLoading(&m, loadKey, fmt.Sprintf("Loading %s...", loadKey))
		return m, fetchFn()
	case "r":
		m.services = nil
		m.containers = nil
		m.updates = nil
		showLoading(&m, "resourcecounts", "Loading resource counts...")
		return m, tea.Batch(m.fetchServices(), m.fetchContainers(), m.fetchUpdates())
	case "esc":
		m.view = viewHostList
	}
	return m, nil
}

func (m Model) handleServiceListKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	filtered := m.filteredServices()

	// detail mode keys
	if m.showServiceDetail {
		filteredLogs := m.filteredServiceLogs()
		switch msg.String() {
		case "up", "k":
			if m.serviceLogCursor > 0 {
				m.serviceLogCursor--
			}
		case "down", "j":
			if m.serviceLogCursor < len(filteredLogs)-1 {
				m.serviceLogCursor++
			}
		case "/":
			m.filterActive = true
			m.filterText = ""
			m.serviceLogCursor = 0
		case "s":
			if m.serviceDetailUnit != "" {
				return m.confirmDetailSvcAction("start")
			}
		case "o":
			if m.serviceDetailUnit != "" {
				return m.confirmDetailSvcAction("stop")
			}
		case "t":
			if m.serviceDetailUnit != "" {
				return m.confirmDetailSvcAction("restart")
			}
		case "r":
			if m.serviceDetailUnit != "" {
				m.flash = fmt.Sprintf("Refreshing %s...", m.serviceDetailUnit)
				m.serviceLogCursor = 0
				return m, m.fetchServiceDetail(m.serviceDetailUnit)
			}
		case "esc":
			if m.filterText != "" {
				m.filterActive = false
				m.filterText = ""
				m.serviceLogCursor = 0
			} else {
				m.filterActive = false
				m.showServiceDetail = false
				m.serviceLogCursor = 0
			}
		}
		return m, nil
	}

	// list mode keys
	switch msg.String() {
	case "up", "k":
		if m.serviceCursor > 0 {
			m.serviceCursor--
		}
	case "down", "j":
		if m.serviceCursor < len(filtered)-1 {
			m.serviceCursor++
		}
	case "/":
		m.filterActive = true
		m.filterText = ""
		m.serviceCursor = 0
	case "enter":
		if len(filtered) > 0 {
			unit := filtered[m.serviceCursor].Name + ".service"
			m.serviceDetailUnit = unit
			m.flash = fmt.Sprintf("Loading %s...", unit)
			return m, m.fetchServiceDetail(unit)
		}
	case "1", "2", "3", "4":
		col := int(msg.Runes[0] - '0')
		if col >= 1 && col <= 4 {
			if col == m.sortColumn {
				m.sortAsc = !m.sortAsc
			} else {
				m.sortColumn = col
				m.sortAsc = true
			}
			m.sortView()
		}
	case "r":
		m.services = nil
		m.sortColumn = 0
		m.filterText = ""
		showLoading(&m, "services", "Loading services...")
		return m, m.fetchServices()
	case "esc":
		if m.filterText != "" {
			m.filterText = ""
			m.serviceCursor = 0
		} else {
			m.view = viewResourcePicker
		}
	}
	return m, nil
}

func (m Model) handleContainerListKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// detail mode
	if m.showContainerDetail {
		switch msg.String() {
		case "up", "k":
			if m.containerDetailCursor > 0 {
				m.containerDetailCursor--
			}
		case "down", "j":
			lines := m.containerDetailLines()
			if m.containerDetailCursor < len(lines)-1 {
				m.containerDetailCursor++
			}
		case "esc":
			m.showContainerDetail = false
		}
		return m, nil
	}

	switch msg.String() {
	case "up", "k":
		if m.containerCursor > 0 {
			m.containerCursor--
		}
	case "down", "j":
		filtered := m.filteredContainers()
		if m.containerCursor < len(filtered)-1 {
			m.containerCursor++
		}
	case "enter":
		filtered := m.filteredContainers()
		if len(filtered) > 0 && m.containerCursor < len(filtered) {
			ctr := filtered[m.containerCursor].Name
			m.flash = fmt.Sprintf("Loading %s...", ctr)
			return m, m.fetchContainerDetail(ctr)
		}
	case "/":
		m.filterActive = true
		m.filterText = ""
		m.containerCursor = 0
	case "l":
		filtered := m.filteredContainers()
		if len(filtered) > 0 && m.containerCursor < len(filtered) {
			h := m.hosts[m.selectedHost]
			ctr := filtered[m.containerCursor].Name
			cmd := fmt.Sprintf("podman logs -f '%s'", shellQuote(ctr))
			return m, sshHandover(h, []string{cmd}, fmt.Sprintf("logs %s on %s (Ctrl+C to stop)", ctr, h.Entry.Name))
		}
	case "i":
		filtered := m.filteredContainers()
		if len(filtered) > 0 && m.containerCursor < len(filtered) {
			h := m.hosts[m.selectedHost]
			ctr := filtered[m.containerCursor].Name
			cmd := fmt.Sprintf("podman inspect '%s' | less", shellQuote(ctr))
			return m, sshHandover(h, []string{cmd}, fmt.Sprintf("inspect %s on %s", ctr, h.Entry.Name))
		}
	case "e":
		filtered := m.filteredContainers()
		if len(filtered) > 0 && m.containerCursor < len(filtered) {
			h := m.hosts[m.selectedHost]
			ctr := filtered[m.containerCursor].Name
			cmd := fmt.Sprintf("podman exec -it '%s' /bin/bash || podman exec -it '%s' /bin/sh", shellQuote(ctr), shellQuote(ctr))
			return m, sshHandover(h, []string{cmd}, fmt.Sprintf("exec %s on %s", ctr, h.Entry.Name))
		}
	case "1", "2", "3":
		col := int(msg.Runes[0] - '0')
		if col >= 1 && col <= 3 {
			if col == m.sortColumn {
				m.sortAsc = !m.sortAsc
			} else {
				m.sortColumn = col
				m.sortAsc = true
			}
			m.sortView()
		}
	case "r":
		m.containers = nil
		m.sortColumn = 0
		showLoading(&m, "containers", "Loading containers...")
		return m, m.fetchContainers()
	case "esc":
		if m.filterText != "" {
			m.filterText = ""
			m.containerCursor = 0
		} else {
			m.view = viewResourcePicker
		}
	}
	return m, nil
}

func (m Model) handleCronListKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.cronCursor > 0 {
			m.cronCursor--
		}
	case "down", "j":
		filtered := m.filteredCronJobs()
		if m.cronCursor < len(filtered)-1 {
			m.cronCursor++
		}
	case "/":
		m.filterActive = true
		m.filterText = ""
		m.cronCursor = 0
	case "1", "2", "3":
		col := int(msg.Runes[0] - '0')
		if col >= 1 && col <= 3 {
			if col == m.sortColumn {
				m.sortAsc = !m.sortAsc
			} else {
				m.sortColumn = col
				m.sortAsc = true
			}
			m.sortView()
		}
	case "r":
		m.cronJobs = nil
		m.sortColumn = 0
		showLoading(&m, "cron", "Loading cron jobs...")
		return m, m.fetchCronJobs()
	case "esc":
		if m.filterText != "" {
			m.filterText = ""
			m.cronCursor = 0
		} else {
			m.view = viewResourcePicker
		}
	}
	return m, nil
}

func (m Model) handleLogLevelPickerKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.logLevelCursor > 0 {
			m.logLevelCursor--
		}
	case "down", "j":
		if m.logLevelCursor < len(m.logLevels)-1 {
			m.logLevelCursor++
		}
	case "enter":
		if len(m.logLevels) > 0 {
			selected := m.logLevels[m.logLevelCursor]
			m.selectedLogLevel = selected.Code
			m.errorCursor = 0
			m.view = viewErrorLogList
			return m, m.fetchErrorLogs()
		}
	case "r":
		m.logLevels = nil
		showLoading(&m, "loglevels", "Loading log levels...")
		return m, m.fetchLogLevels()
	case "esc":
		m.view = viewResourcePicker
	}
	return m, nil
}

func (m Model) handleErrorLogListKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	filtered := m.filteredErrorLogs()

	switch msg.String() {
	case "up", "k":
		if m.errorCursor > 0 {
			m.errorCursor--
		}
	case "down", "j":
		if m.errorCursor < len(filtered)-1 {
			m.errorCursor++
		}
	case "/":
		m.filterActive = true
		m.filterText = ""
		m.errorCursor = 0
	case "enter":
		if len(filtered) > 0 && m.errorCursor < len(filtered) {
			m.showLogDetail = true
		}
	case "l":
		if len(filtered) > 0 {
			h := m.hosts[m.selectedHost]
			since := h.ErrorLogSince
			cmd := fmt.Sprintf("sudo journalctl -p %s --since '%s' --no-pager -q --no-hostname | less", m.selectedLogLevel, since)
			return m, sshHandover(h, []string{cmd}, fmt.Sprintf("%s logs on %s", m.logLevels[m.logLevelCursor].Level, h.Entry.Name))
		}
	case "1", "2", "3":
		col := int(msg.Runes[0] - '0')
		if col >= 1 && col <= 3 {
			if col == m.sortColumn {
				m.sortAsc = !m.sortAsc
			} else {
				m.sortColumn = col
				m.sortAsc = true
			}
			m.sortView()
		}
	case "r":
		m.errorLogs = nil
		m.sortColumn = 0
		m.filterText = ""
		showLoading(&m, "errorlogs", "Loading error logs...")
		return m, m.fetchErrorLogs()
	case "esc":
		if m.filterText != "" {
			m.filterText = ""
			m.errorCursor = 0
		} else {
			m.view = viewLogLevelPicker
		}
	}
	return m, nil
}

func (m Model) handleUpdateListKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// detail mode
	if m.showUpdateDetail {
		switch msg.String() {
		case "up", "k":
			if m.updateDetailCursor > 0 {
				m.updateDetailCursor--
			}
		case "down", "j":
			if m.updateDetailCursor < len(m.updateDetailLines)-1 {
				m.updateDetailCursor++
			}
		case "esc":
			m.showUpdateDetail = false
			m.updateDetailLines = nil
		}
		return m, nil
	}

	switch msg.String() {
	case "up", "k":
		if m.updateCursor > 0 {
			m.updateCursor--
		}
	case "down", "j":
		filtered := m.filteredUpdates()
		if m.updateCursor < len(filtered)-1 {
			m.updateCursor++
		}
	case "enter":
		filtered := m.filteredUpdates()
		if len(filtered) > 0 && m.updateCursor < len(filtered) {
			pkg := filtered[m.updateCursor].Package
			m.flash = fmt.Sprintf("Loading %s...", pkg)
			return m, m.fetchUpdateDetail(pkg)
		}
	case "/":
		m.filterActive = true
		m.filterText = ""
		m.updateCursor = 0
	case "u":
		h := m.hosts[m.selectedHost]
		dnfOpts := []MultiSelectOption{
			{Key: "--allowerasing", Label: "--allowerasing", Description: "replace conflicts"},
			{Key: "--skip-broken", Label: "--skip-broken", Description: "skip failures"},
			{Key: "--nobest", Label: "--nobest", Description: "allow older versions"},
		}
		banner := fmt.Sprintf("full update on %s", h.Entry.Name)
		hh := h
		m.modal = NewOptionsConfirmModal(
			"Update Options",
			fmt.Sprintf("Select flags for dnf update on %s:", h.Entry.Name),
			dnfOpts,
			func(keys []string) string { return buildDnfCommand("sudo dnf update", keys) },
			func(cmd string) tea.Cmd { return sshHandover(hh, []string{cmd}, banner) },
			func() tea.Cmd { return func() tea.Msg { return confirmCancelledMsg{} } },
		)
	case "p":
		h := m.hosts[m.selectedHost]
		dnfOpts := []MultiSelectOption{
			{Key: "--allowerasing", Label: "--allowerasing", Description: "replace conflicts"},
			{Key: "--skip-broken", Label: "--skip-broken", Description: "skip failures"},
			{Key: "--nobest", Label: "--nobest", Description: "allow older versions"},
		}
		banner := fmt.Sprintf("security update on %s", h.Entry.Name)
		hh := h
		m.modal = NewOptionsConfirmModal(
			"Security Update Options",
			fmt.Sprintf("Select flags for dnf update --security on %s:", h.Entry.Name),
			dnfOpts,
			func(keys []string) string { return buildDnfCommand("sudo dnf update --security", keys) },
			func(cmd string) tea.Cmd { return sshHandover(hh, []string{cmd}, banner) },
			func() tea.Cmd { return func() tea.Msg { return confirmCancelledMsg{} } },
		)
	case "1", "2", "3":
		col := int(msg.Runes[0] - '0')
		if col >= 1 && col <= 3 {
			if col == m.sortColumn {
				m.sortAsc = !m.sortAsc
			} else {
				m.sortColumn = col
				m.sortAsc = true
			}
			m.sortView()
		}
	case "r":
		m.updates = nil
		m.sortColumn = 0
		showLoading(&m, "updates", "Loading updates...")
		return m, m.fetchUpdates()
	case "esc":
		if m.filterText != "" {
			m.filterText = ""
			m.updateCursor = 0
		} else {
			m.view = viewResourcePicker
		}
	}
	return m, nil
}

func (m Model) handleDiskListKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// detail mode
	if m.showDiskDetail {
		switch msg.String() {
		case "up", "k":
			if m.diskDetailCursor > 0 {
				m.diskDetailCursor--
			}
		case "down", "j":
			if m.diskDetailCursor < len(m.diskDetailLines)-1 {
				m.diskDetailCursor++
			}
		case "esc":
			m.showDiskDetail = false
			m.diskDetailLines = nil
		}
		return m, nil
	}

	switch msg.String() {
	case "up", "k":
		if m.diskCursor > 0 {
			m.diskCursor--
		}
	case "down", "j":
		filtered := m.filteredDisks()
		if m.diskCursor < len(filtered)-1 {
			m.diskCursor++
		}
	case "enter":
		filtered := m.filteredDisks()
		if len(filtered) > 0 && m.diskCursor < len(filtered) {
			mount := filtered[m.diskCursor].Mount
			m.flash = fmt.Sprintf("Loading %s...", mount)
			return m, m.fetchDiskDetail(mount)
		}
	case "/":
		m.filterActive = true
		m.filterText = ""
		m.diskCursor = 0
	case "1", "2", "3", "4", "5", "6":
		col := int(msg.Runes[0] - '0')
		if col >= 1 && col <= 6 {
			if col == m.sortColumn {
				m.sortAsc = !m.sortAsc
			} else {
				m.sortColumn = col
				m.sortAsc = true
			}
			m.sortView()
		}
	case "r":
		m.disks = nil
		m.sortColumn = 0
		showLoading(&m, "disk", "Loading disk info...")
		return m, m.fetchDisk()
	case "esc":
		if m.filterText != "" {
			m.filterText = ""
			m.diskCursor = 0
		} else {
			m.view = viewResourcePicker
		}
	}
	return m, nil
}

func (m Model) handleSubscriptionKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.subscriptionCursor > 0 {
			m.subscriptionCursor--
		}
	case "down", "j":
		if m.subscriptionCursor < len(m.subscriptions)-1 {
			m.subscriptionCursor++
		}
	case "u":
		regType := ""
		for _, s := range m.subscriptions {
			if s.Field == "Registration" {
				regType = s.Value
				break
			}
		}
		if regType == "" || regType == "Unknown" {
			m.flash = "Host is not registered"
			m.flashError = true
			return m, nil
		}
		h := m.hosts[m.selectedHost]
		cmd := "sudo subscription-manager unregister && sudo subscription-manager clean"
		if regType == "Satellite" {
			cmd += " && sudo dnf remove -y katello-ca-consumer-*"
		}
		banner := fmt.Sprintf("unregister from %s on %s", regType, h.Entry.Name)
		m.modal = NewConfirmModal("Confirm",
			fmt.Sprintf("Unregister from %s? [Y/n]", regType),
			sshHandover(h, []string{cmd}, banner))
	case "g":
		h := m.hosts[m.selectedHost]
		orgID := h.Entry.RHOrgID
		actKey := h.Entry.RHActivationKey
		satURL := h.Entry.SatelliteURL
		if orgID == "" || actKey == "" {
			m.flash = "rh_org_id and rh_activation_key required in config"
			m.flashError = true
			return m, nil
		}
		var cmd, target string
		if satURL != "" {
			target = "Satellite"
			cmd = fmt.Sprintf("sudo subscription-manager clean && sudo dnf install -y 'http://%s/pub/katello-ca-consumer-latest.noarch.rpm' --disablerepo='*' && sudo subscription-manager register --org='%s' --activationkey='%s' --force",
				shellQuote(satURL), shellQuote(orgID), shellQuote(actKey))
		} else {
			target = "Red Hat CDN"
			cmd = fmt.Sprintf("sudo subscription-manager register --org='%s' --activationkey='%s'",
				shellQuote(orgID), shellQuote(actKey))
		}
		banner := fmt.Sprintf("register to %s on %s", target, h.Entry.Name)
		m.modal = NewConfirmModal("Confirm",
			fmt.Sprintf("Register to %s? [Y/n]", target),
			sshHandover(h, []string{cmd}, banner))
	case "d":
		if len(m.subscriptions) > 0 {
			sub := m.subscriptions[m.subscriptionCursor]
			if strings.HasPrefix(sub.Field, "Repo: ") {
				repoID := strings.TrimPrefix(sub.Field, "Repo: ")
				h := m.hosts[m.selectedHost]
				cmd := fmt.Sprintf("sudo dnf config-manager --set-disabled '%s' && echo '' && echo '\u2713 Repo %s disabled'", shellQuote(repoID), repoID)
				banner := fmt.Sprintf("disable %s on %s", repoID, h.Entry.Name)
				m.modal = NewConfirmModal("Confirm",
					fmt.Sprintf("Disable repo %s? [Y/n]", repoID),
					sshHandover(h, []string{cmd}, banner))
			}
		}
	case "c":
		if len(m.subscriptions) > 0 {
			sub := m.subscriptions[m.subscriptionCursor]
			if strings.HasPrefix(sub.Field, "Repo: ") {
				repoID := strings.TrimPrefix(sub.Field, "Repo: ")
				// Redirect dnf's stderr to stdout *inline*. The sudo rewrite adds
				// `sudo -S 2>/dev/null` to suppress the password prompt, and dnf
				// inherits that /dev/null on fd 2 — so without our own `2>&1` after
				// the dnf args, all error lines vanish. Capture dnf's exit into $rc
				// before any subsequent command can stomp on $?.
				cmd := fmt.Sprintf("sudo dnf makecache --refresh --repo='%s' 2>&1; rc=$?; echo; echo \"--- exit: $rc\"", shellQuote(repoID))
				return m, m.startSSHStream(SSHStreamConfig{
					Command:    cmd,
					Title:      "Check repo " + repoID,
					SourceName: "check-" + repoID,
					ReturnView: viewSubscription,
					HostIdx:    m.selectedHost,
					Sudo:       true,
					AutoDone:   true,
				})
			}
		}
	case "r":
		m.subscriptions = nil
		showLoading(&m, "subscription", "Loading subscription...")
		return m, m.fetchSubscription()
	case "esc":
		m.view = viewResourcePicker
	}
	return m, nil
}

func (m Model) handleAccountListKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.showAccountDetail {
		m.showAccountDetail = false
		m.accountDetailSections = nil
		return m, nil
	}
	switch msg.String() {
	case "up", "k":
		if m.accountCursor > 0 {
			m.accountCursor--
		}
	case "down", "j":
		accts := m.filteredAccounts()
		if m.accountCursor < len(accts)-1 {
			m.accountCursor++
		}
	case "enter":
		accts := m.filteredAccounts()
		if len(accts) > 0 {
			user := accts[m.accountCursor].User
			m.flash = fmt.Sprintf("Loading %s...", user)
			return m, m.fetchAccountDetail(user)
		}
	case "/":
		m.filterActive = true
		m.filterText = ""
		m.accountCursor = 0
	case "1", "2", "3", "4", "5":
		col := int(msg.Runes[0] - '0')
		if col >= 1 && col <= 5 {
			if col == m.sortColumn {
				m.sortAsc = !m.sortAsc
			} else {
				m.sortColumn = col
				m.sortAsc = true
			}
			m.sortView()
		}
	case "r":
		m.accounts = nil
		m.sortColumn = 0
		showLoading(&m, "accounts", "Loading accounts...")
		return m, m.fetchAccounts()
	case "esc":
		if m.filterText != "" {
			m.filterText = ""
			m.accountCursor = 0
		} else {
			m.view = viewResourcePicker
		}
	}
	return m, nil
}

func (m Model) handleNetworkPickerKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.networkCursor > 0 {
			m.networkCursor--
		}
	case "down", "j":
		if m.networkCursor < networkSubViewCount-1 {
			m.networkCursor++
		}
	case "enter":
		switch m.networkCursor {
		case 0: // Interfaces
			m.interfaceCursor = 0
			m.sortColumn = 0
			m.view = viewNetworkInterfaces
			return m, m.fetchInterfaces()
		case 1: // Ports
			m.portCursor = 0
			m.sortColumn = 0
			m.filterText = ""
			m.view = viewNetworkPorts
			return m, m.fetchPorts()
		case 2: // Routes & DNS
			m.routeCursor = 0
			m.sortColumn = 0
			m.view = viewNetworkRoutes
			return m, m.fetchRoutes()
		case 3: // Firewall
			m.firewallCursor = 0
			m.sortColumn = 0
			m.firewallBackend = ""
			m.filterText = ""
			m.view = viewNetworkFirewall
			return m, m.fetchFirewall()
		}
	case "r":
		showLoading(&m, "network", "Loading network info...")
		return m, m.fetchNetworkInfo()
	case "esc":
		m.view = viewResourcePicker
	}
	return m, nil
}

func (m Model) handleInterfaceListKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.interfaceCursor > 0 {
			m.interfaceCursor--
		}
	case "down", "j":
		filtered := m.filteredInterfaces()
		if m.interfaceCursor < len(filtered)-1 {
			m.interfaceCursor++
		}
	case "/":
		m.filterActive = true
		m.filterText = ""
		m.interfaceCursor = 0
	case "1", "2", "3", "4":
		col := int(msg.Runes[0] - '0')
		if col >= 1 && col <= 4 {
			if col == m.sortColumn {
				m.sortAsc = !m.sortAsc
			} else {
				m.sortColumn = col
				m.sortAsc = true
			}
			m.sortView()
		}
	case "r":
		m.interfaces = nil
		m.sortColumn = 0
		showLoading(&m, "interfaces", "Loading interfaces...")
		return m, m.fetchInterfaces()
	case "esc":
		if m.filterText != "" {
			m.filterText = ""
			m.interfaceCursor = 0
		} else {
			m.view = viewNetworkPicker
		}
	}
	return m, nil
}

func (m Model) handlePortListKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	filtered := m.filteredPorts()

	switch msg.String() {
	case "up", "k":
		if m.portCursor > 0 {
			m.portCursor--
		}
	case "down", "j":
		if m.portCursor < len(filtered)-1 {
			m.portCursor++
		}
	case "/":
		m.filterActive = true
		m.filterText = ""
		m.portCursor = 0
	case "1", "2", "3", "4":
		col := int(msg.Runes[0] - '0')
		if col >= 1 && col <= 4 {
			if col == m.sortColumn {
				m.sortAsc = !m.sortAsc
			} else {
				m.sortColumn = col
				m.sortAsc = true
			}
			m.sortView()
		}
	case "r":
		m.ports = nil
		m.sortColumn = 0
		m.filterText = ""
		showLoading(&m, "ports", "Loading ports...")
		return m, m.fetchPorts()
	case "esc":
		if m.filterText != "" {
			m.filterText = ""
			m.portCursor = 0
		} else {
			m.view = viewNetworkPicker
		}
	}
	return m, nil
}

func (m Model) handleFirewallListKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.firewallCursor > 0 {
			m.firewallCursor--
		}
	case "down", "j":
		rules := m.filteredFirewallRules()
		if m.firewallCursor < len(rules)-1 {
			m.firewallCursor++
		}
	case "/":
		m.filterActive = true
		m.filterText = ""
		m.firewallCursor = 0
	case "1", "2", "3", "4", "5":
		col := int(msg.Runes[0] - '0')
		if col >= 1 && col <= 5 {
			if col == m.sortColumn {
				m.sortAsc = !m.sortAsc
			} else {
				m.sortColumn = col
				m.sortAsc = true
			}
			m.sortView()
		}
	case "r":
		m.firewallRules = nil
		m.sortColumn = 0
		m.firewallBackend = ""
		showLoading(&m, "firewall", "Loading firewall...")
		return m, m.fetchFirewall()
	case "esc":
		if m.filterText != "" {
			m.filterText = ""
			m.firewallCursor = 0
		} else {
			m.view = viewNetworkPicker
		}
	}
	return m, nil
}

func (m Model) handleRouteListKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.routeCursor > 0 {
			m.routeCursor--
		}
	case "down", "j":
		filtered := m.filteredRoutes()
		if m.routeCursor < len(filtered)-1 {
			m.routeCursor++
		}
	case "/":
		m.filterActive = true
		m.filterText = ""
		m.routeCursor = 0
	case "1", "2", "3", "4":
		col := int(msg.Runes[0] - '0')
		if col >= 1 && col <= 4 {
			if col == m.sortColumn {
				m.sortAsc = !m.sortAsc
			} else {
				m.sortColumn = col
				m.sortAsc = true
			}
			m.sortView()
		}
	case "r":
		m.routes = nil
		m.sortColumn = 0
		showLoading(&m, "routes", "Loading routes...")
		return m, m.fetchRoutes()
	case "esc":
		if m.filterText != "" {
			m.filterText = ""
			m.routeCursor = 0
		} else {
			m.view = viewNetworkPicker
		}
	}
	return m, nil
}

func (m Model) handleFailedLoginKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	filtered := m.filteredFailedLogins()

	switch msg.String() {
	case "up", "k":
		if m.failedLoginCursor > 0 {
			m.failedLoginCursor--
		}
	case "down", "j":
		if m.failedLoginCursor < len(filtered)-1 {
			m.failedLoginCursor++
		}
	case "/":
		m.filterActive = true
		m.filterText = ""
		m.failedLoginCursor = 0
	case "1", "2", "3", "4":
		col := int(msg.Runes[0] - '0')
		if col >= 1 && col <= 4 {
			if col == m.sortColumn {
				m.sortAsc = !m.sortAsc
			} else {
				m.sortColumn = col
				m.sortAsc = true
			}
			m.sortView()
		}
	case "r":
		m.failedLogins = nil
		m.sortColumn = 0
		m.filterText = ""
		showLoading(&m, "failedlogins", "Loading failed logins...")
		return m, m.fetchFailedLogins()
	case "esc":
		if m.filterText != "" {
			m.filterText = ""
			m.failedLoginCursor = 0
		} else {
			m.view = viewResourcePicker
		}
	}
	return m, nil
}

func (m Model) handleSudoKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	filtered := m.filteredSudoEntries()

	switch msg.String() {
	case "up", "k":
		if m.sudoCursor > 0 {
			m.sudoCursor--
		}
	case "down", "j":
		if m.sudoCursor < len(filtered)-1 {
			m.sudoCursor++
		}
	case "/":
		m.filterActive = true
		m.filterText = ""
		m.sudoCursor = 0
	case "1", "2", "3", "4":
		col := int(msg.Runes[0] - '0')
		if col >= 1 && col <= 4 {
			if col == m.sortColumn {
				m.sortAsc = !m.sortAsc
			} else {
				m.sortColumn = col
				m.sortAsc = true
			}
			m.sortView()
		}
	case "r":
		m.sudoEntries = nil
		m.sortColumn = 0
		m.filterText = ""
		showLoading(&m, "sudo", "Loading sudo activity...")
		return m, m.fetchSudoActivity()
	case "esc":
		if m.filterText != "" {
			m.filterText = ""
			m.sudoCursor = 0
		} else {
			m.view = viewResourcePicker
		}
	}
	return m, nil
}

func (m Model) handleSELinuxKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	filtered := m.filteredSELinuxDenials()

	switch msg.String() {
	case "up", "k":
		if m.selinuxCursor > 0 {
			m.selinuxCursor--
		}
	case "down", "j":
		if m.selinuxCursor < len(filtered)-1 {
			m.selinuxCursor++
		}
	case "/":
		m.filterActive = true
		m.filterText = ""
		m.selinuxCursor = 0
	case "1", "2", "3", "4", "5":
		col := int(msg.Runes[0] - '0')
		if col >= 1 && col <= 5 {
			if col == m.sortColumn {
				m.sortAsc = !m.sortAsc
			} else {
				m.sortColumn = col
				m.sortAsc = true
			}
			m.sortView()
		}
	case "r":
		m.selinuxDenials = nil
		m.sortColumn = 0
		m.filterText = ""
		showLoading(&m, "selinux", "Loading SELinux denials...")
		return m, m.fetchSELinuxDenials()
	case "esc":
		if m.filterText != "" {
			m.filterText = ""
			m.selinuxCursor = 0
		} else {
			m.view = viewResourcePicker
		}
	}
	return m, nil
}

func (m Model) handleAuditKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	filtered := m.filteredAuditEvents()

	switch msg.String() {
	case "up", "k":
		if m.auditCursor > 0 {
			m.auditCursor--
		}
	case "down", "j":
		if m.auditCursor < len(filtered)-1 {
			m.auditCursor++
		}
	case "/":
		m.filterActive = true
		m.filterText = ""
		m.auditCursor = 0
	case "1", "2", "3", "4", "5":
		col := int(msg.Runes[0] - '0')
		if col >= 1 && col <= 5 {
			if col == m.sortColumn {
				m.sortAsc = !m.sortAsc
			} else {
				m.sortColumn = col
				m.sortAsc = true
			}
			m.sortView()
		}
	case "r":
		m.auditEvents = nil
		m.sortColumn = 0
		m.filterText = ""
		showLoading(&m, "audit", "Loading audit summary...")
		return m, m.fetchAuditSummary()
	case "esc":
		if m.filterText != "" {
			m.filterText = ""
			m.auditCursor = 0
		} else {
			m.view = viewResourcePicker
		}
	}
	return m, nil
}

func (m Model) handleMetricsKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.metricsCursor > 0 {
			m.metricsCursor--
		}
	case "down", "j":
		if m.metricsCursor < len(m.hosts)-1 {
			m.metricsCursor++
		}
	case "1", "2", "3", "4", "5":
		col := int(msg.Runes[0] - '0')
		if col == m.sortColumn {
			m.sortAsc = !m.sortAsc
		} else {
			m.sortColumn = col
			m.sortAsc = true
		}
		m.sortView()
	case "r":
		m.metrics = make(map[int]config.HostMetrics)
		m.metricErrors = make(map[int]bool)
		m.metricsSortedIdx = nil
		m.sortColumn = 0
		m.flash = "Refreshing..."
		return m, m.fetchAllMetrics()
	case "esc":
		m.view = viewHostList
	}
	return m, nil
}

func (m Model) handleAzureSubListKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.azureSubCursor > 0 {
			m.azureSubCursor--
		}
	case "down", "j":
		if m.azureSubCursor < len(m.azureSubs)-1 {
			m.azureSubCursor++
		}
	case "enter":
		if len(m.azureSubs) > 0 {
			m.selectedAzureSub = m.azureSubCursor
			m.azureResourceCursor = 0
			m.azureResourceCounts = azure.AzureResourceCounts{}
			m.azureResourceErr = nil
			m.azureCountsLoaded = false
			m.view = viewAzureResourcePicker
			return m, m.fetchAzureResourceCounts()
		}
	case "r":
		f := m.fleets[m.selectedFleet]
		m.azureSubs = buildAzureSubList(f)
		m.azureSubCursor = 0
		m.flash = "Refreshing..."
		m.flashError = false
		return m, m.startAzureProbe()
	case "/":
		m.filterActive = true
		m.filterText = ""
		m.azureSubCursor = 0
	case "1", "2", "3":
		col := int(msg.Runes[0] - '0')
		if m.sortColumn == col {
			m.sortAsc = !m.sortAsc
		} else {
			m.sortColumn = col
			m.sortAsc = true
		}
		m.sortView()
	case "esc":
		m.view = viewFleetPicker
		m.sortColumn = 0
		m.filterText = ""
		m.filterActive = false
	case "q":
		return m, tea.Quit
	}
	return m, nil
}

const azureResourceCount = 2

func (m Model) handleAzureResourcePickerKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.azureResourceCursor > 0 {
			m.azureResourceCursor--
		}
	case "down", "j":
		if m.azureResourceCursor < azureResourceCount-1 {
			m.azureResourceCursor++
		}
	case "enter":
		switch m.azureResourceCursor {
		case 0: // VMs
			m.azureVMs = nil
			m.azureVMCursor = 0
			m.sortColumn = 0
			m.filterText = ""
			m.filterActive = false
			m.view = viewAzureVMList
			showLoading(&m, "vms", "Loading VMs...")
			return m, m.fetchAzureVMs()
		case 1: // AKS Clusters
			m.azureAKSClusters = nil
			m.azureAKSCursor = 0
			m.sortColumn = 0
			m.sortAsc = true
			m.filterText = ""
			m.filterActive = false
			m.view = viewAzureAKSList
			showLoading(&m, "aks", "Loading AKS clusters...")
			return m, m.fetchAzureAKSClusters()
		}
	case "r":
		m.azureResourceCounts = azure.AzureResourceCounts{}
		m.azureResourceErr = nil
		m.azureCountsLoaded = false
		showLoading(&m, "resourcecounts", "Loading resource counts...")
		return m, m.fetchAzureResourceCounts()
	case "esc":
		m.view = viewAzureSubList
	case "q":
		return m, tea.Quit
	}
	return m, nil
}

func (m Model) handleAzureVMListKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		filtered := m.filteredAzureVMs()
		if m.azureVMCursor > 0 {
			m.azureVMCursor--
		}
		_ = filtered
	case "down", "j":
		filtered := m.filteredAzureVMs()
		if m.azureVMCursor < len(filtered)-1 {
			m.azureVMCursor++
		}
	case "enter":
		filtered := m.filteredAzureVMs()
		if len(filtered) > 0 && m.azureVMCursor < len(filtered) {
			vm := filtered[m.azureVMCursor]
			m.flash = fmt.Sprintf("Loading %s...", vm.Name)
			m.azureActivityLog = nil
			m.azureActivityCursor = 0
			return m, tea.Batch(m.fetchAzureVMDetail(vm.Name, vm.ResourceGroup), m.fetchAzureActivityLog(vm.ResourceGroup))
		}
	case "s":
		filtered := m.filteredAzureVMs()
		if len(filtered) > 0 && m.azureVMCursor < len(filtered) {
			vm := filtered[m.azureVMCursor]
			if vm.PowerState == "running" {
				m.flash = fmt.Sprintf("%s is already running", vm.Name)
			} else {
				am, sub, logger := m.azure, m.azureSubs[m.selectedAzureSub], m.logger
				vmName, rg := vm.Name, vm.ResourceGroup
				m.modal = NewTransitionConfirmModal(
					fmt.Sprintf("start %s? [Y/n]", vm.Name),
					transition{
						ResourceType: "vm", ResourceName: vm.Name,
						Action: "start", Display: "starting...", TargetState: "running",
						Strategy: "poll",
						ExecFn:   m.executeAzureVMAction(vmName, rg, "start"),
						PollFn: func() (string, error) {
							states, err := azure.FetchVMPowerStates(am, sub.ID, []string{vmName}, logger)
							if err != nil {
								return "", err
							}
							if s, ok := states[strings.ToLower(vmName)]; ok {
								return s, nil
							}
							return "", fmt.Errorf("vm %s not in poll results", vmName)
						},
						RefreshFn:       func() tea.Cmd { return m.fetchAzureVMs() },
						IsTransitioning: isAzureTransitioningState,
					})
			}
		}
	case "o":
		filtered := m.filteredAzureVMs()
		if len(filtered) > 0 && m.azureVMCursor < len(filtered) {
			vm := filtered[m.azureVMCursor]
			if vm.PowerState == "deallocated" {
				m.flash = fmt.Sprintf("%s is already deallocated", vm.Name)
			} else {
				am, sub, logger := m.azure, m.azureSubs[m.selectedAzureSub], m.logger
				vmName, rg := vm.Name, vm.ResourceGroup
				m.modal = NewTransitionConfirmModal(
					fmt.Sprintf("deallocate %s? [Y/n]", vm.Name),
					transition{
						ResourceType: "vm", ResourceName: vm.Name,
						Action: "deallocate", Display: "deallocating...", TargetState: "deallocated",
						Strategy: "poll",
						ExecFn:   m.executeAzureVMAction(vmName, rg, "deallocate"),
						PollFn: func() (string, error) {
							states, err := azure.FetchVMPowerStates(am, sub.ID, []string{vmName}, logger)
							if err != nil {
								return "", err
							}
							if s, ok := states[strings.ToLower(vmName)]; ok {
								return s, nil
							}
							return "", fmt.Errorf("vm %s not in poll results", vmName)
						},
						RefreshFn:       func() tea.Cmd { return m.fetchAzureVMs() },
						IsTransitioning: isAzureTransitioningState,
					})
			}
		}
	case "t":
		filtered := m.filteredAzureVMs()
		if len(filtered) > 0 && m.azureVMCursor < len(filtered) {
			vm := filtered[m.azureVMCursor]
			if vm.PowerState != "running" {
				m.flash = fmt.Sprintf("%s is not running (state: %s)", vm.Name, vm.PowerState)
			} else {
				am, sub, logger := m.azure, m.azureSubs[m.selectedAzureSub], m.logger
				vmName, rg := vm.Name, vm.ResourceGroup
				m.modal = NewTransitionConfirmModal(
					fmt.Sprintf("restart %s? [Y/n]", vm.Name),
					transition{
						ResourceType: "vm", ResourceName: vm.Name,
						Action: "restart", Display: "restarting...", TargetState: "running",
						Strategy: "poll",
						ExecFn:   m.executeAzureVMAction(vmName, rg, "restart"),
						PollFn: func() (string, error) {
							states, err := azure.FetchVMPowerStates(am, sub.ID, []string{vmName}, logger)
							if err != nil {
								return "", err
							}
							if s, ok := states[strings.ToLower(vmName)]; ok {
								return s, nil
							}
							return "", fmt.Errorf("vm %s not in poll results", vmName)
						},
						RefreshFn:       func() tea.Cmd { return m.fetchAzureVMs() },
						IsTransitioning: isAzureTransitioningState,
					})
			}
		}
	case "/":
		m.filterActive = true
		m.filterText = ""
		m.azureVMCursor = 0
	case "1", "2", "3", "4", "5", "6", "7":
		col := int(msg.Runes[0] - '0')
		if m.sortColumn == col {
			m.sortAsc = !m.sortAsc
		} else {
			m.sortColumn = col
			m.sortAsc = true
		}
		m.sortView()
	case "r":
		m.azureVMs = nil
		m.azureVMCursor = 0
		showLoading(&m, "vms", "Loading VMs...")
		return m, m.fetchAzureVMs()
	case "esc":
		m.view = viewAzureResourcePicker
		m.sortColumn = 0
		m.filterText = ""
		m.filterActive = false
	case "q":
		return m, tea.Quit
	}
	return m, nil
}

func (m Model) handleAzureVMDetailKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.azureVMDetailScroll > 0 {
			m.azureVMDetailScroll--
		}
	case "down", "j":
		m.azureVMDetailScroll++
	case "a":
		m.azureActivityLog = nil
		m.azureActivityCursor = 0
		m.azureVMDetailScroll = 0
		m.flash = "Loading activity log..."
		m.flashError = false
		return m, m.fetchAzureActivityLog(m.azureVMDetail.ResourceGroup)
	case "r":
		vm := m.azureVMDetail
		m.azureVMDetailScroll = 0
		m.flash = "Refreshing..."
		m.flashError = false
		return m, tea.Batch(m.fetchAzureVMDetail(vm.Name, vm.ResourceGroup), m.fetchAzureActivityLog(vm.ResourceGroup))
	case "esc":
		m.showAzureVMDetail = false
		m.azureActivityLog = nil
		m.azureVMDetailScroll = 0
		m.view = viewAzureVMList
	case "q":
		return m, tea.Quit
	}
	return m, nil
}

func (m Model) handleAzureAKSListKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		filtered := m.filteredAzureAKS()
		if m.azureAKSCursor > 0 {
			m.azureAKSCursor--
		}
		_ = filtered
	case "down", "j":
		filtered := m.filteredAzureAKS()
		if m.azureAKSCursor < len(filtered)-1 {
			m.azureAKSCursor++
		}
	case "enter":
		filtered := m.filteredAzureAKS()
		if len(filtered) > 0 && m.azureAKSCursor < len(filtered) {
			c := filtered[m.azureAKSCursor]
			m.azureAKSDetail = c
			m.azureActivityLog = nil
			m.azureActivityCursor = 0
			m.view = viewAzureAKSDetail
			return m, m.fetchAzureActivityLog(c.ResourceGroup)
		}
	case "s":
		filtered := m.filteredAzureAKS()
		if len(filtered) > 0 && m.azureAKSCursor < len(filtered) {
			c := filtered[m.azureAKSCursor]
			am, sub, logger := m.azure, m.azureSubs[m.selectedAzureSub], m.logger
			clusterName, rg := c.Name, c.ResourceGroup
			m.modal = NewTransitionConfirmModal(
				fmt.Sprintf("start %s? [Y/n]", c.Name),
				transition{
					ResourceType: "aks", ResourceName: c.Name,
					Action: "start", Display: "starting...", TargetState: "running",
					Strategy: "poll",
					ExecFn:   m.executeAzureAKSAction(clusterName, rg, "start"),
					PollFn: func() (string, error) {
						states, err := azure.FetchAKSPowerStates(am, sub.ID, []string{clusterName}, logger)
						if err != nil {
							return "", err
						}
						if s, ok := states[strings.ToLower(clusterName)]; ok {
							return s, nil
						}
						return "gone", nil
					},
					RefreshFn:       func() tea.Cmd { return m.fetchAzureAKSClusters() },
					IsTransitioning: isAzureTransitioningState,
				})
		}
	case "o":
		filtered := m.filteredAzureAKS()
		if len(filtered) > 0 && m.azureAKSCursor < len(filtered) {
			c := filtered[m.azureAKSCursor]
			am, sub, logger := m.azure, m.azureSubs[m.selectedAzureSub], m.logger
			clusterName, rg := c.Name, c.ResourceGroup
			m.modal = NewTransitionConfirmModal(
				fmt.Sprintf("stop %s? [Y/n]", c.Name),
				transition{
					ResourceType: "aks", ResourceName: c.Name,
					Action: "stop", Display: "stopping...", TargetState: "stopped",
					Strategy: "poll",
					ExecFn:   m.executeAzureAKSAction(clusterName, rg, "stop"),
					PollFn: func() (string, error) {
						states, err := azure.FetchAKSPowerStates(am, sub.ID, []string{clusterName}, logger)
						if err != nil {
							return "", err
						}
						if s, ok := states[strings.ToLower(clusterName)]; ok {
							return s, nil
						}
						return "gone", nil
					},
					RefreshFn:       func() tea.Cmd { return m.fetchAzureAKSClusters() },
					IsTransitioning: isAzureTransitioningState,
				})
		}
	case "d":
		filtered := m.filteredAzureAKS()
		if len(filtered) > 0 && m.azureAKSCursor < len(filtered) {
			c := filtered[m.azureAKSCursor]
			am, sub, logger := m.azure, m.azureSubs[m.selectedAzureSub], m.logger
			clusterName, rg := c.Name, c.ResourceGroup
			m.modal = NewTransitionConfirmModal(
				fmt.Sprintf("DELETE cluster %s? This is irreversible! [Y/n]", c.Name),
				transition{
					ResourceType: "aks", ResourceName: c.Name,
					Action: "delete", Display: "deleting...", TargetState: "gone",
					Strategy: "poll",
					ExecFn:   m.executeAzureAKSAction(clusterName, rg, "delete"),
					PollFn: func() (string, error) {
						states, err := azure.FetchAKSPowerStates(am, sub.ID, []string{clusterName}, logger)
						if err != nil {
							return "", err
						}
						if s, ok := states[strings.ToLower(clusterName)]; ok {
							return s, nil
						}
						return "gone", nil
					},
					RefreshFn:       func() tea.Cmd { return m.fetchAzureAKSClusters() },
					IsTransitioning: isAzureTransitioningState,
				})
		}
	case "/":
		m.filterActive = true
		m.filterText = ""
		m.azureAKSCursor = 0
	case "1", "2", "3", "4", "5", "6", "7", "8", "9":
		col := int(msg.Runes[0] - '0')
		if m.sortColumn == col {
			m.sortAsc = !m.sortAsc
		} else {
			m.sortColumn = col
			m.sortAsc = true
		}
		m.sortView()
	case "r":
		m.azureAKSClusters = nil
		m.azureAKSCursor = 0
		showLoading(&m, "aks", "Loading AKS clusters...")
		return m, m.fetchAzureAKSClusters()
	case "esc":
		m.view = viewAzureResourcePicker
		m.sortColumn = 0
		m.filterText = ""
		m.filterActive = false
	case "q":
		return m, tea.Quit
	}
	return m, nil
}

func (m Model) handleAzureAKSDetailKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.azureActivityCursor > 0 {
			m.azureActivityCursor--
		}
	case "down", "j":
		if m.azureActivityCursor < len(m.azureActivityLog)-1 {
			m.azureActivityCursor++
		}
	case "a":
		m.azureActivityLog = nil
		m.azureActivityCursor = 0
		m.flash = "Loading activity log..."
		m.flashError = false
		return m, m.fetchAzureActivityLog(m.azureAKSDetail.ResourceGroup)
	case "esc":
		m.azureActivityLog = nil
		m.view = viewAzureAKSList
	case "q":
		return m, tea.Quit
	}
	return m, nil
}

const k8sResourceCount = 3

func (m Model) handleK8sClusterListKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.k8sClusterCursor > 0 {
			m.k8sClusterCursor--
		}
	case "down", "j":
		if m.k8sClusterCursor < len(m.k8sClusters)-1 {
			m.k8sClusterCursor++
		}
	case "enter":
		if len(m.k8sClusters) > 0 {
			c := m.k8sClusters[m.k8sClusterCursor]
			if c.Status != k8s.ClusterOnline {
				m.flash = "Cluster not available"
				m.flashError = true
				return m, nil
			}
			m.selectedK8sCluster = m.k8sClusterCursor
			m.k8sContexts = nil
			m.k8sContextCursor = 0
			m.view = viewK8sContextList
			return m, m.fetchK8sContexts(c.Name)
		}
	case "r":
		f := m.fleets[m.selectedFleet]
		m.k8sClusters = buildK8sClusterList(f)
		m.k8sClusterCursor = 0
		return m, m.startK8sProbe()
	case "/":
		m.filterActive = true
		m.filterText = ""
		m.k8sClusterCursor = 0
	case "1", "2", "3":
		col := int(msg.Runes[0] - '0')
		if m.sortColumn == col {
			m.sortAsc = !m.sortAsc
		} else {
			m.sortColumn = col
			m.sortAsc = true
		}
		m.sortView()
	case "esc":
		m.view = viewFleetPicker
		m.sortColumn = 0
		m.filterText = ""
		m.filterActive = false
	case "q":
		return m, tea.Quit
	}
	return m, nil
}

func (m Model) handleK8sContextListKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.k8sContextCursor > 0 {
			m.k8sContextCursor--
		}
	case "down", "j":
		if m.k8sContextCursor < len(m.k8sContexts)-1 {
			m.k8sContextCursor++
		}
	case "enter":
		if len(m.k8sContexts) > 0 && m.k8sContextCursor < len(m.k8sContexts) {
			m.selectedK8sContext = m.k8sContexts[m.k8sContextCursor].Name
			m.k8sResourceCursor = 0
			m.k8sResourceCounts = k8s.K8sResourceCounts{}
			m.k8sResourceErrors = nil
			m.k8sCountsLoaded = false
			m.view = viewK8sResourcePicker
			return m, m.fetchK8sResourceCounts()
		}
	case "d":
		if len(m.k8sContexts) > 0 && m.k8sContextCursor < len(m.k8sContexts) {
			ctx := m.k8sContexts[m.k8sContextCursor]
			ctxName := ctx.Name
			clusterName := m.k8sClusters[m.selectedK8sCluster].Name
			m.modal = NewTransitionConfirmModal(
				fmt.Sprintf("delete context %s? [Y/n]", ctx.Name),
				transition{
					ResourceType: "k8s-context", ResourceName: ctx.Name,
					Action: "delete", Display: "deleting...",
					Strategy: "oneshot",
					ExecFn:   m.executeK8sContextDelete(ctxName),
					RefreshFn: func() tea.Cmd {
						return m.fetchK8sContexts(clusterName)
					},
				})
		}
	case "r":
		m.k8sContexts = nil
		m.k8sContextCursor = 0
		showLoading(&m, "contexts", "Loading contexts...")
		return m, m.fetchK8sContexts(m.k8sClusters[m.selectedK8sCluster].Name)
	case "/":
		m.filterActive = true
		m.filterText = ""
		m.k8sContextCursor = 0
	case "esc":
		m.view = viewK8sClusterList
		m.filterText = ""
		m.filterActive = false
	case "q":
		return m, tea.Quit
	}
	return m, nil
}

func (m Model) handleK8sResourcePickerKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.k8sResourceCursor > 0 {
			m.k8sResourceCursor--
		}
	case "down", "j":
		if m.k8sResourceCursor < k8sResourceCount-1 {
			m.k8sResourceCursor++
		}
	case "enter":
		switch m.k8sResourceCursor {
		case 0:
			m.k8sNamespaces = nil
			m.k8sNamespaceCursor = 0
			m.sortColumn = 0
			m.filterText = ""
			m.filterActive = false
			m.view = viewK8sNamespaceList
			return m, m.fetchK8sNamespaces()
		case 1:
			m.k8sNodes = nil
			m.k8sNodeCursor = 0
			m.sortColumn = 0
			m.filterText = ""
			m.filterActive = false
			m.view = viewK8sNodeList
			return m, m.fetchK8sNodes()
		case 2:
			m.flash = "ArgoCD Apps view coming in next PR"
			m.flashError = false
		}
	case "r":
		m.k8sResourceCounts = k8s.K8sResourceCounts{}
		m.k8sResourceErrors = nil
		m.k8sCountsLoaded = false
		showLoading(&m, "resourcecounts", "Loading resource counts...")
		return m, m.fetchK8sResourceCounts()
	case "esc":
		m.view = viewK8sContextList
	case "q":
		return m, tea.Quit
	}
	return m, nil
}

func (m Model) handleK8sNodeListKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		filtered := m.filteredK8sNodes()
		if m.k8sNodeCursor > 0 {
			m.k8sNodeCursor--
		}
		_ = filtered
	case "down", "j":
		filtered := m.filteredK8sNodes()
		if m.k8sNodeCursor < len(filtered)-1 {
			m.k8sNodeCursor++
		}
	case "enter":
		filtered := m.filteredK8sNodes()
		if len(filtered) > 0 && m.k8sNodeCursor < len(filtered) {
			node := filtered[m.k8sNodeCursor]
			m.flash = fmt.Sprintf("Loading %s...", node.Name)
			m.k8sNodeUsage = nil
			m.k8sNodePods = nil
			m.k8sNodePodCursor = 0
			return m, tea.Batch(
				m.fetchK8sNodeDetail(node.Name),
				m.fetchK8sNodeUsage(node.Name),
				m.fetchK8sNodePods(node.Name),
			)
		}
	case "/":
		m.filterActive = true
		m.filterText = ""
		m.k8sNodeCursor = 0
	case "1", "2", "3", "4", "5", "6", "7", "8", "9":
		col := int(msg.Runes[0] - '0')
		if m.sortColumn == col {
			m.sortAsc = !m.sortAsc
		} else {
			m.sortColumn = col
			m.sortAsc = true
		}
		m.sortView()
	case "r":
		m.k8sNodes = nil
		m.k8sNodeCursor = 0
		showLoading(&m, "nodes", "Loading nodes...")
		return m, m.fetchK8sNodes()
	case "esc":
		m.view = viewK8sResourcePicker
		m.sortColumn = 0
		m.filterText = ""
		m.filterActive = false
	case "q":
		return m, tea.Quit
	}
	return m, nil
}

func (m Model) handleK8sNodeDetailKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		filtered := m.filteredK8sNodePods()
		if m.k8sNodePodCursor > 0 {
			m.k8sNodePodCursor--
		}
		_ = filtered
	case "down", "j":
		filtered := m.filteredK8sNodePods()
		if m.k8sNodePodCursor < len(filtered)-1 {
			m.k8sNodePodCursor++
		}
	case "/":
		m.filterActive = true
		m.filterText = ""
		m.k8sNodePodCursor = 0
	case "1", "2", "3", "4", "5", "6", "7", "8", "9":
		col := int(msg.Runes[0] - '0')
		if m.sortColumn == col {
			m.sortAsc = !m.sortAsc
		} else {
			m.sortColumn = col
			m.sortAsc = true
		}
		m.sortView()
	case "r":
		nodeName := m.k8sNodeDetail.Name
		m.k8sNodeUsage = nil
		m.k8sNodePods = nil
		m.k8sNodePodCursor = 0
		return m, tea.Batch(
			m.fetchK8sNodeDetail(nodeName),
			m.fetchK8sNodeUsage(nodeName),
			m.fetchK8sNodePods(nodeName),
		)
	case "esc":
		if m.filterActive {
			m.filterActive = false
			m.filterText = ""
			m.k8sNodePodCursor = 0
		} else {
			m.view = viewK8sNodeList
			m.filterText = ""
			m.filterActive = false
		}
	case "q":
		return m, tea.Quit
	}
	return m, nil
}

func (m Model) handleK8sNamespaceListKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		filtered := m.filteredK8sNamespaces()
		if m.k8sNamespaceCursor > 0 {
			m.k8sNamespaceCursor--
		}
		_ = filtered
	case "down", "j":
		filtered := m.filteredK8sNamespaces()
		if m.k8sNamespaceCursor < len(filtered)-1 {
			m.k8sNamespaceCursor++
		}
	case "enter":
		filtered := m.filteredK8sNamespaces()
		if len(filtered) > 0 && m.k8sNamespaceCursor < len(filtered) {
			m.selectedK8sNamespace = m.k8sNamespaceCursor
			m.k8sWorkloads = nil
			m.k8sWorkloadCursor = 0
			m.sortColumn = 0
			m.filterText = ""
			m.filterActive = false
			m.view = viewK8sWorkloadList
			return m, m.fetchK8sWorkloads(filtered[m.k8sNamespaceCursor].Name)
		}
	case "/":
		m.filterActive = true
		m.filterText = ""
		m.k8sNamespaceCursor = 0
	case "1", "2", "3", "4", "5", "6", "7":
		col := int(msg.Runes[0] - '0')
		if m.sortColumn == col {
			m.sortAsc = !m.sortAsc
		} else {
			m.sortColumn = col
			m.sortAsc = true
		}
		m.sortView()
	case "r":
		m.k8sNamespaces = nil
		m.k8sNamespaceCursor = 0
		showLoading(&m, "namespaces", "Loading namespaces...")
		return m, m.fetchK8sNamespaces()
	case "esc":
		if m.filterActive {
			m.filterActive = false
			m.filterText = ""
			m.k8sNamespaceCursor = 0
		} else {
			m.view = viewK8sResourcePicker
			m.sortColumn = 0
			m.filterText = ""
			m.filterActive = false
		}
	case "q":
		return m, tea.Quit
	}
	return m, nil
}

func (m Model) handleK8sWorkloadListKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		filtered := m.filteredK8sWorkloads()
		if m.k8sWorkloadCursor > 0 {
			m.k8sWorkloadCursor--
		}
		_ = filtered
	case "down", "j":
		filtered := m.filteredK8sWorkloads()
		if m.k8sWorkloadCursor < len(filtered)-1 {
			m.k8sWorkloadCursor++
		}
	case "enter":
		filtered := m.filteredK8sWorkloads()
		if len(filtered) > 0 && m.k8sWorkloadCursor < len(filtered) {
			w := filtered[m.k8sWorkloadCursor]
			m.selectedK8sWorkload = m.k8sWorkloadCursor
			m.k8sPodList = nil
			m.k8sPodCursor = 0
			m.sortColumn = 0
			m.filterText = ""
			m.filterActive = false
			ns := m.filteredK8sNamespaces()[m.selectedK8sNamespace].Name
			m.view = viewK8sPodList
			return m, m.fetchK8sPods(ns, w.Name)
		}
	case "/":
		m.filterActive = true
		m.filterText = ""
		m.k8sWorkloadCursor = 0
	case "1", "2", "3":
		col := int(msg.Runes[0] - '0')
		if m.sortColumn == col {
			m.sortAsc = !m.sortAsc
		} else {
			m.sortColumn = col
			m.sortAsc = true
		}
		m.sortView()
	case "r":
		m.k8sWorkloads = nil
		m.k8sWorkloadCursor = 0
		ns := m.filteredK8sNamespaces()[m.selectedK8sNamespace].Name
		showLoading(&m, "workloads", "Loading workloads...")
		return m, m.fetchK8sWorkloads(ns)
	case "esc":
		if m.filterActive {
			m.filterActive = false
			m.filterText = ""
			m.k8sWorkloadCursor = 0
		} else {
			m.view = viewK8sNamespaceList
			m.sortColumn = 0
			m.filterText = ""
			m.filterActive = false
		}
	case "q":
		return m, tea.Quit
	}
	return m, nil
}

func (m Model) handleK8sPodListKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		filtered := m.filteredK8sPodList()
		if m.k8sPodCursor > 0 {
			m.k8sPodCursor--
		}
		_ = filtered
	case "down", "j":
		filtered := m.filteredK8sPodList()
		if m.k8sPodCursor < len(filtered)-1 {
			m.k8sPodCursor++
		}
	case "enter":
		filtered := m.filteredK8sPodList()
		if len(filtered) > 0 && m.k8sPodCursor < len(filtered) {
			p := filtered[m.k8sPodCursor]
			m.flash = fmt.Sprintf("Loading %s...", p.Name)
			m.k8sPodContainerCursor = 0
			return m, m.fetchK8sPodDetail(p.Namespace, p.Name)
		}
	case "/":
		m.filterActive = true
		m.filterText = ""
		m.k8sPodCursor = 0
	case "1", "2", "3", "4", "5", "6":
		col := int(msg.Runes[0] - '0')
		if m.sortColumn == col {
			m.sortAsc = !m.sortAsc
		} else {
			m.sortColumn = col
			m.sortAsc = true
		}
		m.sortView()
	case "l":
		filtered := m.filteredK8sPodList()
		if len(filtered) == 0 {
			return m, nil
		}
		var podNames []string
		for _, p := range filtered {
			podNames = append(podNames, p.Name)
		}
		m.k8sPodLogs = nil
		m.k8sPodLogCursor = 0
		m.k8sPodLogStreaming = false
		m.k8sPodLogAllContainers = false
		m.k8sPodLogMinLevel = 0
		m.k8sPodLogFromDetail = false
		m.filterText = ""
		m.filterActive = false
		m.sortColumn = 0
		ns := m.k8sNamespaces[m.selectedK8sNamespace].Name
		return m, m.fetchK8sPodLogs(ns, podNames)
	case "d":
		filtered := m.filteredK8sPodList()
		if len(filtered) > 0 && m.k8sPodCursor < len(filtered) {
			p := filtered[m.k8sPodCursor]
			km, ctxName := m.k8s, m.selectedK8sContext
			podName, podNs := p.Name, p.Namespace
			ns := m.k8sNamespaces[m.selectedK8sNamespace].Name
			wlName := m.k8sWorkloads[m.selectedK8sWorkload].Name
			m.modal = NewTransitionConfirmModal(
				fmt.Sprintf("delete pod %s? [Y/n]", p.Name),
				transition{
					ResourceType: "k8s-pod", ResourceName: p.Name,
					Action: "delete", Display: "deleting...", TargetState: "gone",
					Strategy: "poll",
					ExecFn:   m.executeK8sPodAction(podNs, podName, "delete"),
					PollFn: func() (string, error) {
						_, err := km.RunCommand("get", "pod", podName, "-n", podNs, "--context", ctxName, "-o", "name")
						if err != nil {
							errStr := err.Error()
							if strings.Contains(errStr, "not found") || strings.Contains(errStr, "NotFound") {
								return "gone", nil
							}
							return "", err
						}
						return "exists", nil
					},
					RefreshFn: func() tea.Cmd { return m.fetchK8sPods(ns, wlName) },
				})
		}
	case "r":
		m.k8sPodList = nil
		m.k8sPodCursor = 0
		ns := m.filteredK8sNamespaces()[m.selectedK8sNamespace].Name
		w := m.filteredK8sWorkloads()[m.selectedK8sWorkload]
		showLoading(&m, "pods", "Loading pods...")
		return m, m.fetchK8sPods(ns, w.Name)
	case "esc":
		if m.filterActive {
			m.filterActive = false
			m.filterText = ""
			m.k8sPodCursor = 0
		} else {
			m.view = viewK8sWorkloadList
			m.sortColumn = 0
			m.filterText = ""
			m.filterActive = false
		}
	case "q":
		return m, tea.Quit
	}
	return m, nil
}

func (m Model) handleK8sPodDetailKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.k8sPodContainerCursor > 0 {
			m.k8sPodContainerCursor--
		}
	case "down", "j":
		m.k8sPodContainerCursor++
	case "g":
		m.k8sPodContainerCursor = 0
	case "l":
		if m.k8sPodDetail.Name != "" {
			if m.k8sPodDetail.Status == "Succeeded" {
				m.flash = fmt.Sprintf("%s is not running (status: %s)", m.k8sPodDetail.Name, m.k8sPodDetail.Status)
			} else {
				m.k8sPodLogs = nil
				m.k8sPodLogCursor = 0
				m.k8sPodLogStreaming = false
				m.k8sPodLogAllContainers = false
				m.k8sPodLogMinLevel = 0
				m.k8sPodLogFromDetail = true
				m.filterText = ""
				m.filterActive = false
				m.sortColumn = 0
				ns := m.k8sPodDetail.Namespace
				return m, m.fetchK8sPodLogs(ns, []string{m.k8sPodDetail.Name})
			}
		}
	case "esc":
		m.view = viewK8sPodList
	case "q":
		return m, tea.Quit
	}
	return m, nil
}

func (m Model) handleK8sPodLogKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Detail view: scroll or close
	if m.showK8sLogDetail {
		switch msg.String() {
		case "up", "k":
			if m.k8sLogDetailScroll > 0 {
				m.k8sLogDetailScroll--
			}
		case "down", "j":
			m.k8sLogDetailScroll++
		case "g":
			m.k8sLogDetailScroll = 0
		case "esc", "q", "enter":
			m.showK8sLogDetail = false
			m.k8sLogDetailScroll = 0
			if m.k8sLogDetailWasStreaming {
				m.k8sLogDetailWasStreaming = false
				ns := m.k8sNamespaces[m.selectedK8sNamespace].Name
				var podNames []string
				if m.k8sPodLogFromDetail {
					podNames = []string{m.k8sPodDetail.Name}
				} else {
					for _, p := range m.filteredK8sPodList() {
						podNames = append(podNames, p.Name)
					}
				}
				return m, m.streamK8sPodLogs(ns, podNames)
			}
		}
		return m, nil
	}

	switch msg.String() {
	case "enter":
		filtered := m.filteredK8sPodLogs()
		if len(filtered) > 0 && m.k8sPodLogCursor < len(filtered) {
			m.showK8sLogDetail = true
			m.k8sLogDetailScroll = 0
			// Stop streaming to prevent log updates while viewing detail
			m.k8sLogDetailWasStreaming = m.k8sPodLogStreaming
			if m.k8sPodLogCancel != nil {
				m.k8sPodLogCancel()
			}
			m.k8sPodLogStreaming = false
		}
	case "up", "k":
		if m.k8sPodLogCursor > 0 {
			m.k8sPodLogCursor--
		}
	case "down", "j":
		filtered := m.filteredK8sPodLogs()
		if m.k8sPodLogCursor < len(filtered)-1 {
			m.k8sPodLogCursor++
		}
	case "G":
		filtered := m.filteredK8sPodLogs()
		if len(filtered) > 0 {
			m.k8sPodLogCursor = len(filtered) - 1
		}
	case "g":
		m.k8sPodLogCursor = 0
	case "s":
		if m.k8sPodLogStreaming {
			// Stop streaming
			if m.k8sPodLogCancel != nil {
				m.k8sPodLogCancel()
			}
			m.k8sPodLogStreaming = false
		} else {
			// Resume streaming
			var podNames []string
			for _, p := range m.k8sPodList {
				podNames = append(podNames, p.Name)
			}
			ns := m.k8sNamespaces[m.selectedK8sNamespace].Name
			return m, m.streamK8sPodLogs(ns, podNames)
		}
	case "d":
		// Cycle level filter: All → Info+ → Warn+ → Error+ → All
		m.k8sPodLogMinLevel = (m.k8sPodLogMinLevel + 1) % 4
		m.k8sPodLogCursor = 0
	case "c":
		// Toggle sidecar container logs
		if m.k8sPodLogCancel != nil {
			m.k8sPodLogCancel()
		}
		m.k8sPodLogs = nil
		m.k8sPodLogCursor = 0
		m.k8sPodLogStreaming = false
		m.k8sPodLogAllContainers = !m.k8sPodLogAllContainers
		var podNames []string
		for _, p := range m.k8sPodList {
			podNames = append(podNames, p.Name)
		}
		ns := m.k8sNamespaces[m.selectedK8sNamespace].Name
		return m, m.fetchK8sPodLogs(ns, podNames)
	case "esc":
		if m.filterActive {
			m.filterActive = false
			m.filterText = ""
			m.k8sPodLogCursor = 0
		} else {
			if m.k8sPodLogCancel != nil {
				m.k8sPodLogCancel()
			}
			m.k8sPodLogStreaming = false
			if m.k8sPodLogFromDetail {
				m.view = viewK8sPodDetail
				m.k8sPodLogFromDetail = false
			} else {
				m.view = viewK8sPodList
			}
			m.sortColumn = 0
			m.filterText = ""
			m.filterActive = false
		}
	case "q":
		if m.k8sPodLogCancel != nil {
			m.k8sPodLogCancel()
		}
		return m, tea.Quit
	}
	return m, nil
}

func (m Model) handleProbeListKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	filtered := m.filteredProbeItems()

	if m.filterActive {
		switch msg.String() {
		case "enter":
			m.filterActive = false
			m.probeCursor = 0
		case "esc":
			m.filterActive = false
			m.filterText = ""
			m.probeCursor = 0
		case "backspace":
			if len(m.filterText) > 0 {
				m.filterText = m.filterText[:len(m.filterText)-1]
				m.probeCursor = 0
			}
		default:
			if len(msg.String()) == 1 {
				m.filterText += msg.String()
				m.probeCursor = 0
			}
		}
		return m, nil
	}

	switch msg.String() {
	case "up", "k":
		if m.probeCursor > 0 {
			m.probeCursor--
		}
	case "down", "j":
		if m.probeCursor < len(filtered)-1 {
			m.probeCursor++
		}
	case "enter":
		if len(filtered) > 0 {
			m.selectedProbe = m.probeCursor
			m.probeDetailScroll = 0
			m.view = viewProbeDetail
		}
	case "esc":
		m.stopProbing()
		m.view = viewFleetPicker
	case "/":
		m.filterActive = true
		m.filterText = ""
	case "r":
		// Force re-probe: stop and restart
		m.stopProbing()
		return m, m.startProbing()
	case "1", "2", "3", "4", "5", "6":
		col := int(msg.String()[0] - '0')
		if col == m.sortColumn {
			m.sortAsc = !m.sortAsc
		} else {
			m.sortColumn = col
			m.sortAsc = true
		}
		m.sortProbeItems()
	case "q":
		m.stopProbing()
		return m, tea.Quit
	}
	return m, nil
}

func (m Model) handleProcessListKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	filtered := m.filteredProcesses()

	if m.filterActive {
		switch msg.String() {
		case "enter":
			m.filterActive = false
			m.processCursor = 0
		case "esc":
			m.filterActive = false
			m.filterText = ""
			m.processCursor = 0
		case "backspace":
			if len(m.filterText) > 0 {
				m.filterText = m.filterText[:len(m.filterText)-1]
				m.processCursor = 0
			}
		default:
			if len(msg.String()) == 1 {
				m.filterText += msg.String()
				m.processCursor = 0
			}
		}
		return m, nil
	}

	switch msg.String() {
	case "up", "k":
		if m.processCursor > 0 {
			m.processCursor--
		}
	case "down", "j":
		if m.processCursor < len(filtered)-1 {
			m.processCursor++
		}
	case "s":
		if len(filtered) > 0 {
			p := filtered[m.processCursor]
			return m.confirmProcessAction(p.Name, "start")
		}
	case "o":
		if len(filtered) > 0 {
			p := filtered[m.processCursor]
			return m.confirmProcessAction(p.Name, "stop")
		}
	case "t":
		if len(filtered) > 0 {
			p := filtered[m.processCursor]
			return m.confirmProcessAction(p.Name, "restart")
		}
	case "l":
		if len(filtered) > 0 {
			p := filtered[m.processCursor]
			basename := p.Name
			if idx := strings.LastIndex(p.Name, ":"); idx >= 0 {
				basename = p.Name[idx+1:]
			}
			cmd := fmt.Sprintf("sudo tail -n 200 -f /var/log/supervisor/%s.log", shellQuote(basename))
			return m, m.startSSHStream(SSHStreamConfig{
				Command:     cmd,
				Title:       "Processes › " + p.Name + " log",
				SourceName:  basename + "-log",
				ReturnView:  viewProcessList,
				HostIdx:     m.selectedHost,
				NewestFirst: true,
				AutoDone:    false,
			})
		}
	case "/":
		m.filterActive = true
		m.filterText = ""
	case "r":
		m.processes = nil
		showLoading(&m, "processes", "Loading processes...")
		return m, m.fetchProcesses()
	case "1", "2", "3", "4":
		col := int(msg.String()[0] - '0')
		if col == m.sortColumn {
			m.sortAsc = !m.sortAsc
		} else {
			m.sortColumn = col
			m.sortAsc = true
		}
		m.sortProcesses()
	case "esc":
		m.view = viewResourcePicker
	case "q":
		return m, tea.Quit
	}
	return m, nil
}

func (m Model) handleSSHStreamKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.streamNewestFirst && m.streamCursor > 0 {
			m.streamCursor--
		}
	case "down", "j":
		if m.streamNewestFirst && m.streamCursor < len(m.streamLines)-1 {
			m.streamCursor++
		}
	case " ":
		if m.streamNewestFirst {
			if m.streamStreaming {
				m.stopSSHStream()
			} else if m.streamLastConfig != nil {
				// Resume: restart the stream with the same config, keep existing lines
				cfg := *m.streamLastConfig
				existingLines := m.streamLines
				cmd := m.startSSHStream(cfg)
				m.streamLines = existingLines
				return m, cmd
			}
		}
	case "w":
		if len(m.streamLines) > 0 {
			f := m.fleets[m.selectedFleet]
			h := m.hosts[m.selectedHost]
			lw := newLogWriter(m.appCfg.FleetDir, f.Name, h.Entry.Name, m.streamSourceName)
			if lw != nil {
				lw.WriteLines(m.streamLines)
				m.flash = fmt.Sprintf("Saved %d lines to %s", len(m.streamLines), lw.Path())
				m.flashError = false
				lw.Close()
			} else {
				m.flash = "Failed to save log file"
				m.flashError = true
			}
		}
	case "G":
		if m.streamNewestFirst {
			m.streamCursor = 0
		}
	case "esc":
		m.stopSSHStream()
		retView := m.streamReturnView
		m.view = retView
		// Refresh the return view
		switch retView {
		case viewProcessList:
			return m, m.fetchProcesses()
		case viewLogFileList:
			return m, m.fetchLogFileStats()
		}
	case "q":
		m.stopSSHStream()
		return m, tea.Quit
	}
	return m, nil
}

func (m Model) handleLogFileListKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	filtered := m.filteredLogFiles()

	if m.filterActive {
		switch msg.String() {
		case "enter":
			m.filterActive = false
			m.logFileCursor = 0
		case "esc":
			m.filterActive = false
			m.filterText = ""
			m.logFileCursor = 0
		case "backspace":
			if len(m.filterText) > 0 {
				m.filterText = m.filterText[:len(m.filterText)-1]
				m.logFileCursor = 0
			}
		default:
			if len(msg.String()) == 1 {
				m.filterText += msg.String()
				m.logFileCursor = 0
			}
		}
		return m, nil
	}

	switch msg.String() {
	case "up", "k":
		if m.logFileCursor > 0 {
			m.logFileCursor--
		}
	case "down", "j":
		if m.logFileCursor < len(filtered)-1 {
			m.logFileCursor++
		}
	case "enter":
		if len(filtered) > 0 {
			item := filtered[m.logFileCursor]
			cmd := fmt.Sprintf("tail -n 200 -f %s", shellQuote(item.entry.Path))
			if item.entry.Sudo {
				cmd = "sudo " + cmd
			}
			return m, m.startSSHStream(SSHStreamConfig{
				Command:     cmd,
				Title:       "Logs › " + item.entry.Name,
				SourceName:  item.entry.Name,
				ReturnView:  viewLogFileList,
				HostIdx:     m.selectedHost,
				NewestFirst: true,
				AutoDone:    false,
			})
		}
	case "/":
		m.filterActive = true
		m.filterText = ""
	case "r":
		m.logFileStats = nil
		m.logFileStatDone = false
		showLoading(&m, "logfiles", "Loading log file stats...")
		return m, m.fetchLogFileStats()
	case "esc":
		m.view = viewResourcePicker
	case "q":
		return m, tea.Quit
	}
	return m, nil
}

func (m Model) handleProbeDetailKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.view = viewProbeList
	case "up", "k":
		if m.probeDetailScroll > 0 {
			m.probeDetailScroll--
		}
	case "down", "j":
		m.probeDetailScroll++
	case "r":
		// Force re-probe: stop and restart
		m.stopProbing()
		return m, m.startProbing()
	case "q":
		m.stopProbing()
		return m, tea.Quit
	}
	return m, nil
}
