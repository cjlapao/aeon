package store

// provisions.go — the lifecycle ledger (S59 EV12, EV14).
//
// The rule the whole file exists for: **the row first, then the real action.**
// A container created without a row is a container nobody can find after a
// crash; a row without a container is a reconcile that reports a leak it
// cannot close. Both are recoverable only if the row was written BEFORE the
// runtime was asked, which is why `CreateProvision` lands in `provisioning`
// and every later mark is a one-way move.
//
// # Settle-once
//
// Each mark applies to a row that has not settled yet. A repeat is a NO-OP and
// returns no refusal: a daemon that crashed between the runtime call and the
// mark, then reconciled and re-marked, is the ordinary case, and an error there
// would make the reconcile loop report a failure for the thing it just fixed.
// A mark that moves a row BACKWARD (removing a row that is already removed,
// readying one that was torn down) is a refusal naming the state it found —
// that is not a retry, it is a caller that has lost track of a resource.
//
// # Why there is no Actor here
//
// `environment_change` has no subject kind for a provision, and adding one
// would audit a container as if it were a decision. The ledger's own honesty
// lives in `state`, `handle` and `last_error`; who asked for the container is
// answered by the loop row that owns it. So the ledger takes no actor, and the
// audit stays what it is: the record of a person or an agent changing a record.

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// The provision states (EV12's lifecycle, mirrored in the migration's CHECK).
const (
	ProvisionStateProvisioning = "provisioning"
	ProvisionStateReady        = "ready"
	ProvisionStateTearingDown  = "tearing_down"
	ProvisionStateRemoved      = "removed"
	ProvisionStateFailed       = "failed"
)

// ProvisionOwnerLoop is the loop's own resource; every service container is
// `service:<name>` (EV14), which is what lets one reconcile find and age out
// everything a loop left behind.
const ProvisionOwnerLoop = "loop"

// ProvisionOwnerNetwork is the ledger's third owner: the per-loop network the
// container driver creates so a loop's services and its app container can reach
// each other (EV14). It is a resource the daemon made and must remove, so it
// carries a row like anything else — reconcile's List sees containers only, and
// a network with no row is a network nothing will ever find again.
const ProvisionOwnerNetwork = "network"

const provisionIDPrefix = "prov-"

// Provision is one `environment_provision` row. `Handle` is the runtime's own
// id (a container id) and is empty while provisioning: an empty handle in the
// `ready` state is a bug this type makes unrepresentable only by convention,
// so `MarkProvisionReady` refuses it out loud.
type Provision struct {
	ID            string
	EnvironmentID string
	ComponentRoot string
	LoopSlug      string
	Backend       string
	Handle        string
	Owner         string
	State         string
	CreatedAt     string
	ReadyAt       *string
	RemovedAt     *string
	LastError     *string
}

// Live reports whether the resource still has to be found. This is the
// predicate the partial unique index encodes in SQL, restated in Go so a
// caller reading a row and a caller writing one agree on what "live" means.
func (p Provision) Live() bool { return p.RemovedAt == nil }

// NewProvision is the ledger's insert. `Owner` empty means the loop's own
// resource.
type NewProvision struct {
	EnvironmentID string
	ComponentRoot string
	LoopSlug      string
	Backend       string
	Owner         string
}

// provisionFromRows scans one row; the five reads below share it so no two of
// them can disagree about the column order.
type provisionScanner struct{}

func scanProvision(row interface{ Scan(dest ...any) error }) (Provision, error) {
	var (
		p                       Provision
		ready, removed, lastErr sql.NullString
	)
	err := row.Scan(&p.ID, &p.EnvironmentID, &p.ComponentRoot, &p.LoopSlug, &p.Backend, &p.Handle,
		&p.Owner, &p.State, &p.CreatedAt, &ready, &removed, &lastErr)
	if err != nil {
		return Provision{}, err
	}
	p.ReadyAt = nullStringPtr(ready)
	p.RemovedAt = nullStringPtr(removed)
	p.LastError = nullStringPtr(lastErr)
	return p, nil
}

const provisionColumns = `id, environment_id, component_root, loop_slug, backend, handle, owner, state,
		created_at, ready_at, removed_at, last_error`

