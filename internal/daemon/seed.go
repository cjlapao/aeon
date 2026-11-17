package daemon

import (
	"context"
	"errors"
	"log"
	"strings"
	"time"

	"github.com/cjlapao/aeon/internal/agentseed"
	"github.com/cjlapao/aeon/internal/blueprintseed"
	"github.com/cjlapao/aeon/internal/execenv/envseed"
	"github.com/cjlapao/aeon/internal/intakeseed"
	"github.com/cjlapao/aeon/internal/landingseed"
	"github.com/cjlapao/aeon/internal/mcpseed"
	"github.com/cjlapao/aeon/internal/notifyseed"
	"github.com/cjlapao/aeon/internal/policy"
	"github.com/cjlapao/aeon/internal/policyseed"
	"github.com/cjlapao/aeon/internal/protocol"
	retroseed "github.com/cjlapao/aeon/internal/retro/seed"
	scaffoldseed "github.com/cjlapao/aeon/internal/scaffold/seed"
	"github.com/cjlapao/aeon/internal/seed"
	"github.com/cjlapao/aeon/internal/store"
	"github.com/cjlapao/aeon/internal/toolseed"
	"github.com/cjlapao/aeon/internal/verify/verifyseed"
	"github.com/cjlapao/aeon/internal/watchdog/watchdogseed"
)

// errTxIsNotATransaction is the bridge's one refusal: a seed.Tx that is not
// the *sql.Tx the daemon's WithTx handed it is a bridge misuse, named
// (never a nil-deref).
var errTxIsNotATransaction = errors.New("seed: a transaction that is not the one the daemon opened")

// agentApplier adapts the agentseed applier (which holds the *Store and
// takes *sql.Tx, so it is not a seed.Applier — see T4's note) to the
// seed.Applier seam (which takes seed.Tx). The daemon owns it: it holds the
// store the applier writes through. The seed.Tx the engine hands it is the
// concrete *sql.Tx the daemon's WithTx wrapped, unwrapped here and handed to
// the applier, so no SQL lives outside internal/store (the store's guard).
//
// The applier's methods take a context the seam does not, so the adapter
// uses context.Background: a seed apply is a short, transactional write and
// the transaction's own context (the *sql.Tx) is what governs it.
// blueprintApplier is the S48 T5 twin (S48): the seed.Applier the engine
// holds for blueprints.standard, adapting the store-backed applier.
type blueprintApplier struct {
	ap *blueprintseed.Applier
}

func (a blueprintApplier) Set() string                   { return a.ap.Set() }
func (a blueprintApplier) GuardAffecting() bool          { return a.ap.GuardAffecting() }
func (a blueprintApplier) Operator() seed.OperatorRights { return a.ap.Operator() }

func (a blueprintApplier) Fingerprint(tx seed.Tx, key string) (string, bool, error) {
	sqlTx, err := unwrapSeedTx(tx)
	if err != nil {
		return "", false, err
	}
	return a.ap.Fingerprint(context.Background(), sqlTx, key)
}

func (a blueprintApplier) Adoptable(tx seed.Tx, key string) (bool, error) {
	sqlTx, err := unwrapSeedTx(tx)
	if err != nil {
		return false, err
	}
	return a.ap.Adoptable(context.Background(), sqlTx, key)
}

func (a blueprintApplier) Upsert(tx seed.Tx, r seed.Record, adopt bool) error {
	sqlTx, err := unwrapSeedTx(tx)
	if err != nil {
		return err
	}
	return a.ap.Upsert(context.Background(), sqlTx, r, adopt)
}

func (a blueprintApplier) Retire(tx seed.Tx, key string) error {
	sqlTx, err := unwrapSeedTx(tx)
	if err != nil {
		return err
	}
	return a.ap.Retire(context.Background(), sqlTx, key)
}

// verifyApplier is the S57 T10 twin (VG21): the seed.Applier the engine
// holds for verify.templates, adapting the store-backed applier. Same
// unwrap, same context.Background stance as the blueprint adapter above.
type verifyApplier struct {
	ap *verifyseed.Applier
}

func (a verifyApplier) Set() string                   { return a.ap.Set() }
func (a verifyApplier) GuardAffecting() bool          { return a.ap.GuardAffecting() }
func (a verifyApplier) Operator() seed.OperatorRights { return a.ap.Operator() }

func (a verifyApplier) Fingerprint(tx seed.Tx, key string) (string, bool, error) {
	sqlTx, err := unwrapSeedTx(tx)
	if err != nil {
		return "", false, err
	}
	return a.ap.Fingerprint(context.Background(), sqlTx, key)
}

