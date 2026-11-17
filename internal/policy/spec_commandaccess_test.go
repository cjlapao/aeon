package policy_test

// CommandAccess — the type that stopped being a placeholder, tested as the
// shape S11 decided on.
//
// The claims, in the order the evaluator asks them:
//
//  1. the spec is the pinned three-state object and nothing else (an unknown
//     field is refused by name, an empty or unmatchable pattern is refused at
//     authoring, a state that contradicts the row's action is refused naming
//     both), and every refusal quotes the shape verbatim;
//  2. the fold: ANY denied segment denies the whole command, and an allow must
//     cover EVERY segment — the asymmetry that stops `git status*` permitting
//     `git status && git push`;
//  3. the matcher's own traps, re-proven through the policy path rather than
//     through pattern_test.go's tables: a bare `gh` does not govern `gh pr merge`;
//  4. inertness: an unreadable spec, an empty command, an over-limit command and
//     a row wearing an invented action all match nothing and none of
//     them is ever "unimplemented" — this type decides, so that flag is not
//     theirs to set, and PL5's default deny is what a caller sees. `ask` is
//     NOT on that list since migration 0019: it decides, deny-like, and its
//     own test below pins the asked shape;
//  5. the Notes a refusal leaves behind name a segment INDEX and never the
//     command or the pattern (legal constraint 1).
//
// The deviation this task is on record for — the shipped deny-wins engine is
// reused, and the seed design's ordered first-match fold is NOT built — is
// recorded in docs/aeon/S11-permissions/tasks/T4.md's Notes block. The tests
// here are the behaviour that record describes: a broad early allow never
// rescues a command a later deny names.

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/policy"
)

// ── fixtures ─────────────────────────────────────────────────────────────────

// commandAccessShapeText is the shape every CommandAccess refusal quotes,
// spelled here rather than imported so a failing run shows the operator's
// sentence. The schema lock (TestThePinnedSpecShapesMatchTheirGoStructs) pins
// it against the struct, from the case below.
const commandAccessShapeText = `{"command": string, "state": "allow"|"deny"|"ask"}`

// commandAccessCase is the filled type's row in the schema lock's table. It
// lives here, beside the struct it describes, because the type left the stub
// set: spec_stubs_test.go drives the five that remain and appends this one so
// the lock still covers all seven.
var commandAccessCase = stubCase{
	typ: policy.CommandAccess, shape: commandAccessShapeText,
	key: "command", value: "git push*", hasState: true,
	valid: `{"command":"git push*","state":"deny"}`,
	set:   func(r *policy.Request, v string) { r.Command = v },
}

// commandSpec renders the spec through the exported struct, so the ACCEPTED
// shapes in these tables are produced by the schema itself rather than by a
// string that could drift from it. (The refused shapes are literals — that is
// the whole point of them.)
func commandSpec(tb testing.TB, command, state string) string {
	tb.Helper()
	raw, err := json.Marshal(policy.CommandAccessSpec{Command: command, State: state})
	if err != nil {
		tb.Fatalf("marshal the spec: %v", err)
	}
	return string(raw)
}

// commandPolicy builds one CommandAccess row; spec is the raw JSON to store.
func commandPolicy(tb testing.TB, id string, kind policy.TargetKind, targetID string,
	action policy.Action, spec string) policy.Policy {
	tb.Helper()
	return specPolicy(tb, policy.CommandAccess, id, kind, targetID, action, spec)
}

// commandReq is the question the CommandAccess evaluator is asked.
func commandReq(agent, command string) policy.Request {
	return policy.Request{Type: policy.CommandAccess, Agent: agent, Command: command}
}

// commandEvaluatorForTheRegistry asks the registry for the evaluator, so every
// test below drives the registered one and proves the registration as it goes.
func commandEvaluatorForTheRegistry(tb testing.TB) policy.Evaluator {
	tb.Helper()
	ev, ok := policy.EvaluatorFor(policy.CommandAccess)
	if !ok {
		tb.Fatal("CommandAccess has no evaluator registered; S11's job was to register one")
	}
	return ev
}

// ── the spec is the pinned object, and nothing else ──────────────────────────

