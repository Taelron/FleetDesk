package app

import (
	"github.com/Gaetan-Jaminon/fleetdesk/internal/notes"
)

// isNoteableView reports whether the `n` key should open the Note List for
// the currently displayed view. Only resource list views that carry a
// well-defined selected resource support notes.
func isNoteableView(v view) bool {
	switch v {
	case viewFleetPicker,
		viewHostList,
		viewServiceList,
		viewContainerList,
		viewAzureSubList,
		viewAzureVMList,
		viewAzureAKSList,
		viewK8sClusterList,
		viewK8sNamespaceList,
		viewK8sWorkloadList,
		viewK8sPodList:
		return true
	}
	return false
}

// currentNoteRef returns the ResourceRef for the currently selected item in
// the current view. Returns (_, false) when no ref can be produced (e.g.
// view has no items, or cursor is out of range).
func (m Model) currentNoteRef() (notes.ResourceRef, bool) {
	switch m.view {
	case viewFleetPicker:
		if m.fleetCursor >= len(m.fleets) {
			return notes.ResourceRef{}, false
		}
		return notes.ResourceRef{Fleet: m.fleets[m.fleetCursor].Name}, true

	case viewHostList:
		if m.hostCursor >= len(m.hosts) {
			return notes.ResourceRef{}, false
		}
		return notes.ResourceRef{
			Fleet:    m.fleets[m.selectedFleet].Name,
			Segments: []string{"hosts", m.hosts[m.hostCursor].Entry.Name},
		}, true

	case viewServiceList:
		svcs := m.filteredServices()
		if m.serviceCursor >= len(svcs) {
			return notes.ResourceRef{}, false
		}
		return notes.ResourceRef{
			Fleet: m.fleets[m.selectedFleet].Name,
			Segments: []string{
				"hosts", m.hosts[m.selectedHost].Entry.Name,
				"services", svcs[m.serviceCursor].Name,
			},
		}, true

	case viewContainerList:
		cts := m.filteredContainers()
		if m.containerCursor >= len(cts) {
			return notes.ResourceRef{}, false
		}
		return notes.ResourceRef{
			Fleet: m.fleets[m.selectedFleet].Name,
			Segments: []string{
				"hosts", m.hosts[m.selectedHost].Entry.Name,
				"containers", cts[m.containerCursor].Name,
			},
		}, true

	case viewAzureSubList:
		if m.azureSubCursor >= len(m.azureSubs) {
			return notes.ResourceRef{}, false
		}
		return notes.ResourceRef{
			Fleet:    m.fleets[m.selectedFleet].Name,
			Segments: []string{"azure", m.azureSubs[m.azureSubCursor].Name},
		}, true

	case viewAzureVMList:
		vms := m.filteredAzureVMs()
		if m.azureVMCursor >= len(vms) {
			return notes.ResourceRef{}, false
		}
		vm := vms[m.azureVMCursor]
		return notes.ResourceRef{
			Fleet: m.fleets[m.selectedFleet].Name,
			Segments: []string{
				"azure", m.azureSubs[m.selectedAzureSub].Name,
				vm.ResourceGroup, "vm", vm.Name,
			},
		}, true

	case viewAzureAKSList:
		if m.azureAKSCursor >= len(m.azureAKSClusters) {
			return notes.ResourceRef{}, false
		}
		aks := m.azureAKSClusters[m.azureAKSCursor]
		return notes.ResourceRef{
			Fleet: m.fleets[m.selectedFleet].Name,
			Segments: []string{
				"azure", m.azureSubs[m.selectedAzureSub].Name,
				aks.ResourceGroup, "aks", aks.Name,
			},
		}, true

	case viewK8sClusterList:
		if m.k8sClusterCursor >= len(m.k8sClusters) {
			return notes.ResourceRef{}, false
		}
		return notes.ResourceRef{
			Fleet:    m.fleets[m.selectedFleet].Name,
			Segments: []string{"k8s", m.k8sClusters[m.k8sClusterCursor].Name},
		}, true

	case viewK8sNamespaceList:
		nss := m.filteredK8sNamespaces()
		if m.k8sNamespaceCursor >= len(nss) {
			return notes.ResourceRef{}, false
		}
		return notes.ResourceRef{
			Fleet: m.fleets[m.selectedFleet].Name,
			Segments: []string{
				"k8s", m.k8sClusters[m.selectedK8sCluster].Name,
				m.selectedK8sContext, nss[m.k8sNamespaceCursor].Name,
			},
		}, true

	case viewK8sWorkloadList:
		wls := m.filteredK8sWorkloads()
		if m.k8sWorkloadCursor >= len(wls) {
			return notes.ResourceRef{}, false
		}
		return notes.ResourceRef{
			Fleet: m.fleets[m.selectedFleet].Name,
			Segments: []string{
				"k8s", m.k8sClusters[m.selectedK8sCluster].Name,
				m.selectedK8sContext,
				m.k8sNamespaces[m.selectedK8sNamespace].Name,
				wls[m.k8sWorkloadCursor].Name,
			},
		}, true

	case viewK8sPodList:
		pods := m.filteredK8sPodList()
		if m.k8sPodCursor >= len(pods) {
			return notes.ResourceRef{}, false
		}
		return notes.ResourceRef{
			Fleet: m.fleets[m.selectedFleet].Name,
			Segments: []string{
				"k8s", m.k8sClusters[m.selectedK8sCluster].Name,
				m.selectedK8sContext,
				m.k8sNamespaces[m.selectedK8sNamespace].Name,
				"pods", pods[m.k8sPodCursor].Name,
			},
		}, true
	}
	return notes.ResourceRef{}, false
}

