package execenv

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"
)

// EV16: an agent may only ever PROPOSE a narrower environment. That permission
// rests entirely on this function being right, so it gets one case per axis
// plus a property test — a single missed axis is an agent widening its own
// sandbox through the one door that is supposed to be able only to narrow.

func TestRestrictsAxes(t *testing.T) {
	base := Spec{
		Env: EnvSection{
			Passthrough: []string{"PATH", "HOME", "LC_*"},
			Set:         map[string]string{"CI": "1", "TERM": "dumb"},
			Secrets:     []string{"db-url", "npm-token"},
		},
		Home:   HomeReal,
		Limits: Limits{WallSeconds: intPtr(120), MemoryMiB: intPtr(512), CPUs: f64Ptr(2)},
		Egress: Egress{Mode: "none"},
		Image:  &Image{Ref: "app@sha256:aaaa", Pull: PullNever},
		Services: []Service{
			{Name: "db", Image: "postgres:16", Port: 5432, Env: map[string]string{"POSTGRES_DB": "app"},
				Secrets: []string{"db-pass"}},
		},
	}

	cases := []struct {
		name        string
		mutate      func(*Spec)
		newBackend  string
		wantAxis    string
		wantMember  string
		expectEmpty bool
	}{
		{name: "identical is restrictive", expectEmpty: true},
		{name: "passthrough superset", mutate: func(s *Spec) {
			s.Env.Passthrough = append(s.Env.Passthrough, "AWS_PROFILE")
		}, wantAxis: AxisPassthrough},
		{name: "passthrough subset", mutate: func(s *Spec) {
			s.Env.Passthrough = []string{"PATH"}
		}, expectEmpty: true},
		{name: "a broader passthrough pattern", mutate: func(s *Spec) {
			s.Env.Passthrough = []string{"PATH", "HOME", "LC_*", "L*"}
		}, wantAxis: AxisPassthrough, wantMember: "L*"},
		{name: "a narrower passthrough pattern is fine", mutate: func(s *Spec) {
			s.Env.Passthrough = []string{"PATH", "HOME", "LC_ALL"}
		}, expectEmpty: true},
		{name: "a new set variable", mutate: func(s *Spec) { s.Env.Set["EXTRA"] = "1" },
			wantAxis: AxisSet, wantMember: "EXTRA"},
		{name: "a changed set value is a widening", mutate: func(s *Spec) { s.Env.Set["CI"] = "0" },
			wantAxis: AxisSet},
		{name: "a dropped set variable is a tightening", mutate: func(s *Spec) { delete(s.Env.Set, "TERM") },
			expectEmpty: true},
		{name: "a new secret reference", mutate: func(s *Spec) { s.Env.Secrets = append(s.Env.Secrets, "aws-keys") },
			wantAxis: AxisSecrets, wantMember: "aws-keys"},
		{name: "a dropped secret is a tightening", mutate: func(s *Spec) {
			s.Env.Secrets = []string{"db-url"}
		}, expectEmpty: true},
		{name: "a new service", mutate: func(s *Spec) {
			s.Services = append(s.Services, Service{Name: "cache", Image: "redis", Port: 6379})
		}, wantAxis: AxisServices, wantMember: "cache"},
		{name: "a changed service image", mutate: func(s *Spec) {
			s.Services[0].Image = "postgres:17"
		}, wantAxis: AxisServices},
		{name: "a changed service env", mutate: func(s *Spec) {
			s.Services[0].Env["POSTGRES_HOST_AUTH_METHOD"] = "trust"
		}, wantAxis: AxisServices},
		{name: "wall limit raised", mutate: func(s *Spec) { s.Limits.WallSeconds = intPtr(600) },
			wantAxis: AxisLimits, wantMember: "wallSeconds"},
		{name: "wall limit lowered", mutate: func(s *Spec) { s.Limits.WallSeconds = intPtr(30) },
			expectEmpty: true},
		{name: "a limit removed", mutate: func(s *Spec) { s.Limits.MemoryMiB = nil },
			wantAxis: AxisLimits, wantMember: "memoryMiB limit removed"},
		{name: "a cpu limit lowered is a tightening", mutate: func(s *Spec) { s.Limits.CPUs = f64Ptr(1) },
			expectEmpty: true},
		{name: "memory raised", mutate: func(s *Spec) { s.Limits.MemoryMiB = intPtr(4096) },
			wantAxis: AxisLimits},
		{name: "egress none to unrestricted", mutate: func(s *Spec) { s.Egress.Mode = "unrestricted" },
			wantAxis: AxisEgress},
		{name: "egress none to unenforced", mutate: func(s *Spec) { s.Egress.Mode = "unenforced" },
			wantAxis: AxisEgress},
		{name: "egress unrestricted to none is a tightening", mutate: func(s *Spec) {
			s.Egress.Mode = "none"
		}, expectEmpty: true},
		{name: "egress allow added", mutate: func(s *Spec) { s.Egress.Allow = []string{"api.example"} },
			wantAxis: AxisEgress},
		{name: "image swapped", mutate: func(s *Spec) { s.Image = &Image{Ref: "other@sha256:bbbb"} },
			wantAxis: AxisImage},
		{name: "home changed", mutate: func(s *Spec) { s.Home = HomeScratch }, wantAxis: AxisHome},
		{name: "preview added", mutate: func(s *Spec) { s.Preview = &Preview{TTL: 60} }, wantAxis: AxisPreview},
		{name: "backend weakened", mutate: func(s *Spec) {}, newBackend: BackendHostScrubbed,
			wantAxis: AxisBackend},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			nb := BackendContainer
			if tc.newBackend != "" {
				nb = tc.newBackend
			}
			next := base
			next.Env.Passthrough = append([]string(nil), base.Env.Passthrough...)
			next.Env.Secrets = append([]string(nil), base.Env.Secrets...)
			next.Env.Set = map[string]string{}
			for k, v := range base.Env.Set {
				next.Env.Set[k] = v
			}
			next.Services = make([]Service, 0, len(base.Services))
			for _, sv := range base.Services {
				cp := sv
				cp.Env = map[string]string{}
				for k, v := range sv.Env {
					cp.Env[k] = v
				}
				next.Services = append(next.Services, cp)
			}
			if tc.mutate != nil {
				tc.mutate(&next)
			}
			got := Restricts(base, next, BackendContainer, nb)
			if tc.expectEmpty {
				if len(got) != 0 {
					t.Fatalf("a tightening was reported as a widening: %v", widenings(got))
				}
				return
			}
			var found bool
			for _, w := range got {
				if w.Axis == tc.wantAxis && (tc.wantMember == "" || strings.Contains(w.Member, tc.wantMember)) {
					found = true
				}
			}
			if !found {
				t.Errorf("no widening for axis %q member %q. Got: %v",
					tc.wantAxis, tc.wantMember, widenings(got))
			}
			for _, w := range got {
				if w.Axis == "" {
					t.Errorf("a widening has no axis: %+v", w)
				}
			}
		})
	}
}

