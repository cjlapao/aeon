package action

// S57 T2 — the merge gate over the REAL GitHub implementation (not the
// doubles): red CI refuses the merge BEFORE the forge is asked, green
// CI merges with the checks recorded in the evidence, and the review's
// submitted_at lands in review_at. The wire bodies are committed files
// under testdata/gate_github/ served by a loopback server — the suite
// dials no live host (I7's tripwire covers the repo-wide name).

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/forge"
)

// gateGHServer stands the loopback forge: the one path→fixture table
// below, every body a committed file.
func gateGHServer(t *testing.T, checkruns string) *forge.GitHub {
	t.Helper()
	dir := filepath.Join("testdata", "gate_github")
	body := func(name string) []byte {
		raw, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("fixture missing: %v", err)
		}
		return raw
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/widget/pulls/9":
			w.Write(body("pr.json"))
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/widget/pulls/9/reviews":
			w.Write(body("reviews.json"))
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/repos/acme/widget/commits/aaa/check-runs"):
			w.Write(body(checkruns))
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/widget/commits/aaa/status":
			w.Write(body("status.json"))
		case r.Method == http.MethodPut && r.URL.Path == "/repos/acme/widget/pulls/9/merge":
			w.Write(body("merge.json"))
		default:
			t.Errorf("the gate asked the forge for %s %s — no fixture for that", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return forge.New(forge.Config{
		Owner:       "acme",
		Repo:        "widget",
		BaseURL:     srv.URL, // loopback ONLY — never the real host (O7)
		TokenSource: func() (string, error) { return "fake-token", nil },
		MaxRetries:  0,
	})
}

func gateGHActions(t *testing.T, checkruns string) (*ForgeActions, *mergeProbe) {
	t.Helper()
	a := &ForgeActions{Forge: gateGHServer(t, checkruns), Merge: &mergeProbe{}}
	return a, a.Merge.(*mergeProbe)
}

// TestMergeGateRefusesRedCIOnGitHub — the gate goes LIVE on GitHub:
// ListStatuses now reads the checks of the PR's head SHA, and a check
// that is not green refuses the merge before the forge is asked (the
// probe must not have been told to merge).
func TestMergeGateRefusesRedCIOnGitHub(t *testing.T) {
	a, probe := gateGHActions(t, "checkruns.json")
	_, err := a.MergePR(context.Background(), mergeArgsFor(9))
	if err == nil {
		t.Fatal("a red check refuses the merge — that is the whole gate")
	}
	if !strings.Contains(err.Error(), "check test is "+forge.CheckFailure) {
		t.Fatalf("the refusal names the red check verbatim: %v", err)
	}
	if !strings.Contains(err.Error(), "not green") {
		t.Fatalf("the refusal says the check is not green: %v", err)
	}
	if len(probe.queries) != 0 {
		t.Fatalf("a refusal happens BEFORE the forge is asked — the post-merge probe must be untouched: %v", probe.queries)
	}
}

// The green arm: the same wire, every check success — the merge runs
// and the evidence carries the checks exactly as the forge reported
// them, plus the review's own timestamp (VG8's "the only change in
// production behaviour is checks becoming real", pinned end to end).
func TestMergeGateMergesGreenCIOnGitHub(t *testing.T) {
	a, _ := gateGHActions(t, "checkruns_green.json")
	res, err := a.MergePR(context.Background(), mergeArgsFor(9))
	if err != nil {
		t.Fatalf("green CI merges: %v", err)
	}
	if res.SHA == "" {
		t.Fatal("the merge did not run")
	}
	if got := res.Fields["checks"]; got != "build="+forge.CheckSuccess+" test="+forge.CheckSuccess {
		t.Fatalf("the evidence records the checks verbatim, got %q", got)
	}
	if got := res.Fields["review_at"]; got != "2026-11-01T10:00:00Z" {
		t.Fatalf("review_at carries the server's submitted_at: %q", got)
	}
	if got := res.Fields["review_by"]; got != "mira" {
		t.Fatalf("review_by is the reviewer: %q", got)
	}
}

// TestReviewSubmittedAtIsSet — over the FAKE, the other half of the
// pair: a review the server timestamped must reach the gate's evidence,
// and one it did not must read as the recorded absence.
func TestReviewSubmittedAtIsSet(t *testing.T) {
	f := forge.NewFake()
	f.SeedPR(forge.PR{Number: 3, Head: "task/4a17-version/4a17", BaseRef: "feature/4a17-version"})
	f.SeedReviewAt(3, "lgtm", forge.VerdictApprove, "2026-10-26T09:12:00Z")
	a := &ForgeActions{Forge: f, Merge: &mergeProbe{}}
	res, err := a.MergePR(context.Background(), mergeArgsFor(3))
	if err != nil {
		t.Fatalf("the merge failed: %v", err)
	}
	if got := res.Fields["review_at"]; got != "2026-10-26T09:12:00Z" {
		t.Fatalf("a timestamped review lands in review_at verbatim, got %q", got)
	}

	// The absence arm: no submitted_at, and the evidence records the
	// absence rather than inventing a clock reading.
	f2 := forge.NewFake()
	f2.SeedPR(forge.PR{Number: 3, Head: "task/4a17-version/4a17", BaseRef: "feature/4a17-version"})
	f2.SeedReview(3, "lgtm", forge.VerdictApprove)
	a2 := &ForgeActions{Forge: f2, Merge: &mergeProbe{}}
	res2, err := a2.MergePR(context.Background(), mergeArgsFor(3))
	if err != nil {
		t.Fatalf("the merge failed: %v", err)
	}
	if got := res2.Fields["review_at"]; got != "" {
		t.Fatalf("an untimestamped review records the empty string, not an invented clock: %q", got)
	}
}
