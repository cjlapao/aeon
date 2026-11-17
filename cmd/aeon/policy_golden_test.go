package main

// ════════════════════════════════════════════════════════════════════════════
// S45 T1 Phase A — the characterization goldens for every `aeon policy` and
// `aeon proposal` verb.
//
// # What this file is for
//
// Phase B of T1 moves the effect registry, the seed-corpus runner, the
// per-kind validators and the policy boundary constructor out of `cmd/aeon`
// into a new `internal/desk`. It is a pure refactor: the CLI must print the
// same bytes and write the same rows afterwards. That claim is only checkable
// if it was pinned FIRST, against the untouched code, which is why this file
// lands as its own commit and must be green before a single line moves.
//
// Every case runs the REAL `cmd*` function (no subprocess — the same idiom as
// policy_test.go's `runPolicy` and proposal_test.go's `runProposal`) against a
// real temp store reached through `openStoreDirectly`'s own path, the
// `XDG_STATE_HOME` temp dir `agentHome` sets, so what is pinned is the verb's
// own printing and the rows its write boundary actually stored.
//
// # Two pins per case, never one
//
// Each case compares TWO golden files under `testdata/golden/`:
//
//   - `<name>.txt`       — the CLI's stdout, plus a `$ aeon …` line naming the
//     command that produced it and an `! …` line carrying the verb's returned
//     error text (a refusal is output an operator reads; pinning only stdout
//     would let a refusal silently become a success).
//   - `<name>.rows.txt`  — the resulting store state: every policy row (read
//     back through the repo bridge's own `ListAll`, so the fields as stored),
//     every proposal row, every `policy_event` row, plus whatever a case's
//     `extra` says the world looks like outside the store (an MCP config file,
//     for the two mcp kinds). The rows are the second pin on purpose: a verb
//     that prints the same bytes but writes different rows must fail, not pass.
//
// # Normalisation, in exactly one place
//
// `normalizeGolden` in policy_golden_normalize_test.go is the ONLY normalisation these tests perform:
// RFC3339 instants become `<TIME>` and store-assigned numeric ids become
// `<ID>`. Nothing else is rewritten — no trimming, no whitespace folding, no
// case folding, no dropping of lines nobody cares about. Where an instant is
// load-bearing rather than noise (a policy's `not_after`), the rows dump
// carries the same instant a second time as an epoch integer, which the
// normaliser does not touch, so the pin survives without weakening the rule.
//
// # Regenerating
//
// The goldens are the expected bytes, reviewed by eye once. To rewrite them
// (only when the CLI's output changed on purpose, in a later task, with the
// reason in that task's Notes):
//
//	go test -tags sqlite_fts5 ./cmd/aeon/ -run Golden -count=1 -golden-update
// ════════════════════════════════════════════════════════════════════════════

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/cjlapao/aeon/internal/mcp"
	"github.com/cjlapao/aeon/internal/paths"
	"github.com/cjlapao/aeon/internal/policy"
	"github.com/cjlapao/aeon/internal/store"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// goldenUpdate rewrites the expected bytes from what the CLI does today. It is
// the authoring tool, not a way to make a failing case pass: a Phase B that
// changed a byte has to be deliberately re-recorded, and the diff reviewed.
var goldenUpdate = flag.Bool("golden-update", false,
	"rewrite cmd/aeon/testdata/golden from the current CLI output instead of comparing against it")

// goldenInstant is the clock the FIXTURES use (never the verb's own — the verbs
// read time.Now themselves, which is what the <TIME> normaliser exists for). A
// fixed instant keeps a fixture's `not_after` and acceptance instants identical
// from run to run.
var goldenInstant = time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)

const goldenInstantRFC = "2026-10-20T12:00:00Z"

// goldenCase is one pinned scenario.
type goldenCase struct {
	// name is the golden-file stem; the table in
	// docs/aeon/S45-policy-management/tasks/T1.md names these.
	name string
	// setup stages the world (stored rows, proposals, an MCP config file)
	// through the SAME boundary and store APIs the verbs use.
	setup func(t *testing.T, st *store.Store)
	// steps are the CLI invocations, in order, against one store. Each element
	// is the argument list after `aeon` (`{"policy","list","--type",…}`). It is
	// a function so a step can name an id the setup created.
	steps func(t *testing.T, st *store.Store) [][]string
	// extra appends non-store world state to the rows dump (an MCP config
	// file's bytes are the two mcp kinds' "rows").
	extra func(t *testing.T) string
}

// runGoldenCases runs each case as its own subtest so one drifted verb names
// itself rather than taking the whole family down.
func runGoldenCases(t *testing.T, cases ...goldenCase) {
	t.Helper()
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			isolateStoreEnv(t)
			st := proposalHome(t) // agentHome's XDG_STATE_HOME temp dir + one real handle
			if c.setup != nil {
				c.setup(t, st)
			}
			var transcript strings.Builder
			for _, step := range c.steps(t, st) {
				fmt.Fprintf(&transcript, "$ aeon %s\n", strings.Join(step, " "))
				out, err := runGoldenVerb(t, step)
				transcript.WriteString(out)
				if err != nil {
					fmt.Fprintf(&transcript, "! %s\n", err.Error())
				}
			}
			assertGolden(t, c.name+".txt", normalizeGolden(transcript.String()))
			assertGolden(t, c.name+".rows.txt", normalizeGolden(goldenStoreState(t, st, c.extra)))
		})
	}
}

