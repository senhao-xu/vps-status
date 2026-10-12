CREATE TABLE public_page (
    singleton_id INTEGER PRIMARY KEY CHECK(singleton_id = 1),
    enabled INTEGER NOT NULL CHECK(enabled IN (0, 1)),
    title TEXT NOT NULL CHECK(length(title) BETWEEN 1 AND 128),
    description TEXT NOT NULL CHECK(length(description) <= 512),
    updated_at INTEGER NOT NULL
);

INSERT INTO public_page(singleton_id, enabled, title, description, updated_at)
VALUES (1, 0, 'VPS Status', '', 0);

CREATE TABLE public_nodes (
    node_id TEXT PRIMARY KEY REFERENCES nodes(id) ON DELETE CASCADE,
    published_at INTEGER NOT NULL
) WITHOUT ROWID;

