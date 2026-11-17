// Package stepctx carries the step's identity through a context.Context.
//
// It is its own package, stdlib-only, because the key must be shared by
// `runtime` (which sets it at the one dispatch site), `tools` (which reads it
// inside a tool) and `daemon` (which wires the readers) — and any of those
// imports living in one of them is an import cycle. A context key is the
// smallest thing that deserves a package of its own for exactly this reason:
// unexported where it is used means unreachable where it is needed.
//
// The value is the step ROW's id (OL3: identity is stamped, not argued). A
// tool that must know which step called it reads this and trusts the runtime,
// never the tool arguments — an argument a model can type is not an
// identity. A step whose row does not exist yet (an ad-hoc step before its
// insert) simply carries no id, and tools reading an absent id fail closed.
package stepctx

import "context"

type key struct{}

// WithStepID returns ctx carrying the step row id. Zero is not an identity:
// StepID reports it absent.
func WithStepID(ctx context.Context, id int64) context.Context {
	return context.WithValue(ctx, key{}, id)
}

// StepID reads the step row id, and whether one was there at all.
func StepID(ctx context.Context) (int64, bool) {
	v, ok := ctx.Value(key{}).(int64)
	return v, ok && v > 0
}

type approvedKey struct{}

// ApprovedCall is the one call a person approved when a permission question was answered "approve":
// the tool and, for bash, the exact command. It rides the context of that single dispatch only.
type ApprovedCall struct {
	Tool    string
	Command string
}

// WithApprovedCall marks ctx as carrying a person's approval for exactly this call. Only the runtime's
// resume path sets it, from a stored question a person answered; nothing a model wrote reaches it.
func WithApprovedCall(ctx context.Context, tool, command string) context.Context {
	return context.WithValue(ctx, approvedKey{}, ApprovedCall{Tool: tool, Command: command})
}

// ApprovedFor reports whether ctx carries an approval for this exact tool and command. A command that
// differs by one character is not approved.
func ApprovedFor(ctx context.Context, tool, command string) bool {
	a, ok := ctx.Value(approvedKey{}).(ApprovedCall)
	return ok && a.Tool == tool && a.Command == command
}