// CreateProvision writes the ledger row BEFORE anything is created (EV12). A
// second live row for the same `(component_root, loop_slug, owner)` is a
// refusal that names the live row: two provisioning rows for one owner is a
// caller that has lost track of a container, and the second container would be
// the one nobody ever removes.
func (s *Store) CreateProvision(in NewProvision) (Provision, *EnvironmentRefusal, error) {
	if in.EnvironmentID == "" || in.ComponentRoot == "" || in.LoopSlug == "" {
		return Provision{}, environmentRefusal("provision-identity",
			"a provision needs an environment, a component root and a loop slug — %q/%q/%q is not enough to find it again during reconcile (EV12)",
			in.EnvironmentID, in.ComponentRoot, in.LoopSlug), nil
	}
	if !environmentBackends[in.Backend] {
		return Provision{}, environmentRefusal("provision-backend",
			"backend %q is not one of %s (EV3)", in.Backend,
			strings.Join(sortedKeys(environmentBackends), ", ")), nil
	}
	owner := in.Owner
	if owner == "" {
		owner = ProvisionOwnerLoop
	}
	if ref := provisionOwnerRefusal(owner); ref != nil {
		return Provision{}, ref, nil
	}
	if _, err := s.GetEnvironment(in.EnvironmentID); err != nil {
		if errors.Is(err, ErrNotFound) {
			return Provision{}, environmentRefusal("provision-environment-unknown",
				"%s is not an environment — the ledger must be able to name what it provisioned (EV12)", in.EnvironmentID), nil
		}
		return Provision{}, nil, err
	}

	if live, err := s.liveProvision(in.ComponentRoot, in.LoopSlug, owner); err != nil {
		return Provision{}, nil, err
	} else if live.ID != "" {
		return Provision{}, environmentRefusal("provision-live-exists",
			"%s/%s already has a live provision %s in state %s — reconcile or remove it first (EV12: two live rows for one owner means one container nobody will ever remove)",
			in.ComponentRoot, in.LoopSlug, live.ID, live.State), nil
	}

	now := s.Now()
	p := Provision{
		ID:            provisionIDPrefix + newID(),
		EnvironmentID: in.EnvironmentID,
		ComponentRoot: in.ComponentRoot,
		LoopSlug:      in.LoopSlug,
		Backend:       in.Backend,
		Owner:         owner,
		State:         ProvisionStateProvisioning,
		CreatedAt:     now,
	}
	if _, err := s.w.Exec(`INSERT INTO environment_provision (id, environment_id, component_root, loop_slug, backend, handle, owner, state, created_at)
		VALUES (?,?,?,?,?,?,?,?,?)`,
		p.ID, p.EnvironmentID, p.ComponentRoot, p.LoopSlug, p.Backend, p.Handle, p.Owner, p.State, p.CreatedAt); err != nil {
		return Provision{}, nil, fmt.Errorf("store: create provision %s/%s/%s: %w",
			p.ComponentRoot, p.LoopSlug, p.Owner, err)
	}
	return p, nil, nil
}

// provisionOwnerRefusal is the Go twin of the migration's
// `owner = 'loop' OR owner GLOB 'service:*'` CHECK, with a sentence attached.
func provisionOwnerRefusal(owner string) *EnvironmentRefusal {
	if owner == ProvisionOwnerLoop || owner == ProvisionOwnerNetwork {
		return nil
	}
	if rest, ok := strings.CutPrefix(owner, "service:"); ok {
		if environmentSlugPattern.MatchString(rest) {
			return nil
		}
		return environmentRefusal("provision-owner-shape",
			"owner %q is not a service owner — the part after service: is the service's name as the spec declares it (EV14)", owner)
	}
	return environmentRefusal("provision-owner-shape",
		"owner %q is neither %q, %q nor service:<name> — the ledger addresses a resource by who owns it, and anything else would never be found by reconcile (EV12, EV14)",
		owner, ProvisionOwnerLoop, ProvisionOwnerNetwork)
}

// MarkProvisionReady records that the resource exists, with the runtime's own
// handle. `handle` empty is refused: a ready row with no handle cannot be
// inspected, stopped, or removed, which is the exact leak EV12 exists to
// prevent — and the reconcile that would notice it is the one that reads this
// column.
func (s *Store) MarkProvisionReady(id, handle string) (Provision, *EnvironmentRefusal, error) {
	if strings.TrimSpace(handle) == "" {
		return Provision{}, environmentRefusal("provision-handle",
			"%s cannot become ready without a handle — a ready row with no runtime id is a resource that can never be found again (EV12)", id), nil
	}
	return s.markProvision(id, ProvisionStateReady,
		`handle = ?, ready_at = ?`, []any{handle, s.Now()},
		map[string]bool{ProvisionStateProvisioning: true})
}

