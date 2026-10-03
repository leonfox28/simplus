package mihomo

const ZashboardVersion = "v3.6.0"

type DashboardStatus struct {
	Available         bool
	Version           string
	ControllerAddress string
	URL               string
	Secret            string
}
