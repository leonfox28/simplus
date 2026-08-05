package mihomo

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"net"
	"os"
	"path/filepath"
	"regexp"
)

const ZashboardVersion = "v3.6.0"

var dashboardSecretPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)

type DashboardStatus struct {
	Available         bool
	Version           string
	ControllerAddress string
	URL               string
	Secret            string
}

type DashboardManager struct {
	Root              string
	ControllerAddress string
}

func NewDashboardManager(root, controllerAddress string) *DashboardManager {
	if controllerAddress == "" {
		controllerAddress = "127.0.0.1:19090"
	}
	return &DashboardManager{Root: root, ControllerAddress: controllerAddress}
}

func (manager *DashboardManager) Ensure() (DashboardStatus, error) {
	if manager == nil || !filepath.IsAbs(manager.Root) || !validControllerAddress(manager.ControllerAddress) {
		return DashboardStatus{}, errors.New("Mihomo dashboard configuration is invalid")
	}
	if err := os.MkdirAll(manager.Root, 0o700); err != nil {
		return DashboardStatus{}, err
	}
	secretPath := filepath.Join(manager.Root, "controller-secret")
	secretBody, err := os.ReadFile(secretPath)
	if os.IsNotExist(err) {
		secretBytes := make([]byte, 32)
		if _, err = rand.Read(secretBytes); err != nil {
			return DashboardStatus{}, err
		}
		secretBody = []byte(base64.RawURLEncoding.EncodeToString(secretBytes))
		if err = writePrivateFile(secretPath, secretBody); err != nil && !os.IsExist(err) {
			return DashboardStatus{}, err
		}
		if os.IsExist(err) {
			secretBody, err = os.ReadFile(secretPath)
		}
	}
	secret := string(secretBody)
	if err != nil || !dashboardSecretPattern.MatchString(secret) {
		return DashboardStatus{}, errors.New("Mihomo controller secret is invalid")
	}
	status := DashboardStatus{Version: ZashboardVersion, ControllerAddress: manager.ControllerAddress, Secret: secret}
	host, _, _ := net.SplitHostPort(manager.ControllerAddress)
	if !net.ParseIP(host).IsLoopback() {
		status.URL = "http://" + manager.ControllerAddress + "/ui/"
	}
	if info, statErr := os.Stat(filepath.Join(manager.Root, "runtime", "ui", "index.html")); statErr == nil && info.Mode().IsRegular() {
		status.Available = status.URL != ""
	}
	return status, nil
}

func validControllerAddress(address string) bool {
	host, port, err := net.SplitHostPort(address)
	if err != nil || port != "19090" {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && !ip.IsUnspecified() && (ip.IsLoopback() || ip.IsPrivate())
}
