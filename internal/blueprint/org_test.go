package blueprint

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/handler"
)

// S38 T11 — the output contract keys: output, emits, requires, decides.

func orgStepOpts() ValidationOptions {
	return ValidationOptions{Mode: "standard", Agents: handler.Names(), Resolvers: handler.Resolvers(), AgentTools: handler.ToolsFor}
}

func orgBP(step Step) Blueprint {
	return Blueprint{ID: "org", SchemaVersion: SupportedSchemaVersion, Version: 1,
		Lanes: []Lane{{ID: "one", Step: &step}}}
}

func hasDiag(r Report, state DiagState, kind string) bool {
	for _, d := range r.Diagnostics {
		if d.Kind == kind && (state == "" || d.State == state) {
			return true
		}
	}
	return false
}

// The emit-tool for a FULLY granted test agent: the handler check is the
// point, so most cases run against a name whose grant list we hand the
// validator directly.
func fullGrant(agent string) ([]string, bool) {
	return []string{"read", "goal_create", "epic_create", "brief_create", "task_create", "attach"}, true
}

func TestEmitsOnlyOnModelSteps(t *testing.T) {
	step := Step{ID: "g", Kind: Gate, Question: "q", Emits: []Emit{{Type: "goal"}}}
	b := orgBP(step)
	b.Questions = map[string]Question{"q": {Prompt: "p", Answers: []string{"ok"}}}
	r := Validate(b, orgStepOpts())
	if !hasDiag(r, Invalid, "emits-needs-model-step") {
		t.Fatalf("a gate emitting goals read fine: %+v", r.Diagnostics)
	}
}

func TestEmitsUnknownType(t *testing.T) {
	step := Step{ID: "m", Kind: ModelStep, Agent: "x", Emits: []Emit{{Type: "banana"}}}
	r := Validate(orgBP(step), ValidationOptions{Mode: "standard", Agents: []string{"x"}, AgentTools: fullGrant})
	if !hasDiag(r, Invalid, "emits-unknown-type") {
		t.Fatalf("banana emitted unchallenged: %+v", r.Diagnostics)
	}
}

func TestEmitsToolMissingIsInvalid(t *testing.T) {
	// The REAL body is the witness: intent-clarifier is granted
	// read/write/edit/todo — never task_create — so a step of that agent
	// emitting tasks is an Invalid, naming the agent and the tool.
	step := Step{ID: "m", Kind: ModelStep, Agent: "intent-clarifier", Emits: []Emit{{Type: "task"}}}
	r := Validate(orgBP(step), orgStepOpts())
	if !hasDiag(r, Invalid, "emits-tool-missing") {
		t.Fatalf("an ungranted emit read fine: %+v", r.Diagnostics)
	}
	for _, d := range r.Diagnostics {
		if d.Kind == "emits-tool-missing" && (!strings.Contains(d.Msg, "intent-clarifier") || !strings.Contains(d.Msg, "task_create")) {
			t.Fatalf("the refusal must name agent and tool: %q", d.Msg)
		}
	}
}

func TestEmitsWithAbsentHandlerIsUnverifiable(t *testing.T) {
	// P31: no body is not a false verdict — the third state arrives, and
	// NOT the invalid one.
	step := Step{ID: "m", Kind: ModelStep, Agent: "body-that-does-not-exist", Emits: []Emit{{Type: "goal"}}}
	r := Validate(orgBP(step), ValidationOptions{Mode: "standard", Agents: []string{"body-that-does-not-exist"}, AgentTools: handler.ToolsFor})
	if !hasDiag(r, Unverifiable, "emits-agent-unverifiable") {
		t.Fatalf("absent handler did not answer the third state: %+v", r.Diagnostics)
	}
	if hasDiag(r, Invalid, "emits-tool-missing") {
		t.Fatal("an absent handler was judged as an ungranted one — P31 collapsed")
	}
}

