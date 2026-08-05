package mihomo

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	domain "github.com/leonfox28/simplus/internal/domain/mihomo"
	"gopkg.in/yaml.v3"
)

var subscriptionIDPattern = regexp.MustCompile(`^subscription_[A-Za-z0-9_-]{22}$`)
var subscriptionDefaultNamePattern = regexp.MustCompile(`^[A-Z2-7]{6}$`)

var (
	ErrSubscriptionInvalid  = errors.New("Mihomo subscription request is invalid")
	ErrSubscriptionNotFound = errors.New("Mihomo subscription not found")
)

type SubscriptionStore interface {
	ListMihomoSubscriptions(context.Context) ([]domain.Subscription, error)
	ReadMihomoSubscription(context.Context, string) (domain.Subscription, bool, error)
	UpsertMihomoSubscription(context.Context, domain.Subscription) error
	DeleteMihomoSubscription(context.Context, string) (bool, error)
	ReplaceMihomoSubscriptionNodes(context.Context, string, []domain.Node, time.Time, string, string) error
	ListMihomoSubscriptionNodes(context.Context, string) ([]domain.Node, error)
	MarkMihomoSubscriptionRefreshFailure(context.Context, string, string, time.Time) error
	ReadMihomoRuntimeSelection(context.Context) (string, string, error)
}

type SubscriptionArtifactManager interface {
	BuildSubscription(context.Context, string, []byte, []domain.Node) (ArtifactMetadata, error)
	Select(context.Context, string) (ConfigStatus, error)
	DeleteSubscriptionArtifacts(string) error
	Artifact(string) (ArtifactMetadata, string, error)
}

type SecretCipher interface {
	Encrypt(string, []byte) ([]byte, error)
	Decrypt(string, []byte) ([]byte, error)
}

type SubscriptionView struct {
	ID, DisplayName, URL, URLHint, LastRefreshStatus, LastErrorCode string
	Enabled                                                         bool
	Selected                                                        bool
	ArtifactReady                                                   bool
	NodeCount                                                       int
	LastRefreshAt                                                   time.Time
}

type SubscriptionService struct {
	Store      SubscriptionStore
	Secrets    SecretCipher
	Now        func() time.Time
	HTTPClient *http.Client
	Artifacts  SubscriptionArtifactManager
}

func NewSubscriptionService(store SubscriptionStore, secrets SecretCipher, artifacts ...SubscriptionArtifactManager) *SubscriptionService {
	service := &SubscriptionService{Store: store, Secrets: secrets, Now: time.Now, HTTPClient: newSubscriptionHTTPClient()}
	if len(artifacts) != 0 {
		service.Artifacts = artifacts[0]
	}
	return service
}

func (service *SubscriptionService) List(ctx context.Context) ([]SubscriptionView, error) {
	if service == nil || service.Store == nil {
		return nil, errors.New("Mihomo subscription service is not configured")
	}
	items, err := service.Store.ListMihomoSubscriptions(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]SubscriptionView, 0, len(items))
	selected, _, err := service.Store.ReadMihomoRuntimeSelection(ctx)
	if err != nil {
		return nil, err
	}
	for _, item := range items {
		if item.URLPlaintext == "" {
			if _, migrated, migrateErr := service.subscriptionURL(ctx, item.ID); migrateErr != nil {
				return nil, migrateErr
			} else {
				item = migrated
			}
		}
		view := subscriptionView(item)
		view.Selected = item.ID == selected
		if service.Artifacts != nil {
			_, _, artifactErr := service.Artifacts.Artifact(item.ID)
			view.ArtifactReady = artifactErr == nil
		}
		result = append(result, view)
	}
	return result, nil
}

