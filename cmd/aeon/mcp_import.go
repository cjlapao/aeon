package main

// ════════════════════════════════════════════════════════════════════════════
// S46 T4 — MG3's adoption: the legacy mcp.json carried into the tables
// ONCE, automatically, all-or-nothing, never silently; and the deliberate
// human re-import behind mcp.importFile.
//
// ONE importer function, two callers (Q1's rule, carried from S34's config
// read): the daemon's start (adoptLegacy, actor system:startup) and the
// socket door mcp.importFile (actor from the peer credential, stamped by
// the dispatch layer — never a param). They share every law:
//
//   - ONCE: the import LEDGER (mcp_import) keys on the file's sha256 —
//     the same bytes adopted are never adopted twice (a restart writes
//     nothing, TestAdoptIsIdempotentByHash).
//   - NEVER MERGED INTO A POPULATED WORLD: a start that finds rows already
//     there answers "skipped" and touches nothing — the tables are the
//     truth and a file never rewrites them unasked. Only the human door,
//     mode "replace", overwrites, and it does so as a WIPE-then-insert in
//     one transaction (MG3's replace, never a merge).
//   - ALL-OR-NOTHING: one parse, one Validate (the one validator — MC11's
//     law that grammar is decided in one place), one agent-resolution pass
//     (S43 T2's rule: an unresolvable agent key refuses the WHOLE import,
//     naming every unresolved key), one transaction for the rows + ledger
//     + audit. There is no code path that writes some rows and refuses
//     the rest.
//   - NEVER SILENT: a refused file gets a 'refused' ledger row carrying
//     the validator's own sentence (path and field inside it, the seam
//     adds no interpretation), the daemon still starts at LEVEL-0 (empty
//     tables — an invalid file is not a secret empty config that pretends
//     to be a decision), the line is LOGGED, and doctor reports it
//     (doctor.go's mcp line reads the ledger through McpImportLatestByPath).
//   - THE FILE IS NEVER TOUCHED: no write, no rename, no delete, ever —
//     after adoption it is inert content that doctor may only COMPARE
//     (drift is reported, never re-imported unasked). TestAdoptNeverTouchesTheFile
//     pins inode, mtime and bytes.
//   - NO CREDENTIAL IN ANY ANSWER: env entries are validated NAMES (the
//     validator refuses a value-shaped entry, so a file that smuggles
//     values never lands); every result here carries names and counts only
//     (MG13).
// ════════════════════════════════════════════════════════════════════════════

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/cjlapao/aeon/internal/mcp"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

// The importer's outcomes — the words the ledger, the log line, and the
// method result all speak. They are constants, not ad-hoc strings, because
// serve.go's log switch and the tests all match on them.
const (
	importNoFile   = "no-file"          // nothing to adopt; say nothing, do nothing
	importAlready  = "already-imported" // this content's hash is in the ledger
	importSkipped  = "skipped"          // rows already exist — never merged silently
	importRefused  = "refused"          // validator or resolution said no; ledger records it
	importImported = "imported"         // the startup adoption landed
	importReplaced = "replaced"         // the human door's replace landed
	importDryRun   = "dry-run"          // the plan answered; nothing was written
)

// AdoptResult is the startup answer — the four facts serve.go's one log
// line needs and nothing more (a line that recites the file would be a
// leak waiting to happen; this type cannot hold one).
type AdoptResult struct {
	Outcome string
	Path    string
	Sha     string
	Servers int
	Detail  string
}

// adoptLegacy is the daemon's start path: actor system:startup, replace
// semantics ONLY when the tables are empty (the empty check happens before
// any write, MG3), the legacy path resolved by its one sanctioned owner.
// An error return is reserved for infrastructure (the store refused);
// every domain answer — no-file, already, skipped, refused — is an
// Outcome, not an error, because the daemon must keep starting either way
// (level-0 on a refused file is the loud non-silent state, not a crash).
func adoptLegacy(ctx context.Context, st *store.Store, path string) (AdoptResult, error) {
	if path == "" {
		path = mcpLegacyPath()
	}
	a := newMCPAdmin(st)
	res, err := a.importFromFile(ctx, importRequest{
		Path: path, Mode: "replace", Startup: true,
		ActorKind: "system", ActorID: "startup",
	})
	if err != nil {
		return AdoptResult{}, err
	}
	return AdoptResult{Outcome: res.Outcome, Path: res.Path, Sha: res.Detail, Servers: res.Servers, Detail: detailOf(res)}, nil
}

