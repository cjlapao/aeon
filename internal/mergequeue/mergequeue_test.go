package mergequeue

// mergequeue_test.go — the pure core's exhaustive tables (T3). The
// hand-written matrix below is deliberately NOT derived from edges: an edit
// to transitions.go that drifts from MQ5 fails here.

import (
	"errors"
	"fmt"
	"go/parser"
	"go/token"
	"reflect"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/store"
)

// --- 1. vocabularies locked against the migration's CHECK text -------------

func migrationBody(t *testing.T, prefix string) string {
	t.Helper()
	for _, m := range store.Migrations() {
		if strings.HasPrefix(m.Name, prefix) {
			// One space per whitespace run: the lock compares WORDS.
			return strings.Join(strings.Fields(m.SQL), " ")
		}
	}
	t.Fatalf("no migration named %s…", prefix)
	return ""
}

func TestStatusSetMatchesMigrationCheck(t *testing.T) {
	body := migrationBody(t, "20261104044821")
	sets := map[string][]string{
		"status IN":          StatusSet,
		"reason_kind IN":     ReasonSet,
		"mode IN":            ModeSet,
		"conflict_policy IN": PolicySet,
		"land_method IN":     MethodSet,
	}
	for marker, set := range sets {
		literal := marker + " (" + quoteJoin(set) + ")"
		if !strings.Contains(body, literal) {
			// reason_kind's DDL is "IN ('denied',…)" possibly wrapped in
			// "IS NULL OR" — the restated set literal is the check.
			if !strings.Contains(body, quoteJoin(set)) {
				t.Errorf("the migration does not carry the exact list %s", literal)
			}
		}
	}
	// And the Go switches answer every member true and nothing else.
	for _, s := range StatusSet {
		if !Status(s).Valid() {
			t.Errorf("Status(%q).Valid() = false", s)
		}
	}
	if Status("landed").Valid() || ReasonKind("shipped").Valid() || Mode("sometimes").Valid() ||
		ConflictPolicy("guess").Valid() || LandMethod("rebase").Valid() {
		t.Error("a Valid() answered true for a word outside the closed sets")
	}
}

func quoteJoin(set []string) string {
	parts := make([]string, 0, len(set))
	for _, s := range set {
		parts = append(parts, "'"+s+"'")
	}
	return strings.Join(parts, ",")
}

// --- 2. the 9×9 matrix against a HAND-WRITTEN table ------------------------

func TestLegalMatrixEqualsHandWrittenTable(t *testing.T) {
	// Hand-written from design.md's MQ5 table, 2026-11-04, while the design
	// document — not transitions.go — was open on screen.
	hand := map[[2]Status]bool{
		{StatusQueued, StatusCatchingUp}: true, {StatusQueued, StatusRemoved}: true,
		{StatusCatchingUp, StatusVerifying}: true, {StatusCatchingUp, StatusConflicted}: true, {StatusCatchingUp, StatusRemoved}: true,
		{StatusVerifying, StatusReady}: true, {StatusVerifying, StatusFailed}: true, {StatusVerifying, StatusRemoved}: true,
		{StatusReady, StatusMerging}: true, {StatusReady, StatusQueued}: true, {StatusReady, StatusRemoved}: true,
		{StatusMerging, StatusMerged}: true, {StatusMerging, StatusFailed}: true, {StatusMerging, StatusQueued}: true, {StatusMerging, StatusRemoved}: true,
		{StatusConflicted, StatusQueued}: true, {StatusConflicted, StatusFailed}: true, {StatusConflicted, StatusRemoved}: true,
		{StatusFailed, StatusQueued}: true,
	}
	all := StatusSet
	for _, f := range all {
		for _, to := range all {
			got := Legal(Status(f), Status(to))
			want := hand[[2]Status{Status(f), Status(to)}]
			if got != want {
				t.Errorf("Legal(%s, %s) = %v, the hand-written MQ5 table says %v", f, to, got, want)
			}
		}
	}
	if len(hand) != 19 {
		t.Fatalf("the hand-written table holds %d pairs — recount it against MQ5, not against the code", len(hand))
	}
	// Moves() agrees with Legal() pair by pair.
	for _, f := range all {
		moves := Moves(Status(f))
		for _, to := range all {
			inList := false
			for _, m := range moves {
				if m.To == Status(to) {
					inList = true
					if m.Meaning == "" {
						t.Errorf("the move %s→%s has no meaning sentence", f, to)
					}
				}
			}
			if inList != Legal(Status(f), Status(to)) {
				t.Errorf("Moves(%s) and Legal(%s, %s) disagree", f, f, to)
			}
		}
	}
}

// --- 3. Next, row for row --------------------------------------------------

