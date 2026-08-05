package resourcelease

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/leonfox28/simplus/internal/application/inventory"
	"github.com/leonfox28/simplus/internal/domain/hardware"
	storage "github.com/leonfox28/simplus/internal/storage/sqlite"
)

var (
	ErrResourceGroupNotFound = errors.New("resource group not found")
	ErrResourceGeneration    = errors.New("resource group generation changed")
	ErrLeaseRequestInvalid   = errors.New("resource lease request is invalid")
	ErrResourceCapability    = errors.New("resource group lacks a required capability")
)

var leasePurposeResources = map[string][]string{
	"cellular-call":        {hardware.ResourceRadioControl, hardware.ResourceSIMAccess, hardware.ResourceVoiceMedia},
	"host-vowifi-call":     {hardware.ResourceSIMAccess, hardware.ResourceHostVoWiFiAuth},
	"network-registration": {hardware.ResourceRadioControl, hardware.ResourceSIMAccess},
	"network-scan":         {hardware.ResourceRadioControl, hardware.ResourceNetworkSelection},
	"operator-selection":   {hardware.ResourceRadioControl, hardware.ResourceSIMAccess, hardware.ResourceNetworkSelection},
	"profile-switch":       {hardware.ResourceSIMAccess, hardware.ResourceEUICCProfiles},
	"sim-auth":             {hardware.ResourceSIMAccess, hardware.ResourceSIMAPDU},
	"sim-unlock":           {hardware.ResourceSIMAccess, hardware.ResourceSIMLock},
	"sms-storage":          {hardware.ResourceSIMAccess, hardware.ResourceSMSStorage},
}

