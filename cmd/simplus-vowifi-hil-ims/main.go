package main

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"net"
	"net/netip"
	"os"
	"syscall"
	"time"

	"github.com/leonfox28/simplus/internal/vowifihil"
)

type result struct {
	Stage                     string `json:"stage"`
	RequestSent               bool   `json:"requestSent"`
	ResponseReceived          bool   `json:"responseReceived"`
	Status                    int    `json:"status,omitempty"`
	MinExpires                uint32 `json:"minExpires,omitempty"`
	AKAAlgorithm              string `json:"akaAlgorithm,omitempty"`
	NonceValid                bool   `json:"nonceValid"`
	SecurityServerCandidates  int    `json:"securityServerCandidates"`
	UsableSecurityServer      bool   `json:"usableSecurityServer"`
	TransportOutcome          string `json:"transportOutcome,omitempty"`
	RequestBytes              int    `json:"requestBytes,omitempty"`
	AKAState                  string `json:"akaState,omitempty"`
	GMSecurityInstalled       bool   `json:"gmSecurityInstalled"`
	ProtectedRequestSent      bool   `json:"protectedRequestSent"`
	ProtectedResponseReceived bool   `json:"protectedResponseReceived"`
	ProtectedTransportOutcome string `json:"protectedTransportOutcome,omitempty"`
	ProtectedRequestBytes     int    `json:"protectedRequestBytes,omitempty"`
	ProtectedResponsePath     string `json:"protectedResponsePath,omitempty"`
	RegistrationStatus        int    `json:"registrationStatus,omitempty"`
	Registered                bool   `json:"registered"`
}

