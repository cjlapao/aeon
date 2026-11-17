package main

// spawn_guard_test.go — S59 T6: EV1 and EV5, made mechanical.
//
// The sentence this file enforces is short: **no process a daemon starts
// inherits the daemon's environment.** The reason it needs a guard rather than
// a convention is that the failure is silent in both directions — a `exec.Cmd`
// with a nil `Env` runs with everything the daemon holds, prints nothing
// unusual, and works. The first anyone hears about it is when a cloned
// repository, a `git` subprocess, or a child script reads `ANTHROPIC_API_KEY`
// out of its own environment and sends it somewhere.
//
// So the guards are about the two ways that happens:
//
//   - a WHOLESALE read (`os.Environ()`) reaching a child;
//   - a spawn site that sets no environment at all (nil `Env` means "inherit",
//     which is the same leak wearing no import).
//
// Both are checked over every non-test Go file in `internal/` and `cmd/` —
// measured 763 files on 2026-11-03 — and the walks assert how many files they
// read, because a guard that walked an empty tree has been green for years in
// this family before.
//
// The allow-lists below are not exemptions handed out for convenience. Each
// names a file, the reason its wholesale read is the boundary rather than a
// leak, and is asserted to still contain the read it claims (a stale entry is
// as much a lie as an undocumented one).

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/protocol"
)

// wholesaleEnvReaders is the closed set of non-test files that read the
// process environment wholesale — the only files allowed to ask the OS what
// the daemon's environment holds. Being here is not permission to hand the
// answer to a child: `directInheritance` refuses that everywhere, with no
// exemption, because it is the leak itself.
var wholesaleEnvReaders = map[string]string{
	"internal/daemon/execenv_wire.go":   "EV5's boundary itself: it reads the daemon's environment AS DATA (names through one injected reader) to compose a child's. This is the one place the ambient environment is allowed to be seen, so that every other place does not have to look at it.",
	"internal/workspace/git.go":         "T2's git plumbing: it enumerates NAMES and hands them to execenv.GitEnv, which returns an allowlist. The daemon's own git, D9's SSH_AUTH_SOCK included.",
	"internal/skills/install.go":        "S59 T6: the skills clone reads NAMES only and hands them to execenv.GitEnv too. Before T6 it inherited everything — the finding that made this guard worth writing.",
	"internal/verify/runner.go":         "S57's gate runner: tools.GateEnv(os.Environ()) is the scrubbed gate allowlist (VG5), byte-pinned by S57. It is a scrub, not an inheritance, and it is recorded here rather than grandfathered silently.",
	"internal/terminal/session_unix.go": "The interactive terminal is the person's own shell behind a pty (no model on the other end, T1's golden environment pins it byte for byte). EV1's scope is every spawn a MODEL can reach; this one is recorded as the deliberate exception it is, not slipped past.",
	"cmd/aeon/blueprint_edit.go":        "S59 post-merge: `aeon blueprint edit` reads the CLI's own environment to hand the NAMES to execenv.EditorEnv, which returns a closed allowlist. The verb is a client of the socket, not the daemon; it reads the environment to filter it, which is the act this map records.",
}

