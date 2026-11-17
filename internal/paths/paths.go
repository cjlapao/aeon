// Package paths resolves where Aeon keeps things on disk.
//
// It exists now, before there is a store to put anywhere, because B10 requires
// the daemon to PRINT its socket and store paths on startup: every later
// slice's DEMO.md begins by finding them, and a daemon that hides where it
// lives costs one question per demo for the rest of the project.
//
// This package COMPUTES PATHS. It does not open anything, and it is not the
// store's path authority:
//
//   - The database's location is the FRAMEWORK's config
//     (database.sqlite.storage_path / file_name). internal/appenv feeds
//     StoreDir() into it as Aeon's default, so the framework keeps the
//     connector, the pool and the lifecycle while Aeon supplies the one
//     opinion it holds — where a user's state belongs.
//   - Opening the database, its schema and its migrations are S4's.
package paths

import (
	"os"
	"path/filepath"
	"runtime"
)

// StateDirEnv is the one Aeon-level override of where state lives.
//
// It sits ABOVE the XDG chain and below nothing. It exists for two callers and
// no others: the QA suite, which points every case at a throwaway directory so
// no case can touch a real store (S4 D19), and a person running two daemons on
// one machine deliberately.
//
// It is read HERE rather than in the store, because this package is already the
// one resolver and a second one would be the thing S4's redirect forbids.
// internal/appenv feeds whatever this resolves to into the framework's
// DATABASE_SQLITE_STORAGE_PATH, so the framework's config plane stays the thing
// the store actually reads.
const StateDirEnv = "AEON_STATE_DIR"

// StoreDir returns the directory holding Aeon's database.
//
// POSIX follows the XDG Base Directory spec: $XDG_STATE_HOME/aeon, falling back
// to ~/.local/state/aeon when XDG_STATE_HOME is unset or not absolute. The spec
// requires an absolute path, and a relative one would resolve against the
// process's working directory — which for a daemon is wherever it happened to
// be started, so the store would move depending on how it was launched.
//
// Windows uses %LocalAppData%\aeon: state is machine-local and must not follow
// a roaming profile onto another machine, where the socket path and the
// database would then disagree about which host they belong to.
func StoreDir() (string, error) { return StoreDirFor(os.Getenv) }

// StoreDirFor is StoreDir against an injected environment.
//
// The injection is not decoration: S4's I7 requires the state path to be
// resolvable without mutating the process environment, because a test that
// does os.Setenv to check a fallback has changed the thing every other test in
// the package reads. One resolver, two entry points.
func StoreDirFor(getenv func(string) string) (string, error) {
	if dir := getenv(StateDirEnv); filepath.IsAbs(dir) {
		return dir, nil
	}

	if runtime.GOOS == "windows" {
		if local := getenv("LocalAppData"); local != "" {
			return filepath.Join(local, "aeon"), nil
		}
		home, err := homeDir(getenv)
		if err != nil {
			return "", err
		}
		return filepath.Join(home, "AppData", "Local", "aeon"), nil
	}

	if xdg := getenv("XDG_STATE_HOME"); filepath.IsAbs(xdg) {
		return filepath.Join(xdg, "aeon"), nil
	}
	home, err := homeDir(getenv)
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "state", "aeon"), nil
}

// homeDir prefers the injected environment and falls back to the real one, so
// an injected getenv that says nothing about HOME still resolves rather than
// failing — a test asking "what is the XDG fallback" wants the fallback, not a
// home-directory error.
func homeDir(getenv func(string) string) (string, error) {
	key := "HOME"
	if runtime.GOOS == "windows" {
		key = "USERPROFILE"
	}
	if home := getenv(key); filepath.IsAbs(home) {
		return home, nil
	}
	return os.UserHomeDir()
}

// CursorSigningKeyFile returns the path to the cursor signing key.
//
// The framework requires cursor.signing_key and refuses to initialise config
// without it (framework v0.10.1). It must be STABLE across restarts: it signs
// pagination cursors so a client cannot forge one, and rotating it invalidates
// every cursor already handed out.
//
// It lives beside the store rather than in a config file because it is a
// secret: C17 allows the environment or a dedicated path, never a config file,
// never argv, never a log.
func CursorSigningKeyFile() (string, error) {
	dir, err := StoreDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "cursor.key"), nil
}

// VaultKeyFile returns where the variable manager's encryption key lives: a
// 32-byte key, created on first use, mode 0600, beside the store and NEVER inside
// it or a backup of it — the database holds ciphertext, and this file is what
// makes it readable. Losing the file loses every stored value (they must be set
// again); copying only the database elsewhere yields nothing.
func VaultKeyFile() (string, error) {
	dir, err := StoreDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "vault.key"), nil
}

// StoreFile returns the full path to the database file.
//
// Reported on startup whether or not it exists yet: "where it WILL be" is the
// answer a person needs before the first run, and "not yet created" is a
// different fact from "I cannot tell you where it goes".
func StoreFile() (string, error) {
	dir, err := StoreDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "state.db"), nil
}

