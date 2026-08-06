package vowifihil

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/netip"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

const (
	imsXFRMClientReqID = 6101
	imsXFRMServerReqID = 6102
	imsXFRMPriority    = 500
	imsUDPProtocol     = 17
)

type EPDGTunnelTemplate struct {
	Local  netip.Addr
	Remote netip.Addr
	ReqID  uint32
}

type IMSClientIPSecParameters struct {
	ClientSPI           uint32
	ServerSPI           uint32
	ProtectedClientPort uint16
	ProtectedServerPort uint16
}

func DiscoverEPDGTunnel(ctx context.Context, source netip.Addr) (EPDGTunnelTemplate, error) {
	if !validIMSPrivateIPv4(source) {
		return EPDGTunnelTemplate{}, errors.New("invalid IMS source address")
	}
	command := exec.CommandContext(ctx, "/usr/sbin/ip", "xfrm", "policy", "list", "dir", "out")
	output, err := command.Output()
	if err != nil || len(output) == 0 || len(output) > 1<<20 {
		zeroBytes(output)
		return EPDGTunnelTemplate{}, errors.New("ePDG XFRM policy is unavailable")
	}
	defer zeroBytes(output)
	return parseEPDGTunnelPolicy(string(output), source)
}

func parseEPDGTunnelPolicy(value string, source netip.Addr) (EPDGTunnelTemplate, error) {
	lines := strings.Split(value, "\n")
	selector := false
	direction := false
	var pendingLocal, pendingRemote netip.Addr
	var candidates []EPDGTunnelTemplate
	for _, raw := range lines {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			continue
		}
		if len(raw) > 0 && raw[0] != ' ' && raw[0] != '\t' {
			selector = selectorMatchesEPDG(trimmed, source)
			direction = false
			pendingLocal = netip.Addr{}
			pendingRemote = netip.Addr{}
			continue
		}
		if !selector {
			continue
		}
		if strings.HasPrefix(trimmed, "dir ") {
			direction = strings.HasPrefix(trimmed, "dir out ") || trimmed == "dir out"
			continue
		}
		if !direction {
			continue
		}
		if strings.HasPrefix(trimmed, "tmpl src ") {
			fields := strings.Fields(trimmed)
			if len(fields) == 5 && fields[0] == "tmpl" && fields[1] == "src" && fields[3] == "dst" {
				pendingLocal, _ = netip.ParseAddr(fields[2])
				pendingRemote, _ = netip.ParseAddr(fields[4])
			}
			continue
		}
		if pendingLocal.IsValid() && pendingRemote.IsValid() && strings.HasPrefix(trimmed, "proto esp ") &&
			strings.Contains(trimmed, " mode tunnel") {
			fields := strings.Fields(trimmed)
			var reqID uint64
			for index := 0; index+1 < len(fields); index++ {
				if fields[index] == "reqid" {
					reqID, _ = strconv.ParseUint(fields[index+1], 10, 32)
					break
				}
			}
			candidate := EPDGTunnelTemplate{Local: pendingLocal, Remote: pendingRemote, ReqID: uint32(reqID)}
			if validEPDGTunnelTemplate(candidate, source) {
				candidates = append(candidates, candidate)
			}
			pendingLocal = netip.Addr{}
			pendingRemote = netip.Addr{}
		}
	}
	if len(candidates) != 1 {
		return EPDGTunnelTemplate{}, errors.New("expected one ePDG tunnel policy")
	}
	return candidates[0], nil
}

func selectorMatchesEPDG(value string, source netip.Addr) bool {
	fields := strings.Fields(value)
	if len(fields) < 4 || fields[0] != "src" || fields[2] != "dst" {
		return false
	}
	sourcePrefix, err := netip.ParsePrefix(fields[1])
	if err != nil {
		address, addressErr := netip.ParseAddr(fields[1])
		if addressErr != nil || address != source {
			return false
		}
	} else if sourcePrefix.Bits() != 32 || sourcePrefix.Addr() != source {
		return false
	}
	destination, err := netip.ParsePrefix(fields[3])
	return err == nil && destination == netip.MustParsePrefix("0.0.0.0/0")
}

