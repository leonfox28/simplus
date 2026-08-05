package contact

import (
	"errors"
	"time"
)

var (
	ErrNotFound      = errors.New("contact not found")
	ErrPhoneConflict = errors.New("contact phone number already exists")
)

type Contact struct {
	ID          string
	DisplayName string
	PhoneNumber string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}
