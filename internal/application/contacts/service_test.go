package contacts

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/leonfox28/simplus/internal/domain/contact"
)

type memoryRepository struct{ values map[string]contact.Contact }

func (repo *memoryRepository) CreateContact(_ context.Context, value contact.Contact) (contact.Contact, error) {
	for _, existing := range repo.values {
		if existing.PhoneNumber == value.PhoneNumber {
			return contact.Contact{}, contact.ErrPhoneConflict
		}
	}
	repo.values[value.ID] = value
	return value, nil
}
func (repo *memoryRepository) UpdateContact(_ context.Context, value contact.Contact) (contact.Contact, error) {
	existing, ok := repo.values[value.ID]
	if !ok {
		return contact.Contact{}, contact.ErrNotFound
	}
	value.CreatedAt = existing.CreatedAt
	repo.values[value.ID] = value
	return value, nil
}
func (repo *memoryRepository) DeleteContact(_ context.Context, id string) error {
	if _, ok := repo.values[id]; !ok {
		return contact.ErrNotFound
	}
	delete(repo.values, id)
	return nil
}
func (repo *memoryRepository) ListContacts(context.Context) ([]contact.Contact, error) {
	values := make([]contact.Contact, 0, len(repo.values))
	for _, value := range repo.values {
		values = append(values, value)
	}
	return values, nil
}

func TestServiceNormalizesAndValidatesContacts(t *testing.T) {
	repository := &memoryRepository{values: make(map[string]contact.Contact)}
	service, err := New(repository)
	if err != nil {
		t.Fatal(err)
	}
	service.random = strings.NewReader("0123456789abcdef")
	service.now = func() time.Time { return time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC) }
	created, err := service.Create(context.Background(), "  张三  ", " +8613800138000 ")
	if err != nil {
		t.Fatal(err)
	}
	if created.DisplayName != "张三" || created.PhoneNumber != "+8613800138000" || !contactIDPattern.MatchString(created.ID) {
		t.Fatalf("created = %#v", created)
	}
	if _, err := service.Create(context.Background(), "", "10086"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid create error = %v", err)
	}
	updated, err := service.Update(context.Background(), created.ID, "李四", "13900139000")
	if err != nil || updated.DisplayName != "李四" {
		t.Fatalf("updated = %#v error = %v", updated, err)
	}
	if err := service.Delete(context.Background(), created.ID); err != nil {
		t.Fatal(err)
	}
}
