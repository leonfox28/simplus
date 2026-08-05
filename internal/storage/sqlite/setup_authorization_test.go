package sqlite

import (
	"context"
	"crypto/sha256"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
)

func TestSetupBootstrapConsumptionIsAtomicAndSessionSurvivesRestart(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "db")
	set, err := OpenSet(ctx, root)
	if err != nil {
		t.Fatal(err)
	}

	bootstrapHash := sha256.Sum256([]byte("bootstrap"))
	if err := set.ReplaceSetupBootstrap(ctx, bootstrapHash, 100, 200); err != nil {
		t.Fatal(err)
	}

	var consumedCount atomic.Int32
	var winningHash [32]byte
	var winnerMu sync.Mutex
	var wait sync.WaitGroup
	for index := range 8 {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			sessionHash := sha256.Sum256([]byte{byte(index + 1)})
			consumed, err := set.ConsumeSetupBootstrap(ctx, bootstrapHash, sessionHash, 110, 300)
			if err != nil {
				t.Errorf("ConsumeSetupBootstrap(%d): %v", index, err)
				return
			}
			if consumed {
				consumedCount.Add(1)
				winnerMu.Lock()
				winningHash = sessionHash
				winnerMu.Unlock()
			}
		}(index)
	}
	wait.Wait()
	if consumedCount.Load() != 1 {
		t.Fatalf("successful bootstrap consumptions = %d, want 1", consumedCount.Load())
	}

	if err := set.Close(); err != nil {
		t.Fatal(err)
	}
	set, err = OpenSet(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	defer set.Close()

	expires, flow, found, err := set.ReadSetupSession(ctx, winningHash, 120)
	if err != nil {
		t.Fatal(err)
	}
	if !found || expires != 300 || flow != "create-new" {
		t.Fatalf("session = found:%v expires:%d flow:%q", found, expires, flow)
	}

	replacement := sha256.Sum256([]byte("replacement"))
	if err := set.ReplaceSetupBootstrap(ctx, replacement, 130, 230); err != nil {
		t.Fatal(err)
	}
	if _, _, found, err := set.ReadSetupSession(ctx, winningHash, 131); err != nil {
		t.Fatal(err)
	} else if found {
		t.Fatal("generating a replacement bootstrap did not revoke prior setup sessions")
	}
}
