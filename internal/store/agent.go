// agent.go is S43 T1's row layer (migration 20261030130000_agent). Every
// agent — first-party and imported — is a row here. plugin_agent (S15,
// migration 0023) is the LIFECYCLE table (disabled/enabled/revoked), untouched
// by this slice; the agent table is the CONTENT, joined to plugin_agent by
// name, and keyed by the slug (AM2 — the universal reference key). The domain
// refusals — internal refuses edit/delete, a slug collision refuses, an
// import that is lower-version refuses — live in the higher layers that call
// these; the database's own job is the unique index on slug and the CHECK
// constraints in the migration, which make the wrong shape unwritable.

package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// Agent is one content row. ID is internal identity only (never shown as the
// name of anything); Slug is the reference key code crosses-references by
// (AM2); Name is what a person sees.
type Agent struct {
	ID            int64  `json:"id"`
	Slug          string `json:"slug"`
	Name          string `json:"name"`
	Description   string `json:"description"`
	Functionality string `json:"functionality"`
	Metadata      string `json:"metadata"` // a JSON blob (AM3); "" is the empty blob
	Prompt        string `json:"prompt"`
	Skills        string `json:"skills"` // a JSON array of {id,name,description}
	Version       string `json:"version"`
	Internal      bool   `json:"internal"`
	AvatarType    string `json:"avatarType"`
	AvatarColor   string `json:"avatarColor"`
	AvatarMood    string `json:"avatarMood"`
	Signature     string `json:"signature"`
	Certificate   string `json:"certificate"`
	Signed        bool   `json:"signed"`
	// S43-extra (T4): first-party dispatch reads these from the row; they
	// have no code truth, so they live in the row. mode/variant/command
	// rules/retired/declared stay in the Metadata blob. The Internal flag is
	// S43's and is unchanged (origin is the additive companion).
	Tools       string  `json:"tools"` // a JSON array (allow-list); "[]"
	Temperature float64 `json:"temperature"`
	Handoff     string  `json:"handoff"`     // a JSON object; "{}"
	PolicyAgent string  `json:"policyAgent"` // the linked policy agent's slug
	BodyHash    string  `json:"bodyHash"`    // the body's canonical hash
	Origin      string  `json:"origin"`      // system | user | import
	CreatedAt   string  `json:"createdAt"`
	UpdatedAt   string  `json:"updatedAt"`
}

// ErrAgentSlugExists is the one domain refusal this layer makes: a live row
// already carries the slug. The unique index agrees; this error lets the
// caller name the conflict rather than parse a constraint string (AM2: a
// collision refuses and names it, never silent-suffixed).
var ErrAgentSlugExists = errors.New("a live agent already carries this slug")

func scanAgent(row interface{ Scan(...any) error }) (Agent, error) {
	var a Agent
	var internal, signed int
	var metadata, skills string
	if err := row.Scan(
		&a.ID, &a.Slug, &a.Name, &a.Description, &a.Functionality, &metadata,
		&a.Prompt, &skills, &a.Version, &internal, &a.AvatarType, &a.AvatarColor,
		&a.AvatarMood, &a.Signature, &a.Certificate, &signed, &a.CreatedAt,
		&a.UpdatedAt, &a.Tools, &a.Temperature, &a.Handoff, &a.PolicyAgent,
		&a.BodyHash, &a.Origin,
	); err != nil {
		return Agent{}, err
	}
	a.Metadata = metadata
	a.Skills = skills
	a.Internal = internal == 1
	a.Signed = signed == 1
	return a, nil
}

// agentBySlugTx and agentByNameTx are the transaction-side reads of the two
// identity lookups. The audited doors use them INSIDE their transaction: a
// delete that names the row's name in the ledger has to have read that name
// from the same snapshot it is deleting, and an import's (slug, version)
// decision must be about the row the write will meet. The reader-pool versions
// above stay for everyone who is only looking.
func agentBySlugTx(ctx context.Context, tx *sql.Tx, slug string) (Agent, bool, error) {
	a, err := scanAgent(tx.QueryRowContext(ctx, `SELECT `+agentColumns+` FROM agent WHERE slug = ?`, slug))
	if errors.Is(err, sql.ErrNoRows) {
		return Agent{}, false, nil
	}
	if err != nil {
		return Agent{}, false, err
	}
	return a, true, nil
}

