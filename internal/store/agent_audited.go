package store

// agent_audited.go — S73 T3 (AR2, AR3, AR4): the ONLY exported ways to change
// an `agent` row, and each one names who did it.
//
// # The shape every door here keeps
//
// `actor.Valid()` first, in Go, before a transaction opens — a door that
// started a write and then noticed it had no actor would have to roll something
// back, and a refusal should never have cost anything. Then ONE transaction:
// the row write (through the primitives in `agent.go`, which are unreachable
// any other way) and the `agent_change` row, committed or rolled back together
// (AR2). Then the commit. Nothing between the two writes can refuse in a way
// that leaves one applied: the ledger's own validation runs first inside
// `AgentChangeInsertTx`, and any failure unwinds both.
//
// # What the doors do NOT do
//
// They do not restate the domain's laws. AM1's internal guard, AM2's collision,
// AM3's lower version and AM4's references are refused where they have always
// been refused, in `internal/agentmgr`, before a door is knocked on — this file
// moves plumbing, it does not re-litigate a rule. And `agent.export` is not
// here at all: it writes a file, not a row, and a ledger entry with no row
// would make AR2 unenforceable.
//
// # Why the actor is a parameter and never a default
//
// There is no `s.now()`-style fallback for an identity. A store that could
// invent a person would eventually do it on a path nobody audited; every door
// here takes the actor from its caller, and the callers are the nine roads in
// design §AR4 (T4 wires what each of them stamps).

import (
	"context"
	"database/sql"
	"fmt"
)

// CreateAgentAudited makes a brand-new agent row and records who made it.
// Returns the same refusal the un-audited writer gave (ErrAgentSlugExists
// naming the collision, or "needs a slug / needs a name") — the sentences are
// unchanged, only the ledger row is new.
func (s *Store) CreateAgentAudited(ctx context.Context, actor AgentActor, a Agent, now string) error {
	if err := actor.Valid(); err != nil {
		return err
	}
	return s.AgentWithTx(ctx, func(tx *sql.Tx) error {
		if err := createAgentTx(ctx, tx, a, now); err != nil {
			return err
		}
		return AgentChangeInsertTx(ctx, tx, AgentChange{
			Op: AgentOpCreate, Slug: SlugPtr(a.Slug),
			Summary:    AgentChangeSummaryCreate(a.Slug, a.Name),
			ActorKind:  actor.Kind,
			ActorID:    actor.ID,
			OccurredAt: now,
		})
	})
}

// DuplicateAgentAudited copies a source row into a NEW row: a fresh id, a slug
// freshly generated from the COPY's name (never the source's slug — AM2), and
// `internal` forced false regardless of the source (AM1). The refusal sentences
// are the ones this function has always given; the new fact is that the copy
// and the ledger row arrive on one transaction, so there is no copy nobody
// signed for.
func (s *Store) DuplicateAgentAudited(ctx context.Context, actor AgentActor, source Agent, copyName, now string) (Agent, error) {
	if err := actor.Valid(); err != nil {
		return Agent{}, err
	}
	slug := Slugify(copyName)
	if slug == "" {
		return Agent{}, fmt.Errorf("refused: a duplicate needs a name that slugifies to something")
	}
	out := source
	out.ID = 0
	out.Slug = slug
	out.Name = copyName
	out.Internal = false
	// A copy is the person's own agent, whatever its source was: a copy of a shipped agent is not first-party.
	out.Origin = "user"
	out.CreatedAt = ""
	out.UpdatedAt = ""
	err := s.AgentWithTx(ctx, func(tx *sql.Tx) error {
		if err := createAgentTx(ctx, tx, out, now); err != nil {
			return err
		}
		return AgentChangeInsertTx(ctx, tx, AgentChange{
			Op: AgentOpDuplicate, Slug: SlugPtr(slug),
			Summary:    AgentChangeSummaryDuplicate(source.Slug, slug, copyName),
			ActorKind:  actor.Kind,
			ActorID:    actor.ID,
			OccurredAt: now,
		})
	})
	if err != nil {
		return Agent{}, err
	}
	return out, nil
}

