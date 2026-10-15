# FleetDesk — Project Instructions

Go TUI (Bubble Tea) for managing fleets of Linux VMs over SSH, Azure resources via
the `az` CLI, and Kubernetes clusters via `kubectl`. Taelron product, sold
commercially under the Taelron brand.

## Source of truth

Linear is canonical for all design content — workspace **Taelron**. This file points
at it; it does not duplicate it, and it does not record current state. **Anything
that changes lives in Linear.** When documents disagree, resolution order is
**ADRs > Design Notes > Domain Model > Specification > README**.

Read the applicable documents at the **start of every session** — the Linear copy is
more current than any cached context, and more current than this file. Fetch full
text via `curl` + `$LINEAR_API_KEY` (not the Linear MCP — token cost). The GraphQL
`document(id:)` query accepts the URL slug directly, so the links below are
fetchable as-is.

**Read in this order:**

1. [AI Build Workflow](https://linear.app/taelron/document/ai-build-workflow-bedca8de2907)
   — the loop every Taelron product runs. This is the default; FleetDesk does not
   opt in and has no variant of its own.
2. [AI Workflow](https://linear.app/taelron/document/ai-workflow-805fb2002fce)
   — surfaces, author capability, the Linear write gate. Partly superseded by (1);
   (1) wins on who does what.
3. [Delivery Workflow](https://linear.app/taelron/document/delivery-workflow-edab9d0993e8)
   — milestone rhythm, trivial-issue waiver, verification handoff, drift protocol.
   Its Phase B per-issue cycle is superseded by (1).
4. Other baselines as applicable:
   [Hexagonal Architecture](https://linear.app/taelron/document/hexagonal-architecture-b142001f420e) ·
   [TUI Go Conventions](https://linear.app/taelron/document/tui-go-conventions-1aca4ef63a66) ·
   [UI Patterns](https://linear.app/taelron/document/ui-patterns-9c3982a46ef2) ·
   [Security & Secret Handling](https://linear.app/taelron/document/security-and-secret-handling-75be68be36b6)
5. FleetDesk documents:
   [SPEC](https://linear.app/taelron/document/fleetdesk-spec-25e34eb61c20) ·
   [ADR Index](https://linear.app/taelron/document/fleetdesk-adr-index-877f77ec6d93) ·
   [Design Notes](https://linear.app/taelron/document/fleetdesk-design-notes-f7e62f6f2237) ·
   [Roadmap](https://linear.app/taelron/document/fleetdesk-roadmap-6c87af1c8252) ·
   [UI](https://linear.app/taelron/document/fleetdesk-ui-31a471474d2c) ·
   [Business Model](https://linear.app/taelron/document/fleetdesk-business-model-c01edf4d8e03)
6. The issue you were pointed at, and its milestone.

Issues are `TAE-N` (Taelron team). The `FLE-N` prefix is from the previous Linear
account and appears only in historical references.

**Where a document and the code disagree, check the open issues for a `spec-drift`
issue covering it before assuming either one is right.** Several product documents
are known to lag the shipped code, and which ones is tracked in Linear, not here.

## Taelron AI Build Workflow

This workspace runs the AI Build Workflow (Linear, Taelron Baselines). Read it at
session start; your role prompt says which phase and steps are yours.

- The PO is the notification. Claude Web (the PO Assistant) and Claude Code cannot
  reach each other. Write the details to Linear first, then end your turn with a
  short, ready-to-paste prompt for the other side: what changed, and where to read
  it. Never wait for the PO to ask what is next. The PO does not read Linear.
- Claude Code sessions message each other directly, without the PO.
- Decisions for the PO are numbered, each with your recommendation and why. The PO
  answers by number, or writes "ok go" to apply every recommendation.
- Ask the PO product questions only. Decide technical questions yourself and record
  them where the next reader will find them. Settle facts by measuring.
- If it changes the issue or the plan, it goes on the issue, as a comment.
- When you receive work, from the PO or by a message from another session, your
  first action, before reading or doing anything else, is a pick-up event:
  ~/.local/bin/taelron-trace with --event R --name "Picked up", --ball set to your
  role, the phase and step you are starting, and in --next what you are about to do.
- Send each trace event your role lists, when its step completes, with
  ~/.local/bin/taelron-trace (product, milestone and issue come from your
  handoff prompt). A failed send never blocks the work: say so in your handoff
  prompt and carry on.

## Hard rules

- **Never author a Linear document or ADR.** Your Linear writes are issue comments,
  plus review-gated `spec-drift` issue creation. ADRs are Web Claude's.
- **Never silently diverge from an ADR or the SPEC.** Stop, surface, open a
  `spec-drift` issue, resume only once it is resolved.
- **Evidence, not assertion.** "Tests pass" is not verification; the command and its
  output is.
- **An accepted deviation is a decision, not a defect.** Design Notes records them;
  read it before reporting a structural finding, and do not reopen one without a new
  decision.

## Architecture

`internal/` is organised by concern — `app` is the Bubble Tea UI, with `config`,
`ssh`, `azure`, `k8s`, `probes`, `notes`, `fspath` and `logging` as backends. This
is a **deliberate deviation** from the baseline's four-layer hexagonal split,
recorded in Design Notes. Do not "fix" it.

Invariants that must keep holding:

- Backend packages carry **zero** Bubble Tea / Lipgloss imports.
- Dependency direction is one-way: `app` → backends, never the reverse. No cycles.
- Each view: fetch function → message type → model handler → render function.
- **Action Engine** — generic transition system (poll/oneshot) using closures. The
  engine must NOT switch on resource type; callers set closures for execution,
  polling, refresh and state detection. If a new backend requires editing the engine
  core, the abstraction is broken. The Model is a value type — closures that mutate
  model state capture a stale snapshot.

Structural deviations already accepted are in Design Notes. Read it rather than
re-deriving them.

## Build & test

```bash
make verify      # lint + build + test — required before PR
make regression  # verify + the verification controls — what CI runs
make help
```

The verification handoff in a PR description is an ordered list of **Make targets**,
each with its purpose and expected result, runnable top to bottom without inference.
Where no target exists for what needs verifying, create one as part of the
implementation. `make verify` is the pre-PR gate; CI runs `make regression`, so CI
green is strictly stronger than `verify` green. `make regression` locally is the
closest local reproduction of CI, not an identical claim: `verify-fresh-clone` and
`verify-lint-baseline` clone the last commit, not the working tree, so uncommitted
changes are invisible to them locally exactly as they would be to a CI checkout of
an unpushed branch.

Testing tiers: **Unit** (parsers, formatters, config) and **UI** (navigation, key
bindings, state transitions, rendering — via `teatest`) run in CI. **Integration**
(real SSH / Azure / K8s) is manual, tracked in Linear.

A disposable sshd/sudo test host for manual integration checks (TAE-98) lives
under `test/testhost/` — see its README.md; run with `make testhost-up`.

`teatest` UI tests drive a real `tea.Program`:

- Always set an initial term size; use `WaitFor` with a 2s duration; `WaitFinished`
  with a 2s timeout after the quit key.
- Construct `Model` via a helper that sets `AppConfig.FleetDir` to a non-empty
  placeholder — an empty `FleetDir` triggers the first-run wizard.
- Prefer `bytes.Contains` on raw output over golden files until a view's rendering
  is stable.
- Canonical example: `internal/app/teatest_baseline_test.go`.

## Security

General threat model: Security & Secret Handling baseline. FleetDesk specifics:

- Sanitize user input before shell execution (SSH, `kubectl`, `az`).
- No secrets in code, error messages, logs or debug output; passwords never cached,
  logged or persisted beyond the session.
- SSH auth: try each method individually to avoid `MaxAuthTries` exhaustion
  (ADR-F003).
- Sudo passwords go to `sudo -S` on **stdin, never argv** (ADR-F004).
- API keys from `$LINEAR_API_KEY` / `$ANTHROPIC_API_KEY` — never in code.
- Flag security implications during review even when the plan does not mention them.

Known security findings are open issues. Read them before reporting one as new.

## Git

One PR per issue, squash merge. Branch `feature/tae-NN-description`. Conventional
commits (feat / fix / chore / refactor). PRs open as **drafts** and become ready only
after Gaetan has verified them. CC never merges.

The module path is what `go.mod` says. If an open issue tracks changing it, that
issue owns the change — do not do it ad hoc.

## Linear access

`curl` + GraphQL with `$LINEAR_API_KEY`, not MCP. Fetch documents and issues by ID
or slug — `list_documents` output is truncated.