func agentByNameTx(ctx context.Context, tx *sql.Tx, name string) (Agent, bool, error) {
	a, err := scanAgent(tx.QueryRowContext(ctx,
		`SELECT `+agentColumns+` FROM agent WHERE name = ? OR slug = ? ORDER BY (slug = ?) DESC, id DESC LIMIT 1`, name, name, name))
	if errors.Is(err, sql.ErrNoRows) {
		return Agent{}, false, nil
	}
	if err != nil {
		return Agent{}, false, err
	}
	return a, true, nil
}

const agentColumns = `id, slug, name, description, functionality, metadata, prompt, skills, version, internal, avatar_type, avatar_color, avatar_mood, signature, signer_certificate, signed, created_at, updated_at, tools, temperature, handoff, policy_agent, body_hash, origin`

// createAgentTx writes a new content row on the caller's transaction.
//
// It is unexported, and that IS the rule (AR3): the exported `CreateAgent` is
// gone, so the only ways to create an agent row are the audited doors in
// `agent_audited.go` and the seed pair in `agent_seed.go`, and this function is
// unreachable without a transaction one of them opened. The slug collision
// check moved INSIDE the transaction with it: the count and the INSERT now read
// one snapshot instead of racing each other across the reader and writer
// pools, with the unique index still the last word and this check still the
// sentence in front of it (AM2 refuses a collision and names it, never
// silent-suffixes). id and slug are set here and never after.
func createAgentTx(ctx context.Context, tx *sql.Tx, a Agent, now string) error {
	a.Slug = strings.TrimSpace(a.Slug)
	if a.Slug == "" {
		return fmt.Errorf("refused: an agent needs a slug")
	}
	if strings.TrimSpace(a.Name) == "" {
		return fmt.Errorf("refused: an agent needs a name")
	}
	var live int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM agent WHERE slug = ?`, a.Slug).Scan(&live); err != nil {
		return err
	}
	if live > 0 {
		return ErrAgentSlugExists
	}
	_, err := tx.ExecContext(ctx,
		`INSERT INTO agent (slug, name, description, functionality, metadata, prompt, skills, version, internal, avatar_type, avatar_color, avatar_mood, signature, signer_certificate, signed, tools, handoff, origin, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		a.Slug, a.Name, a.Description, a.Functionality, a.Metadata, a.Prompt, a.Skills,
		versionOr(a.Version, "1"), boolInt(a.Internal), avatarTypeOr(a.AvatarType, "minion"),
		a.AvatarColor, avatarMoodOr(a.AvatarMood, "idle"), a.Signature, a.Certificate,
		boolInt(a.Signed), toolsOr(a.Tools, "[]"), toolsOr(a.Handoff, "{}"), originOr(a.Origin), now, now)
	return err
}

// CustomAgentSlugs are the slugs of the agents a person made or imported (not the shipped set), in
// slug order — the names dispatch adds to the shipped ones.
func (s *Store) CustomAgentSlugs() ([]string, error) {
	rows, err := s.r.Query(`SELECT slug FROM agent WHERE internal = 0 AND origin <> 'system' ORDER BY slug`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var slug string
		if err := rows.Scan(&slug); err != nil {
			return nil, err
		}
		out = append(out, slug)
	}
	return out, rows.Err()
}

// AgentBySlug answers who a slug is — the read the reference checks and the
// migration use. A slug with no row is (zero, false, nil), never an error:
// absence is a normal answer, not a fault (DB19).
func (s *Store) AgentBySlug(slug string) (Agent, bool, error) {
	var a Agent
	var internal, signed int
	var metadata, skills string
	err := s.r.QueryRow(`SELECT `+agentColumns+` FROM agent WHERE slug = ?`, slug).
		Scan(&a.ID, &a.Slug, &a.Name, &a.Description, &a.Functionality, &metadata,
			&a.Prompt, &skills, &a.Version, &internal, &a.AvatarType, &a.AvatarColor,
			&a.AvatarMood, &a.Signature, &a.Certificate, &signed, &a.CreatedAt,
			&a.UpdatedAt, &a.Tools, &a.Temperature, &a.Handoff, &a.PolicyAgent,
			&a.BodyHash, &a.Origin)
	if errors.Is(err, sql.ErrNoRows) {
		return Agent{}, false, nil
	}
	if err != nil {
		return Agent{}, false, err
	}
	a.Metadata = metadata
	a.Skills = skills
	a.Internal = internal == 1
	a.Signed = signed == 1
	return a, true, nil
}

