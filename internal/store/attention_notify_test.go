package store

// attention_notify_test.go — the S58 T1 locks: the six supervision tables
// exist on fresh AND upgraded databases; the migration adds nothing to
// any pre-existing table; the three append-only tables refuse UPDATE and
// DELETE at SQLite and have no mutating accessor in Go; a channel is
// born disabled, person-only, secret-free; subscriptions are all-or-none
// on quiet hours and ''-means-every-root on scope; the class table's
// tone CHECK is exactly the four-value vocabulary; deliveries record
// suppressions and count sends.

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readOwnSource(t *testing.T, name string) string {
	t.Helper()
	// Tests run in the package directory; these are the files the grep
	// lock reads, not fixtures.
	src, err := os.ReadFile(name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(src)
}

func supervisionTables() []string {
	return []string{"attention_ack", "notify_event_class", "notify_channel",
		"notify_subscription", "notify_delivery", "notify_change"}
}

func hasObject(t *testing.T, s *Store, kind, name string) bool {
	t.Helper()
	var got string
	err := s.w.QueryRow(`SELECT name FROM sqlite_master WHERE type=? AND name=?`, kind, name).Scan(&got)
	return err == nil && got == name
}

func TestSupervisionTablesExistFreshAndUpgraded(t *testing.T) {
	// The same walk on both vintages — fresh (every CREATE ran) and the
	// upgraded frozen-vintage database (every ALTER path ran), which is
	// also what TestAFreshSchemaAndAnUpgradedSchemaAreIdentical compares
	// whole-for-whole. Triggers and indexes are pinned here because the
	// fingerprint (by design) reads only tables and indexes' shapes, not
	// trigger text.
	for _, s := range []*Store{openTemp(t), upgradedStore(t)} {
		for _, tbl := range supervisionTables() {
			if !hasObject(t, s, "table", tbl) {
				t.Errorf("table %s missing", tbl)
			}
		}
		for _, trg := range []string{"attention_ack_no_update", "attention_ack_no_delete",
			"notify_delivery_no_update", "notify_delivery_no_delete",
			"notify_change_no_update", "notify_change_no_delete"} {
			if !hasObject(t, s, "trigger", trg) {
				t.Errorf("append-only trigger %s missing", trg)
			}
		}
		for _, idx := range []string{"idx_attention_ack_subject", "idx_notify_delivery_channel"} {
			if !hasObject(t, s, "index", idx) {
				t.Errorf("index %s missing", idx)
			}
		}
	}
}

func TestSupervisionMigrationRebuildsNothing(t *testing.T) {
	// Everything the fingerprint sees BEFORE my migration must still be
	// there UNCHANGED after it: every added fingerprint line names one of
	// my six tables, and no line is only-in-before (no table lost a
	// column, a CHECK or an index).
	all := Migrations()
	at := -1
	for i, m := range all {
		if strings.HasSuffix(m.Name, "_supervision") {
			at = i
		}
	}
	if at < 0 {
		t.Fatal("the supervision migration is not in Migrations()")
	}
	// Sibling migrations may apply AFTER mine (S49's automation_management
	// landed the same day, one second later — exactly the parallel-worktree
	// case the timestamp naming exists for), so the comparison is between
	// the two ledger prefixes: everything BEFORE my migration against
	// everything through it. The delta is then exactly my migration's
	// fingerprint and nothing a sibling added can leak in.
	prefix := filepath.Join(t.TempDir(), "prefix", storeFileName)
	if err := os.MkdirAll(filepath.Dir(prefix), 0o755); err != nil {
		t.Fatal(err)
	}
	g0, closeBefore := rawGorm(t, prefix)
	applyMigrations(t, g0, all[:at])
	raw0, err := g0.DB()
	if err != nil {
		t.Fatal(err)
	}
	before, err := fingerprint(raw0)
	if err != nil {
		t.Fatal(err)
	}
	closeBefore()

	through := filepath.Join(t.TempDir(), "through", storeFileName)
	if err := os.MkdirAll(filepath.Dir(through), 0o755); err != nil {
		t.Fatal(err)
	}
	g1, closeAfter := rawGorm(t, through)
	applyMigrations(t, g1, all[:at+1])
	raw1, err := g1.DB()
	if err != nil {
		t.Fatal(err)
	}
	after, err := fingerprint(raw1)
	if err != nil {
		t.Fatal(err)
	}
	closeAfter()
	diff := before.Diff(after)
	added := 0
	for _, line := range diff {
		if strings.HasPrefix(line, "only in the second:") {
			added++
			mine := false
			for _, tbl := range supervisionTables() {
				if strings.Contains(line, tbl) {
					mine = true
				}
			}
			if !mine {
				t.Errorf("the supervision migration touched a table it does not own: %s", line)
			}
		} else {
			t.Errorf("the supervision migration REMOVED schema shape: %s", line)
		}
	}
	if added < 6 {
		t.Errorf("only %d fingerprint lines were added; the six tables should add at least six", added)
	}
}

func TestSupervisionAppendOnlyTriggersRefuseUpdateAndDelete(t *testing.T) {
	g, s := testStore(t)
	if _, err := s.AppendAttentionAck("loop:l1", "ack", "", "user", "cj"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AppendDelivery(AppendDeliveryIn{ChannelID: "nc-x", Outcome: "sent", Attempts: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateChannel(CreateChannelIn{Name: "ops", URL: "https://hooks.example.test/h", ActorKind: "user", ActorID: "cj"}); err != nil {
		t.Fatal(err) // this also wrote a notify_change row
	}
	cases := []struct{ sql string }{
		{`UPDATE attention_ack SET op='unack'`},
		{`DELETE FROM attention_ack`},
		{`UPDATE notify_delivery SET outcome='sent'`},
		{`DELETE FROM notify_delivery`},
		{`UPDATE notify_change SET summary='quiet'`},
		{`DELETE FROM notify_change`},
	}
	for _, c := range cases {
		if err := g.Exec(c.sql).Error; err == nil {
			t.Errorf("%s succeeded — the append-only trigger is not holding", c.sql)
		} else if !strings.Contains(strings.ToLower(err.Error()), "append-only") {
			t.Errorf("%s failed for the wrong reason: %v", c.sql, err)
		}
	}
}

func TestChannelCheckRefusesNonUserCreator(t *testing.T) {
	// Lock 3 at the SQLite gate: the store door ALSO refuses (below in
	// TestChannelCreatedDisabled), but the CHECK is the one that holds
	// against raw SQL, and this test is the raw SQL.
	_, s := testStore(t)
	err := s.GormDB().Exec(`INSERT INTO notify_channel
		(id, kind, name, url, secret_ref, created_by_kind, created_by_id, created_at, updated_at)
		VALUES ('nc-a','webhook','x','https://x.test/h','secret:notify/nc-a','agent','a','t','t')`).Error
	if err == nil {
		t.Fatal("an agent-created channel row was accepted — lock 3 is only as deep as the store door")
	}
}

func TestChannelCreatedDisabledAndSecretIsOnlyARef(t *testing.T) {
	_, s := testStore(t)
	ch, err := s.CreateChannel(CreateChannelIn{
		Name: "ops", URL: "https://hooks.example.test/h", ActorKind: "user", ActorID: "cj"})
	if err != nil {
		t.Fatal(err)
	}
	if ch.Enabled {
		t.Error("a created channel must be born disabled (SV8): a channel speaks only after its test")
	}
	if want := "secret:notify/" + ch.ID; ch.SecretRef != want {
		t.Errorf("secret_ref = %q, want the pure reference %q", ch.SecretRef, want)
	}
	// No column of the row holds anything but the reference shape.
	var refs int
	if err := s.w.QueryRow(`SELECT COUNT(*) FROM notify_channel WHERE secret_ref=?`, ch.SecretRef).Scan(&refs); err != nil || refs != 1 {
		t.Fatalf("row not addressable by its ref: %v (%d)", err, refs)
	}
	if _, err := s.CreateChannel(CreateChannelIn{
		Name: "sneaky", URL: "https://hooks.example.test/h", ActorKind: "cli", ActorID: "cj"}); !errors.Is(err, ErrNotAPerson) {
		t.Errorf("a cli-created channel at the store door: err = %v, want ErrNotAPerson", err)
	}
}

func TestSubscriptionQuietHoursAllOrNone(t *testing.T) {
	_, s := testStore(t)
	ch, err := s.CreateChannel(CreateChannelIn{Name: "ops", URL: "https://hooks.example.test/h", ActorKind: "user", ActorID: "cj"})
	if err != nil {
		t.Fatal(err)
	}
	// The store door refuses first, with a sentence:
	if _, err := s.SetSubscription(SetSubscriptionIn{
		ChannelID: ch.ID, MinSeverity: "warning", QuietStart: "22:00", ActorKind: "user", ActorID: "cj"}); err == nil ||
		!strings.Contains(err.Error(), "quiet") {
		t.Fatalf("a half-set quiet window at the door: %v", err)
	}
	// And the CHECK holds the gate against raw SQL:
	err = s.GormDB().Exec(`INSERT INTO notify_subscription (channel_id, root_id, min_severity, quiet_start)
		VALUES (?, '', 'warning', '22:00')`, ch.ID).Error
	if err == nil {
		t.Fatal("the CHECK accepted a half-set quiet window")
	}
	// All three lands.
	if _, err := s.SetSubscription(SetSubscriptionIn{
		ChannelID: ch.ID, MinSeverity: "warning", QuietStart: "22:00", QuietEnd: "07:00", QuietZone: "Europe/Lisbon",
		ActorKind: "user", ActorID: "cj"}); err != nil {
		t.Fatalf("the full window: %v", err)
	}
}

func TestStoreChannelHostsOnlyEnabledNonArchived(t *testing.T) {
	_, s := testStore(t)
	mk := func(id, url string) string {
		ch, err := s.CreateChannel(CreateChannelIn{Name: id, URL: url, ActorKind: "user", ActorID: "cj"})
		if err != nil {
			t.Fatalf("%s: %v", id, err)
		}
		return ch.ID
	}
	off := mk("off", "https://off.example.test/h")
	live := mk("live", "https://live.example.test:8443/h")
	gone := mk("gone", "https://gone.example.test/h")
	if _, err := s.SetChannelEnabled(live, true, "user", "cj"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetChannelEnabled(gone, true, "user", "cj"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ArchiveChannel(gone, "superseded", "user", "cj"); err != nil {
		t.Fatal(err)
	}
	hosts, err := s.ChannelHosts()
	if err != nil {
		t.Fatal(err)
	}
	if len(hosts) != 1 || hosts[0] != "live.example.test" {
		t.Errorf("ChannelHosts = %v, want exactly the enabled non-archived host (a disabled or archived host on the allowlist is the SV7 hole)", hosts)
	}
	if hosts2, _ := s.ListChannels(false); len(hosts2) != 2 {
		t.Errorf("live channel listing = %d, want the disabled and archived ones still listed for the person", len(hosts2))
	}
	_ = off
}

func TestNoUpdateOrDeleteOfAppendOnlyRowsInGo(t *testing.T) {
	// The grep lock: no accessor in the two supervision files mutates or
	// deletes an append-only row. The positive control (a pattern that
	// DOES exist, UPDATE notify_channel) proves the scanner reads what it
	// claims.
	mustNot := []string{"UPDATE attention_ack", "DELETE FROM attention_ack",
		"UPDATE notify_delivery", "DELETE FROM notify_delivery",
		"UPDATE notify_change", "DELETE FROM notify_change"}
	must := []string{"UPDATE notify_channel", "UPDATE notify_event_class", "INSERT INTO attention_ack"}
	for _, file := range []string{"attention.go", "notify.go"} {
		src := readOwnSource(t, file)
		for _, bad := range mustNot {
			if strings.Contains(src, bad) {
				t.Errorf("%s contains %q — an append-only table has a mutating accessor", file, bad)
			}
		}
		for _, good := range must {
			if file == "attention.go" && !strings.Contains(good, "attention_ack") {
				continue
			}
			if file == "notify.go" && strings.Contains(good, "attention_ack") {
				continue
			}
			if !strings.Contains(src, good) {
				t.Errorf("%s no longer contains %q — the positive control of this test broke (did the file stop doing what this test assumes?)", file, good)
			}
		}
	}
}

func TestNotifyClassToneCheckAndHumanUpdate(t *testing.T) {
	_, s := testStore(t)
	// The four tones the CHECK admits are exactly protocol.NotifyTone
	// (types.go:156-164) and the app's NotifyTone (ui/src/lib/
	// notifications.ts:5) — the mirror this lock guards.
	for _, tone := range []string{"info", "success", "warning", "danger"} {
		tx, err := s.w.Begin()
		if err != nil {
			t.Fatal(err)
		}
		err = s.UpsertNotifyClassTx(tx, NotifyClass{
			Kind: "loop." + tone, Tone: tone, TitleTemplate: "t", Enabled: true,
			Origin: "system", Version: 1, UpdatedAt: s.Now()})
		if err != nil {
			t.Fatalf("tone %q: %v", tone, err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
	}
	tx, _ := s.w.Begin()
	err := s.UpsertNotifyClassTx(tx, NotifyClass{Kind: "loop.dread", Tone: "urgent", TitleTemplate: "t", UpdatedAt: s.Now()})
	tx.Rollback()
	if err == nil {
		t.Fatal("a tone outside the four was accepted by the CHECK")
	}
	// The human update: tone flip bumps the version and audits once.
	newTone := "danger"
	row, err := s.UpdateNotifyClass("loop.info", "", &newTone, nil, nil, nil, "user", "cj")
	if err != nil {
		t.Fatal(err)
	}
	if row.Tone != "danger" || row.Version != 2 {
		t.Errorf("update landed %+v", row)
	}
	var audits int
	if err := s.w.QueryRow(`SELECT COUNT(*) FROM notify_change WHERE subject_kind='class' AND subject_id='loop.info|'`).Scan(&audits); err != nil || audits != 1 {
		t.Errorf("class audit rows = %d (%v), want 1", audits, err)
	}
	// A no-op update earns no audit row (LC11).
	if _, err := s.UpdateNotifyClass("loop.info", "", &newTone, nil, nil, nil, "user", "cj"); err != nil {
		t.Fatal(err)
	}
	if err := s.w.QueryRow(`SELECT COUNT(*) FROM notify_change WHERE subject_kind='class'`).Scan(&audits); err != nil || audits != 1 {
		t.Errorf("a no-op class update audited: %d rows", audits)
	}
}

// TestNotifyClassTemplateAuditNamesFieldsNeverBodies is SV12 at the row:
// a template edit is audited by FIELD NAME, and the template's own text —
// which an operator could fill with anything shaped like a secret — never
// enters the append-only history. It also locks GetNotifyClassTx (the
// seed applier's read) to the standalone read: one query, two entrances.
func TestNotifyClassTemplateAuditNamesFieldsNeverBodies(t *testing.T) {
	_, s := testStore(t)
	tx, err := s.w.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertNotifyClassTx(tx, NotifyClass{
		Kind: "loop.blocked", Tone: "warning", TitleTemplate: "Loop blocked",
		Enabled: true, Origin: "system", Version: 1, UpdatedAt: s.Now()}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	newTitle := "{loopSlug} blocked: password=hunter2"
	newBody := "the message said: hunter2"
	row, err := s.UpdateNotifyClass("loop.blocked", "", nil, nil, &newTitle, &newBody, "user", "cj")
	if err != nil {
		t.Fatal(err)
	}
	if row.TitleTemplate != newTitle || row.BodyTemplate != newBody || row.Version != 2 {
		t.Fatalf("template edit landed %+v", row)
	}
	var summary string
	var audits int
	if err := s.w.QueryRow(`SELECT COUNT(*) FROM notify_change WHERE subject_kind='class' AND subject_id='loop.blocked|'`).Scan(&audits); err != nil || audits != 1 {
		t.Fatalf("class audit rows = %d (%v), want 1", audits, err)
	}
	if err := s.w.QueryRow(`SELECT summary FROM notify_change WHERE subject_kind='class' AND subject_id='loop.blocked|'`).Scan(&summary); err != nil {
		t.Fatal(err)
	}
	if summary != "titleTemplate bodyTemplate" {
		t.Errorf("summary = %q, want the two field NAMES", summary)
	}
	if strings.Contains(summary, "hunter2") || strings.Contains(summary, "{loopSlug}") {
		t.Errorf("the audit carries template CONTENT: %q (SV12 names fields, never bodies)", summary)
	}
	// The same write again changes nothing — and writes no audit row (LC11).
	if _, err := s.UpdateNotifyClass("loop.blocked", "", nil, nil, &newTitle, &newBody, "user", "cj"); err != nil {
		t.Fatal(err)
	}
	if err := s.w.QueryRow(`SELECT COUNT(*) FROM notify_change WHERE subject_kind='class'`).Scan(&audits); err != nil || audits != 1 {
		t.Errorf("a no-op template update audited: %d rows", audits)
	}
	// GetNotifyClassTx answers exactly what the standalone read answers.
	ro, err := s.w.BeginTx(context.Background(), &sql.TxOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer ro.Rollback()
	viaTx, foundTx, err := s.GetNotifyClassTx(context.Background(), ro, "loop.blocked", "")
	if err != nil || !foundTx {
		t.Fatalf("GetNotifyClassTx: %+v %v (%v)", viaTx, foundTx, err)
	}
	direct, foundDirect, err := s.GetNotifyClass("loop.blocked", "")
	if err != nil {
		t.Fatal(err)
	}
	if viaTx != direct || foundTx != foundDirect {
		t.Errorf("GetNotifyClassTx %+v vs GetNotifyClass %+v — two reads of one row, two answers", viaTx, direct)
	}
	if _, found, _ := s.GetNotifyClassTx(context.Background(), ro, "no.kind", ""); found {
		t.Error("GetNotifyClassTx found a row that does not exist")
	}
}

func TestAttentionAckRoundTripLatestWins(t *testing.T) {
	_, s := testStore(t)
	if _, err := s.AppendAttentionAck("loop:l1", "ack", "", "user", "cj"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AppendAttentionAck("loop:l1", "snooze", "2026-11-04T00:00:00Z", "user", "cj"); err != nil {
		t.Fatal(err)
	}
	// The CHECK's Go twin refuses both wrong couplings:
	if _, err := s.AppendAttentionAck("loop:l2", "snooze", "", "user", "cj"); err == nil {
		t.Error("a snooze with no until was accepted")
	}
	if _, err := s.AppendAttentionAck("loop:l2", "ack", "2026-11-04T00:00:00Z", "user", "cj"); err == nil {
		t.Error("an ack with an until was accepted")
	}
	marks, err := s.LatestAcks([]string{"loop:l1", "loop:never"})
	if err != nil {
		t.Fatal(err)
	}
	m, ok := marks["loop:l1"]
	if !ok || m.Op != "snooze" || m.Until != "2026-11-04T00:00:00Z" {
		t.Errorf("latest mark = %+v (%v), want the snooze — the highest id must win", m, ok)
	}
	if _, ok := marks["loop:never"]; ok {
		t.Error("a subject with no rows must be absent, not zero-valued")
	}
	if _, err := s.AppendAttentionAck("loop:l1", "unack", "", "user", "cj"); err != nil {
		t.Fatal(err)
	}
	marks, _ = s.LatestAcks([]string{"loop:l1"})
	if marks["loop:l1"].Op != "unack" || marks["loop:l1"].Until != "" {
		t.Errorf("after unack: %+v", marks["loop:l1"])
	}
	empty, err := s.LatestAcks(nil)
	if err != nil || len(empty) != 0 {
		t.Errorf("empty key set: %v (%v)", empty, err)
	}
}

func TestSubscriptionUpsertAndRootScope(t *testing.T) {
	_, s := testStore(t)
	ch, err := s.CreateChannel(CreateChannelIn{Name: "ops", URL: "https://hooks.example.test/h", ActorKind: "user", ActorID: "cj"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetSubscription(SetSubscriptionIn{ChannelID: ch.ID, RootID: "", MinSeverity: "warning", ActorKind: "user", ActorID: "cj"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetSubscription(SetSubscriptionIn{ChannelID: ch.ID, RootID: "comp-1", MinSeverity: "info", MaxPerHour: 5, ActorKind: "user", ActorID: "cj"}); err != nil {
		t.Fatal(err)
	}
	// Upsert, not duplicate: same (channel, root) again with a new floor.
	if _, err := s.SetSubscription(SetSubscriptionIn{ChannelID: ch.ID, RootID: "comp-1", MinSeverity: "danger", ActorKind: "user", ActorID: "cj"}); err != nil {
		t.Fatal(err)
	}
	mine, err := s.ListSubscriptionsForRoot("comp-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(mine) != 2 {
		t.Fatalf("root comp-1 sees %d subscriptions, want its own + the every-roots row", len(mine))
	}
	for _, sub := range mine {
		if sub.RootID == "comp-1" && (sub.MinSeverity != "danger" || sub.MaxPerHour != 30) {
			t.Errorf("the upsert kept stale fields: %+v", sub)
		}
	}
	other, _ := s.ListSubscriptionsForRoot("comp-2")
	if len(other) != 1 || other[0].RootID != "" {
		t.Errorf("root comp-2 sees %+v, want only the every-roots row", other)
	}
	// A subscription to no-channel-at-all is refused, not dangling.
	if _, err := s.SetSubscription(SetSubscriptionIn{ChannelID: "nc-nope", MinSeverity: "info", ActorKind: "user", ActorID: "cj"}); !errors.Is(err, ErrNotFound) {
		t.Errorf("dangling subscription: %v", err)
	}
}

func TestChannelEnableArchiveAuditTrail(t *testing.T) {
	_, s := testStore(t)
	ch, err := s.CreateChannel(CreateChannelIn{Name: "ops", URL: "https://hooks.example.test/h", ActorKind: "user", ActorID: "cj"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetChannelEnabled(ch.ID, true, "user", "cj"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ArchiveChannel(ch.ID, "superseded by nc-2", "user", "cj"); err != nil {
		t.Fatal(err)
	}
	// An archived channel refuses to be enabled again — the archive is
	// terminal; a new channel is the way back.
	if _, err := s.SetChannelEnabled(ch.ID, true, "user", "cj"); !errors.Is(err, ErrNotFound) {
		t.Errorf("enabling an archived channel: %v, want ErrNotFound", err)
	}
	rows, err := s.ListChannels(false)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range rows {
		if c.ID == ch.ID {
			t.Error("an archived channel appeared in the non-archived listing")
		}
	}
	all, _ := s.ListChannels(true)
	if len(all) != 1 || all[0].ArchivedAt == "" || all[0].Enabled {
		t.Errorf("archived listing = %+v", all)
	}
	var ops []string
	iter, err := s.w.Query(`SELECT op FROM notify_change WHERE subject_kind='channel' AND subject_id=? ORDER BY id`, ch.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer iter.Close()
	for iter.Next() {
		var op string
		if err := iter.Scan(&op); err != nil {
			t.Fatal(err)
		}
		ops = append(ops, op)
	}
	if strings.Join(ops, ",") != "create,enable,archive" {
		t.Errorf("audit trail = %v, want create,enable,archive (every channel write audits in its own transaction)", ops)
	}
}

func TestDeliverySuppressionsRecordAndSendsCount(t *testing.T) {
	_, s := testStore(t)
	ch, err := s.CreateChannel(CreateChannelIn{Name: "ops", URL: "https://hooks.example.test/h", ActorKind: "user", ActorID: "cj"})
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range []AppendDeliveryIn{
		{ChannelID: ch.ID, Outcome: "sent", Attempts: 1, EventSeq: 11, AttentionKey: "loop:l1"},
		{ChannelID: ch.ID, Outcome: "failed", ErrorKind: "timeout", HTTPStatus: 0, Attempts: 3, EventSeq: 12},
		{ChannelID: ch.ID, Outcome: "suppressed-quiet", EventSeq: 13},
		{ChannelID: ch.ID, Outcome: "suppressed-rate", EventSeq: 14},
	} {
		if _, err := s.AppendDelivery(d); err != nil {
			t.Fatal(err)
		}
	}
	n, err := s.RecentDeliveryCount(ch.ID, "2000-01-01T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("send count = %d, want 2 (a suppression never used a send slot)", n)
	}
	future, _ := s.RecentDeliveryCount(ch.ID, "2999-01-01T00:00:00Z")
	if future != 0 {
		t.Errorf("window in the future counted %d", future)
	}
	list, err := s.ListDeliveries(ch.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 4 || list[0].Outcome != "suppressed-rate" || list[0].EventSeq != 14 {
		t.Errorf("deliveries = %+v, want newest first with the event seq kept", list)
	}
	if list[1].ErrorKind != "" {
		t.Errorf("suppressed-quiet row carries error_kind %q", list[1].ErrorKind)
	}
}
