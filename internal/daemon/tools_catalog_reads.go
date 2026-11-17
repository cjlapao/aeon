// The four doors (S47 T5): `tools.list`, `tools.get`, `tools.byAgent`,
// `tools.status`. Each is one read of `world(ctx)` and then pure assembly;
// a refused read is an error (V11), and the status drift list is the
// DISAGREEMENT list, reported and never repaired (TC11).

package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/cjlapao/aeon/internal/policy"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/seed"
	"github.com/cjlapao/aeon/internal/toolcatalog"
	"github.com/cjlapao/aeon/internal/tools"
	"github.com/cjlapao/aeon/internal/toolseed"
)

// --- `tools.list` -----------------------------------------------------------------

// ToolsList answers `tools.list` with the matching done HERE (TC10: daemon-side
// filtering). A client that filtered locally would fetch the whole catalog on
// every keystroke and own a private copy of the matching rules that the catalog is
// free to change.
//
// An empty result is not an error, and it is not the same fact twice: the
// filtered-empty case gets the table's sentence (added by the door, the only layer
// that saw whether a filter was supplied) and the unfiltered-empty case gets none,
// because that would claim the build contains no tools at all.
func (sc *toolsCatalogService) ToolsList(ctx context.Context, f protocol.ToolsListFilter) ([]protocol.ToolEntry, error) {
	w, err := sc.world(ctx)
	if err != nil {
		return nil, err
	}
	all := w.entries()
	out := make([]protocol.ToolEntry, 0, len(all))
	for _, e := range all {
		if matchesToolFilter(e, f) {
			out = append(out, e)
		}
	}
	if err := checkView(w); err != nil {
		return nil, err
	}
	return out, nil
}

