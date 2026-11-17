package store

import (
	"path/filepath"
	"strings"
	"testing"
)

// message_source_test.go pins migration 0038 and the two doors that write
// its columns (S39 T5, CR6): every thread message and every steer knows
// where it came from — user, template, automation, blueprint — and a row
// nobody typed carries NO source rather than a borrowed one.

// seedPre0038 plants chat rows and a steer in a store parked at migration
// 37, so the next open runs 0038 over the seed (T1's recipe: rawGorm +
// applyMigrations + Open). Columns speak the live names (T0-Q4: 0035
// renamed project_root to component_root; the immutable DDL still says the
// old one).
func seedPre0038(t *testing.T, dir string) {
	t.Helper()
	path := filepath.Join(dir, storeFileName)
	g, closeDB := rawGorm(t, path)
	defer closeDB()
	applyMigrations(t, g, Migrations()[:37])
	if err := g.Exec(`INSERT INTO loop (id,slug,title,goal,root,component_root,mode,phase,status,started_at,updated_at,started_by,trigger_id)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		"l1", "a", "t", "g", "/r", "/r", "standard", "preflight", "running",
		"2026-01-01T00:00:00Z", "2026-01-02T00:00:00Z", "", "").Error; err != nil {
		t.Fatalf("seed loop: %v", err)
	}
	// Three thread rows in the pre-0038 shape: a person typing, an agent
	// answering, the system noting. This seed IS the backfill's evidence.
	for i, author := range []string{"human", "agent", "system"} {
		if err := g.Exec(`INSERT INTO chat_message (loop_slug, component_root, seq, author, kind, body, created_at)
			VALUES (?,?,?,?,?,?,?)`, "a", "/r", i+1, author, "note", "a pre-0038 row", "2026-01-02T00:00:00Z").Error; err != nil {
			t.Fatalf("seed chat row: %v", err)
		}
	}
	if err := g.Exec(`INSERT INTO step (loop_slug,component_root,phase,agent,status,started_at)
		VALUES (?,?,?,?,?,?)`, "a", "/r", "build", "x", "running", "2026-01-01T00:00:00Z").Error; err != nil {
		t.Fatalf("seed step: %v", err)
	}
	if err := g.Exec(`INSERT INTO step_steer (step_id, text, state, created_at)
		VALUES (1,'go left','pending','2026-01-02T00:00:00Z')`).Error; err != nil {
		t.Fatalf("seed steer: %v", err)
	}
}

func TestMessageSourceMigrationAddsSourceColumns(t *testing.T) {
	s := openTemp(t)
	// The tail of the column list IS the ALTER order. 0039_automation added
	// source_ref after 0038 added source, so chat_message ended
	// source, source_ref — and S68 (B6/B7) then appended the chosen-skills
	// receipts after both, so each table now ends source, …, skills. The
	// assertion tracks the ledger, it does not freeze it (2026-10-28,
	// S40 T1; extended at S68).
	for _, tc := range []struct {
		table string
		wants []string
	}{
		{"chat_message", []string{"source", "source_ref", "skills", "attachments"}},
		{"step_steer", []string{"source", "skills", "attachments"}},
	} {
		cols := tableColumns(t, s, tc.table)
		if len(cols) < len(tc.wants) {
			t.Fatalf("%s has %d columns, want at least %d", tc.table, len(cols), len(tc.wants))
		}
		for i, w := range tc.wants {
			if got := cols[len(cols)-len(tc.wants)+i]; got != w {
				t.Fatalf("%s must END with %v (ALTER order), got %v", tc.table, tc.wants, cols)
			}
		}
	}
}

func TestMessageSourceMigrationBackfillsHumanRowsAsUser(t *testing.T) {
	dir := t.TempDir()
	seedPre0038(t, dir)
	s := openAt(t, dir)
	for author, want := range map[string]any{"human": "user", "agent": nil, "system": nil} {
		var got any
		if err := s.r.QueryRow(`SELECT source FROM chat_message WHERE author = ?`, author).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("backfill: author %s reads source %v, want %v — only a human typing IS knowable as 'user'; the rest have NO source (P31)", author, got, want)
		}
	}
}

func TestMessageSourceMigrationSteerRowsDefaultToUser(t *testing.T) {
	dir := t.TempDir()
	seedPre0038(t, dir)
	s := openAt(t, dir)
	var got string
	if err := s.r.QueryRow(`SELECT source FROM step_steer WHERE id = 1`).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != "user" {
		t.Fatalf("a pre-0038 steer reads source %q, want user — before S39 a steer could only be a person's; the default states history", got)
	}
}

func TestChatSourceCHECKRefusesAFifthValue(t *testing.T) {
	s := openTemp(t)
	sc := seedLoop(t, s, "a", "/r")
	// The loop exists (the seed above), so the FK is satisfied and the only
	// thing left standing is the CHECK.
	_, err := s.w.Exec(`INSERT INTO chat_message (loop_slug, component_root, seq, author, kind, body, created_at, source)
		VALUES (?,?,?,?,?,?,?,?)`, sc.LoopSlug, sc.ComponentRoot, 1, "human", "note", "x", "2026-01-01T00:00:00Z", "bot")
	if err == nil || !strings.Contains(err.Error(), "CHECK") {
		t.Fatalf("source='bot': err %v, want the CHECK to refuse a fifth value", err)
	}
}

func TestNoteDefaultsTheSourceByAuthor(t *testing.T) {
	s := openTemp(t)
	sc := seedLoop(t, s, "a", "/r")
	human, err := s.Note(sc, NewChatMessage{Author: "human", Kind: "note", Body: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if human.Source != "user" {
		t.Fatalf("a human note with no explicit source reads %q, want user", human.Source)
	}
	agent, err := s.Note(sc, NewChatMessage{Author: "agent", Kind: "note", Body: "y"})
	if err != nil {
		t.Fatal(err)
	}
	if agent.Source != "" {
		t.Fatalf("an agent note reads source %q, want the empty string — the row has NO source, and reading 'user' would put words in a person's mouth", agent.Source)
	}
}

func TestNoteKeepsAnExplicitSource(t *testing.T) {
	s := openTemp(t)
	sc := seedLoop(t, s, "a", "/r")
	m, err := s.Note(sc, NewChatMessage{Author: "human", Kind: "note", Body: "x", Source: "template"})
	if err != nil {
		t.Fatal(err)
	}
	if m.Source != "template" {
		t.Fatalf("explicit source: got %q, want template — a typed start and a templated kickoff must be told apart (CR6)", m.Source)
	}
	if _, err := s.Note(sc, NewChatMessage{Author: "human", Kind: "note", Body: "z", Source: "bot"}); err == nil {
		t.Fatal("source=bot was accepted at the door — ValidSource is a closed set")
	} else if !strings.Contains(err.Error(), "user, template, automation, blueprint") {
		t.Fatalf("the refusal must name the vocabulary, got: %v", err)
	}
}

func TestThreadReturnsTheSource(t *testing.T) {
	s := openTemp(t)
	sc := seedLoop(t, s, "a", "/r")
	if _, err := s.Note(sc, NewChatMessage{Author: "human", Kind: "note", Body: "typed", Source: "automation"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Note(sc, NewChatMessage{Author: "agent", Kind: "note", Body: "answered"}); err != nil {
		t.Fatal(err)
	}
	rows, err := s.Thread(sc, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("thread has %d rows, want 2", len(rows))
	}
	if rows[0].Source != "automation" {
		t.Fatalf("thread read lost the source: %q, want automation (the SELECT must COALESCE it)", rows[0].Source)
	}
	if rows[1].Source != "" {
		t.Fatalf("agent row reads %q, want empty", rows[1].Source)
	}
}

func TestQueueSteerCarriesTheSource(t *testing.T) {
	s := openTemp(t)
	seedLoop(t, s, "a", "/r") // step's FK: the loop must exist (measured, T1)
	if _, err := s.w.Exec(`INSERT INTO step (loop_slug,component_root,phase,agent,status,started_at)
		VALUES (?,?,?,?,?,?)`, "a", "/r", "build", "x", "running", "2026-01-01T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	var stepID int64
	if err := s.r.QueryRow(`SELECT id FROM step LIMIT 1`).Scan(&stepID); err != nil {
		t.Fatal(err)
	}
	labelled, err := s.QueueSteer(stepID, NewSteer{Text: "x", Source: "blueprint"})
	if err != nil {
		t.Fatal(err)
	}
	if labelled.Source != "blueprint" {
		t.Fatalf("QueueSteer stored source %q, want blueprint — the scan order is the contract", labelled.Source)
	}
	plain, err := s.QueueSteer(stepID, NewSteer{Text: "y"})
	if err != nil {
		t.Fatal(err)
	}
	if plain.Source != "user" {
		t.Fatalf("a steer with no source reads %q, want the user default", plain.Source)
	}
	rows, err := s.ListSteers(stepID)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].Source != "blueprint" || rows[1].Source != "user" {
		t.Fatalf("ListSteers lost the labels: %q, %q", rows[0].Source, rows[1].Source)
	}
	if _, err := s.QueueSteer(stepID, NewSteer{Text: "z", Source: "bot"}); err == nil {
		t.Fatal("QueueSteer accepted source=bot")
	}
}
