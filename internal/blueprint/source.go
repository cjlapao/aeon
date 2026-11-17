package blueprint

// source.go — S48 T2 (BP2): the `Source` seam types. Every reader of
// blueprint content (the fire-time pin, the CLI, the socket reads, the
// validators) will hold a Source, so "where content comes from" is one
// swappable decision — files today (T3's row-backed source lands next),
// rows after. This file is TYPES ONLY plus the embedded source: the shipped
// set read straight from the `standards` embed FS, no disk, which is what
// the offline validate path and every test read when they mean "the shipped
// bytes".

import (
	"os"
	"sync"
)

// Source is where blueprint content comes from. Get returns the bytes
// verbatim, whether the name exists, and an infrastructure error (a missing
// blueprint is false, not an error — the three states stay three). Names is
// the full listing, ordered. Standard is the shipped set (the seed set's
// names, T5) — the source can always tell a shipped name from a custom one.
type Source interface {
	Get(name string) ([]byte, bool, error)
	Names() ([]string, error)
	Standard() []string
}

// embeddedSource reads the embedded shipped set — the only reader of the
// embed FS besides standard.go's own paths (rule 5's pin).
type embeddedSource struct{}

// EmbeddedSource returns the shipped-set Source. It is inert: no disk, no
// store, no environment — the bytes as compiled.
func EmbeddedSource() Source { return embeddedSource{} }

func (embeddedSource) Get(name string) ([]byte, bool, error) {
	if !blueprintName.MatchString(name) {
		return nil, false, nil
	}
	raw, err := standards.ReadFile("blueprints/" + name + ".yaml")
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return raw, true, nil
}

func (embeddedSource) Names() ([]string, error) {
	// The embed holds exactly the shipped set, so the shipped list IS the
	// listing — in the shipped ORDER, which is the order readers listed
	// before rows existed (the listing is a set; the order is a promise).
	return StandardNames(), nil
}

func (embeddedSource) Standard() []string { return StandardNames() }

// ---------------------------------------------------------------------------
// The installed source (S48 T3, BP2). Production wiring calls SetSource once
// (the daemon, from serve, after the store opens and after T4's adoption and
// T5's seed have run — the hook point recorded in T0); the package-level
// Get/Names/Standard delegate to it, so "where content comes from" is ONE
// swappable decision. The embedded source is the default and is NOT counted:
// it is what offline CLI verbs and no-database tests get. A second REAL
// install panics — two sources is two truths (the handler SD8 precedent).
// ---------------------------------------------------------------------------

// installedMu guards the seam. Every daemon New() installs its source
// (last installer wins, by design); a test process that builds daemons
// concurrently — S27 T11's parallel -race law — raced the swap against
// every reader and against itself. The design is unchanged, atomic now.
var installedMu sync.RWMutex

var (
	installed     Source = embeddedSource{}
	installedReal        = false
)

// SetSource installs the production source (the daemon's store-backed one).
// Calling it twice with two real sources panics.
func SetSource(s Source) {
	if s == nil {
		panic("blueprint: SetSource(nil) — the absence of a source is not a source")
	}
	installedMu.Lock()
	defer installedMu.Unlock()
	if _, isEmbedded := s.(embeddedSource); !isEmbedded {
		if installedReal {
			panic("blueprint: SetSource was called twice — two sources is two truths (S48 BP2)")
		}
		installed = s
		installedReal = true
		return
	}
	installed = s
	installedReal = false
}

// UseEmbeddedForOfflineTools installs the embedded source for a CLI verb
// that needs Get/Names without a database (`aeon blueprint list|validate`).
// It is a deliberate, named, documented offline-tool path — never reachable
// from serve (TestProductionNeverUsesEmbeddedSource pins the callers). It
// also UN-counts a real install, which is how a test re-installs its own
// store-backed source without tripping the twice-call panic.
func UseEmbeddedForOfflineTools() { SetSource(embeddedSource{}) }

// Get, Names and Standard read through the installed source. They are the
// ONLY package-level readers of blueprint content; the file trio is
// Legacy-prefixed (standard.go) and confined to the importer and tests.
func Get(name string) ([]byte, bool, error) { return currentSource().Get(name) }

func Names() ([]string, error) { return currentSource().Names() }

func Standard() []string { return currentSource().Standard() }

// currentSource reads the seam under the guard.
func currentSource() Source {
	installedMu.RLock()
	defer installedMu.RUnlock()
	return installed
}
