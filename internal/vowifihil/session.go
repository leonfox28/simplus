package vowifihil

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	ErrIMSReauthenticationRequired = errors.New("IMS reauthentication is required")
	ErrIMSRefreshIntervalRejected  = errors.New("IMS refresh interval was rejected")
	ErrIMSRefreshRejected          = errors.New("IMS refresh was rejected")
	ErrIMSRefreshNoResponse        = errors.New("IMS refresh received no response")
	ErrIMSRefreshResponseUnmatched = errors.New("IMS refresh response did not match the transaction")
	errProtectedIMSNoResponse      = errors.New("protected IMS request received no response")
	errProtectedIMSUnmatched       = errors.New("protected IMS response did not match the transaction")
)

type IMSRegistrationResult struct {
	RegisteredAt time.Time
	NextRefresh  time.Time
	Expires      time.Duration
}

// IMSSession owns the two protected SIP flows, transient Digest AKA material
// and Gm XFRM installation for one registered IMS binding. It is safe for one
// refresh/keepalive caller at a time and must always be closed.
type IMSSession struct {
	mu sync.Mutex

	input                IMSInitialRegisterInput
	pcscf                netip.Addr
	challenge            IMSRegistrationChallenge
	securityClient       string
	res                  []byte
	cnonce               string
	unprotected          *net.UDPConn
	client               *net.UDPConn
	server               *net.UDPConn
	xfrm                 *IMSXFRMInstallation
	sequence             uint64
	nonceCount           uint64
	expires              time.Duration
	registeredAt         time.Time
	nextRefresh          time.Time
	serviceCentreURI     string
	serviceCentreAddress string
	serviceRoutes        []string
	authorizedIdentity   string
	smsSequence          uint64
	rpReference          byte
	smsInReplyToMode     imsSMSInReplyToMode
	smsSubmitReportWait  time.Duration
	pendingSMS           map[string]pendingIMSSMS
	acknowledgedSMS      map[string]string
	submitSegments       map[byte]pendingIMSSubmitSegment
	retiredRPReferences  map[byte]time.Time
	submitOperations     map[string]*pendingIMSSubmitOperation
	completedSMSReports  map[string]IMSSMSSubmitReport
	acknowledgedReports  map[string]struct{}
	smsProtocolCounters  imsSMSProtocolCounters
	closed               bool
}

