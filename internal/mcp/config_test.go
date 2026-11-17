package mcp

import (
	"strings"
	"testing"
)

// The validation tests are Q3's argument in executable form: the shape in
// which a mis-tiered server is HARD TO WRITE. Every case below is a mistake
// an operator could plausibly make, and the config must refuse it at load,
// naming what is wrong — not resolve it silently into some tier.

func mustParse(t *testing.T, raw string) Config {
	t.Helper()
	cfg, err := parseConfig([]byte(raw))
	if err != nil {
		t.Fatalf("parseConfig: %v", err)
	}
	return cfg
}

func parseFails(t *testing.T, raw, wantSub string) {
	t.Helper()
	_, err := parseConfig([]byte(raw))
	if err == nil {
		t.Fatalf("config accepted a mistake; want a refusal naming %q\n%s", wantSub, raw)
	}
	if !strings.Contains(err.Error(), wantSub) {
		t.Errorf("refusal = %v, want it to name %q", err, wantSub)
	}
}

func TestValidConfigParses(t *testing.T) {
	cfg := mustParse(t, `{
	  "public": [{"name":"docsproxy",
	    "transport":{"type":"http","url":"http://127.0.0.1:9/mcp"},
	    "tools":[{"name":"docs","rerun":"safe","effect":"local"}]}],
	  "agents": {"reddit-agent": {
	    "private": [{"name":"fakeserver",
	      "transport":{"type":"stdio","command":"/bin/true"},
	      "tools":[{"name":"post","rerun":"unsafe","effect":"irreversible"}]}],
	    "declares":["docsproxy.docs"]}}
	}`)
	if len(cfg.Public) != 1 || len(cfg.Agents["reddit-agent"].Private) != 1 {
		t.Fatalf("config did not load: %+v", cfg)
	}
}

// TestEmptyConfigIsValid is MC11's floor: no MCP configuration is not an
// error — the file may be absent (the caller sees that) or empty here.
func TestEmptyConfigIsValid(t *testing.T) {
	mustParse(t, `{}`)
	mustParse(t, `{"public":[],"agents":{}}`)
}

// TestUnknownFieldRefused: a misremembered key ("tier", "private_to",
// "visibility") must fail the load, not be ignored. An ignored key is a
// tier silently not set.
func TestUnknownFieldRefused(t *testing.T) {
	parseFails(t, `{"tier":"public"}`, "tier")
	parseFails(t, `{"public":[{"name":"s","tier":"public",
	  "transport":{"type":"http","url":"http://h/mcp"},"tools":[]}]}`, "tier")
	parseFails(t, `{"agents":{"a":{"private_to":"a","private":[],
	  "private servers":[]}}}`, "private_to")
}

// TestServerNameMustNotContainTheSeparator is MC8's precondition: the dot is
// the namespace join, so it may not appear inside a name.
func TestServerNameMustNotContainTheSeparator(t *testing.T) {
	parseFails(t, `{"public":[{"name":"a.b",
	  "transport":{"type":"http","url":"http://h/mcp"},"tools":[]}]}`, "a.b")
	parseFails(t, `{"public":[{"name":"",
	  "transport":{"type":"http","url":"http://h/mcp"},"tools":[]}]}`, "name")
}

// TestSameServerTwoTiersRefused is MC12 by construction: the same server
// name configured public AND private-to-an-agent is refused at load. A
// tier that depends on who is asking has no auditable answer.
func TestSameServerTwoTiersRefused(t *testing.T) {
	parseFails(t, `{
	  "public": [{"name":"shared","transport":{"type":"http","url":"http://h/mcp"},"tools":[]}],
	  "agents": {"a": {"private": [{"name":"shared",
	    "transport":{"type":"stdio","command":"/bin/true"},"tools":[]}]}}
	}`, "shared")
}

// TestSameServerPrivateToTwoAgentsRefused keeps "private" meaning exactly
// one agent: one name, two owners, is not one owner.
func TestSameServerPrivateToTwoAgentsRefused(t *testing.T) {
	parseFails(t, `{
	  "agents": {
	    "a": {"private": [{"name":"reddit","transport":{"type":"stdio","command":"/bin/true"},"tools":[]}]},
	    "b": {"private": [{"name":"reddit","transport":{"type":"stdio","command":"/bin/true"},"tools":[]}]}
	  }}`, "reddit")
}

func TestDuplicateServerInOneSectionRefused(t *testing.T) {
	parseFails(t, `{"public":[
	  {"name":"dup","transport":{"type":"http","url":"http://h/mcp"},"tools":[]},
	  {"name":"dup","transport":{"type":"http","url":"http://h/mcp"},"tools":[]}]}`, "dup")
}

