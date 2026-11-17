package store

// agent_change.go — S73 T3 (AR1–AR4): the agent plane's append-only ledger,
// the actor that must be named to write to it, and the ONE way in.
//
// # What this file is, and what `agent_audited.go` is
//
// The ledger row's shape, its vocabulary and its single insertion live here;
// the six exported doors that pair a row write with one of these rows live in
// `agent_audited.go`. The split is by reason to read: a reader of this file
// wants to know what gets recorded and who may be named as having done it; a
// reader of the other wants to know how a change is made. The primitives that
// actually touch `agent` (`createAgentTx`, `updateAgentTx`, `deleteAgentTx`)
// stay in `agent.go`, beside the row they write, and take a `*sql.Tx` — which
// is the compile-time half of AR3: no road to the `agent` table exists outside
// a transaction somebody opened in an audited function.
//
// # Why the actor is a value and not a string
//
// `store.Actor` (actor.go) belongs to the RECORDS tier — `user|agent|trigger` —
// and neither `cli` nor `system` is in its vocabulary, which is why the agent
// plane has its own two-field actor with its own CHECK. The store will not be
// trusted with a person it invented: the caller names the actor, `Valid` says
// whether that name is usable, and it says so BEFORE SQL, in Go, naming the
// half that is wrong. S46's MCP door and S54's platform door keep the same law
// for the same reason — a stamped identity that failed halfway is worse than
// one that never started.
//
// # Append-only is the database's word, not this file's
//
// There is no update and no delete accessor here, deliberately, exactly as
// `McpChangeInsertTx` argues. Even with one, the two triggers minted in
// migration 20261106181435 answer an UPDATE or a DELETE with
// `agent_change is append-only (S73 AR1)`; the missing accessor is what stops
// the mistake, the trigger is what survives it.

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// AgentActor is who the ledger is allowed to name as having changed an agent
// row. Two fields, three kinds, no more: `user` is a person at the socket (the
// id is their peer uid), `cli` is the direct door (id `cli`), `system` is the
// one genuinely person-less writer, the seed engine (id `seed-engine`). An
// `agent` kind is NOT offered: AR8's measurement is that no agent talks to the
// management plane at all, and a vocabulary that could name one would be a
// claim that it can.
type AgentActor struct {
	Kind string
	ID   string
}

// The three kinds the migration's CHECK admits. Named so the nine paths do not
// type the words out.
const (
	AgentActorUser   = "user"
	AgentActorCLI    = "cli"
	AgentActorSystem = "system"
)

// The seven ops the ledger's CHECK admits. Each is one write to an `agent`
// row; there is no `export` (a file is not a row) and no `noop` (a no-op is not
// a change).
const (
	AgentOpCreate    = "create"
	AgentOpDuplicate = "duplicate"
	AgentOpEdit      = "edit"
	AgentOpDelete    = "delete"
	AgentOpImport    = "import"
	AgentOpMigrate   = "migrate"
	AgentOpSeed      = "seed"
)

// agentOps is the closed vocabulary, in the order the migration states it, for
// the refusal that lists it.
var agentOps = []string{
	AgentOpCreate, AgentOpDuplicate, AgentOpEdit,
	AgentOpDelete, AgentOpImport, AgentOpMigrate, AgentOpSeed,
}

var agentActorKinds = []string{AgentActorUser, AgentActorCLI, AgentActorSystem}

