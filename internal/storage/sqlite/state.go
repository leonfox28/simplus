package sqlite

import (
	"context"
	"fmt"

	coredb "github.com/leonfox28/simplus/internal/storage/sqlite/generated/core"
)

const (
	InstallationUninitialized = "uninitialized"
	InstallationReady         = "ready"
	InstallationMaintenance   = "maintenance"
)

func (set *Set) InstallationState(ctx context.Context) (string, error) {
	if set == nil || set.Core == nil {
		return "", fmt.Errorf("core database is not open")
	}
	state, err := coredb.New(set.Core).GetInstallationState(ctx)
	if err != nil {
		return "", fmt.Errorf("read installation state: %w", err)
	}
	return state, nil
}
