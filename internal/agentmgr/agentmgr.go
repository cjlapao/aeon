// Package agentmgr is S43 T6's domain — the one implementation behind the
// daemon's agent-management methods (P34: the daemon is the primary surface,
// the CLI and the Settings UI are secondary clients of it, never a second
// implementation). It is the single place that the create / import /
// duplicate / delete / edit / list / view operations live; the daemon reaches
// it only through the func seam in daemon.Options (deletability, like agentsvc),
// so the daemon never imports this package.
//
// The refusals this slice exists for, each one a named error rather than a
// silent wrong answer (AM1-AM5):
//
//	- a slug collision refuses and names it, never silent-suffixes (AM2);
//	- an import at a lower version refuses, named (AM3);
//	- a tampered card refuses, named; unsigned is refused by default, behind
//	  the allowUnsignedImport override, and then imported marked `unsigned` (AM3);
//	- an internal agent refuses edit and delete, but not duplication (AM1);
//	- a delete refuses if a blueprint or a policy row still references the slug
//	  (AM4) — the policy half is a real query; the blueprint half is the
//	  derived blueprint_ref query (S48 T6: blueprints are records, and who
//	  uses an agent is SQL, naming every referencing blueprint).
//
// S73 puts a name on every write that passes through here. Each of the six
// write methods — Create, Duplicate, Edit, Delete, Import, Migrate — takes a
// store.AgentActor as its second parameter and refuses an unusable one before
// the store is touched; the store's audited doors then write the `agent` row
// and its `agent_change` ledger row in ONE transaction, so an agent change
// nobody recorded cannot exist (AR1). This manager is where the actor is
// THREADED and not where it is invented (T3 step 6's law, stated where a
// future reader will trip over it): the value arrives from a socket peer
// (internal/protocol's agentWriteEdge stamps it from the connection, never
// from params), from the CLI's own process as `cli:cli`, or from a desk commit
// as the COMMITTER — never the proposal's author. The migrate run carries the
// person's stamp too, by design deviation 3, not a `system:migrate` nobody
// stood behind. The seed engine is deliberately absent from this list: it
// writes through the store's own seed plane (internal/agentseed →
// SeedAgentInsert/Update/Retire) and stamps `system:seed-engine` there, so
// what reaches this package is always a named caller. The reads — View, and
// the paged window the daemon answers through store.AgentPage — carry no
// actor at all: reading is not an act that needs a name (SP6).
//
// It holds no policy of its own: the verdicts are the operator's, surfaced by
// the caller (P31, AM3). The app never judges a signer — it shows the
// verification and the claimed identity and a human decides.

package agentmgr

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/cjlapao/aeon/internal/agentcard"
	"github.com/cjlapao/aeon/internal/handler"
	"github.com/cjlapao/aeon/internal/store"
)

// Manager is the one agent-management implementation. Store is the row layer;
// Verifier is the (stdlib-only, AM3) Ed25519-over-X.509 card verification;
// BlueprintRefs is the injected blueprint-reference READ — since S48 T6 the
// wiring answers it with store.BlueprintRefsFor("agent", slug) over the
// DERIVED blueprint_ref table (the domain still does not own the blueprint
// store; the seam keeps it that way). AllowUnsignedImport is the
// daemon-level `agents.allowUnsignedImport` setting read PER import call, never
// a value captured at construction (CS7/CS11, S44 T10). A nil seam refuses —
// an unwired Manager never silently permits an unsigned card.
type Manager struct {
	Store               *store.Store
	Verifier            Verifier
	BlueprintRefs       func(slug string) ([]string, error)
	AllowUnsignedImport func(context.Context) bool
}

// Verifier is the card-verification seam — agentcard.VerifyCardSignature in
// production, a test double in the suites. It returns the three-state honesty
// (verified / tampered / unsigned) the import must give.
type Verifier func(card, certPEM []byte, sig []byte) (*agentcard.CardSignature, error)

// CardFile is the importable/exportable card on disk — S43's SignedAgentCard:
// the card's exact content, the Ed25519 signature over it, and the signer's
// X.509 certificate (PEM). A card that predates S43 has no Signature and no
// Certificate (unsigned).
type CardFile struct {
	agentcard.SignedAgentCard
}

