package mihomo

import (
	"net/url"
	"testing"
)

func TestSelectStableAssetChoosesBoundedOfficialLinuxAsset(t *testing.T) {
	release := Release{TagName: "v1.19.29", Assets: []ReleaseAsset{
		{Name: "mihomo-linux-amd64-v1.19.29.gz", Size: 10, Digest: "sha256:" + repeat("a", 64), BrowserDownloadURL: "https://github.com/MetaCubeX/mihomo/releases/download/v1.19.29/mihomo-linux-amd64-v1.19.29.gz"},
		{Name: "mihomo-linux-amd64-compatible-v1.19.29.gz", Size: 20, Digest: "sha256:" + repeat("b", 64), BrowserDownloadURL: "https://github.com/MetaCubeX/mihomo/releases/download/v1.19.29/mihomo-linux-amd64-compatible-v1.19.29.gz"},
	}}
	candidate, err := SelectStableAsset(release, "linux", "amd64")
	if err != nil {
		t.Fatal(err)
	}
	if candidate.AssetName != "mihomo-linux-amd64-compatible-v1.19.29.gz" || candidate.SHA256 != repeat("b", 64) {
		t.Fatalf("candidate = %#v", candidate)
	}
}

func TestSelectStableAssetRejectsUnsafeMetadata(t *testing.T) {
	valid := Release{TagName: "v1.2.3", Assets: []ReleaseAsset{{Name: "mihomo-linux-arm64-v1.2.3.gz", Size: 20, Digest: "sha256:" + repeat("a", 64), BrowserDownloadURL: "https://github.com/MetaCubeX/mihomo/releases/download/v1.2.3/mihomo-linux-arm64-v1.2.3.gz"}}}
	tests := []Release{
		{TagName: "Prerelease-Alpha", Prerelease: true},
		{TagName: valid.TagName, Assets: []ReleaseAsset{{Name: valid.Assets[0].Name, Size: 20, Digest: "", BrowserDownloadURL: valid.Assets[0].BrowserDownloadURL}}},
		{TagName: valid.TagName, Assets: []ReleaseAsset{{Name: valid.Assets[0].Name, Size: 20, Digest: valid.Assets[0].Digest, BrowserDownloadURL: "https://example.com/mihomo.gz"}}},
	}
	for index, release := range tests {
		if _, err := SelectStableAsset(release, "linux", "arm64"); err == nil {
			t.Fatalf("case %d accepted", index)
		}
	}
}

func TestAllowedDownloadRedirect(t *testing.T) {
	for _, raw := range []string{"https://github.com/a", "https://release-assets.githubusercontent.com/a", "https://objects.githubusercontent.com/a"} {
		parsed, _ := url.Parse(raw)
		if !AllowedDownloadRedirect(parsed) {
			t.Errorf("rejected %s", raw)
		}
	}
	for _, raw := range []string{"http://github.com/a", "https://github.example/a", "https://example.com/a"} {
		parsed, _ := url.Parse(raw)
		if AllowedDownloadRedirect(parsed) {
			t.Errorf("accepted %s", raw)
		}
	}
}

func repeat(value string, count int) string {
	result := ""
	for range count {
		result += value
	}
	return result
}