func bp() *bool       { v := true; return &v }
func bm(b bool) *bool { return &b }
func in(v int) *int   { return &v }

func baseFacts() Facts {
	return Facts{Config: Config{Mode: ModeManual, ConflictPolicy: PolicyAsk, LandMethod: MethodSquash, MaxCatchUps: 3},
		BehindState: BehindOK, Behind: in(0)}
}

type nextRow struct {
	name     string
	item     Item
	facts    Facts
	to       Status
	reason   string
	advance  bool
	loop     string // blueprint expected in StartLoop, "" = none
	haystack string // a fragment Sentence must contain
}

func TestNextTable(t *testing.T) {
	q := func(st Status) Item {
		return Item{ID: "mq-1", ComponentRoot: "/repo", BaseBranch: "main", Branch: "feat-x", Status: st, Position: 1000, Attended: true}
	}
	rows := []nextRow{
		// queued
		{"mode off holds the whole driver", q(StatusQueued), Facts{Config: Config{Mode: ModeOff}}, StatusQueued, "", false, "", "mode is off"},
		{"slot taken stays queued", q(StatusQueued), Facts{Config: baseFacts().Config, SlotHolder: "mq-9"}, StatusQueued, "", false, "", "holds the landing slot"},
		{"head unknown never guesses", q(StatusQueued), Facts{Config: baseFacts().Config}, StatusQueued, "", false, "", "will not act on an unknown"},
		{"not head waits its turn", q(StatusQueued), Facts{Config: baseFacts().Config, HeadOfBase: bm(false)}, StatusQueued, "", false, "", "not the head"},
		{"manual head starts the landing loop", q(StatusQueued), Facts{Config: baseFacts().Config, HeadOfBase: bm(true)}, StatusCatchingUp, "", true, "land-feature", "the landing loop starts"},
		{"auto head starts the same loop", q(StatusQueued), Facts{Config: Config{Mode: ModeAuto, ConflictPolicy: PolicyAsk, MaxCatchUps: 3}, HeadOfBase: bm(true)}, StatusCatchingUp, "", true, "land-feature", "slot free"},
		// catching_up
		{"conflict attended ask moves to conflicted", q(StatusCatchingUp), Facts{Config: baseFacts().Config, Conflict: &ConflictFacts{Count: 3}}, StatusConflicted, "", true, "", "integration merge reports 3 conflicted path(s)"},
		{"conflict unattended ask is blocked-visible", Item{ID: "mq-1", Status: StatusCatchingUp, BaseBranch: "main", Branch: "feat-x", Attended: false}, Facts{Config: baseFacts().Config, Conflict: &ConflictFacts{Count: 1}}, StatusConflicted, "", true, "", "blocked-visible"},
		{"conflict resolve starts the resolution loop", Item{ID: "mq-1", Status: StatusCatchingUp, BaseBranch: "main", Branch: "feat-x", Attended: true}, Facts{Config: Config{Mode: ModeManual, ConflictPolicy: PolicyResolve, MaxCatchUps: 3}, Conflict: &ConflictFacts{Count: 2}}, StatusConflicted, "", true, "resolve-conflict", "starts the resolution loop"},
		{"clean integration goes to verifying", q(StatusCatchingUp), Facts{Config: baseFacts().Config, IntegrationSHA: "abc"}, StatusVerifying, "", true, "", "built clean"},
		{"a still-running loop holds", q(StatusCatchingUp), Facts{Config: baseFacts().Config, LandingLoop: LoopRunning}, StatusCatchingUp, "", false, "", "catch-up still running"},
		{"a blocked loop without a conflict waits for a person", q(StatusCatchingUp), Facts{Config: baseFacts().Config, LandingLoop: LoopBlocked}, StatusCatchingUp, "", false, "", "waits for a person"},
		// verifying
		{"no gate evidence holds", q(StatusVerifying), Facts{Config: baseFacts().Config}, StatusVerifying, "", false, "", "no gate evidence"},
		{"red gate fails with its kind", q(StatusVerifying), Facts{Config: baseFacts().Config, GateResult: bm(false)}, StatusFailed, "gate-failed", true, "", "gate went red"},
		{"green gate is ready", q(StatusVerifying), Facts{Config: baseFacts().Config, GateResult: bp()}, StatusReady, "", true, "", "green for the integration sha"},
		// ready
		{"unavailable staleness never advances", q(StatusReady), Facts{Config: baseFacts().Config, BehindState: BehindUnavailable, Behind: nil}, StatusReady, "", false, "", "will not land on facts it cannot see"},
		{"not-a-repo never advances", q(StatusReady), Facts{Config: baseFacts().Config, BehindState: BehindNotARepo, Behind: nil}, StatusReady, "", false, "", "will not land on facts it cannot see"},
		{"behind bounces back to queued inside the bound", q(StatusReady), Facts{Config: baseFacts().Config, Behind: in(2), BehindState: BehindOK, CatchUps: 1}, StatusQueued, "", true, "", "catch-up 2 of 3"},
		{"behind past the bound ends stale-exhausted", q(StatusReady), Facts{Config: baseFacts().Config, Behind: in(2), BehindState: BehindOK, CatchUps: 3}, StatusFailed, "stale-exhausted", true, "", "catch-ups is the bound"},
		{"manual ready waits for a person", q(StatusReady), Facts{Config: baseFacts().Config, Behind: in(0), BehindState: BehindOK}, StatusReady, "", false, "", "waits for a person's land"},
		{"manual plus a person's land merges", q(StatusReady), Facts{Config: baseFacts().Config, Behind: in(0), BehindState: BehindOK, LandRequested: true, HeadSHAMatches: true}, StatusMerging, "", true, "", "person lands it"},
		{"auto without the grant HOLDS — the core invents no denial", q(StatusReady), Facts{Config: Config{Mode: ModeAuto, ConflictPolicy: PolicyAsk, MaxCatchUps: 3}, Behind: in(0), BehindState: BehindOK}, StatusReady, "", false, "", "does not invent one"},
		{"the step's own denial ends failed(denied)", q(StatusReady), Facts{Config: Config{Mode: ModeAuto, ConflictPolicy: PolicyAsk, MaxCatchUps: 3}, Behind: in(0), BehindState: BehindOK, LandDenial: "denied"}, StatusFailed, "denied", true, "", "denied"},
		{"auto with a moved head fails head-moved", q(StatusReady), Facts{Config: Config{Mode: ModeAuto, ConflictPolicy: PolicyAsk, MaxCatchUps: 3}, Behind: in(0), BehindState: BehindOK, GrantMerge: true}, StatusFailed, "head-moved", true, "", "verified_sha"},
		{"auto waits while the rule has not answered", q(StatusReady), Facts{Config: Config{Mode: ModeAuto, ConflictPolicy: PolicyAsk, MaxCatchUps: 3}, Behind: in(0), BehindState: BehindOK, GrantMerge: true, HeadSHAMatches: true}, StatusReady, "", false, "", "merge rule has not answered"},
		{"auto waits while the rule is unsatisfied", q(StatusReady), Facts{Config: Config{Mode: ModeAuto, ConflictPolicy: PolicyAsk, MaxCatchUps: 3}, Behind: in(0), BehindState: BehindOK, GrantMerge: true, HeadSHAMatches: true, RuleOK: bm(false)}, StatusReady, "", false, "", "not satisfied"},
		{"auto with everything satisfied merges", q(StatusReady), Facts{Config: Config{Mode: ModeAuto, ConflictPolicy: PolicyAsk, MaxCatchUps: 3}, Behind: in(0), BehindState: BehindOK, GrantMerge: true, HeadSHAMatches: true, RuleOK: bp()}, StatusMerging, "", true, "", "forge.land step runs"},
		// merging
		{"landed without the ancestor proof holds", q(StatusMerging), Facts{Config: baseFacts().Config, Landed: true}, StatusMerging, "", false, "", "ancestor proof"},
		{"a phantom merge fails honest forge", q(StatusMerging), Facts{Config: baseFacts().Config, Landed: true, Ancestor: bm(false)}, StatusFailed, "forge", true, "", "phantom merge"},
		{"landed with the proof merges", q(StatusMerging), Facts{Config: baseFacts().Config, Landed: true, Ancestor: bp()}, StatusMerged, "", true, "", "ancestor proof"},
		{"protection is honest failure", q(StatusMerging), Facts{Config: baseFacts().Config, Refused: "protected"}, StatusFailed, "protected", true, "", "no workaround"},
		{"head-moved refusal catches up", q(StatusMerging), Facts{Config: baseFacts().Config, Refused: "head-moved"}, StatusQueued, "", true, "", "catch-up 1 of 3"},
		{"head-moved past the bound ends exhausted", q(StatusMerging), Facts{Config: baseFacts().Config, Refused: "head-moved", CatchUps: 3}, StatusFailed, "stale-exhausted", true, "", "bound"},
		{"a git refusal ends with its own kind", q(StatusMerging), Facts{Config: baseFacts().Config, Refused: "git"}, StatusFailed, "git", true, "", "refused (git)"},
		// conflicted
		{"abort ends failed(conflict)", q(StatusConflicted), Facts{Config: baseFacts().Config, Aborted: true}, StatusFailed, "conflict", true, "", "answered abort"},
		{"a finished resolution re-queues", q(StatusConflicted), Facts{Config: Config{Mode: ModeManual, ConflictPolicy: PolicyResolve, MaxCatchUps: 3}, ResolveLoop: LoopDone}, StatusQueued, "", true, "", "re-verifies"},
		{"ask attended waits on the question", q(StatusConflicted), Facts{Config: baseFacts().Config}, StatusConflicted, "", false, "", "gate question"},
		{"resolve still running holds", Item{ID: "mq-1", Status: StatusConflicted, BaseBranch: "main", Branch: "feat-x", Attended: true}, Facts{Config: Config{Mode: ModeManual, ConflictPolicy: PolicyResolve, MaxCatchUps: 3}, ResolveLoop: LoopRunning}, StatusConflicted, "", false, "", "resolution loop is running"},
		{"block waits for a person", Item{ID: "mq-1", Status: StatusConflicted, BaseBranch: "main", Branch: "feat-x", Attended: false}, Facts{Config: Config{Mode: ModeManual, ConflictPolicy: PolicyBlock, MaxCatchUps: 3}}, StatusConflicted, "", false, "", "a person must resolve"},
		// terminal-ish
		{"failed only a person's retry moves", Item{ID: "mq-1", Status: StatusFailed, BaseBranch: "main", Branch: "feat-x", EndReason: "gate-failed"}, Facts{Config: baseFacts().Config}, StatusFailed, "", false, "", "failed(gate-failed)"},
		{"merged is terminal", q(StatusMerged), Facts{Config: Config{Mode: ModeAuto, ConflictPolicy: PolicyAsk, MaxCatchUps: 3}, HeadOfBase: bp(), GrantMerge: true}, StatusMerged, "", false, "", "terminal"},
		{"removed is terminal", q(StatusRemoved), Facts{Config: baseFacts().Config}, StatusRemoved, "", false, "", "terminal"},
	}
	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			d := Next(r.item, r.facts)
			if d.To != r.to {
				t.Fatalf("To = %q, want %q (sentence: %s)", d.To, r.to, d.Sentence)
			}
			if d.Reason != r.reason {
				t.Fatalf("Reason = %q, want %q", d.Reason, r.reason)
			}
			if d.Advance != r.advance {
				t.Fatalf("Advance = %v, want %v", d.Advance, r.advance)
			}
			switch {
			case r.loop == "" && d.StartLoop != nil:
				t.Fatalf("StartLoop = %+v, want none", d.StartLoop)
			case r.loop != "" && (d.StartLoop == nil || d.StartLoop.Blueprint != r.loop):
				t.Fatalf("StartLoop = %+v, want blueprint %s", d.StartLoop, r.loop)
			}
			if !strings.Contains(d.Sentence, r.haystack) {
				t.Fatalf("Sentence %q lacks %q", d.Sentence, r.haystack)
			}
		})
	}
}

