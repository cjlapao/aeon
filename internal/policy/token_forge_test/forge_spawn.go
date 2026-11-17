//go:build forge_spawn

// This file is compiled by NO normal build, test, or vet run: it hides behind
// the `forge_spawn` tag, which nothing in the repository ever sets. Its only
// purpose is to be compiled deliberately by TestForgedCodeDoesNotCompile — as
// the exact code a hostile author would write to reach a process while
// skipping the verdict — and to FAIL there with the diagnostic the rule
// promises: "not enough arguments in call to". Spawn's fourth parameter is
// the token, so there is no call shape that reaches exec without one.
//
// If this ever starts compiling, the verdict→receipt→spawn ordering has a
// hole in it and the test fails.

package token_forge_test

import (
	"context"

	"github.com/cjlapao/aeon/internal/tools"
)

// spawnWithoutVerdict tries to reach a process while skipping the verdict.
func spawnWithoutVerdict() error {
	_, err := tools.Spawn(context.Background(), "true", tools.SpawnOptions{})
	return err
}
