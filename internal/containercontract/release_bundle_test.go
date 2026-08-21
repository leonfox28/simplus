package containercontract

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	testReleaseTag    = "v0.1.0"
	testReleaseCommit = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	testReleaseEpoch  = "1700000000"
)

func TestContainerReleaseBundleIsDeterministicAndAllowlisted(t *testing.T) {
	firstOutput := t.TempDir()
	secondOutput := t.TempDir()
	firstArchive := buildContainerReleaseBundle(t, testReleaseTag, testReleaseCommit, testReleaseEpoch, firstOutput)
	secondArchive := buildContainerReleaseBundle(t, testReleaseTag, testReleaseCommit, testReleaseEpoch, secondOutput)

	firstBody, err := os.ReadFile(firstArchive)
	if err != nil {
		t.Fatal(err)
	}
	secondBody, err := os.ReadFile(secondArchive)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(firstBody, secondBody) {
		t.Fatal("release bundle differs across identical builds")
	}

	archiveName := filepath.Base(firstArchive)
	checksumPath := firstArchive + ".sha256"
	checksum, err := os.ReadFile(checksumPath)
	if err != nil {
		t.Fatal(err)
	}
	wantChecksum := fmt.Sprintf("%x  %s\n", sha256.Sum256(firstBody), archiveName)
	if string(checksum) != wantChecksum {
		t.Fatalf("checksum file = %q, want %q", checksum, wantChecksum)
	}
	for _, path := range []string{firstArchive, checksumPath} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o644 {
			t.Fatalf("output %s mode = %04o", filepath.Base(path), info.Mode().Perm())
		}
	}

	bundleName := "simplus-compose-v0.1.0-linux-amd64"
	wantModes := map[string]int64{
		bundleName + "/":                          0o755,
		bundleName + "/.env.example":              0o644,
		bundleName + "/LICENSE":                   0o644,
		bundleName + "/README.md":                 0o644,
		bundleName + "/THIRD_PARTY_NOTICES.md":    0o644,
		bundleName + "/VERSION":                   0o644,
		bundleName + "/check-container-host.sh":   0o755,
		bundleName + "/compose.yaml":              0o644,
		bundleName + "/prepare-container-host.sh": 0o755,
	}
	files, headers := readReleaseArchive(t, firstBody)
	if len(headers) != len(wantModes) {
		t.Fatalf("archive entries = %v", sortedKeys(headers))
	}
	wantTime := time.Unix(1700000000, 0)
	for name, wantMode := range wantModes {
		header, found := headers[name]
		if !found {
			t.Fatalf("archive is missing allowlisted entry %q", name)
		}
		if header.Mode != wantMode || header.Uid != 0 || header.Gid != 0 || !header.ModTime.Equal(wantTime) {
			t.Fatalf("entry %q metadata = mode %04o uid %d gid %d mtime %s", name, header.Mode, header.Uid, header.Gid, header.ModTime)
		}
	}

	if got := string(files[bundleName+"/.env.example"]); got != "SIMPLUS_HTTP_PORT=8080\nSIMPLUS_CONTROLLER_PORT=19090\nSIMPLUS_DEVICE_GID=20\n" {
		t.Fatalf("release environment template = %q", got)
	}
	if got := string(files[bundleName+"/VERSION"]); got != "version=v0.1.0\ncommit="+testReleaseCommit+"\nplatform=linux/amd64\nsource_date_epoch=1700000000\n" {
		t.Fatalf("VERSION = %q", got)
	}
	if !bytes.HasPrefix(files[bundleName+"/LICENSE"], []byte("# PolyForm Noncommercial License 1.0.0")) || len(files[bundleName+"/THIRD_PARTY_NOTICES.md"]) == 0 {
		t.Fatal("release bundle is missing the project license or third-party notices")
	}

	composeBody := string(files[bundleName+"/compose.yaml"])
	for _, forbidden := range []string{"SIMPLUS_IMAGE_TAG", ":latest", "    build:"} {
		if strings.Contains(composeBody, forbidden) {
			t.Fatalf("release Compose contains forbidden development input %q", forbidden)
		}
	}
	var compose composeFile
	decoder := yaml.NewDecoder(strings.NewReader(composeBody))
	decoder.KnownFields(true)
	if err := decoder.Decode(&compose); err != nil {
		t.Fatalf("decode release Compose: %v", err)
	}
	wantImages := map[string]string{
		"data-init": "ghcr.io/leonfox28/simplus-netd:v0.1.0",
		"agent":     "ghcr.io/leonfox28/simplus-agent:v0.1.0",
		"netd":      "ghcr.io/leonfox28/simplus-netd:v0.1.0",
		"app":       "ghcr.io/leonfox28/simplus-control:v0.1.0",
		"bootstrap": "ghcr.io/leonfox28/simplus-control:v0.1.0",
	}
	for name, wantImage := range wantImages {
		if got := compose.Services[name].Image; got != wantImage {
			t.Fatalf("release service %q image = %q, want %q", name, got, wantImage)
		}
	}
}

