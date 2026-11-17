// Every bench page added from here on registers itself (registerBenchPage,
// in its own file) instead of a slice editing routes.ts's BENCH_PAGES
// array, TestBench.tsx's import list, AND TestBench.tsx's dispatch switch —
// three shared-file edits collapsed to one. This file's only job is making
// sure each page module actually gets imported (a registration nobody
// imports never runs): add ONE line per new page, nothing else, so two
// worktrees adding a page append two different lines instead of touching
// an array, an import list and a switch each. See
// docs/aeon/README.md's "Adding a bench page" and AGENTS.md §3.
//
// The existing (pre-registry) bench pages are NOT listed here — they keep
// their hand-written entries in routes.ts's BENCH_PAGES literal and their
// explicit branches in TestBench.tsx, unchanged. This file is additive
// only.

import "./ParallelAgentsPage";
import "./GraphPage";
import "./RetrievalPage";
import "./LearningsPage";
import "./TelemetryPage";
import "./AsyncWorkersPage";
import "./SkillsPage";
import "./AgentMailboxPage";
import "./MergeQueuePage";
import "./RouteTracePage";
