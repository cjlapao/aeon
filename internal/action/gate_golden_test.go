package action

// gate_golden_test.go — S57 T1 (characterization, P2 Phase A). Every branch
// of `mergeGate` + the two `MergePR` guards, driven through the REAL
// `ForgeActions.MergePR` with `forge.Fake` and the `gateForge`-style doubles
// on the untouched tree (1ae086c0). Each case pins (refusal sentence
// VERBATIM, the gate's evidence map, the recorded SHA, the forge call log).
// T2 changes `checks` ONLY on GitHub (these Fake/double rows are untouched
// by it); T6 refactors the evaluation and MUST leave every sentence in
// these goldens byte-identical — the baseline is the contract, the diff is
// the report (VG20's "the merge gate is a refactor, not a rewrite").
//
// Determinism: fixed PR (#3) and branch names (mergeArgsFor), fixed review
// ids/authors/timestamps, a fixed probe, one fresh fake per row.
//
// Generation (run once, on the untouched tree):
//
//	AEON_WRITE_GOLDEN=1 go test ./internal/action/ -run TestMergeGateBaselineByteIdentical -count=1 -tags sqlite_fts5

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/cjlapao/aeon/internal/forge"
)

// goldGateRow is one pinned branch: the refusal (verbatim) or the success
// (SHA + evidence), plus the call log that proves the order — a refusal
// BEFORE the forge is asked has no "MergePR" entry, and that absence is the
// property (the same shape as S11's AllowToken).
type goldGateRow struct {
	Case    string `json:"case"`
	Refused bool   `json:"refused"`
	// PostMerge marks the one refusal that happens AFTER the irreversible
	// call (the phantom-merge guard, D5): its row legitimately carries a
	// MergePR entry — the merge was asked, and the tree's answer overruled
	// it. Every other refusal happens BEFORE the forge is asked.
	PostMerge     bool              `json:"postMerge,omitempty"`
	Sentence      string            `json:"sentence,omitempty"`
	AlreadyMerged bool              `json:"alreadyMerged,omitempty"`
	SHA           string            `json:"sha,omitempty"`
	Evidence      map[string]string `json:"evidence,omitempty"`
	Calls         []string          `json:"calls"`
}

const goldGateDir = "gate_golden"

// goldChecksForge: the server claims the status capability and answers a
// scripted error (not-found = absence, transport = refusal).
type goldChecksForge struct {
	*gateForge
	checksErr *forge.ForgeError
}

func (g *goldChecksForge) ListStatuses(ctx context.Context, pr int) ([]forge.Check, error) {
	if g.checksErr != nil {
		return nil, g.checksErr
	}
	return g.gateForge.ListStatuses(ctx, pr)
}

// goldThreadsForge: the server claims the thread capability and answers a
// scripted error (not-found = absence, refused = absence, transport =
// refusal).
type goldThreadsForge struct {
	*gateForge
	threadsErr *forge.ForgeError
}

func (g *goldThreadsForge) ListUnresolvedThreads(ctx context.Context, pr int) ([]forge.Thread, error) {
	if g.threadsErr != nil {
		return nil, g.threadsErr
	}
	return g.gateForge.ListUnresolvedThreads(ctx, pr)
}

// goldThreadlessForge is the L34 reference server AFTER S57 T2 gave
// forge.Fake the optional capabilities (StatusLister, CheckReader): a
// delegate with exactly the ten Forge methods and nothing else, so the
// gate's no-StatusLister arm — "the server reports no status API" —
// stays pinned on a server that genuinely has none. (Embedding the fake
// would promote its new methods and take the arm off the table.)
type goldThreadlessForge struct{ fake *forge.Fake }

func (g goldThreadlessForge) FindOpenIssue(ctx context.Context, title string, label string) (*forge.Issue, error) {
	return g.fake.FindOpenIssue(ctx, title, label)
}

func (g goldThreadlessForge) OpenIssue(ctx context.Context, req forge.IssueRequest) (*forge.Issue, error) {
	return g.fake.OpenIssue(ctx, req)
}

func (g goldThreadlessForge) ReadIssue(ctx context.Context, number int) (*forge.Issue, error) {
	return g.fake.ReadIssue(ctx, number)
}

func (g goldThreadlessForge) CloseIssue(ctx context.Context, number int) error {
	return g.fake.CloseIssue(ctx, number)
}

func (g goldThreadlessForge) FindOpenPR(ctx context.Context, head string) (*forge.PR, error) {
	return g.fake.FindOpenPR(ctx, head)
}

