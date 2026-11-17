package daemon

// Fixture generator for S47 T5 — run it by hand, never in CI:
//
//	S47_FIXTURES=1 go test -tags sqlite_fts5 ./internal/daemon/ -run TestZZFixtures
//
// # Why the fixtures are GENERATED and not typed by hand
//
// A conformance fixture is a claim about the wire: the protocol suite unmarshals
// every fixture into the Go type, marshals it back and compares as data, so a
// hand-typed fixture that invented a field name, dropped a required one, or
// paraphrased a verdict sentence would either fail the round trip or — worse, if it
// happened to fit — teach the next client a shape the daemon never sends. Every
// value in these files came out of the adapter over a real store: the schema bytes
// are the code's canonical schema, the verdict sentences are `Verdict.Sentence()`,
// the `describe` strings are `policy.Describe`, the hashes are `tools.SchemaHash`.
// Nothing here was written from memory.
//
// The world is four agents (two the app can dispatch, one first-party, one custom),
// four `tool_access` rows (an operator deny, an operator allow over all agents, an
// operator ask, a system-authored allow), and one enabled private MCP server with
// two declared tools, one of them unclassified. The agent arrays are trimmed to
// that roster — the only editorial act performed here, and it trims rows rather
// than inventing them.
//
// The status fixture is dumped AFTER two rows are put out of step with the code, so
// the drift report it states is a report the adapter actually produced.
//
// The file is deleted once the fixtures land. It is kept out of CI by the
// environment variable, exactly like the dump it grew from.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/policy"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/seed"
	"github.com/cjlapao/aeon/internal/store"
	"github.com/cjlapao/aeon/internal/toolcatalog"
	"github.com/cjlapao/aeon/internal/toolseed"
)

