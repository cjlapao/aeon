package execenv

// ProvisionIdent is the identity a non-host backend needs to find the resource
// it is about to create again later (S59 T9/T12, EV12). The generic
// BackendExecutor seam used to carry only the environment and the child
// environment, which meant the container backend could create a container it
// could never re-identify: the provision ledger's whole point is that a daemon
// restart finds the container again and tears it down or reconciles it, and it
// can only do that with the component root, the loop, and the step that asked.
//
// It is a separate type rather than fields on ChildEnv because the child
// environment is about what the command SEES and this is about what the daemon
// OWNS: a host-scrubbed command needs none of it, a container needs all of it,
// and a backend that cannot see the difference is how F-1 (container-bound
// steps refusing with provision-identity) survived a green test suite — the
// package's own tests hand-built the identity while the daemon's seam had no
// place to put one.
type ProvisionIdent struct {
	// ComponentRoot is the workspace root the binding resolved under.
	ComponentRoot string
	// LoopSlug is the loop the step belongs to. The container is one per loop,
	// so an empty slug is not "some loop" — it is an unusable provision, and the
	// backend refuses it out loud rather than creating an orphan.
	LoopSlug string
	// Owner is the step that asked, kept for the log line and the ledger note.
	Owner string
}

// Missing reports the fields that make a provision unfindable. The empty answer
// means the identity is complete.
func (p ProvisionIdent) Missing() []string {
	var out []string
	if p.ComponentRoot == "" {
		out = append(out, "component root")
	}
	if p.LoopSlug == "" {
		out = append(out, "loop slug")
	}
	if p.Owner == "" {
		out = append(out, "owner")
	}
	return out
}
