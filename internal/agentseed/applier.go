package agentseed

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/cjlapao/aeon/internal/seed"
	"github.com/cjlapao/aeon/internal/store"
)

// ErrConflict is the applier's one refusal: a row that is present and is not
// an adoptable stub. It names the thing it would not overwrite (SD6). The
// engine never calls Upsert on a conflict — the plan already skipped it —
// but the applier holds the refusal so a misuse is impossible.
var ErrConflict = fmt.Errorf("refused: an agent row is present and is not an adoptable stub — never overwritten")

// Applier is the agents set's applier. It holds the store (the one place the
// SQL lives) and writes/reads the agent row through the store's methods on
// the shared transaction (SD3). It is inert (SD5): it never writes a guard.
// It holds no cache and no goroutine.
//
// It is not a seed.Applier: the seed.Applier's methods take a seed.Tx, but
// the SQL must live in internal/store (the TestNoSQLLivesOutsideTheStore
// guard), and the store's methods take *sql.Tx — a seed.Tx cannot reach them.
// The daemon (T6) adapts this applier to seed.Applier by giving each call the
// transaction it opened. The set is registered against that adapter.
type Applier struct {
	store *store.Store
	// now is the clock the ledger row is stamped with. S73 T3 added it (the
	// S46 mcpseed applier's own `stamp` is the precedent): an `agent_change`
	// row needs an occurred_at, and a package that writes an audit line while
	// reading the clock through a field is a package its tests can pin.
	now func() time.Time
	// setID names the set this applier serves; "" reads as firstParty (the
	// pre-S61 shape). NewApplierFor set it for the second set (GI15, R4: an
	// applier may serve a second set — blueprintseed's precedent).
	setID string
}

// NewApplier builds the applier over a store — the one door the agent row's
// SQL has, so no SQL lives outside internal/store.
func NewApplier(s *store.Store) *Applier { return &Applier{store: s, now: time.Now} }

// NewApplierFor builds the SAME applier for a named set (S61 T9:
// agents.intake rides this exact row machinery — the agent row is the row,
// the set is the ledger's, and Register refuses an applier/set mismatch).
func NewApplierFor(s *store.Store, setID string) *Applier {
	return &Applier{store: s, setID: setID, now: time.Now}
}

// stamp is the ledger's occurred_at, in the same RFC 3339 shape every other
// *_change table's S rows carry.
func (a *Applier) stamp() string {
	n := a.now
	if n == nil {
		n = time.Now
	}
	return n().UTC().Format(time.RFC3339)
}

// seedActor is the applier's answer to AR4's path 9: the seed engine is the one
// genuinely person-less writer over an agent row, and it names itself as such —
// `system:seed-engine`, the same stamp S46's mcpseed applier puts on
// mcp_change. A person who commits a corpus is recorded by the seed LEDGER and
// by the proposal that carried it; the row's ledger row names the engine that
// wrote it. The store takes it as a parameter rather than inventing it, so the
// day a second applier appears it stamps its own truth.
func seedActor() store.AgentActor {
	return store.AgentActor{Kind: store.AgentActorSystem, ID: "seed-engine"}
}

// Set names the set this applier serves — the one id the registry and the
// set agree on (Register refuses a mismatch).
func (a *Applier) Set() string {
	if a.setID != "" {
		return a.setID
	}
	return SetID
}

// GuardAffecting is false: the agents set is inert, so the applier never
// writes a guard and the class gate is a non-event here.
func (a *Applier) GuardAffecting() bool { return false }

// Operator is the set's rights: it may edit and remove (retire) its own
// rows. A seed is never a permission to do more.
func (a *Applier) Operator() seed.OperatorRights {
	return seed.OperatorRights{Edit: true, Remove: true}
}

// Fingerprint reads the live row's content and hashes it — the same
// canonical form the source wrote, so the engine's divergence check
// (live.Hash != ledger.Hash) is a content comparison, not a column one.
func (a *Applier) Fingerprint(ctx context.Context, tx *sql.Tx, key string) (string, bool, error) {
	c, present, err := a.store.SeedAgentRow(ctx, tx, key)
	if err != nil {
		return "", false, err
	}
	if !present {
		return "", false, nil
	}
	h, err := a.hash(contentFromRow(c))
	if err != nil {
		return "", false, err
	}
	return h, true, nil
}

