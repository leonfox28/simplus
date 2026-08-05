package mihomo

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/leonfox28/simplus/internal/domain/lineegress"
	domain "github.com/leonfox28/simplus/internal/domain/mihomo"
	"gopkg.in/yaml.v3"
)

var (
	ErrConfigNotReady         = errors.New("Mihomo configuration is not ready")
	ErrConfigValidationFailed = errors.New("Mihomo configuration validation failed")
)

type ConfigStore interface {
	ReadMihomoSubscription(context.Context, string) (domain.Subscription, bool, error)
	ListMihomoSubscriptionNodes(context.Context, string) ([]domain.Node, error)
	ReadMihomoRuntimeSelection(context.Context) (string, string, error)
	WriteMihomoSelectedSubscription(context.Context, string, time.Time) error
}

type CountrySummary struct {
	Code, Name string
	NodeCount  int
}

type ArtifactMetadata struct {
	SubscriptionID string           `json:"subscriptionId"`
	Version        string           `json:"version"`
	RawSHA256      string           `json:"rawSha256"`
	ConfigSHA256   string           `json:"configSha256"`
	CoreVersion    string           `json:"coreVersion"`
	GeneratedAt    time.Time        `json:"generatedAt"`
	Countries      []CountrySummary `json:"countries"`
}

type ConfigStatus struct {
	Published              bool      `json:"published"`
	Launchable             bool      `json:"launchable"`
	SHA256                 string    `json:"sha256"`
	GeneratedAt            time.Time `json:"generatedAt"`
	ErrorCode              string    `json:"errorCode"`
	SelectedSubscriptionID string    `json:"selectedSubscriptionId"`
	RunningSubscriptionID  string    `json:"runningSubscriptionId"`
}

type ConfigManager struct {
	Root              string
	Store             ConfigStore
	Core              CoreStatusReader
	Run               CommandRunner
	Now               func() time.Time
	ControllerAddress string
	ControllerSecret  string
	ExternalUI        string
}

func NewConfigManager(root string, store ConfigStore, core CoreStatusReader) *ConfigManager {
	return &ConfigManager{Root: root, Store: store, Core: core, Run: runCommand, Now: time.Now, ControllerAddress: "127.0.0.1:19090"}
}

func (manager *ConfigManager) ConfigureDashboard(status DashboardStatus) {
	manager.ControllerAddress = status.ControllerAddress
	manager.ControllerSecret = status.Secret
	manager.ExternalUI = "ui"
}

// BuildSubscription creates an immutable raw/generated artifact version. The
// subscription's current pointer is changed only after the installed core has
// accepted the generated configuration.
func (manager *ConfigManager) BuildSubscription(ctx context.Context, subscriptionID string, raw []byte, nodes []domain.Node) (ArtifactMetadata, error) {
	if err := manager.configured(); err != nil || !subscriptionIDPattern.MatchString(subscriptionID) || len(raw) == 0 || len(raw) > 5<<20 {
		return ArtifactMetadata{}, ErrConfigNotReady
	}
	content, countries, err := generateSubscriptionConfig(nodes, manager.ControllerAddress, manager.ControllerSecret, manager.ExternalUI)
	if err != nil {
		return ArtifactMetadata{}, err
	}
	core, err := manager.Core.Status()
	if err != nil || !core.Installed || core.BinaryPath == "" {
		return ArtifactMetadata{}, ErrConfigNotReady
	}
	rawDigest, configDigest := sha256.Sum256(raw), sha256.Sum256(content)
	versionDigest := sha256.Sum256(append(rawDigest[:], configDigest[:]...))
	version := hex.EncodeToString(versionDigest[:16])
	metadata := ArtifactMetadata{SubscriptionID: subscriptionID, Version: version, RawSHA256: hex.EncodeToString(rawDigest[:]), ConfigSHA256: hex.EncodeToString(configDigest[:]), CoreVersion: core.Version, GeneratedAt: manager.Now().UTC(), Countries: countries}
	base := filepath.Join(manager.Root, "subscriptions", subscriptionID)
	staging, err := os.MkdirTemp(filepath.Join(manager.Root, "subscriptions"), ".staging-")
	if os.IsNotExist(err) {
		if mkdirErr := os.MkdirAll(filepath.Join(manager.Root, "subscriptions"), 0o700); mkdirErr != nil {
			return ArtifactMetadata{}, mkdirErr
		}
		staging, err = os.MkdirTemp(filepath.Join(manager.Root, "subscriptions"), ".staging-")
	}
	if err != nil {
		return ArtifactMetadata{}, err
	}
	defer os.RemoveAll(staging)
	if err := writePrivateFile(filepath.Join(staging, "raw.yaml"), raw); err != nil {
		return ArtifactMetadata{}, err
	}
	if err := writePrivateFile(filepath.Join(staging, "generated.yaml"), content); err != nil {
		return ArtifactMetadata{}, err
	}
	metaBody, err := json.Marshal(metadata)
	if err != nil {
		return ArtifactMetadata{}, err
	}
	if err := writePrivateFile(filepath.Join(staging, "metadata.json"), metaBody); err != nil {
		return ArtifactMetadata{}, err
	}
	runtimeDir := filepath.Join(manager.Root, "config", "runtime")
	if err := os.MkdirAll(runtimeDir, 0o700); err != nil {
		return ArtifactMetadata{}, err
	}
	if err := manager.validate(ctx, core.BinaryPath, filepath.Join(staging, "generated.yaml"), runtimeDir); err != nil {
		return ArtifactMetadata{}, err
	}
	versionDir := filepath.Join(base, "versions", version)
	if err := os.MkdirAll(filepath.Dir(versionDir), 0o700); err != nil {
		return ArtifactMetadata{}, err
	}
	if _, err := os.Stat(versionDir); os.IsNotExist(err) {
		if err := os.Rename(staging, versionDir); err != nil {
			return ArtifactMetadata{}, err
		}
	} else if err != nil {
		return ArtifactMetadata{}, err
	} else if !artifactFilesMatch(versionDir, metadata) {
		return ArtifactMetadata{}, ErrConfigNotReady
	}
	pointerBody, _ := json.Marshal(metadata)
	if err := writeAtomicPrivateFile(filepath.Join(base, "current.json"), pointerBody); err != nil {
		return ArtifactMetadata{}, err
	}
	return metadata, nil
}

