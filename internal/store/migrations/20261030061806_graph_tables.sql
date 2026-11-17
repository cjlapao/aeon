-- S30 T1 — the code graph's own tables (D1, CG1): graph_node (a symbol with a
-- location) and graph_edge (a typed relation between two symbols), each scoped
-- by component_root — the workspace directory a graph belongs to, named as
-- S38 renamed it (D9). Plain relational rows only — this slice answers
-- structural questions, not similarity ones (N1), so it holds no index of the
-- kind its sibling slices use. Every edge carries extracted/inferred (D4,
-- CG5); the six relation kinds close at the database (this slice's copy of
-- PL3's discipline), so no Go constant is load-bearing here.

CREATE TABLE graph_node (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    component_root TEXT NOT NULL CHECK (length(component_root) > 0),
    file_path    TEXT NOT NULL CHECK (length(file_path) > 0),
    start_line   INTEGER NOT NULL CHECK (start_line >= 1),
    end_line     INTEGER NOT NULL CHECK (end_line >= start_line),
    kind         TEXT NOT NULL CHECK (length(kind) > 0),
    symbol       TEXT NOT NULL CHECK (length(symbol) > 0),
    language     TEXT NOT NULL CHECK (length(language) > 0)
);

CREATE INDEX graph_node_symbol ON graph_node (component_root, symbol);
CREATE INDEX graph_node_file   ON graph_node (component_root, file_path);

CREATE TABLE graph_edge (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    component_root TEXT NOT NULL CHECK (length(component_root) > 0),
    from_node_id INTEGER NOT NULL REFERENCES graph_node (id) ON DELETE CASCADE,
    to_node_id   INTEGER NOT NULL REFERENCES graph_node (id) ON DELETE CASCADE,
    kind         TEXT NOT NULL CHECK (kind IN
                   ('calls', 'imports', 'inherits', 'mixes_in', 'uses', 'references')),
    resolution   TEXT NOT NULL CHECK (resolution IN ('extracted', 'inferred'))
);

CREATE INDEX graph_edge_from ON graph_edge (component_root, from_node_id);
CREATE INDEX graph_edge_to   ON graph_edge (component_root, to_node_id);
