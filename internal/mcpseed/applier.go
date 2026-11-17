package mcpseed

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/cjlapao/aeon/internal/seed"
	"github.com/cjlapao/aeon/internal/store"
)

// ErrConflict is the applier's refusal to overwrite what an operator
// authored (the policyseed ErrConflict's MCP twin): a server row present
// whose origin is not 'system' is the operator's configuration, and a
// seed re-writing it would be the machine editing the human's world — the
// direction MG10 forbids. The engine normally skips a conflict before
// Upsert (Adoptable says a user row is never adoptable — SD9's MCP shape:
// a pre-existing USER server with the seed's name is a CONFLICT, reported,
// never overwritten); the refusal lives here so the misuse is impossible
// rather than merely unplanned.
var ErrConflict = fmt.Errorf("refused: an MCP server row of that name is present and its origin is not system — an operator's configuration is never overwritten by the seed")

// --- record content: the plain local shapes (MC11: NOT the internal/mcp
// types — copied shapes, so deleting that package leaves this file intact).

// serverContent is one mcp.servers record: a server DEFINITION — tier
// public only (a private definition binds an agent, which is a widening
// the inert set may not carry), tools with their classes, and enabled must
// be false. enabled=true or any declaration in this set is a registration
// error: it belongs in mcp.grants (the class gate made concrete).
type serverContent struct {
	Name      string           `json:"name"`
	Tier      string           `json:"tier"`
	Transport transportContent `json:"transport"`
	Tools     []toolContent    `json:"tools"`
	Enabled   bool             `json:"enabled"`
}

type transportContent struct {
	Type    string   `json:"type"` // "stdio" | "http"
	Command string   `json:"command"`
	Args    []string `json:"args"`
	Env     []string `json:"env"` // NAMES only — a value here is refused
	URL     string   `json:"url"`
}

type toolContent struct {
	Name   string `json:"name"`
	Rerun  string `json:"rerun"`
	Effect string `json:"effect"`
}

// grantKind is the closed sum of what an mcp.grants record does. Two
// members, both widening, both guard-class: a declaration and an
// enabled=true flip of a SYSTEM server (MG10's list — nothing else).
type grantKind string

const (
	grantDeclare grantKind = "declare"
	grantEnable  grantKind = "enable"
)

type grantContent struct {
	Kind      grantKind `json:"kind"`
	Agent     string    `json:"agent,omitempty"`
	Qualified string    `json:"qualified,omitempty"`
	Server    string    `json:"server,omitempty"`
}

// declareKey / enableKey are the record keys — stable identities inside
// the set (engine rule 4). A key that does not name the content's own
// fact is refused: the key and the content must name one thing
// (policyseed's id/content law).
func declareKey(agent, qualified string) string { return "declare:" + agent + ":" + qualified }
func enableKey(server string) string            { return "enable:" + server }

// --- the applier ----------------------------------------------------------------

// Applier serves ONE set (the two sets are two appliers — the same
// struct, set-keyed). It holds two doors and no third: the store (the one
// place SQL lives, SD1) for reads and the T1 accessors for writes, and
// the RowValidator seam for the one validator. It holds no cache and no
// goroutine. Like policyseed's it is NOT a seed.Applier directly — the
// engine's Tx seam and *sql.Tx are adapted by the daemon (unwrapSeedTx).
type Applier struct {
	st       *store.Store
	set      string
	validate RowValidator
	now      func() time.Time
}

// NewServersApplier builds the mcp.servers applier (INERT class, MG10:
// definitions nothing may use). validate is the cmd/aeon-supplied seam;
// nil fails closed at apply, never silently at write.
func NewServersApplier(s *store.Store, validate RowValidator, now func() time.Time) *Applier {
	return &Applier{st: s, set: SetServers, validate: validate, now: now}
}

// NewGrantsApplier builds the mcp.grants applier (GUARD class: applying it
// declares a tool to an agent or switches a system server on — it widens
// what a running loop may do, so rule 6 refuses it inert and the engine's
// class gate keeps it off every startup path — D4 by construction).
func NewGrantsApplier(s *store.Store, validate RowValidator, now func() time.Time) *Applier {
	return &Applier{st: s, set: SetGrants, validate: validate, now: now}
}

// stamp renders the clock the way every store timestamp is stored: an
// RFC3339 UTC string (the mcp tables' created_at/updated_at/occurred_at
// columns are TEXT; the clock stays a func for D5's discipline).
func (a *Applier) stamp() string { return a.now().UTC().Format(time.RFC3339) }

// Set names the set this applier serves (rules 6, 7).
func (a *Applier) Set() string { return a.set }

