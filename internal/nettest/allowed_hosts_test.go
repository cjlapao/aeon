package nettest

import (
	"reflect"
	"testing"
)

// TestAllowedHostsIsAUnionNeverAWidening — SV14's assertion list: the
// allowed set is the three configured groups, deduped, in order, with
// nothing implied. A host in none of them is a bug the doctor must find,
// so the union must also never GAIN an entry from an empty group.
func TestAllowedHostsIsAUnionNeverAWidening(t *testing.T) {
	// Hosts are INERT shapes here (S58 T10): the forge guard
	// TestNoOtherTestGrewANetworkDependency refuses any test that names the
	// live forge host — a live call is one refactor away — and this file,
	// shipped at T5, named it as illustration. The union semantics are the
	// test; the hostnames never were.
	got := AllowedHosts(
		[]string{"forge.example.test", ""},
		[]string{"huggingface.co", "forge.example.test"}, // a forge that is also a route: one entry
		[]string{"hooks.example.com"},
	)
	want := []string{"forge.example.test", "huggingface.co", "hooks.example.com"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("AllowedHosts = %v, want %v", got, want)
	}
	if empty := AllowedHosts(nil, nil, nil); len(empty) != 0 {
		t.Fatalf("no groups produced %v — zero configured hosts must assert ZERO connections", empty)
	}
	if dup := AllowedHosts([]string{"a"}, []string{"a"}, []string{"a"}); len(dup) != 1 {
		t.Fatalf("a host named thrice produced %v", dup)
	}
}
