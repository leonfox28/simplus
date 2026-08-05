package sms

import "time"

// InboundFragment is one durably staged concatenated SMS segment. It remains
// an internal recovery record and is never returned through the Web API.
type InboundFragment struct {
	GroupID         string
	SourceMessageID string
	LineID          string
	Sender          string
	Encoding        string
	Reference       byte
	Part            int
	Total           int
	UnitCount       int
	UserData        []byte
	ReceivedAt      time.Time
}
