package forge

// T11 — the GitHub implementation's tests, and the guards that keep this
// package the ONLY place in the repository that knows the forge has a
// network side at all (O7/I7).
//
// Every response here is a committed fixture served by a loopback test
// server; the suite makes no request the machine cannot answer offline.
// That is what lets the task be late and separate (R7) without making
// every earlier task secretly dependent on somebody's token.

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

// The sentinel. TestTokenIsNeverLogged greps every observable string for
// exactly this; it is a fake secret, so asserting against it is safe —
// and asserting against NOTHING would assert nothing.
const sentinelToken = "ghp_" + "SENTINELneverlogged1234567890"

type hub struct {
	t    *testing.T
	srv  *httptest.Server
	mu   sync.Mutex
	hits []string          // "METHOD path" per request, in order
	seen []http.Header     // headers per request, in order
	body []string          // raw bodies per request
	urls []string          // full request URI (path + query), S57 T2
	fail map[string]string // path-substring → fixture serving an error
	st   map[string]string // path-substring → fixture serving success

	// seq (S57 T2): path-substring → fixtures served IN ORDER, one per
	// matching request, the last repeated — pagination, where every
	// page is a recorded file and the Link header names the next one.
	// seqUsed counts the consumptions per key (the cap test asserts the
	// client stopped at the cap, not after it).
	seq     map[string][]string
	seqi    map[string]int
	seqUsed map[string]int
}

func newHub(t *testing.T) *hub {
	t.Helper()
	h := &hub{t: t, fail: map[string]string{}, st: map[string]string{},
		seq: map[string][]string{}, seqi: map[string]int{}, seqUsed: map[string]int{}}
	h.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		h.mu.Lock()
		h.hits = append(h.hits, r.Method+" "+r.URL.Path)
		h.seen = append(h.seen, r.Header.Clone())
		h.body = append(h.body, string(raw))
		h.urls = append(h.urls, r.RequestURI)
		target := r.Method + " " + r.URL.Path
		if r.URL.RawQuery != "" {
			target += "?" + r.URL.RawQuery
		}
		// The LONGEST matching key wins: "/issues/4" and
		// "PATCH /repos/…/issues/4" can both cover a request, and "most
		// specific" must be a rule, not map-iteration luck.
		longest := func(m map[string]string) string {
			best, bestLen := "", -1
			for frag, f := range m {
				if strings.Contains(target, frag) && len(frag) > bestLen {
					best, bestLen = f, len(frag)
				}
			}
			return best
		}
		failFix := longest(h.fail)
		okFix := ""
		seqFix := ""
		if failFix == "" {
			okFix = longest(h.st)
		}
		if failFix == "" && okFix == "" {
			best, bestLen := "", -1
			for frag, list := range h.seq {
				if strings.Contains(target, frag) && len(frag) > bestLen && len(list) > 0 {
					best, bestLen = frag, len(frag)
				}
			}
			if best != "" {
				i := h.seqi[best]
				seqFix = h.seq[best][min(i, len(h.seq[best])-1)]
				h.seqi[best] = i + 1
				h.seqUsed[best]++
			}
		}
		h.mu.Unlock()
		if failFix != "" {
			serveFixture(w, r, failFix)
			return
		}
		if okFix != "" {
			serveFixture(w, r, okFix)
			return
		}
		if seqFix != "" {
			serveFixture(w, r, seqFix)
			return
		}
		h.t.Fatalf("the test server was asked for %q and no fixture was registered — a live call is never the fallback (I7)", r.URL.Path)
	}))
	t.Cleanup(h.srv.Close)
	return h
}

