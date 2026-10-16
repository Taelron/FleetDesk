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
text via `curl`, with the key from `pass` (see *Linear access* in the workflow section; not the Linear MCP — token cost). The GraphQL
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

<!-- taelron:workflow:start -->
## Taelron AI Build Workflow

Every Taelron product runs the
[AI Build Workflow](https://linear.app/taelron/document/ai-build-workflow-bedca8de2907),
version 2. It is canonical; this section is a summary. Re-read it at session start,
and where this section and it disagree, it wins. Your role prompt says which steps
are yours.

### Roles

| # | Name            | Runs on                  | Model    | Role                            |
|---|-----------------|--------------------------|----------|---------------------------------|
| 1 | PO              | Human                    | —        | Product decisions, `go`, merge  |
| 2 | Systems Analyst | Claude Desktop           | Opus 5   | Design, Milestone Plan, close   |
| 3 | Orchestrator    | Claude Code, own clone   | Opus 5   | Runs the milestone              |
| 4 | Architect       | Claude Code              | Fable 5  | Issue check, Implementation Plan |
| 5 | Tester          | Claude Code              | Sonnet 5 | Acceptance tests, from the issue |
| 6 | Dev             | Claude Code              | Sonnet 5 | Code, push, draft PR, brief     |
| 7 | Reviewer        | Claude Code              | Opus 5   | Fresh-context review of a diff  |
| 8 | Verifier        | Claude Code              | Sonnet 5 | Drives the built binary         |

2. Owns design, ADRs, the Milestone Plan and its issues, amendments that change
   what the product promises, and the milestone close. Writes Linear.
3. Runs outside the product repository, from Linear and a read-only clone. Starts
   each role, reviews the Implementation Plan, posts the APPROVED PLAN, settles
   technical disputes and conflicts, routes open items with the PO, writes the
   milestone report. Never runs the Architect's model.
8. Fixtures per fleet type. Reports; never fixes.

### Rules for every phase

- **The PO links the two sides only at the boundary of a milestone.** One paste in
  (Systems Analyst → Orchestrator), one paste out (Orchestrator → Systems Analyst).
  Inside a milestone the Orchestrator carries every handoff, and Claude Code
  sessions message each other directly with `SendMessage`, addressed by session
  name (`<product>-<role>`).
- **The PO answers in one pane, the Orchestrator's.** Every question, the plan
  approval, the merge and the routing reach the PO there; the PO types the answer.
- **Promises and design go to the Systems Analyst.** An amendment that adds, removes
  or weakens an acceptance criterion, or a question about a design decision, leaves
  the milestone through the PO: one paste out, one paste back.
- **A handover across the boundary ends with a prompt.** Details go to Linear first,
  then a short, ready-to-paste prompt: what changed, and where to read it.
- **Decisions for the PO are numbered, each with a recommendation and why.** The PO
  answers by number, or `ok go` to apply every recommendation.
- **The PO is asked product questions only:** what the user sees, what the product
  promises, scope, licence, money, anything irreversible. Anything derivable from a
  baseline, a document or the machine is decided by the session that meets it, and
  recorded where the next reader finds it. A disagreement on a fact is settled by
  measuring.
- **If it changes the issue or the plan, it goes on the issue.** The terminal is for
  conversation; Linear is the record.
- **Comments post directly.** Issue checks, decisions, plans, reviews, the APPROVED
  PLAN, briefs, routes, the milestone report, and an amendment that corrects a fact
  or names an interface. A change to what the product promises, a product
  document, a baseline, or a milestone's scope or exit criterion is shown to the PO
  first, by the Systems Analyst.
- **Two rounds, then escalate.** Every loop stops after two rounds without agreement
  and goes to the PO, through the Orchestrator.
- **One ball.** A session starts a step only when it holds the ball; no two
  sessions hold it at once.
- **No PR merges on red CI.** The Orchestrator checks the PR's CI before it asks
  the PO to merge, and `main`'s CI after the merge.
- **Fact or promise: when in doubt, it is a promise.** An amendment whose effect on
  what the product promises is unclear goes to the Systems Analyst.

### Phase rules

- **A · Planning** (PO, Systems Analyst). One issue per milestone unless a named
  reason forces a split. The plan says what, never how. Nothing is written to Linear
  before the PO validates the plan. The milestone is locked once created. The issue
  names the interface its tests rely on, the repository, the issue branch and the
  source of the toolchain versions.
- **B · Implementation planning** (Orchestrator, Architect). The Architect session
  is fresh for each milestone, when the PO starts the layout; between issues the
  Orchestrator's start prompt names the new issue and its sources. The issue check
  is posted before any amendment it argues for; the decisions before the plan. The
  Architect asks product questions and decides technical ones. The plan is a comment
  titled `Implementation Plan, revision <n>`. A promise change goes to the Systems
  Analyst only once the plan review loop has closed. After the PO's `go`, the
  Orchestrator posts `APPROVED PLAN` with the plan in full. No code is written in
  this phase.
- **C · Build** (Orchestrator, Tester, Dev, Reviewer, Verifier). The Tester and Dev
  share one folder and one branch; the Verifier builds from a clean clone; the
  Orchestrator reads its own clone and writes none. The Tester writes tests from the
  issue alone and commits the red run, taken from a clean copy of its commit; it
  passes the APPROVED PLAN link to Dev with "tests are red", unread. A Tester that
  finds a weakness in its own tests later tells the Orchestrator, which holds Dev.
  Dev never modifies an acceptance test: a dispute goes to the Orchestrator. The
  Reviewer gets the branch's diff against `main`, the Tester's commits included,
  and the issue, never the PR. The Verifier gets the branch, the PR URL, the issue
  and the plan, never the PR body; it reports and never fixes. On disagreement the
  acceptance test wins. Nothing is pushed before the tests and the code; the PR is
  a draft carrying the verification handoff (ordered Make targets, purpose,
  expected result). The brief is a comment on the issue and gates the merge. The
  PO verifies, marks the PR ready and merges, once its CI is green.
- **A post-merge fix** is always a new issue in the milestone: the Orchestrator
  creates it once the PO has routed it, the role that owns the files makes the
  change (the Tester for an acceptance test, Dev otherwise), and it is traced
  through Phase C steps 13 to 21.

### Tracing: pick-up first, then the listed events

- **Pick-up rule.** The moment you receive work, from the PO or from another
  session, your first action, before reading or doing anything else, is the `R`
  event, with `--ball` set to yourself:
  `~/.local/bin/taelron-trace --product <product> --milestone <milestone> --issue <issue> --phase <phase> --event R --step <step> --from <you> --ball <you> --next '<what you do now>'`.
  This covers every handoff, including each round of a loop.
- **A started event is the pick-up for its step.** Where your step opens with a
  started event (A·T1, B·T1, C·T1), send it instead of `R`.
- **Listed events.** Send the events your role prompt lists, only once their step
  is complete. `taelron-trace --help` lists every event; it refuses any other.
  Never invent one. `--link` is optional: the PR or Linear URL the event points to.
- **The `next` rule.** `--next` is what the ball-holder does next, in a few words.
- **A refused event is never silent.** When `taelron-trace` cannot send, it prints
  `taelron-trace: event NOT sent: <reason>` on stderr and exits non-zero. Fix the
  cause and send the event again.

### Linear access

Claude Code reads and writes Linear with curl against
`https://api.linear.app/graphql`, never the Linear MCP. Every role that reads
Linear takes the key from the `pass` entry `taelron/linear/api-key` at each call
and passes the header on a file descriptor:
`-H @<(printf 'Authorization: %s\n' "$(pass show taelron/linear/api-key | head -n1)")`.
The key never goes in a file, an exported variable, argv or shell history.
Fetch documents in full with `document(id:)`, which accepts the URL slug; a list
response is truncated. Writes are issue comments and review-gated `spec-drift`
issues; the Orchestrator may also apply an amendment that corrects a fact or names
an interface to the issue itself, and creates the issue of a routed post-merge
fix. The Reviewer has no Linear access, by design.

### Session read order

1. [AI Build Workflow](https://linear.app/taelron/document/ai-build-workflow-bedca8de2907)
   — participants, models, the three phases.
2. [AI Workflow](https://linear.app/taelron/document/ai-workflow-805fb2002fce)
   — surfaces, author capability, the Linear write gate, phased delivery.
3. [Delivery Workflow](https://linear.app/taelron/document/delivery-workflow-edab9d0993e8)
   and whichever other baselines apply to the task.
4. The product documents: Domain Model, ADRs, Specification, Technical Design or
   Design Notes.
5. The issue you were given, its milestone and its Milestone Plan.

Before offering a decision, open the document that would record it.
<!-- taelron:workflow:end -->

## Hard rules

- **Never author a Linear document or ADR.** Your Linear writes are issue comments,
  plus review-gated `spec-drift` issue creation. ADRs are the Systems Analyst's.
- **Never silently diverge from an ADR or the SPEC.** Stop, surface, open a
  `spec-drift` issue, resume only once it is resolved.
- **Evidence, not assertion.** "Tests pass" is not verification; the command and its
  output is.
- **An accepted deviation is a decision, not a defect.** Design Notes records them;
  read it before reporting a structural finding, and do not reopen one without a new
  decision.

## Architecture

`internal/` is organised by concern — `app` is the Bubble Tea UI, with `config`,
`credential`, `ssh`, `azure`, `k8s`, `probes`, `notes`, `fspath` and `logging` as backends. This
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
- API keys from `pass` (Linear: `taelron/linear/api-key`) or `$ANTHROPIC_API_KEY` — never in code.
- Flag security implications during review even when the plan does not mention them.

Known security findings are open issues. Read them before reporting one as new.

## Git

One PR per issue, squash merge. Branch `feature/tae-NN-description`. Conventional
commits (feat / fix / chore / refactor). PRs open as **drafts** and become ready only
after Gaetan has verified them. CC never merges.

The module path is what `go.mod` says. If an open issue tracks changing it, that
issue owns the change — do not do it ad hoc.

## Linear access

`curl` + GraphQL with the key from `pass` (see the workflow section), not MCP. Fetch documents and issues by ID
or slug — `list_documents` output is truncated.
