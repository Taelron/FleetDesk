package app

import (
	"fmt"
	"strings"
)

// helpSection formats a group of keybinds with a header.
func helpSection(title string, binds [][]string) string {
	var b strings.Builder
	b.WriteString("  " + title + "\n")
	b.WriteString("  " + strings.Repeat("\u2500", 30) + "\n")
	for _, bind := range binds {
		fmt.Fprintf(&b, "  %-12s %s\n", bind[0], bind[1])
	}
	return b.String()
}

func globalHelp(hasFilter bool) string {
	binds := [][]string{
		{"Esc", "Back / close"},
		{"q", "Quit"},
		{"?", "Help"},
	}
	if hasFilter {
		binds = append([][]string{
			{"/", "Filter"},
		}, binds...)
	}
	return helpSection("Global", binds)
}

func helpForView(v view) string {
	switch v {
	case viewFleetPicker:
		return helpFleetPicker()
	case viewHostList:
		return helpHostList()
	case viewMetrics:
		return helpMetrics()
	case viewResourcePicker:
		return helpResourcePicker()
	case viewServiceList:
		return helpServiceList()
	case viewContainerList:
		return helpContainerList()
	case viewCronList:
		return helpCronList()
	case viewLogLevelPicker:
		return helpLogLevelPicker()
	case viewErrorLogList:
		return helpErrorLogList()
	case viewUpdateList:
		return helpUpdateList()
	case viewDiskList:
		return helpDiskList()
	case viewSubscription:
		return helpSubscription()
	case viewAccountList:
		return helpAccountList()
	case viewNetworkPicker:
		return helpNetworkPicker()
	case viewNetworkInterfaces:
		return helpNetworkInterfaces()
	case viewNetworkPorts:
		return helpNetworkPorts()
	case viewNetworkRoutes:
		return helpNetworkRoutes()
	case viewNetworkFirewall:
		return helpNetworkFirewall()
	case viewSecurityFailedLogins:
		return helpSecurityFailedLogins()
	case viewSecuritySudo:
		return helpSecuritySudo()
	case viewSecuritySELinux:
		return helpSecuritySELinux()
	case viewSecurityAudit:
		return helpSecurityAudit()
	case viewAzureSubList:
		return helpAzureSubList()
	case viewAzureResourcePicker:
		return helpAzureResourcePicker()
	case viewAzureVMList:
		return helpAzureVMList()
	case viewAzureVMDetail:
		return helpAzureVMDetail()
	case viewAzureAKSList:
		return helpAzureAKSList()
	case viewAzureAKSDetail:
		return helpAzureAKSDetail()
	case viewK8sClusterList:
		return helpK8sClusterList()
	case viewK8sContextList:
		return helpK8sContextList()
	case viewK8sResourcePicker:
		return helpK8sResourcePicker()
	case viewK8sNodeList:
		return helpK8sNodeList()
	case viewK8sNodeDetail:
		return helpK8sNodeDetail()
	case viewK8sNamespaceList:
		return helpK8sNamespaceList()
	case viewK8sWorkloadList:
		return helpK8sWorkloadList()
	case viewK8sPodList:
		return helpK8sPodList()
	case viewK8sPodDetail:
		return helpK8sPodDetail()
	case viewK8sPodLogs:
		return helpK8sPodLogs()
	case viewConfig:
		return helpConfig()
	default:
		return globalHelp(false)
	}
}

func helpFleetPicker() string {
	return helpSection("Navigation", [][]string{
		{"↑/↓ k/j", "Move cursor"},
		{"Enter", "Select fleet"},
	}) + "\n" + helpSection("Actions", [][]string{
		{"a", "About"},
		{"e", "Edit fleet file"},
		{"c", "Config"},
		{"r", "Reload fleets"},
	}) + "\n" + globalHelp(false)
}

func helpHostList() string {
	return helpSection("Navigation", [][]string{
		{"↑/↓ k/j", "Move cursor"},
		{"Enter", "Drill into host"},
	}) + "\n" + helpSection("Actions", [][]string{
		{"x", "Shell (SSH)"},
		{"K", "Deploy SSH key"},
		{"R", "Reboot host"},
		{"d", "Fleet metrics"},
		{"r", "Refresh"},
	}) + "\n" + globalHelp(false)
}

func helpMetrics() string {
	return helpSection("Navigation", [][]string{
		{"↑/↓ k/j", "Move cursor"},
		{"1-5", "Sort by column"},
	}) + "\n" + helpSection("Actions", [][]string{
		{"r", "Refresh"},
	}) + "\n" + globalHelp(false)
}

