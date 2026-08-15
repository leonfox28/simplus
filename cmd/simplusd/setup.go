package main

import (
	"time"

	"github.com/leonfox28/simplus/internal/application/setup"
	"github.com/leonfox28/simplus/internal/security/managementcert"
	"github.com/leonfox28/simplus/internal/security/password"
	"github.com/leonfox28/simplus/internal/security/secretbox"
	storagefs "github.com/leonfox28/simplus/internal/storage/filesystem"
	sqlitestore "github.com/leonfox28/simplus/internal/storage/sqlite"
)

func newSetupService(stores *sqlitestore.Set, instanceSecretKeyPath string) (*setup.Service, error) {
	return setup.New(setup.Dependencies{
		StateStore:         stores,
		AuthorizationStore: stores,
		AdministratorStore: stores,
		PasswordHasher:     password.NewDefaultHasher(),
		StorageStore:       stores,
		DirectoryPreparer: func(path string) (setup.DirectoryIdentity, error) {
			identity, err := storagefs.PreparePrivateDirectory(path)
			if err != nil {
				return setup.DirectoryIdentity{}, err
			}
			return setup.DirectoryIdentity{
				Path:   identity.Path,
				Device: identity.Device,
				Inode:  identity.Inode,
			}, nil
		},
		ManagementTLSStore: stores,
		SecretProtectorOpener: func() (setup.SecretProtector, error) {
			return secretbox.Open(instanceSecretKeyPath)
		},
		LocalCAGenerator: func(now time.Time, sans []string) (setup.LocalCABundle, error) {
			bundle, err := managementcert.GenerateLocalCA(now, sans)
			if err != nil {
				return setup.LocalCABundle{}, err
			}
			return setup.LocalCABundle{
				CACertificatePEM:   bundle.CACertificatePEM,
				CAPrivateKeyPEM:    bundle.CAPrivateKeyPEM,
				LeafCertificatePEM: bundle.LeafCertificatePEM,
				LeafPrivateKeyPEM:  bundle.LeafPrivateKeyPEM,
				RootFingerprint:    bundle.RootFingerprint,
				LeafNotAfter:       bundle.LeafNotAfter,
				SANs:               bundle.SANs,
			}, nil
		},
		HardwareReviewStore: stores,
		CompletionStore:     stores,
	})
}
