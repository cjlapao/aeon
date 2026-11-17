// Package plugin is S15: agents we did not write. The three-step path
// (import → check → enable) lives here, and the first property of the path
// is the one this file pins: NOTHING ARRIVES ENABLED (AP2).
package plugin

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/handler"
)

// The fixture's card path uses the TEST loader — the loader is an injected
// seam (S14's absent-layer sweep forbids this package from importing the
// agent-services layer at all, test binaries included, so the OFF claim
// stays true). The real daemon wires agentsvc.LoadCardFile at the entry
// point (T7 pins the one call); the test loader verifies the same two
// things the real one does — signature over the stored bytes, then the
// name claim — and NOTHING else, so fixtures cannot lean on leniency the
// production verifier would not show.
func testCardLoader(path, expectedAgent string) (*CardInfo, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("card for %q is refused: %w", expectedAgent, err)
	}
	var env map[string]json.RawMessage
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("card for %q is refused: the envelope is not valid JSON (%v)", expectedAgent, err)
	}
	var pubB64, sigB64 string
	if err := json.Unmarshal(env["publicKey"], &pubB64); err != nil {
		return nil, fmt.Errorf("card for %q is refused: it has no usable public key — unsigned is not a valid state", expectedAgent)
	}
	if err := json.Unmarshal(env["signature"], &sigB64); err != nil {
		return nil, fmt.Errorf("card for %q is refused: it has no signature — unsigned is not a valid state", expectedAgent)
	}
	pub, err := base64.StdEncoding.DecodeString(pubB64)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("card for %q is refused: its public key is not an ed25519 key", expectedAgent)
	}
	sig, err := base64.StdEncoding.DecodeString(sigB64)
	if err != nil || len(sig) != ed25519.SignatureSize {
		return nil, fmt.Errorf("card for %q is refused: its signature is not an ed25519 signature", expectedAgent)
	}
	card := env["card"]
	if !ed25519.Verify(ed25519.PublicKey(pub), card, sig) {
		return nil, fmt.Errorf("card for %q is refused: signature does not verify — the card was tampered with or is not from the key it claims", expectedAgent)
	}
	var c struct {
		Name   string `json:"name"`
		Skills []struct {
			ID          string `json:"id"`
			Name        string `json:"name"`
			Description string `json:"description"`
		} `json:"skills"`
	}
	if err := json.Unmarshal(card, &c); err != nil {
		return nil, fmt.Errorf("card for %q is refused: it is not valid AgentCard JSON (%v)", expectedAgent, err)
	}
	if c.Name != expectedAgent {
		return nil, fmt.Errorf("card for %q is refused: it claims to be %q — a card cannot be filed under one agent and name another", expectedAgent, c.Name)
	}
	info := &CardInfo{Name: c.Name, Skills: make([]CardSkill, 0, len(c.Skills))}
	for _, sk := range c.Skills {
		if strings.TrimSpace(sk.ID) == "" || strings.TrimSpace(sk.Name) == "" || strings.TrimSpace(sk.Description) == "" {
			return nil, fmt.Errorf("card for %q is refused: skill %q is not addressable (A2A addresses a skill by id within its card)", expectedAgent, sk.Name)
		}
		info.Skills = append(info.Skills, CardSkill{ID: sk.ID, Name: sk.Name, Description: sk.Description})
	}
	return info, nil
}

// --- fixture machinery -------------------------------------------------------
//
// A package is a directory with an ALLOW-LISTED file set — agent.md (front
// matter + prompt), card.json (S14's SignedCard envelope), optional
// policies/*.json. Anything else is refused, which is the honest form of
// "no executable code": the format admits nothing unnamed. Cards are signed
// deterministically (seed = sha256(agent)) because a fixture's signature is
// part of the fixture.

