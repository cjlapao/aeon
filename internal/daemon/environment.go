package daemon

// environment.go — S59 T6 (EV2, EV16, EV17, EV18): the daemon's one
// environment service, behind the thirteen socket doors.
//
// This is the only daemon file that writes an environment, a binding, or a
// provision, and the only place `execenv.Validate` is consulted for a wire
// write. Three properties the rest of the plane rests on:
//
//   - **One validator, verbatim refusals.** Every write parses the spec with
//     `execenv.ParseSpec` and gates it with `execenv.Validate`, and ALL of the
//     validator's refusals are returned together — a person fixing a form
//     learns about every problem in one round trip, and nothing is paraphrased:
//     the sentences are the validator's (§4d's one-formatter rule, carried onto
//     this plane). A refusal writes no row, no revision and no audit entry
//     (EV17: refusals are not changing writes).
//   - **The row holds what the validator approved.** The stored spec is
//     `execenv.Canonical` — names and literals, no value can be in it because
//     none is accepted (EV7) — so a later read returns bytes the validator
//     signed, and `Restricts` (EV16) compares two things that came from the
//     same function.
//   - **The actor is the connection's person.** The protocol door resolved it
//     from the peer credential; this service stamps it onto the store call and
//     the audit row, and no params field can reach it (EV17).
//
// `Effective` is the one read that is not a row lookup: it goes through
// `StepExecResolver.Preview`, the SAME composition a step goes through, so the
// panel's preview is not a description of what a step would get — it is the
// resolution, stopped one step before the spawn.

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/cjlapao/aeon/internal/config"
	"github.com/cjlapao/aeon/internal/execenv"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

// errNoStoreForEnvironment is the storeless daemon speaking: no environments,
// no bindings, no references — the doors refuse rather than inventing a
// permissive answer (EV2's fail-closed, the same posture as a nil policy seam).
var errNoStoreForEnvironment = errors.New("daemon: this daemon has no store, so it has no execution environments")

// environmentService implements protocol.EnvironmentService.
type environmentService struct {
	store    *store.Store
	resolver *StepExecResolver
	log      Logger
	// settings is S44's read-only surface (CS11). It is the ONE way this plane
	// reaches a daemon-wide value; the declared key lives in
	// internal/config/settings_decl_execenv.go and the write door is
	// `settings.set` on the human surface, never a method of this service.
	settings config.Reader
}

// newEnvironmentService wires the service. A nil store yields a service that
// refuses every method with its own sentence — and the daemon wires the doors
// only when it has a store at all, so a nil seam (V13) stays the honest
// absence rather than thirteen doors that can only say "no store".
func newEnvironmentService(st *store.Store, resolver *StepExecResolver, log Logger, settings config.Reader) *environmentService {
	return &environmentService{store: st, resolver: resolver, log: log, settings: settings}
}

func (s *environmentService) haveStore() error {
	if s == nil || s.store == nil {
		return errNoStoreForEnvironment
	}
	return nil
}

// --- the refusal voice -------------------------------------------------------------

// environmentRefused renders one store refusal as the plane's single error
// (DB19, the verifyRefusedError shape): one code, the closed-set kind in
// Data.Kind, and the sentence the store actually spoke in Data.What. Nothing
// here rewrites a sentence — a paraphrase would be a second authority over a
// fact the store owns.
func environmentRefused(ref *store.EnvironmentRefusal) *protocol.Error {
	e := protocol.ErrEnvironmentRefused.WithWhat("%s", ref.Sentence)
	e.Data.Kind = ref.Kind
	return e
}

// environmentRefusalFrom renders an execenv refusal (validator, resolver or
// builder) the same way, so a caller sees one error shape whether the sentence
// came from the validator or the row. The `do` clause travels with it: it is
// the part a person acts on, and dropping it would leave a refusal that names
// the problem and not the way out.
func environmentRefusalFrom(ref *execenv.Refusal) *protocol.Error {
	e := protocol.ErrEnvironmentRefused.WithWhat("%s · %s · do: %s", ref.What, ref.Why, ref.Do)
	e.Data.Kind = string(ref.Kind)
	return e
}

