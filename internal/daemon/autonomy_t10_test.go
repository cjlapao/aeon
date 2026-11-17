package daemon

// autonomy_t10_test.go — S56 T10: the daemon-side half of the effective-view
// contract. Descriptors complete enough that a panel types no vocabulary
// (behaviours 1, 6 — proven in internal/autonomy), the dry runs that return
// the daemon's own confirmation and write nothing (behaviours 3, 4), the
// no-edit proof that a new registry action appears everywhere (behaviour 2),
// and the one-formatter lock over the human-readable grant sentences
// (behaviour 5). No ui/ code exists for this slice; this file is the
// contract the later UI task builds against (P7).

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/action"
	"github.com/cjlapao/aeon/internal/autonomy"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

// autonomyUserActor is the actor the dry-run tests stamp: a dry run
// validates as a person's write would, and the actor door is part of what
// must run. (userActor already exists in triggers_write_test.go.)
var autonomyUserActor = protocol.Actor{Kind: "user", ID: "1000"}

// autonomyRowCounts is the store's write surface for autonomy, counted: the
// dry-run proof is that none of it moves (a spy on the write handle is a row
// count before and after — the store has no other writer).
type autonomyRowCounts struct {
	Grants  int
	Changes int
	Uses    int
}

func countAutonomyRows(t *testing.T, s *store.Store) autonomyRowCounts {
	t.Helper()
	g, err := s.ListAutonomyGrants(store.AutonomyGrantFilter{})
	if err != nil {
		t.Fatal(err)
	}
	c, err := s.AutonomyChanges(store.AutonomyChangeFilter{})
	if err != nil {
		t.Fatal(err)
	}
	u, err := s.AutonomyUses(store.AutonomyUseFilter{})
	if err != nil {
		t.Fatal(err)
	}
	return autonomyRowCounts{Grants: len(g), Changes: len(c), Uses: len(u)}
}

// TestDryRunWritesNothing (T10 behaviour 3): each write's dry run returns its
// confirmation or refusal and writes NOTHING — the grant, change and use-log
// row counts are unchanged, for the admitted dry run and the refused one
// alike. A dry run that persisted would audit a decision nobody made.
func TestDryRunWritesNothing(t *testing.T) {
	s, _, svc, _, root := autonomyServiceEnv(t)
	ctx := context.Background()

	// One real grant so the dry-run revoke has a grant to name.
	g := seedAutonomyGrant(t, s, root.ID, store.NewAutonomyGrant{
		SubjectKind: "action", Subject: action.ForgeOpenPR, State: "allow", Conditions: `{}`,
	})
	snap := countAutonomyRows(t, s)

	// An admitted dry run: the confirmation, no row.
	out, refusal, err := svc.Grant(ctx, autonomyUserActor, protocol.AutonomyGrantRequest{
		NodeID: root.ID, Subject: action.ForgeOpenIssue,
		Conditions: protocol.AutonomyConditions{RequireBudget: true, MaxPerDay: 3},
		DryRun:     true,
	})
	if err != nil || refusal != nil {
		t.Fatalf("dry-run grant: refusal=%v err=%v", refusal, err)
	}
	if out.View != nil || out.Changed {
		t.Error("an admitted dry run must carry no view and no change")
	}
	if out.Confirmation == "" {
		t.Error("an admitted dry run must carry the daemon's confirmation")
	}

	// A dry run that would be refused: the refusal, still no row.
	if _, refusal, err := svc.Grant(ctx, autonomyUserActor, protocol.AutonomyGrantRequest{
		NodeID: root.ID, Subject: action.GitCreateBranch, DryRun: true,
	}); err != nil || refusal == nil {
		t.Fatalf("a dry run on a local subject must refuse (local-subject), got refusal=%v err=%v", refusal, err)
	}

	// The restrictive dry runs.
	if out, refusal, err := svc.Deny(ctx, autonomyUserActor, protocol.AutonomyDenyRequest{
		NodeID: root.ID, Subject: action.ForgeOpenPR, DryRun: true,
	}); err != nil || refusal != nil || out.Confirmation == "" {
		t.Fatalf("dry-run deny: refusal=%v err=%v confirmation=%q", refusal, err, out.Confirmation)
	}
	if out, refusal, err := svc.Revoke(ctx, autonomyUserActor, protocol.AutonomyRevokeRequest{
		ID: g.ID, Reason: "the dry run names what it would stop", DryRun: true,
	}); err != nil || refusal != nil || out.Confirmation == "" {
		t.Fatalf("dry-run revoke: refusal=%v err=%v confirmation=%q", refusal, err, out.Confirmation)
	}

	if got := countAutonomyRows(t, s); got != snap {
		t.Fatalf("a dry run wrote: before %v, after %v", snap, got)
	}
}

