package protocol

// tools.matchPattern (S74 TA10): which tools does a ToolAccess pattern name
// today. A read over the tools catalog and the ONE matcher, policy.
// MatchToolAddress, so the policy form's preview can never disagree with the
// gate. It lives with the catalog doors: it needs the catalog seam, and is
// absent when the seam is (V13).

import (
	"context"
	"encoding/json"

	"github.com/cjlapao/aeon/internal/policy"
)

// ToolsMatchPatternParams is the pattern to test: a bare tool name or an address
// pattern, exactly what a ToolAccess spec's toolName may hold.
type ToolsMatchPatternParams struct {
	Pattern string `json:"pattern"`
}

// ToolsMatchPatternResult lists the matching addresses. Zero matches is an
// answer, not an error: the pattern may name a tool that is added later.
type ToolsMatchPatternResult struct {
	Pattern string   `json:"pattern"`
	Matches []string `json:"matches"`
	Count   int      `json:"count"`
}

func (t *Table) handleToolsMatchPattern(ctx context.Context, _ *Session, params json.RawMessage) (any, error) {
	var p ToolsMatchPatternParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	if why := policy.ValidateToolPattern(p.Pattern); why != "" {
		return nil, ErrPolicyReadRefused.WithWhat("%s", why)
	}
	raw, err := t.toolsCatalogCall(ctx, func(svc ToolsCatalog) (any, error) {
		return svc.ToolsList(ctx, ToolsListFilter{})
	})
	if err != nil {
		return nil, err
	}
	entries, _ := raw.([]ToolEntry)
	matches := []string{}
	for _, e := range entries {
		switch {
		case policy.IsToolAddressForm(p.Pattern):
			if policy.MatchToolAddress(p.Pattern, e.Address) {
				matches = append(matches, e.Address)
			}
		case p.Pattern != "" && (e.Name == p.Pattern):
			matches = append(matches, e.Address)
		}
	}
	return ToolsMatchPatternResult{Pattern: p.Pattern, Matches: matches, Count: len(matches)}, nil
}