type fixtureEnvelopeReq struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int    `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type fixtureEnvelopeResp struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int    `json:"id"`
	Result  any    `json:"result,omitempty"`
	Error   any    `json:"error,omitempty"`
}

type fixtureDoc struct {
	Description string              `json:"description"`
	Method      string              `json:"method"`
	Request     fixtureEnvelopeReq  `json:"request"`
	Response    fixtureEnvelopeResp `json:"response"`
}

func TestZZFixtures(t *testing.T) {
	dir := os.Getenv("S47_FIXTURES")
	if dir == "" {
		t.Skip("set S47_FIXTURES=<the conformance/tools directory> to regenerate the S47 fixtures")
	}
	env := newFixtureWorld(t)
	ctx := context.Background()
	next := 640
	id := func() int { n := next; next++; return n }

	write := func(name, description, method string, params any, result any, failure error) {
		t.Helper()
		if failure != nil {
			e, ok := failure.(*protocol.Error)
			if !ok {
				t.Fatalf("%s: the refusal is %T, not a *protocol.Error", name, failure)
			}
			doc := fixtureDoc{
				Description: description, Method: method,
				Request:  fixtureEnvelopeReq{JSONRPC: "2.0", ID: id(), Method: method, Params: params},
				Response: fixtureEnvelopeResp{JSONRPC: "2.0", ID: 0, Error: e},
			}
			doc.Response.ID = doc.Request.ID
			b, err := json.MarshalIndent(doc, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, name), append(b, '\n'), 0o644); err != nil {
				t.Fatal(err)
			}
			return
		}
		doc := fixtureDoc{
			Description: description, Method: method,
			Request: fixtureEnvelopeReq{JSONRPC: "2.0", ID: id(), Method: method, Params: params},
		}
		doc.Response = fixtureEnvelopeResp{JSONRPC: "2.0", ID: doc.Request.ID, Result: result}
		b, err := json.MarshalIndent(doc, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), append(b, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// --- tools.list -------------------------------------------------------------------
	bash := mustList(t, env, protocol.ToolsListFilter{Family: strptr("process")})
	write("list-family.json",
		"tools.list with one filter: the `process` family is `bash` alone, and the entry is the whole TC10 shape — `schema` is the canonical schema the EXECUTOR parses against (TC2, not the row's copy), `schemaHash` its digest, `rerunClass` unsafe because a shell command cannot be re-run without consequence, `capabilities` the closed vocabulary word `spawn_process`, and `governed` naming the two policy types that judge every call (tool_access AND command_access — a tool governed by more than one type is the common case, and a panel that showed only the first would hide the rule that actually refuses it). An absent filter is a nil pointer at the seam and the family is matched case-insensitively against the code's own family, never a client-side guess.",
		string(protocol.MethodToolsList), protocol.ToolsListFilter{Family: strptr("process")},
		protocol.ToolsListResult{Tools: bash}, nil)

	mcp := mustList(t, env, protocol.ToolsListFilter{Source: strptr(protocol.ToolsSourceMCP)})
	write("list-mcp.json",
		"tools.list with source=mcp: the MCP half of the catalog, which is NOT in the `tool` table at all (TC4) and so carries no seeded `version` (0) and no `rowCurrent` claim of its own beyond the server it came from. `acme.post` is MC6 made visible — a declared tool nobody classified, `classified:false`, `available:true` because it is a real tool of a real enabled server, and `effect:outward` with `rerunClass:unsafe`; hiding it would let the management screen pretend an outward-facing capability is absent while its server still advertises it. `description` is EMPTY because no authority holds one (S46's `mcp_server_tool` records name, rerun and effect): an invented sentence would be the catalog authoring a contract it cannot enforce. Both belong to the enabled `acme` server; the disabled `off` server's `sleep` is nowhere, because a tool of a disabled server cannot be called by anyone.",
		string(protocol.MethodToolsList), protocol.ToolsListFilter{Source: strptr(protocol.ToolsSourceMCP)},
		protocol.ToolsListResult{Tools: mcp}, nil)

	unwired := mustList(t, env, protocol.ToolsListFilter{Available: boolptr(false)})
	write("list-unwired.json",
		"tools.list with available=false — P31's honest third state made listable: `search` and `graph` are tools this build IMPLEMENTS and cannot RUN, because their resolver seams are not wired here. They are listed, with their real schemas and hashes, and `available:false`; they are not omitted (an omission reads as \"no such tool\" and sends the reader to the wrong screen) and not refused. `conditional:true` says why, and `tools.status`' `counts.conditionalUnwired` is the same two counted from the same single pass over one world.",
		string(protocol.MethodToolsList), protocol.ToolsListFilter{Available: boolptr(false)},
		protocol.ToolsListResult{Tools: unwired}, nil)

	write("list-no-match.json",
		"tools.list with a `q` that can match nothing. The list is EMPTY (`[]`, never null) and the table's own sentence rides with it, byte for byte, because a filter that matched nothing and a build with no tools are different answers and only the reader knows which it is looking at. The sentence belongs to the layer that saw the filter: an UNFILTERED empty list carries no statement at all, and a non-empty one never does — a decoration on every answer would make the sentence mean nothing.",
		string(protocol.MethodToolsList), protocol.ToolsListFilter{Q: strptr("zzz-nothing")},
		protocol.ToolsListResult{Tools: []protocol.ToolEntry{}, Statement: protocol.ToolsListNoMatchStatement}, nil)

	// --- tools.get --------------------------------------------------------------------
	got := func(name string) protocol.ToolsGetResult {
		res, err := env.svc.ToolsGet(ctx, name)
		if err != nil {
			t.Fatalf("tools.get %s: %v", name, err)
		}
		res.Agents = trimAgents(t, res.Agents)
		return res
	}

	gotBash := got("bash")
	write("get-ok.json",
		"tools.get on `bash`: the tool, EVERY agent that exists, and the rules behind it. The roster is complete on purpose — `atlas` holds bash and a deny refuses it (`granted_denied`, with the engine's sentence verbatim), `brief-author` holds it and no allow matched so PL5 decides (`granted_no_rule`, whose sentence says \"no allow matched\" and must never be rendered as a denial), `sentry` does not hold it at all (`not_granted` with NO reason: the verdict was never sought, and a reason here would be a sentence the evaluator never said), and `cursitor` is a custom agent whose row names tools it can never exercise (`dispatchable:false`, state `not_granted`). `granted_by` says which row put the name there — `default` for the unconditional grant, `handler` for the agent's own row. `policies[].describe` is `policy.Describe` verbatim: one formatter, no client-side assembly. `mcp` is absent because there is no server to link.",
		string(protocol.MethodToolsGet), protocol.ToolsGetParams{Name: "bash"}, gotBash, nil)

	gotWrite := got("write_file")
	write("get-alias.json",
		"tools.get on `write_file`, the hidden alias: the answer is the CANONICAL tool (`write`) with its aliases riding, resolved the same way the grant chain and the policy rows resolve it (SEC-051/SEC-059) — one rule about identity, applied by every door, so a grant written against a spelling cannot disagree with a read written against another. The alias is never an entry of its own in `tools.list` (TC11: one executor rendered twice is the drift the whole table exists to prevent), and `twin` names the management-tool counterpart the same executor serves.",
		string(protocol.MethodToolsGet), protocol.ToolsGetParams{Name: "write_file"}, gotWrite, nil)

	gotMCP := got("acme.search")
	write("get-mcp.json",
		"tools.get on a qualified MCP name: `acme.search` answers with its server half (`server`, `tier`) and the DATA link the UI navigates by (`link.kind:\"mcp_server\"`, `link.id:\"acme\"` — S45's discipline that links are data a client resolves, never a path it builds), and `policies` is empty because this tool's availability is S12's effect-class answer, not a `tool_access` row. Its `agents` column shows `atlas` `granted_allowed` with the view's reason verbatim and `granted_by:[mcp-declare]` — the agent's own declaration is the row behind the grant — while `sentry` never declared against this private server and sees `not_granted`, which is also why no door in this slice can enumerate a peer's servers (MC2).",
		string(protocol.MethodToolsGet), protocol.ToolsGetParams{Name: "acme.search"}, gotMCP, nil)

	_, unknownToolErr := env.svc.ToolsGet(ctx, "reed")
	write("get-unknown.json",
		"tools.get on a name that does not exist: -31911, and the refusal NAMES the nearest tool. A bare not-found would be a dead end, and the usual cause is a typo or an alias spelled from memory — tool names are authored in code, so there is a fixed set to be near. The sentence is the ADAPTER's, produced by the one layer that has read the registry and the rows, and the protocol door ships it untouched under its own code (a paraphrase at the door would be a second authority over a fact it cannot see). The same answer must never be an empty entry: an empty tool would be the plausible wrong answer, readable as \"this tool does nothing\".",
		string(protocol.MethodToolsGet), protocol.ToolsGetParams{Name: "reed"}, nil, unknownToolErr)

	// --- tools.byAgent -----------------------------------------------------------------
	byAgent := func(agent string, keep ...string) protocol.ToolsByAgentResult {
		res, err := env.svc.ToolsByAgent(ctx, agent)
		if err != nil {
			t.Fatalf("tools.byAgent %s: %v", agent, err)
		}
		want := map[string]bool{}
		for _, k := range keep {
			want[k] = true
		}
		rows := []toolcatalog.Entry{}
		for _, e := range res.Tools {
			if want[e.Tool] {
				rows = append(rows, e)
			}
		}
		if len(rows) != len(keep) {
			t.Fatalf("%s: asked for %v, found %v", agent, keep, len(rows))
		}
		res.Tools = rows
		return res
	}

	atlas := byAgent("atlas", "acme.post", "acme.search", "bash", "read", "webfetch", "todo")
	write("by-agent-ok.json",
		"tools.byAgent for `atlas`: the whole view one agent may call, in one read, and the answer the gate itself would give (TC5 — the same `GrantedSet` union and the same evaluator the step's builder uses, checked row by row against both in the daemon's own suite). All five states are here: `granted_allowed` (the allow that decides, `read`, and the effect class that decides for an MCP tool), `granted_ask` (`webfetch`, an ask that outranks the allow and is NOT a permission — S11), `granted_denied` (bash, refused by a named row), `granted_no_rule` (acme.post, held and unclassified: no rule spoke, which is a different fact from a denial and from \"nobody declared it\"), and `not_granted` (`todo`, outside the grant set, with no reason because none was sought). MCP names are qualified and appear ONLY for the servers this agent declared against — an agent cannot ask about another agent, and there is no field in which to try (TC9). The list is every tool that EXISTS, not only the granted ones: \"may call nothing\" and \"this tool was never granted to you\" are the two answers a grants screen exists to tell apart.",
		string(protocol.MethodToolsByAgent), protocol.ToolsByAgentParams{Agent: "atlas"}, atlas, nil)

	cursitor := byAgent("cursitor", "grep", "read", "todo")
	write("by-agent-custom.json",
		"tools.byAgent for a CUSTOM agent: `dispatchable:false` at the top (S43-extra SD7 — a name on a custom agent's row is not a capability the app can ever exercise) and every row `not_granted`, which is the only honest view of an agent no step of its own will run. What is NOT thrown away is `granted_by`: `grep` still names `handler` and `read` still names `default`+`handler`, because those rows really do name those tools and the agent screen that put them there says so — a view that hid them would contradict the screen that set them. The verdict sentences are gone with the state: an agent that can never be dispatched was never a call the engine could be asked about, and a leftover sentence would read as a decision about something that cannot exist.",
		string(protocol.MethodToolsByAgent), protocol.ToolsByAgentParams{Agent: "cursitor"}, cursitor, nil)

	_, unknownAgentErr := env.svc.ToolsByAgent(ctx, "nobody")
	write("by-agent-unknown.json",
		"tools.byAgent on an agent the roster does not carry: -31912 with the known slugs in the sentence. The code is DISTINCT from -31911 because the two facts have different repairs — a missing tool is a spelling the catalog can nearly correct, a missing agent is a name nobody stored — and a client branches on the number, never the prose. An empty tool list here would be the worst answer this door can give: it reads as \"this agent may call nothing\", which sends someone to write grants for an agent that does not exist.",
		string(protocol.MethodToolsByAgent), protocol.ToolsByAgentParams{Agent: "nobody"}, nil, unknownAgentErr)

	// --- tools.status -------------------------------------------------------------------
	// Two rows out of step with the code, by hand: the ONLY writer of a tool row is the
	// applier, so a drift state cannot be conjured any other way without editing the
	// shipped seed record (which the golden version test would refuse).
	row, err := env.st.ToolGet(ctx, "bash")
	if err != nil {
		t.Fatal(err)
	}
	row.Description = "the row says something the code does not"
	row.Schema = `{"type":"object","properties":{"lie":{"type":"string"}}}`
	row.SchemaHash = strings.Repeat("a", 64)
	tx := beginForTest(t, env.st)
	if err := env.st.ToolUpsertTx(tx, row); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	db, err := env.st.GormDB().DB()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DELETE FROM tool WHERE name = 'grep'`); err != nil {
		t.Fatal(err)
	}
	status, err := env.svc.ToolsStatus(ctx)
	if err != nil {
		t.Fatal(err)
	}
	write("status-ok.json",
		"tools.status in a world where two rows have drifted: `bash` reports `differs` with the FIELD NAMES that disagree (description, schema_hash — a report that said only \"wrong\" would leave the operator guessing), and `grep` reports `missing_row` because its row is gone while the code still implements it. Nothing was repaired: the row stayed tampered with and the tool stayed absent, because TC11's ruling is that drift is SAID and the applier is not a read — silently fixing a disagreement is the one outcome worse than admitting it. `seed` is the engine's own ledger read through the ONE engine `seed.status` reads (same set, same versions, no second stamp), and it says `partial` here for the same two rows — the ledger remembers applying `grep` while the world no longer has it, so the engine's own per-record summary and the drift report agree rather than each holding a private answer (a status panel that said `applied, current` beside a drifted row would be two truths in one screen), and `counts` is one pass over the same world the listing serves, so the two panels cannot disagree about how many tools there are. `drift` is the DISAGREEMENT list — a row that agrees contributes nothing here and still sets `rowCurrent:true` on its entry.",
		string(protocol.MethodToolsStatus), nil, status, nil)

	// --- tools.vocabulary -------------------------------------------------------------
	// Dumped AFTER the drift tamper on purpose: the vocabulary is a statement about
	// the WORDS, and the point of that is that it does not wobble when a row does.
	// `families` is read out of the entries the catalog can render in this world —
	// including `acme`, the one enabled server, and excluding `off`, whose tools no
	// one can call — so a screen that builds a filter select from this answer can
	// only ever offer a value `tools.list` answers for. The capability half is
	// `internal/tools`' closed vocabulary with its own sentences: the reason this
	// door exists is that a screen writing its own sentence for `spawn_process`
	// would be a second vocabulary, free to disagree with the first in silence.
	vocab, err := env.svc.ToolsVocabulary(ctx)
	if err != nil {
		t.Fatalf("tools.vocabulary: %v", err)
	}
	write("vocabulary-ok.json",
		"tools.vocabulary in the same drifted world: the words a catalog screen renders, sent by the code that declares them (TC3). `capabilities` is the CLOSED capability vocabulary with the daemon's own one-line meaning beside each name — the meaning exists because a pill that shows only `spawn_process` asks the person to guess what they are allowing, and it is the daemon's sentence rather than the screen's because two definitions of a vocabulary is the failure this slice was commissioned to end. `families` and `sources` are read out of the entries this world can actually render, which is why `acme` (one enabled server, whose tools are listed under that family) is here and `off` (disabled) is not: an option a filter can never match is a lie in a select. `rerunClasses` is the closed three as `internal/tools` declares them, the same spellings the committed `rerun_classes.json` pins for the rows. Note what did NOT move because two rows drifted: the vocabulary is a fact about the words, and a drift report is a fact about the rows — the two doors answer independently and neither flatters the other.",
		string(protocol.MethodToolsVocabulary), nil, vocab, nil)

	// S47 T10 added these two. The generated UI contract is derived from these
	// fixtures, and a field no fixture shows is INVISIBLE to every generated
	// client — `aliases` and `twin` are answered by the door on every row and no
	// earlier fixture's rows happened to include a tool carrying either, so the
	// screen's row (which the spec asks for, name plus aliases) and its socket-twin
	// line had no type to render. The fix belongs here rather than in a cast at the
	// call edge: the fixture is the contract's source.
	aliased := mustList(t, env, protocol.ToolsListFilter{Q: strptr("write")})
	write("list-alias.json",
		"tools.list with a `q` that reaches `write`, the honest name of the tool the hidden `write_file` alias also addresses: the row carries `aliases`, because the listing is where a person learns that one executor answers to two spellings (SEC-051) — and the alias is never a row of its own (TC11: the same executor rendered twice is the drift this table exists to prevent). `twin` rides on the records-management rows in this same answer: it names the socket method the same executor serves, which is how a screen links a tool to the door a person would use instead of the model.",
		string(protocol.MethodToolsList), protocol.ToolsListFilter{Q: strptr("write")},
		protocol.ToolsListResult{Tools: aliased}, nil)

	twinned := mustList(t, env, protocol.ToolsListFilter{Q: strptr("loop_list")})
	write("list-twin.json",
		"tools.list narrowed to the records-management tool `loop_list`, the row that carries `twin`: the socket method its executor also answers (`loop.list`), paired in the one records catalog rather than remembered beside the tool (TC5's one catalog, no second table). A management tool is therefore TWO doors onto one read — the model's tool and the person's method — and the row says so, which is what makes the drawer's socket-twin line render a fact instead of a guess. The twin is empty for a pure runtime tool, absent from the JSON entirely rather than an empty string pretending to name something.",
		string(protocol.MethodToolsList), protocol.ToolsListFilter{Q: strptr("loop_list")},
		protocol.ToolsListResult{Tools: twinned}, nil)

	// The drawer's twin line needs the same field on the DETAIL type, which is
	// derived from the `tools.get` fixtures rather than the listing's, so the
	// management tool is shown from both doors.
	detailTwin := got("loop_list")
	write("get-twin.json",
		"tools.get on the records-management tool `loop_list`, the detail answer that carries `twin`: the socket method the same executor serves (`loop.list`), so the drawer can name the person's door onto the very read the model has as a tool. The field is the records catalog's pairing (TC5), read from the same world as the row — not a second table of tool-to-method guesses the screen keeps beside it. Every other word in this answer is the same entry the listing sends: the drawer is a deeper read of one row, never a different rendering of the same fact.",
		string(protocol.MethodToolsGet), protocol.ToolsGetParams{Name: "loop_list"}, detailTwin, nil)
}

// newFixtureWorld is the fixture world: a real store, ONLY `tools.core` applied (the
// roster is four agents, not the thirty-three a full startup seeds — trimmed rows,
// never invented ones), the four agents, four tool_access rows spanning deny,
// all-agents allow, ask and a system-authored allow, and the MCP seam.
func newFixtureWorld(t *testing.T) *toolsCatalogEnv {
	t.Helper()
	st, err := store.Open(store.Options{Dir: t.TempDir(), Env: func(string) string { return "" }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	engine, err := NewSeedEngine(st, seed.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Apply(context.Background(), toolseed.SetID, nil,
		seed.Actor{Kind: "system", ID: "fixture"}, seed.PathStartup); err != nil {
		t.Fatalf("apply tools.core: %v", err)
	}

	now := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	for _, a := range []store.Agent{
		{Slug: "atlas", Name: "Atlas", Version: "1", Origin: "system", Tools: `["read","list","write","bash","webfetch"]`},
		{Slug: "sentry", Name: "Sentry", Version: "1", Origin: "system", Tools: `["read","list"]`},
		{Slug: "brief-author", Name: "Brief Author", Version: "1", Origin: "system", Tools: `["read","list","write","bash","todo"]`},
		{Slug: "cursitor", Name: "Cursitor", Version: "1", Origin: "user", Tools: `["read","grep"]`},
	} {
		if err := st.CreateAgentAudited(context.Background(), store.AgentActor{Kind: store.AgentActorUser, ID: "1000"}, a, "2026-11-01T00:00:00Z"); err != nil {
			t.Fatal(err)
		}
		db, err := st.GormDB().DB()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`UPDATE agent SET tools = ?, origin = ? WHERE slug = ?`, a.Tools, a.Origin, a.Slug); err != nil {
			t.Fatal(err)
		}
	}

	pol := policy.NewStore(newPolicyRepo(st))
	addPolicy(t, pol, "deny-bash-atlas", "bash", policy.Deny, policy.AgentKind, "atlas", nil)
	addPolicy(t, pol, "allow-read-all", "read", policy.Allow, policy.AllAgents, "", nil)
	addPolicy(t, pol, "ask-webfetch-atlas", "webfetch", policy.Ask, policy.AgentKind, "atlas", nil)
	// One row authored by the SYSTEM rather than a person, so the fixture set shows
	// `origin` doing its job: the panel says where a rule came from, and an operator
	// who sees a row they never wrote needs to be able to tell that it was authored
	// by a seed and not by a peer.
	pdb, err := st.GormDB().DB()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pdb.Exec(`UPDATE policy SET origin = 'system' WHERE id = ?`, "allow-read-all"); err != nil {
		t.Fatalf("author the row as the system: %v", err)
	}

	admin := &fakeMCPAdmin{}
	admin.servers, admin.grants = mcpFixture()
	svc := newToolsCatalogService(Options{Store: st, Seed: engine, MCPAdmin: admin.fn()})
	if svc == nil {
		t.Fatal("no seam")
	}
	sc := svc.(*toolsCatalogService)
	sc.now = func() time.Time { return now }
	return &toolsCatalogEnv{st: st, svc: svc, admin: admin, policies: pol, now: now}
}

// trimAgents keeps the fixture roster (two dispatchable system agents, one
// first-party, one custom) and drops the rest of a real store's agents. It refuses
// quietly-empty results: a fixture that lost a state would state less than it
// claims.
func trimAgents(t *testing.T, rows []toolcatalog.Entry) []toolcatalog.Entry {
	t.Helper()
	keep := map[string]bool{"atlas": true, "sentry": true, "brief-author": true, "cursitor": true}
	out := []toolcatalog.Entry{}
	for _, e := range rows {
		if keep[e.Agent] {
			out = append(out, e)
		}
	}
	if len(out) != len(keep) {
		t.Fatalf("the roster trimmed to %d of %d rows", len(out), len(keep))
	}
	return out
}
