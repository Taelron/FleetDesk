# FleetDesk — behaviour reconnaissance

**Input to TAE-33 (SPEC rewrite). Not a specification.**

| | |
|---|---|
| Base commit | `eb4a287` (`main`) |
| Branch | `feature/tae-33-spec-recon` |
| Method | Read-only source inspection. No binary was run, no host contacted. |
| Scope | `main.go`, `internal/{app,config,ssh,azure,k8s,probes,notes,logging,fspath}`, `Makefile`, `README.md`, `.goreleaser.yaml`, `.github/workflows/` |

This document records **what the code on `main` does today**. It makes no claim
about what it should do. Where the code and an existing document disagree, both
are stated and the disagreement is flagged; deciding which is right is not this
document's job.

Everything here was read off the source. Section 11 lists what could not be
determined even after targeted investigation — those are stated as unknown
rather than omitted, because an absent claim is recoverable and a confident
wrong one is not.

---

## 1. Views

### 1.1 The view enum

`internal/app/model.go:207-253` declares 45 view constants. All 45 have a render
function (`renderCurrentView`) and a key handler (`handleKey`). Seven additional
*detail modes* exist as boolean flags on the model rather than as view constants,
so the true count of distinct screens a user can reach is 52.

The flag-driven detail modes are: `showServiceDetail`, `showContainerDetail`,
`showUpdateDetail`, `showDiskDetail`, `showAccountDetail`, `showLogDetail`
(global, Error Log List only) and `showK8sLogDetail`.

### 1.2 Navigation graph

```
                      ┌──────────────────────────┐
  (FleetDir == "") ──>│ First-Run Wizard (modal) │──> writes config.yaml
                      └──────────────────────────┘
                                   │
                                   v
        ┌──────────────────── Fleet Picker ─────────────────────┐
        │   a → About modal     e → $EDITOR(fleet file)         │
        │   c → Config view     r → rescan fleet_dir            │
        │   n → Note List                                       │
        └───┬──────────┬──────────────┬────────────┬────────────┘
   type:vm  │  type:azure          type:k8s     type:probes
            v          v              v            v
       Host List   Azure Sub    K8s Cluster    Probe List
                     List           List            │
                                                    v
                                              Probe Detail

Config view ── e ──> $EDITOR(~/.config/fleetdesk/config.yaml)
            ── r ──> reload → Fleet Picker
            ── Esc ─> Fleet Picker
```

**VM branch**

```
Host List
 ├ Enter ─(sudo probe; may open Sudo Wizard)─> Resource Picker
 ├ d ──> Metrics Dashboard ──Esc──> Host List
 ├ x ──> terminal handover: interactive ssh shell
 ├ K ──> Confirm modal ──> terminal handover: chown $HOME/.ssh + ssh-copy-id
 ├ R ──> Confirm modal ──> terminal handover: sudo reboot
 ├ c ──> Command Wizard (2 or 3 steps) ──> SSH Stream view
 ├ n ──> Note List
 └ Esc ─> Fleet Picker (closes all SSH connections)

Resource Picker  (rows are dynamic — see 1.4)
 ├ Services ─────────> Service List ──Enter──> Service Detail (flag)
 ├ Processes* ───────> Process List  (s/o/t → Confirm → SSH Stream; l → SSH Stream)
 ├ Containers ───────> Container List ──Enter──> Container Detail (flag)
 │                                     l/i/e → terminal handover (podman)
 ├ Cron Jobs ────────> Cron List (leaf)
 ├ System Logs ──────> Log Level Picker ──Enter──> Error Log List
 │                                                  ├ Enter → Log Detail (flag)
 │                                                  └ l → terminal handover (journalctl|less)
 ├ Updates ──────────> Update List ──Enter──> Update Detail (flag)
 │                                   u / p → Options-Confirm modal → terminal handover
 ├ Disk ─────────────> Disk List ──Enter──> Disk Detail (flag)
 ├ Subscription ─────> Subscription view (leaf)
 │                       u / g / d → Confirm modal → terminal handover
 │                       c → SSH Stream (dnf makecache on selected repo)
 ├ Accounts ─────────> Account List ──Enter──> Account Detail (flag)
 ├ Network ──────────> Network Picker ──> Interfaces | Ports | Routes & DNS | Firewall
 ├ Failed Logins ────> leaf
 ├ Sudo Activity ────> leaf
 ├ SELinux Denials ──> leaf
 ├ Audit Summary ────> leaf
 └ Logs* ────────────> Log File List ──Enter──> SSH Stream (tail -n 200 -f)

 * conditional rows — see 1.4
```

**Azure branch**

```
Azure Subscription List ──Enter──> Azure Resource Picker
                                     ├─ VMs ──> Azure VM List ──Enter──> Azure VM Detail
                                     └─ AKS ──> Azure AKS List ─Enter──> Azure AKS Detail
```

**Kubernetes branch**

```
K8s Cluster List ──Enter──> K8s Context List ──Enter──> K8s Resource Picker
                     (auto-skipped when the cluster        ├─ Workloads (namespaces) ──> Namespace List
                      matches exactly one context)         │     └─Enter─> Workload List ─Enter─> Pod List
                                                           │                    ├ Enter → Pod Detail ─l─> Pod Logs
                                                           │                    └ l     → Pod Logs ─Enter─> Log Detail (flag)
                                                           ├─ Nodes ──> Node List ─Enter─> Node Detail
                                                           └─ ArgoCD Apps → flash only, no view
```

Pod Logs records whether it was opened from the Pod List or the Pod Detail
(`k8sPodLogFromDetail`) and Esc returns to the correct one.

**Notes overlay**

```
any noteable view ──n──> Note List ──Enter──> Note Read ──Esc──> Note List
                            ├ n → create empty file → $EDITOR → reload
                            ├ e → $EDITOR on selected note
                            ├ d → Confirm modal → delete
                            └ Esc → m.previousView
```

Noteable views (`noteref.go:isNoteableView`, 11): Fleet Picker, Host List,
Service List, Container List, Azure Sub List, Azure VM List, Azure AKS List,
K8s Cluster List, K8s Namespace List, K8s Workload List, K8s Pod List.

### 1.3 Every view: what it lists and its columns

Columns marked *(N)* carry a `▲/▼` sort indicator for sort key N. Section 9
audits whether each key actually sorts the column it is printed on.