// validateSpecForWriting parses and validates a candidate spec, answering with
// the canonical JSON the row will hold. Every problem is reported together.
func (s *environmentService) validateSpecForWriting(text, backend string) (string, *protocol.Error) {
	spec, err := execenv.ParseSpec([]byte(text))
	if err != nil {
		e := protocol.ErrEnvironmentRefused.WithWhat("the spec is not something this daemon can read: %v", err)
		e.Data.Kind = string(execenv.RefusalSpecInvalid)
		return "", e
	}
	refusals := execenv.Validate(spec, backend, execenv.ValidateOpts{Drivers: s.registeredBackends()})
	if len(refusals) > 0 {
		sentences := make([]string, 0, len(refusals))
		for i := range refusals {
			sentences = append(sentences, refusals[i].What)
		}
		e := protocol.ErrEnvironmentRefused.WithWhat("the validator refused this spec (%d problem(s)): %s",
			len(refusals), strings.Join(sentences, " | "))
		e.Data.Kind = string(refusals[0].Kind)
		return "", e
	}
	// `Canonical` is total: it renders a validated Spec, and a Spec that came
	// out of ParseSpec and Validate always renders. Storing THAT, and not the
	// caller's bytes, is what lets a later read return the text the validator
	// signed and lets `Restricts` compare two specs by the same rules (EV16).
	return string(execenv.Canonical(spec)), nil
}

// registeredBackends names the drivers this daemon can actually run, so
// `Validate`'s registry question ("is there a driver for this backend?") is
// answered by the registry and not by a string comparison in a door (EV3).
func (s *environmentService) registeredBackends() []string {
	names := make([]string, 0, len(hostBackends)+4)
	for b := range hostBackends {
		names = append(names, b)
	}
	if s.resolver != nil {
		for b := range s.resolver.Backends {
			names = append(names, b)
		}
	}
	sort.Strings(names)
	return names
}

// backendNames is the closed backend set, for a refusal that lists what exists.
func backendNames() []string {
	bs := execenv.Backends()
	out := make([]string, 0, len(bs))
	for _, b := range bs {
		out = append(out, b.Name)
	}
	return out
}

// --- the views ---------------------------------------------------------------------

// environmentView shapes one row for the wire: the row's own fields, the
// backend's capabilities, the validator's warnings (D8's floating tag is a
// warning and never a refusal), the references it injects by name, and where it
// is bound. A spec that does not parse is a REFUSAL to render, not an empty
// view: a panel showing `{}` for a mangled spec would show an environment with
// no allowlist as an environment with no allowlist, and a person would read
// that as "nothing is passed" rather than "this row is broken".
func (s *environmentService) environmentView(ctx context.Context, row store.Environment) (protocol.EnvironmentView, error) {
	spec, err := execenv.ParseSpec([]byte(row.Spec))
	if err != nil {
		return protocol.EnvironmentView{}, environmentRefusalFrom(execenv.NewRefusal(execenv.RefusalSpecInvalid,
			"the stored spec of "+row.Slug+" does not parse: "+err.Error(),
			"an environment whose spec cannot be read cannot be described honestly, run in, or compared with another",
			"repair the spec through environment.update; a row the daemon cannot read is not an environment it can describe"))
	}
	refs, err := s.store.EnvironmentSecretRefs(row.ID)
	if err != nil {
		return protocol.EnvironmentView{}, err
	}
	bindings, err := s.store.ListBindings()
	if err != nil {
		return protocol.EnvironmentView{}, err
	}
	var views []protocol.BindingView
	for _, b := range bindings {
		if b.EnvironmentID != row.ID {
			continue
		}
		view := protocol.BindingView{ScopeKind: b.ScopeKind, EnvironmentID: b.EnvironmentID, BoundAt: b.BoundAt}
		if b.ComponentID != nil {
			view.ComponentID = *b.ComponentID
		}
		views = append(views, view)
	}
	return protocol.EnvironmentView{
		ID: row.ID, Slug: row.Slug, Name: row.Name, Description: row.Description,
		Backend: row.Backend, Origin: row.Origin, Legacy: row.Legacy == 1,
		Revision: row.Revision, Spec: json.RawMessage(row.Spec),
		CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt, ArchivedAt: row.ArchivedAt,
		Capabilities:   execenv.Capabilities(row.Backend),
		Warnings:       execenv.Warnings(spec, row.Backend),
		SecretRefNames: refs,
		Bindings:       views,
	}, nil
}

// --- the reads ---------------------------------------------------------------------

