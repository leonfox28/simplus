package messaging

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/leonfox28/simplus/internal/application/inventory"
	"github.com/leonfox28/simplus/internal/domain/accessmode"
	"github.com/leonfox28/simplus/internal/domain/sms"
	"github.com/leonfox28/simplus/internal/smscodec"
)

type InboundSyncResult struct {
	Persisted                   int
	AlreadyKnown                int
	Acknowledged                int
	OutboundSent                int
	OutboundFailed              int
	OutboundUnconfirmed         int
	OutboundReportsAcknowledged int
}

func (service *Service) SyncInbound(ctx context.Context) (InboundSyncResult, error) {
	if service == nil || service.repository == nil || service.lines == nil {
		return InboundSyncResult{}, ErrInboundSync
	}
	if service.inbox == nil && service.reports == nil {
		return InboundSyncResult{}, nil
	}
	topology, err := service.lines.Topology(ctx)
	if err != nil {
		return InboundSyncResult{}, fmt.Errorf("%w: read line topology: %v", ErrInboundSync, err)
	}
	var result InboundSyncResult
	for _, line := range topology.Lines {
		if line.State != inventory.LineReady || !service.supportsSMS(line) {
			continue
		}
		if line.AccessMode == accessmode.HostVoWiFiOnly && (service.accessPaths == nil || !service.accessPaths.Available(ctx, line.ID)) {
			continue
		}
		lineResult, err := service.syncInboundLine(ctx, line)
		result.Persisted += lineResult.Persisted
		result.AlreadyKnown += lineResult.AlreadyKnown
		result.Acknowledged += lineResult.Acknowledged
		result.OutboundSent += lineResult.OutboundSent
		result.OutboundFailed += lineResult.OutboundFailed
		result.OutboundUnconfirmed += lineResult.OutboundUnconfirmed
		result.OutboundReportsAcknowledged += lineResult.OutboundReportsAcknowledged
		if err != nil {
			return result, err
		}
	}
	return result, nil
}

func (service *Service) syncInboundLine(ctx context.Context, line inventory.Line) (InboundSyncResult, error) {
	gate := service.gate(line.ModemFunctionID)
	select {
	case gate.token <- struct{}{}:
		defer func() { <-gate.token }()
	case <-ctx.Done():
		return InboundSyncResult{}, fmt.Errorf("%w: wait for modem: %v", ErrInboundSync, ctx.Err())
	}
	target := InboxTarget{LineID: line.ID, PhysicalDeviceID: line.PhysicalDeviceID}
	var result InboundSyncResult
	if service.reports != nil {
		reportResult, err := service.syncSubmitReportsLocked(ctx, target)
		result.OutboundSent += reportResult.OutboundSent
		result.OutboundFailed += reportResult.OutboundFailed
		result.OutboundUnconfirmed += reportResult.OutboundUnconfirmed
		result.OutboundReportsAcknowledged += reportResult.OutboundReportsAcknowledged
		if err != nil {
			return result, err
		}
	}
	if service.inbox == nil {
		return result, nil
	}
	references, err := service.inbox.ListSMS(ctx, target)
	if err != nil {
		return result, fmt.Errorf("%w: list transport messages: %v", ErrInboundSync, err)
	}
	for _, reference := range references {
		message, err := service.inbox.ReadSMS(ctx, target, reference.SourceMessageID)
		if err != nil {
			return result, fmt.Errorf("%w: read transport message: %v", ErrInboundSync, err)
		}
		if err := validateInboundMessage(reference, message); err != nil {
			return result, fmt.Errorf("%w: transport message does not match its reference", ErrInboundSync)
		}
		messageResult, err := service.syncInboundMessage(ctx, target, line.ID, message)
		result.Persisted += messageResult.Persisted
		result.AlreadyKnown += messageResult.AlreadyKnown
		result.Acknowledged += messageResult.Acknowledged
		if err != nil {
			return result, err
		}
	}
	return result, nil
}

