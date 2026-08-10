package pagination

import (
	"encoding/base64"
	"encoding/binary"
	"errors"
	"regexp"
	"time"
)

const (
	DefaultLimit       = 20
	MaximumLimit       = 50
	MaximumCursorLen   = 256
	cursorVersion      = byte(1)
	cursorPrefixLen    = 9
	smsCursorVersion   = byte(2)
	smsCursorKind      = byte('s')
	smsCursorPrefixLen = 10
)

var (
	ErrCursorInvalid = errors.New("page cursor is invalid")
	ErrLimitInvalid  = errors.New("page limit is invalid")
	stableIDPattern  = regexp.MustCompile(`^[A-Za-z0-9_-]{16,128}$`)
)

// Cursor is an internal decoded keyset boundary. CreatedAt is used by the v1
// Calls cursor and by legacy SMS cursors. RecordSequence is used only by v2
// SMS cursors. Exactly one ordering field is populated after decoding.
type Cursor struct {
	CreatedAt      time.Time
	RecordSequence int64
	ID             string
}

type Request struct {
	Limit int
	After *Cursor
}

type Page[T any] struct {
	Items []T
	Next  *Cursor
}

func NormalizeLimit(limit int) (int, error) {
	if limit == 0 {
		return DefaultLimit, nil
	}
	if limit < 1 || limit > MaximumLimit {
		return 0, ErrLimitInvalid
	}
	return limit, nil
}

func Encode(cursor Cursor) (string, error) {
	createdAt := cursor.CreatedAt.UTC().UnixMilli()
	if createdAt <= 0 || !stableIDPattern.MatchString(cursor.ID) {
		return "", ErrCursorInvalid
	}
	payload := make([]byte, cursorPrefixLen+len(cursor.ID))
	payload[0] = cursorVersion
	binary.BigEndian.PutUint64(payload[1:cursorPrefixLen], uint64(createdAt))
	copy(payload[cursorPrefixLen:], cursor.ID)
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	if len(encoded) > MaximumCursorLen {
		return "", ErrCursorInvalid
	}
	return encoded, nil
}

func Decode(encoded string) (*Cursor, error) {
	if encoded == "" {
		return nil, nil
	}
	if len(encoded) > MaximumCursorLen {
		return nil, ErrCursorInvalid
	}
	payload, err := base64.RawURLEncoding.Strict().DecodeString(encoded)
	if err != nil || len(payload) <= cursorPrefixLen || payload[0] != cursorVersion {
		return nil, ErrCursorInvalid
	}
	createdAtUnixMilli := binary.BigEndian.Uint64(payload[1:cursorPrefixLen])
	if createdAtUnixMilli == 0 || createdAtUnixMilli > uint64(^uint64(0)>>1) {
		return nil, ErrCursorInvalid
	}
	id := string(payload[cursorPrefixLen:])
	if !stableIDPattern.MatchString(id) {
		return nil, ErrCursorInvalid
	}
	return &Cursor{CreatedAt: time.UnixMilli(int64(createdAtUnixMilli)).UTC(), ID: id}, nil
}

// EncodeSMS emits the SMS-only v2 cursor. Its kind byte prevents the generic
// v1 decoder used by Calls from accepting an SMS sequence boundary.
func EncodeSMS(cursor Cursor) (string, error) {
	if cursor.RecordSequence <= 0 || !stableIDPattern.MatchString(cursor.ID) {
		return "", ErrCursorInvalid
	}
	payload := make([]byte, smsCursorPrefixLen+len(cursor.ID))
	payload[0] = smsCursorVersion
	payload[1] = smsCursorKind
	binary.BigEndian.PutUint64(payload[2:smsCursorPrefixLen], uint64(cursor.RecordSequence))
	copy(payload[smsCursorPrefixLen:], cursor.ID)
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	if len(encoded) > MaximumCursorLen {
		return "", ErrCursorInvalid
	}
	return encoded, nil
}

// DecodeSMS accepts new SMS sequence cursors and legacy v1 time/ID cursors.
// The legacy boundary must subsequently be resolved by the SMS store against
// an existing row in the requested scope.
func DecodeSMS(encoded string) (*Cursor, error) {
	if encoded == "" {
		return nil, nil
	}
	if len(encoded) > MaximumCursorLen {
		return nil, ErrCursorInvalid
	}
	payload, err := base64.RawURLEncoding.Strict().DecodeString(encoded)
	if err != nil || len(payload) == 0 {
		return nil, ErrCursorInvalid
	}
	if payload[0] == cursorVersion {
		return Decode(encoded)
	}
	if len(payload) <= smsCursorPrefixLen || payload[0] != smsCursorVersion || payload[1] != smsCursorKind {
		return nil, ErrCursorInvalid
	}
	sequence := binary.BigEndian.Uint64(payload[2:smsCursorPrefixLen])
	if sequence == 0 || sequence > uint64(^uint64(0)>>1) {
		return nil, ErrCursorInvalid
	}
	id := string(payload[smsCursorPrefixLen:])
	if !stableIDPattern.MatchString(id) {
		return nil, ErrCursorInvalid
	}
	return &Cursor{RecordSequence: int64(sequence), ID: id}, nil
}
