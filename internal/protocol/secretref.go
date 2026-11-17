package protocol

// secretref.go — S59 T6 (EV7, EV16, EV17, EV18): the secret-reference doors.
//
// Seven methods over the daemon's one secret-reference service, registered
// together or not at all (V13). Three reads (list, show, check) and four
// human-door writes (create, update, archive, restore).
//
// This file is the reason §4c's rule holds for a whole plane rather than for
// one route: **there is no field anywhere here that could hold a secret
// value.** A reference is a NAME plus a POINTER — `sourceKind` says which of
// the three closed sources (`daemon-env`, `key-file`, `store`) holds it,
// `source` says where (an env-var name, a file path — never a value),
// `injectAs` says what the child process will call it. `secretref.check`
// resolves the reference and answers with BOOLEANS: whether it resolved,
// whether the source was there, and the validator's kind and sentence when it
// did not — a refusal that names the reference and never the value, because it
// never had one. The reflection guard `TestNoEnvironmentParamCarriesAValue`
// walks every params type in this file and this one's result types, so the
// absence is a test and not a comment.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// --- the method names -------------------------------------------------------------

const (
	MethodSecretRefList    MethodName = "secretref.list"
	MethodSecretRefShow    MethodName = "secretref.show"
	MethodSecretRefCheck   MethodName = "secretref.check"
	MethodSecretRefCreate  MethodName = "secretref.create"
	MethodSecretRefUpdate  MethodName = "secretref.update"
	MethodSecretRefArchive MethodName = "secretref.archive"
	MethodSecretRefRestore MethodName = "secretref.restore"
)

// --- the params -------------------------------------------------------------------

// SecretRefListParams is secretref.list's request.
type SecretRefListParams struct {
	SourceKind      string `json:"sourceKind,omitempty"`
	IncludeArchived bool   `json:"includeArchived,omitempty"`
	Limit           int    `json:"limit,omitempty"`
}

// SecretRefShowParams is secretref.show's request: one reference by id or name.
type SecretRefShowParams struct {
	ID   string `json:"id,omitempty"`
	Name string `json:"name,omitempty"`
}

// SecretRefCheckParams is secretref.check's request. It resolves the reference
// against the daemon's real sources and answers booleans — the way to ask
// "would this bind fail?" without a way to read the answer's value (EV7).
type SecretRefCheckParams struct {
	ID   string `json:"id,omitempty"`
	Name string `json:"name,omitempty"`
}

// SecretRefCreateParams is secretref.create's request. `source` is where the
// value lives, and the three kinds read it differently: for `daemon-env` it is
// the NAME of a variable the daemon holds (and a daemon credential is
// refused here by name — EV11), for `key-file` it is a path, for `store` it is
// the key in the vault (staged: the door refuses it with a sentence, D3).
// There is deliberately no field for the value itself: a value that cannot be
// sent cannot be stored, returned, or logged (EV7, §4c).
type SecretRefCreateParams struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	SourceKind  string `json:"sourceKind"`
	Source      string `json:"source"`
	InjectAs    string `json:"injectAs,omitempty"`
}

// SecretRefUpdateParams is secretref.update's request. A nil pointer is "leave
// that field alone" — the same three-state rule §4c uses for a route's key, so
// editing a description never has to re-present a source it never saw.
type SecretRefUpdateParams struct {
	Name        string  `json:"name"`
	Description *string `json:"description,omitempty"`
	SourceKind  *string `json:"sourceKind,omitempty"`
	Source      *string `json:"source,omitempty"`
	InjectAs    *string `json:"injectAs,omitempty"`
}

// SecretRefNameParams is secretref.archive's and secretref.restore's request:
// one reference by name. Names are the identity a person reads, so they are
// what the door takes.
type SecretRefNameParams struct {
	Name string `json:"name"`
}

// --- the views --------------------------------------------------------------------

// SecretRefView is one reference as a person reads it. Compare it with the
// request types and the property is visible: no value in either direction —
// `hasValue` would be a third state nobody needs, because the store cannot
// have one either (EV7). `referencedBy` names the environments that inject it,
// so archiving a reference says what it would strand before it is archived.
type SecretRefView struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Description  string   `json:"description,omitempty"`
	SourceKind   string   `json:"sourceKind"`
	Source       string   `json:"source"`
	InjectAs     string   `json:"injectAs"`
	Origin       string   `json:"origin"`
	CreatedAt    string   `json:"createdAt"`
	UpdatedAt    string   `json:"updatedAt"`
	ArchivedAt   *string  `json:"archivedAt,omitempty"`
	ReferencedBy []string `json:"referencedBy,omitempty"`
}