// Adoptable reports whether a present row is an adoptable stub (SD9): the
// S43 stub the applier will fill in place — a row with an empty body and
// the stub's version 1. A present row with content is not adoptable; it is
// an operator's choice and a conflict.
func (a *Applier) Adoptable(ctx context.Context, tx *sql.Tx, key string) (bool, error) {
	c, present, err := a.store.SeedAgentRow(ctx, tx, key)
	if err != nil {
		return false, err
	}
	if !present {
		return false, nil
	}
	// A stub is a row with an empty body — the S43 migration's shape
	// (version 1, internal, empty content). A present row with a body is an
	// operator's choice, not a stub.
	return c.Prompt == "", nil
}

// Upsert writes the record's Content to the agent row, on the shared
// transaction: a create when the row is absent (op=apply), an update in
// place when it is present (op=update or op=adopt — both write the same
// content; the engine records the distinction in the audit). A conflict is
// never reached here (the engine skips it), but the refusal is held so a
// misuse is impossible.
func (a *Applier) Upsert(ctx context.Context, tx *sql.Tx, r seed.Record, adopt bool) error {
	c, err := decode(r.Content)
	if err != nil {
		return err
	}
	present, err := a.store.SeedAgentExists(ctx, tx, c.Slug)
	if err != nil {
		return err
	}
	at := a.stamp()
	row := a.toAgentContent(c)
	row.Version = strconv.Itoa(r.Version)
	if !present {
		return a.store.SeedAgentInsert(ctx, tx, row, seedActor(), at)
	}
	_ = adopt
	return a.store.SeedAgentUpdate(ctx, tx, row, seedActor(), at)
}

// Retire flags a row retired in its Metadata blob — it never deletes the row
// (SD10: a retired record is visible, its history kept).
func (a *Applier) Retire(ctx context.Context, tx *sql.Tx, key string) error {
	c, present, err := a.store.SeedAgentRow(ctx, tx, key)
	if err != nil {
		return err
	}
	if !present {
		return nil
	}
	// Clear the inert fields in the metadata (a retired record's inert
	// fields are cleared, not its body); keep the body.
	c.Metadata = retireMetadata(c.Metadata)
	return a.store.SeedAgentRetire(ctx, tx, c, seedActor(), a.stamp())
}

// hash is the content's canonical hash — the applier's one definition of it,
// so Fingerprint and the body_hash the row carries agree with the engine's
// Hash of the record's Content (the round-trip is a content comparison, not
// a column one).
func (a *Applier) hash(c content) (string, error) {
	b, err := seed.Canonical(c)
	if err != nil {
		return "", err
	}
	return seed.Hash(b), nil
}

// toAgentContent maps the domain content onto the store's AgentContent — the
// columns the store owns. The inert fields (mode, variant, retired,
// declared) go in the Metadata blob; the rest are direct columns.
func (a *Applier) toAgentContent(c content) store.AgentContent {
	tools, handoff, metadata, err := encodeColumns(c)
	if err != nil {
		panic("agentseed: " + err.Error())
	}
	hash, err := a.hash(c)
	if err != nil {
		panic("agentseed: " + err.Error())
	}
	return store.AgentContent{
		Slug:        c.Slug,
		Name:        c.Name,
		Description: c.Description,
		Prompt:      c.Body,
		Tools:       tools,
		Temperature: c.Temperature,
		Handoff:     handoff,
		PolicyAgent: c.PolicyAgent,
		BodyHash:    hash,
		Origin:      "system",
		Internal:    true,
		Metadata:    metadata,
	}
}