func validEPDGTunnelTemplate(value EPDGTunnelTemplate, source netip.Addr) bool {
	if !value.Local.Is4() || !value.Remote.Is4() || value.Local == value.Remote || value.ReqID == 0 ||
		value.ReqID == imsXFRMClientReqID || value.ReqID == imsXFRMServerReqID || value.ReqID > 1_000_000 ||
		value.Local == source {
		return false
	}
	for _, allowed := range []string{"88.82.11.221", "88.82.11.208", "148.252.188.96"} {
		if value.Remote == netip.MustParseAddr(allowed) {
			return true
		}
	}
	return false
}

type IMSXFRMInstallation struct {
	cleanup []byte
}

func (installation *IMSXFRMInstallation) Cleanup() {
	if installation == nil || len(installation.cleanup) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = runIPBatch(ctx, installation.cleanup, true)
	zeroBytes(installation.cleanup)
	installation.cleanup = nil
}

func InstallIMSXFRM(ctx context.Context, source, pcscf netip.Addr, tunnel EPDGTunnelTemplate,
	client IMSClientIPSecParameters, server IMSIPSecParameters, material *IMSAKAMaterial) (*IMSXFRMInstallation, error) {
	if !validIMSPrivateIPv4(source) || !validIMSPrivateIPv4(pcscf) || source == pcscf ||
		!validEPDGTunnelTemplate(tunnel, source) || !validIMSClientIPSec(client) ||
		server.Authentication != "hmac-sha-1-96" || server.Encryption != "aes-cbc" ||
		server.Protocol != "esp" || server.Mode != "trans" || material == nil ||
		len(material.RES) < 4 || len(material.RES) > 16 ||
		!uniqueSPIs(client.ClientSPI, client.ServerSPI, server.ClientSPI, server.ServerSPI) {
		return nil, errors.New("invalid IMS XFRM input")
	}
	if err := cleanupReservedIMSXFRM(ctx); err != nil {
		return nil, err
	}

	batch := buildIMSXFRMInstall(source, pcscf, tunnel, client, server, material)
	cleanup := buildIMSXFRMCleanup(source, pcscf, client, server)
	installation := &IMSXFRMInstallation{cleanup: cleanup}
	if err := runIPBatch(ctx, batch, false); err != nil {
		zeroBytes(batch)
		installation.Cleanup()
		return nil, err
	}
	zeroBytes(batch)
	return installation, nil
}

func buildIMSXFRMInstall(source, pcscf netip.Addr, tunnel EPDGTunnelTemplate,
	client IMSClientIPSecParameters, server IMSIPSecParameters, material *IMSAKAMaterial) []byte {
	var install bytes.Buffer
	writeIMSState := func(from, to netip.Addr, spi, reqID uint32) {
		fmt.Fprintf(&install, "xfrm state add src %s dst %s proto esp spi 0x%08x reqid %d mode transport replay-window 32 auth-trunc hmac(sha1) 0x%x00000000 96 enc cbc(aes) 0x%x\n",
			from, to, spi, reqID, material.IK[:], material.CK[:])
	}
	writeIMSState(source, pcscf, server.ServerSPI, imsXFRMClientReqID)
	writeIMSState(pcscf, source, client.ClientSPI, imsXFRMClientReqID)
	writeIMSState(source, pcscf, server.ClientSPI, imsXFRMServerReqID)
	writeIMSState(pcscf, source, client.ServerSPI, imsXFRMServerReqID)

	writeIMSOutPolicy := func(sourcePort, destinationPort uint16, spi, reqID uint32) {
		fmt.Fprintf(&install, "xfrm policy add src %s/32 dst %s/32 proto %d sport %d dport %d dir out priority %d ",
			source, pcscf, imsUDPProtocol, sourcePort, destinationPort, imsXFRMPriority)
		fmt.Fprintf(&install, "tmpl src %s dst %s proto esp spi 0x%08x reqid %d mode transport level required ",
			source, pcscf, spi, reqID)
		fmt.Fprintf(&install, "tmpl src %s dst %s proto esp reqid %d mode tunnel level required\n",
			tunnel.Local, tunnel.Remote, tunnel.ReqID)
	}
	writeIMSInPolicy := func(sourcePort, destinationPort uint16, spi, reqID uint32) {
		fmt.Fprintf(&install, "xfrm policy add src %s/32 dst %s/32 proto %d sport %d dport %d dir in priority %d ",
			pcscf, source, imsUDPProtocol, sourcePort, destinationPort, imsXFRMPriority)
		fmt.Fprintf(&install, "tmpl src %s dst %s proto esp spi 0x%08x reqid %d mode transport level required ",
			pcscf, source, spi, reqID)
		fmt.Fprintf(&install, "tmpl src %s dst %s proto esp reqid %d mode tunnel level required\n",
			tunnel.Remote, tunnel.Local, tunnel.ReqID)
	}
	writeIMSOutPolicy(client.ProtectedClientPort, server.ProtectedServerPort, server.ServerSPI, imsXFRMClientReqID)
	writeIMSInPolicy(server.ProtectedServerPort, client.ProtectedClientPort, client.ClientSPI, imsXFRMClientReqID)
	writeIMSOutPolicy(client.ProtectedServerPort, server.ProtectedClientPort, server.ClientSPI, imsXFRMServerReqID)
	writeIMSInPolicy(server.ProtectedClientPort, client.ProtectedServerPort, client.ServerSPI, imsXFRMServerReqID)
	return append([]byte(nil), install.Bytes()...)
}