// detailOf keeps AdoptResult's Detail the human sentence (the result shape
// carries detail plus diff; the wrapper wants only the sentence).
func detailOf(r protocol.McpImportFileResult) string { return r.Detail }

// importRequest is the shared call shape; Startup marks the automatic path
// (hash-idempotence + non-empty-skip + no wipe), the zero value the human
// door.
type importRequest struct {
	Path      string
	Mode      string // "dry-run" | "replace"
	Startup   bool
	ActorKind string
	ActorID   string
}

// importFromFile is THE importer — one function, both doors. It reads the
// file through mcp.LoadConfigFile (the one sanctioned read; this file is on
// the grep-test's allow-list precisely because MG3's adoption is the file's
// ONE legitimate runtime reader), validates through Validate's own verdict,
// resolves every agent key against the agent tables WITHOUT ever
// fabricating one, and lands rows + ledger + audit in one transaction or
// nothing at all.
func (a *mcpAdmin) importFromFile(ctx context.Context, in importRequest) (protocol.McpImportFileResult, error) {
	res := protocol.McpImportFileResult{Mode: in.Mode, Path: in.Path}

	// (1) The file: absent is a state, not a fault.
	raw, err := os.ReadFile(in.Path)
	if errors.Is(err, os.ErrNotExist) {
		res.Outcome = importNoFile
		return res, nil
	}
	if err != nil {
		return protocol.McpImportFileResult{}, fmt.Errorf("read the legacy MCP file: %w", err)
	}
	sum := sha256.Sum256(raw)
	sha := hex.EncodeToString(sum[:])
	res.Path = in.Path

	// (2) The ledger answers ONCE before anything else: the same bytes are
	// never adopted twice (the unique index on the imported hash makes the
	// ledger itself enforce it — a double-adopt would die at insert).
	if in.Startup {
		if led, found, lerr := a.st.McpImportByHash(ctx, sha); lerr != nil {
			return protocol.McpImportFileResult{}, lerr
		} else if found {
			res.Outcome = importAlready
			res.Servers = led.Servers
			return res, nil
		}
		// (3) Never merge into a populated world.
		nonEmpty, eerr := a.st.McpIsEmpty(ctx)
		if eerr != nil {
			return protocol.McpImportFileResult{}, eerr
		}
		if !nonEmpty { // rows exist
			res.Outcome = importSkipped
			res.Detail = "the database already has MCP configuration — the legacy file was not merged into it (MG3); only mcp.importFile mode:replace rewrites the tables"
			return res, nil
		}
	}

	// (4) One parse, one validator: LoadConfigFile parses AND validates
	// (MC11's law — grammar decided in exactly one place). A refusal
	// writes ONE ledger row and nothing else.
	cfg, err := mcp.LoadConfigFile(in.Path)
	if err != nil && !errors.Is(err, mcp.ErrNoConfig) {
		// The ledger sentence carries the PATH the validator was speaking
		// about (the validator's own sentence names the field; the file is
		// the fact only this caller knows) — MG3's "the sentence includes
		// file path and field", built from the one validator's words,
		// never a paraphrase of them.
		sent := fmt.Sprintf("%s: %v", in.Path, err)
		if werr := a.writeRefusedLedger(ctx, in.Path, sha, sent); werr != nil {
			return protocol.McpImportFileResult{}, werr
		}
		res.Outcome = importRefused
		res.Detail = sent
		return res, nil
	}
	if errors.Is(err, mcp.ErrNoConfig) {
		// An empty/blank file: LoadConfigFile answers legitimate absence —
		// for the importer, an empty world is a VALID import of nothing.
		cfg = mcp.Config{}
	}

	// (5) Resolve every agent key FIRST (S43 T2's all-or-nothing, read
	// side): any unresolvable key refuses the whole import, naming every
	// unresolved key. No agent is ever fabricated here — an import adopts
	// configuration for agents that EXIST.
	slots := map[string]string{} // dispatch key → slug
	var unresolved []string
	resolve := func(key string) {
		if key == "" {
			return
		}
		if _, done := slots[key]; done {
			return
		}
		ag, found, rerr := a.st.AgentByName(key)
		if rerr != nil {
			unresolved = append(unresolved, fmt.Sprintf("%s (the store refused the lookup: %v)", key, rerr))
			return
		}
		if !found {
			unresolved = append(unresolved, key)
			return
		}
		slots[key] = ag.Slug
	}
	for key := range cfg.Agents {
		resolve(key)
	}
	for _, s := range cfg.Public {
		_ = s // public servers bind to no agent
	}
	for _, ac := range cfg.Agents {
		for _, s := range ac.Private {
			// Private servers live under their agent's key — already
			// resolved by the map walk; nothing extra to name.
			_ = s
		}
	}
	if len(unresolved) > 0 {
		sort.Strings(unresolved)
		sent := fmt.Sprintf("%s: agents that do not exist — %s (an import never fabricates agents and never lands half a world; declare the agents first, or edit the file)", in.Path, strings.Join(unresolved, ", "))
		if werr := a.writeRefusedLedger(ctx, in.Path, sha, sent); werr != nil {
			return protocol.McpImportFileResult{}, werr
		}
		res.Outcome = importRefused
		res.Detail = sent
		return res, nil
	}

	// (6a) dry-run: answer the diff, write NOTHING — not even a ledger
	// row, because a plan is not a fact.
	if in.Mode == "dry-run" {
		res.Outcome = importDryRun
		res.Diff = a.diffAgainstRows(ctx, cfg)
		if res.Diff == nil {
			return protocol.McpImportFileResult{}, errors.New("read the current rows for the diff")
		}
		return res, nil
	}

	// (6b) the landing: one transaction, everything or nothing.
	cfg = normalizeAgents(cfg) // the two-empties law survives the mirror
	rows := mcp.Disassemble(cfg)
	now := a.now().UTC().Format(time.RFC3339)

	// Before-grants for the replace answer (read BEFORE the Tx — the
	// immediate-lock writer starves nested handle reads, the measured T3
	// law).
	var affected []protocol.McpAffectedGrants
	if in.Mode == "replace" {
		var aerr error
		affected, aerr = a.beforeAfterGrants(ctx, cfg)
		if aerr != nil {
			return protocol.McpImportFileResult{}, aerr
		}
	}

	toolsN, declaresN := 0, 0
	for _, s := range rows.Servers {
		toolsN += len(s.Tools)
	}
	for _, ag := range rows.Agents {
		declaresN += len(ag.Declares)
	}

	// MG3's ONCE law is STRUCTURAL: the ledger holds at most one
	// 'imported' row per content hash (partial unique index). Read the
	// fact BEFORE the transaction (a handle read inside the immediate-lock
	// writer starves against its own connection — the measured T3 law): a
	// deliberate replace of bytes already imported once replaces the
	// CONFIG (audited, every time) and leaves the LEDGER alone — the
	// historical fact "this content was imported" stands; churn would be
	// the ledger lying about when.
	ledgerHasHash := false
	if _, found, lerr := a.st.McpImportByHash(ctx, sha); lerr != nil {
		return protocol.McpImportFileResult{}, lerr
	} else if found {
		ledgerHasHash = true
	}

	err = a.st.McpWithTx(ctx, func(tx *sql.Tx) error {
		if in.Mode == "replace" {
			if werr := store.McpWipeConfigTx(ctx, tx); werr != nil {
				return werr
			}
		}
		for _, s := range rows.Servers {
			args, e1 := mcpJSONArrayFor(s.Transport.Args)
			env, e2 := mcpJSONArrayFor(s.Transport.Env)
			if e1 != nil || e2 != nil {
				return fmt.Errorf("mcp: encode server %s arrays: %v%v", s.Name, e1, e2)
			}
			r := store.McpServerRow{
				Name: s.Name, Tier: string(s.Tier),
				TransportType: string(s.Transport.Type), Command: s.Transport.Command,
				Args: args, Env: env, URL: s.Transport.URL,
				Enabled: true, Origin: "import", CreatedAt: now, UpdatedAt: now,
			}
			if s.Tier == mcp.TierPrivate {
				slug := slots[s.AgentKey]
				r.AgentSlug = &slug
			}
			for _, t := range s.Tools {
				r.Tools = append(r.Tools, store.McpToolRow{Name: t.Name, Rerun: t.Rerun, Effect: t.Effect})
			}
			if _, aerr := store.McpAddServerTx(ctx, tx, r); aerr != nil {
				return aerr
			}
			summary := fmt.Sprintf("import from %s: server %s (%s, %d tools)", in.Path, s.Name, s.Tier, len(s.Tools))
			name := s.Name
			if cerr := store.McpChangeInsertTx(ctx, tx, store.McpChange{
				Op: "import", Server: &name, Summary: summary,
				ActorKind: in.ActorKind, ActorID: in.ActorID, OccurredAt: now,
			}); cerr != nil {
				return cerr
			}
		}
		// Declarations + narrowings, then the ONE declarations audit row.
		var declParts []string
		for _, ag := range rows.Agents {
			for _, q := range ag.Declares {
				if _, derr := store.McpDeclareTx(ctx, tx, slots[ag.AgentKey], q); derr != nil {
					return derr
				}
			}
			var coreJSON *string
			if ag.Core != nil {
				b, cerr := json.Marshal(*ag.Core)
				if cerr != nil {
					return fmt.Errorf("mcp: encode core for %s: %w", ag.AgentKey, cerr)
				}
				str := string(b)
				coreJSON = &str
			}
			if cerr := store.McpCoreSetTx(ctx, tx, slots[ag.AgentKey], coreJSON); cerr != nil {
				return cerr
			}
			if len(ag.Declares) > 0 {
				ds := append([]string{}, ag.Declares...)
				sort.Strings(ds)
				declParts = append(declParts, fmt.Sprintf("%s [%s]", ag.AgentKey, strings.Join(ds, " ")))
			}
		}
		if len(declParts) > 0 {
			sort.Strings(declParts)
			if cerr := store.McpChangeInsertTx(ctx, tx, store.McpChange{
				Op: "import", Summary: "import declarations: " + strings.Join(declParts, "; "),
				ActorKind: in.ActorKind, ActorID: in.ActorID, OccurredAt: now,
			}); cerr != nil {
				return cerr
			}
		}
		if !ledgerHasHash {
			if _, ierr := store.McpImportInsertTx(ctx, tx, store.McpImport{
				Path: in.Path, Sha256: sha, Result: "imported",
				Detail:     fmt.Sprintf("servers=%d tools=%d declares=%d", len(rows.Servers), toolsN, declaresN),
				Servers:    len(rows.Servers),
				ImportedAt: now,
			}); ierr != nil {
				return ierr
			}
		}
		return nil
	})
	if err != nil {
		return protocol.McpImportFileResult{}, err
	}

	res.Outcome = importImported
	if in.Mode == "replace" && !in.Startup {
		res.Outcome = importReplaced
	}
	res.Servers, res.Tools, res.Declares = len(rows.Servers), toolsN, declaresN
	res.Affected = affected
	return res, nil
}

