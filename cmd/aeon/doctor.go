package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/cjlapao/aeon/internal/backup"
	"github.com/cjlapao/aeon/internal/doctor"
	"github.com/cjlapao/aeon/internal/narrow"
	"github.com/cjlapao/aeon/internal/paths"
	"github.com/cjlapao/aeon/internal/protocol"
)

// doctor.go answers, about the RUNNING process, what a config file cannot
// be trusted to answer: what is actually exposed, right now. O8 named the
// two places the narrow surface must say it is open — at startup (serve.go
// prints narrow.StartupLine) and here — because "a default only helps the
// first time"; six weeks later the thing someone is looking at is the
// running process.
//
// What it can honestly report, and from where:
//
//   - the socket: dialled, like every other verb (this is the primary
//     surface, P34 — the doctor is a client of it too);
//   - the narrow HTTP surface: probed with a CONNECTION ATTEMPT (D7 —
//     reading config would be asking a file what the process is doing),
//     and its address read from the same framework config plane serve set.
//
// The key's VALUE is never printed, consulted, or needed (I6): the
// statement "a key is configured" is structural — serve refuses to bind a
// keyless address at all (T2), so the listener's existence is the proof.
// The environment is read only to add a hint (a key IS set in the
// environment while nothing listens) — existence, never content.

func cmdDoctor(w io.Writer, args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("doctor takes no arguments")
	}

	// The socket leg — dial it like any other verb would. The floor is
	// v0: the doctor asks for the daemon as it is, not a version it
	// hopes for (a legacy v1 floor read a current daemon as too new).
	// The client is held for the backups leg below, which asks the
	// daemon's own status when it is the one that answers.
	var client *protocol.Client
	if c, err := dial(0); err != nil {
		fmt.Fprintf(w, "daemon socket    unreachable: %v\n", err)
	} else {
		client = c
		fmt.Fprintln(w, "daemon socket    reachable — the primary surface (0600 path, peer credentials, P26)")
	}

	// The narrow leg. The address is the framework's config plane as serve
	// set it from the typed flag; unset here means serve never got --http,
	// which is the ordinary state ("nothing binds without an explicit
	// address" is the answer, not a fault).
	addr := doctorAddr()
	listening := false
	if addr != "" {
		if conn, err := net.DialTimeout("tcp", addr, dialTimeout); err == nil {
			_ = conn.Close()
			listening = true
		}
	}
	for _, line := range narrow.DoctorLines(addr, listening, os.Getenv(narrow.KeyEnv) != "") {
		fmt.Fprintln(w, line)
	}

	// The seed leg — one line per set, content-free (ids, versions, states
	// only). A behind-but-working seed is a warning line, not a failure: the
	// doctor's exit code is unchanged by it (SD10), so the line reports drift
	// and points to `aeon seed status`, it never aborts the check.
	seedDoctorLines(w)
	mcpDoctorLines(w)
	blueprintDoctorLines(w)

	// The backup leg (S62 T9, BK15): the database line and the backups
	// lines. The database line is store-free either way (a stat and one
	// read-only connection — the doctor never opens the store, because
	// opening it migrates); the backups lines come from the daemon's
	// own status when the daemon answers, and from the store-free scan
	// when it does not (the header in internal/doctor/backup.go says
	// which facts each source may claim).
	backupDoctorLines(w, client)

	// The notifications leg (S58 T6, SV7): the channels as the daemon
	// sees them — name, HOST ONLY, enabled, last outcome, consecutive
	// failures with three flagged — the inbox source census, and the
	// background capability line. Everything here rides the socket: the
	// doctor opens no store for it, and the view type itself carries no
	// URL path, no secret reference and no credential to print (the
	// renderer cannot print what the struct does not hold).
	notifyDoctorLines(w, client)

	if client != nil {
		_ = client.Close()
	}
	return nil
}

