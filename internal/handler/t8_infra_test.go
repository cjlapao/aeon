package handler

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/cjlapao/aeon/internal/model"
	"github.com/cjlapao/aeon/internal/policy"
)

// T8 — rewrites 21–25: docker-engineer, terraform-engineer, security-engineer,
// legal-advisor, ui-design-engineer.
//
// The batch's center of gravity: docker-engineer's policy is the corpus's
// only mid-pattern-wildcard content (the reason S11's matcher cannot be
// prefix-only — G9), the two specialists' advisory framing reads like
// boilerplate and is therefore the first casualty of any lazy shortening
// pass (A4), and no policy may be added anywhere (opencode/ is read-only to
// this slice — A10/D14).

var t8Batch = []string{
	"docker-engineer",
	"terraform-engineer",
	"security-engineer",
	"legal-advisor",
	"ui-design-engineer",
}

// Named TestFiveInfraHandlersRunOnce: the spec's gate name
// TestFivePassVerifyAndRunOnce is taken (T6 owns it in this package; T7
// recorded the same deviation).
var t8Required = map[string][]string{
	"docker-engineer":    {"size", "scan"},
	"terraform-engineer": {"plan_summary"},
	"security-engineer":  {"scanners", "verdict"},
	"legal-advisor":      {"outcome"},
	"ui-design-engineer": {"visual_proof", "contrast"},
}

func t8Report(t *testing.T, name string) t4Turn {
	t.Helper()
	fields := map[string]string{
		"summary":      name + " completed the step",
		"size":         "final image 41MB, 12 layers; cold build 38s warm 6s",
		"scan":         "trivy image: 0 HIGH/CRITICAL",
		"plan_summary": "plan: +3 ~1 -0 -0 replaces; no destroy on existing resources",
		"scanners":     "govulncheck: 0 critical, 0 high, 1 medium (recorded); gitleaks on diff: 0",
		"verdict":      "pass",
		"outcome":      "clear",
		"visual_proof": "3 screenshots captured for every variant x state",
		"contrast":     "5.2:1 over glass; reduced-transparency fallback verified",
	}
	body := map[string]string{"status": "ok"}
	for _, k := range append([]string{"summary"}, t8Required[name]...) {
		body[k] = fields[k]
	}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal report: %v", err)
	}
	return t4Turn{Text: string(raw)}
}

func TestFiveInfraHandlersRunOnce(t *testing.T) {
	tasks := map[string]string{
		"docker-engineer":    "containerize the Go service: multi-stage Dockerfile + compose",
		"terraform-engineer": "add the S3 bucket module for artifact storage",
		"security-engineer":  "review this task's diff for security issues and record the result",
		"legal-advisor":      "assess legal surface: new AGPL dependency was flagged",
		"ui-design-engineer": "add a glass variant to the Button component",
	}
	for _, n := range t8Batch {
		d, rej := LoadDefinition(n)
		if rej != nil {
			t.Fatalf("%s: %v", n, rej)
		}
		if fs := Verify(d, nil); len(fs) > 0 {
			for _, f := range fs {
				t.Errorf("%s", f.String())
			}
		}
		res, _ := dispatchWithRequired(t, n, tasks[n], append([]string{"summary"}, t8Required[n]...),
			t4Turn{ToolUses: []model.Block{writeBlock("NOTES.md", n+" artifact\n")}},
			t8Report(t, n),
		)
		mustOK(t, res, n)
		if got := fmt.Sprint((*res.Handoff)["summary"]); !strings.Contains(got, n) {
			t.Errorf("%s: handoff summary %q did not come from this handler's scripted report", n, got)
		}
	}
}

// ── docker policy: byte-identical to the corpus, stars in place ────────────

// corpusBashPatterns re-parses one agent's permission.bash keys from the
// opencode/ original independently of S11's extractor — D5: a normalisation
// anywhere in the chain that moved or dropped a `*` shows up as a mismatch
// on both sides.
func corpusBashPatterns(t *testing.T, loopName string) []string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(repoRoot(t), "opencode", "agents", loopName+".md"))
	if err != nil {
		t.Fatalf("read original: %v", err)
	}
	fm := strings.SplitN(string(raw), "---", 3)[1]
	var doc struct {
		Permission struct {
			Bash map[string]string `yaml:"bash"`
		} `yaml:"permission"`
	}
	if err := yaml.Unmarshal([]byte(fm), &doc); err != nil {
		t.Fatalf("parse front matter: %v", err)
	}
	var out []string
	for k := range doc.Permission.Bash {
		out = append(out, k)
	}
	return out
}

