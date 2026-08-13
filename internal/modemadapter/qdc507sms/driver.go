package qdc507sms

import (
	"context"
	"encoding/csv"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/leonfox28/simplus/internal/agentapi"
	"github.com/leonfox28/simplus/internal/modemadapter"
	"github.com/leonfox28/simplus/internal/smscodec"
)

const (
	modeTimeout   = 3 * time.Second
	listTimeout   = 10 * time.Second
	readTimeout   = 5 * time.Second
	deleteTimeout = 5 * time.Second
	sendTimeout   = agentapi.SMSDispatchTimeout

	maxTranscriptLines = 512
	maxTranscriptLine  = 1024
	maxStorageIndex    = 65535
	maxStorageCount    = maxStorageIndex + 1
)

var (
	ErrControlEndpoint    = errors.New("QDC507 SMS control endpoint is unavailable")
	ErrTransport          = errors.New("QDC507 SMS AT transport failed")
	ErrModemRejected      = errors.New("QDC507 modem rejected the SMS operation")
	ErrResponseInvalid    = errors.New("QDC507 SMS AT response is invalid")
	ErrSendOutcomeUnknown = errors.New("QDC507 SMS send outcome is unknown")
)

// Returned lines are trimmed AT result lines without command echo or prompt.
type Transport interface {
	Command(context.Context, string, string, time.Duration) ([]string, error)
	Prompt(context.Context, string, string, []byte, time.Duration) ([]string, error)
}

type Driver struct {
	transport Transport
}

type StoredPDU struct {
	Index      int
	Status     int
	TPDULength int
	PDU        []byte
}

type PartSubmission struct {
	Part             int
	Total            int
	MessageReference int
}

type SendResult struct {
	Parts []PartSubmission
}

type SendFailure struct {
	CompletedParts int
	TotalParts     int
	Cause          error
}

func (failure *SendFailure) Error() string {
	if failure == nil {
		return "QDC507 SMS send failed"
	}
	return fmt.Sprintf("QDC507 SMS send failed after %d of %d parts: %v", failure.CompletedParts, failure.TotalParts, failure.Cause)
}

func (failure *SendFailure) Unwrap() error {
	if failure == nil {
		return nil
	}
	return failure.Cause
}

type ModemError struct {
	Kind       error
	CMSCode    int
	HasCMSCode bool
	Uncertain  bool
	Cause      error
}

func (failure *ModemError) Error() string {
	if failure == nil || failure.Kind == nil {
		return "QDC507 SMS operation failed"
	}
	if failure.HasCMSCode {
		return fmt.Sprintf("%v (CMS code %d)", failure.Kind, failure.CMSCode)
	}
	return failure.Kind.Error()
}

func (failure *ModemError) Unwrap() error {
	if failure == nil {
		return nil
	}
	return failure.Cause
}

func (failure *ModemError) Is(target error) bool {
	if failure == nil {
		return false
	}
	if target == failure.Kind || errors.Is(failure.Cause, target) {
		return true
	}
	return target == agentapi.ErrSMSMessageNotFound && failure.HasCMSCode && failure.CMSCode == 321
}

func NewDriver(transport Transport) (*Driver, error) {
	if transport == nil {
		return nil, errors.New("QDC507 SMS transcript transport is required")
	}
	return &Driver{transport: transport}, nil
}

func (driver *Driver) List(ctx context.Context, device agentapi.DeviceReport) ([]StoredPDU, error) {
	endpoint, err := driver.prepare(ctx, device)
	if err != nil {
		return nil, err
	}
	lines, err := driver.transport.Command(ctx, endpoint, "AT+CMGL=4", listTimeout)
	if err != nil {
		return nil, transportFailure(err)
	}
	body, err := responseBody(lines, false)
	if err != nil {
		return nil, err
	}
	return parseList(body)
}

func (driver *Driver) Read(ctx context.Context, device agentapi.DeviceReport, index int) (StoredPDU, error) {
	if index < 0 || index > maxStorageIndex {
		return StoredPDU{}, errors.New("QDC507 SMS storage index is invalid")
	}
	endpoint, err := driver.prepare(ctx, device)
	if err != nil {
		return StoredPDU{}, err
	}
	lines, err := driver.transport.Command(ctx, endpoint, "AT+CMGR="+strconv.Itoa(index), readTimeout)
	if err != nil {
		return StoredPDU{}, transportFailure(err)
	}
	body, err := responseBody(lines, false)
	if err != nil {
		return StoredPDU{}, err
	}
	return parseRead(index, body)
}

