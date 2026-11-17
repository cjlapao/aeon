package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/cjlapao/aeon/internal/model"
)

// `route`: the configuration that survives a restart — and not the credential.
//
// # The split, and the rule it comes from
//
// C17 is one credential rule for the whole product: a credential comes from
// **the environment, or a dedicated file path — never a config file, never
// argv, never a log.** So:
//
//	the route        provider, model, base URL, output cap, prices,
//	                 reasoning effort, OpenAI endpoint                →  here
//	the credential   the API key                                     →  a 0600
//	                                                                    file,
//	                                                                    internal/secrets
//
// The key never reaches this package. `model.Route.APIKey` carries `json:"-"`
// and the write path below ignores it — `TestNoAPIKeyEverReachesTheDatabase`
// reads the database's raw bytes and fails if one appears, which is a grep over
// the file rather than a convention.
//
// That split also means **S14 can encrypt those files without a migration**. A
// column would have made it into one.

// Route is a stored route, without its credential.
//
// It is `model.Route` with the key omitted rather than a new type, because the
// caller needs a `model.Route` to make a call — so the conversion happens once,
// here, where the key is attached from the secret store.
type Route = model.Route

// UpsertRoute stores a route's configuration.
//
// The credential is NOT written, whatever the caller put in `APIKey`. The
// parameter is not even read: a function that accepted a key and silently
// dropped it would look like it had stored one.
func (s *Store) UpsertRoute(r model.Route) error {
	if err := r.NormalizeMetadata(); err != nil {
		return err
	}
	if err := r.NormalizeWireCapabilities(); err != nil {
		return err
	}
	if strings.TrimSpace(r.ID) == "" {
		return fmt.Errorf("store: a route needs an id")
	}
	if strings.TrimSpace(r.Provider) == "" || strings.TrimSpace(r.Model) == "" {
		return fmt.Errorf("store: route %q needs a provider and a model", r.ID)
	}

	// # The O8 gate — a missing price must be a DECISION (S18 T2)
	//
	// A route that is neither priced nor declared `noPrice` is refused HERE,
	// at the gate new rows enter, and nowhere else. The asymmetry is the
	// whole design: a silently missing price is how a typo makes a metered
	// model report `unknown` forever, because `unknown` is a legitimate
	// answer (M9) nobody re-checks — so NEW rows must say which of the two
	// happened. Rows written before this rule are UPDATEs past this check
	// (the SELECT below finds them) and keep loading and saving: a daemon
	// must not die on a row it accepted under yesterday's contract, and
	// refusing the read would be rejecting the past rather than protecting
	// the future. Environment routes never reach this function at all —
	// ephemeral configuration is not the ledger.
	if !r.Prices.Any() && !r.NoPrice {
		var one int
		err := s.r.QueryRow(`SELECT 1 FROM route WHERE id = ?`, r.ID).Scan(&one)
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("store: route %q has no price and no noPrice declaration (O8): a "+
				"silently missing price is how a typo makes a metered model report `unknown` forever. "+
				"Price it, or declare it unmetered on purpose", r.ID)
		}
		if err != nil {
			return fmt.Errorf("store: asking whether route %q predates the price rule: %w", r.ID, err)
		}
	}

	var prices any
	if r.Prices != nil {
		encoded, err := json.Marshal(r.Prices)
		if err != nil {
			return fmt.Errorf("store: encode the prices of route %q: %w", r.ID, err)
		}
		prices = string(encoded)
	}
	var extra any
	if len(r.ExtraBody) > 0 {
		extra = string(r.ExtraBody)
	}

	var capabilities, efforts, wireCaps any
	if r.Capabilities != nil {
		data, _ := json.Marshal(r.Capabilities)
		capabilities = string(data)
	}
	if r.ReasoningEfforts != nil {
		data, _ := json.Marshal(r.ReasoningEfforts)
		efforts = string(data)
	}
	if r.WireCapabilities != nil {
		data, _ := json.Marshal(r.WireCapabilities)
		wireCaps = string(data)
	}
	noPrice := 0
	if r.NoPrice {
		noPrice = 1
	}
	now := s.Now()
	_, err := s.w.Exec(`
		INSERT INTO route (id, provider, model, base_url, max_output_tokens, prices, extra_body,
			reasoning_effort, openai_endpoint, audio_api, context_window, display_name, display_description, icon, capabilities, reasoning_efforts, wire_capabilities, no_price, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT (id) DO UPDATE SET
			provider = excluded.provider,
			model = excluded.model,
			base_url = excluded.base_url,
			max_output_tokens = excluded.max_output_tokens,
			prices = excluded.prices,
			extra_body = excluded.extra_body,
			reasoning_effort = excluded.reasoning_effort,
			openai_endpoint = excluded.openai_endpoint,
			audio_api = excluded.audio_api,
			context_window = excluded.context_window,
 display_name = excluded.display_name, display_description = excluded.display_description,
 icon = excluded.icon, capabilities = excluded.capabilities, reasoning_efforts = excluded.reasoning_efforts,
 wire_capabilities = excluded.wire_capabilities, no_price = excluded.no_price,
			updated_at = excluded.updated_at`,
		r.ID, r.Provider, r.Model, nullable(r.BaseURL), r.MaxOutputTokens, prices, extra,
		nullable(r.ReasoningEffort), nullable(r.OpenAIEndpoint), nullable(r.AudioAPI), r.ContextWindow, r.DisplayName, r.DisplayDescription, r.Icon, capabilities, efforts, wireCaps, noPrice, now, now)
	if err != nil {
		return fmt.Errorf("store: save route %q: %w", r.ID, err)
	}
	return nil
}

