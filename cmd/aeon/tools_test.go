package main

// tools_test.go — S47 T8: the CLI as a client, tested at the door.
//
// The printer tests speak to a fake door (the `toolsDoorFactory` seam): they
// test what this package is responsible for — layout and the words it adds —
// while every judgement in the rows is a value the door supplied.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/toolcatalog"
)

// fakeToolsDoor is the canned door: it hands back what the daemon would have
// handed back and records what the verb asked for.
type fakeToolsDoor struct {
	listRes   protocol.ToolsListResult
	listRaw   json.RawMessage
	getRes    protocol.ToolsGetResult
	getRaw    json.RawMessage
	agentRes  protocol.ToolsByAgentResult
	agentRaw  json.RawMessage
	statusRes protocol.ToolsStatusResult
	statusRaw json.RawMessage

	listErr   error
	getErr    error
	agentErr  error
	statusErr error

	gotFilter protocol.ToolsListFilter
	gotName   string
	gotAgent  string
	calls     []string
}

func (f *fakeToolsDoor) list(_ context.Context, fl protocol.ToolsListFilter) (protocol.ToolsListResult, json.RawMessage, error) {
	f.calls = append(f.calls, "list")
	f.gotFilter = fl
	if f.listErr != nil {
		return protocol.ToolsListResult{}, nil, f.listErr
	}
	return f.listRes, orRaw(f.listRaw), nil
}

func (f *fakeToolsDoor) get(_ context.Context, name string) (protocol.ToolsGetResult, json.RawMessage, error) {
	f.calls = append(f.calls, "get")
	f.gotName = name
	if f.getErr != nil {
		return protocol.ToolsGetResult{}, nil, f.getErr
	}
	return f.getRes, orRaw(f.getRaw), nil
}

func (f *fakeToolsDoor) agent(_ context.Context, agent string) (protocol.ToolsByAgentResult, json.RawMessage, error) {
	f.calls = append(f.calls, "agent")
	f.gotAgent = agent
	if f.agentErr != nil {
		return protocol.ToolsByAgentResult{}, nil, f.agentErr
	}
	return f.agentRes, orRaw(f.agentRaw), nil
}

func (f *fakeToolsDoor) status(_ context.Context) (protocol.ToolsStatusResult, json.RawMessage, error) {
	f.calls = append(f.calls, "status")
	if f.statusErr != nil {
		return protocol.ToolsStatusResult{}, nil, f.statusErr
	}
	return f.statusRes, orRaw(f.statusRaw), nil
}

// orRaw keeps a canned door from printing nothing at all when a test did not
// bother to supply bytes (the typed result is what the assertions read).
func orRaw(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage(`{}`)
	}
	return raw
}

// useDoor points the verbs at a fake for the duration of one test.
func useDoor(t *testing.T, d toolsDoor) {
	t.Helper()
	prev := toolsDoorFactory
	toolsDoorFactory = func() (toolsDoor, error) { return d, nil }
	t.Cleanup(func() { toolsDoorFactory = prev })
}

func useSurface(t *testing.T, d surfaceDoor) {
	t.Helper()
	prev := surfaceDoorFactory
	surfaceDoorFactory = func() (surfaceDoor, error) { return d, nil }
	t.Cleanup(func() { surfaceDoorFactory = prev })
}

// fakeSurfaceDoor is the canned surface door.
type fakeSurfaceDoor struct {
	listRes  protocol.SurfaceListResult
	listRaw  json.RawMessage
	getRes   protocol.SurfaceGetResult
	getRaw   json.RawMessage
	listErr  error
	getErr   error
	gotF     protocol.SurfaceListFilter
	gotParam string
}

func (f *fakeSurfaceDoor) list(_ context.Context, fl protocol.SurfaceListFilter) (protocol.SurfaceListResult, json.RawMessage, error) {
	f.gotF = fl
	if f.listErr != nil {
		return protocol.SurfaceListResult{}, nil, f.listErr
	}
	return f.listRes, orRaw(f.listRaw), nil
}

func (f *fakeSurfaceDoor) get(_ context.Context, method string) (protocol.SurfaceGetResult, json.RawMessage, error) {
	f.gotParam = method
	if f.getErr != nil {
		return protocol.SurfaceGetResult{}, nil, f.getErr
	}
	return f.getRes, orRaw(f.getRaw), nil
}

