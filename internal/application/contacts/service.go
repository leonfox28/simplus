package contacts

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"io"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/leonfox28/simplus/internal/domain/contact"
)

var ErrInvalid = errors.New("contact request is invalid")

var (
	contactIDPattern = regexp.MustCompile(`^contact_[A-Za-z0-9_-]{16,120}$`)
	phonePattern     = regexp.MustCompile(`^\+?[0-9]{3,20}$`)
)

type Repository interface {
	CreateContact(context.Context, contact.Contact) (contact.Contact, error)
	UpdateContact(context.Context, contact.Contact) (contact.Contact, error)
	DeleteContact(context.Context, string) error
	ListContacts(context.Context) ([]contact.Contact, error)
}

type Service struct {
	repository Repository
	random     io.Reader
	now        func() time.Time
}

func New(repository Repository) (*Service, error) {
	if repository == nil {
		return nil, errors.New("contacts repository is unavailable")
	}
	return &Service{repository: repository, random: rand.Reader, now: time.Now}, nil
}

func (service *Service) List(ctx context.Context) ([]contact.Contact, error) {
	return service.repository.ListContacts(ctx)
}

func (service *Service) Create(ctx context.Context, displayName, phoneNumber string) (contact.Contact, error) {
	displayName, phoneNumber, err := validateContactFields(displayName, phoneNumber)
	if err != nil {
		return contact.Contact{}, err
	}
	random := make([]byte, 16)
	if _, err := io.ReadFull(service.random, random); err != nil {
		return contact.Contact{}, err
	}
	now := service.now().UTC()
	return service.repository.CreateContact(ctx, contact.Contact{
		ID: "contact_" + base64.RawURLEncoding.EncodeToString(random), DisplayName: displayName,
		PhoneNumber: phoneNumber, CreatedAt: now, UpdatedAt: now,
	})
}

func (service *Service) Update(ctx context.Context, id, displayName, phoneNumber string) (contact.Contact, error) {
	if !contactIDPattern.MatchString(id) {
		return contact.Contact{}, ErrInvalid
	}
	displayName, phoneNumber, err := validateContactFields(displayName, phoneNumber)
	if err != nil {
		return contact.Contact{}, err
	}
	return service.repository.UpdateContact(ctx, contact.Contact{
		ID: id, DisplayName: displayName, PhoneNumber: phoneNumber, UpdatedAt: service.now().UTC(),
	})
}

func (service *Service) Delete(ctx context.Context, id string) error {
	if !contactIDPattern.MatchString(id) {
		return ErrInvalid
	}
	return service.repository.DeleteContact(ctx, id)
}

func validateContactFields(displayName, phoneNumber string) (string, string, error) {
	displayName = strings.TrimSpace(displayName)
	phoneNumber = strings.TrimSpace(phoneNumber)
	if displayName == "" || !utf8.ValidString(displayName) || utf8.RuneCountInString(displayName) > 80 || len(displayName) > 320 || !phonePattern.MatchString(phoneNumber) {
		return "", "", ErrInvalid
	}
	return displayName, phoneNumber, nil
}
