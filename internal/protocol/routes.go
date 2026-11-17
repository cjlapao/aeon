package protocol

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"github.com/cjlapao/aeon/internal/model"
)

// # Routes, settable at runtime (S3, amended)
//
// Routes were read from the environment at startup. The UI needs to set them —
// a base URL, a model, a credential — so there are three methods here.
//
// # The one rule this file exists to enforce
//
// **A credential goes IN and never comes OUT.** `SetRouteParams` has an
// `apiKey` field; `RouteView` has a `hasKey` boolean and no key field at all.
// Not a redaction applied on the way out — *a different type*, so returning a
// key is not something a careless change can do. `routes_test.go` asserts the
// view has no field that could carry one.
//
// Three further consequences, each deliberate:
//
//   - **An empty `apiKey` on an existing route keeps the existing key.** So the
//     UI can edit a model name without holding the credential, and never needs
//     to read one back in order to write one.
//   - **Deleting a route forgets its key**, which is the only way to remove one
//     from memory short of restarting.
//   - **Routes PERSIST since S4** — and their credentials do not go in the
//     database. The route's configuration is a row in `route`; the key is a
//     dedicated 0600 file (C17: the environment, or a dedicated path, never a
//     config file). `routes.list` reports `persisted`, and the app shows it,
//     because a settings page that looks like it saved is worse than one that
//     admits it did not. S3 shipped the honest version of that; S4 made it
//     true.

// RouteSource records where a route came from, so a reader can tell a key that
// came from their shell from one they typed.
type RouteSource string

const (
	// RouteSourceEnvironment — derived from a provider key in the environment.
	// The zero-configuration path: it works on a fresh machine and survives a
	// restart because the environment does.
	RouteSourceEnvironment RouteSource = "environment"

	// RouteSourceRuntime — set through `routes.set` on a daemon with no
	// persistence attached. In memory only, and gone on restart. This was every
	// route a client set before S4; it now happens only in tests, because
	// `aeon serve` always attaches the store.
	RouteSourceRuntime RouteSource = "runtime"

	// RouteSourceStored — set through `routes.set` and written through to the
	// store (S4). Survives a restart; its credential lives in a 0600 file
	// beside the store, never in the database.
	RouteSourceStored RouteSource = "stored"
)

// RouteView is what a client is told about a route.
//
// **There is no key field, and that is the design.** A `redactKey()` helper on
// the way out would be one forgotten call away from leaking; a type with
// nowhere to put a key cannot.
type RouteView struct {
	DisplayName        string    `json:"displayName,omitempty"`
	DisplayDescription string    `json:"displayDescription,omitempty"`
	Icon               string    `json:"icon,omitempty"`
	Capabilities       *[]string `json:"capabilities,omitempty"`
	ReasoningEfforts   *[]string `json:"reasoningEfforts,omitempty"`
	ID                 string    `json:"id"`
	Provider           string    `json:"provider"`
	Model              string    `json:"model"`
	BaseURL            string    `json:"baseUrl,omitempty"`

	// HasKey reports whether a credential is set, which is all a client needs
	// to render a settings page honestly.
	HasKey bool `json:"hasKey"`
	// KeyHint is the last four characters, or empty.
	//
	// Four characters cannot reconstruct a key and are enough to tell two
	// apart — which is the actual question somebody has when two environments
	// behave differently. Deliberately not the first four: a prefix identifies
	// the key's *type* and, for some providers, its project.
	KeyHint string `json:"keyHint,omitempty"`

	Source          RouteSource `json:"source"`
	MaxOutputTokens int         `json:"maxOutputTokens"`
	ContextWindow   int         `json:"contextWindow,omitempty"`
	Prices          *PricesView `json:"prices,omitempty"`

	// ReasoningEffort is reported because it is configuration, not a secret —
	// and because a route that cannot run a step needs to say why in the one
	// place somebody looks at routes.
	ReasoningEffort string `json:"reasoningEffort,omitempty"`
	// ExtraBody is the route's extra request fields (compact JSON object), reported because it is configuration and
	// not a secret: it is what explains a model that behaves differently from the plain call.
	ExtraBody string `json:"extraBody,omitempty"`

	// OpenAIEndpoint is reported for the same reason: it decides whether a step
	// on this route can reason at all.
	OpenAIEndpoint string `json:"openaiEndpoint,omitempty"`
	// AudioAPI is reported because it decides which speech endpoints a route is spoken to on.
	AudioAPI string `json:"audioApi,omitempty"`
	// Priced reports whether a cost can be computed at all, so the UI can warn
	// before a call rather than explain an `unknown` cost after one.
	Priced bool `json:"priced"`

	// NoPrice distinguishes the two reasons `Priced` is false (O8): the
	// operator DECLARED the route unmetered, versus a price that is simply
	// absent. Old rows can only ever show `Priced:false, NoPrice:false` —
	// the honest "nobody decided" — and the UI renders that as the warning
	// it is rather than letting it read as a decision.
	NoPrice bool `json:"noPrice,omitempty"`

	// WireCapabilities echoes the route's Q8 wire-behaviour declarations.
	// nil means UNDECLARED, [] means DECLARED-NONE. The three states
	// (declared/verified/contradicted, M28) ride CapabilityStates BELOW —
	// sourced from the probe cache, never from this declaration: a view
	// that showed `verified` without a probe row would be exactly the
	// confident wrong answer P31 exists to prevent.
	WireCapabilities *[]string `json:"wireCapabilities,omitempty"`

	// CapabilityStates carries the cached probe verdicts (S18 T3) — one
	// entry per declared wire flag, so every declared flag has exactly one
	// state on the view. `verified` and `contradicted` are what a probe
	// recorded (with its timestamp and the model it probed, O2);
	// `declared` is the ABSENCE of a verdict row, which is what keeps I5
	// structural: nothing here can make the route claim what only a probe
	// earns. Nil when the daemon has no probe cache — there are no earned
	// verdicts to show, and rendering a state the cache could not back is
	// the same confident wrong answer.
	CapabilityStates []CapabilityStateView `json:"capabilityStates,omitempty"`
}

