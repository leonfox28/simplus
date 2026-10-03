package vowifi

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/leonfox28/simplus/internal/application/inventory"
	lineegressapp "github.com/leonfox28/simplus/internal/application/lineegress"
	mihomoapp "github.com/leonfox28/simplus/internal/application/mihomo"
	lineegressdomain "github.com/leonfox28/simplus/internal/domain/lineegress"
	domain "github.com/leonfox28/simplus/internal/domain/vowifi"
)

var (
	ErrLineNotFound = errors.New("Host VoWiFi Line was not found")
	ErrLineNotReady = errors.New("Host VoWiFi Line is not ready")
)

type Store interface {
	ListVoWiFiDesires(context.Context) ([]domain.Desire, error)
	PutVoWiFiDesire(context.Context, domain.Desire) error
}

type Inventory interface {
	Topology(context.Context) (inventory.Topology, error)
}

type Egress interface {
	List(context.Context) ([]lineegressapp.View, error)
}

type MihomoRuntime interface {
	Start(context.Context) (mihomoapp.RuntimeStatus, error)
	Restart(context.Context) (mihomoapp.RuntimeStatus, error)
}

type Service struct {
	Store      Store
	Inventory  Inventory
	Egress     Egress
	Mihomo     MihomoRuntime
	Supervisor domain.API
	Now        func() time.Time

	mu    sync.Mutex
	gates map[string]chan struct{}
}

func New(store Store, inventoryService Inventory, egress Egress, mihomo MihomoRuntime, supervisor domain.API) (*Service, error) {
	if store == nil || inventoryService == nil || egress == nil || mihomo == nil || supervisor == nil {
		return nil, errors.New("Host VoWiFi service is not configured")
	}
	return &Service{Store: store, Inventory: inventoryService, Egress: egress, Mihomo: mihomo, Supervisor: supervisor, Now: time.Now, gates: make(map[string]chan struct{})}, nil
}

func (service *Service) List(ctx context.Context) ([]domain.State, error) {
	return service.list(ctx)
}

// Available implements the messaging transport availability contract. SMS is
// dispatched only while the exact Line has an online registered Host VoWiFi
// worker; desired or reconnecting states are not sufficient.
func (service *Service) Available(ctx context.Context, lineID string) bool {
	states, err := service.List(ctx)
	if err != nil {
		return false
	}
	for _, state := range states {
		if state.LineID == lineID {
			return state.Online && state.State == domain.StateOnline
		}
	}
	return false
}

func (service *Service) Activate(ctx context.Context, lineID string) (domain.State, error) {
	release, err := service.lockLine(ctx, lineID)
	if err != nil {
		return domain.State{}, err
	}
	defer release()
	environment, err := service.environment(ctx)
	if err != nil {
		return domain.State{}, err
	}
	line, egress, ready := environment.readyLine(lineID)
	if line == nil {
		return domain.State{}, ErrLineNotFound
	}
	if hardwareReady(*line) && egress != nil && requiresMihomoRecovery(*egress) {
		if err := service.recoverMihomo(ctx, *egress); err != nil {
			return domain.State{}, err
		}
		environment, err = service.environment(ctx)
		if err != nil {
			return domain.State{}, err
		}
		line, egress, ready = environment.readyLine(lineID)
	}
	if !ready {
		return domain.State{}, ErrLineNotReady
	}
	if err := service.Store.PutVoWiFiDesire(ctx, domain.Desire{LineID: lineID, DesiredActive: true, UpdatedAt: service.Now().UTC()}); err != nil {
		return domain.State{}, err
	}
	request := supervisorRequest(*line, *egress)
	status, err := service.Supervisor.Start(ctx, request)
	if errors.Is(err, domain.ErrAlreadyRunning) {
		statuses, listErr := service.Supervisor.List(ctx)
		if listErr != nil {
			return domain.State{}, listErr
		}
		for _, current := range statuses {
			if current.LineID == lineID {
				status = current
				err = nil
				break
			}
		}
	}
	if err != nil {
		return stateFor(*line, true, *egress, &status), err
	}
	return stateFor(*line, true, *egress, &status), nil
}

func (service *Service) Deactivate(ctx context.Context, lineID string) (domain.State, error) {
	release, err := service.lockLine(ctx, lineID)
	if err != nil {
		return domain.State{}, err
	}
	defer release()
	environment, err := service.environment(ctx)
	if err != nil {
		return domain.State{}, err
	}
	line := environment.lineByID[lineID]
	if line == nil {
		return domain.State{}, ErrLineNotFound
	}
	if err := service.Store.PutVoWiFiDesire(ctx, domain.Desire{LineID: lineID, DesiredActive: false, UpdatedAt: service.Now().UTC()}); err != nil {
		return domain.State{}, err
	}
	status, err := service.Supervisor.Stop(ctx, lineID)
	runtime := &status
	if errors.Is(err, domain.ErrNotRunning) {
		runtime = nil
	} else if err != nil {
		return domain.State{}, err
	}
	egress := environment.egressByLine[lineID]
	if egress == nil {
		egress = &lineegressapp.View{LineID: lineID, Mode: lineegressdomain.ModeUnconfigured, ReadinessReason: "EGRESS_NOT_CONFIGURED"}
	}
	return stateFor(*line, false, *egress, runtime), nil
}