func (driver *Driver) Delete(ctx context.Context, device agentapi.DeviceReport, index int) error {
	if index < 0 || index > maxStorageIndex {
		return errors.New("QDC507 SMS storage index is invalid")
	}
	endpoint, err := driver.prepare(ctx, device)
	if err != nil {
		return err
	}
	lines, err := driver.transport.Command(ctx, endpoint, "AT+CMGD="+strconv.Itoa(index)+",0", deleteTimeout)
	if err != nil {
		return transportFailure(err)
	}
	body, err := responseBody(lines, false)
	if err != nil {
		return err
	}
	if len(body) != 0 {
		return invalidResponse(nil, false)
	}
	return nil
}

func (driver *Driver) Send(ctx context.Context, device agentapi.DeviceReport, destination, text string) (SendResult, error) {
	if driver == nil {
		return SendResult{}, ErrControlEndpoint
	}
	return dispatchPDU(ctx, driver.transport, device, destination, text)
}

func dispatchPDU(ctx context.Context, transport Transport, device agentapi.DeviceReport, destination, text string) (SendResult, error) {
	return dispatchSMS(ctx, transport.Command, transport.Prompt, device, destination, text)
}

func dispatchSMS(
	ctx context.Context,
	command func(context.Context, string, string, time.Duration) ([]string, error),
	prompt func(context.Context, string, string, []byte, time.Duration) ([]string, error),
	device agentapi.DeviceReport,
	destination, text string,
) (SendResult, error) {
	ctx, cancel := context.WithTimeout(ctx, sendTimeout)
	defer cancel()
	segments, err := smscodec.Encode(text)
	if err != nil {
		return SendResult{}, err
	}
	endpoint, err := prepareSMSCommands(ctx, command, device)
	if err != nil {
		return SendResult{}, &SendFailure{TotalParts: len(segments), Cause: err}
	}
	result := SendResult{Parts: make([]PartSubmission, 0, len(segments))}
	for index, segment := range segments {
		messageReference := byte(index)
		if segment.Total > 1 {
			if segment.Reference > 255 {
				return SendResult{}, errors.New("QDC507 outbound SMS concatenation reference is invalid")
			}
			messageReference = byte(segment.Reference) + byte(index)
		}
		pdu, err := smscodec.EncodeSubmitPDU(destination, segment, messageReference)
		if err != nil {
			return result, &SendFailure{CompletedParts: len(result.Parts), TotalParts: len(segments), Cause: err}
		}
		payload := append([]byte(pdu.Hex()), 0x1a)
		lines, err := prompt(ctx, endpoint, "AT+CMGS="+strconv.Itoa(pdu.TPDULength), payload, sendTimeout)
		if err != nil {
			if errors.Is(err, ErrPromptNotDispatched) {
				return result, &SendFailure{CompletedParts: len(result.Parts), TotalParts: len(segments), Cause: transportFailure(err)}
			}
			unknown := &ModemError{Kind: ErrSendOutcomeUnknown, Uncertain: true, Cause: err}
			return result, &SendFailure{CompletedParts: len(result.Parts), TotalParts: len(segments), Cause: unknown}
		}
		body, err := responseBody(lines, true)
		if err != nil {
			return result, &SendFailure{CompletedParts: len(result.Parts), TotalParts: len(segments), Cause: err}
		}
		reference, err := parseSubmission(body)
		if err != nil {
			return result, &SendFailure{CompletedParts: len(result.Parts), TotalParts: len(segments), Cause: invalidResponse(err, true)}
		}
		result.Parts = append(result.Parts, PartSubmission{Part: segment.Part, Total: segment.Total, MessageReference: reference})
	}
	return result, nil
}

func (driver *Driver) prepare(ctx context.Context, device agentapi.DeviceReport) (string, error) {
	if driver == nil {
		return "", ErrControlEndpoint
	}
	return prepareSMS(ctx, driver.transport, device)
}