func (s *environmentService) List(ctx context.Context, p protocol.EnvironmentListParams) ([]protocol.EnvironmentView, error) {
	if err := s.haveStore(); err != nil {
		return nil, err
	}
	rows, err := s.store.ListEnvironments(store.EnvironmentFilter{
		Backend: p.Backend, Origin: p.Origin, IncludeArchived: p.IncludeArchived, Limit: p.Limit,
	})
	if err != nil {
		return nil, err
	}
	// The two reads per row below are N+1 by design: a listing is a person's
	// screen over a table this plane keeps small by hand, the capability chips
	// come from the code (no read at all), and a join that hid the two keyed
	// reads would also hide one row's failure inside the listing's success.
	out := make([]protocol.EnvironmentView, 0, len(rows))
	for _, row := range rows {
		v, err := s.environmentView(ctx, row)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

func (s *environmentService) Show(ctx context.Context, p protocol.EnvironmentShowParams) (protocol.EnvironmentView, error) {
	if err := s.haveStore(); err != nil {
		return protocol.EnvironmentView{}, err
	}
	id := strings.TrimSpace(p.ID)
	if id == "" {
		id = strings.TrimSpace(p.Slug)
	}
	row, err := s.store.GetEnvironment(id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return protocol.EnvironmentView{}, s.unknownEnvironment(id)
		}
		return protocol.EnvironmentView{}, err
	}
	if strings.TrimSpace(p.ID) != "" && strings.TrimSpace(p.Slug) != "" && strings.TrimSpace(p.Slug) != row.Slug {
		e := protocol.ErrEnvironmentRefused.WithWhat("environment.show was given an id and a slug that name different environments (%s is %q, not %q) — naming one is enough, and naming two that disagree is a request nobody means",
			p.ID, row.Slug, p.Slug)
		e.Data.Kind = "id-slug-disagree"
		return protocol.EnvironmentView{}, e
	}
	return s.environmentView(ctx, row)
}

// unknownEnvironment names the environments that DO exist, because the usual
// cause is a name typed from memory and a list to pick from is one round trip
// instead of a guessing game (the ErrToolUnknown argument, EV18).
func (s *environmentService) unknownEnvironment(id string) *protocol.Error {
	rows, err := s.store.ListEnvironments(store.EnvironmentFilter{IncludeArchived: true, Limit: 200})
	if err != nil {
		e := protocol.ErrEnvironmentRefused.WithWhat("%q is not an environment, and the list could not be read to help (%v)", id, err)
		e.Data.Kind = "environment-unknown"
		return e
	}
	names := make([]string, 0, len(rows))
	for _, r := range rows {
		names = append(names, r.Slug)
	}
	e := protocol.ErrEnvironmentRefused.WithWhat("%q is not an environment (id or slug). This store has: %s",
		id, strings.Join(names, ", "))
	e.Data.Kind = "environment-unknown"
	return e
}

// Effective answers what a step on that component would run in — names,
// sources, capabilities and the downgrade note, and no value anywhere (EV5,
// EV7; T6's scanner test proves it on the wire). It answers through
// `StepExecResolver.Preview`, the same composition `For` uses, so a preview that
// disagrees with reality is a bug in one place rather than a drift between two.
func (s *environmentService) Effective(ctx context.Context, p protocol.EnvironmentEffectiveParams) (protocol.EnvironmentEffectiveView, error) {
	if err := s.haveStore(); err != nil {
		return protocol.EnvironmentEffectiveView{}, err
	}
	if s.resolver == nil {
		return protocol.EnvironmentEffectiveView{}, protocol.ErrEnvironmentRefused.WithWhat(
			"this daemon has no execution resolver, so no step can run and nothing can be previewed — these doors are wired to a daemon built without them (V13)")
	}
	root, resolvedID, err := s.rootFor(p.ComponentID)
	if err != nil {
		return protocol.EnvironmentEffectiveView{}, err
	}
	// No step means no pin, and nobody at this door may declare attendedness
	// (G14 — see EnvironmentEffectiveParams). It is answered on the
	// CONSERVATIVE side: unattended is the answer that never shows an
	// inheritance the spawn would refuse, so a preview outside a step says
	// unattended and, on a legacy binding, says so with EV6's sentence.
	c, err := s.resolver.Preview(ctx, root, nil)
	if err != nil {
		return protocol.EnvironmentEffectiveView{}, err
	}
	if c.refusal != nil {
		// The refusal IS the answer to "what would this run in?": nothing. The
		// door refuses with the resolver's own sentence rather than answering
		// with an empty environment, which is the plausible-wrong-answer this
		// plane keeps refusing (a step that runs with `{}` while the record says
		// `production`).
		return protocol.EnvironmentEffectiveView{}, environmentRefusalFrom(c.refusal)
	}
	view := protocol.EnvironmentEffectiveView{
		ComponentID: resolvedID, Backend: c.res.Env.Backend,
		EnvironmentID: c.res.Env.ID, EnvironmentSlug: c.res.Env.Slug,
		Legacy:         c.res.Env.Legacy || c.child.Legacy(),
		Capabilities:   execenv.Capabilities(c.res.Env.Backend),
		Honesty:        execenv.Honesty,
		Attends:        false, // derived, never accepted: see the params comment (G14)
		SecretRefNames: c.refs,
	}
	if c.bound != nil {
		view.ScopeKind = scopeOf(c.in, c.bound)
	}
	for _, v := range c.child.Names() {
		view.Variables = append(view.Variables, protocol.VariableView{Name: v.Name, Source: v.Source})
	}
	switch {
	case c.child.Legacy():
		// The honest third state (P3): a legacy child sees the daemon's whole
		// environment, and a list of the spec's names would read as "that is
		// everything". The list is therefore left empty and the note says why.
		view.DowngradeNote = "this step inherits the daemon's own environment: what it can read is every name this process has, not a list " +
			"(EV1). Bind a host-scrubbed environment to narrow it."
	case c.res.Downgraded:
		view.DowngradeNote = "EV6 downgraded this step: the binding is a legacy (inheriting) environment and the loop is not attended, " +
			"so the step runs in the system host-scrubbed environment instead — an unattended loop never inherits the daemon's credentials."
	}
	return view, nil
}

// scopeOf names which scope the winning binding came from, for the view. The
// resolver already put it in the matching slot; the question here is which slot
// it is in. "system" is the honest name for "nothing was bound at a scope and
// EV6 supplied the system scrubbed row" — an answer the view must give.
func scopeOf(in execenv.ResolveInput, bound *execenv.BoundEnv) string {
	switch {
	case in.ComponentBinding != nil && in.ComponentBinding.ID == bound.ID:
		return "component"
	case in.RootBinding != nil && in.RootBinding.ID == bound.ID:
		return "root"
	case in.WorkspaceBinding != nil && in.WorkspaceBinding.ID == bound.ID:
		return "workspace"
	default:
		return "system"
	}
}

// rootFor turns the door's `componentId` into what the resolver needs, a
// component ROOT. A person reads ids in the panel and types paths in a shell, so
// both are accepted and the difference is said out loud: a known id is resolved
// to its root, an unknown one is handed on as a root path (the resolver reads a
// component by root anyway, and an unregistered root is refused by
// `ComponentRootKnown` at every exec door), and an empty one previews the
// WORKSPACE scope — the resolver's component lookup finds nothing, which is what
// a workspace preview is.
func (s *environmentService) rootFor(componentID string) (root, resolvedID string, err error) {
	componentID = strings.TrimSpace(componentID)
	if componentID == "" {
		return "", "", nil
	}
	got, err := s.store.RootOf(componentID)
	if err == nil {
		return got, componentID, nil
	}
	if !errors.Is(err, store.ErrNotFound) {
		return "", "", err
	}
	return componentID, "", nil
}

// Vocabulary serves the plane's own definitions: everything a client would
// otherwise retype or guess, plus the two honesty lines, each from the one place
// it is defined (EV1's sentence has exactly one copy).
func (s *environmentService) Vocabulary(ctx context.Context) (protocol.EnvironmentVocabularyView, error) {
	ttl := s.provisionIdleTTL(ctx)
	backends := execenv.Backends()
	views := make([]protocol.BackendView, 0, len(backends))
	for _, b := range backends {
		views = append(views, protocol.BackendView{Name: b.Name, Strength: b.Strength, Capabilities: b.Capabilities})
	}
	deny := execenv.Deny()
	return protocol.EnvironmentVocabularyView{
		Backends: views, DefaultAllowlist: execenv.DefaultPassthrough(),
		DenyWords: deny.Words, DenyPrefixes: deny.Prefixes, DenyExact: deny.Exact,
		DenyLoader: deny.Loader, DenyExempt: deny.Exempt, DenySetOnly: deny.SetOnly,
		ProxyWarnPatterns: execenv.ProxyWarnPatterns(),
		EgressModes:       execenv.EgressModes(), SourceKinds: execenv.SourceKinds(),
		PullPolicies: execenv.PullPolicies(), HomeModes: execenv.HomeModes(),
		Honesty:         execenv.Honesty,
		RedactionLimits: execenv.RedactionLimits,
		// EV15: what the platform knows it cannot do, and whether that is true
		// today. The panel renders these verbatim and keeps no status of its own
		// — a client-side "still true?" string is a folklore generator.
		KnownLimits: knownLimitViews(execenv.KnownLimits()),
		// The sweep interval, from the declared setting rather than a number
		// this file knows (S44). A panel that rendered its own default would be
		// a second authority over what the daemon will actually do tomorrow.
		ProvisionIdleTTL:        ttl.TTL.String(),
		ProvisionIdleTTLSeconds: int64(ttl.TTL / time.Second),
		ProvisionIdleTTLFrom:    ttl.From,
		ProvisionIdleTTLNote:    ttl.Refusal,
		ProvisionIdleTTLKey:     ttl.SettingKey,
	}, nil
}

// knownLimitViews copies the daemon's known-limitation list onto the wire. The
// words are `execenv`'s; this function only changes the type, because the
// protocol package may not import `execenv` any more than the panel may type the
// status.
func knownLimitViews(in []execenv.KnownLimit) []protocol.KnownLimitView {
	out := make([]protocol.KnownLimitView, 0, len(in))
	for _, k := range in {
		out = append(out, protocol.KnownLimitView{Key: k.Key, Sentence: k.Sentence, Active: k.Active})
	}
	return out
}

func (s *environmentService) Changes(ctx context.Context, p protocol.EnvironmentChangesParams) ([]protocol.EnvironmentChangeView, error) {
	if err := s.haveStore(); err != nil {
		return nil, err
	}
	rows, err := s.store.ListChanges(store.EnvironmentChangeFilter{
		SubjectKind: p.SubjectKind, SubjectID: p.SubjectID, Action: p.Action, Limit: p.Limit,
	})
	if err != nil {
		return nil, err
	}
	out := make([]protocol.EnvironmentChangeView, 0, len(rows))
	for _, r := range rows {
		v := protocol.EnvironmentChangeView{
			ID: r.ID, At: r.At, ActorKind: r.ActorKind, ActorID: r.ActorID,
			SubjectKind: r.SubjectKind, SubjectID: r.SubjectID, Action: r.Action,
			Revision: r.Revision, Summary: r.Summary,
		}
		if r.SpecAfter != nil {
			if json.Valid([]byte(*r.SpecAfter)) {
				v.SpecAfter = json.RawMessage(*r.SpecAfter)
			} else {
				// The audit's spec is a record of what was written; damaged bytes
				// are reported AS the damage rather than dropped, so a person
				// reading the audit knows the field is not empty — it is
				// unreadable (P3's third state).
				b, err := json.Marshal(map[string]string{"unreadable": *r.SpecAfter})
				if err != nil {
					return nil, err
				}
				v.SpecAfter = b
			}
		}
		out = append(out, v)
	}
	return out, nil
}

func (s *environmentService) Provisions(ctx context.Context, p protocol.EnvironmentProvisionsParams) ([]protocol.ProvisionView, error) {
	if err := s.haveStore(); err != nil {
		return nil, err
	}
	rows, err := s.store.ListProvisions(p.State, p.Limit)
	if err != nil {
		return nil, err
	}
	out := make([]protocol.ProvisionView, 0, len(rows))
	for _, r := range rows {
		out = append(out, protocol.ProvisionView{
			ID: r.ID, EnvironmentID: r.EnvironmentID, ComponentRoot: r.ComponentRoot, LoopSlug: r.LoopSlug,
			Backend: r.Backend, Handle: r.Handle, Owner: r.Owner, State: r.State,
			CreatedAt: r.CreatedAt, ReadyAt: r.ReadyAt, RemovedAt: r.RemovedAt, LastError: r.LastError,
		})
	}
	return out, nil
}

// --- the writes --------------------------------------------------------------------

func (s *environmentService) Create(ctx context.Context, actor protocol.Actor, p protocol.EnvironmentCreateParams) (protocol.EnvironmentView, error) {
	if err := s.haveStore(); err != nil {
		return protocol.EnvironmentView{}, err
	}
	if execenv.BackendStrength(p.Backend) < 0 {
		e := protocol.ErrEnvironmentRefused.WithWhat("%q is not a backend this daemon knows. The closed set is %s (EV3) — a backend nobody compiled is a backend nothing runs in",
			p.Backend, strings.Join(backendNames(), ", "))
		e.Data.Kind = string(execenv.RefusalSpecInvalid)
		return protocol.EnvironmentView{}, e
	}
	canonical, refusal := s.validateSpecForWriting(p.Spec, p.Backend)
	if refusal != nil {
		return protocol.EnvironmentView{}, refusal
	}
	row, ref, err := s.store.CreateEnvironment(store.Actor{Kind: actor.Kind, ID: actor.ID}, store.NewEnvironment{
		Slug: p.Slug, Name: p.Name, Description: p.Description, Backend: p.Backend, Spec: canonical,
	})
	if err != nil {
		return protocol.EnvironmentView{}, err
	}
	if ref != nil {
		return protocol.EnvironmentView{}, environmentRefused(ref)
	}
	if s.log != nil {
		s.log.Infof("environment: %s created %s (%s)", actor.ID, row.Slug, row.Backend)
	}
	return s.environmentView(ctx, row)
}

func (s *environmentService) Update(ctx context.Context, actor protocol.Actor, p protocol.EnvironmentUpdateParams) (protocol.EnvironmentView, error) {
	if err := s.haveStore(); err != nil {
		return protocol.EnvironmentView{}, err
	}
	row, err := s.store.GetEnvironment(p.ID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return protocol.EnvironmentView{}, s.unknownEnvironment(p.ID)
		}
		return protocol.EnvironmentView{}, err
	}
	if row.Archived() {
		e := protocol.ErrEnvironmentRefused.WithWhat("%s is archived — restore it before changing its spec, or create a new environment (EV16: an archived row is history, not a draft)", row.Slug)
		e.Data.Kind = "environment-archived"
		return protocol.EnvironmentView{}, e
	}
	canonical, refusal := s.validateSpecForWriting(p.Spec, row.Backend)
	if refusal != nil {
		return protocol.EnvironmentView{}, refusal
	}
	updated, ref, err := s.store.UpdateEnvironment(store.Actor{Kind: actor.Kind, ID: actor.ID}, store.UpdateEnvironment{
		ID: row.ID, Spec: canonical, ExpectedRevision: p.ExpectedRevision,
	})
	if err != nil {
		return protocol.EnvironmentView{}, err
	}
	if ref != nil {
		// `stale-revision` is the CAS speaking (EV16): the row moved under the
		// caller, so the write the caller thought it was making is not the one it
		// would be making now.
		return protocol.EnvironmentView{}, environmentRefused(ref)
	}
	return s.environmentView(ctx, updated)
}

