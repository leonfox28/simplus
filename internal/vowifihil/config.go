package vowifihil

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/leonfox28/simplus/internal/agentapi"
)

const (
	RunDirectory        = "/run/simplus-vowifi-hil"
	StrongSwanConfig    = RunDirectory + "/strongswan.conf"
	VICIConfig          = RunDirectory + "/vici.json"
	VICISocket          = RunDirectory + "/charon.vici"
	LogPipe             = RunDirectory + "/charon.pipe"
	SIMAKASocket        = "/run/simplus-agent/sim-aka.sock"
	ReadOnlyAgentSocket = "/run/simplus-agent/simplus-agent.sock"

	ConnectionName = "vowifi-ims"
	IMSAPN         = "ims"
	EPDGFQDN       = "epdg.epc.mnc015.mcc234.pub.3gppnetwork.org"
)

var (
	lowerHex64  = regexp.MustCompile(`^[0-9a-f]{64}$`)
	deviceID    = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,128}$`)
	imsi        = regexp.MustCompile(`^23415[0-9]{10}$`)
	eapIdentity = regexp.MustCompile(`^023415[0-9]{10}@nai\.epc\.mnc015\.mcc234\.3gppnetwork\.org$`)
)

// Input is the transient, root-only material needed to configure a single
// ML307A/VOXI EAP-AKA HIL attempt. The IMSI and derived identity must only be
// kept in memory or in the root-owned RunDirectory, which the runner removes.
type Input struct {
	Target agentapi.SIMAKATarget
	IMSI   string
}

type Config struct {
	StrongSwan []byte
	VICI       []byte
}

type RuntimePaths struct {
	RunDirectory     string
	StrongSwanConfig string
	VICISocket       string
	LogPipe          string
}

type ConnectionInput struct {
	Version  int    `json:"version"`
	Identity string `json:"identity"`
}

func Build(input Input) (Config, error) {
	return BuildAt(input, RuntimePaths{
		RunDirectory: RunDirectory, StrongSwanConfig: StrongSwanConfig,
		VICISocket: VICISocket, LogPipe: LogPipe,
	})
}

func PathsFor(runDirectory string) (RuntimePaths, error) {
	if !filepath.IsAbs(runDirectory) {
		return RuntimePaths{}, errors.New("runtime directory must be absolute")
	}
	runDirectory = filepath.Clean(runDirectory)
	if runDirectory == "/" || !strings.HasPrefix(runDirectory, "/run/") {
		return RuntimePaths{}, errors.New("runtime directory must be private tmpfs state")
	}
	return RuntimePaths{
		RunDirectory: runDirectory, StrongSwanConfig: filepath.Join(runDirectory, "strongswan.conf"),
		VICISocket: filepath.Join(runDirectory, "charon.vici"), LogPipe: filepath.Join(runDirectory, "charon.pipe"),
	}, nil
}

func BuildAt(input Input, paths RuntimePaths) (Config, error) {
	if err := validateInput(input); err != nil {
		return Config{}, err
	}
	expected, err := PathsFor(paths.RunDirectory)
	if err != nil || paths != expected {
		return Config{}, errors.New("invalid strongSwan runtime paths")
	}
	identity := "0" + input.IMSI + "@nai.epc.mnc015.mcc234.3gppnetwork.org"
	target := input.Target

	strongSwan := fmt.Sprintf(`charon {
    load_modular = yes
    install_routes = no
    install_virtual_ip = yes
    filelog {
        hil-pipe {
            path = %s
            append = no
            flush_line = yes
            ike_name = no
            default = 1
            ike = 2
            enc = 1
            net = 1
            knl = 1
            cfg = 1
            job = -1
        }
    }
    plugins {
        random { load = yes }
        nonce { load = yes }
        openssl { load = yes }
        sha1 { load = yes }
        sha2 { load = yes }
        aes { load = yes }
        fips-prf { load = yes }
        hmac { load = yes }
        kdf { load = yes }
        x509 { load = yes }
        pubkey { load = yes }
        pem { load = yes }
        pkcs1 { load = yes }
        pkcs8 { load = yes }
        revocation { load = yes }
        constraints { load = yes }
        kernel-netlink { load = yes }
        socket-default { load = yes }
        vici {
            load = yes
            socket = unix://%s
        }
        eap-identity { load = yes }
        eap-aka { load = yes }
        p-cscf {
            load = yes
            enable {
                %s = yes
            }
        }
        simplus-simaka {
            load = yes
            socket = %s
            agent_instance_id = %s
            snapshot_generation = %d
            snapshot_revision = %s
            device_id = %s
            device_generation = %d
            identity_fingerprint = %s
            expected_identity = %s
        }
    }
}

charon-systemd {
    journal {
        default = -1
    }
}
`, paths.LogPipe, paths.VICISocket, ConnectionName, SIMAKASocket, target.AgentInstanceID,
		target.SnapshotGeneration, target.SnapshotRevision, target.DeviceID,
		target.DeviceGeneration, target.IdentityFingerprint, identity)

	viciConfig, err := json.Marshal(ConnectionInput{Version: 1, Identity: identity})
	if err != nil {
		return Config{}, errors.New("encode transient VICI input")
	}
	viciConfig = append(viciConfig, '\n')
	return Config{StrongSwan: []byte(strongSwan), VICI: viciConfig}, nil
}

func ParseConnectionInput(data []byte) (ConnectionInput, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var input ConnectionInput
	if err := decoder.Decode(&input); err != nil {
		return ConnectionInput{}, errors.New("invalid transient VICI input")
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return ConnectionInput{}, errors.New("invalid transient VICI input")
	}
	if input.Version != 1 || !eapIdentity.MatchString(input.Identity) {
		return ConnectionInput{}, errors.New("invalid transient VICI input")
	}
	return input, nil
}

func validateInput(input Input) error {
	target := input.Target
	if !agentapi.IsValidAgentInstanceID(target.AgentInstanceID) ||
		target.SnapshotGeneration == 0 || !lowerHex64.MatchString(target.SnapshotRevision) ||
		!deviceID.MatchString(target.DeviceID) || target.DeviceGeneration == 0 ||
		!lowerHex64.MatchString(target.IdentityFingerprint) {
		return errors.New("invalid fenced SIM AKA target")
	}
	if !imsi.MatchString(input.IMSI) {
		return errors.New("active SIM is not the expected Vodafone UK/VOXI identity")
	}
	if strings.ContainsAny(target.DeviceID, "{}=\n\r\t ") {
		return errors.New("invalid fenced SIM AKA target")
	}
	return nil
}
