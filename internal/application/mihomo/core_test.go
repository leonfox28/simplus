package mihomo

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"strings"
	"testing"
	"time"
)

func TestInstallArchiveVerifiesAndAtomicallyPublishesCore(t *testing.T) {
	archive := gzipBytes(t, []byte("fake executable"))
	digest := sha256.Sum256(archive)
	manager := NewCoreManager(t.TempDir())
	manager.Run = func(_ context.Context, _ string, args ...string) ([]byte, error) {
		if len(args) != 1 || args[0] != "-v" {
			t.Fatalf("args = %#v", args)
		}
		return []byte("Mihomo Meta v1.19.29 linux amd64"), nil
	}
	manager.Now = func() time.Time { return time.Date(2026, 8, 4, 2, 0, 0, 0, time.UTC) }
	candidate := Candidate{Version: "v1.19.29", AssetName: "mihomo-linux-amd64-compatible-v1.19.29.gz", SHA256: hex.EncodeToString(digest[:]), Size: int64(len(archive)), Architecture: "amd64"}
	status, err := manager.installArchive(context.Background(), candidate, bytes.NewReader(archive))
	if err != nil {
		t.Fatal(err)
	}
	if !status.Installed || status.Version != candidate.Version {
		t.Fatalf("status = %#v", status)
	}
	stored, err := manager.Status()
	if err != nil {
		t.Fatal(err)
	}
	if stored.Version != candidate.Version || stored.BinaryPath == "" {
		t.Fatalf("stored = %#v", stored)
	}
	contents, err := os.ReadFile(stored.BinaryPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != "fake executable" {
		t.Fatalf("binary = %q", contents)
	}
}

func TestInstallArchiveRejectsDigestAndVersionMismatchWithoutPublishing(t *testing.T) {
	archive := gzipBytes(t, []byte("fake executable"))
	manager := NewCoreManager(t.TempDir())
	manager.Run = func(context.Context, string, ...string) ([]byte, error) { return []byte("Mihomo Meta v9.9.9"), nil }
	candidate := Candidate{Version: "v1.19.29", SHA256: strings.Repeat("0", 64), Size: int64(len(archive)), Architecture: "amd64"}
	if _, err := manager.installArchive(context.Background(), candidate, bytes.NewReader(archive)); err == nil {
		t.Fatal("digest mismatch accepted")
	}
	digest := sha256.Sum256(archive)
	candidate.SHA256 = hex.EncodeToString(digest[:])
	if _, err := manager.installArchive(context.Background(), candidate, bytes.NewReader(archive)); err == nil {
		t.Fatal("version mismatch accepted")
	}
	if status, err := manager.Status(); err != nil || status.Installed {
		t.Fatalf("status = %#v, %v", status, err)
	}
}

func gzipBytes(t *testing.T, contents []byte) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := gzip.NewWriter(&buffer)
	if _, err := writer.Write(contents); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}