// CapabilityStateView is one flag's three-state answer on a route view.
// A `declared` entry has no timestamp and no probed model, because the
// probe has never earned an opinion about it — the absence is the fact.
type CapabilityStateView struct {
	Capability  string `json:"capability"`
	State       string `json:"state"`
	Detail      string `json:"detail,omitempty"`
	ProbedAt    string `json:"probedAt,omitempty"`
	ProbedModel string `json:"probedModel,omitempty"`
}

// PricesView mirrors model.Prices with the unit in every name, because a UI
// field labelled "input price" is the ambiguity M25 exists to prevent.
type PricesView struct {
	InputUSDPerMTok      *float64 `json:"inputUsdPerMTok,omitempty"`
	OutputUSDPerMTok     *float64 `json:"outputUsdPerMTok,omitempty"`
	CacheReadUSDPerMTok  *float64 `json:"cacheReadUsdPerMTok,omitempty"`
	CacheWriteUSDPerMTok *float64 `json:"cacheWriteUsdPerMTok,omitempty"`
}

func viewOf(r model.Route, source RouteSource) RouteView {
	v := RouteView{
		DisplayName: r.DisplayName, DisplayDescription: r.DisplayDescription, Icon: r.Icon,
		Capabilities: model.OptionalSelection(r.Capabilities), ReasoningEfforts: model.OptionalSelection(r.ReasoningEfforts),
		ID:               r.ID,
		Provider:         r.Provider,
		Model:            r.Model,
		BaseURL:          r.BaseURL,
		HasKey:           r.APIKey != "",
		Source:           source,
		MaxOutputTokens:  r.MaxOutputTokens,
		ContextWindow:    r.ContextWindow,
		Priced:           r.Prices.Any(),
		ReasoningEffort:  r.ReasoningEffort,
		ExtraBody:        redactExtraBody(r.ExtraBody),
		OpenAIEndpoint:   r.OpenAIEndpoint,
		AudioAPI:         r.AudioAPI,
		NoPrice:          r.NoPrice,
		WireCapabilities: model.OptionalSelection(r.WireCapabilities),
	}
	if r.APIKey != "" {
		v.KeyHint = lastFour(r.APIKey)
	}
	if r.Prices != nil {
		v.Prices = &PricesView{
			InputUSDPerMTok:      r.Prices.InputUSDPerMTok,
			OutputUSDPerMTok:     r.Prices.OutputUSDPerMTok,
			CacheReadUSDPerMTok:  r.Prices.CacheReadUSDPerMTok,
			CacheWriteUSDPerMTok: r.Prices.CacheWriteUSDPerMTok,
		}
	}
	return v
}

// lastFour returns the final four characters, or fewer for a short key.
//
// A short key is a mistyped key, and showing two characters of it is how
// somebody notices they pasted half of one.
func lastFour(key string) string {
	runes := []rune(key)
	if len(runes) <= 4 {
		return strings.Repeat("•", len(runes))
	}
	return "…" + string(runes[len(runes)-4:])
}

// --- the mutable store -------------------------------------------------------

// MutableRoutes is a route store a client can change.
type MutableRoutes struct {
	mu     sync.RWMutex
	by     map[string]model.Route
	source map[string]RouteSource

	// persist writes a change through to wherever routes survive a restart.
	// Nil means they do not — which was the state S3 shipped and S4 was
	// supposed to end.
	persist RoutePersistence
}

