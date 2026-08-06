package vowifisupervisor

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/leonfox28/simplus/internal/vowifihil"
)

var pcscfLogPattern = regexp.MustCompile(`received P-CSCF server IP ([0-9]{1,3}(?:\.[0-9]{1,3}){3})`)

type WorkerConfig struct {
	LineID         string
	HardwareLineID string
	RuntimeDir     string
	LinkAddress    string
	EgressMode     string
	CountryCode    string
	CharonPath     string
	IPPath         string
}

type attemptFailure struct{ code string }

func (failure attemptFailure) Error() string { return failure.code }

// RunWorker owns one Line inside its already-created network namespace. It
// emits only validated, credential-free state events on output.
func RunWorker(ctx context.Context, config WorkerConfig, output io.Writer) error {
	if err := validateWorkerConfig(config); err != nil {
		return err
	}
	emit := func(event workerEvent) {
		event.LineID = config.LineID
		if validWorkerEvent(event, config.LineID) {
			_ = json.NewEncoder(output).Encode(event)
		}
	}
	emit(workerEvent{State: StateStarting, Stage: "PREFLIGHT"})
	backoff := time.Second
	for attempt := 1; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil
		}
		if attempt > 1 {
			emit(workerEvent{State: StateReconnecting, Stage: "BACKOFF", Attempt: attempt})
			timer := time.NewTimer(backoff)
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil
			case <-timer.C:
			}
			if backoff < 30*time.Second {
				backoff *= 2
			}
		}
		err := runWorkerAttempt(ctx, config, attempt, emit)
		if ctx.Err() != nil {
			return nil
		}
		code := "SESSION_FAILED"
		var failure attemptFailure
		if errors.As(err, &failure) && safeStatusToken.MatchString(failure.code) {
			code = failure.code
		}
		emit(workerEvent{State: StateReconnecting, Stage: "RECOVERY", Attempt: attempt, ErrorCode: code})
	}
}

