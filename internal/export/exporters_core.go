package export

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/cjlapao/aeon/internal/store"
)

// The declared reads live in the store — the single definition of the closed
// table set and the per-table column allow-lists, at the SQL boundary where
// identifiers become SQL (internal/store/export.go). The engine builds its
// reads from store.DeclaredColumns and reads through the store's TableSpec,
// the direction internal/backup sets: the engine imports the store, and the
// store never imports the engine (the import guard in store_test.go pins it).

// isCredentialName mirrors the S44 config rule (internal/config/creds.go): a
// name shaped like a credential is never exported. It is a mirror, not an
// import — the engine's only aeon import is internal/store (the shape of the
// SQL boundary it reads through), and config stays out of it; a forked rule
// would be a second truth. The extra marks (certificate, signature) cover the
// signing artifacts the config rule predates.
func isCredentialName(name string) bool {
	n := strings.ToLower(name)
	for _, mark := range []string{"key", "token", "secret", "apikey", "api_key", "password", "credential", "certificate", "signature"} {
		if strings.Contains(n, mark) {
			return true
		}
	}
	return false
}

// — the registry: the closed set, in registry order (see registryOrder) —

var registry = map[string]Exporter{
	"agents": {
		Name:        "agents",
		Description: "The agent records (identity, prompt, skills, tools, policy links), content verbatim; signature and signer_certificate are omitted — signing artifacts are not record content.",
		Tables:      []string{"agent"},
		Read:        readAgents,
	},
	"components": {
		Name:        "components",
		Description: "The component tree: every component with its roots, its parentId and its archivedAt; remote_key and repo_key are omitted (credential-shaped). Effective settings are derived and are not exported.",
		Tables:      []string{"component", "component_root"},
		Read:        readComponents,
	},
	"goals": {
		Name:        "goals",
		Description: "The planning records: epics, goals and briefs, one file per goal/brief version (<id>-v<version>.json).",
		Tables:      []string{"epic", "goal", "brief"},
		Read:        readGoals,
	},
	"memory": {
		Name:        "memory",
		Description: "The learning rows — the loop's scratch memory. Opt-in: absent from a default export, present only with --include-memory.",
		Tables:      []string{"learning"},
		Optional:    true,
		Read:        readMemory,
	},
	"policies": {
		Name:        "policies",
		Description: "The policy rows, content verbatim (type, target, action, spec, source, expiry).",
		Tables:      []string{"policy"},
		Read:        readPolicies,
	},
	"routes": {
		Name:        "routes",
		Description: "The route rows: provider, model, endpoints, prices, capabilities. Keys never ride a route row — they live in the credentials/ files, which the export does not touch.",
		Tables:      []string{"route"},
		Read:        readRoutes,
	},
	"settings": {
		Name:        "settings",
		Description: "The stored settings (S44), one file per non-credential-shaped key; a credential-shaped key is never exported, in name or value.",
		Tables:      []string{"setting"},
		Read:        readSettings,
	},
	"triggers": {
		Name:        "triggers",
		Description: "The trigger records: event triggers (events/) and their schedules (schedules/); secret_env is omitted (credential-shaped).",
		Tables:      []string{"event_trigger", "trigger_schedule"},
		Read:        readTriggers,
	},
}

// — the eight set exporters —

func readAgents(ctx context.Context, r Reader) ([]File, error) {
	rows, err := r.ExportRows(ctx, store.TableSpec{Table: "agent", Columns: store.DeclaredColumns["agent"], OrderBy: "slug"})
	if err != nil {
		return nil, err
	}
	var files []File
	for _, row := range rows {
		name := strVal(row["slug"])
		if name == "" {
			name = idVal(row["id"])
		}
		files = append(files, fileFor("agents/"+SafeName(name)+".json", row))
	}
	return files, nil
}

func readPolicies(ctx context.Context, r Reader) ([]File, error) {
	rows, err := r.ExportRows(ctx, store.TableSpec{Table: "policy", Columns: store.DeclaredColumns["policy"], OrderBy: "id"})
	if err != nil {
		return nil, err
	}
	var files []File
	for _, row := range rows {
		files = append(files, fileFor("policies/"+SafeName(idVal(row["id"]))+".json", row))
	}
	return files, nil
}

func readRoutes(ctx context.Context, r Reader) ([]File, error) {
	rows, err := r.ExportRows(ctx, store.TableSpec{Table: "route", Columns: store.DeclaredColumns["route"], OrderBy: "id"})
	if err != nil {
		return nil, err
	}
	var files []File
	for _, row := range rows {
		files = append(files, fileFor("routes/"+SafeName(idVal(row["id"]))+".json", row))
	}
	return files, nil
}

func readComponents(ctx context.Context, r Reader) ([]File, error) {
	comps, err := r.ExportRows(ctx, store.TableSpec{Table: "component", Columns: store.DeclaredColumns["component"], OrderBy: "slug"})
	if err != nil {
		return nil, err
	}
	roots, err := r.ExportRows(ctx, store.TableSpec{Table: "component_root", Columns: store.DeclaredColumns["component_root"], OrderBy: "component_id"})
	if err != nil {
		return nil, err
	}
	byID := map[string][]map[string]any{}
	for _, rt := range roots {
		key := idVal(rt["component_id"])
		v := map[string]any{
			"component_root": rt["component_root"],
			"is_primary":     rt["is_primary"],
			"added_at":       rt["added_at"],
		}
		byID[key] = append(byID[key], v)
	}
	var files []File
	for _, row := range comps {
		v := make(map[string]any, len(row)+3)
		for k, val := range row {
			v[k] = val
		}
		v["parentId"] = treeID(row["parent_id"])
		v["archivedAt"] = treeID(row["archived_at"])
		list := byID[idVal(row["id"])]
		sort.Slice(list, func(i, j int) bool {
			return strVal(list[i]["component_root"]) < strVal(list[j]["component_root"])
		})
		if list == nil {
			list = []map[string]any{}
		}
		v["roots"] = list
		name := strVal(row["slug"])
		if name == "" {
			name = idVal(row["id"])
		}
		files = append(files, fileFor("components/"+SafeName(name)+".json", v))
	}
	return files, nil
}