// MarkProvisionTearingDown is written BEFORE the runtime call that destroys the
// resource, which is what makes an interrupted teardown recoverable: the row
// says "this was on its way out" and reconcile finishes the job instead of
// treating the container as live.
func (s *Store) MarkProvisionTearingDown(id string) (Provision, *EnvironmentRefusal, error) {
	return s.markProvision(id, ProvisionStateTearingDown, ``, nil,
		map[string]bool{
			ProvisionStateProvisioning: true,
			ProvisionStateReady:        true,
			ProvisionStateFailed:       true,
		})
}

// MarkProvisionRemoved settles the row. `removed_at` is what the partial
// unique index reads to release the owner's slot, so a removed row frees
// `(component_root, loop_slug, owner)` for the next run (EV14's restart).
func (s *Store) MarkProvisionRemoved(id string) (Provision, *EnvironmentRefusal, error) {
	return s.markProvision(id, ProvisionStateRemoved, `removed_at = ?`, []any{s.Now()},
		map[string]bool{
			ProvisionStateProvisioning: true,
			ProvisionStateReady:        true,
			ProvisionStateTearingDown:  true,
			ProvisionStateFailed:       true,
		})
}

// MarkProvisionFailed records the failure WITH its sentence. A silent failure
// row is a reconcile that reports a leak with no reason attached, and the
// reason is the only part a person can act on.
func (s *Store) MarkProvisionFailed(id, reason string) (Provision, *EnvironmentRefusal, error) {
	if strings.TrimSpace(reason) == "" {
		return Provision{}, environmentRefusal("provision-failure-reason",
			"%s cannot be marked failed without the sentence — a failed row that says nothing is the state EV12's honesty line forbids", id), nil
	}
	return s.markProvision(id, ProvisionStateFailed, `last_error = ?`, []any{reason},
		map[string]bool{
			ProvisionStateProvisioning: true,
			ProvisionStateReady:        true,
			// A removal that fails WHILE tearing down is the most common failure
			// there is (S59 T6): the driver answered "no such container", or the
			// daemon was killed mid-`docker rm`. Refusing the mark would strand
			// the row in `tearing_down` with no sentence attached — the one state
			// a person cannot act on, and a direct contradiction of the lifecycle
			// this function's own comment states.
			ProvisionStateTearingDown: true,
		})
}

// markProvision is the one transition writer: from-states declared per target,
// an already-settled row returned as a no-op, and a backward move refused with
// the state it found.
func (s *Store) markProvision(id, target, set string, args []any, from map[string]bool) (Provision, *EnvironmentRefusal, error) {
	cur, err := s.GetProvision(id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return Provision{}, environmentRefusal("provision-unknown",
				"there is no provision %s to mark — the ledger is the only thing that knows a resource exists, so a mark against nothing means the resource was created without a row (EV12)", id), nil
		}
		return Provision{}, nil, err
	}
	if cur.State == target {
		return cur, nil, nil // settle-once: a repeat is the ordinary case, not a failure
	}
	if !from[cur.State] {
		return Provision{}, environmentRefusal("provision-transition",
			"%s is %s and cannot move to %s — the ledger moves one way (provisioning → ready → tearing_down → removed, or → failed) (EV12)",
			id, cur.State, target), nil
	}

	query := `UPDATE environment_provision SET state = ?`
	full := append([]any{target}, args...)
	if set != "" {
		query += ", " + set
	}
	query += ` WHERE id = ? AND state = ?`
	full = append(full, id, cur.State)
	res, err := s.w.Exec(query, full...)
	if err != nil {
		return Provision{}, nil, fmt.Errorf("store: mark provision %s %s: %w", id, target, err)
	}
	if n, err := res.RowsAffected(); err != nil {
		return Provision{}, nil, fmt.Errorf("store: mark provision %s %s: %w", id, target, err)
	} else if n == 0 {
		// A concurrent mark settled it between the read and the write. The
		// settle-once rule makes that a no-op rather than a failure, so re-read
		// and report what the row says now.
		fresh, err := s.GetProvision(id)
		if err != nil {
			return Provision{}, nil, err
		}
		return fresh, nil, nil
	}
	final, err := s.GetProvision(id)
	if err != nil {
		return Provision{}, nil, err
	}
	return final, nil, nil
}

// GetProvision reads one row by id.
func (s *Store) GetProvision(id string) (Provision, error) {
	row := s.r.QueryRow(`SELECT `+provisionColumns+` FROM environment_provision WHERE id = ?`, id)
	p, err := scanProvision(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Provision{}, fmt.Errorf("store: provision %s: %w", id, ErrNotFound)
	}
	if err != nil {
		return Provision{}, fmt.Errorf("store: read provision %s: %w", id, err)
	}
	return p, nil
}