// RoutePersistence is where routes and their credentials survive a restart.
//
// # An interface, because the two halves go to different places
//
// A route's CONFIGURATION goes in the store; its CREDENTIAL goes in a dedicated
// 0600 file (C17 — the environment, or a dedicated path, never a config file).
// `internal/protocol` must know about neither, so this is the seam, implemented
// in `internal/daemon` over `internal/store` and `internal/secrets`.
//
// `Save` receives the route WITH its key, because it is the only thing that
// knows the key must be split off. A caller handing it a route and expecting
// the key to be dropped would be relying on a convention.
type RoutePersistence interface {
	Save(r model.Route) error
	Remove(id string) error
	// Load returns the stored routes, each with its credential attached.
	Load() ([]model.Route, error)
}

// WithPersistence attaches the write-through and loads what is already stored.
//
// Stored routes are loaded FIRST and environment-derived ones fill in only
// where no stored route has that id. The precedence is deliberate and it is the
// one a person expects: **what you typed wins over what the environment
// happened to have**, and a fresh machine with only `ANTHROPIC_API_KEY` set
// still works with no configuration at all.
//
// A load failure is returned rather than swallowed. Starting with no routes
// when some are stored would look exactly like "my configuration vanished",
// which is the bug this whole mechanism exists to fix.
func (m *MutableRoutes) WithPersistence(p RoutePersistence) error {
	if p == nil {
		return nil
	}
	stored, err := p.Load()
	if err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.persist = p
	for _, r := range stored {
		// The environment seeded `by` at construction; a stored route of the
		// same id replaces it, and its source says so.
		m.by[r.ID] = r
		m.source[r.ID] = RouteSourceStored
	}
	return nil
}

// Persisted reports whether a change made now would survive a restart.
//
// `routes.list` carries it, and the app PRINTS it: S3's version said
// "persistence arrives with the store (S4)", and a surface that claims
// durability it does not have is worse than one that admits the gap.
func (m *MutableRoutes) Persisted() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.persist != nil
}

func NewMutableRoutes(envRoutes ...model.Route) *MutableRoutes {
	m := &MutableRoutes{by: map[string]model.Route{}, source: map[string]RouteSource{}}
	for _, r := range envRoutes {
		m.by[r.ID] = r
		m.source[r.ID] = RouteSourceEnvironment
	}
	return m
}

func (m *MutableRoutes) Route(id string) (model.Route, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	r, ok := m.by[id]
	return r, ok
}

func (m *MutableRoutes) RouteIDs() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]string, 0, len(m.by))
	for id := range m.by {
		out = append(out, id)
	}
	sortStrings(out)
	return out
}

// SourceOf reports where a route came from, and whether it exists at all.
// `handleRoutesSet` needs it to tell "this row predates the price rule"
// (RouteSourceStored) from "persisting this would create a NEW stored row"
// (absent, or environment/runtime) — the distinction O8's containment rests
// on. Only MutableRoutes knows it; the Routes interface stays read-shaped.
func (m *MutableRoutes) SourceOf(id string) (RouteSource, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	src, ok := m.source[id]
	return src, ok
}

// Views returns every route, in id order, with no credential in sight.
func (m *MutableRoutes) Views() []RouteView {
	m.mu.RLock()
	defer m.mu.RUnlock()

	ids := make([]string, 0, len(m.by))
	for id := range m.by {
		ids = append(ids, id)
	}
	sortStrings(ids)

	out := make([]RouteView, 0, len(ids))
	for _, id := range ids {
		out = append(out, viewOf(m.by[id], m.source[id]))
	}
	return out
}

// Set creates or replaces a route and returns its view.
//
// `keepKeyIfEmpty` is the whole reason this is not a plain map write: an empty
// key on an existing route means *leave the credential alone*, so a UI can edit
// a model name without ever holding one.
func (m *MutableRoutes) Set(r model.Route) RouteView {
	m.mu.Lock()
	defer m.mu.Unlock()

	if r.APIKey == "" {
		if existing, ok := m.by[r.ID]; ok {
			r.APIKey = existing.APIKey
		}
	}
	m.by[r.ID] = r
	// A route the runtime has touched is no longer an environment route, even
	// if the environment seeded it — its key may no longer be the
	// environment's, and saying "environment" would be wrong.
	//
	// With persistence attached it is `stored`, because that is now true and is
	// what the app shows beside it.
	m.source[r.ID] = RouteSourceRuntime
	if m.persist != nil {
		m.source[r.ID] = RouteSourceStored
	}
	return viewOf(r, m.source[r.ID])
}

// SetAndPersist stores a route and writes it through.
//
// Separate from `Set` rather than folded into it, because a write-through can
// FAIL and `Set` cannot — and a caller that could not tell the difference would
// report a saved route that is only in memory. The route is kept in memory
// either way: refusing to use a credential because it could not be filed would
// make a disk problem into a model-calling problem.
func (m *MutableRoutes) SetAndPersist(r model.Route) (RouteView, error) {
	view := m.Set(r)

	m.mu.RLock()
	p := m.persist
	stored := m.by[r.ID]
	m.mu.RUnlock()

	if p == nil {
		return view, nil
	}
	if err := p.Save(stored); err != nil {
		return view, err
	}
	return view, nil
}

