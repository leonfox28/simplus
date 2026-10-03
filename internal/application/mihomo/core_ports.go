package mihomo

import (
	"errors"
	"time"
)

var ErrVersionAlreadyInstalled = errors.New("Mihomo version is already installed")

type CoreStatus struct {
	Installed    bool      `json:"installed"`
	Version      string    `json:"version"`
	Architecture string    `json:"architecture"`
	SHA256       string    `json:"sha256"`
	BinaryPath   string    `json:"-"`
	InstalledAt  time.Time `json:"installedAt"`
}

type CoreStatusReader interface {
	Status() (CoreStatus, error)
}