// normalizeAgents restores the two-empties distinction the rows cannot
// carry (the same normalization the transitional proposal mirror applies,
// T3's recorded deviation): a config decoded from JSON may hold a nil
// Agents map; writing it out (dry-run diff shapes, future mirrors) must
// not turn `{"agents":{}}` into null.
func normalizeAgents(cfg mcp.Config) mcp.Config {
	if cfg.Agents == nil {
		cfg.Agents = map[string]mcp.AgentConfig{}
	}
	return cfg
}

// writeRefusedLedger appends the ONE 'refused' row. Detail carries the
// sentence (with the path and field inside it, the validator's own words);
// it is CLIPPED to the ledger's 1024-char CHECK so a monster sentence
// degrades to a clipped ledger row rather than a lost record — the full
// sentence still travels to the log and doctor through the returned
// Detail.
func (a *mcpAdmin) writeRefusedLedger(ctx context.Context, path, sha, sentence string) error {
	detail := sentence
	if len(detail) > 1024 {
		detail = detail[:1020] + " …"
	}
	now := a.now().UTC().Format(time.RFC3339)
	return a.st.McpWithTx(ctx, func(tx *sql.Tx) error {
		if _, ierr := store.McpImportInsertTx(ctx, tx, store.McpImport{
			Path: path, Sha256: sha, Result: "refused", Detail: detail, ImportedAt: now,
		}); ierr != nil {
			return ierr
		}
		return nil
	})
}