func (a verifyApplier) Adoptable(tx seed.Tx, key string) (bool, error) {
	sqlTx, err := unwrapSeedTx(tx)
	if err != nil {
		return false, err
	}
	return a.ap.Adoptable(context.Background(), sqlTx, key)
}

func (a verifyApplier) Upsert(tx seed.Tx, r seed.Record, adopt bool) error {
	sqlTx, err := unwrapSeedTx(tx)
	if err != nil {
		return err
	}
	return a.ap.Upsert(context.Background(), sqlTx, r, adopt)
}

func (a verifyApplier) Retire(tx seed.Tx, key string) error {
	sqlTx, err := unwrapSeedTx(tx)
	if err != nil {
		return err
	}
	return a.ap.Retire(context.Background(), sqlTx, key)
}

// retroTemplateApplier is the S63 T3 twin: the seed.Applier the engine
// holds for retro.templates. Same unwrap; the retroseed applier takes the
// bare *sql.Tx (its store doors carry no ctx — the T3 Tx twins were built
// for this call).
type retroTemplateApplier struct {
	ap *retroseed.Applier
}

func (a retroTemplateApplier) Set() string                   { return a.ap.Set() }
func (a retroTemplateApplier) GuardAffecting() bool          { return a.ap.GuardAffecting() }
func (a retroTemplateApplier) Operator() seed.OperatorRights { return a.ap.Operator() }

func (a retroTemplateApplier) Fingerprint(tx seed.Tx, key string) (string, bool, error) {
	sqlTx, err := unwrapSeedTx(tx)
	if err != nil {
		return "", false, err
	}
	return a.ap.Fingerprint(sqlTx, key)
}

func (a retroTemplateApplier) Adoptable(tx seed.Tx, key string) (bool, error) {
	sqlTx, err := unwrapSeedTx(tx)
	if err != nil {
		return false, err
	}
	return a.ap.Adoptable(sqlTx, key)
}

func (a retroTemplateApplier) Upsert(tx seed.Tx, r seed.Record, adopt bool) error {
	sqlTx, err := unwrapSeedTx(tx)
	if err != nil {
		return err
	}
	return a.ap.Upsert(sqlTx, r, adopt)
}

func (a retroTemplateApplier) Retire(tx seed.Tx, key string) error {
	sqlTx, err := unwrapSeedTx(tx)
	if err != nil {
		return err
	}
	return a.ap.Retire(sqlTx, key)
}

// templateApplier is the S61 T3 twin: the seed.Applier the engine holds for
// templates.scaffold. Same unwrap; the scaffoldseed applier takes the bare
// *sql.Tx (its store doors are the SaveTemplateTx/GetTemplateTx twins).
type templateApplier struct {
	ap *scaffoldseed.Applier
}

func (a templateApplier) Set() string                   { return a.ap.Set() }
func (a templateApplier) GuardAffecting() bool          { return a.ap.GuardAffecting() }
func (a templateApplier) Operator() seed.OperatorRights { return a.ap.Operator() }

func (a templateApplier) Fingerprint(tx seed.Tx, key string) (string, bool, error) {
	sqlTx, err := unwrapSeedTx(tx)
	if err != nil {
		return "", false, err
	}
	return a.ap.Fingerprint(sqlTx, key)
}

func (a templateApplier) Adoptable(tx seed.Tx, key string) (bool, error) {
	sqlTx, err := unwrapSeedTx(tx)
	if err != nil {
		return false, err
	}
	return a.ap.Adoptable(sqlTx, key)
}

func (a templateApplier) Upsert(tx seed.Tx, r seed.Record, adopt bool) error {
	sqlTx, err := unwrapSeedTx(tx)
	if err != nil {
		return err
	}
	return a.ap.Upsert(sqlTx, r, adopt)
}

func (a templateApplier) Retire(tx seed.Tx, key string) error {
	sqlTx, err := unwrapSeedTx(tx)
	if err != nil {
		return err
	}
	return a.ap.Retire(sqlTx, key)
}

// notifyApplier is the S58 T4 twin: the seed.Applier the engine holds for
// notify.events, adapting the store-backed applier. Same unwrap, same
// context.Background stance as the adapters above.
type notifyApplier struct {
	ap *notifyseed.Applier
}

func (a notifyApplier) Set() string                   { return a.ap.Set() }
func (a notifyApplier) GuardAffecting() bool          { return a.ap.GuardAffecting() }
func (a notifyApplier) Operator() seed.OperatorRights { return a.ap.Operator() }

