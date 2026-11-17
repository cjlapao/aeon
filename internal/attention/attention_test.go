package attention

import (
	"context"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/store"
)

// attention_test.go — S58 T2's registry, severity, doors, and scope tests.
// The store reads are tested in `internal/store`; the wire mapping is tested
// in `internal/daemon`; this file tests the DERIVATION with fakes at the
// exact seams the sources take.

// ---------- fakes ----------------------------------------------------------

type fakeQuestions struct {
	n    string
	rows []store.AttentionQuestion
	err  error
}

func (f *fakeQuestions) Name() string {
	if f.n != "" {
		return f.n
	}
	return "questions"
}

func (f *fakeQuestions) PendingQuestionsForScope(roots []string) ([]store.AttentionQuestion, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.rows, nil
}

func (f *fakeQuestions) Items(ctx context.Context, q Query) ([]Item, error) {
	return QuestionsSource{Questions: f}.Items(ctx, q)
}

type fakeBlocked struct {
	loops   []store.AttentionBlockedLoop
	reasons map[string]store.BlockedReason
	err     error
}

func (f *fakeBlocked) Name() string { return "blocked" }

func (f *fakeBlocked) BlockedLoopsForScope(roots []string) ([]store.AttentionBlockedLoop, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.loops, nil
}

func (f *fakeBlocked) LatestBlockedReasons(roots []string) (map[string]store.BlockedReason, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.reasons, nil
}

type fakeStopped struct {
	rows []store.AttentionStoppedLoop
	err  error
}

func (fakeStopped) Name() string { return "stopped" }

func (f *fakeStopped) StoppedLoopsForScope(roots []string) ([]store.AttentionStoppedLoop, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.rows, nil
}

func (f *fakeStopped) Items(ctx context.Context, q Query) ([]Item, error) {
	return StoppedSource{Loops: f}.Items(ctx, q)
}

// fakeBlockedSource lets a door test stand a blocked item up without
// threading the store fake through BlockedSource by hand.
type fakeBlockedSource struct{ inner *fakeBlocked }

func (f *fakeBlockedSource) Name() string { return "blocked" }
func (f *fakeBlockedSource) Items(ctx context.Context, q Query) ([]Item, error) {
	return BlockedSource{Loops: f.inner}.Items(ctx, q)
}

type fakeTree struct{ comps []store.AttentionComponent }

func (f fakeTree) LiveComponentTree() ([]store.AttentionComponent, error) { return f.comps, nil }

func testTree() fakeTree {
	return fakeTree{comps: []store.AttentionComponent{
		{ID: "cmp-a", Paths: []string{"/w/a"}},
		{ID: "cmp-b", ParentID: "cmp-a", Paths: []string{"/w/a/b"}},
		{ID: "cmp-c", Paths: []string{"/w/c"}},
	}}
}

// ---------- severity -------------------------------------------------------

func TestSeverityOrderAndAgeTieBreak(t *testing.T) {
	mk := func(key string, sev Severity, since string) Item {
		return Item{Key: key, Severity: sev, Since: since}
	}
	items := []Item{
		mk("d", Info, "2026-11-02T00:00:00Z"),
		mk("a", NeedsYou, "2026-11-01T00:00:00Z"),
		mk("c", Stopped, "2026-11-03T00:00:00Z"),
		mk("b", NeedsYou, "2026-11-02T00:00:00Z"),
		mk("e", Blocked, "2026-10-30T00:00:00Z"),
		mk("f", Stuck, "2026-10-29T00:00:00Z"),
		mk("g", NeedsYou, "2026-11-01T00:00:00Z"), // same since as `a` → key order
		mk("h", Info, "not-a-time"),               // unknown since sorts LAST inside its class
	}
	sort.SliceStable(items, func(i, j int) bool { return Less(items[i], items[j]) })
	var got []string
	for _, it := range items {
		got = append(got, it.Key)
	}
	want := []string{"a", "g", "b", "f", "e", "c", "d", "h"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("inbox order = %v, want %v (needs-you > stuck > blocked > stopped > info; older first; key tie-break; unknown since last)", got, want)
	}
	for _, s := range []string{"needs-you", "stuck", "blocked", "stopped", "info"} {
		if !ValidSeverity(s) {
			t.Fatalf("closed vocabulary rejects %q", s)
		}
	}
	if ValidSeverity("urgent") || ValidSeverity("") {
		t.Fatalf("closed vocabulary accepts a value it must refuse")
	}
}