// Reconcile makes persistent administrator intent match the privileged runtime
// fact. It never changes a Line's identity binding or egress selection.
func (service *Service) lockLine(ctx context.Context, id string) (func(), error) {
	service.mu.Lock()
	if service.gates == nil {
		service.gates = make(map[string]chan struct{})
	}
	gate := service.gates[id]
	if gate == nil {
		gate = make(chan struct{}, 1)
		service.gates[id] = gate
	}
	service.mu.Unlock()
	select {
	case gate <- struct{}{}:
		return func() { <-gate }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (service *Service) reconcileLine(ctx context.Context, lineID string) error {
	release, err := service.lockLine(ctx, lineID)
	if err != nil {
		return err
	}
	defer release()
	environment, err := service.environment(ctx)
	if err != nil {
		return err
	}
	desired := environment.desiredByLine[lineID]
	line, egress, ready := environment.readyLine(lineID)
	if desired && line != nil && hardwareReady(*line) && egress != nil && requiresMihomoRecovery(*egress) {
		if err := service.recoverMihomo(ctx, *egress); err != nil {
			return err
		}
		environment, err = service.environment(ctx)
		if err != nil {
			return err
		}
		line, egress, ready = environment.readyLine(lineID)
	}
	existing := environment.runtimeByLine[lineID]
	if !desired || !ready {
		if existing != nil && existing.State != domain.StateStopped {
			_, err := service.Supervisor.Stop(ctx, lineID)
			if !errors.Is(err, domain.ErrNotRunning) {
				return err
			}
		}
		return nil
	}
	request := supervisorRequest(*line, *egress)
	if existing != nil && existing.State != domain.StateStopped && existing.State != domain.StateFailed {
		if existing.EgressMode == request.EgressMode && existing.CountryCode == request.CountryCode {
			return nil
		}
		if _, err := service.Supervisor.Stop(ctx, lineID); err != nil && !errors.Is(err, domain.ErrNotRunning) {
			return err
		}
	}
	_, err = service.Supervisor.Start(ctx, request)
	if errors.Is(err, domain.ErrAlreadyRunning) {
		return nil
	}
	return err
}

func (service *Service) Reconcile(ctx context.Context) error {
	environment, err := service.environment(ctx)
	if err != nil {
		return err
	}
	ids := map[string]bool{}
	for id := range environment.desiredByLine {
		ids[id] = true
	}
	for id := range environment.runtimeByLine {
		ids[id] = true
	}
	var result error
	// The synchronous entry point is useful for bounded administrative checks.
	// Run schedules each line independently for continuous reconciliation.
	for id := range ids {
		result = errors.Join(result, service.reconcileLine(ctx, id))
	}
	return result
}

func (service *Service) Run(ctx context.Context, interval time.Duration, report func(error)) {
	if interval < time.Second {
		interval = 10 * time.Second
	}
	type completion struct {
		id  string
		err error
	}
	done := make(chan completion)
	var work sync.WaitGroup
	defer work.Wait()
	active := map[string]bool{}
	next := map[string]time.Time{}
	retries := map[string]time.Duration{}
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	schedule := func() {
		cycle, cancel := context.WithTimeout(ctx, 5*time.Second)
		environment, err := service.environment(cycle)
		cancel()
		if err != nil {
			if report != nil {
				report(err)
			}
			return
		}
		ids := map[string]bool{}
		for id := range environment.desiredByLine {
			ids[id] = true
		}
		for id := range environment.runtimeByLine {
			ids[id] = true
		}
		if len(ids) == 0 && report != nil {
			report(nil)
		}
		for id := range ids {
			if active[id] || time.Now().Before(next[id]) {
				continue
			}
			active[id] = true
			work.Go(func() {
				cycle, cancel := context.WithTimeout(ctx, 30*time.Second)
				defer cancel()
				err := service.reconcileLine(cycle, id)
				select {
				case done <- completion{id, err}:
				case <-ctx.Done():
				}
			})
		}
	}
	schedule()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			schedule()
		case result := <-done:
			delete(active, result.id)
			delay := interval
			if result.err != nil {
				delay = retries[result.id] * 2
				if delay < 15*time.Second {
					delay = 15 * time.Second
				}
				if delay > 5*time.Minute {
					delay = 5 * time.Minute
				}
				retries[result.id] = delay
			} else {
				delete(retries, result.id)
			}
			next[result.id] = time.Now().Add(delay)
			if report != nil {
				report(result.err)
			}
		}
	}
}