| View | Reached from | Lists | Columns |
|---|---|---|---|
| **Fleet Picker** | startup | fleet files in `fleet_dir` | FLEET, TYPE (w6, `kubernetes`→`k8s`), TARGETS. Group headers per type. 📝 prefix when notes exist |
| **Config** | Fleet Picker `c` | 2 fixed settings | SETTING, VALUE — `Fleet directory`, `Editor` |
| **Host List** | Fleet Picker Enter (vm) | grouped then ungrouped hosts | HOST, OS, UP SINCE, UPD (`—` when 0). Group headers. Row override `connecting...` / `unreachable (reason)`. 📝 |
| **Metrics** | Host List `d` | all hosts | HOST *(1)*, CPU% *(2)*, MEM% *(3)*, DISK% *(4)*, LOAD *(5)*, UPTIME. Offline `—`, failed fetch `err`. Group headers only when unsorted |
| **Resource Picker** | Host List Enter | dynamic rows (1.4) | RESOURCE, TOTAL, RUNNING, FAILED |
| **Service List** | Resource Picker | systemd units filtered by `service_filter` | SERVICE *(1)*, STATE *(2, w10)*, ENABLED *(3)*, DESCRIPTION *(4)*. 📝 |
| **Service Detail** (flag) | Service List Enter | key/value + last 50 journal lines | Unit, Description, Active, PID, Memory, Tasks, Since, Enabled |
| **Container List** | Resource Picker | `podman ps -a` | CONTAINER *(1)*, IMAGE *(2)*, STATUS *(3)*. 📝 |
| **Container Detail** (flag) | Container List Enter | flat scrollable lines | `--- Details ---` (ID, Image, Status, Created, Command), `--- Ports ---`, `--- Mounts ---`, `--- Environment ---` |
| **Cron List** | Resource Picker | user crontab + `/etc/cron.d/*` | SCHEDULE *(1)*, SOURCE *(2)*, COMMAND *(3)* |
| **Log Level Picker** | Resource Picker | 7 fixed syslog priorities | LEVEL, COUNT — Emergency(0) … Info(6) |
| **Error Log List** | Log Level Picker Enter | journal entries, max 500 | TIME *(1)*, UNIT *(2)*, MESSAGE *(3)* |
| **Log Detail** (global flag) | Error Log List Enter | one entry, parsed `key=value` | `Time:` / `Unit:` header then FIELD/VALUE; `level=error/crit`, `err`, `error` coloured red |
| **Update List** | Resource Picker | `dnf check-update` | PACKAGE *(1)*, VERSION *(2)*, TYPE *(3)* — TYPE values are a closed set, see 10.3 |
| **Update Detail** (flag) | Update List Enter | raw `dnf info <pkg>` | scrollable, no columns |
| **Disk List** | Resource Picker | `df -h` minus tmpfs/devtmpfs | FILESYSTEM *(1)*, SIZE *(2)*, USED *(3)*, AVAIL *(4)*, USE% *(5)*, MOUNT *(6)* |
| **Disk Detail** (flag) | Disk List Enter | `df -h <mount>` + `tune2fs -l` / `lsblk -f` | scrollable |
| **Subscription** | Resource Picker | `subscription-manager` key/values + one row per enabled repo | FIELD, VALUE. Repo rows `Repo: <id>`; `ERROR` values red |
| **Account List** | Resource Picker | users UID ≥ 1000, ≠ 65534, plus `/home/*` owners | USER *(1)*, UID *(2)*, GROUPS *(3)*, SHELL *(4)* |
| **Account Detail** (flag) | Account List Enter | sectioned key/value | from `id`, `chage -l`, `lastlog`, `sudo -l -U` |
| **Network Picker** | Resource Picker | 4 fixed rows | RESOURCE, COUNT, STATUS — Interfaces (`N UP`), Ports (`—`), Routes (`—`), Firewall (backend) |
| **Network Interfaces** | Network Picker | `ip -br addr` | INTERFACE *(1)*, STATE *(2)*, IP ADDRESS *(3)*, MTU *(4)* |
| **Network Ports** | Network Picker | `ss -tlnp` | PORT *(1)*, PROTOCOL *(2)*, PROCESS *(3)*, BIND ADDRESS *(4)* |
| **Network Routes** | Network Picker | `ip route` + `/etc/resolv.conf` | DESTINATION *(1)*, GATEWAY *(2)*, INTERFACE *(3)*, METRIC *(4)*. Above the table, a cyan `DNS: <nameservers, comma-joined>  Search: <search>` line (`—` when no nameservers; the `Search:` half is omitted when empty) |
| **Network Firewall** | Network Picker | firewalld zones, else nftables, else iptables | ZONE *(1)*, SERVICE/PORT *(2)*, PROTOCOL *(3)*, SOURCE *(4)*, ACTION *(5)* |
| **Failed Logins** | Resource Picker | `journalctl -u sshd` grep Failed/Invalid user, 500 | TIME *(1)*, USER *(2)*, SOURCE *(3)*, METHOD *(4)* |
| **Sudo Activity** | Resource Picker | `journalctl _COMM=sudo`, 500 | TIME *(1)*, USER *(2)*, RESULT *(3)*, COMMAND *(4)* |
| **SELinux Denials** | Resource Picker | `journalctl _TRANSPORT=audit` grep `avc:` | TIME *(1)*, ACTION *(2)*, SOURCE *(3)*, TARGET *(4)*, CLASS *(5)* |
| **Audit Summary** | Resource Picker | `aureport --auth -i` | TIME *(1)*, USER *(2)*, RESULT *(3)*, MESSAGE *(4)* |
| **Process List** | Resource Picker (if `supervisorctl` present) | `supervisorctl status` | PROCESS *(1)*, STATE *(2)*, UPTIME *(3)*, PID *(4)* |
| **Log File List** | Resource Picker (if host has `logs:`) | configured paths + stat | NAME *(1)*, PATH *(2)*, SIZE *(3)*, LAST MODIFIED *(4)* — indicators rendered, no keys bound (9.2) |
| **SSH Stream** | Log File List, Process List, Subscription `c`, Command Wizard | streamed stdout+stderr | no columns. Status bar `● LIVE` / `■ STOPPED`, `↑ AUTO-SCROLL`. Cap 1000 newest-first / 5000 append |
| **Azure Sub List** | Fleet Picker Enter (azure) | `groups[].name` as subscription names | SUBSCRIPTION *(1)*, TENANT *(2)*, USER *(3)*. Row override `checking...` / `error (reason)`. 📝 |
| **Azure Resource Picker** | Azure Sub List Enter | 2 fixed rows | RESOURCE, TOTAL — VMs, AKS Clusters |
| **Azure VM List** | Azure Resource Picker | Resource Graph + `az vm list -d` | NAME *(1)*, RESOURCE GROUP *(2)*, STATUS *(3)*, SIZE *(4)*, OS *(5)*, PRIVATE IP *(6)*, HOSTNAME *(7)*. 📝 |
| **Azure VM Detail** | Azure VM List Enter | key/value + activity log | Name, Hostname, Resource Group, Location, Status, Size, OS Type, OS Disk, Private IP, Public IP, VNet, Subnet, OS Disk Name, OS Disk Size, NIC, Created, Tags; then `── Recent Activity (Resource Group, last Nh) ──` TIME, OPERATION, RESOURCE, STATUS, CALLER |
| **Azure AKS List** | Azure Resource Picker | Resource Graph managedClusters | NAME *(1)*, RESOURCE GROUP *(2)*, STATUS *(3)*, PROVISIONING *(4)*, K8S VERSION *(5)*, NODES *(6)*, POOLS *(7)*, CREATED *(8)*, then one column per `display_tags` entry, keys 9…8+N |
| **Azure AKS Detail** | Azure AKS List Enter | key/value + node pools + activity log | Name, Resource Group, Location, Status, Provisioning, Created, K8s Version, Network Plugin, Total Nodes, `Tag: <t>` rows. Pools: POOL, MODE, VM SIZE, NODES, MIN, MAX, VERSION, AUTOSCALE |
| **K8s Cluster List** | Fleet Picker Enter (kubernetes) | `groups[].name` as cluster names | CLUSTER *(1)*, K8S VERSION *(2)*. Row override `checking...` / `unavailable`. 📝 |
| **K8s Context List** | K8s Cluster List Enter | `kubectl config get-contexts` matches | CONTEXT *(1)*, USER *(2)* — indicators rendered, no keys bound (9.2) |
| **K8s Resource Picker** | K8s Context List Enter | 3 fixed rows | RESOURCE, TOTAL — Workloads (namespaces), Nodes, ArgoCD Apps |
| **K8s Namespace List** | K8s Resource Picker | `kubectl get namespaces` + async counts | NAMESPACE *(1)*, STATUS *(2)*, PODS *(3, w5, right)*, DEPLOY *(4, w6, right)*, STS *(5, w4, right)*, DS *(6, w3, right)*, AGE *(7)*. 📝 |
| **K8s Workload List** | Namespace List Enter | Deployments + StatefulSets + DaemonSets | NAME *(1)*, READY *(2)*, AGE *(3)*. `Kind` is held but never rendered. 📝 |
| **K8s Pod List** | Workload List Enter | pods, name-sorted | NAME *(1)*, STATUS *(2)*, READY *(3)*, RESTARTS *(4, right)*, NODE *(5)*, AGE *(6)*. 📝. Auto-refreshes on the fleet tick |
| **K8s Pod Detail** | Pod List Enter | key/value + container tables | Name, Namespace, Node, IP, Status, Ready, Restarts, Age. Containers: NAME, IMAGE, STATE, READY, RESTARTS, CPU REQ, CPU LIM, MEM REQ, MEM LIM (indicators 1-9 rendered, no keys bound). Init containers: same columns, no indicators |
| **K8s Pod Logs** | Pod List `l` / Pod Detail `l` | merged `kubectl logs`, newest first, cap 500 | TIMESTAMP, LEVEL, MESSAGE (no indicators) |
| **K8s Log Detail** (flag) | Pod Logs Enter | one entry | Timestamp, Pod, Level, Message |
| **K8s Node List** | K8s Resource Picker | `kubectl get nodes` + `kubectl top nodes` | NAME *(1)*, STATUS *(2, w10)*, VERSION *(3)*, TAINTS *(4)*, CPU *(5)*, %CPU *(6)*, MEM *(7)*, %MEM *(8)*, CPU/A *(9)*, VM SIZE, AGE |
| **K8s Node Detail** | Node List Enter | 3 key/value sections + taints + conditions + pod table | Version, OS Image, Kernel, Runtime, VM Size, Pool, Internal, Pod CIDR, Created, Images / Status, Unschedulable, CPU Usage, Memory Usage, Running Pods / CPU, Memory, Pods (with allocatable). Pods: NAMESPACE *(1)*, NAME *(2)*, STATUS *(3)*, READY *(4)*, CPU REQ *(5)*, CPU LIM *(6)*, MEM REQ *(7)*, MEM LIM *(8)*, AGE *(9)* |
| **Probe List** | Fleet Picker Enter (probes) | HTTP probes, grouped then ungrouped | NAME *(1)*, URL *(2, truncated)*, STATUS *(3)*, CODE *(4)*, TLS VERIFY, INTERVAL *(6)*, LATENCY *(5)*. Live dot: white `●` pending, green `◉` probed <2s ago, green `●` steady. Group headers |
| **Probe Detail** | Probe List Enter | scrollable sections | `── Summary ──` Name, URL, Protocol, Expected Code, TLS Verify, Status, Last Check; `── Timing ──` Latency, TTFB; `── TLS ──` (HTTPS only) Version, Issuer, Subject, Expires + days; `── Response ──` Status Code + ≤2KB body preview; `── Error ──` Class, Detail |
| **Note List** | `n` on a noteable view | notes for the selected resource, newest first | DATE (`02/01/2006 15:04` local), PREVIEW (first non-empty line ≤80 chars, `(empty)` when blank) |
| **Note Read** | Note List Enter | note contents | plain scrollable lines |

### 1.4 The Resource Picker is dynamic

`view_resource.go:visibleResourceRows()` builds rows in this order:

