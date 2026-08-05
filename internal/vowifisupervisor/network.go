package vowifisupervisor

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/leonfox28/simplus/internal/domain/lineegress"
)

var (
	hardwareLinePattern     = regexp.MustCompile(`^agent-line-[0-9a-f]{32}$`)
	countryPattern          = regexp.MustCompile(`^[A-Z]{2}$`)
	networkTokenPattern     = regexp.MustCompile(`^[0-9a-f]{12}$`)
	errNetworkObjectMissing = errors.New("Host VoWiFi network object is absent")
	errNetworkCleanupFailed = errors.New("Host VoWiFi network cleanup failed")
)

type commandRunner interface {
	Run(context.Context, []byte, string, ...string) error
}

type execRunner struct{}

func (execRunner) Run(ctx context.Context, input []byte, name string, args ...string) error {
	command := exec.CommandContext(ctx, name, args...)
	command.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LC_ALL=C"}
	if input != nil {
		command.Stdin = bytes.NewReader(input)
	}
	var output bytes.Buffer
	command.Stdout = &output
	command.Stderr = &output
	err := command.Run()
	missing := networkObjectMissing(output.String())
	for index := range output.Bytes() {
		output.Bytes()[index] = 0
	}
	if err != nil {
		if missing {
			return errNetworkObjectMissing
		}
		return fmt.Errorf("fixed network operation failed: %w", err)
	}
	return nil
}

func networkObjectMissing(output string) bool {
	output = strings.ToLower(output)
	for _, token := range []string{"no such file or directory", "cannot find device", "no such process", "does not exist"} {
		if strings.Contains(output, token) {
			return true
		}
	}
	return false
}

type networkPlan struct {
	Version       int    `json:"version"`
	LineID        string `json:"lineId"`
	Token         string `json:"token"`
	Namespace     string `json:"namespace"`
	HostInterface string `json:"hostInterface"`
	PeerInterface string `json:"peerInterface"`
	HostAddress   string `json:"hostAddress"`
	PeerAddress   string `json:"peerAddress"`
	Prefix        string `json:"prefix"`
	TableName     string `json:"tableName"`
	RouteTable    int    `json:"routeTable"`
	RulePriority  int    `json:"rulePriority"`
	Mark          int    `json:"mark"`
	EgressMode    string `json:"egressMode"`
	CountryCode   string `json:"countryCode"`
	ListenerPort  int    `json:"listenerPort"`
}

func buildNetworkPlan(request StartRequest) (networkPlan, error) {
	if !validStartRequest(request) {
		return networkPlan{}, ErrRequestInvalid
	}
	digest := sha256.Sum256([]byte(request.LineID))
	token := hex.EncodeToString(digest[:6])
	short := token[:8]
	// Keep the private veth transit identical in kind to the accepted HIL
	// topology. RFC1918 source addresses affected transparent NAT-T reply
	// delivery on IKE_AUTH even though IKE_SA_INIT appeared healthy. Reserve
	// the first and last IPv4LL /24s and deterministically allocate a /30.
	index := int(binary.BigEndian.Uint16(digest[6:8])) % (254 * 64)
	third, fourth := 1+index/64, (index%64)*4
	prefix := netip.PrefixFrom(netip.AddrFrom4([4]byte{169, 254, byte(third), byte(fourth)}), 30)
	host := prefix.Addr().Next()
	peer := host.Next()
	routeSlot := int(binary.BigEndian.Uint16(digest[8:10])) % 1000
	mark := 0x6000 + int(binary.BigEndian.Uint16(digest[10:12])&0x0fff)
	port := 0
	if request.EgressMode == EgressMihomoCountry {
		port = lineegress.CountryListenerPort(request.CountryCode)
	}
	return networkPlan{
		Version: 1, LineID: request.LineID, Token: token,
		Namespace: "svw-" + token, HostInterface: "svh" + short, PeerInterface: "svn" + short,
		HostAddress: host.String(), PeerAddress: peer.String(), Prefix: prefix.String(),
		TableName: "svw_" + short, RouteTable: 16000 + routeSlot, RulePriority: 16000 + routeSlot,
		Mark: mark, EgressMode: request.EgressMode, CountryCode: request.CountryCode, ListenerPort: port,
	}, nil
}

func validStartRequest(request StartRequest) bool {
	if !hardwareLinePattern.MatchString(request.LineID) {
		return false
	}
	return request.EgressMode == EgressDirect && request.CountryCode == "" ||
		request.EgressMode == EgressMihomoCountry && countryPattern.MatchString(request.CountryCode) &&
			lineegress.CountryListenerPort(request.CountryCode) != 0
}

