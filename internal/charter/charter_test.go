package charter

import (
	"context"
	"encoding/json"
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/store"
)

func fixedClock() time.Time { return time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC) }

// openService stands up a real migrated store (the charter package is a
// store consumer; the drill-grade fresh-vs-upgraded proof lives in T1's
// store tests) and a root component to charter.
func openService(t *testing.T, seam AutonomyVocab) (*Service, *store.Store, string) {
	t.Helper()
	st, err := store.Open(store.Options{
		Dir:   t.TempDir(),
		Env:   func(string) string { return "" },
		Clock: fixedClock,
	})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	w, err := st.CreateComponent(store.NewComponent{Name: "/repo", Slug: "repo", Root: "/repo"})
	if err != nil || !w.OK() {
		t.Fatalf("seed root: err=%v refusal=%s %s", err, w.Refusal, w.Reason)
	}
	return &Service{Store: st, Autonomy: seam}, st, w.Component.ID
}

func giBody() Body {
	budget := 10.0
	return Body{
		Goal:        "ship the intake plane",
		Constraints: []string{"no new daemon"},
		NonGoals:    []string{"no UI rewrite"},
		Acceptance: []Acceptance{
			{ID: "ac-1", Text: "the door answers", Check: "command", Ref: "aeon intake status"},
			{ID: "ac-2", Text: "a human approves", Check: "manual"},
		},
		Stack:    []string{"golang", "sqlite"},
		Proposed: &Proposed{BudgetUSD: &budget, EnvironmentRef: "env-x", Verify: []string{"go test ./internal/charter/"}},
	}
}

func TestCanonicalBodyIsStable(t *testing.T) {
	rawA := `{"goal":"ship","stack":["go"],"nonGoals":["no ui"],"constraints":["c"],"proposed":{"budgetUsd":10,"verify":["go test ./..."],"environmentRef":"env-x"},"acceptance":[{"id":"ac-1","text":"door answers","check":"command","ref":"aeon intake"}]}`
	rawB := `{"acceptance":[{"ref":"aeon intake","check":"command","text":"door answers","id":"ac-1"}],"proposed":{"environmentRef":"env-x","verify":["go test ./..."],"budgetUsd":10},"constraints":["c"],"nonGoals":["no ui"],"stack":["go"],"goal":"ship"}`
	a, err := Parse([]byte(rawA))
	if err != nil {
		t.Fatalf("parse A: %v", err)
	}
	b, err := Parse([]byte(rawB))
	if err != nil {
		t.Fatalf("parse B: %v", err)
	}
	ca, err := Canonical(a)
	if err != nil {
		t.Fatalf("canonical A: %v", err)
	}
	cb, err := Canonical(b)
	if err != nil {
		t.Fatalf("canonical B: %v", err)
	}
	if string(ca) != string(cb) {
		t.Fatalf("key order moved the digest:\n A %s\n B %s", ca, cb)
	}
	// Empty lists are absent lists, in both directions.
	empty, err := Parse([]byte(`{"goal":"g","constraints":[],"proposed":{"verify":[]}}`))
	if err != nil {
		t.Fatalf("parse empties: %v", err)
	}
	ce, err := Canonical(empty)
	if err != nil {
		t.Fatalf("canonical empties: %v", err)
	}
	if string(ce) != `{"goal":"g","proposed":{}}` {
		t.Fatalf("empties canonicalize to %s", ce)
	}
	// The golden pins the exact spelling — the digest drift this row
	// exists to catch is the store holding two bodies for one charter.
	golden, err := os.ReadFile(filepath.Join("testdata", "canonical.golden"))
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	if string(ca) != strings.TrimRight(string(golden), "\n") {
		t.Fatalf("canonical drift:\n got  %s\nwant %s", ca, golden)
	}
	// Round trip: canonical bytes parse back to the same bytes.
	again, err := Parse(ca)
	if err != nil {
		t.Fatalf("re-parse: %v", err)
	}
	c2, err := Canonical(again)
	if err != nil || string(c2) != string(ca) {
		t.Fatalf("round trip moved the bytes: %v %s ≠ %s", err, c2, ca)
	}
}