// The five states, named through the engine's own vocabulary rather than a
// string the test invented: a printer keyed to a typo proves nothing.
func stAllowed() toolcatalog.State    { return toolcatalog.StateGrantedAllowed }
func stDenied() toolcatalog.State     { return toolcatalog.StateGrantedDenied }
func stAsk() toolcatalog.State        { return toolcatalog.StateGrantedAsk }
func stNoRule() toolcatalog.State     { return toolcatalog.StateGrantedNoRule }
func stNotGranted() toolcatalog.State { return toolcatalog.StateNotGranted }

// --- the verbs exist and name themselves -----------------------------------

func TestToolsNoVerbNamesTheVerbs(t *testing.T) {
	var out bytes.Buffer
	err := cmdTools(&out, nil)
	if err == nil {
		t.Fatal("`aeon tools` with no verb succeeded — a bare verb is a usage refusal, not a listing")
	}
	for _, want := range []string{"list", "show", "agent", "status"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the no-verb refusal does not name %q: %v", want, err)
		}
	}
	if !strings.Contains(out.String(), "aeon tools") {
		t.Errorf("the no-verb path printed no usage: %q", out.String())
	}
}

func TestToolsUnknownVerbNamesTheVerbs(t *testing.T) {
	var out bytes.Buffer
	err := cmdTools(&out, []string{"frobnicate"})
	if err == nil || !strings.Contains(err.Error(), "frobnicate") {
		t.Fatalf("an unknown verb must be refused naming itself: %v", err)
	}
	for _, want := range []string{"list", "show", "agent", "status"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the unknown-verb refusal does not name %q: %v", want, err)
		}
	}
}

func TestSurfaceNoVerbNamesTheVerbs(t *testing.T) {
	var out bytes.Buffer
	err := cmdSurface(&out, nil)
	if err == nil {
		t.Fatal("`aeon surface` with no verb succeeded")
	}
	if !strings.Contains(err.Error(), "list") || !strings.Contains(err.Error(), "show") {
		t.Errorf("the refusal does not name the two verbs: %v", err)
	}
	if !strings.Contains(out.String(), "aeon surface") {
		t.Errorf("the no-verb path printed no usage: %q", out.String())
	}
}

// --- the P34 parity, both directions ----------------------------------------

// toolsVerbCase is one verb, the method it speaks, and the flags that must be
// able to TRAVEL. A verb whose flag is silently dropped is the failure this
// table exists to catch (policy's cliVerbParityTable is the precedent).
type toolsVerbCase struct {
	verb   string
	method protocol.MethodName
	flags  []string
	// cliOnly lists the flags that never travel because they are presentation.
	cliOnly []string
}

var toolsVerbTable = []toolsVerbCase{
	{verb: "tools list", method: protocol.MethodToolsList, flags: []string{"family", "source", "capability", "available", "q"}, cliOnly: []string{"json"}},
	{verb: "tools show", method: protocol.MethodToolsGet, cliOnly: []string{"json"}},
	{verb: "tools agent", method: protocol.MethodToolsByAgent, cliOnly: []string{"json"}},
	{verb: "tools status", method: protocol.MethodToolsStatus, cliOnly: []string{"json"}},
	{verb: "surface list", method: protocol.MethodSurfaceList, flags: []string{"kind", "door", "human-door-only", "q"}, cliOnly: []string{"json"}},
	{verb: "surface show", method: protocol.MethodSurfaceGet, cliOnly: []string{"json"}},
}

// toolsExemptMethods names the tools.* methods that deliberately have NO CLI
// verb, each with the reason — the `socketOnlyPolicyDoors` escape hatch.
var toolsExemptMethods = map[string]string{
	string(protocol.MethodToolsMineList): "the own-view read answers the calling agent; a CLI call is a person's connection",
	string(protocol.MethodToolsMineShow): "the own-view read answers the calling agent; a CLI call is a person's connection",
	// S34's grants read predates S47 and answers the SAME self-scoped question
	// (`toolcatalog.ForAgent` over the calling agent), so it shares the two
	// own-view reads' reason. S47 adds no verb for it, and no verb would: the
	// person's door onto "what may this agent call" is `aeon tools agent`.
	"tools.grants": "S34's agent-facing grants read answers the calling agent; the person's view is `aeon tools agent`",
	// S47 T10's vocabulary door is the SCREEN's need rather than the terminal's:
	// a select has to be built from data it may not author, while a person at a
	// CLI already types the filter words themselves and sees every class printed
	// on the row beside them. P34 is satisfied by the method existing and the app
	// being its first consumer; a `tools vocabulary` verb would be a second
	// consumer nobody asked for, printing a vocabulary at a terminal that reads
	// it back as free text.
	// S74 T2: the policy form's live preview. A person at a terminal can read the
	// addresses off `aeon tools list` and does not preview a pattern before typing it.
	string(protocol.MethodToolsMatchPattern): "the policy form's live preview of a pattern; at a terminal the address column of `aeon tools list` shows what a pattern would name",
	string(protocol.MethodToolsVocabulary): "the vocabulary is what a screen builds its filter selects and capability pills from; the CLI's filters are free-text flags and every row already prints its own class and capabilities",
}

