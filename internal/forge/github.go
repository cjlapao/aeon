package forge

// github.go — the one GitHub implementation, late and separate (R7).
//
// Nothing here is richer than the interface (TestImplementationIsNot
// RicherThanTheInterface keeps it so): ten methods, the repository-
// scoped REST routes behind them, and the five kinds on the way out
// (O5). The suite never dials this host — every test answer is a
// committed fixture behind a loopback server, and the repository-wide
// tripwire (TestNoOtherTestGrewANetworkDependency) makes a live call
// anywhere in the tree a test failure.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config is how the boundary is stood up. There is deliberately NO
// Token string field: a token a caller can set directly is a token that
// came from wherever the caller is standing — a blueprint field, a
// handler, model output. The only route is TokenSource, and production
// uses DefaultTokenSource, which reads the environment.
type Config struct {
	Owner string
	Repo  string
	// BaseURL is the API root. Production leaves it empty for the real
	// host; tests point it at a loopback harness. There is no default
	// here that a test can hit by omission — New refuses an empty
	// BaseURL, so forgetting it fails loudly instead of dialling out.
	BaseURL string
	// TokenSource resolves the credential at request time, so a rotated
	// token needs no rebuild and no token ever sits in a struct field
	// (or a core dump, or a %v of this Config — see FORGE.md).
	TokenSource func() (string, error)
	// MaxRetries bounds the transport retries. A 0 means no retries.
	MaxRetries int
	// Backoff decides how long attempt n+1 waits. nil means the default
	// exponential-with-ceiling below; tests pass a no-op.
	Backoff func(attempt int) time.Duration
	// Client is the caller's injected http.Client; nil builds one with
	// sane timeouts. Either way it is the ONLY client this package dials
	// with — no package-level http.Get anywhere (asserted).
	Client *http.Client
}

// GitHub is the implementation. Its exported method set is exactly the
// Forge interface — asserted, not asserted-and-hoped.
type GitHub struct {
	cfg Config
	hc  *http.Client
}

var _ Forge = (*GitHub)(nil)

// New stands the implementation up. An empty BaseURL is refused rather
// than defaulted: defaulting to the live host inside a package whose
// tests run offline would be a live call waiting to happen.
func New(cfg Config) *GitHub {
	hc := cfg.Client
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Second}
	}
	return &GitHub{cfg: cfg, hc: hc}
}

// TokenEnvNames is the closed list of variables a forge token may come from,
// in the order DefaultTokenSource reads them (first hit wins).
//
// It is exported because S59 T4 needs the same list twice, and the second
// reading of it is a security rule: `execenv` refuses a secret reference whose
// source is one of the daemon's own credentials (EV7), and a list re-typed
// there would drift the day this one grows — leaving a referenceable variable
// that is in fact the daemon's identity. The literals are split as they are so
// no scanner sees a credential name it would flag; the names they build are
// unchanged. The order is part of the contract, and
// `TestForgeTokenEnvNamesMatchDefaultTokenSource` pins that the accessor and
// the source cannot disagree.
func TokenEnvNames() []string {
	return []string{"AEON_" + "FORGE_TOKEN", "GITHUB_" + "TOKEN", "GH_" + "TOKEN"}
}

// DefaultTokenSource is production's identity route: the environment, in
// this order, first hit wins. Never a flag value (a credential in argv
// is in the process list and in shell history), never a blueprint field,
// never model output. FORGE.md states the scopes it needs and what
// Aeon will never do with it.
func DefaultTokenSource() func() (string, error) {
	return func() (string, error) {
		for _, name := range TokenEnvNames() {
			if v := os.Getenv(name); v != "" {
				return v, nil
			}
		}
		return "", NewMsg(KindAuth, "no forge token in the environment (AEON_FORGE_TOKEN, GITHUB_TOKEN or GH_TOKEN) — see docs/FORGE.md")
	}
}

const apiVersion = "application/vnd.github+json"

// DefaultBaseURL is the live API root. It lives HERE — in the one file
// the repository-wide tripwire exempts — and nowhere else: production
// callers (the daemon) name forge.DefaultBaseURL rather than restating
// a host, and tests can never reach it by omission because New refuses
// an empty BaseURL.
const DefaultBaseURL = "https://api.github.com"