// NewManager wires the one Manager. A nil Verifier is refused at import time
// (a verification seam with no implementation is a wiring error, not a pass).
// allowUnsigned is the per-import-call read of `agents.allowUnsignedImport`;
// nil (an unwired daemon) refuses unsigned imports — the default.
func NewManager(s *store.Store, v Verifier, blueprintRefs func(string) ([]string, error), allowUnsigned func(context.Context) bool) *Manager {
	return &Manager{
		Store:               s,
		Verifier:            v,
		BlueprintRefs:       blueprintRefs,
		AllowUnsignedImport: allowUnsigned,
	}
}

// importAllowUnsigned is the read site's one expression (S44 T10, CS7/CS11): it
// reads the setting live on each import call, never a value captured at
// construction. A nil seam (unwired) refuses — an import is never silently
// permissive (P34).
func (m *Manager) importAllowUnsigned(ctx context.Context) bool {
	if m.AllowUnsignedImport == nil {
		return false
	}
	return m.AllowUnsignedImport(ctx)
}

// There is no `List` here, and S73 T2 deleted the one there was.
//
// Its only caller was the `case "agent.list":` branch of the management seam,
// which never ran: `agent.list` is registered on the ROSTER seam
// (protocol.handleAgentList → Table.AgentRoster → cmd/aeon's agentRoster), and
// a method has one registration. A manager method that answers the busiest read
// on the plane by returning every row — prompts, skills, metadata and all — was
// the second answer PG1's convention exists to retire, and AR5's rule (the
// management window ships nine columns, not the row) is exactly what it
// contradicted. The two honest reads are `agent.list` for identity and
// `agent.rows` for the table; a third would be a fork.

// CreateInput is everything a new agent can be given when it is made. Only the name is required;
// every other part has the default a bare agent always had, so a caller that sends a name alone
// (the CLI, an older client) gets exactly what it did before.
type CreateInput struct {
	Name        string
	Description string
	// Functionality is the free-text "what it does" line cards carry. Nothing dispatches on it.
	Functionality string
	// Prompt is the instructions the agent runs with.
	Prompt string
	// Tech are the technologies the agent works in, from the closed vocabulary.
	Tech []string
	// Tools are the registered tools the agent may ask for.
	Tools []string
	// AvatarColor is the minion's colour: a colour name or a #rrggbb value. Empty is the default.
	AvatarColor string
	// AvatarMinion is which minion character the agent wears: pip, bop or noodle. Empty leaves it to
	// be drawn from a hash of the agent's name, as it always was.
	AvatarMinion string
	// Handoff is what the agent must report when it finishes: field name → purpose, a "?" on the
	// name marking it optional. Empty leaves the agent with no declared fields.
	Handoff map[string]string
}

// minionCharacters are the characters an agent may wear — the app's own built-ins.
var minionCharacters = map[string]bool{"pip": true, "bop": true, "noodle": true}

// avatarColor admits a colour word or a hex value and nothing else: it is painted into a style,
// so it must not carry markup or a url.
var avatarColorPattern = regexp.MustCompile(`^([a-z]{3,16}|#[0-9a-fA-F]{6})$`)