func TestValidateRefusesEveryCap(t *testing.T) {
	long := func(n int) string { return strings.Repeat("x", n) }
	list := func(n int) []string {
		out := make([]string, n)
		for i := range out {
			out[i] = fmt.Sprintf("item %d", i)
		}
		return out
	}
	negative := -0.01
	for _, tc := range []struct {
		name, wantKind, wantFragment string
		body                         Body
	}{
		{"goal-empty", "goal", "required", Body{Goal: ""}},
		{"goal-oversize", "goal", "4096", Body{Goal: long(MaxGoal + 1)}},
		{"goal-nul", "goal", "NUL", Body{Goal: "a\x00b"}},
		{"body-size", "body-size", "65536", Body{
			Goal:       long(MaxGoal),
			Acceptance: heavyAcceptance(MaxAcceptance),
		}},
		{"constraint-empty", "constraints", "constraints[0]", Body{Goal: "g", Constraints: []string{""}}},
		{"constraint-oversize", "constraints", "512", Body{Goal: "g", Constraints: []string{long(MaxItem + 1)}}},
		{"constraint-count", "constraints", "50", Body{Goal: "g", Constraints: list(MaxList + 1)}},
		{"nongoal-count", "nonGoals", "50", Body{Goal: "g", NonGoals: list(MaxList + 1)}},
		{"acceptance-count", "acceptance", "100", Body{Goal: "g", Acceptance: fullAcceptance(MaxAcceptance + 1)}},
		{"acceptance-id-format", "acceptance-id", "ac-<n>", Body{Goal: "g", Acceptance: []Acceptance{{ID: "x-1", Text: "t"}}}},
		{"acceptance-id-zero", "acceptance-id", "ac-<n>", Body{Goal: "g", Acceptance: []Acceptance{{ID: "ac-0", Text: "t"}}}},
		{"acceptance-id-dup", "acceptance-id", "STABLE", Body{Goal: "g", Acceptance: []Acceptance{{ID: "ac-1", Text: "a"}, {ID: "ac-1", Text: "b"}}}},
		{"acceptance-text-empty", "acceptance-text", "acceptance[0].text", Body{Goal: "g", Acceptance: []Acceptance{{ID: "ac-1", Text: ""}}}},
		{"check-unknown", "acceptance-check", "vibes", Body{Goal: "g", Acceptance: []Acceptance{{ID: "ac-1", Text: "t", Check: "vibes"}}}},
		{"ref-without-check", "acceptance-ref", "not there", Body{Goal: "g", Acceptance: []Acceptance{{ID: "ac-1", Text: "t", Ref: "go test"}}}},
		{"manual-with-ref", "acceptance-ref", "human reading", Body{Goal: "g", Acceptance: []Acceptance{{ID: "ac-1", Text: "t", Check: "manual", Ref: "go test"}}}},
		{"command-without-ref", "acceptance-ref", "phantom", Body{Goal: "g", Acceptance: []Acceptance{{ID: "ac-1", Text: "t", Check: "command"}}}},
		{"ref-oversize", "acceptance-ref", "512", Body{Goal: "g", Acceptance: []Acceptance{{ID: "ac-1", Text: "t", Check: "test", Ref: long(MaxItem + 1)}}}},
		{"stack-count", "stack", "30", Body{Goal: "g", Stack: list(MaxTags + 1)}},
		{"tag-oversize", "stack", "64", Body{Goal: "g", Stack: []string{long(MaxTag + 1)}}},
		{"tag-newline", "stack", "no NUL or newline", Body{Goal: "g", Stack: []string{"go\ntest"}}},
		{"budget-negative", "budget", "negative", Body{Goal: "g", Proposed: &Proposed{BudgetUSD: &negative}}},
		{"envref-oversize", "environment-ref", "512", Body{Goal: "g", Proposed: &Proposed{EnvironmentRef: long(MaxEnvRef + 1)}}},
		{"verify-count", "verify", "20", Body{Goal: "g", Proposed: &Proposed{Verify: list(MaxVerify + 1)}}},
		{"verify-newline", "verify", "one command line", Body{Goal: "g", Proposed: &Proposed{Verify: []string{"go test\nrm -rf"}}}},
		{"verify-nul", "verify", "NUL", Body{Goal: "g", Proposed: &Proposed{Verify: []string{"go\x00test"}}}},
		{"verify-empty", "verify", "verify[0]", Body{Goal: "g", Proposed: &Proposed{Verify: []string{""}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := Validate(tc.body, Vocab{})
			if r == nil {
				t.Fatalf("%s: accepted, want a refusal", tc.name)
			}
			if r.Kind != tc.wantKind {
				t.Fatalf("%s: kind %q, want %q (%s)", tc.name, r.Kind, tc.wantKind, r.Sentence)
			}
			if !strings.Contains(r.Sentence, tc.wantFragment) {
				t.Fatalf("%s: sentence %q does not name %q", tc.name, r.Sentence, tc.wantFragment)
			}
			if !strings.HasPrefix(r.Sentence, "charter: ") {
				t.Fatalf("%s: the sentence does not open with its plane: %q", tc.name, r.Sentence)
			}
		})
	}
	// The positive control: a body inside every bound passes.
	if r := Validate(giBody(), Vocab{}); r != nil {
		t.Fatalf("the good body was refused: %s", r.Sentence)
	}
	// budgetUsd exactly 0 passes (≥ 0; nil is unknown, 0 is a decision).
	zero := 0.0
	if r := Validate(Body{Goal: "g", Proposed: &Proposed{BudgetUSD: &zero}}, Vocab{}); r != nil {
		t.Fatalf("a zero budget is a decision, not a cap: %s", r.Sentence)
	}
}

func fullAcceptance(n int) []Acceptance {
	out := make([]Acceptance, n)
	for i := range out {
		out[i] = Acceptance{ID: fmt.Sprintf("ac-%d", i+1), Text: fmt.Sprintf("criterion %d", i+1)}
	}
	return out
}

// heavyAcceptance is the SAME count inside every per-item cap, only heavy
// enough that the WHOLE body crosses its 64 KiB ceiling — the cap that
// exists precisely because per-item caps do not bound the total.
func heavyAcceptance(n int) []Acceptance {
	out := fullAcceptance(n)
	for i := range out {
		out[i].Text = strings.Repeat("y", MaxItem)
		out[i].Check = "test"
		out[i].Ref = strings.Repeat("z", MaxItem)
	}
	return out
}

func TestUnknownFieldRefused(t *testing.T) {
	for _, tc := range []struct{ raw, want string }{
		{`{"goal":"g","evil":1}`, "evil"},
		{`{"goal":"g","proposed":{"secretary":"x"}}`, "secretary"},
		{`{"goal":"g","acceptance":[{"id":"ac-1","text":"t","mode":"any"}]}`, "mode"},
		{`{"goal":"g"} {"goal":"h"}`, "trailing"},
		{`   `, "empty"},
	} {
		_, err := Parse([]byte(tc.raw))
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("Parse(%s): %v, want a refusal naming %q", tc.raw, err, tc.want)
		}
	}
}

