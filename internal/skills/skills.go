// Package skills is S65 — Agent Skills, the SKILL.md format.
//
// # Which "skill" this is (the project has three)
//
// S14's A2A skills are an agent's published card entries — a typed judgement
// one agent asks another (internal/agentsvc/skill.go). S24's sizing "skill"
// is a project mode naming a dispatch (internal/daemon/queue_sizing.go).
// THIS package is the third meaning: the open Agent Skills format
// (https://agentskills.io) — a folder with a SKILL.md, the shape Claude
// Code, Codex and the wider ecosystem already read from ~/.agents/skills.
// A skill here is files on disk, not a capability an agent grants; the
// names never cross (a grep guard pins that no import joins the three).
//
// # What this package does, and what it deliberately does not
//
// It discovers skill folders under two ordered roots, parses and validates
// each SKILL.md against the spec, and installs/removes skills in the
// app-managed root. That is the whole job. It does NOT execute anything a
// skill ships (scripts are inventoried, never run — the install tests prove
// it), it does NOT inject skill text into any prompt (S65's scope ends at
// "available in the test bench and validated showing the capabilities"),
// and it does NOT enforce Claude Code's experimental allowed-tools (it
// records the field and says so).
//
// # Disk is the truth
//
// There is no table and no migration: the files are the record, the same
// way S13's handler bodies on disk are the record for first-party agents.
// A scan is cheap, so the service caches one and re-scans on request;
// every answer names the root it came from and, for a shadowed entry, the
// winner that beat it.
package skills
