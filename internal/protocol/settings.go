package protocol

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/cjlapao/aeon/internal/config"
)

// settings.go — S44 T5: the five settings.* methods (list/get/set/unset/
// history). They are the ONLY door the Settings UI (T8) uses and the CLI
// (T7) is a client of (P34: the daemon surface is primary). Registered
// CONDITIONALLY by RegisterSettings, beside the telemetry block (V13: a nil
// Settings leaves the five ABSENT, not registered-and-refusing — the S22
// search.query precedent). None is Debug and none is projected onto the
// narrow surface (CS9's allow-list never names them).
//
// The seam is the shared *config.Store (one *config.Store is handed to both
// doors, CS11). The interface returns config's own View/Event — the wire
// types below (SettingView/SettingEventView) are THIS package's field-for-
// field twins, so the handler maps config.View → SettingView on the way out
// and a second copy of the fields cannot drift. config imports nothing this
// package needs back (only internal/errs), so importing it here is acyclic
// and safe; the only boundary guard on this package names store/tools/daemon,
// not config. The handler maps config's own domain errors (ErrSettingUnknown,
// ErrSettingInvalidValue) to the protocol's two codes VERBATIM — the three
// parts travel unchanged, so the handler adds no interpretation of its own.

// --- the seam ---------------------------------------------------------------

// SettingsService is the settings accessor the five methods read and write
// through. It is the read/write half of config.Store (the *config.Store a
// daemon is handed satisfies it directly, so the daemon adapter is a nil
// guard, not a translation). It returns config's own View/Event; the handler
// maps them to THIS package's wire twins below. A nil seam (a storeless
// daemon, V13) leaves the five methods absent: RegisterSettings is a no-op,
// so a no-settings build never answers a settings door at all, rather than
// answering a refusal.
type SettingsService interface {
	// List answers list: every declared setting in the area ("" = all), each
	// a full View (the declaration plus the effective value and its source).
	List(ctx context.Context, area string) ([]config.View, error)
	// Get answers get: one setting's full View. An undeclared key is a
	// refusal (ErrSettingUnknown naming the nearest), never an empty success.
	Get(ctx context.Context, key string) (config.View, error)
	// Set answers set: write the value (canonical text) to the key, stamped
	// by the connection's actor. Returns the new View and whether the stored
	// value actually changed (a no-op write writes nothing).
	Set(ctx context.Context, key, value, actor string) (config.View, bool, error)
	// Unset answers unset: drop the stored row (the effective value falls
	// back to the environment or the default). Returns the new View and
	// whether a row was actually removed (an absent key is changed=false,
	// not an error — an idempotent delete).
	Unset(ctx context.Context, key, actor string) (config.View, bool, error)
	// History answers history: the append-only audit, newest first. It does
	// NOT look the key up in the registry (a key removed from the registry
	// keeps its trail); a "" key is every event. limit <= 0 defaults to 50
	// and clamps to 500 (the store does it; the handler only refuses < 0).
	History(ctx context.Context, key string, limit int) ([]config.Event, error)
}

// --- the wire types ---------------------------------------------------------
//
// These are the field-for-field twins of config.View/config.Event/config.
// Setting, the only layer that may know both. The nullable fields (Stored,
// and the pointer-valued OldValue/NewValue) are *string WITHOUT omitempty so
// the generated TS says `string | null`: a null is the honest "there was
// none", which a bare absent field cannot say (absent would read as "unset").

// SettingType is the closed set of value types (CS3), mirroring config's.
type SettingType string

const (
	SettingTypeBool   SettingType = "bool"
	SettingTypeInt    SettingType = "int"
	SettingTypeString SettingType = "string"
	SettingTypeEnum   SettingType = "enum"
)

// SettingSource names WHICH layer an effective value came from (CS4),
// mirroring config's. It is named SettingSource, not Source, because
// config already exports a Source type for the credential rule.
type SettingSource string

const (
	SettingSourceDefault     SettingSource = "default"
	SettingSourceDatabase    SettingSource = "database"
	SettingSourceEnvironment SettingSource = "environment"
)

// Setting is the declared key, the wire twin of config.Setting. It drops
// config's Scope (always global, CS6) and its unexported-by-design fields,
// carrying only what a client reads.
type Setting struct {
	Key         string      `json:"key"`
	Area        string      `json:"area"`
	Type        SettingType `json:"type"`
	Default     string      `json:"default"`
	Description string      `json:"description"`
	Sensitive   bool        `json:"sensitive"`
	// Type-specific bounds (a bound that does not apply to the type is absent,
	// so the UI knows the type is boundless, not just unbound).
	Min        *int     `json:"min,omitempty"`
	Max        *int     `json:"max,omitempty"`
	MaxLen     int      `json:"maxLen,omitempty"`
	EnumValues []string `json:"enumValues,omitempty"`
}