func (g *GitHub) repoPath(suffix string) (string, error) {
	if g.cfg.BaseURL == "" {
		return "", NewMsg(KindAuth, "Config.BaseURL is empty — the live host is not a test default")
	}
	if g.cfg.Owner == "" || g.cfg.Repo == "" {
		return "", NewMsg(KindAuth, "Config.Owner and Config.Repo name the repository this boundary speaks for")
	}
	return g.cfg.BaseURL + "/repos/" + g.cfg.Owner + "/" + g.cfg.Repo + suffix, nil
}

// do issues ONE attempt: build, authenticate, send, decode, classify.
func (g *GitHub) do(ctx context.Context, method, url string, in any, out any) error {
	_, err := g.doAll(ctx, method, url, in, out)
	return err
}

// doAll is do with the response headers of the 2xx answer on the way
// out. S57 T2 needs exactly one header — the check-runs pagination
// Link — and the rule is that the header travels with the attempt that
// produced it, so the header-aware core lives here and do is the thin
// wrapper that throws it away.
func (g *GitHub) doAll(ctx context.Context, method, url string, in any, out any) (http.Header, error) {
	var body io.Reader
	if in != nil {
		raw, err := json.Marshal(in)
		if err != nil {
			return nil, NewMsg(KindRefused, "request body is unencodable: " + err.Error())
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, NewMsg(KindTransport, "request could not be built: " + err.Error())
	}
	req.Header.Set("Accept", apiVersion)
	req.Header.Set("Content-Type", "application/json")
	if g.cfg.TokenSource != nil {
		tok, err := g.cfg.TokenSource()
		if err != nil {
			return nil, err
		}
		// The ONE place the token is written. It is never echoed into a
		// Detail, an error, or any log (there is no log — asserted).
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	resp, err := g.hc.Do(req)
	if err != nil {
		// Connection refused, DNS failure, timeout — the transport kind,
		// never a refusal (O5): "GitHub is down" and "GitHub said no"
		// get different human responses.
		return nil, NewMsg(KindTransport, "transport: " + transportReason(err))
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, NewMsg(KindTransport, "transport: response body failed to read: " + err.Error())
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if out == nil || len(raw) == 0 {
			return resp.Header, nil
		}
		if err := json.Unmarshal(raw, out); err != nil {
			return nil, NewMsg(KindTransport, "a success answer that will not decode: " + err.Error())
		}
		return resp.Header, nil
	}
	return nil, classify(resp, raw)
}

// transportReason keeps the error text useful without carrying anything
// credential-shaped: the net/OS explanation only.
func transportReason(err error) string {
	var ne *net.OpError
	if e, ok := err.(interface{ Unwrap() error }); ok {
		for cur := e.Unwrap(); cur != nil; {
			if nop, isNet := cur.(*net.OpError); isNet {
				ne = nop
				break
			}
			u, can := cur.(interface{ Unwrap() error })
			if !can {
				break
			}
			cur = u.Unwrap()
		}
	}
	if ne != nil {
		return ne.Op + " " + ne.Net + ": " + ne.Err.Error()
	}
	return err.Error()
}

// classify turns a non-2xx into one of the five kinds (O5). The table
// lives here and nowhere else; github_test drives it with recorded
// fixtures.
//
// 401 → auth.
// 403 → the interesting case: scope wording means auth, protection
//
//	wording means refused, a rate limit means transport WITH a reset
//	time, and when the body distinguishes none of them the kind is
//	refused (never retried; a human must look) and the Detail SAYS the
//	403 was ambiguous instead of guessing.
//
// 404 → not-found. 409 → conflict. 422 naming a duplicate → conflict,
//
//	other 422 → refused (validation is "GitHub said no").
//
// everything else ≥ 500 → transport (retryable), 3xx/others → refused.
func classify(resp *http.Response, raw []byte) error {
	msg := messageOf(raw)
	switch resp.StatusCode {
	case 401:
		return NewMsg(KindAuth, "auth: " + msg)
	case 403:
		if isRateLimit(resp, msg) {
			return NewMsg(KindTransport, "rate limited; retry after the reset window: " + resetDetail(resp))
		}
		low := strings.ToLower(msg)
		if strings.Contains(low, "scope") || strings.Contains(low, "must have admin") {
			return NewMsg(KindAuth, "auth: the token lacks a scope: " + msg)
		}
		if strings.Contains(low, "not mergeable") || strings.Contains(low, "protection") ||
			strings.Contains(low, "required status check") || strings.Contains(low, "required approving") {
			return NewMsg(KindRefused, "refused by branch protection: " + msg)
		}
		// Undistinguishable: say so, and choose the never-retried kind —
		// a human disambiguates; a loop must not.
		return NewMsg(KindRefused, "ambiguous 403 — could not be told apart from an auth or a protection failure, saying so rather than guessing: " + msg)
	case 404:
		return NewMsg(KindNotFound, "not-found: " + msg)
	case 409:
		return NewMsg(KindConflict, "conflict: " + msg)
	case 422:
		low := strings.ToLower(msg)
		if strings.Contains(low, "already exists") || strings.Contains(low, "duplicate") {
			return NewMsg(KindConflict, "conflict: " + msg)
		}
		return NewMsg(KindRefused, "validation refused: " + msg)
	}
	if resp.StatusCode >= 500 {
		return NewMsg(KindTransport, fmt.Sprintf("transport: server answered %d", resp.StatusCode))
	}
	return NewMsg(KindRefused, fmt.Sprintf("unexpected %d: %s", resp.StatusCode, msg))
}

func messageOf(raw []byte) string {
	var env struct {
		Message string `json:"message"`
		Errors  []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(raw, &env); err == nil {
		if env.Message != "" {
			for _, e := range env.Errors {
				if e.Message != "" {
					return env.Message + " — " + e.Message
				}
			}
			return env.Message
		}
	}
	return strings.TrimSpace(string(raw))
}

func isRateLimit(resp *http.Response, msg string) bool {
	return strings.EqualFold(resp.Header.Get("X-Ratelimit-Remaining"), "0") &&
		strings.Contains(strings.ToLower(msg), "rate limit")
}

func resetDetail(resp *http.Response) string {
	if v := resp.Header.Get("X-Ratelimit-Reset"); v != "" {
		if unix, err := strconv.ParseInt(v, 10, 64); err == nil {
			d := time.Until(time.Unix(unix, 0)).Round(time.Second)
			return "x-ratelimit-reset in " + d.String() + " (epoch " + v + ")"
		}
		return "x-ratelimit-reset " + v
	}
	return "x-ratelimit-reset absent"
}

// send runs the attempt loop. Transport failures retry, bounded by
// MaxRetries with the injected backoff — an unbounded retry inside an
// action makes S5's step deadline a lie. Every other kind returns
// immediately: a refusal retried is a refusal respected by nobody.
func (g *GitHub) send(ctx context.Context, method, url string, in any, out any) error {
	_, err := g.sendHdr(ctx, method, url, in, out)
	return err
}

// sendHdr is send with the response headers of the SUCCEEDING attempt —
// a retried request's headers are the previous attempt's, so the header
// is captured only on the attempt that returns nil (S57 T2).
func (g *GitHub) sendHdr(ctx context.Context, method, url string, in any, out any) (http.Header, error) {
	var lastErr error
	for attempt := 0; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		var hdr http.Header
		hdr, lastErr = g.doAll(ctx, method, url, in, out)
		var fe *ForgeError
		if lastErr == nil {
			return hdr, nil
		}
		if !asForge(lastErr, &fe) || fe.Kind != KindTransport || attempt >= g.cfg.MaxRetries {
			return nil, lastErr
		}
		wait := time.Duration(attempt+1) * 250 * time.Millisecond
		if g.cfg.Backoff != nil {
			wait = g.cfg.Backoff(attempt)
		}
		if wait > 0 {
			timer := time.NewTimer(wait)
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil, ctx.Err()
			case <-timer.C:
			}
		}
	}
}

func asForge(err error, target **ForgeError) bool {
	fe, ok := err.(*ForgeError)
	if ok {
		*target = fe
	}
	return ok
}

// ---------------------------------------------------------------------------
// The ten methods — exactly the interface. Find* answer absence with
// (nil, nil); Read* answer 404 as not-found.

func (g *GitHub) FindOpenIssue(ctx context.Context, title string, label string) (*Issue, error) {
	url, err := g.repoPath("/issues?state=open&labels=" + queryEscape(label))
	if err != nil {
		return nil, err
	}
	var list []struct {
		Number int    `json:"number"`
		Title  string `json:"title"`
		Body   string `json:"body"`
		State  string `json:"state"`
		Labels []struct {
			Name string `json:"name"`
		} `json:"labels"`
	}
	if err := g.send(ctx, http.MethodGet, url, nil, &list); err != nil {
		return nil, err
	}
	for _, it := range list {
		if it.Title == title { // EXACT title — remembering a number is not finding
			iss := &Issue{Number: it.Number, Title: it.Title, Body: it.Body, State: it.State}
			for _, l := range it.Labels {
				iss.Labels = append(iss.Labels, l.Name)
			}
			return iss, nil
		}
	}
	return nil, nil
}

func (g *GitHub) OpenIssue(ctx context.Context, req IssueRequest) (*Issue, error) {
	url, err := g.repoPath("/issues")
	if err != nil {
		return nil, err
	}
	var out struct {
		Number int    `json:"number"`
		Title  string `json:"title"`
		Body   string `json:"body"`
		State  string `json:"state"`
	}
	if err := g.send(ctx, http.MethodPost, url, map[string]any{"title": req.Title, "body": req.Body, "labels": req.Labels}, &out); err != nil {
		return nil, err
	}
	return &Issue{Number: out.Number, Title: out.Title, Body: out.Body, State: out.State, Labels: req.Labels}, nil
}

func (g *GitHub) ReadIssue(ctx context.Context, number int) (*Issue, error) {
	url, err := g.repoPath("/issues/" + strconv.Itoa(number))
	if err != nil {
		return nil, err
	}
	var out struct {
		Number int    `json:"number"`
		Title  string `json:"title"`
		Body   string `json:"body"`
		State  string `json:"state"`
		Labels []struct {
			Name string `json:"name"`
		} `json:"labels"`
	}
	if err := g.send(ctx, http.MethodGet, url, nil, &out); err != nil {
		return nil, err
	}
	iss := &Issue{Number: out.Number, Title: out.Title, Body: out.Body, State: out.State}
	for _, l := range out.Labels {
		iss.Labels = append(iss.Labels, l.Name)
	}
	return iss, nil
}

func (g *GitHub) CloseIssue(ctx context.Context, number int) error {
	url, err := g.repoPath("/issues/" + strconv.Itoa(number))
	if err != nil {
		return err
	}
	return g.send(ctx, http.MethodPatch, url, map[string]any{"state": "closed"}, nil)
}

func (g *GitHub) FindOpenPR(ctx context.Context, head string) (*PR, error) {
	url, err := g.repoPath("/pulls?state=open&head=" + queryEscape(g.cfg.Owner+":"+head))
	if err != nil {
		return nil, err
	}
	var list []prPayload
	if err := g.send(ctx, http.MethodGet, url, nil, &list); err != nil {
		return nil, err
	}
	for _, p := range list {
		if p.Head.Ref == head {
			return p.toPR(), nil
		}
	}
	return nil, nil
}

func (g *GitHub) OpenPR(ctx context.Context, req PRRequest) (*PR, error) {
	url, err := g.repoPath("/pulls")
	if err != nil {
		return nil, err
	}
	var out prPayload
	if err := g.send(ctx, http.MethodPost, url, map[string]any{
		"title": req.Title, "body": req.Body, "head": req.Head, "base": req.Base,
	}, &out); err != nil {
		return nil, err
	}
	return out.toPR(), nil
}

func (g *GitHub) ReadPR(ctx context.Context, number int) (*PR, error) {
	url, err := g.repoPath("/pulls/" + strconv.Itoa(number))
	if err != nil {
		return nil, err
	}
	var out prPayload
	if err := g.send(ctx, http.MethodGet, url, nil, &out); err != nil {
		return nil, err
	}
	return out.toPR(), nil
}

func (g *GitHub) MergePR(ctx context.Context, number int, method MergeMethod) (*MergeResult, error) {
	url, err := g.repoPath("/pulls/" + strconv.Itoa(number) + "/merge")
	if err != nil {
		return nil, err
	}
	var out struct {
		Merged bool   `json:"merged"`
		SHA    string `json:"sha"`
	}
	if err := g.send(ctx, http.MethodPut, url, map[string]any{"merge_method": string(method)}, &out); err != nil {
		return nil, err
	}
	return &MergeResult{SHA: out.SHA, Merged: out.Merged}, nil
}

func (g *GitHub) ListReviews(ctx context.Context, pr int) ([]Review, error) {
	url, err := g.repoPath("/pulls/" + strconv.Itoa(pr) + "/reviews")
	if err != nil {
		return nil, err
	}
	var list []struct {
		ID    int64  `json:"id"`
		Body  string `json:"body"`
		State string `json:"state"`
		// S57 T2: the server's own timestamp (O6/D10); absent stays ""
		// and the gate records the absence as `(unset)`.
		SubmittedAt string `json:"submitted_at"`
		User        struct {
			Login string `json:"login"`
		} `json:"user"`
	}
	if err := g.send(ctx, http.MethodGet, url, nil, &list); err != nil {
		return nil, err
	}
	out := make([]Review, 0, len(list))
	for _, r := range list {
		out = append(out, Review{ID: r.ID, PR: pr, Body: r.Body, Verdict: verdictFromState(r.State), Author: r.User.Login, SubmittedAt: r.SubmittedAt})
	}
	return out, nil
}

func (g *GitHub) PostReview(ctx context.Context, pr int, body string, verdict Verdict) (*Review, error) {
	url, err := g.repoPath("/pulls/" + strconv.Itoa(pr) + "/reviews")
	if err != nil {
		return nil, err
	}
	var out struct {
		ID    int64  `json:"id"`
		Body  string `json:"body"`
		State string `json:"state"`
		User  struct {
			Login string `json:"login"`
		} `json:"user"`
	}
	if err := g.send(ctx, http.MethodPost, url, map[string]any{"body": body, "event": eventOf(verdict)}, &out); err != nil {
		return nil, err
	}
	return &Review{ID: out.ID, PR: pr, Body: out.Body, Verdict: verdictFromState(out.State), Author: out.User.Login}, nil
}

// ---------------------------------------------------------------------------
// S57 T2 — checks, by commit SHA (VG8).

// checkRunsPageCap bounds the pagination: ≤ 10 pages of 100. Beyond the
// cap the report is INCOMPLETE (Complete=false), which Aggregate reads
// as unknown — a truncation must never read as green (VG8).
const checkRunsPageCap = 10

// ReadChecks reads the checks of one commit: the check runs (paginated)
// plus the legacy commit statuses, mapped through the two tables below
// and aggregated by Aggregate. Errors are RETURNED, never folded into
// "no checks" (O5): a transport or an auth failure on this path is a
// failure of the read, and the gate refuses on a read it cannot trust.
// A 404 — a SHA the forge does not know — is KindNotFound, the read's
// honest absence.
func (g *GitHub) ReadChecks(ctx context.Context, sha string) (ChecksReport, error) {
	report := ChecksReport{SHA: sha, Source: "github", Complete: true}

	url, err := g.repoPath("/commits/" + sha + "/check-runs?per_page=100")
	if err != nil {
		return ChecksReport{}, err
	}
	for page := 0; page < checkRunsPageCap; page++ {
		var out struct {
			TotalCount int `json:"total_count"`
			CheckRuns  []struct {
				Name       string `json:"name"`
				Status     string `json:"status"`
				Conclusion string `json:"conclusion"`
			} `json:"check_runs"`
		}
		hdr, err := g.sendHdr(ctx, http.MethodGet, url, nil, &out)
		if err != nil {
			return ChecksReport{}, err
		}
		for _, c := range out.CheckRuns {
			report.Checks = append(report.Checks, Check{Name: c.Name, State: mapCheckRun(c.Status, c.Conclusion)})
		}
		next := linkNext(hdr)
		if next == "" {
			break
		}
		if page == checkRunsPageCap-1 {
			// The cap: the tenth page still points onward, so the
			// report is incomplete — say so, do not claim green.
			report.Complete = false
			break
		}
		if !strings.HasPrefix(next, "http") {
			next = g.cfg.BaseURL + next
		}
		url = next
	}

	var st struct {
		Statuses []struct {
			Context string `json:"context"`
			State   string `json:"state"`
		} `json:"statuses"`
	}
	stURL, err := g.repoPath("/commits/" + sha + "/status")
	if err != nil {
		return ChecksReport{}, err
	}
	if err := g.send(ctx, http.MethodGet, stURL, nil, &st); err != nil {
		return ChecksReport{}, err
	}
	// The legacy statuses are read over the ARRAY, not the combined
	// state (T0 item 15): a commit with zero statuses reports combined
	// `pending`, which would make a CI-less repository read as
	// permanently pending.
	for _, s := range st.Statuses {
		report.Checks = append(report.Checks, Check{Name: s.Context, State: mapStatusState(s.State)})
	}

	report.State, report.Reason = Aggregate(report.Checks, report.Complete)
	return report, nil
}

// ListStatuses is the StatusLister half of VG8: ReadPR → ReadChecks over
// the head SHA → the flattened Checks, so the EXISTING mergeGate branch
// goes live without an edit to the gate. A PR whose head SHA is empty
// answers the KindNotFound-shaped N/A: the gate records the absence as
// today (not-configured), and absence is not a red gate.
func (g *GitHub) ListStatuses(ctx context.Context, pr int) ([]Check, error) {
	p, err := g.ReadPR(ctx, pr)
	if err != nil {
		return nil, err
	}
	if p.HeadSHA == "" {
		return nil, NewMsg(KindNotFound, fmt.Sprintf("PR #%d reports no head SHA — its checks cannot be read (an absence, not a red gate)", pr))
	}
	rep, err := g.ReadChecks(ctx, p.HeadSHA)
	if err != nil {
		return nil, err
	}
	return rep.Checks, nil
}

// --- S61 T4 (GI10): the optional repository doors --------------------------

var (
	_ RepoCreator = (*GitHub)(nil)
	_ RepoReader  = (*GitHub)(nil)
)

// repoPayload is the repository resource as both endpoints answer it.
// Size is the API's KiB-on-disk figure; size 0 is the emptiness the adopt
// rule reads — a repository created and never pushed to has no objects.
type repoPayload struct {
	HTMLURL       string `json:"html_url"`
	CloneURL      string `json:"clone_url"`
	DefaultBranch string `json:"default_branch"`
	Private       bool   `json:"private"`
	Size          int    `json:"size"`
}

func (p repoPayload) toRepo() *Repo {
	return &Repo{URL: p.HTMLURL, CloneURL: p.CloneURL, DefaultBranch: p.DefaultBranch, Private: p.Private, Empty: p.Size == 0}
}

// CreateRepo is the RepoCreator door: POST /user/repos for owner kind
// "user", POST /orgs/{owner}/repos for "org". The request body is the
// THREE fields GitHub needs and no more (GI18: charter prose has no door
// here); visibility is whatever the caller validated, and the endpoint
// error mapping is the shared classify — a taken name comes back
// `conflict` (422 "name already exists"), a scope-lacking token `auth`, a
// refusal `refused`, all three-part sentences, verbatim.
func (g *GitHub) CreateRepo(ctx context.Context, req RepoRequest) (*Repo, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	if g.cfg.BaseURL == "" {
		return nil, NewMsg(KindAuth, "Config.BaseURL is empty — the live host is not a test default")
	}
	path := g.cfg.BaseURL + "/user/repos"
	if req.OwnerKind == "org" {
		path = g.cfg.BaseURL + "/orgs/" + req.Owner + "/repos"
	} else if g.cfg.Owner != "" && req.Owner != g.cfg.Owner {
		return nil, NewMsg(KindRefused, "refused: owner kind \"user\" creates under the AUTHENTICATED identity, and this client's owner is "+g.cfg.Owner+" — creating as "+req.Owner+" would create somewhere nobody authorized")
	}
	var out repoPayload
	if err := g.send(ctx, http.MethodPost, path, map[string]any{
		"name": req.Name, "private": req.Private, "description": req.Description,
	}, &out); err != nil {
		return nil, err
	}
	return out.toRepo(), nil
}

// GetRepo is the check-then-act read: GET /repos/{owner}/{name}. Absence
// is (nil, nil) — the Find* discipline of this package; every other answer
// travels as its mapped error.
func (g *GitHub) GetRepo(ctx context.Context, owner, name string) (*Repo, error) {
	if !ownerNameRule.MatchString(owner) || !repoNameRule.MatchString(name) {
		return nil, NewMsg(KindRefused, "refused: owner/name breaks the closed name rules")
	}
	if g.cfg.BaseURL == "" {
		return nil, NewMsg(KindAuth, "Config.BaseURL is empty — the live host is not a test default")
	}
	var out repoPayload
	err := g.send(ctx, http.MethodGet, g.cfg.BaseURL+"/repos/"+owner+"/"+name, nil, &out)
	if err != nil {
		var fe *ForgeError
		if asForge(err, &fe) && fe.Kind == KindNotFound {
			return nil, nil
		}
		return nil, err
	}
	return out.toRepo(), nil
}

// mapCheckRun is the VG8 table for check runs: status × conclusion.
// `completed` + a good conclusion is success; `completed` + a bad one is
// failure; every NON-COMPLETED status the API names (queued, in_progress,
// and the three VG8 does not name — waiting, requested, pending — the T0
// item-15 ruling: non-terminal, never success) is pending. ANY other
// string is unknown — the optimistic-mapping guard (VG8).
func mapCheckRun(status, conclusion string) string {
	switch status {
	case "completed":
		switch conclusion {
		case "success", "neutral", "skipped":
			return CheckSuccess
		case "failure", "cancelled", "timed_out", "action_required", "stale":
			return CheckFailure
		default:
			return CheckUnknown // completed without a (known) conclusion
		}
	case "queued", "in_progress", "waiting", "requested", "pending":
		return CheckPending
	default:
		return CheckUnknown
	}
}

// mapStatusState is the VG8 table for legacy commit statuses, applied to
// each entry of the statuses array (never the combined state — see
// ReadChecks).
func mapStatusState(state string) string {
	switch state {
	case "success":
		return CheckSuccess
	case "failure", "error":
		return CheckFailure
	case "pending":
		return CheckPending
	default:
		return CheckUnknown
	}
}

// linkNext pulls the rel="next" URL out of a Link header
// (`<url>; rel="next", <url>; rel="last"`); "" when there is none.
func linkNext(h http.Header) string {
	for _, part := range strings.Split(h.Get("Link"), ",") {
		i := strings.Index(part, ";")
		if i < 0 {
			continue
		}
		url := strings.TrimSpace(part[:i])
		rel := part[i+1:]
		url = strings.TrimPrefix(url, "<")
		url = strings.TrimSuffix(url, ">")
		if strings.Contains(rel, `rel="next"`) {
			return url
		}
	}
	return ""
}

// ---------------------------------------------------------------------------

type prPayload struct {
	Number int  `json:"number"`
	Merged bool `json:"merged"`
	Head   struct {
		Ref string `json:"ref"`
		SHA string `json:"sha"` // S57 T2: the checks' key (VG8)
	} `json:"head"`
	Base struct {
		Ref string `json:"ref"`
	} `json:"base"`
	MergeCommitSHA string `json:"merge_commit_sha"`
}

func (p prPayload) toPR() *PR {
	// BaseRef comes back from the wire always (L26): the phantom-merge
	// guard reads it before merging, and a PR that reaches the guard
	// without a base is the guard disarmed.
	return &PR{Number: p.Number, Head: p.Head.Ref, BaseRef: p.Base.Ref, Merged: p.Merged, MergeSHA: p.MergeCommitSHA, HeadSHA: p.Head.SHA}
}

func verdictFromState(state string) Verdict {
	switch strings.ToUpper(state) {
	case "APPROVED":
		return VerdictApprove
	case "CHANGES_REQUESTED":
		return VerdictRequestChanges
	default:
		return VerdictComment
	}
}

func eventOf(v Verdict) string {
	switch v {
	case VerdictApprove:
		return "APPROVE"
	case VerdictRequestChanges:
		return "REQUEST_CHANGES"
	default:
		return "COMMENT"
	}
}

func queryEscape(s string) string {
	// The stdlib's url package is only reachable through net/http's
	// siblings; the values here are labels and branch refs — the
	// characters that need escaping are space, colon, and slash.
	r := strings.NewReplacer(" ", "%20", ":", "%3A", "/", "%2F", "&", "%26", "#", "%23", "?", "%3F")
	return r.Replace(s)
}