func (g goldThreadlessForge) OpenPR(ctx context.Context, req forge.PRRequest) (*forge.PR, error) {
	return g.fake.OpenPR(ctx, req)
}

func (g goldThreadlessForge) ReadPR(ctx context.Context, number int) (*forge.PR, error) {
	return g.fake.ReadPR(ctx, number)
}

func (g goldThreadlessForge) MergePR(ctx context.Context, number int, method forge.MergeMethod) (*forge.MergeResult, error) {
	return g.fake.MergePR(ctx, number, method)
}

func (g goldThreadlessForge) ListReviews(ctx context.Context, pr int) ([]forge.Review, error) {
	return g.fake.ListReviews(ctx, pr)
}

func (g goldThreadlessForge) PostReview(ctx context.Context, pr int, body string, verdict forge.Verdict) (*forge.Review, error) {
	return g.fake.PostReview(ctx, pr, body, verdict)
}

// goldClearedBase reaches the guard's "cannot say what it merges into" arm:
// SeedPR refuses to seed an empty BaseRef (it is the guard's own
// precondition), so the arm is exercised by delegating every read and
// reporting an empty BaseRef on PR #3 — the one shape the guard's own
// message describes.
type goldClearedBase struct{ *forge.Fake }

func (g *goldClearedBase) ReadPR(ctx context.Context, number int) (*forge.PR, error) {
	pr, err := g.Fake.ReadPR(ctx, number)
	if err != nil {
		return nil, err
	}
	cp := *pr
	cp.BaseRef = ""
	return &cp, nil
}

type goldGateCase struct {
	// forge builds the scenario and returns (the call-log owner, the
	// forge MergePR sees, the post-merge probe). The call-log owner is
	// always the underlying *forge.Fake.
	forge     func() (*forge.Fake, forge.Forge, MergeProbe)
	wantErr   bool
	postMerge bool // the refusal happens after the merge call (D5 guard)
}

