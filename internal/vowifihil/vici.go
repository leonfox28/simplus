package vowifihil

import (
	"context"
	"errors"
	"iter"
	"path/filepath"
	"strings"

	"github.com/strongswan/govici/vici"
)

var requiredPlugins = []string{"eap-aka", "p-cscf", "simplus-simaka"}

var (
	ErrVICIUnavailable            = errors.New("bounded VICI socket is unavailable")
	ErrRequiredPluginsUnavailable = errors.New("required strongSwan plugins are unavailable")
	ErrConnectionLoadFailed       = errors.New("bounded strongSwan connection load failed")
	ErrConnectionVerifyFailed     = errors.New("bounded strongSwan connection verification failed")
	ErrConnectionInitiateFailed   = errors.New("bounded strongSwan connection initiation failed")
)

const vodafoneEPDGIdentity = "fqdn:*.epdg.om.epc.mnc015.mcc234.3gppnetwork.org"

type viciConnection struct {
	Version       int                     `vici:"version"`
	RemoteAddrs   []string                `vici:"remote_addrs"`
	Proposals     []string                `vici:"proposals"`
	Encap         bool                    `vici:"encap"`
	Mobike        bool                    `vici:"mobike"`
	Fragmentation bool                    `vici:"fragmentation"`
	SendCertReq   bool                    `vici:"send_certreq"`
	DPDDelay      string                  `vici:"dpd_delay"`
	Unique        string                  `vici:"unique"`
	Local         *viciLocalAuth          `vici:"local"`
	Remote        *viciRemoteAuth         `vici:"remote"`
	VIPs          []string                `vici:"vips"`
	Children      map[string]*viciChildSA `vici:"children"`
}

type viciLocalAuth struct {
	Auth  string `vici:"auth"`
	ID    string `vici:"id"`
	EAPID string `vici:"eap_id"`
}

type viciRemoteAuth struct {
	Auth string `vici:"auth"`
	ID   string `vici:"id"`
}

type viciChildSA struct {
	Mode         string   `vici:"mode"`
	LocalTS      []string `vici:"local_ts"`
	RemoteTS     []string `vici:"remote_ts"`
	ESPProposals []string `vici:"esp_proposals"`
	StartAction  string   `vici:"start_action"`
	DPDAction    string   `vici:"dpd_action"`
}

func RequiredPlugins() []string {
	return append([]string(nil), requiredPlugins...)
}

func Initiate(ctx context.Context, socketPath string, input ConnectionInput) error {
	if !filepath.IsAbs(socketPath) || !strings.HasPrefix(filepath.Clean(socketPath), "/run/") {
		return errors.New("invalid VICI socket path")
	}
	session, err := vici.NewSession(vici.WithSocketPath(filepath.Clean(socketPath)))
	if err != nil {
		return ErrVICIUnavailable
	}
	defer session.Close()
	stats, err := session.Call(ctx, "stats", nil)
	if err != nil || !messageHasRequiredPlugins(stats) {
		return ErrRequiredPluginsUnavailable
	}
	connection, err := ConnectionMessage(input)
	if err != nil {
		return err
	}
	if _, err := session.Call(ctx, "load-conn", connection); err != nil {
		return ErrConnectionLoadFailed
	}
	known, err := session.Call(ctx, "get-conns", nil)
	if err != nil || !messageHasString(known, "conns", ConnectionName) {
		return ErrConnectionVerifyFailed
	}
	initiate := vici.NewMessage()
	if initiate.Set("ike", ConnectionName) != nil || initiate.Set("child", "ims") != nil ||
		initiate.Set("timeout", 35000) != nil || initiate.Set("loglevel", 0) != nil {
		return errors.New("encode bounded strongSwan initiation")
	}
	return consumeControlLog(session.CallStreaming(ctx, "initiate", "control-log", initiate))
}

func consumeControlLog(events iter.Seq2[*vici.Message, error]) error {
	for _, eventErr := range events {
		if eventErr != nil {
			return ErrConnectionInitiateFailed
		}
	}
	return nil
}

func messageHasRequiredPlugins(message *vici.Message) bool {
	if message == nil {
		return false
	}
	plugins, ok := message.Get("plugins").([]string)
	if !ok {
		return false
	}
	for _, required := range requiredPlugins {
		found := false
		for _, plugin := range plugins {
			if plugin == required {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func messageHasString(message *vici.Message, key, expected string) bool {
	if message == nil {
		return false
	}
	values, ok := message.Get(key).([]string)
	if !ok {
		return false
	}
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func ConnectionMessage(input ConnectionInput) (*vici.Message, error) {
	if input.Version != 1 || !eapIdentity.MatchString(input.Identity) {
		return nil, errors.New("invalid transient VICI input")
	}
	connection := &viciConnection{
		Version:       2,
		RemoteAddrs:   []string{"88.82.11.221", "88.82.11.208", "148.252.188.96"},
		Proposals:     []string{"aes256-sha256-modp2048", "aes128-sha256-modp2048", "aes256-sha1-modp2048", "aes128-sha1-modp2048", "aes256-sha1-modp1024", "aes128-sha1-modp1024"},
		Encap:         true,
		Mobike:        false,
		Fragmentation: true,
		SendCertReq:   false,
		DPDDelay:      "20s",
		Unique:        "replace",
		Local: &viciLocalAuth{
			Auth: "eap-aka", ID: "userfqdn:" + input.Identity, EAPID: "userfqdn:" + input.Identity,
		},
		// Vodafone completes mutual authentication with EAP-only AUTH.  A
		// Vodafone-domain wildcard constrains the responder identity while also
		// preventing strongSwan from sending that wildcard as IDr.  The ePDG
		// already selects the IMS service from the operator-specific endpoint.
		Remote: &viciRemoteAuth{Auth: "eap", ID: vodafoneEPDGIdentity},
		VIPs:   []string{"0.0.0.0"},
		Children: map[string]*viciChildSA{
			"ims": {
				Mode: "tunnel", LocalTS: []string{"dynamic"}, RemoteTS: []string{"0.0.0.0/0"},
				ESPProposals: []string{"aes256-sha256", "aes128-sha256", "aes256-sha1", "aes128-sha1"},
				StartAction:  "none", DPDAction: "clear",
			},
		},
	}
	encoded, err := vici.MarshalMessage(connection)
	if err != nil {
		return nil, errors.New("encode bounded VICI connection")
	}
	message := vici.NewMessage()
	if err := message.Set(ConnectionName, encoded); err != nil {
		return nil, errors.New("encode bounded VICI connection")
	}
	return message, nil
}
