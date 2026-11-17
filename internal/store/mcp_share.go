package store

import (
	"context"
	"database/sql"
	"fmt"
)

// McpShareTx shares (or stops sharing) a public server with every agent. False
// is "already so" — the no-op the write result reports as changed:false.
func McpShareTx(ctx context.Context, tx *sql.Tx, server string, shared bool) (bool, error) {
	var res sql.Result
	var err error
	if shared {
		res, err = tx.ExecContext(ctx,
			`INSERT INTO mcp_share (server_id) SELECT id FROM mcp_server WHERE name = ? AND tier = 'public' ON CONFLICT DO NOTHING`, server)
	} else {
		res, err = tx.ExecContext(ctx,
			`DELETE FROM mcp_share WHERE server_id = (SELECT id FROM mcp_server WHERE name = ?)`, server)
	}
	if err != nil {
		return false, fmt.Errorf("store: mcp share %q: %w", server, err)
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// mcpSharedTx lists the names of the servers shared with every agent.
func mcpSharedTx(ctx context.Context, tx *sql.Tx) ([]string, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT s.name FROM mcp_share h JOIN mcp_server s ON s.id = h.server_id ORDER BY s.name`)
	if err != nil {
		return nil, fmt.Errorf("store: mcp rows: shared: %w", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("store: mcp rows: scan shared: %w", err)
		}
		out = append(out, name)
	}
	return out, rows.Err()
}

// McpIsShared reports whether a server is shared with every agent.
func (s *Store) McpIsShared(ctx context.Context, server string) bool {
	var one int
	err := s.r.QueryRowContext(ctx, `
		SELECT 1 FROM mcp_share h JOIN mcp_server s ON s.id = h.server_id WHERE s.name = ?`, server).Scan(&one)
	return err == nil
}
