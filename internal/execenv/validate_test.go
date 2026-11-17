package execenv

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func intPtr(i int) *int         { return &i }
func f64Ptr(f float64) *float64 { return &f }

// baseSpec is the smallest spec Validate accepts on a host backend. Tests
// change ONE field and assert exactly one new refusal — the discipline that
// keeps "returns every problem" meaningful rather than a count nobody trusts.
func baseSpec() Spec {
	return Spec{
		Env:    EnvSection{Passthrough: []string{"PATH", "HOME"}, Set: map[string]string{"CI": "1"}},
		Home:   HomeReal,
		Egress: Egress{Mode: "unenforced"},
	}
}

func containerSpec() Spec {
	s := baseSpec()
	s.Image = &Image{Ref: "registry.example/app@sha256:aaaa", Pull: PullNever}
	return s
}

func TestValidateReportsEveryProblem(t *testing.T) {
	// Five faults at once. A form that reports one per save turns a security
	// record into whack-a-mole, and the failure mode is people who stop
	// reading.
	s := Spec{
		Env: EnvSection{
			Passthrough: []string{"PATH", "AWS_PROFILE"},
			Set:         map[string]string{"BAD-NAME": "x", "LD_PRELOAD": "/tmp/evil.so"},
			Secrets:     []string{"", ""},
		},
		Home:    "moon",
		Limits:  Limits{WallSeconds: intPtr(99999), MemoryMiB: intPtr(8)},
		Egress:  Egress{Mode: "maybe", Allow: []string{"api.example"}},
		Image:   &Image{Ref: "golang:1.26"},
		Preview: &Preview{TTL: 60},
	}
	got := Validate(s, BackendHostScrubbed, ValidateOpts{})
	if len(got) < 7 {
		t.Fatalf("Validate returned %d refusals, want at least 7:\n%s", len(got), renderRefusals(got))
	}
	kinds := map[string]int{}
	for _, r := range got {
		if r.Kind != RefusalSpecInvalid {
			t.Errorf("refusal %q has kind %q, want %q", r.What, r.Kind, RefusalSpecInvalid)
		}
		kinds[r.Kind]++
	}
	for _, r := range got {
		if r.What == "" || r.Why == "" || r.Do == "" {
			t.Errorf("refusal %q is not three-part: what=%q why=%q do=%q", r.Sentence(), r.What, r.Why, r.Do)
		}
	}
	wants := []string{"moon", "wallSeconds", "egress.mode", "preview", "LD_PRELOAD", "BAD-NAME", "AWS_PROFILE"}
	joined := renderRefusals(got)
	for _, w := range wants {
		if !strings.Contains(joined, w) {
			t.Errorf("no refusal names %q. Got:\n%s", w, joined)
		}
	}
}

func TestValidateAcceptsTheBaseSpec(t *testing.T) {
	if got := Validate(baseSpec(), BackendHostScrubbed, ValidateOpts{}); len(got) != 0 {
		t.Fatalf("the base host spec was refused:\n%s", renderRefusals(got))
	}
	if got := Validate(containerSpec(), BackendContainer, ValidateOpts{}); len(got) != 0 {
		t.Fatalf("the base container spec was refused:\n%s", renderRefusals(got))
	}
	// The legacy backend asks for nothing, so it must accept the empty spec
	// the migration seeds — otherwise an upgraded store cannot be created.
	legacy := Spec{Home: HomeReal, Egress: Egress{Mode: "unenforced"},
		Env: EnvSection{Set: map[string]string{}}}
	if got := Validate(legacy, BackendHostInherit, ValidateOpts{}); len(got) != 0 {
		t.Fatalf("the legacy spec the migration writes was refused:\n%s", renderRefusals(got))
	}
}

