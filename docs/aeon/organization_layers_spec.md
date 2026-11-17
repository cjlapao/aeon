# Organization Layers Spec

Oct 27, 2026 · @Carlos Lapao

## Purpose and how to use this doc

This spec adds the organization layers (Workspace, Project, Component, Epic, Goal, Run, Task) on top of the existing agentic workflow engine. It defines how work is grouped, who approves what, and how automation enters the system. It does not describe the engine's internals.

The Go snippets are examples of shape and relationships only. They are not final field names, persistence schemas or package layout. If the codebase already has an equivalent type, adapt it and keep the rules.

Rules are written as MUST / MUST NOT. The last section is a status matrix of what this spec defines and what it leaves open, so a building agent knows what to implement, what to ask about, and what not to invent.

## Hierarchy overview

Work is organized as a tree of containment with links across it. A Run belongs to a Component, and a Goal reaches its Runs by link, so one Goal can span every Component while each Run stays inside one repo.

&#91;embedded content: organization layers · containment and links\]

Triggers belong to a Project or Component, and Policies attach at Project, Epic, Component or Trigger scope, so neither is drawn. A project-scope Run, such as a brainstorm, has no Component and reads across all of them.

## Entities at a glance

Containment decides where something runs; links decide how it is grouped. A Run is contained by a Component, and only linked to a Goal.

| Entity | Contained by | Linked to | Lifecycle | Review gate |
| --- | --- | --- | --- | --- |
| Workspace | none (root) | none | persistent app shell | no |
| Blueprint | Workspace | none | versioned; system ones immutable | no |
| Project | Workspace | none | persistent; groups components | no |
| Component | Project | none | persistent; one repo | no |
| Epic | Project | Goals (`goal.epic_id`) | status derived from its Goals | no |
| Goal | Project | Epic (optional), Runs | `review_state` stored, `execution_state` derived | always |
| Run | Component, or Project for project-scope runs | Goal (optional), Blueprint at a pinned version | run state machine | per Blueprint gate stages |
| Task | Run | none | lives and dies with its Run | no |
| Brief | Run | Goal (optional) | `review_state` stored | `brief_review` policy |
| Trigger | Project or Component | Blueprint | enabled or disabled | no |
| Policy | Project, Epic, Component or Trigger | gate type | replaced as a whole | n/a |

## Workspace, Project and Component

A Workspace is the app shell. A Project groups the Components of one product (for example backend, dashboard, agent and app), and a Component is one repo where agents actually work.

- A Component MUST belong to exactly one Project and map to exactly one repo.
- A Component owns everything execution needs: folder, default branch, git tracking of changed files, commands, agent settings. Concurrency on the same repo (worktree allocation, one run at a time on the default branch) is a Component concern.
- Single-repo users get one Project and one Component created implicitly when they connect a repo, named after the repo. The UI hides any layer that has a single child. The data model never collapses, so growing from one repo to four needs no migration.
- A quick Run can start directly on a Component with no Goal and no Epic.

Project and Component also carry presentation metadata: a `ShortDescription` (one line, for lists and cards), a `Description` (full text) and an `Icon`. An Icon is either a single emoji or an embedded base64 image. It MUST NOT have both set.

```go
type Workspace struct {
	ID   string
	Name string
}

type Project struct {
	ID               string
	WorkspaceID      string
	Name             string
	ShortDescription string // one line, for lists and cards
	Description      string // full text
	Icon             Icon
	Implicit         bool // auto-created for a single repo; UI hides the layer
	Config           ProjectConfig
}

type Component struct {
	ID               string
	ProjectID        string
	Name             string // "backend", "dashboard", "agent", "app"
	ShortDescription string
	Description      string
	Icon             Icon
	Repo             RepoRef
	Config           ComponentConfig
}

// Icon is an emoji or an embedded image. At most one field is set.
type Icon struct {
	Emoji string // a single emoji
	Image string // base64 data URI, e.g. "data:image/png;base64,..."
}

type RepoRef struct {
	Path          string
	DefaultBranch string
	BranchPrefix  string
}
```