func TestCommandAccessSpecIsExactlyThePinnedShape(t *testing.T) {
	// Every row is the same policy with one thing wrong, and the refusal has to
	// say which — and quote the shape, so an operator who never read the source
	// can see what was wanted. A permissive decode is the failure this table
	// exists to make impossible: {"cmd":"git push"} reading into a zero-value
	// pattern is a rule that matches nothing while looking like a permission.
	cases := []struct {
		name    string
		spec    string
		action  policy.Action
		wantErr string // "" means the spec must be accepted
	}{
		{"the shape itself, allowing", `{"command":"git push*","state":"allow"}`, policy.Allow, ""},
		{"the shape itself, denying", `{"command":"git push*","state":"deny"}`, policy.Deny, ""},
		{"the whole-command pattern is writable", `{"command":"*","state":"deny"}`, policy.Deny, ""},
		{"whitespace around the object is not content", "  \n" + `{"command":"pwd","state":"deny"}` + "\t ", policy.Deny, ""},
		{"a mid-star pattern is writable", `{"command":"docker buildx build*--push*","state":"deny"}`, policy.Deny, ""},
		{"a literal question mark is a byte, not a wildcard", `{"command":"rm -r?","state":"deny"}`, policy.Deny, ""},

		{"an unknown field", `{"cmd":"git push*","state":"deny"}`, policy.Deny, `"cmd"`},
		{"an unknown field spelled near a known one", `{"command":"git push*","stat":"deny"}`, policy.Deny, `"stat"`},
		{"a field added by hope", `{"command":"git push*","state":"deny","segment":2}`, policy.Deny, `"segment"`},
		{"an empty object", `{}`, policy.Allow, `"command"`},
		{"a missing state", `{"command":"git push*"}`, policy.Allow, `"state"`},
		{"an empty state", `{"command":"git push*","state":""}`, policy.Allow, `"state"`},
		{"a wrong state", `{"command":"git push*","state":"permit"}`, policy.Allow, `"state"`},
		{"a state of the wrong JSON type", `{"command":"git push*","state":true}`, policy.Allow, "state"},
		{"a command of the wrong JSON type", `{"command":["git"],"state":"deny"}`, policy.Deny, "command"},
		{"a spec that is not an object", `"git push"`, policy.Deny, "JSON object"},
		{"a spec that is an array", `["git push"]`, policy.Deny, "JSON object"},
		{"a spec that is null", `null`, policy.Deny, "null"},
		{"a spec that is empty bytes", ``, policy.Deny, "empty"},
		{"trailing content after the object", `{"command":"pwd","state":"deny"} {"command":"ls","state":"deny"}`,
			policy.Deny, "trailing"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := commandPolicy(t, "row", policy.AgentKind, "main", c.action, c.spec)
			rej := validateWith(t, policy.CommandAccess, p)
			if c.wantErr == "" {
				if rej != nil {
					t.Fatalf("the validator refused a spec that is exactly the pinned shape: %s", rej.Reason)
				}
				return
			}
			if rej == nil {
				t.Fatalf("the validator accepted %s (%s); it must refuse", c.name, c.spec)
			}
			if !strings.Contains(rej.Reason, c.wantErr) {
				t.Errorf("the refusal %q does not name %q", rej.Reason, c.wantErr)
			}
			if !strings.Contains(rej.Reason, string(policy.CommandAccess)) {
				t.Errorf("the refusal %q does not say which type refused it", rej.Reason)
			}
			if !strings.Contains(rej.Reason, commandAccessShapeText) {
				t.Errorf("the refusal %q does not quote the shape it wanted (%s)", rej.Reason, commandAccessShapeText)
			}
		})
	}
}

func TestCommandAccessEmptyPatternIsRefused(t *testing.T) {
	// The empty pattern is refused twice over, and both refusals must quote the
	// shape: once by the shared schema as an empty required field, once by
	// ValidatePattern as a pattern that matches nothing. MatchPattern's own rule
	// is that "" matches nothing — so a row carrying it is inert while READING
	// like a permission, which is the single failure mode this type is written
	// against. The write boundary is where that gets caught; the evaluator's
	// guard is the belt for a row that reached the table anyway.
	for _, spec := range []string{
		`{"command":"","state":"allow"}`,
		`{"command":"   ","state":"allow"}`,
		`{"state":"allow"}`,
	} {
		p := commandPolicy(t, "empty-pattern", policy.AgentKind, "main", policy.Allow, spec)
		rej := validateWith(t, policy.CommandAccess, p)
		if rej == nil {
			t.Fatalf("%s was accepted; a pattern that matches nothing must be refused at authoring", spec)
		}
		if !strings.Contains(rej.Reason, `"command"`) {
			t.Errorf("the refusal %q does not name the command field", rej.Reason)
		}
		if !strings.Contains(rej.Reason, commandAccessShapeText) {
			t.Errorf("the refusal %q does not quote the shape that was wanted", rej.Reason)
		}
	}

	// The belt, proven by behaviour: a stored row whose pattern is empty matches
	// nothing — including a request whose command is also empty.
	empty := commandPolicy(t, "stored-empty", policy.AgentKind, "main", policy.Allow,
		`{"command":"","state":"allow"}`)
	v := policy.Evaluate([]policy.Policy{empty}, commandReq("main", ""), evalAt)
	assertVerdict(t, v, false, true, false, "", "")
}

