package config

// settings_decl_agents.go — S44 T10's adoption of S43's
// agents.allowUnsignedImport onto the config store (path B). S43 stored the
// value as an env var (AEON_AGENTMANAGE_ALLOW_UNSIGNED_IMPORT, default false),
// read at wire time and captured into the agent-management Manager at
// construction. T10 makes it a declared Setting so the same value is read live
// per import call through config.Reader (env → database → default, CS7/CS11),
// with S43's old env var kept working as a one-release fallback in the read site
// (its name differs from this key's env name, so a deployed script must not
// silently stop working).
//
// This is the FIRST production declaration. T2 left the registry empty and a
// test (TestDefaultRegistryIsEmptyInProduction) pinning it; T10 flips that test
// to assert exactly this one key.
//
// init() ordering: settings.go's init sets defaultRegistry to empty, then this
// file's init (settings_decl_agents.go sorts after settings.go — "." < "_")
// mustDeclare's the key into it. The flipped test below is the safety net: a
// wrong order would leave the registry empty and fail.
func init() {
	mustDeclare(Setting{
		Key:  "agents.allowUnsignedImport",
		Area: "agents",
		Type: TypeBool,
		// The canonical text form the value takes; "false" is the refusal —
		// an unsigned card is refused by default.
		Default: "false",
		// Sensitive (CS8): widening this guard admits an agent a human has not
		// signed, so it requires a confirmation in the panel and the CLI, and
		// the description below says what the default protects.
		Sensitive: true,
		// A sensitive declaration must say what the default protects (≥ 40
		// characters, T2's rule).
		Description: "Admit an agent whose card is unsigned — no signature or certificate. The default (false) refuses an unsigned agent card and only a verified card imports; turning this on imports it marked unsigned.",
	})
}