func TestRestrictsIsReflexiveAndTransitive(t *testing.T) {
	// Reflexive: nothing is looser than itself. Transitive: if B is no looser
	// than A and C is no looser than B, C is no looser than A — which is what
	// lets two approved restricts in a row be trusted. An order bug that only
	// shows on the third element of a chain is exactly the one a review misses.
	specs := generateSpecs(220)

	for _, s := range specs {
		if got := Restricts(s, s, BackendHostScrubbed, BackendHostScrubbed); len(got) != 0 {
			t.Fatalf("Restricts(x, x) reported widenings %v for %+v — the relation is not reflexive",
				widenings(got), s)
		}
	}

	for i := range specs {
		for j := range specs {
			for k := range specs {
				a, b, c := specs[i], specs[j], specs[k]
				ab := len(Restricts(a, b, BackendContainer, BackendContainer)) == 0
				bc := len(Restricts(b, c, BackendContainer, BackendContainer)) == 0
				if !ab || !bc {
					continue
				}
				if ac := Restricts(a, c, BackendContainer, BackendContainer); len(ac) != 0 {
					t.Fatalf("not transitive: A⊒B and B⊒C but A⊏C: %v\nA=%s\nB=%s\nC=%s",
						widenings(ac), Canonical(a), Canonical(b), Canonical(c))
				}
			}
		}
	}
}