func TestCommandAccessPatternChecksRunAtTheWriteBoundary(t *testing.T) {
	// The two checks ToolAccess has no equivalent of, because a command pattern
	// is matcher input rather than a name. ValidatePattern refuses what
	// MatchPattern would otherwise answer surprisingly, and the refusal the
	// validator raises still has to quote the pinned shape — one sentence an
	// operator can act on, whichever half of the validator produced it.
	for _, c := range []struct {
		name    string
		pattern string
		wantErr string
	}{
		{"two adjacent stars", "**", "adjacent"},
		{"a doubled star inside a pattern", "git**push", "adjacent"},
		{"a leading space", " git push", "begins with whitespace"},
		{"a trailing space", "git push ", "ends with whitespace"},
		{"a leading tab", "\tgit push", "begins with whitespace"},
		{"a trailing newline", "git push\n", "ends with whitespace"},
	} {
		p := commandPolicy(t, "bad-pattern", policy.AgentKind, "main", policy.Deny,
			commandSpec(t, c.pattern, "deny"))
		rej := validateWith(t, policy.CommandAccess, p)
		if rej == nil {
			t.Fatalf("%s (%q) was accepted at the write boundary; ValidatePattern refuses it, so the "+
				"validator must not let it through", c.name, c.pattern)
		}
		if !strings.Contains(rej.Reason, c.wantErr) {
			t.Errorf("the refusal %q does not say %q", rej.Reason, c.wantErr)
		}
		if !strings.Contains(rej.Reason, commandAccessShapeText) {
			t.Errorf("the refusal %q does not quote the pinned shape", rej.Reason)
		}
	}

	// And the boundary is not stricter than the specification it guards: every
	// pattern shape the real corpus authors must still be writable here. (The
	// exhaustive sweep is TestEveryCorpusPatternValidates in pattern_test.go;
	// this is the corpus's three characteristic shapes, named.)
	for _, pattern := range []string{"*", "git push*", "docker buildx build*--push*", "gh", "pwd"} {
		p := commandPolicy(t, "corpus-shaped", policy.AgentKind, "main", policy.Deny,
			commandSpec(t, pattern, "deny"))
		if rej := validateWith(t, policy.CommandAccess, p); rej != nil {
			t.Errorf("the write boundary refused a corpus-shaped pattern %q: %s", pattern, rej.Reason)
		}
	}
}

func TestCommandAccessBroadAllowIsNotRefused(t *testing.T) {
	// The `"*"` allow targeting all-agents is the broadest rule in the
	// vocabulary, and it is what this repository's own agent files actually say:
	// refusing it would make the real rule set unseedable, and a guard's job is
	// not to veto the specification it was written against. It deserves a
	// warning at authoring, never a refusal — and the warning is T8's, because
	// the warn channel is a CLI-side function with no seam reaching in here.
	// Pinned as behaviour so "warned, never refused" cannot drift into "refused".
	broad := commandPolicy(t, "everything-for-everyone", policy.AllAgents, "", policy.Allow,
		`{"command":"*","state":"allow"}`)
	if rej := validateWith(t, policy.CommandAccess, broad); rej != nil {
		t.Fatalf("the validator refused the broad allow (%s); the corpus that authors it would become "+
			"unseedable — it is warned about, not vetoed: %s", commandAccessShapeText, rej.Reason)
	}
	// And the guard is genuinely inert-but-not-invisible: it evaluates as an
	// allow when it matches, which is exactly why it needs a warning rather than
	// a silent pass.
	v := policy.Evaluate([]policy.Policy{broad}, commandReq("main", "git push"), evalAt)
	assertVerdict(t, v, true, false, false, broad.ID, "")
}

func TestCommandAccessStateMustAgreeWithAction(t *testing.T) {
	// state is the spec's copy of the row's action, and two copies of one fact
	// are one fact too many if they may disagree. The refusal names both sides
	// AND quotes the shape — this file's criterion, and the reason the check is
	// the shared one rather than a CommandAccess paraphrase of it.
	for _, c := range []struct {
		name   string
		spec   string
		action policy.Action
	}{
		{"the spec allows, the row denies", `{"command":"git push*","state":"allow"}`, policy.Deny},
		{"the spec denies, the row allows", `{"command":"git push*","state":"deny"}`, policy.Allow},
	} {
		t.Run(c.name, func(t *testing.T) {
			p := commandPolicy(t, "contradiction", policy.AgentKind, "main", c.action, c.spec)
			rej := validateWith(t, policy.CommandAccess, p)
			if rej == nil {
				t.Fatalf("a contradictory row was accepted: spec %s action %q", c.spec, c.action)
			}
			for _, want := range []string{`"state"`, strconv.Quote(string(c.action)), commandAccessShapeText} {
				if !strings.Contains(rej.Reason, want) {
					t.Errorf("the refusal %q does not name %s", rej.Reason, want)
				}
			}
		})
	}

	t.Run("a contradictory row that reached the table decides nothing", func(t *testing.T) {
		// The evaluator's share of the same rule: neither half is preferred,
		// because picking one would be the evaluator inventing an intention
		// nobody wrote.
		contrary := commandPolicy(t, "contrary", policy.AgentKind, "main", policy.Deny,
			`{"command":"git push*","state":"allow"}`)
		v := policy.Evaluate([]policy.Policy{contrary}, commandReq("main", "git push"), evalAt)
		assertVerdict(t, v, false, true, false, "", "")

		res := commandEvaluatorForTheRegistry(t).Match(contrary, commandReq("main", "git push"))
		if res.Matched || res.Unimplemented {
			t.Errorf("Match on a contradictory row = %+v; it matches nothing and is not unimplemented", res)
		}
		if !strings.Contains(res.Note, "state") {
			t.Errorf("the Note %q does not say the contradiction is why it declined", res.Note)
		}
	})
}