// diffAgainstRows answers the dry-run: what the file would ADD and REMOVE
// against the rows as they stand — names only (server, server.tool,
// "key declares qualified"), env NAMES only (MG13). nil answers only on a
// store read fault.
func (a *mcpAdmin) diffAgainstRows(ctx context.Context, cfg mcp.Config) *protocol.McpImportDiff {
	all, err := a.st.McpRows(ctx)
	if err != nil {
		return nil
	}
	curServers := map[string]bool{}
	curTools := map[string]bool{}
	curDeclares := map[string]bool{}
	for _, s := range all.Servers {
		curServers[s.Name] = true
		for _, t := range s.Tools {
			curTools[s.Name+"."+t.Name] = true
		}
	}
	for _, d := range all.Declares {
		key, ok := a.mapKey(d.AgentSlug)
		if !ok {
			key = d.AgentSlug
		}
		curDeclares[key+" declares "+d.Qualified] = true
	}
	wantServers, wantTools, wantDeclares := map[string]bool{}, map[string]bool{}, map[string]bool{}
	// The PLAN'S env names — from the file being adopted, never the rows
	// as they stand (a dry run answers what the import WOULD bring; the
	// names are the file's fact, MG13 keeps values out of the shape).
	envNames := map[string]bool{}
	addServer := func(s mcp.ServerConfig) {
		wantServers[s.Name] = true
		for _, t := range s.Tools {
			wantTools[s.Name+"."+t.Name] = true
		}
		for _, e := range s.Transport.Env {
			envNames[e] = true
		}
	}
	for _, s := range cfg.Public {
		addServer(s)
	}
	for key, ac := range cfg.Agents {
		for _, s := range ac.Private {
			addServer(s)
		}
		for _, q := range ac.Declares {
			wantDeclares[key+" declares "+q] = true
		}
	}
	set := func(want, have map[string]bool) []string {
		var out []string
		for k := range want {
			if !have[k] {
				out = append(out, k)
			}
		}
		sort.Strings(out)
		return out
	}
	rem := func(have, want map[string]bool) []string { return set(have, want) }
	names := make([]string, 0, len(envNames))
	for e := range envNames {
		names = append(names, e)
	}
	sort.Strings(names)
	return &protocol.McpImportDiff{
		ServersAdded:    set(wantServers, curServers),
		ServersRemoved:  rem(curServers, wantServers),
		ToolsAdded:      set(wantTools, curTools),
		ToolsRemoved:    rem(curTools, wantTools),
		DeclaresAdded:   set(wantDeclares, curDeclares),
		DeclaresRemoved: rem(curDeclares, wantDeclares),
		EnvNames:        names,
	}
}

