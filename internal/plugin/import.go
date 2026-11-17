// Package plugin is S15 — agents we did not write.
//
// The whole slice rests on one sentence: **import → security check → enable,
// three steps, never two** (AP2). This file is the first step, and its shape
// is the argument: the import path CANNOT name the running state. There is
// no construction, no field, no default by which an arriving agent can be
// anything but not-running. A type that cannot express the mistake is
// cheaper than a test that notices it.
//
// A package is a directory with an ALLOW-LISTED file set — `agent.md` (front
// matter + prompt), `card.json` (S14's SignedCard envelope), and optional
// `policies/*.json` (denials; T2 judges them). Anything else is refused,
// which is the honest form of the format's promise, "no executable code":
// the admission list admits nothing unnamed. That promise is what makes
// "no sandbox" defensible (design, Out of scope) — an imported agent runs no
// code of its own, so its tools are the sandbox, and its tools are granted
// by policy (MC4), not by arrival.
package plugin

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/cjlapao/aeon/internal/handler"
	"github.com/cjlapao/aeon/internal/policy"
	"github.com/cjlapao/aeon/internal/runtime"
	"gopkg.in/yaml.v3"
)

// Requests is what the package DECLARES it wants. Under MC4 a declaration is
// a request and nothing more: an agent has no capability until an operator
// authors a policy (T4 renders these as requests, and T4 writes no policy).
type Requests struct {
	Tools  []string
	Skills []string
	Routes []string
}

// Agent is an imported agent — the same object a first-party body is
// (AP1/Q4: one registry view, the same handler.Build dispatch path; the
// difference is provenance and labelling, not location).
type Agent struct {
	Name        string
	Description string
	State       State
	Requests    Requests
	Prompt      string
	Card        *CardInfo
	Source      string
	ImportedAt  time.Time
	FirstParty  bool // set by the REGISTRY — an import sets false and only false (AP9; T6 completes provenance)
	// Carried holds the package's own DENIALS, accepted after the authorship
	// boundary forced their source to import (check.go). They narrow; the
	// CLI (T7) persists them through policy.Store.Add so self-restriction
	// reaches the set the use gate decides with.
	Carried []policy.Policy
	// ClaimedFirstParty records that the package ASKED to be first-party.
	// The claim changes nothing here; T6 reports it, because a package that
	// tried is worth knowing about.
	ClaimedFirstParty bool
}

// Reject is a refusal: the thing refused and the reason, discriminated from
// infrastructure failure (DB19). The registry refuses; `error` is for disks.
type Reject struct {
	Field  string
	Detail string
}

func (r *Reject) Error() string { return r.Detail }

func refuse(field, format string, args ...any) *Reject {
	return &Reject{Field: field, Detail: fmt.Sprintf(format, args...)}
}

// imported is the process registry of arriving agents. Persistence (the
// records that survive restarts, the transitions, the reasons) is T3's
// store table; this map is what makes an import visible to the registry
// WITHIN the import's process, which is what AP1's tests observe.
var (
	mu       sync.Mutex
	imported = map[string]Agent{}
)

// Import reads a package directory, verifies its card, and returns the
// agent — disabled. It never enables, never fetches, never runs (AP2, AP10).
//
// The order is the check's order of trust: the PATH is decided before the
// disk is touched (a scheme is refused without a filesystem lookup); then
// every file is admitted or the package is refused (the format's promise,
// enforced by an allow-list rather than an executable-detector); then the
// card is verified (AS2) — a card that cannot attest is not a partial
// import; then the manifest, whose name the card must match: the signature
// attests a NAME, and a manifest wearing another agent's verified card is
// the first shape an impostor takes.
func Import(pkgPath string) (*Agent, *Reject) {
	return ImportChecked(pkgPath, nil, time.Now().UTC())
}

// ImportChecked is the ONE public door — the three steps, never skippable:
// read and verify (this file), check as a policy evaluation (check.go), and
// ONLY THEN register. A refusal at any step leaves the registry exactly as
// it was: no half-installed agent with some skills landed. `ps` is the
// operator's current policy set, read by the caller (the CLI, T7) and never
// by this package — plugin does not open stores; it answers what the rules
// already say.
func ImportChecked(pkgPath string, ps []policy.Policy, at time.Time) (*Agent, *Reject) {
	a, rj := readPackage(pkgPath)
	if rj != nil {
		return nil, rj
	}
	carried, rj := ReadCarriedPolicies(pkgPath)
	if rj != nil {
		return nil, rj
	}
	if rj := CheckRequests(a, ps, at); rj != nil {
		return nil, rj
	}
	a.Carried = carried
	// The durable half of arrival (when a store is bound): the new record
	// — which history does NOT overwrite, so a re-import after revocation
	// is a NEW record and "somebody put this back" is visible — and its
	// (none) → disabled transition, recorded like every other move.
	if rj := recordImport(a); rj != nil {
		return nil, rj
	}
	mu.Lock()
	imported[a.Name] = *a
	mu.Unlock()
	return a, nil
}

