package main

// agent_rows_test.go — S73 T2: the DOOR half of the paging convention, tested
// through the one seam `agent.rows` is registered on (Table.AgentManage →
// agentManageHandleFor), against a real migrated store.
//
// # Why a door test beside the store's twelve
//
// internal/store/agent_page_test.go pins the window: the clamp, the filter,
// every sort, the tiebreak, the escape. None of that is what breaks in a door —
// a door breaks on its SHAPE. The seven keys a client reads, the nine keys a row
// carries, which refusals travel as protocol.ErrBadParams (and so reach the wire
// as −32602) versus which travel as a refusal sentence, and above all T3's
// forward-compatibility: a request carrying a key nobody thought of must
// SUCCEED. A store test cannot answer any of those, and a fixture states a
// contract without enforcing one.
//
// # The rows arrive through the family's own door
//
// `agent.create` lays each test row, so the window is read through the same
// seam that wrote it — the test never reaches into the store to arrange state,
// and it keeps working when T3 puts an actor on these writes (the seam already
// takes a ctx, and T4's channel rides in it).

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/daemon"
	"github.com/cjlapao/aeon/internal/protocol"
)

// createAgentThroughTheDoor lays one agent row through the management seam —
// the road a person's "Add agent" button will take from T6 on.
//
// S73 T3 put an actor on the six writes (AR2/AR3), and over the socket the
// protocol stamps it from the peer before the seam runs (agentManageCall).
// This test drives the seam directly — there is no connection here — so it
// stamps what the table would have stamped: a person, not a machine. The
// READS this file also drives (agent.rows) carry no actor at all, which is
// itself the point of SP6, so only this helper carries the stamp.
func createAgentThroughTheDoor(t *testing.T, h func(context.Context, string, json.RawMessage) (any, string, error), name string) {
	t.Helper()
	params, err := json.Marshal(map[string]string{"name": name, "description": "planted for S73 T2", "functionality": "read"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := protocol.WithAgentActor(context.Background(), "user", "1000")
	res, refusal, err := h(ctx, "agent.create", params)
	if err != nil || refusal != "" || res == nil {
		t.Fatalf("agent.create %q: res=%v refusal=%q err=%v", name, res, refusal, err)
	}
}

// dispatchRows calls the door and returns the result as JSON, because the
// contract IS JSON: a Go field nobody marshals is not on the wire.
func dispatchRows(t *testing.T, h func(context.Context, string, json.RawMessage) (any, string, error), params string) map[string]any {
	t.Helper()
	res, refusal, err := h(context.Background(), "agent.rows", json.RawMessage(params))
	if err != nil {
		t.Fatalf("agent.rows(%s) failed: %v", params, err)
	}
	if refusal != "" {
		t.Fatalf("agent.rows(%s) refused: %s", params, refusal)
	}
	raw, err := json.Marshal(res)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("agent.rows' answer is not a JSON object: %v", err)
	}
	return out
}

// jsonKeys names an object's keys, sorted, for a comparison that fails legibly.
func jsonKeys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sameJSONKeys(got []string, want ...string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range want {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

// TestAgentRowsDoorAnswersTheSevenKeysAndNineColumns is PG1 at the door: the
// answer is the five request keys as the daemon applied them, plus the filtered
// total, beside the rows — and each row carries exactly the nine columns a
// management table may hold. `prompt`, `skills` and `metadata` are absent, and
// their absence is asserted, because AR5's rule is a content rule and a rule
// nobody checks is a rule a later change forgets.
func TestAgentRowsDoorAnswersTheSevenKeysAndNineColumns(t *testing.T) {
	agentHome(t)
	s := openAgentStore(t)
	h := agentManageHandleFor(s, &daemon.Options{})
	for _, n := range []string{"S73 Alpha", "S73 Bravo", "S73 Charlie"} {
		createAgentThroughTheDoor(t, h, n)
	}

	got := dispatchRows(t, h, `{"q":"S73 ","pageSize":2,"page":1,"sort":"name","dir":"asc"}`)
	if !sameJSONKeys(jsonKeys(got), "agents", "dir", "page", "pageSize", "q", "sort", "total") {
		t.Fatalf("the wire keys are %v — agent.rows answers exactly agents, page, pageSize, total, sort, dir and q", jsonKeys(got))
	}
	if got["page"].(float64) != 1 || got["pageSize"].(float64) != 2 {
		t.Errorf("the answer echoes the applied window, not the request: page=%v pageSize=%v", got["page"], got["pageSize"])
	}
	if got["total"].(float64) != 3 {
		t.Errorf("total counts the FILTERED set (PG3): got %v, want 3 of the store's rows", got["total"])
	}
	// The echo is the term AS APPLIED, so the search's trim shows here: the
	// caller sent "S73 " and the daemon answered "S73". An echo of the raw
	// request would let the response disagree with the rows it describes.
	if got["q"].(string) != "S73" {
		t.Errorf("q echoes the applied term (trimmed): got %q, want \"S73\"", got["q"])
	}
	rows, _ := got["agents"].([]any)
	if len(rows) != 2 {
		t.Fatalf("pageSize 2 answers 2 rows on page 1, got %d", len(rows))
	}
	first := rows[0].(map[string]any)
	if first["name"].(string) != "S73 Alpha" {
		t.Errorf("sort=name,dir=asc leads with %q, not S73 Alpha", first["name"])
	}
	if !sameJSONKeys(jsonKeys(first), "createdAt", "description", "internal", "name", "origin", "signed", "slug", "updatedAt", "version") {
		t.Fatalf("a row carries %v — the nine columns and nothing else", jsonKeys(first))
	}
	for _, leak := range []string{"prompt", "skills", "metadata", "functionality", "avatarColor", "signature", "certificate"} {
		if _, sent := first[leak]; sent {
			t.Errorf("the window carries %q, which AR5 keeps off the list (and agent.edit would then blank)", leak)
		}
	}

	// Page 2 is the tail of the same filter, not a fresh unfiltered window.
	page2 := dispatchRows(t, h, `{"q":"S73 ","pageSize":2,"page":2,"sort":"name","dir":"asc"}`)
	rows2, _ := page2["agents"].([]any)
	if len(rows2) != 1 || rows2[0].(map[string]any)["name"].(string) != "S73 Charlie" {
		t.Fatalf("page 2 of the filtered set is %v, want the one row the filter leaves", rows2)
	}
	if page2["total"].(float64) != 3 {
		t.Errorf("page 2 still reports the filtered total 3, got %v", page2["total"])
	}
}

// TestAgentRowsDoorIgnoresAKeyItHasNeverHeardOf is conformance/README.md's T3
// walked at the door that most tempts a strict decoder: an unknown key in the
// params must be IGNORED, so a client written after this daemon still works
// against it. DisallowUnknownFields here would turn every additive change into
// a breaking one — which is why the closed refused set below is a named list
// and not a general policy.
func TestAgentRowsDoorIgnoresAKeyItHasNeverHeardOf(t *testing.T) {
	agentHome(t)
	s := openAgentStore(t)
	h := agentManageHandleFor(s, &daemon.Options{})
	got := dispatchRows(t, h, `{"page":1,"pageSize":3,"pageFromTheFuture":true,"sortDirection":"newest"}`)
	if got["pageSize"].(float64) != 3 {
		t.Fatalf("a request carrying a future key must still be answered on its known keys: %v", got)
	}
}

// TestAgentRowsDoorRefusesTheClosedVocabulary is S73's deviation 9 with teeth:
// seven keys whose silence would be a lie are refused BY NAME, each naming the
// alternative, and every refusal is protocol.ErrBadParams so the wire voice is
// −32602 (the same voice agent_mailbox.go:551 uses) rather than −32603.
func TestAgentRowsDoorRefusesTheClosedVocabulary(t *testing.T) {
	agentHome(t)
	s := openAgentStore(t)
	h := agentManageHandleFor(s, &daemon.Options{})
	for _, key := range []string{"limit", "cursor", "offset", "order", "search", "actor", "actorKind"} {
		_, refusal, err := h(context.Background(), "agent.rows", json.RawMessage(`{"`+key+`":1}`))
		if err == nil {
			t.Errorf("%q answered instead of refusing — a caller that sent it believes it asked a question the daemon never did", key)
			continue
		}
		if refusal != "" {
			t.Errorf("%q came back as a domain refusal %q; a shape error is ErrBadParams, not ErrAgentRefused", key, refusal)
		}
		var bp protocol.ErrBadParams
		if !errors.As(err, &bp) {
			t.Errorf("%q refused with %T (%v), not protocol.ErrBadParams — the wire voice would be −32603", key, err, err)
			continue
		}
		msg := err.Error()
		if !strings.Contains(msg, `"`+key+`"`) {
			t.Errorf("%q refusal does not name the key: %s", key, msg)
		}
		if !strings.Contains(msg, "page") || !strings.Contains(msg, "pageSize") {
			t.Errorf("%q refusal names no alternative to fix it by: %s", key, msg)
		}
	}
}

// TestAgentRowsDoorAnswersTheDefaultsForEveryEmptyRequest: `{}`, `null`, and no
// params block at all are three documents meaning "the first page, as big as a
// table asks by default". A paging read that answered page 0 or pageSize 0 for
// one of them would be a read whose default is a bug.
func TestAgentRowsDoorAnswersTheDefaultsForEveryEmptyRequest(t *testing.T) {
	agentHome(t)
	s := openAgentStore(t)
	h := agentManageHandleFor(s, &daemon.Options{})
	for name, params := range map[string]string{
		"absent":       "",
		"null":         "null",
		"empty object": "{}",
	} {
		res, refusal, err := h(context.Background(), "agent.rows", json.RawMessage(params))
		if err != nil || refusal != "" || res == nil {
			t.Fatalf("%s params: res=%v refusal=%q err=%v", name, res, refusal, err)
		}
		raw, _ := json.Marshal(res)
		var got struct {
			Page     int    `json:"page"`
			PageSize int    `json:"pageSize"`
			Sort     string `json:"sort"`
			Dir      string `json:"dir"`
			Q        string `json:"q"`
			Agents   []any  `json:"agents"`
		}
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatalf("%s params: %v", name, err)
		}
		if got.Page != 1 || got.PageSize != 20 || got.Sort != "name" || got.Dir != "asc" || got.Q != "" {
			t.Errorf("%s params answered page=%d pageSize=%d sort=%q dir=%q q=%q, want the bounds' defaults",
				name, got.Page, got.PageSize, got.Sort, got.Dir, got.Q)
		}
		if got.Agents == nil {
			t.Errorf("%s params answered `agents: null`; an empty window is [] (PG3), and null is a client's crash", name)
		}
	}
}

// TestAgentRowsDoorRefusesItsOwnPagingTypos covers the two refusals the door
// makes from the request rather than from the closed set: a sort it has never
// published and a `dir` that is not a direction. Both name the value, both are
// ErrBadParams, and neither touched the database — the store test already pins
// that a bad sort leaves the table alone; here it is the voice that matters.
func TestAgentRowsDoorRefusesItsOwnPagingTypos(t *testing.T) {
	agentHome(t)
	s := openAgentStore(t)
	h := agentManageHandleFor(s, &daemon.Options{})
	for params, want := range map[string]string{
		`{"sort":"colour"}`:                        "colour",
		`{"sort":"name","dir":"newest"}`:           "newest",
		`{"q":"` + strings.Repeat("x", 201) + `"}`: "200",
	} {
		_, refusal, err := h(context.Background(), "agent.rows", json.RawMessage(params))
		if err == nil || refusal != "" {
			t.Errorf("%s: refusal=%q err=%v, want an ErrBadParams with no domain refusal", params, refusal, err)
			continue
		}
		var bp protocol.ErrBadParams
		if !errors.As(err, &bp) {
			t.Errorf("%s refused with %T (%v), not protocol.ErrBadParams", params, err, err)
			continue
		}
		if !strings.Contains(err.Error(), want) {
			t.Errorf("%s refusal does not name %q: %s", params, want, err)
		}
	}
}

// TestAgentRowsDoorClampsAHugePageSizeAndEchoesTheTruth is PG2 met at the door:
// a caller asking for 500 rows gets the bounds' 100 and is TOLD so by the
// answer's own echo. The arithmetic is pinned in internal/paging and the LIMIT
// is pinned in the store; what neither can pin is that the door runs the one and
// then hands the clamped number to the other and echoes it — a door that
// echoed the request would report a 500-row page it did not fetch.
func TestAgentRowsDoorClampsAHugePageSizeAndEchoesTheTruth(t *testing.T) {
	agentHome(t)
	s := openAgentStore(t)
	h := agentManageHandleFor(s, &daemon.Options{})
	got := dispatchRows(t, h, `{"pageSize":500,"page":1}`)
	if got["pageSize"].(float64) != 100 {
		t.Fatalf("pageSize 500 answered %v; AgentRowBounds clamps at 100 and the answer must say so", got["pageSize"])
	}
	rows, _ := got["agents"].([]any)
	if len(rows) > 100 {
		t.Fatalf("the answer carries %d rows behind a 100-row cap", len(rows))
	}
	if got["total"].(float64) < float64(len(rows)) {
		t.Fatalf("total %v is below the %d rows the window returned — the count is not the window's length", got["total"], len(rows))
	}
}