// spawnSiteFiles is the closed list of non-test files that start a process.
// A new one is not forbidden by taste — it is forbidden until it is argued
// into this map with what its child's environment is, which is the review the
// slice asks for.
var spawnSiteFiles = map[string]string{
	// S59 T9: the container driver's engine client. Three sites, one story: the
	// process started is the docker/podman CLI, never the loop's command; the
	// command goes to the engine over its API, and the container's environment is
	// the resolved execenv slice, handed over as NAME=value in THIS client
	// process's environment while argv carries the names only (EV11). The client
	// itself gets two names — PATH and HOME, which the CLI needs to find and talk
	// to a daemon — and never the daemon's own environment, which is why
	// cliProcessEnv builds a list instead of inheriting (EV10).
	"internal/execenv/container/cli_runtime.go": "the engine client (the docker/podman CLI): its child's environment is the two-name cliProcessEnv plus the declared NAME=value pairs, never os.Environ; the loop's command runs inside the container under the resolved slice, and the ledger is written ahead of every one of these three calls (EV10, EV11, EV12).",
	"internal/tools/spawn.go":           "the model-reachable spawn: its child's environment is the resolved execenv.ChildEnv (EV5) and it refuses to run without one; the `Env` field that remains is the gate runner's scrub, and nothing else.",
	"internal/git/plumbing.go":          "git under execenv.GitEnv (T2), with the hardening block appended.",
	"internal/workspace/git.go":         "the app's git reads, same allowlist, ui-read kind.",
	"internal/skills/install.go":        "the skills clone, git allowlist since S59 T6.",
	"internal/mcp/client.go":            "an MCP server child, started with the environment the S46 seam assembled (cmd.Env = env), not the daemon's.",
	"internal/terminal/session_unix.go": "the person's shell, behind terminalEnv (the documented exception above).",
	"cmd/aeon/blueprint_edit.go":        "the person's own editor, launched by `aeon blueprint edit` (S48's verb): cmd.Env = execenv.EditorEnv(os.Environ()), the closed editor allowlist — never the caller's whole environment, which is where a person's tokens live.",
}

// --- the predicates ----------------------------------------------------------
//
// Each is a function over a source STRING, never over `t`, so the canary test
// can feed it the violation and see it come back named. A predicate welded to
// its own reporting cannot be shown to work, and a guard nobody can break is a
// green light.

func wholesaleEnvReadLines(src string) []string {
	var hits []string
	for i, line := range strings.Split(src, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "//") {
			continue // the comment that explains the rule is not an instance of it
		}
		if strings.Contains(line, "os.Environ()") {
			hits = append(hits, fmt.Sprintf("line %d: %s", i+1, trimmed))
		}
	}
	return hits
}

// directInheritance is the leak with no import to blame: an assignment that
// puts the daemon's own environment onto a child. There is no allow-list here
// and there never will be one — every file in `wholesaleEnvReaders` reads the
// environment to FILTER it, which is a different act.
var directInheritanceRe = regexp.MustCompile(`(\.Env\s*=|Env:\s*)[^#\n]*os\.Environ\(\)`)

// sanitizedEnvWrappers are the three shapes where an environment assignment
// that mentions os.Environ() is a SCRUB rather than an inheritance — each a
// named function that returns an allowlist, each with a task behind it. A new
// assignment spelling that is not one of these fires the guard, which is the
// point: "it is scrubbed, trust me" is exactly what an unscrubbed child looks
// like from the outside.
var sanitizedEnvWrappers = map[string]string{
	"terminalEnv(os.Environ())":       "S59-recorded: the person's own pty (T1's byte-pinned golden environment), not a model-reachable spawn.",
	"tools.GateEnv(os.Environ())":     "S57 VG5: the gate's closed allowlist, byte-pinned by S57's own test.",
	"execenv.EditorEnv(os.Environ())": "S59 post-merge (the `aeon blueprint edit` site S48 added): execenv.EditorEnv returns the closed editor allowlist — DefaultPassthrough plus the editor names — and drops anything the deny list refuses; pinned by internal/execenv/editor_test.go.",
}

// inheritanceIsSanitized reports whether every os.Environ() on the line passes
// through one of the named scrubbers.
func inheritanceIsSanitized(hit string) bool {
	for wrapper := range sanitizedEnvWrappers {
		if strings.Contains(hit, wrapper) {
			return true
		}
	}
	return false
}

func directInheritance(src string) []string {
	var hits []string
	for i, line := range strings.Split(src, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "//") {
			continue
		}
		if directInheritanceRe.MatchString(line) {
			hits = append(hits, fmt.Sprintf("line %d: %s", i+1, strings.TrimSpace(line)))
		}
	}
	return hits
}

var spawnSiteRe = regexp.MustCompile(`exec\.Command(Context)?\(`)

func spawnSiteLines(src string) []string {
	var hits []string
	for i, line := range strings.Split(src, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "//") {
			continue
		}
		if spawnSiteRe.MatchString(line) {
			hits = append(hits, fmt.Sprintf("line %d: %s", i+1, strings.TrimSpace(line)))
		}
	}
	return hits
}