func helpResourcePicker() string {
	return helpSection("Navigation", [][]string{
		{"↑/↓ k/j", "Move cursor"},
		{"Enter", "Select resource"},
	}) + "\n" + helpSection("Actions", [][]string{
		{"r", "Refresh"},
	}) + "\n" + globalHelp(false)
}

func helpServiceList() string {
	return helpSection("List Mode", [][]string{
		{"↑/↓ k/j", "Move cursor"},
		{"Enter", "Open detail"},
		{"1-4", "Sort by column"},
		{"r", "Refresh"},
	}) + "\n" + helpSection("Detail Mode", [][]string{
		{"↑/↓ k/j", "Scroll logs"},
		{"s", "Start service"},
		{"o", "Stop service"},
		{"t", "Restart service"},
		{"r", "Refresh detail"},
	}) + "\n" + globalHelp(true)
}

func helpContainerList() string {
	return helpSection("List Mode", [][]string{
		{"↑/↓ k/j", "Move cursor"},
		{"Enter", "Open detail"},
		{"1-3", "Sort by column"},
		{"l", "Follow logs"},
		{"i", "Inspect"},
		{"e", "Exec shell"},
		{"r", "Refresh"},
	}) + "\n" + helpSection("Detail Mode", [][]string{
		{"↑/↓ k/j", "Scroll"},
	}) + "\n" + globalHelp(true)
}

func helpCronList() string {
	return helpSection("Navigation", [][]string{
		{"↑/↓ k/j", "Move cursor"},
		{"1-3", "Sort by column"},
	}) + "\n" + helpSection("Actions", [][]string{
		{"r", "Refresh"},
	}) + "\n" + globalHelp(true)
}

func helpLogLevelPicker() string {
	return helpSection("Navigation", [][]string{
		{"↑/↓ k/j", "Move cursor"},
		{"Enter", "Select level"},
	}) + "\n" + helpSection("Actions", [][]string{
		{"r", "Refresh"},
	}) + "\n" + globalHelp(false)
}

func helpErrorLogList() string {
	return helpSection("Navigation", [][]string{
		{"↑/↓ k/j", "Move cursor"},
		{"Enter", "View detail"},
		{"1-3", "Sort by column"},
	}) + "\n" + helpSection("Actions", [][]string{
		{"l", "Full log (less)"},
		{"r", "Refresh"},
	}) + "\n" + globalHelp(true)
}

func helpUpdateList() string {
	return helpSection("List Mode", [][]string{
		{"↑/↓ k/j", "Move cursor"},
		{"Enter", "View detail"},
		{"1-3", "Sort by column"},
		{"u", "Apply ALL updates"},
		{"p", "Apply security updates"},
		{"r", "Refresh"},
	}) + "\n" + helpSection("Detail Mode", [][]string{
		{"↑/↓ k/j", "Scroll"},
	}) + "\n" + globalHelp(true)
}

func helpDiskList() string {
	return helpSection("List Mode", [][]string{
		{"↑/↓ k/j", "Move cursor"},
		{"Enter", "View detail"},
		{"1-6", "Sort by column"},
		{"r", "Refresh"},
	}) + "\n" + helpSection("Detail Mode", [][]string{
		{"↑/↓ k/j", "Scroll"},
	}) + "\n" + globalHelp(true)
}

func helpSubscription() string {
	return helpSection("Navigation", [][]string{
		{"↑/↓ k/j", "Move cursor"},
	}) + "\n" + helpSection("Actions", [][]string{
		{"u", "Unregister"},
		{"g", "Register"},
		{"c", "Check repo (run dnf makecache --refresh on selected repo)"},
		{"d", "Disable repo"},
		{"r", "Refresh"},
	}) + "\n" + globalHelp(false)
}

func helpAccountList() string {
	return helpSection("Navigation", [][]string{
		{"↑/↓ k/j", "Move cursor"},
		{"Enter", "View detail"},
		{"1-5", "Sort by column"},
	}) + "\n" + helpSection("Actions", [][]string{
		{"r", "Refresh"},
	}) + "\n" + globalHelp(true)
}

func helpNetworkPicker() string {
	return helpSection("Navigation", [][]string{
		{"↑/↓ k/j", "Move cursor"},
		{"Enter", "Select sub-view"},
	}) + "\n" + helpSection("Actions", [][]string{
		{"r", "Refresh"},
	}) + "\n" + globalHelp(false)
}