// SecretRefListView is one reference as an AGENT reads it (S59 T7, EV7):
// `source` is gone — not redacted, absent — along with anything that names
// where a credential lives. This is §4c's rule applied to the agent's half of
// the plane: two types rather than one redaction, so leaking the source is not
// something a forgotten call site can do. What remains is what an agent needs
// to ask for a tightening intelligently: that a reference exists, what it is
// called, the variable it would inject as, and the KIND of source it points at
// — never the source, never a value, and never a length (a fact about the
// secret a caller could binary-search).
//
// AgentView is the one projection between the two types, so a field added to
// SecretRefView later does not silently reach the agent's view: the copy is
// field by field, and a new field is a decision someone has to make here.
type SecretRefListView struct {
	ID           string  `json:"id"`
	Name         string  `json:"name"`
	Description  string  `json:"description,omitempty"`
	SourceKind   string  `json:"sourceKind"`
	InjectAs     string  `json:"injectAs"`
	Origin       string  `json:"origin"`
	CreatedAt    string  `json:"createdAt"`
	UpdatedAt    string  `json:"updatedAt"`
	ArchivedAt   *string `json:"archivedAt,omitempty"`
	ReferencedBy []string `json:"referencedBy,omitempty"`
}

// AgentView projects the person's view down to the agent's. Field by field,
// on purpose: absence must be a decision, not an accident of struct order.
func (v SecretRefView) AgentView() SecretRefListView {
	return SecretRefListView{
		ID:           v.ID,
		Name:         v.Name,
		Description:  v.Description,
		SourceKind:   v.SourceKind,
		InjectAs:     v.InjectAs,
		Origin:       v.Origin,
		CreatedAt:    v.CreatedAt,
		UpdatedAt:    v.UpdatedAt,
		ArchivedAt:   v.ArchivedAt,
		ReferencedBy: v.ReferencedBy,
	}
}

// SecretRefCheckResult is secretref.check's answer: booleans and the refusal
// the resolver spoke. `sourcePresent` is the three-state part — a source that
// is absent is not a source that is empty, and the difference is the whole
// diagnosis for "the token was not injected" (EV5). No field here can carry
// the value: `valueLength` and its like are deliberately absent, because a
// length is a fact about the secret that a caller could binary-search.
type SecretRefCheckResult struct {
	Name            string `json:"name"`
	SourceKind      string `json:"sourceKind"`
	Resolvable      bool   `json:"resolvable"`
	SourcePresent   bool   `json:"sourcePresent"`
	RefusalKind     string `json:"refusalKind,omitempty"`
	RefusalSentence string `json:"refusalSentence,omitempty"`
}

// --- the seam ---------------------------------------------------------------------

// SecretRefService is the daemon's ONE secret-reference service
// (`internal/daemon/secretref.go`). The reads take no actor; the writes take
// the connection's person. `Check` is the only method that touches a real
// source, and it answers booleans — the resolution exists so a bind can be
// predicted, never so a value can be read (EV7).
type SecretRefService interface {
	List(ctx context.Context, p SecretRefListParams) ([]SecretRefView, error)
	Show(ctx context.Context, p SecretRefShowParams) (SecretRefView, error)
	Check(ctx context.Context, p SecretRefCheckParams) (SecretRefCheckResult, error)

	Create(ctx context.Context, actor Actor, p SecretRefCreateParams) (SecretRefView, error)
	Update(ctx context.Context, actor Actor, p SecretRefUpdateParams) (SecretRefView, error)
	Archive(ctx context.Context, actor Actor, p SecretRefNameParams) (SecretRefView, error)
	Restore(ctx context.Context, actor Actor, p SecretRefNameParams) (SecretRefView, error)
}

// --- registration -----------------------------------------------------------------

