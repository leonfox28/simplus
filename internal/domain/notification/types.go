package notification

import "time"

type Channel struct {
	ID, Provider, DisplayName                  string
	WebhookCiphertext, SigningSecretCiphertext []byte
	WebhookHint                                string
	Enabled                                    bool
	EventKinds                                 []string
	LastDeliveryAt                             time.Time
	LastDeliveryStatus, LastErrorCode          string
	CreatedAt, UpdatedAt                       time.Time
}
