package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/loop"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/skills"
	"github.com/cjlapao/aeon/internal/store"
)

// chosen_skills_test.go — S68 T3. The CHOSEN-skills rules (B6) and the drain
// composition (B7) are pinned here: the resolver's refusals, the skipped
// report, the caps, and the one place that still joins chosen lines to the
// operator's pure words.

// scanner returns a hand-built catalog so the resolver is tested without a
// filesystem — the offer tests already prove the real scan; here the subject
// is what the CHOSEN names do against a known set.
type scanner struct {
	res *skills.ScanResult
	err error
}

func (s scanner) Scan(bool) (*skills.ScanResult, error) { return s.res, s.err }

func cat(sks ...*skills.Skill) *skills.ScanResult {
	return &skills.ScanResult{Skills: sks}
}

func ok(name string) *skills.Skill {
	return &skills.Skill{Name: name, Status: skills.StatusValid, Path: "/root/" + name}
}
func bad(name string) *skills.Skill {
	return &skills.Skill{Name: name, Status: skills.StatusInvalid, Path: "/root/" + name}
}
func shadow(name string) *skills.Skill {
	return &skills.Skill{Name: name, Status: skills.StatusValid, Path: "/root/" + name, Shadowed: true}
}

func TestTheChosenSkillSentenceIsTheOneTheChipPreviews(t *testing.T) {
	// B11 shared fixture, daemon side: this exact sentence is what the app's
	// chip tooltip previews (the UI test pins its copy). One template, both
	// sides — what you preview is what rides.
	got := chosenSkillLine("caveman", "/root/caveman")
	want := "use the caveman skill — read /root/caveman/SKILL.md before answering."
	if got != want {
		t.Fatalf("the canonical sentence drifted:\n got %q\nwant %q", got, want)
	}
}

func TestUnknownChosenNamesRefuseNamingEveryOffender(t *testing.T) {
	// P31: a chosen skill that does not exist never reads as one that rode.
	// The refusal names EVERY unknown, not just the first.
	res, err := chosenSkillLines(cat(ok("real")), []string{"ghost", "real", "phantom"})
	if err == nil {
		t.Fatalf("unknown names must refuse, got %+v", res)
	}
	for _, off := range []string{"ghost", "phantom"} {
		if !strings.Contains(err.Error(), off) {
			t.Errorf("the refusal must name %q, got: %v", off, err)
		}
	}
	if res != nil {
		t.Errorf("a refusal carries no resolution")
	}
}

func TestChosenLinesDedupeKeepOrderAndSkipUnpickable(t *testing.T) {
	// Order is the operator's, duplicates collapse, and a known-but-
	// unpickable skill (invalid or shadowed — the OFFER's excluded states)
	// is skipped AND reported, never silently gone.
	res, err := chosenSkillLines(
		cat(ok("alpha"), ok("beta"), bad("broken"), shadow("hidden")),
		[]string{"beta", "alpha", "beta", "broken", "hidden"})
	if err != nil {
		t.Fatalf("no unknowns here: %v", err)
	}
	if len(res.Lines) != 2 {
		t.Fatalf("two ride (broken/hidden skipped), got %d: %v", len(res.Lines), res.Lines)
	}
	if !strings.HasPrefix(res.Lines[0], "use the beta skill") || !strings.HasPrefix(res.Lines[1], "use the alpha skill") {
		t.Fatalf("chosen order lost: %v", res.Lines)
	}
	if strings.Join(res.Skipped, ",") != "broken,hidden" {
		t.Fatalf("the skipped must report both unpickable names, got %v", res.Skipped)
	}
	note := chosenSkipNote(res.Skipped)
	if !strings.Contains(note, "broken") || !strings.Contains(note, "hidden") {
		t.Fatalf("the opening note must name the skipped, got %q", note)
	}
}