func EstablishIMSSession(ctx context.Context, source, pcscf netip.Addr, inspection Inspection) (*IMSSession, IMSRegistrationResult, error) {
	if !validIMSPrivateIPv4(source) || !validIMSPrivateIPv4(pcscf) || source == pcscf ||
		len(inspection.IMSIdentity.PublicIdentities) != 1 {
		return nil, IMSRegistrationResult{}, errors.New("invalid IMS session input")
	}
	session := &IMSSession{
		pcscf: pcscf, smsSequence: 1,
		pendingSMS: make(map[string]pendingIMSSMS), acknowledgedSMS: make(map[string]string),
		submitSegments: make(map[byte]pendingIMSSubmitSegment), retiredRPReferences: make(map[byte]time.Time),
		submitOperations:    make(map[string]*pendingIMSSubmitOperation),
		completedSMSReports: make(map[string]IMSSMSSubmitReport), acknowledgedReports: make(map[string]struct{}),
	}
	fail := func(err error) (*IMSSession, IMSRegistrationResult, error) {
		session.Close()
		return nil, IMSRegistrationResult{}, err
	}

	var err error
	session.unprotected, err = net.ListenUDP("udp4", &net.UDPAddr{IP: net.IP(source.AsSlice()), Port: IMSSIPPort})
	if err != nil {
		return fail(errors.New("bind IMS unprotected flow"))
	}
	session.client, err = net.ListenUDP("udp4", &net.UDPAddr{IP: net.IP(source.AsSlice())})
	if err != nil {
		return fail(errors.New("bind IMS protected client flow"))
	}
	session.server, err = net.ListenUDP("udp4", &net.UDPAddr{IP: net.IP(source.AsSlice())})
	if err != nil {
		return fail(errors.New("bind IMS protected server flow"))
	}
	clientSPI, serverSPI, err := randomSPIPair()
	if err != nil {
		return fail(errors.New("generate IMS security parameters"))
	}
	branch, err := randomHexToken(8)
	if err != nil {
		return fail(err)
	}
	fromTag, err := randomHexToken(8)
	if err != nil {
		return fail(err)
	}
	callID, err := randomHexToken(8)
	if err != nil {
		return fail(err)
	}
	contactUser, err := randomHexToken(8)
	if err != nil {
		return fail(err)
	}
	wlanNodeID, err := randomFixedHex(6)
	if err != nil {
		return fail(err)
	}
	session.input = IMSInitialRegisterInput{
		Source: source, UnprotectedPort: uint16(session.unprotected.LocalAddr().(*net.UDPAddr).Port),
		ProtectedClientPort: uint16(session.client.LocalAddr().(*net.UDPAddr).Port),
		ProtectedServerPort: uint16(session.server.LocalAddr().(*net.UDPAddr).Port),
		ClientSPI:           clientSPI, ServerSPI: serverSPI,
		PrivateIdentity: inspection.IMSIdentity.PrivateIdentity,
		PublicIdentity:  inspection.IMSIdentity.PublicIdentities[0], HomeDomain: inspection.IMSIdentity.HomeDomain,
		Branch: branch, FromTag: fromTag, CallID: callID + "@" + source.String(), ContactUser: contactUser,
		WLANNodeID: wlanNodeID,
	}
	session.authorizedIdentity = session.input.PublicIdentity
	if smsConfiguration := inspection.IMSIdentity.SMSOverIP; smsConfiguration != nil {
		session.input.SMSCapable = true
		session.serviceCentreURI = smsConfiguration.ServiceCentreURI
		session.serviceCentreAddress = smsConfiguration.ServiceCentreAddress
	}
	initialSequence := uint64(1)
	requestedExpires := DefaultIMSRegistrationExpires
	initial, securityClient, err := BuildIMSInitialRegisterSequence(session.input, initialSequence, requestedExpires)
	if err != nil {
		return fail(err)
	}
	defer zeroBytes(initial)
	response, err := exchangeSIP(ctx, session.unprotected, pcscf, IMSSIPPort, initial, session.input.CallID, initialSequence, 12*time.Second)
	if err != nil {
		return fail(err)
	}
	defer zeroBytes(response)
	initialSummary, err := ParseIMSInitialResponse(response, session.input.CallID)
	if err != nil {
		return fail(err)
	}
	if initialSummary.Status == 423 {
		initialSequence++
		requestedExpires = initialSummary.MinExpires
		retryBranch, randomErr := randomHexToken(8)
		if randomErr != nil {
			return fail(randomErr)
		}
		session.input.Branch = retryBranch
		retry, _, buildErr := BuildIMSInitialRegisterSequence(session.input, initialSequence, requestedExpires)
		if buildErr != nil {
			return fail(buildErr)
		}
		defer zeroBytes(retry)
		response, err = exchangeSIP(ctx, session.unprotected, pcscf, IMSSIPPort, retry, session.input.CallID, initialSequence, 12*time.Second)
		if err != nil {
			return fail(err)
		}
		defer zeroBytes(response)
		initialSummary, err = ParseIMSInitialResponse(response, session.input.CallID)
		if err != nil {
			return fail(err)
		}
	}
	if initialSummary.Status != 401 {
		return fail(errors.New("IMS initial registration was not challenged"))
	}
	challenge, err := ExtractIMSRegistrationChallenge(response, session.input.CallID)
	if err != nil {
		return fail(err)
	}
	defer zeroBytes(challenge.RAND[:])
	defer zeroBytes(challenge.AUTN[:])
	material, _, err := AuthenticateIMSChallenge(ctx, inspection.Target, challenge)
	if err != nil {
		return fail(err)
	}
	defer func() {
		zeroBytes(material.CK[:])
		zeroBytes(material.IK[:])
	}()
	tunnel, err := DiscoverEPDGTunnel(ctx, source)
	if err != nil {
		material.Destroy()
		return fail(err)
	}
	clientSecurity := IMSClientIPSecParameters{
		ClientSPI: session.input.ClientSPI, ServerSPI: session.input.ServerSPI,
		ProtectedClientPort: session.input.ProtectedClientPort, ProtectedServerPort: session.input.ProtectedServerPort,
	}
	session.xfrm, err = InstallIMSXFRM(ctx, source, pcscf, tunnel, clientSecurity, challenge.SecurityServer, &material)
	if err != nil {
		material.Destroy()
		return fail(err)
	}
	branch, err = randomHexToken(8)
	if err != nil {
		material.Destroy()
		return fail(err)
	}
	cnonce, err := randomHexToken(8)
	if err != nil {
		material.Destroy()
		return fail(err)
	}
	authenticatedSequence := initialSequence + 1
	authenticated, err := BuildIMSAuthenticatedRegisterSequence(session.input, challenge, material.RES,
		securityClient, branch, cnonce, authenticatedSequence, 1, requestedExpires)
	if err != nil {
		material.Destroy()
		return fail(err)
	}
	defer zeroBytes(authenticated)
	protectedResponse, err := exchangeProtectedSIP(ctx, session.client, session.server, pcscf,
		challenge.SecurityServer, authenticated, session.input.CallID, authenticatedSequence, 15*time.Second)
	if err != nil {
		material.Destroy()
		return fail(err)
	}
	defer zeroBytes(protectedResponse)
	expires, nextNonce, err := parseSuccessfulRegistration(protectedResponse, session.input.CallID, authenticatedSequence)
	if err != nil {
		material.Destroy()
		return fail(err)
	}
	session.challenge = challenge
	zeroBytes(session.challenge.RAND[:])
	zeroBytes(session.challenge.AUTN[:])
	session.securityClient = securityClient
	session.res = append([]byte(nil), material.RES...)
	material.Destroy()
	session.sequence, session.nonceCount, session.cnonce = authenticatedSequence, 1, cnonce
	session.expires = expires
	session.registeredAt = time.Now().UTC()
	session.nextRefresh = refreshDeadline(session.registeredAt, expires)
	if nextNonce != "" && nextNonce != session.challenge.Nonce {
		// A fresh AKA nonce requires coordinated new Gm keying. Reconnect before
		// this registration expires instead of silently reusing old CK/IK.
		session.nextRefresh = session.registeredAt.Add(minDuration(expires/2, 2*time.Minute))
	}
	result := IMSRegistrationResult{RegisteredAt: session.registeredAt, NextRefresh: session.nextRefresh, Expires: expires}
	session.serviceRoutes = registrationServiceRoutes(protectedResponse)
	session.authorizedIdentity = registrationAuthorizedIdentity(protectedResponse, session.input.PublicIdentity)
	return session, result, nil
}

