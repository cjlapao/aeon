package execenv

// Deciding, not guessing: what a spec may ask of a backend, and what a name
// may be (EV4, EV11).
//
// `Validate` returns EVERY problem, never the first. A form that reports one
// error per save makes a person play whack-a-mole with a security record, and
// the risk is not annoyance — it is that they stop reading. Each problem is a
// three-part sentence (what / why / do), because "invalid" is not actionable
// and "the variable AWS_PROFILE is denied because it is in a vendor namespace;
// give it a secret reference" is.

import (
	"fmt"
	"regexp"
	"strings"
)

// The closed refusal vocabulary (EV11). A reason outside this set means a
// surface invented a state nobody designed, so the kinds are constants and a
// test compares them with what the doors emit.
const (
	RefusalEnvironmentUnbound     = "environment-unbound"
	RefusalEnvironmentUnavailable = "environment-unavailable"
	RefusalSecretUnresolvable     = "secret-unresolvable"
	RefusalProvisionFailed        = "provision-failed"
	RefusalServiceUnhealthy       = "service-unhealthy"
	// RefusalSpecInvalid is validation-time only: it never appears on a step
	// record, because a step that never started was not refused a spec. It is
	// here rather than in a separate type so every three-part sentence in the
	// slice is one type with one renderer.
	RefusalSpecInvalid = "spec-invalid"
)

// Refusal is a decision that something does not run, said in three parts
// (EV11). It is a value, not an error: "this transition is not allowed" and
// "the disk is full" are different things and a caller must not have to guess
// which one it got (DB19).
type Refusal struct {
	Kind string `json:"kind"`
	What string `json:"what"`
	Why  string `json:"why"`
	Do   string `json:"do"`
}

// NewRefusal builds the three parts. All three are required by construction
// convention: an empty Do is the shape of a refusal that tells somebody to
// give up.
func NewRefusal(kind, what, why, do string) *Refusal {
	return &Refusal{Kind: kind, What: what, Why: why, Do: do}
}

// Sentence renders the refusal the way every surface prints it — one string,
// three parts, no paraphrase (the one-formatter rule the policy verdict
// follows).
func (r *Refusal) Sentence() string {
	if r == nil {
		return ""
	}
	parts := make([]string, 0, 3)
	for _, p := range []string{r.What, r.Why, r.Do} {
		if p != "" {
			parts = append(parts, p)
		}
	}
	return "refused: " + r.Kind + " · " + strings.Join(parts, " · ")
}

// Error lets a Refusal satisfy error where an API boundary demands one,
// without giving anyone a second renderer to drift.
func (r *Refusal) Error() string { return r.Sentence() }

// varNamePattern is the POSIX shape, with the length bound EV4 sets beside it.
var varNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// refuseName is the deny list applied to one name in one source. `inSet` turns
// on the set-only refusals (PATH may pass through, may not be set).
func refuseName(name, source string, inSet bool) *Refusal {
	if r := refuseVarNameShape(name); r != nil {
		return r
	}
	d := Deny()
	upper := strings.ToUpper(name)
	if hit := deniedBy(d, upper, inSet); hit != "" {
		return NewRefusal(RefusalSpecInvalid,
			"the variable "+name+" is denied where it appears ("+source+")",
			"it matched the deny rule "+hit+"; the deny list wins even when an operator named the variable explicitly, "+
				"because what it refuses are the daemon's own credentials, the agent handles and the loader/shell-injection "+
				"variables that turn an environment into code execution",
			"give it a secret reference instead (secretref.create, then name it in env.secrets) — that path injects the "+
				"value at spawn and redacts it from the output; if the child genuinely needs a non-credential value from "+
				"that namespace, name the exact variable you need in a new reference")
	}
	return nil
}