func (service *SubscriptionService) Create(ctx context.Context, name, rawURL string, enabled bool) (SubscriptionView, error) {
	if service == nil || service.Store == nil || service.Now == nil {
		return SubscriptionView{}, errors.New("Mihomo subscription service is not configured")
	}
	var id string
	for attempt := 0; attempt < 4; attempt++ {
		candidate, err := newSubscriptionID()
		if err != nil {
			return SubscriptionView{}, err
		}
		if _, found, err := service.Store.ReadMihomoSubscription(ctx, candidate); err != nil {
			return SubscriptionView{}, err
		} else if !found {
			id = candidate
			break
		}
	}
	if id == "" {
		return SubscriptionView{}, errors.New("could not allocate a unique Mihomo subscription ID")
	}
	if strings.TrimSpace(name) == "" {
		name = defaultSubscriptionDisplayName(id)
	}
	name, parsed, err := validateSubscriptionInput(name, rawURL)
	if err != nil {
		return SubscriptionView{}, err
	}
	now := service.Now().UTC()
	item := domain.Subscription{ID: id, DisplayName: name, URLCiphertext: make([]byte, 32), URLPlaintext: parsed.String(), URLHint: parsed.Hostname(), Enabled: enabled, LastRefreshStatus: "never", CreatedAt: now, UpdatedAt: now}
	if err := service.Store.UpsertMihomoSubscription(ctx, item); err != nil {
		return SubscriptionView{}, err
	}
	return subscriptionView(item), nil
}

func (service *SubscriptionService) Update(ctx context.Context, id, name, rawURL string, enabled bool) (SubscriptionView, error) {
	if !subscriptionIDPattern.MatchString(id) {
		return SubscriptionView{}, ErrSubscriptionInvalid
	}
	current, found, err := service.Store.ReadMihomoSubscription(ctx, id)
	if err != nil {
		return SubscriptionView{}, err
	}
	if !found {
		return SubscriptionView{}, ErrSubscriptionNotFound
	}
	selected, running, err := service.Store.ReadMihomoRuntimeSelection(ctx)
	if err != nil {
		return SubscriptionView{}, err
	}
	if !enabled && (id == selected || id == running) {
		return SubscriptionView{}, ErrSubscriptionInvalid
	}
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > 80 {
		return SubscriptionView{}, ErrSubscriptionInvalid
	}
	current.DisplayName, current.Enabled, current.UpdatedAt = name, enabled, service.Now().UTC()
	if strings.TrimSpace(rawURL) != "" {
		_, parsed, err := validateSubscriptionInput(name, rawURL)
		if err != nil {
			return SubscriptionView{}, err
		}
		current.URLPlaintext = parsed.String()
		current.URLCiphertext = make([]byte, 32)
		current.URLHint = parsed.Hostname()
	}
	if err := service.Store.UpsertMihomoSubscription(ctx, current); err != nil {
		return SubscriptionView{}, err
	}
	return subscriptionView(current), nil
}

func (service *SubscriptionService) Delete(ctx context.Context, id string) error {
	if !subscriptionIDPattern.MatchString(id) {
		return ErrSubscriptionInvalid
	}
	selected, running, err := service.Store.ReadMihomoRuntimeSelection(ctx)
	if err != nil {
		return err
	}
	if id == selected || id == running {
		return ErrSubscriptionInvalid
	}
	deleted, err := service.Store.DeleteMihomoSubscription(ctx, id)
	if err != nil {
		return err
	}
	if !deleted {
		return ErrSubscriptionNotFound
	}
	if service.Artifacts != nil {
		if err := service.Artifacts.DeleteSubscriptionArtifacts(id); err != nil {
			return err
		}
	}
	return nil
}

func (service *SubscriptionService) Nodes(ctx context.Context, id string) ([]domain.Node, error) {
	if !subscriptionIDPattern.MatchString(id) {
		return nil, ErrSubscriptionInvalid
	}
	if _, found, err := service.Store.ReadMihomoSubscription(ctx, id); err != nil {
		return nil, err
	} else if !found {
		return nil, ErrSubscriptionNotFound
	}
	return service.Store.ListMihomoSubscriptionNodes(ctx, id)
}