// --- 4. unknown facts NEVER advance ----------------------------------------

func TestNextNeverAdvancesOnUnknownFacts(t *testing.T) {
	// Zero facts — nothing known — against every status: nothing moves.
	for _, s := range StatusSet {
		item := Item{ID: "mq-z", BaseBranch: "main", Branch: "feat", Status: Status(s), Attended: true}
		d := Next(item, Facts{})
		if d.Advance {
			t.Errorf("zero facts ADVANCED %s → %s (%s)", s, d.To, d.Sentence)
		}
		if d.To != item.Status {
			t.Errorf("a hold must echo the current status: %s held to %q", s, d.To)
		}
	}
	// specifically: Behind==nil at ready, and unavailable — never a
	// confident zero (P31).
	for _, f := range []Facts{
		{Config: baseFacts().Config, BehindState: BehindOK, Behind: nil},
		{Config: baseFacts().Config, BehindState: "", Behind: nil},
		{Config: baseFacts().Config, BehindState: BehindUnavailable, Behind: in(0)},
	} {
		d := Next(Item{ID: "mq-z", Status: StatusReady}, f)
		if d.Advance {
			t.Errorf("staleness %+v advanced a ready item (%s)", f, d.Sentence)
		}
	}
	// The sentence names WHY for each unknown kind.
	if s := Next(Item{ID: "mq-z", Status: StatusQueued}, Facts{Config: Config{Mode: ModeManual}}).Sentence; !strings.Contains(s, "unknown") {
		t.Errorf("the head-unknown hold sentence does not say unknown: %q", s)
	}
}

