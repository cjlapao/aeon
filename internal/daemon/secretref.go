package daemon

// secretref.go — S59 T6 (EV7, EV11, EV16, EV17, EV18): the daemon's one
// secret-reference service, behind the seven socket doors.
//
// What this plane stores is a NAME and a POINTER — `sourceKind` (which of the
// three closed sources), `source` (where in that source: a variable name, a
// file path, a credential id) and `injectAs` (what the child will call it). A
// value is not a member of any type in this file, and the rule that keeps it
// that way is not "we redact on the way out" but "there is nowhere to put one":
// §4c's discipline, carried from one route onto a whole plane, with the
// reflection guard in `cmd/aeon` proving the params and the views.
//
// `Check` is the only method that touches a real source, and it exists so a
// person can ask "would this bind work?" without a way to read the answer. It
// resolves the reference and DISCARDS the value in the statement that produced
// it — the resolution is the diagnostic, never the credential. `ResolveRef`
// still gates it (owner and mode for a key file, the length floor, the
// daemon-credential list), so what `Check` reports is exactly what a bind would
// have met.

import (
	"context"
	"errors"
	"strings"

	"github.com/cjlapao/aeon/internal/execenv"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

// secretRefService implements protocol.SecretRefService.
type secretRefService struct {
	store    *store.Store
	resolver *StepExecResolver
	log      Logger
}

func newSecretRefService(st *store.Store, resolver *StepExecResolver, log Logger) *secretRefService {
	return &secretRefService{store: st, resolver: resolver, log: log}
}

func (s *secretRefService) haveStore() error {
	if s == nil || s.store == nil {
		return errNoStoreForEnvironment
	}
	return nil
}

// resolveEnv is the world `execenv` may see, assembled from the resolver so the
// ONE place the daemon's environment, key files and credential list live stays
// one place. A nil resolver is a daemon wired without them: every field is nil,
// and `ResolveRef` refuses each kind rather than inventing an answer (an empty
// credential list is "not checked", and P3 forbids reading that as clean).
func (s *secretRefService) resolveEnv() execenv.ResolveEnv {
	if s.resolver == nil {
		return execenv.ResolveEnv{}
	}
	return execenv.ResolveEnv{
		Lookup:      s.resolver.Getenv,
		Files:       s.resolver.Files,
		DaemonUID:   s.resolver.DaemonUID,
		Credentials: s.resolver.Credentials,
		Store:       s.resolver.secretLookupFor(""),
	}
}

// reserved is the daemon's own credential list in the shape the STORE wants
// (EV11's second half): a reference that points at a daemon credential is
// refused at the write, not filtered at the spawn. The list is assembled once,
// from `daemonCredentials()`, so the door, the store and the resolver all judge
// against the same names.
func (s *secretRefService) reserved() []string {
	creds := execenv.DaemonCredentials{}
	if s.resolver != nil {
		creds = s.resolver.Credentials
	} else {
		creds = daemonCredentials()
	}
	out := make([]string, 0, len(creds.Provider)+len(creds.Forge)+len(creds.Extra))
	out = append(out, creds.Provider...)
	out = append(out, creds.Forge...)
	out = append(out, creds.Extra...)
	return out
}

// --- the reads ---------------------------------------------------------------------

func (s *secretRefService) List(ctx context.Context, p protocol.SecretRefListParams) ([]protocol.SecretRefView, error) {
	if err := s.haveStore(); err != nil {
		return nil, err
	}
	rows, err := s.store.ListSecretRefs(store.SecretRefFilter{
		SourceKind: p.SourceKind, IncludeArchived: p.IncludeArchived, Limit: p.Limit,
	})
	if err != nil {
		return nil, err
	}
	out := make([]protocol.SecretRefView, 0, len(rows))
	for _, r := range rows {
		v, err := s.view(r)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

func (s *secretRefService) Show(ctx context.Context, p protocol.SecretRefShowParams) (protocol.SecretRefView, error) {
	if err := s.haveStore(); err != nil {
		return protocol.SecretRefView{}, err
	}
	row, err := s.lookup(p.ID, p.Name)
	if err != nil {
		return protocol.SecretRefView{}, err
	}
	return s.view(row)
}

// lookup resolves a reference by name (the keyed read) or by id (a scan of a
// table this plane keeps small by hand). An id lookup is not worth its own
// prepared statement — and a scan that reuses the listing's own read cannot
// disagree with it about what exists.
func (s *secretRefService) lookup(id, name string) (store.SecretRef, error) {
	if n := strings.TrimSpace(name); n != "" {
		row, err := s.store.GetSecretRef(n)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return store.SecretRef{}, s.unknown(n)
			}
			return store.SecretRef{}, err
		}
		return row, nil
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return store.SecretRef{}, protocol.ErrEnvironmentRefused.WithWhat("a secret reference is named by name or by id, and neither was given")
	}
	rows, err := s.store.ListSecretRefs(store.SecretRefFilter{IncludeArchived: true, Limit: 500})
	if err != nil {
		return store.SecretRef{}, err
	}
	for _, r := range rows {
		if r.ID == id {
			return r, nil
		}
	}
	return store.SecretRef{}, s.unknown(id)
}

func (s *secretRefService) unknown(name string) *protocol.Error {
	rows, err := s.store.ListSecretRefs(store.SecretRefFilter{IncludeArchived: true, Limit: 200})
	if err != nil {
		e := protocol.ErrEnvironmentRefused.WithWhat("%q is not a secret reference, and the list could not be read to help (%v)", name, err)
		e.Data.Kind = "secret-ref-unknown"
		return e
	}
	names := make([]string, 0, len(rows))
	for _, r := range rows {
		names = append(names, r.Name)
	}
	e := protocol.ErrEnvironmentRefused.WithWhat("%q is not a secret reference (name or id). This store has: %s",
		name, strings.Join(names, ", "))
	e.Data.Kind = "secret-ref-unknown"
	return e
}

// view is one row on the wire plus the one derived fact a person needs before
// they archive it: which live environments name it.
func (s *secretRefService) view(r store.SecretRef) (protocol.SecretRefView, error) {
	refs, err := s.store.EnvironmentsReferencingSecretRef(r.Name)
	if err != nil {
		return protocol.SecretRefView{}, err
	}
	return protocol.SecretRefView{
		ID: r.ID, Name: r.Name, Description: r.Description,
		SourceKind: r.SourceKind, Source: r.Source, InjectAs: r.InjectAs,
		Origin: r.Origin, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt, ArchivedAt: r.ArchivedAt,
		ReferencedBy: refs,
	}, nil
}

// Check resolves the reference against the daemon's real sources and answers
// booleans plus the refusal the resolver spoke (EV7's "would this bind work?"
// with no route to the value).
//
// `SourcePresent` is the three-state part (EV5): a source that is ABSENT is not
// a source that is EMPTY, and that difference is the whole diagnosis for "the
// token was not injected". It is answered by asking the source directly — is the
// variable set, does the file exist, does the store know that id — never by
// inspecting a value, so it stays answerable even for a source that would refuse
// to resolve.
func (s *secretRefService) Check(ctx context.Context, p protocol.SecretRefCheckParams) (protocol.SecretRefCheckResult, error) {
	if err := s.haveStore(); err != nil {
		return protocol.SecretRefCheckResult{}, err
	}
	row, err := s.lookup(p.ID, p.Name)
	if err != nil {
		return protocol.SecretRefCheckResult{}, err
	}
	spec := execenv.RefSpec{Name: row.Name, SourceKind: row.SourceKind, Source: row.Source, InjectAs: row.InjectAs}
	result := protocol.SecretRefCheckResult{
		Name: row.Name, SourceKind: row.SourceKind,
		SourcePresent: s.sourcePresent(spec),
	}
	if row.Archived() {
		result.RefusalKind = "secret-ref-archived"
		result.RefusalSentence = "the reference is archived — restore it, or bind an environment that does not name it"
		return result, nil
	}
	value, refusal := execenv.ResolveRef(spec, s.resolveEnv())
	// The value exists for exactly this statement and is then gone. Nothing
	// below it can reach it, no log line takes it, and the result type has no
	// field for it — which is why no future call site can leak it either (§4c's
	// argument, applied here rather than trusted here).
	_ = value
	if refusal != nil {
		result.Resolvable = false
		result.RefusalKind = string(refusal.Kind)
		result.RefusalSentence = refusal.What
		if refusal.Why != "" {
			result.RefusalSentence += " · " + refusal.Why
		}
		return result, nil
	}
	result.Resolvable = true
	result.SourcePresent = true
	return result, nil
}

// sourcePresent asks the source whether it is there, without asking what is in
// it. A source whose door is not wired (no lookup, no file opener, no store
// door) is NOT present — "could not tell" must never read as "yes" in a
// diagnostic a bind is about to rely on.
func (s *secretRefService) sourcePresent(spec execenv.RefSpec) bool {
	env := s.resolveEnv()
	switch spec.SourceKind {
	case "daemon-env":
		return env.Lookup != nil && env.Lookup(spec.Source) != ""
	case "key-file":
		if env.Files == nil {
			return false
		}
		_, err := env.Files.Lstat(spec.Source)
		return err == nil
	case "store":
		if env.Store == nil {
			return false
		}
		_, found := env.Store(spec.Source)
		return found
	default:
		return false
	}
}

// --- the writes --------------------------------------------------------------------

func (s *secretRefService) Create(ctx context.Context, actor protocol.Actor, p protocol.SecretRefCreateParams) (protocol.SecretRefView, error) {
	if err := s.haveStore(); err != nil {
		return protocol.SecretRefView{}, err
	}
	injectAs := strings.TrimSpace(p.InjectAs)
	if injectAs == "" {
		// The common case is "inject it under the same name"; defaulting here is
		// the door being usable, and the store still refuses a name that is not
		// a variable name.
		injectAs = strings.TrimSpace(p.Name)
	}
	spec := execenv.RefSpec{Name: p.Name, SourceKind: p.SourceKind, Source: p.Source, InjectAs: injectAs}
	// EV7's write-time half: a reference that points at one of the daemon's own
	// credentials is refused here, by name, with the list the resolver itself
	// checks against — not filtered later, and never accepted because the
	// daemon "knows" what it holds.
	if refusal := execenv.ValidateRef(spec, s.credentials()); refusal != nil {
		return protocol.SecretRefView{}, environmentRefusalFrom(refusal)
	}
	row, ref, err := s.store.CreateSecretRef(store.Actor{Kind: actor.Kind, ID: actor.ID}, store.NewSecretRef{
		Name: p.Name, Description: p.Description, SourceKind: p.SourceKind, Source: p.Source, InjectAs: injectAs,
	}, s.reserved())
	if err != nil {
		return protocol.SecretRefView{}, err
	}
	if ref != nil {
		return protocol.SecretRefView{}, environmentRefused(ref)
	}
	if s.log != nil {
		s.log.Infof("secretref: %s created %s (%s)", actor.ID, row.Name, row.SourceKind)
	}
	return s.view(row)
}

func (s *secretRefService) Update(ctx context.Context, actor protocol.Actor, p protocol.SecretRefUpdateParams) (protocol.SecretRefView, error) {
	if err := s.haveStore(); err != nil {
		return protocol.SecretRefView{}, err
	}
	// The store's nil pointers are "leave alone" (§4c's rule for a route's key),
	// so the candidate the validator sees is the row with these fields overlaid
	// — the same rule the door renders, applied once, here.
	current, err := s.lookup(p.Name, "")
	if err != nil {
		return protocol.SecretRefView{}, err
	}
	candidate := execenv.RefSpec{
		Name: current.Name, SourceKind: current.SourceKind, Source: current.Source, InjectAs: current.InjectAs,
	}
	if p.SourceKind != nil {
		candidate.SourceKind = *p.SourceKind
	}
	if p.Source != nil {
		candidate.Source = *p.Source
	}
	if p.InjectAs != nil {
		candidate.InjectAs = *p.InjectAs
	}
	if refusal := execenv.ValidateRef(candidate, s.credentials()); refusal != nil {
		return protocol.SecretRefView{}, environmentRefusalFrom(refusal)
	}
	row, ref, err := s.store.UpdateSecretRef(store.Actor{Kind: actor.Kind, ID: actor.ID}, store.UpdateSecretRef{
		Name: p.Name, Description: p.Description, SourceKind: p.SourceKind, Source: p.Source, InjectAs: p.InjectAs,
	})
	if err != nil {
		return protocol.SecretRefView{}, err
	}
	if ref != nil {
		return protocol.SecretRefView{}, environmentRefused(ref)
	}
	return s.view(row)
}

func (s *secretRefService) Archive(ctx context.Context, actor protocol.Actor, p protocol.SecretRefNameParams) (protocol.SecretRefView, error) {
	return s.setArchived(ctx, actor, p, true)
}

func (s *secretRefService) Restore(ctx context.Context, actor protocol.Actor, p protocol.SecretRefNameParams) (protocol.SecretRefView, error) {
	return s.setArchived(ctx, actor, p, false)
}

// setArchived is archive and restore together. An archive of a reference some
// live environment names is the store's to refuse (T2's guard), and it does so
// with the slugs in the sentence — because "you would strand these" is the one
// thing a person needs to hear BEFORE the click, and `view` shows the same list
// on the way in.
func (s *secretRefService) setArchived(ctx context.Context, actor protocol.Actor, p protocol.SecretRefNameParams, archive bool) (protocol.SecretRefView, error) {
	if err := s.haveStore(); err != nil {
		return protocol.SecretRefView{}, err
	}
	a := store.Actor{Kind: actor.Kind, ID: actor.ID}
	var (
		row store.SecretRef
		ref *store.EnvironmentRefusal
		err error
	)
	if archive {
		row, ref, err = s.store.ArchiveSecretRef(a, p.Name)
	} else {
		row, ref, err = s.store.RestoreSecretRef(a, p.Name)
	}
	if err != nil {
		return protocol.SecretRefView{}, err
	}
	if ref != nil {
		return protocol.SecretRefView{}, environmentRefused(ref)
	}
	if s.log != nil {
		s.log.Infof("secretref: %s archived=%v %s", actor.ID, archive, row.Name)
	}
	return s.view(row)
}

// credentials is the same list `reserved` renders, in the struct
// `ValidateRef`/`ResolveRef` take. One assembly, two shapes, one source.
func (s *secretRefService) credentials() execenv.DaemonCredentials {
	if s.resolver != nil {
		return s.resolver.Credentials
	}
	return daemonCredentials()
}
