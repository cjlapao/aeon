package execenv

// The environment for the ONE child a CLI verb starts: the person's own editor
// under `aeon blueprint edit` (S48 T11's verb, accounted for here when S59's
// spawn guard found it unaccounted for after the S48 merge).
//
// It is a different animal from a step command and from git: nobody is
// executing repository content, and there is no model on the other end — the
// person typed `aeon blueprint edit` at a terminal and named their own
// `$VISUAL`/`$EDITOR`. So the child is not confined to a scrubbed step's list
// (an editor without `TERM` is unusable, and a GUI editor without
// `DISPLAY`/`XDG_RUNTIME_DIR` never opens), and it is not handed the parent's
// whole environment either, because "the CLI process happens not to hold any
// credentials today" is a claim about how the process was started rather than
// a property of the child. A closed allowlist, like every other child here.
//
// The list is an allowlist of NAMES, values copied from what the caller hands
// in; the deny list is applied on top so a name that is credential-shaped by
// any of its four shapes is dropped even if it appears in the list. Nothing
// reads the ambient environment from in here (EV5): the caller passes a list.

import (
	"sort"
	"strings"
)

// editorNames are the names on top of the default passthrough. Each is here
// because an editor breaks without it, not because it is a familiar variable:
//
//   - TERM, COLORTERM: a full-screen editor is a terminal program;
//   - DISPLAY, WAYLAND_DISPLAY, XDG_RUNTIME_DIR: the GUI editors — `code -w`
//     is the documented example in blueprint_edit.go's own comment, and it
//     does not open without them.
//
// `EDITOR` and `VISUAL` are deliberately NOT carried: the verb has already
// resolved them into argv (blueprintEditorArgs), the child IS the editor, and
// `EDITOR` sits in the deny list's loader family for a real reason — a child
// that shells out to `git commit` would run whatever it names. `SSH_AUTH_SOCK`
// is not here either: no editor needs the agent.
var editorNames = []string{
	"TERM", "COLORTERM",
	"DISPLAY", "WAYLAND_DISPLAY", "XDG_RUNTIME_DIR",
}

// editorDenyExceptions is the closed set of names that reach the editor child
// even though the deny list refuses them. It is a map rather than a comment
// because EV4 says the deny list wins, and the only honest way to widen that
// for one non-model-reachable child is to name the widening, one literal name
// at a time, and let a test compare the two sets.
//
// Both are exact-name refusals for a step command (EV1's scope is every spawn a
// model can reach: a model-driven child must never see the person's graphical
// session). Neither is a credential: this child is the person's own editor,
// launched by the person's own typed verb, and a GUI editor cannot open a
// window without its display. A credential-worded or namespaced name is in
// neither set and stays refused absolutely.
var editorDenyExceptions = map[string]bool{
	"DISPLAY":         true,
	"WAYLAND_DISPLAY": true,
}

// EditorNames returns the names on top of the default passthrough, so a test
// and the guard can read the list instead of trusting a comment.
func EditorNames() []string {
	out := make([]string, len(editorNames))
	copy(out, editorNames)
	return out
}

// EditorDenyExceptions reports the named widening, for the same reason.
func EditorDenyExceptions() []string {
	out := make([]string, 0, len(editorDenyExceptions))
	for n := range editorDenyExceptions {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// EditorEnv builds the environment for the person's editor child. The ambient
// list arrives as data — the `NAME=VALUE` strings the CLI process was started
// with — and what comes back is an allowlist, sorted, with no credential- or
// loader-shaped name in it.
//
// It never refuses: an editor with no TERM still opens, and a verb that
// refuses to launch because a variable is unset would be a worse answer than
// one that launches with what it has.
func EditorEnv(ambient []string) []string {
	gathered := map[string]string{}
	deny := Deny()

	want := func(name string) bool {
		for _, n := range DefaultPassthrough() {
			if strings.HasSuffix(n, "*") {
				if strings.HasPrefix(name, strings.TrimSuffix(n, "*")) {
					return true
				}
				continue
			}
			if name == n {
				return true
			}
		}
		for _, n := range editorNames {
			if name == n {
				return true
			}
		}
		return false
	}

	for _, kv := range ambient {
		name, value, ok := strings.Cut(kv, "=")
		if !ok || name == "" {
			continue // a malformed entry carries nothing worth copying
		}
		if !want(name) {
			continue
		}
		if deniedBy(deny, name, false) != "" && !editorDenyExceptions[name] {
			continue // an allowlist name that is shaped like a credential loses
		}
		gathered[name] = value
	}

	names := make([]string, 0, len(gathered))
	for n := range gathered {
		names = append(names, n)
	}
	sort.Strings(names)

	out := make([]string, 0, len(names))
	for _, n := range names {
		out = append(out, n+"="+gathered[n])
	}
	return out
}
