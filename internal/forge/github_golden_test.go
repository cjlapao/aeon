package forge

// github_golden_test.go — S57 T1 (characterization, P2 Phase A). The current
// wire round-trip of the five PR-facing methods — `OpenPR`, `ReadPR`,
// `ListReviews`, `PostReview`, `MergePR` — pinned on the untouched tree
// (1ae086c0) with the loopback `hub` and the COMMITTED fixtures: the request
// shape (method, path, query, the Authorization shape — redacted, never the
// token value, the Accept header, and the exact request BODY for the
// writes), the response → struct mapping (and exactly which wire fields the
// mapping DROPS today — `head.sha`, `submitted_at` — the surface T2 widens),
// and the error classification (auth / scope / protection / ambiguous / 404
// / 409 / 422 / rate-limit / 500) with the verbatim sentence and the bounded
// retry count.
//
// T2 changes the GitHub mapping to ADD the dropped fields; it must not
// change any request shape, any other mapped field, or any error sentence
// in these goldens — the diff is the report. The `dropped` list is the pin
// T2 is allowed to shorten (fields stop being dropped), never lengthen.
//
// The rate-limit sentence carries a wall-clock "in <duration>" (the reset
// window), not reproducible across days; that token is normalized to
// `in <Δ>` in the golden so the rest of the sentence stays byte-pinned.
//
// Generation (run once, on the untouched tree):
//
//	AEON_WRITE_GOLDEN=1 go test ./internal/forge/ -run TestGitHubRequestGoldens -count=1 -tags sqlite_fts5

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

const goldWireDir = "github_golden" // under testdata/

// goldWireRow is one pinned request/response. AuthShape is the redacted
// Authorization header; the sentinel token must never appear anywhere in
// the row (TestForgeNeverLogsTheToken asserts that over every golden).
type goldWireRow struct {
	Case        string   `json:"case"`
	Method      string   `json:"method"`
	Path        string   `json:"path"`
	Query       string   `json:"query,omitempty"`
	AuthShape   string   `json:"authShape"`
	Accept      string   `json:"accept,omitempty"`
	Body        string   `json:"body,omitempty"` // the exact request body the client sent
	Attempts    int      `json:"attempts"`
	WireJSON    string   `json:"wire,omitempty"`  // the exact body the server returned
	MappedJSON  string   `json:"mapped,omitempty"` // what the Go struct captured
	WireHas     []string `json:"wireHas,omitempty"`
	MappedHas   []string `json:"mappedHas,omitempty"`
	Dropped     []string `json:"dropped,omitempty"` // WireHas − MappedHas, what is LOST today
	ErrKind     string   `json:"errKind,omitempty"`
	ErrSentence string   `json:"errSentence,omitempty"` // verbatim err.Error()
}

// dropRateLimitClock normalizes the one non-reproducible token in the
// rate-limit sentence: "in 2m3s (epoch …)" → "in <Δ> (epoch …)".
var dropRateLimitClock = regexp.MustCompile(`in \S+ \(epoch`)

func normalizeSentence(s string) string {
	return dropRateLimitClock.ReplaceAllString(s, "in <Δ> (epoch")
}

// wireCase drives one method against a registered fixture. `call` runs the
// client method; `mapped` is the captured struct as JSON ("" on error).
type wireCase struct {
	name      string
	fixture   string
	fail      bool // the fixture is served as the failure (h.fail, not h.st)
	key       string
	call      func(g *GitHub) (mapped string, err error)
	wireHas   []string
	mappedHas []string
}