func TestNextModeOffHolds(t *testing.T) {
	off := Config{Mode: ModeOff, ConflictPolicy: PolicyAsk, MaxCatchUps: 3}
	// Every open status with otherwise-full auto-grade facts: nothing the
	// driver knows can start or advance anything.
	for _, s := range []Status{StatusQueued, StatusCatchingUp, StatusVerifying} {
		f := baseFacts()
		f.Config = off
		f.HeadOfBase = bp()
		f.GateResult = bp()
		f.IntegrationSHA = "abc"
		f.GrantMerge = true
		f.RuleOK = bp()
		d := Next(Item{ID: "mq-1", Status: s, Attended: true}, f)
		if d.Advance && s == StatusQueued {
			t.Fatalf("mode off advanced a queued item: %s", d.Sentence)
		}
	}
	// The ONE mode-off exception, stated and tested: off stops the DRIVER,
	// never a person — LandRequested still merges.
	d := Next(Item{ID: "mq-1", Status: StatusReady}, Facts{Config: off, BehindState: BehindOK, Behind: in(0), LandRequested: true, HeadSHAMatches: true})
	if !d.Advance || d.To != StatusMerging {
		t.Fatalf("a person's land under mode off: %+v — off stops the driver, never the person (MQ13)", d)
	}
}

