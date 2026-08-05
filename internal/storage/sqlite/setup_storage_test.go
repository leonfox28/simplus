package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestSetupStoragePersistsStableDirectoryIdentities(t *testing.T) {
	ctx := context.Background()
	set, err := OpenSet(ctx, filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer set.Close()
	dataRoot, recordingsRoot, _, _, _, _, configured, err := set.ReadSetupStorage(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if configured || dataRoot != set.Root || recordingsRoot != "" {
		t.Fatalf("initial setup storage = data %q recordings %q configured %t", dataRoot, recordingsRoot, configured)
	}

	recordingsRoot = filepath.Join(filepath.Dir(set.Root), "recordings")
	if err := set.ConfigureSetupStorage(ctx, set.Root, recordingsRoot, 1, 2, 3, 4, time.Now()); err != nil {
		t.Fatal(err)
	}
	dataRoot, storedRecordingsRoot, dataDevice, dataInode, recordingsDevice, recordingsInode, configured, err := set.ReadSetupStorage(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !configured || dataRoot != set.Root || storedRecordingsRoot != recordingsRoot || dataDevice != 1 || dataInode != 2 || recordingsDevice != 3 || recordingsInode != 4 {
		t.Fatalf("persisted setup storage = %q %q %d %d %d %d %t", dataRoot, storedRecordingsRoot, dataDevice, dataInode, recordingsDevice, recordingsInode, configured)
	}
}

func TestSetupStorageRejectsDifferentDataRootOrReadyState(t *testing.T) {
	ctx := context.Background()
	set, err := OpenSet(ctx, filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer set.Close()
	if err := set.ConfigureSetupStorage(ctx, "/different", "/recordings", 1, 2, 3, 4, time.Now()); err == nil {
		t.Fatal("different data root was accepted")
	}
	if _, err := set.Core.ExecContext(ctx, `UPDATE installation_state SET state = 'ready' WHERE singleton = 1`); err != nil {
		t.Fatal(err)
	}
	if err := set.ConfigureSetupStorage(ctx, set.Root, "/recordings", 1, 2, 3, 4, time.Now()); err == nil {
		t.Fatal("ready instance storage update was accepted")
	}
}
