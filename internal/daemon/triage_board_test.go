package daemon

// ════════════════════════════════════════════════════════════════════════════
// S51 T2 — the board seam at the daemon: a read that assembles a whole
// backlog row (audit attempts, SCORED candidates, hand-off status) and in
// doing so writes NOTHING.
//
// The read-only claim is the load-bearing one: the suggest path stores an
// embedding because asking for one computes one; the board of forty bugs
// that indexed forty rows would turn browsing into authorship. The test
// proves it behaviourally — a bug the board displayed but never indexed,
// because a second board pass still cannot see it as anyone's candidate.
// ════════════════════════════════════════════════════════════════════════════

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
	"github.com/cjlapao/aeon/internal/triage"
)

func openBoardDaemon(t *testing.T) (*Daemon, string) {
	t.Helper()
	st, err := store.Open(store.Options{Dir: t.TempDir(), Env: func(string) string { return "" }})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	root := t.TempDir()
	if _, err := st.CreateComponent(store.NewComponent{Name: "r", Slug: "r", Root: root}); err != nil {
		t.Fatalf("seed component: %v", err)
	}
	d, _ := newTestDaemonWithStore(t, st)
	d.initTriage()
	return d, root
}

func boardPerson() triage.Actor { return triage.Actor{Kind: "user", ID: "42"} }

func TestBoardCandidatesAreReadOnly(t *testing.T) {
	d, root := openBoardDaemon(t)
	tr := d.Table.Triage
	near, ref := tr.CreateAudited(root, triage.NewBug{Title: "counter counts one too many", Body: "the third try double counts", FiledBy: "person"}, boardPerson())
	if ref != nil {
		t.Fatal(ref)
	}

	far, ref := tr.CreateAudited(root, triage.NewBug{Title: "export drops the year", Body: "only the month prints", FiledBy: "person"}, boardPerson())
	if ref != nil {
		t.Fatal(ref)
	}

	// Seed ONE embedding through the honest door (suggest's own path).
	if err := tr.StoreEmbedding(near, triageEmbedDim, triageEmbedding(near.Title+" "+near.Body)); err != nil {
		t.Fatalf("seed embedding: %v", err)
	}
	probe := func() []store.BugVecHit {
		hits, err := d.store.QueryBugVec(root, triageEmbedDim, triageEmbedding("counter double counts the third try"), 50)
		if err != nil {
			t.Fatalf("vec probe: %v", err)
		}
		return hits
	}
	vecBefore := probe()
	if len(vecBefore) != 1 {
		t.Fatalf("vec seed: %d rows", len(vecBefore))
	}
	rowsBefore, err := d.triageBoard(context.Background(), protocol.TriageBoardParams{ComponentRoot: root})
	if err != nil {
		t.Fatalf("board: %v", err)
	}
	byID := map[int64]protocol.TriageBoardRow{}
	for _, r := range rowsBefore.([]protocol.TriageBoardRow) {
		byID[r.Bug.ID] = r
	}
	// far's row carries near as a scored candidate — the score rides.
	cands := byID[far.ID].Candidates
	if len(cands) != 1 || cands[0].Bug.ID != near.ID || cands[0].Distance <= 0 {
		t.Fatalf("scored candidates: %+v — want the one indexed bug with a distance", cands)
	}
	// And nothing was written: the vec table still holds the ONE seeded
	// row, the bug rows read unchanged, and the audits gained no rows.
	vecAfter := probe()
	if !reflect.DeepEqual(vecBefore, vecAfter) {
		t.Errorf("the board wrote vec rows: %+v vs %+v", vecBefore, vecAfter)
	}
	rows2, err := d.triageBoard(context.Background(), protocol.TriageBoardParams{ComponentRoot: root})
	if err != nil {
		t.Fatalf("board 2: %v", err)
	}
	for _, row := range rows2.([]protocol.TriageBoardRow) {
		if row.Bug.ID == near.ID && len(row.Candidates) != 0 {
			t.Errorf("the board indexed the bug it displayed — near now has candidates %+v (a read that authors is not a read)", row.Candidates)
		}
		hist, ref := tr.History(row.Bug.ID, 0)
		if ref != nil {
			t.Fatal(ref)
		}
		if len(hist) != 1 { // the one file row each bug entered with
			t.Errorf("bug %d gained %d audit rows from a board read", row.Bug.ID, len(hist)-1)
		}
	}
	b, _, _ := tr.Show(far.ID)
	if b.UpdatedAt == "" || b.CreatedAt != b.UpdatedAt {
		t.Errorf("the board touched bug rows (created %q updated %q)", b.CreatedAt, b.UpdatedAt)
	}
}