func TestAutonomyFieldNeedsAVocabulary(t *testing.T) {
	b := Body{Goal: "g", Proposed: &Proposed{Autonomy: "plan"}}
	r := Validate(b, Vocab{})
	if r == nil || r.Kind != "autonomy" || !strings.Contains(r.Sentence, "no autonomy vocabulary (S56)") {
		t.Fatalf("absent seam: %v, want the S56 sentence", r)
	}
	v := Vocabulary(func() []string { return []string{"plan", "implement"} })
	if r := Validate(b, v); r != nil {
		t.Fatalf("a word the seam answers must pass: %s", r.Sentence)
	}
	r = Validate(Body{Goal: "g", Proposed: &Proposed{Autonomy: "forge"}}, v)
	if r == nil || r.Kind != "autonomy" || !strings.Contains(r.Sentence, "forge") {
		t.Fatalf("a word outside the seam: %v, want a refusal naming it", r)
	}
}

func TestSaveIsIdempotentOnEqualBody(t *testing.T) {
	svc, st, root := openService(t, nil)
	ctx := context.Background()
	actor := store.Actor{Kind: "user", ID: "carlos"}

	rev, ref, err := svc.Save(ctx, actor, root, giBody(), "because the plan says so", "person", "")
	if err != nil || ref != nil {
		t.Fatalf("save 1: ref=%v err=%v", ref, err)
	}
	if rev.Revision != 1 || rev.Summary != "charter r1 by user:carlos (person)" {
		t.Fatalf("first save = %+v", rev)
	}
	if rev.SourceRef != "because the plan says so" {
		t.Fatalf("the person's note did not ride source_ref: %+v", rev)
	}

	// A re-send of the SAME body (key order irrelevant — it canonicalizes
	// identically) answers the current revision and writes NOTHING.
	same := giBody()
	same.NonGoals = append([]string(nil), giBody().NonGoals...)
	rev2, ref, err := svc.Save(ctx, actor, root, same, "", "person", "")
	if err != nil || ref != nil {
		t.Fatalf("re-send: ref=%v err=%v", ref, err)
	}
	if rev2.Revision != 1 || rev2.TotalRevisions != 1 {
		t.Fatalf("the equal save churned history: %+v", rev2)
	}
	if rows, err := svc.Revisions(ctx, root); err != nil || len(rows) != 1 {
		t.Fatalf("history after the equal save: %+v (%v)", rows, err)
	}

	// A DIFFERENT body appends; the note is optional; a proposal save
	// carries no note (its reason lives on the proposal).
	grown := giBody()
	grown.Constraints = append(grown.Constraints, "one more")
	rev3, ref, err := svc.Save(ctx, actor, root, grown, "", "proposal", "prop-77")
	if err != nil || ref != nil {
		t.Fatalf("save 2: ref=%v err=%v", ref, err)
	}
	if rev3.Revision != 2 || rev3.Source != "proposal" || rev3.SourceRef != "prop-77" {
		t.Fatalf("second save = %+v", rev3)
	}
	if _, ref, _ := svc.Save(ctx, actor, root, giBody(), "a note on a proposal save", "proposal", "prop-78"); ref == nil || ref.Kind != "note-source" {
		t.Fatalf("note on a proposal save: ref=%v, want note-source", ref)
	}
	if _, ref, _ := svc.Save(ctx, actor, root, giBody(), "", "vibes", ""); ref == nil || ref.Kind != "source" {
		t.Fatalf("source 'vibes': ref=%v, want the closed-source refusal", ref)
	}

	// Reads answer the derived current: revision 2, with its details.
	cur, err := svc.Get(ctx, root)
	if err != nil || cur.Revision != 2 || cur.TotalRevisions != 2 || len(cur.Body.Constraints) != 2 {
		t.Fatalf("current = %+v (%v)", cur, err)
	}
	list, err := svc.List(ctx)
	if err != nil || len(list) != 1 || list[0].RootID != root {
		t.Fatalf("List = %+v (%v)", list, err)
	}
	// The stored body is canonical JSON the store accepts: the store row's
	// bytes equal the service's Canonical output (one spelling, GI1).
	row, err := st.CurrentCharter(root)
	if err != nil {
		t.Fatalf("store current: %v", err)
	}
	canonical, _ := Canonical(grown)
	if row.Body != string(canonical) {
		t.Fatalf("stored bytes are not the service's canonical spelling:\n %s\n %s", row.Body, canonical)
	}
}