func TestEveryToolsVerbHasASocketMethod(t *testing.T) {
	tbl := toolsRoadTable(t)
	for _, c := range toolsVerbTable {
		if _, ok := tbl.Lookup(c.method); !ok {
			t.Errorf("%s speaks %s, which a fully wired daemon does not register — a verb with no method behind it is not finished (P34)", c.verb, c.method)
		}
	}
}

func TestEverySocketToolsMethodHasAVerb(t *testing.T) {
	tbl := toolsRoadTable(t)
	spoken := map[protocol.MethodName]bool{}
	for _, c := range toolsVerbTable {
		spoken[c.method] = true
	}
	seen := 0
	for _, m := range tbl.Methods() {
		name := string(m)
		if !strings.HasPrefix(name, "tools.") {
			continue
		}
		seen++
		if spoken[m] {
			continue
		}
		if _, ok := toolsExemptMethods[name]; ok {
			continue
		}
		t.Errorf("the daemon registers %s and no CLI verb speaks it — either the verb is missing or the method belongs on the exempt list with its reason", name)
	}
	toolsVerbs := 0
	for _, c := range toolsVerbTable {
		if strings.HasPrefix(c.verb, "tools ") {
			toolsVerbs++
		}
	}
	if seen != toolsVerbs+len(toolsExemptMethods) {
		t.Errorf("the tools.* surface is not the %d verbs plus %d named exemptions the CLI claims: %d registered",
			toolsVerbs, len(toolsExemptMethods), seen)
	}
}

func TestToolsExemptMethodsStayNamedAndReasoned(t *testing.T) {
	// Four exemptions, each named and reasoned below. The list is amended by NAME
	// with a sentence, never by loosening the check (S55's precedent): the fourth
	// arrived with S47 T10's `tools.vocabulary`, the vocabulary door whose first
	// consumer is the Settings screen rather than a terminal. The fifth is S74
	// T2's `tools.matchPattern`, the policy form's live preview.
	if len(toolsExemptMethods) != 5 {
		t.Fatalf("the exempt list changed shape: %v", toolsExemptMethods)
	}
	for name, reason := range toolsExemptMethods {
		if strings.TrimSpace(reason) == "" {
			t.Errorf("%s is exempt with no reason", name)
		}
		if !strings.HasPrefix(name, "tools.mine.") && name != "tools.grants" && name != string(protocol.MethodToolsVocabulary) && name != string(protocol.MethodToolsMatchPattern) {
			t.Errorf("%s should not be exempt: it is neither an own-view read, nor S34's agent-facing grants read, nor T10's vocabulary door, nor S74's pattern preview", name)
		}
	}
}

// --- the printers -----------------------------------------------------------

func TestToolsListPrintsCapabilitiesAndFlags(t *testing.T) {
	door := &fakeToolsDoor{listRes: protocol.ToolsListResult{Tools: []protocol.ToolEntry{
		{Name: "read", Family: "workspace", Source: "system", RerunClass: "safe", Capabilities: []string{"workspace_read"}, RowCurrent: true, Available: true},
		{Name: "browser_navigate", Family: "browser", Source: "system", RerunClass: "detectable", Capabilities: []string{"browser_navigate"}, Conditional: true, Available: false, RowCurrent: true},
		{Name: "secret.tool", Family: "mcp", Source: "mcp", RerunClass: "safe", Server: "secret", Tier: "public", Classified: false, Available: true},
	}}}
	useDoor(t, door)
	var out bytes.Buffer
	if err := cmdTools(&out, []string{"list"}); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{
		"read · workspace · system · safe",
		"capabilities: workspace_read",
		"UNWIRED",
		"UNCLASSIFIED",
		"row stale",
		"server secret",
		"tier public",
		"3 tools",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the listing does not print %q:\n%s", want, got)
		}
	}
	if !strings.Contains(got, "browser_navigate") || !strings.Contains(got, "secret.tool") {
		t.Errorf("a tool the answer holds went missing:\n%s", got)
	}
}

