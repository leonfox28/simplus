package vowifihil

import (
	"errors"
	"testing"

	"github.com/strongswan/govici/vici"
)

func TestConsumeControlLogIgnoresMessages(t *testing.T) {
	events := func(yield func(*vici.Message, error) bool) {
		yield(vici.NewMessage(), nil)
	}
	if err := consumeControlLog(events); err != nil {
		t.Fatalf("consume control log: %v", err)
	}
}

func TestConsumeControlLogReturnsStreamingError(t *testing.T) {
	events := func(yield func(*vici.Message, error) bool) {
		yield(nil, errors.New("stream failed"))
	}
	if err := consumeControlLog(events); !errors.Is(err, ErrConnectionInitiateFailed) {
		t.Fatalf("consume control log error = %v, want %v", err, ErrConnectionInitiateFailed)
	}
}