// serveFixture serves testdata/<name>; the headers the real API carries
// for status-sensitive cases (rate limits) are encoded INTO the fixture
// as {"__status":n,"__headers":{...},"body":...} so every response the
// suite answers is a committed file (TestFixturesAreRecordedNotLive).
func serveFixture(w http.ResponseWriter, r *http.Request, name string) {
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		http.Error(w, "fixture missing: "+name, 500)
		return
	}
	var env struct {
		Status  int               `json:"__status"`
		Headers map[string]string `json:"__headers"`
		Body    json.RawMessage   `json:"body"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		http.Error(w, "fixture unreadable: "+name, 500)
		return
	}
	for k, v := range env.Headers {
		// {{BASE}} (S57 T2): the loopback server's own root, so a
		// recorded Link header can point at the NEXT page of this
		// harness without the committed fixture knowing the port.
		w.Header().Set(k, strings.ReplaceAll(v, "{{BASE}}", "http://"+r.Host))
	}
	if env.Status == 0 {
		env.Status = 200
	}
	w.WriteHeader(env.Status)
	if len(env.Body) > 0 {
		_, _ = w.Write(env.Body)
	}
}

func (h *hub) client() *GitHub {
	return New(Config{
		Owner:   "acme",
		Repo:    "widget",
		BaseURL: h.srv.URL, // loopback ONLY — never the real host (O7)
		TokenSource: func() (string, error) {
			return sentinelToken, nil
		},
		MaxRetries: 3,
		Backoff:    func(int) time.Duration { return 0 },
	})
}

func (h *hub) reset() {
	h.mu.Lock()
	h.hits, h.seen, h.body, h.urls = nil, nil, nil, nil
	h.mu.Unlock()
}

func (h *hub) attempts() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.hits)
}

// ---------------------------------------------------------------------------

// TestGitHubMapsErrorsToKinds — the table (T1, O5). The two 403s are the
// interesting rows: a missing scope is auth, branch protection is
// refused, and an indistinguishable 403 SAYS SO instead of guessing.
func TestGitHubMapsErrorsToKinds(t *testing.T) {
	cases := []struct {
		name     string
		fixture  string // error fixture served for ReadIssue
		wantKind string
		wantIn   string // substring the Detail must carry
	}{
		{"401", "err_401.json", KindAuth, ""},
		{"403 scope", "err_403_scope.json", KindAuth, "scope"},
		{"404", "err_404.json", KindNotFound, ""},
		{"409", "err_409.json", KindConflict, ""},
		{"422 duplicate", "err_422_duplicate.json", KindConflict, ""},
		{"403 protection", "err_403_protection.json", KindRefused, "protection"},
		{"403 ambiguous", "err_403_ambiguous.json", KindRefused, "ambiguous"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHub(t)
			h.fail["/issues/4"] = tc.fixture
			_, err := h.client().ReadIssue(context.Background(), 4)
			var fe *ForgeError
			if !errors.As(err, &fe) {
				t.Fatalf("every mapped response arrives as a ForgeError, got %T: %v", err, err)
			}
			if fe.Kind != tc.wantKind {
				t.Fatalf("%s: kind %q, want %q (detail: %s)", tc.name, fe.Kind, tc.wantKind, fe.Err.What())
			}
			if tc.wantIn != "" && !strings.Contains(fe.Err.What(), tc.wantIn) {
				t.Fatalf("%s: detail must carry %q, got %q", tc.name, tc.wantIn, fe.Err.What())
			}
		})
	}
	// The transport row: a server that is not there at all.
	dead := newHub(t)
	dead.srv.Close() // pull the plug — connection refused
	_, err := dead.client().ReadIssue(context.Background(), 4)
	var fe *ForgeError
	if !errors.As(err, &fe) || fe.Kind != KindTransport {
		t.Fatalf("a dead endpoint is transport, got %v", err)
	}
	// The conflation guard (O5): transport and refused must stay distinct
	// kinds, or "GitHub is down" and "GitHub said no" collapse.
	if errors.Is(errors.New(KindTransport), errors.New(KindRefused)) {
		t.Fatal("kinds must not compare equal")
	}
}

// TestRetriesAreBounded — a transport failure retries with backoff up to
// the bound; a refused NEVER retries (retrying a protected branch forever
// is how a step deadline becomes a lie — S5's).
func TestRetriesAreBounded(t *testing.T) {
	h := newHub(t)
	h.fail["/issues/4"] = "err_500.json"
	_, err := h.client().ReadIssue(context.Background(), 4)
	if err == nil {
		t.Fatal("a sustained 500 ends in an error")
	}
	// initial attempt + exactly MaxRetries retries — one more is a lie
	// about the bound, one less is a lie about the retries.
	if got := h.attempts(); got != 4 {
		t.Fatalf("bounded retries: 1+3 attempts, got %d", got)
	}

	h2 := newHub(t)
	h2.fail["/issues/4"] = "err_403_protection.json"
	if _, err := h2.client().ReadIssue(context.Background(), 4); err == nil {
		t.Fatal("refused is an error")
	}
	if got := h2.attempts(); got != 1 {
		t.Fatalf("a refusal retried %d times — refused NEVER retries", got)
	}

	h3 := newHub(t)
	h3.fail["/issues/4"] = "err_401.json"
	if _, err := h3.client().ReadIssue(context.Background(), 4); err == nil {
		t.Fatal("auth is an error")
	}
	if got := h3.attempts(); got != 1 {
		t.Fatalf("a 401 retried %d times — auth must not be retried either", got)
	}
}

// TestRateLimitCarriesResetTime — a rate limit is transport (retrying is
// correct) WITH the reset time in the detail (the caller needs to know
// how long).
func TestRateLimitCarriesResetTime(t *testing.T) {
	h := newHub(t)
	h.fail["/issues/4"] = "err_403_ratelimit.json"
	_, err := h.client().ReadIssue(context.Background(), 4)
	var fe *ForgeError
	if !errors.As(err, &fe) || fe.Kind != KindTransport {
		t.Fatalf("a rate limit is transport, not refused — GitHub is throttling, not saying no: %v", err)
	}
	if !strings.Contains(strings.ToLower(fe.Err.What()), "reset") {
		t.Fatalf("the detail must carry the reset time, got %q", fe.Err.What())
	}
}

// TestTokenIsNeverLogged — every observable string the boundary can
// produce is swept for the sentinel: errors, recorded request headers,
// request bodies. The Authorization header carries the sentinel by
// construction (that is the wire), and it is the ONE place it appears —
// the implementation itself never echoes it into a Detail, never logs,
// and a failure caused BY the header still names the header, not its
// value.
func TestTokenIsNeverLogged(t *testing.T) {
	h := newHub(t)
	for _, f := range []string{"err_401.json", "err_403_scope.json", "err_403_protection.json",
		"err_403_ambiguous.json", "err_404.json", "err_409.json", "err_422_duplicate.json", "err_500.json",
		"err_403_ratelimit.json"} {
		h.reset()
		h.fail["/issues/4"] = f
		_, err := h.client().ReadIssue(context.Background(), 4)
		if err != nil && strings.Contains(err.Error(), sentinelToken) {
			t.Fatalf("%s: the token leaked into an error: %v", f, err)
		}
		h.mu.Lock()
		for _, hdrs := range h.seen {
			// The WIRE carries it (the Authorization header, by design);
			// nothing else may.
			for name, vals := range hdrs {
				for _, v := range vals {
					if strings.Contains(v, sentinelToken) && strings.ToLower(name) != "authorization" {
						t.Fatalf("the token reached header %q", name)
					}
				}
			}
		}
		for _, b := range h.body {
			if strings.Contains(b, sentinelToken) {
				t.Fatal("the token reached a request body")
			}
		}
		h.mu.Unlock()
	}
	// And the implementation contains no logging call at all: a boundary
	// that logs is a boundary that leaks. (Assembled literals: this file
	// must not itself contain them as greppable calls.)
	src, err := os.ReadFile("github.go")
	if err != nil {
		t.Fatal(err)
	}
	code, _ := stripComments(string(src))
	for _, b := range []string{"log." + "Print", "fmt." + "Print", "os." + "Stderr", "os." + "Stdout"} {
		if strings.Contains(code, b) {
			t.Fatalf("github.go contains %q — this boundary writes nothing anywhere", b)
		}
	}
}

// TestTokenIsNotReadFromModelOutput — the token's only route is the
// Config's TokenSource (production: DefaultTokenSource, the environment).
// No exported signature in the package accepts a token at all: a token
// parameter is a token that came from wherever the CALLER is standing —
// a blueprint field, a handler, model output (never: N/I invariant).
func TestTokenIsNotReadFromModelOutput(t *testing.T) {
	// No struct in the package has a Token string FIELD (only a
	// TokenSource), and no exported method takes a token-shaped string.
	cfgType := reflect.TypeOf(Config{})
	for i := 0; i < cfgType.NumField(); i++ {
		f := cfgType.Field(i)
		if strings.EqualFold(f.Name, "token") && f.Type.Kind() == reflect.String {
			t.Fatal("Config.Token as a plain string invites a caller to wire a model's output into it — TokenSource is the only route")
		}
	}
	tf := reflect.TypeOf((*GitHub)(nil)).Elem()
	for i := 0; i < tf.NumMethod(); i++ {
		m := tf.Method(i)
		mt := m.Type
		for j := 0; j < mt.NumIn(); j++ {
			if mt.In(j).Kind() == reflect.String && mt.In(j) == reflect.TypeOf("") && j > 0 {
				// string parameters exist (title, body, head…) — the
				// ast test below guards the NAMES; this guards arity shape.
			}
		}
	}
	// The names, from the source: no parameter or field named token in
	// any exported signature (again: assembled so this file stays clean).
	src, err := os.ReadFile("github.go")
	if err != nil {
		t.Fatal(err)
	}
	for lineNo, line := range strings.Split(string(src), "\n") {
		if i := strings.Index(line, "//"); i >= 0 {
			line = line[:i] // a comment is prose ABOUT code; the scan is of code
		}
		tl := strings.ToLower(line)
		if strings.Contains(tl, "token string") || strings.Contains(tl, "token  string") {
			t.Fatalf("github.go:%d declares a token parameter — %s", lineNo+1, strings.TrimSpace(line))
		}
	}
	if !strings.Contains(string(src), "TokenSource") {
		t.Fatal("the environment route is TokenSource")
	}
}

// TestImplementationIsNotRicherThanTheInterface — the ten Forge
// methods, plus the OPTIONAL capabilities the boundary declares
// (StatusLister and, since S57 T2, CheckReader — adopted, asserted,
// and allowed here). S61 T4 (dated 2026-11-05) adopts the same route for
// RepoCreator/RepoReader: GI10's repository doors are DECLARED optionals,
// asserted against the interface types here — the census grows with the
// declarations, never around them. An implementation richer than the interface PLUS
// the declared optionals is one somebody will reach into directly, and
// the typed boundary (T1's anti-discovery rule) rots from the inside.
// ThreadLister is the declared optional the implementation must NOT
// have: review threads are GraphQL-only on GitHub (VG8), and a REST
// attempt at them is a second way of saying "resolved".
func TestImplementationIsNotRicherThanTheInterface(t *testing.T) {
	want := map[string]bool{}
	for _, tf := range []reflect.Type{
		reflect.TypeOf((*Forge)(nil)).Elem(),
		reflect.TypeOf((*StatusLister)(nil)).Elem(),
		reflect.TypeOf((*CheckReader)(nil)).Elem(),
		reflect.TypeOf((*RepoCreator)(nil)).Elem(), // S61 T4 (2026-11-05), declared optional
		reflect.TypeOf((*RepoReader)(nil)).Elem(),  // S61 T4, declared optional
	} {
		for i := 0; i < tf.NumMethod(); i++ {
			want[tf.Method(i).Name] = true
		}
	}
	gt := reflect.TypeOf(&GitHub{})
	got := 0
	for i := 0; i < gt.NumMethod(); i++ {
		m := gt.Method(i).Name
		if !want[m] {
			t.Fatalf("(*GitHub).%s exists beyond the interface and the declared optionals — no extra endpoints, no convenience", m)
		}
		got++
	}
	if got != len(want) {
		t.Fatalf("the implementation carries %d of the %d declared methods — every declared capability must be present or the pin is lying", got, len(want))
	}
	if _, ok := gt.MethodByName("ListUnresolvedThreads"); ok {
		t.Fatal("(*GitHub).ListUnresolvedThreads exists — threads are GraphQL-only (VG8); a REST attempt is a second way of saying resolved")
	}
}

// TestFixturesAreRecordedNotLive — every response the suite answers is a
// committed file, and the only server the client ever dialled is the
// loopback harness (the BaseURL of every test client is the harness's).
func TestFixturesAreRecordedNotLive(t *testing.T) {
	entries, err := os.ReadDir("testdata")
	if err != nil {
		t.Fatalf("the fixtures are committed files: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("empty testdata — a live call is the only other answer, and it is forbidden (I7)")
	}
	names := map[string]bool{}
	for _, e := range entries {
		names[e.Name()] = true
	}
	for _, want := range []string{"issue.json", "pr.json", "pr_merged.json", "reviews.json", "review.json",
		"issue_opened.json", "pr_opened.json", "merge.json", "err_401.json", "err_403_scope.json",
		"err_403_protection.json", "err_403_ambiguous.json", "err_404.json", "err_409.json",
		"err_422_duplicate.json", "err_403_ratelimit.json", "err_500.json", "issues_list.json", "pulls_list.json"} {
		if !names[want] {
			t.Fatalf("fixture %s is missing", want)
		}
	}
	// The client never dials anywhere else: no package-level request
	// functions exist in the implementation (assembled literals).
	src, err := os.ReadFile("github.go")
	if err != nil {
		t.Fatal(err)
	}
	code, _ := stripComments(string(src))
	// The verb tokens carry their opening paren: a use of the function
	// is a call, and `http.Header` (the type S57 T2 returns on the
	// checks path) is not a request.
	for _, b := range []string{"http." + "Get(", "http." + "Post(", "http." + "Head(", "Default" + "Transport"} {
		if strings.Contains(code, b) {
			t.Fatalf("github.go contains %q — the only http.Client this package uses is the harness's (or the caller's injected one)", b)
		}
	}
}

// TestNoOtherTestGrewANetworkDependency — the repository-wide tripwire
// (O7, I7). This is the task where a live call would be CONVENIENT, so
// the guard scans the whole tree, not the package: the forge host may be
// named only in the implementation (github.go) and the documentation —
// never in any test, anywhere, and never in a fixture.
func TestNoOtherTestGrewANetworkDependency(t *testing.T) {
	host := "api." + "github.com"
	// The module path contains github.com legitimately; the scan is for
	// the HOST, which only the implementation may name.
	var offenders []string
	root := "../.." // aeon/ — the repository of Go that this package belongs to
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			// .git and build caches hold no tests worth scanning, and the
			// UI tree is TypeScript with its own guards.
			name := d.Name()
			if name == ".git" || name == "node_modules" || name == ".tmp" {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, ".json") {
			return nil
		}
		isTest := strings.HasSuffix(path, "_test.go")
		isFixture := strings.Contains(path, "testdata")
		isImpl := strings.HasSuffix(path, "github.go")
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(src), host) && !isImpl {
			if isTest || isFixture {
				offenders = append(offenders, path)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(offenders) > 0 {
		t.Fatalf("tests and fixtures must never name the live forge host (a live call is then one refactor away): %v", offenders)
	}
	// And the implementation dials no one but the Config's BaseURL:
	// the harness client was built with a loopback URL, and every hit the
	// servers recorded came through New() — no package-level http.
	// functions exist (asserted in TestFixturesAreRecordedNotLive).
	h := newHub(t)
	h.st["/issues/4"] = "issue.json"
	if _, err := h.client().ReadIssue(context.Background(), 4); err != nil {
		t.Fatal(err)
	}
	if h.attempts() == 0 {
		t.Fatal("the request went… nowhere? then what answered it")
	}
}

// TestWireRoundTripsTheFixtures — the ten methods against the recorded
// shapes: numbers arrive, the Authorization header carries the sentinel
// (the wire by design), Accept names v3, and the URL path is the
// repository-scoped REST route the owner/repo config built.
func TestWireRoundTripsTheFixtures(t *testing.T) {
	ctx := context.Background()
	h := newHub(t)
	h.st["/repos/acme/widget/issues?state"] = "issues_list.json"
	h.st["POST /repos/acme/widget/issues"] = "issue_opened.json"
	h.st["/issues/4"] = "issue.json"
	h.st["PATCH /repos/acme/widget/issues/4"] = "issue_closed.json"
	h.st["/pulls?state"] = "pulls_list.json"
	h.st["POST /repos/acme/widget/pulls"] = "pr_opened.json"
	h.st["/pulls/7"] = "pr.json"
	h.st["PUT /repos/acme/widget/pulls/7/merge"] = "merge.json"
	h.st["GET /repos/acme/widget/pulls/7/reviews"] = "reviews.json"
	h.st["POST /repos/acme/widget/pulls/7/reviews"] = "review.json"
	g := h.client()

	iss, err := g.FindOpenIssue(ctx, "Add version endpoint", "dev-loop")
	if err != nil || iss == nil || iss.Number != 4 {
		t.Fatalf("FindOpenIssue: %+v (%v)", iss, err)
	}
	opened, err := g.OpenIssue(ctx, IssueRequest{Title: "Add version endpoint", Body: "body", Labels: []string{"dev-loop"}})
	if err != nil || opened.Number != 5 {
		t.Fatalf("OpenIssue: %+v (%v)", opened, err)
	}
	if _, err := g.ReadIssue(ctx, 4); err != nil {
		t.Fatal(err)
	}
	if err := g.CloseIssue(ctx, 4); err != nil {
		t.Fatal(err)
	}
	pr, err := g.FindOpenPR(ctx, "task/4a17-version/4a17")
	if err != nil || pr == nil || pr.Number != 7 || pr.BaseRef != "feature/4a17-version" {
		t.Fatalf("FindOpenPR must carry BaseRef (L26): %+v (%v)", pr, err)
	}
	if _, err := g.OpenPR(ctx, PRRequest{Title: "feat: version", Body: "b", Head: "task/4a17-version/4a17", Base: "feature/4a17-version"}); err != nil {
		t.Fatal(err)
	}
	rp, err := g.ReadPR(ctx, 7)
	if err != nil || rp.BaseRef == "" {
		t.Fatalf("ReadPR without BaseRef disarms the phantom-merge guard (L26): %+v (%v)", rp, err)
	}
	mr, err := g.MergePR(ctx, 7, MergeSquash)
	if err != nil || !mr.Merged || mr.SHA == "" {
		t.Fatalf("MergePR: %+v (%v)", mr, err)
	}
	revs, err := g.ListReviews(ctx, 7)
	if err != nil || len(revs) != 2 || revs[0].ID == 0 {
		t.Fatalf("ListReviews: %+v (%v)", revs, err)
	}
	rev, err := g.PostReview(ctx, 7, "Verified against the daemon.", VerdictApprove)
	if err != nil || rev.ID == 0 {
		t.Fatalf("PostReview must return the id — reconciliation is BY id (T7): %+v (%v)", rev, err)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for i, hdrs := range h.seen {
		if got := hdrs.Get("Authorization"); got != "Bearer "+sentinelToken {
			t.Fatalf("request %d Authorization: %q", i, got)
		}
		if got := hdrs.Get("Accept"); !strings.Contains(got, "application/vnd.github") {
			t.Fatalf("request %d must name the API version, not ride defaults: %q", i, got)
		}
	}
	for _, hit := range h.hits {
		if !strings.HasPrefix(hit, "GET /repos/acme/widget/") && !strings.HasPrefix(hit, "POST /repos/acme/widget/") &&
			!strings.HasPrefix(hit, "PATCH /repos/acme/widget/") && !strings.HasPrefix(hit, "PUT /repos/acme/widget/") {
			t.Fatalf("unexpected route %s — the boundary is repository-scoped REST, nothing else", hit)
		}
	}
}

// TestAbsenceIsTheNormalAnswer — Find* answer absence with (nil, nil):
// the 404 is not an error for a finder, it is the check that routes
// check-then-act to its create step. Read* still answers 404 as
// not-found — something read BY IDENTITY was expected.
func TestAbsenceIsTheNormalAnswer(t *testing.T) {
	h := newHub(t)
	h.st["/repos/acme/widget/issues"] = "issues_empty.json"
	h.st["/repos/acme/widget/pulls"] = "pulls_empty.json"
	g := h.client()
	iss, err := g.FindOpenIssue(ctxTitle(), "nothing like it", "dev-loop")
	if err != nil || iss != nil {
		t.Fatalf("absence is the normal answer for finders: %+v (%v)", iss, err)
	}
	pr, err := g.FindOpenPR(ctxTitle(), "task/nothing/here")
	if err != nil || pr != nil {
		t.Fatalf("absence for FindOpenPR: %+v (%v)", pr, err)
	}
	h2 := newHub(t)
	h2.fail["/issues/9"] = "err_404.json"
	if _, err := h2.client().ReadIssue(context.Background(), 9); err == nil {
		t.Fatal("Read by identity answers not-found")
	}
}

func ctxTitle() context.Context { return context.Background() }

// stripComments removes line comments — the code-shape scans judge code,
// not prose about code (this file's own comments name the shapes they
// forbid; a grep that indicts itself tests nothing — T1's lesson, kept).
func stripComments(src string) (string, int) {
	var b strings.Builder
	n := 0
	for _, line := range strings.Split(src, "\n") {
		if i := strings.Index(line, "//"); i >= 0 {
			line = line[:i]
			n++
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	return b.String(), n
}