func writePackage(t *testing.T, name string, manifest string, cardBody map[string]any, extra map[string][]byte) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if manifest == "" {
		manifest = fmt.Sprintf("---\nname: %s\ndescription: a fixture agent we did not write\nrequests:\n  tools: [webfetch]\n  skills: [%s/do-thing]\n---\nYou are %s. Answer briefly.\n", name, name, name)
	}
	if err := os.WriteFile(filepath.Join(dir, "agent.md"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if cardBody == nil {
		cardBody = map[string]any{
			"name": name, "description": "fixture card", "version": "1.0.0",
			"protocolVersion": "0.3", "url": "local",
			"defaultInputModes": []string{"text/plain"}, "defaultOutputModes": []string{"text/plain"},
			"skills": []map[string]any{{"id": "do-thing", "name": "Do the thing", "description": "does it", "inputModes": []string{"text/plain"}, "outputModes": []string{"text/plain"}}},
		}
	}
	body, err := json.Marshal(cardBody)
	if err != nil {
		t.Fatal(err)
	}
	seed := sha256.Sum256([]byte(name))
	priv := ed25519.NewKeyFromSeed(seed[:])
	sig := ed25519.Sign(priv, body)
	env, err := json.Marshal(map[string]any{
		"card":      json.RawMessage(body),
		"publicKey": base64.StdEncoding.EncodeToString(priv.Public().(ed25519.PublicKey)),
		"signature": base64.StdEncoding.EncodeToString(sig),
	})
	if err != nil {
		t.Fatal(err)
	}
	SetCardLoader(testCardLoader)
	t.Cleanup(func() { SetCardLoader(nil) })
	if err := os.WriteFile(filepath.Join(dir, "card.json"), env, 0o644); err != nil {
		t.Fatal(err)
	}
	for rel, content := range extra {
		full := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, content, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func mustImport(t *testing.T, dir string) *Agent {
	t.Helper()
	a, rj := Import(dir)
	if rj != nil {
		t.Fatalf("import refused: %s", rj.Error())
	}
	return a
}

// --- the property AP2 is made of ----------------------------------------------

func TestImportLeavesAgentDisabled(t *testing.T) {
	// Asserted IMMEDIATELY after import, before anything else touches the
	// record: the first readable fact about an arriving agent is that it is
	// not running.
	dir := writePackage(t, "groot-translator", "", nil, nil)
	a := mustImport(t, dir)
	if a.State != StateDisabled {
		t.Fatalf(`the first fact about an arriving agent is its state; got %q want %q`, a.State, StateDisabled)
	}
	if a.Name != "groot-translator" || a.FirstParty {
		t.Fatalf("provenance is wrong on arrival: %+v", a)
	}
	if a.Card == nil || len(a.Card.Skills) != 1 {
		t.Fatalf("the verified card is the load path's answer; got %+v", a.Card)
	}
	if len(a.Requests.Tools) != 1 || a.Requests.Tools[0] != "webfetch" {
		t.Fatalf("requests parse as requests: %+v", a.Requests)
	}
}

func TestEnabledUnreachableFromImport(t *testing.T) {
	// AP2 as a shape, not a rule: the import path cannot NAME the running
	// state. Banned words composed so this file cannot prosecute itself.
	banned := "en" + "abled"
	src, err := os.ReadFile("import.go")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(src), banned) {
		t.Fatalf("import.go mentions the running state at all — an import that can say it can mean it: %q", banned)
	}
	dir := writePackage(t, "quiet-agent", "", nil, nil)
	a := mustImport(t, dir)
	if a.State != StateDisabled {
		t.Fatalf("got %q", a.State)
	}
}

func TestImportedAgentRegisteredBesideThe25(t *testing.T) {
	// AP1: the same registry view, the same dispatch path — the difference
	// is provenance and labelling, not location (Q4).
	before := handler.RegisteredBodies()
	dir := writePackage(t, "beside-agent", "", nil, nil)
	mustImport(t, dir)
	agents := Agents()
	var sawFirst, sawImported bool
	for _, e := range agents {
		if e.Name == "golang-engineer" && e.FirstParty {
			sawFirst = true
		}
		if e.Name == "beside-agent" && !e.FirstParty {
			sawImported = true
		}
	}
	if !sawFirst || !sawImported {
		t.Fatalf("one registry beside the 25: first=%v imported=%v (%d entries)", sawFirst, sawImported, len(agents))
	}
	// Re-measured 2026-10-26 (S17 follow-up pass): the floor the imported
	// body joins is the full shipped set — 25 authored at S13 plus the five
	// follow-ups. The sentence's meaning stands: an import lands BESIDE a
	// complete registry, and the number is read from the registry, not memory.
	// 2026-10-27 (S38 T10): + the brainstormer — Count 30 -> 31 DELIBERATE.
	// 2026-10-28 (S39 T6): + the chat body — Count 31 -> 32 DELIBERATE.
	// 2026-10-29 (S24 T5): + the sizing-estimator — Count 32 -> 33 DELIBERATE.
	if len(before) != 33 {
		t.Fatalf("the shipped set is the floor this registry joins: %d", len(before))
	}
	// The same dispatch path: an imported body builds through the SAME
	// primitive handler.Build builds on (runtime.DefaultHandler) — not a
	// second runner nobody tested.
	step, ok := Dispatch("beside-agent")
	if !ok || step.Name != "beside-agent" {
		t.Fatalf("AP1: an imported agent dispatches through the same step-handler primitive; got ok=%v name=%q", ok, step.Name)
	}
	if _, ok := Dispatch("nobody-imported-me"); ok {
		t.Fatal("Dispatch answers the unknown plainly")
	}
}

func TestExecutableCodeRefused(t *testing.T) {
	// The format's promise — prompts, a card, denials, NOTHING ELSE — is
	// what makes "no sandbox" defensible. Refused, not warned about.
	cases := map[string]map[string][]byte{
		"unknown file":      {"helper.sh": []byte("#!/bin/sh\nrm -rf /\n")},
		"binary payload":    {"policies/../payload.bin": []byte{0x7f, 'E', 'L', 'F', 0, 1, 2, 3}},
		"non-utf8 manifest": nil, // handled below by rewriting agent.md
	}
	for label, extra := range cases {
		if extra == nil {
			continue
		}
		dir := writePackage(t, "carrier-"+strings.ReplaceAll(label, " ", "-"), "", nil, extra)
		if _, rj := Import(dir); rj == nil {
			t.Fatalf("%s: a package carrying it is refused (the format admits nothing unnamed)", label)
		} else if !strings.Contains(strings.ToLower(rj.Error()), "execut") {
			t.Fatalf("%s: the refusal names the format's promise: %q", label, rj.Error())
		}
	}
	dir := writePackage(t, "binary-card", "", nil, nil)
	if err := os.WriteFile(filepath.Join(dir, "agent.md"), []byte{0xff, 0xfe, 0x00, 0x01}, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, rj := Import(dir); rj == nil {
		t.Fatal("a non-UTF-8 manifest is refused as the executable-content class it is")
	}
	// The exec bit alone is enough, even on an allowed name.
	dir = writePackage(t, "sh-bit", "", nil, map[string][]byte{"policies/extra.json": []byte(`{}`)})
	if err := os.Chmod(filepath.Join(dir, "policies", "extra.json"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, rj := Import(dir); rj == nil {
		t.Fatal("a file with the exec bit is executable code by the plainest definition")
	}
}

func TestURLRefusedImportTakesAPath(t *testing.T) {
	// AP10 where it is true: not at the CLI, at the door.
	for _, u := range []string{"https://example.com/some-agent", "http://x/agent", "s3://bucket/agent"} {
		_, rj := Import(u)
		if rj == nil {
			t.Fatalf("%q: nothing is fetched; a scheme is not a path", u)
		}
		if !strings.Contains(rj.Error(), "path") || !strings.Contains(rj.Error(), "fetch") {
			t.Fatalf("the refusal names the promise: %q", rj.Error())
		}
	}
	if _, rj := Import(filepath.Join(t.TempDir(), "absent")); rj == nil {
		t.Fatal("a missing directory refuses, plainly")
	}
}

func TestImportDoesNotAlterFirstPartyBodies(t *testing.T) {
	before := string(allBodies(t))
	dir := writePackage(t, "visitor", "", nil, nil)
	mustImport(t, dir)
	if after := string(allBodies(t)); after != before {
		t.Fatal("importing must not touch a first-party body, its front matter or its registration")
	}
}

func TestCardNameMustMatchManifest(t *testing.T) {
	// The signature attests a NAME; a manifest wearing another agent's
	// verified card is the first shape an impostor takes.
	dir := writePackage(t, "wearer", "", map[string]any{
		"name": "golang-engineer", "description": "not mine", "version": "1",
		"skills": []map[string]any{{"id": "x", "name": "x", "description": "x"}},
	}, nil)
	_, rj := Import(dir)
	if rj == nil {
		t.Fatal("a card must be for the agent that carries it")
	}
	if !strings.Contains(rj.Error(), "wearer") || !strings.Contains(rj.Error(), "golang-engineer") {
		t.Fatalf("the refusal names both sides: %q", rj.Error())
	}
}

func TestMissingCardOrManifestRefused(t *testing.T) {
	dir := writePackage(t, "no-card", "", nil, nil)
	if err := os.Remove(filepath.Join(dir, "card.json")); err != nil {
		t.Fatal(err)
	}
	if _, rj := Import(dir); rj == nil {
		t.Fatal("a package without its card is not importable — the card is what AS2 attests")
	}
	dir = writePackage(t, "no-manifest", "", nil, nil)
	if err := os.Remove(filepath.Join(dir, "agent.md")); err != nil {
		t.Fatal(err)
	}
	if _, rj := Import(dir); rj == nil {
		t.Fatal("a package without its prompt is not an agent")
	}
}

// --- S37 T2: ReadPackagePreview ---------------------------------------------

// TestReadPackagePreviewRegistersNothing pins AD6: the exported preview is
// read-only — it returns the agent but registers nothing (Lookup finds
// nothing afterwards).
func TestReadPackagePreviewRegistersNothing(t *testing.T) {
	Reset()
	dir := writePackage(t, "peeker", "", nil, nil)
	a, rj := ReadPackagePreview(dir)
	if rj != nil {
		t.Fatalf("refused: %s", rj.Error())
	}
	if a.Name != "peeker" || a.State != StateDisabled {
		t.Fatalf("got %+v", a)
	}
	if _, ok := Lookup("peeker"); ok {
		t.Fatal("a preview must not register the agent")
	}
}

// TestReadPackagePreviewRefusesLikeImport pins that the wrapper and the real
// Import refuse identically, byte for byte — URL, missing dir, missing card.
// A divergence would mean the wrapper is not just readPackage.
func TestReadPackagePreviewRefusesLikeImport(t *testing.T) {
	Reset()
	// A dir without its card.json.
	noCard := writePackage(t, "nocard", "", nil, nil)
	if err := os.Remove(filepath.Join(noCard, "card.json")); err != nil {
		t.Fatalf("remove card: %v", err)
	}
	inputs := []string{
		"https://example.com/x",                       // a URL — AP10
		filepath.Join(t.TempDir(), "absent"),         // a missing dir
		noCard,                                        // a dir without its card
	}
	for _, in := range inputs {
		_, ra := ReadPackagePreview(in)
		_, rb := Import(in)
		if ra == nil {
			t.Fatalf("ReadPackagePreview should refuse %q", in)
		}
		if rb == nil {
			t.Fatalf("Import should refuse %q", in)
		}
		if ra.Error() != rb.Error() {
			t.Fatalf("refusal text must match: preview %q vs import %q", ra.Error(), rb.Error())
		}
	}
}

// TestReadPackagePreviewDoesNotNeedALedger pins that the preview has no store
// dependency: it succeeds with no stateStore bound (Reset clears the registry;
// BindStateStore was never called), where a full Import that touches the ledger
// would not.
func TestReadPackagePreviewDoesNotNeedALedger(t *testing.T) {
	Reset()
	dir := writePackage(t, "noleger", "", nil, nil)
	a, rj := ReadPackagePreview(dir)
	if rj != nil {
		t.Fatalf("preview must succeed with no ledger bound: %s", rj.Error())
	}
	if a.Name != "noleger" || a.State != StateDisabled {
		t.Fatalf("got %+v", a)
	}
}

func allBodies(t *testing.T) []byte {
	t.Helper()
	root := filepath.Join("..", "handler", "bodies")
	var out []byte
	err := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		raw, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		out = append(out, filepath.Base(p)...)
		out = append(out, raw...)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}
