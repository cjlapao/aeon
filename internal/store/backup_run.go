package store

// backup_run.go — the append-only record of every backup, verify, prune,
// restore-plan and export (design BK10). It is the source of "last success",
// "last verified" and "age": backup.status computes them from these rows, and
// nothing else may. History outlives files (P3), so file_name is a reference,
// never a foreign key, and the triggers make history unalterable — a ledger
// that can be edited is a ledger that can lie.
//
// The closed sets live in two places on purpose: the SQL CHECKs are the
// backstop (a raw statement cannot leak a vocabulary), and the Go refusals
// are the person-facing sentence in front of them (DB19). They are held in
// lockstep by the tests, not by review.

import (
	"database/sql"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// The closed sets. A value nobody compiled here is a value the ledger refuses
// (the CHECKs say the same, and a test keeps the two from drifting).
var (
	backupRunKinds           = setOf("backup", "verify", "prune", "restore-plan", "export")
	backupRunReasons         = setOf("manual", "scheduled", "pre-migration", "post-restore-check")
	backupRunActors          = setOf("user", "daemon", "cli") // never "agent": agents have no write path here
	backupRunOutcomes        = setOf("ok", "failed", "refused", "skipped")
	backupRunErrorKinds      = setOf("disk-full", "dir-unwritable", "busy", "lock-held", "integrity", "fk", "checksum", "newer-schema", "io", "cancelled", "unknown")
	backupRunVerifyLevels    = setOf("written", "verified", "drill")
	backupRunFreeSpaceStates = setOf("ok", "low", "unknown")
)

func setOf(values ...string) map[string]bool {
	m := make(map[string]bool, len(values))
	for _, v := range values {
		m[v] = true
	}
	return m
}

// backupRunFileNamePattern is the Go twin of the migration's GLOB on
// file_name. The two are held in lockstep by
// TestBackupRunFileNameGlobMatchesGoRegex: a name the SQL admits and the Go
// regex refuses (or the reverse) is a name one of the two doors will call a
// backup and the other will not. The shape is BK4's:
// aeon-state-<UTC yyyymmddThhmmssZ>-v<applied_migrations>.aeonbak, with the
// GLOB's looseness kept exactly (the ? are any character, the * any run).
var backupRunFileNamePattern = regexp.MustCompile(`^aeon-state-........T......Z-v.*\.aeonbak$`)

// backupRunDetailMax is the detail's size cap in BYTES (the SQL CHECK counts
// characters; ASCII — which the fixed templates are — makes the two agree).
const backupRunDetailMax = 512

// BackupRun is one ledger row.
type BackupRun struct {
	ID                int64
	Kind              string
	Reason            string
	ActorKind         string
	StartedAt         string
	FinishedAt        string
	Outcome           string
	ErrorKind         *string
	Detail            string
	FileName          *string
	SizeBytes         *int64
	SHA256            *string
	AppliedMigrations *int64
	VerifyLevel       *string
	FreeSpaceState    string
}

// NewBackupRun is a caller's completed run, ready to be recorded. The
// nullable columns are empty strings / nil pointers; "" means NULL, never a
// zero value (zero is a real size, not an absence).
type NewBackupRun struct {
	Kind              string
	Reason            string
	ActorKind         string
	StartedAt         string // the caller's ISO-8601 UTC start; the store stamps finished_at itself (I9)
	Outcome           string
	ErrorKind         string // "" = NULL
	Detail            string // a fixed template, never raw error text (SEC-033)
	FileName          string // "" = NULL; the basename, never a path
	SizeBytes         *int64
	SHA256            string // "" = NULL
	AppliedMigrations *int64
	VerifyLevel       string // "" = NULL
	FreeSpaceState    string
}

// BackupRunRefusal is a refused ledger write. It is a result, not an error
// (DB19): it names the reason and the thing refused, and the caller renders
// it verbatim. The CHECKs are the backstop behind it.
type BackupRunRefusal struct {
	Kind   string
	Detail string
}

func (r BackupRunRefusal) Error() string { return fmt.Sprintf("refused: %s · %s", r.Kind, r.Detail) }

// detailHasPathSeparator is the cheap tripwire against raw error text: a path
// in a detail is a path that could name the operator's files (SEC-033), and
// the fixed templates never produce one. Any `/` or `\` refuses — the
// file_name column is where the one allowed name lives.
func detailHasPathSeparator(detail string) bool {
	return strings.ContainsRune(detail, '/') || strings.ContainsRune(detail, '\\')
}

// validateBackupRun is the closed set of person-facing refusals: a value
// outside a closed set, a non-ISO start time, a detail over the cap, or a
// detail that looks like it carries a path. A nil result means "admitted by
// the store" (the CHECKs still backstop the database).
func validateBackupRun(in NewBackupRun) (BackupRunRefusal, bool) {
	check := func(field string, value string, allowed map[string]bool) (BackupRunRefusal, bool) {
		if !allowed[value] {
			return BackupRunRefusal{Kind: field, Detail: fmt.Sprintf("%s %q is not one of the closed set", field, value)}, true
		}
		return BackupRunRefusal{}, false
	}
	if r, bad := check("kind", in.Kind, backupRunKinds); bad {
		return r, true
	}
	if r, bad := check("reason", in.Reason, backupRunReasons); bad {
		return r, true
	}
	// The actor is a parameter the caller's door sets (user socket, daemon
	// ticker, cli direct door) — never read from JSON (BK10/P3). Anything
	// outside the three, "agent" included, is refused here.
	if r, bad := check("actor_kind", in.ActorKind, backupRunActors); bad {
		return r, true
	}
	if r, bad := check("outcome", in.Outcome, backupRunOutcomes); bad {
		return r, true
	}
	if r, bad := check("free_space_state", in.FreeSpaceState, backupRunFreeSpaceStates); bad {
		return r, true
	}
	if in.ErrorKind != "" {
		if r, bad := check("error_kind", in.ErrorKind, backupRunErrorKinds); bad {
			return r, true
		}
	}
	if in.VerifyLevel != "" {
		if r, bad := check("verify_level", in.VerifyLevel, backupRunVerifyLevels); bad {
			return r, true
		}
	}
	if in.StartedAt == "" {
		return BackupRunRefusal{Kind: "started_at", Detail: "the run's start time is required (ISO-8601 UTC)"}, true
	}
	if _, err := time.Parse("2006-01-02T15:04:05.000Z", in.StartedAt); err != nil {
		return BackupRunRefusal{Kind: "started_at", Detail: fmt.Sprintf("started_at %q is not ISO-8601 UTC (2006-01-02T15:04:05.000Z)", in.StartedAt)}, true
	}
	if len([]byte(in.Detail)) > backupRunDetailMax {
		return BackupRunRefusal{Kind: "detail", Detail: fmt.Sprintf("detail is %d bytes, the cap is %d — the templates are fixed and short; a long detail is raw error text wearing a template", len([]byte(in.Detail)), backupRunDetailMax)}, true
	}
	if detailHasPathSeparator(in.Detail) {
		return BackupRunRefusal{Kind: "detail", Detail: "detail carries a path separator; detail is a fixed template, paths live in file_name (SEC-033)"}, true
	}
	if in.FileName != "" && !backupRunFileNamePattern.MatchString(in.FileName) {
		return BackupRunRefusal{Kind: "file_name", Detail: fmt.Sprintf("file_name %q is not an .aeonbak basename (aeon-state-<UTC yyyymmddThhmmssZ>-v<applied>.aeonbak)", in.FileName)}, true
	}
	return BackupRunRefusal{}, false
}

// RecordBackupRun appends one row. The finished_at is the store's clock (I9)
// at the moment of the record; the started_at is the caller's — the store is
// the only one who may stamp its own time, and the caller is the only one who
// knows when the work began.
func (s *Store) RecordBackupRun(in NewBackupRun) (BackupRun, error) {
	if refusal, bad := validateBackupRun(in); bad {
		return BackupRun{}, refusal
	}
	finishedAt := s.Now()

	var (
		errorKind, sha256, verifyLevel, fileName sql.NullString
		sizeBytes, appliedMigrations             sql.NullInt64
	)
	errorKind.String, errorKind.Valid = in.ErrorKind, in.ErrorKind != ""
	sha256.String, sha256.Valid = in.SHA256, in.SHA256 != ""
	verifyLevel.String, verifyLevel.Valid = in.VerifyLevel, in.VerifyLevel != ""
	fileName.String, fileName.Valid = in.FileName, in.FileName != ""
	if in.SizeBytes != nil {
		sizeBytes.Int64, sizeBytes.Valid = *in.SizeBytes, true
	}
	if in.AppliedMigrations != nil {
		appliedMigrations.Int64, appliedMigrations.Valid = *in.AppliedMigrations, true
	}

	res, err := s.w.Exec(`INSERT INTO backup_run (
		kind, reason, actor_kind, started_at, finished_at, outcome, error_kind,
		detail, file_name, size_bytes, sha256, applied_migrations, verify_level, free_space_state
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		in.Kind, in.Reason, in.ActorKind, in.StartedAt, finishedAt, in.Outcome, errorKind,
		in.Detail, fileName, sizeBytes, sha256, appliedMigrations, verifyLevel, in.FreeSpaceState)
	if err != nil {
		return BackupRun{}, fmt.Errorf("store: record the backup run: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return BackupRun{}, fmt.Errorf("store: read the backup run's id: %w", err)
	}
	return BackupRun{
		ID:                id,
		Kind:              in.Kind,
		Reason:            in.Reason,
		ActorKind:         in.ActorKind,
		StartedAt:         in.StartedAt,
		FinishedAt:        finishedAt,
		Outcome:           in.Outcome,
		ErrorKind:         nullStringPtr(errorKind),
		Detail:            in.Detail,
		FileName:          nullStringPtr(fileName),
		SizeBytes:         nullIntPtr(sizeBytes),
		SHA256:            nullStringPtr(sha256),
		AppliedMigrations: nullIntPtr(appliedMigrations),
		VerifyLevel:       nullStringPtr(verifyLevel),
		FreeSpaceState:    in.FreeSpaceState,
	}, nil
}

// BackupRunFilter is a read of the ledger. Empty strings mean "any"; newest
// first. The limit is capped at 500: the ledger is a status source, not an
// export (the export has its own door).
type BackupRunFilter struct {
	Kind    string
	Reason  string
	Outcome string
	Limit   int
}

const backupRunListCap = 500

func (f BackupRunFilter) capLimit() int {
	if f.Limit <= 0 || f.Limit > backupRunListCap {
		return backupRunListCap
	}
	return f.Limit
}

// ListBackupRuns reads the ledger newest first (finished_at, then id for the
// same-instant tie), with the closed-set filters applied as equality.
func (s *Store) ListBackupRuns(f BackupRunFilter) ([]BackupRun, error) {
	query := `SELECT id, kind, reason, actor_kind, started_at, finished_at, outcome, error_kind,
		detail, file_name, size_bytes, sha256, applied_migrations, verify_level, free_space_state
		FROM backup_run`
	var (
		conds []string
		args  []any
	)
	if f.Kind != "" {
		conds = append(conds, "kind = ?")
		args = append(args, f.Kind)
	}
	if f.Reason != "" {
		conds = append(conds, "reason = ?")
		args = append(args, f.Reason)
	}
	if f.Outcome != "" {
		conds = append(conds, "outcome = ?")
		args = append(args, f.Outcome)
	}
	if len(conds) > 0 {
		query += " WHERE " + strings.Join(conds, " AND ")
	}
	query += " ORDER BY finished_at DESC, id DESC LIMIT ?"
	args = append(args, f.capLimit())

	rows, err := s.r.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list the backup runs: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []BackupRun
	for rows.Next() {
		var (
			run BackupRun
			er  sql.NullString
			fi  sql.NullString
			sz  sql.NullInt64
			sh  sql.NullString
			am  sql.NullInt64
			vl  sql.NullString
		)
		if err := rows.Scan(&run.ID, &run.Kind, &run.Reason, &run.ActorKind, &run.StartedAt, &run.FinishedAt,
			&run.Outcome, &er, &run.Detail, &fi, &sz, &sh, &am, &vl, &run.FreeSpaceState); err != nil {
			return nil, fmt.Errorf("store: read a backup run: %w", err)
		}
		run.ErrorKind = nullStringPtr(er)
		run.FileName = nullStringPtr(fi)
		run.SizeBytes = nullIntPtr(sz)
		run.SHA256 = nullStringPtr(sh)
		run.AppliedMigrations = nullIntPtr(am)
		run.VerifyLevel = nullStringPtr(vl)
		out = append(out, run)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: read the backup runs: %w", err)
	}
	return out, nil
}

// LastBackupRun is the newest row of one (kind, outcome) pair — the "last
// success" and "last verified" of backup.status. Nil when the pair has never
// been recorded, which the caller must render as "never", not as "unknown"
// (three-state honesty, P31).
func (s *Store) LastBackupRun(kind, outcome string) (*BackupRun, error) {
	if !backupRunKinds[kind] {
		return nil, BackupRunRefusal{Kind: "kind", Detail: fmt.Sprintf("kind %q is not one of the closed set", kind)}
	}
	if !backupRunOutcomes[outcome] {
		return nil, BackupRunRefusal{Kind: "outcome", Detail: fmt.Sprintf("outcome %q is not one of the closed set", outcome)}
	}
	runs, err := s.ListBackupRuns(BackupRunFilter{Kind: kind, Outcome: outcome, Limit: 1})
	if err != nil {
		return nil, err
	}
	if len(runs) == 0 {
		return nil, nil
	}
	return &runs[0], nil
}

func nullIntPtr(n sql.NullInt64) *int64 {
	if !n.Valid {
		return nil
	}
	v := n.Int64
	return &v
}