func TestCommandAccessAskIsAcceptedAtAuthoringAndDecidesAtEvaluation(t *testing.T) {
	// S11's D1, the name-flip half: `ask` is in the shape AND, since migration
	// 0019, in the closed set — so the row that used to be refused at authoring
	// and inert at evaluation is now ADMITTED at authoring and DECIDES at
	// evaluation, as an ask: never a permission, never a denial. The
	// contradiction arm keeps its old refusal, though its reason has moved: an
	// `ask` state on a `deny` row is refused because the two disagree, not
	// because the vocabulary lacks ask.
	askSpec := `{"command":"git reset --hard*","state":"ask"}`

	// Admitted at authoring: state agrees with action, the pattern is one the
	// matcher accepts, and the boundary's ask rule is deny-like — the agent
	// source here is deliberate; an ask cannot widen anyone, so anyone may
	// write one.
	admitted := commandPolicy(t, "ask-ok", policy.AgentKind, "main", policy.Ask, askSpec)
	if rej := validateWith(t, policy.CommandAccess, admitted); rej != nil {
		t.Fatalf("the write boundary refused an agreeing ask row; ask is in the closed set since 0019: %s", rej.Reason)
	}

	// The contradiction is still refused, naming the state and the shape:
	// two truths about one decision stay impossible.
	p := commandPolicy(t, "ask-row", policy.AgentKind, "main", policy.Deny, askSpec)
	rej := validateWith(t, policy.CommandAccess, p)
	if rej == nil {
		t.Fatal(`an "ask" state on a "deny" row was accepted; state must still say what the action says`)
	}
	if !strings.Contains(rej.Reason, `"state"`) || !strings.Contains(rej.Reason, commandAccessShapeText) {
		t.Errorf("the refusal %q names neither the state field nor the pinned shape", rej.Reason)
	}

	// Decides at evaluation: the agreeing row on both sides (hand-written SQL
	// — the stored shape is identical to what Add writes) asks, names itself,
	// and does not permit.
	askRow := commandPolicy(t, "ask-stored", policy.AgentKind, "main", policy.Ask, askSpec)
	v := policy.Evaluate([]policy.Policy{askRow}, commandReq("main", "git reset --hard HEAD"), evalAt)
	assertAskVerdict(t, v, "ask-stored", "")

	res := commandEvaluatorForTheRegistry(t).Match(askRow, commandReq("main", "git reset --hard HEAD"))
	if !res.Matched || res.Unimplemented {
		t.Errorf("Match on an agreeing ask row = %+v; it matches, deny-like, and this type decides", res)
	}
	if res.Note == "" {
		t.Error("a matched ask carries no Note; the segment index is what T12 reads")
	}

	// The existential arm over segments — the DENY arm's shape, not the allow
	// arm's universal one: a second, innocent segment cannot dodge the
	// question the pattern names.
	v = policy.Evaluate([]policy.Policy{askRow}, commandReq("main", "git status && git reset --hard HEAD"), evalAt)
	assertAskVerdict(t, v, "ask-stored", "")

	// And a command outside the pattern asks nothing: PL5 answers, no rule is
	// credited.
	v = policy.Evaluate([]policy.Policy{askRow}, commandReq("main", "git status"), evalAt)
	assertVerdict(t, v, false, true, false, "", "")
}

// ── the fold ─────────────────────────────────────────────────────────────────

