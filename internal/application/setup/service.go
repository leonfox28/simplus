package setup

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	InstallationUninitialized = "uninitialized"
	InstallationReady         = "ready"
	InstallationMaintenance   = "maintenance"
)

var ErrSetupUnavailable = errors.New("installation is not available")
var ErrAdministratorRequestInvalid = errors.New("initial administrator request is invalid")
var administratorUsernamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{2,31}$`)

type StateStore interface {
	InstallationState(context.Context) (string, error)
}
type AdministratorStore interface {
	ConfigureInitialAdministrator(context.Context, string, string, string, time.Time) error
	ReadInitialAdministrator(context.Context) (string, string, bool, error)
}
type PasswordHasher interface{ Hash(string) (string, error) }
type AdministratorInput struct{ Username, Password, PasswordConfirmation, InstanceDefaultLocale string }
type Status struct {
	InstallationState                   string
	BusinessAPIAvailable, SetupRequired bool
}
type Dependencies struct {
	StateStore         StateStore
	AdministratorStore AdministratorStore
	PasswordHasher     PasswordHasher
}
type Service struct {
	stateStore     StateStore
	administrator  AdministratorStore
	passwordHasher PasswordHasher
	now            func() time.Time
	mutationMu     sync.Mutex
}

func New(d Dependencies) (*Service, error) {
	if d.StateStore == nil || d.AdministratorStore == nil || d.PasswordHasher == nil {
		return nil, errors.New("installation dependencies are incomplete")
	}
	return &Service{stateStore: d.StateStore, administrator: d.AdministratorStore, passwordHasher: d.PasswordHasher, now: time.Now}, nil
}
func (service *Service) Status(ctx context.Context) (Status, error) {
	state, err := service.installationState(ctx)
	return Status{InstallationState: state, BusinessAPIAvailable: state == InstallationReady, SetupRequired: state == InstallationUninitialized}, err
}
func (service *Service) ProvisionAdministrator(ctx context.Context, input AdministratorInput) (bool, error) {
	if service == nil {
		return false, fmt.Errorf("setup service is not configured")
	}
	service.mutationMu.Lock()
	defer service.mutationMu.Unlock()
	if service.administrator == nil || service.passwordHasher == nil {
		return false, fmt.Errorf("initial administrator setup is not configured")
	}
	_, _, configured, err := service.administrator.ReadInitialAdministrator(ctx)
	if err != nil {
		return false, fmt.Errorf("read initial administrator: %w", err)
	}
	if configured {
		return false, nil
	}
	if err := service.requireUninitialized(ctx); err != nil {
		return false, err
	}
	username, err := validateAdministratorInput(input)
	if err != nil {
		return false, err
	}
	passwordHash, err := service.passwordHasher.Hash(input.Password)
	if err != nil {
		return false, fmt.Errorf("hash provisioned administrator password: %w", err)
	}
	if err := service.administrator.ConfigureInitialAdministrator(ctx, username, passwordHash, input.InstanceDefaultLocale, service.currentTime()); err != nil {
		return false, fmt.Errorf("persist provisioned administrator: %w", err)
	}
	return true, nil
}

func validateAdministratorInput(input AdministratorInput) (string, error) {
	username := strings.ToLower(strings.TrimSpace(input.Username))
	if !administratorUsernamePattern.MatchString(username) {
		return "", ErrAdministratorRequestInvalid
	}
	if input.InstanceDefaultLocale != "zh-CN" && input.InstanceDefaultLocale != "en-US" {
		return "", ErrAdministratorRequestInvalid
	}
	if input.Password != input.PasswordConfirmation ||
		len(input.Password) > 256 ||
		utf8.RuneCountInString(input.Password) < 12 ||
		utf8.RuneCountInString(input.Password) > 128 {
		return "", ErrAdministratorRequestInvalid
	}
	for _, character := range input.Password {
		if unicode.IsControl(character) {
			return "", ErrAdministratorRequestInvalid
		}
	}
	return username, nil
}

func (service *Service) installationState(ctx context.Context) (string, error) {
	if service == nil || service.stateStore == nil {
		return "", fmt.Errorf("setup state store is not configured")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	state, err := service.stateStore.InstallationState(ctx)
	if err != nil {
		return "", fmt.Errorf("read setup state: %w", err)
	}
	return state, nil
}

func (service *Service) requireUninitialized(ctx context.Context) error {
	state, err := service.installationState(ctx)
	if err != nil {
		return err
	}
	if state != InstallationUninitialized {
		return ErrSetupUnavailable
	}
	return nil
}

func (service *Service) currentTime() time.Time {
	return service.now().UTC().Truncate(time.Second)
}