func TestToolsListPrintsTheDaemonsEmptyStatement(t *testing.T) {
	door := &fakeToolsDoor{listRes: protocol.ToolsListResult{Tools: []protocol.ToolEntry{}, Statement: protocol.ToolsListNoMatchStatement}}
	useDoor(t, door)
	var out bytes.Buffer
	if err := cmdTools(&out, []string{"list", "--family", "nope"}); err != nil {
		t.Fatal(err)
	}
	if out.String() != protocol.ToolsListNoMatchStatement+"\n" {
		t.Errorf("the filtered-empty answer printed %q, want the daemon's sentence verbatim", out.String())
	}
	if door.gotFilter.Family == nil || *door.gotFilter.Family != "nope" {
		t.Errorf("the --family flag did not travel to the door: %+v", door.gotFilter)
	}
}

func TestToolsListFlagsTravelToTheFilter(t *testing.T) {
	var out bytes.Buffer
	door := &fakeToolsDoor{}
	useDoor(t, door)
	if err := cmdTools(&out, []string{"list", "--family", "workspace", "--source", "mcp", "--capability", "workspace_read", "--available=false", "--q", "sea"}); err != nil {
		t.Fatal(err)
	}
	f := door.gotFilter
	if f.Family == nil || *f.Family != "workspace" || f.Source == nil || *f.Source != "mcp" ||
		f.Capability == nil || *f.Capability != "workspace_read" || f.Available == nil || *f.Available ||
		f.Q == nil || *f.Q != "sea" {
		t.Errorf("a flag was dropped on the way to the wire: %+v", f)
	}
	door2 := &fakeToolsDoor{}
	useDoor(t, door2)
	if err := cmdTools(&out, []string{"list"}); err != nil {
		t.Fatal(err)
	}
	if door2.gotFilter != (protocol.ToolsListFilter{}) {
		t.Errorf("an unfiltered listing sent a filter anyway: %+v", door2.gotFilter)
	}
}

func TestToolsShowPrintsPerAgentReasonsVerbatim(t *testing.T) {
	reason1 := "policy 41 (tool_access) allows this tool for this agent — the row grants it and no refusal overrides it"
	reason2 := "the tool_access row for this agent names a different set, and the default is deny — no policy grants this tool to this agent"
	door := &fakeToolsDoor{getRes: protocol.ToolsGetResult{
		Tool: protocol.ToolEntry{
			Name: "read", Family: "workspace", Source: "system", Description: "Read a file.",
			RerunClass: "safe", Capabilities: []string{"workspace_read"}, Governed: []string{"tool_access"},
			Schema: json.RawMessage(`{"type":"object","required":["path"]}`), SchemaHash: "abc123",
			Available: true, RowCurrent: true, Version: 3, Origin: "system",
		},
		Agents: []protocol.AgentGrantRow{
			{Tool: "read", Agent: "atlas", State: stAllowed(), Reason: reason1, GrantedBy: []string{"grant:9"}, Dispatchable: true},
			{Tool: "read", Agent: "sentry", State: stDenied(), Reason: reason2, Dispatchable: true},
			{Tool: "read", Agent: "ghost", State: stAsk(), Reason: "the row asks instead of deciding", Dispatchable: false},
		},
		Policies: []protocol.PolicyRef{{ID: "41", Action: "tool_access", Target: "agent:atlas", Describe: "allows read for atlas"}},
	}}
	useDoor(t, door)
	var out bytes.Buffer
	if err := cmdTools(&out, []string{"show", "read"}); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{reason1, reason2, "the row asks instead of deciding", "atlas", "sentry", "not dispatchable", "granted by grant:9", "governed by: tool_access", "allows read for atlas", "41", `"required"`} {
		if !strings.Contains(got, want) {
			t.Errorf("the show output lost %q:\n%s", want, got)
		}
	}
	if door.gotName != "read" {
		t.Errorf("the name did not travel: %q", door.gotName)
	}
}

