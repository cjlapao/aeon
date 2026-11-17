package config

import "strings"

// settings_env.go — the environment name derivation (CS4). The env var name is
// derived, never configured: "AEON_SETTING_" + the key upper-cased with every
// "." replaced by "_". A pure function, no table. Camel humps are NOT split
// (deterministic and obvious), so two keys that upper-case to the same name
// collide and are refused (CS4): a.bC and a.bc both map to AEON_SETTING_A_BC.

// EnvName derives the environment variable name from a setting key (CS4):
// prefix "AEON_SETTING_" + upper-case + every "." -> "_". Pure function.
//
//
//	EnvName("agents.allowUnsignedImport") == "AEON_SETTING_AGENTS_ALLOWUNSIGNEDIMPORT"
func EnvName(key string) string {
	return "AEON_SETTING_" + strings.ToUpper(strings.ReplaceAll(key, ".", "_"))
}

// envCollides is the collision-detection helper: it reports whether key's env
// name collides with a previously declared key. NewRegistry uses it to enforce
// CS4 — two keys that map to the same env name are refused, naming both and
// the shared env name. seen maps env name -> the key that declared it first.
func envCollides(key string, seen map[string]string) (env string, collision bool, prev string) {
	env = EnvName(key)
	if prevKey, ok := seen[env]; ok {
		return env, true, prevKey
	}
	return env, false, ""
}
