package main

import (
	"github.com/leonfox28/simplus/internal/application/setup"
	"github.com/leonfox28/simplus/internal/security/password"
	sqlitestore "github.com/leonfox28/simplus/internal/storage/sqlite"
)

func newSetupService(stores *sqlitestore.Set) (*setup.Service, error) {
	return setup.New(setup.Dependencies{StateStore: stores, AdministratorStore: stores, PasswordHasher: password.NewDefaultHasher()})
}