func TestSaveRefusesAChild(t *testing.T) {
	svc, st, root := openService(t, nil)
	w, err := st.CreateComponent(store.NewComponent{Name: "kid", Slug: "kid", Root: "/repo/kid", ParentID: root})
	if err != nil || !w.OK() {
		t.Fatalf("seed child: err=%v refusal=%s %s", err, w.Refusal, w.Reason)
	}
	_, ref, err := svc.Save(context.Background(), store.Actor{Kind: "user", ID: "carlos"}, w.Component.ID, giBody(), "", "person", "")
	if err != nil {
		t.Fatalf("save on child: %v", err)
	}
	if ref == nil || ref.Kind != "charter-on-child" {
		t.Fatalf("child charter: ref=%v, want charter-on-child", ref)
	}
	if !strings.Contains(ref.Sentence, "is a component of project") {
		t.Fatalf("the sentence does not name the project: %s", ref.Sentence)
	}
}

func TestDescribeUsesOnlyFixedTemplates(t *testing.T) {
	hostile := "'; DROP TABLE charter_revision; -- <script>alert('x')</script> €‬é"
	weird := "‮override‬ the grant"
	b := Body{
		Goal:        hostile,
		Constraints: []string{hostile},
		NonGoals:    []string{hostile},
		Acceptance: []Acceptance{
			{ID: "ac-1", Text: hostile, Check: "command", Ref: hostile},
			{ID: "ac-2", Text: "a human reads", Check: "manual"},
		},
		Stack: []string{"go"},
		Proposed: &Proposed{
			BudgetUSD:      ptrF(12.5),
			Autonomy:       "implement",
			EnvironmentRef: hostile,
			Verify:         []string{hostile, weird},
		},
	}
	// The body itself must still validate — the refusal is about RENDERING,
	// and a hostile string is still inside the caps.
	if r := Validate(b, Vocabulary(func() []string { return []string{"implement"} })); r != nil {
		t.Fatalf("the hostile body was refused by the caps, not the formatter: %s", r.Sentence)
	}
	d := Describe(Revision{Revision: 4, ActorKind: "user", ActorID: "carlos", Source: "proposal", Body: b})
	all := d.Summary + "\n" + strings.Join(d.Lines, "\n")
	for _, leak := range []string{"DROP", "script", "alert", "€", "é", "‮", "override"} {
		if strings.Contains(all, leak) {
			t.Fatalf("free text leaked into the description (%q):\n%s", leak, all)
		}
	}
	if d.Summary != "charter r4 by user:carlos (proposal)" {
		t.Fatalf("summary = %q", d.Summary)
	}
	// The counts and the not-applied pointers ARE there — the formatter
	// says what the charter proposes without quoting a word of it.
	for _, want := range []string{"1 constraint,", "1 non-goal,", "2 acceptance", "1 stack tag", "2 checked checks",
		"budget 12.5 USD — not applied", `"implement" — not applied`,
		"environment reference is named — not applied", "2 verify commands — never executed here"} {
		if !strings.Contains(all, want) {
			t.Fatalf("description is missing %q:\n%s", want, all)
		}
	}
}