func TestDockerPolicyMatchesTheCorpus(t *testing.T) {
	c, rej := policy.ParseCorpus(policy.EmbeddedCorpus())
	if rej != nil {
		t.Fatal(rej.Reason)
	}
	rules, ok := c.RulesFor("docker-engineer-loop")
	if !ok {
		t.Fatal("corpus has no docker-engineer-loop agent")
	}
	// The two mid-pattern wildcards, byte-identical, `*` positions included.
	want := []string{"docker buildx build*--push*", "docker buildx bake*--push*"}
	got := map[string]string{}
	for _, r := range rules {
		if strings.Contains(strings.TrimSuffix(r.Pattern, "*"), "*") { // star not only at the tail
			got[r.Pattern] = string(r.Verdict)
		}
	}
	if len(got) != 2 {
		t.Fatalf("docker-engineer-loop carries %d mid-pattern-wildcard rules, want exactly 2: %v", len(got), got)
	}
	for _, w := range want {
		if v, ok := got[w]; !ok {
			t.Errorf("corpus lost the mid-wildcard pattern %q byte-for-byte — these are the only two patterns in the entire corpus that force S11's matcher past prefix-only (G9); a normalisation that moved or dropped a * changes what the guard permits", w)
		} else if v != string(policy.Deny) {
			t.Errorf("%q verdict is %q, want deny", w, v)
		}
	}
	// And they exist nowhere else: the whole corpus has exactly two such rules.
	total := 0
	for _, p := range c.DistinctPatterns() {
		if strings.Contains(strings.TrimSuffix(p, "*"), "*") {
			total++
		}
	}
	if total != 2 {
		t.Errorf("the corpus now carries %d mid-pattern-wildcard patterns, expected exactly the docker pair — either they moved or new ones appeared without an authored source", total)
	}
	// Every docker pattern is byte-identical to the original's own keys.
	orig := corpusBashPatterns(t, "docker-engineer-loop")
	seen := map[string]bool{}
	for _, r := range rules {
		seen[r.Pattern] = true
	}
	for _, p := range orig {
		if !seen[p] {
			t.Errorf("corpus lost docker rule %q authored in the original — A16: policy is only ever the extractor's output, byte-identical", p)
		}
	}
}

func TestNoPolicyWasAdded(t *testing.T) {
	c, rej := policy.ParseCorpus(policy.EmbeddedCorpus())
	if rej != nil {
		t.Fatal(rej.Reason)
	}
	loops := map[string]string{
		"docker-engineer":    "docker-engineer-loop",
		"terraform-engineer": "terraform-engineer-loop",
		"security-engineer":  "security-engineer-loop",
		"legal-advisor":      "legal-advisor-loop",
		"ui-design-engineer": "ui-design-engineer-loop",
	}
	for _, loop := range loops {
		rules, ok := c.RulesFor(loop)
		if !ok {
			t.Fatalf("corpus has no %s", loop)
		}
		corpusSet := map[string]bool{}
		for _, r := range rules {
			corpusSet[r.Pattern] = true
		}
		for _, p := range corpusBashPatterns(t, loop) {
			if !corpusSet[p] {
				t.Errorf("%s: original authors %q but the corpus does not carry it", loop, p)
			}
		}
		origSet := map[string]bool{}
		for _, p := range corpusBashPatterns(t, loop) {
			origSet[p] = true
		}
		for p := range corpusSet {
			if !origSet[p] {
				t.Errorf("%s: the corpus carries %q which no original authors — opencode/ is read-only to this slice; policy is data extracted FROM it, and adding a rule here without an authored source is inventing policy (A10, D14). If terraform warrants an ask rule, that is a FINDING to raise, not an edit to make", loop, p)
			}
		}
	}
}

// ── the specialists stay advisory ──────────────────────────────────────────

func TestAdvisoryFramingSurvives(t *testing.T) {
	sec := bodyOf(t, "security-engineer")
	for _, want := range []string{
		"not a full audit",
		"No false clears",
		"checked, no issues found",
		"I finished the scan",
	} {
		if !strings.Contains(sec, want) {
			t.Errorf("security-engineer lost %q — its output is a scoped assessment honestly recorded, not a certification; a scanner that is missing is said to be missing, and 'done' means the scan ran, not that the code is safe. Losing this framing turns analysis into what a consuming loop reads as sign-off", want)
		}
	}
	legal := bodyOf(t, "legal-advisor")
	for _, want := range []string{
		"NOT licensed counsel",
		"advisory, not a legal opinion",
		"attorney-client",
		"escalate to qualified counsel",
	} {
		if !strings.Contains(legal, want) {
			t.Errorf("legal-advisor lost %q — the One Disclaimer leads every document it writes; this is exactly the line that reads like boilerplate and dies first in a shortening pass, and without it a gap-analysis gets consumed as a legal sign-off", want)
		}
	}
	for _, pair := range []struct{ name, b string }{{"security-engineer", sec}, {"legal-advisor", legal}} {
		lower := strings.ToLower(pair.b)
		for _, cert := range []string{"certif", "warrants", "guarantee", "cleared for production", "audit-complete"} {
			if strings.Contains(lower, cert) {
				t.Errorf("%s reads as authoritative (%q) — both specialists advise; neither certifies (A4)", pair.name, cert)
			}
		}
	}
}

