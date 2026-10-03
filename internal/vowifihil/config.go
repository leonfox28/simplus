package vowifihil

import (
	"github.com/leonfox28/simplus/internal/ims"
)

func Build(input ims.Input) (ims.Config, error) {
	return ims.BuildAt(input, ims.RuntimePaths{
		RunDirectory: RunDirectory, StrongSwanConfig: StrongSwanConfig,
		VICISocket: VICISocket, LogPipe: LogPipe,
	})
}

const (
	RunDirectory     = "/run/simplus-vowifi-hil"
	StrongSwanConfig = RunDirectory + "/strongswan.conf"
	VICIConfig       = RunDirectory + "/vici.json"
	VICISocket       = RunDirectory + "/charon.vici"
	LogPipe          = RunDirectory + "/charon.pipe"
)
