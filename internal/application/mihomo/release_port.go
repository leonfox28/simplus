package mihomo

type Candidate struct {
	Version      string `json:"version"`
	AssetName    string `json:"assetName"`
	DownloadURL  string `json:"downloadUrl"`
	SHA256       string `json:"sha256"`
	Size         int64  `json:"size"`
	Architecture string `json:"architecture"`
}
