// Package verifyseed ships the `verify.templates` set (S57 VG21, P8): the
// inert candidate gates a person can apply to a component. A template is
// DATA a person copies (a verify_template row does nothing until
// `verify.applyTemplate` copies it), which is why this set is inert —
// applying it changes nothing about what any loop may run.
//
// The spec named `internal/verify/seed` + `internal/seed/decl_verify.go`;
// the shipped shape follows the final seed-set convention instead
// (`watchdog/watchdogseed`, `blueprintseed`): one package owning SetID,
// Source, the applier and the declaration. A package literally named
// `seed` under internal/verify would collide with the engine import in
// daemon/seed.go, and no `internal/seed/decl_*.go` file exists to join.
package verifyseed

import (
	"encoding/json"
	"fmt"

	"github.com/cjlapao/aeon/internal/seed"
)

// SetID is the set's stable name, declared once (SD2).
const SetID = "verify.templates"

// Content is one template's shipped content — the canonical object the
// record hash is taken over and the fingerprint rebuilds from the row's
// columns. `version` is deliberately NOT inside it: the version is
// publication metadata (the engine's ledger tracks applied vs available
// versions), the hash covers the COMMAND the person copies.
type Content struct {
	Stack       string `json:"stack"`
	Name        string `json:"name"`
	Command     string `json:"command"`
	Requirement string `json:"requirement"` // required | advisory
	TimeoutS    int    `json:"timeoutS"`
}

// template is one row of the shipped table: the content plus the
// provenance line the test re-checks against the handler body (VG21:
// "extracted, not invented" — every command below is the command the
// cited agent file actually runs, wrapper included where the body wraps).
type template struct {
	Content
	Agent string // provenance: the shipped agent file that runs this command
}

// templates is the VG21 table, ordered by stack then name. Reading of the
// provenance: golang-engineer-loop.md:59-61, go-architect.md:95,
// verifier-engineer-loop.md:57-62, javascript-engineer-loop.md:58-61,
// react-typescript-engineer-loop.md:59-62, python-engineer-loop.md:58-61,
// rust-engineer-loop.md:59-61, terraform-engineer-loop.md:61-62. Two
// recorded decisions inside the table:
//
//   - go/test wraps `timeout 360` around the design's
//     `-timeout 300s`: the body's doctrine (golang-engineer-loop.md:60)
//     is an INNER test-binary timeout and a strictly larger OUTER
//     `timeout` (its own ladder is 300 outer / 120 inner); the design
//     pins the inner at 300 s (T10.md:29), so the outer takes the next
//     rung, 360, and the daemon's `timeout_s` sits above both at 600 —
//     a hung package is printed by `go test`'s own timeout first, and
//     the daemon's kill never races the shell's.
//   - go/lint ships the MEMORY-BOUNDED golangci-lint form, because the
//     bodies say the plain one is not shippable: the bare command is
//     "routinely OOM-killed on a large module" (go-architect.md:95,
//     golang-engineer-loop.md:61), and verifier-engineer-loop.md:62
//     instructs running exactly this bounded form.
//
// terraform/plan is OMITTED — it needs credentials (S59 EV; VG21 states
// it, and the omission is tested). merge_rule and verify_command rows are
// never seeded (VG21: authority/execution-shaping records, S40 AU1 lock 3).
var templates = []template{
	{Content{"go", "build", "timeout 600 go build ./...", "required", 600}, "verifier-engineer-loop.md"},
	{Content{"go", "lint", "GOGC=50 GOMEMLIMIT=4GiB timeout 600 golangci-lint run --concurrency=4 --timeout=9m ./...", "advisory", 600}, "golang-engineer-loop.md"},
	{Content{"go", "test", "timeout 360 go test ./... -count=1 -timeout 300s", "required", 600}, "golang-engineer-loop.md"},
	{Content{"go", "vet", "go vet ./...", "required", 300}, "go-architect.md"},
	{Content{"node", "build", "timeout 600 npm run build", "required", 600}, "react-typescript-engineer-loop.md"},
	{Content{"node", "lint", "timeout 300 npm run lint", "advisory", 300}, "react-typescript-engineer-loop.md"},
	{Content{"node", "test", "CI=true timeout 300 npm run test -- --run", "required", 600}, "javascript-engineer-loop.md"},
	{Content{"python", "lint", "ruff check .", "advisory", 120}, "python-engineer-loop.md"},
	{Content{"python", "test", "timeout 300 pytest -q", "required", 300}, "python-engineer-loop.md"},
	{Content{"rust", "build", "timeout 600 cargo build --all-targets", "required", 600}, "rust-engineer-loop.md"},
	{Content{"rust", "lint", "timeout 300 cargo clippy --all-targets --all-features -- -D warnings", "advisory", 300}, "rust-engineer-loop.md"},
	{Content{"rust", "test", "timeout 600 cargo test --all-features", "required", 600}, "rust-engineer-loop.md"},
	{Content{"terraform", "validate", "terraform validate", "required", 120}, "terraform-engineer-loop.md"},
}

// Key is the record key a template is stable under: "stack/name", which
// is also the row's id. The pair is what the table's UNIQUE index is on
// (live rows), so the seed key and the domain key are the same thing.
func (c Content) Key() string { return c.Stack + "/" + c.Name }

// Version is the record's published version (the golden ritual of
// versions.go decides it; new content is 1).
func (t template) Version() int { return recordVersion(t.Key()) }

// Source returns the set's records (SD2, rule 3: never nil, one per
// shipped template, versions from the golden ritual).
func Source() []seed.Record {
	recs := make([]seed.Record, 0, len(templates))
	for _, t := range templates {
		b, err := json.Marshal(t.Content)
		if err != nil {
			// Structurally impossible (plain string/int fields); a panic
			// here means the Content shape was broken, not that a machine
			// failed — the init-time stance every seed source takes.
			panic("verifyseed: content will not marshal: " + err.Error())
		}
		recs = append(recs, seed.Record{
			Key:     t.Key(),
			Version: t.Version(),
			Content: b,
		})
	}
	return recs
}

// ContentOf decodes a record's content back through the one shape. An
// applier that could not read its own set's record must refuse, not
// guess (the toolseed precedent).
func ContentOf(r seed.Record) (Content, error) {
	var c Content
	if err := json.Unmarshal(r.Content, &c); err != nil {
		return Content{}, fmt.Errorf("verifyseed: record %q content will not decode: %w", r.Key, err)
	}
	return c, nil
}

// contentFromRow rebuilds the record content from a row's columns — every
// column decoded to the same Go shape Source encoded, so the fingerprint
// of an untouched row equals the shipped hash (the toolseed drift rule:
// a hash that flips between two encodings reports divergence for a row
// nobody touched).
func contentFromRow(stack, name, command, requirement string, timeoutS int) Content {
	return Content{Stack: stack, Name: name, Command: command, Requirement: requirement, TimeoutS: timeoutS}
}