func buildIMSXFRMCleanup(source, pcscf netip.Addr, client IMSClientIPSecParameters, server IMSIPSecParameters) []byte {
	var cleanup bytes.Buffer
	writePolicy := func(from, to netip.Addr, sourcePort, destinationPort uint16, direction string) {
		fmt.Fprintf(&cleanup, "xfrm policy delete src %s/32 dst %s/32 proto %d sport %d dport %d dir %s\n",
			from, to, imsUDPProtocol, sourcePort, destinationPort, direction)
	}
	writePolicy(source, pcscf, client.ProtectedClientPort, server.ProtectedServerPort, "out")
	writePolicy(pcscf, source, server.ProtectedServerPort, client.ProtectedClientPort, "in")
	writePolicy(source, pcscf, client.ProtectedServerPort, server.ProtectedClientPort, "out")
	writePolicy(pcscf, source, server.ProtectedClientPort, client.ProtectedServerPort, "in")
	for _, state := range []struct {
		from, to netip.Addr
		spi      uint32
	}{
		{source, pcscf, server.ServerSPI}, {pcscf, source, client.ClientSPI},
		{source, pcscf, server.ClientSPI}, {pcscf, source, client.ServerSPI},
	} {
		fmt.Fprintf(&cleanup, "xfrm state delete src %s dst %s proto esp spi 0x%08x\n", state.from, state.to, state.spi)
	}
	cleanup.Write(reservedIMSXFRMCleanupBatch())
	return append([]byte(nil), cleanup.Bytes()...)
}

func cleanupReservedIMSXFRM(ctx context.Context) error {
	return runIPBatch(ctx, reservedIMSXFRMCleanupBatch(), true)
}

func reservedIMSXFRMCleanupBatch() []byte {
	return []byte(fmt.Sprintf("xfrm policy deleteall priority %d\nxfrm state deleteall reqid %d\nxfrm state deleteall reqid %d\n",
		imsXFRMPriority, imsXFRMClientReqID, imsXFRMServerReqID))
}

func runIPBatch(ctx context.Context, batch []byte, force bool) error {
	arguments := []string{"-batch", "-"}
	if force {
		arguments = []string{"-force", "-batch", "-"}
	}
	command := exec.CommandContext(ctx, "/usr/sbin/ip", arguments...)
	command.Stdin = bytes.NewReader(batch)
	var output bytes.Buffer
	command.Stdout = &output
	command.Stderr = &output
	err := command.Run()
	zeroBytes(output.Bytes())
	if err != nil {
		return errors.New("install transient IMS XFRM state")
	}
	return nil
}

func validIMSClientIPSec(value IMSClientIPSecParameters) bool {
	return value.ClientSPI != 0 && value.ServerSPI != 0 && value.ClientSPI != value.ServerSPI &&
		value.ProtectedClientPort != 0 && value.ProtectedServerPort != 0 &&
		value.ProtectedClientPort != value.ProtectedServerPort &&
		value.ProtectedClientPort != IMSSIPPort && value.ProtectedServerPort != IMSSIPPort
}

func uniqueSPIs(values ...uint32) bool {
	seen := make(map[uint32]struct{}, len(values))
	for _, value := range values {
		if value == 0 {
			return false
		}
		if _, exists := seen[value]; exists {
			return false
		}
		seen[value] = struct{}{}
	}
	return true
}

func validIMSPrivateIPv4(address netip.Addr) bool {
	return address.Is4() && address.IsPrivate() && !address.IsLoopback() && !address.IsUnspecified()
}