func (s *environmentService) Archive(ctx context.Context, actor protocol.Actor, p protocol.EnvironmentIDParams) (protocol.EnvironmentView, error) {
	return s.setArchived(ctx, actor, p, true)
}

func (s *environmentService) Restore(ctx context.Context, actor protocol.Actor, p protocol.EnvironmentIDParams) (protocol.EnvironmentView, error) {
	return s.setArchived(ctx, actor, p, false)
}

func (s *environmentService) setArchived(ctx context.Context, actor protocol.Actor, p protocol.EnvironmentIDParams, archive bool) (protocol.EnvironmentView, error) {
	if err := s.haveStore(); err != nil {
		return protocol.EnvironmentView{}, err
	}
	id := strings.TrimSpace(p.ID)
	if id == "" {
		id = strings.TrimSpace(p.Slug)
	}
	a := store.Actor{Kind: actor.Kind, ID: actor.ID}
	var (
		row store.Environment
		ref *store.EnvironmentRefusal
		err error
	)
	if archive {
		row, ref, err = s.store.ArchiveEnvironment(a, id)
	} else {
		row, ref, err = s.store.RestoreEnvironment(a, id)
	}
	if err != nil {
		return protocol.EnvironmentView{}, err
	}
	if ref != nil {
		return protocol.EnvironmentView{}, environmentRefused(ref)
	}
	return s.environmentView(ctx, row)
}