// AgentByName answers which content row a name is — the join point to
// plugin_agent and the migration's name→slug lookup. A name may match more
// than one row (a duplicate shares its name); the newest live row wins, like
// plugin_state's own read. The slug matches too, and wins over a name: a shipped
// agent's name is its friendly title now ("Go Engineer"), while the callers that
// still pass what the registry calls it ("golang-engineer") are passing its slug.
func (s *Store) AgentByName(name string) (Agent, bool, error) {
	var a Agent
	var internal, signed int
	var metadata, skills string
	err := s.r.QueryRow(
		`SELECT `+agentColumns+` FROM agent WHERE name = ? OR slug = ? ORDER BY (slug = ?) DESC, id DESC LIMIT 1`, name, name, name).
		Scan(&a.ID, &a.Slug, &a.Name, &a.Description, &a.Functionality, &metadata,
			&a.Prompt, &skills, &a.Version, &internal, &a.AvatarType, &a.AvatarColor,
			&a.AvatarMood, &a.Signature, &a.Certificate, &signed, &a.CreatedAt,
			&a.UpdatedAt, &a.Tools, &a.Temperature, &a.Handoff, &a.PolicyAgent,
			&a.BodyHash, &a.Origin)
	if errors.Is(err, sql.ErrNoRows) {
		return Agent{}, false, nil
	}
	if err != nil {
		return Agent{}, false, err
	}
	a.Metadata = metadata
	a.Skills = skills
	a.Internal = internal == 1
	a.Signed = signed == 1
	return a, true, nil
}

