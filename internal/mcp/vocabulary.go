package mcp

// vocabulary.go — S46 T3 (MG5): the validator's own grammar strings, for
// `mcp.vocabulary`. Design.md's rule is that the vocabulary read must be
// TAKEN, never restated: the response carries the SAME strings Validate()
// compiles its three regular expressions from, so the UI and the validator
// cannot drift apart, and a test can mutate the seam to prove the handler
// reads through it (TestMcpVocabularyComesFromTheValidator, cmd/aeon).
//
// This file is additive (S46's rule to internal/mcp: no existing file
// edited); it adds no import and knows no store — the strings come from the
// package's own compiled variables, the single source.

// ServerNameGrammar returns the regexp SOURCE Validate matches server names
// against (config.go's serverNameRE — lower-case, no dot; the dot is the
// namespace join, MC8).
func ServerNameGrammar() string { return serverNameRE.String() }

// ToolNameGrammar returns the regexp SOURCE Validate matches tool names
// against (toolNameRE — dots allowed INSIDE, never leading or trailing).
func ToolNameGrammar() string { return toolNameRE.String() }

// EnvNameGrammar returns the regexp SOURCE Validate matches environment
// variable NAMES against (envNameRE — names only; a value-shaped entry is
// refused outright, C17).
func EnvNameGrammar() string { return envNameRE.String() }
