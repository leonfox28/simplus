package sqlite

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/leonfox28/simplus/internal/domain/contact"
)

func TestContactsPersistUpdateConflictAndDelete(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "db")
	set, err := OpenSet(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	first, err := set.CreateContact(ctx, contact.Contact{
		ID: "contact_0123456789abcdef", DisplayName: "张三", PhoneNumber: "+8613800138000", CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := set.CreateContact(ctx, contact.Contact{
		ID: "contact_fedcba9876543210", DisplayName: "重复号码", PhoneNumber: first.PhoneNumber, CreatedAt: now, UpdatedAt: now,
	}); !errors.Is(err, contact.ErrPhoneConflict) {
		t.Fatalf("duplicate phone error = %v", err)
	}
	first.DisplayName = "张三（工作）"
	first.UpdatedAt = now.Add(time.Minute)
	updated, err := set.UpdateContact(ctx, first)
	if err != nil || updated.DisplayName != first.DisplayName {
		t.Fatalf("updated = %#v error = %v", updated, err)
	}
	if err := set.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenSet(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	listed, err := reopened.ListContacts(ctx)
	if err != nil || len(listed) != 1 || listed[0].DisplayName != first.DisplayName {
		t.Fatalf("reopened contacts = %#v error = %v", listed, err)
	}
	if err := reopened.DeleteContact(ctx, first.ID); err != nil {
		t.Fatal(err)
	}
	if err := reopened.DeleteContact(ctx, first.ID); !errors.Is(err, contact.ErrNotFound) {
		t.Fatalf("second delete error = %v", err)
	}
}