// DeleteAndPersist removes a route and its stored copy, credential included.
func (m *MutableRoutes) DeleteAndPersist(id string) (bool, error) {
	existed := m.Delete(id)

	m.mu.RLock()
	p := m.persist
	m.mu.RUnlock()

	if p == nil {
		return existed, nil
	}
	if err := p.Remove(id); err != nil {
		return existed, err
	}
	return existed, nil
}

// Delete removes a route and forgets its key. Returns whether it existed.
func (m *MutableRoutes) Delete(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, existed := m.by[id]
	delete(m.by, id)
	delete(m.source, id)
	return existed
}

// --- the methods -------------------------------------------------------------

// SetRouteParams is the shape that comes IN. It is the only type in this file
// with a credential field.
type SetRouteParams struct {
	DisplayName        *string   `json:"displayName,omitempty"`
	DisplayDescription *string   `json:"displayDescription,omitempty"`
	Icon               *string   `json:"icon,omitempty"`
	Capabilities       *[]string `json:"capabilities,omitempty"`
	ReasoningEfforts   *[]string `json:"reasoningEfforts,omitempty"`
	ID                 string    `json:"id"`
	Provider           string    `json:"provider"`
	Model              string    `json:"model"`
	BaseURL            string    `json:"baseUrl,omitempty"`

	// APIKey is WRITE-ONLY. Empty means "keep whatever is already set", so a
	// client editing a route never has to read a credential back out.
	APIKey string `json:"apiKey,omitempty"`
	// ClearKey removes the credential, since an empty APIKey means "keep".
	// Explicit, because "unset the key" and "do not change the key" are
	// different intentions and a single empty string cannot express both.
	ClearKey bool `json:"clearKey,omitempty"`

	MaxOutputTokens int         `json:"maxOutputTokens,omitempty"`
	ContextWindow   int         `json:"contextWindow,omitempty"`
	Prices          *PricesView `json:"prices,omitempty"`

	// ReasoningEffort is `none|minimal|low|medium|high|xhigh|max`, or empty to leave the
	// provider's default alone.
	//
	// **`none` is what makes a STEP possible on a reasoning model.** OpenAI's
	// current models refuse function tools unless reasoning is off on
	// `/v1/chat/completions`, and every Aeon step offers tools — so a route
	// without this answers `ask` and fails every step.
	ReasoningEffort string `json:"reasoningEffort,omitempty"`

	// ClearReasoningEffort removes it, since empty means "keep". The same
	// distinction `clearKey` exists for: "unset" and "do not change" are
	// different intentions and one empty string cannot express both.
	ClearReasoningEffort bool `json:"clearReasoningEffort,omitempty"`

	// ExtraBody is a JSON object of extra request fields merged into every call on this route, e.g.
	// `{"chat_template_kwargs":{"enable_thinking":false}}` or `{"reasoning_effort":"low"}`. nil (absent) keeps what
	// is saved; an empty string, `{}` or `null` clears it. The same patch shape as the wire flags: absent and empty
	// are different answers.
	ExtraBody *string `json:"extraBody,omitempty"`

	// OpenAIEndpoint picks which OpenAI API this route talks to: `chat` (the
	// default) or `responses`.
	//
	// **`responses` is what lets a step reason AND use tools**, because
	// `/v1/chat/completions` refuses the combination. Empty means keep, like
	// everything else here.
	OpenAIEndpoint string `json:"openaiEndpoint,omitempty"`

	// AudioAPI says how a route that hears or speaks talks to its server: `openai` (the default, /audio/speech and
	// /audio/transcriptions), `synthesize` or `synthesize-stream` (speech only), or `chat` (an omni model, one model
	// for both). Empty means keep.
	AudioAPI string `json:"audioApi,omitempty"`

	// WireCapabilities patches the Q8 wire-behaviour flags — the closed set
	// tools|streaming-usage|thinking|system-role|parallel-tools, validated
	// in capability.go (never by the metadata vocabulary; both contain the
	// word "thinking" and mean different things by it).
	//
	// nil (field absent) KEEPS the saved declaration; [] declares NONE.
	// The two are different answers and this shape is what keeps them apart
	// across the socket (OptionalSelection's discipline).
	WireCapabilities *[]string `json:"wireCapabilities,omitempty"`

	// NoPrice declares the route UNMETERED on purpose (O8). Absent means
	// keep the existing declaration; a NEW persisted route with no prices and
	// no true here is refused, naming the route — a silently missing price
	// is how a typo makes a metered model report `unknown` forever.
	NoPrice *bool `json:"noPrice,omitempty"`
}