func prepareSMS(ctx context.Context, transport Transport, device agentapi.DeviceReport) (string, error) {
	if transport == nil || device.Profile != agentapi.ProfileQDC507 {
		return "", ErrControlEndpoint
	}
	return prepareSMSCommands(ctx, transport.Command, device)
}

func prepareSMSCommands(ctx context.Context, command func(context.Context, string, string, time.Duration) ([]string, error), device agentapi.DeviceReport) (string, error) {
	if command == nil || device.Profile != agentapi.ProfileQDC507 {
		return "", ErrControlEndpoint
	}
	endpoint, ok := (modemadapter.QDC507SMS{}).Endpoint(device, modemadapter.EndpointPrimaryAT)
	if !ok {
		return "", ErrControlEndpoint
	}
	lines, err := command(ctx, endpoint.Node, "AT+CMGF=0", modeTimeout)
	if err != nil {
		return "", transportFailure(err)
	}
	body, err := responseBody(lines, false)
	if err != nil {
		return "", err
	}
	if len(body) != 0 {
		return "", invalidResponse(nil, false)
	}
	lines, err = command(ctx, endpoint.Node, `AT+CPMS="SM","SM","SM"`, modeTimeout)
	if err != nil {
		return "", transportFailure(err)
	}
	body, err = responseBody(lines, false)
	if err != nil {
		return "", err
	}
	if err := validateCPMSSelection(body); err != nil {
		return "", err
	}
	return endpoint.Node, nil
}

// The Quectel write form returns exactly six counters:
// used1,total1,used2,total2,used3,total3. The selected storage names appear
// only in the read form, so accepting an empty body would turn a fixture
// shortcut into protocol evidence.
func validateCPMSSelection(lines []string) error {
	if len(lines) != 1 || !strings.HasPrefix(lines[0], "+CPMS:") {
		return invalidResponse(nil, false)
	}
	fields, err := csvFields(strings.TrimSpace(strings.TrimPrefix(lines[0], "+CPMS:")))
	if err != nil || len(fields) != 6 {
		return invalidResponse(nil, false)
	}
	for index := 0; index < len(fields); index += 2 {
		used, usedErr := boundedInteger(fields[index], 0, maxStorageCount)
		total, totalErr := boundedInteger(fields[index+1], 0, maxStorageCount)
		if usedErr != nil || totalErr != nil || used > total {
			return invalidResponse(errors.Join(usedErr, totalErr), false)
		}
	}
	return nil
}

func responseBody(lines []string, afterDispatch bool) ([]string, error) {
	normalized, err := normalizeTranscript(lines)
	if err != nil {
		return nil, invalidResponse(err, afterDispatch)
	}
	terminal := normalized[len(normalized)-1]
	switch {
	case terminal == "OK":
		return normalized[:len(normalized)-1], nil
	case terminal == "ERROR" || strings.HasPrefix(terminal, "+CME ERROR:"):
		return nil, &ModemError{Kind: ErrModemRejected}
	case strings.HasPrefix(terminal, "+CMS ERROR:"):
		codeText := strings.TrimSpace(strings.TrimPrefix(terminal, "+CMS ERROR:"))
		code, parseErr := strconv.Atoi(codeText)
		if parseErr != nil || code < 0 || code > 9999 {
			return nil, invalidResponse(parseErr, afterDispatch)
		}
		return nil, &ModemError{Kind: ErrModemRejected, CMSCode: code, HasCMSCode: true}
	default:
		return nil, invalidResponse(nil, afterDispatch)
	}
}

func normalizeTranscript(lines []string) ([]string, error) {
	if len(lines) == 0 || len(lines) > maxTranscriptLines {
		return nil, errors.New("AT transcript line count is invalid")
	}
	normalized := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if len(line) > maxTranscriptLine || strings.IndexFunc(line, func(character rune) bool {
			return unicode.IsControl(character)
		}) >= 0 {
			return nil, errors.New("AT transcript contains an invalid line")
		}
		normalized = append(normalized, line)
	}
	if len(normalized) == 0 {
		return nil, errors.New("AT transcript is empty")
	}
	return normalized, nil
}

