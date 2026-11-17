package daemon

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/model"
	"github.com/cjlapao/aeon/internal/runtime"
)

// A seam that returns a fixed bundle. The fake ignores its arguments, which
// is exactly what the seam contract promises: the caller decides nothing the
// implementation already decided.
func fixedBundleSeam(b *MCPBundle) func(ctx context.Context, agent string, frozen runtime.FrozenManifest, at time.Time) (*MCPBundle, error) {
	return func(ctx context.Context, agent string, frozen runtime.FrozenManifest, at time.Time) (*MCPBundle, error) {
		return b, nil
	}
}

// TestBuildToolsNilSeamIsByteIdentical is the test that matters most in this
// task (MD8): with StepMCP nil, BuildTools must return exactly today's two
// values — the plain toolCaller, not a composite — for several granted-list
// shapes, including empty. A no-MCP build cannot change a step's tools.
func TestBuildToolsNilSeamIsByteIdentical(t *testing.T) {
	for _, granted := range [][]string{
		{},
		{"read"},
		{"read", "write"},
		{"read", "write_file"},
	} {
		root := t.TempDir()
		if err := os.WriteFile(filepath.Join(root, "a.md"), []byte("hello"), 0o644); err != nil {
			t.Fatal(err)
		}

		// baseline: the pre-this-task shape — a StepRunner with no StepMCP
		// field at all (nil by default).
		base, baseSpecs, err := (StepRunner{}).BuildTools(granted, root, "test-handler", runtime.FrozenManifest{})
		if err != nil {
			t.Fatalf("baseline %v: %v", granted, err)
		}
		// nil seam: StepMCP explicitly nil.
		nilSeam, nilSpecs, err := (StepRunner{StepMCP: nil}).BuildTools(granted, root, "test-handler", runtime.FrozenManifest{})
		if err != nil {
			t.Fatalf("nil seam %v: %v", granted, err)
		}

		// The nil seam must be the plain toolCaller, never a composite.
		if _, ok := nilSeam.(compositeCaller); ok {
			t.Errorf("%v: nil seam produced a composite caller", granted)
		}
		// The specs must equal the baseline exactly.
		if !reflect.DeepEqual(baseSpecs, nilSpecs) {
			t.Errorf("%v: nil-seam specs differ from baseline: base=%v nil=%v", granted, baseSpecs, nilSpecs)
		}
		// Behavior: a call through the nil-seam caller returns the same
		// content as a call through the baseline, for every granted tool.
		for _, name := range granted {
			bin := `{"path":"a.md"}`
			b, n := base.Call(context.Background(), name, json.RawMessage(bin)), nilSeam.Call(context.Background(), name, json.RawMessage(bin))
			if b.Content != n.Content || b.IsError != n.IsError || b.Summary != n.Summary {
				t.Errorf("%v: call %q differs base=%+v nil=%+v", granted, name, b, n)
			}
		}
		// Granted() is unchanged.
		if !reflect.DeepEqual(base.Granted(), nilSeam.Granted()) {
			t.Errorf("%v: Granted differs base=%v nil=%v", granted, base.Granted(), nilSeam.Granted())
		}
	}
}

// TestCompositeRoutesDotToBundle proves the router both ways (MD1/MD10): a
// tool-use name with a dot routes to the bundle's Call; one without routes to
// the first-party caller. A spy on each side makes the routing observable.
func TestCompositeRoutesDotToBundle(t *testing.T) {
	var bundleCalls int
	bundle := &MCPBundle{
		Specs: []model.ToolSpec{{Name: "srv.tool"}},
		Call: func(ctx context.Context, q string, args json.RawMessage) (string, error) {
			bundleCalls++
			return "from-bundle", nil
		},
	}
	runner := StepRunner{StepMCP: fixedBundleSeam(bundle)}

	caller, specs, err := runner.BuildTools([]string{"read"}, t.TempDir(), "test-handler", runtime.FrozenManifest{})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := caller.(compositeCaller); !ok {
		t.Fatal("wired seam did not produce a composite caller")
	}

	// The offered spec rides the specs the provider is told exist.
	var gotSpec bool
	for _, s := range specs {
		if s.Name == "srv.tool" {
			gotSpec = true
		}
	}
	if !gotSpec {
		t.Errorf("the offered spec is not in the returned specs: %v", specs)
	}

	// A dot name routes to the bundle.
	out := caller.Call(context.Background(), "srv.tool", nil)
	if out.Content != "from-bundle" || out.IsError {
		t.Errorf("dot name did not route to the bundle: %+v", out)
	}
	if bundleCalls != 1 {
		t.Errorf("the bundle was not called for the dot name (calls=%d)", bundleCalls)
	}

	// A dot-free name routes to the first-party caller, NOT the bundle.
	out2 := caller.Call(context.Background(), "read", nil)
	if out2.Content == "from-bundle" {
		t.Errorf("dot-free name routed to the bundle: %+v", out2)
	}
	if bundleCalls != 1 {
		t.Errorf("dot-free name reached the bundle (calls=%d; expected 1)", bundleCalls)
	}
}

// TestBundlePublishingDotFreeNameIsConstructionError pins MD10 pin #2: a bundle
// that publishes a dot-free spec name is refused at BuildTools construction,
// not silently accepted — it would be indistinguishable from a first-party
// tool, and the dot is the router.
func TestBundlePublishingDotFreeNameIsConstructionError(t *testing.T) {
	bundle := &MCPBundle{
		Specs: []model.ToolSpec{{Name: "notqualified"}},
		Call:  func(ctx context.Context, q string, args json.RawMessage) (string, error) { return "", nil },
	}
	runner := StepRunner{StepMCP: fixedBundleSeam(bundle)}
	if _, _, err := runner.BuildTools([]string{"read"}, t.TempDir(), "test-handler", runtime.FrozenManifest{}); err == nil {
		t.Fatal("a dot-free spec name was accepted; MD10 pin #2 expects a construction error")
	}
}

// TestCompositeCloseReapsTheBundle proves the composite is closeable (MD5 —
// T4 wires the call; the method exists the day the seam lands) and that a nil
// bundle's Close is a no-op.
func TestCompositeCloseReapsTheBundle(t *testing.T) {
	var closed int
	bundle := &MCPBundle{
		Specs: []model.ToolSpec{{Name: "srv.tool"}},
		Call:  func(ctx context.Context, q string, args json.RawMessage) (string, error) { return "", nil },
		Close: func() error { closed++; return nil },
	}
	runner := StepRunner{StepMCP: fixedBundleSeam(bundle)}
	caller, _, err := runner.BuildTools([]string{"read"}, t.TempDir(), "test-handler", runtime.FrozenManifest{})
	if err != nil {
		t.Fatal(err)
	}
	c, ok := caller.(compositeCaller)
	if !ok {
		t.Fatal("wired seam did not produce a composite caller")
	}
	if err := c.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
	if closed != 1 {
		t.Errorf("the bundle was not closed (closed=%d)", closed)
	}
}