type RoutesListResult struct {
	Routes []RouteView `json:"routes"`
	// Providers is what adapters are registered, so a UI can offer a choice
	// rather than a free-text field that fails after a call.
	Providers []string `json:"providers"`
	// Persisted is false until S4. **The UI says so**: a settings page that
	// looks like it saved is worse than one that admits it did not.
	Persisted bool `json:"persisted"`
}

type SetRouteResult struct {
	Route RouteView `json:"route"`
}

type DeleteRouteParams struct {
	ID string `json:"id"`
}

type DeleteRouteResult struct {
	// Deleted is false when there was nothing to delete, which is not an error:
	// a client racing its own refresh is ordinary.
	Deleted bool `json:"deleted"`
}

func (t *Table) handleRoutesList(_ context.Context, _ *Session, _ json.RawMessage) (any, error) {
	store, ok := t.Routes.(*MutableRoutes)
	if !ok {
		return nil, ErrProviderFailed.WithWhat("this daemon's routes cannot be listed")
	}
	views := store.Views()
	if t.Probes != nil {
		// S18 T3: the three states ride the list, sourced ONLY from the
		// probe cache. A route read whose verdicts cannot be read fails the
		// whole list — a silently-missing state column would render every
		// contradiction as `declared`, which is the one wrong answer this
		// surface must not give.
		for i := range views {
			if views[i].WireCapabilities == nil || len(*views[i].WireCapabilities) == 0 {
				continue // nothing declared: nothing to state
			}
			rows, err := t.Probes.ProbeResults(views[i].ID)
			if err != nil {
				return nil, ErrStoreUnavailable.WithWhat("the cached probe verdicts for route %q cannot be read: %v — the states on this list would be unverifiable", views[i].ID, err)
			}
			views[i].CapabilityStates = capabilityStates(*views[i].WireCapabilities, rows)
		}
	}
	return RoutesListResult{
		Routes:    views,
		Providers: model.RegisteredProviders(),
		// True since S4: a route set here is written through to the store, and
		// its credential to a 0600 file beside it. The app PRINTS this, so a
		// daemon without persistence (every test, and nothing that ships) still
		// says so rather than claiming durability it does not have.
		Persisted: store.Persisted(),
	}, nil
}

// capabilityStates derives the three states for a route's declared flags:
// a flag a probe settled reads the probe's recorded state, and a flag with
// no row is `declared` — the representation is the absence, so there is
// nothing to write and nothing to keep in step (I5).
func capabilityStates(declared []string, rows []model.ProbeResult) []CapabilityStateView {
	byCap := map[string]model.ProbeResult{}
	for _, r := range rows {
		byCap[string(r.Capability)] = r
	}
	out := make([]CapabilityStateView, 0, len(declared))
	for _, c := range declared {
		v := CapabilityStateView{Capability: c, State: string(model.StateDeclared)}
		if row, ok := byCap[c]; ok {
			v.State = string(row.State)
			v.Detail = row.Detail
			v.ProbedAt = row.ProbedAt.UTC().Format(time.RFC3339)
			v.ProbedModel = row.Model
		}
		out = append(out, v)
	}
	return out
}