// isolateStoreEnv pins where a case's store lives. `appenv.Apply` sets
// DATABASE_SQLITE_STORAGE_PATH / _FILE_NAME with a process-wide os.Setenv
// (internal/appenv/appenv.go:94) every time a test boots the daemon in process,
// and store.resolveConfig honours those BEFORE XDG_STATE_HOME
// (internal/store/open.go:331-343) — so a golden that runs after such a test
// would otherwise open its neighbour's database and report its rows as a drift.
// That is the same root cause as this package's known environmental failures
// (T0's Notes); a characterization harness cannot inherit it, because the whole
// point of the pin is that the rows are the ones this case wrote. Clearing the
// three keys does not weaken the pin: the store still resolves through the real
// production resolver, only not through a variable an earlier test left behind.
func isolateStoreEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"DATABASE_SQLITE_STORAGE_PATH",
		"DATABASE_SQLITE_FILE_NAME",
		"DATABASE_SQLITE_DSN",
	} {
		t.Setenv(key, "")
	}
}

// runGoldenVerb dispatches to the real verb function, in-process.
func runGoldenVerb(t *testing.T, args []string) (string, error) {
	t.Helper()
	var b strings.Builder
	var err error
	switch args[0] {
	case "policy":
		err = cmdPolicy(&b, args[1:])
	case "proposal":
		err = cmdProposal(&b, args[1:])
	default:
		t.Fatalf("golden case asks for %q — only the policy and proposal verbs are pinned here", args[0])
	}
	return b.String(), err
}

// assertGolden compares against the checked-in bytes, or records them when
// -golden-update asked for it.
func assertGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", "golden", name)
	if *goldenUpdate {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir golden dir: %v", err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatalf("write golden %s: %v", path, err)
		}
		t.Logf("golden %s recorded (%d bytes)", path, len(got))
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v — a missing golden is not a passing one", path, err)
	}
	if got != string(want) {
		t.Errorf("golden %s drifted\n--- want ---\n%s\n--- got ---\n%s", path, string(want), got)
	}
}

// goldenStoreState dumps what the store (and, through extra, the world) looks
// like after the case's steps. Ordering is the queries' own (`ORDER BY id` for
// policy, `ORDER BY id DESC` for proposals, `at DESC, id DESC` for events),
// deterministic for a fresh store; nothing here sorts on its own.
func goldenStoreState(t *testing.T, st *store.Store, extra func(*testing.T) string) string {
	t.Helper()
	var b strings.Builder

	rows, err := policyRepo(st).ListAll(context.Background())
	if err != nil {
		t.Fatalf("dump policy rows: %v", err)
	}
	fmt.Fprintf(&b, "policy rows: %d\n", len(rows))
	for _, r := range rows {
		target := r.TargetKind
		if r.TargetID != "" {
			target = r.TargetKind + ":" + r.TargetID
		}
		notAfter := "none"
		epoch := "-"
		if r.NotAfter != "" {
			notAfter = r.NotAfter
			if at, perr := time.Parse(time.RFC3339Nano, r.NotAfter); perr == nil {
				epoch = fmt.Sprintf("%d", at.UTC().Unix())
			}
		}
		fmt.Fprintf(&b, "  policy %s type=%s target=%s action=%s source=%s subject=%s spec=%s not_after=%s not_after_epoch=%s created_at=%s\n",
			r.ID, r.Type, target, r.Action, r.Source, r.Subject, r.SpecJSON, notAfter, epoch, r.CreatedAt)
	}

	proposals, err := st.ListProposals("")
	if err != nil {
		t.Fatalf("dump proposal rows: %v", err)
	}
	fmt.Fprintf(&b, "proposal rows: %d\n", len(proposals))
	for _, p := range proposals {
		fmt.Fprintf(&b, "  proposal %d kind=%s status=%s created_by=%s:%s created_at=%s payload=%s decided_by=%s:%s decided_at=%s decide_reason=%s applied_error=%s\n",
			p.ID, p.Kind, p.Status, p.CreatedByKind, p.CreatedByID, p.CreatedAt, p.Payload,
			p.DecidedByKind, p.DecidedByID, p.DecidedAt, p.DecideReason, p.AppliedError)
	}

	events, err := st.PolicyEventQuery(context.Background(), "", 200)
	if err != nil {
		t.Fatalf("dump policy_event rows: %v", err)
	}
	fmt.Fprintf(&b, "policy_event rows: %d\n", len(events))
	for _, e := range events {
		fmt.Fprintf(&b, "  event %d at=%s outcome=%s subject=%s:%s policy_id=%s detail=%s\n",
			e.ID, e.At, e.Outcome, e.SubjectKind, e.SubjectID, e.PolicyID, e.Detail)
	}

	if extra != nil {
		b.WriteString(extra(t))
	}
	return b.String()
}

// --- fixture helpers ---------------------------------------------------------

// goldenBoundary wraps the case's own store handle in the policy boundary, so a
// fixture's rows clear the same write boundary a real add clears.
func goldenBoundary(st *store.Store) *policy.Store {
	return policy.NewStore(policyRepo(st))
}

// goldenPolicyRow builds one policy value from the CLI's own spelling.
func goldenPolicyRow(id, typ, target, action, source, spec, notAfter string) policy.Policy {
	p := policy.Policy{
		ID:     id,
		Type:   policy.PolicyType(typ),
		Action: policy.Action(action),
		Spec:   json.RawMessage(spec),
		Source: policy.Source(source),
	}
	kind, idStr, _ := strings.Cut(target, ":")
	p.Target = policy.Target{Kind: policy.TargetKind(kind), ID: idStr}
	if notAfter != "" {
		at, err := time.Parse(time.RFC3339Nano, notAfter)
		if err != nil {
			panic(fmt.Sprintf("golden fixture not_after %q: %v", notAfter, err))
		}
		p.NotAfter = &at
	}
	return p
}