1. `Services` — always
2. `Processes` — only when `Host.SupervisorctlPresent` (the probe runs `command -v supervisorctl`)
3. `Containers`, `Cron Jobs`, `System Logs`, `Updates`, `Disk`, `Subscription`, `Accounts`, `Network` — always
4. `Failed Logins`, `Sudo Activity`, `SELinux Denials`, `Audit Summary` — always
5. `Logs` — only when `len(host.Entry.Logs) > 0`

TOTAL / RUNNING / FAILED come from the pre-fetch (services, containers, updates)
plus probe counters. `Subscription`, `Failed Logins`, `Sudo Activity`,
`SELinux Denials` and `Audit Summary` always render `0 0 0`.

### 1.5 Modals and overlays

| Modal | Trigger | Behaviour |
|---|---|---|
| First-Run Wizard | `NewModel` when `appCfg.FleetDir == ""` | section 6.1 |
| Custom Editor Wizard | editor choice `custom` | 1 free-text step |
| About | Fleet Picker `a` | Version, Repository, Azure CLI, Azure Identity, kubectl — last three async, 5s timeout each, render `loading...` → value / `not found` / `timeout` / `unknown` |
| Help | `?` | scrollable per-view static text; footer `?/Esc close  ↑↓ scroll` |
| SSH Password | probe returns an auth error | 1 masked step, title `SSH Password`; retries **every** host flagged `NeedsPassword` |
| Sudo Password | fetch returns a sudo error, or a cached sudo password failed | 1 masked step, carries a `retry` Cmd |
| Sudo Wizard | Host List Enter when sudo needs a password and no SSH password is cached | section 6.2 |
| Loading | every list fetch, tagged | **non-dismissable** — swallows every key including Esc; cleared only by the matching tagged result |
| Confirm (Y/N) | section 4 | footer `Y/Enter confirm  N/Esc cancel` |
| Transition Confirm | Azure VM/AKS, K8s pod/context actions | Confirm that emits a `transition` into the Action Engine |
| Options-Confirm | Update List `u` / `p` | multi-select dnf flags → confirm resolved command |
| Command Wizard | Host List `c` | 2 steps (one group) or 3 (group picker first) |
| Note delete confirm | Note List `d` | shows date + preview |

Loading tags: `services`, `containers`, `updates`, `cron`, `loglevels`,
`errorlogs`, `disk`, `subscription`, `accounts`, `network`, `interfaces`,
`ports`, `routes`, `firewall`, `failedlogins`, `sudo`, `selinux`, `audit`,
`processes`, `logfiles`, `resourcecounts`, `vms`, `aks`, `contexts`, `nodes`,
`namespaces`, `workloads`, `pods`, `podlogs`, `sudotest`.

---

## 2. Fleet types

Four, validated in `config/parser.go:ParseFleetFile`. Any other value is a parse
error that aborts startup for **all** fleets (`main.go` exits 1).

| `type` | Connects to | Entry view | What `groups[]` means | Distinguishing config |
|---|---|---|---|---|
| `vm` | remote hosts over SSH, in-process Go client — no `ssh` binary for list views | Host List | visual grouping; each group has `hosts[]` and may override `service_filter`, `logs`, `commands` | all of `user/port/timeout/systemd_mode/service_filter/logs/commands/error_log_since/refresh_interval/rh_*` |
| `azure` | local `az` CLI; prerequisite check on Enter distinguishes "not installed" from "not logged in" | Azure Subscription List | **each group name is an Azure subscription name**; `groups[].hosts` is ignored | `tenant_id`, `activity_log_hours` (default 3), `display_tags` |
| `kubernetes` | local `kubectl` | K8s Cluster List | **each group name is a cluster name**, matched against `kubectl config get-contexts` | none beyond `name`/`type`/`groups` |
| `probes` | HTTP(S) endpoints directly from the FleetDesk process, optional proxy | Probe List | visual grouping of probes | parsed by a **separate** function into `Fleet.ProbeFleet`; `defaults{interval,proxy,timeout,insecure_skip_verify}`, `groups[].probes[]`, `probes[]` |

Fleet Picker order is fixed by `fleetTypeOrder`: vm(0) → azure(1) → kubernetes(2)
→ probes(3) → other(4), then by name. `TARGETS` is host count for vm, probe count
for probes, and `len(groups)` for azure and kubernetes.

`ProbeFleet` is `nil` for every non-probes type; the Probe List and Probe Detail
dereference `m.fleets[...].ProbeFleet.Defaults` without a nil guard.

---

## 3. Key handlers

### 3.1 Global pre-dispatch chain

`keys.go:handleKey`, in this exact order:

1. **A modal is open** (`m.modal != nil && !Done()`) → every key goes to the modal; no view handler runs.
2. **`m.showLogDetail`** → *any* key closes the Error-Log detail overlay.
3. **`m.filterActive`** → filter capture: `Enter` commits and resets ~26 cursors, `Esc` clears text and exits, `Backspace` deletes one byte, any rune appends. **Returns unconditionally.**
4. Clears `flash` / `flashError`.
5. **`n`** → if `isNoteableView(m.view)` and `noteEngine != nil`, opens the Note List. Consumed before the view handler.
6. **`q` / `ctrl+c`** → `m.azure.Close()`, quit.
7. **`?`** → per-view help modal.
8. `switch m.view`.

### 3.2 Per-view bindings

`↑/k` = up, `↓/j` = down throughout. Keys in parentheses are bound in the handler
but unreachable — see 3.3.

| View | Bindings |
|---|---|
| Fleet Picker | `↑↓`, `c` Config, `a` About, `e` edit fleet file, `r` rescan, `Enter` open. No `Esc`, no `/` |
| Host List | `↑↓`, `x` shell, `K` deploy key, `R` reboot, `c` Commands, `d` Metrics, `r` re-probe, `Enter` drill in, `Esc` back. No `/`, no sort |
| Metrics | `↑↓`, `1`–`5` sort, `r`, `Esc`. No `/`, no `Enter` |
| Resource Picker | `↑↓`, `Enter`, `r`, `Esc`. No `/`, no sort |
| Service List (list) | `↑↓`, `/`, `Enter` detail, `1`–`4` sort, `r`, `Esc` (clears filter first) |
| Service List (detail) | `↑↓` scroll logs, `/` search logs, `s` start, `o` stop, `t` restart, `r`, `Esc` (clears filter first) |
| Container List (list) | `↑↓`, `Enter`, `/`, `l` logs, `i` inspect, `e` exec, `1`–`3` sort, `r`, `Esc` |
| Container List (detail) | `↑↓`, `Esc` |
| Cron List | `↑↓`, `/`, `1`–`3` sort, `r`, `Esc` |
| Log Level Picker | `↑↓`, `Enter`, `r`, `Esc`. No `/`, no sort |
| Error Log List | `↑↓`, `/`, `Enter` (sets `showLogDetail`), `l` full log, `1`–`3` sort, `r`, `Esc` |
| Update List (list) | `↑↓`, `Enter`, `/`, `u` all updates, `p` security, `1`–`3` sort, `r`, `Esc` |
| Update List (detail) | `↑↓`, `Esc` |
| Disk List (list) | `↑↓`, `Enter`, `/`, `1`–`6` sort, `r`, `Esc` |
| Disk List (detail) | `↑↓`, `Esc` |
| Subscription | `↑↓`, `u` unregister, `g` register, `d` disable repo, `c` check repo, `r`, `Esc`. No `/`, no sort |
| Account List | `↑↓`, `Enter`, `/`, `1`–`5` sort, `r`, `Esc`. In detail mode **any** key closes the detail |
| Network Picker | `↑↓`, `Enter`, `r`, `Esc` |
| Interfaces / Ports / Routes | `↑↓`, `/`, `1`–`4` sort, `r`, `Esc` → Network Picker |
| Firewall | as above but `1`–`5` |
| Failed Logins / Sudo Activity | `↑↓`, `/`, `1`–`4` sort, `r`, `Esc` |
| SELinux Denials / Audit Summary | `↑↓`, `/`, `1`–`5` sort, `r`, `Esc` |
| Process List | `↑↓`, `s`, `o`, `t`, `l`, `/`, `1`–`4` sort, `r`, `Esc`, (`q`) |
| Log File List | `↑↓`, `Enter` tail, `/`, `r`, `Esc`, (`q`). No digit keys |
| SSH Stream | `↑↓` and `Space` and `G` (only when `NewestFirst`), `w` save, `Esc` stop and return, (`q`) |
| Probe List | `↑↓`, `Enter`, `/`, `r` restart probing, `1`–`6` sort, `Esc` (stops probing), (`q`) |
| Probe Detail | `↑↓`, `r`, `Esc`, (`q`) |
| Config | `e`, `r`, `Esc`. No `↑↓` |
| Note List | `↑↓`, `/`, `Enter` read, `n` new, `e` edit, `d` delete, `Esc` → `previousView` |
| Note Read | `↑↓`, `Esc` |
| Azure Sub List | `↑↓`, `Enter`, `r`, `/`, `1`–`3` sort, `Esc`, (`q`) |
| Azure Resource Picker | `↑↓`, `Enter`, `r`, `Esc`, (`q`) |
| Azure VM List | `↑↓`, `Enter`, `s` start, `o` deallocate, `t` restart, `/`, `1`–`7` sort, `r`, `Esc`, (`q`) |
| Azure VM Detail | `↑↓` scroll (`down` unbounded), `a` activity log, `r`, `Esc`, (`q`) |
| Azure AKS List | `↑↓`, `Enter`, `s` start, `o` stop, `d` delete, `/`, `1`–`9` sort (hint label is dynamic), `r`, `Esc`, (`q`) |
| Azure AKS Detail | `↑↓` (moves the *activity-log* cursor, not a page scroll), `a`, `Esc`, (`q`). No `r` |
| K8s Cluster List | `↑↓`, `Enter`, `r`, `/`, `1`–`3` sort, `Esc`, (`q`) |
| K8s Context List | `↑↓`, `Enter`, `d` delete context, `r`, `/`, `Esc`, (`q`). **No digit keys** |
| K8s Resource Picker | `↑↓`, `Enter`, `r`, `Esc`, (`q`) |
| K8s Node List | `↑↓`, `Enter`, `/`, `1`–`9` sort, `r`, `Esc`, (`q`) |
| K8s Node Detail | `↑↓` pod cursor, `/`, `1`–`9` sort pods, `r`, `Esc`, (`q`) |
| K8s Namespace List | `↑↓`, `Enter`, `/`, `1`–`7` sort, `r`, `Esc`, (`q`) |
| K8s Workload List | `↑↓`, `Enter`, `/`, `1`–`3` sort, `r`, `Esc`, (`q`) |
| K8s Pod List | `↑↓`, `Enter`, `/`, `1`–`6` sort, `l` logs, `d` delete pod, `r`, `Esc`, (`q`) |
| K8s Pod Detail | `↑↓` container cursor (`down` unbounded), `g` top, `l` logs, `Esc`, (`q`). **No digit keys** |
| K8s Pod Logs (list) | `Enter` detail, `↑↓`, `G`, `g`, `s` stop/resume stream, `d` cycle level filter, `c` toggle sidecars, `Esc`, (`q`). **No digit keys** |
| K8s Pod Logs (detail) | `↑↓`, `g`, `Esc`/`q`/`Enter` close (resumes streaming if it was live) |