func TestBoardAndListAgree(t *testing.T) {
	d, root := openBoardDaemon(t)
	tr := d.Table.Triage
	b1, ref := tr.CreateAudited(root, triage.NewBug{Title: "one", Body: "b", FiledBy: "person"}, boardPerson())
	if ref != nil {
		t.Fatal(ref)
	}

	b2, ref := tr.CreateAudited(root, triage.NewBug{Title: "two", Body: "b", FiledBy: "person"}, boardPerson())
	if ref != nil {
		t.Fatal(ref)
	}

	reactor := triage.Actor{Kind: "system", ID: "reactor"}
	if _, r := tr.TransitionAudited(b2.ID, triage.ValidationReproduced, reactor); r != nil {
		t.Fatalf("transition: %v", r)
	}
	listed, err := tr.List(root, triage.ListFilter{})
	if err != nil {
		t.Fatal(err)
	}
	rows, err := d.triageBoard(context.Background(), protocol.TriageBoardParams{ComponentRoot: root})
	if err != nil {
		t.Fatalf("board: %v", err)
	}
	boardRows := rows.([]protocol.TriageBoardRow)
	if len(boardRows) != len(listed) {
		t.Fatalf("board rows %d, list rows %d", len(boardRows), len(listed))
	}
	for i := range listed {
		if boardRows[i].Bug.ID != listed[i].ID {
			t.Fatalf("row %d: board %d, list %d — the two reads disagree on ORDER too", i, boardRows[i].Bug.ID, listed[i].ID)
		}
	}
	// The validation filter: the board answers in the vocabulary and the
	// filtered board equals the filtered list.
	if _, ref := tr.TransitionAudited(b1.ID, triage.ValidationReproduced, reactor); ref != nil {
		t.Fatalf("transition 1: %v", ref)
	}
	filtered, err := d.triageBoard(context.Background(), protocol.TriageBoardParams{ComponentRoot: root, Validation: "reproduced"})
	if err != nil {
		t.Fatalf("filtered board: %v", err)
	}
	fr := filtered.([]protocol.TriageBoardRow)
	if len(fr) != 2 {
		t.Fatalf("reproduced board: %d rows want 2", len(fr))
	}
	for _, r := range fr {
		if r.Describe.ValidationSentence == "" || r.Describe.PrioritySentence == "" {
			t.Errorf("bug %d: a sentence went unrendered on the board", r.Bug.ID)
		}
		if !strings.Contains(r.Describe.ValidationSentence, "reproduced") {
			t.Errorf("filter said reproduced, sentence says %q", r.Describe.ValidationSentence)
		}
	}
}

func TestBoardRefusesAWordOutsideTheVocabulary(t *testing.T) {
	d, root := openBoardDaemon(t)
	tr := d.Table.Triage
	if _, err := tr.CreateAudited(root, triage.NewBug{Title: "any", Body: "b", FiledBy: "person"}, boardPerson()); err != nil {
		t.Fatal(err)
	}
	// `brainstorm` is S51 T3's setting word, never a bug state — the board
	// must refuse it BY NAME and change nothing.
	for _, p := range []protocol.TriageBoardParams{
		{Validation: "brainstorm"},
		{Priority: "critical"},
	} {
		_, err := d.triageBoard(context.Background(), p)
		if err == nil {
			t.Fatalf("filter %+v accepted", p)
		}
		word := p.Validation + p.Priority
		if !strings.Contains(err.Error(), word) || !strings.Contains(err.Error(), "triage.vocabulary") {
			t.Errorf("filter %q: refusal %q must name the word and the vocabulary door", word, err)
		}
	}
}

func TestBoardHandoffAndAttemptsDigest(t *testing.T) {
	d, root := openBoardDaemon(t)
	tr := d.Table.Triage
	handed, ref := tr.CreateAudited(root, triage.NewBug{Title: "handed", Body: "b", FiledBy: "person"}, boardPerson())
	if ref != nil {
		t.Fatal(ref)
	}

	plain, ref := tr.CreateAudited(root, triage.NewBug{Title: "plain", Body: "b", FiledBy: "person"}, boardPerson())
	if ref != nil {
		t.Fatal(ref)
	}

	if _, r := tr.RecordHandoffAudited(handed.ID, root, 777, boardPerson()); r != nil {
		t.Fatalf("hand-off: %v", r)
	}
	reactor := triage.Actor{Kind: "system", ID: "reactor"}
	for i := 0; i < 2; i++ {
		if r := tr.AppendValidationAudit(plain.ID, reactor); r != nil {
			t.Fatalf("attempt %d: %v", i, r)
		}
	}
	yes, err := d.triageBoard(context.Background(), protocol.TriageBoardParams{ComponentRoot: root, HandedOff: ptrTrue()})
	if err != nil {
		t.Fatal(err)
	}
	yr := yes.([]protocol.TriageBoardRow)
	if len(yr) != 1 || yr[0].Bug.ID != handed.ID {
		t.Fatalf("handedOff:true board: %+v", yr)
	}
	// Item 777 was never created — the honest answer for a gone item.
	if yr[0].RoadmapStatus != "unknown (the roadmap item row is absent)" {
		t.Errorf("roadmap status: %q", yr[0].RoadmapStatus)
	}
	if !strings.Contains(yr[0].Describe.Handoff, "roadmap item 777") {
		t.Errorf("describe handoff: %q", yr[0].Describe.Handoff)
	}
	no, err := d.triageBoard(context.Background(), protocol.TriageBoardParams{ComponentRoot: root, HandedOff: ptrFalse()})
	if err != nil {
		t.Fatal(err)
	}
	nr := no.([]protocol.TriageBoardRow)
	if len(nr) != 1 || nr[0].Bug.ID != plain.ID {
		t.Fatalf("handedOff:false board: %+v", nr)
	}
	// Attempts come from the audit rows, not a column: two dispatches,
	// newest attempt named.
	var attempts int
	var last string
	for _, r := range nr {
		if r.Bug.ID == plain.ID {
			attempts, last = r.Attempts, r.LastAttemptAt
		}
	}
	if attempts != 2 || last == "" {
		t.Errorf("attempts digest: %d/%q — want 2 attempts and a newest time", attempts, last)
	}
}

func ptrTrue() *bool  { v := true; return &v }
func ptrFalse() *bool { v := false; return &v }