// RegisterSecretRef registers the seven secret-reference doors over a non-nil
// seam (V13: nil ⇒ absent, never registered-and-refusing). Returns whether they
// are in the table so the wiring is asserted, not trusted.
func (t *Table) RegisterSecretRef() bool {
	if t.SecretRefs == nil {
		return false
	}
	for _, m := range []*Method{
		{Name: MethodSecretRefList, RequiresInitialize: true, Handle: t.handleSecretRefList},
		{Name: MethodSecretRefShow, RequiresInitialize: true, Handle: t.handleSecretRefShow},
		{Name: MethodSecretRefCheck, RequiresInitialize: true, Handle: t.handleSecretRefCheck},
		{Name: MethodSecretRefCreate, RequiresInitialize: true, Handle: t.handleSecretRefCreate},
		{Name: MethodSecretRefUpdate, RequiresInitialize: true, Handle: t.handleSecretRefUpdate},
		{Name: MethodSecretRefArchive, RequiresInitialize: true, Handle: t.handleSecretRefArchive},
		{Name: MethodSecretRefRestore, RequiresInitialize: true, Handle: t.handleSecretRefRestore},
	} {
		t.register(m)
	}
	return true
}

// --- the door-shared rules --------------------------------------------------------

// secretRefIdentityKeys are the keys the reference params may never name: the
// actor and the timestamps are the daemon's and the store's, and `origin` is
// the seed's (EV17).
var secretRefIdentityKeys = []string{"actor", "actorKind", "origin", "revision", "createdAt", "updatedAt", "createdBy", "updatedBy"}

// secretRefValueKeys are the keys a caller might reach for to send a value.
// They are refused by NAME before the decode, so the refusal is "this key is
// not accepted here, and no key is" rather than a silent drop by a lenient
// decoder — a person who tried to send a value is told the plane has no door
// for it (EV7). The list is a denial of the obvious spellings; the reflection
// guard is the general one, and both are needed: this refuses the attempt, that
// makes the capacity impossible.
var secretRefValueKeys = []string{"value", "secret", "secretValue", "token", "apiKey", "key", "password", "credential", "contents", "data", "raw", "bytes", "envValue"}

func rejectSecretRefKeys(method string, raw json.RawMessage, keys []string, why string) error {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var bag map[string]json.RawMessage
	if err := json.Unmarshal(raw, &bag); err != nil {
		return nil // decodeParams reports the malformed envelope in its own words
	}
	for _, key := range keys {
		if _, has := bag[key]; has {
			return ErrBadParams{Err: fmt.Errorf("%s params carry %q — %s (S59 EV7)", method, key, why)}
		}
	}
	return nil
}

func rejectSecretRefIdentityKeys(method string, raw json.RawMessage) error {
	return rejectSecretRefKeys(method, raw, secretRefIdentityKeys,
		"the actor and timestamps are stamped by the daemon and the store, never named by the caller (EV17)")
}

func rejectSecretRefValueKeys(method string, raw json.RawMessage) error {
	return rejectSecretRefKeys(method, raw, secretRefValueKeys,
		"a secret reference is a name and a pointer; this plane has no field for a value, in this or any other key — the value lives in the source the reference names")
}

func (t *Table) secretRefSeam() error {
	if t.SecretRefs == nil {
		return ErrStoreUnavailable.WithWhat("the secret-reference doors need the daemon's secret-reference service, and this build did not link it — the surface is absent for the same reason (V13)")
	}
	return nil
}

// --- the read handlers ------------------------------------------------------------

func (t *Table) handleSecretRefList(ctx context.Context, sess *Session, raw json.RawMessage) (any, error) {
	if err := rejectSecretRefIdentityKeys(string(MethodSecretRefList), raw); err != nil {
		return nil, err
	}
	if err := rejectSecretRefValueKeys(string(MethodSecretRefList), raw); err != nil {
		return nil, err
	}
	if err := t.secretRefSeam(); err != nil {
		return nil, err
	}
	var p SecretRefListParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if p.Limit < 0 || p.Limit > maxEnvironmentLimit {
		return nil, ErrBadParams{Err: fmt.Errorf("secretref.list limit is %d; the bound is %d — narrow the filter rather than the patience", p.Limit, maxEnvironmentLimit)}
	}
	return t.SecretRefs.List(ctx, p)
}

func (t *Table) handleSecretRefShow(ctx context.Context, sess *Session, raw json.RawMessage) (any, error) {
	if err := rejectSecretRefIdentityKeys(string(MethodSecretRefShow), raw); err != nil {
		return nil, err
	}
	if err := rejectSecretRefValueKeys(string(MethodSecretRefShow), raw); err != nil {
		return nil, err
	}
	if err := t.secretRefSeam(); err != nil {
		return nil, err
	}
	var p SecretRefShowParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.ID) == "" && strings.TrimSpace(p.Name) == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("secretref.show needs an id or a name — listing every reference is a different method, and a shorter word for it")}
	}
	return t.SecretRefs.Show(ctx, p)
}