func (service *Service) syncSubmitReportsLocked(ctx context.Context, target InboxTarget) (InboundSyncResult, error) {
	reports, err := service.reports.ListSMSSubmitReports(ctx, target)
	if err != nil {
		return InboundSyncResult{}, fmt.Errorf("%w: list outbound submit reports: %v", ErrInboundSync, err)
	}
	if len(reports) > 256 {
		return InboundSyncResult{}, fmt.Errorf("%w: too many outbound submit reports", ErrInboundSync)
	}
	var result InboundSyncResult
	for _, report := range reports {
		if err := validateSubmitReport(report); err != nil {
			return result, fmt.Errorf("%w: invalid outbound submit report", ErrInboundSync)
		}
		switch report.State {
		case SendStateSent:
			_, err = service.repository.MarkOutboundSMSSent(ctx, report.MessageID, report.ProviderMessageID, report.CompletedAt)
		case SendStateFailed:
			_, err = service.repository.MarkOutboundSMSFailed(ctx, report.MessageID, report.ProviderMessageID,
				report.ErrorCode, report.CompletedAt)
		case SendStateUnconfirmed:
			_, err = service.repository.MarkOutboundSMSUnconfirmed(ctx, report.MessageID, report.ProviderMessageID,
				report.ErrorCode, report.CompletedAt)
		}
		if err != nil && !errors.Is(err, sms.ErrMessageNotFound) {
			return result, fmt.Errorf("%w: persist outbound submit report: %v", ErrPersistence, err)
		}
		if err == nil {
			switch report.State {
			case SendStateSent:
				result.OutboundSent++
			case SendStateFailed:
				result.OutboundFailed++
			case SendStateUnconfirmed:
				result.OutboundUnconfirmed++
			}
		}
		acknowledgeID := inboundSourceID("report_ack_", target.LineID, report.ProviderMessageID)
		if err := service.reports.AcknowledgeSMSSubmitReport(ctx, target, report.ProviderMessageID, acknowledgeID); err != nil {
			return result, fmt.Errorf("%w: acknowledge outbound submit report: %v", ErrInboundSync, err)
		}
		result.OutboundReportsAcknowledged++
	}
	return result, nil
}

func validateSubmitReport(report SubmitReport) error {
	if !operationIDPattern.MatchString(report.MessageID) || !operationIDPattern.MatchString(report.ProviderMessageID) ||
		report.CompletedAt.IsZero() {
		return ErrRequestInvalid
	}
	switch report.State {
	case SendStateSent:
		if report.ErrorCode != "" {
			return ErrRequestInvalid
		}
	case SendStateFailed, SendStateUnconfirmed:
		if !errorCodePattern.MatchString(report.ErrorCode) {
			return ErrRequestInvalid
		}
	default:
		return ErrRequestInvalid
	}
	return nil
}

func (service *Service) syncInboundMessage(ctx context.Context, target InboxTarget, lineID string,
	message InboxMessage) (InboundSyncResult, error) {
	if message.Segment == nil {
		return service.persistAndAcknowledgeInbound(ctx, target, lineID, message.SourceMessageID,
			message.SourceMessageID, message.Sender, message.Body, message.ReceivedAt)
	}
	segment := *message.Segment
	decoded, err := smscodec.DecodeSegment(segment)
	if err != nil {
		return InboundSyncResult{}, fmt.Errorf("%w: decode inbound SMS segment: %v", ErrInboundSync, err)
	}
	if segment.Total == 1 {
		return service.persistAndAcknowledgeInbound(ctx, target, lineID, message.SourceMessageID,
			message.SourceMessageID, message.Sender, decoded, message.ReceivedAt)
	}

	groupID := inboundFragmentGroupID(lineID, message.SourceMessageID)
	fragments, _, err := service.repository.StoreInboundSMSFragment(ctx, sms.InboundFragment{
		GroupID: groupID, SourceMessageID: message.SourceMessageID, LineID: lineID,
		Sender: message.Sender, Encoding: string(segment.Encoding), Reference: segment.Reference,
		Part: segment.Part, Total: segment.Total, UnitCount: segment.UnitCount,
		UserData: append([]byte(nil), segment.UserData...), ReceivedAt: message.ReceivedAt,
	})
	if err != nil {
		return InboundSyncResult{}, fmt.Errorf("%w: persist inbound SMS fragment: %v", ErrPersistence, err)
	}
	if len(fragments) == 0 || len(fragments) > segment.Total {
		return InboundSyncResult{}, fmt.Errorf("%w: invalid persisted inbound SMS fragment group", ErrPersistence)
	}
	groupID = fragments[0].GroupID
	if len(fragments) < segment.Total {
		if err := service.acknowledgeInbound(ctx, target, lineID, message.SourceMessageID); err != nil {
			return InboundSyncResult{}, err
		}
		return InboundSyncResult{Acknowledged: 1}, nil
	}
	assembled := make([]smscodec.Segment, 0, len(fragments))
	receivedAt := fragments[0].ReceivedAt
	for _, fragment := range fragments {
		assembled = append(assembled, smscodec.Segment{
			Encoding: smscodec.Encoding(fragment.Encoding), Reference: fragment.Reference,
			Part: fragment.Part, Total: fragment.Total, UnitCount: fragment.UnitCount,
			UserData: append([]byte(nil), fragment.UserData...),
		})
		if fragment.ReceivedAt.Before(receivedAt) {
			receivedAt = fragment.ReceivedAt
		}
	}
	body, err := smscodec.Decode(assembled)
	if err != nil {
		return InboundSyncResult{}, fmt.Errorf("%w: assemble inbound SMS fragments: %v", ErrInboundSync, err)
	}
	return service.persistAndAcknowledgeInbound(ctx, target, lineID, groupID,
		message.SourceMessageID, message.Sender, body, receivedAt)
}