func TestCommandAccessDenyBeatsAllowWithinOneSet(t *testing.T) {
	// The reason the seed design's first-match fold is not here. An operator
	// writes a broad allow first and a specific deny later; first-match would
	// answer the allow and stop reading. The shipped engine (PL4) answers the
	// deny whichever order the rows were written in, whichever side is more
	// specific, and names the grant it overrode.
	grant := commandPolicy(t, "git-for-main", policy.AgentKind, "golang-engineer-loop", policy.Allow,
		commandSpec(t, "git*", "allow"))
	ban := commandPolicy(t, "no-push-anywhere", policy.AllAgents, "", policy.Deny,
		commandSpec(t, "git push*", "deny"))
	req := commandReq("golang-engineer-loop", "git push origin main")

	for name, set := range map[string][]policy.Policy{
		"allow written first": {grant, ban},
		"deny written first":  {ban, grant},
	} {
		t.Run(name, func(t *testing.T) {
			v := policy.Evaluate(set, req, evalAt)
			assertVerdict(t, v, false, false, false, ban.ID, grant.ID)
			if v.By.Target.Kind != policy.AllAgents {
				t.Errorf("the deny the verdict names targets %q; the broad rule is the one that decided",
					v.By.Target.Kind)
			}
		})
	}

	t.Run("the same allow stands when the deny names another command", func(t *testing.T) {
		// The control: the denial above is about the command, not about the
		// agent. Change what it names and the grant is simply an allow.
		other := commandPolicy(t, "no-rm-anywhere", policy.AllAgents, "", policy.Deny,
			commandSpec(t, "rm*", "deny"))
		v := policy.Evaluate([]policy.Policy{grant, other}, req, evalAt)
		assertVerdict(t, v, true, false, false, grant.ID, "")
	})

	t.Run("an earlier broad allow cannot rescue a denied command", func(t *testing.T) {
		// The corpus's own shape: `"*": allow` first, denials after it. This is
		// the exact case the measured first-match disagreement turned on.
		corpusStyle := []policy.Policy{
			commandPolicy(t, "a-star-allow", policy.AgentKind, "code-reviewer-loop", policy.Allow,
				commandSpec(t, "*", "allow")),
			commandPolicy(t, "z-no-push", policy.AgentKind, "code-reviewer-loop", policy.Deny,
				commandSpec(t, "git push*", "deny")),
		}
		v := policy.Evaluate(corpusStyle, commandReq("code-reviewer-loop", "git push"), evalAt)
		assertVerdict(t, v, false, false, false, "z-no-push", "a-star-allow")
	})
}

func TestCommandAccessAnyDeniedSegmentDeniesAll(t *testing.T) {
	// One refused part refuses the whole line. `cd sub && git push` is denied by
	// a rule about `git push` — and the refusal says WHICH part, as an index,
	// because the segment's text is policy content and a Note is a log line.
	ban := commandPolicy(t, "no-push", policy.AgentKind, "main", policy.Deny,
		commandSpec(t, "git push*", "deny"))
	ev := commandEvaluatorForTheRegistry(t)

	t.Run("the second segment decides the whole command", func(t *testing.T) {
		v := policy.Evaluate([]policy.Policy{ban}, commandReq("main", "cd sub && git push origin"), evalAt)
		assertVerdict(t, v, false, false, false, ban.ID, "")
		res := ev.Match(ban, commandReq("main", "cd sub && git push origin"))
		if !res.Matched {
			t.Fatalf("a deny row whose pattern names one segment matched nothing: %+v", res)
		}
		if !strings.Contains(res.Note, "segment 2 of 2") {
			t.Errorf("the Note %q does not name segment 2 of 2; T12's question and `why` need the index", res.Note)
		}
	})

	t.Run("the first segment decides it just as well", func(t *testing.T) {
		v := policy.Evaluate([]policy.Policy{ban}, commandReq("main", "git push && echo done"), evalAt)
		assertVerdict(t, v, false, false, false, ban.ID, "")
		if got := ev.Match(ban, commandReq("main", "git push && echo done")).Note; !strings.Contains(got, "segment 1 of 2") {
			t.Errorf("the Note %q does not name segment 1 of 2", got)
		}
	})

	t.Run("no segment matching is no denial", func(t *testing.T) {
		// The control: the denial above is about the command, not about the
		// agent's right to run anything.
		v := policy.Evaluate([]policy.Policy{ban}, commandReq("main", "cd sub && ls"), evalAt)
		assertVerdict(t, v, false, true, false, "", "")
	})

	t.Run("every separator the guard knows carries a denied segment", func(t *testing.T) {
		for _, line := range []string{"ls; git push", "true || git push", "echo x\ngit push"} {
			v := policy.Evaluate([]policy.Policy{ban}, commandReq("main", line), evalAt)
			assertVerdict(t, v, false, false, false, ban.ID, "")
		}
	})
}

