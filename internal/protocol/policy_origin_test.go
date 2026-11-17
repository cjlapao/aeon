package protocol

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/policy"
)

// S45 T12 (PM16): the read side says PROVENANCE. `policy.list` carries an
// `origin` filter over the three-value closed set — refused by name outside
// it — and every view carries its own origin beside createdAt. The tests
// sit beside the frozen six-read tests, which stand untouched: origin is
// additive, and absence of the field means a renderer that predates it,
// never a row with no provenance.

func TestPolicyListCarriesOriginAndFiltersByIt(t *testing.T) {
	tbl, store, fx := policyTable(t)
	addPolicyAt(t, store, "seeded-one", policy.CommandAccess, mainTarget, policy.Allow,
		`{"command":"ls*","state":"allow"}`, "")
	addPolicyAt(t, store, "hand-one", policy.CommandAccess, mainTarget, policy.Deny,
		`{"command":"rm*","state":"deny"}`, "")
	// The fake answers the listing from the map; the real store gets its
	// origins from the column DEFAULT and the seed door. Set them here the
	// way the column would: one row the seed wrote, one the operator did.
	sys := fx.rows["seeded-one"]
	sys.Origin = "system"
	fx.rows["seeded-one"] = sys
	hand := fx.rows["hand-one"]
	hand.Origin = "user"
	fx.rows["hand-one"] = hand

	res := dispatchPolicy[PolicyListResult](t, tbl, MethodPolicyList, json.RawMessage(`{}`))
	if len(res.Policies) != 2 {
		t.Fatalf("unfiltered list: %d rows", len(res.Policies))
	}
	byID := map[string]PolicyView{}
	for _, v := range res.Policies {
		byID[v.ID] = v
	}
	if byID["seeded-one"].Origin != "system" || byID["hand-one"].Origin != "user" {
		t.Fatalf("origin did not ride the views: %+v", res.Policies)
	}

	res = dispatchPolicy[PolicyListResult](t, tbl, MethodPolicyList,
		json.RawMessage(`{"origin":"system"}`))
	if len(res.Policies) != 1 || res.Policies[0].ID != "seeded-one" {
		t.Fatalf("origin=system: %+v", res.Policies)
	}
	res = dispatchPolicy[PolicyListResult](t, tbl, MethodPolicyList,
		json.RawMessage(`{"origin":"user"}`))
	if len(res.Policies) != 1 || res.Policies[0].ID != "hand-one" {
		t.Fatalf("origin=user: %+v", res.Policies)
	}

	// A value outside the three is refused BY NAME — the boundary's
	// ParseOrigin owns the vocabulary, and a silent-empty answer for a
	// misspelled provenance is the filter that lies.
	_, err := tbl.Dispatch(context.Background(), "c1", MethodPolicyList,
		mustJSON(t, map[string]string{"origin": "machine"}))
	if err == nil || !strings.Contains(err.Error(), "machine") {
		t.Fatalf("err = %v; the refusal must name the origin it refused", err)
	}
}