// TestConfirmationNamesWhatIsAndIsNotAllowed (T10 behaviour 4): the grant's
// dry-run confirmation names the root, each action it would cover, the
// expiry, each condition in words, the S57 rule for a merge — and says what
// the grant does NOT let. A grant that does not cover forge.merge names that
// absence, because merge is a separate decision.
func TestConfirmationNamesWhatIsAndIsNotAllowed(t *testing.T) {
	_, _, svc, _, root := autonomyServiceEnv(t)
	ctx := context.Background()
	exp := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)

	dryGrant := func(subject string, conds protocol.AutonomyConditions, expiresAt *time.Time) string {
		t.Helper()
		out, refusal, err := svc.Grant(ctx, autonomyUserActor, protocol.AutonomyGrantRequest{
			NodeID: root.ID, Subject: subject, Conditions: conds, ExpiresAt: expiresAt, DryRun: true,
		})
		if err != nil || refusal != nil {
			t.Fatalf("dry-run grant %s: refusal=%v err=%v", subject, refusal, err)
		}
		return out.Confirmation
	}
	contains := func(sentence string, frags ...string) {
		t.Helper()
		for _, f := range frags {
			if !strings.Contains(sentence, f) {
				t.Errorf("confirmation %q does not name %q", sentence, f)
			}
		}
	}

	// A single-action grant with a cap, the budget and an expiry.
	s1 := dryGrant(action.ForgeOpenPR, protocol.AutonomyConditions{RequireBudget: true, MaxPerDay: 5}, &exp)
	contains(s1,
		root.ID,
		"open a pull request",
		"up to 5 a day",
		"while the budget has room",
		"until "+exp.UTC().Format(time.RFC3339),
		"It does not let it merge.")

	// The outward shorthand names EVERY action it would cover — and still
	// says the merge is not among them (the class never covers an
	// irreversible action, and a panel must not be told otherwise).
	s2 := dryGrant(autonomy.ClassShorthand, protocol.AutonomyConditions{RequireBudget: true}, nil)
	contains(s2, root.ID, "every outward action:", "while the budget has room", "with no expiry", "It does not let it merge.")
	for _, n := range autonomyOutwardActions() {
		contains(s2, autonomy.SubjectLabel(n))
	}

	// The merge grant names the S57 rule, its forced condition and the
	// bounded expiry — and says nothing it cannot say: no "It does not let
	// it merge." on the grant that IS the merge.
	s3 := dryGrant(action.ForgeMerge, protocol.AutonomyConditions{RequireGates: true, RequireBudget: true}, &exp)
	contains(s3, root.ID, "merge a pull request", "only when the merge rule (S57) passes", "until "+exp.UTC().Format(time.RFC3339))
	if strings.Contains(s3, "It does not let it merge.") {
		t.Errorf("the merge grant's confirmation says %q", s3)
	}

	// A bare grant says exactly what it allows and nothing more: the default
	// budget, no expiry, and the merge absence.
	s4 := dryGrant(action.ForgeCloseIssue, protocol.AutonomyConditions{RequireBudget: true}, nil)
	contains(s4, root.ID, "close an issue", "while the budget has room", "with no expiry", "It does not let it merge.")
}

// TestNewActionAppearsInVocabularyAndEffectiveWithoutAnEdit (T10 behaviour
// 2): a registry action — test double for the next one to be added —
// appears in the vocabulary, in the outward expansion and in the effective
// view as deny — no grant, with NO EDIT to any autonomy file. The registry
// is CLOSED (D14: no exported mutation), so the test names no action at
// all: it derives every expected set from action.Names()/action.Classes(),
// and a hardcoded list that missed the new name is what fails it. The
// label fallback (a name with no phrase answers itself) is what keeps the
// completeness test green for a name nobody described yet.
func TestNewActionAppearsInVocabularyAndEffectiveWithoutAnEdit(t *testing.T) {
	_, _, svc, _, root := autonomyServiceEnv(t)
	ctx := context.Background()
	classes := action.Classes()

	// (1) The vocabulary's subjects are exactly the non-local registry
	// actions plus the one class shorthand — read, never copied.
	v, err := svc.Vocabulary()
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	var outwardCovers []string
	for _, s := range v.Subjects {
		got[s.Name] = true
		if s.Kind == "class" {
			outwardCovers = s.Covers
		}
	}
	for _, name := range action.Names() {
		if classes[name] == action.EffectLocal {
			if got[name] {
				t.Errorf("a local action %s must not be a subject", name)
			}
			continue
		}
		if !got[name] {
			t.Errorf("the non-local action %s is missing from the vocabulary — a hardcoded list?", name)
		}
	}
	wantCovers := []string{}
	for _, name := range action.Names() {
		if classes[name] == action.EffectOutward {
			wantCovers = append(wantCovers, name)
		}
	}
	sort.Strings(outwardCovers)
	sort.Strings(wantCovers)
	if !reflect.DeepEqual(outwardCovers, wantCovers) {
		t.Errorf("the outward shorthand's covers = %v, want the registry's current outward set %v", outwardCovers, wantCovers)
	}

	// (2) The effective view on a root with no grants: every registry action
	// appears, and every non-local one denies with the no-grant sentence.
	ev, refusal, err := svc.Effective(ctx, root.ID, "schedule", nil)
	if err != nil || refusal != nil {
		t.Fatalf("effective: refusal=%v err=%v", refusal, err)
	}
	byAction := map[string]protocol.EffectiveAction{}
	for _, a := range ev.Actions {
		byAction[a.Action] = a
	}
	if len(byAction) != len(action.Names()) {
		t.Fatalf("the effective view has %d actions, want every registry action (%d)", len(byAction), len(action.Names()))
	}
	for _, name := range action.Names() {
		ae, ok := byAction[name]
		if !ok {
			t.Errorf("the registry action %s is missing from the effective view", name)
			continue
		}
		if classes[name] == action.EffectLocal {
			if ae.Verdict != string(action.VerdictRun) {
				t.Errorf("the local action %s = %s, want run", name, ae.Verdict)
			}
			continue
		}
		if ae.Verdict != string(action.VerdictDeny) || !strings.Contains(ae.Because, "no autonomy grant on this root covers it") {
			t.Errorf("the outward/irreversible action %s = %s (%s), want deny with the no-grant sentence", name, ae.Verdict, ae.Because)
		}
	}

	// (3) The derivation is by the registry's accessors, not by typed names:
	// no production autonomy file carries a registry name as a string
	// literal. (A sentence that mentions a name — the validator's refusal
	// for the merge — names it mid-string, not as the literal of a list.)
	if bad := autonomyNameLiterals(t); len(bad) > 0 {
		t.Fatalf("typed registry-name literals in production autonomy files:\n%s", strings.Join(bad, "\n"))
	}
}

