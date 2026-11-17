package skills

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/cjlapao/aeon/internal/paths"
)

// catalog.go is the service the daemon holds: the two roots, one cached
// scan, and the read doors. The cache never goes stale on a timer on
// purpose — a timer is a second clock nobody declared; `skills.list`
// answers the last scan, `skills.validate` re-derives from disk, and the
// bench makes re-deriving the obvious button. Discovery is a directory
// walk of a few dozen folders; freshness costs a click, not a poll.

// ErrNotFound is a lookup miss, discriminated from infrastructure failure
// (DB19): "no skill named X" is an answer, "the disk failed" is a fault,
// and the socket translates these two into different sentences.
var ErrNotFound = errors.New("skills: not found")

// ErrRefused is the installer's and remover's refusal — the thing refused
// and why, in the Detail — discriminated from disk faults the same way.
type ErrRefused struct {
	Field  string
	Detail string
}

func (e *ErrRefused) Error() string { return e.Detail }

func refuse(field, format string, args ...any) *ErrRefused {
	return &ErrRefused{Field: field, Detail: fmt.Sprintf(format, args...)}
}

// Options builds a service. Getenv and Clock are the same injectables the
// rest of the project's disk-touching packages carry — no test mutates the
// process environment or waits on the real clock through this door.
// Clone overrides the GitHub transport (tests inject a fake; nil means the
// real shallow `git clone`).
type Options struct {
	Getenv func(string) string
	Clock  func() time.Time
	Limits *Limits
	Clone  func(ctx context.Context, url, ref, dir string) error
	// WatchDebounce is the quiescent window the watcher requires before
	// re-deriving (0 → DefaultWatchDebounce). It is an injectable so a
	// test does not wait half a second per event.
	WatchDebounce time.Duration
	// OnWatchChange is called (synchronously, from the watch goroutine)
	// after a change-triggered refresh produced a diff. nil is silent:
	// the daemon wires it to the `skills.changed` emitter; a service
	// built without it still watches, it just does not announce.
	OnWatchChange func(ChangeEvent)
}

// Ctx is context.Context under a name this package's Clone signature can
// spell without pulling context into every file.
type Ctx = context.Context

// Service is the daemon's skills catalog. One per daemon (the asyncworkers
// rule, carried): the socket, the CLI client and the bench all read the
// SAME cached scan, and a second scan implementation is exactly what P34
// exists to prevent.
type Service struct {
	userRoot    string
	managedRoot string
	caps        Limits
	clock       func() time.Time
	clone       func(ctx Ctx, url, ref, dir string) error

	mu    sync.Mutex
	cache *ScanResult

	// Watch state (S66). Same mutex as the cache on purpose: the scan
	// result CARRIES the watch state, so reading either is one lock.
	watching      bool
	watchEnabled  bool
	watchReason   string
	watchErrs     []string
	debounce      time.Duration
	onWatchChange func(ChangeEvent)
}

// NewService resolves the two roots (env override → XDG → home, all the
// way through paths, never a second resolver) and returns the catalog.
// Roots need not exist yet: the empty state is a first run, not a fault.
func NewService(opts Options) (*Service, error) {
	getenv := opts.Getenv
	if getenv == nil {
		getenv = os.Getenv
	}
	clock := opts.Clock
	if clock == nil {
		clock = time.Now
	}
	userRoot, err := paths.UserSkillsDirFor(getenv)
	if err != nil {
		return nil, fmt.Errorf("resolve the user skills directory: %w", err)
	}
	managedRoot, err := paths.SkillsDirFor(getenv)
	if err != nil {
		return nil, fmt.Errorf("resolve the app-managed skills directory: %w", err)
	}
	caps := DefaultLimits()
	if opts.Limits != nil {
		caps = *opts.Limits
	}
	clone := opts.Clone
	if clone == nil {
		clone = gitCloneShallow
	}
	debounce := opts.WatchDebounce
	if debounce <= 0 {
		debounce = DefaultWatchDebounce
	}
	return &Service{
		userRoot:      userRoot,
		managedRoot:   managedRoot,
		caps:          caps,
		clock:         clock,
		clone:         clone,
		debounce:      debounce,
		onWatchChange: opts.OnWatchChange,
	}, nil
}

// Roots answers where discovery looks, in precedence order (user first —
// a person's own file outranks a downloaded copy of a same-named one).
func (s *Service) Roots() []Root {
	return []Root{{SourceUser, s.userRoot}, {SourceManaged, s.managedRoot}}
}