## Epic and Goal

A Goal is a proposal that must be reviewed before anything runs; an Epic is an optional container that groups Goals inside a Project.

**Epic rules**

- An Epic belongs to one Project and has no review gate. Its Goals carry the gates.
- Epic status is derived from its Goals and never stored. Cost, time and effort roll up the same way.
- An optional budget caps spending: policy limits can stop auto-approval when the Epic's remaining budget is too low.

**Goal rules**

- Every Goal MUST pass `draft → in_review` and be decided there (`approved`, `rejected` or `parked`), whatever created it. No path may jump to `approved`.
- Auto-approval is a review decision made by a policy actor, recorded with the rule that matched and the input values. It is never a skipped review.
- Runs MAY start only from an `approved` Goal, and reference the Goal version that was approved.
- A material edit to an approved Goal (body, components, estimates) increments `Version` and returns it to `in_review`.
- `ExecutionState` (`not_started`, `running`, `blocked`, `done`, `failed`) is derived from the Goal's Runs. It MUST NOT be stored or set by hand.
- Estimates and confidence live per component, with rollups: cost is the sum, confidence is the lowest.
- `Source.Untrusted` is derived from `Source.Kind` (true for webhook-originated Goals), never set by the agent.

```go
type Epic struct {
	ID, ProjectID string
	Title         string
	Objective     string // business outcome, not a task list
	Owner         UserRef
	Stakeholders  []UserRef
	TargetDate    *time.Time
	BudgetUSD     *float64
	// status: derived from Goals
}

type Goal struct {
	ID, ProjectID string
	EpicID        *string // link, not containment
	Version       int
	Title, Body   string // Body is filled from the goal output template
	Type          GoalType
	Source        Source
	Components    []GoalComponent
	ReviewState   ReviewState
	Review        *Review
	CreatedBy     Actor
	// ExecutionState: derived from Runs
}

type GoalType string // bug | feature | chore | refactor | investigation

type Source struct {
	Kind      string // manual | brainstorm | webhook | cron
	Ref       string // e.g. "github:org/repo#123"
	RunID     *string
	TriggerID *string
	Untrusted bool // derived from Kind
}

type Confidence int // ConfLow < ConfMedium < ConfHigh

type GoalComponent struct {
	ComponentID string
	Rationale   string
	EstimateUSD float64
	Confidence  Confidence
	Basis       []string // reproducible, root_cause_known, single_component, ...
}

type ReviewState string // draft | in_review | approved | rejected | parked

type Review struct {
	Decision   ReviewState
	By         Actor // Kind: user | policy | agent
	Reason     string
	PolicyEval *PolicyEval // set when By.Kind == "policy"
	DecidedAt  time.Time
}

type Actor struct{ Kind, ID string }
```

## Run and Task

A Run is one execution of a Blueprint, and the unit that does the work. It runs inside a Component (or at Project level) and is only linked to a Goal. This is the entity currently called a loop.

- A Run has a scope. A **component-scope** Run MUST have a `ComponentID`; it gets a worktree, a branch, and file-change tracking, and reads that Component's config. A **project-scope** Run (brainstorm, impact analysis) has read-only access to all the Project's Components, no worktree, and its only writes are artifacts such as Goals, Epics and reports.
- A Run MUST pin the Blueprint version it started with. Editing a Blueprint never changes Runs in flight.
- `GoalID` is optional. A quick Run has none, and a standalone Run MAY be attached to a Goal later.
- A Goal normally has one Run per affected Component, spawned when the Goal is approved. Runs may start at different times or in parallel.
- A Task belongs to a Run and inherits its Component, repo and branch.
- Cost, time and effort are recorded per Run and Task. Every rollup (Goal, Epic, Component, Project, Blueprint) is a group-by over these records, not separate stored totals.