func TestAttachMustBeProduced(t *testing.T) {
	step := Step{ID: "m", Kind: ModelStep, Agent: "x", Emits: []Emit{{Type: "goal", Attach: []string{"draft.md"}}}}
	r := Validate(orgBP(step), ValidationOptions{Mode: "standard", Agents: []string{"x"}, AgentTools: fullGrant})
	if !hasDiag(r, Invalid, "emits-attach-not-produced") {
		t.Fatalf("a pointer to nobody's file passed: %+v", r.Diagnostics)
	}
	step.Produces = []string{"draft.md"}
	r = Validate(orgBP(step), ValidationOptions{Mode: "standard", Agents: []string{"x"}, AgentTools: fullGrant})
	if hasDiag(r, Invalid, "emits-attach-not-produced") {
		t.Fatalf("a produced file still refused: %+v", r.Diagnostics)
	}
	// The attach half of the grant: attaching needs the attach tool too.
	step2 := Step{ID: "m", Kind: ModelStep, Agent: "x", Produces: []string{"draft.md"}, Emits: []Emit{{Type: "goal", Attach: []string{"draft.md"}}}}
	r = Validate(orgBP(step2), ValidationOptions{Mode: "standard", Agents: []string{"x"},
		AgentTools: func(string) ([]string, bool) { return []string{"goal_create"}, true }})
	if !hasDiag(r, Invalid, "emits-tool-missing") {
		t.Fatalf("attaching without the attach tool passed: %+v", r.Diagnostics)
	}
}

func TestDecidesOnlyOnGates(t *testing.T) {
	step := Step{ID: "m", Kind: ModelStep, Agent: "x", Decides: "goal"}
	r := Validate(orgBP(step), ValidationOptions{Mode: "standard", Agents: []string{"x"}})
	if !hasDiag(r, Invalid, "decides-unknown-or-misplaced") {
		t.Fatalf("a model step deciding read fine: %+v", r.Diagnostics)
	}
	// orgBP takes the Step by value, so each shape gets its own blueprint
	// (mutating a local afterwards would mutate nothing — measured).
	legit := Step{ID: "g", Kind: Gate, Question: "q", Decides: "goal"}
	b := orgBP(legit)
	b.Questions = map[string]Question{"q": {Prompt: "p", Answers: []string{"ok"}}}
	r = Validate(b, orgStepOpts())
	if hasDiag(r, Invalid, "decides-unknown-or-misplaced") {
		t.Fatalf("the legitimate shape was refused: %+v", r.Diagnostics)
	}
	// A gate decides goal or brief — an epic has no single human verdict here.
	bad := Step{ID: "g", Kind: Gate, Question: "q", Decides: "epic"}
	bb := orgBP(bad)
	bb.Questions = map[string]Question{"q": {Prompt: "p", Answers: []string{"ok"}}}
	r = Validate(bb, orgStepOpts())
	if !hasDiag(r, Invalid, "decides-unknown-or-misplaced") {
		t.Fatalf("deciding an epic read fine: %+v", r.Diagnostics)
	}
}

func TestRequiresVocabulary(t *testing.T) {
	b := orgBP(Step{ID: "m", Kind: ModelStep, Agent: "x"})
	b.Requires = []Requirement{{AnyOf: []RequirementTerm{{Type: "banana", State: "approved"}}}}
	r := Validate(b, ValidationOptions{Mode: "standard", Agents: []string{"x"}})
	if !hasDiag(r, Invalid, "requires-unknown-type") {
		t.Fatalf("banana required: %+v", r.Diagnostics)
	}
	b.Requires = []Requirement{{AnyOf: []RequirementTerm{{Type: "goal", State: "merged"}}}}
	r = Validate(b, ValidationOptions{Mode: "standard", Agents: []string{"x"}})
	if !hasDiag(r, Invalid, "requires-unknown-state") {
		t.Fatalf("a non-personal state required: %+v", r.Diagnostics)
	}
	// The legitimate shape: goal approved OR task approved.
	b.Requires = []Requirement{{AnyOf: []RequirementTerm{{Type: "goal", State: "approved"}, {Type: "task", State: "approved"}}}}
	r = Validate(b, ValidationOptions{Mode: "standard", Agents: []string{"x"}})
	if hasDiag(r, Invalid, "requires-unknown-type") || hasDiag(r, Invalid, "requires-unknown-state") {
		t.Fatalf("the legitimate vocabulary was refused: %+v", r.Diagnostics)
	}
}

