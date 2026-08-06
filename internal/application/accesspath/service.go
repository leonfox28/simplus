package accesspath

import (
	"context"
	"errors"
	"regexp"
	"sort"

	"github.com/leonfox28/simplus/internal/application/inventory"
	"github.com/leonfox28/simplus/internal/domain/accesspath"
)

var ErrInvalid = errors.New("access path request invalid")

var lineIDPattern = regexp.MustCompile(`^line_[A-Za-z0-9_-]{22}$`)

type Repository interface {
	ListAccessPathConfigurations(context.Context) ([]accesspath.Configuration, error)
	PutAccessPathConfiguration(context.Context, accesspath.Configuration) error
}

type LineSource interface {
	Topology(context.Context) (inventory.Topology, error)
}

type Service struct {
	repository Repository
	lines      LineSource
}

func New(repository Repository, lines LineSource) (*Service, error) {
	if repository == nil || lines == nil {
		return nil, errors.New("access path dependencies are unavailable")
	}
	return &Service{repository: repository, lines: lines}, nil
}

func (service *Service) List(ctx context.Context) ([]accesspath.State, error) {
	values, err := service.repository.ListAccessPathConfigurations(ctx)
	if err != nil {
		return nil, err
	}
	topology, err := service.lines.Topology(ctx)
	if err != nil {
		return nil, err
	}
	configured := make(map[string]accesspath.Configuration, len(values))
	for _, value := range values {
		configured[value.LineID] = value
	}
	states := make([]accesspath.State, 0, len(topology.Lines))
	for _, line := range topology.Lines {
		if !lineIDPattern.MatchString(line.ID) {
			continue
		}
		value, exists := configured[line.ID]
		if !exists {
			value = accesspath.Configuration{LineID: line.ID, Mode: "direct", MihomoState: "stopped"}
		}
		states = append(states, derive(value))
	}
	sort.Slice(states, func(left, right int) bool { return states[left].LineID < states[right].LineID })
	return states, nil
}

func (service *Service) Configure(ctx context.Context, lineID, mode, mihomoState string) (accesspath.State, error) {
	if !lineIDPattern.MatchString(lineID) || (mode != "direct" && mode != "mihomo-required") ||
		(mihomoState != "running" && mihomoState != "stopped" && mihomoState != "failed") {
		return accesspath.State{}, ErrInvalid
	}
	topology, err := service.lines.Topology(ctx)
	if err != nil {
		return accesspath.State{}, err
	}
	found := false
	for _, line := range topology.Lines {
		found = found || line.ID == lineID
	}
	if !found {
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
