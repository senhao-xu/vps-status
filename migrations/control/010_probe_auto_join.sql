ALTER TABLE probe_tasks ADD COLUMN auto_join INTEGER NOT NULL DEFAULT 0 CHECK(auto_join IN (0, 1));