func (a notifyApplier) Fingerprint(tx seed.Tx, key string) (string, bool, error) {
	sqlTx, err := unwrapSeedTx(tx)
	if err != nil {
		return "", false, err
	}
	return a.ap.Fingerprint(context.Background(), sqlTx, key)
}

func (a notifyApplier) Adoptable(tx seed.Tx, key string) (bool, error) {
	sqlTx, err := unwrapSeedTx(tx)
	if err != nil {
		return false, err
	}
	return a.ap.Adoptable(context.Background(), sqlTx, key)
}

func (a notifyApplier) Upsert(tx seed.Tx, r seed.Record, adopt bool) error {
	sqlTx, err := unwrapSeedTx(tx)
	if err != nil {
		return err
	}
	return a.ap.Upsert(context.Background(), sqlTx, r, adopt)
}

func (a notifyApplier) Retire(tx seed.Tx, key string) error {
	sqlTx, err := unwrapSeedTx(tx)
	if err != nil {
		return err
	}
	return a.ap.Retire(context.Background(), sqlTx, key)
}

type agentApplier struct {
	ap *agentseed.Applier
}

func (a agentApplier) Set() string                   { return a.ap.Set() }
func (a agentApplier) GuardAffecting() bool          { return a.ap.GuardAffecting() }
func (a agentApplier) Operator() seed.OperatorRights { return a.ap.Operator() }

func (a agentApplier) Fingerprint(tx seed.Tx, key string) (string, bool, error) {
	sqlTx, err := unwrapSeedTx(tx)
	if err != nil {
		return "", false, err
	}
	return a.ap.Fingerprint(context.Background(), sqlTx, key)
}

func (a agentApplier) Adoptable(tx seed.Tx, key string) (bool, error) {
	sqlTx, err := unwrapSeedTx(tx)
	if err != nil {
		return false, err
	}
	return a.ap.Adoptable(context.Background(), sqlTx, key)
}

func (a agentApplier) Upsert(tx seed.Tx, r seed.Record, adopt bool) error {
	sqlTx, err := unwrapSeedTx(tx)
	if err != nil {
		return err
	}
	return a.ap.Upsert(context.Background(), sqlTx, r, adopt)
}

func (a agentApplier) Retire(tx seed.Tx, key string) error {
	sqlTx, err := unwrapSeedTx(tx)
	if err != nil {
		return err
	}
	return a.ap.Retire(context.Background(), sqlTx, key)
}

// policyApplier is the S45 T12 twin of agentApplier: it adapts the
// policyseed applier (store-backed for reads, boundary-backed for writes —
// never a seed.Applier directly for the reason agentseed records) to the
// seed.Applier seam. The daemon owns it because it holds the two doors the
// applier writes through: the *Store (the one place SQL lives) and the
// policy write BOUNDARY (AddInTx/RemoveInTx/RecordAuthored — there is no
// raw policy write below the boundary, so the seed has none either).
//
// The applier's methods take a context the seam does not; context.Background
// for the same reason as agentApplier's: a seed apply is a short
// transactional write governed by the transaction's own context.
type policyApplier struct {
	ap *policyseed.Applier
}

func (a policyApplier) Set() string                   { return a.ap.Set() }
func (a policyApplier) GuardAffecting() bool          { return a.ap.GuardAffecting() }
func (a policyApplier) Operator() seed.OperatorRights { return a.ap.Operator() }

func (a policyApplier) Fingerprint(tx seed.Tx, key string) (string, bool, error) {
	sqlTx, err := unwrapSeedTx(tx)
	if err != nil {
		return "", false, err
	}
	return a.ap.Fingerprint(context.Background(), sqlTx, key)
}

// watchdogApplier adapts the watchdogseed applier (which holds the *Store
// and takes *sql.Tx, so it is not a seed.Applier — see its note) to the
// seed.Applier seam, exactly as agentApplier does: the seed.Tx the engine
// hands it is the concrete *sql.Tx the daemon's WithTx wrapped, unwrapped
// here and handed to the applier, so no SQL lives outside internal/store.
type watchdogApplier struct {
	ap *watchdogseed.Applier
}

func (a watchdogApplier) Set() string                   { return a.ap.Set() }
func (a watchdogApplier) GuardAffecting() bool          { return a.ap.GuardAffecting() }
func (a watchdogApplier) Operator() seed.OperatorRights { return a.ap.Operator() }

func (a watchdogApplier) Fingerprint(tx seed.Tx, key string) (string, bool, error) {
	sqlTx, err := unwrapSeedTx(tx)
	if err != nil {
		return "", false, err
	}
	return a.ap.Fingerprint(context.Background(), sqlTx, key)
}