func (session *IMSSession) Keepalive(ctx context.Context) error {
	session.mu.Lock()
	defer session.mu.Unlock()
	if session.closed || session.client == nil {
		return errors.New("IMS session is closed")
	}
	if deadline, ok := ctx.Deadline(); ok {
		_ = session.client.SetWriteDeadline(deadline)
	} else {
		_ = session.client.SetWriteDeadline(time.Now().Add(3 * time.Second))
	}
	defer session.client.SetWriteDeadline(time.Time{})
	packet := []byte("\r\n\r\n")
	_, err := session.client.WriteToUDP(packet, &net.UDPAddr{IP: net.IP(session.pcscf.AsSlice()), Port: int(session.challenge.SecurityServer.ProtectedServerPort)})
	zeroBytes(packet)
	if err != nil {
		return errors.New("IMS keepalive failed")
	}
	return nil
}

func (session *IMSSession) Refresh(ctx context.Context) (IMSRegistrationResult, error) {
	session.mu.Lock()
	defer session.mu.Unlock()
	if session.closed || session.client == nil || len(session.res) == 0 {
		return IMSRegistrationResult{}, errors.New("IMS session is closed")
	}
	branch, err := randomHexToken(8)
	if err != nil {
		return IMSRegistrationResult{}, err
	}
	cnonce, nonceCount, err := session.nextRefreshDigestState()
	if err != nil {
		return IMSRegistrationResult{}, err
	}
	sequence := session.sequence + 1
	requestedExpires, ok := registrationExpiresSeconds(session.expires)
	if !ok {
		return IMSRegistrationResult{}, errors.New("invalid IMS registration interval")
	}
	packet, err := BuildIMSAuthenticatedRegisterSequence(session.input, session.challenge, session.res,
		session.securityClient, branch, cnonce, sequence, nonceCount, requestedExpires)
	if err != nil {
		return IMSRegistrationResult{}, err
	}
	defer zeroBytes(packet)
	response, err := session.exchangeProtectedRequestLocked(ctx, packet, session.input.CallID, sequence, "REGISTER", 15*time.Second)
	if err != nil {
		if errors.Is(err, errProtectedIMSNoResponse) {
			return IMSRegistrationResult{}, ErrIMSRefreshNoResponse
		}
		if errors.Is(err, errProtectedIMSUnmatched) {
			return IMSRegistrationResult{}, ErrIMSRefreshResponseUnmatched
		}
		return IMSRegistrationResult{}, err
	}
	defer zeroBytes(response)
	status, err := ParseIMSAuthenticatedResponseSequence(response, session.input.CallID, sequence)
	if err != nil {
		return IMSRegistrationResult{}, err
	}
	if status == 401 {
		return IMSRegistrationResult{}, ErrIMSReauthenticationRequired
	}
	if status == 423 {
		return IMSRegistrationResult{}, ErrIMSRefreshIntervalRejected
	}
	if status < 200 || status > 299 {
		return IMSRegistrationResult{}, ErrIMSRefreshRejected
	}
	expires, nextNonce, err := parseSuccessfulRegistration(response, session.input.CallID, sequence)
	if err != nil {
		return IMSRegistrationResult{}, err
	}
	session.sequence, session.nonceCount = sequence, nonceCount
	session.expires = expires
	session.registeredAt = time.Now().UTC()
	session.nextRefresh = refreshDeadline(session.registeredAt, expires)
	session.serviceRoutes = registrationServiceRoutes(response)
	session.authorizedIdentity = registrationAuthorizedIdentity(response, session.messagePublicIdentityLocked())
	if nextNonce != "" && nextNonce != session.challenge.Nonce {
		return IMSRegistrationResult{}, ErrIMSReauthenticationRequired
	}
	return IMSRegistrationResult{RegisteredAt: session.registeredAt, NextRefresh: session.nextRefresh, Expires: expires}, nil
}