func (service *Service) list(ctx context.Context) ([]domain.State, error) {
	environment, err := service.environment(ctx)
	if err != nil {
		return nil, err
	}
	states := make([]domain.State, 0, len(environment.lineByID))
	for lineID, line := range environment.lineByID {
		egress := environment.egressByLine[lineID]
		if egress == nil {
			egress = &lineegressapp.View{LineID: lineID, Mode: lineegressdomain.ModeUnconfigured, ReadinessReason: "EGRESS_NOT_CONFIGURED"}
		}
		states = append(states, stateFor(*line, environment.desiredByLine[lineID], *egress, environment.runtimeByLine[lineID]))
	}
	sort.Slice(states, func(left, right int) bool { return states[left].LineID < states[right].LineID })
	return states, nil
}

type runtimeEnvironment struct {
	lineByID      map[string]*inventory.Line
	egressByLine  map[string]*lineegressapp.View
	desiredByLine map[string]bool
	runtimeByLine map[string]*domain.Status
}

func (service *Service) environment(ctx context.Context) (runtimeEnvironment, error) {
	topology, err := service.Inventory.Topology(ctx)
	if err != nil {
		return runtimeEnvironment{}, err
	}
	egresses, err := service.Egress.List(ctx)
	if err != nil {
		return runtimeEnvironment{}, err
	}
	desires, err := service.Store.ListVoWiFiDesires(ctx)
	if err != nil {
		return runtimeEnvironment{}, err
	}
	runtimeStatuses, err := service.Supervisor.List(ctx)
	if err != nil {
		return runtimeEnvironment{}, err
	}
	environment := runtimeEnvironment{
		lineByID: make(map[string]*inventory.Line), egressByLine: make(map[string]*lineegressapp.View),
		desiredByLine: make(map[string]bool), runtimeByLine: make(map[string]*domain.Status),
	}
	for index := range topology.Lines {
		line := &topology.Lines[index]
		environment.lineByID[line.ID] = line
	}
	for index := range egresses {
		value := &egresses[index]
		environment.egressByLine[value.LineID] = value
	}
	for _, desire := range desires {
		environment.desiredByLine[desire.LineID] = desire.DesiredActive
	}
	for index := range runtimeStatuses {
		value := &runtimeStatuses[index]
		environment.runtimeByLine[value.LineID] = value
	}
	return environment, nil
}

func (environment runtimeEnvironment) readyLine(lineID string) (*inventory.Line, *lineegressapp.View, bool) {
	line := environment.lineByID[lineID]
	egress := environment.egressByLine[lineID]
	if line == nil || egress == nil {
		return line, egress, false
	}
	ready := hardwareReady(*line) && egress.Ready
	return line, egress, ready
}

func supervisorRequest(line inventory.Line, egress lineegressapp.View) domain.StartRequest {
	mode := domain.EgressDirect
	country := ""
	if egress.Mode == lineegressdomain.ModeMihomoCountry {
		mode, country = domain.EgressMihomoCountry, egress.CountryCode
	}
	return domain.StartRequest{
		LineID: line.ID, HardwareLineID: line.RuntimeLineID, EgressMode: mode, CountryCode: country,
	}
}

func hardwareReady(line inventory.Line) bool {
	return line.RuntimeLineID != "" && line.State == inventory.LineReady && line.Capabilities.HostVoWiFiAuth
}

func requiresMihomoRecovery(egress lineegressapp.View) bool {
	return egress.Mode == lineegressdomain.ModeMihomoCountry &&
		(egress.ReadinessReason == "MIHOMO_NOT_RUNNING" || egress.ReadinessReason == "MIHOMO_RESTART_REQUIRED")
}

func (service *Service) recoverMihomo(ctx context.Context, egress lineegressapp.View) error {
	if egress.ReadinessReason == "MIHOMO_RESTART_REQUIRED" {
		_, err := service.Mihomo.Restart(ctx)
		return err
	}
	_, err := service.Mihomo.Start(ctx)
	return err
}

func stateFor(line inventory.Line, desired bool, egress lineegressapp.View, runtime *domain.Status) domain.State {
	state := domain.State{
		LineID: line.ID, DesiredActive: desired, EgressMode: egress.Mode,
		CountryCode: egress.CountryCode, CountryName: egress.CountryName,
		Eligible:      hardwareReady(line) && egress.Ready,
		ReadinessCode: egress.ReadinessReason, State: domain.StateStopped,
	}
	if !line.Capabilities.HostVoWiFiAuth {
		state.ReadinessCode = "LINE_VOWIFI_UNSUPPORTED"
	} else if line.State != inventory.LineReady || line.RuntimeLineID == "" {
		state.ReadinessCode = "LINE_HARDWARE_NOT_READY"
	}
	if runtime != nil {
		state.State, state.Stage, state.Online = runtime.State, runtime.Stage, runtime.Online
		state.RegisteredAt, state.NextRefreshAt = runtime.RegisteredAt, runtime.NextRefresh
		state.PhoneNumber = runtime.PhoneNumber
		state.Attempt, state.LastErrorCode = runtime.Attempt, runtime.ErrorCode
	}
	return state
}
