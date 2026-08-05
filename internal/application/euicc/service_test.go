package euicc_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/leonfox28/simplus/internal/application/euicc"
	sqlitestore "github.com/leonfox28/simplus/internal/storage/sqlite"
)

func TestSwitchAtoBtoAPersistsAndReadsBack(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "db")
	stores, err := sqlitestore.OpenSet(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	service, _ := euicc.New(stores)
	state, err := service.State(ctx)
	if err != nil || !state.Profiles[0].Active {
		t.Fatalf("initial=%#v err=%v", state, err)
	}
	state, err = service.Switch(ctx, "simulator-euicc-profile-b")
	if err != nil || !state.Profiles[1].Active {
		t.Fatalf("B=%#v err=%v", state, err)
	}
	if err := stores.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := sqlitestore.OpenSet(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	restarted, _ := euicc.New(reopened)
	state, err = restarted.Switch(ctx, "simulator-euicc-profile-a")
	if err != nil || !state.Profiles[0].Active {
		t.Fatalf("A=%#v err=%v", state, err)
	}
}