**Run states:** `queued → running → waiting → running → completed | failed | cancelled`. A Run is `waiting` for a dependency or for a human gate, and `WaitReason` says which. While waiting on a dependency it releases its agent slot and worktree and accrues no cost. `Stale` is a flag, not a state: see the Wait section.

```go
type RunScope string // "component" | "project"
type RunState string // queued | running | waiting | completed | failed | cancelled
type WaitReason string // dependency | gate

type Run struct {
	ID, ProjectID    string
	Scope            RunScope
	ComponentID      *string // required when Scope == "component"
	GoalID           *string // link; may be nil, may be set later
	GoalVersion      *int
	BlueprintID      string
	BlueprintVersion int // pinned
	Input            RunInput // prompt, goal ref or none, per Blueprint.Input
	Params           map[string]string // run-level param values
	State            RunState
	WaitReason       *WaitReason
	CurrentStageID   string
	DependsOn        []Dependency
	Stale            bool
	Cost             CostTotals
}

type Task struct {
	ID, RunID, StageID string
	Title              string
	State              string
	Cost               CostTotals
}
```

## Brief

A Brief is the longer, rule-bearing form of a Goal for one Component, produced by an agent stage early in a Run and reviewed before implementation starts.

- One Brief per (Goal, Component) pair, owned by the Run that produced it. `GoalID` is nil for quick Runs.
- The Brief has its own review gate (`brief_review` in the policy model), with the same states and review record as a Goal.
- Quick Runs synthesize a Brief from the prompt, and the default policy for that case is `approve`.
- A material edit increments `Version` and returns the Brief to `in_review`.
- `Contracts` reference artifacts shared between Runs, such as an API spec published by the backend Run. `DependsOn` feeds the Wait mechanism.

```go
type Brief struct {
	ID, RunID, ComponentID string
	GoalID                 *string
	Version                int
	Body                   string // filled from the brief output template
	Scope, OutOfScope      string
	AcceptanceCriteria     []string
	Constraints            []string // conventions, do-not-touch paths
	TestPlan               string
	DependsOn              []Dependency
	Contracts              []ArtifactRef
	ReviewState            ReviewState
	Review                 *Review
}
```

## Blueprint and stages

A Blueprint is a workflow definition, owned by the Workspace and available to every Project and Component. Runs are its instances.

- **System** Blueprints ship with the app and are immutable. Users clone them into **user** Blueprints they can edit. A clone records its source and version.
- A Blueprint cannot be restricted to a Project. Availability is app-wide. What it needs to run is separate: `RunScope` says whether a Run needs a Component (worktree, repo) or only Project context, and the UI only offers valid Blueprints.
- `Mode` is `autonomous` or `interactive`. Interactive Blueprints (brainstorm) present a chat, and finish when the output templates are filled (Goal and Brief).
- Blueprints are parameterized. Stages reference params like `{test_cmd}`, supplied by Component and Project config. Structure belongs to the Blueprint, environment to the config.
- The retry loop is expressed through transitions: a failing `check` routes back to an `agent` stage up to `RetryMax`.

| Stage kind | Purpose |
| --- | --- |
| `agent` | Autonomous work that produces artifacts |
| `conversation` | A human chats with the agent; exits when the output template is filled |
| `gate` | Review or approval, resolved by a user or by policy |
| `check` | Automated command such as `{test_cmd}` or `{lint_cmd}`; failure routes back |
| `spawn` | Creates Runs per affected Component when a Goal is approved |
| `wait` | Blocks until another Run or artifact is ready |

```go
type Blueprint struct {
	ID           string
	Name         string
	Version      int
	Origin       string // "system" | "user"; system is immutable
	ClonedFrom   *BlueprintRef
	RunScope     RunScope // component | project
	Mode         string   // autonomous | interactive
	Input        string   // none | prompt | goal
	Outcome      string   // pr | goals | epic+goals | report
	Params       []ParamDef
	Stages       []Stage
}

type Stage struct {
	ID        string
	Kind      string // agent | conversation | gate | check | spawn | wait
	Config    map[string]any // kind-specific: agent, prompt ref, tools, output template, wait spec
	Outputs   []string       // artifact types produced
	OnSuccess string         // next stage ID, or "end"
	OnFailure Failure
}

type Failure struct {
	RetryMax int
	Then     string // stage ID, or "fail"
}
```