// goldenStore stages a row through the boundary at the fixed golden instant.
func goldenStore(t *testing.T, st *store.Store, p policy.Policy) {
	t.Helper()
	if rej := goldenBoundary(st).Add(context.Background(), p, goldenInstant); rej != nil {
		t.Fatalf("fixture policy %q refused: %s", p.ID, rej.Reason)
	}
}

// goldenStageProposal writes an inert proposal row directly, which is what the
// socket's proposal.create does — the CLI has no create verb, so a commit case
// has to start from the row.
func goldenStageProposal(t *testing.T, st *store.Store, kind string, payload any) store.Proposal {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal %s payload: %v", kind, err)
	}
	p, err := st.CreateProposal(kind, string(raw), store.Actor{Kind: "user", ID: "1000"}, goldenInstantRFC)
	if err != nil {
		t.Fatalf("stage %s proposal: %v", kind, err)
	}
	return p
}

// goldenSteps is the trivial steps function for cases whose commands need no
// generated id.
func goldenSteps(steps ...[]string) func(t *testing.T, st *store.Store) [][]string {
	return func(t *testing.T, st *store.Store) [][]string { return steps }
}

// goldenMCPConfig points the MCP layer at a temp config file holding these
// bytes and returns the path. S46 T3: the same fixture fact additionally
// seeds the TABLES (the truth the validators and effects read —
// mcp_seed_test.go); the file stays for goldenMCPFile's world-state dump
// and the importer's world (T4); call sites unchanged.
func goldenMCPConfig(t *testing.T, contents string) string {
	t.Helper()
	isolateStoreEnv(t) // the rows half seeds through the real resolver (see its comment)
	path := filepath.Join(t.TempDir(), "mcp.json")
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("write mcp config: %v", err)
	}
	t.Setenv(paths.MCPConfigEnv, path)
	seedMCPConfigRows(t, mcpConfigFromJSON(t, contents))
	return path
}

