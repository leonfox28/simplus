package mihomo

import (
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const maxExpandedBinaryBytes = 256 << 20

var ErrVersionAlreadyInstalled = errors.New("Mihomo version is already installed")

type CommandRunner func(context.Context, string, ...string) ([]byte, error)

type CoreStatus struct {
	Installed    bool      `json:"installed"`
	Version      string    `json:"version"`
	Architecture string    `json:"architecture"`
	SHA256       string    `json:"sha256"`
	BinaryPath   string    `json:"-"`
	InstalledAt  time.Time `json:"installedAt"`
}

type CoreManager struct {
	Root       string
	Releases   *ReleaseClient
	HTTPClient *http.Client
	Run        CommandRunner
	Now        func() time.Time
}

func NewCoreManager(root string) *CoreManager {
	downloadClient := &http.Client{Timeout: 5 * time.Minute}
	downloadClient.CheckRedirect = func(request *http.Request, via []*http.Request) error {
		if len(via) > 5 || !AllowedDownloadRedirect(request.URL) {
			return errors.New("Mihomo asset redirect is not allowed")
		}
		return nil
	}
	return &CoreManager{Root: root, Releases: NewReleaseClient(), HTTPClient: downloadClient, Run: runCommand, Now: time.Now}
}

func runCommand(ctx context.Context, path string, args ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, path, args...)
	command.Env = []string{"PATH=/usr/bin:/bin", "HOME=/nonexistent"}
	return command.CombinedOutput()
}

func (manager *CoreManager) CheckLatest(ctx context.Context) (Candidate, error) {
	if manager == nil || manager.Releases == nil {
		return Candidate{}, errors.New("Mihomo core manager is not configured")
	}
	return manager.Releases.Latest(ctx)
}

func (manager *CoreManager) InstallLatest(ctx context.Context) (CoreStatus, error) {
	candidate, err := manager.CheckLatest(ctx)
	if err != nil {
		return CoreStatus{}, err
	}
	return manager.installCandidate(ctx, candidate)
}