// Create makes a brand-new agent from the UI (AM1). Its slug is generated from
// its name (AM2); a slug that collides is refused, never silent-suffixed; the
// new row is non-internal (a UI-created agent is the operator's own, editable).
// The tech tags and tools it is given are checked against what the daemon
// compiled — a tag or a tool nobody has is refused by name, never dropped.
func (m *Manager) Create(ctx context.Context, actor store.AgentActor, in CreateInput) (store.Agent, error) {
	if err := actor.Valid(); err != nil {
		return store.Agent{}, err
	}
	slug := store.Slugify(in.Name)
	if slug == "" {
		return store.Agent{}, fmt.Errorf("refused: a name that slugifies to nothing cannot make a slug")
	}
	a := store.Agent{
		Slug:          slug,
		Name:          in.Name,
		Description:   in.Description,
		Functionality: in.Functionality,
		Prompt:        in.Prompt,
		Version:       "1",
		Internal:      false,
	}
	if color := strings.TrimSpace(in.AvatarColor); color != "" {
		if !avatarColorPattern.MatchString(color) {
			return store.Agent{}, fmt.Errorf("refused: avatar colour %q is not a colour name or a #rrggbb value", color)
		}
		a.AvatarColor = color
	}
	if len(in.Tech) > 0 {
		clean, err := handler.NormalizeTech(in.Tech)
		if err != nil {
			return store.Agent{}, fmt.Errorf("refused: %w", err)
		}
		if a.Metadata, err = store.WithTech("", clean); err != nil {
			return store.Agent{}, err
		}
	}
	if minion := strings.TrimSpace(in.AvatarMinion); minion != "" {
		if !minionCharacters[minion] {
			return store.Agent{}, fmt.Errorf("refused: minion %q is not one of pip, bop, noodle", minion)
		}
		var err error
		if a.Metadata, err = store.WithMinion(a.Metadata, minion); err != nil {
			return store.Agent{}, err
		}
	}
	if len(in.Tools) > 0 {
		clean, err := handler.NormalizeTools(in.Tools)
		if err != nil {
			return store.Agent{}, fmt.Errorf("refused: %w", err)
		}
		raw, err := json.Marshal(clean)
		if err != nil {
			return store.Agent{}, err
		}
		a.Tools = string(raw)
	}
	if len(in.Handoff) > 0 {
		clean, err := normalizeHandoff(in.Handoff)
		if err != nil {
			return store.Agent{}, err
		}
		a.Handoff = clean
	}
	if err := m.Store.CreateAgentAudited(ctx, actor, a, m.now()); err != nil {
		return store.Agent{}, err
	}
	return a, nil
}

// Import imports a card from a file (AM3), through ImportCard: it verifies the card (tamper /
// unsigned), maps it to an agent, and stores it idempotently by (slug,
// version). A tampered card is refused, named. An unsigned card is refused by
// default; behind the override it is imported marked `unsigned`.
func (m *Manager) Import(ctx context.Context, actor store.AgentActor, path string, opts ...ImportOptions) (store.Agent, store.ImportResult, error) {
	if err := actor.Valid(); err != nil {
		return store.Agent{}, store.ImportResultRefused, err
	}
	raw, err := readCardFile(path)
	if err != nil {
		return store.Agent{}, store.ImportResultRefused, err
	}
	return m.ImportCard(ctx, actor, raw, opts...)
}

// ImportOptions are the per-import choices a caller can make.
type ImportOptions struct {
	// AcceptUnsigned admits an unsigned card for this import alone, marked unsigned. It is the
	// person's explicit yes (the app's confirmation dialog), never a default.
	AcceptUnsigned bool
}

// ImportCard imports a card whose bytes the caller already holds — the same
// verification, the same idempotent store, the same refusals as Import, minus
// the file read. A client that has the card in hand (a window with a dropped
// file) sends its contents; the daemon never has to be handed a path on its own
// machine. An empty card is refused the way an empty file is.
func (m *Manager) ImportCard(ctx context.Context, actor store.AgentActor, raw []byte, opts ...ImportOptions) (store.Agent, store.ImportResult, error) {
	var opt ImportOptions
	if len(opts) > 0 {
		opt = opts[0]
	}
	if err := actor.Valid(); err != nil {
		return store.Agent{}, store.ImportResultRefused, err
	}
	if len(raw) == 0 {
		return store.Agent{}, store.ImportResultRefused, fmt.Errorf("refused: the card is empty")
	}
	sc, err := loadSignedAgentCard(raw)
	if err != nil {
		return store.Agent{}, store.ImportResultRefused, err
	}
	// Verify — the three-state honesty (P31/AM3).
	sig, verr := m.verify(sc)
	if verr != nil {
		return store.Agent{}, store.ImportResultRefused, verr
	}
	switch sig.State {
	case agentcard.CardStateVerified:
		// A verified card: import it, carrying the signature and the
		// certificate so the row can show who signed it.
		a, err := agentFromCard(sc)
		if err != nil {
			return store.Agent{}, store.ImportResultRefused, err
		}
		a.Signature = sc.Signature
		a.Certificate = sc.Certificate
		a.Signed = true
		return m.storeImport(ctx, actor, a)
	case agentcard.CardStateUnsigned:
		// Admitted by the operator's standing setting, or by this person saying so for THIS import —
		// the app asks them to confirm, and the confirmation is the AcceptUnsigned they send. Either
		// way the row is marked unsigned, and stays marked.
		if !opt.AcceptUnsigned && !m.importAllowUnsigned(ctx) {
			return store.Agent{}, store.ImportResultRefused, fmt.Errorf("refused: %s is unsigned, and unsigned imports are refused by default — confirm the import to admit it marked unsigned, or set agents.allowUnsignedImport", slugOfCard(sc))
		}
		a, err := agentFromCard(sc)
		if err != nil {
			return store.Agent{}, store.ImportResultRefused, err
		}
		a.Signed = false
		return m.storeImport(ctx, actor, a)
	case agentcard.CardStateTampered:
		return store.Agent{}, store.ImportResultRefused, fmt.Errorf("%s", sig.Detail)
	}
	return store.Agent{}, store.ImportResultRefused, fmt.Errorf("refused: unrecognised card verification state %q", sig.State)
}

