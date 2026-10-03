package messaging

import "github.com/leonfox28/simplus/internal/domain/sms"

type SendSMSCommand = sms.SendSMSCommand
type SendSMSResult = sms.SendSMSResult
type Sender = sms.Sender
type InboxMessageReference = sms.InboxMessageReference
type InboxMessage = sms.InboxMessage
type InboxTarget = sms.InboxTarget
type Inbox = sms.Inbox
type SubmitReport = sms.SubmitReport
type SubmitReportInbox = sms.SubmitReportInbox
type TransportError = sms.TransportError
