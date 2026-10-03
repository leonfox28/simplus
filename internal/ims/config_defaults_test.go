package ims

const (
	RunDirectory     = "/run/simplus-vowifi-hil"
	StrongSwanConfig = RunDirectory + "/strongswan.conf"
	VICIConfig       = RunDirectory + "/vici.json"
	VICISocket       = RunDirectory + "/charon.vici"
	LogPipe          = RunDirectory + "/charon.pipe"
)

func Build(input Input) (Config, error) {
	paths, _ := PathsFor(RunDirectory)
	return BuildAt(input, paths)
}