// Rehydrate puts an ALREADY-INSTALLED package into this process's hands —
// the CLI's verb process opens the ledger, finds the row, and must read the
// package before it can enable (an unread prompt must never run; that is
// why enable refuses without it). It is deliberately NOT an import: it
// writes no record and no transition (a re-read of a disk is not an event
// in a life), it cannot create anything (no row → refused, "import first"),
// and its state comes from the DURABLE record — including revoked, which
// stays terminal and refuses enable like any other transition the table
// forbids. AP2 holds here by the same construction: Rehydrate reaches no
// state of its own; the rows say what the rows say.
func Rehydrate(pkgPath string, ps []policy.Policy, at time.Time) (*Agent, *Reject) {
	if stateStore == nil {
		return nil, refuse("ledger", "refused: no ledger is wired — Rehydrate speaks from the rows, and there are no rows to speak from")
	}
	a, rj := readPackage(pkgPath)
	if rj != nil {
		return nil, rj
	}
	st, found := stateStore.StateOf(a.Name)
	if !found {
		return nil, refuse("state", "refused: %s has no record — rehydration is not installation (import writes the record; this only reads the package) · remedy: aeon agent import %s", a.Name, pkgPath)
	}
	carried, rj := ReadCarriedPolicies(pkgPath)
	if rj != nil {
		return nil, rj
	}
	// The check runs again on the re-read (the disk may have changed since
	// the import — a package that grew a tool request after install is
	// caught HERE, before a verb trusts it; AP3's check is not a one-time
	// courtesy).
	a.Carried = carried
	if rj := CheckRequests(a, ps, at); rj != nil {
		return nil, rj
	}
	a.State = st
	mu.Lock()
	imported[a.Name] = *a
	mu.Unlock()
	return a, nil
}

// PackageClaimsFirstParty reads whether the package at pkgPath ASKED to be
// first-party — for `aeon agent list --provenance`, which renders from the
// rows and cannot see the in-memory claim of a process that has since
// exited. The claim is honoured by nothing (AP9); this is its report.
func PackageClaimsFirstParty(pkgPath string) (bool, *Reject) {
	m, rj := readManifest(filepath.Join(pkgPath, "agent.md"))
	if rj != nil {
		return false, rj
	}
	return m.firstPartySeen, nil
}

// ReadPackagePreview is readPackage, exported — the read-only first step of
// ImportChecked, with no change to its body. It reads the directory,
// admits or refuses every file and verifies the card; it registers NOTHING
// and writes NO row. Named Preview, not Read, so a caller reaching for it
// understands it performs no registration and no store write (mirroring
// Rehydrate's naming discipline: a read that is not an import says so).
// Callers: cmd/aeon only — TestNoUnconditionalImportOfPlugin forbids any
// other package from importing this one.
func ReadPackagePreview(pkgPath string) (*Agent, *Reject) { return readPackage(pkgPath) }