func (t *Table) handleRoutesSet(_ context.Context, _ *Session, params json.RawMessage) (any, error) {
	var p SetRouteParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}

	store, ok := t.Routes.(*MutableRoutes)
	if !ok {
		return nil, ErrProviderFailed.WithWhat("this daemon's routes cannot be changed")
	}

	p.ID = strings.TrimSpace(p.ID)
	p.Provider = strings.TrimSpace(p.Provider)
	p.Model = strings.TrimSpace(p.Model)

	if p.ID == "" {
		return nil, ErrInvalidRoute.WithWhat("a route needs an id; it is what `--route` names")
	}
	if p.Model == "" {
		return nil, ErrInvalidRoute.WithWhat("route %q has no model name", p.ID)
	}

	// The provider must have an adapter, checked HERE rather than at call time.
	// A route saved against a provider nobody implements is a route that fails
	// later, in a different screen, after a prompt has been typed.
	if _, err := model.AdapterFor(p.Provider); err != nil {
		return nil, ErrInvalidRoute.WithWhat("route %q names provider %q, which has no adapter; "+
			"the registered providers are %s", p.ID, p.Provider,
			strings.Join(model.RegisteredProviders(), ", "))
	}

	// # Absent means KEEP, for an existing route
	//
	// `routes.set` was a full REPLACE for everything except the credential, and
	// that made every piece of advice about it destructive. Found by reading
	// this slice's own error message back: it suggested
	//
	//	aeon routes set --id openai --provider openai --model gpt-6-luna \
	//	  --reasoning-effort none
	//
	// which silently cleared the route's base URL — and then the next call went
	// to the real api.openai.com instead of the local stand-in it had been
	// pointed at. A verb that needs every field re-typed is a verb that loses
	// one.
	//
	// So the rule is the one the key already had: **set what you name, keep
	// what you do not.** The UI is unaffected (its form sends every field), and
	// a NEW route still gets the default cap.
	existing, isExisting := t.Routes.Route(p.ID)

	outputCap := p.MaxOutputTokens
	if outputCap <= 0 {
		outputCap = defaultRouteOutputCap
		if isExisting && existing.MaxOutputTokens > 0 {
			outputCap = existing.MaxOutputTokens
		}
	}
	if p.ContextWindow < 0 {
		return nil, ErrInvalidRoute.WithWhat("route %q has a negative context window %d", p.ID, p.ContextWindow)
	}
	contextWindow := p.ContextWindow
	if contextWindow == 0 && isExisting {
		contextWindow = existing.ContextWindow
	}

	effort := strings.TrimSpace(p.ReasoningEffort)
	if !model.ValidReasoningEffort(effort) {
		// Checked here rather than forwarded. A typo would otherwise become a
		// provider 400 about a field the person believed they had set, three
		// layers from where they typed it.
		return nil, ErrInvalidRoute.WithWhat("route %q sets reasoningEffort %q; the accepted values "+
			"are %s, or empty to leave the provider's own default alone. `none` is the one that lets a "+
			"step use tools on a reasoning model",
			p.ID, p.ReasoningEffort, strings.Join(model.ReasoningEfforts(), ", "))
	}

	endpoint := strings.TrimSpace(p.OpenAIEndpoint)
	if !model.ValidOpenAIEndpoint(endpoint) {
		return nil, ErrInvalidRoute.WithWhat("route %q sets openaiEndpoint %q; the accepted values "+
			"are %s, or empty for %s. `%s` is the one that lets a step reason while using tools",
			p.ID, p.OpenAIEndpoint, strings.Join(model.OpenAIEndpoints(), ", "),
			model.OpenAIEndpointChat, model.OpenAIEndpointResponses)
	}

	audioAPI := strings.TrimSpace(p.AudioAPI)
	if !model.ValidAudioAPI(audioAPI) {
		return nil, ErrInvalidRoute.WithWhat("route %q sets audioApi %q; the accepted values are %s, or empty for %s",
			p.ID, p.AudioAPI, strings.Join(model.AudioAPIs(), ", "), model.AudioAPIOpenAI)
	}

	route := model.Route{
		ID:              p.ID,
		Provider:        p.Provider,
		Model:           p.Model,
		BaseURL:         strings.TrimSpace(p.BaseURL),
		APIKey:          p.APIKey,
		MaxOutputTokens: outputCap,
		ContextWindow:   contextWindow,
		ReasoningEffort: effort,
		OpenAIEndpoint:  endpoint,
		AudioAPI:        audioAPI,
	}
	if audioAPI == "" && isExisting {
		route.AudioAPI = existing.AudioAPI
	}
	if endpoint == "" && isExisting {
		route.OpenAIEndpoint = existing.OpenAIEndpoint
	}
	if route.BaseURL == "" && isExisting {
		route.BaseURL = existing.BaseURL
	}
	if effort == "" && !p.ClearReasoningEffort && isExisting {
		route.ReasoningEffort = existing.ReasoningEffort
	}
	// ExtraBody is carried too unless the set names it: a set that does not mention it
	// must not delete a field the person saved.
	if isExisting {
		route.ExtraBody = existing.ExtraBody
	}
	if p.ExtraBody != nil {
		compact, err := ValidateExtraBody(*p.ExtraBody)
		if err == nil {
			compact, err = restoreRedacted(compact, existing.ExtraBody)
		}
		if err != nil {
			return nil, ErrInvalidRoute.WithWhat("route %q: %v", p.ID, err)
		}
		route.ExtraBody = nil
		if compact != "" {
			route.ExtraBody = json.RawMessage(compact)
		}
	}
	switch {
	case p.Prices != nil:
		route.Prices = &model.Prices{
			InputUSDPerMTok:      p.Prices.InputUSDPerMTok,
			OutputUSDPerMTok:     p.Prices.OutputUSDPerMTok,
			CacheReadUSDPerMTok:  p.Prices.CacheReadUSDPerMTok,
			CacheWriteUSDPerMTok: p.Prices.CacheWriteUSDPerMTok,
		}
	case isExisting:
		// Kept. Losing prices turns every later call's cost into `unknown`,
		// which is honest about the arithmetic and wrong about the route.
		route.Prices = existing.Prices
	}

	// Metadata updates are patches: older clients cannot erase fields they never read.
	route.DisplayName, route.DisplayDescription, route.Icon = existing.DisplayName, existing.DisplayDescription, existing.Icon
	route.Capabilities, route.ReasoningEfforts = existing.Capabilities, existing.ReasoningEfforts
	if p.DisplayName != nil {
		route.DisplayName = *p.DisplayName
	}
	if p.DisplayDescription != nil {
		route.DisplayDescription = *p.DisplayDescription
	}
	if p.Icon != nil {
		route.Icon = *p.Icon
	}
	if p.Capabilities != nil {
		route.Capabilities = *p.Capabilities
	}
	if p.ReasoningEfforts != nil {
		route.ReasoningEfforts = *p.ReasoningEfforts
	}
	// The wire flags and the price declaration patch the same way — absent
	// keeps what is saved — but they validate through their OWN function
	// (capability.go), never through the metadata normalizer below.
	route.WireCapabilities = existing.WireCapabilities
	if p.WireCapabilities != nil {
		route.WireCapabilities = *p.WireCapabilities
	}
	route.NoPrice = existing.NoPrice
	if p.NoPrice != nil {
		route.NoPrice = *p.NoPrice
	}
	if err := route.NormalizeMetadata(); err != nil {
		return nil, ErrInvalidRoute.WithWhat("route %q: %v", p.ID, err)
	}
	if err := route.NormalizeWireCapabilities(); err != nil {
		return nil, ErrInvalidRoute.WithWhat("route %q: %v", p.ID, err)
	}

	// # O8 at the socket, too — and only for NEW persisted routes
	//
	// The store gates the write (UpsertRoute), but refusing there would let
	// `Set` land the route in memory first and return "usable now but not
	// saved" — the wrong story. A route whose row predates the rule keeps
	// saving as it stands (yesterday's contract), and an environment route
	// is ephemeral configuration until someone persists it: the moment a
	// STORED row would be created is the moment the decision is required.
	if !route.Prices.Any() && !route.NoPrice && store.Persisted() {
		src, seen := store.SourceOf(p.ID)
		if !seen || src != RouteSourceStored {
			return nil, ErrInvalidRoute.WithWhat("route %q has no price and no noPrice declaration (O8): "+
				"a silently missing price is how a typo makes a metered model report `unknown` forever, and "+
				"`unknown` is a legitimate answer nobody re-checks. Price it, or declare it unmetered on "+
				"purpose (noPrice / --no-price). A stored route that predates this rule saves as it stands.", p.ID)
		}
	}

	if p.ClearKey {
		// Explicit removal. `Set` would otherwise keep the existing key,
		// because an empty APIKey means "leave it alone". The stored copy and
		// the credential file go with it, or a restart would bring the key
		// back — which is the specific surprise `clearKey` exists to prevent.
		if _, err := store.DeleteAndPersist(p.ID); err != nil {
			return nil, ErrProviderFailed.WithWhat("the route's credential could not be removed: %v", err)
		}
	}

	view, err := store.SetAndPersist(route)
	if err != nil {
		// The route IS usable — it is in memory — and saying so matters: the
		// failure is about durability, not about the model call the person was
		// about to make.
		return nil, ErrRouteNotSaved.WithWhat("route %q is usable now but was not saved: %v", p.ID, err)
	}
	return SetRouteResult{Route: view}, nil
}

