CREATE TABLE installation (
    singleton_id INTEGER PRIMARY KEY CHECK (singleton_id = 1),
    control_url TEXT NOT NULL DEFAULT '' CHECK (length(control_url) <= 2048),
    agent_release_version TEXT NOT NULL DEFAULT '' CHECK (length(agent_release_version) <= 64)
);
INSERT INTO installation(singleton_id) VALUES (1);