// goldenMCPFile pins the config file's bytes as part of a case's world state.
func goldenMCPFile(t *testing.T) string {
	t.Helper()
	path, err := paths.MCPConfigFile()
	if err != nil {
		t.Fatalf("resolve mcp config: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Sprintf("mcp.json: (unreadable: %v)\n", err)
	}
	return fmt.Sprintf("mcp.json:\n%s\n", string(raw))
}

// --- policy list --------------------------------------------------------------

func TestGoldenPolicyList(t *testing.T) {
	runGoldenCases(t,
		goldenCase{
			name:  "policy-list-empty",
			steps: goldenSteps([]string{"policy", "list"}),
		},
		goldenCase{
			name: "policy-list-mixed",
			setup: func(t *testing.T, st *store.Store) {
				goldenStore(t, st, goldenPolicyRow("mixed-tool", "tool_access", "agent:alice", "deny", "operator", `{"toolName":"bash","state":"deny"}`, ""))
				goldenStore(t, st, goldenPolicyRow("mixed-command", "command_access", "agent:alice", "allow", "operator", `{"command":"git status","state":"allow"}`, ""))
				goldenStore(t, st, goldenPolicyRow("mixed-route", "route_access", "loop:nightly", "deny", "operator", `{"route":"remote","state":"deny"}`, ""))
				goldenStore(t, st, goldenPolicyRow("mixed-effect", "effect_class", "all-agents", "deny", "agent", `{"effect":"spend","state":"deny"}`, ""))
				goldenStore(t, st, goldenPolicyRow("mixed-cost", "cost_cap", "all-agents", "deny", "operator", `{"token":"session","max":1000}`, ""))
				goldenStore(t, st, goldenPolicyRow("mixed-skill", "skill_access", "lane-type:review", "ask", "operator", `{"skill":"shell","state":"ask"}`, ""))
				goldenStore(t, st, goldenPolicyRow("mixed-trigger", "trigger_access", "mcp:github", "deny", "mcp_server", `{"trigger":"webhook","state":"deny"}`, goldenInstantRFC))
				goldenStore(t, st, goldenPolicyRow("mixed-expired", "tool_access", "agent:carol", "deny", "operator", `{"toolName":"webfetch","state":"deny"}`, "2020-01-02T03:04:05Z"))
			},
			steps: goldenSteps(
				[]string{"policy", "list"},
				[]string{"policy", "list", "--type", "command_access"},
				[]string{"policy", "list", "--type", "trigger_access"},
				[]string{"policy", "list", "--type", "skill_access"},
			),
		},
	)
}

// --- policy add, accepted -------------------------------------------------------

func TestGoldenPolicyAddOK(t *testing.T) {
	add := func(name string, args ...string) goldenCase {
		steps := append([]string{"policy", "add"}, args...)
		return goldenCase{
			name:  name,
			steps: goldenSteps(steps, []string{"policy", "list"}),
		}
	}
	runGoldenCases(t,
		add("policy-add-ok-tool-access", "--type", "tool_access", "--id", "ok-tool",
			"--target", "agent:alice", "--action", "deny", "--tool", "bash",
			"--spec", `{"state":"deny"}`),
		add("policy-add-ok-command-access", "--type", "command_access", "--id", "ok-command",
			"--target", "agent:alice", "--action", "allow", "--command", "git status"),
		add("policy-add-ok-route-access", "--type", "route_access", "--id", "ok-route",
			"--target", "agent:dave", "--action", "deny", "--spec", `{"route":"remote","state":"deny"}`),
		add("policy-add-ok-effect-class", "--type", "effect_class", "--id", "ok-effect",
			"--target", "all-agents", "--action", "deny", "--spec", `{"effect":"spend","state":"deny"}`),
		add("policy-add-ok-cost-cap", "--type", "cost_cap", "--id", "ok-cost",
			"--target", "all-agents", "--action", "deny", "--spec", `{"token":"session","max":250}`),
		add("policy-add-ok-skill-access", "--type", "skill_access", "--id", "ok-skill",
			"--target", "agent:erin", "--action", "ask", "--spec", `{"skill":"web","state":"ask"}`),
		add("policy-add-ok-trigger-access", "--type", "trigger_access", "--id", "ok-trigger",
			"--target", "all-agents", "--action", "deny", "--spec", `{"trigger":"schedule","state":"deny"}`),
		add("policy-add-ok-expiring", "--type", "tool_access", "--id", "ok-expiring",
			"--target", "agent:frank", "--action", "deny", "--tool", "grep",
			"--spec", `{"state":"deny"}`, "--source", "operator", "--not-after", "2026-11-02T00:00:00Z"),
		add("policy-add-ok-agent-source-deny", "--type", "tool_access", "--id", "ok-agent-deny",
			"--target", "agent:grace", "--action", "deny", "--tool", "webfetch",
			"--spec", `{"state":"deny"}`, "--source", "agent"),
	)
}

// --- policy add, refused and warned --------------------------------------------

func TestGoldenPolicyAddRefused(t *testing.T) {
	refused := func(name string, args ...string) goldenCase {
		steps := append([]string{"policy", "add"}, args...)
		return goldenCase{
			name:  name,
			steps: goldenSteps(steps, []string{"policy", "list"}),
		}
	}
	runGoldenCases(t,
		refused("policy-add-refused-pl1", "--type", "tool_access", "--id", "pl1-allow",
			"--target", "agent:alice", "--action", "allow", "--tool", "bash", "--source", "agent"),
		refused("policy-add-refused-closed-set", "--type", "tool_accesss", "--id", "typo",
			"--target", "agent:alice", "--action", "deny", "--tool", "bash",
			"--spec", `{"state":"deny"}`),
		refused("policy-add-refused-identifier", "--type", "tool_access", "--id", "bad id",
			"--target", "agent:alice", "--action", "deny", "--tool", "bash",
			"--spec", `{"state":"deny"}`),
		refused("policy-add-refused-bad-target", "--type", "tool_access", "--id", "bad-target",
			"--target", "agentt:alice", "--action", "deny", "--tool", "bash"),
		refused("policy-add-refused-spec-sugar-pairing", "--type", "command_access", "--id", "pairing",
			"--target", "agent:alice", "--action", "deny", "--command", "git status",
			"--spec", `{"command":"git status","state":"deny"}`),
		goldenCase{
			// DEF-038: a loop / lane-type / mcp target is stored AND warned about
			// (warn, never refuse) — the warning is part of the printed bytes, so
			// it is pinned beside the refusals the task's table names it with.
			name: "policy-add-warn-unmatchable-target",
			steps: goldenSteps(
				[]string{"policy", "add", "--type", "tool_access", "--id", "warn-lane",
					"--target", "lane-type:review", "--action", "deny", "--tool", "bash",
					"--spec", `{"state":"deny"}`},
				[]string{"policy", "add", "--type", "tool_access", "--id", "warn-loop",
					"--target", "loop:nightly", "--action", "deny", "--tool", "bash",
					"--spec", `{"state":"deny"}`},
				[]string{"policy", "add", "--type", "tool_access", "--id", "warn-mcp",
					"--target", "mcp:github", "--action", "deny", "--tool", "bash",
					"--spec", `{"state":"deny"}`},
				[]string{"policy", "list"},
			),
		},
	)
}

// --- policy remove ---------------------------------------------------------------

func TestGoldenPolicyRemove(t *testing.T) {
	runGoldenCases(t,
		goldenCase{
			name: "policy-remove-ok",
			setup: func(t *testing.T, st *store.Store) {
				goldenStore(t, st, goldenPolicyRow("remove-me", "tool_access", "agent:alice", "deny", "operator", `{"toolName":"bash","state":"deny"}`, ""))
			},
			steps: goldenSteps(
				[]string{"policy", "remove", "--id", "remove-me"},
				[]string{"policy", "list"},
			),
		},
		goldenCase{
			name:  "policy-remove-missing",
			steps: goldenSteps([]string{"policy", "remove", "--id", "never-existed"}),
		},
	)
}

// --- policy seed-corpus ------------------------------------------------------------

func TestGoldenPolicySeedCorpus(t *testing.T) {
	const agent = "memory-steward-loop" // the corpus's smallest agent: five rules
	runGoldenCases(t,
		goldenCase{
			name: "policy-seed-dry-run",
			steps: goldenSteps(
				[]string{"policy", "seed-corpus", "--agent", agent, "--dry-run"},
				[]string{"policy", "list"},
			),
		},
		goldenCase{
			name: "policy-seed-write",
			steps: goldenSteps(
				[]string{"policy", "seed-corpus", "--agent", agent},
				[]string{"policy", "list", "--type", "command_access"},
			),
		},
		goldenCase{
			name: "policy-seed-idempotent",
			steps: goldenSteps(
				[]string{"policy", "seed-corpus", "--agent", agent},
				[]string{"policy", "seed-corpus", "--agent", agent},
				[]string{"policy", "list", "--type", "command_access"},
			),
		},
	)
}

// --- policy why ---------------------------------------------------------------------

func TestGoldenPolicyWhy(t *testing.T) {
	runGoldenCases(t,
		goldenCase{
			name: "policy-why-allow",
			setup: func(t *testing.T, st *store.Store) {
				goldenStore(t, st, goldenPolicyRow("grant-read", "tool_access", "agent:alice", "allow", "operator", `{"toolName":"read","state":"allow"}`, ""))
			},
			steps: goldenSteps([]string{"policy", "why", "--agent", "alice", "--tool", "read"}),
		},
		goldenCase{
			name: "policy-why-deny",
			setup: func(t *testing.T, st *store.Store) {
				goldenStore(t, st, goldenPolicyRow("no-bash", "tool_access", "agent:alice", "deny", "operator", `{"toolName":"bash","state":"deny"}`, ""))
			},
			steps: goldenSteps([]string{"policy", "why", "--agent", "alice", "--tool", "bash"}),
		},
		goldenCase{
			name: "policy-why-ask",
			setup: func(t *testing.T, st *store.Store) {
				goldenStore(t, st, goldenPolicyRow("ask-shell", "tool_access", "agent:alice", "ask", "operator", `{"toolName":"bash","state":"ask"}`, ""))
			},
			steps: goldenSteps([]string{"policy", "why", "--agent", "alice", "--tool", "bash"}),
		},
		goldenCase{
			name:  "policy-why-default-deny",
			steps: goldenSteps([]string{"policy", "why", "--agent", "nobody", "--tool", "bash"}),
		},
		goldenCase{
			name: "policy-why-unimplemented",
			setup: func(t *testing.T, st *store.Store) {
				goldenStore(t, st, goldenPolicyRow("effect-rule", "effect_class", "agent:alice", "deny", "operator", `{"effect":"spend","state":"deny"}`, ""))
			},
			steps: goldenSteps([]string{"policy", "why", "--agent", "alice", "--type", "effect_class", "--effect", "spend"}),
		},
		goldenCase{
			name: "policy-why-expired",
			setup: func(t *testing.T, st *store.Store) {
				goldenStore(t, st, goldenPolicyRow("temp-grant", "tool_access", "agent:alice", "allow", "operator", `{"toolName":"read","state":"allow"}`, "2026-10-21T00:00:00Z"))
			},
			steps: goldenSteps(
				[]string{"policy", "why", "--agent", "alice", "--tool", "read", "--at", "2026-10-20T12:00:00Z"},
				[]string{"policy", "why", "--agent", "alice", "--tool", "read", "--at", "2026-10-22T00:00:00Z"},
			),
		},
	)
}

// --- policy in-force ------------------------------------------------------------------

func TestGoldenPolicyInForce(t *testing.T) {
	runGoldenCases(t,
		goldenCase{
			name: "policy-inforce-unacknowledged",
			setup: func(t *testing.T, st *store.Store) {
				goldenStore(t, st, goldenPolicyRow("no-bash", "tool_access", "agent:alice", "deny", "operator", `{"toolName":"bash","state":"deny"}`, ""))
			},
			steps: goldenSteps([]string{"policy", "in-force", "--agent", "alice"}),
		},
		goldenCase{
			name: "policy-inforce-acknowledged",
			setup: func(t *testing.T, st *store.Store) {
				goldenStore(t, st, goldenPolicyRow("no-bash", "tool_access", "agent:alice", "deny", "operator", `{"toolName":"bash","state":"deny"}`, ""))
				if rej := goldenBoundary(st).RecordAcceptance(context.Background(), "no-bash",
					policy.Target{Kind: policy.AgentKind, ID: "alice"}, "alice", goldenInstant); rej != nil {
					t.Fatalf("fixture acceptance: %s", rej.Reason)
				}
			},
			steps: goldenSteps([]string{"policy", "in-force", "--agent", "alice"}),
		},
		goldenCase{
			name: "policy-inforce-could-not-determine",
			setup: func(t *testing.T, st *store.Store) {
				goldenStore(t, st, goldenPolicyRow("cap-rule", "cost_cap", "agent:alice", "deny", "operator", `{"token":"session","max":500}`, ""))
			},
			steps: goldenSteps([]string{"policy", "in-force", "--agent", "alice"}),
		},
		goldenCase{
			name:  "policy-inforce-none",
			steps: goldenSteps([]string{"policy", "in-force", "--agent", "stranger"}),
		},
	)
}

// --- policy events ---------------------------------------------------------------------

func TestGoldenPolicyEvents(t *testing.T) {
	// The populated fixture authors one rule's audit row and records two real
	// refusals, so the listing carries both kinds of row an audit can hold.
	populate := func(t *testing.T, st *store.Store) {
		b := goldenBoundary(st)
		goldenStore(t, st, goldenPolicyRow("no-bash", "tool_access", "agent:alice", "deny", "operator", `{"toolName":"bash","state":"deny"}`, ""))
		if rej := b.RecordAuthored(context.Background(), "agent:alice", "no-bash", goldenInstant); rej != nil {
			t.Fatalf("fixture authored row: %s", rej.Reason)
		}
		rows, rej := b.List(context.Background(), goldenInstant)
		if rej != nil {
			t.Fatalf("fixture list: %s", rej.Reason)
		}
		alice := policy.Evaluate(rows, policy.Request{Type: policy.ToolAccess, Agent: "alice", Tool: "bash"}, goldenInstant)
		if rej := b.RecordRefusal(context.Background(), "agent:alice", alice, goldenInstant); rej != nil {
			t.Fatalf("fixture refusal row (alice): %s", rej.Reason)
		}
		bob := policy.Evaluate(rows, policy.Request{Type: policy.ToolAccess, Agent: "bob", Tool: "bash"}, goldenInstant)
		if rej := b.RecordRefusal(context.Background(), "agent:bob", bob, goldenInstant); rej != nil {
			t.Fatalf("fixture refusal row (bob): %s", rej.Reason)
		}
	}
	runGoldenCases(t,
		goldenCase{
			name:  "policy-events-empty",
			steps: goldenSteps([]string{"policy", "events"}),
		},
		goldenCase{
			name:  "policy-events-populated",
			setup: populate,
			steps: goldenSteps([]string{"policy", "events"}),
		},
		goldenCase{
			name:  "policy-events-subject",
			setup: populate,
			steps: goldenSteps(
				[]string{"policy", "events", "--subject", "agent:alice"},
				[]string{"policy", "events", "--subject", "agent:nobody"},
			),
		},
		goldenCase{
			name:  "policy-events-depth",
			setup: populate,
			steps: goldenSteps([]string{"policy", "events", "--depth", "1"}),
		},
	)
}

// --- proposal list ------------------------------------------------------------------------

func TestGoldenProposalList(t *testing.T) {
	runGoldenCases(t,
		goldenCase{
			name:  "proposal-list-empty",
			steps: goldenSteps([]string{"proposal", "list"}),
		},
		goldenCase{
			name: "proposal-list-mixed",
			setup: func(t *testing.T, st *store.Store) {
				goldenStageProposal(t, st, "policy_add", policy.Policy{
					ID: "listed-add", Type: policy.ToolAccess,
					Target: policy.Target{Kind: policy.AllAgents}, Action: policy.Deny,
					Source: policy.Operator, Spec: json.RawMessage(`{"toolName":"bash","state":"deny"}`),
				})
				goldenStageProposal(t, st, "policy_remove", policyRemovePayload{ID: "listed-remove"})
				committed := goldenStageProposal(t, st, "seed_corpus", seedCorpusPayload{Agents: []string{"brief-author"}})
				if err := st.DecideProposal(committed.ID, "committed", store.Actor{Kind: "user", ID: "cli"}, "", goldenInstantRFC); err != nil {
					t.Fatalf("stage a committed row: %v", err)
				}
				rejected := goldenStageProposal(t, st, "mcp_server_remove", mcpServerRemovePayload{Name: "github"})
				if err := st.DecideProposal(rejected.ID, "rejected", store.Actor{Kind: "user", ID: "cli"}, "not now", goldenInstantRFC); err != nil {
					t.Fatalf("stage a rejected row: %v", err)
				}
				discarded := goldenStageProposal(t, st, "mcp_server_add", mcpServerAddPayload{
					Tier:   "public",
					Server: mcp.ServerConfig{Name: "linear", Transport: mcp.TransportConfig{Type: "stdio", Command: "linear-mcp"}, Tools: []mcp.ToolConfig{{Name: "list_issues", Rerun: "safe", Effect: "local"}}},
				})
				if err := st.DiscardProposal(discarded.ID, store.Actor{Kind: "user", ID: "cli"}, goldenInstantRFC); err != nil {
					t.Fatalf("stage a discarded row: %v", err)
				}
			},
			steps: goldenSteps(
				[]string{"proposal", "list"},
				[]string{"proposal", "list", "--status", "proposed"},
				[]string{"proposal", "list", "--status", "committed"},
				[]string{"proposal", "list", "--status", "bogus"},
			),
		},
	)
}

// --- proposal commit -----------------------------------------------------------------------

func TestGoldenProposalCommitPolicyAdd(t *testing.T) {
	var staged store.Proposal
	runGoldenCases(t,
		goldenCase{
			name: "proposal-commit-policy-add",
			setup: func(t *testing.T, st *store.Store) {
				staged = goldenStageProposal(t, st, "policy_add", policy.Policy{
					ID: "committed-add", Type: policy.ToolAccess,
					Target: policy.Target{Kind: policy.AllAgents}, Action: policy.Deny,
					Source: policy.Operator, Spec: json.RawMessage(`{"toolName":"webfetch","state":"deny"}`),
				})
			},
			steps: func(t *testing.T, st *store.Store) [][]string {
				return goldenSteps(
					[]string{"proposal", "commit", itoa(staged.ID)},
					[]string{"policy", "list"},
					[]string{"proposal", "list"},
				)(t, st)
			},
		},
		goldenCase{
			// TD4's crash window: the effect already stands (applied by hand,
			// the flip lost) — the commit must complete the flip, not re-apply
			// and not refuse on the primary key.
			name: "proposal-commit-already-applied",
			setup: func(t *testing.T, st *store.Store) {
				staged = goldenStageProposal(t, st, "policy_add", policy.Policy{
					ID: "crash-retry-golden", Type: policy.ToolAccess,
					Target: policy.Target{Kind: policy.AllAgents}, Action: policy.Deny,
					Source: policy.Operator, Spec: json.RawMessage(`{"toolName":"bash","state":"deny"}`),
				})
				var seeded policy.Policy
				if err := json.Unmarshal([]byte(staged.Payload), &seeded); err != nil {
					t.Fatalf("decode the staged payload: %v", err)
				}
				if rej := goldenBoundary(st).Add(context.Background(), seeded, goldenInstant); rej != nil {
					t.Fatalf("stage the applied effect by hand: %s", rej.Reason)
				}
			},
			steps: func(t *testing.T, st *store.Store) [][]string {
				return goldenSteps(
					[]string{"proposal", "commit", itoa(staged.ID)},
					[]string{"policy", "list"},
					[]string{"proposal", "list"},
				)(t, st)
			},
		},
		goldenCase{
			// TD4's other remove shape: the policy the proposal names is ALREADY
			// gone, so the remove is already effective — the commit completes the
			// flip instead of refusing for the wrong reason (the membership check
			// catches a MISTAKEN remove, never a retried one).
			name: "proposal-commit-remove-already-gone",
			setup: func(t *testing.T, st *store.Store) {
				staged = goldenStageProposal(t, st, "policy_remove", policyRemovePayload{ID: "gone-already"})
			},
			steps: func(t *testing.T, st *store.Store) [][]string {
				return goldenSteps([]string{"proposal", "commit", itoa(staged.ID)},
					[]string{"proposal", "list"})(t, st)
			},
		},
		goldenCase{
			// A decided row is immutable history — the second commit is refused.
			name: "proposal-commit-decided-refused",
			setup: func(t *testing.T, st *store.Store) {
				staged = goldenStageProposal(t, st, "policy_add", policy.Policy{
					ID: "committed-twice", Type: policy.ToolAccess,
					Target: policy.Target{Kind: policy.AllAgents}, Action: policy.Deny,
					Source: policy.Operator, Spec: json.RawMessage(`{"toolName":"grep","state":"deny"}`),
				})
			},
			steps: func(t *testing.T, st *store.Store) [][]string {
				return goldenSteps(
					[]string{"proposal", "commit", itoa(staged.ID)},
					[]string{"proposal", "commit", itoa(staged.ID)},
					[]string{"policy", "list"},
				)(t, st)
			},
		},
		goldenCase{
			name: "proposal-commit-bad-id",
			steps: goldenSteps(
				[]string{"proposal", "commit", "not-a-number"},
				[]string{"proposal", "commit"},
			),
		},
	)
}

func TestGoldenProposalCommitMCP(t *testing.T) {
	var staged store.Proposal
	var rival store.Proposal
	runGoldenCases(t,
		goldenCase{
			name: "proposal-commit-mcp-add",
			setup: func(t *testing.T, st *store.Store) {
				goldenMCPConfig(t, `{"public":[],"agents":{}}`)
				staged = goldenStageProposal(t, st, "mcp_server_add", mcpServerAddPayload{
					Tier:   "public",
					Server: mcp.ServerConfig{Name: "github", Transport: mcp.TransportConfig{Type: "stdio", Command: "gh-mcp"}, Tools: []mcp.ToolConfig{{Name: "list_repos", Rerun: "safe", Effect: "local"}}},
				})
			},
			steps: func(t *testing.T, st *store.Store) [][]string {
				return goldenSteps(
					[]string{"proposal", "commit", itoa(staged.ID)},
					[]string{"proposal", "list"},
				)(t, st)
			},
			extra: goldenMCPFile,
		},
		goldenCase{
			name: "proposal-commit-mcp-remove",
			setup: func(t *testing.T, st *store.Store) {
				goldenMCPConfig(t, `{"public":[{"name":"github","transport":{"type":"stdio","command":"gh-mcp"},"tools":[{"name":"list_repos","rerun":"safe","effect":"local"}]}],"agents":{}}`)
				staged = goldenStageProposal(t, st, "mcp_server_remove", mcpServerRemovePayload{Name: "github"})
			},
			steps: func(t *testing.T, st *store.Store) [][]string {
				return goldenSteps(
					[]string{"proposal", "commit", itoa(staged.ID)},
					[]string{"proposal", "list"},
				)(t, st)
			},
			extra: goldenMCPFile,
		},
		goldenCase{
			// Failed re-validation (TD4): the world moved after create. The
			// server name is configured now, with a DIFFERENT definition, so the
			// payload is not already-applied and the validator runs and refuses.
			// The row must stay proposed with applied_error set — a refusal must
			// never flip the row, and never half-apply the effect either.
			name: "proposal-commit-failed-revalidation",
			setup: func(t *testing.T, st *store.Store) {
				goldenMCPConfig(t, `{"public":[{"name":"github","transport":{"type":"stdio","command":"a-different-gh-mcp"}}],"agents":{}}`)
				staged = goldenStageProposal(t, st, "mcp_server_add", mcpServerAddPayload{
					Tier:   "public",
					Server: mcp.ServerConfig{Name: "github", Transport: mcp.TransportConfig{Type: "stdio", Command: "gh-mcp"}, Tools: []mcp.ToolConfig{{Name: "list_repos", Rerun: "safe", Effect: "local"}}},
				})
			},
			steps: func(t *testing.T, st *store.Store) [][]string {
				return goldenSteps([]string{"proposal", "commit", itoa(staged.ID)})(t, st)
			},
			extra: goldenMCPFile,
		},
		goldenCase{
			// The already-applied half of the same kind: the server is
			// configured with EXACTLY the proposed definition, so the retry
			// after a lost flip completes the flip instead of tripping MC12's
			// "configured twice" refusal on the server it already added.
			name: "proposal-commit-mcp-already-applied",
			setup: func(t *testing.T, st *store.Store) {
				goldenMCPConfig(t, `{"public":[{"name":"github","transport":{"type":"stdio","command":"gh-mcp"},"tools":[{"name":"list_repos","rerun":"safe","effect":"local"}]}],"agents":{}}`)
				staged = goldenStageProposal(t, st, "mcp_server_add", mcpServerAddPayload{
					Tier:   "public",
					Server: mcp.ServerConfig{Name: "github", Transport: mcp.TransportConfig{Type: "stdio", Command: "gh-mcp"}, Tools: []mcp.ToolConfig{{Name: "list_repos", Rerun: "safe", Effect: "local"}}},
				})
			},
			steps: func(t *testing.T, st *store.Store) [][]string {
				return goldenSteps([]string{"proposal", "commit", itoa(staged.ID)},
					[]string{"proposal", "list"})(t, st)
			},
			extra: goldenMCPFile,
		},
		goldenCase{
			// …while an ADD with no config file at all is MC11's legitimate
			// third state: the candidate is one server added to an otherwise
			// empty config, so it is accepted and the file is created (the
			// validator's ErrNoConfig arm opens the door, `SaveConfig` writes
			// it) — the opposite verdict from the remove above, on purpose.
			name: "proposal-commit-mcp-add-no-config",
			setup: func(t *testing.T, st *store.Store) {
				dir := t.TempDir()
				t.Setenv(paths.MCPConfigEnv, filepath.Join(dir, "absent.json"))
				staged = goldenStageProposal(t, st, "mcp_server_add", mcpServerAddPayload{
					Tier:   "public",
					Server: mcp.ServerConfig{Name: "github", Transport: mcp.TransportConfig{Type: "stdio", Command: "gh-mcp"}, Tools: []mcp.ToolConfig{{Name: "list_repos", Rerun: "safe", Effect: "local"}}},
				})
			},
			steps: func(t *testing.T, st *store.Store) [][]string {
				return goldenSteps([]string{"proposal", "commit", itoa(staged.ID)})(t, st)
			},
			extra: goldenMCPFile,
		},
		goldenCase{
			// The agent-tier half of the add: a private server under one agent,
			// and a second add of the same name into a DIFFERENT tier, which is
			// legitimate (MC12's refusal is per scope).
			name: "proposal-commit-mcp-agent-tier",
			setup: func(t *testing.T, st *store.Store) {
				goldenMCPConfig(t, `{"public":[],"agents":{"ops":{"private":[{"name":"sentry","transport":{"type":"stdio","command":"sentry-mcp"}}]}}}`)
				staged = goldenStageProposal(t, st, "mcp_server_add", mcpServerAddPayload{
					Tier:   "private",
					Agent:  "ops",
					Server: mcp.ServerConfig{Name: "linear", Transport: mcp.TransportConfig{Type: "stdio", Command: "linear-mcp"}, Tools: []mcp.ToolConfig{{Name: "list_issues", Rerun: "safe", Effect: "local"}}},
				})
				rival = goldenStageProposal(t, st, "mcp_server_remove", mcpServerRemovePayload{Name: "sentry"})
			},
			steps: func(t *testing.T, st *store.Store) [][]string {
				return goldenSteps(
					[]string{"proposal", "commit", itoa(staged.ID)},
					[]string{"proposal", "commit", itoa(rival.ID)},
					[]string{"proposal", "list"},
				)(t, st)
			},
			extra: goldenMCPFile,
		},
		goldenCase{
			// MC11's third state: no config file at all. A REMOVE whose config is
			// absent is TD4's already-applied shape — nothing to remove IS the
			// effect standing — so the flip completes; the validator's "no MCP
			// configuration exists to remove … from" line is what the create door
			// says, never the commit door.
			name: "proposal-commit-mcp-no-config-remove",
			setup: func(t *testing.T, st *store.Store) {
				dir := t.TempDir()
				t.Setenv(paths.MCPConfigEnv, filepath.Join(dir, "absent.json"))
				staged = goldenStageProposal(t, st, "mcp_server_remove", mcpServerRemovePayload{Name: "github"})
			},
			steps: func(t *testing.T, st *store.Store) [][]string {
				return goldenSteps([]string{"proposal", "commit", itoa(staged.ID)})(t, st)
			},
		},
	)
}

// --- proposal reject --------------------------------------------------------------------------

func TestGoldenProposalReject(t *testing.T) {
	var staged store.Proposal
	runGoldenCases(t,
		goldenCase{
			name: "proposal-reject-with-reason",
			setup: func(t *testing.T, st *store.Store) {
				staged = goldenStageProposal(t, st, "policy_remove", policyRemovePayload{ID: "not-yet"})
			},
			steps: func(t *testing.T, st *store.Store) [][]string {
				return goldenSteps(
					// The flag comes BEFORE the positional: Go's flag parser stops
					// at the first non-flag argument, so `reject 1 --reason …` is
					// not the verb's shape. Both spellings are pinned; see T1's Notes.
					[]string{"proposal", "reject", "--reason", "the target is still live", itoa(staged.ID)},
					[]string{"proposal", "list"},
				)(t, st)
			},
		},
		goldenCase{
			// TD5: rejecting loosens nothing, so the CLI requires no reason and
			// accepts the empty one. (The task's own table guessed "refused";
			// the pinned bytes are the truth, and T1's Notes record it — the
			// socket door is where PM7.5 makes the reason mandatory.)
			name: "proposal-reject-without-reason",
			setup: func(t *testing.T, st *store.Store) {
				staged = goldenStageProposal(t, st, "policy_remove", policyRemovePayload{ID: "loosens-nothing"})
			},
			steps: func(t *testing.T, st *store.Store) [][]string {
				return goldenSteps(
					[]string{"proposal", "reject", itoa(staged.ID)},
					[]string{"proposal", "list"},
				)(t, st)
			},
		},
		goldenCase{
			// The same verb with the flag after the id: the flag parser has
			// already stopped, so `--reason` arrives as two more positionals
			// and the verb refuses its own input. Pinned because it is what an
			// operator types first, and the socket door (T5) will not have it.
			name: "proposal-reject-reason-after-id",
			setup: func(t *testing.T, st *store.Store) {
				staged = goldenStageProposal(t, st, "policy_remove", policyRemovePayload{ID: "flag-after"})
			},
			steps: func(t *testing.T, st *store.Store) [][]string {
				return goldenSteps([]string{"proposal", "reject", itoa(staged.ID), "--reason", "too late"})(t, st)
			},
		},
		goldenCase{
			name: "proposal-reject-bad-args",
			steps: goldenSteps(
				[]string{"proposal", "reject"},
				[]string{"proposal", "reject", "1", "2"},
				[]string{"proposal", "reject", "0"},
			),
		},
	)
}