func TestCommandAccessAllowRequiresEverySegment(t *testing.T) {
	// The asymmetry that keeps an allow inside its own command: an allow row
	// matches only when EVERY segment falls inside its pattern. The mirror
	// ("any segment matches") is the over-permit this type exists to refuse —
	// it would let `git status*` grant `git status && git push`, because the
	// half it checked was the half it liked.
	grant := commandPolicy(t, "status-only", policy.AgentKind, "main", policy.Allow,
		commandSpec(t, "git status*", "allow"))

	for _, c := range []struct {
		name    string
		command string
		allowed bool
	}{
		{"one segment inside the pattern", "git status", true},
		{"two segments, both inside", "git status && git status --short", true},
		{"the second segment is outside", "git status && git push", false},
		{"the first segment is outside", "git push && git status", false},
		{"a separator the guard does not know stays one segment", "git status && git push --help", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			v := policy.Evaluate([]policy.Policy{grant}, commandReq("main", c.command), evalAt)
			assertVerdict(t, v, c.allowed, !c.allowed, false, allowed(c.allowed, grant.ID), "")
		})
	}

	t.Run("a denied segment wins over an allow that covers most of the line", func(t *testing.T) {
		// The two arms together: the allow covers `git status*`, the deny names
		// `git push*`, and the line has both. The allow row is dropped entirely
		// — it does not cover every segment — so the verdict names the deny and
		// no overridden grant, which is the asymmetry made visible: a partial
		// allow is not a grant that something else had to beat.
		ban := commandPolicy(t, "no-push", policy.AgentKind, "main", policy.Deny,
			commandSpec(t, "git push*", "deny"))
		v := policy.Evaluate([]policy.Policy{grant, ban}, commandReq("main", "git status && git push"), evalAt)
		assertVerdict(t, v, false, false, false, ban.ID, "")
	})

	t.Run("an allow that does cover every segment is the grant a deny beats", func(t *testing.T) {
		// The control on the assertion above: when the allow DOES cover every
		// segment it is a grant, and PL4 makes the deny name it as overridden.
		// Without this, the empty OverriddenAllow above would be indistinguishable
		// from a broken verdict.
		covers := commandPolicy(t, "all-git", policy.AgentKind, "main", policy.Allow,
			commandSpec(t, "git*", "allow"))
		ban := commandPolicy(t, "no-push", policy.AgentKind, "main", policy.Deny,
			commandSpec(t, "git push*", "deny"))
		v := policy.Evaluate([]policy.Policy{covers, ban}, commandReq("main", "git status && git push"), evalAt)
		assertVerdict(t, v, false, false, false, ban.ID, covers.ID)
	})
}

func TestCommandAccessExactMatchTrap(t *testing.T) {
	// The matcher's rule, re-proven through the policy path: a pattern with no
	// '*' matches ONLY the identical string. A bare `gh` is not a prefix and not
	// a wildcard — the over-permit it would otherwise be is a grant no operator
	// authored, which is why this is a named test rather than a table row.
	grant := commandPolicy(t, "gh-exact", policy.AgentKind, "main", policy.Allow,
		commandSpec(t, "gh", "allow"))

	for _, c := range []struct {
		name    string
		command string
		allowed bool
	}{
		{"the identical command", "gh", true},
		{"a longer command", "gh pr merge", false},
		{"the same word with an argument", "gh --help", false},
		{"a same-length neighbour", "ghe", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			v := policy.Evaluate([]policy.Policy{grant}, commandReq("main", c.command), evalAt)
			assertVerdict(t, v, c.allowed, !c.allowed, false, allowed(c.allowed, grant.ID), "")
		})
	}

	t.Run("a trailing star is the only way to write a prefix", func(t *testing.T) {
		// The readable alternative, asserted so the trap above is a rule about
		// spelling and not a mystery.
		prefix := commandPolicy(t, "gh-any", policy.AgentKind, "main", policy.Allow,
			commandSpec(t, "gh*", "allow"))
		v := policy.Evaluate([]policy.Policy{prefix}, commandReq("main", "gh pr merge"), evalAt)
		assertVerdict(t, v, true, false, false, prefix.ID, "")
	})
}

// ── inertness: the evaluator decides, and declines, but never refuses to decide

func TestCommandAccessUnreadableSpecIsInert(t *testing.T) {
	// The write boundary and the evaluator are two different guarantees, and the
	// evaluator's is the one that protects the runtime: rows predate validators
	// and can be written by hand. None of them may become a panic in the daemon
	// or, worse, a permission. Matched false AND Unimplemented false, every
	// time — the type decides now, so "could not determine" is not an answer it
	// is allowed to give.
	bad := []struct {
		name string
		spec string
	}{
		{"malformed JSON", `{"command":"git push",`},
		{"not an object", `42`},
		{"an array", `["git push","git pull"]`},
		{"null", `null`},
		{"empty", ``},
		{"an unknown field", `{"cmd":"git push"}`},
		{"a spec of nothing but a secret", `{"secret":"hunter2-in-a-spec"}`},
		{"the wrong field type", `{"command":{"nested":"deep"},"state":"deny"}`},
		{"trailing content", `{"command":"pwd","state":"deny"}{"command":"ls","state":"deny"}`},
		// The duplicated key that matters is the one whose LAST value contradicts
		// the row: a lenient decode keeps the last "state", and the row's action
		// then disagrees with the spec it carries.
		{"a duplicated key", `{"command":"pwd","state":"allow","state":"deny"}`},
	}
	ev := commandEvaluatorForTheRegistry(t)
	for _, c := range bad {
		t.Run(c.name, func(t *testing.T) {
			row := commandPolicy(t, "bad", policy.AgentKind, "main", policy.Allow, c.spec)
			if rej := validateWith(t, policy.CommandAccess, row); rej == nil {
				t.Fatalf("the validator accepted %s (%s); the evaluator's totality is a backstop, not a "+
					"reason to loosen the write path", c.name, c.spec)
			}
			res := ev.Match(row, commandReq("main", "git push"))
			if res.Matched || res.Unimplemented {
				t.Errorf("Match(%s) = %+v; an unreadable spec matches nothing and is not unimplemented", c.spec, res)
			}
			if strings.TrimSpace(res.Note) == "" {
				t.Errorf("Match(%s) declined with no Note; a refusal names what it refused", c.spec)
			}
			v := policy.Evaluate([]policy.Policy{row}, commandReq("main", "git push"), evalAt)
			assertVerdict(t, v, false, true, false, "", "")
		})
	}
}

