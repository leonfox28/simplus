package health

import (
	"context"
	"fmt"

	"github.com/leonfox28/simplus/internal/buildinfo"
)

const APIVersion = "v1"

type StateStore interface {
	InstallationState(context.Context) (string, error)
}

type Snapshot struct {
	Status            string
	Version           string
	APIVersion        string
	InstallationState string
	Backend           string
	DatabaseCount     int
}

type Service struct {
	store   StateStore
	backend string
}

func New(store StateStore, backend string) *Service {
	return &Service{store: store, backend: backend}
}

func (service *Service) Snapshot(ctx context.Context) (Snapshot, error) {
	if service == nil || service.store == nil {
		return Snapshot{}, fmt.Errorf("health state store is not configured")
	}
	state, err := service.store.InstallationState(ctx)
	if err != nil {
		return Snapshot{}, fmt.Errorf("health snapshot: %w", err)
	}
	return Snapshot{
		Status:            "ok",
		Version:           buildinfo.Current().Version,
		APIVersion:        APIVersion,
		InstallationState: state,
		Backend:           service.backend,
		DatabaseCount:     5,
	}, nil
}