// refuseInjectionName is the deny question for the ONE name a credential is
// allowed to arrive under: the variable a secret reference injects. It is a
// separate function because its answer differs, and the difference is the
// plane's argument rather than an oversight (S59 T6, recorded as a deviation on
// T4's rule):
//
//   - The credential WORDS (`TOKEN`, `KEY`, `SECRET`) and the vendor
//     namespaces (`AWS_`, `GH_`, `OPENAI_`) are NOT refused here. They are
//     refused everywhere else because copying such a name out of the daemon's
//     environment is how a token leaks by accident — and the remedy
//     `refuseName` itself offers for that refusal is "give it a secret
//     reference instead". A name that looks like a credential is what a
//     correct answer looks like when the value IS one: delivered on purpose,
//     resolved at spawn, redacted from the child's output, and named in the
//     audit. Refusing it here would close the only door EV7 opens, and a plane
//     whose sanctioned path does not work gets worked around by re-widening an
//     allowlist — the failure this whole slice exists to prevent.
//   - The loader/shell-injection names, the exact agent and session handles,
//     the set-only names and the daemon's own `AEON_` namespace ARE refused
//     here as everywhere: an injected `LD_PRELOAD` is code execution as the
//     daemon's user, and an injected name in the daemon's own namespace would
//     forge what EV14 derives. Where the name is a MECHANISM rather than a
//     secret, the deny list still wins (PL4).
func refuseInjectionName(name, reference string) *Refusal {
	if r := refuseVarNameShape(name); r != nil {
		return r
	}
	d := Deny()
	upper := strings.ToUpper(name)
	for _, l := range d.Loader {
		if upper == l {
			return refuseInjection(name, reference, "the loader/shell-injection variable "+l)
		}
		if strings.HasSuffix(l, "*") && strings.HasPrefix(upper, strings.TrimSuffix(l, "*")) {
			return refuseInjection(name, reference, "the loader/shell-injection prefix "+l)
		}
	}
	for _, e := range d.Exact {
		if upper == e {
			return refuseInjection(name, reference, "the exact agent/session handle "+e)
		}
	}
	for _, s := range d.SetOnly {
		if upper == s {
			return refuseInjection(name, reference, "the set-only name "+s)
		}
	}
	for _, p := range d.Prefixes {
		// Only the daemon's OWN namespace stays denied for an injection: an
		// injected `AEON_SERVICE_DB_HOST` would forge what EV14 derives, and an
		// injected `AEON_*` name is a control variable of this process. A
		// vendor's namespace is a different thing — `AWS_SECRET_ACCESS_KEY` is
		// the name the child's tool reads, and it is the reason the reference
		// exists.
		if p == aeonNamespace && strings.HasPrefix(upper, p) {
			return refuseInjection(name, reference, "the daemon's own namespace "+p)
		}
	}
	return nil
}

// refuseInjection is the refusal behind every arm above, in one place so the
// four arms cannot drift apart in tone or detail, and so the sentence always
// names the reference whose injection was refused (the person edits the
// reference, not the spec, and the sentence has to send them to the right door).
func refuseInjection(name, reference, rule string) *Refusal {
	return NewRefusal(RefusalSpecInvalid,
		"the secret reference "+reference+" may not inject "+name+" — it matched "+rule,
		"an injected name is still a name the child's shell exports and its programs obey; "+
			"this is the one place a credential is allowed to arrive under a credential-shaped name, and a mechanism is not a credential",
		"give the reference an injectAs that names the secret (AWS_SECRET_ACCESS_KEY, NETLIFY_TOKEN) and not a loader variable, an agent handle, the daemon's own AEON_ namespace, or a set-only name like PATH")
}

// refuseVarNameShape is the POSIX shape check both name doors share: an empty,
// over-long or unexportable name is refused whatever is being injected or
// copied, and refusing it in one place is what keeps the two doors from
// disagreeing about what a name is.
func refuseVarNameShape(name string) *Refusal {
	if name == "" {
		return NewRefusal(RefusalSpecInvalid, "a variable name is empty",
			"a child cannot be given a nameless variable", "remove the entry")
	}
	if len(name) > MaxVarNameLen {
		return NewRefusal(RefusalSpecInvalid, "the variable name "+name+" is longer than "+
			fmt.Sprint(MaxVarNameLen)+" characters",
			"a name that long is not a variable a shell can be expected to handle",
			"shorten it")
	}
	if !varNamePattern.MatchString(name) {
		return NewRefusal(RefusalSpecInvalid, "the variable name "+name+" is not a legal variable name",
			"a name must be a letter or underscore followed by letters, digits or underscores, or the shell will not export it",
			"rename it (for example "+suggestName(name)+")")
	}
	return nil
}

