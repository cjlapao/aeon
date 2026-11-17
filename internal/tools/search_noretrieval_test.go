//go:build noretrieval

package tools

// search_noretrieval_test.go — V13 from inside the build where the tool
// cannot exist. This file compiles ONLY under the noretrieval tag; the
// retrieval package's TestSearchAbsentWithoutThePackage runs it as a
// subprocess so the claim is made by the build itself, not about it.

import "testing"

func TestSearchAbsentInNoRetrievalBuild(t *testing.T) {
	if SearchRegistered() {
		t.Fatal("the resolver seam is wired in the no-retrieval build — nothing here may wire it (V13, D1)")
	}
	if _, ok := Registry()["search"]; ok {
		t.Fatal("search exists in the no-retrieval registry — absent means it does not exist, not that it returns nothing")
	}
	// The floor again: this test must run against the real registry.
	if n := len(Registry()); n < 40 {
		t.Fatalf("the registry holds %d tools — too few to be the real one", n)
	}
}
