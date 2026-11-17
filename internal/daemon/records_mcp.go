package daemon

// records_mcp.go — S46 T8 (MG8, MG9, MC2, 2026-11-02): the five MCP catalog
// arms, implemented ON the records service so each has its socket twin and
// its tool through the one Do (internal/records' catalog is the definition),
// exactly the autonomy arms' shape (S56 T8's precedent).
//
// THE READS (mcp_servers, mcp_grants, mcp_status) forward to the cmd/aeon
// seam's SCOPED entries — the same rows, the same registry, the same probe
// cache the person's management reads use, but reached as SELF: the agent
// identity is resolved here from the step (OL3, the Identify chain Do already
// runs) and carried to the seam through protocol.WithMCPActor, the channel
// the import door already stamped peers through. It is a STAMP, not a
// param: the schema carries no agent field, a smuggled one is a parse
// refusal at the strict decode, and a socket caller cannot reach these
// names as a person at all (personDoorRefusal answers with the pointer
// sentence — the mcp.mine.* doors are agent-scoped in both directions).
// MC2 is thereby structural: the seam entries build from the caller's own
// view (private rows keyed by the stamp, declarations keyed by the stamp),
// so a peer's private server is not filtered out of an answer — it is never
// in the answer's input.
//
// THE PROPOSES (mcp_propose_remove_server, mcp_propose_revoke_grant) split
// as the two doors split (a recorded deviation from T8's file list, which
// put the whole propose in cmd/aeon): the visibility-and-shape half runs in
// cmd/aeon over the rows and the registry (an invisible server and a
// nonexistent one come back as the SAME "no such server" refusal — no
// existence oracle), and the cap-validator-create half stays in the ONE
// inert-proposal path this file's siblings already use (createInertProposal:
// the 8 KiB bound, the per-agent open cap, the shared validator TD3, then
// the shared ProposalStore stamped by `by`). A second create path in
// cmd/aeon would be a third door the parity tests then have to straddle.
//
// V13 honesty: no MCP seam wired ⇒ the five arms refuse with a named
// sentence (the autonomylessRefusal precedent), never answering zero.
import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/cjlapao/aeon/internal/desk"
	"github.com/cjlapao/aeon/internal/protocol"
)

// mcplessRefusal is the V13 half of the MCP arms: a mount without the MCP
// admin seam says so rather than answering an empty view — an empty own-view
// would read as "you have no servers", which is a confident wrong answer
// (P31) about a surface this build never wired.
func mcplessRefusal(tool string) *Failure {
	return &Failure{Sentence: fmt.Sprintf("refused: %s needs the MCP surface, and this build did not wire it — the mcp.* management methods are absent for the same reason (V13)", tool)}
}

// mcpSelf resolves the stamped agent for a mine arm. The person half never
// reaches here (personDoorRefusal turns it back first); an empty slug means
// the step resolved but names no agent, which is a broken stamp, refused.
func mcpSelf(tool string, ident Identity) (string, *Failure) {
	if ident.Agent == "" {
		return "", &Failure{Sentence: fmt.Sprintf("refused: %s answers from the agent's own view, and this step stamps no agent — identity is read from the store, never guessed (OL3)", tool)}
	}
	return ident.Agent, nil
}

// mcpForward is the one call the five arms share: stamp self into the ctx
// channel and enter the seam's scoped entry. The seam answers (payload,
// refusal sentence, infrastructure error) — refusals travel verbatim, the
// way every other arm sends Failure.Sentence unchanged.
func (r *RecordsService) mcpForward(ctx context.Context, tool, self, method string, input json.RawMessage) (any, *Failure, error) {
	out, sentence, err := r.mcpAdmin(protocol.WithMCPActor(ctx, "agent", self), method, input)
	if err != nil {
		return nil, nil, fmt.Errorf("daemon: %s: %w", tool, err)
	}
	if sentence != "" {
		return nil, &Failure{Sentence: sentence}, nil
	}
	return out, nil, nil
}

// — the three reads —

func (r *RecordsService) mcpMineServers(ctx context.Context, ident Identity, input json.RawMessage) (any, *Failure, error) {
	if r.mcpAdmin == nil {
		return nil, mcplessRefusal("mcp_servers"), nil
	}
	if f, err := decode(input, &struct{}{}); f != nil || err != nil {
		return nil, f, err
	}
	self, f := mcpSelf("mcp_servers", ident)
	if f != nil {
		return nil, f, nil
	}
	return r.mcpForward(ctx, "mcp_servers", self, string(protocol.MethodMCPMineServers), nil)
}

func (r *RecordsService) mcpMineGrants(ctx context.Context, ident Identity, input json.RawMessage) (any, *Failure, error) {
	if r.mcpAdmin == nil {
		return nil, mcplessRefusal("mcp_grants"), nil
	}
	if f, err := decode(input, &struct{}{}); f != nil || err != nil {
		return nil, f, err
	}
	self, f := mcpSelf("mcp_grants", ident)
	if f != nil {
		return nil, f, nil
	}
	return r.mcpForward(ctx, "mcp_grants", self, string(protocol.MethodMCPMineGrants), nil)
}