func helpNetworkInterfaces() string {
	return helpSection("Navigation", [][]string{
		{"↑/↓ k/j", "Move cursor"},
		{"1-4", "Sort by column"},
	}) + "\n" + helpSection("Actions", [][]string{
		{"r", "Refresh"},
	}) + "\n" + globalHelp(true)
}

func helpNetworkPorts() string {
	return helpSection("Navigation", [][]string{
		{"↑/↓ k/j", "Move cursor"},
		{"1-4", "Sort by column"},
	}) + "\n" + helpSection("Actions", [][]string{
		{"r", "Refresh"},
	}) + "\n" + globalHelp(true)
}

func helpNetworkRoutes() string {
	return helpSection("Navigation", [][]string{
		{"↑/↓ k/j", "Move cursor"},
		{"1-4", "Sort by column"},
	}) + "\n" + helpSection("Actions", [][]string{
		{"r", "Refresh"},
	}) + "\n" + globalHelp(true)
}

func helpNetworkFirewall() string {
	return helpSection("Navigation", [][]string{
		{"↑/↓ k/j", "Move cursor"},
		{"1-5", "Sort by column"},
	}) + "\n" + helpSection("Actions", [][]string{
		{"r", "Refresh"},
	}) + "\n" + globalHelp(true)
}

func helpSecurityFailedLogins() string {
	return helpSection("Navigation", [][]string{
		{"↑/↓ k/j", "Move cursor"},
		{"1-4", "Sort by column"},
	}) + "\n" + helpSection("Actions", [][]string{
		{"r", "Refresh"},
	}) + "\n" + globalHelp(true)
}

func helpSecuritySudo() string {
	return helpSection("Navigation", [][]string{
		{"↑/↓ k/j", "Move cursor"},
		{"1-4", "Sort by column"},
	}) + "\n" + helpSection("Actions", [][]string{
		{"r", "Refresh"},
	}) + "\n" + globalHelp(true)
}

func helpSecuritySELinux() string {
	return helpSection("Navigation", [][]string{
		{"↑/↓ k/j", "Move cursor"},
		{"1-5", "Sort by column"},
	}) + "\n" + helpSection("Actions", [][]string{
		{"r", "Refresh"},
	}) + "\n" + globalHelp(true)
}

func helpSecurityAudit() string {
	return helpSection("Navigation", [][]string{
		{"↑/↓ k/j", "Move cursor"},
		{"1-5", "Sort by column"},
	}) + "\n" + helpSection("Actions", [][]string{
		{"r", "Refresh"},
	}) + "\n" + globalHelp(true)
}

func helpAzureSubList() string {
	return helpSection("Navigation", [][]string{
		{"↑/↓ k/j", "Move cursor"},
		{"Enter", "Select subscription"},
		{"1-3", "Sort by column"},
	}) + "\n" + helpSection("Actions", [][]string{
		{"r", "Refresh"},
	}) + "\n" + globalHelp(true)
}

func helpAzureResourcePicker() string {
	return helpSection("Navigation", [][]string{
		{"↑/↓ k/j", "Move cursor"},
		{"Enter", "Select resource type"},
	}) + "\n" + helpSection("Actions", [][]string{
		{"r", "Refresh"},
	}) + "\n" + globalHelp(false)
}

func helpAzureVMList() string {
	return helpSection("Navigation", [][]string{
		{"↑/↓ k/j", "Move cursor"},
		{"Enter", "View detail"},
		{"1-7", "Sort by column"},
	}) + "\n" + helpSection("Actions", [][]string{
		{"s", "Start VM"},
		{"o", "Deallocate VM"},
		{"t", "Restart VM"},
		{"r", "Refresh"},
	}) + "\n" + globalHelp(true)
}

func helpAzureVMDetail() string {
	return helpSection("Navigation", [][]string{
		{"↑/↓ k/j", "Scroll"},
	}) + "\n" + helpSection("Actions", [][]string{
		{"a", "Refresh activity log"},
		{"r", "Refresh all"},
	}) + "\n" + globalHelp(false)
}

func helpAzureAKSList() string {
	return helpSection("Navigation", [][]string{
		{"↑/↓ k/j", "Move cursor"},
		{"Enter", "View detail"},
		{"1-9", "Sort by column"},
	}) + "\n" + helpSection("Actions", [][]string{
		{"s", "Start cluster"},
		{"o", "Stop cluster"},
		{"d", "Delete cluster"},
		{"r", "Refresh"},
	}) + "\n" + globalHelp(true)
}