func (service *SubscriptionService) subscriptionURL(ctx context.Context, id string) (*url.URL, domain.Subscription, error) {
	item, found, err := service.Store.ReadMihomoSubscription(ctx, id)
	if err != nil {
		return nil, item, err
	}
	if !found {
		return nil, item, ErrSubscriptionNotFound
	}
	plaintext := item.URLPlaintext
	if plaintext == "" {
		if service.Secrets == nil {
			return nil, item, errors.New("legacy encrypted Mihomo subscription URL cannot be migrated")
		}
		decoded, err := service.Secrets.Decrypt(subscriptionSecretLabel(id), item.URLCiphertext)
		if err != nil {
			return nil, item, fmt.Errorf("decrypt legacy Mihomo subscription URL: %w", err)
		}
		plaintext = string(decoded)
		item.URLPlaintext = plaintext
		item.URLCiphertext = make([]byte, 32)
		item.UpdatedAt = service.Now().UTC()
		if err := service.Store.UpsertMihomoSubscription(ctx, item); err != nil {
			return nil, item, fmt.Errorf("migrate Mihomo subscription URL to plaintext: %w", err)
		}
	}
	_, parsed, err := validateSubscriptionInput(item.DisplayName, plaintext)
	return parsed, item, err
}

func validateSubscriptionInput(name, rawURL string) (string, *url.URL, error) {
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > 80 || len(rawURL) > 4096 {
		return "", nil, ErrSubscriptionInvalid
	}
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" {
		return "", nil, ErrSubscriptionInvalid
	}
	host := parsed.Hostname()
	if strings.EqualFold(host, "localhost") {
		return "", nil, ErrSubscriptionInvalid
	}
	if ip := net.ParseIP(host); ip != nil && (ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() || ip.IsMulticast()) {
		return "", nil, ErrSubscriptionInvalid
	}
	return name, parsed, nil
}

func newSubscriptionID() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return "subscription_" + base64.RawURLEncoding.EncodeToString(raw), nil
}
func defaultSubscriptionDisplayName(id string) string {
	digest := sha256.Sum256([]byte(id))
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(digest[:])[:6]
}
func subscriptionSecretLabel(id string) string { return "mihomo-subscription-url:v1:" + id }
func subscriptionView(item domain.Subscription) SubscriptionView {
	return SubscriptionView{ID: item.ID, DisplayName: item.DisplayName, URL: item.URLPlaintext, URLHint: item.URLHint, Enabled: item.Enabled, LastRefreshAt: item.LastRefreshAt, LastRefreshStatus: item.LastRefreshStatus, NodeCount: item.NodeCount, LastErrorCode: item.LastErrorCode}
}