// storeImport is the (slug, version) idempotent half of import (AM3), plus the
// agent's slug from the card's own Slug (never regenerated — that is the
// point of re-importing the SAME agent at a new version) or, for a card that
// predates S43, derived from its name.
func (m *Manager) storeImport(ctx context.Context, actor store.AgentActor, a store.Agent) (store.Agent, store.ImportResult, error) {
	// Whatever the card says, a row that arrived through import is marked as imported: where it came from is the daemon's fact.
	a.Origin = "import"
	if a.Slug == "" {
		a.Slug = store.Slugify(a.Name)
		if a.Slug == "" {
			return store.Agent{}, store.ImportResultRefused, fmt.Errorf("refused: the card has no slug and its name slugifies to nothing")
		}
	}
	meta, err := checkTech(a.Metadata)
	if err != nil {
		return store.Agent{}, store.ImportResultRefused, err
	}
	a.Metadata = meta
	res, err := m.Store.ImportAgentCardAudited(ctx, actor, a)
	if err != nil {
		return store.Agent{}, store.ImportResultRefused, err
	}
	out, found, err := m.Store.AgentBySlug(a.Slug)
	if err != nil {
		return store.Agent{}, store.ImportResultRefused, err
	}
	if !found {
		return store.Agent{}, store.ImportResultRefused, fmt.Errorf("refused: %s was not stored", a.Slug)
	}
	return out, res, nil
}

// Duplicate copies an agent into a new, editable, non-internal row with a slug
// freshly generated from the COPY's name (AM1: internal refuses edit/delete
// but not duplication). A slug that collides is refused, never suffixed (AM2).
func (m *Manager) Duplicate(ctx context.Context, actor store.AgentActor, sourceSlug, copyName string) (store.Agent, error) {
	if err := actor.Valid(); err != nil {
		return store.Agent{}, err
	}
	src, found, err := m.Store.AgentBySlug(sourceSlug)
	if err != nil {
		return store.Agent{}, err
	}
	if !found {
		return store.Agent{}, fmt.Errorf("refused: %s has no row", sourceSlug)
	}
	return m.Store.DuplicateAgentAudited(ctx, actor, src, copyName, m.now())
}

// Edit writes a content change to an agent — any field except slug and id
// (AM2: both immutable). An internal row refuses (AM1: internal is the
// first-party guard, not a class).
func (m *Manager) Edit(ctx context.Context, actor store.AgentActor, slug string, fields store.Agent, now string) error {
	if err := actor.Valid(); err != nil {
		return err
	}
	a, found, err := m.Store.AgentBySlug(slug)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("refused: %s has no row", slug)
	}
	if a.Internal {
		return fmt.Errorf("refused: %s is internal (first-party) — internal agents refuse edits; duplicate it to make an editable copy", slug)
	}
	a.Name = fields.Name
	a.Description = fields.Description
	a.Functionality = fields.Functionality
	a.Prompt = fields.Prompt
	a.Skills = fields.Skills
	if fields.Metadata, err = checkTech(fields.Metadata); err != nil {
		return err
	}
	a.Metadata = fields.Metadata
	a.AvatarType = fields.AvatarType
	a.AvatarColor = fields.AvatarColor
	a.AvatarMood = fields.AvatarMood
	// Tools: a caller that names none (an old client, the CLI) leaves the allow-list as it is; a
	// caller that names the list — even an empty one, "[]" — sets it, checked against the registry.
	if strings.TrimSpace(fields.Tools) == "" {
		fields.Tools = a.Tools
	} else if fields.Tools, err = normalizeToolsJSON(fields.Tools); err != nil {
		return err
	}
	a.Tools = fields.Tools
	// Handoff, the same way: none named keeps it, "{}" clears it, a map is checked and set.
	if strings.TrimSpace(fields.Handoff) == "" {
		fields.Handoff = a.Handoff
	} else if fields.Handoff, err = normalizeHandoffJSON(fields.Handoff); err != nil {
		return err
	}
	a.Handoff = fields.Handoff
	n, err := m.Store.UpdateAgentAudited(ctx, actor, a, now)
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("refused: %s vanished between read and write — re-read and decide again", slug)
	}
	return nil
}