// Valid refuses, in Go and before any statement, the four actor values that
// would otherwise reach SQL and either fail a CHECK with a sentence about
// constraint names or — worse, if a CHECK were ever widened — succeed. Each
// refusal names WHICH half is unusable, because the caller is a door and the
// answer has to be forwardable.
func (a AgentActor) Valid() error {
	if strings.TrimSpace(a.Kind) == "" {
		return fmt.Errorf("refused: the agent ledger names an actor kind, and none was given — a change nobody performed is not a record")
	}
	ok := false
	for _, k := range agentActorKinds {
		if a.Kind == k {
			ok = true
			break
		}
	}
	if !ok {
		return fmt.Errorf("refused: actor kind %q is not one of the kinds the agent ledger admits — %s",
			a.Kind, strings.Join(agentActorKinds, ", "))
	}
	if strings.TrimSpace(a.ID) == "" {
		return fmt.Errorf("refused: actor kind %q needs an id to name — %s alone is a kind, not an actor", a.Kind, a.Kind)
	}
	if a.ID == "0" {
		// uid 0 is the root of the machine, not a person at the endpoint. The
		// MCP and environment doors refuse it the same way, for the same
		// reason: it is the id a bug reaches for when it has no peer.
		return fmt.Errorf("refused: actor id \"0\" is not an identity — the agent ledger names a person, a CLI, or the seed engine")
	}
	if len(a.ID) > 256 {
		return fmt.Errorf("refused: actor id is %d characters, and the agent ledger holds 256 — truncating an identity is inventing one", len(a.ID))
	}
	return nil
}

// String is the short form for a log line or a refusal. The ledger itself
// stores the two fields apart, because that is what makes them filterable.
func (a AgentActor) String() string { return a.Kind + ":" + a.ID }

// AgentChange is one row of the ledger. JSON tags are camelCase, the way the
// `Agent` row and `McpChange` present themselves, because this struct is also
// what `agent.changes` answers with (T5).
//
// Slug is a *string, not a string: the `migrate` run has no single row to name
// (AR1's CHECK lets exactly that op carry a NULL, and no other op), and a
// History tab renders the absence as `every agent` rather than as an empty key
// that reads like a bug.
type AgentChange struct {
	ID         int64   `json:"id"`
	Op         string  `json:"op"`
	Slug       *string `json:"slug"`
	Summary    string  `json:"summary"`
	ActorKind  string  `json:"actorKind"`
	ActorID    string  `json:"actorId"`
	OccurredAt string  `json:"occurredAt"`
}

// Actor is the ledger row's actor read back as the value that wrote it.
func (c AgentChange) Actor() AgentActor { return AgentActor{Kind: c.ActorKind, ID: c.ActorID} }

// Validate is the Go-side twin of the migration's CHECKs, run before the
// INSERT so a mistake names its own field instead of a constraint. The CHECKs
// stay in the DDL regardless: they are the last word when the caller is a bug
// rather than a reader of this file.
func (c AgentChange) Validate() error {
	op := false
	for _, o := range agentOps {
		if c.Op == o {
			op = true
			break
		}
	}
	if !op {
		return fmt.Errorf("refused: %q is not an op the agent ledger records — %s", c.Op, strings.Join(agentOps, ", "))
	}
	if c.Slug != nil {
		if strings.TrimSpace(*c.Slug) == "" {
			return fmt.Errorf("refused: an agent_change slug that is blank is not the same as one that is absent — %s asked for the unfiltered shape with an empty key", c.Op)
		}
		if len(*c.Slug) > 256 {
			return fmt.Errorf("refused: an agent_change slug is %d characters, and the ledger holds 256", len(*c.Slug))
		}
	} else if c.Op != AgentOpMigrate {
		return fmt.Errorf("refused: a %s row must name the slug it changed — only a migrate run has no single row to name", c.Op)
	}
	if n := len(c.Summary); n < 1 || n > 1024 {
		return fmt.Errorf("refused: an agent_change summary is %d characters and the ledger holds 1 to 1024 — %q", n, c.Summary)
	}
	if err := c.Actor().Valid(); err != nil {
		return err
	}
	if strings.TrimSpace(c.OccurredAt) == "" {
		return fmt.Errorf("refused: an agent_change without an occurred_at is a claim with no when")
	}
	return nil
}

