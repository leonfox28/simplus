package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
)

var (
	ErrInvalidCredentials     = errors.New("invalid administrator credentials")
	ErrUnauthorized           = errors.New("administrator session is unauthorized")
	ErrCSRFInvalid            = errors.New("CSRF token is invalid")
	ErrLoginRateLimited       = errors.New("administrator login is rate limited")
	ErrInstanceNotReady       = errors.New("instance is not ready for administrator login")
	ErrPasswordRequestInvalid = errors.New("administrator password request is invalid")
	ErrCurrentPasswordInvalid = errors.New("current administrator password is invalid")
)

const (
	defaultSessionLifetime = 12 * time.Hour
	failureLimit           = 5
	failureWindow          = 30 * time.Second
)

type StateStore interface {
	InstallationState(context.Context) (string, error)
}

type Store interface {
	ReadAdministrator(context.Context) (string, string, string, int64, bool, error)
	CreateAdministratorSession(context.Context, [32]byte, [32]byte, string, int64, int64, int64) error
	ReadAdministratorSession(context.Context, [32]byte, int64) (string, [32]byte, int64, int64, bool, error)
	DeleteAdministratorSession(context.Context, [32]byte) error
}

type PasswordVerifier interface {
	Verify(string, string) (bool, error)
}

type PasswordHasher interface{ Hash(string) (string, error) }
type PasswordChanger interface {
	ChangeAdministratorPassword(context.Context, string, string, int64, time.Time) (bool, error)
}

type User struct {
	Username string
	Locale   string
}

type LoginResult struct {
	SessionToken string
	CSRFToken    string
	ExpiresAt    time.Time
	User         User
}

type Session struct {
	ExpiresAt time.Time
	User      User
}

type Service struct {
	stateStore StateStore
	store      Store
	verifier   PasswordVerifier
	random     io.Reader
	now        func() time.Time
	lifetime   time.Duration

	failureMu    sync.Mutex
	failureCount int
	blockedUntil time.Time
}

func NewService(stateStore StateStore, store Store, verifier PasswordVerifier) *Service {
	return &Service{
		stateStore: stateStore,
		store:      store,
		verifier:   verifier,
		random:     rand.Reader,
		now:        time.Now,
		lifetime:   defaultSessionLifetime,
	}
}

func (service *Service) Login(ctx context.Context, username, secret string) (LoginResult, error) {
	if service == nil || service.stateStore == nil || service.store == nil || service.verifier == nil || service.random == nil {
		return LoginResult{}, fmt.Errorf("authentication service is not configured")
	}
	state, err := service.stateStore.InstallationState(ctx)
	if err != nil {
		return LoginResult{}, fmt.Errorf("read installation state for login: %w", err)
	}
	if state != "ready" && state != "uninitialized" {
		return LoginResult{}, ErrInstanceNotReady
	}
	now := service.now().UTC()
	if service.loginBlocked(now) {
		return LoginResult{}, ErrLoginRateLimited
	}
	storedUsername, encodedPassword, locale, sessionGeneration, found, err := service.store.ReadAdministrator(ctx)
	if err != nil {
		return LoginResult{}, fmt.Errorf("read administrator credential: %w", err)
	}
	valid := false
	if found {
		passwordValid, verifyErr := service.verifier.Verify(encodedPassword, secret)
		if verifyErr != nil {
			return LoginResult{}, fmt.Errorf("verify administrator password: %w", verifyErr)
		}
		valid = passwordValid && strings.TrimSpace(username) == storedUsername
	}
	if !valid {
		service.recordFailure(now)
		return LoginResult{}, ErrInvalidCredentials
	}
	service.clearFailures()
	sessionToken, sessionHash, err := service.issueToken()
	if err != nil {
		return LoginResult{}, err
	}
	csrfToken, csrfHash, err := service.issueToken()
	if err != nil {
		return LoginResult{}, err
	}
	expiresAt := now.Add(service.lifetime)
	if err := service.store.CreateAdministratorSession(ctx, sessionHash, csrfHash, storedUsername, sessionGeneration, now.Unix(), expiresAt.Unix()); err != nil {
		return LoginResult{}, fmt.Errorf("persist administrator session: %w", err)
	}
	return LoginResult{
		SessionToken: sessionToken,
		CSRFToken:    csrfToken,
		ExpiresAt:    expiresAt,
		User:         User{Username: storedUsername, Locale: locale},
	}, nil
}