// readPackage is step one: admit or refuse every file, parse the manifest,
// verify the card (AS2). It registers NOTHING — registration is the reward
// for surviving all three steps.
func readPackage(pkgPath string) (*Agent, *Reject) {
	if strings.Contains(pkgPath, "://") {
		return nil, refuse("path", "refused: import takes a local path — nothing is fetched (AP10) · remedy: fetch it yourself if you must, and import the directory you fetched")
	}
	info, err := os.Stat(pkgPath)
	if err != nil || !info.IsDir() {
		return nil, refuse("path", "refused: %s · an agent package is a directory that exists · remedy: point import at the directory, not a URL and not a guess", pkgPath)
	}

	// The allow-list, walked before any file is read.
	var sawManifest, sawCard bool
	err = filepath.WalkDir(pkgPath, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			return refuse("file", "refused: %s · a package is plain files; a symlink can name anything · remedy: replace the link with the file it should have been", rel(p, pkgPath))
		}
		relName := rel(p, pkgPath)
		if !admitted(relName) {
			return refuse("file", "refused: %s · an agent package carries a prompt (agent.md), a signed card (card.json) and denials (policies/*.json) — nothing else (no executable code: the format admits nothing unnamed, which is what makes \"no sandbox\" defensible) · remedy: remove %s and import again", relName, relName)
		}
		raw, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		fi, err := d.Info()
		if err != nil {
			return err
		}
		if fi.Mode()&0o111 != 0 {
			return refuse("file", "refused: %s · the exec bit means executable code by the plainest definition, and a package carries none (AP10's format promise) · remedy: chmod -x it, and remember: it still does not belong here", relName)
		}
		if !utf8.Valid(raw) {
			return refuse("file", "refused: %s · not UTF-8 text, so not a prompt, a card or a denial — the executable-content class it is · remedy: remove %s", relName, relName)
		}
		switch relName {
		case "agent.md":
			sawManifest = true
		case "card.json":
			sawCard = true
		}
		return nil
	})
	if err != nil {
		if rj, ok := err.(*Reject); ok {
			return nil, rj
		}
		return nil, refuse("path", "refused: reading %s: %v", pkgPath, err)
	}
	if !sawManifest {
		return nil, refuse("agent.md", "refused: %s · a package without its prompt (agent.md) is not an agent", pkgPath)
	}
	if !sawCard {
		return nil, refuse("card.json", "refused: %s · a package without its card is not importable — the card is what AS2 attests", pkgPath)
	}

	manifest, rj := readManifest(filepath.Join(pkgPath, "agent.md"))
	if rj != nil {
		return nil, rj
	}

	// AS2 at the door: the card verifies or the package does not import.
	// LoadCardFile verifies signature first, then the claim, then shape —
	// and the claim is checked against the MANIFEST's name, so a verified
	// card cannot be worn by a different agent.
	if cardLoader == nil {
		// The safe reading (AS12's discipline applied to attestation): no
		// verifier wired means NO card can be verified, and an unverifiable
		// card is refused — never trusted-by-default, never skipped.
		return nil, refuse("card.json", "refused: %s · no card verifier is wired in this binary — an unverifiable card is refused, and that is the verification working (AP8)", pkgPath)
	}
	card, err := cardLoader(filepath.Join(pkgPath, "card.json"), manifest.Name)
	if err != nil {
		return nil, refuse("card.json", "refused: %v · remedy: the card is the callee's own signature over its own card — re-export the package from whoever signs it (a warning would scroll past; this is a refusal)", err)
	}

	a := Agent{
		Name:              manifest.Name,
		Description:       manifest.Description,
		State:             StateDisabled, // the ONLY state import can leave (AP2)
		Requests:          manifest.Requests,
		Prompt:            manifest.Prompt,
		Card:              card,
		Source:            pkgPath,
		ImportedAt:        time.Now().UTC(),
		FirstParty:        false, // set by the registry; an import sets false and only false (AP9)
		ClaimedFirstParty: manifest.firstPartySeen,
	}
	return &a, nil
}

// Agents is the ONE registry view (AP1/Q4): the 25 first-party bodies and
// every imported agent, side by side, distinguished only by provenance.
func Agents() []Agent {
	mu.Lock()
	defer mu.Unlock()
	out := make([]Agent, 0, len(imported)+len(handler.RegisteredBodies()))
	for _, name := range handler.RegisteredBodies() {
		out = append(out, Agent{Name: name, FirstParty: true, State: "running"}) // first-party bodies run when their loop dispatches them; the plugin state machine governs imported agents only
	}
	for _, a := range imported {
		out = append(out, a)
	}
	return out
}

// Lookup resolves an agent by name across the one view.
func Lookup(name string) (Agent, bool) {
	for _, a := range Agents() {
		if a.Name == name {
			return a, true
		}
	}
	return Agent{}, false
}

// Reset drops the in-process registry. The daemon does not call it — it
// exists for tests and for the level-0 proof (T8), where "no imported
// agents" must be expressible as a state, not a flag.
func Reset() {
	mu.Lock()
	imported = map[string]Agent{}
	mu.Unlock()
}

// --- package reading -----------------------------------------------------

type manifest struct {
	Name           string   `yaml:"name"`
	Description    string   `yaml:"description"`
	Requests       Requests `yaml:"requests"`
	firstPartySeen bool
	Prompt         string `yaml:"-"`
}