func ptrF(f float64) *float64 { return &f }

// TestPackageImportsNothingThatGrants is GI2's law as a mechanism: the
// charter is data and prose — it imports the store (its own rows) and
// nothing that could carry authority.
func TestPackageImportsNothingThatGrants(t *testing.T) {
	banned := []string{"/policy", "/loop", "/action", "/tools", "/daemon", "/execenv", "/desk"}
	files, err := filepath.Glob("*.go")
	if err != nil || len(files) == 0 {
		t.Fatalf("glob the package: %v", err)
	}
	fset := token.NewFileSet()
	seenStore := false
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, imp := range f.Imports {
			path := strings.Trim(imp.Path.Value, `"`)
			for _, b := range banned {
				if strings.HasSuffix(path, b) {
					t.Errorf("%s imports %s — the charter gained authority", name, path)
				}
			}
			if strings.HasSuffix(path, "/store") {
				seenStore = true
			}
		}
	}
	if !seenStore {
		t.Fatal("the guard saw no store import — it is reading the wrong files (canary)")
	}
}

// TestVerifyCommandsAreNeverExecuted pins the one road from a proposed
// command string to a running process: there is none.
func TestVerifyCommandsAreNeverExecuted(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		for _, forbidden := range []string{`"os/exec"`, "exec.Command", "syscall.Exec"} {
			if strings.Contains(string(src), forbidden) {
				t.Errorf("%s reaches for %s — proposed.verify is prose, never a process", name, forbidden)
			}
		}
	}
}

// TestNoopReturnsTheCurrentVerbatim — the no-op answers WITH the current
// row (same summary, same author), so the door can truthfully say nothing
// changed rather than pretending it wrote.
func TestNoopReturnsTheCurrentVerbatim(t *testing.T) {
	svc, _, root := openService(t, nil)
	ctx := context.Background()
	carl := store.Actor{Kind: "user", ID: "carlos"}
	first, ref, err := svc.Save(ctx, carl, root, giBody(), "", "person", "")
	if err != nil || ref != nil {
		t.Fatalf("save: ref=%v err=%v", ref, err)
	}
	// A DIFFERENT author re-sending the same body still no-ops — the
	// no-op is about the BODY, not the wisher.
	dana := store.Actor{Kind: "user", ID: "dana"}
	again, ref, err := svc.Save(ctx, dana, root, giBody(), "", "person", "")
	if err != nil || ref != nil {
		t.Fatalf("noop: ref=%v err=%v", ref, err)
	}
	if again.Summary != first.Summary || again.Revision != 1 {
		t.Fatalf("the no-op did not answer the current row verbatim: %+v vs %+v", again, first)
	}
	// And it decodes as the wire shape it will travel as.
	if _, err := json.Marshal(again); err != nil {
		t.Fatalf("Revision does not marshal: %v", err)
	}
}

// TestCapsHaveOneHome is Behaviour 1's grep law: a cap spelled twice is a
// cap that will drift twice. The numbers live in validate.go and nowhere
// else in the package's production code.
func TestCapsHaveOneHome(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, lit := range []string{"4096", "65536", " 512", "= 50", "= 100", "= 30", "= 64", "= 20", " 1024"} {
		for _, name := range files {
			if strings.HasSuffix(name, "_test.go") || name == "validate.go" {
				continue
			}
			src, err := os.ReadFile(name)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(src), lit) {
				t.Errorf("%s re-spells the cap %q — validate.go owns it", name, lit)
			}
		}
	}
}