func runWorkerAttempt(ctx context.Context, config WorkerConfig, attempt int, emit func(workerEvent)) error {
	attemptDir := filepath.Join(config.RuntimeDir, "session")
	if err := os.RemoveAll(attemptDir); err != nil {
		return attemptFailure{"RUNTIME_CLEANUP_FAILED"}
	}
	if err := os.Mkdir(attemptDir, 0o700); err != nil {
		return attemptFailure{"RUNTIME_CREATE_FAILED"}
	}
	defer os.RemoveAll(attemptDir)

	preflightCtx, cancelPreflight := context.WithTimeout(ctx, 35*time.Second)
	inspection, err := vowifihil.InspectHostVoWiFiLine(preflightCtx, config.HardwareLineID)
	cancelPreflight()
	if err != nil {
		return attemptFailure{"SIM_PREFLIGHT_FAILED"}
	}
	paths, err := vowifihil.PathsFor(attemptDir)
	if err != nil {
		return attemptFailure{"RUNTIME_PATH_INVALID"}
	}
	strongSwan, err := vowifihil.BuildAt(vowifihil.Input{Target: inspection.Target, IMSI: inspection.IMSI}, paths)
	inspection.IMSI = ""
	if err != nil {
		return attemptFailure{"STRONGSWAN_CONFIG_FAILED"}
	}
	defer zero(strongSwan.StrongSwan)
	defer zero(strongSwan.VICI)
	if err := writeAtomicPrivateFile(paths.StrongSwanConfig, strongSwan.StrongSwan); err != nil {
		return attemptFailure{"STRONGSWAN_CONFIG_FAILED"}
	}
	if err := syscall.Mkfifo(paths.LogPipe, 0o600); err != nil {
		return attemptFailure{"STRONGSWAN_LOG_PIPE_FAILED"}
	}
	logReader, err := os.OpenFile(paths.LogPipe, os.O_RDWR, 0)
	if err != nil {
		return attemptFailure{"STRONGSWAN_LOG_PIPE_FAILED"}
	}
	defer logReader.Close()
	pcscf := make(chan netip.Addr, 4)
	diagnostics := make(chan string, 8)
	trace := &strongSwanTrace{}
	readerDone := make(chan struct{})
	go readStrongSwanLog(logReader, pcscf, diagnostics, trace, readerDone)

	charon := exec.Command(config.CharonPath)
	charon.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "HOME=/nonexistent", "STRONGSWAN_CONF=" + paths.StrongSwanConfig}
	charon.Stdout, charon.Stderr = io.Discard, io.Discard
	charon.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGTERM}
	if err := charon.Start(); err != nil {
		return attemptFailure{"STRONGSWAN_START_FAILED"}
	}
	charonDone := make(chan error, 1)
	go func() {
		charonDone <- charon.Wait()
		close(charonDone)
	}()
	defer func() {
		terminateProcess(charon, charonDone)
		select {
		case <-readerDone:
		case <-time.After(time.Second):
		}
	}()
	if !waitForSocket(ctx, paths.VICISocket, charonDone, 8*time.Second) {
		return attemptFailure{"STRONGSWAN_NOT_READY"}
	}
	input, err := vowifihil.ParseConnectionInput(strongSwan.VICI)
	if err != nil {
		return attemptFailure{"STRONGSWAN_CONFIG_FAILED"}
	}
	emit(workerEvent{State: StateConnecting, Stage: "EPDG", Attempt: attempt})
	initiateCtx, cancelInitiate := context.WithTimeout(ctx, 55*time.Second)
	err = vowifihil.Initiate(initiateCtx, paths.VICISocket, input)
	cancelInitiate()
	if err != nil {
		return attemptFailure{latestDiagnostic(diagnostics, func() string { return trace.failureCode(initiateErrorCode(err)) })}
	}
	source, err := discoverInnerAddress(config.IPPath, config.LinkAddress)
	if err != nil {
		return attemptFailure{"EPDG_INNER_ADDRESS_MISSING"}
	}
	pcscfAddresses := collectPCSCF(ctx, pcscf, 4, 4*time.Second)
	if len(pcscfAddresses) == 0 {
		return attemptFailure{"PCSCF_MISSING"}
	}
	emit(workerEvent{State: StateRegistering, Stage: "IMS", Attempt: attempt})
	var session *vowifihil.IMSSession
	var registration vowifihil.IMSRegistrationResult
	for _, address := range pcscfAddresses {
		registerCtx, cancelRegister := context.WithTimeout(ctx, 50*time.Second)
		session, registration, err = vowifihil.EstablishIMSSession(registerCtx, source, address, inspection)
		cancelRegister()
		if err == nil {
			break
		}
	}
	if session == nil || err != nil {
		return attemptFailure{"IMS_REGISTER_FAILED"}
	}
	defer session.Close()
	smsService := &workerSMSService{session: session}
	smsServer, smsListener, smsServerErrors, err := startWorkerSMSServer(config.RuntimeDir, smsService)
	if err != nil {
		return attemptFailure{"SMS_CONTROL_FAILED"}
	}
	defer stopWorkerSMSServer(smsServer, smsListener, config.RuntimeDir)
	emit(workerEvent{State: StateOnline, Stage: "REGISTERED", Online: true, Attempt: attempt,
		RegisteredAt: registration.RegisteredAt, NextRefresh: registration.NextRefresh})
	keepalive := time.NewTicker(25 * time.Second)
	health := time.NewTicker(15 * time.Second)
	smsPoll := time.NewTicker(250 * time.Millisecond)
	defer keepalive.Stop()
	defer health.Stop()
	defer smsPoll.Stop()
	refresh := time.NewTimer(time.Until(registration.NextRefresh))
	defer refresh.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-charonDone:
			return attemptFailure{"EPDG_PROCESS_EXITED"}
		case serverErr := <-smsServerErrors:
			if !errors.Is(serverErr, http.ErrServerClosed) {
				return attemptFailure{"SMS_CONTROL_FAILED"}
			}
			return attemptFailure{"SMS_CONTROL_FAILED"}
		case <-smsPoll.C:
			pollCtx, cancel := context.WithTimeout(ctx, 150*time.Millisecond)
			err := session.PollSMS(pollCtx)
			cancel()
			if err != nil && !errors.Is(err, vowifihil.ErrIMSSMSUnavailable) {
				return attemptFailure{"IMS_SMS_RECEIVE_FAILED"}
			}
		case <-keepalive.C:
			keepaliveCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
			err := session.Keepalive(keepaliveCtx)
			cancel()
			if err != nil {
				return attemptFailure{"IMS_KEEPALIVE_FAILED"}
			}
		case <-health.C:
			if !xfrmHealthy(config.IPPath, 6) {
				return attemptFailure{"IPSEC_STATE_LOST"}
			}
		case <-refresh.C:
			refreshCtx, cancel := context.WithTimeout(ctx, 25*time.Second)
			updated, err := session.Refresh(refreshCtx)
			cancel()
			if err != nil {
				return attemptFailure{imsRefreshErrorCode(err)}
			}
			registration = updated
			emit(workerEvent{State: StateOnline, Stage: "REGISTERED", Online: true, Attempt: attempt,
				RegisteredAt: updated.RegisteredAt, NextRefresh: updated.NextRefresh})
			refresh.Reset(maxDuration(time.Until(updated.NextRefresh), time.Second))
		}
	}
}