// Scan returns the catalog, walking disk on the first call or when force
// is set. The zero-argument freshness question is `validate`, not a timer:
// this returns the cached truth, which may predate an edit the operator
// made with an editor open, and says so with ScannedAt.
func (s *Service) Scan(force bool) (*ScanResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// The caller gets a SHALLOW COPY stamped with the current watch
	// state: the cache itself is shared, and stamping it in place would
	// race with a marshal of the previous answer. The nested structures
	// are read-only after Discover, so copying the header is enough.
	if s.cache != nil && !force {
		out := *s.cache
		s.stampWatch(&out)
		return &out, nil
	}
	res, err := Discover(s.Roots(), s.caps, s.clock)
	if err != nil {
		return nil, err
	}
	s.cache = res
	out := *res
	s.stampWatch(&out)
	return &out, nil
}

// stampWatch writes the two watch fields (the caller holds s.mu).
func (s *Service) stampWatch(res *ScanResult) {
	st := watchState{enabled: s.watchEnabled, reason: s.watchReason, errs: s.watchErrs}
	res.Watching = st.enabled
	res.WatchError = st.watchError()
}

// SetEventEmit replaces the watcher's announce door after construction.
// The daemon needs it because its event path (table.OnEvent) exists only
// after the service does — a construction-order fact, not a second
// source of truth. It is also the test's probe: nothing forges watcher
// events for a reason the daemon itself would not have.
func (s *Service) SetEventEmit(fn func(ChangeEvent)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onWatchChange = fn
}

// RecheckNow re-derives and announces any diff — the watcher's own
// refresh path, opened for callers that need freshness NOW (and for
// tests that will not wait on a filesystem event).
func (s *Service) RecheckNow() {
	s.mu.Lock()
	st := &watchState{enabled: s.watchEnabled, reason: s.watchReason, errs: s.watchErrs}
	s.mu.Unlock()
	s.refreshOnWatch(st)
}

// Invalidate drops the cached scan (after any install or removal, so the
// next read reflects the write without forcing every reader to think
// about which read is post-write).
func (s *Service) Invalidate() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cache = nil
}

// Get answers one skill by name plus its SKILL.md body, truncated at
// BodyMax with the flag raised rather than the text silently shortened.
// A shadowed entry is reachable by path (GetUnder), because the second
// copy of a name is exactly what someone debugging a shadow wants to see.
func (s *Service) Get(name string) (*GetResult, error) {
	res, err := s.Scan(false)
	if err != nil {
		return nil, err
	}
	for _, sk := range res.Skills {
		if sk.Name == name && !sk.Shadowed {
			return s.readBody(sk), nil
		}
	}
	return nil, ErrNotFound
}

// GetUnder names a skill by folder path (a shadowed entry has a name its
// winner already owns; the path is its real identity).
func (s *Service) GetUnder(path string) (*GetResult, error) {
	res, err := s.Scan(false)
	if err != nil {
		return nil, err
	}
	clean := filepath.Clean(path)
	for _, sk := range res.Skills {
		if filepath.Clean(sk.Path) == clean {
			return s.readBody(sk), nil
		}
	}
	return nil, ErrNotFound
}

// readBody attaches the SKILL.md body under the cap. A file that vanished
// between the scan and the read is answered with the honest state (body
// absent, an issue appended to a COPY) rather than a failure of the whole
// read — the catalog row remains true even while the folder is mid-edit.
func (s *Service) readBody(sk *Skill) *GetResult {
	out := *sk
	res := &GetResult{Skill: &out}
	data, over, err := readCapped(filepath.Join(sk.Path, "SKILL.md"), s.caps.BodyMax)
	if err != nil {
		out.Issues = append(append([]Issue{}, sk.Issues...), Issue{
			Field: "SKILL.md", Detail: err.Error(), Severity: SeverityWarning,
		})
		return res
	}
	split := splitFrontmatter(data)
	body := strings.TrimSpace(split.BodyText)
	if over {
		// The read ran one byte past the cap to LEARN it is over the cap;
		// the body is answered at or under the cap, flagged, never beyond.
		if int64(len(body)) > s.caps.BodyMax {
			body = strings.ToValidUTF8(body[:s.caps.BodyMax], "")
		}
		res.Truncated = true
	}
	res.Body = body
	return res
}