func TestCommandAccessNeverAnswersUnimplemented(t *testing.T) {
	// The D10 flag belongs to the placeholder this file replaced. If
	// CommandAccess ever started answering Unimplemented, the in-force answer
	// would move back to "could not determine" — an operator would go looking
	// for the evaluator that has already shipped. Every shape the store can
	// hand back is swept, and none of them may set the flag.
	ev := commandEvaluatorForTheRegistry(t)
	rows := []policy.Policy{
		commandPolicy(t, "good-deny", policy.AgentKind, "main", policy.Deny, commandSpec(t, "git push*", "deny")),
		commandPolicy(t, "good-allow", policy.AgentKind, "main", policy.Allow, commandSpec(t, "git*", "allow")),
		commandPolicy(t, "unreadable", policy.AgentKind, "main", policy.Allow, `{"command":`),
		commandPolicy(t, "contradictory", policy.AgentKind, "main", policy.Deny, commandSpec(t, "git*", "allow")),
		commandPolicy(t, "empty-pattern", policy.AgentKind, "main", policy.Allow, commandSpec(t, "", "allow")),
		commandPolicy(t, "bad-pattern", policy.AgentKind, "main", policy.Deny, commandSpec(t, "git**push", "deny")),
		commandPolicy(t, "good-ask", policy.AgentKind, "main", policy.Ask, commandSpec(t, "git*", "ask")),
		commandPolicy(t, "no-action", policy.AgentKind, "main", policy.Action("permit"), commandSpec(t, "git*", "permit")),
	}
	for _, row := range rows {
		for _, command := range []string{"", "git push", "git status && git push", "a && " + strings.Repeat("x && ", policy.MaxSegments)} {
			res := ev.Match(row, commandReq("main", command))
			if res.Unimplemented {
				t.Errorf("Match(row %q, %q) answered Unimplemented; this type decides — the flag is the "+
					"placeholder's, and it went away with the placeholder", row.ID, command)
			}
			if res.Matched && res.Note == "" && (row.Action == policy.Deny || row.Action == policy.Ask) {
				t.Errorf("a matching %s carried no Note; the segment index is what T12 reads: %+v", row.Action, res)
			}
			v := policy.Evaluate([]policy.Policy{row}, commandReq("main", command), evalAt)
			if v.Unimplemented {
				t.Errorf("Evaluate over row %q with command %q refused to decide (%+v)", row.ID, command, v)
			}
		}
	}
}

func TestCommandAccessOverLimitCommandIsNotPermitted(t *testing.T) {
	// More segments than MaxSegments is not analysed at all, and "not analysed"
	// resolves toward DENY (D11). The splitter states OverLimit rather than
	// truncating quietly; the evaluator must read that flag and refuse to match,
	// so a pathological line can neither be permitted nor be denied by a rule
	// that only saw the first 32 segments.
	many := strings.Join(append(repeat("git push", policy.MaxSegments+1), "echo done"), " && ")
	if got := len(policy.Split(many).Items); got != policy.MaxSegments {
		t.Fatalf("the splitter kept %d items, not the documented MaxSegments=%d; the fixture below "+
			"proves nothing", got, policy.MaxSegments)
	}
	if !policy.Split(many).OverLimit {
		t.Fatal("the fixture is not over the limit; the assertion below would pass vacuously")
	}

	allow := commandPolicy(t, "allow-everything-matching", policy.AgentKind, "main", policy.Allow,
		commandSpec(t, "git push*", "allow"))
	ban := commandPolicy(t, "deny-git-push", policy.AgentKind, "main", policy.Deny,
		commandSpec(t, "git push*", "deny"))

	v := policy.Evaluate([]policy.Policy{allow}, commandReq("main", many), evalAt)
	assertVerdict(t, v, false, true, false, "", "")

	// The deny arm is inert too: an incomplete reading must not become a named
	// denial either, because the refusal would claim a segment the engine never
	// read. The command still does not run (PL5), and no rule is credited.
	v = policy.Evaluate([]policy.Policy{ban}, commandReq("main", many), evalAt)
	assertVerdict(t, v, false, true, false, "", "")

	res := commandEvaluatorForTheRegistry(t).Match(allow, commandReq("main", many))
	if res.Matched || res.Unimplemented {
		t.Errorf("Match on an over-limit command = %+v; it matches nothing and does not refuse to decide", res)
	}
	if !strings.Contains(res.Note, "segment") {
		t.Errorf("the Note %q does not say the segment limit is why nothing was permitted", res.Note)
	}
}