func TestContainerReleaseBundleRejectsInvalidMetadata(t *testing.T) {
	validOutput := t.TempDir()
	nonexistentOutput := filepath.Join(t.TempDir(), "missing")
	symlinkOutput := filepath.Join(t.TempDir(), "output-link")
	if err := os.Symlink(validOutput, symlinkOutput); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name string
		args []string
	}{
		{name: "missing arguments", args: nil},
		{name: "non-version tag", args: []string{"latest", testReleaseCommit, testReleaseEpoch, validOutput}},
		{name: "prerelease tag", args: []string{"v0.1.0-rc.1", testReleaseCommit, testReleaseEpoch, validOutput}},
		{name: "short commit", args: []string{testReleaseTag, "abc123", testReleaseEpoch, validOutput}},
		{name: "uppercase commit", args: []string{testReleaseTag, strings.Repeat("A", 40), testReleaseEpoch, validOutput}},
		{name: "negative epoch", args: []string{testReleaseTag, testReleaseCommit, "-1", validOutput}},
		{name: "missing output directory", args: []string{testReleaseTag, testReleaseCommit, testReleaseEpoch, nonexistentOutput}},
		{name: "symbolic link output directory", args: []string{testReleaseTag, testReleaseCommit, testReleaseEpoch, symlinkOutput}},
	} {
		t.Run(test.name, func(t *testing.T) {
			command := exec.Command(filepath.Join(repositoryRoot(t), "scripts", "release", "build-container-release-bundle.sh"), test.args...)
			if output, err := command.CombinedOutput(); err == nil {
				t.Fatalf("invalid invocation succeeded: %s", output)
			}
		})
	}
}

func TestPublishedContainerImageInspectionRejectsInvalidMetadata(t *testing.T) {
	script := filepath.Join(repositoryRoot(t), "scripts", "release", "inspect-published-container-image.sh")
	for _, test := range []struct {
		name string
		args []string
	}{
		{name: "missing arguments", args: nil},
		{name: "rolling tag", args: []string{"ghcr.io/leonfox28/simplus-control:latest", "latest", testReleaseCommit}},
		{name: "wrong repository", args: []string{"ghcr.io/leonfox28/other:v0.1.0", testReleaseTag, testReleaseCommit}},
		{name: "mismatched tag", args: []string{"ghcr.io/leonfox28/simplus-control:v0.1.1", testReleaseTag, testReleaseCommit}},
		{name: "short commit", args: []string{"ghcr.io/leonfox28/simplus-control:v0.1.0", testReleaseTag, "abc123"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			command := exec.Command(script, test.args...)
			if output, err := command.CombinedOutput(); err == nil {
				t.Fatalf("invalid invocation succeeded: %s", output)
			}
		})
	}
}