func TestDuplicateToolInOneServerRefused(t *testing.T) {
	parseFails(t, `{"public":[{"name":"s","transport":{"type":"http","url":"http://h/mcp"},
	  "tools":[{"name":"search","rerun":"safe","effect":"local"},
	           {"name":"search","rerun":"safe","effect":"local"}]}]}`, "search")
}

// TestDeclareMustNameAPublicTool is the public tier's referential integrity:
// declaring is naming, and naming something that does not exist (unknown
// server, unknown tool, or a PRIVATE server's tool) is a config error, not a
// no-op — a no-op declare is how a typo becomes "the tool disappeared".
func TestDeclareMustNameAPublicTool(t *testing.T) {
	base := `{
	  "public": [{"name":"docsproxy","transport":{"type":"http","url":"http://h/mcp"},
	    "tools":[{"name":"docs","rerun":"safe","effect":"local"}]}],
	  "agents": {"a": %s}}`
	parseFails(t, sprintf(base, `{"declares":["nobody.docs"]}`), "nobody")
	parseFails(t, sprintf(base, `{"declares":["docsproxy.nope"]}`), "docsproxy.nope")
	parseFails(t, sprintf(base, `{"declares":["docsproxy"]}`), "docsproxy")
	// Declaring another agent's private tool is the mis-tier write the shape
	// is designed to make loud.
	parseFails(t, `{
	  "public": [{"name":"docsproxy","transport":{"type":"http","url":"http://h/mcp"},
	    "tools":[{"name":"docs","rerun":"safe","effect":"local"}]}],
	  "agents": {
	    "a": {"private":[{"name":"fakeserver","transport":{"type":"stdio","command":"/bin/true"},
	          "tools":[{"name":"post","rerun":"unsafe","effect":"irreversible"}]}]},
	    "b": {"declares":["fakeserver.post"]}
	  }}`, "fakeserver.post")
}

// TestTransportShape: exactly the fields its type means; stdio needs a
// command; http needs a URL. A half-declared transport is a server that
// "exists" and answers nothing — degradation (T8) is for servers that were
// really configured.
func TestTransportShape(t *testing.T) {
	parseFails(t, `{"public":[{"name":"s","transport":{"type":"carrier-pigeon"},
	  "tools":[]}]}`, "carrier-pigeon")
	parseFails(t, `{"public":[{"name":"s","transport":{"type":"stdio"},
	  "tools":[]}]}`, "command")
	parseFails(t, `{"public":[{"name":"s","transport":{"type":"http"},
	  "tools":[]}]}`, "url")
	parseFails(t, `{"public":[{"name":"s","transport":{"type":"http","url":"not-a-url"},
	  "tools":[]}]}`, "not-a-url")
	parseFails(t, `{"public":[{"name":"s",
	  "transport":{"type":"stdio","command":"/bin/true","url":"http://h/mcp"},"tools":[]}]}`, "url")
}

// TestCredentialNeverInConfig is C17 at the config boundary: transport env
// carries variable NAMES; a value-shaped field fails to parse.
func TestCredentialNeverInConfig(t *testing.T) {
	cfg := mustParse(t, `{"public":[{"name":"s",
	  "transport":{"type":"stdio","command":"/bin/true","env":["GITHUB_TOKEN"]},
	  "tools":[]}]}`)
	if len(cfg.Public[0].Transport.Env) != 1 || cfg.Public[0].Transport.Env[0] != "GITHUB_TOKEN" {
		t.Errorf("env = %v, want the variable NAME", cfg.Public[0].Transport.Env)
	}
	parseFails(t, `{"public":[{"name":"s",
	  "transport":{"type":"stdio","command":"/bin/true","apiKey":"sekret"},
	  "tools":[]}]}`, "apiKey")
}

// TestCoreSubsetNamesExist: a core grant naming something outside the
// registry is refused — otherwise a typo ("red") is indistinguishable from
// "granted nothing".
func TestCoreSubsetNamesExist(t *testing.T) {
	parseFails(t, `{"agents":{"a":{"core":["read","red"]}}}`, "red")
}

// TestRefusedToolStillNeedsAName — small, but a nameless tool config is the
// kind of thing that parses into a nil-keyed map and surfaces at call time.
func TestRefusedToolStillNeedsAName(t *testing.T) {
	parseFails(t, `{"public":[{"name":"s","transport":{"type":"http","url":"http://h/mcp"},
	  "tools":[{"rerun":"safe","effect":"local"}]}]}`, "name")
	parseFails(t, `{"public":[{"name":"s","transport":{"type":"http","url":"http://h/mcp"},
	  "tools":[{"name":"bad name"}]}]}`, "bad name")
}

func sprintf(format, one string) string {
	return strings.Replace(format, "%s", one, 1)
}
