package mihomo

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	domain "github.com/leonfox28/simplus/internal/domain/mihomo"
)

var egressIDPattern = regexp.MustCompile(`^egress_[A-Za-z0-9_-]{22}$`)
var ErrEgressProfileInvalid = errors.New("Mihomo egress profile request is invalid")
var ErrEgressProfileNotFound = errors.New("Mihomo egress profile not found")

type EgressStore interface {
	ListMihomoEgressProfiles(context.Context) ([]domain.EgressProfile, error)
	UpsertMihomoEgressProfile(context.Context, domain.EgressProfile) error
	DeleteMihomoEgressProfile(context.Context, string) (bool, error)
	ReadMihomoSubscription(context.Context, string) (domain.Subscription, bool, error)
	ListMihomoSubscriptionNodes(context.Context, string) ([]domain.Node, error)
}
type CoreStatusReader interface{ Status() (CoreStatus, error) }
type EgressProfileView struct {
	ID, DisplayName, SubscriptionID, LineID, SelectionType, SelectedNodeID, SelectedNodeName, SelectedCountryCode, SelectedCountryName, SourceCIDR string
	Enabled, Ready                                                                                                                                 bool
	ReadinessReason                                                                                                                                string
}
type EgressService struct {
	Store EgressStore
	Core  CoreStatusReader
	Now   func() time.Time
}

func NewEgressService(store EgressStore, core CoreStatusReader) *EgressService {
	return &EgressService{Store: store, Core: core, Now: time.Now}
}

func (service *EgressService) List(ctx context.Context) ([]EgressProfileView, error) {
	items, err := service.Store.ListMihomoEgressProfiles(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]EgressProfileView, 0, len(items))
	for _, item := range items {
		if item.SelectionType == "" {
			item.SelectionType = "node"
		}
		if item.SourceCIDR == "" {
			item.SourceCIDR = sourceCIDRForProfile(item.ID)
			if err := service.Store.UpsertMihomoEgressProfile(ctx, item); err != nil {
				return nil, err
			}
		}
		view, err := service.view(ctx, item)
		if err != nil {
			return nil, err
		}
		result = append(result, view)
	}
	return result, nil
}
func (service *EgressService) Create(ctx context.Context, name, subscriptionID, lineID, selectionType, nodeID, countryCode string, enabled bool) (EgressProfileView, error) {
	if err := validateEgressInput(name, subscriptionID, lineID, selectionType, nodeID, countryCode); err != nil {
		return EgressProfileView{}, err
	}
	id, err := newEgressID()
	if err != nil {
		return EgressProfileView{}, err
	}
	now := service.Now().UTC()
	item := domain.EgressProfile{ID: id, DisplayName: strings.TrimSpace(name), SubscriptionID: subscriptionID, LineID: strings.TrimSpace(lineID), SelectionType: selectionType, SelectedNodeID: nodeID, SelectedCountryCode: countryCode, SourceCIDR: sourceCIDRForProfile(id), Enabled: enabled, CreatedAt: now, UpdatedAt: now}
	if available, err := service.lineAvailable(ctx, item.LineID, ""); err != nil || !available {
		if err != nil {
			return EgressProfileView{}, err
		}
		return EgressProfileView{}, ErrEgressProfileInvalid
	}
	if _, err := service.view(ctx, item); err != nil {
		return EgressProfileView{}, err
	}
	if exists, err := service.selectionExists(ctx, item); err != nil || !exists {
		if err != nil {
			return EgressProfileView{}, err
		}
		return EgressProfileView{}, ErrEgressProfileInvalid
	}
	if err := service.Store.UpsertMihomoEgressProfile(ctx, item); err != nil {
		return EgressProfileView{}, err
	}
	return service.view(ctx, item)
}
func (service *EgressService) Update(ctx context.Context, id, name, subscriptionID, lineID, selectionType, nodeID, countryCode string, enabled bool) (EgressProfileView, error) {
	if !egressIDPattern.MatchString(id) || validateEgressInput(name, subscriptionID, lineID, selectionType, nodeID, countryCode) != nil {
		return EgressProfileView{}, ErrEgressProfileInvalid
	}
	items, err := service.Store.ListMihomoEgressProfiles(ctx)
	if err != nil {
		return EgressProfileView{}, err
	}
	var item domain.EgressProfile
	found := false
	for _, candidate := range items {
		if candidate.ID == id {
			item = candidate
			found = true
			break
		}
	}
	if !found {
		return EgressProfileView{}, ErrEgressProfileNotFound
	}
	item.DisplayName, item.SubscriptionID, item.LineID, item.SelectionType, item.SelectedNodeID, item.SelectedCountryCode, item.Enabled, item.UpdatedAt = strings.TrimSpace(name), subscriptionID, strings.TrimSpace(lineID), selectionType, nodeID, countryCode, enabled, service.Now().UTC()
	if available, err := service.lineAvailable(ctx, item.LineID, item.ID); err != nil || !available {
		if err != nil {
			return EgressProfileView{}, err
		}
		return EgressProfileView{}, ErrEgressProfileInvalid
	}
	if item.SourceCIDR == "" {
		item.SourceCIDR = sourceCIDRForProfile(item.ID)
	}
	if _, err := service.view(ctx, item); err != nil {
		return EgressProfileView{}, err
	}
	if exists, err := service.selectionExists(ctx, item); err != nil || !exists {
		if err != nil {
			return EgressProfileView{}, err
		}
		return EgressProfileView{}, ErrEgressProfileInvalid
	}
	if err := service.Store.UpsertMihomoEgressProfile(ctx, item); err != nil {
		return EgressProfileView{}, err
	}
	return service.view(ctx, item)
}
func (service *EgressService) Delete(ctx context.Context, id string) error {
	if !egressIDPattern.MatchString(id) {
		return ErrEgressProfileInvalid
	}
	deleted, err := service.Store.DeleteMihomoEgressProfile(ctx, id)
	if err != nil {
		return err
	}
	if !deleted {
		return ErrEgressProfileNotFound
	}
	return nil
}
func (service *EgressService) view(ctx context.Context, item domain.EgressProfile) (EgressProfileView, error) {
	subscription, found, err := service.Store.ReadMihomoSubscription(ctx, item.SubscriptionID)
	if err != nil {
		return EgressProfileView{}, err
	}
	if !found {
		return EgressProfileView{}, ErrSubscriptionNotFound
	}
	nodes, err := service.Store.ListMihomoSubscriptionNodes(ctx, item.SubscriptionID)
	if err != nil {
		return EgressProfileView{}, err
	}
	nodeName, countryName := "", ""
	for _, node := range nodes {
		if node.ID == item.SelectedNodeID {
			nodeName = node.DisplayName
			break
		}
		if node.CountryCode == item.SelectedCountryCode && countryName == "" {
			countryName = node.CountryName
		}
	}
	view := EgressProfileView{ID: item.ID, DisplayName: item.DisplayName, SubscriptionID: item.SubscriptionID, LineID: item.LineID, SelectionType: item.SelectionType, SelectedNodeID: item.SelectedNodeID, SelectedNodeName: nodeName, SelectedCountryCode: item.SelectedCountryCode, SelectedCountryName: countryName, SourceCIDR: item.SourceCIDR, Enabled: item.Enabled}
	status, err := service.Core.Status()
	if err != nil {
		return view, err
	}
	switch {
	case !item.Enabled:
		view.ReadinessReason = "PROFILE_DISABLED"
	case !status.Installed:
		view.ReadinessReason = "CORE_NOT_INSTALLED"
	case !subscription.Enabled:
		view.ReadinessReason = "SUBSCRIPTION_DISABLED"
	case subscription.LastRefreshStatus != "success":
		view.ReadinessReason = "SUBSCRIPTION_NOT_READY"
	case item.SelectionType == "node" && nodeName == "":
		view.ReadinessReason = "NODE_NOT_FOUND"
	case item.SelectionType == "country" && countryName == "":
		view.ReadinessReason = "COUNTRY_NOT_FOUND"
	default:
		view.Ready = true
		view.ReadinessReason = "READY"
	}
	return view, nil
}