// MCPConfigEnv overrides where the MCP configuration file lives.
//
// It sits above the XDG chain and below nothing, exactly as StateDirEnv does
// for the store: the QA suite and the DEMO walk point it at a throwaway file
// so no case reads — or writes — a real operator's configuration, and a
// person running two daemons on one host can give them two files. It is read
// HERE because this package is the one path resolver (S4's redirect: never a
// second one).
const MCPConfigEnv = "AEON_MCP_CONFIG"

// MCPConfigFile returns the path to the MCP configuration file (S12, MC3:
// the client is configured, never discovered — this file is the configuring
// door, and its absence is the level-0 state MC11 requires).
//
// POSIX: $AEON_MCP_CONFIG when absolute, else $XDG_CONFIG_HOME/aeon/mcp.json,
// else ~/.config/aeon/mcp.json. Windows: %APPDATA%\aeon\mcp.json — the
// roaming-side directory, because unlike the store (machine-local by
// design), a configuration of which third-party servers exist is a
// preference, and the Base Directory spec puts preferences there.
//
// It reports the path whether or not the file exists; "there is no MCP
// configuration" is an answer CLI verbs render (DEMO §1), not a missing path.
func MCPConfigFile() (string, error) { return MCPConfigFileFor(os.Getenv) }

// MCPConfigFileFor is MCPConfigFile against an injected environment — the
// same I7 discipline as StoreDirFor: a test checking a fallback must not
// mutate the environment every other test reads.
func MCPConfigFileFor(getenv func(string) string) (string, error) {
	if file := getenv(MCPConfigEnv); filepath.IsAbs(file) {
		return file, nil
	}

	if runtime.GOOS == "windows" {
		if appData := getenv("APPDATA"); appData != "" {
			return filepath.Join(appData, "aeon", "mcp.json"), nil
		}
		home, err := homeDir(getenv)
		if err != nil {
			return "", err
		}
		return filepath.Join(home, "AppData", "Roaming", "aeon", "mcp.json"), nil
	}

	if xdg := getenv("XDG_CONFIG_HOME"); filepath.IsAbs(xdg) {
		return filepath.Join(xdg, "aeon", "mcp.json"), nil
	}
	home, err := homeDir(getenv)
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "aeon", "mcp.json"), nil
}

// SkillsDirEnv overrides where Aeon's own (app-managed) skills live.
//
// Same station as StateDirEnv and MCPConfigEnv: absolute or ignored, above
// the XDG chain, read only HERE — the one path resolver rule again. The QA
// suite and the S65 demo point it at a throwaway directory so no case
// installs a skill into a real operator's app folder.
const SkillsDirEnv = "AEON_SKILLS_DIR"

// UserSkillsDirEnv overrides the shared user skills folder Aeon discovers
// from. It exists for the same reason SkillsDirEnv does — a test or demo
// must be able to point discovery at fixture folders — and the operator
// override is the honest general case of that need.
const UserSkillsDirEnv = "AEON_USER_SKILLS_DIR"

// SkillsDir returns the app-managed skills directory: where
// `aeon skills install` lands skills, and the only skills location Aeon
// ever writes.
//
// POSIX follows the Base Directory spec's DATA side ($XDG_DATA_HOME/aeon/
// skills, falling back to ~/.local/share/aeon/skills): installed content
// is user data that survives rebuilds and belongs neither in state (which
// is the database) nor in config (which is hand-authored). Windows uses
// %LocalAppData%\aeon\skills — machine-local content, like the store,
// because an installed skill's scripts are built for the machine that
// installed them.
//
// The path is reported whether or not it exists; "nothing is installed"
// and "I cannot tell you where things go" are different answers (B10).
func SkillsDir() (string, error) { return SkillsDirFor(os.Getenv) }

// SkillsDirFor is SkillsDir against an injected environment (the I7
// discipline StoreDirFor set).
func SkillsDirFor(getenv func(string) string) (string, error) {
	if dir := getenv(SkillsDirEnv); filepath.IsAbs(dir) {
		return dir, nil
	}

	if runtime.GOOS == "windows" {
		if local := getenv("LocalAppData"); local != "" {
			return filepath.Join(local, "aeon", "skills"), nil
		}
		home, err := homeDir(getenv)
		if err != nil {
			return "", err
		}
		return filepath.Join(home, "AppData", "Local", "aeon", "skills"), nil
	}

	if xdg := getenv("XDG_DATA_HOME"); filepath.IsAbs(xdg) {
		return filepath.Join(xdg, "aeon", "skills"), nil
	}
	home, err := homeDir(getenv)
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "share", "aeon", "skills"), nil
}

// UserSkillsDir returns the shared user skills folder — ~/.agents/skills —
// the cross-tool location the Agent Skills ecosystem reads (Codex's
// USER-scope path, the folder this project's own harness loads skills
// from). Aeon READS it and never writes it: files under a person's home
// are theirs, and an uninstall that ate someone's hand-authored skill
// would be the slice's one unforgivable act.
func UserSkillsDir() (string, error) { return UserSkillsDirFor(os.Getenv) }

// UserSkillsDirFor is UserSkillsDir against an injected environment.
func UserSkillsDirFor(getenv func(string) string) (string, error) {
	if dir := getenv(UserSkillsDirEnv); filepath.IsAbs(dir) {
		return dir, nil
	}
	home, err := homeDir(getenv)
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".agents", "skills"), nil
}
