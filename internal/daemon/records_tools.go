// records_tools.go — S47 T7 (TC9, OL3, MC2, 2026-11-02): the two catalog arms of
// the agent's own tool view, `tools_mine` and `tool_show`.
//
// These are the arms of ONE definition with its socket twin: the catalog entry is
// the definition, `registerRecords` projects its `Method` (`tools.mine.list`,
// `tools.mine.show`), and both doors land in `RecordsService.Do` with the caller's
// kind already stamped by whoever holds the connection. What lives in THIS file is
// the half that only a step's own view can decide — the stamp, the refusals and
// the V13 absence — and it deliberately stops there: the computation itself is
// `toolsCatalogService.ToolsMine`/`ToolShow`, the same code the person's
// `tools.byAgent` reads through, because two implementations of "what does this
// agent hold" would be two answers to one question (T4's differential test is what
// makes that disagreement visible, and a duplicated grant chain would fail it for
// the wrong reason). That is a deviation from T7's file list, which put the
// implementation here; recorded in the task's Notes with the reason.
//
// The person half is refused BEFORE the switch by `personDoorRefusal` (rule 6),
// with the pointer sentence that names the door that does answer a person. A
// person holding the endpoint could otherwise walk the agent-scoped doors one slug
// at a time and read back what every agent holds — which is exactly the use S46
// closed for `mcp.mine.*` and which is why the unscoped reads stay separate.
package daemon

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/cjlapao/aeon/internal/protocol"
)

// toolsViewAPI is the half of the catalog service the two own-view arms need,
// named as an interface for the same reason `protocol.ToolsCatalog` is one: the
// daemon assigns the concrete service, and a test can hand the records service a
// spy that records WHICH agent each call was made for — which is how rule 1's
// "stamped, never named" is proven at the arm rather than asserted in a comment.
type toolsViewAPI interface {
	ToolsMine(ctx context.Context, agent string) (protocol.ToolsMineResult, error)
	ToolShow(ctx context.Context, agent, name string) (protocol.ToolShowResult, error)
}

// toolsViewlessRefusal is the V13 half of the two arms: a mount with no catalog
// service says so rather than answering an empty inventory. An empty own-view is
// the most dangerous plausible answer this door can produce — it reads as "you may
// call nothing", about an agent that may be able to call plenty, from a build that
// never wired the half that would have told it (P31). The four `tools.*` person
// methods are absent for the same reason, and the sentence says so.
func toolsViewlessRefusal(tool string) *Failure {
	return &Failure{Sentence: fmt.Sprintf("refused: %s needs the tools catalog, and this build did not wire it — the tools.list/get/byAgent/status doors are absent for the same reason (V13)", tool)}
}

// toolsMineSelf is OL3 for the own view: the agent is the step's stamp, and a step
// that names no agent has no own view to answer from. There is no fallback here —
// not the caller's uid, not a default agent — because a guessed identity that
// happens to resolve is a view of somebody else's grants served as yours.
func toolsMineSelf(tool string, ident Identity) (string, *Failure) {
	if ident.User {
		// Unreachable through the socket (personDoorRefusal turns the person back
		// first) and refused again anyway: an arm that answers from a stamp must
		// not answer from a person's uid even when a caller has bypassed the fork.
		return "", &Failure{Sentence: fmt.Sprintf("refused: %s is agent-scoped; a person reads the same rows through tools.byAgent and tools.get", tool)}
	}
	if ident.Agent == "" {
		return "", &Failure{Sentence: fmt.Sprintf("refused: %s answers from the agent's own view, and this step stamps no agent — identity is read from the store, never guessed (OL3)", tool)}
	}
	return ident.Agent, nil
}

// toolsMine is the `tools_mine` arm: the agent's inventory and nothing else.
func (r *RecordsService) toolsMine(ctx context.Context, ident Identity, input json.RawMessage) (any, *Failure, error) {
	if r.toolsCatalog == nil {
		return nil, toolsViewlessRefusal("tools_mine"), nil
	}
	// The schema is `{}` with additionalProperties:false: a param naming another
	// agent is refused at this strict decode, before the stamp is read (rule 1).
	if f, err := decode(input, &struct{}{}); f != nil || err != nil {
		return nil, f, err
	}
	self, f := toolsMineSelf("tools_mine", ident)
	if f != nil {
		return nil, f, nil
	}
	// The seam's error is returned UNTOUCHED, not folded into a Failure: the
	// refusal an own-view read makes is a typed one (`ErrToolAgentUnknown`),
	// and a sentence paraphrase on the way out would turn -31912 into prose.
	// The socket ships a `*protocol.Error` verbatim; the tool path renders it.
	res, err := r.toolsCatalog.ToolsMine(ctx, self)
	if err != nil {
		return nil, nil, err
	}
	return res, nil, nil
}

// toolShow is the `tool_show` arm: one tool of the caller's own, in full.
func (r *RecordsService) toolShow(ctx context.Context, ident Identity, input json.RawMessage) (any, *Failure, error) {
	if r.toolsCatalog == nil {
		return nil, toolsViewlessRefusal("tool_show"), nil
	}
	var in struct {
		Name string `json:"name"`
	}
	if f, err := decode(input, &in); f != nil || err != nil {
		return nil, f, err
	}
	self, f := toolsMineSelf("tool_show", ident)
	if f != nil {
		return nil, f, nil
	}
	// Same discipline as `tools_mine`: the identical refusal an unheld name and a
	// nonexistent one produce is a typed error, and it is the ANSWER rule 3
	// tests — so it must survive the trip to the caller unchanged.
	res, err := r.toolsCatalog.ToolShow(ctx, self, in.Name)
	if err != nil {
		return nil, nil, err
	}
	return res, nil, nil
}
