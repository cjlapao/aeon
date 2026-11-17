package config

import "testing"

// settings_decl_test.go — a TEST-ONLY declaration file. It holds the
// testRegistry(t) helper the T2/T3/T4/T5 tests build from the same recipe,
// documented here so the three stay alike (T2.md). It is a _test.go file so no
// test declaration is shipped in the production registry (CS3): none of these is
// declared in package-level init(). The one production declaration (S44 T10's
// agents.allowUnsignedImport, in settings_decl_agents.go) is asserted by
// TestDefaultRegistryContainsExactlyTheAdoptedKey.

// testRegistry builds a *Registry of representative declarations:
//
//	test.flag  bool   default "false"
//	test.count int    Min 1 Max 10, default "3"
//	test.label string MaxLen 16, default "x"
//	test.mode  enum   auto|manual, default "auto"
//	test.guard bool   default "false", Sensitive: true, a 50+ char description
func testRegistry(t *testing.T) *Registry {
	t.Helper()
	// A 50+ character description for the sensitive guard (CS8): it must say
	// what the default protects, so a one-word description is refused.
	guardDesc := "This guard widens an import check: when true, an unsigned card may be imported; the default false keeps the restrictive state and refuses unsigned imports."
	r, err := NewRegistry(
		Setting{Key: "test.flag", Type: TypeBool, Default: "false", Description: "A test boolean setting."},
		Setting{Key: "test.count", Type: TypeInt, Min: intPtr(1), Max: intPtr(10), Default: "3", Description: "A test count setting."},
		Setting{Key: "test.label", Type: TypeString, MaxLen: 16, Default: "x", Description: "A test label setting."},
		Setting{Key: "test.mode", Type: TypeEnum, EnumValues: []string{"auto", "manual"}, Default: "auto", Description: "A test mode setting."},
		Setting{Key: "test.guard", Type: TypeBool, Default: "false", Sensitive: true, Description: guardDesc},
	)
	if err != nil {
		t.Fatalf("testRegistry: %v", err)
	}
	return r
}

// intPtr is a *int helper for the test registry's bounds.
func intPtr(n int) *int { return &n }
