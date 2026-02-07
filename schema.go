package ace

import "database/sql"

const schemaSQL = `
CREATE TABLE IF NOT EXISTS objects (
    id      TEXT PRIMARY KEY,
    json    TEXT NOT NULL,
    expires TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS access (
    id   TEXT NOT NULL REFERENCES objects(id) ON DELETE CASCADE,
    type TEXT NOT NULL CHECK(type IN ('in', 'rd')),
    iid  TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_access ON access(id, type);

CREATE TABLE IF NOT EXISTS branches (
    id TEXT NOT NULL REFERENCES objects(id) ON DELETE CASCADE,
    b  TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_branches_b ON branches(b);
`

func initSchema(db *sql.DB) error {
	_, err := db.Exec(schemaSQL)
	return err
}