func (service *Service) Authenticate(ctx context.Context, token, csrfToken string, requireCSRF bool) (Session, error) {
	if service == nil || service.store == nil {
		return Session{}, fmt.Errorf("authentication service is not configured")
	}
	tokenHash, ok := decodeTokenHash(token)
	if !ok {
		return Session{}, ErrUnauthorized
	}
	now := service.now().UTC()
	username, expectedCSRF, sessionGeneration, expiresAtUnix, found, err := service.store.ReadAdministratorSession(ctx, tokenHash, now.Unix())
	if err != nil {
		return Session{}, fmt.Errorf("read administrator session: %w", err)
	}
	if !found {
		return Session{}, ErrUnauthorized
	}
	if requireCSRF {
		actualCSRF, ok := decodeTokenHash(csrfToken)
		if !ok || subtle.ConstantTimeCompare(actualCSRF[:], expectedCSRF[:]) != 1 {
			return Session{}, ErrCSRFInvalid
		}
	}
	currentUsername, _, locale, currentGeneration, found, err := service.store.ReadAdministrator(ctx)
	if err != nil || !found {
		if err != nil {
			return Session{}, fmt.Errorf("read administrator profile: %w", err)
		}
		return Session{}, ErrUnauthorized
	}
	if currentUsername != username || currentGeneration != sessionGeneration {
		return Session{}, ErrUnauthorized
	}
	return Session{ExpiresAt: time.Unix(expiresAtUnix, 0).UTC(), User: User{Username: username, Locale: locale}}, nil
}

func (service *Service) Logout(ctx context.Context, token, csrfToken string) error {
	if _, err := service.Authenticate(ctx, token, csrfToken, true); err != nil {
		return err
	}
	hash, _ := decodeTokenHash(token)
	if err := service.store.DeleteAdministratorSession(ctx, hash); err != nil {
		return fmt.Errorf("delete administrator session: %w", err)
	}
	return nil
}

func (service *Service) ChangePassword(ctx context.Context, token, csrfToken, currentPassword, newPassword, confirmation string) error {
	if newPassword != confirmation || utf8.RuneCountInString(newPassword) < 12 || utf8.RuneCountInString(newPassword) > 128 || len(newPassword) > 256 {
		return ErrPasswordRequestInvalid
	}
	for _, character := range newPassword {
		if unicode.IsControl(character) {
			return ErrPasswordRequestInvalid
		}
	}
	session, err := service.Authenticate(ctx, token, csrfToken, true)
	if err != nil {
		return err
	}
	username, encodedPassword, _, generation, found, err := service.store.ReadAdministrator(ctx)
	if err != nil {
		return fmt.Errorf("read administrator for password replacement: %w", err)
	}
	if !found || username != session.User.Username {
		return ErrUnauthorized
	}
	valid, err := service.verifier.Verify(encodedPassword, currentPassword)
	if err != nil {
		return fmt.Errorf("verify current administrator password: %w", err)
	}
	if !valid {
		return ErrCurrentPasswordInvalid
	}
	hasher, ok := service.verifier.(PasswordHasher)
	if !ok {
		return errors.New("administrator password hasher is unavailable")
	}
	changer, ok := service.store.(PasswordChanger)
	if !ok {
		return errors.New("administrator password store is unavailable")
	}
	encodedNewPassword, err := hasher.Hash(newPassword)
	if err != nil {
		return fmt.Errorf("hash new administrator password: %w", err)
	}
	changed, err := changer.ChangeAdministratorPassword(ctx, username, encodedNewPassword, generation, service.now().UTC())
	if err != nil {
		return err
	}
	if !changed {
		return ErrUnauthorized
	}
	return nil
}

func (service *Service) issueToken() (string, [32]byte, error) {
	raw := make([]byte, 32)
	if _, err := io.ReadFull(service.random, raw); err != nil {
		return "", [32]byte{}, fmt.Errorf("generate authentication token: %w", err)
	}
	encoded := base64.RawURLEncoding.EncodeToString(raw)
	return encoded, sha256.Sum256([]byte(encoded)), nil
}

func decodeTokenHash(token string) ([32]byte, bool) {
	if len(token) != 43 {
		return [32]byte{}, false
	}
	raw, err := base64.RawURLEncoding.Strict().DecodeString(token)
	if err != nil || len(raw) != 32 {
		return [32]byte{}, false
	}
	return sha256.Sum256([]byte(token)), true
}

func (service *Service) loginBlocked(now time.Time) bool {
	service.failureMu.Lock()
	defer service.failureMu.Unlock()
	return now.Before(service.blockedUntil)
}

func (service *Service) recordFailure(now time.Time) {
	service.failureMu.Lock()
	defer service.failureMu.Unlock()
	service.failureCount++
	if service.failureCount >= failureLimit {
		service.blockedUntil = now.Add(failureWindow)
		service.failureCount = 0
	}
}

func (service *Service) clearFailures() {
	service.failureMu.Lock()
	defer service.failureMu.Unlock()
	service.failureCount = 0
	service.blockedUntil = time.Time{}
}
