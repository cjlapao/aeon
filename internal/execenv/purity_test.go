package execenv

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The purity that makes every test in this package a unit test: the standard
// library and nothing else, and no access to the process environment.
//
// `internal/records` established the shape (a module path has a dot in its
// first element, a stdlib root never does). This package adds the second half,
// because the claim that matters here is not "no dependencies" but "nobody
// reads os.Getenv in here" — the daemon's environment is a FUNCTION argument
// (EV5), which is the only reason a test can hand in a hostile map and prove
// what a child sees.

func TestCorePackageImportsOnlyStdlib(t *testing.T) {
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	var checked int
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		checked++
		for _, imp := range f.Imports {
			path := strings.Trim(imp.Path.Value, `"`)
			root := path
			if i := strings.IndexByte(root, '/'); i >= 0 {
				root = path[:i]
			}
			// A module path has a dot in its first element; a stdlib root never
			// does. This is `internal/records`'s guard, applied to a package
			// where the stake is higher: a store or daemon dependency puts a
			// database between a security decision and the test that checks it.
			if strings.Contains(root, ".") {
				t.Errorf("%s imports %q — the execenv core is stdlib-only", name, path)
			}
		}
		// The second half of the claim, and the one that makes every test in
		// this package a unit test: nobody reads the process environment here.
		// The daemon's environment is a FUNCTION argument (EV5).
		ast.Inspect(f, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			pkg, ok := sel.X.(*ast.Ident)
			if !ok || pkg.Name != "os" {
				return true
			}
			switch sel.Sel.Name {
			case "Getenv", "LookupEnv", "Environ", "Setenv", "Unsetenv":
				t.Errorf("%s touches os.%s — the daemon's environment is an ARGUMENT (EV5); a package "+
					"that reads it cannot be tested with a map, which is the whole design", name, sel.Sel.Name)
			}
			return true
		})
	}
	if checked == 0 {
		t.Fatal("no package files found — the walk is asserting against nothing")
	}
}

func TestGitEnvKindsDifferInTheOneWayTheyShould(t *testing.T) {
	de, hostile := hostileEnv()

	daemon := strings.Join(GitEnv(de, GitEnvDaemon), "\n")
	uiRead := strings.Join(GitEnv(de, GitEnvUIRead), "\n")

	// Neither path inherits the daemon's credentials, which is what EV10's
	// table means by `daemon-git` and `ui-read`.
	for name, value := range hostile {
		if strings.HasPrefix(name, "PATH") || strings.HasPrefix(name, "HOME") ||
			strings.HasPrefix(name, "USER") || strings.HasPrefix(name, "LANG") ||
			strings.HasPrefix(name, "LC_") || strings.HasPrefix(name, "TZ") {
			continue
		}
		if name == "SSH_AUTH_SOCK" {
			continue // asserted on its own below: present on daemon-git, absent on ui-read
		}
		if strings.Contains(daemon, name+"=") || strings.Contains(uiRead, name+"=") {
			t.Errorf("%s reached a git child (daemon-git or ui-read): the hook a model wrote can read it", name)
		}
		if strings.Contains(daemon, value) || strings.Contains(uiRead, value) {
			t.Errorf("the value of %s reached a git child", name)
		}
	}

	// D9: the ssh agent socket only on the daemon path, where pushing is the
	// point of the command.
	if !strings.Contains(daemon, "SSH_AUTH_SOCK=") {
		t.Error("daemon-git has no SSH_AUTH_SOCK; D9 passes it so ssh remotes can be pushed to")
	}
	if strings.Contains(uiRead, "SSH_AUTH_SOCK=") {
		t.Error("ui-read has SSH_AUTH_SOCK; a read-only status/diff has no business reaching the agent")
	}

	// Ambient GIT_* is stripped on BOTH paths (F16's rule, extended to plumbing).
	for _, kv := range GitEnv(DaemonEnv{
		Lookup: func(string) string { return "/tmp/someone-elses-repo" },
		Names:  func() []string { return []string{"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE"} },
	}, GitEnvUIRead) {
		if strings.HasPrefix(kv, "GIT_DIR=") || strings.HasPrefix(kv, "GIT_WORK_TREE=") ||
			strings.HasPrefix(kv, "GIT_INDEX_FILE=") {
			t.Errorf("an ambient %s survived; it retargets the operation (F16)", kv)
		}
	}

	// The hardening the UI path already had stays on the UI path, and the
	// daemon path keeps the two lines that stop a hang.
	for _, want := range GitHardening() {
		if !strings.Contains(uiRead, strings.SplitN(want, "=", 2)[0]+"=") {
			t.Errorf("ui-read lost the hardening line %s", want)
		}
	}
	if !strings.Contains(daemon, "GIT_TERMINAL_PROMPT=0") {
		t.Error("daemon-git lost GIT_TERMINAL_PROMPT=0; a credential question becomes a hung step")
	}
	if strings.Contains(daemon, "GIT_CONFIG_GLOBAL=/dev/null") {
		t.Error("daemon-git disables the person's global config; the daemon pushes to real remotes — " +
			"the leak EV10 closes is the daemon's credentials, not the repository's settings")
	}
}