func (t *Table) handleRoutesDelete(_ context.Context, _ *Session, params json.RawMessage) (any, error) {
	var p DeleteRouteParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	store, ok := t.Routes.(*MutableRoutes)
	if !ok {
		return nil, ErrProviderFailed.WithWhat("this daemon's routes cannot be changed")
	}
	deleted, err := store.DeleteAndPersist(strings.TrimSpace(p.ID))
	if err != nil {
		return nil, ErrRouteNotSaved.WithWhat("route %q was removed from this daemon but its stored "+
			"copy or its credential remains: %v", p.ID, err)
	}
	return DeleteRouteResult{Deleted: deleted}, nil
}

// defaultRouteOutputCap is the cap a route gets when none is given. Required by
// one provider and a sensible bound on both (M16).
const defaultRouteOutputCap = 4096

// --- the probe (S18 T3) -------------------------------------------------------
//
// One minimal request per route, run ONLY because an operator asked
// (M32: never auto-fires — no step, no list, no startup path below calls
// any of this). The implementation lives in internal/model/probe.go — the
// socket handler and `aeon routes probe` are both thin callers of that one
// function (P34: the CLI is a client of this door, never a second door).

// ProbeStore is the daemon's cached verdicts, injected so this package keeps
// importing nothing storage-shaped (T11/I1). `routes.probe` writes what a
// probe earned; `routes.list` and the ask/step pre-checks read them.
type ProbeStore interface {
	PutProbeResults(routeID string, results []model.ProbeResult) error
	ProbeResults(routeID string) ([]model.ProbeResult, error)
}

type ProbeRouteParams struct {
	Route string `json:"route"`
}

type ProbeRouteResult struct {
	Route string `json:"route"`
	// Probes is the verdict per declared capability — including the honest
	// `declared` ones with their reasons, which are shown and never stored.
	Probes []model.ProbeResult `json:"probes,omitempty"`
	// Saved says whether the verdicts reached the cache. An unsaved verdict
	// is a verdict that does not survive the restart — the surface says so.
	Saved bool `json:"saved"`
	// Note is the daemon's own honest sentence (no cache; or a cache that
	// refused), not a paraphrase of the verdicts.
	Note string `json:"note,omitempty"`
	// Failure is the THIRD answer (P31): the probe ran and could not check.
	// It is data, not an RPC error, because "could not check" is a complete
	// honest result — and nothing was contradicted.
	Failure string `json:"failure,omitempty"`
}

