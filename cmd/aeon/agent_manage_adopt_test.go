package main

// agent_manage_adopt_test.go — S44 T10's path-B acceptance: the unsigned-import
// override is read per import call (CS7/CS11) through the wiring, and S43's old
// env var (AEON_AGENTMANAGE_ALLOW_UNSIGNED_IMPORT) keeps working as a
// one-release fallback so a deployed script does not silently stop working.
// With no config store (opts.Settings nil) the env var is the only source, so
// setting it admits an unsigned card and not setting it refuses.
import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/daemon"
	"github.com/cjlapao/aeon/internal/protocol"
)

// unsignedCardPath writes a card with no signature and no certificate — the
// real verifier (agentcard.VerifyCardSignature) answers CardStateUnsigned for
// it, so the read site's allowUnsignedImport override is what decides it.
func unsignedCardPath(t *testing.T, slug, name string) string {
	t.Helper()
	raw, err := json.Marshal(map[string]any{
		"card":        map[string]string{"slug": slug, "name": name, "version": "1"},
		"signature":   "",
		"certificate": "",
	})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "card.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestAgentManageAdoptsAllowUnsignedImportViaOldEnvFallback(t *testing.T) {
	s := openAgentStore(t)
	// No config store: the one-release env fallback is the only source, so the
	// old env var (not the config key's env) is what the read site sees.
	h := agentManageHandleFor(s, &daemon.Options{})
	// S73 T3: agent.import is one of the six writes that now carry an actor
	// (AR2), stamped from the peer by the protocol table over the socket. This
	// test drives the seam with no connection, so it stamps the person the
	// table would have stamped — the import below must be admitted on the
	// unsigned-override question, not refused for a missing actor.
	ctx := protocol.WithAgentActor(context.Background(), "user", "1000")
	cardPath := unsignedCardPath(t, "groot", "Groot")
	importParams, _ := json.Marshal(map[string]string{"path": cardPath})

	// Refused by default: no env var, no config store → the default (false).
	// The closure returns a refusal as a non-empty message (second value), not
	// as a hard error (third value).
	res, msg, err := h(ctx, "agent.import", importParams)
	if err != nil {
		t.Fatalf("import returned a hard error: %v", err)
	}
	if res != nil || msg == "" {
		t.Fatalf("unsigned by default (no env var): expected a refusal, got res=%v msg=%q", res, msg)
	}

	// Behind the one-release fallback: the old env var set → admitted, marked
	// unsigned. The read is live per call (CS7/CS11), so setting it now admits.
	t.Setenv("AEON_AGENTMANAGE_ALLOW_UNSIGNED_IMPORT", "1")
	res, msg, err = h(ctx, "agent.import", importParams)
	if err != nil || msg != "" || res == nil {
		t.Fatalf("unsigned with the old env var set: err=%v msg=%q res=%v", err, msg, res)
	}
	agents, err := s.AgentsAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(agents) != 1 || agents[0].Slug != "groot" {
		t.Fatalf("the env fallback should have stored the unsigned import; got %d agents", len(agents))
	}
}

// A card can arrive as its own contents — a window holding a dropped file has no path on this
// machine to name — and then goes through the same verification and the same unsigned-import
// refusal as a card read from a path. Exactly one of the two is sent.
func TestAgentManageImportsACardFromItsContents(t *testing.T) {
	s := openAgentStore(t)
	h := agentManageHandleFor(s, &daemon.Options{})
	ctx := protocol.WithAgentActor(context.Background(), "user", "1000")
	raw, err := os.ReadFile(unsignedCardPath(t, "groot", "Groot"))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]string{"content": string(raw)})

	// Unsigned is refused by default, from contents exactly as from a path.
	res, msg, err := h(ctx, "agent.import", body)
	if err != nil || res != nil || msg == "" || !strings.Contains(msg, "unsigned") {
		t.Fatalf("unsigned contents by default: want an unsigned refusal, got res=%v msg=%q err=%v", res, msg, err)
	}

	t.Setenv("AEON_AGENTMANAGE_ALLOW_UNSIGNED_IMPORT", "1")
	res, msg, err = h(ctx, "agent.import", body)
	if err != nil || msg != "" || res == nil {
		t.Fatalf("unsigned contents with the override: err=%v msg=%q res=%v", err, msg, res)
	}
	agents, err := s.AgentsAll()
	if err != nil || len(agents) != 1 || agents[0].Slug != "groot" {
		t.Fatalf("the contents import should have stored groot; got %v err=%v", agents, err)
	}

	// Exactly one way in: neither, or both, is refused before anything is read.
	for name, params := range map[string]string{
		"neither": `{}`,
		"both":    `{"path":"/x","content":"{}"}`,
		"empty":   `{"content":""}`,
	} {
		res, msg, err := h(ctx, "agent.import", json.RawMessage(params))
		if err != nil || res != nil || !strings.Contains(msg, "either a file path or the card's contents") {
			t.Fatalf("%s: want the one-way-in refusal, got res=%v msg=%q err=%v", name, res, msg, err)
		}
	}
}

// agent.create carries the parts a new agent is given — instructions, tech, tools, a minion
// colour — through the seam to the manager, and a tool nobody compiled is a refusal naming it.
func TestAgentManageCreateCarriesPromptTechToolsAndColour(t *testing.T) {
	s := openAgentStore(t)
	h := agentManageHandleFor(s, &daemon.Options{})
	ctx := protocol.WithAgentActor(context.Background(), "user", "1000")

	body, _ := json.Marshal(map[string]any{
		"name": "Release notes writer", "description": "d", "prompt": "You write release notes.",
		"tech": []string{"docs"}, "tools": []string{"read", "grep"}, "avatarColor": "pink",
	})
	res, msg, err := h(ctx, "agent.create", body)
	if err != nil || msg != "" || res == nil {
		t.Fatalf("create: err=%v msg=%q res=%v", err, msg, res)
	}
	got, found, err := s.AgentBySlug("release-notes-writer")
	if err != nil || !found || got.Tools != `["grep","read"]` || got.Prompt != "You write release notes." || got.AvatarColor != "pink" {
		t.Fatalf("the row did not carry the parts: %+v found=%v err=%v", got, found, err)
	}

	bad, _ := json.Marshal(map[string]any{"name": "Broken", "tools": []string{"wrtie"}})
	res, msg, err = h(ctx, "agent.create", bad)
	if err != nil || res != nil || !strings.Contains(msg, "wrtie") {
		t.Fatalf("an unknown tool must be refused by name: res=%v msg=%q err=%v", res, msg, err)
	}
	if _, found, _ := s.AgentBySlug("broken"); found {
		t.Fatal("a refused create left a row")
	}
}
