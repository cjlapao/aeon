package mcp

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/tools"
)

// T3: W22 applied to a surface we do not author. Both classes are required
// before a tool is callable, the classes live in OUR configuration (Q4), and
// the safe-looking default — `read-only` — is exactly the dangerous one, so
// no code path in this package invents one.

// classifiedConfig builds a config with one public server whose single tool
// carries whatever classes the test names ("" = unclassified).
func classifiedConfig(server, tool, rerun, effect string, declares bool) Config {
	cfg := Config{
		Public: []ServerConfig{{
			Name:      server,
			Transport: TransportConfig{Type: TransportStdio, Command: "unused"},
			Tools:     []ToolConfig{{Name: tool, Rerun: rerun, Effect: effect}},
		}},
	}
	if declares {
		cfg.Agents = map[string]AgentConfig{"agent-a": {Declares: []string{server + "." + tool}}}
	}
	return cfg
}

// TestUnclassifiedToolRefusedNamingToolAndWhatIsMissing is D6/MC6's first
// half: "unavailable" alone is not actionable; the sentence names the tool
// AND what it lacks.
func TestUnclassifiedToolRefusedNamingToolAndWhatIsMissing(t *testing.T) {
	r, err := New(classifiedConfig("alpha", "mystery", "", "", true))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	st, missing, _ := r.Availability("agent-a", "alpha.mystery")
	if st != AvailabilityUnavailable {
		t.Fatalf("availability = %v, want unavailable (not unknown: it exists; not available: unclassified)", st)
	}
	if len(missing) != 2 {
		t.Fatalf("missing = %v, want rerun+effect", missing)
	}
	sent := UnavailableReason("alpha.mystery", missing)
	for _, want := range []string{"unavailable", "alpha.mystery", "re-run class", "effect class"} {
		if !strings.Contains(sent, want) {
			t.Errorf("unavailable sentence %q does not name %q", sent, want)
		}
	}
	// And it is NOT callable: the capability list never carries it.
	for _, tool := range r.ToolsFor("agent-a") {
		if tool.Qualified == "alpha.mystery" {
			t.Error("unclassified tool reached the capability list")
		}
	}
}

func TestToolMissingOnlyRerunIsRefused(t *testing.T) {
	r, err := New(classifiedConfig("alpha", "half", "", "local", true))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	st, missing, _ := r.Availability("agent-a", "alpha.half")
	if st != AvailabilityUnavailable || len(missing) != 1 || missing[0] != "rerun" {
		t.Fatalf("st=%v missing=%v, want unavailable/[rerun]", st, missing)
	}
	if got := UnavailableReason("alpha.half", missing); !strings.Contains(got, "re-run") || strings.Contains(got, "effect class") {
		t.Errorf("sentence names the wrong absence: %q", got)
	}
}

func TestToolMissingOnlyEffectIsRefused(t *testing.T) {
	r, err := New(classifiedConfig("alpha", "half", "safe", "", true))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	st, missing, _ := r.Availability("agent-a", "alpha.half")
	if st != AvailabilityUnavailable || len(missing) != 1 || missing[0] != "effect" {
		t.Fatalf("st=%v missing=%v, want unavailable/[effect]", st, missing)
	}
}

// TestNoDefaultClassificationExists is MC6's grep (R3): the one unacceptable
// failure of this task "will look like tidy code" — a `default:` arm handing
// out a class. classify.go is the file that would hold it, and a tidy switch
// over classes MUST enumerate; if a future edit wants a fallback, this test
// failing is the review conversation, not a formality.
func TestNoDefaultClassificationExists(t *testing.T) {
	src, err := os.ReadFile("classify.go")
	if err != nil {
		t.Fatalf("classify.go missing — this test guards the file that owns classification: %v", err)
	}
	var codeLines []string
	for _, line := range strings.Split(string(src), "\n") {
		if t := strings.TrimSpace(line); strings.HasPrefix(t, "//") {
			continue // comments DOCUMENT the rule; they must not trip it
		}
		codeLines = append(codeLines, line)
	}
	text := strings.Join(codeLines, "\n")
	if strings.Contains(text, "default:") {
		t.Error("classify.go contains a `default:` arm — MC6 refuses a fallback classification, and 'it looked tidy' is how R3 happens")
	}
	// And no fallback-assignment idiom: a zero-value-then-fix flow.
	for _, bad := range []string{"if rerun == \"\" {", "if effect == \"\" {"} {
		if strings.Contains(text, bad) {
			t.Errorf("classify.go fills an empty class (%s) — an empty class is a refusal, never a value", bad)
		}
	}
}