// Delete removes an agent, refusing if a blueprint, a policy row, or (since
// S46 T5) an MCP row still references its slug (AM4: a delete that orphans a
// live reference is refused and names it). The policy half is a real query;
// the blueprint half — S48 T6: a real query over the derived blueprint_ref
// table, naming every blueprint that still uses the agent —; the MCP half — added by S46 as ONE accessor
// beside the other two — names every mcp_server it owns, every declaration
// it holds, and its own mcp_agent narrowing. S43's refusals are untouched;
// this is one more case, equally named (an orphan or a raw FK error is the
// failure this prevents).
func (m *Manager) Delete(ctx context.Context, actor store.AgentActor, slug string) error {
	if err := actor.Valid(); err != nil {
		return err
	}
	a, found, err := m.Store.AgentBySlug(slug)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("refused: %s has no row to delete", slug)
	}
	if a.Internal {
		return fmt.Errorf("refused: %s is internal (first-party) — internal agents refuse deletion", slug)
	}
	// The two halves of AM4, both named refusals, not a silent gap.
	if refs, err := m.Store.AgentReferencedByPolicy(slug); err != nil {
		return err
	} else if len(refs) > 0 {
		return fmt.Errorf("refused: delete of %s — %d policy row(s) still reference it (%s); remove or re-point them first (AM4)", slug, len(refs), refs[0])
	}
	// S48 T6: the blueprint half of AM4 moved from the text scan of pinned
	// loop content to the DERIVED table — `blueprint_ref` (kind agent) is
	// rebuilt from current content on every write (BP11), so the refusal
	// names every BLUEPRINT that still uses the agent, not loop ids. One
	// changed branch; S43's refusals above and below are untouched.
	if m.BlueprintRefs != nil {
		if refs, err := m.BlueprintRefs(slug); err != nil {
			return err
		} else if len(refs) > 0 {
			return fmt.Errorf("refused: delete of %s — %d blueprint(s) still reference it (%s); remove or re-point them first (AM4)", slug, len(refs), strings.Join(refs, ", "))
		}
	}
	// S46 T5: the third AM4 half. An agent that OWNS a private server,
	// HOLDS declarations, or has a core narrowing is still a live part of
	// the MCP configuration — deleting it would orphan rows the MCP tables
	// have no CASCADE for by design (declarations are history-bearing).
	// The refusal names every reference, so the operator knows exactly
	// which doors to close first (remove server / revoke / core-set).
	if refs, err := m.Store.McpReferencesFor(ctx, slug); err != nil {
		return err
	} else if len(refs) > 0 {
		// The agent's OWN mcp_agent row is not a live reference while it
		// merely records the whole-core default (core NULL): it is deleted
		// with the agent below. A narrowing (core NOT NULL) IS a decision
		// worth naming. Servers owned and declarations held always block.
		var coreNarrowed bool
		if all, aerr := m.Store.McpRows(ctx); aerr != nil {
			return aerr
		} else {
			for _, ag := range all.Agents {
				if ag.AgentSlug == slug && ag.Core != nil {
					coreNarrowed = true
				}
			}
		}
		blocked := make([]string, 0, len(refs))
		for _, r := range refs {
			if r.Kind == "agent" && !coreNarrowed {
				continue
			}
			blocked = append(blocked, fmt.Sprintf("%s %s", r.Kind, r.Name))
		}
		if len(blocked) > 0 {
			return fmt.Errorf("refused: delete of %s — %d MCP row(s) still reference it (%s); remove the server, revoke the declarations, or clear the narrowing first (S46, AM4 carried to the MCP tables)", slug, len(blocked), strings.Join(blocked, ", "))
		}
	}
	// S52 LM7: the fourth live-reference half, and the sharpest — an agent
	// that has LEARNED things still owns memory. Deleting the row would
	// leave agent-scoped learnings whose scope_key names nobody, memory no
	// board and no query can ever attribute again. The refusal counts the
	// WHOLE scope — active and retracted both (a retracted learning is
	// still a row, still the agent's, still not a stranger's to inherit) —
	// and names the number, so the operator knows how many retracts (or
	// how much deliberate adoption) come first. There is no cascade:
	// memory is never deleted by proxy (LM2).
	if n, err := m.Store.LearningsScopedTo(slug); err != nil {
		return err
	} else if n > 0 {
		return fmt.Errorf("refused: delete of %s — %d learning(s) are still scoped to it (retracted ones count: they are still its memory, not nobody's); retract them through learnings.retract or move the decision deliberately, then delete (S52 LM7)", slug, n)
	}
	// The bare narrowing row, if any, leaves with its agent (S46 T5).
	if err := m.Store.McpWithTx(ctx, func(tx *sql.Tx) error {
		_, err := store.McpAgentDeleteTx(ctx, tx, slug)
		return err
	}); err != nil {
		return err
	}
	return m.Store.DeleteAgentAudited(ctx, actor, slug, m.now())
}