func TestVocabularyServesEverythingAClientWouldRetype(t *testing.T) {
	// The reason this function exists: a vocabulary a client types again is a
	// vocabulary that drifts. So every closed set has to be reachable from the
	// one call, including the two honesty sentences.
	v := Vocabulary()
	if len(v.Backends) != 4 {
		t.Errorf("Backends() = %d entries, want the four of EV3", len(v.Backends))
	}
	for _, b := range v.Backends {
		if b.Name == "" || len(b.Capabilities) == 0 {
			t.Errorf("backend %+v has no name or no capabilities; the panel's chips read this", b)
		}
	}
	if v.Honesty != Honesty || v.RedactionLimits != RedactionLimits {
		t.Error("Vocabulary did not serve the honesty sentences; a paraphrase is a second truth")
	}
	if strings.Contains(strings.Join(v.DefaultAllowed, ","), "CI") {
		t.Error("the default allowlist sets CI; D4 refuses it because CI changes what tools do")
	}
	for _, kind := range SourceKinds() {
		if !contains(v.SourceKinds, kind) {
			t.Errorf("source kind %q is missing from the served vocabulary", kind)
		}
	}
	// The deny list is served, so a form can warn before the daemon refuses.
	d := Deny()
	if len(d.Words) == 0 || len(d.Prefixes) == 0 || len(d.Exact) == 0 || len(d.Loader) == 0 {
		t.Errorf("Deny() is incomplete: %+v", d)
	}
	if v.MaxWallSeconds != MaxWallSeconds {
		t.Errorf("MaxWallSeconds = %d, want %d (tools.BashTimeout)", v.MaxWallSeconds, MaxWallSeconds)
	}
	// Capabilities() and HasCapability() agree, or a panel chip lies.
	for _, b := range Backends() {
		for _, c := range b.Capabilities {
			if !HasCapability(b.Name, c) {
				t.Errorf("HasCapability(%q, %q) = false but the table lists it", b.Name, c)
			}
		}
		if got := Capabilities("no-such-backend"); got != nil {
			t.Errorf("Capabilities of an unknown backend = %v, want nil (Validate refuses the name "+
				"rather than defaulting)", got)
		}
	}
}

func TestNamespacedServiceVarsAreBuildableNames(t *testing.T) {
	// EV14: the daemon derives these names. If one were unbuildable, the loop
	// would start and then lose its database host at the first command.
	for _, name := range []string{"postgres", "redis-cache", "pg16"} {
		host, port := NamespacedServiceHost(name), NamespacedServicePort(name)
		if r := refuseName(host, "service", true); r != nil {
			t.Errorf("the derived name %q is refused by its own rules: %s", host, r.Sentence())
		}
		if r := refuseName(port, "service", true); r != nil {
			t.Errorf("the derived name %q is refused: %s", port, r.Sentence())
		}
	}
	if got := NamespacedServiceHost("redis-cache"); got != "AEON_SERVICE_REDIS_CACHE_HOST" {
		t.Errorf("NamespacedServiceHost = %q", got)
	}
}

func TestSortedCopyDoesNotAlias(t *testing.T) {
	// Canonical() normalises order. If it sorted in place it would mutate the
	// caller's spec, and a spec whose service order is semantic would have been
	// quietly reordered by a function whose name says "render".
	in := []string{"c", "a", "b"}
	got := sortedCopy(in)
	if in[0] != "c" {
		t.Errorf("sortedCopy mutated the input: %v", in)
	}
	if got[0] != "a" || got[1] != "b" || got[2] != "c" {
		t.Errorf("sortedCopy did not sort: %v", got)
	}
	if sortedCopy(nil) != nil {
		t.Error("a nil list must stay nil, not become an empty slice — Canonical compares them apart")
	}
}