// SettingView is what get/set/unset return (and list returns a slice of):
// the declaration (embedded), the EFFECTIVE value in canonical form, and the
// facts that name which layer won (CS4). It is the wire twin of
// config.View — same fields, the same null-meaning for the pointer fields.
type SettingView struct {
	Setting

	Value string `json:"value"` // EFFECTIVE value, canonical form
	Source SettingSource `json:"source"` // "default" | "database" | "environment"

	// Stored is what the table holds, or null when no row — the null is the
	// honest "never set", which a bare absent field cannot say.
	Stored *string `json:"stored"`
	// ShadowedBy is the env var name when the environment is in force AND a
	// row exists ("your saved value is being overridden"), "" otherwise.
	ShadowedBy string `json:"shadowedBy"`
	// EnvInvalid is the env var name whose value failed validation and was
	// NOT used, "" otherwise — a loud signal a quiet default would hide.
	EnvInvalid string `json:"envInvalid"`
	// StoredInvalid is the row's value failing the CURRENT declaration, in
	// which case the effective value is the Default.
	StoredInvalid bool `json:"storedInvalid"`
	// UpdatedAt/UpdatedBy come from the row; "" when none (the value is the
	// environment or the default, neither of which has a write timestamp).
	UpdatedAt string `json:"updatedAt"`
	UpdatedBy string `json:"updatedBy"`
}

// SettingEventView is one audit entry, the wire twin of config.Event.
// OldValue/NewValue are pointers (nil = "there was none" — a first write's
// old, an unset's new), without omitempty so the null is honest.
type SettingEventView struct {
	ID         int64   `json:"id"`
	Key        string  `json:"key"`
	Op         string  `json:"op"`
	OldValue   *string `json:"oldValue"`
	NewValue   *string `json:"newValue"`
	Sensitive  bool    `json:"sensitive"`
	ShadowedBy string  `json:"shadowedBy"`
	Actor      string  `json:"actor"`
	OccurredAt string  `json:"occurredAt"`
}

// --- params and results -----------------------------------------------------

// SettingsListParams is list's params: the area to scope the listing to.
// Absent (nil) means every area.
type SettingsListParams struct {
	Area *string `json:"area,omitempty"`
}

// SettingsListResult is list's result: every setting in the area.
type SettingsListResult struct {
	Settings []SettingView `json:"settings"`
}

// SettingsGetParams is get's params: the one key to read.
type SettingsGetParams struct {
	Key string `json:"key"`
}

// SettingsGetResult is get's result: the one setting's full View.
type SettingsGetResult struct {
	Setting SettingView `json:"setting"`
}

// SettingsSetParams is set's params. Value is *string on purpose: a missing
// value (nil) is refused (a set with no value is a silent no-op), while an
// EMPTY string (present, "") is a real value for a string key — the
// missing/empty distinction a plain string cannot hold. A JSON true/3 is not
// a string, so decodeParams refuses it (a value is always canonical text).
type SettingsSetParams struct {
	Key   string  `json:"key"`
	Value *string `json:"value"`
}

// SettingsSetResult is set's result: the new View and whether the stored
// value actually changed (a no-op write, where the value was already this,
// writes nothing and reports changed=false).
type SettingsSetResult struct {
	Setting SettingView `json:"setting"`
	Changed bool        `json:"changed"`
}

// SettingsUnsetParams is unset's params: the one key to unset.
type SettingsUnsetParams struct {
	Key string `json:"key"`
}

// SettingsUnsetResult is unset's result: the new View (the value fell back to
// the environment or the default) and whether a row was actually removed
// (an absent key is changed=false, not an error — an idempotent delete).
type SettingsUnsetResult struct {
	Setting SettingView `json:"setting"`
	Changed bool        `json:"changed"`
}

// SettingsHistoryParams is history's params: the key (absent = every event)
// and the limit (absent = the store's 50).
type SettingsHistoryParams struct {
	Key   *string `json:"key,omitempty"`
	Limit *int    `json:"limit,omitempty"`
}

// SettingsHistoryResult is history's result: the audit entries, newest first.
type SettingsHistoryResult struct {
	Events []SettingEventView `json:"events"`
}

// --- registration -----------------------------------------------------------