func TestChosenCapsRefuseRatherThanTruncate(t *testing.T) {
	// The choice was made or it was not; a truncated choice is a lie.
	var many []string
	for i := 0; i < skillsInjectMaxEntries+1; i++ {
		many = append(many, fmt.Sprintf("s%d", i))
	}
	var sks []*skills.Skill
	for _, n := range many {
		sks = append(sks, ok(n))
	}
	if _, err := chosenSkillLines(cat(sks...), many); err == nil {
		t.Fatal("over the entry cap must refuse, not truncate")
	}

	// A large render (long paths) trips the byte cap on its own.
	var wide []*skills.Skill
	var names []string
	for i := 0; i < 40; i++ {
		n := fmt.Sprintf("w%d", i)
		wide = append(wide, &skills.Skill{Name: n, Status: skills.StatusValid, Path: strings.Repeat("p", 1200)})
		names = append(names, n)
	}
	if _, err := chosenSkillLines(cat(wide...), names); err == nil ||
		!strings.Contains(err.Error(), "over the") {
		t.Fatalf("a too-large render must refuse naming the bound, got %v", err)
	}
}

func TestTheChosenDoorRefusesWhatTheOfferOnlyLogs(t *testing.T) {
	// Divergence from the offer, on purpose: a catalog the offer would skip
	// and log, the CHOICE refuses — a chosen skill whose catalog is
	// unreadable cannot be confirmed to have ridden (P31).
	if _, err := chosenSkillScan(scanner{err: fmt.Errorf("disk gone")}, []string{"x"}); err == nil {
		t.Fatal("a catalog fault must refuse a choice, not pass it")
	}
	// No catalog wired: a choice is refused with a sentence, empty passes.
	if _, err := chosenCatalogResolver(nil)([]string{"x"}); err == nil {
		t.Fatal("choosing with no catalog must refuse")
	}
	if r, err := chosenCatalogResolver(nil)(nil); r != nil || err != nil {
		t.Fatalf("choosing nothing passes through untouched: %+v %v", r, err)
	}
}

func TestRodeNamesKeepsOrderDedupesAndDropsSkipped(t *testing.T) {
	// The row can only claim what rode: skipped names are out, order and
	// dedup are the operator's choice as filed.
	res, err := chosenSkillLines(
		cat(ok("a"), ok("b"), bad("c")), []string{"b", "a", "b", "c"})
	if err != nil {
		t.Fatal(err)
	}
	rode := rodeNames([]string{"b", "a", "b", "c"}, res)
	if strings.Join(rode, ",") != "b,a" {
		t.Fatalf("rode = %v, want [b a] (skipped dropped, order kept, deduped)", rode)
	}
	if rodeNames([]string{"x"}, nil) != nil {
		t.Fatal("nothing resolved ⇒ nothing rode")
	}
}

func TestSteerWireTextIsLinesBlankLineWords(t *testing.T) {
	// B7: the wire message the model reads is the chosen lines, a blank
	// line, then the operator's PURE words — joined only here.
	resolve := func(names []string) (*chosenSkillResolution, error) {
		var lines []string
		for _, n := range names {
			lines = append(lines, chosenSkillLine(n, "/r/"+n))
		}
		return &chosenSkillResolution{Lines: lines}, nil
	}
	row := store.Steer{Text: "Also mention Go.", Skills: []string{"caveman"}}
	got := steerWireText(row, resolve)
	want := "use the caveman skill — read /r/caveman/SKILL.md before answering.\n\nAlso mention Go."
	if got != want {
		t.Fatalf("drain composition wrong:\n got %q\nwant %q", got, want)
	}

	// Nothing chosen ⇒ the person's words byte-for-byte (pre-S68 shape).
	if steerWireText(store.Steer{Text: "plain"}, resolve) != "plain" {
		t.Fatal("a skill-less steer must be byte-identical to today")
	}
	// A catalog that moved since the queue delivers the PURE text: the
	// instruction outranks its rider, and the row still shows the ask.
	broken := func([]string) (*chosenSkillResolution, error) { return nil, fmt.Errorf("catalog gone") }
	if steerWireText(row, broken) != "Also mention Go." {
		t.Fatal("a resolver fault must not drop the person's words")
	}
	if steerWireText(row, nil) != "Also mention Go." {
		t.Fatal("a nil resolver must deliver pure text")
	}
}