// GuardAffecting is the class split in code: the servers set permits
// nothing (definitions, disabled, undeclared), the grants set widens.
func (a *Applier) GuardAffecting() bool { return a.set == SetGrants }

// Operator is the set's rights over its own system rows (SD4, SD6): no
// EDIT (a hand-edited system row is diverged and skipped, never silently
// corrected — T5 already refuses editing a system row at the write door);
// REMOVE yes (an operator may remove a system server — audited through
// the human door — the engine then reports it removed and never
// resurrects it; seed.restore brings it back).
func (a *Applier) Operator() seed.OperatorRights {
	return seed.OperatorRights{Edit: false, Remove: true}
}

// Fingerprint hashes the LIVE row's content in the record's canonical
// shape. origin is NOT hashed (provenance is not content — the policyseed
// PM15 finding); for the servers set enabled is NOT hashed either: the
// enabled flip belongs to the guard set, and hashing it would read every
// lawful grant-set flip as the inert set's divergence, after which a
// re-apply would either refuse or undo the operator's widening. The
// servers Upsert's update path likewise never touches enabled.
func (a *Applier) Fingerprint(ctx context.Context, tx *sql.Tx, key string) (string, bool, error) {
	switch a.set {
	case SetServers:
		r, present, err := a.st.McpServerGet(ctx, key)
		if err != nil || !present {
			return "", false, err
		}
		h, err := hashContent(contentFromRow(r))
		return h, true, err
	case SetGrants:
		kind, g, err := parseGrantKey(key)
		if err != nil {
			return "", false, err
		}
		all, err := a.st.McpRows(ctx)
		if err != nil {
			return "", false, err
		}
		switch kind {
		case grantDeclare:
			for _, d := range all.Declares {
				if d.AgentSlug == g.Agent && d.Qualified == g.Qualified {
					h, err := hashContent(g)
					return h, true, err
				}
			}
			return "", false, nil
		case grantEnable:
			for _, s := range all.Servers {
				if s.Name == g.Server {
					if !s.Enabled {
						return "", false, nil // the flip is not in force: absent, a create is due
					}
					h, err := hashContent(g)
					return h, true, err
				}
			}
			return "", false, nil
		}
		return "", false, fmt.Errorf("mcpseed: grant key %q names no kind", key)
	}
	return "", false, fmt.Errorf("mcpseed: applier serves unknown set %q", a.set)
}

// Adoptable is SD9's MCP shape, and it differs from policyseed's on
// purpose: a pre-existing row is adoptable ONLY when it is a system row
// whose content already matches (nothing writes it twice). A USER row is
// never adoptable — MG10/T10 behaviour 5: a pre-existing user server with
// the seed's name is a CONFLICT (the engine reports it; the row is never
// overwritten). A declare/enable already in force with matching content
// is adoptable (the world already says yes; no re-write).
func (a *Applier) Adoptable(ctx context.Context, tx *sql.Tx, key string) (bool, error) {
	switch a.set {
	case SetServers:
		r, present, err := a.st.McpServerGet(ctx, key)
		if err != nil || !present {
			return false, err
		}
		if r.Origin != "system" {
			return false, nil // a user server with the seed's name: conflict, reported
		}
		h, err := hashContent(contentFromRow(r))
		if err != nil {
			return false, err
		}
		for _, rec := range ServersRecords() {
			if rec.Key == key {
				return h == seed.Hash(rec.Content), nil
			}
		}
		return false, nil
	case SetGrants:
		_, _, _, present, err := a.grantPresent(ctx, key)
		return present, err
	}
	return false, nil
}

// grantPresent reads whether a grant record's fact is already in force.
func (a *Applier) grantPresent(ctx context.Context, key string) (grantKind, grantContent, *store.McpAll, bool, error) {
	kind, g, err := parseGrantKey(key)
	if err != nil {
		return "", g, nil, false, err
	}
	all, err := a.st.McpRows(ctx)
	if err != nil {
		return kind, g, nil, false, err
	}
	switch kind {
	case grantDeclare:
		for _, d := range all.Declares {
			if d.AgentSlug == g.Agent && d.Qualified == g.Qualified {
				return kind, g, &all, true, nil
			}
		}
	case grantEnable:
		for _, s := range all.Servers {
			if s.Name == g.Server && s.Enabled {
				return kind, g, &all, true, nil
			}
		}
	}
	return kind, g, &all, false, nil
}

