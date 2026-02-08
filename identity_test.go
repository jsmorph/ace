package ace

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestRegisterBasic(t *testing.T) {
	s := newTestSpace(t)

	ident, err := s.Register("")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(ident.ID, "ace:") {
		t.Fatalf("id should have ace: prefix, got %q", ident.ID)
	}
	if ident.Name != ident.ID {
		t.Fatalf("unnamed identity should use id as name: got name=%q id=%q", ident.Name, ident.ID)
	}
	if len(ident.Key) != 64 {
		t.Fatalf("key should be 64 hex chars, got %d", len(ident.Key))
	}
	if _, err := hex.DecodeString(ident.Key); err != nil {
		t.Fatalf("key is not valid hex: %v", err)
	}
	hexID := strings.TrimPrefix(ident.ID, "ace:")
	if len(hexID) != 64 {
		t.Fatalf("id hex part should be 64 chars, got %d", len(hexID))
	}
}

func TestRegisterWithName(t *testing.T) {
	s := newTestSpace(t)

	ident, err := s.Register("alice")
	if err != nil {
		t.Fatal(err)
	}
	if ident.Name != "acen:alice" {
		t.Fatalf("expected name acen:alice, got %q", ident.Name)
	}
	if !strings.HasPrefix(ident.ID, "ace:") {
		t.Fatalf("id should have ace: prefix, got %q", ident.ID)
	}
}

func TestRegisterDuplicateName(t *testing.T) {
	s := newTestSpace(t)

	if _, err := s.Register("bob"); err != nil {
		t.Fatal(err)
	}
	_, err := s.Register("bob")
	if err == nil {
		t.Fatal("expected error for duplicate name")
	}
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("expected ValidationError, got %T: %v", err, err)
	}
}

func TestRegisterInvalidName(t *testing.T) {
	s := newTestSpace(t)

	cases := []string{
		"has space",
		"has.dot",
		"a-very-long-name-that-exceeds-twenty-characters",
		"unicode-é",
	}
	for _, name := range cases {
		_, err := s.Register(name)
		if err == nil {
			t.Fatalf("expected error for name %q", name)
		}
	}
}

func TestLookupKey(t *testing.T) {
	s := newTestSpace(t)

	ident, err := s.Register("worker")
	if err != nil {
		t.Fatal(err)
	}

	found, err := s.LookupKey(ident.Key)
	if err != nil {
		t.Fatal(err)
	}
	if found == nil {
		t.Fatal("expected identity, got nil")
	}
	if found.ID != ident.ID {
		t.Fatalf("expected id %q, got %q", ident.ID, found.ID)
	}
	if found.Name != ident.Name {
		t.Fatalf("expected name %q, got %q", ident.Name, found.Name)
	}
}

func TestLookupKeyNotFound(t *testing.T) {
	s := newTestSpace(t)

	found, err := s.LookupKey("nonexistent")
	if err != nil {
		t.Fatal(err)
	}
	if found != nil {
		t.Fatalf("expected nil, got %+v", found)
	}
}

func TestLookupID(t *testing.T) {
	s := newTestSpace(t)

	ident, err := s.Register("worker")
	if err != nil {
		t.Fatal(err)
	}

	found, err := s.LookupID(ident.ID)
	if err != nil {
		t.Fatal(err)
	}
	if found == nil {
		t.Fatal("expected identity, got nil")
	}
	if found.Key != ident.Key {
		t.Fatalf("expected key %q, got %q", ident.Key, found.Key)
	}
}

func TestLookupName(t *testing.T) {
	s := newTestSpace(t)

	ident, err := s.Register("worker")
	if err != nil {
		t.Fatal(err)
	}

	found, err := s.LookupName("acen:worker")
	if err != nil {
		t.Fatal(err)
	}
	if found == nil {
		t.Fatal("expected identity, got nil")
	}
	if found.ID != ident.ID {
		t.Fatalf("expected id %q, got %q", ident.ID, found.ID)
	}
}