func (t *Table) handleSecretRefCheck(ctx context.Context, sess *Session, raw json.RawMessage) (any, error) {
	if err := t.secretRefSeam(); err != nil {
		return nil, err
	}
	if err := rejectSecretRefIdentityKeys(string(MethodSecretRefCheck), raw); err != nil {
		return nil, err
	}
	if err := rejectSecretRefValueKeys(string(MethodSecretRefCheck), raw); err != nil {
		return nil, err
	}
	var p SecretRefCheckParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.ID) == "" && strings.TrimSpace(p.Name) == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("secretref.check needs an id or a name — checking nothing resolves nothing")}
	}
	return t.SecretRefs.Check(ctx, p)
}

// --- the write handlers -----------------------------------------------------------

func (t *Table) handleSecretRefCreate(ctx context.Context, sess *Session, raw json.RawMessage) (any, error) {
	if err := t.secretRefSeam(); err != nil {
		return nil, err
	}
	if err := rejectSecretRefIdentityKeys(string(MethodSecretRefCreate), raw); err != nil {
		return nil, err
	}
	if err := rejectSecretRefValueKeys(string(MethodSecretRefCreate), raw); err != nil {
		return nil, err
	}
	var p SecretRefCreateParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.Name) == "" || strings.TrimSpace(p.SourceKind) == "" || strings.TrimSpace(p.Source) == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("name, sourceKind and source are required — a reference that points nowhere is a value by another route")}
	}
	actor, err := t.environmentActor(ctx, sess)
	if err != nil {
		return nil, err
	}
	return t.SecretRefs.Create(ctx, actor, p)
}

func (t *Table) handleSecretRefUpdate(ctx context.Context, sess *Session, raw json.RawMessage) (any, error) {
	if err := t.secretRefSeam(); err != nil {
		return nil, err
	}
	if err := rejectSecretRefIdentityKeys(string(MethodSecretRefUpdate), raw); err != nil {
		return nil, err
	}
	if err := rejectSecretRefValueKeys(string(MethodSecretRefUpdate), raw); err != nil {
		return nil, err
	}
	var p SecretRefUpdateParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.Name) == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("secretref.update needs the name of the reference being edited — the name is the identity a person reads")}
	}
	if p.Description == nil && p.SourceKind == nil && p.Source == nil && p.InjectAs == nil {
		return nil, ErrBadParams{Err: fmt.Errorf("secretref.update changes nothing — every field is absent; say which one you mean, or archive the reference instead")}
	}
	actor, err := t.environmentActor(ctx, sess)
	if err != nil {
		return nil, err
	}
	return t.SecretRefs.Update(ctx, actor, p)
}

func (t *Table) handleSecretRefArchive(ctx context.Context, sess *Session, raw json.RawMessage) (any, error) {
	return t.secretRefNameWrite(ctx, sess, raw, string(MethodSecretRefArchive), t.SecretRefs.Archive)
}

func (t *Table) handleSecretRefRestore(ctx context.Context, sess *Session, raw json.RawMessage) (any, error) {
	return t.secretRefNameWrite(ctx, sess, raw, string(MethodSecretRefRestore), t.SecretRefs.Restore)
}

// secretRefNameWrite is the shared door behind archive and restore: the two
// differ in one word, so the door is one function and the rule (a name is
// required, the actor is the connection's) exists in exactly one place.
func (t *Table) secretRefNameWrite(ctx context.Context, sess *Session, raw json.RawMessage, method string, call func(context.Context, Actor, SecretRefNameParams) (SecretRefView, error)) (any, error) {
	if err := t.secretRefSeam(); err != nil {
		return nil, err
	}
	if err := rejectSecretRefIdentityKeys(method, raw); err != nil {
		return nil, err
	}
	if err := rejectSecretRefValueKeys(method, raw); err != nil {
		return nil, err
	}
	var p SecretRefNameParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.Name) == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("%s needs the name of one reference — an archive is an act on a record, not on a class of them", method)}
	}
	actor, err := t.environmentActor(ctx, sess)
	if err != nil {
		return nil, err
	}
	return call(ctx, actor, p)
}