// SlugPtr is the one way to build the nullable slug, so a caller cannot
// accidentally write a pointer to an empty string (which Validate refuses, but
// only after they have built it).
func SlugPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// AgentChangeInsertTx appends one ledger row on the caller's transaction.
// There is no non-Tx form on purpose: AR2's law is that the row and its
// ledger move together, and a function that could commit a ledger row on its
// own is the hole in that sentence.
func AgentChangeInsertTx(ctx context.Context, tx *sql.Tx, c AgentChange) error {
	if err := c.Validate(); err != nil {
		return err
	}
	if tx == nil {
		return fmt.Errorf("refused: agent_change is written on the caller's transaction, never on a nil one")
	}
	_, err := tx.ExecContext(ctx,
		`INSERT INTO agent_change (op, slug, summary, actor_kind, actor_id, occurred_at) VALUES (?, ?, ?, ?, ?, ?)`,
		c.Op, c.Slug, c.Summary, c.ActorKind, c.ActorID, c.OccurredAt)
	if err != nil {
		return fmt.Errorf("store: agent change insert (%s): %w", c.Op, err)
	}
	return nil
}

// --- the summary formatters -------------------------------------------------
//
// Nine roads change an agent row (AR4). Nine sentences would be nine shapes a
// History tab has to special-case and nine places a detail can be forgotten,
// so the sentences are built here and nowhere else. The house voice is
// `mcp_change`'s: the op word leads, the subject follows, the detail is in
// parentheses — ids and names, never a value that could be a credential.

// AgentChangeSummaryCreate names the row that arrived.
func AgentChangeSummaryCreate(slug, name string) string {
	return fmt.Sprintf("create %s (%s)", slug, quotedOrNoName(name))
}

// AgentChangeSummaryDuplicate names both sides: the row that was copied and
// the row that arrived, with the copy's name. The source keeps existing, so a
// ledger that named only one of them would be a riddle.
func AgentChangeSummaryDuplicate(source, target, name string) string {
	return fmt.Sprintf("duplicate %s as %s (%s)", source, target, quotedOrNoName(name))
}

// AgentChangeSummaryEdit is the design's own sentence. Nine content fields is
// a fact about `agentmgr.Edit`'s write, not a count of what changed — the
// write is whole, and AR2 records it as whole.
func AgentChangeSummaryEdit(slug string) string {
	return fmt.Sprintf("edit %s (9 content fields)", slug)
}

// AgentChangeSummaryDelete names a slug that is already gone. The name rides
// along because after this row it exists nowhere else.
func AgentChangeSummaryDelete(slug, name string) string {
	return fmt.Sprintf("delete %s (%s)", slug, quotedOrNoName(name))
}

// AgentChangeSummaryImport states which of AM3's two outcomes happened (a
// create and an update; the no-op mints no row at all, so it is not a case
// here), and names the proposal when the write arrived through the desk — the
// one road where the ledger row is about a decision as well as a change.
func AgentChangeSummaryImport(slug, version string, created bool, proposalID string) string {
	kind := "updated"
	if created {
		kind = "created"
	}
	if strings.TrimSpace(proposalID) == "" {
		return fmt.Sprintf("import %s v%s (%s)", slug, versionOr(version, "1"), kind)
	}
	return fmt.Sprintf("import %s v%s (%s, proposal %s)", slug, versionOr(version, "1"), kind, proposalID)
}

// AgentChangeSummaryMigrate is ONE row for a whole run (AR4): the write is the
// run, and a row per derived agent would bury the ledger in entries that name
// nobody individually.
func AgentChangeSummaryMigrate(created int) string {
	return fmt.Sprintf("migrate derived %d rows from plugin_agent", created)
}

// AgentChangeSummarySeed is the seed engine's sentence, in three verbs:
// `inserted` (an apply laid a row), `updated` (an adopt or update rewrote one)
// and `retired` (SD10's flag, which is a write and therefore a change).
func AgentChangeSummarySeed(slug, verb string) string {
	return fmt.Sprintf("seed %s %s", slug, verb)
}

// quotedOrNoName keeps a name readable in a summary without letting an empty
// one look like a bug.
func quotedOrNoName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "no name recorded"
	}
	if len(name) > 120 {
		name = name[:120]
	}
	return "\"" + strings.ReplaceAll(name, "\"", "'") + "\""
}