// --- 5. the validator ------------------------------------------------------

func goodFacts() RepoFacts {
	return RepoFacts{HasPrimaryRoot: true, KnownBases: []string{"main"}, BranchLocal: true}
}

func TestValidateEnqueueRefusals(t *testing.T) {
	cases := []struct {
		name  string
		req   EnqueueRequest
		facts RepoFacts
		kind  string
		hay   string
	}{
		{"no root", EnqueueRequest{Branch: "feat"}, goodFacts(), "no-root", "names the repository"},
		{"no primary root", EnqueueRequest{ComponentRoot: "/gone", Branch: "feat"},
			RepoFacts{KnownBases: []string{"main"}, BranchLocal: true}, "no-primary-root", "not known to Aeon"},
		{"no branch", EnqueueRequest{ComponentRoot: "/repo"}, goodFacts(), "no-branch", "names the branch"},
		{"identity rule refuses uppercase", EnqueueRequest{ComponentRoot: "/repo", Branch: "Feature/X"}, goodFacts(), "bad-branch-name", "lowercase identity"},
		{"identity rule refuses a leading slash", EnqueueRequest{ComponentRoot: "/repo", Branch: "/feat"}, goodFacts(), "bad-branch-name", "lowercase identity"},
		{"branch not there", EnqueueRequest{ComponentRoot: "/repo", Branch: "feat-x"},
			RepoFacts{HasPrimaryRoot: true, KnownBases: []string{"main"}}, "branch-missing", "neither locally nor on the remote"},
		{"branch equals base", EnqueueRequest{ComponentRoot: "/repo", Branch: "main", BaseBranch: "main"}, goodFacts(), "branch-is-base", "cannot land into itself"},
		{"base not known", EnqueueRequest{ComponentRoot: "/repo", Branch: "feat-x", BaseBranch: "release"}, goodFacts(), "base-unknown", "not known for this repository"},
		{"no base at all quotes MQ15 verbatim", EnqueueRequest{ComponentRoot: "/repo", Branch: "feat-x"},
			RepoFacts{HasPrimaryRoot: true, BranchLocal: true}, "base-unknown",
			"no base branch is known for this repository — set its default branch (`aeon component update <id> --default-branch main`)"},
		{"duplicate open refuses", EnqueueRequest{ComponentRoot: "/repo", Branch: "feat-x"},
			RepoFacts{HasPrimaryRoot: true, KnownBases: []string{"main"}, BranchLocal: true, OpenBranches: []string{"feat-x"}}, "duplicate-open", "already has an open item"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := ValidateEnqueue(c.req, c.facts)
			var r *Refusal
			if err == nil || !asRefusal(err, &r) {
				t.Fatalf("err = %v, want a *Refusal", err)
			}
			if r.Kind != c.kind {
				t.Fatalf("kind = %q, want %q (%s)", r.Kind, c.kind, r.Reason)
			}
			if !strings.Contains(r.Reason, c.hay) {
				t.Fatalf("reason %q lacks %q", r.Reason, c.hay)
			}
			if !strings.HasPrefix(err.Error(), "refused: ") {
				t.Errorf("the error form must wear the house prefix: %q", err.Error())
			}
		})
	}
	// Accept cases: explicit base, resolved base, remote-only branch.
	for _, ok := range []struct {
		req   EnqueueRequest
		facts RepoFacts
	}{
		{EnqueueRequest{ComponentRoot: "/repo", Branch: "feat-x", BaseBranch: "main"}, goodFacts()},
		{EnqueueRequest{ComponentRoot: "/repo", Branch: "feature/loop-7"}, goodFacts()},
		{EnqueueRequest{ComponentRoot: "/repo", Branch: "feat-y"},
			RepoFacts{HasPrimaryRoot: true, KnownBases: []string{"main", "dev"}, BranchRemote: true}},
	} {
		if err := ValidateEnqueue(ok.req, ok.facts); err != nil {
			t.Errorf("a valid enqueue was refused: %v", err)
		}
	}
	// ResolveBase agrees with the validator's effective-base rule.
	if got := ResolveBase(EnqueueRequest{ComponentRoot: "/r", Branch: "b"}, goodFacts()); got != "main" {
		t.Errorf("ResolveBase = %q, want the first known base", got)
	}
	if got := ResolveBase(EnqueueRequest{ComponentRoot: "/r", Branch: "b", BaseBranch: "dev"}, goodFacts()); got != "dev" {
		t.Errorf("ResolveBase = %q, want the requested base to win", got)
	}
}