func TestDenyWinsEvenWhenExplicit(t *testing.T) {
	// Every one of these is something an operator CAN type into a spec, and
	// every one is a credential, an agent handle, or code execution as the
	// daemon's user. The deny list wins; the sentence points at secret
	// references, which is the path that actually holds a value honestly.
	names := []string{
		"ANTHROPIC_API_KEY", "OPENAI_API_KEY", "AEON_HTTP_KEY", "GITHUB_TOKEN", "GH_TOKEN",
		"AWS_SECRET_ACCESS_KEY", "AZURE_CLIENT_SECRET", "GOOGLE_APPLICATION_CREDENTIALS",
		"GCP_SERVICE_ACCOUNT_TOKEN", "STRIPE_SECRET_KEY", "NPM_TOKEN", "DB_PASSWORD",
		"MY_APP_SESSION_COOKIE", "OAUTH_TOKEN", "SSH_AUTH_SOCK", "GPG_AGENT_INFO",
		"DOCKER_HOST", "KUBECONFIG", "LD_PRELOAD", "LD_LIBRARY_PATH", "DYLD_INSERT_LIBRARIES",
		"BASH_ENV", "ENV", "SHELLOPTS", "PROMPT_COMMAND", "PS4", "IFS", "GLOBIGNORE",
		"PYTHONPATH", "NODE_OPTIONS", "GIT_CONFIG", "GIT_SSH_COMMAND", "GIT_DIR", "GIT_INDEX_FILE",
	}
	for _, name := range names {
		t.Run(name+" in passthrough", func(t *testing.T) {
			s := baseSpec()
			s.Env.Passthrough = []string{"PATH", name}
			if got := Validate(s, BackendHostScrubbed, ValidateOpts{}); !namesRefusal(got, name) {
				t.Errorf("a spec passing through %s was accepted:\n%s", name, renderRefusals(got))
			}
		})
		t.Run(name+" in set", func(t *testing.T) {
			s := baseSpec()
			s.Env.Set = map[string]string{"CI": "1", name: "x"}
			if got := Validate(s, BackendHostScrubbed, ValidateOpts{}); !namesRefusal(got, name) {
				t.Errorf("a spec setting %s was accepted:\n%s", name, renderRefusals(got))
			}
		})
	}

	t.Run("a denied pattern is refused, not silently matched by nothing", func(t *testing.T) {
		s := baseSpec()
		s.Env.Passthrough = []string{"AWS_*"}
		if got := Validate(s, BackendHostScrubbed, ValidateOpts{}); len(got) == 0 {
			t.Errorf("the pattern AWS_* was accepted:\n%s", renderRefusals(got))
		}
	})

	t.Run("the whole environment wildcard is refused", func(t *testing.T) {
		s := baseSpec()
		s.Env.Passthrough = []string{"*"}
		if got := Validate(s, BackendHostScrubbed, ValidateOpts{}); !namesRefusal(got, "*") {
			t.Errorf("`*` was accepted; that is the inheritance this slice exists to close:\n%s",
				renderRefusals(got))
		}
	})

	t.Run("PATH may pass through but may not be set", func(t *testing.T) {
		ok := baseSpec()
		ok.Env.Passthrough = []string{"PATH"}
		if got := Validate(ok, BackendHostScrubbed, ValidateOpts{}); len(got) != 0 {
			t.Errorf("passing PATH through was refused:\n%s", renderRefusals(got))
		}
		bad := baseSpec()
		bad.Env.Set = map[string]string{"PATH": "/tmp/bin"}
		if got := Validate(bad, BackendHostScrubbed, ValidateOpts{}); !namesRefusal(got, "PATH") {
			t.Errorf("setting PATH was accepted; a literal PATH redirects every relative command:\n%s",
				renderRefusals(got))
		}
	})

	t.Run("a mechanism name in an inject_as is refused by Build too", func(t *testing.T) {
		// Validate is the door that tells a person their spec is wrong; Build
		// is the last place that can be sure. Both must say no — to a name that
		// is a MECHANISM. (Amended at S59 T6: the credential words and vendor
		// namespaces are no longer refused for an injection, because the
		// injection is the remedy this very refusal points at. See
		// `refuseInjectionName`; the credential-shaped arm is pinned the other
		// way by TestAnInjectedCredentialKeepsItsName.)
		for _, name := range []string{"LD_PRELOAD", "NODE_OPTIONS", "PATH", "SSH_AUTH_SOCK", "AEON_SERVICE_DB_HOST"} {
			s := baseSpec()
			_, r := Build(s, DaemonEnv{}, []Injection{NewInjection("token", name,
				newSecret("token", []byte("0123456789abcdef")))})
			if r == nil {
				t.Errorf("Build injected a secret as %s", name)
				continue
			}
			if !strings.Contains(r.Sentence(), name) {
				t.Errorf("the refusal does not name the variable: %s", r.Sentence())
			}
			if !strings.Contains(r.Sentence(), "token") {
				t.Errorf("the refusal does not name the reference a person would have to edit: %s", r.Sentence())
			}
		}
	})

	t.Run("every refusal points at secret references", func(t *testing.T) {
		s := baseSpec()
		s.Env.Set = map[string]string{"MY_TOKEN": "x"}
		got := Validate(s, BackendHostScrubbed, ValidateOpts{})
		if !namesRefusal(got, "MY_TOKEN") {
			t.Fatalf("MY_TOKEN accepted:\n%s", renderRefusals(got))
		}
		if !strings.Contains(renderRefusals(got), "secret reference") {
			t.Errorf("the refusal does not offer the way out (secret references):\n%s", renderRefusals(got))
		}
	})
}

