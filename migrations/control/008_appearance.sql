CREATE TABLE appearance (
    singleton_id INTEGER PRIMARY KEY CHECK (singleton_id = 1),
    default_theme TEXT NOT NULL CHECK (default_theme IN ('emerald', 'classic'))
);

INSERT INTO appearance(singleton_id, default_theme) VALUES (1, 'emerald');