// autonomyNameLiterals walks the production autonomy files for a registry
// name carried as its own string literal — the shape of a hardcoded list.
func autonomyNameLiterals(t *testing.T) []string {
	t.Helper()
	var bad []string
	files := []string{}
	err := filepath.WalkDir("../../internal/autonomy", func(path string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"../../internal/daemon/autonomy.go"} {
		files = append(files, f)
	}
	for _, path := range files {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, name := range action.Names() {
			if strings.Contains(string(raw), `"`+name+`"`) {
				bad = append(bad, path+": "+name)
			}
		}
	}
	return bad
}

// TestOneFormatterForGrantSentences (T10 behaviour 5): describe, because and
// live.sentence are the ONLY human-readable strings a surface shows for a
// grant, and each family is built in exactly one place — a second formatter
// is a second truth (AL17). The client surface (ui/ and cmd/) carries none
// of it: it prints the wire's strings verbatim or not at all.
func TestOneFormatterForGrantSentences(t *testing.T) {
	type family struct {
		fragment string
		wantFile string // the one file that may build it
	}
	families := []family{
		// describe — the daemon's own sentence on the written grant.
		{"for unattended loops on ", "../../internal/daemon/autonomy.go"},
		// The pure Plan's denies and the live sentence for a grant in force —
		// the core's own words.
		{"no autonomy grant on this root covers it", "../../internal/autonomy/sentences.go"},
		{"effects confined to the working tree (a local action needs no grant)", "../../internal/autonomy/sentences.go"},
		{"is in force: it ", "../../internal/autonomy/sentences.go"},
		// The veto/revoked/expiry words: the decision site (T5) sits in the
		// lower package and the core wraps it, never the reverse — so the
		// SHARED builder is in internal/action, and every other file (the
		// core's sentences.go included) renders through it. A re-typed copy
		// anywhere, including ui/ and cmd/, is a second truth (AL17).
		{"vetoes it (a veto overrides every grant on this component and its root)", "../../internal/action/grant.go"},
		{"was revoked at %s by %s", "../../internal/action/grant.go"},
		{"grant %s expired", "../../internal/action/grant.go"},
	}
	// Part 1: each family is built in exactly its one file (non-test Go).
	err := filepath.WalkDir("../..", func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			if d != nil && d.IsDir() && (d.Name() == "node_modules" || d.Name() == ".git" || d.Name() == "dist" || d.Name() == "target" || d.Name() == "binaries" || d.Name() == ".dev") {
				return filepath.SkipDir
			}
			return err
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, f := range families {
			if !strings.Contains(string(raw), f.fragment) {
				continue
			}
			if path != f.wantFile {
				t.Errorf("the sentence %q is built in %s — its one home is %s", f.fragment, path, f.wantFile)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// Every family is built SOMEWHERE (the one home is not vacuous).
	for _, f := range families {
		raw, err := os.ReadFile(f.wantFile)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(raw), f.fragment) {
			t.Errorf("the sentence %q is not built in its one home %s — the formatter moved", f.fragment, f.wantFile)
		}
	}
	// Part 2: the client surface carries none of it, in any file type.
	// Build outputs (including the embedded daemon) are not client sources.
	for _, clientDir := range []string{"../../ui", "../../cmd"} {
		err := filepath.WalkDir(clientDir, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				if d != nil && d.IsDir() && (d.Name() == "node_modules" || d.Name() == ".git" || d.Name() == "dist" || d.Name() == "target" || d.Name() == "binaries" || d.Name() == ".dev") {
					return filepath.SkipDir
				}
				return err
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			for _, f := range families {
				if strings.Contains(string(raw), f.fragment) {
					t.Errorf("the client surface carries a built grant sentence: %s contains %q", path, f.fragment)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}
