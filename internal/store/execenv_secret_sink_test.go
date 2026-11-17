package store_test

// execenv_secret_sink_test.go — S59 T4's acceptance (the scan of every sink)
// and the lock that keeps the two halves of EV7 speaking one vocabulary.
//
// This file is an EXTERNAL test (package store_test) for a reason that will
// bite whoever moves it: `envtest` imports `store` to reach *store.Store, so an
// in-package test here would be an import cycle. The store's own helpers are
// reached through their exported doors instead, which is the same door the
// daemon uses.

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/execenv"
	"github.com/cjlapao/aeon/internal/execenv/envtest"
	"github.com/cjlapao/aeon/internal/store"
)

// The sentinel is the daemon-environment value a reference resolves to. It
// appears in this file exactly twice on purpose: once as the value the resolver
// is handed, and once in the vacuity check that proves the test could have
// failed.
const sinkSentinel = "s59-t4-daemon-env-value-9f3xQk2Lm7pRt4Vw8Zy"

func openSinkStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(store.Options{Dir: t.TempDir(), Env: func(string) string { return "" }})
	if err != nil {
		t.Fatalf("throwaway store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func TestSentinelNeverInAnySink(t *testing.T) {
	st := openSinkStore(t)
	actor := store.Actor{Kind: "user", ID: "carlos"}

	if _, _, err := st.CreateSecretRef(actor, store.NewSecretRef{
		Name: "app-token", Description: "the application's token", SourceKind: "daemon-env",
		Source: "APP_TOKEN", InjectAs: "APP_TOKEN",
	}, []string{"AEON_FORGE_TOKEN", "ANTHROPIC_API_KEY"}); err != nil {
		t.Fatalf("create the reference: %v", err)
	}
	if _, r, err := st.CreateEnvironment(actor, store.NewEnvironment{
		Slug: "sinks", Name: "Sinks", Backend: "host-scrubbed",
		Spec: `{"env":{"passthrough":["PATH"],"set":{},"secrets":["app-token"]},"home":"real",` +
			`"limits":{},"egress":{"mode":"unenforced"},"image":null,"services":[],"preview":null}`,
	}); r != nil || err != nil {
		t.Fatalf("create the environment: r=%v err=%v", r, err)
	}

	// Resolve the reference the way a spawn will (T5), then capture a command
	// that printed the value in three forms — the shape of the accident EV8 is
	// about, not a hypothetical: `curl -v`, a JSON error body, a debug dump.
	env := execenv.ResolveEnv{
		Lookup:      func(string) string { return sinkSentinel },
		DaemonUID:   execenv.CurrentUID(),
		Credentials: execenv.DaemonCredentials{Provider: []string{"ANTHROPIC_API_KEY"}, Forge: []string{"GH_TOKEN"}},
	}
	inj, ref := execenv.ResolveRef(execenv.RefSpec{
		Name: "app-token", SourceKind: "daemon-env", Source: "APP_TOKEN", InjectAs: "APP_TOKEN",
	}, env)
	if ref != nil {
		t.Fatalf("the reference did not resolve: %s", ref.Sentence())
	}
	red := execenv.NewRedactor([]execenv.Injection{inj})

	leaked := strings.Join([]string{
		"curl -H 'Authorization: Bearer " + sinkSentinel + "'",
		`{"error":"auth failed","token":"` + base64.StdEncoding.EncodeToString([]byte(sinkSentinel)) + `"}`,
		"dump: " + hex.EncodeToString([]byte(sinkSentinel)),
	}, "\n")

	// The vacuity check: had the redactor not run, every assertion below would
	// still have passed, which is the failure mode a sink scan has to defend
	// against in itself.
	if !strings.Contains(leaked, sinkSentinel) {
		t.Fatal("the fixture does not contain the sentinel; this test would prove nothing")
	}
	captured := red.RedactString(leaked)
	if strings.Contains(captured, sinkSentinel) {
		t.Fatal("the redactor let the value through, so the scan below is not testing what it claims")
	}

	// The sinks, as the step's life ends: the daemon log buffer, a response
	// body, and the step_environment row.
	// The daemon's log buffer: what a `daemon.log` reader sees. The redaction
	// has to happen BEFORE this line, which is what T5 wires into the capture.
	logBuf := &bytes.Buffer{}
	fmt.Fprintf(logBuf, "step 4001 · bash · output:\n%s\n", captured)

	if _, err := st.RecordStepEnvironment(store.NewStepEnvironment{
		StepID:     4001,
		VarNames:   []string{"PATH", "APP_TOKEN"},
		SecretRefs: []string{"app-token"},
	}); err != nil {
		t.Fatalf("record the step environment: %v", err)
	}
	rows, err := st.StepEnvironments(4001)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || strings.Join(rows[0].SecretRefs, ",") != "app-token" {
		t.Fatalf("the step record is not the names-only row EV9 describes: %+v", rows)
	}

	envtest.Scan(t, envtest.Sinks{
		Store:     st,
		LogBuf:    logBuf,
		Responses: [][]byte{[]byte(captured), []byte(ref.Sentence())},
	}, sinkSentinel)

	// And the redaction report itself, printed, must carry names only.
	for _, nc := range red.Redactions() {
		if strings.Contains(nc.Name, sinkSentinel) || nc.Count == 0 {
			t.Errorf("the redaction report is wrong or leaking: %+v", nc)
		}
	}
	if len(red.Redactions()) != 1 {
		t.Errorf("Redactions() = %+v, want one row for the one secret", red.Redactions())
	}
}

// TestStoreAndExecenvShareOneRefusalVocabulary locks the two halves of EV7
// together. The store refuses a bad reference at the write; execenv refuses the
// same shapes at the read. If the strings drift, a UI or CLI that branches on
// the kind silently stops branching, and the row that was refused at one door
// reads as a different problem at the other.
func TestStoreAndExecenvShareOneRefusalVocabulary(t *testing.T) {
	pins := []struct {
		storeKind string
		want      string
	}{
		{"daemon-credential", execenv.KindDaemonCredential},
		{"daemon-env-shape", execenv.KindDaemonEnvShape},
		{"key-file-relative", execenv.KindKeyFileRelative},
		{"key-file-traversal", execenv.KindKeyFileTraversal},
		{"store-source-shape", execenv.KindStoreSourceShape},
		{"source-kind-unknown", execenv.KindSourceKindUnknown},
		{"source-empty", execenv.KindSourceEmpty},
	}
	for _, p := range pins {
		if p.storeKind != p.want {
			t.Errorf("the store's write-time kind %q is not execenv's %q — one of the two doors invented "+
				"a vocabulary", p.storeKind, p.want)
		}
	}

	// The lock is not just text: fire the store's door and compare what it
	// actually answers with the constant execenv will answer with.
	st := openSinkStore(t)
	actor := store.Actor{Kind: "user", ID: "carlos"}
	cases := []struct {
		name, kind, source string
		want               string
	}{
		{"the daemon's provider key", "daemon-env", "ANTHROPIC_API_KEY", execenv.KindDaemonCredential},
		{"a variable name with a space", "daemon-env", "APP TOKEN", execenv.KindDaemonEnvShape},
		{"a relative key file", "key-file", "secrets/db.key", execenv.KindKeyFileRelative},
		{"a traversing key file", "key-file", "/etc/../home/x/db.key", execenv.KindKeyFileTraversal},
		{"a path for a store credential", "store", "../state.db", execenv.KindStoreSourceShape},
		{"an unknown kind", "vault", "whatever", execenv.KindSourceKindUnknown},
		{"no source at all", "key-file", "", execenv.KindSourceEmpty},
	}
	for _, tc := range cases {
		_, r, err := st.CreateSecretRef(actor, store.NewSecretRef{
			Name: "pin-" + strings.ReplaceAll(tc.want, "-", "_"), SourceKind: tc.kind,
			Source: tc.source, InjectAs: "APP_VAR",
		}, []string{"ANTHROPIC_API_KEY"})
		if err != nil {
			t.Fatalf("%s: a refusal must not be an error: %v", tc.name, err)
		}
		if r == nil {
			t.Fatalf("%s: the store accepted the row", tc.name)
		}
		if r.Kind != tc.want {
			t.Errorf("%s: the store answered %q, execenv answers %q (%s)", tc.name, r.Kind, tc.want, r.Error())
		}
	}
}
