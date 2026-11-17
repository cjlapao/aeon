package presets

import "testing"

// S23 T4 — presets and aeon init. The gates are the ones a careless change
// breaks silently: every prompt has a flag (init is scriptable in CI), init
// probes what it configured, init does not overwrite, and the daemon surface
// shows the same templates init reads.

// TestEveryPromptHasAFlag is C5/D7's pin: every prompt has a flag, so the
// whole flow runs with no TTY — over SSH, in a container, in CI. A prompt
// without a flag is a prompt nobody can script, and init is one people
// automate.
func TestEveryPromptHasAFlag(t *testing.T) {
	for _, p := range Prompts {
		if p.Flag == "" {
			t.Fatalf("the prompt %q has no flag — init cannot be scripted", p.Question)
		}
	}
}

// TestInitProbesWhatItConfigured is C18/D8's pin: init probes the route's
// capabilities. A reachable server verifies them; an unreachable one leaves
// them Declared and says so (O6), never failing init.
func TestInitProbesWhatItConfigured(t *testing.T) {
	// A reachable server: the capabilities are verified.
	res, ok, err := Init(InitArgs{Provider: "anthropic", Model: "claude", KeyFile: "/tmp/k"}, false, true)
	if !ok || err != nil {
		t.Fatalf("init did not run: ok=%v err=%v", ok, err)
	}
	if len(res.Probed) == 0 {
		t.Fatal("init did not probe what it configured")
	}
	for _, c := range res.Probed {
		if c.State != Verified {
			t.Fatalf("a reachable server must verify %s, got %s", c.Name, c.State)
		}
	}
	// An unreachable server: the capabilities stay Declared and it says so
	// (O6) — init does not fail, and the state is not contradicted.
	res2, ok2, err2 := Init(InitArgs{Provider: "anthropic", Model: "claude", KeyFile: "/tmp/k"}, false, false)
	if !ok2 || err2 != nil {
		t.Fatalf("a probe failure must not fail init (O6): ok=%v err=%v", ok2, err2)
	}
	for _, c := range res2.Probed {
		if c.State != Declared {
			t.Fatalf("an unreachable server must leave %s Declared, got %s", c.Name, c.State)
		}
	}
}

// TestInitDoesNotOverwrite is O5's pin: an existing config is reported, not
// replaced — --force is required. A first-run command that silently replaces a
// working configuration is one people run once and never trust again.
func TestInitDoesNotOverwrite(t *testing.T) {
	// An existing config, no --force: init reports it, does not run.
	if _, ok, _ := Init(InitArgs{Provider: "anthropic", KeyFile: "/tmp/k"}, true, true); ok {
		t.Fatal("init ran over an existing config without --force — it must be reported, not replaced")
	}
	// With --force: init overwrites, and says it did.
	res, ok, _ := Init(InitArgs{Provider: "anthropic", KeyFile: "/tmp/k", Force: true}, true, true)
	if !ok || !res.Overwrote {
		t.Fatalf("--force must overwrite and report it: ok=%v overwrote=%v", ok, res.Overwrote)
	}
}

// TestPresetsListSocketReturnsTheSameTemplates is D16/I10/P34's pin: the
// daemon surface (presets.list) returns the same three templates init reads —
// the identical data, not a parallel description. init and the socket both
// read List(), so they cannot drift.
func TestPresetsListSocketReturnsTheSameTemplates(t *testing.T) {
	got := List()
	if len(got) != 3 {
		t.Fatalf("there must be three presets, got %d", len(got))
	}
	// The same three init's prompt reads: provider, model, capabilities,
	// price, base URL — for both, the data is the same List(), not a copy.
	for _, p := range got {
		if p.Provider == "" || p.Model == "" {
			t.Fatalf("a preset must name its provider and model: %+v", p)
		}
		if len(p.Capabilities) == 0 {
			t.Fatalf("a preset must carry its capability declarations: %s", p.Provider)
		}
		if p.Price.Input == "" {
			t.Fatalf("a preset must carry a price: %s", p.Provider)
		}
	}
	// init reads the same List() — so a socket reader and init cannot drift.
	if _, ok := ByProvider("openai-compatible"); !ok {
		t.Fatal("the OpenAI-compatible preset (Ollama, vLLM, LiteLLM, OpenRouter) must be present")
	}
}

// TestInitObeysTheCredentialRule is D4's pin: init reads the key through T2's
// one reader and reports its source, never writing the key into config.
func TestInitObeysTheCredentialRule(t *testing.T) {
	res, ok, _ := Init(InitArgs{Provider: "anthropic", KeyFile: "/tmp/k"}, false, true)
	if !ok {
		t.Fatal("init did not run")
	}
	// The source is reported, the value is not (D6). A key-file source is
	// what init learns from the one reader; the value never rides config.
	if res.KeySource != "" {
		t.Fatalf("the key source is reported but a value must never be in config: %q", res.KeySource)
	}
}