func main() {
	if os.Geteuid() != 0 {
		fatal("privilege")
	}
	syscall.Umask(0o077)
	flags := flag.NewFlagSet("simplus-vowifi-hil-ims", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	sourceValue := flags.String("source", "", "inner IPv4 address")
	pcscfValue := flags.String("pcscf", "", "P-CSCF IPv4 address")
	if err := flags.Parse(os.Args[1:]); err != nil || flags.NArg() != 0 {
		fatal("arguments")
	}
	source, sourceErr := netip.ParseAddr(*sourceValue)
	pcscf, pcscfErr := netip.ParseAddr(*pcscfValue)
	if sourceErr != nil || pcscfErr != nil || !validPrivateIPv4(source) || !validPrivateIPv4(pcscf) || source == pcscf {
		fatal("arguments")
	}

	output, ok := run(source, pcscf)
	writeResult(output)
	if !ok {
		os.Exit(1)
	}
}

func run(source, pcscf netip.Addr) (result, bool) {
	output := result{Stage: "sim-preflight"}
	preflightContext, cancelPreflight := context.WithTimeout(context.Background(), 25*time.Second)
	inspection, err := vowifihil.InspectML307AVOXI(preflightContext)
	cancelPreflight()
	if err != nil || len(inspection.IMSIdentity.PublicIdentities) != 1 {
		return output, false
	}

	unprotected, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IP(source.AsSlice()), Port: vowifihil.IMSSIPPort})
	if err != nil {
		output.Stage = "unprotected-port"
		return output, false
	}
	defer unprotected.Close()
	protectedClient, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IP(source.AsSlice())})
	if err != nil {
		output.Stage = "protected-client-port"
		return output, false
	}
	defer protectedClient.Close()
	protectedServer, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IP(source.AsSlice())})
	if err != nil {
		output.Stage = "protected-server-port"
		return output, false
	}
	defer protectedServer.Close()

	clientSPI, serverSPI, err := randomSPIPair()
	if err != nil {
		output.Stage = "random"
		return output, false
	}
	branch, err := randomToken()
	if err != nil {
		output.Stage = "random"
		return output, false
	}
	fromTag, err := randomToken()
	if err != nil {
		output.Stage = "random"
		return output, false
	}
	callToken, err := randomToken()
	if err != nil {
		output.Stage = "random"
		return output, false
	}
	contactUser, err := randomToken()
	if err != nil {
		output.Stage = "random"
		return output, false
	}
	wlanNodeID, err := randomHex(6)
	if err != nil {
		output.Stage = "random"
		return output, false
	}
	unprotectedPort := uint16(unprotected.LocalAddr().(*net.UDPAddr).Port)
	input := vowifihil.IMSInitialRegisterInput{
		Source: source, UnprotectedPort: unprotectedPort,
		ProtectedClientPort: uint16(protectedClient.LocalAddr().(*net.UDPAddr).Port),
		ProtectedServerPort: uint16(protectedServer.LocalAddr().(*net.UDPAddr).Port),
		ClientSPI:           clientSPI, ServerSPI: serverSPI,
		PrivateIdentity: inspection.IMSIdentity.PrivateIdentity,
		PublicIdentity:  inspection.IMSIdentity.PublicIdentities[0],
		HomeDomain:      inspection.IMSIdentity.HomeDomain,
		Branch:          branch, FromTag: fromTag, CallID: callToken + "@" + source.String(), ContactUser: contactUser,
		WLANNodeID: wlanNodeID,
	}
	packet, securityClient, err := vowifihil.BuildIMSInitialRegister(input)
	if err != nil {
		output.Stage = "register-build"
		return output, false
	}
	defer zero(packet)

	output.Stage = "initial-register"
	output.RequestBytes = len(packet)
	response, sent, transportOutcome := exchangeInitialRegister(unprotected, pcscf, packet)
	output.RequestSent = sent
	output.TransportOutcome = transportOutcome
	if transportOutcome != "response" {
		return output, false
	}
	defer zero(response)
	output.ResponseReceived = true
	summary, parseErr := vowifihil.ParseIMSInitialResponse(response, input.CallID, input.HomeDomain)
	output.Status = summary.Status
	output.MinExpires = summary.MinExpires
	output.AKAAlgorithm = summary.AKAAlgorithm
	output.NonceValid = summary.NonceValid
	output.SecurityServerCandidates = summary.SecurityServerCandidates
	output.UsableSecurityServer = summary.UsableSecurityServer
	if parseErr != nil || summary.Status != 401 {
		return output, false
	}

	challenge, err := vowifihil.ExtractIMSRegistrationChallenge(response, input.CallID, input.HomeDomain)
	if err != nil {
		output.Stage = "challenge-parse"
		return output, false
	}
	defer zero(challenge.RAND[:])
	defer zero(challenge.AUTN[:])
	authContext, cancelAuth := context.WithTimeout(context.Background(), 15*time.Second)
	material, akaState, err := vowifihil.AuthenticateIMSChallenge(authContext, inspection.Target, challenge)
	cancelAuth()
	output.AKAState = akaState
	if err != nil {
		vowifihil.DiscardIMSAKASynchronizationFailure(err)
		output.Stage = "ims-aka"
		return output, false
	}
	defer material.Destroy()

	tunnelContext, cancelTunnel := context.WithTimeout(context.Background(), 3*time.Second)
	tunnel, err := vowifihil.DiscoverEPDGTunnel(tunnelContext, source)
	cancelTunnel()
	if err != nil {
		output.Stage = "epdg-policy"
		return output, false
	}
	clientSecurity := vowifihil.IMSClientIPSecParameters{
		ClientSPI: input.ClientSPI, ServerSPI: input.ServerSPI,
		ProtectedClientPort: input.ProtectedClientPort, ProtectedServerPort: input.ProtectedServerPort,
	}
	installContext, cancelInstall := context.WithTimeout(context.Background(), 4*time.Second)
	installation, err := vowifihil.InstallIMSXFRM(installContext, source, pcscf, tunnel,
		clientSecurity, challenge.SecurityServer, &material)
	cancelInstall()
	if err != nil {
		output.Stage = "gm-security"
		return output, false
	}
	defer installation.Cleanup()
	output.GMSecurityInstalled = true

	protectedBranch, err := randomToken()
	if err != nil {
		output.Stage = "random"
		return output, false
	}
	cnonce, err := randomToken()
	if err != nil {
		output.Stage = "random"
		return output, false
	}
	protectedPacket, err := vowifihil.BuildIMSAuthenticatedRegister(input, challenge, material.RES,
		securityClient, protectedBranch, cnonce)
	if err != nil {
		output.Stage = "authenticated-register-build"
		return output, false
	}
	defer zero(protectedPacket)
	output.ProtectedRequestBytes = len(protectedPacket)
	status, sent, protectedOutcome, responsePath := exchangeAuthenticatedRegister(protectedClient, protectedServer,
		pcscf, challenge.SecurityServer, protectedPacket, input.CallID)
	output.ProtectedRequestSent = sent
	output.ProtectedTransportOutcome = protectedOutcome
	output.ProtectedResponsePath = responsePath
	if protectedOutcome != "response" {
		output.Stage = "authenticated-register"
		return output, false
	}
	output.ProtectedResponseReceived = true
	output.RegistrationStatus = status
	output.Registered = status >= 200 && status <= 299
	if !output.Registered {
		output.Stage = "authenticated-register"
		return output, false
	}
	output.Stage = "registered"
	return output, true
}