func goldWireCases() []wireCase {
	readPR := func(g *GitHub) (string, error) {
		pr, err := g.ReadPR(context.Background(), 7)
		if err != nil {
			return "", err
		}
		b, _ := json.Marshal(pr)
		return string(b), nil
	}
	listReviews := func(g *GitHub) (string, error) {
		revs, err := g.ListReviews(context.Background(), 7)
		if err != nil {
			return "", err
		}
		b, _ := json.Marshal(revs)
		return string(b), nil
	}
	failRead := func(g *GitHub) (string, error) {
		_, err := g.ReadPR(context.Background(), 7)
		return "", err
	}
	openPR := func(g *GitHub) (string, error) {
		pr, err := g.OpenPR(context.Background(), PRRequest{
			Title: "feat: version", Body: "b", Head: "task/4a17-version/4a17", Base: "feature/4a17-version",
		})
		if err != nil {
			return "", err
		}
		b, _ := json.Marshal(pr)
		return string(b), nil
	}
	postReview := func(g *GitHub) (string, error) {
		rev, err := g.PostReview(context.Background(), 7, "Verified against the daemon.", VerdictApprove)
		if err != nil {
			return "", err
		}
		b, _ := json.Marshal(rev)
		return string(b), nil
	}
	mergePR := func(g *GitHub) (string, error) {
		mr, err := g.MergePR(context.Background(), 7, MergeSquash)
		if err != nil {
			return "", err
		}
		b, _ := json.Marshal(mr)
		return string(b), nil
	}
	return []wireCase{
		{
			name: "openpr", fixture: "pr_opened.json", key: "POST /repos/acme/widget/pulls", call: openPR,
			wireHas:   []string{"number", "head.ref", "base.ref", "merged"},
			mappedHas: []string{"number", "head.ref", "base.ref", "merged"},
		},
		{
			name: "postreview", fixture: "review.json", key: "POST /repos/acme/widget/pulls/7/reviews", call: postReview,
			wireHas:   []string{"id", "body", "state", "user.login"},
			mappedHas: []string{"id", "body", "state", "user.login"},
		},
		{
			name: "mergepr", fixture: "merge.json", key: "PUT /repos/acme/widget/pulls/7/merge", call: mergePR,
			wireHas:   []string{"merged", "sha"},
			mappedHas: []string{"merged", "sha"},
		},
		{
			// S57 T2: head.sha is MAPPED now (PR.HeadSHA, the checks'
			// key) — the dropped list shortens by exactly one.
			name: "readpr", fixture: "pr.json", key: "/pulls/7", call: readPR,
			wireHas:   []string{"number", "head.ref", "head.sha", "base.ref", "base.sha", "merged", "state"},
			mappedHas: []string{"number", "head.ref", "head.sha", "base.ref", "merged"},
		},
		{
			name: "listreviews", fixture: "reviews.json",
			key: "GET /repos/acme/widget/pulls/7/reviews", call: listReviews,
			wireHas:   []string{"id", "body", "state", "user.login"},
			mappedHas: []string{"id", "body", "state", "user.login"},
		},
		{
			// S57 T2: the fixture CARRIES submitted_at and the mapping
			// KEEPS it now (Review.SubmittedAt; the gate's review_at).
			name: "listreviews-with-submitted-at", fixture: "gold_reviews_full.json",
			key: "GET /repos/acme/widget/pulls/7/reviews", call: listReviews,
			wireHas:   []string{"id", "body", "state", "user.login", "submitted_at"},
			mappedHas: []string{"id", "body", "state", "user.login", "submitted_at"},
		},
		{
			name: "readpr-404", fixture: "err_404.json", fail: true, key: "/pulls/7", call: failRead,
		},
		{
			name: "readpr-409-conflict", fixture: "err_409.json", fail: true, key: "/pulls/7", call: failRead,
		},
		{
			name: "readpr-422-validation", fixture: "gold_422_validation.json", fail: true, key: "/pulls/7", call: failRead,
		},
		{
			name: "readpr-422-duplicate", fixture: "err_422_duplicate.json", fail: true, key: "/pulls/7", call: failRead,
		},
		{
			name: "readpr-401-auth", fixture: "err_401.json", fail: true, key: "/pulls/7", call: failRead,
		},
		{
			name: "readpr-403-scope", fixture: "err_403_scope.json", fail: true, key: "/pulls/7", call: failRead,
		},
		{
			name: "readpr-403-protection", fixture: "err_403_protection.json", fail: true, key: "/pulls/7", call: failRead,
		},
		{
			name: "readpr-403-ambiguous", fixture: "err_403_ambiguous.json", fail: true, key: "/pulls/7", call: failRead,
		},
		{
			name: "readpr-403-ratelimit", fixture: "err_403_ratelimit.json", fail: true, key: "/pulls/7", call: failRead,
		},
		{
			name: "readpr-500", fixture: "err_500.json", fail: true, key: "/pulls/7", call: failRead,
		},
	}
}

// requestOf splits a hub hit line into method/path/query.
func requestOf(hit string) (method, path, query string) {
	sp := strings.SplitN(hit, " ", 2)
	method = sp[0]
	rest := sp[1]
	if i := strings.IndexByte(rest, '?'); i >= 0 {
		return method, rest[:i], rest[i+1:]
	}
	return method, rest, ""
}