func goldGateCases() []struct {
	name  string
	case_ goldGateCase
} {
	approve := func() *gateForge {
		return &gateForge{Fake: gateFake(), reviews: []forge.Review{approvedBy(1, "mira", "2026-10-26T09:12:00Z")}}
	}
	return []struct {
		name  string
		case_ goldGateCase
	}{
		{
			name: "no-reviews",
			case_: goldGateCase{
				forge: func() (*forge.Fake, forge.Forge, MergeProbe) {
					g := &gateForge{Fake: gateFake()}
					return g.Fake, g, goldGateProbe()
				},
				wantErr: true,
			},
		},
		{
			name: "latest-comment",
			case_: goldGateCase{
				forge: func() (*forge.Fake, forge.Forge, MergeProbe) {
					g := &gateForge{Fake: gateFake(), reviews: []forge.Review{
						{ID: 1, PR: 3, Verdict: forge.VerdictComment, Author: "mira", SubmittedAt: "2026-10-26T09:12:00Z"},
					}}
					return g.Fake, g, goldGateProbe()
				},
				wantErr: true,
			},
		},
		{
			name: "latest-request-changes",
			case_: goldGateCase{
				forge: func() (*forge.Fake, forge.Forge, MergeProbe) {
					g := &gateForge{Fake: gateFake(), reviews: []forge.Review{
						{ID: 1, PR: 3, Verdict: forge.VerdictApprove, Author: "mira", SubmittedAt: "2026-10-26T09:00:00Z"},
						{ID: 2, PR: 3, Verdict: forge.VerdictRequestChanges, Author: "mira", SubmittedAt: "2026-10-26T09:12:00Z"},
					}}
					return g.Fake, g, goldGateProbe()
				},
				wantErr: true,
			},
		},
		{
			name: "outstanding-rc-cleared-by-same-author",
			case_: goldGateCase{
				forge: func() (*forge.Fake, forge.Forge, MergeProbe) {
					g := &gateForge{Fake: gateFake(), reviews: []forge.Review{
						{ID: 1, PR: 3, Verdict: forge.VerdictRequestChanges, Author: "mira", SubmittedAt: "2026-10-26T09:00:00Z"},
						{ID: 2, PR: 3, Verdict: forge.VerdictApprove, Author: "mira", SubmittedAt: "2026-10-26T09:12:00Z"},
					}}
					return g.Fake, g, goldGateProbe()
				},
			},
		},
		{
			name: "outstanding-rc-answered-by-other-author",
			case_: goldGateCase{
				forge: func() (*forge.Fake, forge.Forge, MergeProbe) {
					g := &gateForge{Fake: gateFake(), reviews: []forge.Review{
						{ID: 1, PR: 3, Verdict: forge.VerdictRequestChanges, Author: "mira", SubmittedAt: "2026-10-26T09:00:00Z"},
						{ID: 2, PR: 3, Verdict: forge.VerdictApprove, Author: "joao", SubmittedAt: "2026-10-26T09:12:00Z"},
					}}
					return g.Fake, g, goldGateProbe()
				},
				wantErr: true,
			},
		},
		{
			// No StatusLister at all — the threadless server's checks
			// arm. S57 T2 gave forge.Fake the capability, so the case
			// stands the delegate (exactly the ten Forge methods) in.
			name: "approve-no-statuslister",
			case_: goldGateCase{
				forge: func() (*forge.Fake, forge.Forge, MergeProbe) {
					f := gateFake()
					if _, err := f.PostReview(context.Background(), 3, "lgtm", forge.VerdictApprove); err != nil {
						panic(err)
					}
					return f, goldThreadlessForge{fake: f}, goldGateProbe()
				},
			},
		},
		{
			name: "approve-statuses-all-success",
			case_: goldGateCase{
				forge: func() (*forge.Fake, forge.Forge, MergeProbe) {
					g := approve()
					g.checks = []forge.Check{{Name: "build", State: "success"}, {Name: "test", State: "success"}}
					g.checksN = true
					return g.Fake, g, goldGateProbe()
				},
			},
		},
		{
			name: "approve-statuses-one-failure",
			case_: goldGateCase{
				forge: func() (*forge.Fake, forge.Forge, MergeProbe) {
					g := approve()
					g.checks = []forge.Check{{Name: "build", State: "success"}, {Name: "test", State: "failure"}}
					g.checksN = true
					return g.Fake, g, goldGateProbe()
				},
				wantErr: true,
			},
		},
		{
			name: "approve-statuses-one-pending",
			case_: goldGateCase{
				forge: func() (*forge.Fake, forge.Forge, MergeProbe) {
					g := approve()
					g.checks = []forge.Check{{Name: "build", State: "success"}, {Name: "test", State: "pending"}}
					g.checksN = true
					return g.Fake, g, goldGateProbe()
				},
				wantErr: true,
			},
		},
		{
			// The server CLAIMS the capability and answers not-found:
			// absence passes, recorded.
			name: "approve-statuses-not-found",
			case_: goldGateCase{
				forge: func() (*forge.Fake, forge.Forge, MergeProbe) {
					f := gateFake()
					g := &goldChecksForge{gateForge: &gateForge{Fake: f, reviews: []forge.Review{approvedBy(1, "mira", "2026-10-26T09:12:00Z")}},
						checksErr: forge.NewMsg(forge.KindNotFound, "no status API (scenario)")}
					return f, g, goldGateProbe()
				},
			},
		},
		{
			// The server claims the capability and the read fails
			// transport: unverifiable is NOT green before something
			// irreversible (O5).
			name: "approve-statuses-transport",
			case_: goldGateCase{
				forge: func() (*forge.Fake, forge.Forge, MergeProbe) {
					f := gateFake()
					g := &goldChecksForge{gateForge: &gateForge{Fake: f, reviews: []forge.Review{approvedBy(1, "mira", "2026-10-26T09:12:00Z")}},
						checksErr: forge.NewMsg(forge.KindTransport, "connection refused (scenario)")}
					return f, g, goldGateProbe()
				},
				wantErr: true,
			},
		},
		{
			name: "approve-threads-none",
			case_: goldGateCase{
				forge: func() (*forge.Fake, forge.Forge, MergeProbe) {
					g := approve()
					return g.Fake, g, goldGateProbe()
				},
			},
		},
		{
			name: "approve-threads-some",
			case_: goldGateCase{
				forge: func() (*forge.Fake, forge.Forge, MergeProbe) {
					g := approve()
					g.threads = []forge.Thread{{Path: "src/x.go", Line: 12, Author: "mira"}}
					return g.Fake, g, goldGateProbe()
				},
				wantErr: true,
			},
		},
		{
			// The server claims threads and answers not-found: honoured as
			// absence, never a blocker.
			name: "approve-threads-not-found",
			case_: goldGateCase{
				forge: func() (*forge.Fake, forge.Forge, MergeProbe) {
					f := gateFake()
					g := &goldThreadsForge{gateForge: &gateForge{Fake: f, reviews: []forge.Review{approvedBy(1, "mira", "2026-10-26T09:12:00Z")}},
						threadsErr: forge.NewMsg(forge.KindNotFound, "no threads API (scenario)")}
					return f, g, goldGateProbe()
				},
			},
		},
		{
			// No ThreadLister (and, since S57 T2, the checks arm needs
			// the delegate too, or the fake's new capability would move
			// the pinned checks sentence): the L34 reference server.
			name: "approve-threads-unsupported",
			case_: goldGateCase{
				forge: func() (*forge.Fake, forge.Forge, MergeProbe) {
					f := gateFake()
					if _, err := f.PostReview(context.Background(), 3, "lgtm", forge.VerdictApprove); err != nil {
						panic(err)
					}
					return f, goldThreadlessForge{fake: f}, goldGateProbe()
				},
			},
		},
		{
			name: "reviews-unreadable",
			case_: goldGateCase{
				forge: func() (*forge.Fake, forge.Forge, MergeProbe) {
					f := gateFake()
					f.FailMethod("ListReviews", forge.NewMsg(forge.KindTransport, "connection reset (scenario)"))
					return f, f, goldGateProbe()
				},
				wantErr: true,
			},
		},
		{
			name: "already-merged",
			case_: goldGateCase{
				forge: func() (*forge.Fake, forge.Forge, MergeProbe) {
					f := forge.NewFake()
					f.SeedPR(forge.PR{Number: 3, Head: "task/4a17-version/4a17", BaseRef: "feature/4a17-version", Merged: true, MergeSHA: "deadbeefcafe"})
					return f, f, goldGateProbe()
				},
			},
		},
		{
			name: "base-ref-empty",
			case_: goldGateCase{
				forge: func() (*forge.Fake, forge.Forge, MergeProbe) {
					f := gateFake()
					if _, err := f.PostReview(context.Background(), 3, "lgtm", forge.VerdictApprove); err != nil {
						panic(err)
					}
					return f, &goldClearedBase{Fake: f}, goldGateProbe()
				},
				wantErr: true,
			},
		},
		{
			name: "base-ref-wrong",
			case_: goldGateCase{
				forge: func() (*forge.Fake, forge.Forge, MergeProbe) {
					f := forge.NewFake()
					f.SeedPR(forge.PR{Number: 3, Head: "task/4a17-version/4a17", BaseRef: "main"})
					if _, err := f.PostReview(context.Background(), 3, "lgtm", forge.VerdictApprove); err != nil {
						panic(err)
					}
					return f, f, goldGateProbe()
				},
				wantErr: true,
			},
		},
		{
			// The merge lands but the tree says the SHA is not an ancestor
			// of the feature branch: the tree wins (D5).
			name: "phantom-merge",
			case_: goldGateCase{
				forge: func() (*forge.Fake, forge.Forge, MergeProbe) {
					f := gateFake()
					if _, err := f.PostReview(context.Background(), 3, "lgtm", forge.VerdictApprove); err != nil {
						panic(err)
					}
					// S57 T2: the delegate keeps the call log byte-
					// identical (a plain fake would add the checks
					// reads the golden does not carry).
					return f, goldThreadlessForge{fake: f}, &goldNoAncestorProbe{}
				},
				wantErr:   true,
				postMerge: true,
			},
		},
	}
}

