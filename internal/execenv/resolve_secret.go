package execenv

// Resolving a secret reference — the one place in the whole slice a value is
// read (EV7).
//
// A reference is a row that NAMES where a value lives: a daemon variable, a
// file on disk, or a credential record the daemon can reach. Resolution turns
// one into a `Secret`, whose bytes then have exactly two doors (`ChildEnv.Env`
// and the `Redactor`), so this file is where the slice's security claim is
// either true or a comment. The three rules it exists to enforce:
//
//   - The daemon's OWN credentials are not referenceable. `ValidateRef` is the
//     door for that, and the caller passes the names in (`daemoncreds.go`)
//     because knowing what the daemon holds belongs to the daemon.
//   - A key file is read only if it is a regular file, not a symlink, no
//     wider than 0600, owned by the daemon user, and small. Every one of those
//     is a separate refusal, because "the file is not safe to read" has four
//     different fixes and the operator has to know which one to do.
//   - A value shorter than `MinSecretBytes` is refused. That is not fussiness:
//     a 4-byte secret that the command can print in another form
//     (`ab` + `cd` in a table) defeats redaction for every OTHER secret in the
//     same capture, because a missed match looks exactly like a passing one.
//
// Nothing here returns a value in a refusal, and nothing returns a length:
// "the value is 3 bytes" is a disclosure, and the length of a key is part of
// what makes it a key.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// The bounds EV7 puts on a resolvable value.
const (
	// MinSecretBytes is the shortest value resolution will produce. Below it
	// redaction cannot be trusted (see the header), so the value never becomes
	// an injection at all.
	MinSecretBytes = 8
	// MaxKeyFileBytes caps a key file. A "key file" of 40 MB is not a key file;
	// reading it would make the daemon's memory a function of a path somebody
	// typed into a form.
	MaxKeyFileBytes = 64 << 10
)

// The resolution-time refusal kinds. The store's write-time door
// (`internal/store.secretRefSourceRefusal`) refuses the same shapes with the
// same strings, so one vocabulary describes a reference both before and after
// it is a row; `TestRefusalKindsMatchTheStoresVocabulary` in the store's test
// set pins the two halves against each other.
const (
	KindDaemonCredential  = "daemon-credential"
	KindDaemonEnvShape    = "daemon-env-shape"
	KindKeyFileRelative   = "key-file-relative"
	KindKeyFileTraversal  = "key-file-traversal"
	KindKeyFileMissing    = "key-file-missing"
	KindKeyFileSymlink    = "key-file-symlink"
	KindKeyFileNotRegular = "key-file-not-regular"
	KindKeyFileMode       = "key-file-mode"
	KindKeyFileOwner      = "key-file-owner"
	KindKeyFileTooLarge   = "key-file-too-large"
	KindStoreSourceShape  = "store-source-shape"
	KindStoreNotWired     = "store-not-wired"
	KindSourceKindUnknown = "source-kind-unknown"
	KindSourceEmpty       = "source-empty"
	KindSecretTooShort    = "secret-too-short"
)

// RefSpec is one secret reference as the record holds it — a name, where the
// value lives, and the variable it is injected as. It has no value field
// because nothing outside this package may carry one, and a struct with room
// for a value is a struct somebody eventually fills in.
type RefSpec struct {
	Name       string `json:"name"`
	SourceKind string `json:"sourceKind"`
	Source     string `json:"source"`
	InjectAs   string `json:"injectAs"`
}

// FileFacts is what a stat answers about a key file, in the three terms the
// rules are written in. It is a struct rather than an os.FileInfo because the
// rules must be checkable by a test without chmod and chown, and the daemon
// must be able to answer them from whatever it trusts.
type FileFacts struct {
	// Regular is true for a plain file. A directory, a device or a FIFO is not
	// a key file and reading one is a hang waiting to happen.
	Regular bool
	// Symlink is true for a link. Following it would let a path that passes
	// every check name a file the check was never about (/etc/shadow behind a
	// friendly name), so a symlink is refused rather than followed.
	Symlink bool
	// Perm is the permission bits only.
	Perm os.FileMode
	// Size is the byte length, used for the cap before any read.
	Size int64
	// UID is the owning user id. -1 means the platform could not say, which is
	// a refusal rather than a pass (see ownerIsDaemon).
	UID int
}

// FileOpener is the daemon's view of the filesystem, as small as the rules
// allow, and shaped the way the rules need it shaped:
//
//   - Lstat does NOT follow a symlink, and is the only call made on a path that
//     has not yet been judged.
//   - Open is reached only after every check has passed, and must report the
//     facts of the inode it ACTUALLY read — the ones from a fstat after the
//     open, not the ones from the earlier stat. Without that, the window
//     between "the file is a 0600 regular file owned by you" and "here are its
//     bytes" is a window in which somebody can swap the file for a symlink to
//     something else, and every check above becomes a check on a file that is
//     no longer there.
type FileOpener interface {
	Lstat(path string) (FileFacts, error)
	Open(path string) (FileFacts, []byte, error)
}