// ---------- the registry ---------------------------------------------------

func TestUnwiredSourcesAreNamed(t *testing.T) {
	r := NewRegistry(nil, nil)
	r.Register(&fakeSrc{n: "questions"})
	res, err := r.Evaluate(context.Background(), Query{})
	if err != nil {
		t.Fatal(err)
	}
	census := map[string]string{}
	for _, s := range res.Sources {
		census[s.Name] = s.State
	}
	if census["questions"] != "reporting" {
		t.Fatalf("census = %v, want questions reporting", census)
	}
	for _, name := range []string{"budget", "stuck", "gates", "scheduler"} {
		if census[name] != "not-wired" {
			t.Fatalf("census = %v, want %s not-wired (SV1: the claim is visible per source)", census, name)
		}
	}
	// The sibling seam: registering a source by a known name flips its slot.
	r2 := NewRegistry(nil, nil)
	r2.Register(&fakeSrc{n: "questions"})
	r2.Register(&fakeSrc{n: "budget"})
	res2, err := r2.Evaluate(context.Background(), Query{})
	if err != nil {
		t.Fatal(err)
	}
	states := map[string]int{}
	for _, s := range res2.Sources {
		states[s.Name]++
		if s.Name == "budget" && s.State != "reporting" {
			t.Fatalf("registered budget shows %q", s.State)
		}
	}
	if states["budget"] != 1 {
		t.Fatalf("budget listed %d times, want exactly 1 (the not-wired slot must close)", states["budget"])
	}
}

func TestFailedSourceDoesNotHideOthers(t *testing.T) {
	r := NewRegistry(nil, nil)
	r.Register(&fakeQuestions{rows: []store.AttentionQuestion{{
		QuestionID: 1, LoopSlug: "s", ComponentRoot: "/w/a", Kind: "clarify",
		Prompt: "p", AskedAt: "2026-11-02T00:00:00Z",
	}}, n: "x"})
	r.Register(&failingSrc{})
	res, err := r.Evaluate(context.Background(), Query{})
	if err != nil {
		t.Fatalf("a broken source must not fail the evaluation: %v", err)
	}
	if len(res.Items) != 1 {
		t.Fatalf("the healthy source's items vanished: %d", len(res.Items))
	}
	var failed *SourceStatus
	for i, s := range res.Sources {
		if s.State == "failed" {
			failed = &res.Sources[i]
		}
	}
	if failed == nil || !strings.Contains(failed.Error, "boom") {
		t.Fatalf("no failed census entry carrying the source's own sentence: %+v", res.Sources)
	}
}

func TestRegistryDuplicateNamePanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("two sources named the same did not panic")
		}
	}()
	r := NewRegistry(nil, nil)
	r.Register(&fakeSrc{n: "questions"})
	r.Register(&fakeSrc{n: "questions"})
}

func TestLimitDefaultAndCap(t *testing.T) {
	var rows []store.AttentionQuestion
	for i := int64(1); i <= 601; i++ {
		rows = append(rows, store.AttentionQuestion{
			QuestionID: i, LoopSlug: "s", ComponentRoot: "/w/a", Kind: "clarify",
			Prompt: "p", AskedAt: "2026-11-02T00:00:00Z",
		})
	}
	q := &fakeQuestions{rows: rows}
	r := NewRegistry(nil, nil)
	r.Register(q)
	res, err := r.Evaluate(context.Background(), Query{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Items) != DefaultLimit {
		t.Fatalf("default limit = %d, want %d", len(res.Items), DefaultLimit)
	}
	res, err = r.Evaluate(context.Background(), Query{Limit: 9999})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Items) != MaxLimit {
		t.Fatalf("capped limit = %d, want %d (params cannot raise the ceiling)", len(res.Items), MaxLimit)
	}
}