func TestOutputUnreachable(t *testing.T) {
	b := orgBP(Step{ID: "m", Kind: ModelStep, Agent: "x"})
	b.Output = []string{"goal"} // nobody emits
	r := Validate(b, ValidationOptions{Mode: "standard", Agents: []string{"x"}})
	if !hasDiag(r, Invalid, "output-unreachable") {
		t.Fatalf("an unreachable deliverable read fine: %+v", r.Diagnostics)
	}
	b.Lanes[0].Step.Emits = []Emit{{Type: "goal"}}
	r = Validate(b, ValidationOptions{Mode: "standard", Agents: []string{"x"}, AgentTools: fullGrant})
	if hasDiag(r, Invalid, "output-unreachable") {
		t.Fatalf("the reachable goal was refused: %+v", r.Diagnostics)
	}
	// A file reachable through produces; and unreachable when not.
	b.Output = []string{"report.md"}
	r = Validate(b, ValidationOptions{Mode: "standard", Agents: []string{"x"}, AgentTools: fullGrant})
	if !hasDiag(r, Invalid, "output-unreachable") {
		t.Fatalf("a file nothing produces read fine: %+v", r.Diagnostics)
	}
	b.Lanes[0].Step.Produces = []string{"report.md"}
	r = Validate(b, ValidationOptions{Mode: "standard", Agents: []string{"x"}, AgentTools: fullGrant})
	if hasDiag(r, Invalid, "output-unreachable") {
		t.Fatalf("the reachable file was refused: %+v", r.Diagnostics)
	}
}