// Bind answers the person's question in the person's own words before it changes
// anything: the statement comes from `execenv.DescribeBind` (one formatter for
// the dry run, the applied write, the CLI and the panel), and a dry run writes
// nothing at all — not a row, not an audit entry (P3).
func (s *environmentService) Bind(ctx context.Context, actor protocol.Actor, p protocol.EnvironmentBindParams) (protocol.BindResult, error) {
	if err := s.haveStore(); err != nil {
		return protocol.BindResult{}, err
	}
	row, err := s.store.GetEnvironment(p.EnvironmentID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return protocol.BindResult{}, s.unknownEnvironment(p.EnvironmentID)
		}
		return protocol.BindResult{}, err
	}
	refs, err := s.store.EnvironmentSecretRefs(row.ID)
	if err != nil {
		return protocol.BindResult{}, err
	}
	previous, err := s.previousBackend(p.Scope, p.ComponentID)
	if err != nil {
		return protocol.BindResult{}, err
	}
	statement := execenv.DescribeBind(execenv.BindStatement{
		ScopeKind: p.Scope, ComponentID: p.ComponentID,
		EnvironmentSlug: row.Slug, EnvironmentID: row.ID,
		Backend: row.Backend, PreviousBackend: previous,
		SecretRefNames: refs, DryRun: p.DryRun,
	})
	changed, err := s.bindingWouldChange(p.Scope, p.ComponentID, row.ID)
	if err != nil {
		return protocol.BindResult{}, err
	}
	binding, ref, err := s.store.BindEnvironment(store.Actor{Kind: actor.Kind, ID: actor.ID}, store.NewBinding{
		ScopeKind: p.Scope, ComponentID: p.ComponentID, EnvironmentID: row.ID, DryRun: p.DryRun,
	})
	if err != nil {
		return protocol.BindResult{}, err
	}
	if ref != nil {
		// The store's own guards run even on a dry run (T2's design: a dry run
		// validates everything and writes nothing), so `secret-ref-unknown`
		// answers here — the bind checks that a reference EXISTS and is live, and
		// never reads what it points at (EV7).
		return protocol.BindResult{}, environmentRefused(ref)
	}
	view := protocol.BindingView{ScopeKind: binding.ScopeKind, EnvironmentID: binding.EnvironmentID, BoundAt: binding.BoundAt}
	if binding.ComponentID != nil {
		view.ComponentID = *binding.ComponentID
	}
	return protocol.BindResult{
		Statement: statement, DryRun: p.DryRun,
		Changed: changed && !p.DryRun, Binding: &view,
	}, nil
}

