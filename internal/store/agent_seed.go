package store

import (
	"context"
	"database/sql"
	"fmt"
)

// S43-extra T4: the agent row's content write and read — the SQL the seed
// applier needs, kept in internal/store so no SQL lives outside it (the
// TestNoSQLLivesOutsideTheStore guard). The applier (internal/agentseed)
// calls these through the shared transaction; the SQL is here, the domain is
// there. A content is the set of columns the applier owns; the identity
// columns (id, slug, created_at, the signature columns) are never touched.

// AgentContent is the content columns the seed applier writes to an agent
// row — everything except the identity and signature columns (AM2: id and
// slug are set once and never after; the signature is the import's, not the
// seed's). It is the store's view of a record's content; the applier maps
// its own content onto it.
type AgentContent struct {
	Slug        string
	Name        string
	Description string
	Prompt      string
	Tools       string
	Temperature float64
	Handoff     string
	PolicyAgent string
	BodyHash    string
	Origin      string
	Internal    bool
	Metadata    string
	// Version is the agent row's version word — the seed record's version, so a
	// re-seed to a higher set version shows as the new version on the row. Empty
	// means "1" (a row the seed never versioned).
	Version string
}

// SeedAvatarColor is the colour a seeded agent's minion wears: minions are
// yellow. The update only recolours a row still wearing the old seed default
// (blue) or none — a colour a person chose is theirs and stays.
const SeedAvatarColor = "yellow"

func orVersion(v string) string {
	if v == "" {
		return "1"
	}
	return v
}

// SeedAgentInsert writes a new system agent row from its content, on the
// shared transaction. The row is a system row: origin='system', internal=1,
// signed=0, the signature empty. The identity and signature columns are
// never touched (AM2).
//
// S73 T3 added the actor and the stamp, and with them the ledger row this write
// always owed (AR1: a seed apply changes an agent row, so it is a `seed` entry;
// AR2: on the SAME transaction). Taking the actor as a parameter is the point —
// the store does not get to decide it was the seed engine; the applier says so,
// and `system:seed-engine` is what the applier passes (AR4 path 9). The
// actor-first refusal is the same shape as the doors in agent_audited.go: an
// unusable name stops before any statement.
func (s *Store) SeedAgentInsert(ctx context.Context, tx *sql.Tx, c AgentContent, actor AgentActor, at string) error {
	if err := actor.Valid(); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO agent (slug, name, description, prompt, tools, temperature, handoff, policy_agent, body_hash, origin, internal, metadata, version, avatar_type, avatar_color, avatar_mood, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?, 'minion', ?, 'idle', '', '')`,
		c.Slug, c.Name, c.Description, c.Prompt, c.Tools, c.Temperature, c.Handoff, c.PolicyAgent, c.BodyHash, c.Origin, c.Metadata,
		orVersion(c.Version), SeedAvatarColor); err != nil {
		return err
	}
	return AgentChangeInsertTx(ctx, tx, AgentChange{
		Op: AgentOpSeed, Slug: SlugPtr(c.Slug),
		Summary:    AgentChangeSummarySeed(c.Slug, "inserted"),
		ActorKind:  actor.Kind,
		ActorID:    actor.ID,
		OccurredAt: at,
	})
}

// SeedAgentUpdate updates an existing agent row's content columns (adopt or
// update — the same write) on the shared transaction. The slug, id,
// created_at and the signature columns are never touched (AM2). The ledger row
// rides the same transaction, and the sentence says `updated` for both adopt and
// update because the ENGINE records that distinction in the seed audit; the
// agent ledger records that the row changed and who said so.
func (s *Store) SeedAgentUpdate(ctx context.Context, tx *sql.Tx, c AgentContent, actor AgentActor, at string) error {
	if err := actor.Valid(); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE agent SET name=?, description=?, prompt=?, tools=?, temperature=?, handoff=?, policy_agent=?, body_hash=?, metadata=?, version=?,
		   avatar_color = CASE WHEN avatar_color IN ('', 'blue') THEN ? ELSE avatar_color END WHERE slug=?`,
		c.Name, c.Description, c.Prompt, c.Tools, c.Temperature, c.Handoff, c.PolicyAgent, c.BodyHash, c.Metadata,
		orVersion(c.Version), SeedAvatarColor, c.Slug); err != nil {
		return err
	}
	return AgentChangeInsertTx(ctx, tx, AgentChange{
		Op: AgentOpSeed, Slug: SlugPtr(c.Slug),
		Summary:    AgentChangeSummarySeed(c.Slug, "updated"),
		ActorKind:  actor.Kind,
		ActorID:    actor.ID,
		OccurredAt: at,
	})
}

// SeedAgentRow reads one agent row's content by slug on the shared
// transaction. present is false (and the zero content) when no row carries
// the slug. The columns read are the content the applier owns; the identity
// and signature columns are not read (the applier never writes them).
func (s *Store) SeedAgentRow(ctx context.Context, tx *sql.Tx, key string) (AgentContent, bool, error) {
	rows, err := tx.QueryContext(ctx,
		`SELECT name, description, prompt, tools, temperature, handoff, policy_agent, metadata, version, internal FROM agent WHERE slug = ?`, key)
	if err != nil {
		return AgentContent{}, false, err
	}
	defer rows.Close()
	if !rows.Next() {
		return AgentContent{}, false, nil
	}
	var c AgentContent
	var internal int
	if err := rows.Scan(&c.Name, &c.Description, &c.Prompt, &c.Tools, &c.Temperature, &c.Handoff, &c.PolicyAgent, &c.Metadata, &c.BodyHash, &internal); err != nil {
		return AgentContent{}, false, err
	}
	c.Slug = key
	c.Internal = internal == 1
	return c, true, nil
}

// SeedAgentExists reports whether an agent row carries the slug, on the
// shared transaction.
func (s *Store) SeedAgentExists(ctx context.Context, tx *sql.Tx, key string) (bool, error) {
	rows, err := tx.QueryContext(ctx, `SELECT 1 FROM agent WHERE slug = ?`, key)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	return rows.Next(), nil
}

// SeedAgentRetire flags a row retired in its Metadata blob, on the shared
// transaction — it never deletes the row (SD10). It is a write, not a delete.
//
// It is the third seed writer, and AR4's nine-path table named only the pair:
// measured, the seed plane has three statements against the `agent` table (see
// the S73 T3 Notes), and a retire is exactly as much a change to the row as an
// update is — it moves `metadata` and `updated_at`. So it carries the ledger row
// too, with its own verb. A guard that let this one through while refusing the
// other two would be a guard that had not read the file.
func (s *Store) SeedAgentRetire(ctx context.Context, tx *sql.Tx, c AgentContent, actor AgentActor, at string) error {
	if err := actor.Valid(); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE agent SET metadata=?, updated_at='' WHERE slug=?`, c.Metadata, c.Slug); err != nil {
		return fmt.Errorf("seed: retire a row: %w", err)
	}
	return AgentChangeInsertTx(ctx, tx, AgentChange{
		Op: AgentOpSeed, Slug: SlugPtr(c.Slug),
		Summary:    AgentChangeSummarySeed(c.Slug, "retired"),
		ActorKind:  actor.Kind,
		ActorID:    actor.ID,
		OccurredAt: at,
	})
}