// UpdateAgentAudited writes the nine content fields and records the edit. The
// row count is the caller's to interpret (`agentmgr.Edit` re-reads when it is
// zero), and 0 mints NO ledger row: a write that touched nothing did not change
// anything, and the ledger is a record of changes, not of attempts. An edit
// that DID write always mints one, whether or not a content field moved —
// `updated_at` moved, which is a change (deviation 4).
func (s *Store) UpdateAgentAudited(ctx context.Context, actor AgentActor, a Agent, now string) (int64, error) {
	if err := actor.Valid(); err != nil {
		return 0, err
	}
	var n int64
	err := s.AgentWithTx(ctx, func(tx *sql.Tx) error {
		written, err := updateAgentTx(ctx, tx, a, now)
		if err != nil {
			return err
		}
		n = written
		if written == 0 {
			return nil
		}
		return AgentChangeInsertTx(ctx, tx, AgentChange{
			Op: AgentOpEdit, Slug: SlugPtr(a.Slug),
			Summary:    AgentChangeSummaryEdit(a.Slug),
			ActorKind:  actor.Kind,
			ActorID:    actor.ID,
			OccurredAt: now,
		})
	})
	if err != nil {
		return 0, err
	}
	return n, nil
}

// DeleteAgentAudited removes a row and records the removal — including the
// NAME, which after this transaction exists nowhere else in the database. The
// row is read on the same transaction, so the name in the ledger is the name of
// the row that went, not the name someone remembered.
func (s *Store) DeleteAgentAudited(ctx context.Context, actor AgentActor, slug, now string) error {
	if err := actor.Valid(); err != nil {
		return err
	}
	return s.AgentWithTx(ctx, func(tx *sql.Tx) error {
		name := ""
		if a, found, err := agentBySlugTx(ctx, tx, slug); err != nil {
			return err
		} else if found {
			name = a.Name
		}
		n, err := deleteAgentTx(ctx, tx, slug)
		if err != nil {
			return err
		}
		if n == 0 {
			return fmt.Errorf("refused: %s has no row to delete", slug)
		}
		return AgentChangeInsertTx(ctx, tx, AgentChange{
			Op: AgentOpDelete, Slug: SlugPtr(slug),
			Summary:    AgentChangeSummaryDelete(slug, name),
			ActorKind:  actor.Kind,
			ActorID:    actor.ID,
			OccurredAt: now,
		})
	})
}

// ImportAgentCardAudited is the (slug, version) idempotency of import (AM3),
// byte-for-byte the law the un-audited door ran: a repeated (slug, version) is
// a no-op, a higher version updates the row in place (never a second row — a
// card is one evolving thing), a lower version is refused and named, a new slug
// creates. The signature and certificate are the caller's verified answer (the
// verify seam ran before this door was reached): signed=true with both set, or
// signed=false for an unsigned import admitted behind the override.
//
// The ledger mints a row for the two WRITES and nothing for the no-op, because
// a no-op is not a change (AR1: the ledger pairs with row writes).
//
// S73 T4, on the empty string the two summaries below pass: `AgentChangeSummaryImport`
// can print `proposal <id>`, because AR4 said a commit of an `agent_import` proposal
// names the proposal it rode. Measured, no production road in this tree reaches this
// door from a desk commit — the CLI's `aeon agent import` and the commit of that kind
// both end in `plugin.ImportChecked`, the LIFECYCLE import, which never writes the
// `agent` table (design deviation 15, `cmd/aeon/agent_actor_test.go`). So the door
// deliberately takes no proposalID parameter rather than accepting one nothing can
// fill: a road that arrives with a proposal would add the parameter in the same
// change, refuse the combination otherwise, and never invent an id. The empty string
// here is a documented absence, not a guess.
func (s *Store) ImportAgentCardAudited(ctx context.Context, actor AgentActor, a Agent) (ImportResult, error) {
	if err := actor.Valid(); err != nil {
		return ImportResultRefused, err
	}
	var res ImportResult
	err := s.AgentWithTx(ctx, func(tx *sql.Tx) error {
		at := s.Now()
		existing, found, err := agentBySlugTx(ctx, tx, a.Slug)
		if err != nil {
			return err
		}
		if !found {
			if a.CreatedAt == "" {
				a.CreatedAt = at
			}
			a.UpdatedAt = a.CreatedAt
			if err := createAgentTx(ctx, tx, a, a.CreatedAt); err != nil {
				return err
			}
			res = ImportResultCreated
			return AgentChangeInsertTx(ctx, tx, AgentChange{
				Op: AgentOpImport, Slug: SlugPtr(a.Slug),
				Summary:    AgentChangeSummaryImport(a.Slug, a.Version, true, ""),
				ActorKind:  actor.Kind,
				ActorID:    actor.ID,
				OccurredAt: at,
			})
		}
		cmp, err := CompareVersions(a.Version, existing.Version)
		if err != nil {
			return err
		}
		switch cmp {
		case 0:
			// Same (slug, version): a no-op, not an error — re-running an
			// import script must be safe (AM3) — and no ledger row.
			res = ImportResultNoOp
			return nil
		case -1:
			// Lower version than what is stored: refused, named, never
			// silently accepted or ignored (AM3).
			res = ImportResultRefused
			return fmt.Errorf("refused: %s is version %s, lower than the stored %s — a lower version is not an update", a.Slug, a.Version, existing.Version)
		default:
			existing.Description = a.Description
			existing.Functionality = a.Functionality
			existing.Metadata = a.Metadata
			existing.Prompt = a.Prompt
			existing.Skills = a.Skills
			existing.Version = a.Version
			existing.Signature = a.Signature
			existing.Certificate = a.Certificate
			existing.Signed = a.Signed
			existing.AvatarType = a.AvatarType
			existing.AvatarColor = a.AvatarColor
			existing.AvatarMood = a.AvatarMood
			n, err := updateAgentTx(ctx, tx, existing, at)
			if err != nil {
				return err
			}
			if n == 0 {
				res = ImportResultRefused
				return fmt.Errorf("refused: %s vanished between read and write — re-read and decide again", a.Slug)
			}
			res = ImportResultUpdated
			return AgentChangeInsertTx(ctx, tx, AgentChange{
				Op: AgentOpImport, Slug: SlugPtr(a.Slug),
				Summary:    AgentChangeSummaryImport(a.Slug, a.Version, false, ""),
				ActorKind:  actor.Kind,
				ActorID:    actor.ID,
				OccurredAt: at,
			})
		}
	})
	if err != nil {
		if res == ImportResultRefused {
			return res, err
		}
		return ImportResultRefused, err
	}
	return res, nil
}