// Select validates an existing immutable artifact with the currently installed
// core and records it for the next start/restart. It never manages a process.
func (manager *ConfigManager) Select(ctx context.Context, subscriptionID string) (ConfigStatus, error) {
	if err := manager.configured(); err != nil || !subscriptionIDPattern.MatchString(subscriptionID) {
		return ConfigStatus{}, ErrConfigNotReady
	}
	subscription, found, err := manager.Store.ReadMihomoSubscription(ctx, subscriptionID)
	if err != nil {
		return ConfigStatus{}, err
	}
	if !found || !subscription.Enabled {
		return ConfigStatus{}, ErrConfigNotReady
	}
	metadata, configPath, err := manager.artifact(subscriptionID)
	if err != nil {
		return ConfigStatus{}, err
	}
	core, err := manager.Core.Status()
	if err != nil || !core.Installed {
		return ConfigStatus{}, ErrConfigNotReady
	}
	if err := manager.validate(ctx, core.BinaryPath, configPath, filepath.Join(manager.Root, "config", "runtime")); err != nil {
		return ConfigStatus{}, err
	}
	if err := manager.Store.WriteMihomoSelectedSubscription(ctx, subscriptionID, manager.Now().UTC()); err != nil {
		return ConfigStatus{}, err
	}
	_, running, err := manager.Store.ReadMihomoRuntimeSelection(ctx)
	if err != nil {
		return ConfigStatus{}, err
	}
	return ConfigStatus{Published: true, Launchable: true, SHA256: metadata.ConfigSHA256, GeneratedAt: metadata.GeneratedAt, SelectedSubscriptionID: subscriptionID, RunningSubscriptionID: running}, nil
}

func (manager *ConfigManager) Status(ctx context.Context) (ConfigStatus, error) {
	if err := manager.configured(); err != nil {
		return ConfigStatus{}, err
	}
	selected, running, err := manager.Store.ReadMihomoRuntimeSelection(ctx)
	if err != nil {
		return ConfigStatus{}, err
	}
	status := ConfigStatus{SelectedSubscriptionID: selected, RunningSubscriptionID: running}
	if selected == "" {
		return status, nil
	}
	metadata, configPath, err := manager.artifact(selected)
	if err != nil {
		status.ErrorCode = "SELECTED_CONFIG_MISSING"
		return status, nil
	}
	status.Published, status.SHA256, status.GeneratedAt = true, metadata.ConfigSHA256, metadata.GeneratedAt
	core, err := manager.Core.Status()
	if err != nil || !core.Installed {
		status.ErrorCode = "CORE_NOT_INSTALLED"
		return status, nil
	}
	if err := manager.validate(ctx, core.BinaryPath, configPath, filepath.Join(manager.Root, "config", "runtime")); err != nil {
		status.ErrorCode = "SELECTED_CONFIG_VALIDATION_FAILED"
		return status, nil
	}
	status.Launchable = true
	return status, nil
}