func (manager *CoreManager) installCandidate(ctx context.Context, candidate Candidate) (CoreStatus, error) {
	if manager == nil || manager.HTTPClient == nil {
		return CoreStatus{}, errors.New("Mihomo core manager is not configured")
	}
	if err := validateCandidate(candidate); err != nil {
		return CoreStatus{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, candidate.DownloadURL, nil)
	if err != nil {
		return CoreStatus{}, err
	}
	request.Header.Set("Accept", "application/octet-stream")
	request.Header.Set("User-Agent", "Simplus")
	response, err := manager.HTTPClient.Do(request)
	if err != nil {
		return CoreStatus{}, fmt.Errorf("download official Mihomo asset: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return CoreStatus{}, fmt.Errorf("official Mihomo asset returned HTTP %d", response.StatusCode)
	}
	if response.ContentLength > 0 && response.ContentLength != candidate.Size {
		return CoreStatus{}, errors.New("Mihomo asset content length does not match release metadata")
	}
	return manager.installArchive(ctx, candidate, response.Body)
}

func (manager *CoreManager) installArchive(ctx context.Context, candidate Candidate, archive io.Reader) (CoreStatus, error) {
	if err := validateCandidate(candidate); err != nil {
		return CoreStatus{}, err
	}
	if manager.Root == "" || !filepath.IsAbs(manager.Root) || manager.Run == nil || manager.Now == nil {
		return CoreStatus{}, errors.New("Mihomo core manager is not configured")
	}
	staging := filepath.Join(manager.Root, "staging")
	if err := os.MkdirAll(staging, 0o700); err != nil {
		return CoreStatus{}, fmt.Errorf("create Mihomo staging directory: %w", err)
	}
	archiveFile, err := os.CreateTemp(staging, "asset-*.gz")
	if err != nil {
		return CoreStatus{}, fmt.Errorf("create Mihomo asset staging file: %w", err)
	}
	archivePath := archiveFile.Name()
	defer os.Remove(archivePath)
	hash := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(archiveFile, hash), io.LimitReader(archive, candidate.Size+1))
	closeErr := archiveFile.Close()
	if copyErr != nil || closeErr != nil {
		return CoreStatus{}, fmt.Errorf("stage Mihomo asset: %w", errors.Join(copyErr, closeErr))
	}
	if written != candidate.Size {
		return CoreStatus{}, fmt.Errorf("Mihomo asset size mismatch: got %d, want %d", written, candidate.Size)
	}
	actualDigest := hex.EncodeToString(hash.Sum(nil))
	if actualDigest != candidate.SHA256 {
		return CoreStatus{}, errors.New("Mihomo asset SHA-256 mismatch")
	}

	archiveReader, err := os.Open(archivePath)
	if err != nil {
		return CoreStatus{}, err
	}
	defer archiveReader.Close()
	gzipReader, err := gzip.NewReader(archiveReader)
	if err != nil {
		return CoreStatus{}, fmt.Errorf("open Mihomo gzip asset: %w", err)
	}
	binaryFile, err := os.CreateTemp(staging, "mihomo-*")
	if err != nil {
		_ = gzipReader.Close()
		return CoreStatus{}, err
	}
	binaryPath := binaryFile.Name()
	defer os.Remove(binaryPath)
	expanded, copyErr := io.Copy(binaryFile, io.LimitReader(gzipReader, maxExpandedBinaryBytes+1))
	closeErr = errors.Join(gzipReader.Close(), binaryFile.Close())
	if copyErr != nil || closeErr != nil {
		return CoreStatus{}, fmt.Errorf("expand Mihomo asset: %w", errors.Join(copyErr, closeErr))
	}
	if expanded <= 0 || expanded > maxExpandedBinaryBytes {
		return CoreStatus{}, errors.New("expanded Mihomo binary size is invalid")
	}
	if err := os.Chmod(binaryPath, 0o700); err != nil {
		return CoreStatus{}, err
	}
	probeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	output, err := manager.Run(probeCtx, binaryPath, "-v")
	if err != nil {
		return CoreStatus{}, fmt.Errorf("probe staged Mihomo binary: %w", err)
	}
	if !strings.Contains(string(output), strings.TrimPrefix(candidate.Version, "v")) {
		return CoreStatus{}, errors.New("staged Mihomo version does not match release metadata")
	}

	versionDirectory := filepath.Join(manager.Root, "versions", candidate.Version)
	if err := os.MkdirAll(versionDirectory, 0o700); err != nil {
		return CoreStatus{}, err
	}
	finalBinary := filepath.Join(versionDirectory, "mihomo")
	if _, err := os.Lstat(finalBinary); err == nil {
		return CoreStatus{}, ErrVersionAlreadyInstalled
	} else if !os.IsNotExist(err) {
		return CoreStatus{}, err
	}
	if err := os.Rename(binaryPath, finalBinary); err != nil {
		return CoreStatus{}, fmt.Errorf("activate Mihomo version binary: %w", err)
	}
	installedAt := manager.Now().UTC()
	status := CoreStatus{Installed: true, Version: candidate.Version, Architecture: candidate.Architecture, SHA256: candidate.SHA256, BinaryPath: finalBinary, InstalledAt: installedAt}
	if err := manager.writeStatus(status); err != nil {
		_ = os.Remove(finalBinary)
		return CoreStatus{}, err
	}
	return status, nil
}

func (manager *CoreManager) Status() (CoreStatus, error) {
	if manager == nil || manager.Root == "" {
		return CoreStatus{}, errors.New("Mihomo core manager is not configured")
	}
	body, err := os.ReadFile(filepath.Join(manager.Root, "current.json"))
	if os.IsNotExist(err) {
		return CoreStatus{}, nil
	}
	if err != nil {
		return CoreStatus{}, err
	}
	var status CoreStatus
	if err := json.Unmarshal(body, &status); err != nil {
		return CoreStatus{}, fmt.Errorf("decode Mihomo core status: %w", err)
	}
	if !status.Installed || !stableTagPattern.MatchString(status.Version) || !sha256Pattern.MatchString("sha256:"+status.SHA256) {
		return CoreStatus{}, errors.New("stored Mihomo core status is invalid")
	}
	status.BinaryPath = filepath.Join(manager.Root, "versions", status.Version, "mihomo")
	info, err := os.Lstat(status.BinaryPath)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&0o111 == 0 {
		return CoreStatus{}, errors.New("installed Mihomo binary is missing or invalid")
	}
	return status, nil
}

func (manager *CoreManager) writeStatus(status CoreStatus) error {
	body, err := json.Marshal(status)
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(manager.Root, "current-*.json")
	if err != nil {
		return err
	}
	path := temporary.Name()
	defer os.Remove(path)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(body); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(path, filepath.Join(manager.Root, "current.json"))
}

func validateCandidate(candidate Candidate) error {
	if !stableTagPattern.MatchString(candidate.Version) || candidate.Size <= 0 || candidate.Size > maxCompressedAssetBytes || !sha256Pattern.MatchString("sha256:"+candidate.SHA256) {
		return errors.New("Mihomo release candidate is invalid")
	}
	if candidate.Architecture != "amd64" && candidate.Architecture != "arm64" {
		return errors.New("Mihomo release architecture is invalid")
	}
	return nil
}