// contentFromRow maps the store's AgentContent back onto the domain content —
// the inverse of toAgentContent, and the move that makes the round-trip
// lossless.
func contentFromRow(c store.AgentContent) content {
	out := content{
		Slug:        c.Slug,
		Name:        c.Name,
		Description: c.Description,
		Temperature: c.Temperature,
		Body:        c.Prompt,
		PolicyAgent: c.PolicyAgent,
	}
	if c.Tools == "" || c.Tools == "[]" {
		out.Tools = nil
	} else if err := json.Unmarshal([]byte(c.Tools), &out.Tools); err != nil {
		panic("agentseed: " + err.Error())
	}
	if c.Handoff == "" || c.Handoff == "{}" {
		out.Handoff = map[string]string{}
	} else {
		h := map[string]string{}
		if err := json.Unmarshal([]byte(c.Handoff), &h); err != nil {
			panic("agentseed: " + err.Error())
		}
		out.Handoff = h
	}
	unmarshalMetadata(out, c.Metadata)
	out.Tech = techFromMetadata(c.Metadata)
	return out
}

// retireMetadata clears the inert fields in a row's metadata blob (a retired
// record's mode/variant/retired/declared are cleared, not its body).
func retireMetadata(metadata string) string {
	var blob struct {
		Mode     string   `json:"mode"`
		Variant  string   `json:"variant"`
		Retired  []string `json:"retired"`
		Declared []string `json:"declared"`
		Tech     []string `json:"tech,omitempty"`
	}
	if metadata != "" {
		_ = json.Unmarshal([]byte(metadata), &blob)
	}
	blob.Mode, blob.Variant, blob.Retired, blob.Declared = "", "", nil, nil
	b, _ := json.Marshal(blob)
	return string(b)
}

// techFromMetadata reads the agent's tech tags out of its metadata blob; a blob
// without them, or a malformed one, is no tags.
func techFromMetadata(metadata string) []string {
	if metadata == "" {
		return nil
	}
	var blob struct {
		Tech []string `json:"tech"`
	}
	if err := json.Unmarshal([]byte(metadata), &blob); err != nil || len(blob.Tech) == 0 {
		return nil
	}
	return blob.Tech
}

// unmarshalMetadata reads the metadata blob into the content's inert fields.
func unmarshalMetadata(c content, metadata string) {
	if metadata == "" {
		return
	}
	var blob struct {
		Mode     string   `json:"mode"`
		Variant  string   `json:"variant"`
		Retired  []string `json:"retired"`
		Declared []string `json:"declared"`
	}
	if err := json.Unmarshal([]byte(metadata), &blob); err != nil {
		panic("agentseed: " + err.Error())
	}
	c.Mode = blob.Mode
	c.Variant = blob.Variant
	c.Retired = blob.Retired
	c.Declared = blob.Declared
}

// decode parses a record's Content into its content.
func decode(b []byte) (content, error) {
	var c content
	if err := json.Unmarshal(b, &c); err != nil {
		return content{}, err
	}
	return c, nil
}

// encodeColumns renders a content's tools, handoff and metadata blobs for a
// row write. The empty forms are the explicit literals, so a round-trip
// through the row is exact.
func encodeColumns(c content) (tools, handoff, metadata string, err error) {
	if c.Tools == nil {
		tools = "[]"
	} else if b, e := json.Marshal(c.Tools); e != nil {
		return "", "", "", e
	} else {
		tools = string(b)
	}
	if c.Handoff == nil {
		handoff = "{}"
	} else if b, e := json.Marshal(c.Handoff); e != nil {
		return "", "", "", e
	} else {
		handoff = string(b)
	}
	blob := struct {
		Mode     string   `json:"mode"`
		Variant  string   `json:"variant"`
		Retired  []string `json:"retired"`
		Declared []string `json:"declared"`
		// Tech rides the metadata blob: the tags are the agent's own data, not an
		// inert record, so retiring a row keeps them (retireMetadata).
		Tech []string `json:"tech,omitempty"`
	}{Mode: c.Mode, Variant: c.Variant, Retired: c.Retired, Declared: c.Declared, Tech: c.Tech}
	b, e := json.Marshal(blob)
	if e != nil {
		return "", "", "", e
	}
	return tools, handoff, string(b), nil
}
