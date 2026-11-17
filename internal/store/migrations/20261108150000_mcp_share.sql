-- A public MCP server can be shared with every agent: one row here makes each of its classified tools declared for all agents, those that exist now and those created later, without a mcp_declare row per agent. The row is a declaration only; whether a tool may be used is still the policy's answer.
CREATE TABLE mcp_share (
  server_id INTEGER PRIMARY KEY REFERENCES mcp_server (id) ON DELETE CASCADE
);