// deniedBy returns the rule text the name matched, or "" when it matched none.
func deniedBy(d DenyList, upper string, inSet bool) string {
	for _, w := range d.Words {
		if strings.Contains(upper, w) {
			return "the credential word " + w
		}
	}
	for _, p := range d.Prefixes {
		if strings.HasPrefix(upper, p) && !exempted(upper, d.Exempt) {
			return "the namespace prefix " + p
		}
	}
	for _, e := range d.Exact {
		if upper == e {
			return "the exact name " + e
		}
	}
	for _, l := range d.Loader {
		if upper == l {
			return "the loader/shell-injection variable " + l
		}
		if strings.HasSuffix(l, "*") && strings.HasPrefix(upper, strings.TrimSuffix(l, "*")) {
			return "the loader/shell-injection prefix " + l
		}
	}
	if inSet {
		for _, s := range d.SetOnly {
			if upper == s {
				return "the set-only name " + s
			}
		}
	}
	return ""
}

// exempted reports whether a name sits in a namespace the deny prefixes
// explicitly let through (EV14's derived service variables).
func exempted(upper string, exempt []string) bool {
	for _, e := range exempt {
		if strings.HasPrefix(upper, e) {
			return true
		}
	}
	return false
}

// refusePrefix checks a `PREFIX_*` passthrough pattern's prefix. A pattern
// whose prefix is itself denied matches nothing that would be allowed anyway,
// and refusing it out loud beats a list that silently matches nothing.
func refusePrefix(prefix string) *Refusal {
	if prefix == "" {
		return NewRefusal(RefusalSpecInvalid, "the passthrough pattern \"*\" matches the entire daemon environment",
			"that is the inheritance this slice exists to close, expressed as a wildcard",
			"list the names or the prefixes you need, for example LC_*")
	}
	d := Deny()
	if hit := deniedBy(d, strings.ToUpper(prefix)+"_DENYPROBE", false); hit != "" {
		return NewRefusal(RefusalSpecInvalid, "the passthrough pattern "+prefix+"* is denied",
			"its prefix matched the deny rule "+hit+"; a pattern in a denied namespace matches only things "+
				"that would be refused anyway",
			"remove it, or name the exact variable you need through a secret reference")
	}
	return nil
}

// suggestName turns an illegal name into the closest legal one, so the refusal
// tells somebody what to type rather than only what not to.
func suggestName(name string) string {
	var b strings.Builder
	for i, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r == '_':
			b.WriteRune(r)
		case r >= '0' && r <= '9' && i > 0:
			b.WriteRune(r)
		case i == 0 && (r >= '0' && r <= '9'):
			b.WriteString("_")
			b.WriteRune(r)
		default:
			b.WriteString("_")
		}
	}
	out := b.String()
	if out == "" {
		return "AEON_EXAMPLE"
	}
	if len(out) > MaxVarNameLen {
		out = out[:MaxVarNameLen]
	}
	return out
}

// ValidateOpts carries what a spec's validity depends on but cannot know:
// which drivers this daemon actually registered, and the wall ceiling it must
// not exceed.
type ValidateOpts struct {
	// Drivers lists the backend names a driver is registered for. `vm` is a
	// slot until one registers, and the registry — not a flag, not a string
	// comparison in a UI — is the truth (EV3).
	Drivers []string
	// WallCeilingSeconds is tools.BashTimeout in seconds. Zero means the
	// default from this package (F4: a spec may only shorten the ceiling).
	WallCeilingSeconds int
}