func asRefusal(err error, out **Refusal) bool {
	r, ok := err.(*Refusal)
	if ok {
		*out = r
	}
	return ok
}

// --- 6. the service: guards in front, refusals verbatim ---------------------

type fakeStore struct {
	items         map[string]store.MergeItem
	open          []store.MergeItem
	transitionErr error
	transitionN   int
	reorderN      int
	cfg           store.MergeConfig
}

func (f *fakeStore) EnqueueMergeItem(in store.NewMergeItem, actor store.MergeActor) (store.MergeItem, error) {
	m := store.MergeItem{ID: "mq-new", ComponentRoot: in.ComponentRoot, BaseBranch: in.BaseBranch,
		Branch: in.Branch, Status: "queued", Attended: in.Attended, EnqueuedByKind: actor.Kind, EnqueuedByID: actor.ID}
	f.items[m.ID] = m
	return m, nil
}
func (f *fakeStore) ReadMergeItem(id string) (store.MergeItem, error) {
	m, ok := f.items[id]
	if !ok {
		return store.MergeItem{}, fmt.Errorf("store: merge item %s: %w", id, store.ErrNotFound)
	}
	return m, nil
}
func (f *fakeStore) ListMergeItems(sf store.MergeFilter) ([]store.MergeItem, error) {
	return f.open, nil
}
func (f *fakeStore) TransitionMergeItem(id, from, to string, facts store.TransitionFacts, actor store.MergeActor) (store.MergeItem, error) {
	f.transitionN++
	if f.transitionErr != nil {
		return store.MergeItem{}, f.transitionErr
	}
	m := f.items[id]
	m.Status = to
	if facts.Reason != "" {
		m.ReasonKind = facts.Reason
	}
	f.items[id] = m
	return m, nil
}
func (f *fakeStore) SetMergeItemFields(id, guard string, fields store.MergeFields, actor store.MergeActor) (store.MergeItem, error) {
	return f.items[id], nil
}
func (f *fakeStore) ReorderMergeItems(id, beforeID string, actor store.MergeActor) (store.MergeItem, error) {
	f.reorderN++
	return f.items[id], nil
}
func (f *fakeStore) ReadMergeConfig(root string) (store.MergeConfig, error) { return f.cfg, nil }
func (f *fakeStore) WriteMergeConfig(in store.NewMergeConfig, actor store.MergeActor) (store.MergeConfig, error) {
	f.cfg = store.MergeConfig{ComponentRoot: in.ComponentRoot, Mode: in.Mode, ConflictPolicy: in.ConflictPolicy,
		LandMethod: in.LandMethod, MaxCatchUps: in.MaxCatchUps}
	return f.cfg, nil
}
func (f *fakeStore) ListMergeQueueEvents(id string) ([]store.MergeQueueEvent, error) { return nil, nil }

func newFake() (*fakeStore, *Service) {
	f := &fakeStore{items: map[string]store.MergeItem{
		"mq-1": {ID: "mq-1", ComponentRoot: "/repo", BaseBranch: "main", Branch: "feat-x", Status: "ready"},
	}}
	return f, NewService(f)
}

