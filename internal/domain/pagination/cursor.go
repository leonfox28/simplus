package pagination

import (
	"encoding/base64"
	"encoding/binary"
	"errors"
	"regexp"
	"time"
)

const (
	DefaultLimit     = 20
	MaximumLimit     = 50
	MaximumCursorLen = 256
	cursorVersion    = byte(1)
	cursorPrefixLen  = 9
)

var (
	ErrCursorInvalid = errors.New("page cursor is invalid")
	ErrLimitInvalid  = errors.New("page limit is invalid")
	stableIDPattern  = regexp.MustCompile(`^[A-Za-z0-9_-]{16,128}$`)
)

// Cursor is the decoded keyset boundary shared by durable history stores.
// The public representation is opaque and contains only a version, time, and
// stable business identifier.
type Cursor struct {
	CreatedAt time.Time
	ID        string
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