// matchesToolFilter applies the five optional filters. A nil field is "no
// opinion", never "match the empty string" — the three states (absent, present,
// present-and-empty) are why `ToolsListFilter`'s fields are pointers. `q` is a
// case-insensitive substring over the name and the description; for an MCP tool
// that is its qualified name, which is the only text that authority holds.
func matchesToolFilter(e protocol.ToolEntry, f protocol.ToolsListFilter) bool {
	if f.Family != nil && !strings.EqualFold(*f.Family, e.Family) {
		return false
	}
	if f.Source != nil && *f.Source != e.Source {
		return false
	}
	if f.Capability != nil {
		found := false
		for _, c := range e.Capabilities {
			if strings.EqualFold(*f.Capability, c) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	if f.Access != nil && *f.Access != e.Access {
		return false
	}
	if f.Available != nil && *f.Available != e.Available {
		return false
	}
	if f.Q != nil {
		q := strings.ToLower(strings.TrimSpace(*f.Q))
		if q == "" {
			return false // a present-and-empty q matches nothing: the empty filter's own answer
		}
		if !strings.Contains(strings.ToLower(e.Name), q) &&
			!strings.Contains(strings.ToLower(e.Description), q) {
			return false
		}
	}
	return true
}

// --- `tools.get` -------------------------------------------------------------------

// ToolsGet answers `tools.get`: the tool read in full, every agent that EXISTS
// answered for it, and the rules that speak about it.
//
// The agents table is the whole roster, granted or not. A table listing only the
// granted agents would let a missing row carry the meaning "denied", which inverts
// the fact: TC5's question is who can call each tool and why NOT, and the "why
// not" for an agent that holds nothing is the grant chain, not a rule.
func (sc *toolsCatalogService) ToolsGet(ctx context.Context, name string) (protocol.ToolsGetResult, error) {
	w, err := sc.world(ctx)
	if err != nil {
		return protocol.ToolsGetResult{}, err
	}
	name = strings.TrimSpace(name)
	all := w.entries()
	var entry *protocol.ToolEntry
	for i := range all {
		if toolEntryMatches(all[i], name) {
			entry = &all[i]
			break
		}
	}
	if entry == nil {
		// The typed refusal, from the seam: only this layer sees the registry and
		// the rows, so only it can name the near ones (and the door ships the
		// `*Error` untouched rather than paraphrasing the fact).
		return protocol.ToolsGetResult{}, protocol.ErrToolUnknown.WithWhat(
			"the tools.get read named %q, which is neither a tool this build implements nor a declared MCP tool; the nearest names are: %s",
			name, nearestToolNames(name, entryNames(all)))
	}

	res := protocol.ToolsGetResult{
		Tool:     *entry,
		Agents:   toolcatalog.ForTool(entry.Name, w.agentSlugs(), w.inputs),
		Policies: w.policyRefs(entry.Name),
	}
	if res.Agents == nil {
		res.Agents = []toolcatalog.Entry{}
	}
	if res.Policies == nil {
		res.Policies = []protocol.PolicyRef{}
	}
	if toolcatalog.IsMCPToolName(entry.Name) {
		res.MCP = &protocol.ToolMCPLink{
			Server: entry.Server,
			Tier:   entry.Tier,
			Link:   protocol.Link{Kind: "mcp_server", ID: entry.Server},
		}
	}
	if err := checkView(w); err != nil {
		return protocol.ToolsGetResult{}, err
	}
	return res, nil
}

// toolEntryMatches resolves a name the way the runtime does before anything else,
// through `tools.CanonicalToolName`, so the hidden `write_file` spelling answers
// `write` — the same resolution the grant chain and the policy rows share
// (SEC-051/SEC-059), and the reason an alias is never its own entry. An MCP name
// is matched exactly: the qualified spelling IS its identity, and no
// canonicalisation may rewrite `reddit.search` into `search`.
func toolEntryMatches(e protocol.ToolEntry, name string) bool {
	if name == "" {
		return false
	}
	if toolcatalog.IsMCPToolName(name) || toolcatalog.IsMCPToolName(e.Name) {
		return e.Name == name
	}
	return e.Name == tools.CanonicalToolName(name)
}

// policyRefs renders the `tool_access` rows that can speak about this tool (TC10's
// "the policy rows that touch it"). `toolcatalog.PoliciesTouching` selects them —
// canonical name, so a row written `write_file` is found for `write`, and only the
// target kinds that can govern an agent — and `policy.Describe` says what each one
// does (S45's one describer, verbatim: the same sentence `policy.list` prints and
// the CLI shows, because a second formatter here would be two authorities over what
// a rule means).
//
// `Expired` is shown and not hidden: a lapsed row is a fact about what was once
// decided, and a panel that omits it tells someone their tool is unprotected while
// a rule they cannot see still outranks the moment it is re-dated.
func (w *catalogWorld) policyRefs(tool string) []protocol.PolicyRef {
	rows := toolcatalog.PoliciesTouching(tool, w.policies)
	out := make([]protocol.PolicyRef, 0, len(rows))
	for _, p := range rows {
		toolName := ""
		var spec struct {
			ToolName string `json:"toolName"`
		}
		if json.Unmarshal(p.Spec, &spec) == nil {
			toolName = spec.ToolName
		}
		expired := false
		if p.NotAfter != nil {
			expired = !p.NotAfter.After(w.at)
		}
		out = append(out, protocol.PolicyRef{
			ID:       p.ID,
			Action:   string(p.Action),
			Source:   string(p.Source),
			Origin:   w.origin[p.ID],
			Target:   p.Target.String(),
			ToolName: toolName,
			Describe: policy.Describe(p),
			Expired:  expired,
			Link:     protocol.Link{Kind: "policy", ID: p.ID},
		})
	}
	return out
}

// --- `tools.byAgent` -----------------------------------------------------------------

// ToolsByAgent answers the person's question about one subject: this agent's whole
// tool view — every tool that exists, with its state and the engine's verbatim
// reason. The subject is a name the person chose; the caller is the peer at the
// endpoint and is never a param, which is what keeps this the human door and
// `tools_mine` (T7) the agent's own.
//
// An unknown agent is the typed refusal naming the known agents. An empty view
// would be the worse answer: it reads as "that agent may call nothing" about an
// agent that does not exist, and the person then debugs a grant that was never
// missing.
//
// The universe is the code's names (the honest registry names plus the conditionals,
// so an unwired `search` still appears, answered `not_granted` with the grant chain
// as its whole story) plus the qualified names THIS agent's own view shows (MC2), so
// the door cannot be used to enumerate a peer's private servers: the answer is every
// tool that exists FOR THIS AGENT — for a custom agent, the system half plus nothing.
// `dispatchable:false` says why once, at the top, so no panel can render a green
// matrix for an agent no step of its own can run (SD7); the `GrantedBy` lists still
// ride on the rows, because the agent's row does name those tools and hiding that
// would contradict the agent screen that put them there.
func (sc *toolsCatalogService) ToolsByAgent(ctx context.Context, agent string) (protocol.ToolsByAgentResult, error) {
	w, err := sc.world(ctx)
	if err != nil {
		return protocol.ToolsByAgentResult{}, err
	}
	agent = strings.TrimSpace(agent)
	row, found := w.lookupAgent(agent)
	if !found {
		return protocol.ToolsByAgentResult{}, protocol.ErrToolAgentUnknown.WithWhat(
			"the tools.byAgent read named %q, which is not an agent this store carries; the known agents are: %s",
			agent, quotedNames(w.agentSlugs()))
	}

	// The same construction the agent's own view uses, reached by calling it rather
	// than by repeating it — the comment beside `heldNames` claims the two are one
	// construction, and an inlined copy is how that claim goes stale (the S47 T11
	// walk found exactly that: the copy had no dedupe, so this door answered 146
	// rows for 73 tools).
	entries := toolcatalog.SortedForRendering(toolcatalog.ForAgent(row.Slug, w.heldNames(row.Slug), w.inputs))
	res := protocol.ToolsByAgentResult{
		Agent:        row.Slug,
		Dispatchable: true, // a person's own agent is dispatched like any other: its rules are what gate it
		Tools:        entries,
	}
	if res.Tools == nil {
		res.Tools = []toolcatalog.Entry{}
	}
	if err := checkView(w); err != nil {
		return protocol.ToolsByAgentResult{}, err
	}
	return res, nil
}

// --- `tools.status` ---------------------------------------------------------------

// ToolsStatus answers `tools.status`: what the code offers, what the ledger says
// was applied, every drift verdict, and the shape of the catalog.
//
// It writes NOTHING and repairs NOTHING (TC11). The applier is never called from
// here — a catalog that could fix a row would eventually "fix" one nobody agreed
// to, and the disagreement that `differs` exists to surface would be gone before
// anyone read it. The golden test `TestToolVersionsBumpWithContent` is what refuses
// a content change without a version bump at `go test` time; this door is where a
// person SEES the same fact about a running database.
//
// An empty drift list is the healthy case. It carries no statement, because nothing
// was filtered: the absence of drift here is the result of a check that ran, and
// `tools.list`'s sentence discipline (filtered-empty versus empty-table) does not
// apply to a report that has no filter to begin with.
func (sc *toolsCatalogService) ToolsStatus(ctx context.Context) (protocol.ToolsStatusResult, error) {
	w, err := sc.world(ctx)
	if err != nil {
		return protocol.ToolsStatusResult{}, err
	}
	sys := w.systemEntries()
	mcp := w.mcpEntries()

	unwired := 0
	for _, e := range sys {
		if e.Conditional && !e.Available {
			unwired++
		}
	}

	// The drift list is the DISAGREEMENT list, not a roster: `toolseed.Drift` answers
	// for every tool including the ones that agree, and a panel that had to filter
	// 70 `current` rows to find the one real disagreement would learn to ignore the
	// field. `current` entries still did their work — they set `rowCurrent` on each
	// entry — and the empty list here means "checked, nothing to say", which the
	// protocol test pins as `[]` and never null.
	names := make([]string, 0, len(w.drift))
	for n := range w.drift {
		if w.drift[n].State == toolseed.DriftCurrent {
			continue
		}
		names = append(names, n)
	}
	sort.Strings(names)
	drift := make([]protocol.ToolDriftEntry, 0, len(names))
	for _, n := range names {
		d := w.drift[n]
		drift = append(drift, protocol.ToolDriftEntry{Name: d.Name, State: string(d.State), Fields: d.Fields})
	}

	seedState, err := sc.seedState(ctx)
	if err != nil {
		return protocol.ToolsStatusResult{}, err
	}
	return protocol.ToolsStatusResult{
		Seed:  seedState,
		Drift: drift,
		Counts: protocol.ToolsCounts{
			System:             len(sys),
			MCP:                len(mcp),
			ConditionalUnwired: unwired,
		},
	}, nil
}

// ToolsSeedStateUnavailable is the seed half's honest third state: no seed engine is
// wired in this build, so the ledger was never read. `appliedVersion` 0 then means
// NOT READ and not "never applied" — a different process may well have applied the
// set, and a reader that collapsed the two would report a healthy database as an
// unseeded one (P31: three values where verification can be unavailable).
const ToolsSeedStateUnavailable = "unavailable"

// seedState reads the `tools.core` set through the ONE engine the daemon holds —
// the same value `seed.status` reads (P34: one engine, one truth; the catalog keeps
// no second ledger of what it seeded, which is TC11's note 5).
func (sc *toolsCatalogService) seedState(ctx context.Context) (protocol.ToolsSeedState, error) {
	st := protocol.ToolsSeedState{
		Set:              toolseed.SetID,
		AvailableVersion: toolseed.SetVersion(),
		State:            ToolsSeedStateUnavailable,
	}
	if sc.engine == nil {
		return st, nil
	}
	statuses, err := sc.engine.Status(ctx, toolseed.SetID)
	if err != nil {
		return protocol.ToolsSeedState{}, fmt.Errorf("tools catalog: the seed ledger could not be read: %w", err)
	}
	for _, s := range statuses {
		if s.ID != toolseed.SetID {
			continue
		}
		st.AppliedVersion = s.AppliedVersion
		if s.AvailableVersion > 0 {
			st.AvailableVersion = s.AvailableVersion
		}
		st.State = summarizeSet(s)
		return st, nil
	}
	// The engine knows no such set: no applier is registered, so the ledger cannot
	// be read. Same word, same reason — and not "never_applied", which would be a
	// claim about a history nobody looked at.
	return st, nil
}

// summarizeSet groups the engine's own per-record states at the set level. It is a
// SUMMARY and not a sixth vocabulary — the words are the ledger's states read from
// one row up, and the per-record detail stays available on `seed.status`, which is
// the engine's own door (one engine, one truth).
//
// `never_applied` is separated from `partial` deliberately: a daemon that started
// without ever running the inert set's apply is a healthy daemon in a state nobody
// has reached yet, while a partially applied set is something to look at.
func summarizeSet(s seed.SetStatus) string {
	if s.AppliedVersion == 0 {
		return "never_applied"
	}
	bad := false
	for _, r := range s.Records {
		switch r.State {
		case seed.StateFailed:
			return "failed"
		case seed.StateAbsent, seed.StateRemoved, seed.StateDiverged, seed.StateRetired:
			bad = true
		}
	}
	if bad || s.AppliedVersion != s.AvailableVersion {
		return "partial"
	}
	return "current"
}