func goldGateProbe() *mergeProbe { return &mergeProbe{} }

// goldNoAncestorProbe always answers "not an ancestor": the phantom-merge
// arm is the one where the FORGE reported a merge and the TREE says that
// sha never landed on the feature branch. The probe's scripted-true
// default cannot express that, so the case gets its own probe.
type goldNoAncestorProbe struct{ queries []string }

func (p *goldNoAncestorProbe) IsAncestor(_ context.Context, _, sha, _ string) (bool, error) {
	p.queries = append(p.queries, sha)
	return false, nil
}

func writeGoldGateRow(t *testing.T, row goldGateRow) {
	t.Helper()
	dir := filepath.Join("testdata", goldGateDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := json.MarshalIndent(row, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, row.Case+".json"), append(data, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

func compareGoldGateRow(t *testing.T, name string, want, got goldGateRow) {
	t.Helper()
	if want.Refused != got.Refused {
		t.Fatalf("%s: refused=%v, golden says %v", name, got.Refused, want.Refused)
	}
	if want.Refused && want.Sentence != got.Sentence {
		t.Fatalf("%s: refusal sentence drifted\n  now:  %q\n  gold: %q", name, got.Sentence, want.Sentence)
	}
	if !want.Refused {
		if want.SHA != got.SHA {
			t.Fatalf("%s: sha drifted: now %q, gold %q", name, got.SHA, want.SHA)
		}
		if want.AlreadyMerged != got.AlreadyMerged {
			t.Fatalf("%s: alreadyMerged drifted: now %v, gold %v", name, got.AlreadyMerged, want.AlreadyMerged)
		}
		if !reflect.DeepEqual(want.Evidence, got.Evidence) {
			t.Fatalf("%s: evidence drifted\n  now:  %#v\n  gold: %#v", name, got.Evidence, want.Evidence)
		}
	}
	if !reflect.DeepEqual(want.Calls, got.Calls) {
		t.Fatalf("%s: forge call log drifted\n  now:  %v\n  gold: %v", name, got.Calls, want.Calls)
	}
}

// TestMergeGateBaselineByteIdentical (T6: renamed from TestMergeGateGoldens)
// — every gate branch pinned (refusal sentence verbatim, evidence map,
// recorded SHA, forge call log). The AEON_WRITE_GOLDEN run on the untouched
// tree generated the fixtures; afterwards this test only READS them and
// fails on any drift, naming the case and the field. It is the VG11 half of
// T6: the refactor moved the evaluation into mergerule.Evaluate under the
// effective rule, and the BASELINE (no rule row wired — a nil RuleSource)
// must reproduce the T1 gate byte for byte. The absence of a "MergePR"
// entry in a refusal row's call log is the ordering property (the gate is
// reached before the irreversible call, never after).
func TestMergeGateBaselineByteIdentical(t *testing.T) {
	write := os.Getenv("AEON_WRITE_GOLDEN") == "1"
	for _, tc := range goldGateCases() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			fake, fg, probe := tc.case_.forge()
			a := &ForgeActions{Forge: fg, Merge: probe}
			res, err := a.MergePR(context.Background(), mergeArgsFor(3))

			row := goldGateRow{Case: tc.name, PostMerge: tc.case_.postMerge}
			if err != nil {
				row.Refused = true
				row.Sentence = err.Error()
				if !tc.case_.wantErr {
					t.Fatalf("case %s expected to pass but was refused: %v", tc.name, err)
				}
			} else {
				if tc.case_.wantErr {
					t.Fatalf("case %s expected to be refused but merged", tc.name)
				}
				row.SHA = res.SHA
				row.AlreadyMerged = res.AlreadyMerged
				row.Evidence = res.Fields
			}
			for _, c := range fake.Calls() {
				row.Calls = append(row.Calls, c.Method)
			}
			if write {
				writeGoldGateRow(t, row)
				return
			}
			data, err := os.ReadFile(filepath.Join("testdata", goldGateDir, tc.name+".json"))
			if err != nil {
				t.Fatalf("golden %s missing (generate with AEON_WRITE_GOLDEN=1 on the untouched tree): %v", tc.name, err)
			}
			var want goldGateRow
			if err := json.Unmarshal(data, &want); err != nil {
				t.Fatal(err)
			}
			compareGoldGateRow(t, tc.name, want, row)
		})
	}
}