// ResolveEnv is the world resolution is allowed to see. Every field is
// injected: the core never reads the process environment itself (the purity
// guard in purity_test.go fails the build if it starts), and a test supplies a
// map instead of a machine.
type ResolveEnv struct {
	// Lookup reads one daemon variable (the `daemon-env` kind). nil means the
	// daemon has no environment to offer, which refuses the kind rather than
	// answering "" for everything.
	Lookup func(string) string
	// Files answers the `key-file` kind.
	Files FileOpener
	// DaemonUID is the uid the key file must be owned by. The daemon passes its
	// own; a test passes whatever it built the facts with.
	DaemonUID int
	// Credentials is the daemon's own credential-name list, passed through to
	// ValidateRef. An empty list is a refusal for a daemon-env reference rather
	// than a pass — see daemoncreds.go.
	Credentials DaemonCredentials
	// Store resolves the `store` kind by credential id (A1 GRANTED at T0: the
	// value lives in `internal/secrets`' 0600 credential file, whose paths
	// belong to that package, so the daemon hands over a lookup rather than a
	// path). nil means the kind is not wired on this daemon.
	Store func(credentialID string) (value string, found bool)
}

// ValidateRef is the write-time and read-time door EV7 asks for: a reference
// that names one of the daemon's own credentials is refused before it can
// become anyone's application secret. The names come IN — the core does not
// import `daemon`, `config`, `forge`, `narrow` or `secrets` to learn them
// (daemoncreds.go), and a caller that passes an empty list gets a refusal that
// says the list was empty rather than a pass.
func ValidateRef(ref RefSpec, creds DaemonCredentials) *Refusal {
	if strings.TrimSpace(ref.Name) == "" {
		return NewRefusal(KindSourceEmpty, "a secret reference with no name",
			"the name is what a step record and a redaction report can point at",
			"give the reference a name before resolving it")
	}
	switch ref.SourceKind {
	case "daemon-env":
		if strings.TrimSpace(ref.Source) == "" {
			return NewRefusal(KindSourceEmpty, fmt.Sprintf("secret reference %q has no source variable", ref.Name),
				"the source names which daemon variable holds the value",
				"set the source, or archive the reference")
		}
		if !isEnvName(ref.Source) {
			return NewRefusal(KindDaemonEnvShape, fmt.Sprintf("secret reference %q reads %q", ref.Name, ref.Source),
				"a daemon variable name is a letter or underscore followed by letters, digits or underscores",
				"fix the source so it is a variable name, not a path or a value")
		}
		if creds.empty() {
			return NewRefusal(KindDaemonCredential, fmt.Sprintf("secret reference %q reads the daemon variable %q with no credential list to check it against",
				ref.Name, ref.Source),
				"the daemon's own credential names are what keeps ANTHROPIC_API_KEY from becoming an application secret, and an empty list checks nothing",
				"pass the daemon's provider, forge and extra credential names")
		}
		if creds.Holds(ref.Source) {
			return daemonCredentialRefusal(ref.Name, ref.Source)
		}
	case "key-file":
		if strings.TrimSpace(ref.Source) == "" {
			return NewRefusal(KindSourceEmpty, fmt.Sprintf("secret reference %q has no file path", ref.Name),
				"a key-file reference names the file to read",
				"set the source to an absolute path, or archive the reference")
		}
		if !filepath.IsAbs(ref.Source) {
			return NewRefusal(KindKeyFileRelative, fmt.Sprintf("secret reference %q names the relative path %q", ref.Name, ref.Source),
				"a relative key-file path resolves against whatever directory the daemon happened to start in, which is not a place a credential lives",
				"give the absolute path")
		}
		if strings.Contains(ref.Source, "..") {
			return NewRefusal(KindKeyFileTraversal, fmt.Sprintf("secret reference %q has %q in its path", ref.Name, ".."),
				"a traversal component turns a path that passed the directory check into a different file",
				"point at the file directly, without ..")
		}
	case "store":
		if strings.TrimSpace(ref.Source) == "" {
			return NewRefusal(KindSourceEmpty, fmt.Sprintf("secret reference %q has no credential id", ref.Name),
				"the store kind names a credential record by id, never by path",
				"set the source to the credential id")
		}
		if strings.ContainsAny(ref.Source, "/\\") || strings.HasPrefix(ref.Source, ".") {
			return NewRefusal(KindStoreSourceShape, fmt.Sprintf("secret reference %q names %q, which is shaped like a path", ref.Name, ref.Source),
				"the store kind resolves through the daemon's credential records, whose file lives where that package keeps it and is not a path a row may carry",
				"use the credential id")
		}
	default:
		return NewRefusal(KindSourceKindUnknown, fmt.Sprintf("secret reference %q has source kind %q", ref.Name, ref.SourceKind),
			"the kinds are "+strings.Join(SourceKinds(), ", ")+" — a kind outside them is a state nothing knows how to resolve",
			"pick one of the known kinds")
	}
	// inject_as is checked against the credential list too: a reference that
	// reads an innocent variable but injects it AS the daemon's provider key is
	// the same smuggle wearing the other hat, and the deny list only catches
	// the vendor namespaces, not a name the daemon itself reads.
	if creds.Holds(ref.InjectAs) {
		return daemonCredentialRefusal(ref.Name, ref.InjectAs)
	}
	return nil
}

