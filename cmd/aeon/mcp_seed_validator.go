// mcp_seed_validator.go — S46 T10 (MG10). The RowValidator seam the seed
// engine carries to every MCP record: the candidate rows are assembled
// into a Config and passed through THE validator — `internal/mcp`'s
// Config.Validate — verbatim, exactly the gate the write doors pass (T5's
// candidateValidated). mcpseed itself never imports the grammar (MC11);
// cmd/aeon is the named importer that supplies the seam at the two engine
// build sites (serve.go's daemon engine and the CLI's direct-door engine).
package main

import (
	"context"

	"github.com/cjlapao/aeon/internal/mcp"
	"github.com/cjlapao/aeon/internal/mcpseed"
	"github.com/cjlapao/aeon/internal/store"
)

// mcpSeedValidator builds the seam over a store handle. The candidate is
// the FULL post-write world the applier computed; assembly uses the
// include-disabled view (the registry door's view — a disabled system
// server is still a shape that must validate), and the refusal sentence
// is the validator's own, returned untouched (no reword at any door).
func mcpSeedValidator(st *store.Store) mcpseed.RowValidator {
	admin := newMCPAdmin(st)
	return func(ctx context.Context, candidate store.McpAll) error {
		rows, err := admin.toRows(candidate)
		if err != nil {
			return err
		}
		cfg, err := mcp.Assemble(rows, mcp.AssembleOptions{IncludeDisabled: true})
		if err != nil {
			return err
		}
		return cfg.Validate()
	}
}