func TestInboxCachesNothing(t *testing.T) {
	rows := []store.AttentionQuestion{{
		QuestionID: 1, LoopSlug: "s", ComponentRoot: "/w/a", Kind: "clarify",
		Prompt: "p", AskedAt: time.Now().UTC().Add(-time.Hour).Format(time.RFC3339),
	}}
	q := &fakeQuestions{rows: rows}
	r := NewRegistry(nil, nil)
	r.Register(q)
	first, err := r.Evaluate(context.Background(), Query{})
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Items) != 1 {
		t.Fatalf("want 1 item, got %d", len(first.Items))
	}
	// The row is answered; the NEXT evaluation must see the new truth with no
	// invalidation anywhere.
	q.rows = nil
	second, err := r.Evaluate(context.Background(), Query{})
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Items) != 0 {
		t.Fatalf("the old answer survived the row going away: %+v", second.Items)
	}
	if second.EvaluatedAt.Before(first.EvaluatedAt) {
		t.Fatal("evaluatedAt moved backwards")
	}
	// The grep half of the claim: no package-level var except the two closed
	// read-only tables (the registry's slots, doors' kinds).
	varTable := regexp.MustCompile(`(?m)^var\s+[A-Za-z_][A-Za-z0-9_]*`)
	// The allow-list is every package-level var that is STATE nothing: two
	// closed read-only tables and the sentinel errors (errors.New at init,
	// never reassigned — a sentinel is a NAME, not a memory; T3 added the
	// ghost and horizon sentinels beside the scope one).
	allowed := map[string]bool{"knownUnwired": true, "knownBlockedKinds": true,
		"ErrUnknownScope": true, "ErrGhostKey": true, "ErrSnoozeHorizon": true}
	err = filepath.Walk(".", func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, m := range varTable.FindAllString(string(src), -1) {
			name := strings.TrimPrefix(m, "var ")
			if !allowed[name] {
				t.Errorf("%s: package-level var %q (N8: the registry holds sources, not answers)", path, name)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// ---------- doors ----------------------------------------------------------

var golden = flag.String("doors.golden", "", "write the door-table golden here (maintainer run)")

func TestEveryDoorNamesARegisteredMethod(t *testing.T) {
	// The closed set of methods T2's table may name, verified against the
	// protocol constants' meaning at the daemon (TestInboxWiringReachesTheDoor
	// runs the LIVE check); here: every template's method is one of these,
	// gated or not, and every NON-gated method exists unconditionally.
	registered := map[string]bool{"answer": true, "step.continue": true, "loop.retry": true}
	for kind, tpl := range doorTable() {
		if tpl.Method == "" {
			continue
		}
		if !registered[tpl.Method] && !tpl.Gated {
			t.Fatalf("kind %q names %q, which is neither registered nor gated", kind, tpl.Method)
		}
	}
	// loop.raise is the one gated template: wired ⇒ it names the method,
	// unwired ⇒ generic sentence and NO method (S19's F-1).
	it := Item{LoopSlug: "s"}
	wired := NewRegistry(func(m string) bool { return m == "loop.raise" }, nil)
	unwired := NewRegistry(func(string) bool { return false }, nil)
	mSent := func(reg *Registry, kind string) (string, string) {
		fb := &fakeBlocked{
			loops:   []store.AttentionBlockedLoop{{LoopID: "l", Slug: "s", ComponentRoot: "/w/a", LastReason: "x", UpdatedAt: "2026-11-02T00:00:00Z"}},
			reasons: map[string]store.BlockedReason{"l": {Kind: strings.TrimPrefix(kind, "blocked."), At: "2026-11-02T00:00:00Z"}},
		}
		items, err := (&fakeBlockedSource{inner: fb}).Items(context.Background(), Query{})
		if err != nil {
			t.Fatal(err)
		}
		return Door(items[0].Kind, items[0], reg.wired)
	}
	if m, _ := mSent(wired, "blocked.exhaustion"); m != "loop.raise" {
		t.Fatalf("wired exhaustion door = %q, want loop.raise", m)
	}
	m, sent := mSent(unwired, "blocked.exhaustion")
	if m != "" || !strings.Contains(sent, "no single command") {
		t.Fatalf("unwired exhaustion door = (%q, %q), want no method and the generic sentence", m, sent)
	}
	_ = it
}

func TestDoorTableGolden(t *testing.T) {
	kind := func(s string) Item {
		return Item{LoopSlug: "demo-loop", ItemRef: "77"}
	}
	_ = kind
	var lines []string
	for _, k := range []string{
		"question", "stopped", "blocked.action-denied", "blocked.step-blocked",
		"blocked.model-error", "blocked.runner-error", "blocked.exhaustion",
		"blocked.cap-hit", "blocked.stuck", "blocked.other", "question.free",
		"gate-failed", "proposal", "bugs-unvalidated",
	} {
		it := Item{
			Key: "k", Kind: k, LoopSlug: "demo-loop", ItemRef: "77",
			options: `["approve","deny"]`,
		}
		allWired := func(string) bool { return true }
		noneWired := func(string) bool { return false }
		m, sent := Door(k, it, allWired)
		lines = append(lines, "WIRED   "+k+"\t"+m+"\t"+sent)
		m, sent = Door(k, it, noneWired)
		lines = append(lines, "UNWIRED "+k+"\t"+m+"\t"+sent)
	}
	body := "# S58 T2 door-table golden (SV2). One line per kind per wiring state:\n" +
		"# <STATE> <kind>\t<method>\t<sentence>.\n" +
		"# Changing a sentence, or a kind's door, means changing THIS file in the\n" +
		"# same commit as doors.go — and the header line above keeps its promise.\n" +
		strings.Join(lines, "\n") + "\n"
	if *golden != "" {
		if err := os.WriteFile(*golden, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Skip("golden written")
	}
	want, err := os.ReadFile(filepath.Join("testdata", "doors.golden"))
	if err != nil {
		t.Fatalf("golden missing (run: go test ./internal/attention -run TestDoorTableGolden -args -doors.golden=testdata/doors.golden): %v", err)
	}
	if string(want) != body {
		t.Fatalf("door table drifted from its golden.\n--- want ---\n%s\n--- got ---\n%s", want, body)
	}
}

// ---------- sources --------------------------------------------------------

func TestInboxQuestionsCarryPromptLocallyAndFixedReason(t *testing.T) {
	q := &fakeQuestions{rows: []store.AttentionQuestion{{
		QuestionID: 5, LoopSlug: "demo", ComponentRoot: "/w/a", Kind: "clarify",
		Prompt: "the question's own prompt text", OptionsRaw: `["approve","deny"]`,
		AskedAt: "2026-11-02T00:00:00Z",
	}}}
	r := NewRegistry(nil, nil)
	r.Register(QuestionsSource{Questions: q})
	res, err := r.Evaluate(context.Background(), Query{})
	if err != nil {
		t.Fatal(err)
	}
	it := res.Items[0]
	if it.Reason != "waiting for your answer" {
		t.Fatalf("reason = %q (fixed sentence, not row text)", it.Reason)
	}
	if strings.Contains(it.Reason, "prompt") {
		t.Fatal("the prompt leaked into reason")
	}
	if it.Prompt != "the question's own prompt text" {
		t.Fatalf("prompt field = %q", it.Prompt)
	}
	if !strings.Contains(it.DoorSentence, "approve | deny") || it.DoorMethod != "answer" {
		t.Fatalf("door = (%q, %q), want answer with the closed options", it.DoorMethod, it.DoorSentence)
	}
}

func TestBlockedKindIsReadNotGuessed(t *testing.T) {
	seed := []struct {
		name       string
		lastReason string
		reason     store.BlockedReason
		wantKind   string
		wantSev    Severity
	}{
		{"payload kind wins", "unattended loop denied an outward action",
			store.BlockedReason{Kind: "action-denied", At: "2026-11-01T00:00:00Z"},
			"blocked.action-denied", Blocked},
		{"cap-hit from payload", "cap", store.BlockedReason{Kind: "cap-hit", At: "2026-11-01T00:00:00Z"},
			"blocked.cap-hit", NeedsYou},
		{"stuck from payload", "stuck", store.BlockedReason{Kind: "stuck", At: "2026-11-01T00:00:00Z"},
			"blocked.stuck", Stuck},
		{"lastReason marker when no event", "cap-hit", store.BlockedReason{},
			"blocked.cap-hit", NeedsYou},
		{"unknown kind classifies other", "loop budget exhausted",
			store.BlockedReason{Kind: "quantum-drift", At: "2026-11-01T00:00:00Z"},
			"blocked.other", Blocked},
		{"no marker at all classifies other", "some daemon sentence nobody compiled",
			store.BlockedReason{}, "blocked.other", Blocked},
	}
	for _, tc := range seed {
		t.Run(tc.name, func(t *testing.T) {
			b := &fakeBlocked{
				loops: []store.AttentionBlockedLoop{{
					LoopID: "l1", Slug: "s", ComponentRoot: "/w/a",
					LastReason: tc.lastReason, UpdatedAt: "2026-11-02T00:00:00Z",
				}},
				reasons: map[string]store.BlockedReason{"l1": tc.reason},
			}
			r := NewRegistry(nil, nil)
			r.Register(BlockedSource{Loops: b})
			res, err := r.Evaluate(context.Background(), Query{})
			if err != nil {
				t.Fatal(err)
			}
			it := res.Items[0]
			if it.Kind != tc.wantKind || it.Severity != tc.wantSev {
				t.Fatalf("kind/severity = %s/%s, want %s/%s", it.Kind, it.Severity, tc.wantKind, tc.wantSev)
			}
			if it.Reason != tc.lastReason {
				t.Fatalf("reason = %q, want LoopView's stored sentence VERBATIM", it.Reason)
			}
			if tc.reason.At != "" && it.Since != tc.reason.At {
				t.Fatalf("since = %q, want the event instant %q", it.Since, tc.reason.At)
			}
			if tc.wantKind == "blocked.other" && it.DoorMethod != "" {
				t.Fatalf("blocked.other named a door method %q (generic only)", it.DoorMethod)
			}
		})
	}
}

func TestStoppedDoorCarriesStepID(t *testing.T) {
	st := &fakeStopped{rows: []store.AttentionStoppedLoop{
		{LoopID: "l1", Slug: "s", ComponentRoot: "/w/a", StepID: 42, UpdatedAt: "2026-11-02T00:00:00Z"},
		{LoopID: "l2", Slug: "s2", ComponentRoot: "/w/a", StepID: 0, UpdatedAt: "2026-11-02T00:00:00Z"},
	}}
	r := NewRegistry(func(m string) bool { return m == "step.continue" }, nil)
	r.Register(StoppedSource{Loops: st})
	res, err := r.Evaluate(context.Background(), Query{})
	if err != nil {
		t.Fatal(err)
	}
	byLoop := map[string]Item{}
	for _, it := range res.Items {
		byLoop[it.LoopID] = it
	}
	withStep := byLoop["l1"]
	if withStep.DoorMethod != "step.continue" || !strings.Contains(withStep.DoorSentence, "42") {
		t.Fatalf("stopped door = (%q, %q), want step.continue naming step 42", withStep.DoorMethod, withStep.DoorSentence)
	}
	withStep.Severity = Stopped
	noStep := byLoop["l2"]
	if noStep.DoorMethod != "step.continue" || strings.Contains(noStep.DoorSentence, "step continue 0") {
		t.Fatalf("stopped door invented a step id: (%q, %q)", noStep.DoorMethod, noStep.DoorSentence)
	}
}

func TestRecurrenceIsANewKey(t *testing.T) {
	k1 := KeyOf("blocked", "l1", "2026-11-01T00:00:00Z")
	k2 := KeyOf("blocked", "l1", "2026-11-02T00:00:00Z")
	if k1 == k2 {
		t.Fatal("a recurrence reuses its key (an old ack would hide the fresh problem)")
	}
	if !strings.HasPrefix(k2, "blocked:l1:") {
		t.Fatalf("key shape = %q", k2)
	}
}

func TestAckedItemHidesAndReappears(t *testing.T) {
	q := &fakeQuestions{rows: []store.AttentionQuestion{{
		QuestionID: 9, LoopSlug: "s", ComponentRoot: "/w/a", Kind: "clarify",
		Prompt: "p", AskedAt: "2026-11-02T00:00:00Z",
	}}}
	r := NewRegistry(nil, func(keys []string) (map[string]Ack, error) {
		return map[string]Ack{keys[0] + "": {}}, nil
	})
	r.Register(QuestionsSource{Questions: q})
	res, err := r.Evaluate(context.Background(), Query{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Items) != 0 {
		t.Fatalf("acked item still visible: %+v", res.Items)
	}
	res, err = r.Evaluate(context.Background(), Query{IncludeAcked: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Items) != 1 || res.Items[0].Acked == nil {
		t.Fatalf("acked item did not reappear with its state: %+v", res.Items)
	}
}

func TestSnoozeExpiry(t *testing.T) {
	now := time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC)
	future := now.Add(time.Hour).Format(time.RFC3339)
	past := now.Add(-time.Hour).Format(time.RFC3339)
	if snoozeExpired(Ack{Until: future}, now) {
		t.Fatal("a live snooze reported expired")
	}
	if !snoozeExpired(Ack{Until: past}, now) {
		t.Fatal("an expired snooze still hid the item")
	}
	if snoozeExpired(Ack{}, now) {
		t.Fatal("a plain ack expires on its own (SV3: it hides until undone)")
	}
}

// ---------- scope ----------------------------------------------------------

func TestScopeUnknownRefusal(t *testing.T) {
	tree := testTree()
	if _, err := ResolveScope(tree, "cmp-nope"); !errors.Is(err, ErrUnknownScope) {
		t.Fatalf("unknown id = %v, want the typed refusal (never an empty answer)", err)
	}
	sc, err := ResolveScope(tree, "cmp-a")
	if err != nil {
		t.Fatal(err)
	}
	// A root resolves to its OWN path — the store's subtree arm carries the
	// children (S22: a child lives UNDER its parent's path).
	if len(sc.RootPaths) != 1 || sc.RootPaths[0] != "/w/a" {
		t.Fatalf("root scope = %v, want [/w/a]", sc.RootPaths)
	}
	sc, err = ResolveScope(tree, "cmp-b")
	if err != nil {
		t.Fatal(err)
	}
	if len(sc.RootPaths) != 1 || sc.RootPaths[0] != "/w/a/b" {
		t.Fatalf("child scope = %v, want [/w/a/b] (a child scopes to the child)", sc.RootPaths)
	}
	every, err := EveryScope(tree)
	if err != nil {
		t.Fatal(err)
	}
	if len(every.RootPaths) != 2 {
		t.Fatalf("every-scope = %v, want the two ROOTS (children ride their parents' subtree arm)", every.RootPaths)
	}
}

func TestOwnerMapsLongestPrefix(t *testing.T) {
	om, err := Owners(testTree())
	if err != nil {
		t.Fatal(err)
	}
	if c, r := om.Owner("/w/a/b"); c != "cmp-b" || r != "cmp-a" {
		t.Fatalf("owner(/w/a/b) = %s/%s, want cmp-b under cmp-a", c, r)
	}
	if c, r := om.Owner("/w/a/sub/dir"); c != "cmp-a" || r != "cmp-a" {
		t.Fatalf("owner(/w/a/sub/dir) = %s/%s, want cmp-a (the subtree, no deeper component claims it)", c, r)
	}
	if c, r := om.Owner("/somewhere/else"); c != "" || r != "" {
		t.Fatalf("owner of an unclaimed path = %s/%s, want no ids rather than wrong ones", c, r)
	}
}

// ---------- tiny fake sources ----------------------------------------------

type fakeSrc struct{ n string }

func (f *fakeSrc) Name() string                                 { return f.n }
func (f *fakeSrc) Items(context.Context, Query) ([]Item, error) { return nil, nil }

type failingSrc struct{}

func (failingSrc) Name() string { return "broken" }
func (failingSrc) Items(context.Context, Query) ([]Item, error) {
	return nil, errors.New("boom: the read said no")
}