func exchangeInitialRegister(connection *net.UDPConn, pcscf netip.Addr, packet []byte) ([]byte, bool, string) {
	target := &net.UDPAddr{IP: net.IP(pcscf.AsSlice()), Port: vowifihil.IMSSIPPort}
	buffer := make([]byte, 64<<10)
	deadline := time.Now().Add(10 * time.Second)
	backoff := 500 * time.Millisecond
	for time.Now().Before(deadline) {
		if _, err := connection.WriteToUDP(packet, target); err != nil {
			return nil, false, classifySendError(err)
		}
		readUntil := time.Now().Add(backoff)
		if readUntil.After(deadline) {
			readUntil = deadline
		}
		if err := connection.SetReadDeadline(readUntil); err != nil {
			return nil, true, "read-setup"
		}
		for {
			count, sender, err := connection.ReadFromUDP(buffer)
			if timeout, ok := err.(net.Error); ok && timeout.Timeout() {
				break
			}
			if err != nil {
				return nil, true, "read"
			}
			if sender.IP.Equal(target.IP) && count > 0 {
				return append([]byte(nil), buffer[:count]...), true, "response"
			}
		}
		if backoff < 4*time.Second {
			backoff *= 2
		}
	}
	return nil, true, "timeout"
}

func exchangeAuthenticatedRegister(client, server *net.UDPConn, pcscf netip.Addr,
	security vowifihil.IMSIPSecParameters, packet []byte, callID string) (int, bool, string, string) {
	target := &net.UDPAddr{IP: net.IP(pcscf.AsSlice()), Port: int(security.ProtectedServerPort)}
	buffer := make([]byte, 64<<10)
	defer zero(buffer)
	deadline := time.Now().Add(15 * time.Second)
	backoff := 500 * time.Millisecond
	nextSend := time.Now()
	sent := false
	for time.Now().Before(deadline) {
		if !time.Now().Before(nextSend) {
			if _, err := client.WriteToUDP(packet, target); err != nil {
				return 0, sent, classifySendError(err), ""
			}
			sent = true
			nextSend = time.Now().Add(backoff)
			if backoff < 4*time.Second {
				backoff *= 2
			}
		}
		for _, candidate := range []struct {
			connection *net.UDPConn
			path       string
		}{
			{connection: client, path: "client"},
			{connection: server, path: "server"},
		} {
			readUntil := time.Now().Add(125 * time.Millisecond)
			if readUntil.After(nextSend) {
				readUntil = nextSend
			}
			if readUntil.After(deadline) {
				readUntil = deadline
			}
			if err := candidate.connection.SetReadDeadline(readUntil); err != nil {
				return 0, sent, "read-setup", ""
			}
			count, sender, err := candidate.connection.ReadFromUDP(buffer)
			if timeout, ok := err.(net.Error); ok && timeout.Timeout() {
				continue
			}
			if err != nil {
				return 0, sent, "read", ""
			}
			validPort := sender.Port == int(security.ProtectedServerPort) ||
				sender.Port == int(security.ProtectedClientPort)
			if !sender.IP.Equal(target.IP) || !validPort || count == 0 {
				zero(buffer[:count])
				continue
			}
			status, parseErr := vowifihil.ParseIMSAuthenticatedResponse(buffer[:count], callID)
			zero(buffer[:count])
			if parseErr != nil || status < 200 {
				continue
			}
			return status, sent, "response", candidate.path
		}
	}
	return 0, sent, "timeout", ""
}

func classifySendError(err error) string {
	switch {
	case errors.Is(err, syscall.ENETUNREACH), errors.Is(err, syscall.EHOSTUNREACH):
		return "send-unreachable"
	case errors.Is(err, syscall.EADDRNOTAVAIL):
		return "send-address"
	case errors.Is(err, syscall.EACCES), errors.Is(err, syscall.EPERM), errors.Is(err, syscall.ENOKEY):
		return "send-policy"
	default:
		return "send"
	}
}

func randomSPIPair() (uint32, uint32, error) {
	first, err := randomSPI()
	if err != nil {
		return 0, 0, err
	}
	for {
		second, err := randomSPI()
		if err != nil {
			return 0, 0, err
		}
		if second != first {
			return first, second, nil
		}
	}
}

func randomSPI() (uint32, error) {
	var value [4]byte
	if _, err := rand.Read(value[:]); err != nil {
		return 0, err
	}
	const minimum = uint64(1_000_000_000)
	span := uint64(^uint32(0)) - minimum + 1
	return uint32(minimum + uint64(binary.BigEndian.Uint32(value[:]))%span), nil
}

func randomToken() (string, error) {
	return randomHex(8)
}

func randomHex(size int) (string, error) {
	value := make([]byte, size)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	defer zero(value)
	return hex.EncodeToString(value), nil
}

func validPrivateIPv4(address netip.Addr) bool {
	return address.Is4() && address.IsPrivate() && !address.IsLoopback() && !address.IsUnspecified()
}

func writeResult(value result) {
	_ = json.NewEncoder(os.Stdout).Encode(value)
}

func fatal(stage string) {
	writeResult(result{Stage: stage})
	os.Exit(1)
}

func zero(value []byte) {
	for index := range value {
		value[index] = 0
	}
}
