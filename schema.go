package ace

import "database/sql"

const schemaSQL = `
CREATE TABLE IF NOT EXISTS objects (
    id              TEXT PRIMARY KEY,
    json            TEXT NOT NULL,
    expires         TEXT NOT NULL,
    delete_id       TEXT,
    invisible_until TEXT
);

CREATE TABLE IF NOT EXISTS access (
    id   TEXT NOT NULL REFERENCES objects(id) ON DELETE CASCADE,
    type TEXT NOT NULL CHECK(type IN ('in', 'rd')),
    iid  TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_access ON access(id, type, iid);

CREATE TABLE IF NOT EXISTS branches (
    id TEXT NOT NULL REFERENCES objects(id) ON DELETE CASCADE,
    b  TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_branches ON branches(id, b);
CREATE INDEX IF NOT EXISTS idx_objects_delete_id ON objects(delete_id) WHERE delete_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_objects_expires ON objects(expires);

CREATE TABLE IF NOT EXISTS identities (
    key         TEXT PRIMARY KEY,
    id          TEXT NOT NULL UNIQUE,
    name        TEXT NOT NULL UNIQUE,
    last_active TEXT NOT NULL
);
`

func initSchema(db *sql.DB) error {
	_, err := db.Exec(schemaSQL)
	return err
}
