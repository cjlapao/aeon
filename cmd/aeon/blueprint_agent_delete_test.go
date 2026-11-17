// S48 T13 — criterion 7's missing evidence, written while re-running the
// acceptance table.
//
// The spec (and T6's Notes) name `TestAgentDeleteRefusedNamingReferencingBlueprints`.
// T6 shipped the BRANCH (`agentmgr.Manager.Delete`'s blueprint half, one changed
// branch over the derived `blueprint_ref` table) and T1 shipped the QUERY
// (`TestBlueprintRefsForFindsUsers`), and T13's fresh walk found no test that
// ever drove the branch: no test assigned `Manager.BlueprintRefs`, and no test
// asserted the refusal sentence. A branch nobody drives is not evidence, so this
// file is that evidence — S48's own, added in T13, no S43/S46 file edited.
package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/agentmgr"
	"github.com/cjlapao/aeon/internal/blueprint"
	"github.com/cjlapao/aeon/internal/store"
)

// The one write path, with the refs the write path itself would derive.
func putBlueprintNamingAgent(t *testing.T, st *store.Store, name, agent, content string) {
	t.Helper()
	ctx := context.Background()
	sum := sha256.Sum256([]byte(content))
	hash := hex.EncodeToString(sum[:])
	bp, err := blueprint.Load([]byte(content))
	if err != nil {
		t.Fatalf("the fixture YAML must parse: %v", err)
	}
	var refs []store.RefRow
	for _, r := range blueprint.References(&bp) {
		refs = append(refs, store.RefRow{Name: name, Kind: r.Kind, Target: r.Target})
	}
	row := store.BlueprintRow{Name: name, Version: 4, Content: content, ContentHash: hash, Origin: "user", UpdatedBy: "user:operator"}
	rev := store.RevisionRow{Name: name, Version: 4, Content: content, ContentHash: hash, Op: "save", ActorKind: "user", ActorID: "operator"}
	if err := st.BlueprintWithTx(ctx, func(tx *sql.Tx) error {
		return store.BlueprintPutTx(tx, row, rev, refs)
	}); err != nil {
		t.Fatalf("put %s: %v", name, err)
	}
}

func TestAgentDeleteRefusedNamingReferencingBlueprints(t *testing.T) {
	_, st := writeFixture(t)
	ctx := context.Background()
	slug := store.Slugify("golang-engineer")
	named := "id: uses-golang\nschemaVersion: 1\nversion: 4\nlanes:\n  - id: survey\n    step: {kind: model-step, agent: golang-engineer}\n"
	quiet := "id: uses-golang\nschemaVersion: 1\nversion: 5\nlanes:\n  - id: survey\n    step: {kind: gate, question: q}\nquestions:\n  q:\n    prompt: q?\n    answers: [yes]\n"
	putBlueprintNamingAgent(t, st, "uses-golang", slug, named)

	// The derived table is what answers, not a text scan: the query behind the
	// seam sees the row that names the agent.
	if got, err := st.BlueprintRefsFor(ctx, "agent", slug); err != nil || len(got) != 1 || got[0] != "uses-golang" {
		t.Fatalf("BlueprintRefsFor(agent, %s) = %v, %v — want [uses-golang]", slug, got, err)
	}

	mgr := agentmgr.NewManager(st, nil, func(target string) ([]string, error) {
		return st.BlueprintRefsFor(ctx, "agent", target)
	}, nil)

	err := mgr.Delete(ctx, store.AgentActor{Kind: store.AgentActorUser, ID: "1000"}, slug)
	if err == nil {
		t.Fatal("the delete of an agent a blueprint still names SUCCEEDED — the orphan BP11/BP10 exist to prevent")
	}
	msg := err.Error()
	for _, want := range []string{"uses-golang", "blueprint", slug} {
		if !strings.Contains(msg, want) {
			t.Errorf("the refusal must name %q — an unnamed refusal sends the operator hunting: %s", want, msg)
		}
	}
	// Nothing was deleted on the refusal: not the agent, not the blueprint.
	if _, found, ferr := st.AgentBySlug(slug); ferr != nil || !found {
		t.Fatalf("the refused delete removed the agent: found=%v err=%v", found, ferr)
	}
	if _, found, berr := st.BlueprintGet(ctx, "uses-golang"); berr != nil || !found {
		t.Fatalf("the refused delete touched the blueprint: found=%v err=%v", found, berr)
	}

	// The refusal IS the reference: rewrite the blueprint so it no longer names
	// the agent (BP11 — the derived refs are rebuilt from current content on
	// every write) and the same delete walks. The operator's path is open
	// without touching the agent table at all.
	sum := sha256.Sum256([]byte(quiet))
	bp, perr := blueprint.Load([]byte(quiet))
	if perr != nil {
		t.Fatalf("quiet YAML: %v", perr)
	}
	var refs []store.RefRow
	for _, r := range blueprint.References(&bp) {
		refs = append(refs, store.RefRow{Name: "uses-golang", Kind: r.Kind, Target: r.Target})
	}
	if err := st.BlueprintWithTx(ctx, func(tx *sql.Tx) error {
		return store.BlueprintPutTx(tx,
			store.BlueprintRow{Name: "uses-golang", Version: 5, Content: quiet, ContentHash: hex.EncodeToString(sum[:]), Origin: "user", UpdatedBy: "user:operator"},
			store.RevisionRow{Name: "uses-golang", Version: 5, Content: quiet, ContentHash: hex.EncodeToString(sum[:]), Op: "save", ActorKind: "user", ActorID: "operator"},
			refs)
	}); err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	if got, _ := st.BlueprintRefsFor(ctx, "agent", slug); len(got) != 0 {
		t.Fatalf("the refs were not rebuilt from the new content: %v", got)
	}
	if err := mgr.Delete(ctx, store.AgentActor{Kind: store.AgentActorUser, ID: "1000"}, slug); err != nil {
		t.Fatalf("with no blueprint naming it, the delete must walk: %v", err)
	}
}

// The same delete, asked of a manager whose blueprint seam is unwired, is not a
// silent pass over blueprints: it says nothing about them because it cannot ask.
// The daemon always wires the accessor (T6); this pins which half is whose, so a
// wiring regression is visible as a behaviour change rather than as a mystery.
func TestAgentDeleteWithoutTheBlueprintSeamNamesNoBlueprint(t *testing.T) {
	_, st := writeFixture(t)
	ctx := context.Background()
	slug := store.Slugify("manny")
	named := "id: uses-manny\nschemaVersion: 1\nversion: 4\nlanes:\n  - id: survey\n    step: {kind: model-step, agent: manny}\n"
	putBlueprintNamingAgent(t, st, "uses-manny", slug, named)

	unwired := agentmgr.NewManager(st, nil, nil, nil)
	if err := unwired.Delete(ctx, store.AgentActor{Kind: store.AgentActorUser, ID: "1000"}, slug); err != nil {
		t.Fatalf("an unwired seam must not invent a refusal: %v", err)
	}
	wired := agentmgr.NewManager(st, nil, func(target string) ([]string, error) {
		return st.BlueprintRefsFor(ctx, "agent", target)
	}, nil)
	// (the agent is gone above; the wired manager's answer for the same slug is
	// the row-less refusal, which is S43's own sentence, not the blueprint half)
	err := wired.Delete(ctx, store.AgentActor{Kind: store.AgentActorUser, ID: "1000"}, slug)
	if err == nil || strings.Contains(err.Error(), "uses-manny") {
		t.Fatalf("expected S43's own no-row refusal, got: %v", err)
	}
}
