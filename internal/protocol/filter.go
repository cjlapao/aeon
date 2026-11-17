package protocol

import "strings"

// # The filter (T4, O1)
//
// One daemon multiplexes every project on the machine. A filter that silently
// matches everything LOOKS like it works — events arrive, the client renders
// them — while shipping every repository's activity to every client, which on a
// busy machine is most of the traffic, paid for on both sides and then
// discarded.
//
// So: subscriptions rather than server push. The client says what it wants,
// once, and the daemon sends only that.

// Validate rejects a filter that cannot mean what its author intended (O1).
//
// # Why an invalid wildcard is an ERROR and not a literal
//
// `kinds` matches exactly, or by a single trailing `*`. Treating `*.started` as
// a literal kind name would be defensible and is wrong, because **a filter that
// silently matches nothing is the hardest bug in this slice to diagnose from
// outside**: the client is connected, the subscription exists, and events
// simply never arrive. Every other wildcard form is therefore named and
// refused:
//
//	loop.started   ok — exact
//	loop.*         ok — one trailing star
//	*              ok — everything, stated explicitly
//	*.started      refused — leading
//	loop.*.x       refused — interior
//	loop.?tarted   refused — single-character wildcard
//	loop.[abc]     refused — character class
//	loop.**        refused — two stars
func (f Filter) Validate() error {
	for _, kind := range f.Kinds {
		if kind == "" {
			return ErrInvalidFilter.WithWhat("`kinds` contains an empty string")
		}
		if err := validateKindPattern(kind); err != nil {
			return err
		}
	}
	return nil
}

func validateKindPattern(kind string) error {
	// One trailing star is the whole wildcard dialect. A glob dialect in a
	// filter is a parser and a security question; a prefix match is neither.
	body := kind
	if strings.HasSuffix(kind, "*") {
		body = kind[:len(kind)-1]
	}

	if strings.Contains(body, "*") {
		return ErrInvalidFilter.WithWhat(
			"kind pattern %q has a `*` that is not a single trailing one; "+
				"use an exact kind or one trailing star, as in `loop.*`", kind)
	}
	for _, bad := range []string{"?", "[", "]", "{", "}"} {
		if strings.Contains(kind, bad) {
			return ErrInvalidFilter.WithWhat(
				"kind pattern %q contains %q; the only wildcard is a single trailing `*`", kind, bad)
		}
	}
	return nil
}

// Matches reports whether an event belongs to this filter.
//
// An empty filter matches everything — stated explicitly, because that is what
// the app's dashboard subscribes with, and because an empty filter that matched
// nothing would make `aeon watch` with no flags silently useless.
func (f Filter) Matches(kind, componentRoot string) bool {
	return f.matchesComponent(componentRoot) && f.matchesKind(kind)
}

// matchesComponent compares EXACTLY. No prefix matching on paths: `/repo` must
// not match `/repo-backup`, which is the kind of near-miss that leaks one
// project's activity into another's client and is invisible until it matters.
//
// A daemon-wide event (empty componentRoot, such as `daemon.started`) reaches a
// project-scoped subscriber: it is about the daemon serving that project, and
// withholding it would make a client's view of the daemon's lifecycle depend on
// whether it had scoped itself.
// MatchesEvent is Matches for an event whose notify hint is known: a NotifyOnly filter takes any hinted event, and an unhinted one
// only if a named kind matches.
func (f Filter) MatchesEvent(kind, componentRoot string, hinted bool) bool {
	if !f.matchesComponent(componentRoot) {
		return false
	}
	if f.NotifyOnly {
		if hinted {
			return true
		}
		return len(f.Kinds) > 0 && f.matchesKind(kind)
	}
	return f.matchesKind(kind)
}

func (f Filter) matchesComponent(componentRoot string) bool {
	if f.ComponentRoot == "" {
		return true
	}
	if componentRoot == "" {
		return true
	}
	return f.ComponentRoot == componentRoot
}

func (f Filter) matchesKind(kind string) bool {
	if len(f.Kinds) == 0 {
		return true
	}
	for _, pattern := range f.Kinds {
		if matchKind(pattern, kind) {
			return true
		}
	}
	return false
}

func matchKind(pattern, kind string) bool {
	if pattern == "*" {
		return true
	}
	if strings.HasSuffix(pattern, "*") {
		return strings.HasPrefix(kind, pattern[:len(pattern)-1])
	}
	return pattern == kind
}