// RegisterSettings registers the five settings.* methods over the wired
// seam. It rides with the wiring rather than NewTable because the seam is
// assigned after construction (the daemon's own order, the S33 telemetry
// precedent), and a registration that ran inside NewTable would only ever
// see nil. The nil seam is the V13 configuration speaking — every
// no-settings build arrives here with nil and the five methods stay absent
// (not registered-and-refusing); that is the designed outcome, not a silent
// failure. Returns whether the methods are in the table, so the daemon's
// wiring can be asserted rather than trusted.
func (t *Table) RegisterSettings() bool {
	if t.Settings == nil {
		return false
	}
	t.register(&Method{Name: MethodSettingsList, RequiresInitialize: true, Handle: t.handleSettingsList})
	t.register(&Method{Name: MethodSettingsGet, RequiresInitialize: true, Handle: t.handleSettingsGet})
	t.register(&Method{Name: MethodSettingsSet, RequiresInitialize: true, Handle: t.handleSettingsSet})
	t.register(&Method{Name: MethodSettingsUnset, RequiresInitialize: true, Handle: t.handleSettingsUnset})
	t.register(&Method{Name: MethodSettingsHistory, RequiresInitialize: true, Handle: t.handleSettingsHistory})
	return true
}

// --- the actor --------------------------------------------------------------

// settingActor is the write boundary's audit identity (CS8): the actor comes
// from the CONNECTION (the daemon's ActorFor seam), never from params — a
// params actor would let a caller forge the audit identity of a write to
// their own rules. It is the honest absence (a storeless daemon has no
// persons) that refuses a write, and an empty id that is refused as well.
// The returned string is `kind:id` (e.g. "user:1000"), never empty, so the
// store's own checkActor (non-empty, capped) passes by construction.
func (t *Table) settingActor(ctx context.Context, sess *Session) (string, error) {
	if t.ActorFor == nil {
		return "", ErrBadParams{Err: fmt.Errorf("settings are written by a person; no identity is available on this connection")}
	}
	a := t.ActorFor(ctx, sess)
	if strings.TrimSpace(a.ID) == "" {
		return "", ErrBadParams{Err: fmt.Errorf("settings are written by a person; no identity is available on this connection")}
	}
	return a.Kind + ":" + a.ID, nil
}

// --- the handlers -----------------------------------------------------------

func (t *Table) handleSettingsList(ctx context.Context, _ *Session, params json.RawMessage) (any, error) {
	// Unreachable in any built configuration — registration is conditional on
	// this same field. The guard is the tripwire that says registration and
	// the seam disagreed, spoken rather than silent (the search.query shape).
	if t.Settings == nil {
		return nil, ErrStoreUnavailable.WithWhat("settings.* needs the settings store, and this build did not link it — the settings surface is absent for the same reason (V13)")
	}
	var p SettingsListParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	views, err := t.Settings.List(ctx, areaOf(p))
	if err != nil {
		return nil, t.settingError(err)
	}
	result := SettingsListResult{Settings: make([]SettingView, 0, len(views))}
	for _, v := range views {
		result.Settings = append(result.Settings, settingView(v))
	}
	return result, nil
}

func (t *Table) handleSettingsGet(ctx context.Context, _ *Session, params json.RawMessage) (any, error) {
	if t.Settings == nil {
		return nil, ErrStoreUnavailable.WithWhat("settings.* needs the settings store, and this build did not link it — the settings surface is absent for the same reason (V13)")
	}
	var p SettingsGetParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.Key) == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("key is required — name the setting to read")}
	}
	v, err := t.Settings.Get(ctx, p.Key)
	if err != nil {
		return nil, t.settingError(err)
	}
	return SettingsGetResult{Setting: settingView(v)}, nil
}

func (t *Table) handleSettingsSet(ctx context.Context, sess *Session, params json.RawMessage) (any, error) {
	if t.Settings == nil {
		return nil, ErrStoreUnavailable.WithWhat("settings.* needs the settings store, and this build did not link it — the settings surface is absent for the same reason (V13)")
	}
	var p SettingsSetParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.Key) == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("key is required — name the setting to set")}
	}
	if p.Value == nil {
		// The missing/empty distinction a plain string cannot hold (CS10): a
		// set with NO value is a silent no-op, refused; an empty string IS a
		// real value for a string key, allowed.
		return nil, ErrBadParams{Err: fmt.Errorf("value is required — a set with no value does nothing; an empty string is a real value for a string key, not an absence")}
	}
	actor, err := t.settingActor(ctx, sess)
	if err != nil {
		return nil, err
	}
	v, changed, err := t.Settings.Set(ctx, p.Key, *p.Value, actor)
	if err != nil {
		return nil, t.settingError(err)
	}
	return SettingsSetResult{Setting: settingView(v), Changed: changed}, nil
}

func (t *Table) handleSettingsUnset(ctx context.Context, sess *Session, params json.RawMessage) (any, error) {
	if t.Settings == nil {
		return nil, ErrStoreUnavailable.WithWhat("settings.* needs the settings store, and this build did not link it — the settings surface is absent for the same reason (V13)")
	}
	var p SettingsUnsetParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.Key) == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("key is required — name the setting to unset")}
	}
	actor, err := t.settingActor(ctx, sess)
	if err != nil {
		return nil, err
	}
	v, changed, err := t.Settings.Unset(ctx, p.Key, actor)
	if err != nil {
		return nil, t.settingError(err)
	}
	return SettingsUnsetResult{Setting: settingView(v), Changed: changed}, nil
}