## Policy: approval gates

A Policy decides how a gate is resolved: by a person, automatically, or conditionally. Gates are `goal_review`, `brief_review` and `pr_merge`.

- A Policy is set at Project, Epic, Component or Trigger scope. The most specific scope wins and **replaces** the whole Policy. Rules are never merged, so every decision is explainable by one rule list.
- Rules are ordered and the first match wins. `Default` is `manual` when omitted.
- A rule can only `approve` or send to `manual`. Rejection is always a human decision.
- Evaluation fails closed: a missing or malformed field, an unknown value or an exceeded limit means `manual`.
- Signals: Goal type, source kind and ref, author, labels, per-Goal confidence (ordinal: low < medium < high), estimated cost, number of affected Components. A multi-component Goal is evaluated on the worst values (lowest confidence, summed cost).
- If `Source.Untrusted` is true, a Policy MUST name explicit conditions such as an author or repo allowlist. A bare `auto` on an untrusted source is invalid.
- Every auto-approval is recorded as a `Review` with `By.Kind == "policy"` and a `PolicyEval` snapshot (rule ID, inputs, whether it defaulted to manual).
- Limits cap the automated path: per-Goal cost, auto-approvals per day, concurrent auto Runs, and remaining Epic budget.

```go
type Gate string // goal_review | brief_review | pr_merge

type Policy struct {
	ID     string
	Gate   Gate
	Scope  PolicyScope // project | epic | component | trigger, plus owner ID
	Rules  []Rule
	Limits Limits
	// Default is always manual
}

type Rule struct {
	ID   string
	When Conditions
	Then string // "approve" | "manual"
}

type Conditions struct {
	Types           []GoalType
	Source          *SourceMatch // kind + ref glob
	AuthorIn        string       // allowlist name
	MinConfidence   Confidence
	MaxEstimateUSD  *float64
	MaxComponents   *int
}

type Limits struct {
	MaxCostPerGoalUSD       *float64
	MaxAutoApprovalsPerDay  *int
	MaxConcurrentAutoRuns   *int
	RespectEpicBudget       bool
}

type PolicyEval struct {
	PolicyID, RuleID    string
	Inputs              map[string]any // snapshot at decision time
	DefaultedToManual   bool
}
```

## Triggers

A Trigger starts a Run from a webhook or a cron schedule, through the same path as a Run started in the UI. Triggers are what enable fully automated workflows.

- A Trigger belongs to a Project, optionally pinned to a Component. A webhook resolves its target Component through a repo-to-Component mapping.
- It references a Blueprint, either pinned to a version or following `latest`, and an input mapping from the event payload to the Run input.
- Webhook-originated input is untrusted: treated as data for the agent, never as instructions, and the Run gets narrower permissions (no secrets, restricted tools).
- Idempotency: a `DedupeKey` extracted from the payload (for example an issue ID) prevents duplicate Goals from repeated events.
- `Limits` bound runs per hour and cost per day.
- Auth stores a **reference** to the secret in the secret store. The secret value is never stored in the Trigger.
- A Trigger MAY carry a Policy override, which is the most specific Policy scope.

```go
type Trigger struct {
	ID, ProjectID string
	ComponentID   *string
	Source        string // "webhook" | "cron"
	Match         EventMatch // event type and filters, e.g. issues.opened + labels
	Schedule      string     // cron expression, cron only
	BlueprintID   string
	PinVersion    *int       // nil = follow latest
	InputMapping  map[string]string // run input field -> JSONPath into the payload
	PolicyID      *string
	Limits        TriggerLimits
	SecretRef     string // reference only, never the value
	Enabled       bool
}

type TriggerLimits struct {
	MaxRunsPerHour   int
	MaxCostPerDayUSD float64
	DedupeKey        string // JSONPath, e.g. "$.issue.id"
}
```