func (service *SubscriptionService) Refresh(ctx context.Context, id string) (SubscriptionView, []domain.Node, error) {
	if service == nil || service.HTTPClient == nil || service.Now == nil || !subscriptionIDPattern.MatchString(id) {
		return SubscriptionView{}, nil, ErrSubscriptionInvalid
	}
	target, item, err := service.subscriptionURL(ctx, id)
	if err != nil {
		return SubscriptionView{}, nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return SubscriptionView{}, nil, err
	}
	request.Header.Set("Accept", "application/yaml,text/yaml,text/plain,application/octet-stream")
	request.Header.Set("User-Agent", "Simplus")
	response, err := service.HTTPClient.Do(request)
	if err != nil {
		return service.refreshFailed(ctx, item, "SUBSCRIPTION_FETCH_FAILED", fmt.Errorf("fetch Mihomo subscription: %w", err))
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return service.refreshFailed(ctx, item, "SUBSCRIPTION_FETCH_FAILED", fmt.Errorf("Mihomo subscription returned HTTP %d", response.StatusCode))
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 5<<20+1))
	if err != nil || len(body) > 5<<20 {
		return service.refreshFailed(ctx, item, "SUBSCRIPTION_FETCH_FAILED", errors.New("Mihomo subscription response exceeds limit"))
	}
	nodes, err := parseSubscriptionNodes(id, body)
	if err != nil {
		return service.refreshFailed(ctx, item, "SUBSCRIPTION_PARSE_FAILED", err)
	}
	if service.Artifacts == nil {
		return service.refreshFailed(ctx, item, "SUBSCRIPTION_CONFIG_UNAVAILABLE", ErrConfigNotReady)
	}
	if _, err := service.Artifacts.BuildSubscription(ctx, id, body, nodes); err != nil {
		code := "SUBSCRIPTION_CONFIG_GENERATION_FAILED"
		if errors.Is(err, ErrConfigValidationFailed) {
			code = "SUBSCRIPTION_CONFIG_VALIDATION_FAILED"
		}
		return service.refreshFailed(ctx, item, code, err)
	}
	now := service.Now().UTC()
	if err := service.Store.ReplaceMihomoSubscriptionNodes(ctx, id, nodes, now, "success", ""); err != nil {
		return SubscriptionView{}, nil, err
	}
	item.LastRefreshAt, item.LastRefreshStatus, item.LastErrorCode, item.NodeCount, item.UpdatedAt = now, "success", "", len(nodes), now
	if selected, _, selectionErr := service.Store.ReadMihomoRuntimeSelection(ctx); selectionErr == nil && selected == "" {
		if _, selectionErr := service.Artifacts.Select(ctx, id); selectionErr != nil {
			return SubscriptionView{}, nil, selectionErr
		}
	}
	return subscriptionView(item), nodes, nil
}

func (service *SubscriptionService) refreshFailed(ctx context.Context, item domain.Subscription, code string, cause error) (SubscriptionView, []domain.Node, error) {
	now := service.Now().UTC()
	_ = service.Store.MarkMihomoSubscriptionRefreshFailure(ctx, item.ID, code, now)
	item.LastRefreshAt, item.LastRefreshStatus, item.LastErrorCode, item.UpdatedAt = now, "failed", code, now
	return subscriptionView(item), nil, cause
}

func parseSubscriptionNodes(subscriptionID string, body []byte) ([]domain.Node, error) {
	var document struct {
		Proxies []map[string]any `yaml:"proxies"`
	}
	var yamlDocument yaml.Node
	yamlErr := yaml.Unmarshal(body, &yamlDocument)
	if yamlErr == nil {
		count := 0
		if err := validateSubscriptionYAMLNode(&yamlDocument, 0, &count); err != nil {
			return nil, err
		}
		yamlErr = yamlDocument.Decode(&document)
	}
	if yamlErr == nil && len(document.Proxies) != 0 {
		if len(document.Proxies) > 10000 {
			return nil, ErrSubscriptionInvalid
		}
		nodes := make([]domain.Node, 0, len(document.Proxies))
		seen := make(map[string]struct{})
		for _, proxy := range document.Proxies {
			name, _ := proxy["name"].(string)
			kind, _ := proxy["type"].(string)
			node, err := summarizedNode(subscriptionID, name, kind)
			if err != nil {
				return nil, err
			}
			if !supportedProxyKind(node.Kind) {
				return nil, fmt.Errorf("%w: unsupported proxy type %q", ErrSubscriptionInvalid, node.Kind)
			}
			serialized, err := yaml.Marshal(proxy)
			if err != nil || len(serialized) > 64<<10 {
				return nil, ErrSubscriptionInvalid
			}
			node.ProxyYAML = string(serialized)
			node.CountryCode, node.CountryName = classifyNodeCountry(node.DisplayName)
			if _, duplicate := seen[node.ID]; duplicate {
				return nil, ErrSubscriptionInvalid
			}
			seen[node.ID] = struct{}{}
			nodes = append(nodes, node)
		}
		sort.Slice(nodes, func(i, j int) bool { return nodes[i].DisplayName < nodes[j].DisplayName })
		return nodes, nil
	}
	decoded := body
	compact := strings.TrimSpace(string(body))
	if raw, err := base64.StdEncoding.DecodeString(compact); err == nil {
		decoded = raw
	} else if raw, err := base64.RawStdEncoding.DecodeString(compact); err == nil {
		decoded = raw
	}
	lines := strings.Split(string(decoded), "\n")
	nodes := make([]domain.Node, 0)
	seen := make(map[string]struct{})
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parsed, err := url.Parse(line)
		if err != nil {
			return nil, ErrSubscriptionInvalid
		}
		kind := strings.ToLower(parsed.Scheme)
		switch kind {
		case "ss", "ssr", "vmess", "vless", "trojan", "hysteria2", "hy2", "tuic":
		default:
			return nil, ErrSubscriptionInvalid
		}
		name, _ := url.QueryUnescape(parsed.Fragment)
		if strings.TrimSpace(name) == "" {
			name = fmt.Sprintf("%s-%d", kind, len(nodes)+1)
		}
		node, err := summarizedNode(subscriptionID, name, kind)
		if err != nil {
			return nil, err
		}
		if _, duplicate := seen[node.ID]; duplicate {
			return nil, ErrSubscriptionInvalid
		}
		seen[node.ID] = struct{}{}
		node.CountryCode, node.CountryName = classifyNodeCountry(node.DisplayName)
		nodes = append(nodes, node)
		if len(nodes) > 10000 {
			return nil, ErrSubscriptionInvalid
		}
	}
	if len(nodes) == 0 {
		return nil, ErrSubscriptionInvalid
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].DisplayName < nodes[j].DisplayName })
	return nodes, nil
}