// notifyDoctorLines is the S58 T6 leg. Three states per question: no
// daemon answers "the daemon holds this", a daemon answers with its
// daemon's own words, and a failing method prints its refusal verbatim —
// the doctor never paraphrases another slice's sentence.
func notifyDoctorLines(w io.Writer, client *protocol.Client) {
	if client == nil {
		fmt.Fprintln(w, "notifications    the daemon holds channels, secrets and the source census — start it with `aeon serve`")
	} else {
		var ch protocol.NotifyChannelListResult
		if err := client.Call(protocol.MethodNotifyChannelList, struct{}{}, &ch); err != nil {
			renderChannelFacts(w, ch, err)
		} else {
			renderChannelFacts(w, ch, nil)
		}
		var sum protocol.InboxSummaryResult
		if err := client.Call(protocol.MethodInboxSummary, struct{}{}, &sum); err != nil {
			renderSourceCensus(w, sum, err)
		} else {
			renderSourceCensus(w, sum, nil)
		}
	}
	// The background capability line (T7): a capability the app has not
	// reported is SHOWN as not reported, never absent from the screen —
	// the doctor's own three-state rule.
	fmt.Fprintln(w, "background        not reported — no tray or background agent has told this daemon it exists (S58 T7)")
}

// renderChannelFacts prints the channel lines from the daemon's OWN answer
// — extracted from notifyDoctorLines at S58 T6 so the honesty rule (hosts
// only, never a path, never a secret — the doctor shows what the door
// shows) is testable against crafted facts the way the goldens pin them.
// A refusal error renders verbatim and stops; the doctor paraphrases nothing.
func renderChannelFacts(w io.Writer, ch protocol.NotifyChannelListResult, callErr error) {
	if callErr != nil {
		fmt.Fprintf(w, "notifications    the daemon refused the channel read: %v\n", callErr)
		return
	}
	if len(ch.Channels) == 0 {
		fmt.Fprintln(w, "notifications    0 channels (a channel is born disabled; `notify channel create` makes one)")
		return
	}
	fmt.Fprintf(w, "notifications    %d channel(s)\n", len(ch.Channels))
	for _, c := range ch.Channels {
		state := "enabled"
		if !c.Enabled {
			state = "disabled"
		}
		if c.ArchivedAt != "" {
			state = "archived"
		}
		tail := ""
		if c.LastOutcome != "" {
			tail = "  last " + c.LastOutcome
		}
		flag := ""
		if c.ConsecutiveFailures >= 3 {
			flag = "  FAILING (" + strconv.Itoa(c.ConsecutiveFailures) + " in a row) — deliveries are being dropped"
		}
		fmt.Fprintf(w, "  channel %-20s host %-30s %s%s%s\n", c.Name, c.Host, state, tail, flag)
	}
}

// renderSourceCensus prints the per-source reporting truth from
// inbox.summary's own census (SV1): a source that is not wired reads
// `not-wired` — never zero, never absent.
func renderSourceCensus(w io.Writer, sum protocol.InboxSummaryResult, callErr error) {
	if callErr != nil {
		fmt.Fprintf(w, "notify sources    the daemon refused the census read: %v\n", callErr)
		return
	}
	parts := make([]string, 0, len(sum.Sources))
	for _, src := range sum.Sources {
		parts = append(parts, src.Name+":"+src.State)
	}
	fmt.Fprintf(w, "notify sources    %s\n", strings.Join(parts, "  "))
}