func helpAzureAKSDetail() string {
	return helpSection("Navigation", [][]string{
		{"↑/↓ k/j", "Scroll activity log"},
	}) + "\n" + helpSection("Actions", [][]string{
		{"a", "Refresh activity log"},
	}) + "\n" + globalHelp(false)
}

func helpK8sClusterList() string {
	return helpSection("Navigation", [][]string{
		{"↑/↓ k/j", "Move cursor"},
		{"Enter", "Select cluster"},
		{"1-3", "Sort by column"},
	}) + "\n" + helpSection("Actions", [][]string{
		{"r", "Refresh"},
	}) + "\n" + globalHelp(true)
}

func helpK8sContextList() string {
	return helpSection("Navigation", [][]string{
		{"↑/↓ k/j", "Move cursor"},
		{"Enter", "Select context"},
	}) + "\n" + helpSection("Actions", [][]string{
		{"d", "Delete context"},
		{"r", "Refresh"},
	}) + "\n" + globalHelp(true)
}

func helpK8sResourcePicker() string {
	return helpSection("Navigation", [][]string{
		{"↑/↓ k/j", "Move cursor"},
		{"Enter", "Select resource type"},
	}) + "\n" + helpSection("Actions", [][]string{
		{"r", "Refresh"},
	}) + "\n" + globalHelp(false)
}

func helpK8sNodeList() string {
	return helpSection("Navigation", [][]string{
		{"↑/↓ k/j", "Move cursor"},
		{"Enter", "View detail"},
		{"1-9", "Sort by column"},
	}) + "\n" + helpSection("Actions", [][]string{
		{"r", "Refresh"},
	}) + "\n" + globalHelp(true)
}

func helpK8sNodeDetail() string {
	return helpSection("Navigation", [][]string{
		{"↑/↓ k/j", "Move cursor"},
		{"1-9", "Sort pods by column"},
	}) + "\n" + helpSection("Actions", [][]string{
		{"r", "Refresh"},
	}) + "\n" + globalHelp(true)
}

func helpK8sNamespaceList() string {
	return helpSection("Navigation", [][]string{
		{"↑/↓ k/j", "Move cursor"},
		{"Enter", "Select namespace"},
		{"1-7", "Sort by column"},
	}) + "\n" + helpSection("Actions", [][]string{
		{"r", "Refresh"},
	}) + "\n" + globalHelp(true)
}

func helpK8sWorkloadList() string {
	return helpSection("Navigation", [][]string{
		{"↑/↓ k/j", "Move cursor"},
		{"Enter", "Select workload"},
		{"1-3", "Sort by column"},
	}) + "\n" + helpSection("Actions", [][]string{
		{"r", "Refresh"},
	}) + "\n" + globalHelp(true)
}

func helpK8sPodList() string {
	return helpSection("Navigation", [][]string{
		{"↑/↓ k/j", "Move cursor"},
		{"Enter", "View pod detail"},
		{"1-6", "Sort by column"},
	}) + "\n" + helpSection("Actions", [][]string{
		{"l", "View logs (workload)"},
		{"d", "Delete pod"},
		{"r", "Refresh"},
	}) + "\n" + globalHelp(true)
}

func helpK8sPodDetail() string {
	return helpSection("Navigation", [][]string{
		{"↑/↓ k/j", "Move cursor"},
		{"g", "Go to top"},
	}) + "\n" + helpSection("Actions", [][]string{
		{"l", "View logs (pod)"},
	}) + "\n" + globalHelp(false)
}

func helpK8sPodLogs() string {
	return helpSection("List Mode", [][]string{
		{"↑/↓ k/j", "Move cursor"},
		{"G", "Go to bottom"},
		{"g", "Go to top"},
		{"Enter", "View log detail"},
	}) + "\n" + helpSection("Actions", [][]string{
		{"s", "Start/stop streaming"},
		{"d", "Cycle level filter"},
		{"c", "Toggle sidecar logs"},
	}) + "\n" + helpSection("Detail Mode", [][]string{
		{"↑/↓ k/j", "Scroll"},
		{"g", "Go to top"},
	}) + "\n" + globalHelp(false)
}

func helpConfig() string {
	return helpSection("Actions", [][]string{
		{"e", "Edit config"},
		{"r", "Reload"},
	}) + "\n" + globalHelp(false)
}