// wireBody returns the raw fixture body (the wire side of the mapping).
func wireBody(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	var env struct {
		Body json.RawMessage `json:"body"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatal(err)
	}
	return string(env.Body)
}

func diffDrop(wire, mapped []string) []string {
	has := map[string]bool{}
	for _, m := range mapped {
		has[m] = true
	}
	var out []string
	for _, w := range wire {
		if !has[w] {
			out = append(out, w)
		}
	}
	return out
}

func writeGoldWireRow(t *testing.T, row goldWireRow) {
	t.Helper()
	dir := filepath.Join("testdata", goldWireDir)
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

func compareGoldWireRow(t *testing.T, name string, want, got goldWireRow) {
	t.Helper()
	fields := []struct{ label, want, got string }{
		{"method", want.Method, got.Method},
		{"path", want.Path, got.Path},
		{"query", want.Query, got.Query},
		{"authShape", want.AuthShape, got.AuthShape},
		{"accept", want.Accept, got.Accept},
		{"body", want.Body, got.Body},
		{"attempts", strconv.Itoa(want.Attempts), strconv.Itoa(got.Attempts)},
		{"wire", want.WireJSON, got.WireJSON},
		{"mapped", want.MappedJSON, got.MappedJSON},
		{"errKind", want.ErrKind, got.ErrKind},
		{"errSentence", want.ErrSentence, got.ErrSentence},
	}
	for _, f := range fields {
		if f.want != f.got {
			t.Fatalf("%s: %s drifted\n  now:  %q\n  gold: %q", name, f.label, f.got, f.want)
		}
	}
	if !equalStrings(want.Dropped, got.Dropped) {
		t.Fatalf("%s: dropped drifted\n  now:  %v\n  gold: %v", name, got.Dropped, want.Dropped)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestGitHubRequestGoldens — the current wire round-trip, pinned. The
// AEON_WRITE_GOLDEN run on the untouched tree generated the fixtures;
// afterwards this test only READS them and fails on any drift, naming the
// case and the field.
func TestGitHubRequestGoldens(t *testing.T) {
	write := os.Getenv("AEON_WRITE_GOLDEN") == "1"
	for _, tc := range goldWireCases() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			h := newHub(t)
			if tc.fail {
				h.fail[tc.key] = tc.fixture
			} else {
				h.st[tc.key] = tc.fixture
			}
			g := h.client()
			mapped, err := tc.call(g)

			row := goldWireRow{Case: tc.name}
			if len(h.hits) > 0 {
				row.Method, row.Path, row.Query = requestOf(h.hits[0])
			}
			if len(h.seen) > 0 {
				// Bearer <sentinel> → Bearer <redacted>: the golden proves
				// the header is SENT, never what it carries.
				row.AuthShape = "Bearer <redacted>"
				row.Accept = h.seen[0].Get("Accept")
			}
			if len(h.body) > 0 && h.body[0] != "" {
				row.Body = h.body[0]
			}
			row.Attempts = h.attempts()
			if tc.wireHas != nil {
				row.WireHas = tc.wireHas
				row.MappedHas = tc.mappedHas
				row.Dropped = diffDrop(tc.wireHas, tc.mappedHas)
				row.WireJSON = wireBody(t, tc.fixture)
			}
			row.MappedJSON = mapped
			if err != nil {
				row.ErrSentence = normalizeSentence(err.Error())
				var fe *ForgeError
				if asForge(err, &fe) {
					row.ErrKind = string(fe.Kind)
				}
			}
			if write {
				writeGoldWireRow(t, row)
				return
			}
			data, rerr := os.ReadFile(filepath.Join("testdata", goldWireDir, tc.name+".json"))
			if rerr != nil {
				t.Fatalf("golden %s missing (generate with AEON_WRITE_GOLDEN=1 on the untouched tree): %v", tc.name, rerr)
			}
			var want goldWireRow
			if err := json.Unmarshal(data, &want); err != nil {
				t.Fatal(err)
			}
			compareGoldWireRow(t, tc.name, want, row)
		})
	}
}

// TestForgeNeverLogsTheToken — the canary over the goldens themselves: no
// committed golden may carry the sentinel token (in an error sentence, a
// header, a wire body or a mapped struct). Asserting against a fake secret
// is safe; asserting against nothing would assert nothing.
func TestForgeNeverLogsTheToken(t *testing.T) {
	dir := filepath.Join("testdata", goldWireDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("golden dir missing: %v", err)
	}
	found := false
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		found = true
		data, rerr := os.ReadFile(filepath.Join(dir, e.Name()))
		if rerr != nil {
			t.Fatal(rerr)
		}
		if strings.Contains(string(data), sentinelToken) {
			t.Fatalf("%s carries the token — a credential must never reach a golden", e.Name())
		}
		if strings.Contains(string(data), "SENTINEL") {
			t.Fatalf("%s carries the token's fingerprint", e.Name())
		}
	}
	if !found {
		t.Fatal("no wire goldens found — generate them first")
	}
}
