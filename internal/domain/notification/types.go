package notification

import "time"

type DeliveryMode string

const (
	DeliveryModeWebhook   DeliveryMode = "webhook"
	DeliveryModeFeishuApp DeliveryMode = "feishu_app"
)

type Channel struct {
	ID, Provider, DisplayName                  string
	DeliveryMode                               DeliveryMode
	WebhookCiphertext, SigningSecretCiphertext []byte
	FeishuAppIDCiphertext                      []byte
	FeishuAppSecretCiphertext                  []byte
	FeishuRecipientOpenIDCiphertext            []byte
	WebhookHint                                string
	Enabled                                    bool
	EventKinds                                 []string
	LastDeliveryAt                             time.Time
	LastDeliveryStatus, LastErrorCode          string
	CreatedAt, UpdatedAt                       time.Time
}
