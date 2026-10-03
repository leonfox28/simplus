package mihomoassets

import (
	"regexp"

	app "github.com/leonfox28/simplus/internal/application/mihomo"
)

type CoreStatus = app.CoreStatus
type CoreStatusReader = app.CoreStatusReader
type CountrySummary = app.CountrySummary
type ArtifactMetadata = app.ArtifactMetadata
type ConfigStatus = app.ConfigStatus
type DashboardStatus = app.DashboardStatus
type Candidate = app.Candidate

var ErrVersionAlreadyInstalled = app.ErrVersionAlreadyInstalled
var ErrConfigNotReady = app.ErrConfigNotReady
var ErrConfigValidationFailed = app.ErrConfigValidationFailed
var subscriptionIDPattern = regexp.MustCompile(`^subscription_[A-Za-z0-9_-]{22}$`)

const ZashboardVersion = app.ZashboardVersion

func countryPort(code string) int { return app.CountryListenerPort(code) }