func validateSubscriptionYAMLNode(node *yaml.Node, depth int, count *int) error {
	if node == nil || depth > 64 {
		return ErrSubscriptionInvalid
	}
	*count++
	if *count > 100000 || node.Kind == yaml.AliasNode {
		return ErrSubscriptionInvalid
	}
	for _, child := range node.Content {
		if err := validateSubscriptionYAMLNode(child, depth+1, count); err != nil {
			return err
		}
	}
	return nil
}

func supportedProxyKind(kind string) bool {
	switch kind {
	case "ss", "ssr", "vmess", "vless", "trojan", "hysteria", "hysteria2", "hy2", "tuic", "anytls", "http", "socks5", "wireguard":
		return true
	default:
		return false
	}
}

var countryPatterns = []struct {
	code, name string
	markers    []string
}{
	{"HK", "香港", []string{"🇭🇰", "香港", "hong kong"}}, {"US", "美国", []string{"🇺🇸", "美国", "united states", " usa", " us "}},
	{"SG", "新加坡", []string{"🇸🇬", "新加坡", "singapore"}}, {"JP", "日本", []string{"🇯🇵", "日本", "japan"}},
	{"TW", "台湾", []string{"🇹🇼", "台湾", "taiwan"}}, {"KR", "韩国", []string{"🇰🇷", "韩国", "korea"}},
	{"GB", "英国", []string{"🇬🇧", "英国", "united kingdom", " uk"}}, {"DE", "德国", []string{"🇩🇪", "德国", "germany"}},
	{"FR", "法国", []string{"🇫🇷", "法国", "france"}}, {"CA", "加拿大", []string{"🇨🇦", "加拿大", "canada"}},
	{"AU", "澳大利亚", []string{"🇦🇺", "澳大利亚", "australia"}}, {"MO", "澳门", []string{"🇲🇴", "澳门", "macao", "macau"}},
	{"TH", "泰国", []string{"🇹🇭", "泰国", "thailand"}}, {"MY", "马来西亚", []string{"🇲🇾", "马来西亚", "malaysia"}},
	{"VN", "越南", []string{"🇻🇳", "越南", "vietnam"}}, {"ID", "印度尼西亚", []string{"🇮🇩", "印度尼西亚", "indonesia"}},
	{"NL", "荷兰", []string{"🇳🇱", "荷兰", "netherlands"}}, {"IN", "印度", []string{"🇮🇳", "印度", "india"}},
	{"IT", "意大利", []string{"🇮🇹", "意大利", "italy"}}, {"TR", "土耳其", []string{"🇹🇷", "土耳其", "turkey"}},
	{"BR", "巴西", []string{"🇧🇷", "巴西", "brazil"}}, {"NZ", "新西兰", []string{"🇳🇿", "新西兰", "new zealand"}},
	{"MN", "蒙古", []string{"🇲🇳", "蒙古", "mongolia"}}, {"MX", "墨西哥", []string{"🇲🇽", "墨西哥", "mexico"}},
	{"PH", "菲律宾", []string{"🇵🇭", "菲律宾", "philippines"}}, {"AQ", "南极洲", []string{"🇦🇶", "南极洲", "antarctica"}},
	{"EG", "埃及", []string{"🇪🇬", "埃及", "egypt"}}, {"AR", "阿根廷", []string{"🇦🇷", "阿根廷", "argentina"}},
	{"KH", "柬埔寨", []string{"🇰🇭", "柬埔寨", "cambodia"}}, {"LA", "老挝", []string{"🇱🇦", "老挝", "laos"}},
	{"MM", "缅甸", []string{"🇲🇲", "缅甸", "myanmar"}}, {"BN", "文莱", []string{"🇧🇳", "文莱", "brunei"}},
	{"PK", "巴基斯坦", []string{"🇵🇰", "巴基斯坦", "pakistan"}}, {"MV", "马尔代夫", []string{"🇲🇻", "马尔代夫", "maldives"}},
	{"GU", "关岛", []string{"🇬🇺", "关岛", "guam"}}, {"GL", "格陵兰岛", []string{"🇬🇱", "格陵兰岛", "greenland"}},
	{"IS", "冰岛", []string{"🇮🇸", "冰岛", "iceland"}},
}