### 3.3 Unreachable bindings and asymmetries

Observations about the code as written. No recommendation is implied.

- **`q` is intercepted globally**, so every per-view `case "q"` is unreachable. The cleanup those cases perform — `m.stopProbing()`, `m.stopSSHStream()`, `m.k8sPodLogCancel()` — therefore never runs on quit. Only `m.azure.Close()` does.
- **The global `filterActive` block returns unconditionally**, so the per-view filter blocks in `handleProbeListKeys`, `handleProcessListKeys` and `handleLogFileListKeys` are unreachable. Those three views also never have their cursor reset by the global block, which resets ~26 other cursors plus `noteCursor`.
- **`/` is not bound** in: Fleet Picker, Host List, Metrics, Resource Picker, Log Level Picker, Subscription, Network Picker, Azure Resource Picker, K8s Resource Picker, Azure VM Detail, Azure AKS Detail, K8s Pod Detail, Config, Note Read.
- **`r` is not bound** in: Azure AKS Detail, K8s Pod Detail, K8s Pod Logs, Note List, Note Read.
- **`Esc` is bound everywhere except the Fleet Picker** (the root).
- **`n` is a global intercept**, so the 11 noteable views cannot bind `n` themselves. It also fires while the Service List is in *detail* mode, resolving the ref from `filteredServices()[serviceCursor]`.
- **`d` means four things**: Metrics (Host List), disable repo (Subscription), delete (AKS List, K8s Context List, K8s Pod List), cycle level filter (Pod Logs).
- **`c` means four things**: Config (Fleet Picker), Commands (Host List), Check Repo (Subscription), toggle containers (Pod Logs).
- **`s`/`o`/`t`** are start/stop/restart in Service Detail, Process List and Azure VM List; `s`/`o` only in AKS List; `s` alone is stop/resume streaming in Pod Logs.
- **Detail-mode Esc differs**: Service List clears the filter before closing; Container/Update/Disk close immediately; Account detail closes on *any* key.
- **Unbounded scroll increments**: Azure VM Detail `down`, K8s Pod Detail `down`, K8s Log Detail `down`, Probe Detail `down`. Bounds are applied at render time only.
- Azure VM List, AKS List, K8s Node/Namespace/Workload/Pod List `up` handlers compute `filtered := ...` then discard it (`_ = filtered`).

### 3.4 Help coverage

`helpForView` covers 41 views. The `default` branch returns `globalHelp(false)`,
which is what **Probe List, Probe Detail, Process List, Log File List, SSH Stream,
Note List and Note Read** receive. Those seven views' real bindings (`s`/`o`/`t`/`l`,
`w`, `Space`, `n`/`e`/`d`) appear only in the hint bar, never in the `?` overlay.

---

## 4. Actions

### 4.1 Through the Action Engine

The engine (`model.go` `transition` struct plus `actionResultMsg` / `pollResultMsg`
/ `transitionExpireMsg` handling, `commands.go:startPoll`) is closure-driven:
`ExecFn`, `PollFn`, `RefreshFn`, `IsTransitioning`. It never switches on resource
type. Two strategies: `poll` and `oneshot`.

| Action | Key | Runs | Confirm | Strategy | Target |
|---|---|---|---|---|---|
| Azure VM start | `s` VM List | `az vm start --name … --resource-group … --subscription … --no-wait [--tenant …]` | `start <vm>? [Y/n]` | poll | `running` |
| Azure VM deallocate | `o` | `az vm deallocate …` | `deallocate <vm>? [Y/n]` | poll | `deallocated` |
| Azure VM restart | `t` | `az vm restart …` | `restart <vm>? [Y/n]` | poll | `running` |
| AKS start | `s` AKS List | `az aks start …` | yes | poll | `running` |
| AKS stop | `o` | `az aks stop …` | yes | poll | `stopped` |
| **AKS delete** | `d` | `az aks delete … --yes --no-wait` | `DELETE cluster <n>? This is irreversible! [Y/n]` | poll | `gone` |
| K8s pod delete | `d` Pod List | `kubectl delete pod <n> -n <ns> --context <ctx> --wait=false` | `delete pod <n>? [Y/n]` | poll | `gone` |
| K8s context delete | `d` Context List | `kubectl config delete-context <n>` | `delete context <n>? [Y/n]` | **oneshot** | n/a |

State guards before the modal opens: VM `start` refuses when already `running`,
`deallocate` when already `deallocated`, `restart` when not `running`. **AKS
actions have no state guard.**

A poll transition clears when the polled state equals `TargetState` **and** either
a transitioning state was previously observed (`Confirmed`) or `PollCount >= 3`.
Failures set `Display` to `"<action> failed"` and schedule an expiry.
Transitioning states recognised (case-insensitive): `starting`, `stopping`,
`deallocating`, `restarting`, `deleting`.

**One path drives the engine without user action.** When `fetchAzureAKSClusters`
returns a cluster whose `ProvisioningState != "Succeeded"`, a transition is
synthesised with `Confirmed: true` and polling starts — no confirmation, because
the user did not initiate it. It is adopting an in-flight change made elsewhere.

### 4.2 Bespoke — terminal handover

All run through `sshHandover` (`ssh -t -o StrictHostKeyChecking=no -p <port> user@host '<cmd>'`)
or `cmdHandover`, print a banner, and wait for Enter before returning to the TUI.

| Action | Key / view | Command | Confirm |
|---|---|---|---|
| Interactive shell | `x` Host List | `ssh -t … user@host` (no command) | No |
| Deploy SSH key | `K` Host List | `ssh -t … 'sudo mkdir -p $HOME/.ssh && sudo chown -R $(id -u):$(id -g) $HOME && chmod 700 $HOME/.ssh' && ssh-copy-id …` via `bash -c` | Yes |
| **Reboot host** | `R` Host List | `sudo reboot; echo 'Reboot initiated'` | `REBOOT <host>? [Y/n]` |
| Service start/stop/restart | `s`/`o`/`t`, Service **Detail** only | `sudo systemctl <action> '<unit>'` (or `systemctl --user`) then `status` | `<action> <unit>? [Y/n]` |
| Container logs | `l` Container List | `podman logs -f '<name>'` | No |
| Container inspect | `i` | `podman inspect '<name>' \| less` | No |
| Container exec | `e` | `podman exec -it '<n>' /bin/bash \|\| podman exec -it '<n>' /bin/sh` | No |
| Full journal | `l` Error Log List | `sudo journalctl -p <lvl> --since '<since>' … \| less` | No |
| **Apply all updates** | `u` Update List | `sudo dnf update [flags] --setopt=skip_if_unavailable=1 -y` | 2-step options + confirm |
| **Apply security updates** | `p` | `sudo dnf update --security [flags] --setopt=skip_if_unavailable=1 -y` | 2-step options + confirm |
| **Unregister subscription** | `u` Subscription | `sudo subscription-manager unregister && sudo subscription-manager clean` (+ `&& sudo dnf remove -y katello-ca-consumer-*` when Satellite) | `Unregister from <type>? [Y/n]` |
| **Register subscription** | `g` Subscription | Satellite: `sudo subscription-manager clean && sudo dnf install -y 'http://<sat>/pub/katello-ca-consumer-latest.noarch.rpm' --disablerepo='*' && sudo subscription-manager register --org=… --activationkey=… --force`. CDN: `sudo subscription-manager register --org=… --activationkey=…` | `Register to <target>? [Y/n]` |
| **Disable repo** | `d` Subscription | `sudo dnf config-manager --set-disabled '<repo>'` | `Disable repo <id>? [Y/n]` |
| User-defined command | `c` Host List → wizard | the fleet file's `run:` string verbatim, in an SSH Stream | final wizard step shows the resolved `run:` |

