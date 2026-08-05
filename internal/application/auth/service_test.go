package auth

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type authTestState struct{ state string }

func (state authTestState) InstallationState(context.Context) (string, error) {
	return state.state, nil
}

type authTestVerifier struct{}

func (authTestVerifier) Verify(encoded, secret string) (bool, error) {
	return encoded == "hash" && secret == "correct", nil
}

type authPasswordCodec struct{}

func (authPasswordCodec) Verify(encoded, secret string) (bool, error) {
	return (encoded == "hash" && secret == "correct") || (encoded == "new-hash" && secret == "new-password-value"), nil
}
func (authPasswordCodec) Hash(secret string) (string, error) {
	if secret != "new-password-value" {
		return "", errors.New("unexpected password")
	}
	return "new-hash", nil
}

type authPasswordStore struct {
	authTestStore
	hash       string
	generation int64
}

func (store *authPasswordStore) ReadAdministrator(context.Context) (string, string, string, int64, bool, error) {
	return "admin", store.hash, "zh-CN", store.generation, true, nil
}
func (store *authPasswordStore) ChangeAdministratorPassword(_ context.Context, username, hash string, generation int64, _ time.Time) (bool, error) {
	if username != "admin" || generation != store.generation {
		return false, nil
	}
	store.hash = hash
	store.generation++
	store.sessions = make(map[[32]byte]authTestSession)
	return true, nil
}

type authTestSession struct {
	csrf       [32]byte
	expires    int64
	user       string
	generation int64
}

type authTestStore struct {
	mu       sync.Mutex
	sessions map[[32]byte]authTestSession
}

func (store *authTestStore) ReadAdministrator(context.Context) (string, string, string, int64, bool, error) {
	return "admin", "hash", "zh-CN", 1, true, nil
}
func (store *authTestStore) CreateAdministratorSession(_ context.Context, token, csrf [32]byte, username string, generation, _, expires int64) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.sessions == nil {
		store.sessions = make(map[[32]byte]authTestSession)
	}
	store.sessions[token] = authTestSession{csrf: csrf, expires: expires, user: username, generation: generation}
	return nil
}
func (store *authTestStore) ReadAdministratorSession(_ context.Context, token [32]byte, now int64) (string, [32]byte, int64, int64, bool, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	session, ok := store.sessions[token]
	if !ok || session.expires <= now {
		return "", [32]byte{}, 0, 0, false, nil
	}
	return session.user, session.csrf, session.generation, session.expires, true, nil
}
func (store *authTestStore) DeleteAdministratorSession(_ context.Context, token [32]byte) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	delete(store.sessions, token)
	return nil
}

func TestLoginAuthenticateCSRFAndLogout(t *testing.T) {
	store := &authTestStore{}
	service := NewService(authTestState{state: "ready"}, store, authTestVerifier{})
	service.now = func() time.Time { return time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC) }
	result, err := service.Login(context.Background(), "admin", "correct")
	if err != nil {
		t.Fatal(err)
	}
	if result.User.Username != "admin" || result.User.Locale != "zh-CN" || len(result.SessionToken) != 43 || len(result.CSRFToken) != 43 {
		t.Fatalf("login result = %#v", result)
	}
	if _, err := service.Authenticate(context.Background(), result.SessionToken, "", false); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Authenticate(context.Background(), result.SessionToken, "wrong", true); !errors.Is(err, ErrCSRFInvalid) {
		t.Fatalf("invalid CSRF error = %v", err)
	}
	if err := service.Logout(context.Background(), result.SessionToken, result.CSRFToken); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Authenticate(context.Background(), result.SessionToken, "", false); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("logged-out session error = %v", err)
	}
}

func TestLoginGuardsStateCredentialsAndRate(t *testing.T) {
	if _, err := NewService(authTestState{state: "uninitialized"}, &authTestStore{}, authTestVerifier{}).Login(context.Background(), "admin", "correct"); err != nil {
		t.Fatalf("uninitialized administrator login error = %v", err)
	}
	service := NewService(authTestState{state: "ready"}, &authTestStore{}, authTestVerifier{})
	now := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }
	for attempt := 0; attempt < failureLimit; attempt++ {
		if _, err := service.Login(context.Background(), "admin", "wrong"); !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("attempt %d = %v", attempt, err)
		}
	}
	if _, err := service.Login(context.Background(), "admin", "correct"); !errors.Is(err, ErrLoginRateLimited) {
		t.Fatalf("rate-limit error = %v", err)
	}
	now = now.Add(failureWindow)
	if _, err := service.Login(context.Background(), "admin", "correct"); err != nil {
		t.Fatal(err)
	}
}

func TestChangePasswordVerifiesCurrentPasswordAndRevokesSessions(t *testing.T) {
	store := &authPasswordStore{hash: "hash", generation: 1}
	service := NewService(authTestState{state: "ready"}, store, authPasswordCodec{})
	service.now = func() time.Time { return time.Date(2026, 8, 4, 1, 0, 0, 0, time.UTC) }
	login, err := service.Login(context.Background(), "admin", "correct")
	if err != nil {
		t.Fatal(err)
	}
	if err := service.ChangePassword(context.Background(), login.SessionToken, login.CSRFToken, "wrong", "new-password-value", "new-password-value"); !errors.Is(err, ErrCurrentPasswordInvalid) {
		t.Fatalf("wrong current password error = %v", err)
	}
	if err := service.ChangePassword(context.Background(), login.SessionToken, login.CSRFToken, "correct", "new-password-value", "new-password-value"); err != nil {
		t.Fatal(err)
	}
	if store.hash != "new-hash" || store.generation != 2 {
		t.Fatalf("credential = %q/%d", store.hash, store.generation)
	}
	if _, err := service.Authenticate(context.Background(), login.SessionToken, "", false); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("old session error = %v", err)
	}
}