// chosenForLoop is the reload-after-restart door: the cache is a fast path
// over the durable kickoff row, never the truth itself.
func TestChosenForLoopReloadsFromTheKickoffRowAfterRestart(t *testing.T) {
	s, d := chosenStoreDaemon(t)
	sc := store.Scope{LoopSlug: "chosen", ComponentRoot: "/r"}
	// The launch's receipt row: a human kickoff carrying the chosen names.
	if _, err := s.Note(sc, store.NewChatMessage{Author: "human", Kind: "note",
		Body: "the goal", Skills: []string{"caveman", "broken"}}); err != nil {
		t.Fatal(err)
	}
	st := loop.LoopState{ID: "loop-1", Slug: "chosen", ComponentRoot: "/r"}
	// The resolver knows both names, but "broken" has since turned invalid:
	// a reload must keep riding caveman and REPORT the one that cannot
	// ride, rather than die or silently drop the operator's choice.
	d.resolveChosen = func(names []string) (*chosenSkillResolution, error) {
		return chosenSkillLines(cat(ok("caveman"), bad("broken")), names)
	}
	got := d.chosenForLoop(st)
	if got == nil {
		t.Fatal("a launch that chose must reload its choice")
	}
	if len(got.Lines) != 1 || !strings.HasPrefix(got.Lines[0], "use the caveman skill") {
		t.Fatalf("reload rendered %v", got.Lines)
	}
	if !strings.Contains(got.Note, "broken") {
		t.Fatalf("the reload must report the skill that could not ride, got %q", got.Note)
	}
	// A loop that chose nothing (no human row) reloads to nil — the pre-S68
	// shape, cached as absence so dispatch stops re-reading.
	st2 := loop.LoopState{ID: "loop-2", Slug: "quiet", ComponentRoot: "/r"}
	if _, err := s.CreateLoop(store.NewLoop{Slug: "quiet", ComponentRoot: "/r"}); err != nil {
		t.Fatal(err)
	}
	if d.chosenForLoop(st2) != nil {
		t.Fatal("a loop with no receipt chose nothing")
	}
}

func TestForgetChosenMakesTheNextReadComeFromTheRows(t *testing.T) {
	s, d := chosenStoreDaemon(t)
	_ = s
	st := loop.LoopState{ID: "loop-1", Slug: "chosen", ComponentRoot: "/r"}
	// Seed a stale launch choice, then forget it (a restart's act): the next
	// read must fall through to the rows, not the poisoned cache.
	d.rememberChosen(st.ID, chosenLaunch{Lines: []string{"STALE"}})
	d.forgetChosen(st.ID)
	d.resolveChosen = func([]string) (*chosenSkillResolution, error) { return nil, nil }
	if got := d.chosenForLoop(st); got != nil {
		t.Fatalf("after forget the stale entry is gone and no row chose: %+v", got)
	}
}

// chosenStoreDaemon builds a daemon over a fresh store with one loop, enough
// to exercise the reload door without a socket or a model.
func chosenStoreDaemon(t *testing.T) (*store.Store, *Daemon) {
	t.Helper()
	s, err := store.Open(store.Options{Dir: t.TempDir(), Env: func(string) string { return "" }})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if _, err := s.CreateLoop(store.NewLoop{Slug: "chosen", ComponentRoot: "/r", Goal: "the goal"}); err != nil {
		t.Fatal(err)
	}
	d := &Daemon{store: s, log: discardLogger{}, chosen: map[string]chosenLaunch{}}
	return s, d
}

