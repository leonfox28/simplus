package accesspath_test

import (
	"context"
	app "github.com/leonfox28/simplus/internal/application/accesspath"
	sqlitestore "github.com/leonfox28/simplus/internal/storage/sqlite"
	"path/filepath"
	"testing"
)

func TestMihomoRequiredFailsClosedWithoutDirectFallback(t *testing.T) {
	ctx := context.Background()
	stores, err := sqlitestore.OpenSet(ctx, filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer stores.Close()
	service, _ := app.New(stores)
	state, err := service.Configure(ctx, "simulator-line-1", "mihomo-required", "failed")
	if err != nil || state.LineState != "offline" || state.DirectFallback || state.EPDG != "blocked" {
		t.Fatalf("failed=%#v err=%v", state, err)
	}
	state, err = service.Configure(ctx, "simulator-line-1", "mihomo-required", "running")
	if err != nil || state.LineState != "online" || state.IMS != "registered" {
		t.Fatalf("running=%#v err=%v", state, err)
	}
}