// previousBackend names the backend that is EFFECTIVE for this scope today, so
// the statement compares against what a step would actually get (including a
// workspace default covering a component) — and "" only when nothing anywhere
// covers it.
func (s *environmentService) previousBackend(scope, componentID string) (string, error) {
	target := ""
	if scope == "component" {
		target = strings.TrimSpace(componentID)
	}
	b, _, err := s.store.BindingFor(target)
	if err != nil {
		return "", err
	}
	if b.EnvironmentID == "" {
		return "", nil
	}
	env, err := s.store.GetEnvironment(b.EnvironmentID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			// A binding to a row that is gone is not "nothing was bound": it is a
			// broken record, and the statement says so rather than comparing
			// against a fiction.
			return "", environmentRefusalFrom(execenv.NewRefusal(execenv.RefusalEnvironmentUnavailable,
				"the binding at this scope points at "+b.EnvironmentID+", which is not an environment",
				"the row was deleted under the binding — EV2 has no fallback, so this scope cannot resolve a step either",
				"bind this scope to an environment that exists"))
		}
		return "", err
	}
	return env.Backend, nil
}

// bindingWouldChange answers whether the write would change a row, so the
// result's `changed` is a fact about the store rather than a claim about having
// run a statement. The store makes the same comparison and skips its audit row
// on a no-op (P3); this is the door's mirror of that fact, not a second gate.
func (s *environmentService) bindingWouldChange(scope, componentID, environmentID string) (bool, error) {
	rows, err := s.store.ListBindings()
	if err != nil {
		return false, err
	}
	for _, b := range rows {
		if b.ScopeKind != scope {
			continue
		}
		if scope == "workspace" {
			return b.EnvironmentID != environmentID, nil
		}
		if b.ComponentID != nil && *b.ComponentID == strings.TrimSpace(componentID) {
			return b.EnvironmentID != environmentID, nil
		}
	}
	return true, nil
}