// Validate reports every problem with a spec under a backend (EV3, EV4). An
// empty slice means it may be written.
//
// It is a function and not a method because the same spec is valid under
// `container` and invalid under `host-scrubbed`: validity is a property of the
// pair.
func Validate(spec Spec, backend string, opts ValidateOpts) []Refusal {
	var out []Refusal
	add := func(r *Refusal) {
		if r != nil {
			out = append(out, *r)
		}
	}

	if BackendStrength(backend) < 0 {
		add(NewRefusal(RefusalSpecInvalid, "the backend "+backend+" is not a backend this daemon knows",
			"the closed set is host-inherit, host-scrubbed, container and vm",
			"pick one of those, or register a driver for it first"))
		return out // everything below would be noise against a name that does not exist
	}
	if backend == BackendVM && !registered(opts.Drivers, backend) {
		add(NewRefusal(RefusalSpecInvalid, "no "+BackendVM+" driver is built into this daemon",
			"an environment of a backend with no driver can never run, and a record that can never run is a wrong record",
			"create it once a driver registers, or bind the component to a built backend"))
	}

	switch spec.Home {
	case HomeReal, HomeScratch:
	default:
		add(NewRefusal(RefusalSpecInvalid, "home must be "+HomeReal+" or "+HomeScratch+", got "+fmt.Sprintf("%q", spec.Home),
			"home decides which directory the child's `~` is, and there is no third answer",
			"set home explicitly"))
	}
	if spec.Home == HomeScratch && backend != BackendContainer && backend != BackendVM {
		add(NewRefusal(RefusalSpecInvalid, "home: scratch is refused on the "+backend+" backend",
			"a scratch HOME needs a directory the lifecycle ledger creates and removes, and a host child has no provision to own it (D5)",
			"use home: real on a host, or bind this to the container backend"))
	}

	// Egress.
	if !contains(EgressModes(), spec.Egress.Mode) {
		add(NewRefusal(RefusalSpecInvalid, "egress.mode "+fmt.Sprintf("%q", spec.Egress.Mode)+" is not one of "+
			strings.Join(EgressModes(), ", "),
			"the mode is the whole network posture, so a typo must not silently mean something",
			"name one of the three modes"))
	}
	if len(spec.Egress.Allow) > 0 {
		add(NewRefusal(RefusalSpecInvalid, "egress.allow is refused in this slice",
			"a host allowlist needs an egress proxy, and refusing at the write is honest where accepting the field and ignoring it would be a lie (EV13's staged half)",
			"leave egress.allow empty; run with egress.mode: none on the container backend when isolation is what you need"))
	}
	if spec.Egress.Mode == "none" && !HasCapability(backend, CapEgressNone) {
		add(NewRefusal(RefusalSpecInvalid, "egress.mode: none cannot be enforced on the "+backend+" backend",
			"nothing on a host stops the child opening a socket, and a limit the backend cannot enforce is a limit that will be believed anyway",
			"use the container backend for egress: none, or set egress.mode: unenforced and read the honesty line"))
	}

	// Limits.
	if l := spec.Limits; l.WallSeconds != nil {
		ceiling := opts.WallCeilingSeconds
		if ceiling <= 0 {
			ceiling = MaxWallSeconds
		}
		if *l.WallSeconds < 1 || *l.WallSeconds > ceiling {
			add(NewRefusal(RefusalSpecInvalid, "limits.wallSeconds must be between 1 and "+fmt.Sprint(ceiling)+
				", got "+fmt.Sprint(*l.WallSeconds),
				"a wall limit may only shorten the mandatory command timeout, never lengthen it (F4)",
				"lower it, or omit it and take the daemon's ceiling"))
		}
	}
	if spec.Limits.MemoryMiB != nil && !HasCapability(backend, CapResourceLimits) {
		add(NewRefusal(RefusalSpecInvalid, "limits.memoryMiB cannot be enforced on the "+backend+" backend",
			"a host child is bounded by the machine, and a memory number written on a host record is a number that does nothing",
			"drop it, or use the container backend, which passes it to the runtime"))
	}
	if spec.Limits.MemoryMiB != nil && *spec.Limits.MemoryMiB < MinMemoryMiB {
		add(NewRefusal(RefusalSpecInvalid, "limits.memoryMiB must be at least "+fmt.Sprint(MinMemoryMiB)+
			", got "+fmt.Sprint(*spec.Limits.MemoryMiB),
			"below that nothing useful runs, and the container would be OOM-killed before a command reported anything",
			"raise it"))
	}
	if c := spec.Limits.CPUs; c != nil {
		if *c < MinCPUs || *c > MaxCPUs {
			add(NewRefusal(RefusalSpecInvalid, "limits.cpus must be between "+fmt.Sprint(MinCPUs)+" and "+
				fmt.Sprint(MaxCPUs)+", got "+fmt.Sprint(*c),
				"it is passed straight to the runtime, which rejects the range outside this",
				"pick a value in the range"))
		}
		if !HasCapability(backend, CapResourceLimits) {
			add(NewRefusal(RefusalSpecInvalid, "limits.cpus cannot be enforced on the "+backend+" backend",
				"there is no cgroup to set on a host child",
				"drop it, or use the container backend"))
		}
	}

	// Image.
	if backend == BackendContainer {
		if spec.Image == nil || strings.TrimSpace(spec.Image.Ref) == "" {
			add(NewRefusal(RefusalSpecInvalid, "a container environment requires image.ref",
				"the container backend starts one image; with none named there is nothing to start",
				"set image.ref, digest-pinned if you can (a floating tag is accepted and warned about in reads)"))
		} else {
			if spec.Image.Pull != "" && !contains(PullPolicies(), spec.Image.Pull) {
				add(NewRefusal(RefusalSpecInvalid, "image.pull "+fmt.Sprintf("%q", spec.Image.Pull)+
					" is not one of "+strings.Join(PullPolicies(), ", "),
					"pull decides whether a missing image is an error or a download, and a typo must not guess",
					"use "+PullNever+" (the default) or "+PullMissing))
			}
		}
	} else if spec.Image != nil {
		add(NewRefusal(RefusalSpecInvalid, "image is a container field and this environment runs on "+backend,
			"an image on a host environment is a field nobody reads",
			"set image to null, or move to the container backend"))
	}

	// Services.
	if len(spec.Services) > 0 && !HasCapability(backend, CapServices) {
		add(NewRefusal(RefusalSpecInvalid, "services are a container feature and this environment runs on "+backend,
			"a service container is started, health-checked and torn down by the provisioning ledger, which only the container backend has (EV14)",
			"drop services, or bind this to the container backend"))
	}
	if len(spec.Services) > MaxServices {
		add(NewRefusal(RefusalSpecInvalid, "a loop may declare at most "+fmt.Sprint(MaxServices)+
			" services, got "+fmt.Sprint(len(spec.Services)),
			"each is a container started and health-checked before the first command, so an unbounded list is an unbounded startup",
			"split the topology or drop what the loop does not use"))
	}
	seenSvc := map[string]bool{}
	for _, s := range spec.Services {
		if s.Name == "" || !servicePattern.MatchString(s.Name) {
			add(NewRefusal(RefusalSpecInvalid, "the service name "+fmt.Sprintf("%q", s.Name)+" is not usable",
				"the name becomes a container name and the variable AEON_SERVICE_<NAME>_HOST, so it is a lowercase slug",
				"use letters, digits and dashes, starting with a letter"))
		}
		if seenSvc[s.Name] {
			add(NewRefusal(RefusalSpecInvalid, "two services are named "+s.Name,
				"one name addresses two containers, so which one the loop reaches is undefined",
				"give them distinct names"))
		}
		seenSvc[s.Name] = true
		if strings.TrimSpace(s.Image) == "" {
			add(NewRefusal(RefusalSpecInvalid, "service "+s.Name+" has no image",
				"a service is a container, and a container starts an image",
				"set image"))
		}
		if s.Port <= 0 || s.Port > 65535 {
			add(NewRefusal(RefusalSpecInvalid, "service "+s.Name+" has port "+fmt.Sprint(s.Port),
				"the port is what AEON_SERVICE_"+strings.ToUpper(s.Name)+"_PORT answers and what the health check dials",
				"set a port between 1 and 65535"))
		}
		for name := range s.Env {
			if r := refuseName(name, "service:"+s.Name, true); r != nil {
				add(r)
			}
		}
	}

	// Preview is reserved.
	if spec.Preview != nil {
		add(NewRefusal(RefusalSpecInvalid, "preview is specified, not built",
			"a preview deployment needs a build step, a URL, a TTL and an egress-in allowance, and none of that is designed yet (EV14)",
			"leave preview null"))
	}

	// Names, in every place a name can arrive.
	if len(spec.Env.Passthrough) > MaxPassthroughLen {
		add(NewRefusal(RefusalSpecInvalid, "env.passthrough has more than "+fmt.Sprint(MaxPassthroughLen)+" entries",
			"an allowlist that long is not a list anybody can read, and reading it is the point",
			"narrow it to what the commands actually use"))
	}
	for _, entry := range spec.Env.Passthrough {
		name := strings.TrimSpace(entry)
		if name == "" {
			continue
		}
		if strings.HasSuffix(name, "*") {
			add(refusePrefix(strings.TrimSuffix(name, "*")))
			continue
		}
		add(refuseName(name, SourcePassthrough, false))
	}

	if len(spec.Env.Set) > MaxSetEntries {
		add(NewRefusal(RefusalSpecInvalid, "env.set has more than "+fmt.Sprint(MaxSetEntries)+" entries",
			"a spec is a record a person reads; a wall of literals is not reviewable",
			"move what belongs into a secret reference or drop it"))
	}
	for name, value := range spec.Env.Set {
		add(refuseName(name, SourceSet, true))
		if len(value) > MaxSetValueBytes {
			add(NewRefusal(RefusalSpecInvalid, "the value of env.set "+name+" is longer than "+
				fmt.Sprint(MaxSetValueBytes)+" bytes",
				"env.set is for CI=1 and TERM=dumb; a value that long is a payload, and one that looks like a credential is refused by name",
				"give it a secret reference, or shorten it"))
		}
		if credentialShaped(name) && !deniedAlready(name) {
			add(NewRefusal(RefusalSpecInvalid, "the variable "+name+" is credential-shaped",
				"a literal in a spec is stored, shown and audited in the clear, so a name that says credential must not be set by literal",
				"give it a secret reference (secretref.create with source daemon-env or key-file), then name it in env.secrets"))
		}
	}

	if len(spec.Env.Secrets) > MaxSecretRefs {
		add(NewRefusal(RefusalSpecInvalid, "env.secrets names more than "+fmt.Sprint(MaxSecretRefs)+
			" references",
			"every one is resolved and redacted at each spawn, and a step that injects dozens of secrets is a step that can leak dozens",
			"narrow the list to what these commands use"))
	}
	seenRef := map[string]bool{}
	for _, ref := range spec.Env.Secrets {
		if strings.TrimSpace(ref) == "" {
			add(NewRefusal(RefusalSpecInvalid, "an empty secret reference name appears in env.secrets",
				"it resolves to nothing and reads as a reference that exists",
				"remove it"))
			continue
		}
		if seenRef[ref] {
			add(NewRefusal(RefusalSpecInvalid, "the secret reference "+ref+" is named twice",
				"one reference listed twice injects one variable twice and hides a mistake in the spec",
				"remove the duplicate"))
		}
		seenRef[ref] = true
	}
	return out
}