// TestRegistryAndConfigNeverDefaultEither widens the grep to the two other
// files that could quietly grow a fallback: neither may contain a default
// class string outside validation/enumeration contexts. (validRerun and
// validEffect ENUMERATE legal values; they never PRODUCE one — the regex
// here is intentionally narrow: assignment of a literal class.)
func TestRegistryAndConfigNeverDefaultEither(t *testing.T) {
	for _, file := range []string{"registry.go", "config.go"} {
		src, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("ReadFile %s: %v", file, err)
		}
		for _, line := range strings.Split(string(src), "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "//") {
				continue
			}
			// Declaring the VOCABULARY (`EffectLocal = "local"`) is legal
			// and necessary; ASSIGNING a default to a tool is what is
			// refused. The difference is the left side: a vocabulary
			// declaration names a constant.
			if strings.HasPrefix(trimmed, "Effect") || strings.HasPrefix(trimmed, "Rerun") ||
				strings.HasPrefix(trimmed, "EffectLocal") || strings.HasPrefix(trimmed, "EffectClass") {
				continue
			}
			for _, lit := range []string{`"safe"`, `"detectable"`, `"unsafe"`, `"local"`, `"outward"`, `"irreversible"`} {
				if strings.Contains(trimmed, "=") && strings.Contains(trimmed, lit) &&
					!strings.Contains(trimmed, "case ") && !strings.Contains(trimmed, "==") &&
					!strings.Contains(trimmed, "EffectClass(") && !strings.Contains(trimmed, "RerunClass(") {
					t.Errorf("%s: %q assigns a class literal — classes arrive from configuration or nowhere (MC6)", file, trimmed)
				}
			}
		}
	}
}

// ── Q4: the server's claim is a hint that may RAISE and never LOWER ───────

// The fake's echo tool claims readOnlyHint (T2's fake carries real MCP
// annotations). Our config calls it outward/unsafe. **Our classification
// wins** — trusting a stranger's self-assessment of how dangerous its own
// tool is is the mistake MC6 refuses, with extra steps.
func TestServerClaimCannotLowerOurClass(t *testing.T) {
	bin := fakeServerPath(t)
	cfg := classifiedConfig("alpha", "echo", "unsafe", "outward", true)
	cfg.Public[0].Transport = TransportConfig{Type: TransportStdio, Command: bin}
	r, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	c, err := Dial(ctx, cfg.Public[0], nil)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer c.Close()
	remote, err := c.ListTools(ctx)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	// Precondition: the fake really does claim read-only for echo, or this
	// test proves nothing.
	var claimed *RemoteTool
	for i, rt := range remote {
		if rt.Name == "echo" {
			claimed = &remote[i]
		}
	}
	if claimed == nil || claimed.Annotations == nil || claimed.Annotations.ReadOnlyHint == nil || !*claimed.Annotations.ReadOnlyHint {
		t.Fatal("the fake stopped claiming readOnlyHint for echo — the test would pass vacuously")
	}
	if err := r.ApplyListings("alpha", remote); err != nil {
		t.Fatalf("ApplyListings: %v", err)
	}
	for _, tool := range r.ToolsFor("agent-a") {
		if tool.Qualified != "alpha.echo" {
			continue
		}
		if tool.Effect != EffectClassOutward || tool.Rerun != tools.RerunUnsafe {
			t.Errorf("the server talked us out of our own class: got %s/%s, want outward/unsafe (Q4)", tool.Rerun, tool.Effect)
		}
	}
}

// The mirror: a server that admits its tool is DESTRUCTIVE makes our class
// more careful, never less. A raise is safe because the operator reviewing
// the diff sees only the class the system decided with — and the decision
// can only move toward caution.
func TestServerDestructiveClaimRaisesButNeverInvents(t *testing.T) {
	bin := fakeServerPath(t)
	// `post` is the fake's destructive-claiming tool. Classified mild here.
	cfg := classifiedConfig("alpha", "post", "safe", "local", true)
	cfg.Public[0].Transport = TransportConfig{Type: TransportStdio, Command: bin}
	r, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	c, err := Dial(ctx, cfg.Public[0], nil)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer c.Close()
	remote, err := c.ListTools(ctx)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	var claimed *RemoteTool
	for i, rt := range remote {
		if rt.Name == "post" {
			claimed = &remote[i]
		}
	}
	if claimed == nil || claimed.Annotations == nil || claimed.Annotations.DestructiveHint == nil || !*claimed.Annotations.DestructiveHint {
		t.Fatal("the fake stopped claiming destructiveHint for post")
	}
	if err := r.ApplyListings("alpha", remote); err != nil {
		t.Fatalf("ApplyListings: %v", err)
	}
	for _, tool := range r.ToolsFor("agent-a") {
		if tool.Qualified != "alpha.post" {
			continue
		}
		if tool.Effect != EffectClassOutward && tool.Effect != EffectClassIrreversible {
			t.Errorf("destructive admission did not raise effect: %s (must move toward caution)", tool.Effect)
		}
		if tool.Rerun == tools.RerunSafe {
			t.Errorf("destructive admission did not raise rerun: still safe (a crashed recovery would re-post)")
		}
	}
}

