package messaging

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/leonfox28/simplus/internal/application/inventory"
	"github.com/leonfox28/simplus/internal/domain/sms"
)

type InboundSyncResult struct {
	Persisted    int
	AlreadyKnown int
	Acknowledged int
}

func (service *Service) SyncInbound(ctx context.Context) (InboundSyncResult, error) {
	if service == nil || service.repository == nil || service.lines == nil {
		return InboundSyncResult{}, ErrInboundSync
	}
	if service.inbox == nil {
		return InboundSyncResult{}, nil
	}
	topology, err := service.lines.Topology(ctx)
	if err != nil {
		return InboundSyncResult{}, fmt.Errorf("%w: read line topology: %v", ErrInboundSync, err)
	}
	var result InboundSyncResult
	for _, line := range topology.Lines {
		if line.State != inventory.LineReady || !line.Capabilities.SMS {
			continue
		}
		lineResult, err := service.syncInboundLine(ctx, line)
		result.Persisted += lineResult.Persisted
		result.AlreadyKnown += lineResult.AlreadyKnown
		result.Acknowledged += lineResult.Acknowledged
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
	references, err := service.inbox.ListSMS(ctx, line.PhysicalDeviceID)
	if err != nil {
		return InboundSyncResult{}, fmt.Errorf("%w: list Agent messages: %v", ErrInboundSync, err)
	}
	var result InboundSyncResult
	for _, reference := range references {
		message, err := service.inbox.ReadSMS(ctx, line.PhysicalDeviceID, reference.SourceMessageID)
		if err != nil {
			return result, fmt.Errorf("%w: read Agent message: %v", ErrInboundSync, err)
		}
		if message.SourceMessageID != reference.SourceMessageID || !message.ReceivedAt.Equal(reference.ReceivedAt) ||
			message.ReceivedAt.IsZero() || !remoteAddressPattern.MatchString(message.Sender) || strings.TrimSpace(message.Body) == "" ||
			!utf8.ValidString(message.Body) || utf8.RuneCountInString(message.Body) > 1600 || len(message.Body) > 6400 {
			return result, fmt.Errorf("%w: Agent message does not match its reference", ErrInboundSync)
		}
		messageID := inboundSourceID("msg_in_", line.ID, message.SourceMessageID)
		operationID := inboundSourceID("in_", line.ID, message.SourceMessageID)
		stored, replayed, err := service.repository.CreateInboundSMS(ctx, sms.Message{
			ID: messageID, OperationID: operationID, Direction: sms.DirectionInbound, LineID: line.ID,
			RemoteAddress: message.Sender, Body: message.Body, Status: sms.StatusReceived,
			ProviderMessageID: message.SourceMessageID, CreatedAt: message.ReceivedAt, UpdatedAt: service.currentTime(),
		})
		if err != nil {
			return result, fmt.Errorf("%w: persist inbound SMS: %v", ErrPersistence, err)
		}
		if replayed {
			result.AlreadyKnown++
		} else {
			result.Persisted++
		}
		acknowledgeID := inboundAcknowledgeOperationID(line.ID, stored.ProviderMessageID)
		if err := service.inbox.AcknowledgeSMS(ctx, line.PhysicalDeviceID, stored.ProviderMessageID, acknowledgeID); err != nil {
			return result, fmt.Errorf("%w: acknowledge persisted Agent message: %v", ErrInboundSync, err)
		}
		result.Acknowledged++
	}
	return result, nil
}

func inboundAcknowledgeOperationID(lineID, sourceMessageID string) string {
	return inboundSourceID("ack_", lineID, sourceMessageID)
}

func inboundSourceID(prefix, lineID, sourceMessageID string) string {
	digest := sha256.Sum256([]byte(lineID + "\x00" + sourceMessageID))
	return prefix + base64.RawURLEncoding.EncodeToString(digest[:])
}