func (plan networkPlan) valid() bool {
	if plan.Version != 1 || !validStartRequest(StartRequest{LineID: plan.LineID, EgressMode: plan.EgressMode, CountryCode: plan.CountryCode}) ||
		!networkTokenPattern.MatchString(plan.Token) || plan.Namespace != "svw-"+plan.Token ||
		plan.HostInterface != "svh"+plan.Token[:8] || plan.PeerInterface != "svn"+plan.Token[:8] ||
		plan.TableName != "svw_"+plan.Token[:8] || plan.RouteTable < 16000 || plan.RouteTable > 16999 ||
		plan.RulePriority != plan.RouteTable || plan.Mark < 0x6000 || plan.Mark > 0x6fff {
		return false
	}
	prefix, prefixErr := netip.ParsePrefix(plan.Prefix)
	host, hostErr := netip.ParseAddr(plan.HostAddress)
	peer, peerErr := netip.ParseAddr(plan.PeerAddress)
	if prefixErr != nil || hostErr != nil || peerErr != nil || prefix.Bits() != 30 || !prefix.Contains(host) ||
		!prefix.Contains(peer) || host == peer || !host.Is4() || !peer.Is4() || !prefix.Addr().IsLinkLocalUnicast() {
		return false
	}
	if plan.EgressMode == EgressMihomoCountry {
		return plan.ListenerPort == lineegress.CountryListenerPort(plan.CountryCode)
	}
	return plan.ListenerPort == 0
}

type networkManager struct {
	runner    commandRunner
	readFile  func(string) ([]byte, error)
	ipPath    string
	nftPath   string
	netnsRoot string
}

func newNetworkManager() *networkManager {
	return &networkManager{runner: execRunner{}, readFile: os.ReadFile, ipPath: "/usr/sbin/ip", nftPath: "/usr/sbin/nft", netnsRoot: "/var/run/netns"}
}

func (manager *networkManager) Setup(ctx context.Context, plan networkPlan) (err error) {
	if manager == nil || manager.runner == nil || manager.readFile == nil || !plan.valid() {
		return ErrRequestInvalid
	}
	if plan.EgressMode == EgressDirect {
		forwarding, readErr := manager.readFile("/proc/sys/net/ipv4/ip_forward")
		if readErr != nil || strings.TrimSpace(string(forwarding)) != "1" {
			return errors.New("DIRECT_FORWARDING_DISABLED")
		}
	}
	if _, statErr := os.Lstat(filepath.Join(manager.netnsRoot, plan.Namespace)); statErr == nil || !errors.Is(statErr, os.ErrNotExist) {
		return errors.New("VOWIFI_NETWORK_STALE")
	}
	defer func() {
		if err != nil {
			if cleanupErr := manager.Cleanup(context.WithoutCancel(ctx), plan); cleanupErr != nil {
				err = errors.Join(err, cleanupErr)
			}
		}
	}()
	run := func(name string, args ...string) error { return manager.runner.Run(ctx, nil, name, args...) }
	if err = run(manager.ipPath, "netns", "add", plan.Namespace); err != nil {
		return err
	}
	if err = run(manager.ipPath, "link", "add", plan.HostInterface, "type", "veth", "peer", "name", plan.PeerInterface); err != nil {
		return err
	}
	if err = run(manager.ipPath, "link", "set", plan.PeerInterface, "netns", plan.Namespace); err != nil {
		return err
	}
	if err = run(manager.ipPath, "addr", "add", plan.HostAddress+"/30", "dev", plan.HostInterface); err != nil {
		return err
	}
	if err = run(manager.ipPath, "link", "set", plan.HostInterface, "up"); err != nil {
		return err
	}
	for _, args := range [][]string{
		{"netns", "exec", plan.Namespace, manager.ipPath, "link", "set", "lo", "up"},
		{"netns", "exec", plan.Namespace, manager.ipPath, "addr", "add", plan.PeerAddress + "/30", "dev", plan.PeerInterface},
		{"netns", "exec", plan.Namespace, manager.ipPath, "link", "set", plan.PeerInterface, "up"},
		{"netns", "exec", plan.Namespace, manager.ipPath, "route", "add", "default", "via", plan.HostAddress},
	} {
		if err = run(manager.ipPath, args...); err != nil {
			return err
		}
	}
	if plan.EgressMode == EgressMihomoCountry {
		if err = run(manager.ipPath, "rule", "add", "fwmark", fmt.Sprintf("0x%x", plan.Mark), "lookup", strconv.Itoa(plan.RouteTable), "priority", strconv.Itoa(plan.RulePriority)); err != nil {
			return err
		}
		if err = run(manager.ipPath, "route", "add", "local", "0.0.0.0/0", "dev", "lo", "table", strconv.Itoa(plan.RouteTable)); err != nil {
			return err
		}
	}
	if err = manager.runner.Run(ctx, nftProgram(plan), manager.nftPath, "-f", "-"); err != nil {
		return err
	}
	return nil
}