// TestCredentialWordsSupersetOfConfigMarks pins the two lists against each
// other. `internal/config`'s marks are the ONE credential rule (C4/C17); this
// package's deny words decide whether a variable reaches a child. If the two
// disagree, one of them is confident while the other leaks.
//
// config's `isCredentialName` is unexported, so the guard reads its source the
// way this repository's other guards read the structures they police: extract
// the marks from the file, then assert this package covers every one.
func TestCredentialWordsSupersetOfConfigMarks(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "config", "creds.go"))
	if err != nil {
		t.Fatalf("read config/creds.go: %v", err)
	}
	marks := extractCredentialMarks(string(src))
	if len(marks) == 0 {
		t.Fatal("found no credential marks in config/creds.go — the guard is asserting against nothing; " +
			"if the marker list moved, update this extractor rather than deleting the test")
	}
	d := Deny()
	for _, mark := range marks {
		upper := strings.ToUpper(mark)
		covered := false
		for _, w := range d.Words {
			if strings.Contains(upper, w) || strings.Contains(w, upper) {
				covered = true
				break
			}
		}
		if !covered {
			t.Errorf("config marks %q as a credential and execenv's deny words do not cover it — "+
				"the two lists have drifted (deny words: %v)", mark, d.Words)
		}
	}
}

// extractCredentialMarks pulls the string literals out of the
// `for _, mark := range []string{…}` line of config's isCredentialName.
func extractCredentialMarks(src string) []string {
	const anchor = "for _, mark := range []string{"
	i := strings.Index(src, anchor)
	if i < 0 {
		return nil
	}
	rest := src[i+len(anchor):]
	end := strings.Index(rest, "}")
	if end < 0 {
		return nil
	}
	var marks []string
	for _, part := range strings.Split(rest[:end], ",") {
		part = strings.TrimSpace(part)
		if len(part) >= 2 && part[0] == '"' && part[len(part)-1] == '"' {
			marks = append(marks, strings.Trim(part, `"`))
		}
	}
	return marks
}