// GenerateAndPublish is retained for the existing API button. Artifacts are
// generated during subscription refresh, so this operation only revalidates
// the selected artifact and never starts or reloads Mihomo.
func (manager *ConfigManager) GenerateAndPublish(ctx context.Context) (ConfigStatus, error) {
	selected, _, err := manager.Store.ReadMihomoRuntimeSelection(ctx)
	if err != nil {
		return ConfigStatus{}, err
	}
	if selected == "" {
		return ConfigStatus{}, ErrConfigNotReady
	}
	return manager.Select(ctx, selected)
}

func (manager *ConfigManager) Artifact(subscriptionID string) (ArtifactMetadata, string, error) {
	return manager.artifact(subscriptionID)
}
func (manager *ConfigManager) DeleteSubscriptionArtifacts(subscriptionID string) error {
	if manager == nil || !subscriptionIDPattern.MatchString(subscriptionID) || !filepath.IsAbs(manager.Root) {
		return ErrConfigNotReady
	}
	return os.RemoveAll(filepath.Join(manager.Root, "subscriptions", subscriptionID))
}

func artifactFilesMatch(versionDir string, metadata ArtifactMetadata) bool {
	raw, rawErr := os.ReadFile(filepath.Join(versionDir, "raw.yaml"))
	config, configErr := os.ReadFile(filepath.Join(versionDir, "generated.yaml"))
	if rawErr != nil || configErr != nil {
		return false
	}
	rawDigest, configDigest := sha256.Sum256(raw), sha256.Sum256(config)
	return hex.EncodeToString(rawDigest[:]) == metadata.RawSHA256 && hex.EncodeToString(configDigest[:]) == metadata.ConfigSHA256
}

func (manager *ConfigManager) artifact(subscriptionID string) (ArtifactMetadata, string, error) {
	if !subscriptionIDPattern.MatchString(subscriptionID) {
		return ArtifactMetadata{}, "", ErrConfigNotReady
	}
	base := filepath.Join(manager.Root, "subscriptions", subscriptionID)
	body, err := os.ReadFile(filepath.Join(base, "current.json"))
	if err != nil {
		return ArtifactMetadata{}, "", ErrConfigNotReady
	}
	var metadata ArtifactMetadata
	if json.Unmarshal(body, &metadata) != nil || metadata.SubscriptionID != subscriptionID || len(metadata.Version) != 32 || len(metadata.RawSHA256) != 64 || len(metadata.ConfigSHA256) != 64 {
		return ArtifactMetadata{}, "", ErrConfigNotReady
	}
	versionDir := filepath.Join(base, "versions", metadata.Version)
	raw, err := os.ReadFile(filepath.Join(versionDir, "raw.yaml"))
	if err != nil {
		return ArtifactMetadata{}, "", ErrConfigNotReady
	}
	configPath := filepath.Join(versionDir, "generated.yaml")
	config, err := os.ReadFile(configPath)
	if err != nil {
		return ArtifactMetadata{}, "", ErrConfigNotReady
	}
	rawDigest, configDigest := sha256.Sum256(raw), sha256.Sum256(config)
	if hex.EncodeToString(rawDigest[:]) != metadata.RawSHA256 || hex.EncodeToString(configDigest[:]) != metadata.ConfigSHA256 {
		return ArtifactMetadata{}, "", ErrConfigNotReady
	}
	return metadata, configPath, nil
}

func (manager *ConfigManager) validate(ctx context.Context, binary, config, runtimeDir string) error {
	probeCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if _, err := manager.Run(probeCtx, binary, "-t", "-f", config, "-d", runtimeDir); err != nil {
		return fmt.Errorf("%w: %v", ErrConfigValidationFailed, err)
	}
	return nil
}

func (manager *ConfigManager) configured() error {
	if manager == nil || manager.Store == nil || manager.Core == nil || manager.Run == nil || manager.Now == nil || !filepath.IsAbs(manager.Root) {
		return ErrConfigNotReady
	}
	return nil
}