func (a policyApplier) Adoptable(tx seed.Tx, key string) (bool, error) {
	sqlTx, err := unwrapSeedTx(tx)
	if err != nil {
		return false, err
	}
	return a.ap.Adoptable(context.Background(), sqlTx, key)
}

func (a watchdogApplier) Adoptable(tx seed.Tx, key string) (bool, error) {
	sqlTx, err := unwrapSeedTx(tx)
	if err != nil {
		return false, err
	}
	return a.ap.Adoptable(context.Background(), sqlTx, key)
}

func (a policyApplier) Upsert(tx seed.Tx, r seed.Record, adopt bool) error {
	sqlTx, err := unwrapSeedTx(tx)
	if err != nil {
		return err
	}
	return a.ap.Upsert(context.Background(), sqlTx, r, adopt)
}

func (a watchdogApplier) Upsert(tx seed.Tx, r seed.Record, adopt bool) error {
	sqlTx, err := unwrapSeedTx(tx)
	if err != nil {
		return err
	}
	return a.ap.Upsert(context.Background(), sqlTx, r, adopt)
}

func (a policyApplier) Retire(tx seed.Tx, key string) error {
	sqlTx, err := unwrapSeedTx(tx)
	if err != nil {
		return err
	}
	return a.ap.Retire(context.Background(), sqlTx, key)
}

func (a watchdogApplier) Retire(tx seed.Tx, key string) error {
	sqlTx, err := unwrapSeedTx(tx)
	if err != nil {
		return err
	}
	return a.ap.Retire(context.Background(), sqlTx, key)
}

// mcpApplier adapts an mcpseed.Applier to the engine's seed.Tx seam — the
// policyApplier shape reused verbatim for the same reason: the engine
// hands a seed.Tx, the store methods take *sql.Tx, and the SQL lives in
// internal/store (SD1).
type mcpApplier struct {
	ap *mcpseed.Applier
}

func (a mcpApplier) Set() string                   { return a.ap.Set() }
func (a mcpApplier) GuardAffecting() bool          { return a.ap.GuardAffecting() }
func (a mcpApplier) Operator() seed.OperatorRights { return a.ap.Operator() }

func (a mcpApplier) Fingerprint(tx seed.Tx, key string) (string, bool, error) {
	sqlTx, err := unwrapSeedTx(tx)
	if err != nil {
		return "", false, err
	}
	return a.ap.Fingerprint(context.Background(), sqlTx, key)
}

func (a mcpApplier) Adoptable(tx seed.Tx, key string) (bool, error) {
	sqlTx, err := unwrapSeedTx(tx)
	if err != nil {
		return false, err
	}
	return a.ap.Adoptable(context.Background(), sqlTx, key)
}

func (a mcpApplier) Upsert(tx seed.Tx, r seed.Record, adopt bool) error {
	sqlTx, err := unwrapSeedTx(tx)
	if err != nil {
		return err
	}
	return a.ap.Upsert(context.Background(), sqlTx, r, adopt)
}

func (a mcpApplier) Retire(tx seed.Tx, key string) error {
	sqlTx, err := unwrapSeedTx(tx)
	if err != nil {
		return err
	}
	return a.ap.Retire(context.Background(), sqlTx, key)
}

