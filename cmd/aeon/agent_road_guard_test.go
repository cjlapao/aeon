package main

// agent_road_guard_test.go — S73 T4 (AR4, AR3): the source walk that keeps the
// agent content writers down to the roads the slice measured, plus the canary
// that proves the walk has teeth.
//
// # What is being pinned, and why a source walk is the right shape
//
// AR4 says every road that changes an `agent` row names its actor, and a road
// that cannot name one refuses. Measured (T4's Notes, design deviation 15), the
// tree contains SEVEN such roads, not nine: the six socket writes — all six of
// them flowing through the ONE wiring file — and the seed engine. The CLI's
// `aeon agent import` and a committed `agent_import` proposal, which the design
// listed as paths 7 and 8, write the LIFECYCLE table and never touch the content
// table, so they are not roads here at all (their own audit is `plugin_transition`,
// and `agent_actor_test.go` proves both halves).
//
// A guard can prove the small set stays small, which is exactly what AR3 is for.
// It walks the same files the compiler did, skips tests (a test that stamps a
// person by hand is not a new road — it is the test for this road), and refuses
// three things:
//
//	(1) a second file calling an agentmgr write method — the wiring file is one;
//	(2) a production file inventing an identity — an `AgentActor{…}` literal
//	    outside internal/store, where the type is defined, is allowed in exactly
//	    ONE file: the seed engine's own stamp;
//	(3) an agentmgr call whose actor argument is not the named `actor` — the
//	    value the socket road derived from the connection. A placeholder
//	    (`store.AgentActor{}`, a literal, a zero value) is what T4's "Do not" list
//	    forbids, and this predicate is the only thing that notices.
//
// # Why the walk is a floor, not a count
//
// `walked >= 50` is the house rule (S57's gate guard, S46's MCP one before it):
// a walk that saw nothing passes, so the walk must say how much it saw. The
// named exemptions are checked for EXISTENCE too, or an exemption quietly outlives
// the file it was written for.

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// agentMgrWriteMethods are the six domain doors. The guard looks for them ONLY in
// files that import internal/agentmgr, which is what keeps another plane's
// `Create(ctx, …)` — agentsvc's task ledger, for one — from tripping it.
var agentMgrWriteMethods = []string{"Create", "Duplicate", "Edit", "Delete", "Import", "Migrate"}

// agentRoadOwner is the one production file allowed to call an agentmgr write
// method, and agentActorOwners are the files allowed to name an actor in code.
// The seed engine is the only one that names a MACHINE; everything else reads a
// person off a connection.
const (
	agentRoadOwner = "cmd/aeon/agent_management.go"
	agentSeedRoad  = "internal/agentseed/applier.go"
)

// agentActorCallRe finds `<receiver>.<Method>(ctx, <second arg>` — enough to see
// what the actor argument IS, which is the thing AR3 forbids guessing.
var agentActorCallRe = regexp.MustCompile(`\.(` + "Create|Duplicate|Edit|Delete|Import|Migrate" + `)\(\s*ctx\s*,\s*([^,)]+)`)

// agentActorLiteralRe finds a NON-EMPTY `AgentActor{…}` literal, with or without
// the package qualifier (a file inside internal/store writes it unqualified). The
// empty form is deliberately not matched: `store.AgentActor{}` returned beside a
// refusal error is Go saying "no value", not a value anybody is about to write.
var agentActorLiteralRe = regexp.MustCompile(`([a-z]+\.)?AgentActor\{[^}]+\}`)

// agentMachineMigrateNeedle is the stamp deviation 3 refuses: `system:migrate`
// on a run a person pressed. It is built by concatenation, and only so that this
// slice's own Definition-of-done grep — that two-word quoted pair, searched
// across internal and cmd, which must answer NOTHING — stays clean over a tree
// that contains this guard. The value compared is the same string; the source
// line is not. A guard that made its own DoD grep ambiguous is a guard the next
// reader has to re-litigate, so the pair appears literally exactly once in the
// tree: in the task record that asks for it.
var agentMachineMigrateNeedle = `"system",` + ` "migrate"`

// agentQuotedIdentityRe finds a literal whose kind or id is a STRING LITERAL —
// the shape of an identity invented in code rather than read from a connection.
var agentQuotedIdentityRe = regexp.MustCompile(`(Kind|ID):\s*"`)