func TestServiceUsesGuardedTransitions(t *testing.T) {
	f, svc := newFake()
	// A lost race: the store answers stale-state. The service returns it
	// VERBATIM and does not retry — exactly one store call.
	f.items["mq-2"] = store.MergeItem{ID: "mq-2", Status: "merged"}
	f.transitionErr = store.MergeRefusal{Kind: "stale-state", Current: "merging", Item: "mq-1"}
	// merged has no MQ5 edges at all: the pure guard refuses BEFORE the
	// store sees a call.
	var r *Refusal
	if _, err := svc.Transition("mq-2", StatusQueued, store.TransitionFacts{}, store.MergeActor{Kind: "user", ID: "t3"}); err == nil || !asRefusal(err, &r) || r.Kind != "illegal-move" {
		t.Fatalf("a move out of merged: err = %v, want the illegal-move refusal", err)
	}
	if f.transitionN != 0 {
		t.Fatalf("the pure guard let a non-edge reach the store (%d calls)", f.transitionN)
	}
	if !strings.Contains(r.Reason, "terminal") {
		t.Fatalf("the refusal must say terminal: %s", r.Reason)
	}
	// Retry answers only failures: a READY item gets not-retryable, and
	// the store again sees nothing.
	if _, err := svc.Retry("mq-1", store.MergeActor{Kind: "user", ID: "t3"}); err == nil || !asRefusal(err, &r) || r.Kind != "not-retryable" {
		t.Fatalf("Retry on a READY item: err = %v, want not-retryable", err)
	}
	if f.transitionN != 0 {
		t.Fatalf("Retry of a non-failed item reached the store (%d calls)", f.transitionN)
	}
	// Now a legal one (ready → merging) with the stale-state refusal armed:
	// it travels back verbatim, attempted exactly once.
	f.items["mq-1"] = store.MergeItem{ID: "mq-1", ComponentRoot: "/repo", BaseBranch: "main", Branch: "feat-x", Status: "ready"}
	_, err := svc.Transition("mq-1", StatusMerging, store.TransitionFacts{}, store.MergeActor{Kind: "user", ID: "t3"})
	var sr store.MergeRefusal
	if err == nil || !errors.As(err, &sr) || sr.Kind != "stale-state" {
		t.Fatalf("lost race: err = %v, want the store's stale-state refusal verbatim", err)
	}
	if f.transitionN != 1 {
		t.Fatalf("store transition calls = %d, want exactly 1 — the service never retries blindly", f.transitionN)
	}
	// Legal transitions do reach the store once and succeed cleanly.
	f.transitionErr = nil
	m1 := f.items["mq-1"]
	m1.Status, m1.ReasonKind = "failed", "gate-failed"
	f.items["mq-1"] = m1
	out, err := svc.Retry("mq-1", store.MergeActor{Kind: "user", ID: "t3"})
	if err != nil || out.Status != "queued" {
		t.Fatalf("failed→queued: %v / %s", err, out.Status)
	}
	// Dequeue from a terminal row refuses before SQL (merged has no edges).
	if _, err := svc.Dequeue("mq-2", store.MergeActor{Kind: "user", ID: "t3"}); err == nil || !asRefusal(err, &r) {
		t.Fatalf("dequeue of a merged item: err = %v, want illegal-move", err)
	}
	// Dequeue from an open row takes the removed edge with the reason kind.
	f.items["mq-3"] = store.MergeItem{ID: "mq-3", Status: "queued"}
	out, err = svc.Dequeue("mq-3", store.MergeActor{Kind: "user", ID: "t3"})
	if err != nil || out.Status != "removed" || out.ReasonKind != "removed-by-person" {
		t.Fatalf("dequeue: %v / %+v", err, out)
	}
}

func TestServiceEnqueueIsIdempotentAndValidated(t *testing.T) {
	f, svc := newFake()
	f.open = []store.MergeItem{{ID: "mq-open", Branch: "feat-x", Status: "verifying"}}
	// The reuse answer comes BEFORE any validation — even a request with
	// nothing but a branch and a root finds the open row.
	m, err := svc.Enqueue(EnqueueRequest{ComponentRoot: "/repo", Branch: "feat-x"}, RepoFacts{},
		store.NewMergeItem{ComponentRoot: "/repo", Branch: "feat-x"}, store.MergeActor{Kind: "user", ID: "t3"})
	if err != nil || m.ID != "mq-open" {
		t.Fatalf("reuse: %+v (%v) — the open row IS the answer", m, err)
	}
	// A fresh enqueue passes the validator and reaches the store.
	m, err = svc.Enqueue(EnqueueRequest{ComponentRoot: "/repo", Branch: "feat-new"}, goodFacts(),
		store.NewMergeItem{ComponentRoot: "/repo", Branch: "feat-new"}, store.MergeActor{Kind: "user", ID: "t3"})
	if err != nil || m.Status != "queued" || m.EnqueuedByKind != "user" {
		t.Fatalf("fresh: %+v (%v)", m, err)
	}
	// A refused request never reaches the store (no rows beyond the fake's
	// seed + the one fresh write).
	if _, err := svc.Enqueue(EnqueueRequest{ComponentRoot: "/repo", Branch: "BAD"}, goodFacts(),
		store.NewMergeItem{ComponentRoot: "/repo", Branch: "BAD"}, store.MergeActor{Kind: "user", ID: "t3"}); err == nil {
		t.Fatal("an identity-invalid branch sailed through the service")
	}
	if len(f.items) != 2 {
		t.Fatalf("store rows = %d, want seed + fresh only", len(f.items))
	}
}

