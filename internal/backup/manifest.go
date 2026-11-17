package backup

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"time"
)

// ManifestFormat is the only format this binary writes and reads. A file
// naming a higher format is from a newer binary; a lower one is from a
// binary this one has outgrown. Both are answered by the version checks,
// never by a guess.
const ManifestFormat = 1

// createdFormat is the manifest's instant, with milliseconds — the same
// shape the store stamps on backup_run.started_at, so a manifest and its
// ledger row read in one voice.
const createdFormat = "2006-01-02T15:04:05.000Z"

// The manifest's reason is a subset of the ledger's: a file is made by one
// of these four hands (T3's ledger admits exactly these as reason).
// post-restore-check is RESERVED and never used: the hand it was named for
// (T7's restore door) records no ledger row at all — a restore is not
// recorded in the database it replaces (BK10) — and the value stays in the
// set so the lockstep with the ledger holds.
var manifestReasons = map[string]bool{
	"manual":             true,
	"scheduled":          true,
	"pre-migration":      true,
	"post-restore-check": true,
}

// sha256hex is the shape of a 64-character hex digest — db_sha256 and
// schema_fingerprint_sha256 both carry it, and a shorter or foreign string
// is a manifest that has been edited, not one this package wrote.
var sha256hex = regexp.MustCompile(`^[0-9a-f]{64}$`)

// DefaultOutsideDB is the list the manifest's outside_db field carries while
// S46/S48 have not taught the package what is outside the database on a
// given machine. T0 measured it (2026-11-02, walked against a full demo
// store): the state dir holds state.db with its live sidecars — the
// database, and what the backup copies — plus the cursor signing key and
// the per-route credential files, which the backup does NOT contain. The
// legacy MCP config lives outside the state dir entirely. S46 (mcp as a
// database record) and S48 (the blueprint store) will shrink this list;
// until then it is a code constant, and a test keeps it credential-free in
// wording: identifiers only, never a path.
var DefaultOutsideDB = []string{
	"cursor.key (the cursor signing key, in the state dir)",
	"credentials/ (the per-route key files, in the state dir)",
	"the legacy MCP config file (outside the state dir)",
}

// Manifest is the archive's first member: a closed shape, read strictly
// (unknown fields refused) and validated on every read. It is the backup's
// own statement of what it is — counts and digests only, no paths, no row
// content (BK2/SEC-033): the tables map is names to counts, and
// credentials_included is always false because a true one is a lie this
// package's writer can never produce.
type Manifest struct {
	Format               int              `json:"format"`
	Name                 string           `json:"name"`
	CreatedAt            string           `json:"created_at"`
	Reason               string           `json:"reason"`
	AeonVersion          string           `json:"aeon_version"`
	AppliedMigrations    int              `json:"applied_migrations"`
	LastMigration        string           `json:"last_migration"`
	SchemaFingerprintSHA string           `json:"schema_fingerprint_sha256"`
	Tables               map[string]int64 `json:"tables"`
	DBBytes              int64            `json:"db_bytes"`
	DBSHA256             string           `json:"db_sha256"`
	JournalModeInFile    string           `json:"journal_mode_in_file"`
	CredentialsIncluded  bool             `json:"credentials_included"`
	Confidential         bool             `json:"confidential"`
	OutsideDB            []string         `json:"outside_db"`
	Verified             string           `json:"verified"`
}

// Marshal renders the manifest deterministically: the struct's declared
// field order and the map's sorted keys, so two writes of the same fact
// produce the same bytes and the archive's first member is diff-able.
func (m Manifest) Marshal() ([]byte, error) {
	b, err := json.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("backup: render the manifest: %w", err)
	}
	return b, nil
}

// DecodeManifest parses and validates one. It refuses unknown fields (a
// newer format wearing an older face), trailing data, and every shape the
// writer cannot have produced.
func DecodeManifest(b []byte) (Manifest, error) {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	var m Manifest
	if err := dec.Decode(&m); err != nil {
		return Manifest{}, fmt.Errorf("refused: the manifest will not parse (%v)", err.Error())
	}
	tok, err := dec.Token()
	if err == nil && tok != nil {
		return Manifest{}, fmt.Errorf("refused: the manifest carries trailing data after its JSON object")
	}
	if err := m.Validate(); err != nil {
		return Manifest{}, err
	}
	return m, nil
}

// Validate is the closed-shape check: everything the writer guarantees,
// asserted on the read side where a hand-edited file has to meet its
// maker's standard.
func (m Manifest) Validate() error {
	if m.Format != ManifestFormat {
		return fmt.Errorf("refused: the manifest is format %d, this binary is format %d", m.Format, ManifestFormat)
	}
	if m.Name != Name {
		return fmt.Errorf("refused: the manifest names %q, a backup is named %q", m.Name, Name)
	}
	if _, err := time.Parse(createdFormat, m.CreatedAt); err != nil {
		return fmt.Errorf("refused: the manifest's created_at %q is not ISO-8601 UTC with milliseconds", m.CreatedAt)
	}
	if !manifestReasons[m.Reason] {
		return fmt.Errorf("refused: the manifest's reason %q is not one of manual, scheduled, pre-migration, post-restore-check", m.Reason)
	}
	if m.AeonVersion == "" {
		return fmt.Errorf("refused: the manifest carries no aeon_version")
	}
	if m.AppliedMigrations < 0 {
		return fmt.Errorf("refused: the manifest's applied_migrations is negative")
	}
	if !sha256hex.MatchString(m.SchemaFingerprintSHA) {
		return fmt.Errorf("refused: the manifest's schema_fingerprint_sha256 is not a 64-hex digest")
	}
	if !sha256hex.MatchString(m.DBSHA256) {
		return fmt.Errorf("refused: the manifest's db_sha256 is not a 64-hex digest")
	}
	if m.DBBytes < 0 {
		return fmt.Errorf("refused: the manifest's db_bytes is negative")
	}
	if m.Tables == nil {
		return fmt.Errorf("refused: the manifest carries no table counts")
	}
	if m.CredentialsIncluded {
		return fmt.Errorf("refused: the manifest claims credentials_included — no backup this package writes ever does")
	}
	if !m.Confidential {
		return fmt.Errorf("refused: the manifest is not marked confidential; a backup of the state database always is")
	}
	if m.Verified != "unverified" && m.Verified != "verified" {
		return fmt.Errorf("refused: the manifest's verified state %q is not unverified or verified", m.Verified)
	}
	return nil
}

// Created is the manifest's instant, parsed.
func (m Manifest) Created() time.Time {
	t, err := time.Parse(createdFormat, m.CreatedAt)
	if err != nil {
		return time.Time{}
	}
	return t
}

// journalModeDelete is the one-file proof: the snapshot is switched to
// DELETE before close, so the archive's database has no sidecars. A file
// answering anything else was made by a path this package did not take.
const journalModeDelete = "delete"
