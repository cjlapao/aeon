//go:build forge_literal

// This file is compiled by NO normal build, test, or vet run: it hides behind
// the `forge_literal` tag, which nothing in the repository ever sets. Its only
// purpose is to be compiled deliberately by TestForgedCodeDoesNotCompile — as
// the exact code a hostile author would write to hand-produce spawn
// authority — and to FAIL there with the diagnostic the unforgeability rule
// promises: "cannot refer to unexported field". minted, command and nonce are
// unexported, so no composite literal outside the policy package can carry a
// minted sentinel, a bound command, or a mint nonce.
//
// It lives in its own file and its own tag because the type-checker aborts a
// file at its first error: with both forgeries in one file, the second would
// never reach the compiler, and an unproven forgery is an unenforced rule.
// If this ever starts compiling, the token is forgeable and the test fails.

package token_forge_test

import "github.com/cjlapao/aeon/internal/policy"

// forged tries to hand-write spawn authority.
var forged = policy.AllowToken{minted: true, command: "true", nonce: 7}