// The dangerous pair to the dangerous default: a server claiming
// readOnlyHint for a tool whose classes OUR config never supplied must NOT
// become callable. The hint is a modifier on our decision, never a source
// of one.
func TestServerHintNeverInventsAClass(t *testing.T) {
	bin := fakeServerPath(t)
	cfg := classifiedConfig("alpha", "mystery", "", "", true) // unclassified on our side
	cfg.Public[0].Transport = TransportConfig{Type: TransportStdio, Command: bin}
	r, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	c, err := Dial(ctx, cfg.Public[0], nil)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer c.Close()
	remote, err := c.ListTools(ctx)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	var claimed *RemoteTool
	for i, rt := range remote {
		if rt.Name == "mystery" {
			claimed = &remote[i]
		}
	}
	if claimed == nil || claimed.Annotations == nil || claimed.Annotations.ReadOnlyHint == nil || !*claimed.Annotations.ReadOnlyHint {
		t.Fatal("the fake's mystery tool stopped claiming readOnlyHint — this test is about a tempting claim")
	}
	if err := r.ApplyListings("alpha", remote); err != nil {
		t.Fatalf("ApplyListings: %v", err)
	}
	st, _, _ := r.Availability("agent-a", "alpha.mystery")
	if st != AvailabilityUnavailable {
		t.Errorf("the server's readOnlyHint classified our unclassified tool (availability=%v) — self-assessment is not a class (Q4/MC6)", st)
	}
	for _, tool := range r.ToolsFor("agent-a") {
		if tool.Qualified == "alpha.mystery" {
			t.Error("a server-claimed read-only made an unclassified tool callable — this is the exact leak MC6 exists to prevent")
		}
	}
}

// TestUnavailableDistinguishableFromDeniedAndUnknown is the three-refusals
// table from T5 pinned at the type level: three values, three sentences.
// (The denied-by-policy sentence is S10's own, verbatim — T7 attaches it;
// this asserts unavailable and unknown are NOT each other.)
func TestUnavailableDistinguishableFromDeniedAndUnknown(t *testing.T) {
	r, err := New(fixtureConfig())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	unknown, _, _ := r.Availability("golang-engineer-loop", "fakeserver.search")
	unavail, missing, _ := r.Availability("reddit-agent", "fakeserver.mystery")
	if unknown == unavail {
		t.Fatal("unknown and unavailable are the same value — the transcript cannot tell them apart (T5)")
	}
	u1 := "unknown tool: fakeserver.search"
	u2 := UnavailableReason("fakeserver.mystery", missing)
	if u1 == u2 {
		t.Fatal("the two refusals render identically")
	}
	if !strings.HasPrefix(u2, "unavailable:") || !strings.HasPrefix(u1, "unknown tool:") {
		t.Errorf("renderings: %q / %q — each must lead with its kind", u1, u2)
	}
}

// TestClassifiedToolIsCallable completes the picture: classes on both axes
// mean availability, and the effective classes are ours (possibly raised).
func TestClassifiedToolIsCallable(t *testing.T) {
	r, err := New(classifiedConfig("alpha", "search", "safe", "local", true))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	st, _, tier := r.Availability("agent-a", "alpha.search")
	if st != AvailabilityAvailable || tier != TierPublic {
		t.Fatalf("st=%v tier=%v, want available/public", st, tier)
	}
	for _, tool := range r.ToolsFor("agent-a") {
		if tool.Qualified == "alpha.search" && (tool.Rerun != tools.RerunSafe || tool.Effect != EffectClassLocal) {
			t.Errorf("effective classes drifted: %s/%s", tool.Rerun, tool.Effect)
		}
	}
}

