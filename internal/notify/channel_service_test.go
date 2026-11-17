package notify

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/secrets"
	"github.com/cjlapao/aeon/internal/store"
)

// channel_service_test.go — S58 T5: the channel service and the secret
// file, over a real store. The claims here are the SV7 shape: born
// disabled, enable gated on a real secret, the secret file's own rules
// (mode, emptiness, size), and every actor that is not a person refused
// before SQL.

func openChannelStore(t *testing.T) *store.Store {
	st, _ := openChannelStoreIn(t, t.TempDir())
	return st
}

func openChannelStoreIn(t *testing.T, dir string) (*store.Store, string) {
	t.Helper()
	st, err := store.Open(store.Options{Dir: dir, Env: func(string) string { return "" },
		Clock: func() time.Time { return time.Date(2026, 11, 3, 9, 0, 0, 0, time.UTC) }})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st, dir
}

// isolateSecrets points the secrets package at a scratch state dir for
// the duration of the test (the same env the daemon resolves).
func isolateSecrets(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("AEON_STATE_DIR", dir)
	return dir
}

func TestChannelCreatedDisabledAndEnableChecksSecret(t *testing.T) {
	st := openChannelStore(t)
	isolateSecrets(t)
	svc := &ChannelService{Store: st, Secrets: FileSecrets{}}

	ch, err := svc.Create(ChannelArgs{Name: "ops", URL: "https://hooks.example.com/aeon",
		ActorKind: "user", ActorID: "carlos"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if ch.Enabled {
		t.Fatal("a channel was created ENABLED — SV7: born disabled")
	}
	if ch.SecretRef != "secret:notify/"+ch.ID {
		t.Errorf("secret_ref = %q — the row carries only a reference", ch.SecretRef)
	}

	// Enable without a secret: refused, and the row stands disabled.
	if _, err := svc.Enable(ch.ID, "user", "carlos"); err == nil {
		t.Fatal("enable succeeded with no secret stored")
	} else if !strings.Contains(err.Error(), "secret") {
		t.Errorf("enable refusal = %v, want the secret sentence", err)
	}
	if got, _, err := st.GetChannel(ch.ID); err != nil || got.Enabled {
		t.Fatalf("the refused enable still flipped the row: %v", got.Enabled)
	}

	// With the secret in place, enable passes.
	if err := secrets.SaveChannelSecret(ch.ID, "s3cret-value"); err != nil {
		t.Fatal(err)
	}
	row, err := svc.Enable(ch.ID, "user", "carlos")
	if err != nil {
		t.Fatalf("enable with a secret: %v", err)
	}
	if !row.Enabled {
		t.Fatal("enable with a secret left the channel disabled")
	}
	if !svc.SecretPresent(ch.SecretRef) {
		t.Error("SecretPresent says no after a save")
	}
}

func TestChannelValidateDoor(t *testing.T) {
	cases := []struct {
		name, url    string
		allowPrivate bool
		wantOK       bool
	}{
		{"good", "https://hooks.example.com/x", false, true},
		{"http refused", "http://hooks.example.com/x", false, false},
		{"http local without flag refused", "http://127.0.0.1:9/x", false, false},
		{"http local with flag", "http://127.0.0.1:9/x", true, true},
		{"http localhost with flag", "http://localhost:9/x", true, true},
		{"http PUBLIC host with flag refused", "http://hooks.example.com/x", true, false}, // flag alone is not enough
		{"userinfo refused", "https://user:pw@hooks.example.com/x", false, false},
		{"no host", "https:///x", false, false},
		{"not a url", "not a url at all", false, false},
	}
	for _, c := range cases {
		err := ValidateChannel(c.name, c.url, c.allowPrivate)
		if c.wantOK && err != nil {
			t.Errorf("%s: refused (%v)", c.name, err)
		}
		if !c.wantOK && err == nil {
			t.Errorf("%s: accepted — the door must refuse it", c.name)
		}
	}
	// Size caps.
	if err := ValidateChannel(strings.Repeat("n", MaxChannelNameBytes+1), "https://x.example.com", false); err == nil {
		t.Error("an over-cap name was accepted")
	}
	if err := ValidateChannel("n", "https://x.example.com/"+strings.Repeat("p", MaxChannelURLBytes), false); err == nil {
		t.Error("an over-cap url was accepted")
	}
}

func TestChannelWritesRefuseNonPersons(t *testing.T) {
	st := openChannelStore(t)
	isolateSecrets(t)
	svc := &ChannelService{Store: st, Secrets: FileSecrets{}}
	for _, kind := range []string{"agent", ""} {
		if _, err := svc.Create(ChannelArgs{Name: "n", URL: "https://x.example.com",
			ActorKind: kind, ActorID: "whoever"}); err == nil {
			t.Errorf("Create accepted actor kind %q", kind)
		}
	}
}

func TestChannelArchiveIsRetirementNotDeletion(t *testing.T) {
	st := openChannelStore(t)
	isolateSecrets(t)
	svc := &ChannelService{Store: st, Secrets: FileSecrets{}}
	ch, err := svc.Create(ChannelArgs{Name: "ops", URL: "https://hooks.example.com/aeon", ActorKind: "user", ActorID: "c"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Archive(ch.ID, "", "user", "c"); err == nil {
		t.Error("archive without a reason was accepted — the row outlives the memory")
	}
	row, err := svc.Archive(ch.ID, "moved", "user", "c")
	if err != nil {
		t.Fatal(err)
	}
	if row.Enabled || row.ArchivedAt == "" || row.ArchiveReason != "moved" {
		t.Fatalf("archive shape wrong: %+v", row)
	}
	if _, err := svc.Enable(ch.ID, "user", "c"); err == nil {
		t.Error("an archived channel was enabled — archived is retired")
	}
	// The row still EXISTS (never deleted): the log has no FK by design.
	if _, found, err := st.GetChannel(ch.ID); err != nil || !found {
		t.Fatal("the archived channel's row was gone — archive never deletes")
	}
	if _, err := svc.Disable(ch.ID, "user", "c"); err == nil {
		t.Error("disable after archive accepted — retired is retired")
	}
}

func TestSecretFileModeAndEmptyRefused(t *testing.T) {
	dir := isolateSecrets(t)
	// Save writes 0600.
	if err := secrets.SaveChannelSecret("nc-a", "value"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "credentials", "notify-nc-a.key")
	fi, err := os.Stat(path)
	if err != nil {
		// The directory name is the secrets package's own — locate it if moved.
		entries, _ := os.ReadDir(filepath.Join(dir))
		t.Fatalf("secret file not where the package puts it (dir %v): %v", entries, err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("secret mode %v, want 0600", fi.Mode().Perm())
	}
	// Empty save refused rather than silently "keeping" the old value.
	if err := secrets.SaveChannelSecret("nc-b", ""); err == nil {
		t.Error("an empty secret was saved — there is no keep-existing reading here")
	}
	// ReadSecretFile: a group-readable file is refused with the chmod sentence.
	bad := filepath.Join(t.TempDir(), "k.key")
	if err := os.WriteFile(bad, []byte("value\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadSecretFile(bad); err == nil || !strings.Contains(err.Error(), "0") {
		t.Errorf("group-readable secret file accepted: %v", err)
	}
	if _, err := ReadSecretFile(bad); err == nil || !strings.Contains(err.Error(), "chmod") {
		t.Errorf("refusal must name the fix (chmod): %v", err)
	}
	// Empty file refused; missing file is absence not error via the store read.
	empty := filepath.Join(t.TempDir(), "e.key")
	if err := os.WriteFile(empty, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadSecretFile(empty); err == nil {
		t.Error("an empty secret file was accepted")
	}
	big := filepath.Join(t.TempDir(), "b.key")
	if err := os.WriteFile(big, []byte(strings.Repeat("a", 5000)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadSecretFile(big); err == nil {
		t.Error("a 5 KB secret was accepted — the cap exists so a log file becomes a key")
	}
	// One trailing newline is trimmed, a second is KEPT (it is content).
	nl := filepath.Join(t.TempDir(), "n.key")
	if err := os.WriteFile(nl, []byte("v\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := ReadSecretFile(nl)
	if err != nil || string(s.Bytes()) != "v" {
		t.Errorf("trailing newline trim: %q err %v", s.Bytes(), err)
	}
}

func TestSecretNeverAppearsAnywhere(t *testing.T) {
	// The Secret holder answers every rendering path with the redaction,
	// and JSON REFUSES rather than emitting the bytes: a struct that
	// embeds a Secret cannot accidentally serialise one.
	s, err := ReadSecretFile(writeScratchSecret(t))
	if err != nil {
		t.Fatal(err)
	}
	const raw = "SUP3R-SEKRIT-do-not-log"
	if string(s.Bytes()) != raw {
		t.Fatal("Bytes() must give the real thing to the signer")
	}
	renderings := map[string]string{
		"%s":  s.String(),
		"%v":  s.String(),
		"%#v": s.GoString(),
		"json-embedded": mustJSON(t, struct {
			S Secret `json:"s"`
		}{s}),
	}
	for path, out := range renderings {
		if strings.Contains(out, raw) {
			t.Errorf("secret bytes appear via the %s rendering path", path)
		}
	}
	if _, jerr := json.Marshal(s); jerr == nil {
		t.Error("marshalling a Secret succeeded — it must refuse (there is no correct output)")
	}
}

func writeScratchSecret(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "k.key")
	if err := os.WriteFile(p, []byte("SUP3R-SEKRIT-do-not-log\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		return "REFUSED:" + err.Error()
	}
	return string(b)
}

func TestSubscriptionValidationVocabularies(t *testing.T) {
	cases := []struct {
		desc string
		in   store.SetSubscriptionIn
		ok   bool
	}{
		{"empty severity default", store.SetSubscriptionIn{ChannelID: "c"}, true},
		{"bad severity", store.SetSubscriptionIn{ChannelID: "c", MinSeverity: "urgent"}, false},
		{"kinds not json", store.SetSubscriptionIn{ChannelID: "c", Kinds: "loop"}, false},
		{"empty kinds list", store.SetSubscriptionIn{ChannelID: "c", Kinds: "[]"}, false},
		{"unknown kind", store.SetSubscriptionIn{ChannelID: "c", Kinds: `["nope.happened"]`}, false},
		{"known kind + digest", store.SetSubscriptionIn{ChannelID: "c", Kinds: `["loop.finished","inbox.digest"]`}, true},
		{"rate 1 refused", store.SetSubscriptionIn{ChannelID: "c", MaxPerHour: 1}, false},
		{"rate 2 ok", store.SetSubscriptionIn{ChannelID: "c", MaxPerHour: 2}, true},
		{"rate 1001 refused", store.SetSubscriptionIn{ChannelID: "c", MaxPerHour: 1001}, false},
		{"start without end", store.SetSubscriptionIn{ChannelID: "c", QuietStart: "22:00", QuietZone: "Europe/Lisbon"}, false},
		{"window without zone", store.SetSubscriptionIn{ChannelID: "c", QuietStart: "22:00", QuietEnd: "07:00"}, false},
		{"zone without window", store.SetSubscriptionIn{ChannelID: "c", QuietZone: "Europe/Lisbon"}, false},
		{"bad clock", store.SetSubscriptionIn{ChannelID: "c", QuietStart: "25:00", QuietEnd: "07:00", QuietZone: "Europe/Lisbon"}, false},
		{"zone not IANA", store.SetSubscriptionIn{ChannelID: "c", QuietStart: "22:00", QuietEnd: "07:00", QuietZone: "Not/AZone"}, false},
		{"good window", store.SetSubscriptionIn{ChannelID: "c", QuietStart: "22:00", QuietEnd: "07:00", QuietZone: "Europe/Lisbon"}, true},
	}
	for _, c := range cases {
		err := ValidateSubscription(c.in)
		if c.ok && err != nil {
			t.Errorf("%s: refused (%v)", c.desc, err)
		}
		if !c.ok && err == nil {
			t.Errorf("%s: accepted", c.desc)
		}
	}
}
