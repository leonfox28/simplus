package accesspath

import (
	"context"
	"errors"
	"github.com/leonfox28/simplus/internal/domain/accesspath"
)

var ErrInvalid = errors.New("access path request invalid")

type Repository interface {
	ListAccessPathConfigurations(context.Context) ([]accesspath.Configuration, error)
	PutAccessPathConfiguration(context.Context, accesspath.Configuration) error
}
type Service struct{ repository Repository }

func New(repository Repository) (*Service, error) {
	if repository == nil {
		return nil, errors.New("access path repository unavailable")
	}
	return &Service{repository: repository}, nil
}
func (service *Service) List(ctx context.Context) ([]accesspath.State, error) {
	values, err := service.repository.ListAccessPathConfigurations(ctx)
	if err != nil {
		return nil, err
	}
	states := make([]accesspath.State, 0, len(values))
	for _, value := range values {
		states = append(states, derive(value))
	}
	return states, nil
}
func (service *Service) Configure(ctx context.Context, lineID, mode, mihomoState string) (accesspath.State, error) {
	if (lineID != "simulator-line-1" && lineID != "simulator-line-2") || (mode != "direct" && mode != "mihomo-required") || (mihomoState != "running" && mihomoState != "stopped" && mihomoState != "failed") {
		return accesspath.State{}, ErrInvalid
	}
	value := accesspath.Configuration{LineID: lineID, Mode: mode, MihomoState: mihomoState}
	if err := service.repository.PutAccessPathConfiguration(ctx, value); err != nil {
		return accesspath.State{}, err
	}
	return derive(value), nil
}
func derive(value accesspath.Configuration) accesspath.State {
	state := accesspath.State{LineID: value.LineID, Mode: value.Mode, MihomoState: value.MihomoState, Authentication: "simulated-aka-complete", EPDG: "connected", IMS: "registered", LineState: "online", DirectFallback: false}
	if value.Mode == "mihomo-required" && value.MihomoState != "running" {
		state.LineState = "offline"
		state.EPDG = "blocked"
		state.IMS = "offline"
	}
	return state
}

func (service *Service) Available(ctx context.Context, lineID string) bool {
	states, err := service.List(ctx)
	if err != nil {
		return false
	}
	for _, state := range states {
		if state.LineID == lineID {
			return state.LineState == "online"
		}
	}
	return false
}