### 4.3 Bespoke — SSH Stream, stays in the TUI

| Action | Key / view | Command | Confirm |
|---|---|---|---|
| Check repo | `c` Subscription | `sudo dnf makecache --refresh --repo='<id>' 2>&1; rc=$?; …` | No |
| Process start/stop/restart | `s`/`o`/`t` Process List | `sudo supervisorctl <action> '<name>'; …; sudo supervisorctl status` | `<action> <name>? [Y/n]` |
| Process log tail | `l` Process List | `sudo tail -n 200 -f /var/log/supervisor/<basename>.log` | No (read-only) |
| Log file tail | `Enter` Log File List | `tail -n 200 -f <path>`, prefixed `sudo ` when the entry sets `sudo: true` | No (read-only) |

The stream action is validated against an allowlist (`start`/`stop`/`restart`) in
the `startProcessActionViewMsg` handler; anything else flashes `Invalid action: <x>`.

### 4.4 Local state changes

| Action | Key / view | Effect |
|---|---|---|
| Edit fleet file | `e` Fleet Picker | opens `Fleet.Path` in `$editor`; on return re-scans `fleet_dir` |
| Edit config | `e` Config | opens `config.yaml`; on return reloads config **and** re-scans fleets |
| Save stream output | `w` SSH Stream | `<fleet_dir>/logs/<fleet>/<host>/<source>-<YYYY-MM-DD_HHMMSS>.log`, dir 0755 |
| Create note | `n` Note List | `<fleet_dir>/notes/<fleet>/<segments…>/<UTC ts>.<ms>_note.txt`, opens `$editor`, **deletes on exit if still whitespace-only** |
| Edit note | `e` Note List | opens the note in `$editor` |
| Delete note | `d` Note List | `os.Remove` after confirm |
| Write config.yaml | first-run wizard | dir 0755, file 0644 |

### 4.5 Sudo handling

`sudo` never reaches argv. `Manager.RewriteSudoInCmd` rewrites every occurrence of
the literal substring `"sudo "` into `echo '<password>' | sudo -S 2>/dev/null `.
Passwords are held in the SSH `Manager`, per host index, in memory only.
`handleSudoOrFlash` re-prompts when a cached sudo password fails; the SSH
connection password is silently tried as the sudo password first when one exists.
**A successful sudo password is cached against every host index, not only the one
it was tested on.**

---

## 5. Config

### 5.1 App config — `~/.config/fleetdesk/config.yaml`

Path is `os.UserHomeDir()` + `.config/fleetdesk` (`config/scanner.go:configDir`),
hard-coded with no override. `main.go` creates the directory 0755 on every start.

| Field | Type | Default | Notes | Documented in repo? |
|---|---|---|---|---|
| `fleet_dir` | string | **none — required**; empty is a fatal error | leading `~`/`~/` expanded (no `~user`); must exist, be a directory, and be writable (proved by creating and removing a `.fleetdesk-check-*` temp file) | **No.** Shown in the Config view as `Fleet directory` |
| `editor` | string | `""` → `$EDITOR` → `$VISUAL` → `vi` at read time | stored in an **unexported** struct field, reachable only via `Editor()` | **No.** Shown in the Config view as `Editor`, rendering the *resolved* value, not the stored one |

Any validation failure is fatal at startup, before the TUI initialises and before
the logger exists — so config failures are never logged, only printed to stderr.

### 5.2 Fleet file schema

Any `*.yaml` / `*.yml` in `fleet_dir` except `config.yaml` / `config.yml`.
Subdirectories are skipped. **A parse error in any one file aborts startup for all
of them.**

**Top level** (`config/parser.go:fleetFile`)

| Field | Type | Default | Applies to | Documented? |
|---|---|---|---|---|
| `name` | string | filename minus extension | all | README |
| `type` | string | **required**: `vm`\|`azure`\|`kubernetes`\|`probes` | all | README, for vm/azure/kubernetes only |
| `tenant_id` | string | `""` | azure — appended as `--tenant` to every `az` call | No |
| `activity_log_hours` | int | `3` (applied when `<= 0`) | azure — activity-log window | No |
| `display_tags` | []string | nil | azure — one extra AKS list column per tag, plus `Tag: <t>` detail rows | No |
| `defaults` | object | below | vm, probes | README: `user` and `timeout` only |
| `groups` | []object | nil | all four, different meanings (section 2) | README |
| `hosts` | []object | nil | vm — ungrouped hosts | README |
| `probes` | []object | nil | probes — ungrouped probes | No |

**`defaults` for `vm`**

| Field | Type | Default | Notes | Documented? |
|---|---|---|---|---|
| `user` | string | `""` | falls through at dial time to `~/.ssh/config` `User`, then `$USER` | README |
| `port` | int | `22` | see 10.2 — the `~/.ssh/config` lookup always returns `"22"` even with no config file | No |
| `timeout` | duration | `10s` | invalid value is a parse error | README |
| `systemd_mode` | string | `system` | only `user` is special-cased (`systemctl --user`, `journalctl --user-unit`) | No |
| `service_filter` | []string | nil | `filepath.Match` globs; empty means show all; **whole-list replace**, not merge. Matched against the **bare** unit name — `ParseServiceLine` strips the `.service` suffix before `MatchesFilter` runs, so a pattern of `nginx.service` never matches; `nginx*` does | No |
| `logs` | `[]{name,path,sudo}` | nil | merged additively by `name`; path validated (5.3) | No |
| `commands` | `[]{name,group,description,run}` | nil | merged additively by `group/name`; `name`, `group`, `run` required | No |
| `error_log_since` | string | `1 hour ago` | passed verbatim to `journalctl --since` | No |
| `refresh_interval` | duration | `15s` | drives `tickMsg`; **an unparsable value silently disables the tick** (`tickCmd` returns nil) | No |
| `rh_org_id` | string | `""` | subscription register | No |
| `rh_activation_key` | string | `""` | subscription register | No |
| `satellite_url` | string | `""` | when set, register targets Satellite instead of CDN | No |

**`groups[]` (vm)**: `name`, `hosts[]`, `service_filter`, `logs`, `commands`.

**`hosts[]`**: `name` (**required**), `hostname` (**required**), `user`, `port`,
`timeout`, `systemd_mode`, `service_filter`, `logs`, `commands`, `rh_org_id`,
`rh_activation_key`, `satellite_url`.

Cascade rules:

- Scalars: host value wins, else `defaults`.
- `service_filter`: host if non-empty, else group if non-empty, else defaults. Whole-list replace.
- `logs`: `MergeLogEntries(defaults, group, host)` — additive catalog, dedupe by `name`, later level overrides.
- `commands`: `MergeCommands(defaults, group, host)` — additive, dedupe by `group + "/" + name`.
- RH fields: **if the host sets `rh_org_id`, that is a full override and `satellite_url` is deliberately not inherited**; only `rh_activation_key` falls back. If the host does not set `rh_org_id`, all three are inherited.

**`defaults` for `probes`**

| Field | Type | Default | Notes |
|---|---|---|---|
| `interval` | duration | `30s` | **minimum 5s**, enforced as a parse error |
| `proxy` | string | `""` | validated by `url.Parse`; passed only into the `http.Transport` closure, never logged |
| `timeout` | duration | `10s` | HTTP client timeout |
| `insecure_skip_verify` | bool | `false` | skip TLS verification |

**`probes[]` / `groups[].probes[]`**

| Field | Type | Default | Notes |
|---|---|---|---|
| `name` | string | **required** | |
| `url` | string | **required** | scheme must be `http` or `https` |
| `protocol` | string | `http` | anything else is a parse error ("v1 supports http only") |
| `expected_code` | int | `200` | |
| `interval` | duration | 0 → fleet default | minimum 5s |
| `insecure_skip_verify` | `*bool` | nil → fleet default | three-state per-probe override |

Probe group `name` is required. A TLS certificate within `CertWarnDays` (7) of
expiry downgrades a probe to `DEGRADED`. Error classes are a closed enum —
`none`, `dns`, `connect`, `timeout`, `tls`, `http_status` — and raw Go error
strings are redacted before reaching the UI.

### 5.3 Validation rules that reject a fleet file

- Unknown or missing `type`.
- Host missing `name` or `hostname`.
- Unparsable `timeout` at fleet or host level.
- Log entry missing `name`; log path empty, not absolute, longer than 512 chars, containing any of the 17 characters in `config/logs.go:shellMetachars` — `` $ ``, `` ` ``, `;`, `&`, `|`, `<`, `>`, newline, carriage return, backslash, `(`, `)`, `{`, `}`, `[`, `]`, `'` — or not equal to `filepath.Clean(path)`.
- Command missing `name`, `group`, or `run`.
- Probe missing `name` or `url`, non-http/https scheme, non-`http` protocol, interval < 5s, unparsable proxy URL, probe group missing `name`.