func TestCapabilitiesGateTheSpec(t *testing.T) {
	// Each case asks a backend for something it cannot enforce. The rule is
	// not "accept and ignore" — a limit that does nothing is a limit that
	// will be believed, which is worse than a refusal.
	cases := []struct {
		name     string
		backend  string
		mutate   func(*Spec)
		opts     ValidateOpts
		wantText string
	}{
		{"memory on a host", BackendHostScrubbed, func(s *Spec) { s.Limits.MemoryMiB = intPtr(512) },
			ValidateOpts{}, "memoryMiB"},
		{"cpus on a host", BackendHostScrubbed, func(s *Spec) { s.Limits.CPUs = f64Ptr(2) },
			ValidateOpts{}, "cpus"},
		{"egress allow anywhere", BackendContainer, func(s *Spec) { s.Egress.Allow = []string{"api.example"} },
			ValidateOpts{}, "egress.allow"},
		{"egress none on a host", BackendHostScrubbed, func(s *Spec) { s.Egress.Mode = "none" },
			ValidateOpts{}, "cannot be enforced"},
		{"preview set", BackendContainer, func(s *Spec) { s.Preview = &Preview{TTL: 30} },
			ValidateOpts{}, "preview"},
		{"scratch home on a host", BackendHostScrubbed, func(s *Spec) { s.Home = HomeScratch },
			ValidateOpts{}, "scratch"},
		{"vm with no driver", BackendVM, func(s *Spec) {},
			ValidateOpts{}, "no vm driver"},
		{"vm with a driver registers", BackendVM, func(s *Spec) { s.Home = HomeScratch },
			ValidateOpts{Drivers: []string{BackendVM}}, "zzz-not-refused"},
		{"container without an image", BackendContainer, func(s *Spec) { s.Image = nil },
			ValidateOpts{}, "image.ref"},
		{"services on a host", BackendHostScrubbed, func(s *Spec) {
			s.Services = []Service{{Name: "db", Image: "postgres:16", Port: 5432}}
		}, ValidateOpts{}, "services"},
		{"wall longer than the ceiling", BackendHostScrubbed, func(s *Spec) { s.Limits.WallSeconds = intPtr(9999) },
			ValidateOpts{}, "wallSeconds"},
		{"unknown backend", "qemu", func(s *Spec) {}, ValidateOpts{}, "not a backend this daemon knows"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := baseSpec()
			if tc.backend == BackendContainer {
				s = containerSpec()
			}
			tc.mutate(&s)
			got := Validate(s, tc.backend, tc.opts)
			if tc.wantText == "zzz-not-refused" {
				if len(got) != 0 {
					t.Fatalf("a vm environment with a registered driver was refused:\n%s", renderRefusals(got))
				}
				return
			}
			if !namesRefusal(got, tc.wantText) {
				t.Errorf("no refusal names %q:\n%s", tc.wantText, renderRefusals(got))
			}
			for _, r := range got {
				if !strings.Contains(r.Sentence(), tc.backend) && r.Kind == RefusalSpecInvalid &&
					strings.Contains(r.What, tc.wantText) &&
					(tc.wantText == "memoryMiB" || tc.wantText == "cpus" || tc.wantText == "cannot be enforced") {
					// The sentence must name the backend that cannot do it —
					// otherwise the operator optimises the wrong record.
					t.Errorf("the refusal does not name the backend: %s", r.Sentence())
				}
			}
		})
	}
}

func TestWarningsAreHonestAndNeverRefuse(t *testing.T) {
	s := containerSpec()
	s.Env.Passthrough = []string{"PATH", "HTTPS_PROXY"}
	s.Image = &Image{Ref: "golang:1.26"}
	warns := Warnings(s, BackendContainer)
	joined := strings.Join(warns, "\n")
	for _, want := range []string{"HTTPS_PROXY", "floating tag", "memoryMiB"} {
		if !strings.Contains(joined, want) {
			t.Errorf("no warning mentions %q. Got:\n%s", want, joined)
		}
	}
	if got := Validate(s, BackendContainer, ValidateOpts{}); len(got) != 0 {
		t.Errorf("a warning refused the write instead of warning:\n%s", renderRefusals(got))
	}
}

func TestValidateRejectsAnOversizedSet(t *testing.T) {
	s := baseSpec()
	s.Env.Set = map[string]string{}
	for i := 0; i < MaxSetEntries+1; i++ {
		s.Env.Set[fmt.Sprintf("A_VAR_%d", i)] = "1"
	}
	if got := Validate(s, BackendHostScrubbed, ValidateOpts{}); !namesRefusal(got, "env.set") {
		t.Errorf("an unbounded env.set was accepted:\n%s", renderRefusals(got))
	}

	s = baseSpec()
	s.Env.Set = map[string]string{"BUILD_FLAG": strings.Repeat("x", MaxSetValueBytes+1)}
	if got := Validate(s, BackendHostScrubbed, ValidateOpts{}); !namesRefusal(got, "BUILD_FLAG") {
		t.Errorf("a value over the byte cap was accepted:\n%s", renderRefusals(got))
	}
}

