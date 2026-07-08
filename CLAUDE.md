# FleetDesk — Project Instructions

Go TUI (Bubble Tea) for managing fleets of Linux VMs over SSH, Azure resources via the `az` CLI, and Kubernetes clusters via `kubectl`.

## Source of truth

Linear is canonical for all design content — workspace **Taelron**. This file points at it; it does not duplicate it. When documents disagree, resolution order is **ADRs > Domain Model > Specification > README**.

Read the applicable documents at the **start of every session** — the Linear copy is more current than any cached context. Fetch full text via `curl` + `$LINEAR_API_KEY` (not the Linear MCP — token cost); fetch by document ID (`list_documents` is truncated).

**Taelron baselines** (Taelron Baselines project — apply to every product):

- [AI Workflow](https://linear.app/taelron/document/ai-workflow-805fb2002fce) — collaboration model, session startup, review gates
- [Delivery Workflow](https://linear.app/taelron/document/delivery-workflow-edab9d0993e8) — milestone/issue rhythm, verification handoff, drift handling
- [Hexagonal Architecture](https://linear.app/taelron/document/hexagonal-architecture-b142001f420e) — layer structure, port/adapter contract
- [TUI Go Conventions](https://linear.app/taelron/document/tui-go-conventions-1aca4ef63a66) — Bubble Tea, async, context lifecycle
- [UI Patterns](https://linear.app/taelron/document/ui-patterns-9c3982a46ef2) — TUI design patterns

**FleetDesk product docs** (FleetDesk project):

- [FleetDesk SPEC](https://linear.app/taelron/document/fleetdesk-spec-25e34eb61c20)
- [FleetDesk ADR Index](https://linear.app/taelron/document/fleetdesk-adr-index-877f77ec6d93)
- [FleetDesk Roadmap](https://linear.app/taelron/document/fleetdesk-roadmap-6c87af1c8252)
- [FleetDesk Business Model](https://linear.app/taelron/document/fleetdesk-business-model-c01edf4d8e03)
- [FleetDesk UI](https://linear.app/taelron/document/fleetdesk-ui-31a471474d2c)

## Phase & workflow

FleetDesk is in **Phase 2 — refinement** (per @AI Workflow): incremental, issue-driven work; new ADRs are rare; the product evolves rather than emerges.

Every non-trivial change runs through the **two-session `/feature-dev` split** against a single Linear issue (per @Delivery Workflow) — **not** stock `/feature-dev`:

```
/feature-dev-plan  FLE-N     # session 1: explore + design → post plan to the Linear issue → stop
/feature-dev-build FLE-N     # session 2: implement the approved plan → in-session review → PR
```

The plan-approval pause is structural: implementation lives only in `/feature-dev-build`, which the human starts as a separate session after reviewing the plan posted on the issue. The PR carries a verification handoff (ordered Make targets, purpose, expected result). Claude reviews the PR (advisory); the human verifies behavior locally and merges. CC never merges.

## Architecture (FleetDesk-specific)

- Packages: `internal/config/`, `internal/ssh/`, `internal/azure/`, `internal/k8s/`, `internal/app/`.
- Backend packages (`ssh/`, `azure/`, `k8s/`) must NOT import Bubble Tea — pure data-fetching and parsing (general rule: @Hexagonal Architecture).
- Each view: fetch function → message type → model handler → render function.
- **Action Engine** — generic transition system (poll/oneshot) using closures. The engine must NOT switch on resource type; callers set closures for execution, polling, refresh, and state detection. If a new backend requires editing the engine core, the abstraction is broken. The Bubble Tea Model is a value type — closures that mutate model state capture a stale snapshot.

## Build & test

```bash
make check    # build + test + lint (required before PR)
make build
make test
```

Testing tiers: **Unit** (parsers/formatters/config) and **UI** (navigation, key bindings, state transitions, rendering — via `teatest`) run in CI; **Integration** (real SSH/Azure/K8s) is manual, tracked in Linear.

`teatest` UI tests drive a real `tea.Program`:

- Always set an initial term size; use `WaitFor` with a 2s duration; `WaitFinished` with a 2s timeout after the quit key.
- Construct `Model` via a helper that sets `AppConfig.FleetDir` to a non-empty placeholder (empty FleetDir triggers the first-run wizard).
- Prefer `bytes.Contains` on raw output over golden files until a view's rendering is stable.
- Canonical example: `internal/app/teatest_baseline_test.go`.

## Security (FleetDesk-specific)

General secret/config threat model: @Security & Secret Handling. FleetDesk specifics:

- Sanitize user input before shell execution (SSH, `kubectl`, `az`).
- No secrets in code, error messages, logs, or debug output; passwords never cached/logged/persisted beyond the session.
- SSH auth: try each method individually to avoid MaxAuthTries exhaustion.
- API keys from `$LINEAR_API_KEY` / `$ANTHROPIC_API_KEY` — never in code.
- Flag security implications during design review even when the plan doesn't mention them.

## Git

One PR per issue (squash merge). Branch `feature/fle-NN-description`. Conventional commits (feat/fix/chore/refactor).

## Linear access

`curl` + GraphQL with `$LINEAR_API_KEY` (not MCP). Fetch documents and issues by ID.
