package store

// notify.go — the store half of S58's outbound notify plane (T1). Three
// kinds of row live here: the class table the resolver will read (T4),
// the channel/subscription records (authority rows: created disabled,
// every write audited in the same transaction), and the append-only
// delivery log. The secret NEVER lives here — a channel row holds a
// `secret:notify/<id>` reference and the bytes live in the secrets store
// behind it (§4c's discipline one floor down from the routes precedent).
//
// Lock 3 (SV8) is two gates deep: the migration's
// `created_by_kind CHECK (= 'user')` refuses at SQLite, and CreateChannel
// refuses one statement earlier with a sentence. The delivery log has no
// foreign key to the channel on purpose (SV12): a channel's history
// outlives the channel, and a log that cascaded away with its row would
// make "did we notify anyone?" unanswerable the day someone archives the
// thing.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
)

// NotifyClass is one notify_event_class row — a (kind, variant) pair and
// how it should read when it fires. Variant is ” for the kind's default
// shape; the blocked/waiting families use the variant to split (T4/T2
// decide matching, this file only stores). Version is the seed engine's
// counter (the applier owns it; nothing here bumps it on its own).
type NotifyClass struct {
	Kind          string
	Variant       string
	Tone          string // info | success | warning | danger (CHECK, mirrored from protocol.NotifyTone and the app)
	TitleTemplate string
	BodyTemplate  string
	Enabled       bool
	Origin        string // system | user | import
	Version       int
	UpdatedAt     string
}

// ListNotifyClasses reads the whole class table. It is small by
// construction — kinds are a closed list and variants are curated — so
// the resolver reads it whole rather than per lookup.
func (s *Store) ListNotifyClasses() ([]NotifyClass, error) {
	rows, err := s.r.Query(`SELECT kind, variant, tone, title_template, body_template, enabled, origin, version, updated_at
		FROM notify_event_class ORDER BY kind, variant`)
	if err != nil {
		return nil, fmt.Errorf("store: list notify classes: %w", err)
	}
	defer rows.Close()
	var out []NotifyClass
	for rows.Next() {
		c, err := scanNotifyClass(rows)
		if err != nil {
			return nil, fmt.Errorf("store: list notify classes: %w", err)
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list notify classes: %w", err)
	}
	return out, nil
}

// GetNotifyClass reads one (kind, variant) row. Absent is (zero, false,
// nil): an unclassified kind is the normal state, not an error.
func (s *Store) GetNotifyClass(kind, variant string) (NotifyClass, bool, error) {
	return getNotifyClassQuery(s.r, kind, variant)
}

// GetNotifyClassTx reads one class row INSIDE the caller's transaction —
// the seed applier's fingerprint read (T4): the engine holds the write
// transaction and the applier must see exactly what the write will see
// (the BlueprintRowTx precedent).
func (s *Store) GetNotifyClassTx(ctx context.Context, tx *sql.Tx, kind, variant string) (NotifyClass, bool, error) {
	return getNotifyClassQuery(tx, kind, variant)
}

type notifyClassQuerier interface {
	QueryRow(query string, args ...any) *sql.Row
}

func getNotifyClassQuery(q notifyClassQuerier, kind, variant string) (NotifyClass, bool, error) {
	row := q.QueryRow(`SELECT kind, variant, tone, title_template, body_template, enabled, origin, version, updated_at
		FROM notify_event_class WHERE kind=? AND variant=?`, kind, variant)
	c, err := scanNotifyClass(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return NotifyClass{}, false, nil
		}
		return NotifyClass{}, false, fmt.Errorf("store: get notify class %s/%s: %w", kind, variant, err)
	}
	return c, true, nil
}

func scanNotifyClass(r rowScanner) (NotifyClass, error) {
	var c NotifyClass
	var enabled int
	err := r.Scan(&c.Kind, &c.Variant, &c.Tone, &c.TitleTemplate, &c.BodyTemplate,
		&enabled, &c.Origin, &c.Version, &c.UpdatedAt)
	c.Enabled = enabled != 0
	return c, err
}

