package daemon

// The walk found this code; these tests are why the walk exists — the
// shapes the CLI cannot reach, pinned at the unit.

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/cjlapao/aeon/internal/action"
	"github.com/cjlapao/aeon/internal/store"
)

func gitInitWithOrigin(t *testing.T, url string) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		c := exec.Command("git", args...)
		c.Dir = dir
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-b", "main", dir)
	if err := os.WriteFile(filepath.Join(dir, "f"), []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	run("config", "user.email", "t@t")
	run("config", "user.name", "t")
	run("add", "-A")
	run("commit", "-m", "seed")
	run("remote", "add", "origin", url)
	return dir
}

func TestGithubOriginForms(t *testing.T) {
	cases := []struct {
		url         string
		owner, repo string
		ok          bool
	}{
		{"https://github.com/acme/widget.git", "acme", "widget", true},
		{"git@github.com:acme/widget.git", "acme", "widget", true},
		{"ssh://git@github.com/acme/widget", "acme", "widget", true},
		{"https://gitlab.com/acme/widget.git", "", "", false}, // not a GitHub identity — never "close enough"
		{"https://github.com/acme", "", "", false},            // not owner/repo
		{"", "", "", false}, // no origin
	}
	for _, tc := range cases {
		dir := ""
		if tc.url != "" {
			dir = gitInitWithOrigin(t, tc.url)
		}
		o, r, ok := githubOrigin(dir)
		if ok != tc.ok || o != tc.owner || r != tc.repo {
			t.Fatalf("origin %q: got (%q,%q,%v), want (%q,%q,%v)", tc.url, o, r, ok, tc.owner, tc.repo, tc.ok)
		}
	}
}

// TestActionServicesNilForgeForNonGitHub — a GitLab-origin repository
// gets Git and Audit but NO Forge: outward forge actions then refuse
// naming the missing service, rather than dialling GitHub for someone
// else's repository.
func TestActionServicesNilForgeForNonGitHub(t *testing.T) {
	st, err := store.Open(store.Options{Dir: t.TempDir(), Env: func(string) string { return "" }})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	d := &Daemon{store: st}
	dir := gitInitWithOrigin(t, "https://gitlab.com/acme/widget.git")
	svc := d.actionServices(dir)
	if svc == nil || svc.Forge != nil {
		t.Fatal("a non-GitHub origin must not mint a forge client")
	}
	if svc.Git == nil || svc.Audit == nil {
		t.Fatal("Git and Audit belong to the repository, not the forge")
	}
	n, err := svc.Audit(context.Background())
	if err != nil || n != 0 {
		t.Fatalf("audit on an empty store reads zero, honestly: %d %v", n, err)
	}
}

// TestStoreIntentsRoundTripsTheOneTable — the adapter is the only
// translation between the port and the table, and OpenIntent returns the
// OPEN receipt (or nil) keyed with the receipt-first action's name.
func TestStoreIntentsRoundTripsTheOneTable(t *testing.T) {
	s, err := store.Open(store.Options{Dir: t.TempDir(), Env: func(string) string { return "" }})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ad := storeIntents{s: s}
	ctx := context.Background()
	// The FK is the point: a receipt belongs to a real step, which
	// belongs to a real loop — the whole parentage, through exported APIs.
	if _, err := s.CreateLoop(store.NewLoop{Slug: "4a17", ComponentRoot: "/repo", Root: "/repo", Goal: "walk"}); err != nil {
		t.Fatal(err)
	}
	step, err := s.CreateStep(store.Scope{LoopSlug: "4a17", ComponentRoot: "/repo"}, store.NewStep{Phase: "review", Agent: "code-reviewer"})
	if err != nil {
		t.Fatal(err)
	}
	stepID := step.ID
	in := action.Intent{Loop: "4a17", StepID: stepID, Attempt: 1, Action: action.ForgePostReview, PR: 7}
	if got, err := ad.OpenIntent(ctx, 7, stepID, 1); err != nil || got != nil {
		t.Fatalf("no receipt yet: %+v %v", got, err)
	}
	if err := ad.BeginIntent(ctx, in); err != nil {
		t.Fatal(err)
	}
	open, err := ad.OpenIntent(ctx, 7, stepID, 1)
	if err != nil || open == nil || open.ReviewID != 0 {
		t.Fatalf("posted-unknown receipt: %+v %v", open, err)
	}
	if err := ad.RecordIntent(ctx, in, 4321); err != nil {
		t.Fatal(err)
	}
	if got, err := ad.OpenIntent(ctx, 7, stepID, 1); err != nil || got != nil {
		t.Fatal("a recorded receipt is no longer open")
	}
	n, err := s.CountActionIntents()
	if err != nil || n != 1 {
		t.Fatalf("the audit count sees the table: %d %v", n, err)
	}
}
