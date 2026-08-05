package euicc

import (
	"context"
	"errors"
	"regexp"

	domaineuicc "github.com/leonfox28/simplus/internal/domain/euicc"
)

var (
	ErrInvalid  = errors.New("eUICC request is invalid")
	ErrNotFound = errors.New("eUICC profile not found")
)
var profilePattern = regexp.MustCompile(`^simulator-euicc-profile-[ab]$`)

type Profile = domaineuicc.Profile
type State = domaineuicc.State
type Repository interface {
	ListSimulatorEUICCProfiles(context.Context) ([]domaineuicc.Profile, error)
	SwitchSimulatorEUICCProfile(context.Context, string) error
}
type Service struct{ repository Repository }

func New(repository Repository) (*Service, error) {
	if repository == nil {
		return nil, errors.New("eUICC repository unavailable")
	}
	return &Service{repository: repository}, nil
}
func (service *Service) State(ctx context.Context) (domaineuicc.State, error) {
	profiles, err := service.repository.ListSimulatorEUICCProfiles(ctx)
	if err != nil {
		return domaineuicc.State{}, err
	}
	return domaineuicc.State{EIDHint: "EID •••• 0001", Profiles: profiles}, nil
}
func (service *Service) Switch(ctx context.Context, id string) (domaineuicc.State, error) {
	if !profilePattern.MatchString(id) {
		return domaineuicc.State{}, ErrInvalid
	}
	profiles, err := service.repository.ListSimulatorEUICCProfiles(ctx)
	if err != nil {
		return domaineuicc.State{}, err
	}
	found := false
	for _, profile := range profiles {
		found = found || profile.ID == id
	}
	if !found {
		return domaineuicc.State{}, ErrNotFound
	}
	if err := service.repository.SwitchSimulatorEUICCProfile(ctx, id); err != nil {
		return domaineuicc.State{}, err
	}
	state, err := service.State(ctx)
	if err != nil {
		return domaineuicc.State{}, err
	}
	for _, profile := range state.Profiles {
		if profile.ID == id && profile.Active {
			return state, nil
		}
	}
	return domaineuicc.State{}, errors.New("eUICC switch read-back did not confirm target")
}
