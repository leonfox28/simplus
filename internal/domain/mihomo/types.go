package mihomo

import "time"

type Subscription struct {
	ID                string
	DisplayName       string
	URLCiphertext     []byte
	URLPlaintext      string
	URLHint           string
	Enabled           bool
	LastRefreshAt     time.Time
	LastRefreshStatus string
	NodeCount         int
	LastErrorCode     string
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type Node struct {
	SubscriptionID string
	ID             string
	DisplayName    string
	Kind           string
	ProxyYAML      string
	CountryCode    string
	CountryName    string
}

type EgressProfile struct {
	ID                   string
	DisplayName          string
	SubscriptionID       string
	LineID               string
	SelectionType        string
	SelectedNodeID       string
	SelectedCountryCode  string
	SourceCIDR           string
	Enabled              bool
	CreatedAt, UpdatedAt time.Time
}