// generateSpecs builds specs out of a small fixed pool so the relation is
// exercised over real subset relationships rather than random noise. Every one
// is a legal host/container spec.
func generateSpecs(n int) []Spec {
	rng := rand.New(rand.NewSource(20261103))
	pool := []string{"PATH", "HOME", "USER", "LANG", "LC_*", "TZ", "TMPDIR", "SHELL"}
	setPool := [][2]string{{"CI", "1"}, {"TERM", "dumb"}, {"GOFLAGS", "-mod=mod"}, {"COLUMNS", "120"}}
	secretPool := []string{"db-url", "npm-token", "sentry-dsn"}
	svcPool := []Service{
		{Name: "db", Image: "postgres:16", Port: 5432, Env: map[string]string{"POSTGRES_DB": "app"}},
		{Name: "cache", Image: "redis:7", Port: 6379, Env: map[string]string{}},
	}
	egress := []string{"none", "unrestricted", "unenforced"}
	homes := []string{HomeReal, HomeScratch}

	out := make([]Spec, 0, n)
	for i := 0; i < n; i++ {
		s := Spec{Env: EnvSection{Set: map[string]string{}},
			Egress: Egress{Mode: egress[rng.Intn(len(egress))]},
			Home:   homes[rng.Intn(len(homes))]}
		for _, p := range pool {
			if rng.Intn(2) == 0 {
				s.Env.Passthrough = append(s.Env.Passthrough, p)
			}
		}
		for _, kv := range setPool {
			if rng.Intn(3) == 0 {
				s.Env.Set[kv[0]] = kv[1]
			}
		}
		for _, ref := range secretPool {
			if rng.Intn(2) == 0 {
				s.Env.Secrets = append(s.Env.Secrets, ref)
			}
		}
		for idx, svc := range svcPool {
			if rng.Intn(2) == 0 {
				cp := svc
				cp.Env = map[string]string{}
				for k, v := range svc.Env {
					cp.Env[k] = v
				}
				if idx == 0 && rng.Intn(3) == 0 {
					cp.Image = "postgres:17"
				}
				s.Services = append(s.Services, cp)
			}
		}
		if rng.Intn(3) == 0 {
			s.Limits.WallSeconds = intPtr(30 * (1 + rng.Intn(20)))
		}
		if rng.Intn(3) == 0 {
			s.Limits.MemoryMiB = intPtr(256 * (1 + rng.Intn(8)))
		}
		if rng.Intn(4) == 0 {
			s.Limits.CPUs = f64Ptr(0.5 * float64(1+rng.Intn(8)))
		}
		if rng.Intn(2) == 0 {
			s.Image = &Image{Ref: fmt.Sprintf("app@sha256:%04x", rng.Intn(1<<16)), Pull: PullNever}
		}
		if rng.Intn(5) == 0 {
			s.Preview = &Preview{TTL: rng.Intn(3)}
		}
		out = append(out, s)
	}
	return out
}

func widenings(ws []Widening) []string {
	out := make([]string, 0, len(ws))
	for _, w := range ws {
		out = append(out, w.Sentence())
	}
	return out
}

func TestRestrictsBackendStrengthUsesTheTableNotTheString(t *testing.T) {
	// An unrecognised backend scores below everything, so it can never read as
	// "stronger" and let a widening through on a typo.
	if BackendStrength("qemu") >= 0 {
		t.Errorf("an unknown backend scored %d; it must be below every real one", BackendStrength("qemu"))
	}
	if BackendStrength(BackendHostInherit) >= BackendStrength(BackendHostScrubbed) ||
		BackendStrength(BackendHostScrubbed) >= BackendStrength(BackendContainer) ||
		BackendStrength(BackendContainer) >= BackendStrength(BackendVM) {
		t.Error("EV3's strength order is wrong; EV6's downgrade and EV16 both compare on it")
	}
	if got := Restricts(Spec{}, Spec{}, BackendContainer, "qemu"); len(got) == 0 {
		t.Error("moving to an unknown backend was not reported as a backend widening")
	}
}