func TestToolsShowRefusalTravelsUntouched(t *testing.T) {
	want := `the tool named "reed" does not exist — the nearest names are: read, redo, grep`
	door := &fakeToolsDoor{getErr: errors.New(want)}
	useDoor(t, door)
	var out bytes.Buffer
	err := cmdTools(&out, []string{"show", "reed"})
	if err == nil {
		t.Fatal("an unknown tool was not refused")
	}
	if !strings.Contains(err.Error(), want) {
		t.Errorf("the refusal was reworded: %v", err)
	}
	if out.Len() != 0 {
		t.Errorf("a refusal printed output as well: %q", out.String())
	}
}

func TestToolsShowNeedsAName(t *testing.T) {
	useDoor(t, &fakeToolsDoor{})
	var out bytes.Buffer
	err := cmdTools(&out, []string{"show"})
	if err == nil || !strings.Contains(err.Error(), "needs a tool name") {
		t.Fatalf("`tools show` without a name should refuse: %v", err)
	}
}

func TestToolsAgentUnknownPrintsKnownAgents(t *testing.T) {
	want := `refused: the agent "atlss" is unknown — the agents this store holds are: atlas, sentry, worker`
	door := &fakeToolsDoor{agentErr: errors.New(want)}
	useDoor(t, door)
	var out bytes.Buffer
	err := cmdTools(&out, []string{"agent", "atlss"})
	if err == nil {
		t.Fatal("an unknown agent was not refused")
	}
	if !strings.Contains(err.Error(), "atlas, sentry, worker") || !strings.Contains(err.Error(), "atlss") {
		t.Errorf("the refusal lost the known-agent names or the misspelling: %v", err)
	}
	if out.Len() != 0 {
		t.Errorf("an unknown agent printed a view anyway: %q", out.String())
	}
}