// agentRoadOffenses is the pure predicate the walk runs, split out so the canary
// can hand it a rogue file without touching the repository.
func agentRoadOffenses(rel, src string) []string {
	var out []string
	importsAgentMgr := strings.Contains(src, `"github.com/cjlapao/aeon/internal/agentmgr"`)
	if importsAgentMgr {
		for _, m := range agentMgrWriteMethods {
			if !strings.Contains(src, "."+m+"(ctx,") && !strings.Contains(src, "."+m+"(ctx ,") {
				continue
			}
			if rel != agentRoadOwner {
				out = append(out, "calls agentmgr's "+m+" — the wiring file is the one road onto the content writers")
				continue
			}
			// The one legal caller still has to pass a NAMED actor.
			for _, g := range agentActorCallRe.FindAllStringSubmatch(src, -1) {
				if g[1] != m {
					continue
				}
				arg := strings.TrimSpace(g[2])
				if arg != "actor" {
					out = append(out, m+" passes "+arg+" — the actor is the peer read from the connection, never a literal or a zero value")
				}
			}
		}
	}
	if strings.HasPrefix(rel, "internal/store/") {
		return out // the package that DEFINES AgentActor may of course build one
	}
	lits := agentActorLiteralRe.FindAllString(src, -1)
	switch rel {
	case agentSeedRoad:
		// The engine is the one declared machine identity in the family, and it
		// gets exactly one literal — its own stamp, and no others.
		if n := len(lits); n != 1 {
			out = append(out, fmt.Sprintf("builds %d AgentActor literals — the engine gets exactly one, its own", n))
		}
	case agentRoadOwner:
		// The socket road builds ONE literal, from the values it just read off
		// the connection. A quoted kind or id in there is an invented identity
		// wearing the wiring file's coat, which is the exact mistake AR3 exists
		// to keep impossible.
		for _, l := range lits {
			if agentQuotedIdentityRe.MatchString(l) {
				out = append(out, "builds an AgentActor from a string literal ("+l+"…) — the socket road's actor is read from the connection, never typed in")
			}
		}
	default:
		if len(lits) > 0 {
			out = append(out, "builds an AgentActor literal — an identity is read from a connection or declared by the seed engine, never invented here")
		}
	}
	if strings.Contains(src, agentMachineMigrateNeedle) {
		out = append(out, `stamps a migrate run system:migrate — the measurement says a person pressed that button (deviation 3)`)
	}
	return out
}