// AgentsAll lists every content row — the whole surface, for `aeon agent
// list` and the settings page. Ordered by name then id, like the plugin
// registry's own listing.
func (s *Store) AgentsAll() ([]Agent, error) {
	rows, err := s.r.Query(`SELECT ` + agentColumns + ` FROM agent ORDER BY name, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Agent
	for rows.Next() {
		var a Agent
		var internal, signed int
		var metadata, skills string
		if err := rows.Scan(&a.ID, &a.Slug, &a.Name, &a.Description, &a.Functionality,
			&metadata, &a.Prompt, &skills, &a.Version, &internal, &a.AvatarType,
			&a.AvatarColor, &a.AvatarMood, &a.Signature, &a.Certificate, &signed,
			&a.CreatedAt, &a.UpdatedAt, &a.Tools, &a.Temperature, &a.Handoff,
			&a.PolicyAgent, &a.BodyHash, &a.Origin); err != nil {
			return nil, err
		}
		a.Metadata = metadata
		a.Skills = skills
		a.Internal = internal == 1
		a.Signed = signed == 1
		out = append(out, a)
	}
	return out, rows.Err()
}

// updateAgentTx writes the nine content fields plus updated_at, on the
// caller's transaction (see createAgentTx for why this is unexported). Any
// field EXCEPT slug and id, which are immutable after creation (AM2). A row
// that is not here returns RowsAffected 0 and leaves the decision to the
// caller; the database does not invent a missing row. `agentmgr.Edit` reaches
// it through UpdateAgentAudited, which mints the ledger row the same tx does
// (AR2) — and `Edit` writes these nine fields WHOLESALE, which is why the
// management table's nine-column window cannot seed an edit form (AR5).
func updateAgentTx(ctx context.Context, tx *sql.Tx, a Agent, now string) (int64, error) {
	res, err := tx.ExecContext(ctx,
		`UPDATE agent SET name=?, description=?, functionality=?, metadata=?, prompt=?, skills=?, version=?, avatar_type=?, avatar_color=?, avatar_mood=?, tools=?, handoff=?, updated_at=? WHERE id=?`,
		a.Name, a.Description, a.Functionality, a.Metadata, a.Prompt, a.Skills,
		a.Version, a.AvatarType, a.AvatarColor, a.AvatarMood, toolsOr(a.Tools, "[]"), toolsOr(a.Handoff, "{}"), now, a.ID)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// deleteAgentTx removes one content row by slug, on the caller's transaction
// (see createAgentTx for why this is unexported). It reports how many rows
// went — 0 is the caller's "there was nothing to delete", which DeleteAgent
// Audited turns into a refusal naming the slug, because a delete that reported
// success about a row that was not there would be a ledger entry about nothing.
// The reference refusals (a blueprint or a policy row still points at the slug
// — AM4) run at the caller, before the transaction opens.
func deleteAgentTx(ctx context.Context, tx *sql.Tx, slug string) (int64, error) {
	res, err := tx.ExecContext(ctx, `DELETE FROM agent WHERE slug = ?`, slug)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// HasAgentSlug is the collision check AM2's create/duplicate paths use before
// writing. It is a read, not a write: the unique index is the last word, but
// the caller needs to name the conflict.
func (s *Store) HasAgentSlug(slug string) (bool, error) {
	var n int
	err := s.r.QueryRow(`SELECT count(*) FROM agent WHERE slug = ?`, slug).Scan(&n)
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// AgentReferencedByPolicy is the policy half of AM4's deletion check: a
// policy row whose agent-kind target is the slug. A real query against the
// policy table (not a text scan), since the policy is a real table, not text
// (T2). The target_id is the slug after T2's migration.
func (s *Store) AgentReferencedByPolicy(slug string) ([]string, error) {
	// The rules written for the agent's own tools (ids `grant.<slug>.<tool>`, internal/protocol's
	// AgentGrantPrefix) are the agent's: they go with it when it is deleted, so they are not a
	// reference that refuses the delete.
	rows, err := s.r.Query(`SELECT id FROM policy WHERE target_kind = 'agent' AND target_id = ? AND id NOT LIKE 'grant.%'`, slug)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// DuplicateAgent, ImportAgentCard and MigrateFirstPartyAgents stood here until
// S73 T3. They were exported writers that could not name who did the writing,
// and AR3's answer was not to add an audited twin beside each one — a twin is
// a second door, and a second door is a door a caller can pick — but to move
// the write itself behind a transaction that carries the ledger row with it.
// Their bodies now live in `agent_audited.go` as DuplicateAgentAudited,
// ImportAgentCardAudited and MigrateFirstPartyAgentsAudited, each of them
// calling the primitives above and `AgentChangeInsertTx` on ONE transaction
// (AR2). A reader who came here for the import idempotency law (AM3) finds the
// law in that file, unchanged; a reader who came for the SQL finds it above.

// MigrateAgentPolicyReferences is T2's name→slug migration for the policy
// table: every policy row whose agent-kind target is still an agent NAME is
// re-pointed at the agent's SLUG (AM2 — the slug replaces the name as the
// reference key). It is idempotent: a row whose target_id is already a slug
// is skipped, a row whose target_id is a name is resolved to the slug. A
// name that resolves to no agent refuses the WHOLE migration (not one row,
// not a silent gap) — the T2 call (recorded in the task Notes): a partial
// migration would leave some rows name-keyed and some slug-keyed, which is
// the exact orphan the rule exists to prevent, so the migration is all-or-
// nothing. It returns how many rows it re-pointed.
func (s *Store) MigrateAgentPolicyReferences() (int, error) {
	rows, err := s.r.Query(`SELECT id, target_id FROM policy WHERE target_kind = 'agent' AND target_id IS NOT NULL`)
	if err != nil {
		return 0, err
	}
	type ref struct {
		id       string
		targetID string
	}
	var refs []ref
	for rows.Next() {
		var r ref
		if err := rows.Scan(&r.id, &r.targetID); err != nil {
			rows.Close()
			return 0, err
		}
		refs = append(refs, r)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	// Phase 1: resolve every row's name to a slug. A single unresolved name
	// refuses the whole migration (the all-or-nothing call above).
	type resolved struct {
		policyID string
		slug     string
	}
	var toUpdate []resolved
	for _, r := range refs {
		// Already a slug (a row the migration already did): skip, idempotent.
		if _, isSlug, err := s.AgentBySlug(r.targetID); err != nil {
			return 0, err
		} else if isSlug {
			continue
		}
		// A name: resolve it to the slug through the agent table.
		a, found, err := s.AgentByName(r.targetID)
		if err != nil {
			return 0, err
		}
		if !found {
			return 0, fmt.Errorf("refused: policy %s targets agent name %q, which resolves to no agent — a name with no agent row is an orphan, and the migration refuses rather than half-migrating (AM2/DB19)", r.id, r.targetID)
		}
		toUpdate = append(toUpdate, resolved{policyID: r.id, slug: a.Slug})
	}
	// Phase 2: apply the re-points.
	for _, u := range toUpdate {
		if _, err := s.w.Exec(`UPDATE policy SET target_id = ? WHERE id = ?`, u.slug, u.policyID); err != nil {
			return 0, err
		}
	}
	return len(toUpdate), nil
}

// BlueprintsReferencingAgent is the blueprint half of AM4's deletion check —
// a loop whose pinned blueprint still names the slug's agent. Blueprints live
// in files (the source), but the daemon loads them through the pinned copies
// in loop_blueprint.content, which is what a running loop is decided against
// (D12); a delete that orphans a pinned reference orphans a live loop, so the
// check is against the pinned copies, a real query. The content is YAML/JSON
// text, so the match is a LIKE on the slug — deny-leaning by design: a false
// match refuses a delete (safe), it never deletes, so an over-match is
// harmless. It returns the loop ids that name the slug, for the refusal to
// name them.
func (s *Store) BlueprintsReferencingAgent(slug string) ([]string, error) {
	rows, err := s.r.Query(`SELECT loop_id FROM loop_blueprint WHERE content LIKE ?`, "%"+slug+"%")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// ImportResult is the (slug, version) answer of an import — the three states
// (AM3): created, updated in place, or a no-op (same version already here).
type ImportResult int

const (
	ImportResultCreated ImportResult = iota
	ImportResultUpdated
	ImportResultNoOp
	ImportResultRefused
)

func (r ImportResult) String() string {
	switch r {
	case ImportResultCreated:
		return "created"
	case ImportResultUpdated:
		return "updated"
	case ImportResultNoOp:
		return "no-op (same version already stored)"
	case ImportResultRefused:
		return "refused"
	default:
		return "refused"
	}
}

func versionOr(v, def string) string {
	if strings.TrimSpace(v) == "" {
		return def
	}
	return v
}

// toolsOr is the tools column's value: the allow-list JSON the caller gave, or the empty list. An
// empty string would be stored as an empty string and read back as "malformed", so the default is
// written, not left to the column.
func toolsOr(v, def string) string {
	if strings.TrimSpace(v) == "" {
		return def
	}
	return v
}

// originOr is where a new row came from: system (the seed), import (a card someone brought in) or
// user (made here). An unnamed origin is a person's own agent — what the column's default always said.
func originOr(v string) string {
	switch v {
	case "system", "import":
		return v
	}
	return "user"
}

func avatarTypeOr(v, def string) string {
	if strings.TrimSpace(v) == "" {
		return def
	}
	return v
}

func avatarMoodOr(v, def string) string {
	if strings.TrimSpace(v) == "" {
		return def
	}
	return v
}

// Ensure the skills/metadata JSON helpers exist so a blank import is a valid,
// empty blob rather than a malformed one.

// Slugify turns a name into the immutable reference key (AM2): lowercase,
// non-alphanumerics collapsed to single hyphens, trimmed of leading/trailing
// hyphens. It is a pure function of the name, so the same name slugifies to
// the same slug — which is why a duplicate must carry a different name for
// its slug to differ (a same-name duplicate would collide, and is refused,
// never silently suffixed).
func Slugify(name string) string {
	var b strings.Builder
	lastHyphen := false
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		switch {
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			b.WriteRune(r)
			lastHyphen = false
		case r == ' ' || r == '-' || r == '_' || r == '.' || r == '/':
			if !lastHyphen && b.Len() > 0 {
				b.WriteByte('-')
				lastHyphen = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

// CompareVersions orders two dotted numeric versions: -1 (a<b), 0 (a==b), 1
// (a>b). Import idempotency (AM3) uses it: a repeated (slug,version) is a
// no-op, a higher version updates in place, a lower version is refused.
// Non-numeric segments compare lexically, never a parse error — a version is
// third-party text by plan (the policy's own rule for ids and target ids).
func CompareVersions(a, b string) (int, error) {
	as := strings.Split(strings.TrimSpace(a), ".")
	bs := strings.Split(strings.TrimSpace(b), ".")
	for i := 0; i < len(as) || i < len(bs); i++ {
		var av, bv string
		if i < len(as) {
			av = as[i]
		}
		if i < len(bs) {
			bv = bs[i]
		}
		an, aerr := parseIntLoose(av)
		bn, berr := parseIntLoose(bv)
		if aerr == nil && berr == nil {
			if an < bn {
				return -1, nil
			}
			if an > bn {
				return 1, nil
			}
			continue
		}
		if av < bv {
			return -1, nil
		}
		if av > bv {
			return 1, nil
		}
	}
	return 0, nil
}

func parseIntLoose(s string) (int, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("empty")
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, fmt.Errorf("not numeric: %q", s)
		}
	}
	var n int
	if _, err := fmt.Sscanf(s, "%d", &n); err != nil {
		return 0, err
	}
	return n, nil
}