// View is the agent plus its content — what the settings page shows for a
// selected agent (AM5: the content view is a daemon read, not a second store).
func (m *Manager) View(ctx context.Context, slug string) (store.Agent, bool, error) {
	return m.Store.AgentBySlug(slug)
}

// Migrate back-fills the agent table from the plugin_agent lifecycle rows
// (T2/T3's name→slug half): every plugin_agent that has no agent row gets one
// derived from its name. It is idempotent (re-running it makes no new rows)
// and refuses, named, a name that slugifies to nothing (DB19: never a
// silent gap). A slug that collides is skipped, named — never suffixed (AM2).
// Then it re-points every policy row that still targets an agent NAME at the
// agent's SLUG (AM2), all-or-nothing on an unresolved name (the T2 call).
// It returns how many agent rows it created.
//
// S73 (design deviation 3): the ledger row this mints carries the PERSON who
// ran it, not `system:migrate`. Measured, so the tempting constant has its
// answer where it is tempting: the only non-test caller of this method in the
// tree is `cmd/aeon/agent_management.go`'s `agent.migrate` case — a human door
// (surface_table_agents.go marks the family HumanDoorOnly), so the peer the
// protocol stamped IS a person, and a run has no idea worth laundering behind
// a machine name. A `system:migrate` actor would be the audit saying "nobody
// did this".
//
// The policy re-point below is deliberately OUTSIDE the audited transaction:
// `agent_change` records changes to agent CONTENT (AR1's set), and the
// name→slug sweep moves `policy` rows, which are that plane's own business.
// The honest consequence is stated rather than hidden: if the sweep refuses
// after the back-fill committed, the ledger row stands for the content change
// that did happen, and this returns its error — the run is not atomic across
// the two planes, and was not before S73 either.
func (m *Manager) Migrate(ctx context.Context, actor store.AgentActor) (int, error) {
	if err := actor.Valid(); err != nil {
		return 0, err
	}
	created, err := m.Store.MigrateFirstPartyAgentsAudited(ctx, actor, m.now())
	if err != nil {
		return created, err
	}
	if _, err := m.Store.MigrateAgentPolicyReferences(); err != nil {
		return created, err
	}
	return created, nil
}

// verify is the card-verification seam call. A nil Verifier is a wiring error,
// refused loudly (AS2: one implementation, no silent pass).
func (m *Manager) verify(sc agentcard.SignedAgentCard) (*agentcard.CardSignature, error) {
	if m.Verifier == nil {
		return nil, fmt.Errorf("refused: the card-verification seam is not wired — a missing verifier is a wiring error, not a pass")
	}
	return m.Verifier(sc.Card, []byte(sc.Certificate), []byte(sc.Signature))
}

func (m *Manager) now() string {
	return m.Store.Now()
}

// Now is the wall clock for an edit's updated_at — exposed so a caller (the
// daemon seam) can pass a stable instant without reaching into the store.
func (m *Manager) Now() string {
	return m.now()
}

func slugOfCard(sc agentcard.SignedAgentCard) string {
	var c struct {
		Slug string `json:"slug"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(sc.Card, &c); err != nil {
		return "(the card is not valid JSON)"
	}
	if c.Slug != "" {
		return c.Slug
	}
	return c.Name
}