// blueprintDoctorLines reports the blueprint truth in a few lines
// (S48 T4): the DATABASE is authoritative; files the adoption refused are
// listed from the ledger, never silently; and a file that differs from
// what was imported is DRIFT, reported as such because the database wins
// — the doctor never writes, never adopts, never deletes.
//
// It opened with the doctor's shared non-migrating open (openStoreSkipMigrations),
// the way the mcp leg's open was rewired at S62 T9: a health check never
// fires the pre-migration safety net and never migrates the store it is
// checking. The seed leg above it already migrates a fresh store, so the
// tables exist by the time this leg reads them; a store that was never
// opened at all answers "no such table" and the line says unreachable.
func blueprintDoctorLines(w io.Writer) {
	st, err := openStoreSkipMigrations()
	if err != nil {
		fmt.Fprintf(w, "blueprints    unreachable: %v\n", err)
		return
	}
	defer func() { _ = st.Close() }()
	ctx := context.Background()
	rows, err := st.BlueprintList(ctx, "", "")
	if err != nil {
		fmt.Fprintf(w, "blueprints    unreachable: %v\n", err)
		return
	}
	fmt.Fprintf(w, "blueprints    database authoritative (%d blueprints)\n", len(rows))
	refused, err := st.ImportRefusals(ctx)
	if err != nil {
		fmt.Fprintf(w, "blueprints    the refusal ledger is unreadable: %v\n", err)
		return
	}
	if len(refused) > 0 {
		names := make([]string, 0, len(refused))
		for _, r := range refused {
			names = append(names, r.Name)
		}
		fmt.Fprintf(w, "blueprints    %d file(s) refused at adoption (%s) — the ledger holds the diagnostics; fix the file and rename it to retry\n",
			len(refused), strings.Join(names, ", "))
	}
	dir, derr := paths.StoreDir()
	if derr != nil {
		return
	}
	entries, derr := os.ReadDir(filepath.Join(dir, "blueprints"))
	if derr != nil {
		return // no directory: nothing on disk to drift
	}
	drift := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		fi, serr := os.Lstat(filepath.Join(dir, "blueprints", e.Name()))
		if serr != nil || fi.Mode()&os.ModeSymlink != 0 {
			continue
		}
		raw, serr := os.ReadFile(filepath.Join(dir, "blueprints", e.Name()))
		if serr != nil {
			continue
		}
		sum := sha256.Sum256(raw)
		hash := hex.EncodeToString(sum[:])
		if _, ok, err := st.ImportByHash(ctx, hash); err == nil && ok {
			continue // the file is exactly what was imported
		}
		name := strings.TrimSuffix(e.Name(), ".yaml")
		row, ok, err := st.BlueprintGet(ctx, name)
		if err == nil && ok && row.ContentHash != hash {
			drift++ // a row owns the name and the file says otherwise
		}
	}
	if drift > 0 {
		fmt.Fprintf(w, "blueprints    %d file(s) differ from what was imported (the database wins; files are a stale mirror)\n", drift)
	}
}