// NewSeedEngine builds the one seed engine the daemon (and cmd/aeon) hold:
// over the store's repo, with the store-backed appliers the daemon owns
// (agentseed and watchdogseed). The registry is the package-global one the
// domains registered into at init (decl.go); the appliers map overrides the
// registry's init-time stubs with the store-backed appliers the daemon
// holds. Exported so cmd/aeon builds the one instance and the daemon's
// Options takes it.
//
// S45 T12 mounts the policy.corpus set HERE rather than from a policyseed
// init(): the package-global registry is also what test engines build
// against with their own appliers maps (seed.NewEngine refuses a registry
// set with no applier), and a domain may not widen it from a distance. The
// one engine every door holds — serve's, the seed CLI's direct door, the
// corpus door — is built by this function, so mounting here is the
// registration that matters: this engine carries the corpus set, its class
// stays guard (ApplyInertAtStart cannot reach it — D4 by construction), and
// the plain test engines carry exactly the sets their tests declared.
func NewSeedEngine(s *store.Store, opt seed.Options, mcpValidate ...mcpseed.RowValidator) (*seed.Engine, error) {
	var validator mcpseed.RowValidator
	if len(mcpValidate) > 0 {
		validator = mcpValidate[0]
	}
	if opt.Registry == nil {
		opt.Registry = seed.DefaultRegistry()
	}
	if opt.Appliers == nil {
		opt.Appliers = map[string]seed.Applier{}
	}
	// The agents applier the daemon owns: the store-backed applier over the
	// registry's init-time stub (decl.go's Adapter, which refused until now).
	opt.Appliers[agentseed.SetID] = agentApplier{ap: agentseed.NewApplier(s)}
	// The watchdog set's applier the same way (T9, LC18): the inert default
	// rule set, applied at start before the watchdog service's first tick.
	// Mounted BEFORE the S45 T12 corpus block on purpose (2026-11-02 merge):
	// that block rebuilds the registry from the appliers map when policy is
	// not init-declared, and a set missing from the map at rebuild time is a
	// refused engine (seed: set has no applier) — every mount the rebuild
	// must see has to stand above it.
	opt.Appliers[watchdogseed.SetID] = watchdogApplier{ap: watchdogseed.NewApplier(s)}
	// S48 T5: the shipped blueprints, inert — the `blueprints.standard`
	// set applies at daemon start AFTER the file adoption (serve runs it
	// before this) and before loops are accepted; rows are written only
	// through store.BlueprintPutTx. Mounted above the corpus rebuild block
	// for the reason the watchdog mount records: a set missing from the
	// appliers map at rebuild time is a refused engine.
	opt.Appliers[blueprintseed.SetID] = blueprintApplier{ap: blueprintseed.NewApplier(s)}
	// S60 T7: the LANDING blueprints (land-feature, resolve-conflict),
	// inert like every shipped set (P8) — the `blueprints.landing` set
	// applies through the SAME applier machinery (blueprintseed's applier
	// parameterised by set, landingseed owning the rows). Loops cannot
	// start either blueprint until these rows stand; the reactor starts
	// landing loops only for light repositories, and a repository can be
	// lightened only through the settings door.
	opt.Appliers[landingseed.SetID] = blueprintApplier{ap: blueprintseed.NewApplierFor(s, landingseed.SetID, landingseed.Source)}
	// S57 T10: the shipped candidate gates, inert — the `verify.templates`
	// set writes verify_template rows only (VG21: a template does nothing
	// until a person applies it), through the store's one seed write path.
	// Mounted above the corpus rebuild block for the reason the watchdog
	// mount records: a set missing from the appliers map at rebuild time is
	// a refused engine.
	opt.Appliers[verifyseed.SetID] = verifyApplier{ap: verifyseed.NewApplier(s)}
	// S63 T3 (RT16): the shipped retro template bodies, inert — the
	// `retro.templates` set writes retro_template rows only (a body is a
	// renderer's input, never a verdict), through the store's T3 Tx doors.
	// Mounted above the corpus rebuild block for the reason the watchdog
	// mount records: a set missing from the appliers map at rebuild time is
	// a refused engine.
	opt.Appliers[retroseed.SetID] = retroTemplateApplier{ap: retroseed.NewApplier(s)}
	// S61 T3 (GI14/GI15): the shipped starter templates, inert — the
	// `templates.scaffold` set writes scaffold_template rows only (a menu
	// entry scaffolds nothing; repo.scaffold is a person's action), through
	// the store's T3 Tx twins. Mounted above the corpus rebuild block for
	// the reason the watchdog mount records: a set missing from the
	// appliers map at rebuild time is a refused engine.
	opt.Appliers[scaffoldseed.SetID] = templateApplier{ap: scaffoldseed.NewApplier(s)}
	// S58 T4 (SV4): the shipped event classes, inert — the `notify.events`
	// set writes notify_event_class rows only (a hint notifies, it permits
	// nothing), through the store's GetNotifyClassTx/UpsertNotifyClassTx.
	// Mounted above the corpus rebuild block for the reason the watchdog
	// mount records: a set missing from the appliers map at rebuild time is
	// a refused engine.
	opt.Appliers[notifyseed.SetID] = notifyApplier{ap: notifyseed.NewApplier(s)}
	// The corpus applier the daemon owns (S45 T12), mounted beside it — the
	// boundary's tx hooks are this applier's only write path.
	opt.Appliers[policyseed.SetID] = policyApplier{ap: policyseed.NewApplier(s, policy.NewStore(newPolicyRepo(s)), time.Now)}
	// The first-party agents' shipped permissions (2026-11-15): class DEFAULT, so it applies at start with
	// no setup, through the same write boundary and the same ledger discipline as the corpus.
	opt.Appliers[policyseed.FirstPartySetID] = policyApplier{ap: policyseed.NewFirstPartyApplier(s, policy.NewStore(newPolicyRepo(s)), time.Now)}
	// The two MCP seed sets the daemon owns (S46 T10, MG10): inert
	// definitions and guard widenings, mounted beside the corpus — the
	// same function-mount ruling (a domain may not widen the package-global
	// registry from a distance; policyseed/decl.go records why). Production
	// records are EMPTY (ServersRecords/GrantsRecords), so mounting costs
	// an honest 0/0 line in seed.status and nothing else; the class split
	// is structural — ApplyInertAtStart can never reach the guard set, and
	// the one validator reaches every record through the seam cmd/aeon
	// supplies (nil fails closed at apply; empty production records never
	// reach it).
	opt.Appliers[mcpseed.SetServers] = mcpApplier{ap: mcpseed.NewServersApplier(s, validator, time.Now)}
	opt.Appliers[mcpseed.SetGrants] = mcpApplier{ap: mcpseed.NewGrantsApplier(s, validator, time.Now)}
	if _, declared := opt.Registry.Lookup(policyseed.SetID); !declared {
		sets := append(opt.Registry.All(), policyseed.Declaration(), policyseed.FirstPartyDeclaration())
		for _, d := range mcpseed.Declarations() {
			if _, have := opt.Registry.Lookup(d.ID); !have {
				sets = append(sets, d)
			}
		}
		appliers := make([]seed.Applier, 0, len(sets))
		for _, st := range sets {
			a, ok := opt.Appliers[st.ID]
			if !ok {
				// The engine's own refusal will fire in NewEngine for a set
				// this daemon does not hold an applier for; building the
				// registry list is not the place to invent one.
				continue
			}
			appliers = append(appliers, a)
		}
		reg, err := seed.NewRegistry(sets, appliers)
		if err != nil {
			return nil, err
		}
		opt.Registry = reg
	}
	// The watchdog set's applier the same way (T9, LC18): the inert default
	// rule set, applied at start before the watchdog service's first tick.
	opt.Appliers[watchdogseed.SetID] = watchdogApplier{ap: watchdogseed.NewApplier(s)}
	// S47 T3: the tool catalog set. Its applier mounts above the rebuilds for
	// the same reason the watchdog's does — a set the rebuild cannot see an
	// applier for is a refused engine — and its declaration is mounted here
	// rather than from a `toolseed` init() for policyseed/decl.go's recorded
	// reason (the package-global registry is also what test engines build
	// against with their own appliers maps). The set is INERT, so
	// ApplyInertAtStart reaches it and nothing about a start widens: the rows
	// are the catalog's display copy, and no execution path reads them (TC2,
	// pinned in `internal/store` and here by `TestRuntimeStillBuildsSpecsFromTheRegistry`).
	opt.Appliers[toolseed.SetID] = toolApplier{ap: toolseed.NewApplier(s)}
	if _, declared := opt.Registry.Lookup(toolseed.SetID); !declared {
		sets := append(opt.Registry.All(), toolseed.Declaration())
		appliers := make([]seed.Applier, 0, len(sets))
		for _, st := range sets {
			a, ok := opt.Appliers[st.ID]
			if !ok {
				continue // The engine's own refusal names a set with no applier; do not invent one here.
			}
			appliers = append(appliers, a)
		}
		reg, err := seed.NewRegistry(sets, appliers)
		if err != nil {
			return nil, err
		}
		opt.Registry = reg
	}
	// S59 T6 (EV19): the `environments.standard` set — the system
	// `host-scrubbed` environment and the workspace's first binding. Inert, and
	// inert is the whole argument for it applying at start: it creates a row
	// that does not exist and binds the workspace ONLY where nothing is bound,
	// so a person's choice is never moved back to the shipped default by a
	// restart. Declared here rather than from an init() for the recorded reason
	// (policyseed, mcpseed, toolseed): the global registry is what test engines
	// build against, and a set they have no applier for is a refused engine.
	opt.Appliers[envseed.SetID] = envseedApplier{ap: envseed.NewApplier(s)}
	if _, declared := opt.Registry.Lookup(envseed.SetID); !declared {
		sets := append(opt.Registry.All(), envseed.Declaration())
		appliers := make([]seed.Applier, 0, len(sets))
		for _, st := range sets {
			a, ok := opt.Appliers[st.ID]
			if !ok {
				continue // The engine's own refusal names a set with no applier; do not invent one here.
			}
			appliers = append(appliers, a)
		}
		reg, err := seed.NewRegistry(sets, appliers)
		if err != nil {
			return nil, err
		}
		opt.Registry = reg
	}
	// S61 T9 (GI15, P8): the goal-intake pair — `agents.intake` (the
	// intake-architect body, its own set; firstParty's census untouched by
	// the content.go exclusion) and `blueprints.intake` (the goal-intake
	// blueprint through the SAME blueprintseed machinery landingseed proved,
	// R4: an applier may serve a second set). Both inert; both ride the
	// daemon's own appliers; both declared here rather than from an init()
	// for the recorded reason (policyseed, mcpseed, toolseed, envseed): the
	// global registry is what test engines build against, and a set they
	// have no applier for is a refused engine. Applied at start like every
	// inert set — a row that permits nothing; the plane's writes stay the
	// person's doors.
	opt.Appliers[agentseed.IntakeSetID] = agentApplier{ap: agentseed.NewApplierFor(s, agentseed.IntakeSetID)}
	if _, declared := opt.Registry.Lookup(agentseed.IntakeSetID); !declared {
		sets := append(opt.Registry.All(), agentseed.IntakeDeclaration())
		appliers := make([]seed.Applier, 0, len(sets))
		for _, st := range sets {
			a, ok := opt.Appliers[st.ID]
			if !ok {
				continue // The engine's own refusal names a set with no applier; do not invent one here.
			}
			appliers = append(appliers, a)
		}
		reg, err := seed.NewRegistry(sets, appliers)
		if err != nil {
			return nil, err
		}
		opt.Registry = reg
	}
	opt.Appliers[intakeseed.SetID] = blueprintApplier{ap: blueprintseed.NewApplierFor(s, intakeseed.SetID, intakeseed.Source)}
	if _, declared := opt.Registry.Lookup(intakeseed.SetID); !declared {
		sets := append(opt.Registry.All(), intakeseed.Declaration())
		appliers := make([]seed.Applier, 0, len(sets))
		for _, st := range sets {
			a, ok := opt.Appliers[st.ID]
			if !ok {
				continue // The engine's own refusal names a set with no applier; do not invent one here.
			}
			appliers = append(appliers, a)
		}
		reg, err := seed.NewRegistry(sets, appliers)
		if err != nil {
			return nil, err
		}
		opt.Registry = reg
	}
	return seed.NewEngine(NewSeedRepo(s), opt)
}

