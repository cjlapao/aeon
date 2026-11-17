package tools

// gateenv.go — the gate's environment (S57 VG5). A gate command is authority:
// a loop will run it, and it must run with NOTHING the daemon carries that is
// not on the allowlist. Dropping is by omission — a name not on the list is
// gone. The set is closed, so a new secret-shaped variable is dropped the day
// it appears: the list was never written for it, and there is no negative
// entry to forget.

// gateEnvNames is the closed allowlist, in display order. LC_* is a PREFIX,
// not a name: every locale variable the parent carries is kept (they name
// the locale, they carry no secrets), and that is the only wildcard the
// scrub has.
var gateEnvNames = []string{
	"PATH",
	"HOME",
	"USER",
	"LOGNAME",
	"SHELL",
	"LANG",
	"TERM",
	"TMPDIR",
	"TZ",
	// The closed set of toolchain locations. These name WHERE a toolchain
	// lives so the gate's commands can find their compilers and caches; they
	// carry no credentials.
	"GOPATH",
	"GOCACHE",
	"GOMODCACHE",
	"GOFLAGS",
	"CARGO_HOME",
	"RUSTUP_HOME",
	"NPM_CONFIG_CACHE",
	"PNPM_HOME",
	"VIRTUAL_ENV",
}

// gateEnvLCPrefix is the one prefix the allowlist accepts: the locale
// variables.
const gateEnvLCPrefix = "LC_"

// gateEnvCI is the constant the gate's environment always carries: a gate is
// a machine-run build, and CI tooling that branches on CI=1 gets the
// non-interactive behaviour it expects.
const gateEnvCI = "CI=1"

// GateEnvNames returns the allowlist's fixed names, in display order. The
// LC_ prefix is part of the scrub (every LC_* variable is kept) and is
// documented here rather than spelled out as one name each; the constant
// CI=1 is appended by GateEnv itself. The daemon's verify.vocabulary reads
// this — no client types the list.
func GateEnvNames() []string {
	out := make([]string, len(gateEnvNames))
	copy(out, gateEnvNames)
	return out
}

// gateEnvKept reports whether name may cross into a gate's environment.
func gateEnvKept(name string) bool {
	for _, n := range gateEnvNames {
		if name == n {
			return true
		}
	}
	return len(name) > len(gateEnvLCPrefix) && name[:len(gateEnvLCPrefix)] == gateEnvLCPrefix
}

// GateEnv returns the child's COMPLETE environment for a gate: the parent
// entries whose names are on the allowlist (parent order, first occurrence
// wins), plus the constant CI=1. Every other parent entry is dropped —
// including AEON_*, GITHUB_TOKEN, GH_TOKEN, any provider key and any
// *TOKEN*|*KEY*|*SECRET*|*PASSWORD* shape: the list is closed, so there is
// nothing to forget. The result is a complete environment (Spawn hands it
// to cmd.Env as-is); it is never an overlay on the inherited one.
func GateEnv(parent []string) []string {
	var out []string
	seen := make(map[string]bool, len(parent))
	for _, kv := range parent {
		name, value, ok := splitEnvKV(kv)
		if !ok || seen[name] {
			continue
		}
		if !gateEnvKept(name) {
			continue
		}
		seen[name] = true
		out = append(out, name+"="+value)
	}
	if !seen["CI"] {
		out = append(out, gateEnvCI)
	}
	return out
}

// splitEnvKV splits one KEY=VALUE environment string. A entry with no '='
// has no value and no name (the shell says unset, not empty) — the allowlist
// decides on names, so it is dropped.
func splitEnvKV(kv string) (name, value string, ok bool) {
	i := 0
	for i < len(kv) && kv[i] != '=' {
		i++
	}
	if i == 0 || i == len(kv) {
		return "", "", false
	}
	return kv[:i], kv[i+1:], true
}