## Config inheritance

Blueprints say what happens; Project and Component config say how, in that environment. A Run's effective config resolves in this order, most specific last: Blueprint defaults, Project config, Component config, Run input.

- A Blueprint declares its parameters with `ParamDef`. Only declared params can be set by config layers; undeclared keys are ignored.
- `SettableAt` limits which layers may set a param. `Locked` makes it non-overridable.
- Stages reference params by interpolation, such as `{test_cmd}`. An unresolved required param fails the Run at start, not mid-way.
- Secrets are always references, never values.
- Policies resolve separately, by the replace-whole rule in the Policy section.

```go
type ParamDef struct {
	Name       string
	Type       string // string | int | bool | list
	Required   bool
	Default    *string
	SettableAt []string // "project" | "component" | "run"
	Locked     bool
}

type ProjectConfig struct {
	DefaultBlueprintID *string
	Policies           []string // policy IDs
	BudgetUSD          *float64
	Conventions        []string // shared prompt snippets
	Params             map[string]string
}

type ComponentConfig struct {
	Commands    Commands // test, lint, build
	Conventions []string
	Agent       AgentSettings // model, tool allowlist
	Params      map[string]string
}
```

## Wait and run dependencies

A `wait` stage blocks a Run until another Run reaches a point or an artifact is published, which lets cross-component Goals such as SSO order their work.

- Dependency edges are declared per Run instance in `Run.DependsOn`, not in the Blueprint. The Blueprint only says where to wait. The `spawn` stage, or a user, supplies the edges.
- Runs and edges form a DAG. Cycles MUST be rejected when edges are created.
- A waiting Run releases its agent slot and worktree and accrues no cost. When it resumes it queues if another Run holds the Component.
- Place `wait` after the brief gate and before implementation, so dependent Runs can brief and be reviewed in parallel.
- A Goal's `ExecutionState` is `blocked` when all its remaining Runs are waiting on unmet dependencies.
- If an upstream artifact is republished at a new version after a downstream Run consumed it, the downstream Run gets `Stale = true`. It is not restarted automatically; a user or policy decides.
- Dependency failure follows `OnDependencyFailure`: `block`, `fail` or `notify`. Timeouts follow `OnTimeout`.

```go
type Dependency struct {
	RunID         *string
	Until         string  // "completed" | "pr_opened" | "stage:<id>"
	ArtifactTypes []string // e.g. "api_spec"
}

type WaitSpec struct {
	For                  []Dependency
	Timeout              time.Duration
	OnTimeout            string // notify | fail
	OnDependencyFailure  string // block | fail | notify
}

type ArtifactRef struct {
	Type    string
	RunID   string
	Version int
}
```

## Automation guardrails

Fully automated paths take external input and can approve and run code unattended, so these rules apply to every Trigger and every auto-approving Policy.

- Content from webhooks, emails, issues and files is data, never instructions to an agent.
- Auto-approval on an untrusted source requires explicit conditions (allowlisted authors or repos, labels, confidence and cost thresholds).
- Budgets and rate limits (per Goal, per Trigger, per day, per Epic) are enforced on actual spend at runtime, not just on estimates.
- Webhook events are deduplicated by `DedupeKey`.
- Runs from untrusted sources get restricted tools and no access to secrets.
- Agent-reported confidence is a soft signal and is only ever combined with hard conditions. Missing values fail closed to manual review.
- By default a person still merges the PR. Full autonomy through merge is an explicit opt-in on the `pr_merge` gate.
- Every automated decision is auditable through `PolicyEval`. Track outcomes of auto-approved Goals (rejected later, failed, reverted) against stated confidence per Blueprint to show whether confidence can be trusted.

