package policy_test

// S74 T2: an address pattern in a ToolAccess row, through the engine.

import (
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/policy"
)

func addrReq(agent, tool, address string) policy.Request {
	r := toolReq(agent, tool)
	r.ToolAddress = address
	return r
}

func TestAnAddressPatternAllowsReadsAndNothingThatWrites(t *testing.T) {
	allow := toolPolicy(t, "reads", policy.AgentKind, "main", policy.Allow, toolSpec(t, "workspace.*.read", "allow"))
	ps := []policy.Policy{allow}

	v := policy.Evaluate(ps, addrReq("main", "glob", "workspace.glob.read"), evalAt)
	assertVerdict(t, v, true, false, false, allow.ID, "")

	v = policy.Evaluate(ps, addrReq("main", "write", "workspace.write.write"), evalAt)
	assertVerdict(t, v, false, true, false, "", "")
}

func TestADenyBeatsAWildcardAllow(t *testing.T) {
	allow := toolPolicy(t, "reads", policy.AgentKind, "main", policy.Allow, toolSpec(t, "*.*.read", "allow"))
	deny := toolPolicy(t, "noread", policy.AgentKind, "main", policy.Deny, toolSpec(t, "workspace.read.read", "deny"))
	ps := []policy.Policy{allow, deny}

	if v := policy.Evaluate(ps, addrReq("main", "read", "workspace.read.read"), evalAt); v.Allowed {
		t.Fatalf("a deny must beat a wildcard allow: %+v", v)
	}
	if v := policy.Evaluate(ps, addrReq("main", "glob", "workspace.glob.read"), evalAt); !v.Allowed {
		t.Fatalf("glob is still allowed by the wildcard: %+v", v)
	}
}

func TestAnUnresolvedToolMatchesNoPattern(t *testing.T) {
	allow := toolPolicy(t, "all", policy.AgentKind, "main", policy.Allow, toolSpec(t, "*.*.*", "allow"))
	// No address and no resolver answer for this name: absent is not zero.
	v := policy.Evaluate([]policy.Policy{allow}, toolReq("main", "no-such-tool"), evalAt)
	if v.Allowed {
		t.Fatalf("an unresolved tool must not match a wildcard: %+v", v)
	}
}

func TestABareNameStillMatchesExactlyAlongsideAPattern(t *testing.T) {
	bare := toolPolicy(t, "bash", policy.AgentKind, "main", policy.Allow, toolSpec(t, "bash", "allow"))
	v := policy.Evaluate([]policy.Policy{bare}, addrReq("main", "bash", "process.bash.write"), evalAt)
	assertVerdict(t, v, true, false, false, bare.ID, "")
}

func TestThePatternWriteBoundaryNamesTheOffence(t *testing.T) {
	row := toolPolicy(t, "bad", policy.AgentKind, "main", policy.Allow, toolSpec(t, "wo*.*.read", "allow"))
	val, _ := policy.ValidatorFor(policy.ToolAccess)
	rej := val.Validate(row)
	if rej == nil || !strings.Contains(rej.Reason, "segment 1") || !strings.Contains(rej.Reason, "partial wildcard") {
		t.Fatalf("a partial star must be refused naming segment 1: %v", rej)
	}
}
