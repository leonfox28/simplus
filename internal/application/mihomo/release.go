package mihomo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"runtime"
	"strings"
	"time"
)

const (
	OfficialLatestReleaseURL = "https://api.github.com/repos/MetaCubeX/mihomo/releases/latest"
	maxReleaseMetadataBytes  = 2 << 20
	maxCompressedAssetBytes  = 128 << 20
)

var (
	stableTagPattern = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+$`)
	sha256Pattern    = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
)

type ReleaseAsset struct {
	Name               string `json:"name"`
	Size               int64  `json:"size"`
	Digest             string `json:"digest"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

type Release struct {
	TagName    string         `json:"tag_name"`
	Draft      bool           `json:"draft"`
	Prerelease bool           `json:"prerelease"`
	Assets     []ReleaseAsset `json:"assets"`
}

type Candidate struct {
	Version      string `json:"version"`
	AssetName    string `json:"assetName"`
	DownloadURL  string `json:"downloadUrl"`
	SHA256       string `json:"sha256"`
	Size         int64  `json:"size"`
	Architecture string `json:"architecture"`
}

type ReleaseClient struct {
	HTTPClient  *http.Client
	MetadataURL string
	GOOS        string
	GOARCH      string
}

func NewReleaseClient() *ReleaseClient {
	return &ReleaseClient{HTTPClient: &http.Client{Timeout: 20 * time.Second}, MetadataURL: OfficialLatestReleaseURL, GOOS: runtime.GOOS, GOARCH: runtime.GOARCH}
}

func (client *ReleaseClient) Latest(ctx context.Context) (Candidate, error) {
	if client == nil || client.HTTPClient == nil || client.MetadataURL == "" {
		return Candidate{}, errors.New("mihomo release client is not configured")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, client.MetadataURL, nil)
	if err != nil {
		return Candidate{}, err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("User-Agent", "Simplus")
	response, err := client.HTTPClient.Do(request)
	if err != nil {
		return Candidate{}, fmt.Errorf("fetch official Mihomo release: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return Candidate{}, fmt.Errorf("official Mihomo release returned HTTP %d", response.StatusCode)
	}
	limited := io.LimitReader(response.Body, maxReleaseMetadataBytes+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		return Candidate{}, fmt.Errorf("read official Mihomo release: %w", err)
	}
	if len(body) > maxReleaseMetadataBytes {
		return Candidate{}, errors.New("official Mihomo release metadata exceeds size limit")
	}
	var release Release
	// GitHub release objects contain many fields. Decode the bounded envelope
	// without DisallowUnknownFields while strict validation stays on selected fields.
	if err := json.Unmarshal(body, &release); err != nil {
		return Candidate{}, fmt.Errorf("decode official Mihomo release: %w", err)
	}
	return SelectStableAsset(release, client.GOOS, client.GOARCH)
}

func SelectStableAsset(release Release, goos, goarch string) (Candidate, error) {
	if release.Draft || release.Prerelease || !stableTagPattern.MatchString(release.TagName) {
		return Candidate{}, errors.New("Mihomo release is not a stable semantic version")
	}
	assetName := ""
	switch {
	case goos == "linux" && goarch == "amd64":
		assetName = "mihomo-linux-amd64-compatible-" + release.TagName + ".gz"
	case goos == "linux" && goarch == "arm64":
		assetName = "mihomo-linux-arm64-" + release.TagName + ".gz"
	default:
		return Candidate{}, fmt.Errorf("Mihomo core is unsupported on %s/%s", goos, goarch)
	}
	for _, asset := range release.Assets {
		if asset.Name != assetName {
			continue
		}
		if asset.Size <= 0 || asset.Size > maxCompressedAssetBytes {
			return Candidate{}, errors.New("Mihomo asset size is invalid")
		}
		if !sha256Pattern.MatchString(asset.Digest) {
			return Candidate{}, errors.New("Mihomo asset has no valid SHA-256 digest")
		}
		parsed, err := url.Parse(asset.BrowserDownloadURL)
		expectedPrefix := "/MetaCubeX/mihomo/releases/download/" + release.TagName + "/"
		if err != nil || parsed.Scheme != "https" || parsed.Hostname() != "github.com" || !strings.HasPrefix(parsed.EscapedPath(), expectedPrefix) || parsed.RawQuery != "" || parsed.Fragment != "" {
			return Candidate{}, errors.New("Mihomo asset URL is not an official release URL")
		}
		return Candidate{Version: release.TagName, AssetName: asset.Name, DownloadURL: asset.BrowserDownloadURL, SHA256: strings.TrimPrefix(asset.Digest, "sha256:"), Size: asset.Size, Architecture: goarch}, nil
	}
	return Candidate{}, fmt.Errorf("official Mihomo release has no supported %s/%s asset", goos, goarch)
}

func AllowedDownloadRedirect(target *url.URL) bool {
	if target == nil || target.Scheme != "https" {
		return false
	}
	switch strings.ToLower(target.Hostname()) {
	case "github.com", "release-assets.githubusercontent.com", "objects.githubusercontent.com":
		return true
	default:
		return false
	}
}