func classifyNodeCountry(name string) (string, string) {
	lower := strings.ToLower(" " + name)
	for _, country := range countryPatterns {
		for _, marker := range country.markers {
			if strings.Contains(lower, strings.ToLower(marker)) {
				return country.code, country.name
			}
		}
	}
	return "", ""
}

func summarizedNode(subscriptionID, name, kind string) (domain.Node, error) {
	name, kind = strings.TrimSpace(name), strings.ToLower(strings.TrimSpace(kind))
	if name == "" || len([]rune(name)) > 120 || kind == "" || len(kind) > 32 {
		return domain.Node{}, ErrSubscriptionInvalid
	}
	for _, value := range []string{name, kind} {
		for _, character := range value {
			if character < 0x20 || character == 0x7f {
				return domain.Node{}, ErrSubscriptionInvalid
			}
		}
	}
	digest := sha256.Sum256([]byte(kind + "\x00" + name))
	id := "node_" + base64.RawURLEncoding.EncodeToString(digest[:16])
	return domain.Node{SubscriptionID: subscriptionID, ID: id, DisplayName: name, Kind: kind}, nil
}

func newSubscriptionHTTPClient() *http.Client {
	transport := &http.Transport{Proxy: nil, TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: 20 * time.Second}
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		addresses, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil || len(addresses) == 0 {
			return nil, fmt.Errorf("resolve subscription host: %w", err)
		}
		for _, address := range addresses {
			if isUnsafeSubscriptionIP(address.IP) {
				return nil, errors.New("subscription host resolved to a private or local address")
			}
		}
		var dialer net.Dialer
		return dialer.DialContext(ctx, network, net.JoinHostPort(addresses[0].IP.String(), port))
	}
	client := &http.Client{Transport: transport, Timeout: 30 * time.Second}
	client.CheckRedirect = func(request *http.Request, via []*http.Request) error {
		if len(via) > 3 {
			return errors.New("too many subscription redirects")
		}
		_, _, err := validateSubscriptionInput("redirect", request.URL.String())
		return err
	}
	return client
}

func isUnsafeSubscriptionIP(ip net.IP) bool {
	return ip == nil || ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() || ip.IsMulticast()
}
