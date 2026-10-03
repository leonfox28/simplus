package smstransport

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

const ErrorOutcomeUnknownAfterRestart = sms.ErrorOutcomeUnknownAfterRestart
const ErrorSendOutcomeUnknown = sms.ErrorSendOutcomeUnknown
const ErrorAcceptedAwaitingReport = sms.ErrorAcceptedAwaitingReport
const ErrorCancelledBeforeDispatch = sms.ErrorCancelledBeforeDispatch
const ErrorTransportFailed = sms.ErrorTransportFailed
const ErrorSIMNotReady = sms.ErrorSIMNotReady
const ErrorSIMIdentityChanged = sms.ErrorSIMIdentityChanged
const ErrorEquipmentIdentityChanged = sms.ErrorEquipmentIdentityChanged
const ErrorRFOff = sms.ErrorRFOff
const ErrorRegistrationDenied = sms.ErrorRegistrationDenied
const ErrorNotRegistered = sms.ErrorNotRegistered
const ErrorStatusUnavailable = sms.ErrorStatusUnavailable
const ErrorDeviceStale = sms.ErrorDeviceStale
const SendStateAccepted = sms.SendStateAccepted
const SendStateSent = sms.SendStateSent
const SendStateFailed = sms.SendStateFailed
const SendStateUnconfirmed = sms.SendStateUnconfirmed