func TestCommandAccessEmptyCommandIsNotPermitted(t *testing.T) {
	// A command with no segment asks for nothing, and an allow arm over zero
	// segments is vacuously true. That is how every content-free command would
	// inherit every grant, so the arm is guarded explicitly rather than left to
	// the luck of an empty loop.
	grant := commandPolicy(t, "any-git", policy.AgentKind, "main", policy.Allow,
		commandSpec(t, "*", "allow"))
	for _, command := range []string{"", "   ", ";", " && ", "\n"} {
		v := policy.Evaluate([]policy.Policy{grant}, commandReq("main", command), evalAt)
		assertVerdict(t, v, false, true, false, "", "")
	}
}

func TestCommandAccessNotesNeverCarryCommandContent(t *testing.T) {
	// MatchRes.Note is a log line, and the slice promises its logs and events
	// carry no policy content (legal constraint 1). The command and the pattern
	// are both policy content — including whatever an agent typed into a
	// command — so a Note may name an index and a shape and nothing else.
	ev := commandEvaluatorForTheRegistry(t)
	secrets := []string{"hunter2", "s3cr3t-path", "topsecret"}
	commands := []string{"cat hunter2", "ls s3cr3t-path", "rm topsecret"}
	// The banned set is the content itself, never a short word: "rm" appears in
	// fixed prose ("permitted"), and a guard that cries wolf on its own wording
	// gets weakened until it catches nothing.
	banned := append(append([]string{}, secrets...), commands...)
	rows := []policy.Policy{
		commandPolicy(t, "leaky-spec", policy.AgentKind, "main", policy.Allow, `{"secret":"hunter2"}`),
		commandPolicy(t, "leaky-pattern", policy.AgentKind, "main", policy.Deny, commandSpec(t, "s3cr3t-path*", "deny")),
		commandPolicy(t, "leaky-state", policy.AgentKind, "main", policy.Deny, commandSpec(t, "topsecret*", "allow")),
	}
	for _, row := range rows {
		for _, command := range commands {
			res := ev.Match(row, commandReq("main", command))
			for _, leak := range banned {
				if strings.Contains(res.Note, leak) {
					t.Errorf("the Note %q leaks %q (row %q, command %q); a log line names the shape, never "+
						"the content", res.Note, leak, row.ID, command)
				}
			}
		}
	}
}

func TestCommandAccessForeignTypeIsNotMe(t *testing.T) {
	// The engine filters by type before it calls Match, so this is the belt
	// rather than the braces — and a belt nobody has tested is a belt nobody
	// can rely on. Asked about another type, CommandAccess says "not me": not a
	// match, and not a refusal to decide either.
	ev := commandEvaluatorForTheRegistry(t)
	row := commandPolicy(t, "fine-but-not-my-type", policy.AgentKind, "main", policy.Allow,
		commandSpec(t, "git*", "allow"))
	res := ev.Match(row, policy.Request{Type: policy.ToolAccess, Agent: "main", Tool: "bash", Command: "git push"})
	if res.Matched || res.Unimplemented || strings.TrimSpace(res.Note) == "" {
		t.Errorf("Match on a foreign type = %+v; the answer is \"not me\", which is neither a match nor a "+
			"refusal to decide", res)
	}
}

// ── the registry, from the filled type's side ────────────────────────────────

func TestCommandAccessIsRegisteredAsARealType(t *testing.T) {
	// The registration flip, asserted where the flip lives: both halves come
	// from spec_commandaccess.go, the type is not a stub, and the shape the
	// validator reports is the three-state one the refusals quote.
	ev, ok := policy.EvaluatorFor(policy.CommandAccess)
	if !ok {
		t.Fatal("CommandAccess has no evaluator; S11's whole task was to give it one")
	}
	if marker, isStub := ev.(policy.StubMarker); isStub && marker.Stub() {
		t.Fatal("CommandAccess still declares itself the placeholder; the marker travels with the evaluator " +
			"it replaces, and the in-force answer must move to an answer about enforcement")
	}
	schema := schemaOf(t, policy.CommandAccess)
	if schema.Type != policy.CommandAccess || schema.Shape != commandAccessShapeText {
		t.Errorf("the validator reports %+v; the shape must be exactly %s", schema, commandAccessShapeText)
	}
	if !strings.Contains(schema.Shape, `"ask"`) {
		t.Errorf("the shape %q does not advertise the third state; the evaluator is written against the "+
			"final vocabulary once, not patched twice", schema.Shape)
	}
}

// ── small local helpers ──────────────────────────────────────────────────────

// allowed maps a bool to the policy id a verdict should name, so the table
// above can stay a table.
func allowed(ok bool, id string) string {
	if ok {
		return id
	}
	return ""
}

// repeat builds n distinct copies of s, for the over-limit fixture.
func repeat(s string, n int) []string {
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, fmt.Sprintf("%s%d", s, i))
	}
	return out
}
