package handler

import (
	"encoding/json"
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/blueprint"
)

func TestRegistryResolvesByName(t *testing.T) {
	a, ok := Lookup("default")
	if !ok {
		t.Fatal("default absent")
	}
	b, ok := Lookup("read-only")
	if !ok {
		t.Fatal("read-only absent")
	}
	if a.SystemPrompt == b.SystemPrompt || reflect.DeepEqual(a.Tools, b.Tools) {
		t.Fatal("handlers must differ in prompt and tools")
	}
}
func TestUnknownHandlerIsADomainRefusal(t *testing.T) {
	if _, ok := Lookup("missing"); ok {
		t.Fatal("unknown handler resolved")
	}
}
func TestRegistryCannotGrantTools(t *testing.T) {
	forbidden := regexp.MustCompile(`(?i)tool|permission|allow|grant`)
	for _, ty := range []reflect.Type{reflect.TypeOf(blueprint.Step{}), reflect.TypeOf(blueprint.Lane{})} {
		for i := 0; i < ty.NumField(); i++ {
			if forbidden.MatchString(ty.Field(i).Name) {
				t.Fatalf("blueprint field %s could grant tools", ty.Field(i).Name)
			}
		}
	}
}
func TestCompileTakesToolsFromHandlerOnly(t *testing.T) {
	got, ok := Build("read-only", "survey")
	if !ok || !reflect.DeepEqual(got.Tools, []string{"read", "list"}) {
		t.Fatal(got, ok)
	}
}
func TestRegistryMatchesCommittedList(t *testing.T) {
	raw, err := os.ReadFile("testdata/handlers.json")
	if err != nil {
		t.Fatal(err)
	}
	var want []string
	if err := json.Unmarshal(raw, &want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(want, Names()) {
		t.Fatal(want, Names())
	}
}

// --- T19: policy carried, never granting -------------------------------------

// seedTestEntry puts a throwaway entry into the map directly — the tests are
// in-package precisely so the registry can be exercised with authored shapes
// that never ship.
func seedTestEntry(t *testing.T, h Handler) {
	t.Helper()
	entries[h.Name] = h
	t.Cleanup(func() { delete(entries, h.Name) })
}

func TestPolicyNamingARegisteredToolRefusesTheBuild(t *testing.T) {
	// The substitution gap T19 closes: a handler whose Policy names a
	// registered tool is a grant wearing a declaration's clothes. Refused
	// with false and no panic — the same shape as the unknown-name refusal.
	seedTestEntry(t, Handler{
		Name:         "t19-smuggler",
		SystemPrompt: "x",
		Tools:        []string{"read", "list"},
		Policy:       []string{"bash"},
	})
	if _, ok := Build("t19-smuggler", "p"); ok {
		t.Fatal("Build granted through a policy claim naming a registered tool")
	}

	// The hidden alias counts too: naming write_file names write's
	// capability, and capability is what a grant grants.
	seedTestEntry(t, Handler{
		Name:         "t19-alias-smuggler",
		SystemPrompt: "x",
		Tools:        []string{"read"},
		Policy:       []string{"write_file"},
	})
	if _, ok := Build("t19-alias-smuggler", "p"); ok {
		t.Fatal("Build granted through a policy claim naming the hidden alias")
	}
}

func TestBenignPolicyBuildsWithUnchangedGrants(t *testing.T) {
	// A policy list that names rule SUBJECTS — what the field is for —
	// builds, and the grants are exactly the template's Tools: the policy
	// declaration adds nothing to them and removes nothing.
	seedTestEntry(t, Handler{
		Name:         "t19-audited",
		SystemPrompt: "x",
		Tools:        []string{"read", "list"},
		Policy:       []string{"command_access/audit-only"},
	})
	got, ok := Build("t19-audited", "survey")
	if !ok {
		t.Fatal("a benign policy declaration was refused")
	}
	if !reflect.DeepEqual(got.Tools, []string{"read", "list"}) {
		t.Errorf("grants changed under a policy declaration: %v", got.Tools)
	}
}

func TestEmptyPolicyBehavesExactlyAsToday(t *testing.T) {
	// The negative control: entries without a Policy (every shipped entry,
	// until one authors a declaration) build exactly as before — same grants,
	// same name, same composed system prompt.
	seedTestEntry(t, Handler{
		Name:         "t19-plain",
		SystemPrompt: "plain prompt",
		Tools:        []string{"read"},
	})
	got, ok := Build("t19-plain", "task")
	if !ok {
		t.Fatal("an entry without a policy declaration was refused")
	}
	if !reflect.DeepEqual(got.Tools, []string{"read"}) || got.Name != "t19-plain" {
		t.Errorf("the plain entry built differently: %+v", got)
	}
	if !strings.Contains(got.System, "plain prompt") || !strings.Contains(got.System, "task") {
		t.Errorf("the system prompt composition changed: %q", got.System)
	}
}

func TestNoPresetGrantsWebfetchWhileTheHostSpaceIsUnscoped(t *testing.T) {
	// SEC-T16-01's precondition, asserted at the registration audit: webfetch
	// is REGISTERED (the tool exists for an author to grant) but no preset —
	// no shipped handler entry — grants the egress while no host-scope seam
	// bounds the address space. A preset that named webfetch would put the
	// SSRF exposure on the default path instead of behind an operator's
	// explicit, per-deployment decision. The seam (DialContext over RESOLVED
	// addresses, default-deny on 169.254.0.0/16 + link-local + the metadata
	// IP, documented loopback opt-in) is the precondition for making this
	// preset-able at all; it is recorded with the grantability gate.
	for name, h := range entries {
		for _, tool := range h.Tools {
			if tool == "webfetch" {
				t.Fatalf("preset %q grants webfetch while the host space is unscoped (SEC-T16-01)", name)
			}
		}
	}
}