var setsEnvRe = regexp.MustCompile(`\.Env\s*=|Env:\s*[^\s,)]`)

func sourceSetsAnEnvironment(src string) bool {
	for _, line := range strings.Split(src, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "//") {
			continue
		}
		if setsEnvRe.MatchString(line) {
			return true
		}
	}
	return false
}

// walkSources walks every non-test .go file under roots and hands each one's
// text to `visit` with its repo-relative path. It returns how many files it
// read, which every caller asserts.
func walkSources(t *testing.T, roots []string, visit func(rel, text string)) int {
	t.Helper()
	var walked int
	for _, root := range roots {
		err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return nil // an unreadable directory holds no Go code we own
			}
			if d.IsDir() {
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			b, rerr := os.ReadFile(path)
			if rerr != nil {
				return nil
			}
			walked++
			visit(filepath.ToSlash(path), string(b))
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return walked
}

func relPath(path string) string {
	// The walks start at ../../internal and ../../cmd; the guard reports paths
	// the way a reader opens them — relative to aeon/.
	if i := strings.Index(path, "internal/"); i >= 0 {
		return path[i:]
	}
	if i := strings.Index(path, "cmd/"); i >= 0 {
		return path[i:]
	}
	return path
}

// --- the guards --------------------------------------------------------------

// TestNoSpawnInheritsTheDaemonEnvironment is EV1 and EV5 over the whole tree:
// a wholesale environment read only in the five files that compose or scrub an
// environment, never handed to a child as-is anywhere at all, and every process
// this binary starts in the closed list of spawn sites that each account for
// their child's environment.
func TestNoSpawnInheritsTheDaemonEnvironment(t *testing.T) {
	roots := []string{filepath.Join("..", "..", "internal"), filepath.Join("..", "..", "cmd")}
	foundReaders := map[string]bool{}
	foundSpawns := map[string][]string{}

	// The floor is measured, not hoped for: 763 non-test files on 2026-11-03.
	// A walk that comes back thin has lost a directory, and a lost directory is
	// a guard that has stopped guarding without anyone watching it stop.
	n := walkSources(t, roots, func(rel, text string) {
		rel = relPath(rel)
		for _, hit := range directInheritance(text) {
			if inheritanceIsSanitized(hit) {
				continue
			}
			t.Errorf("%s hands the daemon's own environment to a child: %s. Compose the child's environment through execenv (EV5) — a nil or inherited Env is the leak this slice exists to close (EV1)", rel, hit)
		}
		if hits := wholesaleEnvReadLines(text); len(hits) != 0 {
			foundReaders[rel] = true
			why, ok := wholesaleEnvReaders[rel]
			if !ok {
				t.Errorf("%s reads the process environment wholesale (%s) and is not in the closed list — the only sanctioned readers are the ones that compose or scrub an environment. What does this file need from the daemon's environment, and could it come through an injected reader instead? (EV5)",
					rel, strings.Join(hits, " | "))
			} else if why == "" {
				t.Errorf("%s is allow-listed with no reason; an undocumented exemption is an exemption nobody reviewed", rel)
			}
		}
		if hits := spawnSiteLines(text); len(hits) != 0 {
			foundSpawns[rel] = hits
		}
	})
	if n < 700 {
		t.Fatalf("the walk read %d non-test files, want the ~763 measured on 2026-11-03 — a thin walk is a green light, not a guard", n)
	}

	// Every allow-listed reader must still be reading. A stale entry reads as
	// "five files legitimately see the environment" forever after the fifth
	// stopped, which is how an allow-list becomes fiction.
	for rel := range wholesaleEnvReaders {
		if !foundReaders[rel] {
			b, err := os.ReadFile(filepath.Join("..", "..", rel))
			if err != nil {
				t.Errorf("the allow-list names %s, which cannot be read: %v", rel, err)
				continue
			}
			t.Errorf("the allow-list names %s, which no longer reads os.Environ() — drop the entry; an allow-list of files that do not need it is how a leak gets a precedent", rel)
			_ = b
		}
	}

	// The spawn list, both directions: a new site is unaccounted for, and a
	// listed site that stopped spawning is a stale claim.
	for rel, hits := range foundSpawns {
		why, ok := spawnSiteFiles[rel]
		if !ok {
			t.Errorf("%s starts a process (%s) and is not in the closed spawn list — say what the child runs with, in this slice's terms, before it runs at all (EV1)",
				rel, strings.Join(hits, " | "))
			continue
		}
		if !sourceSetsAnEnvironment(readPlaneSource(t, rel)) {
			t.Errorf("%s starts a process (%s) and never sets an environment on it — a nil Env is the daemon's own environment. Account for it in code, not in this map (%s)", rel, strings.Join(hits, " | "), why)
		}
	}
	for rel := range spawnSiteFiles {
		if _, ok := foundSpawns[rel]; !ok {
			t.Errorf("the spawn list names %s, which starts no process any more — remove the entry so the list keeps meaning what it says", rel)
		}
	}
	if len(foundSpawns) < 5 {
		t.Errorf("only %d spawn sites found; the tree starts more processes than that and the pattern has stopped matching", len(foundSpawns))
	}
}

// readPlaneSource reads a file relative to aeon/ WITHOUT the comment-stripping
// the shared `readSource` helper does: these guards must see the comment that
// explains a rule (a comment is evidence), while their predicates skip comment
// lines when looking for an instance of it.
func readPlaneSource(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", rel))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return string(b)
}

// TestSpawnSiteGuardHasTeeth feeds each predicate the violation and demands it
// come back named — including the one violation the allow-list exists to make
// look legal, which is the whole reason the two predicates are separate.
func TestSpawnSiteGuardHasTeeth(t *testing.T) {
	canary := "package x\n\nimport \"os\"\nimport \"os/exec\"\n\nfunc f() {\n\tcmd := exec.Command(\"sh\")\n\tcmd.Env = os.Environ()\n}\n"
	if len(directInheritance(canary)) != 1 {
		t.Fatalf("a child handed os.Environ() directly was not flagged — the guard that catches the actual leak cannot fail")
	}
	if len(wholesaleEnvReadLines(canary)) != 1 {
		t.Fatalf("a wholesale environment read was not counted — the allow-list guard would pass on any file")
	}
	if len(spawnSiteLines(canary)) != 1 {
		t.Fatalf("a spawn site was not found — the closed spawn list is checked against nothing")
	}
	if sourceSetsAnEnvironment(canary) != true {
		t.Fatal("a file that sets cmd.Env was reported as not setting one")
	}
	// The complement, which is what makes the first three assertions mean
	// something: the sanctioned shapes must NOT fire.
	// A SANCTIONED scrub passes; an unnamed one does not. Both halves matter:
	// the first keeps the two recorded cases from being re-litigated, the second
	// is the guard, and a `sanitize(...)` nobody named must not pass by looking
	// like one that was.
	sanctioned := "package x\n\nclass f { }\nfunc g() { cmd.Env = tools.GateEnv(os.Environ()) }\n"
	sanctionedHits := directInheritance(sanctioned)
	if len(sanctionedHits) != 1 || !inheritanceIsSanitized(sanctionedHits[0]) {
		t.Errorf("tools.GateEnv(os.Environ()) was not recognised as the recorded scrub it is: %v", sanctionedHits)
	}
	// The third recorded scrub, added when this guard caught S48's editor site
	// in the post-merge run: the same assertion in both directions, because a
	// map that grew by one entry has to be shown to still be closed.
	editorSanctioned := "package x\n\nfunc g() { cmd.Env = execenv.EditorEnv(os.Environ()) }\n"
	if hits := directInheritance(editorSanctioned); len(hits) != 1 || !inheritanceIsSanitized(hits[0]) {
		t.Errorf("execenv.EditorEnv(os.Environ()) was not recognised as the recorded scrub it is: %v", hits)
	}
	editorLookalike := "package x\n\nfunc g() { cmd.Env = execenv.EditorishEnv(os.Environ()) }\n"
	if hits := directInheritance(editorLookalike); len(hits) != 1 || inheritanceIsSanitized(hits[0]) {
		t.Errorf("a name only close to the editor scrub passed the guard: %v — the sanctioned set is closed, not a prefix match", hits)
	}
	unnamed := "package x\n\nfunc g() { cmd.Env = myScrub(os.Environ()) }\n"
	if hits := directInheritance(unnamed); len(hits) != 1 || inheritanceIsSanitized(hits[0]) {
		t.Errorf("an unnamed scrubber passed the guard: %v — an unscrubbed child looks exactly like this", hits)
	}
	plainAssign := "package x\n\nfunc g() { cmd.Env = os.Environ() }\n"
	if hits := directInheritance(plainAssign); len(hits) != 1 || inheritanceIsSanitized(hits[0]) {
		t.Errorf("a bare cmd.Env = os.Environ() was not refused outright: %v", hits)
	}
	plain := "package x\n\nimport \"os/exec\"\n\nfunc f() *os.Process { return exec.Command(\"git\").Process }\n"
	if sourceSetsAnEnvironment(plain) {
		t.Error("a file that sets no environment was reported as setting one — every spawn site would pass")
	}
	if len(spawnSiteLines(plain)) != 1 {
		t.Error("exec.Command without the Context suffix was not found")
	}
	if _, err := execCommandRegexSanity(); err != nil {
		t.Fatal(err)
	}
}

// execCommandRegexSanity pins the two spawn spellings apart, so a future edit
// to `spawnSiteRe` that matches only one of them is caught here rather than by
// a process starting unguarded.
func execCommandRegexSanity() (bool, error) {
	if len(spawnSiteLines("exec.Command(")) != 1 || len(spawnSiteLines("exec.CommandContext(")) != 1 {
		return false, fmt.Errorf("spawnSiteRe does not match both spellings of the spawn")
	}
	return true, nil
}

// TestOnlyTestsImportEnvtest keeps the test harness where it belongs. The
// envtest package hands a caller a fake daemon environment on a plate; a
// production file importing it is a file that has stopped asking what its
// child's environment actually is, and a guard that only reads production
// source cannot see the difference between a fake and a real one.
func TestOnlyTestsImportEnvtest(t *testing.T) {
	path := "github.com/cjlapao/aeon/internal/execenv/envtest"
	var offenders []string
	n := walkSources(t,
		[]string{filepath.Join("..", "..", "internal"), filepath.Join("..", "..", "cmd"), filepath.Join("..", "..", "tools")},
		func(rel, text string) {
			if strings.Contains(text, path) {
				offenders = append(offenders, relPath(rel))
			}
		})
	if n < 700 {
		t.Fatalf("the walk read %d files, want ~763 — the guard checked too little to mean anything", n)
	}
	if len(offenders) != 0 {
		sort.Strings(offenders)
		t.Errorf("production source imports the test harness: %s — a daemon that composes a child's environment out of a fake is not composing it at all", strings.Join(offenders, ", "))
	}
}

// TestNoEnvironmentParamCarriesAValue is EV7 at the type level. The wire guard
// (internal/protocol/environment_test.go) refuses a params bag that carries a
// credential-shaped KEY; this one says the structs themselves have nowhere to
// put one, which is the difference between a door that checks and a room that
// could not hold it. A value the protocol cannot express is a value that cannot
// accidentally be logged, cached, or replayed.
func TestNoEnvironmentParamCarriesAValue(t *testing.T) {
	forbidden := map[string]string{
		"value": "a secret's value, in a params struct, is a secret in a log line, a proxy trace, and a core dump",
		"key":   "an API key is the thing this plane points at and never carries",
		"token": "same", "apikey": "same", "secret": "same", "secretvalue": "same",
		"password": "same", "credential": "same", "contents": "same", "data": "same",
		"raw": "same", "bytes": "same", "envvalue": "same",
		"actor":     "the author is the connection's peer, never a caller's claim (EV17)",
		"actorkind": "same",
		"createdby": "stamped by the store (EV17)",
		"updatedby": "same",
		"createdat": "same",
		"updatedat": "same",
		"legacy":    "the legacy flag is the row's shape, not a caller's wish — a param here is a door onto inheriting the daemon (EV1, EV6)",
	}
	params := []any{
		protocol.EnvironmentListParams{}, protocol.EnvironmentShowParams{},
		protocol.EnvironmentEffectiveParams{}, protocol.EnvironmentChangesParams{},
		protocol.EnvironmentProvisionsParams{}, protocol.EnvironmentCreateParams{},
		protocol.EnvironmentUpdateParams{}, protocol.EnvironmentIDParams{},
		protocol.EnvironmentBindParams{}, protocol.EnvironmentUnbindParams{},
		protocol.ProvisionIDParams{}, protocol.SecretRefListParams{},
		protocol.SecretRefShowParams{}, protocol.SecretRefCheckParams{},
		protocol.SecretRefCreateParams{}, protocol.SecretRefUpdateParams{},
		protocol.SecretRefNameParams{},
	}
	checked := 0
	for _, p := range params {
		rt := reflect.TypeOf(p)
		if rt.Kind() != reflect.Struct {
			t.Fatalf("%T is not a struct", p)
		}
		for i := 0; i < rt.NumField(); i++ {
			checked++
			f := rt.Field(i)
			low := strings.ToLower(f.Name)
			if why, bad := forbidden[low]; bad {
				t.Errorf("%s.%s is a field the plane may never have (%s) — %s", rt.Name(), f.Name, low, why)
			}
		}
	}
	// Measured 2026-11-03: 48 fields across these 17 structs. The floor is
	// that number minus the slack a rename could cost, not a guess: a walk that
	// silently stops seeing the reference params would report a clean plane.
	if checked < 44 || len(params) != 17 {
		t.Fatalf("only %d param fields were reflected over across %d structs, want the ~48 over 17 measured on 2026-11-03 — the reflection walked almost nothing", checked, len(params))
	}
	// And the walk proves it reached the reference plane: a params struct
	// naming where a value lives is the field the forbidden list is about.
	sawSource := false
	for _, p := range params {
		rt := reflect.TypeOf(p)
		for i := 0; i < rt.NumField(); i++ {
			if strings.EqualFold(rt.Field(i).Name, "source") || strings.EqualFold(rt.Field(i).Name, "sourceKind") {
				sawSource = true
			}
		}
	}
	if !sawSource {
		t.Fatal("no params struct carried a `source`/`sourceKind` field — the reflection is not looking at the reference plane at all")
	}
}

// TestTheSeedNeverLaysTheLegacyBackend is EV19's limit at the one writer that
// could do it silently: the standard set lays `host-scrubbed` and the first
// binding, and nothing else. A seed that could write `host-inherit` would be a
// startup path that quietly widens every loop in the workspace — the inheritance
// this slice exists to close, arriving as a default.
func TestTheSeedNeverLaysTheLegacyBackend(t *testing.T) {
	var files int
	check := func(rel, text string) {
		files++
		if strings.Contains(text, "host-inherit") {
			t.Errorf("%s names the legacy backend — the seed lays host-scrubbed and never the row that inherits the daemon (EV19)", rel)
		}
	}
	for _, root := range []string{
		filepath.Join("..", "..", "internal", "execenv", "envseed"),
	} {
		if n := walkSources(t, []string{root}, func(rel, text string) { check(relPath(rel), text) }); n == 0 {
			t.Fatalf("the seed set's directory walked no files — the guard checked nothing")
		}
	}
	// The seed's store half too: the write door itself must not be able to say
	// the word, since the seed is the only caller that reaches it.
	for _, rel := range []string{"internal/store/environment_seed.go"} {
		text := readPlaneSource(t, rel)
		files++
		if strings.Contains(text, "host-inherit") {
			t.Errorf("%s names the legacy backend — a startup write must not be able to lay it (EV19)", rel)
		}
	}
	if files < 4 {
		t.Errorf("the seed walk saw %d files; the set has more than that and the guard has lost its target", files)
	}
}