func (s *environmentService) Unbind(ctx context.Context, actor protocol.Actor, p protocol.EnvironmentUnbindParams) (protocol.UnbindResult, error) {
	if err := s.haveStore(); err != nil {
		return protocol.UnbindResult{}, err
	}
	componentID := strings.TrimSpace(p.ComponentID)
	if componentID == "workspace" {
		// Said out loud because a person WILL try it: the workspace binding is
		// the floor the seed laid, and removing it leaves every unbound command in
		// the workspace refused (EV2 has no code fallback) — which is not what
		// "unbind" sounds like it means.
		e := protocol.ErrEnvironmentRefused.WithWhat("the workspace binding cannot be unbound (EV18): with no binding the workspace has no environment at all, and EV2 refuses every command in it. Bind the workspace to another environment through environment.bind — that is the replacement, not the absence of one")
		e.Data.Kind = "workspace-binding-immovable"
		return protocol.UnbindResult{}, e
	}
	unbound, ref, err := s.store.UnbindEnvironment(store.Actor{Kind: actor.Kind, ID: actor.ID}, componentID)
	if err != nil {
		return protocol.UnbindResult{}, err
	}
	if ref != nil {
		return protocol.UnbindResult{}, environmentRefused(ref)
	}
	if s.log != nil {
		s.log.Infof("environment: %s unbound component %s", actor.ID, componentID)
	}
	return protocol.UnbindResult{Unbound: unbound}, nil
}