func (r *RecordsService) mcpMineStatus(ctx context.Context, ident Identity, input json.RawMessage) (any, *Failure, error) {
	if r.mcpAdmin == nil {
		return nil, mcplessRefusal("mcp_status"), nil
	}
	if f, err := decode(input, &struct{}{}); f != nil || err != nil {
		return nil, f, err
	}
	self, f := mcpSelf("mcp_status", ident)
	if f != nil {
		return nil, f, nil
	}
	// The seam reads the CACHE only — this arm spawns nothing, and the
	// cmd/aeon test TestMcpStatusToolDoesNotProbe proves it with a spy.
	return r.mcpForward(ctx, "mcp_status", self, string(protocol.MethodMCPMineStatus), nil)
}

// — the two proposes —
//
// The input shapes mirror the catalog schemas exactly (the strict decode
// makes a smuggled agent/actor/status a parse refusal). The reason rule is
// the ONE desk sentence (desk.ProposeReasonSentence, S45/S56's), run here
// before the seam is asked for anything — a refusal that costs no rows.
// The seam answers the canonical payload (visibility checked, self stamped
// into the payload's own agent field); createInertProposal then runs the
// caps, the shared validator, and the stamped create.

type mcpMineProposeRemoveInput struct {
	Server string `json:"server"`
	Reason string `json:"reason"`
}

func (r *RecordsService) mcpMineProposeRemove(ctx context.Context, by Actor, ident Identity, input json.RawMessage) (any, *Failure, error) {
	if r.mcpAdmin == nil {
		return nil, mcplessRefusal("mcp_propose_remove_server"), nil
	}
	var in mcpMineProposeRemoveInput
	if f, err := decode(input, &in); f != nil || err != nil {
		return nil, f, err
	}
	if in.Server == "" {
		return nil, &Failure{Sentence: "refused: mcp_propose_remove_server needs the server's name — a removal from nothing is a proposal a person would waste a commit on (TD3); see the view with mcp_servers"}, nil
	}
	if s := desk.ProposeReasonSentence(in.Reason, "mcp_propose_remove_server"); s != "" {
		return nil, &Failure{Sentence: s}, nil
	}
	self, f := mcpSelf("mcp_propose_remove_server", ident)
	if f != nil {
		return nil, f, nil
	}
	ready, f, err := r.mcpReady(ctx, "mcp_propose_remove_server", self, string(protocol.MethodMCPMineProposeRemove), input)
	if f != nil || err != nil {
		return nil, f, err
	}
	return r.createInertProposal(ctx, by, ready.Kind, ready.Payload)
}

type mcpMineProposeRevokeInput struct {
	Server string `json:"server"`
	Tool   string `json:"tool"`
	Reason string `json:"reason"`
}

func (r *RecordsService) mcpMineProposeRevoke(ctx context.Context, by Actor, ident Identity, input json.RawMessage) (any, *Failure, error) {
	if r.mcpAdmin == nil {
		return nil, mcplessRefusal("mcp_propose_revoke_grant"), nil
	}
	var in mcpMineProposeRevokeInput
	if f, err := decode(input, &in); f != nil || err != nil {
		return nil, f, err
	}
	if in.Server == "" || in.Tool == "" {
		return nil, &Failure{Sentence: "refused: mcp_propose_revoke_grant needs both halves of the declared pair — server and tool, split — a revocation from nothing is a proposal a person would waste a commit on (TD3); see the grants with mcp_grants"}, nil
	}
	if s := desk.ProposeReasonSentence(in.Reason, "mcp_propose_revoke_grant"); s != "" {
		return nil, &Failure{Sentence: s}, nil
	}
	self, f := mcpSelf("mcp_propose_revoke_grant", ident)
	if f != nil {
		return nil, f, nil
	}
	ready, f, err := r.mcpReady(ctx, "mcp_propose_revoke_grant", self, string(protocol.MethodMCPMineProposeRevoke), input)
	if f != nil || err != nil {
		return nil, f, err
	}
	return r.createInertProposal(ctx, by, ready.Kind, ready.Payload)
}

// mcpReady asks the seam's visibility-and-shape half and unpacks the
// canonical answer. The kind is the seam's to name (the two narrowing kinds
// — an answer naming any other kind is an infrastructure fault, not a
// proposal: widening is not on this table).
func (r *RecordsService) mcpReady(ctx context.Context, tool, self, method string, input json.RawMessage) (*protocol.MineProposeReady, *Failure, error) {
	out, f, err := r.mcpForward(ctx, tool, self, method, input)
	if err != nil || f != nil {
		return nil, f, err
	}
	raw, merr := json.Marshal(out)
	if merr != nil {
		return nil, nil, fmt.Errorf("daemon: %s: the seam's ready answer will not encode: %w", tool, merr)
	}
	var ready protocol.MineProposeReady
	if uerr := json.Unmarshal(raw, &ready); uerr != nil {
		return nil, nil, fmt.Errorf("daemon: %s: the seam's ready answer is not a MineProposeReady: %w", tool, uerr)
	}
	if ready.Kind != string(desk.KindMCPServerRem) && ready.Kind != string(desk.KindMCPGrantRevoke) {
		return nil, nil, fmt.Errorf("daemon: %s: the seam named kind %q — the agent's MCP proposes know exactly the two narrowing kinds (MG9)", tool, ready.Kind)
	}
	if len(ready.Payload) == 0 {
		return nil, nil, fmt.Errorf("daemon: %s: the seam answered a ready answer without a payload", tool)
	}
	return &ready, nil, nil
}