func imsRefreshErrorCode(err error) string {
	switch {
	case errors.Is(err, vowifihil.ErrIMSReauthenticationRequired):
		return "IMS_REAUTH_REQUIRED"
	case errors.Is(err, vowifihil.ErrIMSRefreshIntervalRejected):
		return "IMS_REFRESH_INTERVAL_REJECTED"
	case errors.Is(err, vowifihil.ErrIMSRefreshRejected):
		return "IMS_REFRESH_REJECTED"
	case errors.Is(err, vowifihil.ErrIMSRefreshNoResponse):
		return "IMS_REFRESH_NO_RESPONSE"
	case errors.Is(err, vowifihil.ErrIMSRefreshResponseUnmatched):
		return "IMS_REFRESH_RESPONSE_UNMATCHED"
	default:
		return "IMS_REFRESH_FAILED"
	}
}

func initiateErrorCode(err error) string {
	switch {
	case errors.Is(err, vowifihil.ErrVICIUnavailable):
		return "STRONGSWAN_VICI_UNAVAILABLE"
	case errors.Is(err, vowifihil.ErrRequiredPluginsUnavailable):
		return "STRONGSWAN_PLUGINS_MISSING"
	case errors.Is(err, vowifihil.ErrConnectionLoadFailed):
		return "STRONGSWAN_CONNECTION_LOAD_FAILED"
	case errors.Is(err, vowifihil.ErrConnectionVerifyFailed):
		return "STRONGSWAN_CONNECTION_VERIFY_FAILED"
	case errors.Is(err, vowifihil.ErrConnectionInitiateFailed):
		return "EPDG_CONNECT_FAILED"
	default:
		return "EPDG_CONNECT_FAILED"
	}
}

func validateWorkerConfig(config WorkerConfig) error {
	request := StartRequest{LineID: config.LineID, HardwareLineID: config.HardwareLineID, EgressMode: config.EgressMode, CountryCode: config.CountryCode}
	link, linkErr := netip.ParseAddr(config.LinkAddress)
	if !validStartRequest(request) || !filepath.IsAbs(config.RuntimeDir) || !strings.HasPrefix(filepath.Clean(config.RuntimeDir), "/run/simplus-netd/vowifi/") ||
		!filepath.IsAbs(config.CharonPath) || !filepath.IsAbs(config.IPPath) || linkErr != nil || !link.Is4() || !link.IsLinkLocalUnicast() {
		return ErrRequestInvalid
	}
	return nil
}