// mcpDoctorLines reports the MCP configuration truth in ONE line
// (S46 T4, MG3): the database is authoritative, the legacy file is inert
// content that may only be COMPARED. It prints no env value (the counts
// and the validator's own sentence are all it carries — MG13 by shape:
// there is no field here a value could travel through). The ledger is the
// memory of what the importer said; the hash ledger is the memory of what
// it imported — a file edited after adoption shows as drift, never as a
// re-import (only the human door replaces).
//
// S62 T9 (F-T1-1): the open is SkipMigrations — a health check that
// migrates the store would trip the pre-migration hook, and a file that
// does not exist is the level-0 state, not a reason to create one (an
// open that creates the store is an open that migrates it).
func mcpDoctorLines(w io.Writer) {
	stateDir, err := paths.StoreDir()
	if err != nil {
		fmt.Fprintf(w, "mcp           unreachable: %v\n", err)
		return
	}
	if _, err := os.Stat(filepath.Join(stateDir, "state.db")); errors.Is(err, os.ErrNotExist) {
		// No store: the ledger is empty by definition, so the only
		// states this leg can hold are the two the file settles —
		// no file at all (level-0) and a file that adoption has not
		// run on (the daemon has not opened the store since it
		// appeared). Neither needs an open to say.
		if _, ferr := os.Stat(mcpLegacyPath()); errors.Is(ferr, os.ErrNotExist) {
			fmt.Fprintln(w, "mcp           no MCP configuration (level-0)")
		} else {
			fmt.Fprintln(w, "mcp           legacy file not yet adopted (aeon mcp import-config)")
		}
		return
	}
	st, err := openStoreSkipMigrations()
	if err != nil {
		fmt.Fprintf(w, "mcp           unreachable: %v\n", err)
		return
	}
	defer func() { _ = st.Close() }()
	ctx := context.Background()
	all, err := st.McpRows(ctx)
	if err != nil {
		fmt.Fprintf(w, "mcp           unreachable: %v\n", err)
		return
	}
	n := len(all.Servers)
	path := mcpLegacyPath()
	raw, ferr := os.ReadFile(path)
	if n > 0 {
		if ferr != nil {
			fmt.Fprintf(w, "mcp           database authoritative (%d servers)\n", n)
			return
		}
		sum := sha256.Sum256(raw)
		if _, found, lerr := st.McpImportByHash(ctx, hex.EncodeToString(sum[:])); lerr == nil && found {
			fmt.Fprintf(w, "mcp           database authoritative (%d servers)\n", n)
		} else {
			fmt.Fprintf(w, "mcp           legacy file differs from what was imported (aeon mcp import-config --dry-run)\n")
		}
		return
	}
	// Empty tables: level-0, or a file the importer already judged.
	if ferr != nil {
		fmt.Fprintln(w, "mcp           no MCP configuration (level-0)")
		return
	}
	if led, found, lerr := st.McpImportLatestByPath(ctx, path); lerr == nil && found && led.Result == "refused" {
		fmt.Fprintf(w, "mcp           legacy file refused at %s: %s\n", led.ImportedAt, led.Detail)
		return
	}
	// A file present, rows empty, nothing refused: the daemon has not run
	// its one adoption yet (or a replace emptied the world and the file is
	// new) — honest fifth state, pending, never auto-imported here.
	fmt.Fprintln(w, "mcp           legacy file not yet adopted (aeon mcp import-config)")
}

// seedDoctorLines reports, per seed set, whether it is up to date or what is
// out of date (S43-extra T7). It is a client of the same door `aeon seed`
// speaks through (dial, or the direct door if there is no daemon), so the
// doctor and the CLI cannot disagree about the drift. It prints no record
// content, and a behind seed is a warning, never a failure (the exit code is
// unchanged).
func seedDoctorLines(w io.Writer) {
	door, err := seedDoorFactory()
	if err != nil {
		fmt.Fprintf(w, "seed          unreachable: %v\n", err)
		return
	}
	res, err := door.status(context.Background(), "")
	if err != nil {
		fmt.Fprintf(w, "seed          unreachable: %v\n", err)
		return
	}
	if len(res.Sets) == 0 {
		fmt.Fprintln(w, "seed          no seed sets declared")
		return
	}
	for _, s := range res.Sets {
		fmt.Fprintf(w, "seed %s %s\n", s.ID, seedDoctorSetLine(s))
	}
}

// seedDoctorSetLine is the one line per set the doctor prints (SD10): up to
// date, or the count of what is behind, removed, diverged or failed — never
// the content, and always pointing to `aeon seed status` for the detail.
func seedDoctorSetLine(s protocol.SeedSetStatus) string {
	var behind, removed, diverged, failed int
	for _, r := range s.Records {
		switch r.State {
		case "removed":
			removed++
		case "diverged":
			diverged++
		case "failed":
			failed++
		default:
			if r.State != "applied" {
				behind++
			}
		}
	}
	if behind == 0 && removed == 0 && diverged == 0 && failed == 0 {
		return fmt.Sprintf("up to date (%d/%d applied)", s.AppliedVersion, s.AvailableVersion)
	}
	parts := []string{}
	if behind > 0 {
		parts = append(parts, fmt.Sprintf("%d behind", behind))
	}
	if removed > 0 {
		parts = append(parts, fmt.Sprintf("%d removed", removed))
	}
	if diverged > 0 {
		parts = append(parts, fmt.Sprintf("%d diverged", diverged))
	}
	if failed > 0 {
		parts = append(parts, fmt.Sprintf("%d failed", failed))
	}
	return strings.Join(parts, ", ") + " (aeon seed status)"
}

