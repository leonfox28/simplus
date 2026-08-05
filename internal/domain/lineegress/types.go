package lineegress

import "time"

const (
	ModeDirect        = "direct"
	ModeMihomoCountry = "mihomo-country"
)

type Binding struct {
	LineID      string
	Mode        string
	CountryCode string
	UpdatedAt   time.Time
}

// CountryListenerPort is the stable, subscription-independent localhost
// TPROXY entry assigned to an ISO alpha-2 country group.
func CountryListenerPort(code string) int {
	if len(code) != 2 || code[0] < 'A' || code[0] > 'Z' || code[1] < 'A' || code[1] > 'Z' {
		return 0
	}
	return 20000 + int(code[0]-'A')*26 + int(code[1]-'A')
}