// MigrateFirstPartyAgentsAudited is the back-fill: every plugin_agent that has
// no agent row yet gets one derived from its name (the first-party bodies are
// the rows every agent must have, AM1). Idempotent — a re-run makes no new rows
// — and it refuses, named, a name that slugifies to nothing (DB19: never a
// silent gap) or a slug that collides (AM2: never a silent suffix).
//
// TWO things changed when it became a door, and both are recorded rather than
// slipped: the run is now ONE transaction, so a refusal part-way through leaves
// nothing behind where it once left the rows written so far (AR2's law is the
// row and its ledger together, and for a run the row is the run); and the whole
// run mints ONE ledger row, not one per derived agent (AR4) — the derived rows
// are discoverable by their own createdAt, and a ledger buried under forty
// entries that name nobody individually is a ledger nobody reads.
func (s *Store) MigrateFirstPartyAgentsAudited(ctx context.Context, actor AgentActor, now string) (int, error) {
	if err := actor.Valid(); err != nil {
		return 0, err
	}
	agents, err := s.PluginAgentsAll(false)
	if err != nil {
		return 0, err
	}
	created := 0
	err = s.AgentWithTx(ctx, func(tx *sql.Tx) error {
		for _, pa := range agents {
			if _, found, err := agentByNameTx(ctx, tx, pa.Name); err != nil {
				return err
			} else if found {
				continue
			}
			slug := Slugify(pa.Name)
			if slug == "" {
				return fmt.Errorf("refused: %s has a name that slugifies to nothing — a slug is required, not a gap (DB19)", pa.Name)
			}
			a := Agent{Slug: slug, Name: pa.Name, Version: "1", Internal: true}
			if err := createAgentTx(ctx, tx, a, now); err != nil {
				return err
			}
			created++
		}
		return AgentChangeInsertTx(ctx, tx, AgentChange{
			// The run has no single slug to name — the one op the CHECK lets
			// through with a NULL (AR1), and the reason agent.changes {slug}
			// cannot show this row (AR6).
			Op: AgentOpMigrate, Slug: nil,
			Summary:    AgentChangeSummaryMigrate(created),
			ActorKind:  actor.Kind,
			ActorID:    actor.ID,
			OccurredAt: now,
		})
	})
	if err != nil {
		return created, err
	}
	return created, nil
}

// AgentWithTx is this plane's transaction, the sibling of McpWithTx,
// BlueprintWithTx, SeedWithTx and the rest of the family: one Begin, one
// Commit, a Rollback on any error, and the ledger row riding inside the same
// one. It is exported because "the row and its ledger commit together" should
// be one implementation rather than six copies of the same three calls that
// could each forget a step — AR2's framework. The doors above are its only
// callers today, and the seed applier gets its transaction from the seed
// engine, which owns the wider unit.
func (s *Store) AgentWithTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	tx, err := s.w.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: agent with tx: %w", err)
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: agent commit: %w", err)
	}
	return nil
}
