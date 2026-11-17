package main

// agent_changes_test.go — S73 T5: the ledger's DOOR, tested through the one
// seam `agent.changes` is registered on, against a real migrated store.
//
// # What this file owns that the store test does not
//
// internal/store/agent_changes_test.go pins the window: newest-first, the
// filter, the honest total, the NULL slug. A window cannot fail in the ways a
// door fails, and those are the interesting ones — the five keys of the answer,
// the seven keys of a row, `slug` arriving as JSON `null` rather than as an
// absent key or an empty string, which refusals travel as ErrBadParams (and so
// reach the wire as −32602) rather than as a domain refusal, and T3's
// forward-compatibility: a request carrying a key nobody thought of must
// SUCCEED while the ten keys this read cannot honour are refused by name.
//
// # The rows arrive through the roads that really make them
//
// `agent.create` (a person at the socket, via the shared helper from
// agent_rows_test.go), the seed plane's writer (the engine's own stamp), and a
// `migrate` run (the one row with no slug). No test here hand-writes an
// `agent_change` row: a ledger row this file invented would prove the SELECT
// and nothing about whether the family's own writes are findable through the
// door — which is the point of T3 and T5 meeting.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/daemon"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

// dispatchChanges calls the door and returns its answer as JSON, because the
// contract IS JSON: a Go field nobody marshals is not on the wire, and a
// `*string` that marshals as `""` instead of `null` is a type the app will get
// wrong.
func dispatchChanges(t *testing.T, h doorHandle, params string) map[string]any {
	t.Helper()
	res, refusal, err := h(context.Background(), "agent.changes", json.RawMessage(params))
	if err != nil {
		t.Fatalf("agent.changes(%s) failed: %v", params, err)
	}
	if refusal != "" {
		t.Fatalf("agent.changes(%s) refused: %s", params, refusal)
	}
	raw, err := json.Marshal(res)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("agent.changes' answer is not a JSON object: %v", err)
	}
	return out
}

// changesThroughTheDoor is the row list of an answer, as JSON objects.
func changesThroughTheDoor(t *testing.T, h doorHandle, params string) []map[string]any {
	t.Helper()
	got := dispatchChanges(t, h, params)
	raw, err := json.Marshal(got["changes"])
	if err != nil {
		t.Fatal(err)
	}
	var out []map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("agent.changes' `changes` is not an array of objects: %s", raw)
	}
	return out
}

// laySeedRowThroughTheSeedWriter writes one system agent with the seed plane's
// OWN writer and the engine's own stamp, so the ledger row under test is the
// one T4's applier mints on a real apply — not a copy of it.
func laySeedRowThroughTheSeedWriter(t *testing.T, s *store.Store, slug string) {
	t.Helper()
	c := store.AgentContent{
		Slug: slug, Name: "Seeded " + slug, Description: "laid by the seed writer",
		BodyHash: "sha256:" + slug, Origin: "system",
	}
	err := s.AgentWithTx(context.Background(), func(tx *sql.Tx) error {
		return s.SeedAgentInsert(context.Background(), tx, c,
			store.AgentActor{Kind: store.AgentActorSystem, ID: "seed-engine"}, "2026-11-06T09:00:00Z")
	})
	if err != nil {
		t.Fatalf("seed %s: %v", slug, err)
	}
}

// TestAgentChangesDoorAnswersFiveKeysAndSevenColumns is AR6 at the wire: the
// answer is the window plus the two paging keys as the daemon applied them, the
// filtered total and the echoed slug — five keys, no more — and each row is the
// ledger's seven columns. The row's `slug` is asserted PRESENT as a key and
// NULL as a value on the migrate run's row: `null` and `""` and an absent key
// are three different documents, and the app's type must be `string | null`
// because of the first one.
func TestAgentChangesDoorAnswersFiveKeysAndSevenColumns(t *testing.T) {
	agentHome(t)
	s := openAgentStore(t)
	h := agentManageHandleFor(s, &daemon.Options{})
	createAgentThroughTheDoor(t, h, "S73 Window One")

	got := dispatchChanges(t, h, `{"page":1,"pageSize":20}`)
	if !sameJSONKeys(jsonKeys(got), "changes", "page", "pageSize", "slug", "total") {
		t.Fatalf("the wire keys are %v — agent.changes answers exactly changes, page, pageSize, total and slug", jsonKeys(got))
	}
	rows := changesThroughTheDoor(t, h, `{"page":1,"pageSize":20}`)
	if len(rows) != 1 {
		t.Fatalf("one create answered %d rows: %v", len(rows), rows)
	}
	if !sameJSONKeys(jsonKeys(rows[0]), "actorId", "actorKind", "id", "occurredAt", "op", "slug", "summary") {
		t.Fatalf("a ledger row carries %v — the seven columns AR1's table has", jsonKeys(rows[0]))
	}

	// The run with no name to give.
	if _, err := s.MigrateFirstPartyAgentsAudited(context.Background(),
		store.AgentActor{Kind: store.AgentActorCLI, ID: "cli"}, "2026-11-06T10:00:00Z"); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	rows = changesThroughTheDoor(t, h, `{}`)
	if len(rows) != 2 {
		t.Fatalf("a create and a run answered %d rows", len(rows))
	}
	run := rows[0]
	if run["op"] != "migrate" {
		t.Fatalf("the newest row is %v, want the run", run)
	}
	slugVal, present := run["slug"]
	if !present {
		t.Fatal("the run's row has no `slug` key at all; AR1's NULL must reach the wire as null, not as an absent key")
	}
	if slugVal != nil {
		t.Fatalf("the run's `slug` is %#v, want JSON null", slugVal)
	}
}