func (t *Table) handleSettingsHistory(ctx context.Context, _ *Session, params json.RawMessage) (any, error) {
	if t.Settings == nil {
		return nil, ErrStoreUnavailable.WithWhat("settings.* needs the settings store, and this build did not link it — the settings surface is absent for the same reason (V13)")
	}
	var p SettingsHistoryParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	// limit < 0 is refused (a negative count is a caller mistake); the store
	// clamps 0→50 and >500→500, so the handler only guards the < 0 case.
	limit := 0
	if p.Limit != nil {
		if *p.Limit < 0 {
			return nil, ErrBadParams{Err: fmt.Errorf("limit must be non-negative — the store clamps an absent limit to its 50 default, it does not accept a negative one")}
		}
		limit = *p.Limit
	}
	key := ""
	if p.Key != nil {
		key = *p.Key
	}
	// history does not look the key up in the registry (a key removed from the
	// registry keeps its trail), so an undeclared key here is NOT unknown.
	events, err := t.Settings.History(ctx, key, limit)
	if err != nil {
		return nil, t.settingError(err)
	}
	result := SettingsHistoryResult{Events: make([]SettingEventView, 0, len(events))}
	for _, e := range events {
		result.Events = append(result.Events, settingEventView(e))
	}
	return result, nil
}

// --- the mapping ------------------------------------------------------------
//
// The mapping is the whole reason the wire types exist: config's View/Event
// are the source of truth, and the handler projects them to THIS package's
// twins without re-deriving anything (the precedence, the invalid flags and
// ShadowedBy are computed ONCE in config's resolve — the handler copies the
// fields, it does not recompute them). A second precedence would be a second
// truth, which is the bug CS4 exists to prevent.

func areaOf(p SettingsListParams) string {
	if p.Area == nil {
		return ""
	}
	return *p.Area
}

func settingView(v config.View) SettingView {
	return SettingView{
		Setting:       wireSetting(v.Setting),
		Value:         v.Value,
		Source:        SettingSource(v.Source),
		Stored:        v.Stored,
		ShadowedBy:    v.ShadowedBy,
		EnvInvalid:    v.EnvInvalid,
		StoredInvalid: v.StoredInvalid,
		UpdatedAt:     v.UpdatedAt,
		UpdatedBy:     v.UpdatedBy,
	}
}

func wireSetting(s config.Setting) Setting {
	return Setting{
		Key:         s.Key,
		Area:        s.Area,
		Type:        SettingType(s.Type),
		Default:     s.Default,
		Description: s.Description,
		Sensitive:   s.Sensitive,
		Min:         s.Min,
		Max:         s.Max,
		MaxLen:      s.MaxLen,
		EnumValues:  s.EnumValues,
	}
}

func settingEventView(e config.Event) SettingEventView {
	return SettingEventView{
		ID:         e.ID,
		Key:        e.Key,
		Op:         e.Op,
		OldValue:   e.OldValue,
		NewValue:   e.NewValue,
		Sensitive:  e.Sensitive,
		ShadowedBy: e.ShadowedBy,
		Actor:      e.Actor,
		OccurredAt: e.OccurredAt,
	}
}

// settingError maps the store's own domain refusals to the protocol's two
// codes, VERBATIM: the three parts (What/Means/ToDo) travel unchanged, the
// handler only fills the What with the specifics the store already named
// (the key, the nearest declared keys, the expected form and what was
// given). An infrastructure error (a repo read that failed, P31) passes
// through as-is — it is not a domain refusal, so it keeps its own message
// and the transport's -32603, not a confident wrong Aeon code.
func (t *Table) settingError(err error) error {
	var unknown *config.ErrSettingUnknown
	if errors.As(err, &unknown) {
		return ErrSettingUnknown.WithWhat(
			"the setting %q is not a declared setting; nearest declared: %s",
			unknown.Key, nearestText(unknown.Nearest),
		)
	}
	var invalid *config.ErrSettingInvalidValue
	if errors.As(err, &invalid) {
		return ErrSettingInvalidValue.WithWhat(
			"the setting %q cannot be set to %s — expected %s",
			invalid.Key, invalid.Got, invalid.Expected,
		)
	}
	return err
}

// nearestText renders the store's nearest declared keys for the What; an
// empty registry is the honest "none declared", not a confident typo.
func nearestText(nearest []string) string {
	if len(nearest) == 0 {
		return "(none declared)"
	}
	return strings.Join(nearest, ", ")
}
