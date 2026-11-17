// Every Configuration page added from here on registers itself
// (registerConfigPage, in its own file) — this file's only job is making
// sure each page module actually gets imported, since a registration
// nobody imports never runs. Add ONE line per new page, nothing else, so
// two worktrees adding a page append two different lines. S72 CF2; the
// same convention as features/test-bench/pages/registerAll.ts.
//
// Imported once by app/routes.ts for its side effect.

import "./daemon-settings";
import "./backups";
import "./system-records";
import "./agents";
import "./policy";
import "./mcp";
import "./tools";
import "./gates";
import "./environments";
import "./automation";
import "./triage";
import "./memory";
import "./continuous";
import "./platform-status";
import "./variables";
import "./skills";
