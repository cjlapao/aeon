// Package backup is Aeon's S62 backup engine: one-file online backups of the
// state database, verification in three honest levels, retention, and the
// single-flight and free-space rules that keep a backup a fact instead of a
// hope.
//
// Confidentiality (BK2). A backup IS the database: it contains every row in
// it — the loops, the autonomy history, the budgets, the event log. It
// contains NO credential, key or token (F13/F14: route keys live in
// separate files under credentials/, the cursor signing key is cursor.key,
// and webhook/HMAC secrets are stored as environment-variable NAMES, not
// values), and it NEVER leaves the machine — nothing in this package opens a
// socket, dials a host or imports a network package (a guard test keeps it
// that way). It is 0600 in a 0700 directory from the moment it exists.
//
// The one-file promise (BK3). The artefact is a plain tar with exactly two
// members — manifest.json, then state.db — and the database inside it is
// journal_mode=DELETE, so the file stands alone: no -wal, no -shm beside it.
// A two-file backup copied by half is undetectable corruption, and one file
// is the unit the checksum, the container and the restore all think in.
//
// The honesty promise. Every run is recorded in the store's backup_run
// ledger (T3) with a closed-set outcome and error kind; verification has
// three levels (written / verified / drill) and a backup is shown as
// unverified until level 2 has actually been run on it; the free-space
// answer is three-state (ok / low / unknown) and "cannot say" is never a
// silent pass (BK17); a backup from a newer schema is refused for levels 2
// and 3 with the unknown migration named, not guessed at.
//
// This package contains no SQL — the snapshot, the file checks and the
// ledger calls are store functions (T2/T3), reached through the small
// interfaces in run.go so a test never needs a real store.
package backup
