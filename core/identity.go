package core

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
	"time"
)

const (
	idPrefix   = "ace:"
	namePrefix = "acen:"
)

var nameRegexp = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,20}$`)

// Identity represents a registered client identity.
type Identity struct {
	Key        string `json:"key"`
	ID         string `json:"id"`
	Name       string `json:"name"`
	LastActive string `json:"last_active"`
}

// Register creates a new client identity. If name is empty, the id is
// used as the name.
func (s *Space) Register(name string) (*Identity, error) {
	defer s.logSlowOp("register")()

	if name != "" && !nameRegexp.MatchString(name) {
		return nil, validationErr(fmt.Errorf(
			"name must be 1-20 characters of a-zA-Z0-9_-"))
	}

	keyBytes := make([]byte, 32)
	if _, err := rand.Read(keyBytes); err != nil {
		return nil, fmt.Errorf("generate key: %w", err)
	}
	idBytes := make([]byte, 32)
	if _, err := rand.Read(idBytes); err != nil {
		return nil, fmt.Errorf("generate id: %w", err)
	}

	key := hex.EncodeToString(keyBytes)
	id := idPrefix + hex.EncodeToString(idBytes)

	storedName := id
	if name != "" {
		storedName = namePrefix + name
	}

	now := time.Now().UTC().Format(timestampFormat)

	_, err := s.db.Exec(
		"INSERT INTO identities (key, id, name, last_active) VALUES (?, ?, ?, ?)",
		key, id, storedName, now,
	)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed: identities.name") {
			return nil, validationErr(fmt.Errorf("name %q already exists", name))
		}
		return nil, fmt.Errorf("insert identity: %w", err)
	}

	return &Identity{Key: key, ID: id, Name: storedName, LastActive: now}, nil
}

// LookupKey retrieves the identity for a key and updates last_active.
func (s *Space) LookupKey(key string) (*Identity, error) {
	defer s.logSlowOp("lookup key")()
	now := time.Now().UTC().Format(timestampFormat)

	tx, err := s.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback()

	res, err := tx.Exec("UPDATE identities SET last_active = ? WHERE key = ?", now, key)
	if err != nil {
		return nil, fmt.Errorf("update last_active: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("rows affected: %w", err)
	}
	if n == 0 {
		return nil, nil
	}

	var ident Identity
	err = tx.QueryRow(
		"SELECT key, id, name, last_active FROM identities WHERE key = ?", key,
	).Scan(&ident.Key, &ident.ID, &ident.Name, &ident.LastActive)
	if err != nil {
		return nil, fmt.Errorf("select identity: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}
	return &ident, nil
}

// LookupID retrieves an identity by its id.
func (s *Space) LookupID(id string) (*Identity, error) {
	defer s.logSlowOp("lookup id")()
	var ident Identity
	err := s.db.QueryRow(
		"SELECT key, id, name, last_active FROM identities WHERE id = ?", id,
	).Scan(&ident.Key, &ident.ID, &ident.Name, &ident.LastActive)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("lookup id: %w", err)
	}
	return &ident, nil
}

// LookupName retrieves an identity by its name.
func (s *Space) LookupName(name string) (*Identity, error) {
	defer s.logSlowOp("lookup name")()
	var ident Identity
	err := s.db.QueryRow(
		"SELECT key, id, name, last_active FROM identities WHERE name = ?", name,
	).Scan(&ident.Key, &ident.ID, &ident.Name, &ident.LastActive)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("lookup name: %w", err)
	}
	return &ident, nil
}

// ResolveAccess resolves acen: name references in access lists to ace:
// id values. Entries with other prefixes pass through unchanged.
func (s *Space) ResolveAccess(access *Access) (*Access, error) {
	if access == nil {
		return nil, nil
	}
	resolved := &Access{}
	if access.In != nil {
		in, err := s.resolveIDs(access.In)
		if err != nil {
			return nil, fmt.Errorf("resolve access.in: %w", err)
		}
		resolved.In = in
	}
	if access.Rd != nil {
		rd, err := s.resolveIDs(access.Rd)
		if err != nil {
			return nil, fmt.Errorf("resolve access.rd: %w", err)
		}
		resolved.Rd = rd
	}
	return resolved, nil
}

func (s *Space) resolveIDs(ids []string) ([]string, error) {
	result := make([]string, 0, len(ids))
	for _, entry := range ids {
		if strings.HasPrefix(entry, namePrefix) {
			ident, err := s.LookupName(entry)
			if err != nil {
				return nil, err
			}
			if ident == nil {
				return nil, validationErr(fmt.Errorf("unknown identity %q", entry))
			}
			result = append(result, ident.ID)
		} else {
			result = append(result, entry)
		}
	}
	return result, nil
}

// ValidateAccessPrefixes checks that all access list entries have an
// ace: or acen: prefix. Call only when InsecureIDs is false.
func ValidateAccessPrefixes(access *Access) error {
	if access == nil {
		return nil
	}
	check := func(ids []string, field string) error {
		for _, id := range ids {
			if !strings.HasPrefix(id, idPrefix) && !strings.HasPrefix(id, namePrefix) {
				return fmt.Errorf(
					"access %s identity %q requires ace: or acen: prefix", field, id)
			}
		}
		return nil
	}
	if err := check(access.In, "in"); err != nil {
		return validationErr(err)
	}
	return validationErr(check(access.Rd, "rd"))
}

// DeleteExpiredIdentities removes identities whose last_active is
// older than IdentityTTL.
func (s *Space) DeleteExpiredIdentities() (int64, error) {
	defer s.logSlowOp("delete expired identities")()
	if s.cfg.IdentityTTL <= 0 {
		return 0, nil
	}
	cutoff := time.Now().UTC().Add(-s.cfg.IdentityTTL).Format(timestampFormat)
	res, err := s.db.Exec("DELETE FROM identities WHERE last_active <= ?", cutoff)
	if err != nil {
		return 0, fmt.Errorf("delete expired identities: %w", err)
	}
	return res.RowsAffected()
}
