package accesspath

type Configuration struct{ LineID, Mode, MihomoState string }
type State struct {
	LineID, Mode, MihomoState, LineState, Authentication, EPDG, IMS string
	DirectFallback                                                  bool
}