// Upsert writes the record's row — the candidate through the ONE validator
// (via the seam) BEFORE anything is written, the whole record and its
// mcp_change audit row in the same transaction (SD3: row and ledger commit
// together; a refusal leaves NOTHING).
func (a *Applier) Upsert(ctx context.Context, tx *sql.Tx, r seed.Record, adopt bool) error {
	if a.validate == nil {
		return fmt.Errorf("mcpseed: refusing to apply %s records — no validator seam was supplied (the one validator gates every record; nil fails closed)", a.set)
	}
	all, err := a.st.McpRows(ctx)
	if err != nil {
		return err
	}
	switch a.set {
	case SetServers:
		c, err := decodeServer(r)
		if err != nil {
			return err
		}
		if c.Tier != "public" {
			return fmt.Errorf("mcpseed: record %q carries tier %q — the inert set ships PUBLIC definitions only; binding a server to an agent is a widening and belongs in %s", r.Key, c.Tier, SetGrants)
		}
		if c.Enabled {
			return fmt.Errorf("mcpseed: record %q carries enabled=true — an enabled server widens what a loop may do; the inert set refuses it (MG10: it belongs in %s)", r.Key, SetGrants)
		}
		if c.Name != r.Key {
			return fmt.Errorf("mcpseed: record %q carries content for server %q — the key and the content must name one server", r.Key, c.Name)
		}
		cur, present, err := a.st.McpServerGet(ctx, r.Key)
		if err != nil {
			return err
		}
		if present && !adopt && cur.Origin != "system" {
			return ErrConflict
		}
		cand, err := a.overlay(ctx, all, c, "", grantContent{})
		if err != nil {
			return err
		}
		if err := a.validate(ctx, cand); err != nil {
			return err // the validator's sentence, verbatim
		}
		at := a.stamp()
		if present {
			if err := store.McpUpdateServerTx(ctx, tx, serverRowFromContent(c, at)); err != nil {
				return err
			}
		} else {
			if _, err := store.McpAddServerTx(ctx, tx, serverRowFromContent(c, at)); err != nil {
				return err
			}
		}
		return a.auditTx(ctx, tx, store.McpChange{
			Op:         "seed",
			Server:     &c.Name,
			Summary:    fmt.Sprintf("seed %s apply %s (v%d)", a.set, r.Key, r.Version), // ids-only
			ActorKind:  "system",
			ActorID:    "seed-engine",
			OccurredAt: at,
		})
	case SetGrants:
		kind, g, err := decodeGrant(r)
		if err != nil {
			return err
		}
		if want := grantKeyOf(kind, g); want != r.Key {
			return fmt.Errorf("mcpseed: record %q carries content for %q — the key and the content must name one grant", r.Key, want)
		}
		cand, err := a.overlay(ctx, all, serverContent{}, kind, g)
		if err != nil {
			return err
		}
		if err := a.validate(ctx, cand); err != nil {
			return err
		}
		at := a.stamp()
		switch kind {
		case grantDeclare:
			if _, err := store.McpDeclareTx(ctx, tx, g.Agent, g.Qualified); err != nil {
				return err
			}
			return a.auditTx(ctx, tx, store.McpChange{
				Op:         "seed",
				AgentSlug:  &g.Agent,
				Qualified:  &g.Qualified,
				Summary:    fmt.Sprintf("seed %s apply %s (v%d)", a.set, r.Key, r.Version),
				ActorKind:  "system",
				ActorID:    "seed-engine",
				OccurredAt: at,
			})
		case grantEnable:
			srv, present, err := a.st.McpServerGet(ctx, g.Server)
			if err != nil {
				return err
			}
			if !present {
				return fmt.Errorf("mcpseed: enable record %q names server %q which does not exist — the seed never fabricates a server (zero-fabrication, MG3's law)", r.Key, g.Server)
			}
			if srv.Origin != "system" {
				return fmt.Errorf("mcpseed: enable record %q names an %s server — the guard set flips SYSTEM servers only", r.Key, srv.Origin)
			}
			if _, err := store.McpSetEnabledTx(ctx, tx, g.Server, true, at); err != nil {
				return err
			}
			return a.auditTx(ctx, tx, store.McpChange{
				Op:         "seed",
				Server:     &g.Server,
				Summary:    fmt.Sprintf("seed %s apply %s (v%d)", a.set, r.Key, r.Version),
				ActorKind:  "system",
				ActorID:    "seed-engine",
				OccurredAt: at,
			})
		}
		return fmt.Errorf("mcpseed: grant record %q carries unknown kind %q", r.Key, kind)
	}
	return fmt.Errorf("mcpseed: applier serves unknown set %q", a.set)
}

// Retire flags the row as retired — its record left the code — keeping the
// row (never deleted, SD6). The MCP tables carry no retired column (the
// ledger's state IS the flag the engine reads; policyseed's Retire is the
// same no-op for the same reason), so this writes nothing: the row rides
// on, which is the whole content of "kept".
func (a *Applier) Retire(_ context.Context, _ *sql.Tx, _ string) error { return nil }

