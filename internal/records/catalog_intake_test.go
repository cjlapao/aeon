package records

// catalog_intake_test.go — S61 T8 (GI17): the goal-intake family's catalog
// census. What each test pins, in order:
//
//   - the family carries EXACTLY the seven specs — five reads and two
//     asks — each twinned to its plane door, the reads class safe with
//     Mutates:false, the two asks class detectable with Mutates:true
//     (they file a row — the only thing they file);
//   - no DECISION or WRITE of the plane reaches a spec, dotted or
//     underscored, as a Name or a Method, anywhere in the catalog:
//     charter.save · template.save · intake.start · intake.commit (and
//     scaffold/publish words) are human doors and the T7 guard holds the
//     wire side; this file holds the catalog side of the same law (GI14);
//   - the two PROPOSE schemas carry no identity, path, option, status,
//     kind or linkage field: the actor is the step's stamp, the intake row
//     is the loop's stamp, and a param that could only be overwritten is a
//     param that lies (OL3, GI17's forged-linkage law).

import (
	"regexp"
	"sort"
	"strings"
	"testing"
)

// intakePlaneWriteShape matches every spelling of the plane's writes that
// could hide in a tool name or Method.
var intakePlaneWriteShape = regexp.MustCompile(`(?i)(charter|template|intake)[._](save|start|commit|scaffold|publish|decide|approve|apply|land)`)

func TestCatalogHasTheSevenIntakeSpecs(t *testing.T) {
	want := map[string]struct {
		method  string
		mutates bool
		class   string
	}{
		"charter_list":    {"charter.list", false, "safe"},
		"charter_show":    {"charter.get", false, "safe"},
		"template_list":   {"template.list", false, "safe"},
		"template_show":   {"template.get", false, "safe"},
		"intake_status":   {"intake.status", false, "safe"},
		"charter_propose": {"charter.propose", true, "detectable"},
		"intake_propose":  {"intake.propose", true, "detectable"},
	}
	got := map[string]Spec{}
	for _, sp := range Catalog() {
		if sp.Family == "intake" {
			got[sp.Name] = sp
		}
	}
	if len(got) != len(want) {
		names := make([]string, 0, len(got))
		for n := range got {
			names = append(names, n)
		}
		sort.Strings(names)
		t.Fatalf("the goal-intake family holds %d specs (%v), want exactly %d (GI17)", len(got), names, len(want))
	}
	for name, w := range want {
		sp, ok := got[name]
		if !ok {
			t.Fatalf("%s is missing from the catalog — the agent's door has no spec", name)
		}
		if sp.Method != w.method {
			t.Errorf("%s is twinned to %q, want %q (one definition, two doors)", name, sp.Method, w.method)
		}
		if sp.Mutates != w.mutates {
			t.Errorf("%s Mutates=%v, want %v (an ask files a row; a read files nothing)", name, sp.Mutates, w.mutates)
		}
		if sp.Class != w.class {
			t.Errorf("%s class %q, want %q", name, sp.Class, w.class)
		}
		// GI17's honesty sentences ride the description: inert until a
		// person commits, and the text-is-data law.
		if strings.Contains(sp.Name, "propose") && !strings.Contains(strings.ToLower(sp.Description), "inert") {
			t.Errorf("%s description lost the inert-until-commit promise", name)
		}
	}
}

func TestNoDecisionOrWriteToolExists(t *testing.T) {
	for _, sp := range Catalog() {
		if intakePlaneWriteShape.MatchString(sp.Name) || intakePlaneWriteShape.MatchString(sp.Method) {
			t.Errorf("catalog spec %s → %s names a goal-intake WRITE — the plane's four writes are human doors and have no spec (GI14)", sp.Name, sp.Method)
		}
	}
}

// forbiddenIntakeKeys are the fields a propose schema may NEVER carry:
// identity (S45 T7's law), the forgeable linkage (GI17), paths and options
// (the commit is a person's act with the person's target), and the state
// words a desk row owns.
var forbiddenIntakeKeys = []string{
	"actor", "actorKind", "actorId", "identity", "origin", "createdBy",
	"status", "state", "kind", "decidedBy", "decidedAt",
	"path", "rootPath", "targetPath", "filePath", "scratchPath", "dir",
	"options", "option", "publish", "publishMode", "mode",
	"intakeId", "intake_id", "proposalId", "id",
}

func TestProposeSchemasCarryNoIdentityPathOrStatus(t *testing.T) {
	for _, sp := range Catalog() {
		if sp.Family != "intake" || !strings.Contains(sp.Name, "propose") {
			continue
		}
		keys := map[string]bool{}
		collectSchemaKeys(sp.Schema, keys)
		for _, bad := range forbiddenIntakeKeys {
			if keys[bad] {
				t.Errorf("%s schema carries %q — identity, linkage, path, option and state are stamped or owned elsewhere; a param that could only be overwritten is a param that lies (GI17)", sp.Name, bad)
			}
		}
	}
}

// collectSchemaKeys walks an obj/array schema collecting every PROPERTY
// name at any depth (array item shapes included).
func collectSchemaKeys(schema any, into map[string]bool) {
	m, ok := schema.(map[string]any)
	if !ok {
		return
	}
	if props, ok := m["properties"].(map[string]any); ok {
		for k, v := range props {
			into[k] = true
			collectSchemaKeys(v, into)
		}
	}
	if items, ok := m["items"]; ok {
		collectSchemaKeys(items, into)
	}
}