// backupDoctorLines is the backup leg's four lines (S62 T9, BK15): the
// database line, and the backups lines. The source of the backups lines
// follows the socket leg — the daemon's own status when the daemon
// answers (its verdicts are the ledger's, and only the daemon can read
// them), the store-free scan otherwise (which is why the daemon-down
// lines carry no verify token: a line that could not read the verdict
// does not print a guess for it). A status call the daemon refuses is
// an honest line plus the store-free fallback, never a silent pass.
func backupDoctorLines(w io.Writer, c *protocol.Client) {
	stateDir, err := paths.StoreDir()
	if err != nil {
		fmt.Fprintf(w, "backup          unavailable: the state dir will not resolve: %v\n", err)
		return
	}
	db, err := doctor.DatabaseFacts(stateDir)
	if err != nil {
		fmt.Fprintf(w, "backup          the database's facts could not be read: %v\n", err)
		return
	}
	fmt.Fprintln(w, db.Line())

	if c != nil {
		var res protocol.BackupStatusView
		var ls protocol.BackupListResult
		serr := c.Call(protocol.MethodBackupStatus, struct{}{}, &res)
		if serr == nil {
			// The count and the verify token are the list's: the
			// daemon's own join of each file with its newest verify
			// row (the ledger's verdict — the source only the daemon
			// can claim).
			serr = c.Call(protocol.MethodBackupList, protocol.BackupListParams{IncludePreMigration: false}, &ls)
		}
		if serr != nil {
			fmt.Fprintf(w, "backups    the daemon's status will not answer: %v\n", serr)
			b, ferr := doctor.BackupFacts(stateDir, time.Now())
			if ferr != nil {
				fmt.Fprintf(w, "backups    the store-free scan could not read the dirs: %v\n", ferr)
				return
			}
			for _, l := range b.Lines() {
				fmt.Fprintln(w, l)
			}
			return
		}
		b := doctor.Backups{
			Dir:        res.Dir,
			Count:      len(ls.Backups),
			StaleAfter: staleAfterFromSettings(res.Settings),
			Stale:      res.State == "stale",
		}
		if n := len(ls.Backups); n > 0 {
			b.LastAge = time.Duration(ls.Backups[0].AgeSeconds) * time.Second
			b.LastVerified = ls.Backups[0].Verified == "verified"
			if b.Dir == "" && len(ls.Dirs) > 0 {
				b.Dir = ls.Dirs[0]
			}
		}
		for _, l := range b.Lines() {
			fmt.Fprintln(w, l)
		}
		return
	}

	b, err := doctor.BackupFacts(stateDir, time.Now())
	if err != nil {
		fmt.Fprintf(w, "backups    the store-free scan could not read the dirs: %v\n", err)
		return
	}
	for _, l := range b.Lines() {
		fmt.Fprintln(w, l)
	}
}

// staleAfterFromSettings is the retention's limit from the status's
// settings block (the closed set's backup.staleAfter key), the resolver's
// default when the block carries none.
func staleAfterFromSettings(views []protocol.BackupSettingView) time.Duration {
	for _, v := range views {
		if v.Key == backup.KeyStaleAfter {
			if d, err := time.ParseDuration(v.Value); err == nil {
				return d
			}
		}
	}
	return backup.DefaultStaleAfter
}

// doctorAddr rebuilds the bound address from the framework's config-plane
// environment names (the same two serve sets — SERVER_BIND_ADDRESS is the
// framework's own explicit env tag, measured on v0.10.1, and SERVER_API_PORT
// the generated one). No address in the plane is not a guess that it is
// localhost: it is "serve was not given --http", which DoctorLines says in
// those words.
func doctorAddr() string {
	host := strings.TrimSpace(os.Getenv("SERVER_BIND_ADDRESS"))
	port := strings.TrimSpace(os.Getenv("SERVER_API_PORT"))
	if host == "" || port == "" {
		return ""
	}
	return net.JoinHostPort(host, port)
}
