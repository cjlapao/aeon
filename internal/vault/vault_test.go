package vault

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/store"
)

var person = store.Actor{Kind: "user", ID: "1000"}

func s(v string) *string { return &v }
func b(v bool) *bool     { return &v }

func newVault(t *testing.T) (*Vault, *store.Store) {
	t.Helper()
	st, err := store.Open(store.Options{Dir: t.TempDir(), Env: func(string) string { return "" }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	v, err := Open(st, filepath.Join(t.TempDir(), "state", "vault.key"))
	if err != nil {
		t.Fatal(err)
	}
	return v, st
}

func root(t *testing.T, st *store.Store, name string) string {
	t.Helper()
	c, err := st.CreateComponent(store.NewComponent{Name: name, Slug: name, Root: "/repo/" + name})
	if err != nil || !c.OK() {
		t.Fatalf("root %s: %v", name, err)
	}
	return c.Component.ID
}

func TestAValueIsStoredAsCiphertextAndASecretNeverComesBack(t *testing.T) {
	v, st := newVault(t)
	ctx := context.Background()
	if _, err := v.Set(ctx, SetInput{Name: "GITHUB_TOKEN", Value: s("ghp_supersecret123")}, person); err != nil {
		t.Fatal(err)
	}
	row, ok, err := st.EnvVarGet("GITHUB_TOKEN", "")
	if err != nil || !ok {
		t.Fatal(err)
	}
	if strings.Contains(string(row.Cipher), "ghp_supersecret123") {
		t.Fatal("the database holds the value in the clear")
	}
	list, _ := v.List(ctx)
	if len(list) != 1 || list[0].Value != "" || !list[0].Secret {
		t.Fatalf("a secret reads without its value: %+v", list)
	}
	if got, ok := v.Resolve("GITHUB_TOKEN", ""); !ok || got != "ghp_supersecret123" {
		t.Fatalf("the daemon resolves it: %q %v", got, ok)
	}
	// A plain variable is shown back.
	if _, err := v.Set(ctx, SetInput{Name: "REGION", Value: s("eu-west-1"), Secret: b(false)}, person); err != nil {
		t.Fatal(err)
	}
	list, _ = v.List(ctx)
	for _, x := range list {
		if x.Name == "REGION" && x.Value != "eu-west-1" {
			t.Fatalf("a plain variable shows its value: %+v", x)
		}
	}
}

func TestACiphertextCopiedUnderAnotherNameDoesNotDecrypt(t *testing.T) {
	v, st := newVault(t)
	if _, err := v.Set(context.Background(), SetInput{Name: "A_KEY", Value: s("alpha")}, person); err != nil {
		t.Fatal(err)
	}
	if _, err := v.Set(context.Background(), SetInput{Name: "B_KEY", Value: s("beta")}, person); err != nil {
		t.Fatal(err)
	}
	a, _, _ := st.EnvVarGet("A_KEY", "")
	// Plant A's ciphertext into B's row: a row moved under another name is not trusted.
	if _, err := st.EnvVarUpsert(context.Background(), store.EnvVarRow{Name: "B_KEY", Secret: true, Cipher: a.Cipher}, person, "tamper"); err != nil {
		t.Fatal(err)
	}
	if got, ok := v.Resolve("B_KEY", ""); ok {
		t.Fatalf("a swapped ciphertext decrypted to %q", got)
	}
}

func TestAgentsNeverSeeAVariableUnlessAPersonSaidSo(t *testing.T) {
	v, st := newVault(t)
	ctx := context.Background()
	agent := store.Actor{Kind: "agent", ID: "legal-advisor"}
	// New secret: closed. New plain variable: open. Both only until the person says otherwise.
	v.Set(ctx, SetInput{Name: "DB_PASSWORD", Value: s("hunter2")}, person)
	v.Set(ctx, SetInput{Name: "REGION", Value: s("eu"), Secret: b(false)}, person)
	v.Set(ctx, SetInput{Name: "STRIPE_KEY", Value: s("sk_live_x"), AgentVisible: b(true)}, person) // a secret, explicitly shared

	list, _ := v.List(ctx)
	vis := map[string]bool{}
	for _, x := range list {
		vis[x.Name] = x.AgentVisible
	}
	if vis["DB_PASSWORD"] || !vis["REGION"] || !vis["STRIPE_KEY"] {
		t.Fatalf("defaults: %+v", vis)
	}

	// Hidden and absent answer the SAME sentence — no oracle for what exists.
	_, hidden := v.AgentGet(ctx, "DB_PASSWORD", "", agent)
	_, absent := v.AgentGet(ctx, "NOPE", "", agent)
	if hidden == "" || hidden != strings.Replace(absent, "NOPE", "DB_PASSWORD", 1) {
		t.Fatalf("hidden %q vs absent %q", hidden, absent)
	}
	if val, refusal := v.AgentGet(ctx, "STRIPE_KEY", "", agent); refusal != "" || val != "sk_live_x" {
		t.Fatalf("a shared secret reads: %q %q", val, refusal)
	}
	// The read is on the ledger, naming the agent and never the value.
	ch, _ := st.EnvVarChanges("STRIPE_KEY", 10)
	if len(ch) == 0 || ch[0].Op != "agent_read" || ch[0].ActorID != "legal-advisor" || strings.Contains(ch[0].Summary, "sk_live") {
		t.Fatalf("ledger: %+v", ch)
	}
	names, _ := v.AgentList("")
	if len(names) != 2 || names[0].Name != "REGION" || names[1].Name != "STRIPE_KEY" || !names[1].Secret {
		t.Fatalf("an agent is told only what is visible: %+v", names)
	}
}

func TestAProjectOverrideWinsAndCanHideWhatTheGlobalShows(t *testing.T) {
	v, st := newVault(t)
	ctx := context.Background()
	alpha, beta := root(t, st, "alpha"), root(t, st, "beta")
	v.Set(ctx, SetInput{Name: "API_URL", Value: s("https://global"), Secret: b(false)}, person)
	v.Set(ctx, SetInput{Name: "API_URL", Project: alpha, Value: s("https://alpha"), Secret: b(false), AgentVisible: b(false)}, person)

	if got, _ := v.Resolve("API_URL", alpha); got != "https://alpha" {
		t.Fatalf("alpha's own value wins: %q", got)
	}
	if got, _ := v.Resolve("API_URL", beta); got != "https://global" {
		t.Fatalf("beta falls back to the global: %q", got)
	}
	// The override says agents may not see it, so the global being visible does not leak it into alpha.
	if _, refusal := v.AgentGet(ctx, "API_URL", alpha, person); refusal == "" {
		t.Fatal("an override that hides a variable hides it")
	}
	if val, refusal := v.AgentGet(ctx, "API_URL", beta, store.Actor{Kind: "agent", ID: "x"}); refusal != "" || val != "https://global" {
		t.Fatalf("beta's agents get the global: %q %q", val, refusal)
	}
	// An override hangs on a ROOT component.
	child, err := st.CreateComponent(store.NewComponent{Name: "kid", Slug: "kid", Root: "/repo/kid", ParentID: alpha})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := v.Set(ctx, SetInput{Name: "X", Project: child.Component.ID, Value: s("v")}, person); err == nil || !strings.Contains(err.Error(), "ROOT component") {
		t.Fatalf("a child is not a project: %v", err)
	}
}

func TestNamesAndValuesAreHeldToTheirRules(t *testing.T) {
	v, _ := newVault(t)
	ctx := context.Background()
	for _, bad := range []string{"1BAD", "has space", "a-b", "", "AEON_KEY", "aeon_x", "PATH", "ld_preload", "NODE_OPTIONS"} {
		if _, err := v.Set(ctx, SetInput{Name: bad, Value: s("x")}, person); err == nil {
			t.Fatalf("%q must be refused", bad)
		}
	}
	if _, err := v.Set(ctx, SetInput{Name: "OK", Value: s("")}, person); err == nil {
		t.Fatal("an empty value is refused")
	}
	if _, err := v.Set(ctx, SetInput{Name: "BIG", Value: s(strings.Repeat("x", MaxValueBytes+1))}, person); err == nil {
		t.Fatal("an oversized value is refused")
	}
	if _, err := v.Set(ctx, SetInput{Name: "NOVALUE"}, person); err == nil || !strings.Contains(err.Error(), "needs one") {
		t.Fatalf("a new variable needs a value: %v", err)
	}
}

func TestAFlagsOnlyEditKeepsTheValueAndEveryChangeIsOnTheLedger(t *testing.T) {
	v, st := newVault(t)
	ctx := context.Background()
	v.Set(ctx, SetInput{Name: "TOKEN", Value: s("keepme")}, person)
	if _, err := v.Set(ctx, SetInput{Name: "TOKEN", Description: s("the CI token"), AgentVisible: b(true)}, person); err != nil {
		t.Fatal(err)
	}
	if got, _ := v.Resolve("TOKEN", ""); got != "keepme" {
		t.Fatalf("the value survived a flags edit: %q", got)
	}
	if ok, err := v.Delete(ctx, "TOKEN", "", person); err != nil || !ok {
		t.Fatalf("delete: %v %v", ok, err)
	}
	if _, ok := v.Resolve("TOKEN", ""); ok {
		t.Fatal("deleted")
	}
	ch, _ := st.EnvVarChanges("TOKEN", 10)
	if len(ch) != 3 || ch[0].Op != "delete" || ch[2].Op != "set" {
		t.Fatalf("ledger: %+v", ch)
	}
	for _, c := range ch {
		if strings.Contains(c.Summary, "keepme") {
			t.Fatal("a value rode the ledger")
		}
	}
}

func TestTheKeyFileIsCreatedPrivateReusedAndNeverReplaced(t *testing.T) {
	st, _ := store.Open(store.Options{Dir: t.TempDir(), Env: func(string) string { return "" }})
	t.Cleanup(func() { _ = st.Close() })
	path := filepath.Join(t.TempDir(), "d", "vault.key")
	v1, err := Open(st, path)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("key file mode: %v %v", info, err)
	}
	if _, err := v1.Set(context.Background(), SetInput{Name: "K", Value: s("v")}, person); err != nil {
		t.Fatal(err)
	}
	v2, err := Open(st, path) // a restart reads the same key
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := v2.Resolve("K", ""); !ok || got != "v" {
		t.Fatalf("a restart reads what was stored: %q %v", got, ok)
	}
	// A damaged key file is refused, not regenerated.
	if err := os.WriteFile(path, []byte("short"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(st, path); err == nil || !strings.Contains(err.Error(), "not replaced") {
		t.Fatalf("damaged key: %v", err)
	}
	if b, _ := os.ReadFile(path); string(b) != "short" {
		t.Fatal("the damaged key file was overwritten")
	}
}