func TestChosenSkillsRideOneTurnOfTheConversationAndNoOther(t *testing.T) {
	// The turn-scoped ruling (B4/B6), end to end through the socket: the
	// names ride the ONE turn they were sent with — the lines land in that
	// turn's system half, the names land on that turn's thread row, and the
	// person's user message stays their own words alone (B3). The next
	// message chooses again, or nothing.
	f, m := gatedFixture(t, []gatedTurn{{deltas: []string{turnOK}}, {deltas: []string{turnOK}}})
	f.d.resolveChosen = func(names []string) (*chosenSkillResolution, error) {
		return chosenSkillLines(cat(ok("caveman")), names)
	}
	initConn(t, f.d, context.Background(), "c-sk", "cli")
	l := restedLoop(t, f, "")
	res := dispatchJSON(t, f, "c-sk", protocol.MethodLoopMessage, map[string]any{
		"id": l.ID, "text": "check the tests", "skills": []string{"caveman"}})
	if res["turn"] != "started" {
		t.Fatalf("a turn with a valid choice was not started: %v", res)
	}
	waitFor(t, "the model call", func() bool { return m.callCount() >= 1 })
	waitTurnIdle(t, f.d, l.ID)

	msgs := m.lastSent(0)
	var all, words string
	for _, msg := range msgs {
		for _, b := range msg.Content {
			all += b.Text
		}
	}
	last := msgs[len(msgs)-1]
	if string(last.Role) != "user" {
		t.Fatalf("the wire does not end on the person's message: %v", msgs)
	}
	for _, b := range last.Content {
		words += b.Text
	}
	if words != "check the tests" {
		t.Fatalf("the user message is not the words alone (B3): %q", words)
	}
	if !strings.Contains(all, "## Chosen skills") ||
		!strings.Contains(all, "use the caveman skill — read /root/caveman/SKILL.md") {
		t.Fatalf("the chosen line is not on this turn's system half: %q", all)
	}

	// The thread row: pure body, names as receipt.
	rows, err := f.s.Thread(store.Scope{LoopSlug: "chat", ComponentRoot: f.root}, 0)
	if err != nil {
		t.Fatal(err)
	}
	var human *store.ChatMessage
	for i := range rows {
		if rows[i].Author == "human" {
			human = &rows[i]
			break
		}
	}
	if human == nil || human.Body != "check the tests" ||
		len(human.Skills) != 1 || human.Skills[0] != "caveman" {
		t.Fatalf("the thread row is not pure-words-plus-receipt: %+v", human)
	}

	// The NEXT turn, sent without skills: nothing chosen for it — not the
	// last turn's lines wearing a turn they were never chosen for.
	if res2 := dispatchJSON(t, f, "c-sk", protocol.MethodLoopMessage, map[string]any{
		"id": l.ID, "text": "and now nothing"}); res2["turn"] != "started" {
		t.Fatalf("the second turn did not start: %v", res2)
	}
	waitFor(t, "the second model call", func() bool { return m.callCount() >= 2 })
	waitTurnIdle(t, f.d, l.ID)
	msgs2 := m.lastSent(1)
	var all2, words2 string
	for _, msg := range msgs2 {
		for _, b := range msg.Content {
			all2 += b.Text
		}
	}
	for _, b := range msgs2[len(msgs2)-1].Content {
		words2 += b.Text
	}
	if words2 != "and now nothing" {
		t.Fatalf("the second user message was decorated: %q", words2)
	}
	if strings.Contains(all2, "caveman") {
		t.Fatalf("a skill chosen for one turn rode the next: %q", all2)
	}
}

func TestTheMessageDoorRefusesUnknownSkillsAndTouchesNothing(t *testing.T) {
	// B6 at the live door: an unknown name refuses the WHOLE call, names
	// every offender, and leaves the loop exactly as it was — no thread row,
	// no turn, no model call. A partly-applied choice is no choice.
	f, m := gatedFixture(t, nil)
	f.d.resolveChosen = func(names []string) (*chosenSkillResolution, error) {
		return chosenSkillLines(cat(ok("caveman")), names)
	}
	initConn(t, f.d, context.Background(), "c-rej", "cli")
	l := restedLoop(t, f, "")
	sc := store.Scope{LoopSlug: "chat", ComponentRoot: f.root}
	before, err := f.s.Thread(sc, 0)
	if err != nil {
		t.Fatal(err)
	}

	raw, err := json.Marshal(map[string]any{"id": l.ID, "text": "hi",
		"skills": []string{"ghost", "phantom"}})
	if err != nil {
		t.Fatal(err)
	}
	_, derr := f.d.Table.Dispatch(context.Background(), "c-rej", protocol.MethodLoopMessage, raw)
	var bp protocol.ErrBadParams
	if !errors.As(derr, &bp) {
		t.Fatalf("an unknown-skill choice must refuse as ErrBadParams, got %v", derr)
	}
	for _, off := range []string{"ghost", "phantom"} {
		if !strings.Contains(derr.Error(), off) {
			t.Errorf("the refusal must name %q, got: %v", off, derr)
		}
	}
	if strings.Contains(derr.Error(), "caveman") {
		t.Errorf("the refusal must not punish the valid name: %v", derr)
	}
	after, err := f.s.Thread(sc, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before) {
		t.Fatalf("a refused call wrote %d rows over %d", len(after), len(before))
	}
	if m.callCount() != 0 {
		t.Fatalf("a refused call reached the model")
	}
}

