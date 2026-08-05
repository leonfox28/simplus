package sqlite

import (
	"context"
	"crypto/sha256"
	"path/filepath"
	"testing"
)

func TestAdministratorSessionPersistsAcrossOrdinaryRestart(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "db")
	set, err := OpenSet(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	tokenHash := sha256.Sum256([]byte("session-token"))
	csrfHash := sha256.Sum256([]byte("csrf-token"))
	if err := set.CreateAdministratorSession(ctx, tokenHash, csrfHash, "admin", 1, 100, 200); err != nil {
		t.Fatal(err)
	}
	if err := set.Close(); err != nil {
		t.Fatal(err)
	}
	set, err = OpenSet(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	defer set.Close()
	username, storedCSRF, generation, expires, found, err := set.ReadAdministratorSession(ctx, tokenHash, 150)
	if err != nil {
		t.Fatal(err)
	}
	if !found || username != "admin" || storedCSRF != csrfHash || generation != 1 || expires != 200 {
		t.Fatalf("session = %q %x %d %d %t", username, storedCSRF, generation, expires, found)
	}
	if err := set.DeleteAdministratorSession(ctx, tokenHash); err != nil {
		t.Fatal(err)
	}
	if _, _, _, _, found, err := set.ReadAdministratorSession(ctx, tokenHash, 150); err != nil || found {
		t.Fatalf("deleted session found = %t, err = %v", found, err)
	}
}