// probeFlight is the memory-only in-flight lock (D7): a second
// routes.probe while one is running WAITS for the first and shares its
// answer rather than paying for a second request. It guards concurrency,
// not facts — the facts are the route_probe rows.
type probeFlight struct {
	done   chan struct{}
	result ProbeRouteResult
}

func (t *Table) beginProbe(routeID string) (*probeFlight, bool) {
	t.probeMu.Lock()
	defer t.probeMu.Unlock()
	if t.probeFlights == nil {
		t.probeFlights = map[string]*probeFlight{}
	}
	if f := t.probeFlights[routeID]; f != nil {
		return f, false
	}
	f := &probeFlight{done: make(chan struct{})}
	t.probeFlights[routeID] = f
	return f, true
}

func (t *Table) completeProbe(routeID string, f *probeFlight, r ProbeRouteResult) {
	t.probeMu.Lock()
	if t.probeFlights[routeID] == f {
		delete(t.probeFlights, routeID)
	}
	f.result = r
	t.probeMu.Unlock()
	close(f.done)
}

func (t *Table) handleRoutesProbe(ctx context.Context, _ *Session, params json.RawMessage) (any, error) {
	var p ProbeRouteParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.Route) == "" {
		return nil, ErrInvalidRoute.WithWhat("routes.probe needs a route id")
	}
	if t.Routes == nil || t.Models == nil {
		return nil, ErrProbeUnavailable.WithWhat("this daemon has no model layer wired; routes.probe cannot ask anything until it does")
	}
	route, ok := t.Routes.Route(p.Route)
	if !ok {
		return nil, ErrUnknownRoute.WithWhat("route %q is not configured; there is nothing to probe", p.Route)
	}
	if len(route.WireCapabilities) == 0 {
		// Nothing declared: the honest zero-request answer. The probe has
		// nothing to ask, so it asks nothing — and inventing a request to
		// check an empty claim would make the operator pay for theatre.
		return ProbeRouteResult{Route: route.ID, Note: "route declares no wire capabilities — nothing to ask, zero requests sent"}, nil
	}

	flight, fresh := t.beginProbe(route.ID)
	if !fresh {
		// One request per route (D7): the running probe's answer is this
		// caller's answer too. The wait is bounded by the runner's own
		// model.ProbeTimeout — a hung server times the flight out, it does
		// not hang the second caller forever.
		<-flight.done
		return flight.result, nil
	}

	out := ProbeRouteResult{Route: route.ID}
	pctx, cancel := context.WithTimeout(ctx, model.ProbeTimeout)
	defer cancel()

	// ONE function for socket and CLI alike (P34): model.Probe. The route
	// is passed by value; Probe is forbidden from rewriting the declaration
	// (I5), and a test in internal/model pins that it does not.
	res, err := model.Probe(pctx, route, t.Models, time.Now)
	if err != nil {
		// "Could not check." The cache keeps its previous earned rows
		// untouched — a failed probe contradicts nothing.
		out.Failure = err.Error()
	} else {
		out.Probes = res
		if t.Probes != nil {
			if perr := t.Probes.PutProbeResults(route.ID, res); perr != nil {
				out.Note = "verdicts returned but the cache refused them: " + perr.Error()
			} else {
				out.Saved = true
			}
		} else {
			out.Note = "this daemon has no probe cache; the verdicts are returned but not stored — they will not survive a restart"
		}
	}
	t.completeProbe(route.ID, flight, out)
	return out, nil
}

// contradictionRefusal is the use-time half of refusing twice (O3, D6):
// someone who probed on Monday and runs on Friday does not have to
// remember. `ask` calls it here; the daemon's StepRunner carries its own
// wired copy for the step path. A cache that cannot be read refuses too —
// sending the request anyway could replay exactly the call a probe already
// proved would fail, and "we could not look" is not a licence to proceed.
func (t *Table) contradictionRefusal(route model.Route) error {
	if t.Probes == nil {
		return nil // storeless daemon: no verdicts can exist, nothing to consult
	}
	rows, err := t.Probes.ProbeResults(route.ID)
	if err != nil {
		return ErrStoreUnavailable.WithWhat("the cached probe verdicts for route %q cannot be read: %v — sending the request anyway could replay the exact call a probe already proved would fail", route.ID, err)
	}
	if row, bad := route.FirstContradiction(rows); bad {
		return ErrRouteContradicted.WithWhat("%s", model.ContradictionSentence(route.ID, string(row.Capability), row.ProbedAt, row.Model, row.Detail))
	}
	return nil
}