// TestMergeGateRefusalsLeaveNoMergeCall — the ordering property, checked
// over the goldens themselves: a pre-merge refusal's call log contains no
// "MergePR" entry (the refusal happened before the irreversible call); an
// already-merged success contains none either (the early return asks the
// forge nothing); a fresh merge and the post-merge phantom refusal contain
// it exactly once.
func TestMergeGateRefusalsLeaveNoMergeCall(t *testing.T) {
	entries, err := os.ReadDir(filepath.Join("testdata", goldGateDir))
	if err != nil {
		t.Fatalf("golden dir missing: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("no goldens found — generate them first")
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join("testdata", goldGateDir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		var row goldGateRow
		if err := json.Unmarshal(data, &row); err != nil {
			t.Fatal(err)
		}
		n := 0
		for _, c := range row.Calls {
			if c == "MergePR" {
				n++
			}
		}
		// The property, per row shape: a pre-merge refusal and an
		// already-merged success NEVER ask the forge to merge; a fresh
		// merge and the post-merge (phantom) refusal ask it exactly once.
		want := 1
		if (row.Refused && !row.PostMerge) || row.AlreadyMerged {
			want = 0
		}
		if n != want {
			t.Fatalf("%s: MergePR called %d times, want %d (refused=%v postMerge=%v alreadyMerged=%v)",
				e.Name(), n, want, row.Refused, row.PostMerge, row.AlreadyMerged)
		}
	}
}