// envseedApplier is the engine-facing adapter over the standard environment
// set's applier — the mcpApplier/toolApplier shape again: the engine hands a
// `seed.Tx` and the applier wants the `*sql.Tx` underneath it, so the
// environment row, its first binding and their audit rows share the one
// transaction the engine opened. A split across transactions would be the
// failure EV19 cannot have: a workspace bound to an environment whose row never
// committed.
type envseedApplier struct {
	ap *envseed.Applier
}

func (a envseedApplier) Set() string                   { return a.ap.Set() }
func (a envseedApplier) GuardAffecting() bool          { return a.ap.GuardAffecting() }
func (a envseedApplier) Operator() seed.OperatorRights { return a.ap.Operator() }

func (a envseedApplier) Fingerprint(tx seed.Tx, key string) (string, bool, error) {
	sqlTx, err := unwrapSeedTx(tx)
	if err != nil {
		return "", false, err
	}
	return a.ap.Fingerprint(context.Background(), sqlTx, key)
}

func (a envseedApplier) Adoptable(tx seed.Tx, key string) (bool, error) {
	sqlTx, err := unwrapSeedTx(tx)
	if err != nil {
		return false, err
	}
	return a.ap.Adoptable(context.Background(), sqlTx, key)
}

func (a envseedApplier) Upsert(tx seed.Tx, r seed.Record, adopt bool) error {
	sqlTx, err := unwrapSeedTx(tx)
	if err != nil {
		return err
	}
	return a.ap.Upsert(context.Background(), sqlTx, r, adopt)
}