func nftProgram(plan networkPlan) []byte {
	var output strings.Builder
	family := "inet"
	fmt.Fprintf(&output, "add table %s %s\n", family, plan.TableName)
	if plan.EgressMode == EgressMihomoCountry {
		fmt.Fprintf(&output, "add chain %s %s prerouting { type filter hook prerouting priority mangle; policy accept; }\n", family, plan.TableName)
		fmt.Fprintf(&output, "add rule %s %s prerouting iifname \"%s\" meta l4proto tcp meta mark set 0x%x tproxy ip to 127.0.0.1:%d accept\n", family, plan.TableName, plan.HostInterface, plan.Mark, plan.ListenerPort)
		fmt.Fprintf(&output, "add rule %s %s prerouting iifname \"%s\" meta l4proto udp meta mark set 0x%x tproxy ip to 127.0.0.1:%d accept\n", family, plan.TableName, plan.HostInterface, plan.Mark, plan.ListenerPort)
		fmt.Fprintf(&output, "add rule %s %s prerouting iifname \"%s\" drop\n", family, plan.TableName, plan.HostInterface)
	} else {
		fmt.Fprintf(&output, "add chain %s %s forward { type filter hook forward priority -10; policy accept; }\n", family, plan.TableName)
		fmt.Fprintf(&output, "add rule %s %s forward iifname \"%s\" accept\n", family, plan.TableName, plan.HostInterface)
		fmt.Fprintf(&output, "add rule %s %s forward oifname \"%s\" ct state established,related accept\n", family, plan.TableName, plan.HostInterface)
		fmt.Fprintf(&output, "add chain %s %s postrouting { type nat hook postrouting priority srcnat; policy accept; }\n", family, plan.TableName)
		fmt.Fprintf(&output, "add rule %s %s postrouting ip saddr %s iifname \"%s\" masquerade\n", family, plan.TableName, plan.Prefix, plan.HostInterface)
	}
	return []byte(output.String())
}

func (manager *networkManager) Cleanup(ctx context.Context, plan networkPlan) error {
	if manager == nil || manager.runner == nil || !plan.valid() {
		return ErrRequestInvalid
	}
	var cleanupErr error
	run := func(input []byte, name string, args ...string) {
		if err := manager.runner.Run(ctx, input, name, args...); err != nil && !errors.Is(err, errNetworkObjectMissing) && cleanupErr == nil {
			cleanupErr = err
		}
	}
	run(nil, manager.nftPath, "delete", "table", "inet", plan.TableName)
	if plan.EgressMode == EgressMihomoCountry {
		run(nil, manager.ipPath, "rule", "del", "fwmark", fmt.Sprintf("0x%x", plan.Mark), "lookup", strconv.Itoa(plan.RouteTable), "priority", strconv.Itoa(plan.RulePriority))
		run(nil, manager.ipPath, "route", "del", "local", "0.0.0.0/0", "dev", "lo", "table", strconv.Itoa(plan.RouteTable))
	}
	run(nil, manager.ipPath, "link", "delete", plan.HostInterface)
	run(nil, manager.ipPath, "netns", "del", plan.Namespace)
	if cleanupErr != nil {
		return fmt.Errorf("%w: %v", errNetworkCleanupFailed, cleanupErr)
	}
	return nil
}

func writeNetworkManifest(path string, plan networkPlan) error {
	if !plan.valid() {
		return ErrRequestInvalid
	}
	body, err := json.Marshal(plan)
	if err != nil {
		return err
	}
	return writeAtomicPrivateFile(path, body)
}

func readNetworkManifest(path string) (networkPlan, error) {
	body, err := os.ReadFile(path)
	if err != nil || len(body) == 0 || len(body) > 16<<10 {
		return networkPlan{}, errors.New("invalid Host VoWiFi network manifest")
	}
	var plan networkPlan
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&plan) != nil || !plan.valid() {
		return networkPlan{}, errors.New("invalid Host VoWiFi network manifest")
	}
	return plan, nil
}

func writeAtomicPrivateFile(path string, body []byte) error {
	if !filepath.IsAbs(path) {
		return errors.New("private file path must be absolute")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".tmp-")
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(body); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryName, path)
}