// refsInView returns one ResourceRef per visible item in the current view.
// Used by FLE-79 to batch-load note counts on view entry. Returns nil if the
// view does not support notes.
func (m Model) refsInView() []notes.ResourceRef {
	if !isNoteableView(m.view) {
		return nil
	}
	fleet := func() string {
		if m.selectedFleet < len(m.fleets) {
			return m.fleets[m.selectedFleet].Name
		}
		return ""
	}

	switch m.view {
	case viewFleetPicker:
		refs := make([]notes.ResourceRef, 0, len(m.fleets))
		for _, f := range m.fleets {
			refs = append(refs, notes.ResourceRef{Fleet: f.Name})
		}
		return refs

	case viewHostList:
		refs := make([]notes.ResourceRef, 0, len(m.hosts))
		for _, h := range m.hosts {
			refs = append(refs, notes.ResourceRef{
				Fleet:    fleet(),
				Segments: []string{"hosts", h.Entry.Name},
			})
		}
		return refs

	case viewServiceList:
		svcs := m.filteredServices()
		refs := make([]notes.ResourceRef, 0, len(svcs))
		hostName := m.hosts[m.selectedHost].Entry.Name
		for _, svc := range svcs {
			refs = append(refs, notes.ResourceRef{
				Fleet:    fleet(),
				Segments: []string{"hosts", hostName, "services", svc.Name},
			})
		}
		return refs

	case viewContainerList:
		cts := m.filteredContainers()
		refs := make([]notes.ResourceRef, 0, len(cts))
		hostName := m.hosts[m.selectedHost].Entry.Name
		for _, c := range cts {
			refs = append(refs, notes.ResourceRef{
				Fleet:    fleet(),
				Segments: []string{"hosts", hostName, "containers", c.Name},
			})
		}
		return refs

	case viewAzureSubList:
		refs := make([]notes.ResourceRef, 0, len(m.azureSubs))
		for _, sub := range m.azureSubs {
			refs = append(refs, notes.ResourceRef{
				Fleet:    fleet(),
				Segments: []string{"azure", sub.Name},
			})
		}
		return refs

	case viewAzureVMList:
		vms := m.filteredAzureVMs()
		refs := make([]notes.ResourceRef, 0, len(vms))
		subName := m.azureSubs[m.selectedAzureSub].Name
		for _, vm := range vms {
			refs = append(refs, notes.ResourceRef{
				Fleet:    fleet(),
				Segments: []string{"azure", subName, vm.ResourceGroup, "vm", vm.Name},
			})
		}
		return refs

	case viewAzureAKSList:
		refs := make([]notes.ResourceRef, 0, len(m.azureAKSClusters))
		subName := m.azureSubs[m.selectedAzureSub].Name
		for _, aks := range m.azureAKSClusters {
			refs = append(refs, notes.ResourceRef{
				Fleet:    fleet(),
				Segments: []string{"azure", subName, aks.ResourceGroup, "aks", aks.Name},
			})
		}
		return refs

	case viewK8sClusterList:
		refs := make([]notes.ResourceRef, 0, len(m.k8sClusters))
		for _, c := range m.k8sClusters {
			refs = append(refs, notes.ResourceRef{
				Fleet:    fleet(),
				Segments: []string{"k8s", c.Name},
			})
		}
		return refs

	case viewK8sNamespaceList:
		nss := m.filteredK8sNamespaces()
		refs := make([]notes.ResourceRef, 0, len(nss))
		clusterName := m.k8sClusters[m.selectedK8sCluster].Name
		for _, ns := range nss {
			refs = append(refs, notes.ResourceRef{
				Fleet:    fleet(),
				Segments: []string{"k8s", clusterName, m.selectedK8sContext, ns.Name},
			})
		}
		return refs

	case viewK8sWorkloadList:
		wls := m.filteredK8sWorkloads()
		refs := make([]notes.ResourceRef, 0, len(wls))
		clusterName := m.k8sClusters[m.selectedK8sCluster].Name
		nsName := m.k8sNamespaces[m.selectedK8sNamespace].Name
		for _, wl := range wls {
			refs = append(refs, notes.ResourceRef{
				Fleet:    fleet(),
				Segments: []string{"k8s", clusterName, m.selectedK8sContext, nsName, wl.Name},
			})
		}
		return refs

	case viewK8sPodList:
		pods := m.filteredK8sPodList()
		refs := make([]notes.ResourceRef, 0, len(pods))
		clusterName := m.k8sClusters[m.selectedK8sCluster].Name
		nsName := m.k8sNamespaces[m.selectedK8sNamespace].Name
		for _, p := range pods {
			refs = append(refs, notes.ResourceRef{
				Fleet:    fleet(),
				Segments: []string{"k8s", clusterName, m.selectedK8sContext, nsName, "pods", p.Name},
			})
		}
		return refs
	}
	return nil
}