func (session *IMSSession) Result() IMSRegistrationResult {
	session.mu.Lock()
	defer session.mu.Unlock()
	return IMSRegistrationResult{RegisteredAt: session.registeredAt, NextRefresh: session.nextRefresh, Expires: session.expires}
}

func (session *IMSSession) Close() {
	if session == nil {
		return
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	if session.closed {
		return
	}
	session.closed = true
	if session.xfrm != nil {
		session.xfrm.Cleanup()
		session.xfrm = nil
	}
	for _, connection := range []*net.UDPConn{session.unprotected, session.client, session.server} {
		if connection != nil {
			_ = connection.Close()
		}
	}
	session.unprotected, session.client, session.server = nil, nil, nil
	zeroBytes(session.res)
	session.res = nil
	session.challenge.Nonce = ""
	session.challenge.Opaque = ""
	session.challenge.SecurityServer.Raw = ""
	session.cnonce = ""
	session.input.PrivateIdentity = ""
	session.input.PublicIdentity = ""
	session.serviceCentreURI = ""
	session.serviceCentreAddress = ""
	session.serviceRoutes = nil
	session.authorizedIdentity = ""
	clear(session.pendingSMS)
	clear(session.acknowledgedSMS)
	clear(session.submitSegments)
	clear(session.retiredRPReferences)
	clear(session.submitOperations)
	clear(session.completedSMSReports)
	clear(session.acknowledgedReports)
}

func registrationServiceRoutes(packet []byte) []string {
	_, headers, err := parseSIPResponse(packet)
	if err != nil || len(headers["service-route"]) > 8 {
		return nil
	}
	routes := make([]string, 0, len(headers["service-route"]))
	for _, value := range headers["service-route"] {
		if !validSIPHeaderValue(value, 2048) {
			return nil
		}
		routes = append(routes, value)
	}
	return routes
}

func registrationAuthorizedIdentity(packet []byte, fallback string) string {
	if !validIMSURI(fallback) {
		return ""
	}
	_, headers, err := parseSIPResponse(packet)
	if err != nil {
		return fallback
	}
	values := headers["p-associated-uri"]
	if len(values) == 0 {
		return fallback
	}
	if len(values) > 16 {
		return fallback
	}
	first := firstSIPHeaderListValue(values[0])
	if identity := firstSIPURI([]string{first}); identity != "" {
		return identity
	}
	return fallback
}

func firstSIPHeaderListValue(value string) string {
	value = strings.TrimSpace(value)
	quoted, escaped, angleDepth := false, false, 0
	for index, current := range value {
		switch {
		case escaped:
			escaped = false
		case quoted && current == '\\':
			escaped = true
		case current == '"':
			quoted = !quoted
		case !quoted && current == '<':
			angleDepth++
		case !quoted && current == '>' && angleDepth > 0:
			angleDepth--
		case !quoted && angleDepth == 0 && current == ',':
			return strings.TrimSpace(value[:index])
		}
	}
	return value
}

func exchangeSIP(ctx context.Context, connection *net.UDPConn, pcscf netip.Addr, port uint16, packet []byte,
	callID string, sequence uint64, budget time.Duration) ([]byte, error) {
	target := &net.UDPAddr{IP: net.IP(pcscf.AsSlice()), Port: int(port)}
	buffer := make([]byte, 64<<10)
	defer zeroBytes(buffer)
	deadline := time.Now().Add(budget)
	if contextDeadline, ok := ctx.Deadline(); ok && contextDeadline.Before(deadline) {
		deadline = contextDeadline
	}
	backoff := 500 * time.Millisecond
	for time.Now().Before(deadline) {
		if _, err := connection.WriteToUDP(packet, target); err != nil {
			return nil, errors.New("send IMS REGISTER")
		}
		readUntil := time.Now().Add(backoff)
		if readUntil.After(deadline) {
			readUntil = deadline
		}
		_ = connection.SetReadDeadline(readUntil)
		for {
			count, sender, err := connection.ReadFromUDP(buffer)
			if timeout, ok := err.(net.Error); ok && timeout.Timeout() {
				break
			}
			if err != nil {
				return nil, errors.New("read IMS response")
			}
			if !sender.IP.Equal(target.IP) || count == 0 {
				continue
			}
			if _, headers, parseErr := parseSIPResponse(buffer[:count]); parseErr == nil && matchingRegisterTransaction(headers, callID, sequence) {
				return append([]byte(nil), buffer[:count]...), nil
			}
		}
		if backoff < 4*time.Second {
			backoff *= 2
		}
	}
	return nil, errors.New("IMS REGISTER timed out")
}

func exchangeProtectedSIP(ctx context.Context, client, server *net.UDPConn, pcscf netip.Addr,
	security IMSIPSecParameters, packet []byte, callID string, sequence uint64, budget time.Duration) ([]byte, error) {
	target := &net.UDPAddr{IP: net.IP(pcscf.AsSlice()), Port: int(security.ProtectedServerPort)}
	buffer := make([]byte, 64<<10)
	defer zeroBytes(buffer)
	deadline := time.Now().Add(budget)
	if contextDeadline, ok := ctx.Deadline(); ok && contextDeadline.Before(deadline) {
		deadline = contextDeadline
	}
	backoff, nextSend := 500*time.Millisecond, time.Now()
	sawUnmatchedResponse := false
	for time.Now().Before(deadline) {
		if !time.Now().Before(nextSend) {
			if _, err := client.WriteToUDP(packet, target); err != nil {
				return nil, errors.New("send protected IMS REGISTER")
			}
			nextSend = time.Now().Add(backoff)
			if backoff < 4*time.Second {
				backoff *= 2
			}
		}
		for _, connection := range []*net.UDPConn{client, server} {
			readUntil := time.Now().Add(125 * time.Millisecond)
			if readUntil.After(nextSend) {
				readUntil = nextSend
			}
			if readUntil.After(deadline) {
				readUntil = deadline
			}
			_ = connection.SetReadDeadline(readUntil)
			count, sender, err := connection.ReadFromUDP(buffer)
			if timeout, ok := err.(net.Error); ok && timeout.Timeout() {
				continue
			}
			if err != nil {
				return nil, errors.New("read protected IMS response")
			}
			validPort := sender.Port == int(security.ProtectedServerPort) || sender.Port == int(security.ProtectedClientPort)
			if !sender.IP.Equal(target.IP) || !validPort || count == 0 {
				continue
			}
			if _, headers, parseErr := parseSIPResponse(buffer[:count]); parseErr == nil {
				if matchingRegisterTransaction(headers, callID, sequence) {
					return append([]byte(nil), buffer[:count]...), nil
				}
				sawUnmatchedResponse = true
			}
		}
	}
	if sawUnmatchedResponse {
		return nil, errProtectedIMSUnmatched
	}
	return nil, errProtectedIMSNoResponse
}

func parseSuccessfulRegistration(packet []byte, callID string, sequence uint64) (time.Duration, string, error) {
	status, headers, err := parseSIPResponse(packet)
	if err != nil || !matchingRegisterTransaction(headers, callID, sequence) || status < 200 || status > 299 {
		return 0, "", errors.New("IMS registration was not accepted")
	}
	expires := 600
	if values := headers["expires"]; len(values) == 1 {
		if parsed, parseErr := strconv.Atoi(strings.TrimSpace(values[0])); parseErr == nil {
			expires = parsed
		}
	}
	for _, contact := range headers["contact"] {
		parts := splitSIPValue(contact, ';')
		if len(parts) < 2 {
			continue
		}
		parameters := parseSIPParameters(strings.Join(parts[1:], ";"), ';')
		if parsed, parseErr := strconv.Atoi(parameters["expires"]); parseErr == nil {
			expires = parsed
			break
		}
	}
	if expires < 60 {
		expires = 60
	}
	if expires > 86400 {
		expires = 86400
	}
	nextNonce := ""
	for _, value := range headers["authentication-info"] {
		parameters := parseSIPParameters(value, ',')
		candidate := parameters["nextnonce"]
		if candidate != "" && len(candidate) <= 1024 && validDigestValue(candidate, 1024) {
			nextNonce = candidate
			break
		}
	}
	return time.Duration(expires) * time.Second, nextNonce, nil
}

func refreshDeadline(registeredAt time.Time, expires time.Duration) time.Time {
	lead := expires / 5
	if lead < 30*time.Second {
		lead = 30 * time.Second
	}
	if lead > 5*time.Minute {
		lead = 5 * time.Minute
	}
	refreshAfter := expires - lead
	if refreshAfter < 30*time.Second {
		refreshAfter = 30 * time.Second
	}
	return registeredAt.Add(refreshAfter)
}

func registrationExpiresSeconds(expires time.Duration) (uint32, bool) {
	seconds := expires / time.Second
	if seconds < 60 || seconds > 86400 {
		return 0, false
	}
	return uint32(seconds), true
}

func (session *IMSSession) nextRefreshDigestState() (string, uint64, error) {
	if session.nonceCount == 0 || session.nonceCount >= 0xffffffff ||
		session.challenge.QOP != "" && !validIMSToken(session.cnonce, 16, 64) {
		return "", 0, errors.New("invalid IMS refresh digest state")
	}
	// Nonce-count is scoped to the server nonce. Keep the client nonce stable
	// for the lifetime of that challenge, as established by the first accepted
	// authenticated REGISTER, while incrementing nc for subsequent requests.
	return session.cnonce, session.nonceCount + 1, nil
}

func minDuration(left, right time.Duration) time.Duration {
	if left < right {
		return left
	}
	return right
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

func randomFixedHex(size int) (string, error) {
	if size < 1 || size > 64 {
		return "", errors.New("invalid random value size")
	}
	value := make([]byte, size)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	defer zeroBytes(value)
	return hex.EncodeToString(value), nil
}
