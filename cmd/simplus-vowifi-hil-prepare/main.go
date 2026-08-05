package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"syscall"
	"time"

	"github.com/leonfox28/simplus/internal/vowifihil"
)

func main() {
	if os.Geteuid() != 0 {
		fatal("this HIL preparer must run as root")
	}
	syscall.Umask(0o077)
	if _, err := os.Lstat(vowifihil.RunDirectory); !errors.Is(err, os.ErrNotExist) {
		fatal("the fixed HIL run directory already exists; clean it before retrying")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	inspection, err := vowifihil.InspectML307AVOXI(ctx)
	if err != nil {
		fatal("the ML307A/VOXI RF-off preflight failed")
	}
	config, err := vowifihil.Build(vowifihil.Input{Target: inspection.Target, IMSI: inspection.IMSI})
	if err != nil {
		fatal("the ML307A/VOXI identity does not satisfy the HIL profile")
	}

	if err := os.Mkdir(vowifihil.RunDirectory, 0o700); err != nil {
		fatal("create the fixed HIL run directory")
	}
	committed := false
	defer func() {
		if !committed {
			_ = os.RemoveAll(vowifihil.RunDirectory)
		}
	}()
	if err := writeExclusive(vowifihil.StrongSwanConfig, config.StrongSwan); err != nil {
		fatal("write the transient strongSwan config")
	}
	if err := writeExclusive(vowifihil.VICIConfig, config.VICI); err != nil {
		fatal("write the transient VICI config")
	}
	if err := syscall.Mkfifo(vowifihil.LogPipe, 0o600); err != nil {
		fatal("create the transient strongSwan redaction pipe")
	}
	committed = true

	result := struct {
		Prepared      bool   `json:"prepared"`
		Operator      string `json:"operator"`
		RF            string `json:"rf"`
		SIM           string `json:"sim"`
		APN           string `json:"apn"`
		EPDG          string `json:"epdg"`
		IMS           string `json:"imsIdentitySource"`
		IMPUCount     int    `json:"imsPublicIdentityCount"`
		IMSDiscovery  string `json:"imsApplicationDiscovery"`
		IMSCandidates int    `json:"imsApplicationCandidates"`
	}{true, "VOXI / Vodafone UK", "off", "ready", vowifihil.IMSAPN, vowifihil.EPDGFQDN,
		inspection.IMSIdentity.IdentitySource, len(inspection.IMSIdentity.PublicIdentities),
		inspection.IMSIdentity.ApplicationDiscovery, inspection.IMSIdentity.ApplicationCandidates}
	if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
		fatal("write the redacted HIL summary")
	}
}

func writeExclusive(path string, value []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := file.Write(value); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

func fatal(message string) {
	fmt.Fprintln(os.Stderr, message)
	os.Exit(1)
}