func (service *Service) persistAndAcknowledgeInbound(ctx context.Context, target InboxTarget, lineID,
	providerMessageID, acknowledgeMessageID, sender, body string, receivedAt time.Time) (InboundSyncResult, error) {
	messageID := inboundSourceID("msg_in_", lineID, providerMessageID)
	operationID := inboundSourceID("in_", lineID, providerMessageID)
	_, replayed, err := service.repository.CreateInboundSMS(ctx, sms.Message{
		ID: messageID, OperationID: operationID, Direction: sms.DirectionInbound, LineID: lineID,
		RemoteAddress: sender, Body: body, Status: sms.StatusReceived,
		ProviderMessageID: providerMessageID, CreatedAt: receivedAt, UpdatedAt: service.currentTime(),
	})
	if err != nil {
		return InboundSyncResult{}, fmt.Errorf("%w: persist inbound SMS: %v", ErrPersistence, err)
	}
	result := InboundSyncResult{}
	if replayed {
		result.AlreadyKnown = 1
	} else {
		result.Persisted = 1
	}
	if err := service.acknowledgeInbound(ctx, target, lineID, acknowledgeMessageID); err != nil {
		return result, err
	}
	result.Acknowledged = 1
	return result, nil
}

func (service *Service) acknowledgeInbound(ctx context.Context, target InboxTarget, lineID, sourceMessageID string) error {
	acknowledgeID := inboundAcknowledgeOperationID(lineID, sourceMessageID)
	if err := service.inbox.AcknowledgeSMS(ctx, target, sourceMessageID, acknowledgeID); err != nil {
		return fmt.Errorf("%w: acknowledge persisted transport message: %v", ErrInboundSync, err)
	}
	return nil
}

func validateInboundMessage(reference InboxMessageReference, message InboxMessage) error {
	if message.SourceMessageID != reference.SourceMessageID || !operationIDPattern.MatchString(message.SourceMessageID) ||
		!message.ReceivedAt.Equal(reference.ReceivedAt) || message.ReceivedAt.IsZero() ||
		!remoteAddressPattern.MatchString(message.Sender) {
		return ErrRequestInvalid
	}
	if message.Segment == nil {
		if strings.TrimSpace(message.Body) == "" || !utf8.ValidString(message.Body) ||
			utf8.RuneCountInString(message.Body) > 1600 || len(message.Body) > 6400 {
			return ErrRequestInvalid
		}
		return nil
	}
	decoded, err := smscodec.DecodeSegment(*message.Segment)
	if err != nil {
		return err
	}
	if message.Segment.Total == 1 && message.Body != decoded || message.Segment.Total > 1 && message.Body != "" {
		return ErrRequestInvalid
	}
	return nil
}

func inboundFragmentGroupID(lineID, sourceMessageID string) string {
	return inboundSourceID("infrag_", lineID, sourceMessageID)
}

func inboundAcknowledgeOperationID(lineID, sourceMessageID string) string {
	return inboundSourceID("ack_", lineID, sourceMessageID)
}

func inboundSourceID(prefix, lineID, sourceMessageID string) string {
	digest := sha256.Sum256([]byte(lineID + "\x00" + sourceMessageID))
	return prefix + base64.RawURLEncoding.EncodeToString(digest[:])
}