// auditTx writes the record's mcp_change row — behaviour 6: every apply
// leaves the domain's own audit beside the engine's seed_event, ids-only
// summary, actor system/seed-engine (the machine applied it; a person
// opened the guard door).
func (a *Applier) auditTx(ctx context.Context, tx *sql.Tx, c store.McpChange) error {
	return store.McpChangeInsertTx(ctx, tx, c)
}

// --- decode + key helpers --------------------------------------------------------

func decodeServer(r seed.Record) (serverContent, error) {
	var c serverContent
	if err := json.Unmarshal(r.Content, &c); err != nil {
		return c, fmt.Errorf("mcpseed: record %q carries content this set cannot decode: %v", r.Key, err)
	}
	return c, nil
}

func decodeGrant(r seed.Record) (grantKind, grantContent, error) {
	var g grantContent
	if err := json.Unmarshal(r.Content, &g); err != nil {
		return "", g, fmt.Errorf("mcpseed: record %q carries content this set cannot decode: %v", r.Key, err)
	}
	switch g.Kind {
	case grantDeclare:
		if g.Agent == "" || g.Qualified == "" {
			return "", g, fmt.Errorf("mcpseed: declare record %q is missing agent or qualified tool", r.Key)
		}
	case grantEnable:
		if g.Server == "" {
			return "", g, fmt.Errorf("mcpseed: enable record %q is missing server", r.Key)
		}
	default:
		return "", g, fmt.Errorf("mcpseed: grant record %q carries unknown kind %q (declare | enable — nothing else widens)", r.Key, g.Kind)
	}
	return g.Kind, g, nil
}

func grantKeyOf(kind grantKind, g grantContent) string {
	switch kind {
	case grantDeclare:
		return declareKey(g.Agent, g.Qualified)
	case grantEnable:
		return enableKey(g.Server)
	}
	return "unknown:" + string(kind)
}

func parseGrantKey(key string) (grantKind, grantContent, error) {
	for i := 0; i < len(key); i++ {
		if key[i] == ':' {
			switch key[:i] {
			case "declare":
				rest := key[i+1:]
				for j := 0; j < len(rest); j++ {
					if rest[j] == ':' {
						return grantDeclare, grantContent{Kind: grantDeclare, Agent: rest[:j], Qualified: rest[j+1:]}, nil
					}
				}
				return "", grantContent{}, fmt.Errorf("mcpseed: declare key %q must be declare:AGENT:SERVER.TOOL", key)
			case "enable":
				return grantEnable, grantContent{Kind: grantEnable, Server: key[i+1:]}, nil
			}
			break
		}
	}
	return "", grantContent{}, fmt.Errorf("mcpseed: grant key %q names no kind (declare:AGENT:SERVER.TOOL | enable:SERVER)", key)
}

// contentFromRow maps a live server row back onto the canonical content —
// the inverse of what the source wrote, the move that makes Fingerprint
// hash the same bytes. Enabled is reported false always: the flip is the
// guard set's fact, not this set's content (see Fingerprint). origin is
// not content (PM15).
func contentFromRow(r store.McpServerRow) serverContent {
	c := serverContent{
		Name: r.Name,
		Tier: r.Tier,
		Transport: transportContent{
			Type:    r.TransportType,
			Command: r.Command,
			URL:     r.URL,
		},
		Enabled: false,
	}
	_ = json.Unmarshal([]byte(r.Args), &c.Transport.Args)
	_ = json.Unmarshal([]byte(r.Env), &c.Transport.Env)
	for _, t := range r.Tools {
		c.Tools = append(c.Tools, toolContent{Name: t.Name, Rerun: t.Rerun, Effect: t.Effect})
	}
	// The live rows come back in name order (McpServerGet's ORDER BY); a
	// shipped record therefore DECLARES its tools in name order — the
	// convention that makes the record's hash and the row's fingerprint the
	// same bytes (canonical-at-write, the same stance as the seed's canon
	// for agents). Sorted here so the Fingerprint never depends on the
	// read path's ordering.
	sort.Slice(c.Tools, func(i, j int) bool { return c.Tools[i].Name < c.Tools[j].Name })
	return c
}

func hashContent(v any) (string, error) {
	b, err := seed.Canonical(v)
	if err != nil {
		return "", err
	}
	return seed.Hash(b), nil
}

func jsonMarshalStrings(ss []string) (string, error) {
	if ss == nil {
		ss = []string{}
	}
	b, err := json.Marshal(ss)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