// UpsertNotifyClassTx is the class write inside a caller's transaction —
// the form the seed applier uses, so a class row and its ledger row
// commit together (the WatchdogSetTx precedent). The values land as
// given: origin and version are the engine's to set, not ours to second-
// guess, and this file writes no notify_change row for a class — the
// engine's own audit covers seeded writes; the human door's
// notify.class.update (T4) audits through UpdateNotifyClass.
func (s *Store) UpsertNotifyClassTx(tx *sql.Tx, c NotifyClass) error {
	_, err := tx.Exec(`INSERT INTO notify_event_class
		(kind, variant, tone, title_template, body_template, enabled, origin, version, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?)
		ON CONFLICT(kind, variant) DO UPDATE SET
		  tone=excluded.tone, title_template=excluded.title_template, body_template=excluded.body_template,
		  enabled=excluded.enabled, origin=excluded.origin, version=excluded.version, updated_at=excluded.updated_at`,
		c.Kind, c.Variant, c.Tone, c.TitleTemplate, c.BodyTemplate,
		boolInt(c.Enabled), c.Origin, c.Version, c.UpdatedAt)
	if err != nil {
		return fmt.Errorf("store: upsert notify class %s/%s: %w", c.Kind, c.Variant, err)
	}
	return nil
}

// UpdateNotifyClass is the human door's write (tone/enabled flip on an
// existing row), audited in the same transaction. A missing row is
// ErrNotFound — the door offers updates, the seed engine offers creates.
// UpdateNotifyClass is the human door's ONE write over a class row:
// enabled, tone and the two templates, each optional (nil keeps the
// value). The template CONTENT is validated by the caller (the notify
// package's closed placeholder set, SV4) — the store keeps the column
// shapes; the door keeps the vocabulary. A changing write bumps the row
// version (so the seed fingerprint sees an edit as a divergence) and
// appends exactly one notify_change row with a FIXED-template summary —
// field names and enums only, never a template body (SV12: the audit
// quotes what changed, never what a person wrote). A no-op returns the
// row and writes nothing (LC11 discipline).
func (s *Store) UpdateNotifyClass(kind, variant string, tone *string, enabled *bool, titleTemplate, bodyTemplate *string, actorKind, actorID string) (NotifyClass, error) {
	tx, err := s.w.Begin()
	if err != nil {
		return NotifyClass{}, fmt.Errorf("store: notify class update: %w", err)
	}
	defer tx.Rollback()
	var cur NotifyClass
	var curEnabled int
	err = tx.QueryRow(`SELECT kind, variant, tone, title_template, body_template, enabled, origin, version, updated_at
		FROM notify_event_class WHERE kind=? AND variant=?`, kind, variant).
		Scan(&cur.Kind, &cur.Variant, &cur.Tone, &cur.TitleTemplate, &cur.BodyTemplate,
			&curEnabled, &cur.Origin, &cur.Version, &cur.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return NotifyClass{}, fmt.Errorf("store: notify class %s/%s: %w", kind, variant, ErrNotFound)
		}
		return NotifyClass{}, fmt.Errorf("store: notify class update: %w", err)
	}
	cur.Enabled = curEnabled != 0
	summary := []string{}
	newTone, newEnabled := cur.Tone, cur.Enabled
	newTitle, newBody := cur.TitleTemplate, cur.BodyTemplate
	if tone != nil && *tone != cur.Tone {
		newTone = *tone
		summary = append(summary, "tone="+*tone)
	}
	if enabled != nil && *enabled != cur.Enabled {
		newEnabled = *enabled
		summary = append(summary, fmt.Sprintf("enabled=%t", *enabled))
	}
	if titleTemplate != nil && *titleTemplate != cur.TitleTemplate {
		newTitle = *titleTemplate
		summary = append(summary, "titleTemplate") // the NAME only — the template body never enters the audit (SV12)
	}
	if bodyTemplate != nil && *bodyTemplate != cur.BodyTemplate {
		newBody = *bodyTemplate
		summary = append(summary, "bodyTemplate")
	}
	if len(summary) == 0 {
		return cur, nil // a no-op write earns no audit row (LC11 discipline)
	}
	now := s.Now()
	if _, err := tx.Exec(`UPDATE notify_event_class SET tone=?, enabled=?, title_template=?, body_template=?, version=version+1, updated_at=?
		WHERE kind=? AND variant=?`, newTone, boolInt(newEnabled), newTitle, newBody, now, kind, variant); err != nil {
		return NotifyClass{}, fmt.Errorf("store: notify class update %s/%s: %w", kind, variant, err)
	}
	if err := appendNotifyChange(tx, "class", kind+"|"+variant, "update", actorKind, actorID, now, strings.Join(summary, " ")); err != nil {
		return NotifyClass{}, err
	}
	if err := tx.Commit(); err != nil {
		return NotifyClass{}, fmt.Errorf("store: notify class update: %w", err)
	}
	cur.Tone, cur.Enabled, cur.TitleTemplate, cur.BodyTemplate, cur.Version, cur.UpdatedAt = newTone, newEnabled, newTitle, newBody, cur.Version+1, now
	return cur, nil
}

