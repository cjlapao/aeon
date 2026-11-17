package main

// S53 T4 — the parity record between the two doors `coverage` and
// `vocabulary` ride (HV6, P5). The law proved here: BOTH arms answer
// byte-identically because they run the same answer functions; the both-
// roots refusal is the same sentence on both roads (DB19, and the sentence
// now lives inside protocol.AnswerHistoryCoverage, so the roads cannot
// diverge by editing one); the selection routes a "no daemon" to the direct
// arm with the fixed notice and routes any OTHER dial fault to a refusal —
// never around it; and the printer keeps the three states as three words
// with the floor statement verbatim (P32/HV1). The three S25 verbs are not
// re-touched here — their goldens live in history_test.go and ride the
// unchanged direct road.

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/history"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

// seedHistoryParityStore lays the S25 fixture PLUS one rotten archive: the
// fixture (one live loop with a known-cost step, one blocked with a reason,
// one sound archive) plus an archive whose cold blob is corrupt, so the
// view's three states — shown, cannot say, not shown — are all exercised by
// one seed. Same recipe, twice: the two stores the two arms read must be
// identical for byte-parity to mean anything.
func seedHistoryParityStore(t *testing.T) *store.Store {
	t.Helper()
	return seedHistoryParityStoreIn(t, t.TempDir())
}

func seedHistoryParityStoreIn(t *testing.T, dir string) *store.Store {
	t.Helper()
	s, err := store.Open(store.Options{Dir: dir, Env: func(string) string { return "" }})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	seedHistoryFixture(t, s)
	if _, err := s.CreateLoop(store.NewLoop{Slug: "rotten", ComponentRoot: "/proj", Goal: "an archive that will not decode"}); err != nil {
		t.Fatalf("create rotten loop: %v", err)
	}
	if _, err := s.WriteDocument(store.Scope{LoopSlug: "rotten", ComponentRoot: "/proj"}, "brief.md", "# the brief", "cli"); err != nil {
		t.Fatalf("write document: %v", err)
	}
	if _, err := s.ArchiveLoop(store.Scope{LoopSlug: "rotten", ComponentRoot: "/proj"}, "cold"); err != nil {
		t.Fatalf("archive rotten: %v", err)
	}
	var rottenID string
	if err := s.GormDB().Raw(`SELECT id FROM loop_archive WHERE slug = 'rotten'`).Scan(&rottenID).Error; err != nil || rottenID == "" {
		t.Fatalf("find the rotten archive id: %q %v", rottenID, err)
	}
	if err := s.GormDB().Exec(`UPDATE loop_archive_file SET content = ? WHERE archive_id = ?`,
		[]byte{0x00, 0x01, 0x02}, rottenID).Error; err != nil {
		t.Fatalf("corrupt the blob: %v", err)
	}
	return s
}

// standHistorySocket stands the daemon half of an in-memory pipe: a real
// table with HistoryStore wired (the restoration makes any store-backed
// daemon carry it; here it is wired straight, as boot does) and the client
// dialed through the historyDial stand-in. The parityPipeServer (the
// autonomy parity harness, same package) dispatches through the REAL
// handlers, so the socket arm of the door speaks to the same code path
// production's dial would reach.
func standHistorySocket(t *testing.T, s *store.Store) {
	t.Helper()
	tbl := protocol.NewTable(protocol.NewSessions(), protocol.NewSubscriptions(), protocol.NewEventLog())
	tbl.HistoryStore = history.NewStore(s)
	conn := parityPipeServer(t, tbl)
	restore := historyDial
	t.Cleanup(func() { historyDial = restore })
	historyDial = func(int) (*protocol.Client, error) {
		c := protocol.NewClient(conn, ClientName, Version)
		if _, err := c.Connect(0); err != nil {
			_ = c.Close()
			return nil, err
		}
		return c, nil
	}
}