func (a envseedApplier) Retire(tx seed.Tx, key string) error {
	sqlTx, err := unwrapSeedTx(tx)
	if err != nil {
		return err
	}
	return a.ap.Retire(context.Background(), sqlTx, key)
}

// toolApplier is the engine-facing adapter over the tool catalog applier, the
// same shape as mcpApplier: the engine hands a seed.Tx and the applier wants the
// *sql.Tx underneath it, so the row and its ledger row share one transaction.
type toolApplier struct {
	ap *toolseed.Applier
}

func (a toolApplier) Set() string                   { return a.ap.Set() }
func (a toolApplier) GuardAffecting() bool          { return a.ap.GuardAffecting() }
func (a toolApplier) Operator() seed.OperatorRights { return a.ap.Operator() }

func (a toolApplier) Fingerprint(tx seed.Tx, key string) (string, bool, error) {
	sqlTx, err := unwrapSeedTx(tx)
	if err != nil {
		return "", false, err
	}
	return a.ap.Fingerprint(context.Background(), sqlTx, key)
}

func (a toolApplier) Adoptable(tx seed.Tx, key string) (bool, error) {
	sqlTx, err := unwrapSeedTx(tx)
	if err != nil {
		return false, err
	}
	return a.ap.Adoptable(context.Background(), sqlTx, key)
}