// TestRaiseMergeIsTotal pins the pure function: every (ours × hint) pair
// lands on a legal class, never below ours. Exhaustive over the six legal
// values × four hint states — a property test where a count-free table is.
func TestRaiseMergeIsTotal(t *testing.T) {
	reruns := []tools.RerunClass{tools.RerunSafe, tools.RerunDetectable, tools.RerunUnsafe}
	effects := []EffectClass{EffectClassLocal, EffectClassOutward, EffectClassIrreversible}
	rank := map[EffectClass]int{EffectClassLocal: 0, EffectClassOutward: 1, EffectClassIrreversible: 2}
	for _, re := range reruns {
		for _, ef := range effects {
			for _, hint := range []struct {
				readOnly, destructive *bool
			}{
				{nil, nil}, {boolPtr(true), nil}, {nil, boolPtr(true)}, {boolPtr(true), boolPtr(true)},
			} {
				gotRerun, gotEffect := raiseClasses(re, ef, &ToolAnnotations{ReadOnlyHint: hint.readOnly, DestructiveHint: hint.destructive})
				if !gotRerun.Valid() {
					t.Errorf("raise produced an illegal rerun class %q", gotRerun)
				}
				if rank[gotEffect] < rank[ef] {
					t.Errorf("raise LOWERED effect %s → %s (readOnly=%v destructive=%v) — never", ef, gotEffect, hint.readOnly, hint.destructive)
				}
				if hint.destructive != nil && *hint.destructive {
					if gotEffect == EffectClassLocal {
						t.Errorf("destructive hint left effect local")
					}
					if gotRerun == tools.RerunSafe {
						t.Errorf("destructive hint left rerun safe")
					}
				}
				if hint.readOnly != nil && *hint.readOnly && hint.destructive == nil {
					if gotRerun != re || gotEffect != ef {
						t.Errorf("a readOnly-only claim changed anything: %s/%s → %s/%s — a kind word from a stranger is not evidence", re, ef, gotRerun, gotEffect)
					}
				}
			}
		}
	}
}

func boolPtr(b bool) *bool { return &b }

// TestClassifyFileIsTheOnlyRaiseSite: the merge lives in classify.go; if a
// second call site merges hints itself, the rules will drift.
func TestClassifyFileIsTheOnlyRaiseSite(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	seen := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		src, err := os.ReadFile(e.Name())
		if err != nil {
			t.Fatalf("ReadFile %s: %v", e.Name(), err)
		}
		n := strings.Count(string(src), "raiseClasses(")
		if n > 0 && e.Name() != "classify.go" {
			t.Errorf("%s calls raiseClasses %d times — classification merges in classify.go only", e.Name(), n)
		}
		seen += n
	}
	if seen == 0 {
		t.Error("nothing merges hints at all — then what did the hint fields arrive for?")
	}
}

// TestHintDataArrivesOverBothTransports: annotations parse over stdio AND
// http — a hint lost in one transport silently disables raise-not-lower
// there, which fails CLOSED but invisibly (a raise that never lands looks
// like a config that was never raised).
func TestHintDataArrivesOverBothTransports(t *testing.T) {
	mk := []struct {
		name string
		fn   func(*testing.T, string) ServerConfig
	}{
		{"stdio", func(t *testing.T, n string) ServerConfig { return stdioConfig(t, n) }},
		{"http", startHTTPFake},
	}
	for _, m := range mk {
		t.Run(m.name, func(t *testing.T) {
			c := dialFake(t, m.fn(t, "fakeserver"))
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			remote, err := c.ListTools(ctx)
			if err != nil {
				t.Fatalf("ListTools: %v", err)
			}
			byName := map[string]RemoteTool{}
			for _, rt := range remote {
				byName[rt.Name] = rt
			}
			for _, want := range []struct {
				tool  string
				check func(*ToolAnnotations) bool
				desc  string
			}{
				{"echo", func(a *ToolAnnotations) bool { return a != nil && a.ReadOnlyHint != nil && *a.ReadOnlyHint }, "readOnlyHint"},
				{"post", func(a *ToolAnnotations) bool { return a != nil && a.DestructiveHint != nil && *a.DestructiveHint }, "destructiveHint"},
				{"mystery", func(a *ToolAnnotations) bool { return a != nil && a.ReadOnlyHint != nil && *a.ReadOnlyHint }, "readOnlyHint"},
			} {
				rt, ok := byName[want.tool]
				if !ok {
					t.Fatalf("fake lost tool %q", want.tool)
				}
				if !want.check(rt.Annotations) {
					t.Errorf("%s over %s: %s did not arrive (annotations=%+v)", want.tool, m.name, want.desc, rt.Annotations)
				}
			}
		})
	}
}