func generateSubscriptionConfig(nodes []domain.Node, controllerAddress, controllerSecret, externalUI string) ([]byte, []CountrySummary, error) {
	byCountry := map[string][]domain.Node{}
	countryNames := map[string]string{}
	for _, node := range nodes {
		if node.ProxyYAML == "" || len(node.CountryCode) != 2 || node.CountryName == "" {
			continue
		}
		byCountry[node.CountryCode] = append(byCountry[node.CountryCode], node)
		countryNames[node.CountryCode] = node.CountryName
	}
	codes := make([]string, 0, len(byCountry))
	for code := range byCountry {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	if len(codes) == 0 {
		return nil, nil, ErrConfigNotReady
	}
	proxies := make([]map[string]any, 0, len(nodes))
	groups := make([]map[string]any, 0, len(codes)+1)
	listeners := make([]map[string]any, 0, len(codes))
	countries := make([]CountrySummary, 0, len(codes))
	allProxyNames := make([]string, 0, len(nodes))
	seenProxyNames := make(map[string]struct{}, len(nodes))
	for _, code := range codes {
		countryNodes := byCountry[code]
		sort.Slice(countryNodes, func(i, j int) bool { return countryNodes[i].ID < countryNodes[j].ID })
		proxyNames := make([]string, 0, len(countryNodes))
		for _, node := range countryNodes {
			var proxy map[string]any
			if yaml.Unmarshal([]byte(node.ProxyYAML), &proxy) != nil {
				return nil, nil, ErrConfigNotReady
			}
			name, ok := proxy["name"].(string)
			if !ok || name != node.DisplayName {
				return nil, nil, ErrConfigNotReady
			}
			if _, duplicate := seenProxyNames[name]; duplicate {
				return nil, nil, ErrConfigNotReady
			}
			seenProxyNames[name] = struct{}{}
			proxies = append(proxies, proxy)
			proxyNames = append(proxyNames, name)
			allProxyNames = append(allProxyNames, name)
		}
		groupName := countryFlag(code) + " " + countryNames[code]
		groupType := "select"
		if len(proxyNames) > 1 {
			groupType = "url-test"
		}
		group := map[string]any{"name": groupName, "type": groupType, "proxies": proxyNames}
		if groupType == "url-test" {
			group["url"] = "https://cp.cloudflare.com/generate_204"
			group["interval"] = 300
		}
		groups = append(groups, group)
		listeners = append(listeners, map[string]any{"name": "country-" + strings.ToLower(code), "type": "tproxy", "listen": "127.0.0.1", "port": countryPort(code), "udp": true, "proxy": groupName})
		countries = append(countries, CountrySummary{Code: code, Name: countryNames[code], NodeCount: len(proxyNames)})
	}
	groups = append([]map[string]any{{
		"name": "🌐 DNS", "type": "url-test", "proxies": allProxyNames,
		"url": "https://cp.cloudflare.com/generate_204", "interval": 300, "timeout": 5000, "tolerance": 50, "lazy": false,
	}}, groups...)
	if !validControllerAddress(controllerAddress) {
		return nil, nil, ErrConfigNotReady
	}
	document := map[string]any{
		"mode": "rule", "log-level": "warning", "allow-lan": false, "ipv6": true, "external-controller": controllerAddress,
		"profile": map[string]any{"store-selected": true},
		"dns": map[string]any{
			"enable": true, "listen": "127.0.0.1:1053", "enhanced-mode": "redir-host", "respect-rules": true,
			"default-nameserver":      []string{"223.5.5.5", "119.29.29.29"},
			"nameserver":              []string{"https://1.1.1.1/dns-query", "https://8.8.8.8/dns-query"},
			"proxy-server-nameserver": []string{"https://dns.alidns.com/dns-query", "https://doh.pub/dns-query"},
		},
		"listeners": listeners, "proxies": proxies, "proxy-groups": groups,
		"rules": []string{"IP-CIDR,1.1.1.1/32,🌐 DNS,no-resolve", "IP-CIDR,8.8.8.8/32,🌐 DNS,no-resolve", "MATCH,REJECT"},
	}
	if controllerSecret != "" {
		if !dashboardSecretPattern.MatchString(controllerSecret) {
			return nil, nil, ErrConfigNotReady
		}
		document["secret"] = controllerSecret
	}
	if externalUI != "" {
		if externalUI != "ui" {
			return nil, nil, ErrConfigNotReady
		}
		document["external-ui"] = externalUI
	}
	body, err := yaml.Marshal(document)
	return body, countries, err
}

func countryFlag(code string) string {
	if len(code) != 2 {
		return "🌐"
	}
	runes := []rune(strings.ToUpper(code))
	return string([]rune{0x1F1E6 + runes[0] - 'A', 0x1F1E6 + runes[1] - 'A'})
}
func countryPort(code string) int {
	return lineegress.CountryListenerPort(strings.ToUpper(code))
}

func CountryListenerPort(code string) int {
	return lineegress.CountryListenerPort(code)
}

func writePrivateFile(path string, body []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := file.Write(body); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	return file.Close()
}
func writeAtomicPrivateFile(path string, body []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".current-")
	if err != nil {
		return err
	}
	temporary := file.Name()
	defer os.Remove(temporary)
	if err := file.Chmod(0o600); err != nil {
		file.Close()
		return err
	}
	if _, err := file.Write(body); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(temporary, path)
}
