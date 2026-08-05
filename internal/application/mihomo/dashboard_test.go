package mihomo

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDashboardManagerCreatesStablePrivateSecretAndReportsInstalledUI(t *testing.T) {
	root := t.TempDir()
	ui := filepath.Join(root, "runtime", "ui")
	if err := os.MkdirAll(ui, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ui, "index.html"), []byte("zashboard"), 0o600); err != nil {
		t.Fatal(err)
	}
	manager := NewDashboardManager(root, "192.168.50.10:19090")
	first, err := manager.Ensure()
	if err != nil {
		t.Fatal(err)
	}
	second, err := manager.Ensure()
	if err != nil {
		t.Fatal(err)
	}
	if !first.Available || first.URL != "http://192.168.50.10:19090/ui/" || first.Secret != second.Secret || !dashboardSecretPattern.MatchString(first.Secret) {
		t.Fatalf("first=%#v second=%#v", first, second)
	}
	info, err := os.Stat(filepath.Join(root, "controller-secret"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("secret mode=%v err=%v", info.Mode().Perm(), err)
	}
}

func TestDashboardManagerAcceptsIPv4WildcardController(t *testing.T) {
	root := t.TempDir()
	ui := filepath.Join(root, "runtime", "ui")
	if err := os.MkdirAll(ui, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ui, "index.html"), []byte("zashboard"), 0o600); err != nil {
		t.Fatal(err)
	}
	status, err := NewDashboardManager(root, "0.0.0.0:19090").Ensure()
	if err != nil || !status.Available || status.URL != "http://0.0.0.0:19090/ui/" {
		t.Fatalf("status=%#v err=%v", status, err)
	}
}

func TestDashboardManagerRejectsPublicIPv6WildcardOrUnexpectedPort(t *testing.T) {
	for _, address := range []string{"[::]:19090", "8.8.8.8:19090", "192.168.50.10:9090"} {
		if _, err := NewDashboardManager(t.TempDir(), address).Ensure(); err == nil {
			t.Fatalf("accepted controller %q", address)
		}
	}
}
