package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/leonfox28/simplus/internal/domain/contact"
	"modernc.org/sqlite"
)

func (set *Set) CreateContact(ctx context.Context, value contact.Contact) (contact.Contact, error) {
	if set == nil || set.Contacts == nil {
		return contact.Contact{}, errors.New("contacts database is not open")
	}
	_, err := set.Contacts.ExecContext(ctx, `
INSERT INTO contacts (contact_id, display_name, phone_number, created_at_unix_ms, updated_at_unix_ms)
VALUES (?, ?, ?, ?, ?)
`, value.ID, value.DisplayName, value.PhoneNumber, value.CreatedAt.UTC().UnixMilli(), value.UpdatedAt.UTC().UnixMilli())
	if err != nil {
		return contact.Contact{}, classifyContactWrite("create contact", err)
	}
	stored, found, err := set.contactByID(ctx, value.ID)
	if err != nil {
		return contact.Contact{}, err
	}
	if !found {
		return contact.Contact{}, errors.New("created contact disappeared")
	}
	return stored, nil
}

func (set *Set) UpdateContact(ctx context.Context, value contact.Contact) (contact.Contact, error) {
	if set == nil || set.Contacts == nil {
		return contact.Contact{}, errors.New("contacts database is not open")
	}
	result, err := set.Contacts.ExecContext(ctx, `
UPDATE contacts SET display_name = ?, phone_number = ?, updated_at_unix_ms = MAX(created_at_unix_ms, ?)
WHERE contact_id = ?
`, value.DisplayName, value.PhoneNumber, value.UpdatedAt.UTC().UnixMilli(), value.ID)
	if err != nil {
		return contact.Contact{}, classifyContactWrite("update contact", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return contact.Contact{}, fmt.Errorf("read contact update result: %w", err)
	}
	if changed != 1 {
		return contact.Contact{}, contact.ErrNotFound
	}
	stored, _, err := set.contactByID(ctx, value.ID)
	return stored, err
}

func (set *Set) DeleteContact(ctx context.Context, id string) error {
	if set == nil || set.Contacts == nil {
		return errors.New("contacts database is not open")
	}
	result, err := set.Contacts.ExecContext(ctx, `DELETE FROM contacts WHERE contact_id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete contact: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read contact delete result: %w", err)
	}
	if changed != 1 {
		return contact.ErrNotFound
	}
	return nil
}

func (set *Set) ListContacts(ctx context.Context) ([]contact.Contact, error) {
	if set == nil || set.Contacts == nil {
		return nil, errors.New("contacts database is not open")
	}
	rows, err := set.Contacts.QueryContext(ctx, `
SELECT contact_id, display_name, phone_number, created_at_unix_ms, updated_at_unix_ms
FROM contacts ORDER BY display_name COLLATE NOCASE, contact_id
`)
	if err != nil {
		return nil, fmt.Errorf("list contacts: %w", err)
	}
	defer rows.Close()
	values := make([]contact.Contact, 0)
	for rows.Next() {
		value, err := scanContact(rows)
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate contacts: %w", err)
	}
	return values, nil
}

func (set *Set) contactByID(ctx context.Context, id string) (contact.Contact, bool, error) {
	row := set.Contacts.QueryRowContext(ctx, `
SELECT contact_id, display_name, phone_number, created_at_unix_ms, updated_at_unix_ms
FROM contacts WHERE contact_id = ?
`, id)
	value, err := scanContact(row)
	if errors.Is(err, sql.ErrNoRows) {
		return contact.Contact{}, false, nil
	}
	return value, err == nil, err
}

type contactScanner interface{ Scan(...any) error }

func scanContact(scanner contactScanner) (contact.Contact, error) {
	var value contact.Contact
	var createdAt, updatedAt int64
	if err := scanner.Scan(&value.ID, &value.DisplayName, &value.PhoneNumber, &createdAt, &updatedAt); err != nil {
		return contact.Contact{}, err
	}
	value.CreatedAt = time.UnixMilli(createdAt).UTC()
	value.UpdatedAt = time.UnixMilli(updatedAt).UTC()
	return value, nil
}

func classifyContactWrite(operation string, err error) error {
	var sqliteErr *sqlite.Error
	if errors.As(err, &sqliteErr) && sqliteErr.Code() == 2067 && strings.Contains(strings.ToLower(err.Error()), "phone_number") {
		return contact.ErrPhoneConflict
	}
	return fmt.Errorf("%s: %w", operation, err)
}