func (a toolApplier) Upsert(tx seed.Tx, r seed.Record, adopt bool) error {
	sqlTx, err := unwrapSeedTx(tx)
	if err != nil {
		return err
	}
	return a.ap.Upsert(context.Background(), sqlTx, r, adopt)
}

func (a toolApplier) Retire(tx seed.Tx, key string) error {
	sqlTx, err := unwrapSeedTx(tx)
	if err != nil {
		return err
	}
	return a.ap.Retire(context.Background(), sqlTx, key)
}

// corpusSeedDoor is the protocol SeedService seam the daemon wires (S45
// T12): the engine's four methods ride untouched EXCEPT seed.apply for the
// policy.corpus set, which is routed through policyseed.ApplyCorpus — the
// corpus door that adds the actor rule and the run's ONE policy_change
// op='seed' audit row to the engine's apply. Routing it here means every
// door that reaches seed.apply (the socket, and the CLI direct door which
// mirrors this file's mapping) books the corpus the same way; a bare
// engine.Apply for the corpus set stays the engine's generic door for
// tests, and nothing in production reaches seed.apply without this wrapper.
type corpusSeedDoor struct {
	e *seed.Engine
	s *store.Store
}

// newCorpusSeedDoor keeps V13's nil discipline: a storeless daemon leaves
// the seam nil (RegisterSeed leaves seed.* ABSENT, not
// registered-and-refusing), exactly as the bare wiring did before.
func newCorpusSeedDoor(e *seed.Engine, s *store.Store) protocol.SeedService {
	if e == nil {
		return nil
	}
	return corpusSeedDoor{e: e, s: s}
}

func (d corpusSeedDoor) Status(ctx context.Context, setID string) ([]seed.SetStatus, error) {
	return d.e.Status(ctx, setID)
}

func (d corpusSeedDoor) Plan(ctx context.Context, setID string, keys []string) (seed.SetPlan, error) {
	return d.e.Plan(ctx, setID, keys)
}

func (d corpusSeedDoor) Apply(ctx context.Context, setID string, keys []string, by seed.Actor, via seed.Path) (seed.ApplyResult, error) {
	if setID == policyseed.SetID {
		r, err := policyseed.ApplyCorpus(ctx, d.e, d.s, nil, keys, false, by, time.Now())
		if err != nil {
			return seed.ApplyResult{}, err
		}
		return r.Applied, nil
	}
	return d.e.Apply(ctx, setID, keys, by, via)
}

func (d corpusSeedDoor) Restore(ctx context.Context, setID, key string, by seed.Actor) (seed.ApplyResult, error) {
	return d.e.Restore(ctx, setID, key, by)
}

// ApplyInertAtStart is the daemon-start hook (S43-extra T6, SD5): it applies
// every inert set, actor system:startup, before loops are accepted. A
// failure to apply an inert set is logged and visible in seed.status as
// failed records but does NOT abort start (a half-seeded agent set must not
// brick the daemon) — and dispatch refuses a missing agent by name (T5), so
// the failure is loud where it matters. It logs one line per set, never
// silent.
func ApplyInertAtStart(ctx context.Context, e *seed.Engine) {
	if e == nil {
		return
	}
	results, err := e.ApplyInert(ctx, seed.Actor{Kind: "system", ID: "startup"})
	if err != nil {
		// A whole-set failure is logged, not fatal: the daemon starts, the
		// set stays absent (seed.status shows it), and dispatch refuses a
		// missing agent by name.
		log.Printf("seed: startup apply of an inert set failed: %v", err)
		return
	}
	for _, r := range results {
		// A record is FAILED only when the engine says so ("failed — …", engine.go);
		// a skip carries a reason too ("up to date", "diverged …") and is not a
		// failure — counting every reason as one made a healthy, fully-seeded
		// store log "applied 0 (33 failed)" on every start.
		var applied, skipped, failed int
		for _, rec := range r.Records {
			switch {
			case strings.HasPrefix(rec.Reason, "failed"):
				failed++
			case rec.Op == seed.OpSkip:
				skipped++
			default:
				applied++
			}
		}
		log.Printf("seed: %s applied %d, skipped %d, failed %d", r.SetID, applied, skipped, failed)
	}
}