// ListRoutes reads every stored route, in id order, with no credential.
//
// The caller attaches the key — `internal/daemon` does, from
// `internal/secrets` — because this package must not know what a credential is.
func (s *Store) ListRoutes() ([]model.Route, error) {
	rows, err := s.r.Query(`
		SELECT id, provider, model, COALESCE(base_url,''), max_output_tokens,
		       COALESCE(prices,''), COALESCE(extra_body,''), COALESCE(reasoning_effort,''),
		       COALESCE(openai_endpoint,''), COALESCE(audio_api,''), context_window, display_name, display_description, icon, capabilities, reasoning_efforts,
		       wire_capabilities, no_price
		FROM route ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("store: list routes: %w", err)
	}
	defer rows.Close()

	var out []model.Route
	for rows.Next() {
		r, err := scanRoute(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ReadRoute reads one stored route.
func (s *Store) ReadRoute(id string) (model.Route, error) {
	row := s.r.QueryRow(`
		SELECT id, provider, model, COALESCE(base_url,''), max_output_tokens,
		       COALESCE(prices,''), COALESCE(extra_body,''), COALESCE(reasoning_effort,''),
		       COALESCE(openai_endpoint,''), COALESCE(audio_api,''), context_window, display_name, display_description, icon, capabilities, reasoning_efforts,
		       wire_capabilities, no_price
		FROM route WHERE id = ?`, id)
	r, err := scanRoute(row)
	if errors.Is(err, sql.ErrNoRows) {
		return model.Route{}, fmt.Errorf("store: route %q: %w", id, ErrNotFound)
	}
	return r, err
}

// DeleteRoute removes a stored route, reporting whether one existed.
//
// It does NOT remove the credential: that file belongs to `internal/secrets`,
// and deleting it is the caller's decision in the same breath as this one.
// Splitting it would be worse — a key left behind for a route nobody can call
// is a secret on disk with no owner.
func (s *Store) DeleteRoute(id string) (bool, error) {
	res, err := s.w.Exec(`DELETE FROM route WHERE id = ?`, id)
	if err != nil {
		return false, fmt.Errorf("store: delete route %q: %w", id, err)
	}
	// A route's cached verdicts go with it. A `contradicted` row outliving
	// its route is not evidence about anything — and an orphaned `verified`
	// could be re-adopted by a NEW route reusing the id (S18 T3).
	if _, err := s.w.Exec(`DELETE FROM route_probe WHERE route_id = ?`, id); err != nil {
		return false, fmt.Errorf("store: delete probes of route %q: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("store: delete route %q: %w", id, err)
	}
	return n > 0, nil
}

// --- the probe cache (S18 T3) -------------------------------------------------
//
// `route_probe` holds only the verdicts a probe EARNED. `declared` is the
// absence of a row (I5), which is why there is no PutProbe for it and why
// the schema's CHECK would refuse one: the three states stay derivable and
// the probe has no column to rewrite the declaration into.

// PutProbeResults REPLACES one route's cached verdicts with this probe's.
//
// Replace, not merge: the record must reflect the latest probe, or a
// capability that contradicted in September and verified in October keeps
// refusing steps on a stale claim. Atomicity is the whole safety of that
// choice — a half-applied replace (delete done, insert failed) would wipe
// the contradictions and silently unblock a route that should be refused,
// so it happens in ONE transaction or not at all.
//
// Non-definitive verdicts (`declared`, with the probe's reason) are not
// stored — they ride the response for display and vanish; storing them
// would make absence mean "nobody ever probed" when it meant "the last
// probe could not tell".
func (s *Store) PutProbeResults(routeID string, results []model.ProbeResult) error {
	tx, err := s.w.Begin()
	if err != nil {
		return fmt.Errorf("store: begin probe replace: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM route_probe WHERE route_id = ?`, routeID); err != nil {
		return fmt.Errorf("store: clear probes of route %q: %w", routeID, err)
	}
	for _, p := range results {
		if !model.ValidWireCapability(string(p.Capability)) {
			return fmt.Errorf("store: probe result for route %q names unknown capability %q", routeID, p.Capability)
		}
		if p.State != model.StateVerified && p.State != model.StateContradicted {
			continue // declared is the absence of a row — never a write
		}
		if p.ProbedAt.IsZero() || strings.TrimSpace(p.Model) == "" {
			// O2: a verdict that cannot be aged or attributed must not be
			// cached. Refuse the cache, not the probe — the caller already
			// has the answer to show.
			return fmt.Errorf("store: probe of route %q capability %q has no timestamp or no probed model — refusing to cache an un-ageable verdict", routeID, p.Capability)
		}
		if _, err := tx.Exec(`
INSERT INTO route_probe (route_id, capability, state, detail, probed_model, probed_at)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT (route_id, capability) DO UPDATE SET
  state = excluded.state, detail = excluded.detail,
  probed_model = excluded.probed_model, probed_at = excluded.probed_at`,
			routeID, string(p.Capability), string(p.State), p.Detail, p.Model, p.ProbedAt.UTC().Format(time.RFC3339Nano)); err != nil {
			return fmt.Errorf("store: cache probe verdict %s/%s: %w", routeID, p.Capability, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: commit probe replace for route %q: %w", routeID, err)
	}
	return nil
}

// ProbeResults reads a route's cached verdicts, in canonical flag order.
// A route never probed returns no rows and no error — absence is `declared`.
func (s *Store) ProbeResults(routeID string) ([]model.ProbeResult, error) {
	rows, err := s.r.Query(`
SELECT capability, state, detail, probed_model, probed_at
FROM route_probe WHERE route_id = ?`, routeID)
	if err != nil {
		return nil, fmt.Errorf("store: read probes of route %q: %w", routeID, err)
	}
	defer rows.Close()
	var out []model.ProbeResult
	for rows.Next() {
		var capName, state, detail, probedModel, probedAt string
		if err := rows.Scan(&capName, &state, &detail, &probedModel, &probedAt); err != nil {
			return nil, fmt.Errorf("store: read probes of route %q: %w", routeID, err)
		}
		at, err := time.Parse(time.RFC3339Nano, probedAt)
		if err != nil {
			// A row that cannot be dated is not a verdict — report it rather
			// than render it as a fresh one.
			return nil, fmt.Errorf("store: probe verdict %s/%s carries an unreadable probed_at %q", routeID, capName, probedAt)
		}
		out = append(out, model.ProbeResult{
			Capability: model.Capability(capName),
			State:      model.CapabilityState(state),
			Detail:     detail, ProbedAt: at, Model: probedModel,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: read probes of route %q: %w", routeID, err)
	}
	sortProbeResults(out)
	return out, nil
}

// sortProbeResults puts verdicts in the canonical flag order, never in
// typing or row order — the same discipline WireCapabilityFlags enforces on
// declarations (P31's surfaces must not depend on insertion).
func sortProbeResults(rs []model.ProbeResult) {
	rank := map[string]int{}
	for i, c := range model.WireCapabilityFlags() {
		rank[c] = i
	}
	sort.SliceStable(rs, func(i, j int) bool {
		return rank[string(rs[i].Capability)] < rank[string(rs[j].Capability)]
	})
}

func scanRoute(row interface{ Scan(...any) error }) (model.Route, error) {
	var r model.Route
	var prices, extra string
	var capabilities, efforts, wireCaps sql.NullString
	var noPrice int
	if err := row.Scan(&r.ID, &r.Provider, &r.Model, &r.BaseURL, &r.MaxOutputTokens,
		&prices, &extra, &r.ReasoningEffort, &r.OpenAIEndpoint, &r.AudioAPI, &r.ContextWindow, &r.DisplayName, &r.DisplayDescription, &r.Icon, &capabilities, &efforts, &wireCaps, &noPrice); err != nil {
		return model.Route{}, err
	}
	if capabilities.Valid {
		if err := json.Unmarshal([]byte(capabilities.String), &r.Capabilities); err != nil {
			return r, fmt.Errorf("invalid stored model capabilities: %w", err)
		}
	}
	if efforts.Valid {
		if err := json.Unmarshal([]byte(efforts.String), &r.ReasoningEfforts); err != nil {
			return r, fmt.Errorf("invalid stored reasoning efforts: %w", err)
		}
	}
	// NULL wire_capabilities means UNDECLARED and decodes to nil; the empty
	// JSON array means DECLARED-NONE. nil and [] are different answers, and
	// a DEFAULT '[]' would have erased the distinction at the schema.
	if wireCaps.Valid {
		if err := json.Unmarshal([]byte(wireCaps.String), &r.WireCapabilities); err != nil {
			return r, fmt.Errorf("invalid stored wire capabilities for route %q: %w", r.ID, err)
		}
	}
	r.NoPrice = noPrice != 0
	if prices != "" {
		if err := json.Unmarshal([]byte(prices), &r.Prices); err != nil {
			return model.Route{}, fmt.Errorf("the stored prices of route %q are not valid JSON: %w", r.ID, err)
		}
	}
	if extra != "" {
		r.ExtraBody = json.RawMessage(extra)
	}
	// APIKey is left EMPTY, deliberately. There is nowhere in the database it
	// could have come from.
	return r, nil
}