func TestResolveAccessNames(t *testing.T) {
	s := newTestSpace(t)

	ident, err := s.Register("alice")
	if err != nil {
		t.Fatal(err)
	}

	acc := &Access{
		In: []string{"acen:alice"},
		Rd: []string{"acen:alice", "ace:other"},
	}
	resolved, err := s.ResolveAccess(acc)
	if err != nil {
		t.Fatal(err)
	}
	if len(resolved.In) != 1 || resolved.In[0] != ident.ID {
		t.Fatalf("expected In resolved to %q, got %v", ident.ID, resolved.In)
	}
	if len(resolved.Rd) != 2 || resolved.Rd[0] != ident.ID || resolved.Rd[1] != "ace:other" {
		t.Fatalf("expected Rd [%q, ace:other], got %v", ident.ID, resolved.Rd)
	}
}

func TestResolveAccessUnknownName(t *testing.T) {
	s := newTestSpace(t)

	acc := &Access{In: []string{"acen:nobody"}}
	_, err := s.ResolveAccess(acc)
	if err == nil {
		t.Fatal("expected error for unknown name")
	}
}

func TestResolveAccessNil(t *testing.T) {
	s := newTestSpace(t)

	resolved, err := s.ResolveAccess(nil)
	if err != nil {
		t.Fatal(err)
	}
	if resolved != nil {
		t.Fatalf("expected nil, got %+v", resolved)
	}
}

func TestValidateAccessPrefixes(t *testing.T) {
	err := ValidateAccessPrefixes(&Access{In: []string{"ace:abc"}, Rd: []string{"acen:bob"}})
	if err != nil {
		t.Fatalf("should accept prefixed entries: %v", err)
	}

	err = ValidateAccessPrefixes(&Access{In: []string{"bare-string"}})
	if err == nil {
		t.Fatal("should reject unprefixed entry")
	}

	err = ValidateAccessPrefixes(nil)
	if err != nil {
		t.Fatalf("should accept nil: %v", err)
	}
}

func TestDeleteExpiredIdentities(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Blocking = BlockingPoll
	cfg.InsecureIDs = true
	cfg.IdentityTTL = 100 * time.Millisecond
	s := newTestSpaceWithConfig(t, cfg)

	if _, err := s.Register("temp"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(150 * time.Millisecond)

	n, err := s.DeleteExpiredIdentities()
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("expected 1 deletion, got %d", n)
	}

	found, err := s.LookupName("acen:temp")
	if err != nil {
		t.Fatal(err)
	}
	if found != nil {
		t.Fatal("identity should have been deleted")
	}
}

func TestDeleteExpiredIdentitiesNoneExpired(t *testing.T) {
	s := newTestSpace(t)

	if _, err := s.Register("fresh"); err != nil {
		t.Fatal(err)
	}

	n, err := s.DeleteExpiredIdentities()
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("expected 0 deletions, got %d", n)
	}
}

func TestOutWithRegisteredAccess(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Blocking = BlockingPoll
	s := newTestSpaceWithConfig(t, cfg)

	ident, err := s.Register("writer")
	if err != nil {
		t.Fatal(err)
	}

	acc := &Access{In: []string{ident.ID}}
	_, err = s.Out(json.RawMessage(`{"x":1}`), acc, 0)
	if err != nil {
		t.Fatal(err)
	}
}

func TestOutRejectsUnprefixedWhenNotInsecure(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Blocking = BlockingPoll
	s := newTestSpaceWithConfig(t, cfg)

	acc := &Access{In: []string{"bare-string"}}
	_, err := s.Out(json.RawMessage(`{"x":1}`), acc, 0)
	if err == nil {
		t.Fatal("expected error for unprefixed access identity")
	}
}

func TestOutAllowsUnprefixedWhenInsecure(t *testing.T) {
	s := newTestSpace(t)

	acc := &Access{In: []string{"bare-string"}}
	_, err := s.Out(json.RawMessage(`{"x":1}`), acc, 0)
	if err != nil {
		t.Fatalf("should allow unprefixed when insecure: %v", err)
	}
}