### 5.4 Documentation status

Of the 30 fleet-file fields above, the README documents seven: `name`, `type`,
`defaults.user`, `defaults.timeout`, `groups`, `hosts[].name`, `hosts[].hostname`.
Neither app-config field is documented anywhere. `docs/` contains no config
reference. See section 11.

---

## 6. Wizards and first-run

### 6.1 First-run wizard

Trigger: `NewModel` sets `m.modal = NewFirstRunWizard()` when
`appCfg.FleetDir == ""` — i.e. `config.yaml` is missing or unreadable
(`ErrNoConfig`). Other load errors are fatal before this point.

- **Step 1** — "Enter the path to your fleet files directory". Free text, validated live by `config.ValidateFleetDir`. Errors render under the input and the step does not advance.
- **Step 2** — "Select your preferred editor". Arrow-key list: `vim`, `neovim`, `nano`, `custom`. `neovim` is stored as `nvim`.
- **Step 2b (conditional)** — `custom` emits `wizardNeedCustomEditorMsg` and opens a fresh 1-step modal, "Enter your editor command", **unvalidated**.

Writes `~/.config/fleetdesk/config.yaml` with `fleet_dir` and `editor` (dir 0755,
file 0644), reloads the config, scans fleets, and lands on the Fleet Picker with
flash `Setup complete`.

Esc on step 2 goes **back** to step 1 (the overlay trims `results`); it does not
cancel. Esc on step 1 cancels: the program quits and `main.go` prints
`FleetDesk requires a configuration file. Run fleetdesk again to complete setup.`
A failure quits and prints `Setup failed: <err>`.

### 6.2 Sudo wizard

Trigger: `Enter` on a host whose `SudoReady` is false → `testSudo` runs
`sudo -n true 2>&1`. If sudo needs a password: an already-cached SSH connection
password is silently tried first; otherwise the wizard opens.

One masked step `Sudo password for <user>@<host>:` → on Enter the modal is
replaced by a non-dismissable `Testing sudo...` overlay while `sudo true` runs →
success caches the password against **every** host index and marks every online
host `SudoReady`, then navigates to the Resource Picker. Failure flashes
`Wrong sudo password` and re-opens the wizard. Writes nothing to disk.

### 6.3 Command wizard

Trigger: `c` on the Host List, `vm` fleet, online host with at least one
`commands:` entry (otherwise flashes `No commands defined for this host`).

- One group → 2 steps: "Select command — `<group>`" (commands sorted by name, displayed as `name — description`), then confirm showing the literal `run:` string.
- Two or more groups → 3 steps: "Select command group" (groups sorted alphabetically), "Select command", confirm.

On confirm it emits `startCommandStreamMsg`. **If the `run:` string contains the
substring `sudo` and no sudo password is cached for that host, a Sudo Password
modal is inserted first.** Then the command runs in an SSH Stream
(`NewestFirst: false`, `AutoDone: true`, return view = Host List).

### 6.4 dnf options-confirm

Trigger: `u` or `p` on the Update List. Step 1 is a checkbox multi-select
(`Space` toggles) over `--allowerasing`, `--skip-broken`, `--nobest`. Step 2 shows
the fully resolved command. Confirm hands over to the terminal.
`--setopt=skip_if_unavailable=1 -y` and a trailing
`echo ''; echo 'Done. Press Enter to return...'` are always appended.

### 6.5 Note create flow

`n` on the Note List creates an empty `<UTC ts>.<ms>_note.txt` under the sanitised
resource directory, hands the terminal to `$editor`, and on return **deletes the
file if it still contains only whitespace**. Either way it invalidates the
note-count cache for that ref and reloads the list.

---

## 7. CLI surface

**Flags** — `main.go`, a hand-rolled loop over `os.Args[1:]`, no `flag` package:

| Flag | Effect |
|---|---|
| `--debug` | enables the file logger |
| `--version` | prints `fleetdesk <version> (<commit>)` and exits 0 |

There are **no subcommands**, no `-h` / `--help`, no short forms, and **unknown
arguments are silently ignored** — `fleetdesk --dbeug` starts normally with debug
off. `version` and `commit` default to `dev` / `none` and are set by goreleaser
through `-X main.version` / `-X main.commit`.

**Environment variables read** — exhaustive; there are no `FLEETDESK_*` variables:

| Variable | Read at | Use |
|---|---|---|
| `EDITOR` | `config/appconfig.go:Editor()`, `handover.go:editorExec.Run` | editor fallback 1 |
| `VISUAL` | same two places | editor fallback 2 |
| `USER` | `ssh/manager.go:dial` | SSH user, last resort |
| `SSH_AUTH_SOCK` | `ssh/manager.go:sshAgentAuth` | agent auth |

Also consulted, though not env vars: `~/.ssh/config` via `kevinburke/ssh_config`
for exactly four keys — `User`, `Port`, `Hostname`, `IdentityFile`. See 10.2.

**Filesystem paths**

| Path | Mode | When |
|---|---|---|
| `~/.config/fleetdesk/` | 0755 | created on every start |
| `~/.config/fleetdesk/config.yaml` | 0644 | written by the first-run wizard |
| `<fleet_dir>/*.yaml`, `*.yml` | read | on start, on `r`, and after any editor return |
| `~/.local/share/fleetdesk/debug.log` | dir 0755 | **only** with `--debug`; truncated on every start |
| `<fleet_dir>/logs/<fleet>/<host>/<src>-<ts>.log` | dir 0755 | SSH Stream `w` |
| `<fleet_dir>/notes/<fleet>/<segments…>/<ts>_note.txt` | dir 0755 | note create |

`logging.NewTargetLogger` — per-host / per-subscription / per-context log files
named `host-*.log`, `sub-*.log`, `ctx-*.log` — **has no callers**. Those files are
never written.

**External binaries invoked**: `ssh` and `ssh-copy-id` (terminal handover only —
list views use the in-process Go SSH client), `az`, `kubectl`, `bash -c`
(deploy-key only), and whatever `editor` resolves to. `az` and `kubectl` are
located with `exec.LookPath` at fleet-entry time.

**Make targets**: `test`, `test-report`, `build`, `lint`, `check`, `clean`,
`verify-nolint`, `verify-lint-baseline`, `verify-fresh-clone`.

**Release**: goreleaser builds `linux`/`darwin` × `amd64`/`arm64`, `CGO_ENABLED=0`,
archive `fleetdesk_{os}_{arch}.tar.gz`, release owner `Gaetan-Jaminon`.

---

## 8. Everything else a specification must describe

**Startup order and failure modes.** `main.go` runs: create config dir → load
config → scan fleets → init logger → build model → run program. A fleet-file parse
error, an unreadable config, or a `fleet_dir` that fails validation exits 1 with a
bare stderr message before the TUI appears. The logger is initialised *after*
config loading, so config failures are never logged.

**SSH authentication chain.** `dial()` builds an ordered method list and tries each
in a **separate `gossh.Dial`** — deliberately, to avoid `MaxAuthTries` exhaustion
(ADR-F003): (1) agent via `$SSH_AUTH_SOCK`, (2) `IdentityFile` from
`~/.ssh/config` for that hostname, (3) `~/.ssh/id_ed25519`, `id_rsa`, `id_ecdsa`
where present. **A password is not in this chain** — passwords are used only via
the separate `ConnectWithPassword` path, triggered when the probe returns
`IsAuthError`. `HostKeyCallback` is `InsecureIgnoreHostKey()` (flagged in-code
against TAE-22).

**Host probe.** One compound shell command returns 13 fixed lines: FQDN, uptime,
`PRETTY_NAME`, cron count, `journalctl -p err` count since `error_log_since`, disk
count, disks at ≥80%, user count, interfaces up, interfaces total, listening
ports, pending updates, `supervisorctl` present. If the command fails it retries
with the opposite `systemd_mode` and a 3-line fallback (FQDN, uptime, OS), leaving
every counter at 0. Output before an `---PROBE---` sentinel is stripped to survive
shell login noise.

**Periodic refresh.** One `tickMsg` loop, interval = the *selected fleet's*
`defaults.refresh_interval`. It re-probes only on the Host List, Azure Sub List,
Azure AKS List and K8s Pod List. On every tick, in every view, it also refreshes
note counts for the visible rows.

**Note-count indicators.** Counts load asynchronously per view; until loaded, rows
render without the 📝 prefix — deliberate, to avoid flicker. The cache is
invalidated on note create, edit and delete.