// Teardown marks the provision `tearing_down` and asks the REGISTERED backend to
// remove its container (EV11, EV12). Two things it never does: run a backend
// nobody registered (the refusal says so with the same force EV11 uses at spawn
// time — "Docker is not installed, so we removed it here instead" is a sentence
// about a container that is still running), or pretend a host provision has
// something to remove.
func (s *environmentService) Teardown(ctx context.Context, actor protocol.Actor, p protocol.ProvisionIDParams) (protocol.ProvisionView, error) {
	if err := s.haveStore(); err != nil {
		return protocol.ProvisionView{}, err
	}
	id := strings.TrimSpace(p.ProvisionID)
	row, err := s.store.GetProvision(id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			e := protocol.ErrEnvironmentRefused.WithWhat("%q is not a provision this store has (EV12: teardown acts on a ledger row, and the ledger is the only thing that knows a container is out there)", id)
			e.Data.Kind = "provision-unknown"
			return protocol.ProvisionView{}, e
		}
		return protocol.ProvisionView{}, err
	}
	if row.State == store.ProvisionStateRemoved {
		// Already gone. The answer is the row — a second removal would be a
		// claim about an act that did not happen, and the audit would record it.
		return provisionView(row), nil
	}
	if hostBackends[row.Backend] {
		e := protocol.ErrEnvironmentRefused.WithWhat("%s runs in the %s backend, which has no container to remove — a host provision is a working directory, and teardown does not delete a person's work (EV12)",
			row.ID, row.Backend)
		e.Data.Kind = "provision-nothing-to-remove"
		return protocol.ProvisionView{}, e
	}
	driver := s.teardownDriver(row.Backend)
	if driver == nil {
		e := protocol.ErrEnvironmentRefused.WithWhat("%s names the %s backend and this daemon has no driver registered for it (EV11): that container is a resource that exists somewhere, and a daemon which cannot speak to it cannot claim to have removed it. Register the driver, or remove it yourself and let the start-up reconcile settle the row",
			row.ID, row.Backend)
		e.Data.Kind = "backend-unregistered"
		return protocol.ProvisionView{}, e
	}
	// `tearing_down` FIRST, before the driver is asked: a daemon that dies
	// mid-teardown then leaves a row a reconcile can find and finish, rather
	// than one still claiming the container is live (EV12's whole argument for
	// a ledger over a poll).
	updating, ref, err := s.store.MarkProvisionTearingDown(row.ID)
	if err != nil {
		return protocol.ProvisionView{}, err
	}
	if ref != nil {
		return protocol.ProvisionView{}, environmentRefused(ref)
	}
	if err := driver.Teardown(ctx, updating); err != nil {
		// The row goes to `failed` with the driver's own sentence: a container
		// nobody removed is precisely the thing the reconcile needs to find, and
		// a row that said `removed` would hide it there forever.
		marked, ref2, err2 := s.store.MarkProvisionFailed(row.ID, err.Error())
		switch {
		case err2 != nil:
			if s.log != nil {
				s.log.Infof("environment: teardown of %s failed (%v) AND the ledger could not say so: %v", row.ID, err, err2)
			}
		case ref2 != nil:
			if s.log != nil {
				s.log.Infof("environment: the ledger refused to mark %s failed: %s", row.ID, ref2.Sentence)
			}
		}
		// The sentence says what the ledger NOW says, not what this door hoped
		// it would say: a promise about a row this write could not write would
		// be the plane's own version of the lie EV12 exists to prevent.
		ledger := "the row stays in the ledger as failed, because an unremoved container is exactly what EV12's reconcile is for"
		if ref2 != nil || err2 != nil {
			ledger = "and the ledger could NOT record the failure — read the row's own state before trusting this answer, and re-run environment.teardown once the driver is reachable"
		} else if marked.State != store.ProvisionStateFailed {
			ledger = "and the ledger now reads " + marked.State
		}
		e := protocol.ErrEnvironmentRefused.WithWhat("the %s backend refused to remove %s (%v) — %s",
			row.Backend, row.ID, err, ledger)
		e.Data.Kind = "teardown-failed"
		return protocol.ProvisionView{}, e
	}
	removed, ref, err := s.store.MarkProvisionRemoved(row.ID)
	if err != nil {
		return protocol.ProvisionView{}, err
	}
	if ref != nil {
		return protocol.ProvisionView{}, environmentRefused(ref)
	}
	if s.log != nil {
		s.log.Infof("environment: %s tore down provision %s (%s)", actor.ID, row.ID, row.Backend)
	}
	return provisionView(removed), nil
}

// teardownDriver answers whether a registered backend can also REMOVE what it
// creates. The capability is asserted, not assumed: a backend that can run
// commands but cannot be asked to delete its container is refused out loud,
// because marking the row `removed` without the removal is how a daemon
// launders a leaked container into "nothing is running".
func (s *environmentService) teardownDriver(backend string) BackendTeardown {
	if s.resolver == nil {
		return nil
	}
	be, ok := s.resolver.Backends[backend]
	if !ok {
		return nil
	}
	bt, ok := be.(BackendTeardown)
	if !ok {
		return nil
	}
	return bt
}

// provisionView is one ledger row on the wire.
func provisionView(r store.Provision) protocol.ProvisionView {
	return protocol.ProvisionView{
		ID: r.ID, EnvironmentID: r.EnvironmentID, ComponentRoot: r.ComponentRoot, LoopSlug: r.LoopSlug,
		Backend: r.Backend, Handle: r.Handle, Owner: r.Owner, State: r.State,
		CreatedAt: r.CreatedAt, ReadyAt: r.ReadyAt, RemovedAt: r.RemovedAt, LastError: r.LastError,
	}
}