// credentialShaped is EV4's heuristic, and the refusal that uses it says it is
// one: the real mechanism is EV7, this is the tripwire that stops the obvious
// mistake. The list is the deny words plus what config marks a credential.
func credentialShaped(name string) bool {
	n := strings.ToLower(name)
	for _, mark := range []string{"key", "token", "secret", "apikey", "api_key", "password",
		"passwd", "credential", "auth", "cookie", "private"} {
		if strings.Contains(n, mark) {
			return true
		}
	}
	return false
}

// deniedAlready reports whether the deny list already covers the name, so a
// name is not refused twice by two rules that say almost the same thing.
func deniedAlready(name string) bool { return deniedBy(Deny(), strings.ToUpper(name), true) != "" }

var servicePattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)

func registered(drivers []string, backend string) bool { return contains(drivers, backend) }

func contains(hay []string, needle string) bool {
	for _, h := range hay {
		if h == needle {
			return true
		}
	}
	return false
}

// Warnings are the things that are allowed and worth saying out loud (EV4,
// D8). A warning never refuses a write: refusing a floating tag would push
// people to disable the check rather than pin the image.
func Warnings(spec Spec, backend string) []string {
	var out []string
	for _, entry := range spec.Env.Passthrough {
		up := strings.ToUpper(strings.TrimSpace(entry))
		for _, p := range ProxyWarnPatterns() {
			if strings.Contains(up, p) {
				out = append(out, "the variable "+entry+" may carry credentials inside its value (a proxy URL often does) — "+
					"a secret reference keeps it out of the record and redacts it from the output")
			}
		}
	}
	if spec.Image != nil && strings.Contains(spec.Image.Ref, ":") &&
		!strings.Contains(spec.Image.Ref, "@") {
		out = append(out, "the image "+spec.Image.Ref+" is a floating tag — a rebuild of that tag changes what your "+
			"loops run; a digest-pinned ref (@sha256:…) does not")
	}
	if backend == BackendContainer && spec.Limits.MemoryMiB == nil {
		out = append(out, "no limits.memoryMiB on a container environment — the container inherits the host's memory, "+
			"so an OOM inside it can take the daemon's own cache with it")
	}
	return out
}