// TestAllShippedBlueprintsLoadUnchanged is step 5's proof: the four keys
// are OPTIONAL, so every shipped blueprint must load with zero invalid and
// not one BYTE of the shipped set may move in this task (T12/T14 edit them,
// with their own commits and their own proofs).
func TestAllShippedBlueprintsLoadUnchanged(t *testing.T) {
	want := map[string]string{
		"conversational-map":       "46796e123b0c68fde0e2e9deb95d6372219a7bfeeb1422c257a565208d45b4b9",
		"development-map-direct":   "9b6666048806c75a179becdfa1160ecd0a50623011e68fe51b2e762341114ee1",
		"development-map-pipeline": "866b6e2674fe407b4b665707f6754d22ad4ee9da0644008df2fbf1a7fead8ab7",
		"dev-loop":                 "a19fd6cc26811185c45b46dc9cffe7918796dea8071743403fe35320d28fb72d",
		"proposal-brief":           "b5f744d3775a4ad576b7ce5e5ef44c0efd801b1d9c805f233ac6f61560614dda",
		"research-map":             "a3a499ca009f80807009025bd8144357e3473dc09a3da89913c1c1718984cbc4",
		"tag-only":                 "b5bc01598ea0ca069f9eec180bd15b3167b8b575925c1fbb31263b7e27c0b08e",
		"two-step":                 "73021e75544f611b0648fce28c3ee020062cacb6843f8e6e3d9f159a3de3cff8",
		// The S38 T14 pair. The byte-identity promise for a NEW file is its
		// table sha from its landing commit onward; the git-show leg below
		// skips names HEAD does not carry yet (measured: the test runs in
		// the same commit that adds the files).
		"brainstorm":    "36d747903062024a8e0f4abfe5fd2465c5a895de3a806e3b456a6bc2d2224bf8",
		"goal-to-brief": "46addfc2d327175a2e48655d518cb992e542f305beaecdd871acf2deaea14ca2",
		// S39 T6 (CR1/CR3): the plain conversation ships — one lane, one
		// step by chat, after: converse. New file: the git-show leg below
		// skips it until HEAD carries it; the table sha is its pin from
		// this commit.
		"chat": "a8f14a981201cd89e55decb3bfe592696492318542d99f3094931f2b71c34e36",
		// S50 T2 (RM3): the plan host moves from a daemon string constant
		// to a seeded record — the file is the OLD constant's bytes
		// verbatim (sha pinned below; the characterization test in
		// internal/daemon keeps the identity), so the pin starts here.
		"roadmap-plan": "08aa5c6c34760d0bb6f0888ec1e1df490483c6b5d970bf688770491f3e8aa394",
	}
	// Authorized in-place edits to the shipped set, named per the slice's
	// amendment rule: name → the bytes HEAD must still carry (the table
	// above pins the NEW on-disk truth; HEAD pinned to the pre-edit sha
	// proves the rest of history has not moved and the diff is exactly
	// this slice's rest). brainstorm: S39 T6 added `after: converse` +
	// `converse: brainstormer` UNRELEASED (no tag contains its landing
	// commit 5db30369), so version stays 1 (T6 step 5's in-place branch).
	preEdit := map[string]string{
		"brainstorm": "ca4e9ce778b48f726f6d51ec4d2a9cac621f712975c29817afb381511f9ff669",
	}
	// StandardNames and the table must be one truth — a name arriving
	// un-tabled is a file nobody pinned.
	if len(StandardNames()) != len(want) {
		t.Fatalf("the shipped set is %d files, the table names %d — re-measure both", len(StandardNames()), len(want))
	}
	root, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		t.Skipf("no git to consult: %v", err)
	}
	repo := string(trim(root))
	for _, name := range StandardNames() {
		raw, err := os.ReadFile(filepath.Join("blueprints", name+".yaml"))
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(raw)
		if hex := hexOf(sum); hex != want[name] {
			t.Errorf("%s is not the byte we shipped: sha %s", name, hex)
		}
		if err := exec.Command("git", "-C", repo, "cat-file", "-e", "HEAD:aeon/internal/blueprint/blueprints/"+name+".yaml").Run(); err != nil {
			continue // brand-new file: the table sha is its pin from this commit
		}
		head, err := exec.Command("git", "-C", repo, "show", "HEAD:aeon/internal/blueprint/blueprints/"+name+".yaml").Output()
		if err != nil {
			t.Fatalf("%s has no HEAD bytes (unshipped?): %v", name, err)
		}
		// HEAD must carry EITHER the currently-pinned bytes (the normal
		// byte-identity leg) OR the pre-edit bytes of an authorised in-place
		// edit — which it does only until that edit's own commit lands;
		// after it, the first branch holds again. Any third shape of history
		// is movement nobody scheduled. (Amended S39 T7: the original form
		// compared preEdit ONLY, which the T6 commit itself broke — HEAD
		// catches up to the rest, and a pin must not expire on landing.)
		if h := hexOf(sha256.Sum256(head)); h != want[name] {
			if p, ok := preEdit[name]; !ok || h != p {
				t.Errorf("%s differs from git show HEAD — this task does not touch shipped blueprints (T12/T14 own that, with their own proofs; S39 T6 owns brainstorm's rest, pinned above)", name)
			}
		}
		r := Analyze(raw, ValidationOptions{Mode: "standard", Agents: handler.Names(), Resolvers: handler.Resolvers(), Actions: s16ActionSet, AgentTools: handler.ToolsFor})
		if _, invalid, _ := r.Counts(); invalid != 0 {
			t.Errorf("%s gained invalid diagnostics under the new keys: %+v", name, r.Diagnostics)
		}
	}
}

func trim(b []byte) []byte {
	for len(b) > 0 && (b[len(b)-1] == '\n' || b[len(b)-1] == '\r') {
		b = b[:len(b)-1]
	}
	return b
}

func hexOf(sum [sha256.Size]byte) string { return hex.EncodeToString(sum[:]) }
