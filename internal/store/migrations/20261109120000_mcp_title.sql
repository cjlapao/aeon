-- An MCP server has a friendly name and a description beside its slug. `name` stays the slug every reference uses (the tool address `mcp.<name>.<tool>`, grants, policies); `title` is what the app shows and `description` says what the server is for. Both are display text only: nothing executes on them and an empty value falls back to the slug.
ALTER TABLE mcp_server ADD COLUMN title       TEXT NOT NULL DEFAULT '';
ALTER TABLE mcp_server ADD COLUMN description TEXT NOT NULL DEFAULT '';