func (service *EgressService) selectionExists(ctx context.Context, item domain.EgressProfile) (bool, error) {
	nodes, err := service.Store.ListMihomoSubscriptionNodes(ctx, item.SubscriptionID)
	if err != nil {
		return false, err
	}
	for _, node := range nodes {
		if item.SelectionType == "node" && node.ID == item.SelectedNodeID && node.ProxyYAML != "" {
			return true, nil
		}
		if item.SelectionType == "country" && node.CountryCode == item.SelectedCountryCode && node.ProxyYAML != "" {
			return true, nil
		}
	}
	return false, nil
}

func (service *EgressService) lineAvailable(ctx context.Context, lineID, exceptID string) (bool, error) {
	profiles, err := service.Store.ListMihomoEgressProfiles(ctx)
	if err != nil {
		return false, err
	}
	for _, profile := range profiles {
		if profile.ID != exceptID && profile.LineID == lineID {
			return false, nil
		}
	}
	return true, nil
}
func validateEgressInput(name, subscriptionID, lineID, selectionType, nodeID, countryCode string) error {
	name = strings.TrimSpace(name)
	lineID = strings.TrimSpace(lineID)
	validSelection := selectionType == "node" && regexp.MustCompile(`^node_[A-Za-z0-9_-]{22}$`).MatchString(nodeID) && countryCode == "" || selectionType == "country" && nodeID == "" && regexp.MustCompile(`^[A-Z]{2}$`).MatchString(countryCode)
	if name == "" || len([]rune(name)) > 80 || lineID == "" || len(lineID) > 160 || strings.ContainsAny(lineID, "\r\n\x00") || !subscriptionIDPattern.MatchString(subscriptionID) || !validSelection {
		return ErrEgressProfileInvalid
	}
	return nil
}

func sourceCIDRForProfile(id string) string {
	digest := sha256.Sum256([]byte(id))
	block := binary.BigEndian.Uint16(digest[:2])%16383 + 1
	address := uint32(block) * 4
	return fmt.Sprintf("169.254.%d.%d/30", address>>8, address&0xff)
}
func newEgressID() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return "egress_" + base64.RawURLEncoding.EncodeToString(raw), nil
}