func TestToolsAgentGroupsByStateAndPrintsUndispatchable(t *testing.T) {
	var out bytes.Buffer
	door := &fakeToolsDoor{agentRes: protocol.ToolsByAgentResult{
		Agent:        "atlas",
		Dispatchable: true,
		Tools: []protocol.AgentGrantRow{
			{Tool: "grep", Agent: "atlas", State: stAllowed(), Reason: "the row grants it", Dispatchable: true},
			{Tool: "write", Agent: "atlas", State: stDenied(), Reason: "the gate refuses it", Dispatchable: true},
			{Tool: "ask_agent", Agent: "atlas", State: stNoRule(), Dispatchable: true},
			{Tool: "read", Agent: "atlas", State: stNotGranted(), Dispatchable: true},
			{Tool: "peek", Agent: "atlas", State: stAsk(), Reason: "the row asks instead of deciding", Dispatchable: true},
		},
	}}
	useDoor(t, door)
	if err := cmdTools(&out, []string{"agent", "atlas"}); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{"granted_allowed (1)", "granted_ask (1)", "granted_denied (1)", "granted_no_rule (1)", "not_granted (1)", "the row grants it", "the gate refuses it", "dispatchable: yes"} {
		if !strings.Contains(got, want) {
			t.Errorf("the agent view does not print %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "no tools in the answer") {
		t.Errorf("a populated view printed the empty answer:\n%s", got)
	}

	door2 := &fakeToolsDoor{agentRes: protocol.ToolsByAgentResult{
		Agent: "ghost", Dispatchable: false,
		Tools: []protocol.AgentGrantRow{{Tool: "grep", Agent: "ghost", State: stAllowed(), Dispatchable: false}},
	}}
	useDoor(t, door2)
	var out2 bytes.Buffer
	if err := cmdTools(&out2, []string{"agent", "ghost"}); err != nil {
		t.Fatal(err)
	}
	got2 := out2.String()
	if !strings.Contains(got2, "dispatchable: no") {
		t.Errorf("a non-dispatchable agent did not say so:\n%s", got2)
	}
	if strings.Contains(got2, "grep") {
		t.Errorf("a non-dispatchable agent got a grant table anyway:\n%s", got2)
	}
}

func TestToolsAgentNeedsAnAgent(t *testing.T) {
	useDoor(t, &fakeToolsDoor{})
	var out bytes.Buffer
	err := cmdTools(&out, []string{"agent"})
	if err == nil || !strings.Contains(err.Error(), "needs an agent slug") {
		t.Fatalf("`tools agent` without a slug should refuse: %v", err)
	}
}

func TestToolsStatusPrintsVersionsAndDriftFields(t *testing.T) {
	door := &fakeToolsDoor{statusRes: protocol.ToolsStatusResult{
		Seed:   protocol.ToolsSeedState{Set: "tools.core", AvailableVersion: 4, AppliedVersion: 3, State: "diverged"},
		Drift:  []protocol.ToolDriftEntry{{Name: "read", State: "differs", Fields: []string{"description", "schema_hash"}}, {Name: "grep", State: "missing_row"}},
		Counts: protocol.ToolsCounts{System: 70, MCP: 2, ConditionalUnwired: 2},
	}}
	useDoor(t, door)
	var out bytes.Buffer
	if err := cmdTools(&out, []string{"status"}); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{"available v4", "applied v3", "tools.core", "diverged", "70 system", "2 mcp", "read · differs", "description, schema_hash", "grep · missing_row"} {
		if !strings.Contains(got, want) {
			t.Errorf("the status output does not print %q:\n%s", want, got)
		}
	}
}

func TestToolsStatusCleanSaysSo(t *testing.T) {
	door := &fakeToolsDoor{statusRes: protocol.ToolsStatusResult{
		Seed:   protocol.ToolsSeedState{Set: "tools.core", AvailableVersion: 4, AppliedVersion: 4, State: "applied"},
		Counts: protocol.ToolsCounts{System: 70},
	}}
	useDoor(t, door)
	var out bytes.Buffer
	if err := cmdTools(&out, []string{"status"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "no drift") {
		t.Errorf("a clean drift report did not say so:\n%s", out.String())
	}
}

// --- surface ----------------------------------------------------------------

func TestSurfaceListPrintsDoorsAndHumanDoorMarker(t *testing.T) {
	door := &fakeSurfaceDoor{listRes: protocol.SurfaceListResult{
		Methods: []protocol.SurfaceEntry{
			{Method: "tools.list", Kind: protocol.SurfaceRead, Doors: protocol.SurfaceDoors{Socket: true, Public: true, Narrow: "none", ToolTwin: "tools_list"}},
			{Method: "agent.create", Kind: protocol.SurfaceWrite, HumanDoorOnly: true, Doors: protocol.SurfaceDoors{Socket: true, Narrow: "unknown"}},
			{Method: "tool.call", Kind: protocol.SurfaceControl, Doors: protocol.SurfaceDoors{Socket: true, Public: true, Narrow: "call_tool", AgentService: true}},
		},
		Counts: protocol.SurfaceCounts{Total: 3, Unfiltered: 271, ByKind: map[protocol.SurfaceKind]int{protocol.SurfaceRead: 1, protocol.SurfaceWrite: 1, protocol.SurfaceControl: 1}},
	}}
	useSurface(t, door)
	var out bytes.Buffer
	if err := cmdSurface(&out, []string{"list"}); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{
		"tools.list · read", "person public", "tool:tools_list",
		"HUMAN-DOOR-ONLY", "narrow:unknown", "narrow:call_tool", "agent-service",
		"3 of 271 methods",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the surface listing does not print %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "narrow:none") {
		t.Errorf("a method with no narrow door printed one:\n%s", got)
	}
}

func TestSurfaceListFilteredEmptyPrintsTheSentence(t *testing.T) {
	door := &fakeSurfaceDoor{listRes: protocol.SurfaceListResult{
		Statement: "no methods match this filter.",
		Counts:    protocol.SurfaceCounts{Total: 0, Unfiltered: 271},
	}}
	useSurface(t, door)
	var out bytes.Buffer
	if err := cmdSurface(&out, []string{"list", "--kind", "nonsense"}); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out.String()) != "no methods match this filter." {
		t.Errorf("a filtered-empty listing printed %q instead of the daemon's sentence", out.String())
	}
	if door.gotF.Kind == nil || *door.gotF.Kind != "nonsense" {
		t.Errorf("--kind did not travel: %+v", door.gotF)
	}
}

func TestSurfaceShowNeedsAMethod(t *testing.T) {
	useSurface(t, &fakeSurfaceDoor{})
	var out bytes.Buffer
	err := cmdSurface(&out, []string{"show"})
	if err == nil || !strings.Contains(err.Error(), "needs a method") {
		t.Fatalf("`surface show` without a name should refuse: %v", err)
	}
}