func TestValidateRefusesDuplicateServiceNamesAndBadNames(t *testing.T) {
	s := containerSpec()
	s.Services = []Service{
		{Name: "db", Image: "postgres:16", Port: 5432},
		{Name: "db", Image: "postgres:16", Port: 5432},
		{Name: "BAD NAME", Image: "redis", Port: 6379},
		{Name: "cache", Image: "", Port: 0},
	}
	got := Validate(s, BackendContainer, ValidateOpts{})
	joined := renderRefusals(got)
	for _, want := range []string{"named db", "BAD NAME", "no image", "port 0"} {
		if !strings.Contains(joined, want) {
			t.Errorf("no refusal names %q:\n%s", want, joined)
		}
	}
}

// namesRefusal is the assertion helper: a refusal naming the thing refused,
// which is the whole of DB19's "a refusal names its reason, and the thing
// refused".
func namesRefusal(rs []Refusal, fragment string) bool {
	for _, r := range rs {
		if strings.Contains(r.Sentence(), fragment) {
			return true
		}
	}
	return false
}

func renderRefusals(rs []Refusal) string {
	var b strings.Builder
	for _, r := range rs {
		b.WriteString("  - ")
		b.WriteString(r.Sentence())
		b.WriteString("\n")
	}
	return b.String()
}

// The Refusal type itself: three parts, one renderer, and a JSON shape the
// panel can read without inventing a second one.
func TestRefusalSentenceIsTheOneRenderer(t *testing.T) {
	r := NewRefusal(RefusalEnvironmentUnbound, "no environment is bound", "there is no binding",
		"run `aeon env bind`")
	want := "refused: environment-unbound · no environment is bound · there is no binding · run `aeon env bind`"
	if r.Sentence() != want {
		t.Errorf("Sentence() =\n %q\nwant\n %q", r.Sentence(), want)
	}
	if r.Error() != want {
		t.Errorf("Error() diverged from Sentence(): %q", r.Error())
	}
	raw, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, part := range []string{"environment-unbound", "no environment is bound", "run `aeon env bind`"} {
		if !strings.Contains(string(raw), part) {
			t.Errorf("the JSON dropped %q: %s", part, raw)
		}
	}
	var nilR *Refusal
	if nilR.Sentence() != "" {
		t.Error("a nil *Refusal must render as the empty string, not panic")
	}
}

// TestAnInjectedCredentialKeepsItsName is the other half of S59 T6's amendment
// to the injection rule (the mechanism arm stays refused, above). The deny
// list exists because a token copied out of the daemon's environment by
// accident is the leak this slice closes — and the remedy it offers for that
// refusal is a secret reference. A reference injects under the name the child's
// tool actually reads, and every name a real tool reads for a credential looks
// like a credential. Refusing those would close the one sanctioned door EV7
// opens, and a person left with a refused build widens an allowlist to get
// their CI working: the deny list would then be the reason for the leak.
//
// So these names inject, the value arrives, and it is still attributed to the
// reference rather than to the daemon's environment — which is what makes the
// streaming redactor able to refuse to print it.
func TestAnInjectedCredentialKeepsItsName(t *testing.T) {
	for _, name := range []string{
		"GITHUB_TOKEN", "GH_TOKEN", "AWS_SECRET_ACCESS_KEY", "NETLIFY_TOKEN",
		"DEPLOY_TOKEN", "OPENAI_API_KEY", "STRIPE_SECRET_KEY",
	} {
		s := baseSpec()
		env, r := Build(s, DaemonEnv{}, []Injection{NewInjection(strings.ToLower(name), name,
			newSecret(strings.ToLower(name), []byte("0123456789abcdef")))})
		if r != nil {
			t.Errorf("the sanctioned path refused %s: %s", name, r.Sentence())
			continue
		}
		var found *Var
		for i := range env.Names() {
			if env.Names()[i].Name == name {
				found = &env.Names()[i]
			}
		}
		if found == nil {
			t.Errorf("%s did not arrive in the child environment: %v", name, env.Names())
			continue
		}
		if !strings.HasPrefix(found.Source, SourceSecret) {
			t.Errorf("%s arrived from %q, not from its secret reference — the source is what the redactor and the panel both tell the truth with", name, found.Source)
		}
		if !strings.Contains(strings.Join(env.Env(), "\n"), name+"=0123456789abcdef") {
			t.Errorf("%s arrived without its value: %v", name, env.Env())
		}
	}
}
