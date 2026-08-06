package vowifi

import "time"

type Desire struct {
	LineID        string
	DesiredActive bool
	UpdatedAt     time.Time
}

type State struct {
	LineID        string
	DesiredActive bool
	Eligible      bool
	ReadinessCode string
	State         string
	Stage         string
	Online        bool
	EgressMode    string
	CountryCode   string
	CountryName   string
	RegisteredAt  time.Time
	NextRefreshAt time.Time
	PhoneNumber   string
	Attempt       int
	LastErrorCode string
}