func daemonCredentialRefusal(name, varName string) *Refusal {
	return NewRefusal(KindDaemonCredential,
		fmt.Sprintf("secret reference %q points at %q", name, varName),
		"that variable is one the daemon holds for itself; reading it into an application secret hands a model the daemon's own identity, and redaction cannot promise anything about a value the daemon prints for its own reasons",
		"reference a value that belongs to the application instead, or add the variable to the environment's set section if it is not a credential")
}

// ResolveRef turns one validated reference into an Injection. The value is
// read here and nowhere else; the returned Injection's bytes have no accessor,
// so the caller's only moves are Build and the Redactor.
//
// A refusal here is the spawn-time answer EV11 wants: it names the reference
// and the source, never the value and never its length.
func ResolveRef(ref RefSpec, env ResolveEnv) (Injection, *Refusal) {
	if r := ValidateRef(ref, env.Credentials); r != nil {
		return Injection{}, r
	}
	var raw []byte
	switch ref.SourceKind {
	case "daemon-env":
		if env.Lookup == nil {
			return Injection{}, NewRefusal(RefusalSecretUnresolvable,
				fmt.Sprintf("secret reference %q reads the daemon variable %q but this daemon offered no environment", ref.Name, ref.Source),
				"resolution reads a variable through the caller, and no reader was given",
				"wire the daemon's environment reader before resolving")
		}
		v := env.Lookup(ref.Source)
		if strings.TrimSpace(v) == "" {
			return Injection{}, NewRefusal(RefusalSecretUnresolvable,
				fmt.Sprintf("secret reference %q reads the daemon variable %q and it is not set", ref.Name, ref.Source),
				"the value is absent, so the command would run with the variable empty — which most programs read as configured-but-broken rather than as not-configured",
				"set "+ref.Source+" for the daemon, or archive the reference until it applies")
		}
		raw = []byte(v)
	case "key-file":
		facts, err := env.Files.Lstat(ref.Source)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return Injection{}, NewRefusal(KindKeyFileMissing,
					fmt.Sprintf("secret reference %q names the file %q and it is not there", ref.Name, ref.Source),
					"a missing key file is not an empty one: the file was deleted, moved, or never provisioned on this machine",
					"restore the file or archive the reference")
			}
			return Injection{}, NewRefusal(RefusalSecretUnresolvable,
				fmt.Sprintf("secret reference %q could not be stat-ed at %q", ref.Name, ref.Source),
				"the daemon cannot read the file's metadata, so it cannot decide whether reading the contents is safe — and reading it anyway is the failure this rule exists to prevent",
				"check the path and the directory's permissions")
		}
		if r := keyFileProblem(ref, facts, env.DaemonUID); r != nil {
			return Injection{}, r
		}
		opened, b, err := env.Files.Open(ref.Source)
		if err != nil {
			return Injection{}, NewRefusal(RefusalSecretUnresolvable,
				fmt.Sprintf("secret reference %q could not read %q", ref.Name, ref.Source),
				"the file passed its metadata checks and still could not be read, which is a race with a swap or a directory the daemon cannot traverse",
				"check the file and its parent directory")
		}
		// The same checks again, against the inode that was actually opened.
		// Repeating them is not nervousness: between the stat and the open the
		// name can be re-pointed at a different file, and the first pass then
		// described a file the bytes did not come from.
		if r := keyFileProblem(ref, opened, env.DaemonUID); r != nil {
			return Injection{}, r
		}
		// One trailing newline is how a key file gets written
		// (`echo $TOKEN > token.key`), and leaving it in would put a byte
		// inside every value that redaction then cannot match.
		raw = []byte(strings.TrimSuffix(string(b), "\n"))
	case "store":
		if env.Store == nil {
			return Injection{}, NewRefusal(KindStoreNotWired,
				fmt.Sprintf("secret reference %q is a store reference and this daemon wired no credential store", ref.Name),
				"the values live in the daemon's credential records; without the lookup there is nothing to resolve to, and answering with the empty string would run the command unconfigured",
				"wire the daemon's credential lookup, or reference a daemon-env or key-file source")
		}
		v, found := env.Store(ref.Source)
		if !found || strings.TrimSpace(v) == "" {
			return Injection{}, NewRefusal(RefusalSecretUnresolvable,
				fmt.Sprintf("secret reference %q names the credential %q and the daemon has no such record", ref.Name, ref.Source),
				"the record was deleted, renamed, or belongs to a different daemon",
				"re-create the credential or archive the reference")
		}
		raw = []byte(v)
	}
	if len(raw) < MinSecretBytes {
		return Injection{}, NewRefusal(KindSecretTooShort,
			fmt.Sprintf("secret reference %q resolves to a value shorter than %d bytes", ref.Name, MinSecretBytes),
			"redaction matches the value's exact bytes and their encodings; below this length a command can print the value in a form the matcher will not see, and then every other secret in the same capture is unprotected by a redactor that reports success",
			"use a real credential, or move the value into the environment's set section where it is not a secret at all")
	}
	return NewInjection(ref.Name, ref.InjectAs, newSecret(ref.Name, raw)), nil
}