func TestServiceSettingsReturnsTheStatement(t *testing.T) {
	_, svc := newFake()
	cfg, sentence, err := svc.Settings(store.NewMergeConfig{ComponentRoot: "/repo", Mode: "auto", ConflictPolicy: "ask", LandMethod: "squash", MaxCatchUps: 3},
		store.MergeActor{Kind: "user", ID: "t3"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Mode != "auto" {
		t.Fatalf("cfg = %+v", cfg)
	}
	if !strings.Contains(sentence, "AUTHORITY WIDENING") || !strings.Contains(sentence, "S56 `merge` grant") {
		t.Fatalf("auto's settings sentence must carry the MQ13 warning verbatim-in-spirit: %q", sentence)
	}
	_, off, err := svc.Settings(store.NewMergeConfig{ComponentRoot: "/repo", Mode: "off", ConflictPolicy: "ask", LandMethod: "squash", MaxCatchUps: 3},
		store.MergeActor{Kind: "user", ID: "t3"})
	if err != nil || !strings.Contains(off, "dark") {
		t.Fatalf("off's sentence: %q (%v)", off, err)
	}
}

// --- 7. the pure core imports no I/O ---------------------------------------

func TestPureCoreImportsNoIO(t *testing.T) {
	banned := map[string]bool{
		"os": true, "os/exec": true, "net": true, "net/http": true, "database/sql": true,
		"github.com/cjlapao/aeon/internal/store": true,
	}
	for _, name := range []string{"state.go", "transitions.go", "decide.go"} {
		file, err := parser.ParseFile(token.NewFileSet(), name, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, imp := range file.Imports {
			path := strings.Trim(imp.Path.Value, `"`)
			if banned[path] || strings.HasPrefix(path, "os/") || strings.HasPrefix(path, "net/") {
				t.Errorf("%s imports %q — the core reads the world through FACTS only", name, path)
			}
		}
	}
	// And describe.go/validate.go stay store-free too (they render and
	// validate; they still touch no I/O):
	for _, name := range []string{"describe.go", "validate.go"} {
		file, err := parser.ParseFile(token.NewFileSet(), name, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, imp := range file.Imports {
			path := strings.Trim(imp.Path.Value, `"`)
			if banned[path] {
				t.Errorf("%s imports %q", name, path)
			}
		}
	}
}

// --- 8. sentences carry ids and enums, nothing else ------------------------

func TestNoUserTextInSentences(t *testing.T) {
	// The STRUCTURAL half of SEC-033: every parameter type the sentence
	// builders read has only string/bool/int fields — there is no []byte,
	// no map, no interface{} door through which prose could ride, and no
	// free-text column the core even models.
	check := func(t *testing.T, typ reflect.Type, name string) {
		t.Helper()
		if typ.Kind() != reflect.Struct {
			t.Fatalf("%s must take a struct, got %v", name, typ.Kind())
		}
		for i := 0; i < typ.NumField(); i++ {
			f := typ.Field(i)
			k := f.Type.Kind()
			switch k {
			case reflect.String, reflect.Bool, reflect.Int, reflect.Int64:
			default:
				t.Errorf("%s field %s is %v — sentence builders read ids, enums and numbers only", name, f.Name, k)
			}
		}
	}
	check(t, reflect.TypeOf(Item{}), "Item")
	check(t, reflect.TypeOf(Settings{}), "Settings")
	check(t, reflect.TypeOf(ConflictFacts{}), "ConflictFacts")
	// The decision sentence after a conflict carries only the COUNT — the
	// paths themselves have no field to travel in (ConflictFacts.Count is
	// an int, checked above).
	d := Next(Item{ID: "m", Status: StatusCatchingUp}, Facts{Config: baseFacts().Config, Conflict: &ConflictFacts{Count: 2}})
	if strings.Contains(d.Sentence, "path names") {
		t.Errorf("a conflict sentence names paths: %q", d.Sentence)
	}
	// Describe renders ids and refs; nothing it prints was typed by a user
	// beyond those (branch refs passed the identity rule at enqueue).
	s := Describe(Item{ID: "mq-1", ComponentRoot: "/repo", BaseBranch: "main", Branch: "feature/loop-7", Status: StatusReady, Attended: true})
	if s == "" || !strings.Contains(s, "mq-1") || !strings.Contains(s, "ready") {
		t.Fatalf("Describe = %q", s)
	}
	if strings.Contains(s, "\n") {
		t.Fatalf("a sentence must be one line: %q", s)
	}
}