func readManifest(path string) (*manifest, *Reject) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, refuse("agent.md", "refused: reading agent.md: %v", err)
	}
	text := string(raw)
	const fence = "---"
	if !strings.HasPrefix(text, fence+"\n") {
		return nil, refuse("agent.md", "refused: agent.md · no front matter — the same shape a skill's SKILL.md uses, mirrored because that is how capability arrives here (Q1)")
	}
	rest := text[len(fence)+1:]
	end := strings.Index(rest, "\n"+fence)
	if end < 0 {
		return nil, refuse("agent.md", "refused: agent.md · front matter never closes")
	}
	head := rest[:end]
	m := &manifest{}
	var probe map[string]any
	if err := yaml.Unmarshal([]byte(head), &probe); err != nil {
		return nil, refuse("agent.md", "refused: agent.md front matter is not valid YAML: %v", err)
	}
	// first_party is read ONLY to record the attempt. The claim changes
	// nothing (AP9); nothing here unmarshals it into the field that decides
	// labelling (T6's grep pins that the field never rides package input).
	for k := range probe {
		if k == "first_party" || k == "firstParty" {
			m.firstPartySeen = true
		}
	}
	if err := yaml.Unmarshal([]byte(head), m); err != nil {
		return nil, refuse("agent.md", "refused: agent.md front matter does not parse into a manifest: %v", err)
	}
	if strings.TrimSpace(m.Name) == "" {
		return nil, refuse("agent.md", "refused: name · an agent must say who it is — the card attests a name and the manifest must carry the same one")
	}
	m.Prompt = strings.TrimSpace(rest[end+len("\n"+fence):])
	if m.Prompt == "" {
		return nil, refuse("agent.md", "refused: %s · an agent whose body is empty has nothing to be (AP1: prompt, card, declarations — that is the whole of it)", m.Name)
	}
	return m, nil
}

func admitted(relName string) bool {
	switch relName {
	case "agent.md", "card.json":
		return true
	}
	if strings.HasPrefix(relName, "policies/") {
		rest := strings.TrimPrefix(relName, "policies/")
		// One flat directory of .json denials — no nesting that could hide
		// anything, no other extension that could mean code.
		return !strings.Contains(rest, "/") && strings.HasSuffix(rest, ".json")
	}
	return false
}

// CardInfo is the plugin's own view of a verified card — deliberately NOT
// the agentsvc type: the absent-layer sweep (agentsvc's own S14 T8 test)
// forbids this package from importing the agent-services layer, so the card
// arrives through an INJECTED loader exactly like SetFirstParty: one
// production wiring (cmd/aeon, delegated to agentsvc.LoadCardFile — the one
// implementation, AS2), pinned to one call at the entry point (T7). The
// fields kept are the ones consent, provenance and the check actually read.
type CardInfo struct {
	Name        string
	Description string
	Version     string
	Skills      []CardSkill
	// Slug travels with the card and is the reference key the import keys on;
	// it is never regenerated on import (unlike a brand-new agent made from the
	// UI). Empty on a card that predates S43 (the import derives it from Name).
	Slug string
	// Functionality is the card's free-text "what it does" (AM3).
	Functionality string
	// Metadata is the card's open-ended JSON blob (AM3, the operator's own word).
	Metadata string
	// The avatar the settings list shows at rest (AM3): a static flavour
	// default, never a live runtime state (a named non-goal in the brief).
	AvatarType  string
	AvatarColor string
	AvatarMood  string
	// Signature and Certificate are the AM3 envelope: an Ed25519 signature
	// over the card's own bytes, verified against Certificate's X.509 public
	// key. Unsigned is a valid state only behind the allowUnsignedImport
	// override, and then the import marks the row `unsigned`.
	Signature   string
	Certificate string
}

// CardSkill is one attested skill — A2A addresses by id within the card.
type CardSkill struct {
	ID          string
	Name        string
	Description string
}

// cardLoader verifies a card file for the named agent, or returns the
// refusal reason. nil — the unwired default — verifies NOTHING: every card
// becomes unverifiable, which is the safe refusal, never a silent pass.
var cardLoader func(path, expectedAgent string) (*CardInfo, error)

// SetCardLoader injects the verifier (the entry point wires it once,
// delegating to the ONE card implementation; a test binds its own).
func SetCardLoader(fn func(path, expectedAgent string) (*CardInfo, error)) { cardLoader = fn }

func rel(p, root string) string {
	r, err := filepath.Rel(root, p)
	if err != nil {
		return p
	}
	return filepath.ToSlash(r)
}

// Dispatch builds the step handler for an imported agent on the SAME
// primitive handler.Build builds on — one runner for first-party and
// imported alike (AP1). Tools come from the default grant, never from the
// package's declarations: a declaration is a request (MC4), and the use-time
// policy gate is what turns a request into a capability. The handler here is
// what runs WHEN the state machine (T3/T5) says the agent may run at all.
func Dispatch(name string) (runtime.StepHandler, bool) {
	a, ok := Lookup(name)
	if !ok || a.FirstParty {
		return runtime.StepHandler{}, false
	}
	step := runtime.DefaultHandler(a.Prompt, runtime.DefaultGrantNames())
	step.Name = a.Name
	return step, true
}