func readStrongSwanLog(file *os.File, output chan<- netip.Addr, diagnostics chan<- string, trace *strongSwanTrace, done chan<- struct{}) {
	defer close(done)
	if file == nil {
		return
	}
	seen := make(map[netip.Addr]struct{})
	// The worker can remain online for days. Keep each log line bounded, but
	// continue draining the FIFO for the lifetime of charon so its writer can
	// never block after an arbitrary cumulative byte limit.
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 4096), 64<<10)
	for scanner.Scan() {
		line := scanner.Text()
		trace.observe(line)
		if code := classifyStrongSwanDiagnostic(line); code != "" {
			select {
			case diagnostics <- code:
			default:
			}
		}
		match := pcscfLogPattern.FindStringSubmatch(line)
		if len(match) != 2 {
			continue
		}
		address, err := netip.ParseAddr(match[1])
		if err != nil || !address.Is4() || !address.IsPrivate() {
			continue
		}
		if _, found := seen[address]; found {
			continue
		}
		seen[address] = struct{}{}
		select {
		case output <- address:
		default:
		}
	}
}

type strongSwanTrace struct {
	mu                                                  sync.Mutex
	sentInit, initResponse, sentAuth, authResponse, eap bool
	apnIDr, simAKA                                      bool
}

func (trace *strongSwanTrace) observe(line string) {
	if trace == nil {
		return
	}
	lower := strings.ToLower(line)
	trace.mu.Lock()
	defer trace.mu.Unlock()
	switch {
	case strings.Contains(lower, "generating ike_sa_init request"):
		trace.sentInit = true
	case strings.Contains(lower, "parsed ike_sa_init response"):
		trace.initResponse = true
	case strings.Contains(lower, "generating ike_auth request"):
		trace.sentAuth = true
	case strings.Contains(lower, "parsed ike_auth response"):
		trace.authResponse = true
	}
	if strings.Contains(lower, "eap/req") || strings.Contains(lower, "eap request") {
		trace.eap = true
	}
	if strings.Contains(lower, "simplus ims apn idr added") {
		trace.apnIDr = true
	}
	if strings.Contains(lower, "simplus sim aka agent exchange completed") {
		trace.simAKA = true
	}
}

func (trace *strongSwanTrace) failureCode(fallback string) string {
	if trace == nil {
		return fallback
	}
	trace.mu.Lock()
	defer trace.mu.Unlock()
	switch {
	case trace.simAKA:
		return "EPDG_POST_AKA_FAILED"
	case trace.eap:
		return "EPDG_EAP_FAILED"
	case trace.authResponse:
		return "EPDG_AUTH_REJECTED"
	case trace.sentAuth && !trace.apnIDr:
		return "EPDG_APN_IDR_MISSING"
	case trace.sentAuth:
		return "EPDG_IKE_AUTH_NO_RESPONSE"
	case trace.initResponse:
		return "EPDG_IKE_AUTH_SETUP_FAILED"
	case trace.sentInit:
		return "EPDG_NO_RESPONSE"
	default:
		return fallback
	}
}

func classifyStrongSwanDiagnostic(line string) string {
	lower := strings.ToLower(line)
	switch {
	case strings.Contains(lower, "simplus sim aka identity fence did not match"):
		return "SIM_IDENTITY_CHANGED"
	case strings.Contains(lower, "simplus sim aka agent exchange failed"):
		return "SIM_AKA_FAILED"
	case strings.Contains(lower, "simplus sim aka plugin requires"):
		return "SIM_AKA_UNAVAILABLE"
	case strings.Contains(lower, "no proposal chosen"):
		return "EPDG_PROPOSAL_REJECTED"
	case strings.Contains(lower, "authentication_failed"), strings.Contains(lower, "eap authentication failed"),
		strings.Contains(lower, "eap-aka failed"), strings.Contains(lower, "verification of auth payload"):
		return "EPDG_AUTH_FAILED"
	case strings.Contains(lower, "establishing ike_sa failed, peer not responding"), strings.Contains(lower, "giving up after"):
		return "EPDG_NO_RESPONSE"
	case strings.Contains(lower, "sending packet failed"):
		return "EPDG_SEND_FAILED"
	default:
		return ""
	}
}

