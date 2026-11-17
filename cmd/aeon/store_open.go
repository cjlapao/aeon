package main

import (
	"io"
	"os"
	"time"

	"github.com/cjlapao/aeon/internal/backup"
	"github.com/cjlapao/aeon/internal/store"
)

// storeOpenOptions is the ONE constructor for every store.Open call in this
// package (S62 T5, BK6). The pre-migration hook is wired where the options
// are built, so a door that forgets the hook is a call the guard test
// (bk_hook_guard_test.go) refuses at the parser level, not a gap found in
// production.
//
// noBackup is the operator's --no-backup, scoped to the pre-migration
// backup alone: it never reaches the schedule (T6) and it is the only
// door off the safety net — no environment variable takes it (BK18).
// actorKind stamps the run row with the door that holds the store:
// "daemon" for serve, "cli" for a verb.
func storeOpenOptions(noBackup bool, actorKind string) store.Options {
	return store.Options{
		PreMigrate: backup.NewPreMigrate(backup.Deps{
			// Store is nil on purpose: the live store does not exist yet.
			// The hook receives it from Open and binds it at its moment.
			CheckFile: store.CheckFile,
			OpenStore: func(dir string) (io.Closer, error) {
				return store.Open(drillOpenOptions(dir))
			},
			Clock:     time.Now,
			Settings:  backup.NewEnvSettings(),
			Version:   Version,
			OutsideDB: backup.DefaultOutsideDB,
		}, backup.PreMigrateOptions{
			NoBackup:  noBackup,
			ActorKind: actorKind,
			Out:       os.Stderr,
		}),
	}
}

// drillOpenOptions is the ONE other constructor: the verify drill's door
// (level 3), which opens a backed-up store as it would be restored. It
// migrates (that is part of the drill) but carries NO hook, deliberately:
// the store it opens is a copy that already had its safety net at its own
// open, and a drill that re-ran the net would take a pre-migration backup
// of the backup into the same pre-migration dir it is inspecting. The
// guard test (bk_hook_guard_test.go) accepts exactly this named
// constructor beside storeOpenOptions and SkipMigrations — a raw
// store.Options literal at a drill is a new door the guard must see.
func drillOpenOptions(dir string) store.Options {
	return store.Options{Dir: dir}
}
