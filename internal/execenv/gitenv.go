package execenv

// The environment for the daemon's OTHER subprocesses: the two git paths
// (EV10's `daemon-git` and `ui-read`).
//
// These are not step commands — no model authored the command line — but both
// execute whatever the repository says, and a repository is model-authored
// content: hooks, `textconv`, an external diff, `core.fsmonitor`,
// `core.sshCommand`. So a git child gets the same thing a scrubbed step gets:
// an allowlist, never the daemon's environment. Today both inherit everything
// (`git/plumbing.go`'s realExec sets no `cmd.Env` at all), which means a hook
// committed by a model can print the daemon's `ANTHROPIC_API_KEY`.
//
// Two shapes, because the two paths need different things and one list would
// quietly break one of them:
//
//   - `daemon-git` passes `SSH_AUTH_SOCK` (D9: ssh remotes cannot be pushed
//     without it) and keeps the user's git config, because the daemon's
//     code-actions push to real remotes;
//   - `ui-read` keeps every hardening line S31's `gitEnv()` already sets
//     (`GIT_CONFIG_GLOBAL=/dev/null` and friends), because a status/diff read
//     must not be retargeted by an ambient `GIT_DIR` or a machine config.
//
// `SSH_AUTH_SOCK` is denied for a step command and allowed here. That is not a
// contradiction: an agent shell reaching the ssh agent is a widening nobody
// asked for; the daemon pushing the person's own commit is the thing the agent
// is for.

import (
	"sort"
	"strings"
)

// The two git environment kinds.
const (
	GitEnvDaemon = "daemon-git"
	GitEnvUIRead = "ui-read"
)

// GitHardening is the fixed tail both git children get. It is exported because
// S31's `workspace/git.go` keeps its own copy for the golden to compare
// against, and a second list that drifts is how the hardening quietly stops
// being there.
func GitHardening() []string {
	return []string{
		"GIT_CONFIG_NOSYSTEM=1", // machine output, not this box's config
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_ATTR_NOSYSTEM=1",
		"GIT_TERMINAL_PROMPT=0", // never block on a credential question
		"GIT_PAGER=cat",
		"GIT_LITERAL_PATHSPECS=1", // a path is a path, not a glob
		"LC_ALL=C",
	}
}

// GitEnv builds the environment for one git child. The daemon environment
// arrives as data (EV5: never os.Getenv in here), so a test hands in a map and
// the ambient-state question is answerable without a process.
//
// Ambient `GIT_*` is never copied on either path: an inherited `GIT_DIR`,
// `GIT_WORK_TREE` or `GIT_INDEX_FILE` retargets the operation, and the UI path
// already strips them (F16) — this makes the plumbing path do the same.
func GitEnv(daemonEnv DaemonEnv, kind string) []string {
	gathered := map[string]string{}

	for _, name := range DefaultPassthrough() {
		if strings.HasSuffix(name, "*") {
			if daemonEnv.Names != nil {
				prefix := strings.TrimSuffix(name, "*")
				for _, cand := range daemonEnv.Names() {
					if strings.HasPrefix(cand, prefix) && !strings.HasPrefix(cand, "GIT_") {
						if v, ok := lookupValue(daemonEnv, cand); ok {
							gathered[cand] = v
						}
					}
				}
			}
			continue
		}
		if strings.HasPrefix(name, "GIT_") {
			continue
		}
		if v, ok := lookupValue(daemonEnv, name); ok {
			gathered[name] = v
		}
	}

	if kind == GitEnvDaemon {
		// D9: only here, and only this name.
		if v, ok := lookupValue(daemonEnv, "SSH_AUTH_SOCK"); ok {
			gathered["SSH_AUTH_SOCK"] = v
		}
	}

	out := make([]string, 0, len(gathered)+7)
	names := make([]string, 0, len(gathered))
	for n := range gathered {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		out = append(out, n+"="+gathered[n])
	}

	if kind == GitEnvDaemon {
		// The daemon's code-actions push to real remotes, so the person's
		// global config stays — the leak EV10 closes is the daemon's
		// credentials, not the repository's own settings. Only the prompt
		// guard is added, and it is the one line that turns a hang into an
		// error a step can report.
		return append(out, "GIT_TERMINAL_PROMPT=0", "GIT_PAGER=cat", "LC_ALL=C")
	}
	return append(out, GitHardening()...)
}
