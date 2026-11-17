package secrets

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cjlapao/aeon/internal/paths"
)

// Route credentials, on disk, one file each.
//
// # Why a file and not a column
//
// C17 is one credential rule for the whole product: a credential comes from
// **the environment, or a dedicated file path — never a config file, never
// argv, never a log.** The store is neither of the two it names, so a route's
// key does not go in the database.
//
// This is the same mechanism the cursor signing key already uses, and that is
// the argument for it: one pattern, one set of permissions, one place to look.
//
//	$STATE/aeon/credentials/<route-id>.key     0600, in a 0700 directory
//
// P26 — access control is the filesystem. The daemon already relies on that for
// the socket; a secret beside it relies on the same thing rather than on a
// second mechanism.
//
// # What this buys S14
//
// S14 brings the framework's `encryption` service for at-rest credentials.
// Wrapping a FILE is a change to this package; wrapping a COLUMN would have
// been a migration over a live store holding secrets, which is the worst
// possible place to need one.
//
// # What it does not do
//
// It does not read the environment. `RoutesFromEnvironment` does that, and a
// key from the environment is never written here — an operator injecting from a
// vault would otherwise find Aeon had copied their secret onto disk, which is
// exactly the surprise C17 exists to prevent.

// credentialsDirName is the subdirectory holding route keys.
const credentialsDirName = "credentials"

// RouteKeyPath is where a route's credential lives.
//
// The id is validated rather than escaped: a route id reaching a path join is
// the shape of a traversal, and an id is a short lower-case name. Refusing is
// cheaper than sanitising and cannot be got wrong quietly.
func RouteKeyPath(routeID string) (string, error) {
	if err := validRouteID(routeID); err != nil {
		return "", err
	}
	dir, err := credentialsDir()
	if err != nil {
		return "", err
	}
	// A `/` is allowed in an id (a model name such as hexgrad/Kokoro-82M) but never reaches the path as one: it is
	// written as %2F. `%` is refused in every other position, so two ids cannot land on the same file.
	return filepath.Join(dir, strings.ReplaceAll(routeID, "/", "%2F")+".key"), nil
}

// SaveRouteKey writes a route's credential, 0600.
//
// An EMPTY key is not an error and not a write: "keep the existing credential"
// is what an empty key means everywhere else in this product (S3's `apiKey`
// semantics, AGENTS §4c), and writing an empty file would turn that into "unset
// it".
func SaveRouteKey(routeID, key string) error {
	if strings.TrimSpace(key) == "" {
		return nil
	}
	path, err := RouteKeyPath(routeID)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create the credentials directory: %w", err)
	}
	// The mode is set EXPLICITLY rather than left to the umask, for the same
	// reason the cursor key's is: a umask of 022 would make this world-readable,
	// and nothing would say so.
	if err := os.WriteFile(path, []byte(key+"\n"), 0o600); err != nil {
		return fmt.Errorf("write the credential for route %q: %w", routeID, err)
	}
	return nil
}

// ReadRouteKey reads a route's credential, or "" when there is none.
//
// A missing file is not an error: a route with no credential is a normal state
// — it is what `hasKey: false` means in the app, and what a route configured
// before its key looks like.
func ReadRouteKey(routeID string) (string, error) {
	path, err := RouteKeyPath(routeID)
	if err != nil {
		return "", err
	}
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		// A credential that exists and cannot be read is reported, not treated
		// as absent: "no key" and "a key I cannot reach" send somebody to
		// different fixes, and the second is usually permissions.
		return "", fmt.Errorf("read the credential for route %q: %w", routeID, err)
	}
	return strings.TrimSpace(string(raw)), nil
}

// DeleteRouteKey removes a route's credential, reporting whether one existed.
//
// Called when a route is deleted and when `clearKey` is set. A key left behind
// for a route nobody can call is a secret on disk with no owner.
func DeleteRouteKey(routeID string) (bool, error) {
	path, err := RouteKeyPath(routeID)
	if err != nil {
		return false, err
	}
	if err := os.Remove(path); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("remove the credential for route %q: %w", routeID, err)
	}
	return true, nil
}

// credentialsDir is the directory holding route keys, beside the store.
func credentialsDir() (string, error) {
	dir, err := paths.StoreDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, credentialsDirName), nil
}

// validRouteID refuses anything that is not a plain route name.
//
// A route id is chosen by a person in a form and travels to a path join. The
// set is deliberately narrow — letters, digits, `-`, `_`, `.`, and `/` between two
// non-empty parts — and a `.` may not start the name or repeat, so `..` cannot appear
// at all. A `/` is escaped before the id is used as a file name (RouteKeyPath).
func validRouteID(id string) error {
	if id == "" {
		return fmt.Errorf("a route id is required")
	}
	if len(id) > 128 {
		return fmt.Errorf("the route id %q is too long to be a file name", id)
	}
	if strings.HasPrefix(id, ".") {
		return fmt.Errorf("the route id %q cannot start with a dot", id)
	}
	if strings.Contains(id, "..") {
		return fmt.Errorf("the route id %q contains '..' and is refused rather than sanitised", id)
	}
	if strings.HasPrefix(id, "/") || strings.HasSuffix(id, "/") || strings.Contains(id, "//") || strings.Contains(id, "/.") {
		return fmt.Errorf("the route id %q has an empty or dot-led part around a '/'", id)
	}
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '-', r == '_', r == '.', r == '/':
		default:
			return fmt.Errorf("the route id %q contains %q; ids are letters, digits, '-', '_', '.' and '/' "+
				"because an id becomes a file name", id, r)
		}
	}
	return nil
}
