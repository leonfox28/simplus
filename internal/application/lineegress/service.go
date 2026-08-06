package lineegress

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"time"

	"github.com/leonfox28/simplus/internal/application/inventory"
	mihomoapp "github.com/leonfox28/simplus/internal/application/mihomo"
	"github.com/leonfox28/simplus/internal/domain/accessmode"
	domain "github.com/leonfox28/simplus/internal/domain/lineegress"
	mihomodomain "github.com/leonfox28/simplus/internal/domain/mihomo"
)

var (
	ErrInvalidBinding = errors.New("line egress binding is invalid")
	ErrLineNotFound   = errors.New("line was not found")
	ErrLineMode       = errors.New("line is not configured for Host VoWiFi")
	lineIDPattern     = regexp.MustCompile(`^line_[A-Za-z0-9_-]{22}$`)
	countryPattern    = regexp.MustCompile(`^[A-Z]{2}$`)
)

type Store interface {
	ListLineEgressBindings(context.Context) ([]domain.Binding, error)
	UpsertLineEgressBinding(context.Context, domain.Binding) error
	ReadMihomoRuntimeSelection(context.Context) (string, string, error)
	ListMihomoSubscriptionNodes(context.Context, string) ([]mihomodomain.Node, error)
}

type Inventory interface {
	Topology(context.Context) (inventory.Topology, error)
}

type Runtime interface {
	Status(context.Context) (mihomoapp.RuntimeStatus, error)
}

type View struct {
	LineID, Mode, CountryCode, CountryName, ReadinessReason string
	ListenerPort                                            int
	Ready                                                   bool
}

type Service struct {
	Store     Store
	Inventory Inventory
	Runtime   Runtime
	Now       func() time.Time
}

func New(store Store, inventoryService Inventory, runtime Runtime) *Service {
	return &Service{Store: store, Inventory: inventoryService, Runtime: runtime, Now: time.Now}
}

func (service *Service) List(ctx context.Context) ([]View, error) {
	if err := service.configured(); err != nil {
		return nil, err
	}
	topology, err := service.Inventory.Topology(ctx)
	if err != nil {
		return nil, err
	}
	stored, err := service.Store.ListLineEgressBindings(ctx)
	if err != nil {
		return nil, err
	}
	byLine := make(map[string]domain.Binding, len(stored))
	for _, binding := range stored {
		byLine[binding.LineID] = binding
	}
	environment, err := service.environment(ctx)
	if err != nil {
		return nil, err
	}
	views := make([]View, 0, len(topology.Lines))
	for _, line := range topology.Lines {
		binding, found := byLine[line.ID]
		if !found {
			binding = domain.Binding{LineID: line.ID, Mode: domain.ModeDirect}
		}
		views = append(views, view(line, binding, environment))
	}
	sort.Slice(views, func(left, right int) bool { return views[left].LineID < views[right].LineID })
	return views, nil
}

func (service *Service) Put(ctx context.Context, lineID, mode, countryCode string) (View, error) {
	if err := service.configured(); err != nil {
		return View{}, err
	}
	if !validBinding(lineID, mode, countryCode) {
		return View{}, ErrInvalidBinding
	}
	topology, err := service.Inventory.Topology(ctx)
	if err != nil {
		return View{}, err
	}
	var selected inventory.Line
	found := false
	for _, line := range topology.Lines {
		if line.ID == lineID {
			selected = line
			found = true
			break
		}
	}
	if !found {
		return View{}, ErrLineNotFound
	}
	if selected.AccessMode != accessmode.HostVoWiFiOnly {
		return View{}, ErrLineMode
	}
	environment, err := service.environment(ctx)
	if err != nil {
		return View{}, err
	}
	if mode == domain.ModeMihomoCountry && environment.countryNames[countryCode] == "" {
		return View{}, ErrInvalidBinding
	}
	binding := domain.Binding{LineID: lineID, Mode: mode, CountryCode: countryCode, UpdatedAt: service.Now().UTC()}
	if err := service.Store.UpsertLineEgressBinding(ctx, binding); err != nil {
		return View{}, fmt.Errorf("persist line egress binding: %w", err)
	}
	return view(selected, binding, environment), nil
}

type runtimeEnvironment struct {
	selectedSubscriptionID string
	runningSubscriptionID  string
	runtimeState           string
	countryNames           map[string]string
}

func (service *Service) environment(ctx context.Context) (runtimeEnvironment, error) {
	selected, running, err := service.Store.ReadMihomoRuntimeSelection(ctx)
	if err != nil {
		return runtimeEnvironment{}, err
	}
	environment := runtimeEnvironment{selectedSubscriptionID: selected, runningSubscriptionID: running, countryNames: map[string]string{}}
	if selected != "" {
		nodes, err := service.Store.ListMihomoSubscriptionNodes(ctx, selected)
		if err != nil {
			return runtimeEnvironment{}, err
		}
		for _, node := range nodes {
			if countryPattern.MatchString(node.CountryCode) && node.CountryName != "" {
				environment.countryNames[node.CountryCode] = node.CountryName
			}
		}
	}
	status, err := service.Runtime.Status(ctx)
	if err != nil {
		return runtimeEnvironment{}, err
	}
	environment.runtimeState = status.State
	return environment, nil
}

func view(line inventory.Line, binding domain.Binding, environment runtimeEnvironment) View {
	result := View{LineID: line.ID, Mode: binding.Mode, CountryCode: binding.CountryCode}
	if line.AccessMode != accessmode.HostVoWiFiOnly {
		result.ReadinessReason = "LINE_NOT_HOST_VOWIFI"
		return result
	}
	if binding.Mode == domain.ModeDirect {
		result.Ready = true
		result.ReadinessReason = "READY"
		return result
	}
	result.CountryName = environment.countryNames[binding.CountryCode]
	result.ListenerPort = mihomoapp.CountryListenerPort(binding.CountryCode)
	switch {
	case environment.selectedSubscriptionID == "":
		result.ReadinessReason = "SUBSCRIPTION_NOT_SELECTED"
	case result.CountryName == "":
		result.ReadinessReason = "COUNTRY_NOT_FOUND"
	case environment.runtimeState != "running":
		result.ReadinessReason = "MIHOMO_NOT_RUNNING"
	case environment.runningSubscriptionID != environment.selectedSubscriptionID:
		result.ReadinessReason = "MIHOMO_RESTART_REQUIRED"
	default:
		result.Ready = true
		result.ReadinessReason = "READY"
	}
	return result
}

func validBinding(lineID, mode, countryCode string) bool {
	if !lineIDPattern.MatchString(lineID) {
		return false
	}
	return mode == domain.ModeDirect && countryCode == "" || mode == domain.ModeMihomoCountry && countryPattern.MatchString(countryCode)
}

func (service *Service) configured() error {
	if service == nil || service.Store == nil || service.Inventory == nil || service.Runtime == nil || service.Now == nil {
		return errors.New("line egress service is not configured")
	}
	return nil
}