// ── the start door: chosen skills on loop.start ─────────────────────────────

func TestTheStartDoorRefusesUnknownSkillsAndCreatesNoLoop(t *testing.T) {
	// B6 before the first row: the choice is checked BEFORE anything is
	// written — a refused start leaves no loop, no run, no thread, nothing
	// for a later reader to mistake for a launch that chose.
	d, s, path, root := orgStartEnv(t, plainYAML)
	d.resolveChosen = func(names []string) (*chosenSkillResolution, error) {
		return chosenSkillLines(cat(ok("caveman")), names)
	}
	_, err := d.Table.StartBlueprintLoop(context.Background(), protocol.LoopStartParams{
		Blueprint: path, Goal: "audit the deps", WorkspaceRoot: root, Slug: "ghosted",
		Skills: []string{"ghost"}})
	var bp protocol.ErrBadParams
	if !errors.As(err, &bp) {
		t.Fatalf("unknown skills at the start door must refuse as ErrBadParams, got %v", err)
	}
	if !strings.Contains(err.Error(), "ghost") {
		t.Fatalf("the refusal must name the offender, got: %v", err)
	}
	if _, gerr := s.GetLoop(store.Scope{LoopSlug: "ghosted", ComponentRoot: root}); gerr == nil {
		t.Fatal("a refused start created the loop row anyway")
	}
}

func TestAStartWithChosenSkillsFilesTheReceiptAndCachesTheLaunch(t *testing.T) {
	// The launch's durable receipt: the goal row is the person's own words
	// with the names beside them (B8's purity, B6's durability), and the
	// resolved lines are cached so every step of THIS launch dispatches with
	// them — the reload path (chosenForLoop) is already proven by its own
	// test, which is what makes the cache a fast path and not a truth.
	d, s, path, root := orgStartEnv(t, plainYAML)
	d.resolveChosen = func(names []string) (*chosenSkillResolution, error) {
		return chosenSkillLines(cat(ok("caveman")), names)
	}
	orgStart(t, d, s, protocol.LoopStartParams{Blueprint: path, Goal: "audit the deps",
		WorkspaceRoot: root, Slug: "chosen-start", Skills: []string{"caveman"}})

	sc := store.Scope{LoopSlug: "chosen-start", ComponentRoot: root}
	l, err := s.GetLoop(sc)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := s.Thread(sc, 0)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, r := range rows {
		if r.Author == "human" && r.Body == "audit the deps" {
			found = true
			if len(r.Skills) != 1 || r.Skills[0] != "caveman" {
				t.Fatalf("the kickoff row lost its receipt: %+v", r)
			}
		}
	}
	if !found {
		t.Fatalf("no kickoff row carries the goal: %+v", rows)
	}
	d.chosenMu.Lock()
	launch, ok := d.chosen[l.ID]
	d.chosenMu.Unlock()
	if !ok || len(launch.Lines) != 1 || !strings.HasPrefix(launch.Lines[0], "use the caveman skill") {
		t.Fatalf("the launch cached no lines: %+v (ok=%v)", launch, ok)
	}
	if launch.Note != "" {
		t.Fatalf("nothing was skipped, so the note must be empty: %q", launch.Note)
	}
}
