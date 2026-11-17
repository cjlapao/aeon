// Package errs is the one error shape the whole product speaks in (S23 C16).
//
// C9 is the whole argument: "the audience cannot ask the author." A
// convention that errors should have three parts reliably produces errors
// with two — the one that gets dropped is always the third. So the third
// part is not a convention, it is a field the type will not construct
// without, and there is no literal that names the other two without it.
//
// The three parts are what happened, what it means, and what to do. A
// message that names the first two and omits the third is the failure C9
// was written about, and the type here makes that failure unbuildable.
package errs

// Error is the product's error shape. All three fields are required, and
// they are unexported so the only way to make one is New: a composite
// literal in another package cannot name what/why/do, and one that omits
// do cannot exist. That is the difference between a convention and a rule
// — the rule is the compiler.
type Error struct {
	what string
	why  string
	do   string
}

// New is the only constructor. There is no zero-value path and no literal
// that names two fields without the third, because the fields are not the
// package's business to name.
func New(what, why, do string) Error {
	return Error{what: what, why: why, do: do}
}

// What is the first part: what happened.
func (e Error) What() string { return e.what }

// Why is the second part: what it means.
func (e Error) Why() string { return e.why }

// Do is the third part: what to do. An action, a command, a path or a
// named next step — never "check your configuration" (O4, C9).
func (e Error) Do() string { return e.do }

// Error renders the three parts. A caller that greps the prose still gets
// all three, and a caller that branches on nothing reads the same text a
// human would.
func (e Error) Error() string {
	return e.what + "\n  why:  " + e.why + "\n  do:   " + e.do
}

// Kinded carries a kind (what a caller branches on) alongside the three
// parts (what a human reads). S16's five forge kinds, S19's four narrow
// kinds and S22's four availability reasons survive as the kind; the
// shape is what the message says, the kind is what the code decides on,
// and both ride the same value.
type Kinded struct {
	Kind string
	Err  Error
}

// NewKinded is the only constructor for Kinded. It is the one place a kind
// and the three parts are joined, and it takes all four, so no Kinded can
// be built with a missing part the way the unexported Error cannot.
func NewKinded(kind, what, why, do string) Kinded {
	return Kinded{Kind: kind, Err: New(what, why, do)}
}

func (k Kinded) Error() string { return k.Kind + ": " + k.Err.Error() }

// What/Why/Do let a renderer or a caller reach any of the four
// without knowing the others exist.
func (k Kinded) What() string { return k.Err.What() }
func (k Kinded) Why() string  { return k.Err.Why() }
func (k Kinded) Do() string   { return k.Err.Do() }