func readTriggers(ctx context.Context, r Reader) ([]File, error) {
	events, err := r.ExportRows(ctx, store.TableSpec{Table: "event_trigger", Columns: store.DeclaredColumns["event_trigger"], OrderBy: "name"})
	if err != nil {
		return nil, err
	}
	scheds, err := r.ExportRows(ctx, store.TableSpec{Table: "trigger_schedule", Columns: store.DeclaredColumns["trigger_schedule"], OrderBy: "id"})
	if err != nil {
		return nil, err
	}
	files := make([]File, 0, len(events)+len(scheds))
	for _, row := range events {
		files = append(files, fileFor("triggers/events/"+SafeName(idVal(row["id"]))+".json", row))
	}
	for _, row := range scheds {
		files = append(files, fileFor("triggers/schedules/"+SafeName(idVal(row["id"]))+".json", row))
	}
	return files, nil
}

func readGoals(ctx context.Context, r Reader) ([]File, error) {
	epics, err := r.ExportRows(ctx, store.TableSpec{Table: "epic", Columns: store.DeclaredColumns["epic"], OrderBy: "id"})
	if err != nil {
		return nil, err
	}
	goals, err := r.ExportRows(ctx, store.TableSpec{Table: "goal", Columns: store.DeclaredColumns["goal"], OrderBy: "id"})
	if err != nil {
		return nil, err
	}
	briefs, err := r.ExportRows(ctx, store.TableSpec{Table: "brief", Columns: store.DeclaredColumns["brief"], OrderBy: "id"})
	if err != nil {
		return nil, err
	}
	files := make([]File, 0, len(epics)+len(goals)+len(briefs))
	for _, row := range epics {
		files = append(files, fileFor("goals/epics/"+SafeName(idVal(row["id"]))+".json", row))
	}
	for _, row := range versionedRows(goals) {
		files = append(files, fileFor("goals/goals/"+SafeName(idVal(row["id"]))+"-v"+SafeName(idVal(row["version"]))+".json", row))
	}
	for _, row := range versionedRows(briefs) {
		files = append(files, fileFor("goals/briefs/"+SafeName(idVal(row["id"]))+"-v"+SafeName(idVal(row["version"]))+".json", row))
	}
	return files, nil
}

func readSettings(ctx context.Context, r Reader) ([]File, error) {
	rows, err := r.ExportRows(ctx, store.TableSpec{Table: "setting", Columns: store.DeclaredColumns["setting"], OrderBy: "key"})
	if err != nil {
		return nil, err
	}
	var files []File
	for _, row := range rows {
		key := strVal(row["key"])
		if isCredentialName(key) {
			// Non-credential-shaped keys only: a credential-shaped key is never
			// exported, in name or value (the S44 rule, mirrored).
			continue
		}
		files = append(files, fileFor("settings/"+SafeName(key)+".json", row))
	}
	return files, nil
}

func readMemory(ctx context.Context, r Reader) ([]File, error) {
	rows, err := r.ExportRows(ctx, store.TableSpec{Table: "learning", Columns: store.DeclaredColumns["learning"], OrderBy: "id"})
	if err != nil {
		return nil, err
	}
	var files []File
	for _, row := range rows {
		files = append(files, fileFor("memory/"+SafeName(idVal(row["id"]))+".json", row))
	}
	return files, nil
}

// — row shaping helpers —

// fileFor encodes a shaped record into a File. Encode fails only for
// non-JSON-able values, and rows are JSON-safe scalars from the seam — a
// failure here is a programmer error, named as such.
func fileFor(path string, v any) File {
	b, err := Encode(v)
	if err != nil {
		panic(fmt.Sprintf("export: encoding record %s: %v (rows must be JSON-safe scalars)", path, err))
	}
	return File{Path: path, Bytes: b}
}

// versionedRows orders a versioned table's rows by (id, version) so a
// component's goal versions read in a stable, human order.
func versionedRows(rows []map[string]any) []map[string]any {
	out := append([]map[string]any(nil), rows...)
	sort.Slice(out, func(i, j int) bool {
		a, b := idVal(out[i]["id"]), idVal(out[j]["id"])
		if a != b {
			return a < b
		}
		return idVal(out[i]["version"]) < idVal(out[j]["version"])
	})
	return out
}

// idVal renders a row's id/version as its file-name string (empty when absent,
// which SafeName then renders as "unnamed").
func idVal(v any) string {
	switch n := v.(type) {
	case int64:
		return fmt.Sprint(n)
	case int:
		return fmt.Sprint(n)
	case string:
		return n
	case nil:
		return ""
	default:
		return fmt.Sprint(n)
	}
}

// strVal is the string form of a cell, "" for absent.
func strVal(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// treeID renders a component's parent_id/archived_at: the value when present
// (a real parent, a real archive), nil when absent (a root, a live component)
// — the tree's two nulls, which the file must carry, not drop.
func treeID(v any) any {
	switch n := v.(type) {
	case nil:
		return nil
	case int64:
		if n == 0 {
			return nil
		}
		return n
	case int:
		if n == 0 {
			return nil
		}
		return n
	case string:
		if n == "" {
			return nil
		}
		return n
	default:
		return v
	}
}