func TestHistoryCoverageDoorsAnswerIdenticalBytes(t *testing.T) {
	// ONE store, TWO doors — the strongest form of the claim: the same
	// rows, the same second, answered over the pipe and over the direct
	// arm, byte-for-byte. (Two stores would race their timestamps.)
	s := seedHistoryParityStore(t)
	restoreErr := historyErrOut
	t.Cleanup(func() { historyErrOut = restoreErr })

	standHistorySocket(t, s)
	var sockOut, noticeOut bytes.Buffer
	historyErrOut = &noticeOut
	if err := historyCoverage(&sockOut, []string{"--root", "/proj", "--json"}); err != nil {
		t.Fatalf("socket coverage: %v", err)
	}
	if noticeOut.Len() != 0 {
		t.Errorf("the socket road printed a notice: %q", noticeOut.String())
	}

	restoreDial := historyDial
	restoreOpen := historyDirectStoreOpen
	t.Cleanup(func() { historyDial = restoreDial; historyDirectStoreOpen = restoreOpen })
	historyDial = func(int) (*protocol.Client, error) {
		return nil, &noDaemonError{path: "/tmp/none.sock", cause: os.ErrNotExist}
	}
	historyDirectStoreOpen = func() (*store.Store, error) { return s, nil }
	var directJSON bytes.Buffer
	if err := historyCoverage(&directJSON, []string{"--root", "/proj", "--json"}); err != nil {
		t.Fatalf("direct coverage: %v", err)
	}
	if sockOut.String() != directJSON.String() {
		t.Errorf("the two doors' --json bytes differ:\nsocket: %s\ndirect: %s", sockOut.String(), directJSON.String())
	}
	// The fixture really exercised cannot-say.
	var v history.CoverageView
	if err := json.Unmarshal(directJSON.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	if v.Undecodable.Count != 1 {
		t.Fatalf("the fixture broke: undecodable = %d, want 1", v.Undecodable.Count)
	}
}

func TestHistoryVocabularyDoorsAnswerIdenticalBytes(t *testing.T) {
	s := seedHistoryParityStore(t)
	standHistorySocket(t, s)
	var sockOut bytes.Buffer
	if err := historyVocabulary(&sockOut, nil); err != nil {
		t.Fatalf("socket vocabulary: %v", err)
	}
	noDaemon := historyDial
	historyDial = func(int) (*protocol.Client, error) {
		return nil, &noDaemonError{path: "/tmp/none.sock", cause: os.ErrNotExist}
	}
	t.Cleanup(func() { historyDial = noDaemon })
	var directOut bytes.Buffer
	if err := historyVocabulary(&directOut, nil); err != nil {
		t.Fatalf("direct vocabulary: %v", err)
	}
	if sockOut.String() != directOut.String() {
		t.Errorf("the two doors' vocabulary bytes differ:\nsocket: %s\ndirect: %s", sockOut.String(), directOut.String())
	}
	// And it is the plane's own function verbatim — lanes, outcomes, filters.
	v := history.Vocabulary()
	for _, word := range append(append(v.Lanes, v.Outcomes...), v.Filters...) {
		if !strings.Contains(directOut.String(), word) {
			t.Errorf("the printer lost the word %q", word)
		}
	}
}

func TestHistoryBothRootsRefuseTheSameSentenceBothDoors(t *testing.T) {
	s := seedHistoryParityStore(t)
	standHistorySocket(t, s)
	sockErr := historyCoverage(&bytes.Buffer{}, []string{"--root", "/a", "--component", "/b"})
	if sockErr == nil || !strings.Contains(sockErr.Error(), "set one, not both") {
		t.Fatalf("the socket road did not refuse both-roots verbatim: %v", sockErr)
	}
	noDaemon := historyDial
	historyDial = func(int) (*protocol.Client, error) {
		return nil, &noDaemonError{path: "/tmp/none.sock", cause: os.ErrNotExist}
	}
	t.Cleanup(func() { historyDial = noDaemon })
	restoreOpen := historyDirectStoreOpen
	historyDirectStoreOpen = func() (*store.Store, error) { return s, nil }
	t.Cleanup(func() { historyDirectStoreOpen = restoreOpen })
	directErr := historyCoverage(&bytes.Buffer{}, []string{"--root", "/a", "--component", "/b"})
	if directErr == nil || directErr.Error() != sockErr.Error() {
		t.Errorf("the roads refuse differently:\nsocket: %v\ndirect: %v", sockErr, directErr)
	}
}

func TestHistoryDoorSelectionRefusesOnADialFault(t *testing.T) {
	fault := errors.New("dial: connection reset by peer")
	restore := historyDial
	t.Cleanup(func() { historyDial = restore })
	historyDial = func(int) (*protocol.Client, error) { return nil, fault }
	var out bytes.Buffer
	err := historyCoverage(&out, nil)
	if !errors.Is(err, fault) {
		t.Fatalf("a dial fault that is not \"no daemon\" must travel as itself, got %v", err)
	}
	if out.Len() != 0 {
		t.Errorf("the refused verb printed %q anyway", out.String())
	}
}

func TestHistoryOfflineArmPrintsTheNoticeFirst(t *testing.T) {
	// The arm CLOSES the store it is handed (the direct door owns its open),
	// so verification reads the same dir through a fresh handle — the
	// self-consistency check is view-vs-lines, both from this one seed.
	dir := t.TempDir()
	s := seedHistoryParityStoreIn(t, dir)
	restoreDial := historyDial
	t.Cleanup(func() { historyDial = restoreDial })
	historyDial = func(int) (*protocol.Client, error) {
		return nil, &noDaemonError{path: "/tmp/none.sock", cause: os.ErrNotExist}
	}
	restoreOpen := historyDirectStoreOpen
	t.Cleanup(func() { historyDirectStoreOpen = restoreOpen })
	historyDirectStoreOpen = func() (*store.Store, error) { return s, nil }
	restoreErr := historyErrOut
	t.Cleanup(func() { historyErrOut = restoreErr })
	var errOut bytes.Buffer
	historyErrOut = &errOut
	var out bytes.Buffer
	if err := historyCoverage(&out, []string{"--root", "/proj"}); err != nil {
		t.Fatal(err)
	}
	if errOut.String() != historyDirectNotice+"\n" {
		t.Errorf("notice = %q, want the fixed line", errOut.String())
	}
	// The three states as three words, and the floor statement verbatim.
	lines := out.String()
	for _, word := range []string{"shown       live:", "shown       archived:", "cannot say  1 archive(s)", "floor       "} {
		if !strings.Contains(lines, word) {
			t.Errorf("the printer lost %q:\n%s", word, lines)
		}
	}
	reopened, err := store.Open(store.Options{Dir: dir, Env: func(string) string { return "" }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	view, err := protocol.AnswerHistoryCoverage(history.NewStore(reopened), protocol.HistoryCoverageParams{ProjectRoot: "/proj"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(lines, view.Floor.Statement) {
		t.Errorf("the floor statement did not ride verbatim:\n%s", lines)
	}
	if strings.Contains(lines, "not shown   none") && len(view.NotShown) != 0 {
		t.Error("the printer said \"not shown none\" while the view names reasons")
	}
	// The not-shown word on the lane where S25's shape produces it: the
	// printer must print reason rows as `not shown  N — reason`, exercised
	// straight off a view (no flag reaches coverage's lane today; the
	// printer is the unit here).
	withNS := view
	withNS.NotShown = []history.NotShown{{Reason: history.NotShownArchivedSteps, Count: 3}}
	var nsOut bytes.Buffer
	renderHistoryCoverage(&nsOut, withNS)
	if !strings.Contains(nsOut.String(), "not shown   3 — "+history.NotShownArchivedSteps) {
		t.Errorf("the not-shown row did not print:\n%s", nsOut.String())
	}
}

// TestHistoryClientCopiesNoLogic — the roadmap T6 shape, re-pointed: the
// direct door's body is the open, the shared answer, and the verbatim map;
// no SQL word, no lane switch, no count arithmetic lives in this file.
func TestHistoryClientCopiesNoLogic(t *testing.T) {
	src, err := os.ReadFile("history_client.go")
	if err != nil {
		t.Fatal(err)
	}
	s := string(src)
	for _, forbidden := range []string{"SELECT", "COUNT(", "GROUP BY", "Merge(", "Query("} {
		if strings.Contains(s, forbidden) {
			t.Errorf("history_client.go contains %q — the CLI recomputes nothing (T4's do-not list)", forbidden)
		}
	}
	for _, required := range []string{"openStoreDirectly(false)", "protocol.AnswerHistoryCoverage", "history.Vocabulary()", "historyVerbatim(err)"} {
		if !strings.Contains(s, required) {
			t.Errorf("history_client.go lost %q — the direct arm must be a call into the daemon's own answer, not a re-implementation", required)
		}
	}
	var _ historyDoor = historyDirectDoor{} // the direct arm satisfies the door, by the compiler and not by comment
}