// beforeAfterGrants answers the replace's visibility duty: for every agent
// the replace TOUCHES (declares change, or their servers' tools do), the
// qualified grants before and after. Read side: before-grants come from
// the rows as they stand (BEFORE the write transaction); after-grants come
// from the file being adopted.
func (a *mcpAdmin) beforeAfterGrants(ctx context.Context, cfg mcp.Config) ([]protocol.McpAffectedGrants, error) {
	all, err := a.st.McpRows(ctx)
	if err != nil {
		return nil, err
	}
	// before: agent key → its declared qualified tools (as stored).
	before := map[string][]string{}
	for _, d := range all.Declares {
		key, ok := a.mapKey(d.AgentSlug)
		if !ok {
			key = d.AgentSlug
		}
		before[key] = append(before[key], d.Qualified)
	}
	// after: agent key → declares from the file (sorted); agents present in
	// the file always have an (possibly empty) answer.
	after := map[string][]string{}
	for key, ac := range cfg.Agents {
		ds := append([]string{}, ac.Declares...)
		sort.Strings(ds)
		after[key] = ds
	}
	// The touched set: any key whose answer differs, before or after.
	touched := map[string]bool{}
	for k, v := range before {
		b := append([]string{}, v...)
		sort.Strings(b)
		if !equalStrings(b, after[k]) {
			touched[k] = true
		}
	}
	for k, v := range after {
		b := append([]string{}, before[k]...)
		sort.Strings(b)
		if !equalStrings(b, v) {
			touched[k] = true
		}
	}
	var out []protocol.McpAffectedGrants
	for k := range touched {
		b := append([]string{}, before[k]...)
		sort.Strings(b)
		a2 := append([]string{}, after[k]...)
		sort.Strings(a2)
		out = append(out, protocol.McpAffectedGrants{Agent: k, Before: b, After: a2})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Agent < out[j].Agent })
	return out, nil
}