**Path sanitisation.** `fspath.Sanitize` maps `/`→`_`, `\`→`_`, space→`-`, `:`→`_`
per path component. It does **not** handle `..`, leading dots, or empty strings.
It is applied to fleet names, host names, every note ref segment, and
target-logger filenames.

**Streaming and generation counters.** SSH streams, K8s pod-log streams and probe
runs each carry a `generation` int; messages from an earlier generation are
discarded. Caps: SSH stream 1000 lines newest-first / 5000 append, K8s pod logs
500, batch size 50 lines. Auto-scroll holds the cursor at the newest entry only
when it was already there.

**Flash messages.** A single hint-bar line, green or red, cleared on the next
keypress. It is the only channel for a large class of outcomes:
`Host is not reachable`, `az CLI not found — install Azure CLI to use Azure fleets`,
`Not logged in to Azure — run 'az login' first`,
`kubectl not found — install kubectl to use Kubernetes fleets`,
`rh_org_id and rh_activation_key required in config`,
`No commands defined for this host`, `Unsupported fleet type`,
`ArgoCD Apps view coming in next PR`, `Cancelled`, `Reloaded`, `Setup complete`,
`Saved N lines to <path>`, `Wrong sudo password`, and every `Failed: <err>`.

**Terminal-handover return refresh.** After any handover,
`sshHandoverFinishedMsg` re-enters the alt screen and re-fetches the current
view's data, via a switch covering Service (detail or list), Container, Update,
Subscription, Account, Network Picker, Process, Log File and SSH Stream. Every
other view gets only `EnterAltScreen`.

**Rendering contract.** Each list view is a bordered box: header line
(`fleetdesk › breadcrumb` left, `FleetDesk <version>` and `cursor/total` right),
top border, column header, separator, rows (alternating `altRowStyle` /
`normalRowStyle`, cursor row `selectedRowStyle`, cursor marker ` ▸ `),
`padToBottom` to `height-3`, bottom border, hint bar. Rows wider than the box are
truncated with `…`. Group headers render as `── label ──`. **Two column engines
coexist**: the shared `renderList` / `ListConfig` (10 views) and hand-rolled
`fmt.Sprintf` headers (the rest).

**Terminal size.** `tea.WindowSizeMsg` sets `m.width` / `m.height`. Before the
first one arrives both are 0; render functions substitute width 80 when
`width < 20`, and `maxVisible` clamps to ≥1. There is no minimum-size warning.

**Colour and theming.** A fixed 256-colour palette in `styles.go`. No light/dark
detection, no configuration. Several views bypass lipgloss and emit raw ANSI
(`ansiColor`, `\033[32m`) for status colouring.

**Azure Resource Graph vs. legacy CLI.** VM, AKS and count queries prefer a single
`az graph query --first 1000` against the subscription UUID, falling back to
per-resource `az` calls when the UUID is unknown or the graph query fails. **The
`--first 1000` cap is silent** — a subscription with more than 1000 matching
resources is truncated with no indication in the UI.

**K8s context auto-skip.** When a cluster matches exactly one kubectl context the
Context List is skipped and the user lands on the Resource Picker. Esc from there
still returns to the Context List, which then shows that single row.

---

## 9. Sort-key audit

Every `sortX` function was compared against the header its view renders. Four
mismatches, listed individually. Each is a spec-drift candidate; what it means is
not decided here.

### 9.1 Confirmed header/sort mismatches

**M1 — Account List** (`view_account.go:102`, `helpers.go:526`)

| Key | Indicator printed on | Actually sorts |
|---|---|---|
| 1 | `USER` | `User` ✓ |
| 2 | `UID` | `Groups` |
| 3 | `GROUPS` | `Shell` |
| 4 | `SHELL` | `LastLogin` |
| 5 | *(no column)* | `PasswordStatus` |

`UID` is never sortable. The handler binds `1`–`5`, the hint bar says `1-4`
(`view_account.go:160`), the help overlay says `1-5` (`help.go:helpAccountList`).

**M2 — Audit Summary** (`view_security_audit.go:49`, `helpers.go:727`)

| Key | Indicator printed on | Actually sorts |
|---|---|---|
| 1 | `TIME` | `Time` ✓ |
| 2 | `USER` | `Type` — not a rendered column |
| 3 | `RESULT` | `User` |
| 4 | `MESSAGE` | `Result` |
| 5 | *(no column)* | `Message` |

The handler binds `1`–`5`, the hint bar says `1-4`
(`view_security_audit.go:105`), the help overlay says `1-5`.

**M3 — Azure VM List, column 5** (`view_azure_vms.go:71`, `helpers.go:1002`)

The column is labelled `OS` and its `RowBuilder` renders `vm.OSType` (`Linux` /
`Windows`). Sort key 5 sorts `vm.OSDisk` (`OS distro info, e.g. "RHEL 9"`). The
displayed value and the sort key are different fields. Keys 1-4, 6, 7 match.

**M4 — K8s Cluster List** (`view_k8s_clusters.go:38`, `helpers.go:1109`)

| Key | Indicator printed on | Actually sorts |
|---|---|---|
| 1 | `CLUSTER` | `Name` ✓ |
| 2 | `K8S VERSION` | `Status` — an int enum driving the row-override text, not a column |
| 3 | *(no column)* | `ContextCount` — never rendered in this view |

`K8sVersion` is never sortable. The handler binds `1`–`3`, the hint bar says
`1-2` (`view_k8s_clusters.go:81`), the help overlay says `1-3`.

### 9.2 Sort indicators rendered with no key bound

Not mismatches — the mapping is consistent where one exists, but the user is shown
an affordance that does nothing.

| View | Indicators rendered | Keys bound | `sortView` entry |
|---|---|---|---|
| K8s Context List | 1-2 | none | none |
| K8s Pod Detail (container table) | 1-9 | none | none |
| Log File List | 1-4 | none | none |
| K8s Pod Logs | none | none | `sortK8sPodLogs` exists and is dispatched — unreachable |

### 9.3 Verified correct

`sortServices`, `sortContainers`, `sortCronJobs`, `sortErrorLogs`, `sortUpdates`,
`sortDisks`, `sortInterfaces`, `sortPorts`, `sortRoutes`, `sortFirewallRules`,
`sortFailedLogins`, `sortSudoEntries`, `sortSELinuxDenials`, `sortMetricsIdx`,
`sortAzureSubs`, `sortAzureAKS` (including the dynamic `display_tags` columns at
keys 9+), `sortK8sNodes`, `sortK8sNodePods`, `sortK8sNamespaces`,
`sortK8sWorkloads`, `sortK8sPodList`, `sortProcesses`.

**`sortProbeItems` is correct.** An earlier reading of this codebase reported the
Probe List sort keys as suspect because the indicators run left-to-right as
`1, 2, 3, 4, —, 6, 5`. Reading the sort function settles it: key 5 sorts
`Latency` and its indicator is printed on `LATENCY`; key 6 sorts `Interval` and
its indicator is printed on `INTERVAL`. The labels and the sorts agree. Only the
*numbering* is out of visual order, which affects the `1-6` hint's legibility, not
correctness.

### 9.4 A different class: lexical sorting of numeric strings

Not header/sort mismatches, recorded separately because they produce visibly wrong
ordering while the mapping is correct.

These sort **string** fields lexically where the content is numeric or
unit-suffixed, so e.g. `"9%"` orders after `"23%"`, and `"900m"` after `"1200m"`:

- K8s Node List keys 5-9 — `CPUUsage` (`"321m"`), `CPUPct` (`"8%"`), `MemUsage` (`"6211Mi"`), `MemPct` (`"23%"`), `CPUA` (`"3860m"`).
- K8s Node Detail pod table keys 5-9 — `CPUReq`, `CPULim`, `MemReq`, `MemLim`, `Age`.
- K8s Namespace List key 7, Workload List key 3, Pod List key 6 — `Age` (`"5d"`, `"12h"`).
- Process List keys 3-4 — `Uptime`, `PID`.
- Azure AKS List key 8 — `CreatedDate`.

By contrast `sortDisks` (keys 2-5) and `sortRoutes` (key 4) *do* use
`parseNumericPrefix`, and `sortMetricsIdx` key 5 parses the load average with
`Sscanf` — so the numeric-aware approach exists in the codebase and is applied
inconsistently.

---

## 10. Resolved investigations

Three questions an earlier pass left open. All three are now settled from source.

### 10.1 ProxyJump is not honoured in the in-TUI path

**Settled: not honoured.** Evidence:

- `grep -rni 'proxyjump\|proxycommand\|bastion\|jumphost'` across all `*.go` returns **zero hits**.
- `ssh_config.Get` is called exactly four times (`ssh/manager.go:384,390,402,416`) for `User`, `Port`, `Hostname`, `IdentityFile`. Nothing else is read from `~/.ssh/config`.
- The connection is `gossh.Dial("tcp", addr, sshConfig)` (`ssh/manager.go:265,455`) — a direct TCP dial. `golang.org/x/crypto/ssh` does not read OpenSSH config files at all.

So every in-TUI list view — the host probe and every `RunCommand` / `RunSudoCommand`
fetch — connects **directly** to `hostname:port`. A host reachable only through a
bastion will fail to probe.

**The terminal-handover paths behave differently.** `sshExec.Run` and the
deploy-key `cmdHandover` shell out to the real `ssh` binary, which reads
`~/.ssh/config` itself — so `ProxyJump` *does* apply to `x` (shell), `R` (reboot),
`K` (deploy key), the podman actions, the full-journal view, the dnf updates and
the subscription actions.

**A second-order consequence worth recording:** `dial()` resolves `Hostname` from
`~/.ssh/config` and dials the resolved address, while `sshExec` passes the raw
`h.Entry.Hostname` to `ssh`. Where a `Host` block rewrites `Hostname`, the in-TUI
path and the handover path can target different machines.

The README's SSH Authentication section lists `~/.ssh/config (IdentityFile, User,
Port, ProxyJump)` without distinguishing the two paths.

### 10.2 `ssh_config.Get` returns library defaults, not only file contents

Discovered while settling 10.1; it changes what two config fields mean.

The package-level `ssh_config.Get` wraps `DefaultUserSettings.Get` →
`GetStrict`, which ends `return Default(key), nil` — a built-in defaults table
(`validators.go:83`). Of the four keys FleetDesk reads, two have defaults:

| Key | Default returned when absent from `~/.ssh/config` | Effect |
|---|---|---|
| `Port` | `"22"` | `configPort` is never empty, so `if port == 0 && configPort != ""` always fires and the later `if port == 0 { port = 22 }` is unreachable. Same result, different route. |
| `IdentityFile` | `"~/.ssh/identity"` | With no `IdentityFile` directive, `publicKeyFile(ExpandPath("~/.ssh/identity"))` is attempted first. The file almost never exists, `publicKeyFile` returns nil, and no auth method is added — benign, but it means the auth list is always built against a phantom key path before the real defaults. |
| `User` | none — returns `""` | falls through to `$USER` as documented |
| `Hostname` | none — returns `""` | no rewrite when absent |

### 10.3 Update List `TYPE` is a closed set of three values

**Settled: `error`, `security`, `bugfix`.** All three are produced in
`commands.go:fetchUpdates`; nothing else ever writes `config.Update.Type`.

- `error` — assigned to any line of `dnf check-update` output starting with `Error:` or `Warning:`. For these rows **`Package` holds the entire error line and `Version` is empty**, so the PACKAGE column renders prose.
- `security` — the package name appears in the `dnf updateinfo list --security` set, matched via `ssh.ExtractPkgName` on the NVRA.
- `bugfix` — every other package line. This is the default, not a claim from `dnf`: any non-security update is labelled `bugfix` whether or not it actually is one.

Default ordering is `error` → `security` → `bugfix`, then alphabetical by package.
Lines matching `Last metadata`, `Is this ok`, `Not root`, `Microsoft`, `Importing`,
`Userid`, `Fingerprint`, `From`, and the entire `Obsoleting Packages` section, are
skipped.

---

## 11. Where the product describes itself

Every place in the repo that tells a user what FleetDesk is or how to use it, and
what each currently claims. The SPEC is not the only stale document.

| Surface | Location | What it currently claims |
|---|---|---|
| **README — feature table** | `README.md` "What You Can Manage" | Lists 14 VM resources. Omits Processes, Log Tail, Notes and user-defined Commands, all shipped |
| **README — Azure** | `README.md` | "**Azure Subscriptions (coming soon)** — Resource groups, VMs, costs — via local `az` CLI." Azure ships with subscription list, VM list and detail, AKS list and detail, start/stop/deallocate/restart/delete actions, and activity log. There is no cost feature |
| **README — Kubernetes** | `README.md` | "**Kubernetes Clusters (coming soon)** — Pods, deployments, services, nodes." K8s ships with cluster, context, namespace, workload, pod, pod-detail, pod-logs, node and node-detail views plus pod and context deletion. There is no services view |
| **README — probes** | `README.md` | **Absent.** The `probes` fleet type is not mentioned anywhere |
| **README — config example** | `README.md` "Configure" | Documents 7 of ~30 fleet-file fields. Shows `type: azure` and `type: kubernetes` skeletons annotated "coming soon" |
| **README — navigation diagram** | `README.md` "Navigation" | Shows Fleet Picker → Host List → Resource Picker → View, plus Metrics and SSH. Omits the Azure, Kubernetes, Probes and Notes branches entirely |
| **README — key table** | `README.md` | 9 keys. Omits `n`, `a`, `c`, `K`, `R`, `w`, `Space`, `s`/`o`/`t`, `l`, `i`, `e`, `u`, `p`, `g` |
| **README — SSH auth** | `README.md` | Lists `~/.ssh/config (IdentityFile, User, Port, ProxyJump)`. ProxyJump applies only to handover paths, not in-TUI (10.1). Also lists "Password fallback (inline masked prompt)" as step 4 of the chain; it is a separate retry path, not a member of the auth chain |
| **README — license** | `README.md` badge + "## License" | Badge links `LICENSE` and the section says MIT. **There is no LICENSE file in the repo.** The badge link is broken |
| **README — install** | `README.md` | `go install github.com/Gaetan-Jaminon/fleetdesk@latest` — a personal GitHub account, while CLAUDE.md describes FleetDesk as a Taelron product sold under the Taelron brand |
| **README — badges** | `README.md` | CI, Release, Go Version, License — all pointing at `Gaetan-Jaminon/fleetdesk` |
| **Screenshots** | `docs/screenshot-{fleet-picker,host-list,resource-picker,service-list,container-list}.png` | Five images, last changed **2026-04-05**. Only `screenshot-fleet-picker.png` is referenced by the README; the other four are orphaned but still present for anyone browsing `docs/` |
| **`--version`** | `main.go` | `fleetdesk <version> (<commit>)`. No description, no usage, no flag list |
| **No `--help`** | `main.go` | There is no help flag. An unknown argument produces no output at all |
| **First-run wizard** | `modal_wizard.go` | Title `FleetDesk Setup`; steps "Enter the path to your fleet files directory", "Select your preferred editor" (`vim`, `neovim`, `nano`, `custom`), "Enter your editor command". This is the only place a new user is told anything about configuration, and it explains neither what a fleet file is nor what belongs in the directory |
| **Startup failure text** | `main.go` | `FleetDesk requires a configuration file. Run fleetdesk again to complete setup.` and `Setup failed: <err>` |
| **About modal** | `modal_about.go` | Version, Repository (`github.com/Gaetan-Jaminon/fleetdesk`), Azure CLI, Azure Identity, kubectl |
| **`?` help overlays** | `help.go` | Per-view keybindings for 41 of 48 views. The 7 uncovered are listed in 3.4. Several disagree with the hint bar of the same view — see M1, M2, M4 in section 9 |
| **Hint bars** | every `view_*.go` | The primary discoverability surface. Three known disagreements with the `?` overlay (9.1) |
| **Flash messages** | throughout | Includes `ArgoCD Apps view coming in next PR` — a promise about an unshipped feature, visible in the K8s Resource Picker |
| **`go.mod` module path** | `go.mod` | `github.com/Gaetan-Jaminon/fleetdesk`. CLAUDE.md states the module path is whatever `go.mod` says and that any change is owned by its own issue |
| **`.goreleaser.yaml`** | repo root | `project_name: fleetdesk`, release owner `Gaetan-Jaminon` |
| **`CLAUDE.md`** | repo root | Describes the product to *contributors*, not users: "Go TUI (Bubble Tea) for managing fleets of Linux VMs over SSH, Azure resources via the `az` CLI, and Kubernetes clusters via `kubectl`. Taelron product, sold commercially under the Taelron brand." Does not mention the probes fleet type either |
| **`Makefile`** | repo root | `##` help text on each target |
| **`docs/incidents/2026-04-16-aap-dev-os-update.md`** | `docs/incidents/` | An incident record, not product description. Noted for completeness |

**Why the README is stale, precisely.** Its product content was last rewritten in
`68ea4eb` — "docs: rewrite README for multi-platform vision (#74)", **2026-04-08**.
The very next day `76acbd3` (#75) landed "Azure + Kubernetes fleet types, AKS
actions, pod logs (FLE-44 to FLE-53)". The "coming soon" labels were therefore
stale within 24 hours of being written, and have stood for roughly four and a half
months. Everything since — probes (#95, #96, #99), first-run wizard and config
view (#91), modal prompts (#92), help and about modals (#93), log tail and
processes (#100), user-defined commands (#101), Team Notes (#103) — postdates the
last product-facing README edit. The two README commits after that date
(`e11985e`, `eb4a287`) touched only the Development section.

---

## 12. What could not be determined

Stated rather than omitted.

1. **Whether any of the four sort mismatches in 9.1 is intentional.** Each looks like an off-by-one introduced when a column was added or removed, but nothing in the code says so, and Design Notes may record one of them as an accepted deviation. Not checked — this pass did not open Linear.

2. **Whether an open issue already governs the module path / repo owner mismatch.** CLAUDE.md says such a change is owned by its own issue if one exists. Whether that issue exists was not checked, for the same reason.

3. **Whether the four orphaned screenshots are stale.** They were last changed 2026-04-05 and the UI has changed substantially since, but confirming this needs a running binary, which this pass did not use.

4. **The exact set of `STATE` values the Service List can render.** The mechanism is determined: `ssh.ParseServiceLine` renders the systemd *SubState* when ActiveState is `active`, and the ActiveState otherwise — so `running`, `exited`, `waiting`, `inactive` and `failed` all appear in one column. The Resource Picker counters special-case exactly `running` and `failed`. The full set of SubStates systemd can emit is data-dependent and not enumerable from this source.

5. **Whether `probes` fleets ignore `groups[].hosts` silently or error.** `parseProbeFleetFile` unmarshals into a different struct, so `hosts:` keys in a probes file are simply not read. Whether that is silent or produces a YAML strict-mode error was not tested.
