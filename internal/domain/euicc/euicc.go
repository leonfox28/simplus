package euicc

type Profile struct {
	ID, DisplayName, DisplayIdentityHint string
	Active                               bool
}
type State struct {
	EIDHint  string
	Profiles []Profile
}
