package execenv

import (
	"strings"
	"testing"
)

// EV2's order and EV6's one tightening. The four cells of
// (attended true | false | nil) × (legacy | not) are the whole rule, and the
// two refusals are the two states where "just run something" would be the
// dangerous answer.

func scrubbedRow() *BoundEnv {
	return &BoundEnv{ID: EnvSystemScrubbed, Slug: "host-scrubbed", Backend: BackendHostScrubbed,
		Origin: "system", Spec: baseSpec()}
}

func legacyRow() *BoundEnv {
	return &BoundEnv{ID: EnvLegacyRow, Slug: "host-inherit", Backend: BackendHostInherit,
		Legacy: true, Origin: "system", Spec: Spec{Home: HomeReal, Egress: Egress{Mode: "unenforced"}}}
}

func containerRow() *BoundEnv {
	return &BoundEnv{ID: "env-build", Slug: "build", Backend: BackendContainer,
		Spec: containerSpec()}
}

func TestResolveOrder(t *testing.T) {
	comp, root, ws := containerRow(), scrubbedRow(), legacyRow()

	cases := []struct {
		name       string
		in         ResolveInput
		wantSlug   string
		wantSource string
	}{
		{"child wins over everything",
			ResolveInput{ComponentBinding: comp, RootBinding: root, WorkspaceBinding: ws,
				Attended: boolPtr(true), ScrubbedSystem: scrubbedRow()},
			"build", SourceComponent},
		{"root wins over workspace",
			ResolveInput{RootBinding: root, WorkspaceBinding: ws,
				Attended: boolPtr(true), ScrubbedSystem: scrubbedRow()},
			"host-scrubbed", SourceRoot},
		{"workspace is the last answer",
			ResolveInput{WorkspaceBinding: scrubbedRow(),
				Attended: boolPtr(true), ScrubbedSystem: scrubbedRow()},
			"host-scrubbed", SourceWorkspace},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, r := Resolve(tc.in)
			if r != nil {
				t.Fatalf("Resolve: %s", r.Sentence())
			}
			if got.Env.Slug != tc.wantSlug || got.Source != tc.wantSource {
				t.Errorf("resolved to %q via %q, want %q via %q",
					got.Env.Slug, got.Source, tc.wantSlug, tc.wantSource)
			}
			if got.Downgraded {
				t.Errorf("a normal resolution reported Downgraded")
			}
		})
	}
}

func TestUnattendedNeverInherits(t *testing.T) {
	// The four cells. Each is a different story in the step record, so each is
	// its own case rather than a table row that could be quietly dropped.
	t.Run("unattended + legacy ⇒ scrubbed and recorded", func(t *testing.T) {
		no := false
		got, r := Resolve(ResolveInput{WorkspaceBinding: legacyRow(), Attended: &no,
			ScrubbedSystem: scrubbedRow()})
		if r != nil {
			t.Fatalf("Resolve: %s", r.Sentence())
		}
		if got.Env.Backend != BackendHostScrubbed || !got.Downgraded {
			t.Errorf("an unattended legacy loop ran in %q (downgraded=%v)", got.Env.Backend, got.Downgraded)
		}
		if got.Reason == "" || !strings.Contains(got.Reason, "unattended") {
			t.Errorf("the downgrade has no reason a step record could show: %q", got.Reason)
		}
		if got.Source != SourceWorkspace {
			t.Errorf("source = %q; the downgrade does not change WHERE the binding was found", got.Source)
		}
	})

	t.Run("never declared + legacy ⇒ the same answer (F10)", func(t *testing.T) {
		got, r := Resolve(ResolveInput{WorkspaceBinding: legacyRow(), Attended: nil,
			ScrubbedSystem: scrubbedRow()})
		if r != nil {
			t.Fatalf("Resolve: %s", r.Sentence())
		}
		if !got.Downgraded || got.Env.Backend != BackendHostScrubbed {
			t.Error(`attended IS NULL must read as unattended — "nobody ever said" is not ` +
				`"somebody said someone is watching"`)
		}
	})

	t.Run("attended + legacy ⇒ legacy, exactly today", func(t *testing.T) {
		yes := true
		got, r := Resolve(ResolveInput{WorkspaceBinding: legacyRow(), Attended: &yes,
			ScrubbedSystem: scrubbedRow()})
		if r != nil {
			t.Fatalf("Resolve: %s", r.Sentence())
		}
		if got.Env.Backend != BackendHostInherit || got.Downgraded {
			t.Errorf("an attended loop on an upgraded store lost the continuity EV20 promises: %q",
				got.Env.Backend)
		}
	})

	t.Run("unattended + legacy + no scrubbed row ⇒ refused, never inherited", func(t *testing.T) {
		no := false
		_, r := Resolve(ResolveInput{WorkspaceBinding: legacyRow(), Attended: &no, ScrubbedSystem: nil})
		if r == nil {
			t.Fatal("the command was allowed to run in the inherited environment because the scrubbed row " +
				"is gone — EV6's whole point is that this cannot happen")
		}
		if r.Kind != RefusalEnvironmentUnavailable {
			t.Errorf("kind = %q, want %q", r.Kind, RefusalEnvironmentUnavailable)
		}
		if !strings.Contains(r.Do, "host-scrubbed") {
			t.Errorf("the refusal does not name the fix: %s", r.Sentence())
		}
	})

	t.Run("a non-legacy binding is never downgraded, unattended or not", func(t *testing.T) {
		no := false
		got, r := Resolve(ResolveInput{WorkspaceBinding: containerRow(), Attended: &no})
		if r != nil {
			t.Fatalf("Resolve: %s", r.Sentence())
		}
		if got.Downgraded || got.Env.Backend != BackendContainer {
			t.Errorf("EV6 is the ONLY tightening the daemon makes, and it applies to inherit→scrub: %+v", got)
		}
	})
}

func TestNoBindingRefuses(t *testing.T) {
	_, r := Resolve(ResolveInput{Attended: nil, ScrubbedSystem: scrubbedRow()})
	if r == nil {
		t.Fatal("a step with no binding anywhere ran anyway; the fallback that would have saved it is the " +
			"state this slice forbids (EV2)")
	}
	if r.Kind != RefusalEnvironmentUnbound {
		t.Errorf("kind = %q, want %q", r.Kind, RefusalEnvironmentUnbound)
	}
	for _, want := range []string{"no execution environment is bound", "aeon env bind"} {
		if !strings.Contains(r.Sentence(), want) {
			t.Errorf("the refusal is missing %q: %s", want, r.Sentence())
		}
	}
}

func TestEffectiveIsLegacyAnswersTheDoctor(t *testing.T) {
	if !EffectiveIsLegacy(legacyRow()) {
		t.Error("the legacy workspace row did not read as legacy; the panel's warning banner is EV6's cost")
	}
	if EffectiveIsLegacy(scrubbedRow()) || EffectiveIsLegacy(nil) {
		t.Error("a scrubbed or absent workspace binding read as legacy")
	}
	// A record that says host-inherit without the legacy flag is still the
	// inherited environment — the flag is a convenience, the backend is truth.
	if !EffectiveIsLegacy(&BoundEnv{Backend: BackendHostInherit}) {
		t.Error("backend host-inherit without legacy=1 read as safe")
	}
}

func boolPtr(b bool) *bool { return &b }