// liveProvision finds the row that holds an owner's slot. An empty Provision is
// the answer "nothing is live", which is why the caller tests `.ID`.
func (s *Store) liveProvision(componentRoot, loopSlug, owner string) (Provision, error) {
	row := s.r.QueryRow(`SELECT `+provisionColumns+` FROM environment_provision
		WHERE component_root = ? AND loop_slug = ? AND owner = ? AND removed_at IS NULL
		ORDER BY id DESC LIMIT 1`, componentRoot, loopSlug, owner)
	p, err := scanProvision(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Provision{}, nil
	}
	if err != nil {
		return Provision{}, fmt.Errorf("store: read live provision %s/%s/%s: %w", componentRoot, loopSlug, owner, err)
	}
	return p, nil
}

// ListLiveProvisions is what reconcile reads at daemon start (EV12): every row
// whose resource still has to be found or proved gone. Ordered oldest first,
// because the oldest live row is the one most likely to be a leak from a crash.
func (s *Store) ListLiveProvisions() ([]Provision, error) {
	return s.queryProvisions(`WHERE removed_at IS NULL ORDER BY created_at ASC, id ASC`)
}

// ProvisionStates is the closed set of provision states (EV12), in the order a
// listing reads them. One definition, because three callers need the same
// answer — the store's filter, `environment.vocabulary`, and the CLI's
// `--state` completion — and a set typed out twice is a set that drifts, which
// is how a state becomes unnameable while its rows still exist.
func ProvisionStates() []string {
	return []string{
		ProvisionStateProvisioning, ProvisionStateReady, ProvisionStateTearingDown,
		ProvisionStateRemoved, ProvisionStateFailed,
	}
}

// provisionStateKnown is the filter's membership test over that one list.
func provisionStateKnown(state string) bool {
	for _, s := range ProvisionStates() {
		if s == state {
			return true
		}
	}
	return false
}

// ProvisionsFor is one loop's whole ledger, live and settled (EV12's
// per-loop view, EV14's services). Newest first: the panel and `aeon env
// provision list` want the current state on top.
func (s *Store) ProvisionsFor(componentRoot, loopSlug string) ([]Provision, error) {
	return s.queryProvisions(`WHERE component_root = ? AND loop_slug = ? ORDER BY id DESC`,
		componentRoot, loopSlug)
}

// ProvisionsForLoop is the terminal teardown's read (S59 T12, W2-2): one loop's
// rows by the slug the ledger is keyed by, whatever component root they were
// created under. The hook that answers `loop.finished` knows the loop and
// nothing else — making it also supply a root is what hid the defect, because
// with the wrong root AND the wrong column the read answered "no provisions"
// and looked exactly like a loop that had never needed a container.
func (s *Store) ProvisionsForLoop(loopSlug string) ([]Provision, error) {
	return s.queryProvisions(`WHERE loop_slug = ? ORDER BY id DESC`, loopSlug)
}

// ListProvisions is `environment.provisions`' read (S59 T6, EV18): the whole
// ledger — live and settled — optionally narrowed to one state, newest first.
//
// An unnameable state is a REFUSAL and not an empty listing. A filter nobody can
// name answering "no provisions" is the plausible wrong answer this plane keeps
// refusing: the caller would report "nothing is running" when the truth is "you
// asked for a state that does not exist", and a stale `ready` container is
// exactly the thing a person is looking for when they open this read.
func (s *Store) ListProvisions(state string, limit int) ([]Provision, error) {
	if state != "" {
		if !provisionStateKnown(state) {
			return nil, fmt.Errorf("store: %q is not a provision state — the closed set is %s", state, strings.Join(ProvisionStates(), ", "))
		}
	}
	clause := `ORDER BY id DESC`
	args := make([]any, 0, 2)
	if state != "" {
		clause = `WHERE state = ? ` + clause
		args = append(args, state)
	}
	if limit > 0 {
		clause += ` LIMIT ?`
		args = append(args, limit)
	}
	return s.queryProvisions(clause, args...)
}

func (s *Store) queryProvisions(clause string, args ...any) ([]Provision, error) {
	rows, err := s.r.Query(`SELECT `+provisionColumns+` FROM environment_provision `+clause, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list provisions: %w", err)
	}
	defer rows.Close()
	var out []Provision
	for rows.Next() {
		p, err := scanProvision(rows)
		if err != nil {
			return nil, fmt.Errorf("store: scan provision: %w", err)
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list provisions: %w", err)
	}
	return out, nil
}