## Status matrix: defined and open

This spec defines the organization layers and their rules. Everything marked Open or Partly defined needs a decision before it is built. Agents MUST ask rather than invent these parts.

| Area | Status | Notes |
| --- | --- | --- |
| Hierarchy and containment vs link rules | Defined | Workspace, Project, Component, Epic, Goal, Run, Task |
| Single-repo implicit Project and Component | Defined | UI hides single-child layers |
| Goal states, review invariant, versioning | Defined | Two axes: stored review, derived execution |
| Epic (project level) | Defined | Container, derived status, optional budget |
| Brief schema and gate | Defined | Per Goal and Component |
| Blueprint structure and stage kinds | Defined | `Stage.Config` payload per kind is not |
| Policy rule format and evaluation | Defined | Allowlist storage and management not defined |
| Trigger schema | Defined | Event match filter grammar not defined |
| Config inheritance and ParamDef | Defined | Concrete param catalog for system Blueprints not defined |
| Wait stage and dependencies | Defined | Worktree retention during a gate wait is Open |
| Run state machine | Partly defined | Transitions between states beyond the main path are not enumerated |
| Output templates for Goal and Brief | Open | Fields are listed; template content and format are not |
| System Blueprint catalog | Open | Feature, quick fix and brainstorm named; stages not specified |
| Dynamic spawning of extra Runs mid-flight | Open | Fixed set at spawn time, or discovered during planning? |
| Brainstorm output handling | Open | Goals always enter `in_review`; duplicate merging not specified |
| Naming of Run versus Loop | Open | Run (neutral) or keep Loop as the product word |
| Roles, permissions and access control | Open | Not specified; affects any later cross-project grouping |
| Cross-project grouping (workspace level) | Deferred | Epic stays at Project level for now |
| Verifier agent and confidence calibration | Deferred | Outcome tracking is specified, the verifier is not |
| UI views and analytics dashboards | Open | Derived rollups are defined; screens are not |
| Icon image limits | Open | Max size and allowed formats for the base64 image on Project and Component not specified |

---

## Questions raised by S38 (margin note — added 2026-10-27 at slice close; the rules above are not edited)

The spec's own Status matrix already names most gaps (allowlist management,
event-filter grammar, param catalog, worktree retention during gate-wait,
off-main-path transitions). S38's runs add — copied from
`S38-org-layers/investigation.md` Q10, plus the close-out:

1. A Brief is "owned by the Run" **and** unique per (Goal, Component) — a
   retried loop satisfies at most one reading until OL8's versioning; the
   uniqueness row should say `(goal_id, goal_version, component_root)`.
2. Review `by.kind` includes `agent`/`policy` while the invariant says
   rejection is always human — the shipped CHECK is `user`-only; widen only
   when an owner appears.
3. `execution_state`'s `blocked` is defined over *dependencies* no slice has
   built — S38 writes and tests it on synthetic input and says so in code
   (OL11).
4. The spec never says what a *completed* conversation-loop's status means —
   Q4's parking rule is the slice's answer; the spec should adopt or replace
   it.
5. `Goal.source` has `brainstorm`/`manual` but no actor for an **automation**
   creating a goal (S40 will need it — name it now to avoid a CHECK rebuild).

From the walk and the deletion drill (DEMO §S38 F1–F8): (6) the spec's
`task type` vocabulary and the ladder's status vocabulary are never listed
together — the CLI now refuses both by naming the lists (five types;
fourteen statuses, terminal `complete` — **there is no `done`**); a spec
table would stop implementations inventing words. (7) The spec has no door
for **registering a component from the CLI** — `loop create` does not
register one and no verb does; if the terminal is a first-class client (P34),
the component door belongs in the spec's surface list. (8) The catalog is
ONE definition with TWO doors; the drill showed the projection is
load-bearing for the human door too (seam types, registrations) — if the
spec ever allows the catalog to be lifted out, it should say what the human
door is registered by afterwards.