// NotifyChannel is one notify_channel row. Enabled defaults to false at
// every door that creates a row (SV8: a channel must be tested before it
// can speak, so it is born silent); ArchivedAt != "" means archived —
// an archive is never a delete, and archived channels never sit on the
// host allowlist.
type NotifyChannel struct {
	ID            string
	Kind          string // webhook (the only kind today; CHECK)
	Name          string
	Enabled       bool
	URL           string
	SecretRef     string
	AllowPrivate  bool
	CreatedByKind string
	CreatedByID   string
	CreatedAt     string
	UpdatedAt     string
	ArchivedAt    string
	ArchiveReason string
}

// CreateChannelIn is the create's input. There is deliberately NO
// Enabled field: a created channel is disabled, full stop — enabling is
// its own audited act (SetChannelEnabled) whose test-gate lives at the
// daemon door (T5), not in the caller's hands.
type CreateChannelIn struct {
	Name         string
	URL          string
	AllowPrivate bool
	ActorKind    string
	ActorID      string
}

// ErrNotAPerson names the lock-3 refusal before SQL does: only a person
// creates a channel.
var ErrNotAPerson = errors.New("store: channel creator must be a person (S58 SV8 lock 3)")

func (s *Store) CreateChannel(in CreateChannelIn) (NotifyChannel, error) {
	if in.ActorKind != "user" {
		return NotifyChannel{}, ErrNotAPerson
	}
	if strings.TrimSpace(in.Name) == "" || strings.TrimSpace(in.URL) == "" {
		return NotifyChannel{}, fmt.Errorf("store: create channel: %w", errors.New("name and url are required"))
	}
	if _, err := channelHost(in.URL); err != nil {
		return NotifyChannel{}, err
	}
	id := "nc-" + newID()
	tx, err := s.w.Begin()
	if err != nil {
		return NotifyChannel{}, fmt.Errorf("store: create channel: %w", err)
	}
	defer tx.Rollback()
	now := s.Now()
	ch := NotifyChannel{
		ID: id, Kind: "webhook", Name: in.Name, Enabled: false, URL: in.URL,
		SecretRef: "secret:notify/" + id, AllowPrivate: in.AllowPrivate,
		CreatedByKind: in.ActorKind, CreatedByID: in.ActorID, CreatedAt: now, UpdatedAt: now,
	}
	if _, err := tx.Exec(`INSERT INTO notify_channel
		(id, kind, name, enabled, url, secret_ref, allow_private, created_by_kind, created_by_id, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		ch.ID, ch.Kind, ch.Name, boolInt(ch.Enabled), ch.URL, ch.SecretRef, boolInt(ch.AllowPrivate),
		ch.CreatedByKind, ch.CreatedByID, ch.CreatedAt, ch.UpdatedAt); err != nil {
		return NotifyChannel{}, fmt.Errorf("store: create channel: %w", err)
	}
	if err := appendNotifyChange(tx, "channel", id, "create", in.ActorKind, in.ActorID,
		"now",
		"webhook "+ch.Name+" host="+mustChannelHost(ch.URL)); err != nil {
		return NotifyChannel{}, err
	}
	if err := tx.Commit(); err != nil {
		return NotifyChannel{}, fmt.Errorf("store: create channel: %w", err)
	}
	return ch, nil
}

// UpdateChannelIn carries the editable fields; nil means "do not touch"
// (the empty-string-vs-unset discipline routes.ts taught). The URL may
// change; the id, the kind, the creator and the secret_ref never do.
type UpdateChannelIn struct {
	Name         *string
	URL          *string
	AllowPrivate *bool
	ActorKind    string
	ActorID      string
}

func (s *Store) UpdateChannel(id string, in UpdateChannelIn) (NotifyChannel, error) {
	tx, err := s.w.Begin()
	if err != nil {
		return NotifyChannel{}, fmt.Errorf("store: channel update: %w", err)
	}
	defer tx.Rollback()
	cur, err := getChannelTx(tx, id)
	if err != nil {
		return NotifyChannel{}, err
	}
	sets, args, summary := []string{}, []any{}, []string{}
	if in.Name != nil && *in.Name != cur.Name {
		if strings.TrimSpace(*in.Name) == "" {
			return NotifyChannel{}, fmt.Errorf("store: channel update: %w", errors.New("name must not be empty"))
		}
		sets = append(sets, "name=?")
		args = append(args, *in.Name)
		summary = append(summary, "name")
	}
	if in.URL != nil && *in.URL != cur.URL {
		host, err := channelHost(*in.URL)
		if err != nil {
			return NotifyChannel{}, err
		}
		sets = append(sets, "url=?")
		args = append(args, *in.URL)
		summary = append(summary, "url host="+host) // the host, never the URL — it may carry a token in its query
	}
	if in.AllowPrivate != nil && *in.AllowPrivate != cur.AllowPrivate {
		sets = append(sets, "allow_private=?")
		args = append(args, boolInt(*in.AllowPrivate))
		summary = append(summary, fmt.Sprintf("allow_private=%t", *in.AllowPrivate))
	}
	if len(sets) == 0 {
		return cur, nil
	}
	now := s.Now()
	sets = append(sets, "updated_at=?")
	args = append(args, now)
	args = append(args, id)
	if _, err := tx.Exec("UPDATE notify_channel SET "+strings.Join(sets, ", ")+" WHERE id=?", args...); err != nil {
		return NotifyChannel{}, fmt.Errorf("store: channel update %s: %w", id, err)
	}
	if err := appendNotifyChange(tx, "channel", id, "update", in.ActorKind, in.ActorID, now, strings.Join(summary, " ")); err != nil {
		return NotifyChannel{}, err
	}
	if err := tx.Commit(); err != nil {
		return NotifyChannel{}, fmt.Errorf("store: channel update: %w", err)
	}
	return getChannelQuery(s.r, id)
}

// SetChannelEnabled is the enable/disable write — its own audited act,
// never a side effect of an update. An archived channel refuses: an
// archive is terminal (un-archive is not in the surface; a new channel
// is the way back).
func (s *Store) SetChannelEnabled(id string, enabled bool, actorKind, actorID string) (NotifyChannel, error) {
	tx, err := s.w.Begin()
	if err != nil {
		return NotifyChannel{}, fmt.Errorf("store: channel enabled: %w", err)
	}
	defer tx.Rollback()
	cur, err := getChannelTx(tx, id)
	if err != nil {
		return NotifyChannel{}, err
	}
	if cur.ArchivedAt != "" {
		return NotifyChannel{}, fmt.Errorf("store: channel %s is archived: %w", id, ErrNotFound)
	}
	if cur.Enabled == enabled {
		return cur, nil
	}
	now := s.Now()
	if _, err := tx.Exec(`UPDATE notify_channel SET enabled=?, updated_at=? WHERE id=?`,
		boolInt(enabled), now, id); err != nil {
		return NotifyChannel{}, fmt.Errorf("store: channel enable %s: %w", id, err)
	}
	op := "disable"
	if enabled {
		op = "enable"
	}
	if err := appendNotifyChange(tx, "channel", id, op, actorKind, actorID, now, op+"d"); err != nil {
		return NotifyChannel{}, err
	}
	if err := tx.Commit(); err != nil {
		return NotifyChannel{}, fmt.Errorf("store: channel enabled: %w", err)
	}
	cur.Enabled, cur.UpdatedAt = enabled, now
	return cur, nil
}

// ArchiveChannel retires a channel: enabled off, archived_at stamped,
// reason kept. The row and its delivery history survive (SV12).
func (s *Store) ArchiveChannel(id, reason, actorKind, actorID string) (NotifyChannel, error) {
	tx, err := s.w.Begin()
	if err != nil {
		return NotifyChannel{}, fmt.Errorf("store: channel archive: %w", err)
	}
	defer tx.Rollback()
	cur, err := getChannelTx(tx, id)
	if err != nil {
		return NotifyChannel{}, err
	}
	if cur.ArchivedAt != "" {
		return cur, nil
	}
	now := s.Now()
	if _, err := tx.Exec(`UPDATE notify_channel SET enabled=0, archived_at=?, archive_reason=?, updated_at=? WHERE id=?`,
		now, reason, now, id); err != nil {
		return NotifyChannel{}, fmt.Errorf("store: channel archive %s: %w", id, err)
	}
	if err := appendNotifyChange(tx, "channel", id, "archive", actorKind, actorID, now, reason); err != nil {
		return NotifyChannel{}, err
	}
	if err := tx.Commit(); err != nil {
		return NotifyChannel{}, fmt.Errorf("store: channel archive: %w", err)
	}
	cur.Enabled, cur.ArchivedAt, cur.ArchiveReason, cur.UpdatedAt = false, now, reason, now
	return cur, nil
}

// AuditChannelSecret records the act of replacing a channel's secret.
// The value itself lives in the credentials directory and never here —
// the audit names the field that changed and nothing more (SV12: names,
// never bodies), and `updated_at` moves because the channel really did
// change.
func (s *Store) AuditChannelSecret(id, actorKind, actorID string) error {
	if actorKind != "user" {
		return ErrNotAPerson
	}
	tx, err := s.w.Begin()
	if err != nil {
		return fmt.Errorf("store: channel secret audit: %w", err)
	}
	defer tx.Rollback()
	if _, err := getChannelTx(tx, id); err != nil {
		return err
	}
	now := s.Now()
	if _, err := tx.Exec(`UPDATE notify_channel SET updated_at=? WHERE id=?`, now, id); err != nil {
		return fmt.Errorf("store: channel secret audit %s: %w", id, err)
	}
	if err := appendNotifyChange(tx, "channel", id, "update", actorKind, actorID, now, "secret"); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) GetChannel(id string) (NotifyChannel, bool, error) {
	ch, err := getChannelQuery(s.r, id)
	if errors.Is(err, ErrNotFound) {
		return NotifyChannel{}, false, nil
	}
	if err != nil {
		return NotifyChannel{}, false, err
	}
	return ch, true, nil
}

// ListChannels walks the channel table; archived rows are in only when
// asked for (the door shows them greyed; the dispatcher never sees them).
func (s *Store) ListChannels(includeArchived bool) ([]NotifyChannel, error) {
	q := `SELECT id, kind, name, enabled, url, secret_ref, allow_private, created_by_kind, created_by_id,
		created_at, updated_at, COALESCE(archived_at,''), COALESCE(archive_reason,'')
		FROM notify_channel`
	if !includeArchived {
		q += ` WHERE archived_at IS NULL`
	}
	rows, err := s.r.Query(q + " ORDER BY created_at, id")
	if err != nil {
		return nil, fmt.Errorf("store: list channels: %w", err)
	}
	defer rows.Close()
	var out []NotifyChannel
	for rows.Next() {
		ch, err := scanChannel(rows)
		if err != nil {
			return nil, fmt.Errorf("store: list channels: %w", err)
		}
		out = append(out, ch)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list channels: %w", err)
	}
	return out, nil
}

// ChannelHosts is the nettest allowlist source (SV7): the hosts of every
// ENABLED, non-archived channel, parsed from the stored URLs. A disabled
// channel is not on the list — silence does not get to define where
// traffic may go; only a live channel's host is allowed, and the
// dispatcher's refusal of every other address is the invariant the
// nettest guard pins.
func (s *Store) ChannelHosts() ([]string, error) {
	rows, err := s.r.Query(`SELECT url FROM notify_channel WHERE enabled=1 AND archived_at IS NULL`)
	if err != nil {
		return nil, fmt.Errorf("store: channel hosts: %w", err)
	}
	defer rows.Close()
	set := map[string]bool{}
	for rows.Next() {
		var u string
		if err := rows.Scan(&u); err != nil {
			return nil, fmt.Errorf("store: channel hosts: %w", err)
		}
		host, err := channelHost(u)
		if err != nil {
			return nil, err
		}
		set[host] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: channel hosts: %w", err)
	}
	out := make([]string, 0, len(set))
	for h := range set {
		out = append(out, h)
	}
	sort.Strings(out)
	return out, nil
}

func getChannelTx(tx *sql.Tx, id string) (NotifyChannel, error) {
	return getChannelQuery(tx, id)
}

// getChannelQuery reads one channel row through anything that answers a
// *sql.Row — the writer (a tx, mid-transaction) or the reader pool.
func getChannelQuery(q interface {
	QueryRow(query string, args ...any) *sql.Row
}, id string) (NotifyChannel, error) {
	row := q.QueryRow(`SELECT id, kind, name, enabled, url, secret_ref, allow_private, created_by_kind, created_by_id,
		created_at, updated_at, COALESCE(archived_at,''), COALESCE(archive_reason,'') FROM notify_channel WHERE id=?`, id)
	ch, err := scanChannel(row)
	if errors.Is(err, sql.ErrNoRows) {
		return NotifyChannel{}, fmt.Errorf("store: notify channel %s: %w", id, ErrNotFound)
	}
	if err != nil {
		return NotifyChannel{}, fmt.Errorf("store: read channel %s: %w", id, err)
	}
	return ch, nil
}

func scanChannel(r rowScanner) (NotifyChannel, error) {
	var ch NotifyChannel
	var enabled, allowPrivate int
	err := r.Scan(&ch.ID, &ch.Kind, &ch.Name, &enabled, &ch.URL, &ch.SecretRef, &allowPrivate,
		&ch.CreatedByKind, &ch.CreatedByID, &ch.CreatedAt, &ch.UpdatedAt, &ch.ArchivedAt, &ch.ArchiveReason)
	ch.Enabled, ch.AllowPrivate = enabled != 0, allowPrivate != 0
	return ch, err
}

// channelHost parses the one URL shape a channel may hold (http/https,
// an explicit host). The store refuses nonsense at the door so the
// dispatcher never meets a URL it cannot name a host for — and an
// https-only RULE is T5/door policy, not this file's invention.
func channelHost(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", fmt.Errorf("store: channel url: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("store: channel url: scheme %q is not http or https", u.Scheme)
	}
	if u.Hostname() == "" {
		return "", fmt.Errorf("store: channel url: no host")
	}
	return strings.ToLower(u.Hostname()), nil
}

func mustChannelHost(raw string) string {
	h, _ := channelHost(raw)
	return h
}

// NotifySubscription scopes one channel to one root (” = every root,
// the S40 convention). MinSeverity is the attention floor the dispatcher
// compares against T2's severities; Kinds is an optional JSON list
// narrowing the kinds (opaque here — T5's dispatcher parses it). The
// quiet window is all-or-none (the CHECK + the pre-check): a half-set
// window is a silent window nobody configured on purpose.
type NotifySubscription struct {
	ChannelID   string
	RootID      string
	MinSeverity string
	Kinds       string
	QuietStart  string
	QuietEnd    string
	QuietZone   string
	MaxPerHour  int
}

type SetSubscriptionIn struct {
	ChannelID   string
	RootID      string
	MinSeverity string
	Kinds       string
	QuietStart  string
	QuietEnd    string
	QuietZone   string
	MaxPerHour  int // 0 = default (30)
	ActorKind   string
	ActorID     string
}

// SetSubscription upserts the (channel, root) row and audits it in the
// same transaction. The channel must exist — the table has no FK to
// component on purpose (a subscription survives tree changes), but the
// channel is a row the subscription attaches TO, so its absence is a
// refusal, not a dangling key.
func (s *Store) SetSubscription(in SetSubscriptionIn) (NotifySubscription, error) {
	if in.MaxPerHour == 0 {
		in.MaxPerHour = 30
	}
	if in.MaxPerHour < 1 || in.MaxPerHour > 1000 {
		return NotifySubscription{}, fmt.Errorf("store: subscription: max_per_hour %d is outside 1..1000", in.MaxPerHour)
	}
	set := 0
	for _, v := range []string{in.QuietStart, in.QuietEnd, in.QuietZone} {
		if v != "" {
			set++
		}
	}
	if set != 0 && set != 3 {
		return NotifySubscription{}, fmt.Errorf("store: subscription: quiet hours are all three fields or none (got %d of 3)", set)
	}
	if _, found, err := s.GetChannel(in.ChannelID); err != nil {
		return NotifySubscription{}, err
	} else if !found {
		return NotifySubscription{}, fmt.Errorf("store: subscription: channel %s: %w", in.ChannelID, ErrNotFound)
	}
	tx, err := s.w.Begin()
	if err != nil {
		return NotifySubscription{}, fmt.Errorf("store: set subscription: %w", err)
	}
	defer tx.Rollback()
	null := func(v string) any {
		if v == "" {
			return nil
		}
		return v
	}
	_, err = tx.Exec(`INSERT INTO notify_subscription
		(channel_id, root_id, min_severity, kinds, quiet_start, quiet_end, quiet_zone, max_per_hour)
		VALUES (?,?,?,?,?,?,?,?)
		ON CONFLICT(channel_id, root_id) DO UPDATE SET
		  min_severity=excluded.min_severity, kinds=excluded.kinds,
		  quiet_start=excluded.quiet_start, quiet_end=excluded.quiet_end,
		  quiet_zone=excluded.quiet_zone, max_per_hour=excluded.max_per_hour`,
		in.ChannelID, in.RootID, in.MinSeverity, null(in.Kinds),
		null(in.QuietStart), null(in.QuietEnd), null(in.QuietZone), in.MaxPerHour)
	if err != nil {
		return NotifySubscription{}, fmt.Errorf("store: set subscription %s/%s: %w", in.ChannelID, in.RootID, err)
	}
	scope := in.RootID
	if scope == "" {
		scope = "all roots"
	}
	if err := appendNotifyChange(tx, "subscription", in.ChannelID+"|"+in.RootID, "set", in.ActorKind, in.ActorID, s.Now(),
		fmt.Sprintf("scope=%s min_severity=%s max_per_hour=%d", scope, in.MinSeverity, in.MaxPerHour)); err != nil {
		return NotifySubscription{}, err
	}
	if err := tx.Commit(); err != nil {
		return NotifySubscription{}, fmt.Errorf("store: set subscription: %w", err)
	}
	return NotifySubscription{
		ChannelID: in.ChannelID, RootID: in.RootID, MinSeverity: in.MinSeverity, Kinds: in.Kinds,
		QuietStart: in.QuietStart, QuietEnd: in.QuietEnd, QuietZone: in.QuietZone, MaxPerHour: in.MaxPerHour,
	}, nil
}

// ListSubscriptionsForRoot answers "which subscriptions cover this
// root": the root's own rows plus the every-roots (”) rows.
func (s *Store) ListSubscriptionsForRoot(rootID string) ([]NotifySubscription, error) {
	return s.listSubscriptions(`WHERE (root_id=? OR root_id='')`, rootID)
}

// ListSubscriptions lists a channel's rows (the show door).
func (s *Store) ListSubscriptions(channelID string) ([]NotifySubscription, error) {
	return s.listSubscriptions(`WHERE channel_id=?`, channelID)
}

func (s *Store) listSubscriptions(where string, arg string) ([]NotifySubscription, error) {
	rows, err := s.r.Query(`SELECT channel_id, root_id, min_severity, COALESCE(kinds,''),
		COALESCE(quiet_start,''), COALESCE(quiet_end,''), COALESCE(quiet_zone,''), max_per_hour
		FROM notify_subscription `+where+" ORDER BY channel_id, root_id", arg)
	if err != nil {
		return nil, fmt.Errorf("store: list subscriptions: %w", err)
	}
	defer rows.Close()
	var out []NotifySubscription
	for rows.Next() {
		var sub NotifySubscription
		if err := rows.Scan(&sub.ChannelID, &sub.RootID, &sub.MinSeverity, &sub.Kinds,
			&sub.QuietStart, &sub.QuietEnd, &sub.QuietZone, &sub.MaxPerHour); err != nil {
			return nil, fmt.Errorf("store: list subscriptions: %w", err)
		}
		out = append(out, sub)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list subscriptions: %w", err)
	}
	return out, nil
}

// NotifyDelivery is one append-only outcome row: what left (or refused
// to leave) a channel and how it ended. Suppressions are outcomes too —
// the quiet window, the rate ceiling, a disabled channel all RECORD
// (SV12: an unrecorded suppression is invisible debugging), and their
// absence from the outcome set is what makes "was it suppressed?" a
// query instead of an inference.
type NotifyDelivery struct {
	ID           int64
	ChannelID    string
	EventSeq     int64 // 0 = not tied to an event (a channel test)
	AttentionKey string
	Outcome      string
	ErrorKind    string
	HTTPStatus   int // 0 = none
	Attempts     int
	At           string
}

type AppendDeliveryIn struct {
	ChannelID    string
	EventSeq     int64
	AttentionKey string
	Outcome      string
	ErrorKind    string
	HTTPStatus   int
	Attempts     int
	At           string // empty = now
}

func (s *Store) AppendDelivery(in AppendDeliveryIn) (int64, error) {
	if in.ChannelID == "" || in.Outcome == "" {
		return 0, fmt.Errorf("store: append delivery: %w", errors.New("channel and outcome are required"))
	}
	tx, err := s.w.Begin()
	if err != nil {
		return 0, fmt.Errorf("store: append delivery: %w", err)
	}
	defer tx.Rollback()
	at := in.At
	if at == "" {
		at = s.Now()
	}
	null := func(v any, zero any) any {
		if v == zero {
			return nil
		}
		return v
	}
	res, err := tx.Exec(`INSERT INTO notify_delivery
		(channel_id, event_seq, attention_key, outcome, error_kind, http_status, attempts, at)
		VALUES (?,?,?,?,?,?,?,?)`,
		in.ChannelID, null(in.EventSeq, int64(0)), null(in.AttentionKey, ""), in.Outcome,
		null(in.ErrorKind, ""), null(in.HTTPStatus, 0), in.Attempts, at)
	if err != nil {
		return 0, fmt.Errorf("store: append delivery %s: %w", in.ChannelID, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("store: append delivery: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("store: append delivery: %w", err)
	}
	return id, nil
}

// ListDeliveries reads a channel's recent outcomes, newest first. limit
// <= 0 falls back to 50, the ListWatchdogEvents convention.
func (s *Store) ListDeliveries(channelID string, limit int) ([]NotifyDelivery, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.r.Query(`SELECT id, channel_id, COALESCE(event_seq,0), COALESCE(attention_key,''),
		outcome, COALESCE(error_kind,''), COALESCE(http_status,0), attempts, at
		FROM notify_delivery WHERE channel_id=? ORDER BY id DESC LIMIT ?`, channelID, limit)
	if err != nil {
		return nil, fmt.Errorf("store: list deliveries: %w", err)
	}
	defer rows.Close()
	var out []NotifyDelivery
	for rows.Next() {
		var d NotifyDelivery
		if err := rows.Scan(&d.ID, &d.ChannelID, &d.EventSeq, &d.AttentionKey,
			&d.Outcome, &d.ErrorKind, &d.HTTPStatus, &d.Attempts, &d.At); err != nil {
			return nil, fmt.Errorf("store: list deliveries: %w", err)
		}
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list deliveries: %w", err)
	}
	return out, nil
}

// RecentDeliveryCount counts a channel's ACTUAL sends since `since`
// (sent or failed — a suppression never used a send slot). This is the
// rate window's read. T1 deviation, recorded: the task text names a
// `(channel, sub, since)` shape, but the delivery table (SV12's design)
// carries no subscription column — a delivery belongs to a channel — so
// the ceiling is counted per channel and the dispatcher (T5) compares
// each subscription's own max_per_hour against it.
func (s *Store) RecentDeliveryCount(channelID, since string) (int, error) {
	var n int
	err := s.r.QueryRow(`SELECT COUNT(*) FROM notify_delivery
		WHERE channel_id=? AND at>=? AND outcome IN ('sent','failed')`, channelID, since).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("store: recent delivery count %s: %w", channelID, err)
	}
	return n, nil
}

// appendNotifyChange writes one audit row, always inside the caller's
// transaction — a channel/subscription/class write and its audit commit
// together or not at all (the policy_change discipline).
func appendNotifyChange(tx *sql.Tx, subjectKind, subjectID, op, actorKind, actorID, occurredAt, summary string) error {
	_, err := tx.Exec(`INSERT INTO notify_change
		(subject_kind, subject_id, op, actor_kind, actor_id, occurred_at, summary)
		VALUES (?,?,?,?,?,?,?)`,
		subjectKind, subjectID, op, actorKind, actorID, occurredAt, summary)
	if err != nil {
		return fmt.Errorf("store: notify change %s %s: %w", subjectKind, op, err)
	}
	return nil
}