func TestSecurityToolingIsDeferred(t *testing.T) {
	sec := bodyOf(t, "security-engineer")
	// The scanner command list is Aeon-environment-dependent tooling: gone
	// from the body, named in the record. What stays is the judgement.
	for _, tool := range []string{"semgrep", "gitleaks", "trufflehog", "govulncheck", "trivy", "checkov", "tfsec", "gosec", "cargo audit", "npm audit"} {
		if strings.Contains(sec, tool) {
			t.Errorf("security-engineer's body names %q — scanners may not exist in an installation; the body states the judgement (what to look for, what severity blocks on) and leaves the capability list to the drop record (A18: DEFERRED with the capability named)", tool)
		}
	}
	rec, rej := ParseDropRecord("drops/security-engineer.md")
	if rej != nil {
		t.Fatal(rej)
	}
	deferred := strings.Join(rec.Sections["DEFERRED"], "\n")
	named := 0
	for _, tool := range []string{"semgrep", "gitleaks", "trufflehog", "govulncheck", "trivy", "checkov", "tfsec", "gosec", "audit"} {
		if strings.Contains(deferred, tool) {
			named++
		}
	}
	if named < 6 {
		t.Errorf("the security record's DEFERRED names %d scanner capabilities; it must name them, not wave at 'tooling' — A18 records what was left out by capability", named)
	}
	for _, want := range []string{"blocks the PR", "Medium", "Critical", "recorded"} {
		if !strings.Contains(sec, want) {
			t.Errorf("security-engineer lost severity judgement (%q) — what survives the tooling is knowing what blocks and what only gets recorded", want)
		}
	}
}

// ── docker: push is policy now (A12) ───────────────────────────────────────

func TestDockerPushProseIsGone(t *testing.T) {
	b := bodyOf(t, "docker-engineer")
	for _, forbidden := range []string{"docker push", "--push", "never publish", "do not push", "NEVER push"} {
		if strings.Contains(strings.ToLower(b), strings.ToLower(forbidden)) {
			t.Errorf("docker-engineer's body still carries the don't-push instruction (%q) — the corpus denies it (docker push*, buildx build*--push*, bake*--push*); prose that restates enforcement is a second, staler copy of the guard (A12). The One Rule's image-you-push-at-3am line is craft, not an instruction, and stays", forbidden)
		}
	}
	// The craft line itself survives.
	if !strings.Contains(b, "debug at 3am") {
		t.Error("docker-engineer lost the One Rule — the small/pinned/scanned/non-root/healthchecked bar is its gate identity")
	}
}

func TestNoBodiesNameMCP(t *testing.T) {
	// T6's rule generalised: a body that names MCP tooling instructs calls to
	// servers an installation may not have configured; those surfaces are
	// DEFERRED (with the name) in the record.
	for _, n := range t8Batch {
		if strings.Contains(strings.ToLower(bodyOf(t, n)), "mcp") {
			t.Errorf("%s's body names MCP tooling — the capability rides with S12's client when an operator configures it; the body instructs none of it (A18)", n)
		}
		if strings.Contains(strings.ToLower(bodyOf(t, n)), "playwright") {
			t.Errorf("%s's body names Playwright — the screenshot tool is the deferred surface; the body keeps the visual-evidence requirement and the honest-blocked fallback (T6 ruling)", n)
		}
	}
}

func TestInfraPlacementIsDelegated(t *testing.T) {
	for _, n := range t8Batch {
		b := bodyOf(t, n)
		for _, f := range []string{".projects/loops", "loopSlug", "$ROOT", "file lane", "ui-shots/"} {
			if strings.Contains(b, f) {
				t.Errorf("%s still says where to put things (%q) — produces: decides placement; the body decides shape (T7 ruling, extended to this batch)", n, f)
			}
		}
	}
	for _, n := range []string{"security-engineer", "legal-advisor", "ui-design-engineer"} {
		if !strings.Contains(bodyOf(t, n), "the step's request names") {
			t.Errorf("%s never says its per-loop output path comes from the step's request — without the body-side instruction the handler invents a path", n)
		}
	}
}