// ResolveAll resolves every reference of an environment. It resolves them ALL
// rather than stopping at the first refusal, because the answer a caller needs
// at save time is "these three are broken on this machine", and EV11 decides
// the spawn-time half: one unresolvable reference refuses the whole command,
// since a command that runs with a missing credential does something other
// than what its author meant.
func ResolveAll(refs []RefSpec, env ResolveEnv) ([]Injection, []Refusal) {
	injections := make([]Injection, 0, len(refs))
	var refusals []Refusal
	for _, ref := range refs {
		inj, r := ResolveRef(ref, env)
		if r != nil {
			refusals = append(refusals, *r)
			continue
		}
		injections = append(injections, inj)
	}
	return injections, refusals
}

// keyFileProblem is the key-file rule as one function, applied twice: once to
// what a stat says the file is and once to what the opened inode turned out to
// be. Each refusal names the one thing an operator can change, because "the
// file is not safe" has four different fixes and picking the wrong one costs an
// hour.
func keyFileProblem(ref RefSpec, facts FileFacts, daemonUID int) *Refusal {
	if facts.Symlink {
		return NewRefusal(KindKeyFileSymlink,
			fmt.Sprintf("secret reference %q names %q, which is a symlink", ref.Name, ref.Source),
			"a link lets a path that passed every other check name a different file, and the check would have been about the link, not the target",
			"point the reference at the regular file itself")
	}
	if !facts.Regular {
		return NewRefusal(KindKeyFileNotRegular,
			fmt.Sprintf("secret reference %q names %q, which is not a regular file", ref.Name, ref.Source),
			"a directory, a device or a FIFO is not a credential; reading one either fails or blocks",
			"point the reference at a regular file")
	}
	if facts.Perm.Perm()&0o077 != 0 {
		return NewRefusal(KindKeyFileMode,
			fmt.Sprintf("secret reference %q names %q, readable beyond its owner", ref.Name, ref.Source),
			"a credential file any other user can read is not the daemon's secret any more; the mode is the only thing between it and every account on this machine",
			"chmod 600 "+ref.Source)
	}
	if !ownerIsDaemon(facts.UID, daemonUID) {
		return NewRefusal(KindKeyFileOwner,
			fmt.Sprintf("secret reference %q names %q, which the daemon's user does not own", ref.Name, ref.Source),
			"another user's file is that user's content: a mode and a path that look right prove nothing about who wrote it, and a writable file can be swapped between the check and the read",
			"chown the file to the daemon's user, or run the daemon as the user who owns it")
	}
	if facts.Size > MaxKeyFileBytes {
		return NewRefusal(KindKeyFileTooLarge,
			fmt.Sprintf("secret reference %q names %q, which is larger than %d bytes", ref.Name, ref.Source, MaxKeyFileBytes),
			"a key file is a token, not an archive; the cap keeps a path typed into a form from turning the daemon's memory into a function of it",
			"point at the real key file")
	}
	return nil
}

// ownerIsDaemon is fail-closed on purpose: a platform that cannot report an
// owner has UID == -1, and "we could not tell" is not "the daemon owns it".
func ownerIsDaemon(fileUID, daemonUID int) bool {
	if fileUID < 0 || daemonUID < 0 {
		return false
	}
	return fileUID == daemonUID
}

// isEnvName is the shape of a variable name, kept local so ValidateRef can
// check a source before anything tries to use it as a name. refuseName in
// childenv.go asks the harder question (may this name be in a child at all);
// this one only asks whether it is a name.
func isEnvName(s string) bool {
	if s == "" || strings.ContainsAny(s, "= \t\n\x00") {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		ok := c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (i > 0 && c >= '0' && c <= '9')
		if !ok {
			return false
		}
	}
	return true
}
