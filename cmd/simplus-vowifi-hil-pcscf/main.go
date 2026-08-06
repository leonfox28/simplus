package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/netip"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/leonfox28/simplus/internal/agentapi"
)

const (
	tcpTimeout = 2 * time.Second
	udpTimeout = 3 * time.Second
)

type targetList []string

func (values *targetList) String() string { return strings.Join(*values, ",") }

func (values *targetList) Set(value string) error {
	*values = append(*values, value)
	return nil
}

type result struct {
	Targets          int            `json:"targets"`
	TCP5060Open      int            `json:"tcp5060Open"`
	TCP5061Open      int            `json:"tcp5061Open"`
	UDP5060Responses int            `json:"udp5060Responses"`
	UDPStatusClasses map[string]int `json:"udpStatusClasses,omitempty"`
	Reachable        bool           `json:"reachable"`
}

func main() {
	if os.Geteuid() != 0 {
		fail("this HIL P-CSCF probe must run as root")
	}
	flags := flag.NewFlagSet("simplus-vowifi-hil-pcscf", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	sourceText := flags.String("source", "", "ePDG-assigned private IPv4 address")
	homeDomain := flags.String("home-domain", "", "SIM-provided IMS Home Domain")
	var targets targetList
	flags.Var(&targets, "target", "private P-CSCF IPv4 address; repeat up to four times")
	if err := flags.Parse(os.Args[1:]); err != nil || flags.NArg() != 0 {
		os.Exit(2)
	}
	source, err := privateIPv4(*sourceText)
	if err != nil || len(targets) == 0 || len(targets) > 4 || !agentapi.IsValidIMSHomeDomain(*homeDomain) {
		fail("invalid bounded P-CSCF probe input")
	}
	parsedTargets := make([]netip.Addr, 0, len(targets))
	seen := make(map[netip.Addr]struct{}, len(targets))
	for _, value := range targets {
		target, parseErr := privateIPv4(value)
		if parseErr != nil {
			fail("invalid bounded P-CSCF probe input")
		}
		if _, exists := seen[target]; exists {
			continue
		}
		seen[target] = struct{}{}
		parsedTargets = append(parsedTargets, target)
	}
	if len(parsedTargets) == 0 {
		fail("invalid bounded P-CSCF probe input")
	}

	state := result{Targets: len(parsedTargets), UDPStatusClasses: map[string]int{}}
	for _, target := range parsedTargets {
		if probeTCP(source, target, 5060) {
			state.TCP5060Open++
		}
		if probeTCP(source, target, 5061) {
			state.TCP5061Open++
		}
		if status, ok := probeUDPOptions(source, target, *homeDomain); ok {
			state.UDP5060Responses++
			state.UDPStatusClasses[strconv.Itoa(status/100)+"xx"]++
		}
	}
	state.Reachable = state.TCP5060Open != 0 || state.TCP5061Open != 0 || state.UDP5060Responses != 0
	if len(state.UDPStatusClasses) == 0 {
		state.UDPStatusClasses = nil
	}
	_ = json.NewEncoder(os.Stdout).Encode(state)
}

func privateIPv4(value string) (netip.Addr, error) {
	address, err := netip.ParseAddr(value)
	if err != nil || !address.Is4() || !address.IsPrivate() {
		return netip.Addr{}, errors.New("address must be a private IPv4 address")
	}
	return address.Unmap(), nil
}

func probeTCP(source, target netip.Addr, port uint16) bool {
	ctx, cancel := context.WithTimeout(context.Background(), tcpTimeout)
	defer cancel()
	dialer := net.Dialer{
		Timeout:   tcpTimeout,
		LocalAddr: &net.TCPAddr{IP: net.IP(source.AsSlice())},
	}
	connection, err := dialer.DialContext(ctx, "tcp4", net.JoinHostPort(target.String(), strconv.Itoa(int(port))))
	if err != nil {
		return false
	}
	_ = connection.Close()
	return true
}

func probeUDPOptions(source, target netip.Addr, homeDomain string) (int, bool) {
	connection, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IP(source.AsSlice())})
	if err != nil {
		return 0, false
	}
	defer connection.Close()
	_ = connection.SetDeadline(time.Now().Add(udpTimeout))
	local, ok := connection.LocalAddr().(*net.UDPAddr)
	if !ok {
		return 0, false
	}
	token := make([]byte, 12)
	if _, err := rand.Read(token); err != nil {
		return 0, false
	}
	id := hex.EncodeToString(token)
	request := fmt.Sprintf(
		"OPTIONS sip:%s SIP/2.0\r\n"+
			"Via: SIP/2.0/UDP %s:%d;rport;branch=z9hG4bK%s\r\n"+
			"Max-Forwards: 0\r\n"+
			"From: <sip:anonymous@anonymous.invalid>;tag=%s\r\n"+
			"To: <sip:%s>\r\n"+
			"Call-ID: %s@anonymous.invalid\r\n"+
			"CSeq: 1 OPTIONS\r\n"+
			"Contact: <sip:anonymous@%s:%d>\r\n"+
			"Content-Length: 0\r\n\r\n",
		homeDomain, source, local.Port, id, id, homeDomain, id, source, local.Port,
	)
	if _, err := connection.WriteToUDP([]byte(request), &net.UDPAddr{IP: net.IP(target.AsSlice()), Port: 5060}); err != nil {
		return 0, false
	}
	response := make([]byte, 2048)
	count, _, err := connection.ReadFromUDP(response)
	if err != nil {
		return 0, false
	}
	defer clear(response)
	return parseSIPStatus(response[:count])
}

func parseSIPStatus(response []byte) (int, bool) {
	line := response
	if index := strings.Index(string(line), "\r\n"); index >= 0 {
		line = line[:index]
	}
	fields := strings.Fields(string(line))
	if len(fields) < 2 || fields[0] != "SIP/2.0" {
		return 0, false
	}
	status, err := strconv.Atoi(fields[1])
	return status, err == nil && status >= 100 && status <= 699
}

func fail(message string) {
	fmt.Fprintln(os.Stderr, message)
	os.Exit(1)
}