func latestDiagnostic(input <-chan string, fallback func() string) string {
	latest := ""
	timer := time.NewTimer(100 * time.Millisecond)
	defer timer.Stop()
	for {
		select {
		case value := <-input:
			if safeStatusToken.MatchString(value) {
				latest = value
			}
		case <-timer.C:
			if latest != "" {
				return latest
			}
			return fallback()
		}
	}
}

func collectPCSCF(ctx context.Context, input <-chan netip.Addr, maximum int, budget time.Duration) []netip.Addr {
	timer := time.NewTimer(budget)
	defer timer.Stop()
	values := make([]netip.Addr, 0, maximum)
	for len(values) < maximum {
		select {
		case <-ctx.Done():
			return values
		case <-timer.C:
			return values
		case value := <-input:
			values = append(values, value)
			if len(values) >= 1 {
				select {
				case next := <-input:
					values = append(values, next)
				default:
					return values
				}
			}
		}
	}
	return values
}

func waitForSocket(ctx context.Context, path string, process <-chan error, budget time.Duration) bool {
	deadline := time.NewTimer(budget)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer deadline.Stop()
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return false
		case <-process:
			return false
		case <-deadline.C:
			return false
		case <-ticker.C:
			if info, err := os.Stat(path); err == nil && info.Mode()&os.ModeSocket != 0 {
				return true
			}
		}
	}
}

func discoverInnerAddress(ipPath, linkAddress string) (netip.Addr, error) {
	command := exec.Command(ipPath, "-o", "-4", "addr", "show")
	body, err := command.Output()
	if err != nil || len(body) > 1<<20 {
		return netip.Addr{}, errors.New("inner address unavailable")
	}
	defer zero(body)
	link, _ := netip.ParseAddr(linkAddress)
	var candidates []netip.Addr
	for _, line := range strings.Split(string(body), "\n") {
		fields := strings.Fields(line)
		for index := 0; index+1 < len(fields); index++ {
			if fields[index] != "inet" {
				continue
			}
			prefix, parseErr := netip.ParsePrefix(fields[index+1])
			if parseErr == nil && prefix.Addr().Is4() && prefix.Addr().IsPrivate() && prefix.Addr() != link && !prefix.Addr().IsLoopback() {
				candidates = append(candidates, prefix.Addr())
			}
		}
	}
	if len(candidates) != 1 {
		return netip.Addr{}, errors.New("expected one inner address")
	}
	return candidates[0], nil
}

func xfrmHealthy(ipPath string, minimum int) bool {
	command := exec.Command(ipPath, "xfrm", "state", "count")
	body, err := command.Output()
	if err != nil || len(body) > 1024 {
		return false
	}
	defer zero(body)
	for _, field := range strings.Fields(string(body)) {
		count, parseErr := strconv.Atoi(field)
		if parseErr == nil {
			return count >= minimum
		}
	}
	return false
}

func terminateProcess(command *exec.Cmd, done <-chan error) {
	if command == nil || command.Process == nil {
		return
	}
	_ = command.Process.Signal(syscall.SIGTERM)
	select {
	case <-done:
		return
	case <-time.After(5 * time.Second):
		_ = command.Process.Kill()
	}
	select {
	case <-done:
	case <-time.After(time.Second):
	}
}

func maxDuration(left, right time.Duration) time.Duration {
	if left > right {
		return left
	}
	return right
}

func zero(value []byte) {
	for index := range value {
		value[index] = 0
	}
}