// TestAgentChangesDoorRefusesItsClosedVocabulary is the ten keys whose silence
// would be a lie, each refused NAMING itself and offering the alternative, and
// each as ErrBadParams — because a shape error that arrives as a domain refusal
// would reach the wire as ErrAgentRefused and teach the caller its request was
// a policy problem.
func TestAgentChangesDoorRefusesItsClosedVocabulary(t *testing.T) {
	agentHome(t)
	s := openAgentStore(t)
	h := agentManageHandleFor(s, &daemon.Options{})
	for _, key := range []string{"limit", "cursor", "offset", "order", "search", "actor", "actorKind", "sort", "dir", "q"} {
		_, refusal, err := h(context.Background(), "agent.changes", json.RawMessage(`{"`+key+`":1}`))
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

// TestAgentChangesDoorIgnoresAKeysItHasNeverHeardOf is conformance/README.md's
// T3, re-pinned on the second paged read: refusing a closed set is NOT
// unknown-key rejection, and a client of the future that adds a key to this
// request must succeed. A door that used DisallowUnknownFields would turn every
// additive change to this protocol into a breaking one.
func TestAgentChangesDoorIgnoresAKeysItHasNeverHeardOf(t *testing.T) {
	agentHome(t)
	s := openAgentStore(t)
	h := agentManageHandleFor(s, &daemon.Options{})
	createAgentThroughTheDoor(t, h, "S73 Window Two")
	for _, params := range []string{
		`{"pageFromTheFuture":3}`,
		`{"page":1,"reason":"the tab opened"}`,
		`{"slugFilter":"someone-elses-idea"}`,
	} {
		got := dispatchChanges(t, h, params)
		if got["page"].(float64) != 1 || got["pageSize"].(float64) != 20 {
			t.Fatalf("%s did not answer the defaults it should have: %v", params, got)
		}
	}
}

// TestAgentChangesDoorAnswersTheDefaultsForEveryEmptyRequest: `{}`, `null`, and
// no params block at all are three documents meaning "the newest page of
// everything". Each must answer page 1 at the default size with an empty-but-
// present window on a fresh store — `changes: null` on a daemon that has never
// written a ledger row is a client-side crash waiting to happen.
func TestAgentChangesDoorAnswersTheDefaultsForEveryEmptyRequest(t *testing.T) {
	agentHome(t)
	s := openAgentStore(t)
	h := agentManageHandleFor(s, &daemon.Options{})
	for name, params := range map[string]string{
		"absent":       "",
		"null":         "null",
		"empty object": "{}",
		"null slug":    `{"slug":null}`,
		"empty slug":   `{"slug":""}`,
	} {
		got := dispatchChanges(t, h, params)
		if got["page"].(float64) != 1 || got["pageSize"].(float64) != 20 {
			t.Errorf("%s params answered page=%v pageSize=%v, want 1 and 20", name, got["page"], got["pageSize"])
		}
		if got["slug"] != "" {
			t.Errorf("%s params echoed slug=%#v; a null or empty slug is \"the whole ledger\", and the echo says so", name, got["slug"])
		}
		if got["changes"] == nil {
			t.Errorf("%s params answered `changes: null`; an empty window is [] (PG3)", name)
		}
		if got["total"].(float64) != 0 {
			t.Errorf("%s params found %v changes on a store that has written nothing", name, got["total"])
		}
	}
}

// TestAgentChangesDoorClampsAHugePageSizeAndEchoesTheTruth is PG2 at this
// door: a caller asking for 500 rows gets the ledger bounds' 100, and the
// answer's own echo says 100. A door that echoed the REQUEST would report a
// 500-row page it never fetched.
func TestAgentChangesDoorClampsAHugePageSizeAndEchoesTheTruth(t *testing.T) {
	agentHome(t)
	s := openAgentStore(t)
	h := agentManageHandleFor(s, &daemon.Options{})
	got := dispatchChanges(t, h, `{"pageSize":500,"page":1}`)
	if got["pageSize"].(float64) != 100 {
		t.Fatalf("pageSize 500 answered %v; AgentChangeBounds clamps at 100 and the answer must say so", got["pageSize"])
	}
}

// TestAgentChangesDoorNarrowsToOneAgentAndPastTheEndAnswersHonestNothing joins
// the two shapes a History tab actually meets: a slug that isolates (with the
// TOTAL narrowing with the window, PG3), and a page past the end that answers
// [] with the whole filtered count still intact.
func TestAgentChangesDoorNarrowsToOneAgentAndPastTheEndAnswersHonestNothing(t *testing.T) {
	agentHome(t)
	s := openAgentStore(t)
	h := agentManageHandleFor(s, &daemon.Options{})
	createAgentThroughTheDoor(t, h, "S73 Mine")
	createAgentThroughTheDoor(t, h, "S73 Yours")

	mine := dispatchChanges(t, h, `{"slug":"s73-mine","pageSize":20}`)
	if mine["total"].(float64) != 1 {
		t.Fatalf("one agent's history counted %v while the ledger holds four rows: %v", mine["total"], mine)
	}
	if mine["slug"] != "s73-mine" {
		t.Fatalf("the answer echoed slug=%#v, want the caller's own back", mine["slug"])
	}
	for _, r := range changesThroughTheDoor(t, h, `{"slug":"s73-mine"}`) {
		if r["slug"] != "s73-mine" {
			t.Fatalf("a filtered window holds another agent's row: %v", r)
		}
	}

	past := dispatchChanges(t, h, `{"page":40,"pageSize":20}`)
	if past["page"].(float64) != 40 {
		t.Fatalf("a past-the-end page moved the caller to page %v; PG3 echoes the page asked for", past["page"])
	}
	if past["total"].(float64) != 2 {
		t.Fatalf("a past-the-end page lost the total: %v, want 2", past["total"])
	}
	if past["changes"] == nil {
		t.Fatal("a past-the-end page answered changes: null, not []")
	}
	if n := len(changesThroughTheDoor(t, h, `{"page":40,"pageSize":20}`)); n != 0 {
		t.Fatalf("page 40 of a two-row ledger holds %d rows", n)
	}
}

// TestAgentChangesDoorShowsThreeRoadsInArrivalOrder puts all three kinds of
// actor on the ledger through their real roads and reads the window back over
// the wire: the seed engine's row, a run's row, and a person's create, newest
// first, each naming who it was. AR4's three identities and AR6's one order,
// proved in one read — this is where T3, T4 and T5 meet at the door.
func TestAgentChangesDoorShowsThreeRoadsInArrivalOrder(t *testing.T) {
	agentHome(t)
	s := openAgentStore(t)
	h := agentManageHandleFor(s, &daemon.Options{})
	createAgentThroughTheDoor(t, h, "S73 Person")
	if _, err := s.MigrateFirstPartyAgentsAudited(context.Background(),
		store.AgentActor{Kind: store.AgentActorCLI, ID: "cli"}, "2026-11-06T10:00:00Z"); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	laySeedRowThroughTheSeedWriter(t, s, "s73-seeded")

	rows := changesThroughTheDoor(t, h, `{"pageSize":20}`)
	if len(rows) != 3 {
		t.Fatalf("three roads answered %d rows: %v", len(rows), rows)
	}
	want := []struct{ op, kind, id string }{
		{"seed", "system", "seed-engine"},
		{"migrate", "cli", "cli"},
		{"create", "user", "1000"},
	}
	for i, w := range want {
		got := rows[i]
		if got["op"] != w.op || got["actorKind"] != w.kind || got["actorId"] != w.id {
			t.Errorf("row %d is op=%v actor=%v:%v, want %s by %s:%s", i, got["op"], got["actorKind"], got["actorId"], w.op, w.kind, w.id)
		}
	}
}

// TestAgentChangesFixtureCarriesTheNullSlug proves the four fixtures against
// the door that answers them — the unfiltered read really does carry a row whose
// `slug` is JSON null, so the app's generated type gets its nullability from the
// contract rather than from a guess about it.
func TestAgentChangesFixtureCarriesTheNullSlug(t *testing.T) {
	agentHome(t)
	s := openAgentStore(t)
	h := agentManageHandleFor(s, &daemon.Options{})
	if _, err := s.MigrateFirstPartyAgentsAudited(context.Background(),
		store.AgentActor{Kind: store.AgentActorUser, ID: "1000"}, "2026-11-06T10:00:00Z"); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	raw, err := json.Marshal(dispatchChanges(t, h, `{"page":1,"pageSize":2}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"slug":null`) {
		t.Fatalf("the unfiltered window marshalled no null slug — the fixture would teach the app a shape the wire never sends: %s", raw)
	}
}