func TestPublishedContainerImageInspectionValidatesPlatformAndLabels(t *testing.T) {
	if _, err := exec.LookPath("jq"); err != nil {
		t.Skip("jq is required to exercise the release-only image inspection helper")
	}
	const (
		rootDigest     = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
		platformDigest = "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	)
	validManifest := `{"digest":"` + rootDigest + `","manifests":[` +
		`{"digest":"` + platformDigest + `","platform":{"os":"linux","architecture":"amd64"}},` +
		`{"digest":"sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",` +
		`"platform":{"os":"unknown","architecture":"unknown"}}]}`
	validLabels := `{"org.opencontainers.image.source":"https://github.com/leonfox28/simplus",` +
		`"org.opencontainers.image.version":"v0.1.0",` +
		`"org.opencontainers.image.revision":"` + testReleaseCommit + `",` +
		`"org.opencontainers.image.licenses":"LicenseRef-PolyForm-Noncommercial-1.0.0"}`

	mockDirectory := t.TempDir()
	mockDocker := filepath.Join(mockDirectory, "docker")
	mockBody := `#!/bin/sh
set -eu
[ "$1 $2 $3" = "buildx imagetools inspect" ]
case "$4" in
  *:v0.1.0) printf '%s\n' "$MOCK_ROOT_MANIFEST" ;;
  *@sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc) printf '%s\n' "$MOCK_IMAGE_LABELS" ;;
  *) exit 90 ;;
esac
`
	if err := os.WriteFile(mockDocker, []byte(mockBody), 0o755); err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		name     string
		manifest string
		labels   string
		wantOK   bool
	}{
		{name: "valid image", manifest: validManifest, labels: validLabels, wantOK: true},
		{
			name:     "extra runtime platform",
			manifest: strings.Replace(validManifest, `"os":"unknown","architecture":"unknown"`, `"os":"linux","architecture":"arm64"`, 1),
			labels:   validLabels,
		},
		{
			name:     "mismatched OCI revision",
			manifest: validManifest,
			labels:   strings.Replace(validLabels, testReleaseCommit, strings.Repeat("e", 40), 1),
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			command := exec.Command(
				filepath.Join(repositoryRoot(t), "scripts", "release", "inspect-published-container-image.sh"),
				"ghcr.io/leonfox28/simplus-control:v0.1.0", testReleaseTag, testReleaseCommit,
			)
			command.Env = append(os.Environ(),
				"PATH="+mockDirectory+string(os.PathListSeparator)+os.Getenv("PATH"),
				"MOCK_ROOT_MANIFEST="+test.manifest,
				"MOCK_IMAGE_LABELS="+test.labels,
			)
			output, err := command.CombinedOutput()
			if test.wantOK {
				if err != nil {
					t.Fatalf("valid image inspection failed: %v\n%s", err, output)
				}
				if string(output) != rootDigest+"\n" {
					t.Fatalf("inspection digest = %q, want %q", output, rootDigest)
				}
			} else if err == nil {
				t.Fatalf("invalid published image passed inspection: %s", output)
			}
		})
	}
}

func buildContainerReleaseBundle(t *testing.T, tag, commit, epoch, output string) string {
	t.Helper()
	command := exec.Command(
		filepath.Join(repositoryRoot(t), "scripts", "release", "build-container-release-bundle.sh"),
		tag, commit, epoch, output,
	)
	if body, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build release bundle: %v\n%s", err, body)
	}
	return filepath.Join(output, "simplus-compose-"+tag+"-linux-amd64.tar.gz")
}

func readReleaseArchive(t *testing.T, body []byte) (map[string][]byte, map[string]*tar.Header) {
	t.Helper()
	compressed, err := gzip.NewReader(bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer compressed.Close()
	if !compressed.ModTime.IsZero() || compressed.Name != "" || compressed.Comment != "" {
		t.Fatalf("gzip header is not normalized: %#v", compressed.Header)
	}

	files := make(map[string][]byte)
	headers := make(map[string]*tar.Header)
	reader := tar.NewReader(compressed)
	var names []string
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		copy := *header
		headers[header.Name] = &copy
		names = append(names, header.Name)
		if header.Typeflag == tar.TypeReg || header.Typeflag == tar.TypeRegA {
			contents, err := io.ReadAll(reader)
			if err != nil {
				t.Fatal(err)
			}
			files[header.Name] = contents
		}
	}
	wantOrder := append([]string(nil), names...)
	sort.Strings(wantOrder)
	if !reflect.DeepEqual(names, wantOrder) {
		t.Fatalf("archive order = %v, want lexical order", names)
	}
	return files, headers
}

func sortedKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
