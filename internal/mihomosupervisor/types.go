package mihomosupervisor

import "github.com/leonfox28/simplus/internal/domain/mihomo"

type StartRequest = mihomo.StartRequest
type Status = mihomo.Status
type API = mihomo.API

var ErrAlreadyRunning = mihomo.ErrAlreadyRunning
var ErrNotRunning = mihomo.ErrNotRunning
var ErrRequestInvalid = mihomo.ErrRequestInvalid
var ErrStartupFailed = mihomo.ErrStartupFailed