var (
	operationIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)
	purposePattern     = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)
	holderPattern      = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:@/-]{0,127}$`)
)

type TopologyProvider interface {
	Topology(context.Context) (inventory.Topology, error)
}

type Repository interface {
	AcquireResourceGroupLease(context.Context, storage.ResourceLeaseAcquire) (storage.ResourceLease, bool, error)
	RenewResourceGroupLease(context.Context, string, uint64, time.Time, time.Time) (storage.ResourceLease, error)
	ReleaseResourceGroupLease(context.Context, string, uint64) error
	ActiveResourceGroupLeases(context.Context, string, time.Time) ([]storage.ResourceLease, error)
}

type AcquireRequest struct {
	OperationID             string
	ResourceGroupID         string
	ExpectedGroupGeneration uint64
	Kind                    string
	Purpose                 string
	Holder                  string
	TTL                     time.Duration
}

type Service struct {
	topology TopologyProvider
	repo     Repository
	now      func() time.Time
}

func New(topology TopologyProvider, repository Repository) *Service {
	return &Service{topology: topology, repo: repository, now: time.Now}
}

func (service *Service) Acquire(ctx context.Context, request AcquireRequest) (storage.ResourceLease, bool, error) {
	if service == nil || service.topology == nil || service.repo == nil {
		return storage.ResourceLease{}, false, fmt.Errorf("resource lease service is not configured")
	}
	if !operationIDPattern.MatchString(request.OperationID) || !purposePattern.MatchString(request.Purpose) ||
		!holderPattern.MatchString(request.Holder) || request.ExpectedGroupGeneration == 0 ||
		(request.Kind != storage.ResourceLeaseOperation && request.Kind != storage.ResourceLeaseCall) ||
		request.TTL < 5*time.Second || request.TTL > 10*time.Minute {
		return storage.ResourceLease{}, false, ErrLeaseRequestInvalid
	}
	requiredResources, knownPurpose := leasePurposeResources[request.Purpose]
	isCallPurpose := request.Purpose == "cellular-call" || request.Purpose == "host-vowifi-call"
	if !knownPurpose || (request.Kind == storage.ResourceLeaseCall) != isCallPurpose {
		return storage.ResourceLease{}, false, ErrLeaseRequestInvalid
	}
	group, err := service.currentGroup(ctx, request.ResourceGroupID, request.ExpectedGroupGeneration)
	if err != nil {
		return storage.ResourceLease{}, false, err
	}
	for _, required := range requiredResources {
		if !contains(group.Resources, required) {
			return storage.ResourceLease{}, false, ErrResourceCapability
		}
	}
	leaseID, err := randomLeaseID()
	if err != nil {
		return storage.ResourceLease{}, false, fmt.Errorf("generate resource lease id: %w", err)
	}
	now := service.currentTime()
	return service.repo.AcquireResourceGroupLease(ctx, storage.ResourceLeaseAcquire{
		LeaseID: leaseID, OperationID: request.OperationID, ResourceGroupID: request.ResourceGroupID, Kind: request.Kind,
		Purpose: request.Purpose, Holder: request.Holder, ResourceGroupGeneration: request.ExpectedGroupGeneration,
		MaxActiveCalls: group.MaxActiveCalls, MaxConcurrentOperations: group.MaxConcurrentOps,
		Now: now, ExpiresAt: now.Add(request.TTL),
	})
}

func (service *Service) Renew(ctx context.Context, lease storage.ResourceLease, ttl time.Duration) (storage.ResourceLease, error) {
	if service == nil || service.topology == nil || service.repo == nil {
		return storage.ResourceLease{}, fmt.Errorf("resource lease service is not configured")
	}
	if lease.LeaseID == "" || lease.FencingToken == 0 || lease.ResourceGroupGeneration == 0 || ttl < 5*time.Second || ttl > 10*time.Minute {
		return storage.ResourceLease{}, ErrLeaseRequestInvalid
	}
	if _, err := service.currentGroup(ctx, lease.ResourceGroupID, lease.ResourceGroupGeneration); err != nil {
		return storage.ResourceLease{}, err
	}
	now := service.currentTime()
	return service.repo.RenewResourceGroupLease(ctx, lease.LeaseID, lease.FencingToken, now, now.Add(ttl))
}

func (service *Service) Release(ctx context.Context, lease storage.ResourceLease) error {
	if service == nil || service.repo == nil {
		return fmt.Errorf("resource lease service is not configured")
	}
	if lease.LeaseID == "" || lease.FencingToken == 0 {
		return ErrLeaseRequestInvalid
	}
	return service.repo.ReleaseResourceGroupLease(ctx, lease.LeaseID, lease.FencingToken)
}

func (service *Service) Active(ctx context.Context, groupID string) ([]storage.ResourceLease, error) {
	if service == nil || service.topology == nil || service.repo == nil {
		return nil, fmt.Errorf("resource lease service is not configured")
	}
	group, err := service.currentGroup(ctx, groupID, 0)
	if err != nil {
		return nil, err
	}
	leases, err := service.repo.ActiveResourceGroupLeases(ctx, groupID, service.currentTime())
	if err != nil {
		return nil, err
	}
	active := leases[:0]
	for _, lease := range leases {
		if lease.ResourceGroupGeneration == group.Generation {
			active = append(active, lease)
		}
	}
	return active, nil
}

func (service *Service) currentGroup(ctx context.Context, groupID string, expectedGeneration uint64) (inventoryResourceGroup, error) {
	if !purposePattern.MatchString(groupID) {
		return inventoryResourceGroup{}, ErrLeaseRequestInvalid
	}
	topology, err := service.topology.Topology(ctx)
	if err != nil {
		return inventoryResourceGroup{}, fmt.Errorf("read topology for resource lease: %w", err)
	}
	for _, candidate := range topology.ResourceGroups {
		if candidate.ID != groupID {
			continue
		}
		if expectedGeneration != 0 && candidate.Generation != expectedGeneration {
			return inventoryResourceGroup{}, ErrResourceGeneration
		}
		return inventoryResourceGroup{
			Generation: candidate.Generation, Resources: append([]string(nil), candidate.Resources...),
			MaxActiveCalls: candidate.MaxActiveCalls, MaxConcurrentOps: candidate.MaxConcurrentOps,
		}, nil
	}
	return inventoryResourceGroup{}, ErrResourceGroupNotFound
}

func (service *Service) currentTime() time.Time {
	if service.now == nil {
		return time.Now().UTC()
	}
	return service.now().UTC()
}

type inventoryResourceGroup struct {
	Generation       uint64
	Resources        []string
	MaxActiveCalls   int
	MaxConcurrentOps int
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func randomLeaseID() (string, error) {
	value := make([]byte, 24)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}
