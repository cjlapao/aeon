// agent_admission.go — S37 T3: the create-time preview for kind agent_import,
// built as a cmd/aeon function (AD3). It is a strict subset of what
// `ImportChecked` re-decides at commit (AA3): it runs the three READ-ONLY
// steps — ReadPackagePreview, ReadCarriedPolicies, CheckRequests — against the
// policy set in force now, and renders the consent the human would decide
// against; it registers NOTHING and writes NO row. The commit door (T5) is the
// only place the agent is ever imported or a carried denial ever stored.
//
// It lives in cmd/aeon because TestNoUnconditionalImportOfPlugin forbids any
// package other than cmd/aeon (and internal/plugin) from importing
// internal/plugin; the protocol layer receives it through a function seam
// (T4, mirroring S15's Table.Plugins). absent_test.go stays zero-diff.
package main

import (
	"context"
	"fmt"
	"time"

	"github.com/cjlapao/aeon/internal/plugin"
	"github.com/cjlapao/aeon/internal/policy"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

// agentImportPreviewFor builds the closure the socket calls at
// `proposal.create` for kind agent_import (T4 plugs it into the validator
// seam). Same return shape as pluginsHandleFor (agent.go): a refusal is the
// second return value (the Reject's own text, verbatim), an infrastructure
// failure the third.
//
// It writes nothing: no ImportChecked, no recordImport, no
// StoreCarriedPluginDenial, no policy.Store.Add, no Enable, and no store row.
// A carried Deny is only ever added at commit (T5), through the human door —
// the preview shows it so a proposer can see what they would carry, not to
// add it.
func agentImportPreviewFor(st *store.Store) func(ctx context.Context, pkgPath string) (preview *protocol.AgentImportPreview, refusal string, err error) {
	return func(ctx context.Context, pkgPath string) (*protocol.AgentImportPreview, string, error) {
		if err := wirePluginSeams(st); err != nil {
			return nil, "", err // card verifier + ledger, as the daemon wires them
		}
		// Step one: read the package (read-only — registers nothing).
		a, rj := plugin.ReadPackagePreview(pkgPath)
		if rj != nil {
			return nil, rj.Error(), nil
		}
		// The carried denials the package would add if committed — shown, not stored.
		carried, rj := plugin.ReadCarriedPolicies(pkgPath)
		if rj != nil {
			return nil, rj.Error(), nil
		}
		now := time.Now().UTC()
		// The policy set in force now — the same read the daemon's door makes.
		ps, prj := policy.NewStore(policyRepo(st)).List(ctx, now)
		if prj != nil {
			// agentCtx.set()'s own text (agent.go), kept equal — the check
			// judges against the set in force, and inventing one would judge
			// against nothing.
			return nil, fmt.Sprintf("the policy set could not be read (%s) — the check judges against the set in force, and inventing one would judge against nothing", prj.Reason), nil
		}
		// The blocklist, against the set in force.
		if rj := plugin.CheckRequests(a, ps, now); rj != nil {
			return nil, rj.Error(), nil
		}
		// ImportChecked sets this before registering; RenderConsent reads it.
		a.Carried = carried
		out := &protocol.AgentImportPreview{
			Agent:       a.Name,
			Consent:     plugin.RenderConsent(a, ps).String(),
			CarriedDeny: []protocol.AgentImportCarried{},
		}
		for _, p := range carried {
			out.CarriedDeny = append(out.CarriedDeny, protocol.AgentImportCarried{
				ID:     p.ID,
				Target: protocol.AgentImportTarget{Kind: string(p.Target.Kind), ID: p.Target.ID},
			})
		}
		return out, "", nil
	}
}