func equalStrings(x, y []string) bool {
	if len(x) != len(y) {
		return false
	}
	for i := range x {
		if x[i] != y[i] {
			return false
		}
	}
	return true
}

// --- the seam handler (mcp.importFile) ---------------------------------------

// mcpImportFileRead is the human door's seam half. The protocol layer has
// already refused identity keys in params and (on the socket) stamped the
// peer credential into the context; what is left is the DOOR's own facts:
// the mode is one of two named acts (no default — guessing which one a
// human meant is how a plan becomes a mutation), the path is absolute and
// a regular file (the filesystem is the entry point's, so this is where
// that shape is checked), and the actor exists (a direct-door caller — the
// CLI, a test — stamps the kernel's uid the same honest way dispatch does).
// Every domain answer is a RESULT (outcomes), not a refusal: "skipped" is
// not an error, and neither is a validator's "refused" — those are facts
// the human asked for. Only shape faults and infrastructure are errors.
func mcpImportFileRead(ctx context.Context, a *mcpAdmin, params json.RawMessage) (any, string, error) {
	var p protocol.McpImportFileParams
	if err := json.Unmarshal(normalizeParams(params), &p); err != nil {
		return nil, "", protocol.ErrBadParams{Err: fmt.Errorf("mcp.importFile params are {path?, mode}: %w", err)}
	}
	if p.Mode != "dry-run" && p.Mode != "replace" {
		return nil, "", protocol.ErrBadParams{Err: fmt.Errorf("mcp.importFile mode must be \"dry-run\" or \"replace\" — got %q; a plan and a mutation are different acts and there is no default", p.Mode)}
	}
	path := mcpLegacyPath()
	if p.Path != nil {
		path = *p.Path
		if !filepath.IsAbs(path) {
			return nil, "", protocol.ErrBadParams{Err: fmt.Errorf("mcp.importFile path must be absolute — %q is relative; the door reads one named file, never a world-relative guess", path)}
		}
	}
	fi, serr := os.Stat(path)
	if serr != nil || !fi.Mode().IsRegular() {
		return nil, "", protocol.ErrBadParams{Err: fmt.Errorf("mcp.importFile path %q is not a regular readable file — the door imports one file, never a directory, a device, or a missing name", path)}
	}
	kind, id, ok := protocol.MCPActor(ctx)
	if !ok {
		return nil, "", protocol.ErrMcpNoActor.WithWhat("the MCP import needs a person and this call carried no actor stamp — socket callers get the peer credential from dispatch (Table.ActorFor), direct-door callers stamp the kernel's uid; params never carry an actor")
	}
	res, err := a.importFromFile(ctx, importRequest{
		Path: path, Mode: p.Mode, ActorKind: kind, ActorID: id,
	})
	if err != nil {
		return nil, "", err
	}
	return res, "", nil
}