func parseList(lines []string) ([]StoredPDU, error) {
	messages := make([]StoredPDU, 0, len(lines)/2)
	for index := 0; index < len(lines); {
		header := lines[index]
		if !strings.HasPrefix(header, "+CMGL:") || index+1 >= len(lines) {
			return nil, invalidResponse(nil, false)
		}
		fields, err := csvFields(strings.TrimSpace(strings.TrimPrefix(header, "+CMGL:")))
		if err != nil || len(fields) < 3 {
			return nil, invalidResponse(err, false)
		}
		storageIndex, err := boundedInteger(fields[0], 0, maxStorageIndex)
		if err != nil {
			return nil, invalidResponse(err, false)
		}
		status, err := boundedInteger(fields[1], 0, 4)
		if err != nil {
			return nil, invalidResponse(err, false)
		}
		tpduLength, err := boundedInteger(fields[len(fields)-1], 1, 255)
		if err != nil {
			return nil, invalidResponse(err, false)
		}
		pdu, err := parseStoredPDU(lines[index+1], tpduLength)
		if err != nil {
			return nil, err
		}
		messages = append(messages, StoredPDU{Index: storageIndex, Status: status, TPDULength: tpduLength, PDU: pdu})
		index += 2
	}
	return messages, nil
}

func parseRead(index int, lines []string) (StoredPDU, error) {
	if len(lines) != 2 || !strings.HasPrefix(lines[0], "+CMGR:") {
		return StoredPDU{}, invalidResponse(nil, false)
	}
	fields, err := csvFields(strings.TrimSpace(strings.TrimPrefix(lines[0], "+CMGR:")))
	if err != nil || len(fields) < 2 {
		return StoredPDU{}, invalidResponse(err, false)
	}
	status, err := boundedInteger(fields[0], 0, 4)
	if err != nil {
		return StoredPDU{}, invalidResponse(err, false)
	}
	tpduLength, err := boundedInteger(fields[len(fields)-1], 1, 255)
	if err != nil {
		return StoredPDU{}, invalidResponse(err, false)
	}
	pdu, err := parseStoredPDU(lines[1], tpduLength)
	if err != nil {
		return StoredPDU{}, err
	}
	return StoredPDU{Index: index, Status: status, TPDULength: tpduLength, PDU: pdu}, nil
}

func parseStoredPDU(encoded string, expectedTPDULength int) ([]byte, error) {
	if len(encoded) < 4 || len(encoded) > 512 || len(encoded)%2 != 0 {
		return nil, invalidResponse(nil, false)
	}
	pdu, err := hex.DecodeString(encoded)
	if err != nil || len(pdu) < 2 {
		return nil, invalidResponse(err, false)
	}
	smscLength := int(pdu[0])
	if smscLength+1 >= len(pdu) || len(pdu)-smscLength-1 != expectedTPDULength {
		return nil, invalidResponse(nil, false)
	}
	return pdu, nil
}

func parseSubmission(lines []string) (int, error) {
	if len(lines) != 1 || !strings.HasPrefix(lines[0], "+CMGS:") {
		return 0, errors.New("CMGS result is missing")
	}
	value := strings.TrimSpace(strings.TrimPrefix(lines[0], "+CMGS:"))
	if comma := strings.IndexByte(value, ','); comma >= 0 {
		value = value[:comma]
	}
	return boundedInteger(value, 0, 255)
}

func csvFields(value string) ([]string, error) {
	reader := csv.NewReader(strings.NewReader(value))
	reader.TrimLeadingSpace = true
	reader.FieldsPerRecord = -1
	fields, err := reader.Read()
	if err != nil {
		return nil, err
	}
	if _, err := reader.Read(); !errors.Is(err, io.EOF) {
		return nil, errors.New("AT result contains multiple CSV records")
	}
	for index := range fields {
		fields[index] = strings.TrimSpace(fields[index])
	}
	return fields, nil
}

func boundedInteger(value string, minimum, maximum int) (int, error) {
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || parsed < minimum || parsed > maximum {
		return 0, errors.New("AT integer is outside its allowed range")
	}
	return parsed, nil
}

func transportFailure(cause error) error {
	return &ModemError{Kind: ErrTransport, Cause: cause}
}

func invalidResponse(cause error, afterDispatch bool) error {
	kind := ErrResponseInvalid
	if afterDispatch {
		kind = ErrSendOutcomeUnknown
	}
	return &ModemError{Kind: kind, Uncertain: afterDispatch, Cause: cause}
}