// TestAgentContentWritesHaveOneRoadEach is the walk over the real tree.
func TestAgentContentWritesHaveOneRoadEach(t *testing.T) {
	var walked int
	seen := map[string]bool{}
	err := filepath.WalkDir("../../", func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // unreadable dirs (dist/, node_modules) hold no Go code we own
		}
		if d.IsDir() {
			switch d.Name() {
			case "dist", "node_modules", "ui", ".git", ".dev", "vendor", ".tmp":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel := filepath.ToSlash(strings.TrimPrefix(filepath.ToSlash(path), "../../"))
		src, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		walked++
		seen[rel] = true
		if bad := agentRoadOffenses(rel, string(src)); len(bad) != 0 {
			t.Errorf("%s: %s — a second road onto an agent content write, or an invented identity (S73 AR3/AR4)",
				rel, strings.Join(bad, "; "))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if walked < 50 {
		t.Fatalf("the walk saw only %d non-test files — it checked nothing (never pass on an empty walk)", walked)
	}

	// The two named files must exist and still carry the needles, or an
	// exemption is hiding a move nobody recorded.
	if !seen[agentRoadOwner] {
		t.Fatalf("%s is gone — the guard's allow-list names a file the walk never saw", agentRoadOwner)
	}
	ownerSrc, err := os.ReadFile(filepath.Join("..", "..", "cmd", "aeon", "agent_management.go"))
	if err != nil {
		t.Fatalf("read the wiring file: %v", err)
	}
	for _, needle := range []string{"protocol.AgentActor(ctx)", "writeActor()", "store.AgentActor"} {
		if !strings.Contains(string(ownerSrc), needle) {
			t.Errorf("the wiring file no longer contains %q — the socket road's actor is derived there, and this guard's contract with it moved", needle)
		}
	}
	seedSrc, err := os.ReadFile(filepath.Join("..", "..", "internal", "agentseed", "applier.go"))
	if err != nil {
		t.Fatalf("read the seed applier: %v", err)
	}
	if !strings.Contains(string(seedSrc), `ID: "seed-engine"`) {
		t.Error(`the seed applier no longer stamps "seed-engine" — path 9's actor moved, and the design table says who it is`)
	}

	// The measured absences, kept: nothing stamps a migrate run as a machine
	// (deviation 3), and the wiring file invents no identity — the two DoD
	// greps of this task, run as an assertion instead of by a person at a
	// prompt, so they fail when they stop being true.
	if strings.Contains(string(ownerSrc), agentMachineMigrateNeedle) {
		t.Error(`the wiring file stamps a migrate run "system","migrate" — the measurement says a person pressed that button (deviation 3)`)
	}
	if agentQuotedIdentityRe.MatchString(strings.Join(agentActorLiteralRe.FindAllString(string(ownerSrc), -1), " ")) {
		t.Error("the wiring file builds an AgentActor from a string literal — the road's actor is read from the connection")
	}
}

// TestAgentRoadGuardHasTeeth feeds the predicate files the tree does not contain.
// A guard with no canary is a rumour (the house line, S46 onward), and this one
// has three separate refusals that all have to fire.
func TestAgentRoadGuardHasTeeth(t *testing.T) {
	rogue := `package main

import (
	"context"

	"github.com/cjlapao/aeon/internal/agentmgr"
	"github.com/cjlapao/aeon/internal/store"
)

func road(ctx context.Context, mgr *agentmgr.Manager) error {
	return mgr.Delete(ctx, store.AgentActor{Kind: "system", ID: "nobody"}, "some-agent")
}
`
	bad := agentRoadOffenses("cmd/aeon/rogue_road.go", rogue)
	var namedRoad, namedActor bool
	for _, b := range bad {
		if strings.Contains(b, "one road onto the content writers") {
			namedRoad = true
		}
		if strings.Contains(b, "AgentActor literal") {
			namedActor = true
		}
	}
	if !namedRoad || !namedActor {
		t.Fatalf("the rogue was refused for %+v — the guard must name BOTH the second road and the invented identity", bad)
	}

	// The same rogue with an EMPTY actor is still caught. The empty literal is
	// not itself the offence (Go writes it beside a refused return all the time),
	// so this case pins that the second-road predicate fires on its own — without
	// it, the only thing the guard would ever notice is literals.
	emptyRogue := strings.Replace(rogue, `store.AgentActor{Kind: "system", ID: "nobody"}`, "store.AgentActor{}", 1)
	if badEmpty := agentRoadOffenses("cmd/aeon/rogue_road.go", emptyRogue); len(badEmpty) != 1 ||
		!strings.Contains(badEmpty[0], "one road onto the content writers") {
		t.Fatalf("the empty-actor rogue was refused for %v, want exactly the second-road refusal", badEmpty)
	}

	// The invented-identity predicate on the ONE file that is allowed an actor:
	// the wiring file, building its actor from strings instead of from the
	// connection. This is the placeholder T4's "Do not" list forbids.
	literalRoad := `package main

import (
	"context"

	"github.com/cjlapao/aeon/internal/agentmgr"
	"github.com/cjlapao/aeon/internal/store"
)

func road(ctx context.Context, mgr *agentmgr.Manager) error {
	actor := store.AgentActor{Kind: "system", ID: "migrate"}
	return mgr.Delete(ctx, actor, "some-agent")
}
`
	if bad2 := agentRoadOffenses("cmd/aeon/agent_management.go", literalRoad); len(bad2) == 0 {
		t.Fatal("the wiring file building an invented actor passed the guard — that is the placeholder T4 forbids")
	}

	// And a clean file: the road the tree actually has, with the peer-derived
	// actor, must produce nothing. Without this half the predicate is just loud.
	clean := `package main

import (
	"context"

	"github.com/cjlapao/aeon/internal/agentmgr"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

func road(ctx context.Context, mgr *agentmgr.Manager) error {
	kind, id, ok := protocol.AgentActor(ctx)
	if !ok {
		return protocol.ErrAgentNoActor
	}
	actor := store.AgentActor{Kind: kind, ID: id}
	return mgr.Delete(ctx, actor, "some-agent")
}
`
	if bad3 := agentRoadOffenses("cmd/aeon/agent_management.go", clean); len(bad3) != 0 {
		t.Fatalf("the real shape of the socket road offended the guard: %v", bad3)
	}
}
