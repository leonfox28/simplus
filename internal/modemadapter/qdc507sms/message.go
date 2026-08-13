package qdc507sms

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/leonfox28/simplus/internal/smscodec"
)

var ErrMultipartAmbiguous = errors.New("QDC507 multipart SMS is ambiguous")

var (
	errInboundPDUDecode         = errors.New("QDC507 inbound SMS PDU structure is invalid")
	errInboundContentValidation = errors.New("QDC507 inbound SMS decoded content is invalid")
)

const maxMultipartAssemblySpan = 10 * time.Minute

// InboundSegment binds an Agent message to the modem storage entry that must
// be acknowledged. PDUDigest protects a newly reused storage index from being
// deleted as though it still held the original message.
type InboundSegment struct {
	Index         int
	PDUDigest     [sha256.Size]byte
	DeleteStarted bool `json:",omitempty"`
	Deleted       bool
}

// InboundRecord is the minimum state needed to keep list/read stable while a
// multipart acknowledgement is partially complete.
type InboundRecord struct {
	MessageID       string
	SubscriptionKey string
	Sender          string
	Body            string
	ReceivedAt      time.Time
	Segments        []InboundSegment
	Acknowledged    bool
}

type decodedStoredPDU struct {
	stored    StoredPDU
	delivered smscodec.DeliverPDU
	digest    [sha256.Size]byte
}

type multipartKey struct {
	sender    string
	encoding  smscodec.Encoding
	dcs       byte
	reference uint16
	total     int
}

func assembleInbound(subscriptionKey string, stored []StoredPDU) ([]InboundRecord, error) {
	if !subscriptionKeyPattern.MatchString(subscriptionKey) {
		return nil, errors.New("QDC507 inbound SMS requires a subscription key")
	}
	seenIndexes := make(map[int]struct{}, len(stored))
	singles := make([]decodedStoredPDU, 0, len(stored))
	groups := make(map[multipartKey][]decodedStoredPDU)
	for _, candidate := range stored {
		// CMGL=4 also returns outgoing drafts and submitted messages. Only
		// received-unread and received-read entries belong in the inbox.
		if candidate.Status != 0 && candidate.Status != 1 {
			continue
		}
		if _, duplicate := seenIndexes[candidate.Index]; duplicate {
			return nil, fmt.Errorf("%w: duplicate QDC507 SMS storage index %d", errInboundPDUDecode, candidate.Index)
		}
		seenIndexes[candidate.Index] = struct{}{}
		delivered, err := smscodec.DecodeDeliverPDU(candidate.PDU)
		if err != nil {
			return nil, fmt.Errorf("%w: decode QDC507 SMS storage index %d: %v", errInboundPDUDecode, candidate.Index, err)
		}
		decoded := decodedStoredPDU{stored: candidate, delivered: delivered, digest: sha256.Sum256(candidate.PDU)}
		if delivered.Segment.Total == 1 {
			singles = append(singles, decoded)
			continue
		}
		key := multipartKey{
			sender: delivered.Sender, encoding: delivered.Segment.Encoding,
			dcs: delivered.DCS, reference: delivered.Segment.Reference, total: delivered.Segment.Total,
		}
		groups[key] = append(groups[key], decoded)
	}

	records := make([]InboundRecord, 0, len(singles)+len(groups))
	for _, single := range singles {
		record, err := inboundRecord(subscriptionKey, []decodedStoredPDU{single})
		if err != nil {
			return nil, fmt.Errorf("%w: %v", errInboundContentValidation, err)
		}
		records = append(records, record)
	}
	for _, group := range groups {
		parts := make(map[int]struct{}, len(group))
		ambiguous := len(group) != group[0].delivered.Segment.Total
		earliest := group[0].delivered.ReceivedAt
		latest := earliest
		for _, segment := range group {
			if _, duplicate := parts[segment.delivered.Segment.Part]; duplicate {
				ambiguous = true
			}
			parts[segment.delivered.Segment.Part] = struct{}{}
			if segment.delivered.ReceivedAt.Before(earliest) {
				earliest = segment.delivered.ReceivedAt
			}
			if segment.delivered.ReceivedAt.After(latest) {
				latest = segment.delivered.ReceivedAt
			}
		}
		// An incomplete group is expected while segments arrive or after a
		// partial ACK. A duplicate part means the 8-bit reference was reused;
		// both cases are held back rather than risking a mixed message.
		if ambiguous || latest.Sub(earliest) > maxMultipartAssemblySpan {
			continue
		}
		record, err := inboundRecord(subscriptionKey, group)
		if err != nil {
			return nil, fmt.Errorf("%w: %w: %v", ErrMultipartAmbiguous, errInboundContentValidation, err)
		}
		records = append(records, record)
	}
	sort.Slice(records, func(left, right int) bool {
		if records[left].ReceivedAt.Equal(records[right].ReceivedAt) {
			return records[left].MessageID < records[right].MessageID
		}
		return records[left].ReceivedAt.Before(records[right].ReceivedAt)
	})
	return records, nil
}

func inboundRecord(subscriptionKey string, decoded []decodedStoredPDU) (InboundRecord, error) {
	sort.Slice(decoded, func(left, right int) bool {
		return decoded[left].delivered.Segment.Part < decoded[right].delivered.Segment.Part
	})
	segments := make([]smscodec.Segment, 0, len(decoded))
	storedSegments := make([]InboundSegment, 0, len(decoded))
	receivedAt := decoded[0].delivered.ReceivedAt
	for _, part := range decoded {
		segments = append(segments, part.delivered.Segment)
		storedSegments = append(storedSegments, InboundSegment{Index: part.stored.Index, PDUDigest: part.digest})
		if part.delivered.ReceivedAt.Before(receivedAt) {
			receivedAt = part.delivered.ReceivedAt
		}
	}
	body, err := smscodec.Decode(segments)
	if err != nil {
		return InboundRecord{}, fmt.Errorf("assemble QDC507 SMS: %w", err)
	}
	if strings.TrimSpace(body) == "" || !utf8.ValidString(body) || utf8.RuneCountInString(body) > 1600 || len(body) > 6400 {
		return InboundRecord{}, errors.New("assembled QDC507 SMS is outside the Agent message limits")
	}
	return InboundRecord{
		MessageID: stableInboundMessageID(subscriptionKey, decoded), SubscriptionKey: subscriptionKey,
		Sender: decoded[0].delivered.Sender, Body: body, ReceivedAt: receivedAt.UTC(), Segments: storedSegments,
	}, nil
}

func stableInboundMessageID(subscriptionKey string, decoded []decodedStoredPDU) string {
	digest := sha256.New()
	writeHashField(digest, []byte(subscriptionKey))
	for _, part := range decoded {
		var index [8]byte
		binary.BigEndian.PutUint64(index[:], uint64(part.stored.Index))
		writeHashField(digest, index[:])
		writeHashField(digest, part.stored.PDU)
	}
	return "qdc507-in-" + base64.RawURLEncoding.EncodeToString(digest.Sum(nil))
}

type hashWriter interface {
	Write([]byte) (int, error)
}

func writeHashField(destination hashWriter, value []byte) {
	var length [8]byte
	binary.BigEndian.PutUint64(length[:], uint64(len(value)))
	_, _ = destination.Write(length[:])
	_, _ = destination.Write(value)
}
