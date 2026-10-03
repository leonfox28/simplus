package httpapi

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/leonfox28/simplus/internal/api/openapi"
	callapp "github.com/leonfox28/simplus/internal/application/calls"
	contactapp "github.com/leonfox28/simplus/internal/application/contacts"
	messageapp "github.com/leonfox28/simplus/internal/application/messaging"
	"github.com/leonfox28/simplus/internal/application/realtime"
	"github.com/leonfox28/simplus/internal/domain/call"
	"github.com/leonfox28/simplus/internal/domain/contact"
	"github.com/leonfox28/simplus/internal/domain/pagination"
	"github.com/leonfox28/simplus/internal/domain/sms"
)

func (server *Server) ListMessages(w http.ResponseWriter, r *http.Request, params openapi.ListMessagesParams) {
	if !server.requireBusinessAPI(w, r) {
		return
	}
	if server.messages == nil {
		writeJSON(w, http.StatusServiceUnavailable, openapi.ApiError{Code: "MESSAGING_UNAVAILABLE", Retryable: true})
		return
	}
	request := messageapp.PageRequest{}
	if params.Limit != nil {
		if *params.Limit == 0 {
			writeJSON(w, http.StatusBadRequest, openapi.ApiError{Code: "PAGE_LIMIT_INVALID", Retryable: false})
			return
		}
		request.Limit = *params.Limit
	}
	if params.Cursor != nil {
		if *params.Cursor == "" {
			writeJSON(w, http.StatusBadRequest, openapi.ApiError{Code: "PAGE_CURSOR_INVALID", Retryable: false})
			return
		}
		request.Cursor = *params.Cursor
	} else if _, present := r.URL.Query()["cursor"]; present {
		writeJSON(w, http.StatusBadRequest, openapi.ApiError{Code: "PAGE_CURSOR_INVALID", Retryable: false})
		return
	}
	linePresent := false
	if params.LineId != nil {
		request.LineID = *params.LineId
		linePresent = true
	} else if _, present := r.URL.Query()["lineId"]; present {
		linePresent = true
	}
	remotePresent := false
	if params.RemoteAddress != nil {
		request.RemoteAddress = *params.RemoteAddress
		remotePresent = true
	} else if _, present := r.URL.Query()["remoteAddress"]; present {
		remotePresent = true
	}
	if (linePresent && !remotePresent) || (linePresent && request.LineID == "") || (remotePresent && request.RemoteAddress == "") {
		writeJSON(w, http.StatusBadRequest, openapi.ApiError{Code: "MESSAGE_FILTER_INVALID", Retryable: false})
		return
	}
	page, err := server.messages.ListPage(r.Context(), request)
	if err != nil {
		if errors.Is(err, pagination.ErrCursorInvalid) {
			writeJSON(w, http.StatusBadRequest, openapi.ApiError{Code: "PAGE_CURSOR_INVALID", Retryable: false})
			return
		}
		if errors.Is(err, pagination.ErrLimitInvalid) {
			writeJSON(w, http.StatusBadRequest, openapi.ApiError{Code: "PAGE_LIMIT_INVALID", Retryable: false})
			return
		}
		if errors.Is(err, messageapp.ErrRequestInvalid) {
			writeJSON(w, http.StatusBadRequest, openapi.ApiError{Code: "MESSAGE_FILTER_INVALID", Retryable: false})
			return
		}
		server.logger.ErrorContext(r.Context(), "message history read failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, openapi.ApiError{Code: "MESSAGE_HISTORY_UNAVAILABLE", Retryable: true})
		return
	}
	response := make([]openapi.SMSMessage, 0, len(page.Messages))
	for _, message := range page.Messages {
		response = append(response, smsMessageResponse(message))
	}
	stats, err := server.messages.Stats(r.Context())
	if err != nil {
		server.logger.ErrorContext(r.Context(), "message history stats failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, openapi.ApiError{Code: "MESSAGE_HISTORY_UNAVAILABLE", Retryable: true})
		return
	}
	result := openapi.SMSMessageListResponse{
		Messages: response, TotalCount: stats.TotalCount, Capacity: stats.Capacity, NearCapacity: stats.NearCapacity,
	}
	if page.NextCursor != "" {
		result.NextCursor = &page.NextCursor
	}
	if page.ReadThroughToken != "" {
		result.ReadThroughToken = &page.ReadThroughToken
	}
	writeJSON(w, http.StatusOK, result)
}

func (server *Server) ListMessageConversations(w http.ResponseWriter, r *http.Request, params openapi.ListMessageConversationsParams) {
	if !server.requireBusinessAPI(w, r) {
		return
	}
	if server.messages == nil {
		writeJSON(w, http.StatusServiceUnavailable, openapi.ApiError{Code: "MESSAGING_UNAVAILABLE", Retryable: true})
		return
	}
	limit := 0
	if params.Limit != nil {
		if *params.Limit == 0 {
			writeJSON(w, http.StatusBadRequest, openapi.ApiError{Code: "PAGE_LIMIT_INVALID", Retryable: false})
			return
		}
		limit = *params.Limit
	}
	cursor := ""
	if params.Cursor != nil {
		if *params.Cursor == "" {
			writeJSON(w, http.StatusBadRequest, openapi.ApiError{Code: "PAGE_CURSOR_INVALID", Retryable: false})
			return
		}
		cursor = *params.Cursor
	} else if _, present := r.URL.Query()["cursor"]; present {
		writeJSON(w, http.StatusBadRequest, openapi.ApiError{Code: "PAGE_CURSOR_INVALID", Retryable: false})
		return
	}
	page, err := server.messages.ListConversationPage(r.Context(), limit, cursor)
	if err != nil {
		switch {
		case errors.Is(err, pagination.ErrCursorInvalid):
			writeJSON(w, http.StatusBadRequest, openapi.ApiError{Code: "PAGE_CURSOR_INVALID", Retryable: false})
		case errors.Is(err, pagination.ErrLimitInvalid):
			writeJSON(w, http.StatusBadRequest, openapi.ApiError{Code: "PAGE_LIMIT_INVALID", Retryable: false})
		default:
			server.logger.ErrorContext(r.Context(), "message conversation read failed", "error", err)
			writeJSON(w, http.StatusInternalServerError, openapi.ApiError{Code: "MESSAGE_HISTORY_UNAVAILABLE", Retryable: true})
		}
		return
	}
	stats, err := server.messages.Stats(r.Context())
	if err != nil {
		server.logger.ErrorContext(r.Context(), "message conversation stats failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, openapi.ApiError{Code: "MESSAGE_HISTORY_UNAVAILABLE", Retryable: true})
		return
	}
	conversations := make([]openapi.SMSConversationSummary, 0, len(page.Conversations))
	for _, item := range page.Conversations {
		response := openapi.SMSConversationSummary{
			RemoteAddress: item.RemoteAddress,
			LastMessage:   smsMessageResponse(item.LastMessage),
			UnreadCount:   item.UnreadCount,
		}
		if item.LastOutboundLineID != "" {
			response.LastOutboundLineId = &item.LastOutboundLineID
		}
		conversations = append(conversations, response)
	}
	result := openapi.SMSConversationListResponse{
		Conversations: conversations, ConversationTotalCount: page.TotalCount,
		MessageTotalCount: stats.TotalCount, Capacity: stats.Capacity, NearCapacity: stats.NearCapacity,
	}
	if page.NextCursor != "" {
		result.NextCursor = &page.NextCursor
	}
	writeJSON(w, http.StatusOK, result)
}

func (server *Server) MarkMessageConversationRead(w http.ResponseWriter, r *http.Request) {
	if !server.requireBusinessAPI(w, r) {
		return
	}
	if server.messages == nil {
		writeJSON(w, http.StatusServiceUnavailable, openapi.ApiError{Code: "MESSAGING_UNAVAILABLE", Retryable: true})
		return
	}
	var request openapi.MarkSMSConversationReadRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeJSON(w, http.StatusBadRequest, openapi.ApiError{Code: "MESSAGE_READ_STATE_INVALID", Retryable: false})
		return
	}
	changed, err := server.messages.MarkConversationRead(r.Context(), request.RemoteAddress, request.ReadThroughToken)
	if err != nil {
		switch {
		case errors.Is(err, messageapp.ErrRequestInvalid):
			writeJSON(w, http.StatusBadRequest, openapi.ApiError{Code: "MESSAGE_READ_STATE_INVALID", Retryable: false})
		case errors.Is(err, sms.ErrMessageNotFound):
			writeJSON(w, http.StatusNotFound, openapi.ApiError{Code: "MESSAGE_READ_BOUNDARY_NOT_FOUND", Retryable: false})
		default:
			server.logger.ErrorContext(r.Context(), "message read state update failed", "error", err)
			writeJSON(w, http.StatusInternalServerError, openapi.ApiError{Code: "MESSAGE_PERSIST_FAILED", Retryable: true})
		}
		return
	}
	if changed {
		server.publish([]realtime.Topic{realtime.TopicMessages}, "")
	}
	w.WriteHeader(http.StatusNoContent)
}

func (server *Server) DeleteMessage(w http.ResponseWriter, r *http.Request, messageID string) {
	if !server.requireBusinessAPI(w, r) {
		return
	}
	if server.messages == nil {
		writeJSON(w, http.StatusServiceUnavailable, openapi.ApiError{Code: "MESSAGING_UNAVAILABLE", Retryable: true})
		return
	}
	if err := server.messages.Delete(r.Context(), messageID); err != nil {
		switch {
		case errors.Is(err, messageapp.ErrRequestInvalid):
			writeJSON(w, http.StatusBadRequest, openapi.ApiError{Code: "MESSAGE_REQUEST_INVALID", Retryable: false})
		case errors.Is(err, sms.ErrMessageNotFound):
			writeJSON(w, http.StatusNotFound, openapi.ApiError{Code: "MESSAGE_NOT_FOUND", Retryable: false})
		default:
			server.logger.ErrorContext(r.Context(), "message deletion failed", "message_id", messageID, "error", err)
			writeJSON(w, http.StatusInternalServerError, openapi.ApiError{Code: "MESSAGE_PERSIST_FAILED", Retryable: true})
		}
		return
	}
	server.publish([]realtime.Topic{realtime.TopicMessages}, "")
	w.WriteHeader(http.StatusNoContent)
}

func (server *Server) SendMessage(w http.ResponseWriter, r *http.Request) {
	if !server.requireBusinessAPI(w, r) {
		return
	}
	if server.messages == nil {
		writeJSON(w, http.StatusServiceUnavailable, openapi.ApiError{Code: "MESSAGING_UNAVAILABLE", Retryable: true})
		return
	}
	var request openapi.SendSMSRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeJSON(w, http.StatusBadRequest, openapi.ApiError{Code: "MESSAGE_REQUEST_INVALID", Retryable: false})
		return
	}
	result, err := server.messages.Send(r.Context(), messageapp.SendRequest{
		OperationID: request.OperationId,
		LineID:      request.LineId,
		Destination: request.Destination,
		Body:        request.Body,
	})
	if err != nil {
		server.writeMessageError(w, r, err, request.OperationId, request.LineId)
		return
	}
	status := http.StatusCreated
	if result.Replayed {
		status = http.StatusOK
	}
	if !result.Replayed && result.Message.Status == sms.StatusFailed {
		server.enqueueNotice(r.Context(), "sms.failed", result.Message.ID, fmt.Sprintf("[Simplus] 短信发送失败 · 线路 %s · %s", result.Message.LineID, result.Message.ErrorCode))
	}
	server.publish([]realtime.Topic{realtime.TopicMessages}, "")
	writeJSON(w, status, smsMessageResponse(result.Message))
}

func (server *Server) ListCalls(w http.ResponseWriter, r *http.Request, params openapi.ListCallsParams) {
	if !server.requireBusinessAPI(w, r) {
		return
	}
	if featureDisabled(server.calls) {
		writeJSON(w, http.StatusServiceUnavailable, openapi.ApiError{Code: "CALLS_UNAVAILABLE", Retryable: false})
		return
	}
	limit := 0
	cursor := ""
	if params.Limit != nil {
		if *params.Limit == 0 {
			writeJSON(w, http.StatusBadRequest, openapi.ApiError{Code: "PAGE_LIMIT_INVALID", Retryable: false})
			return
		}
		limit = *params.Limit
	}
	if params.Cursor != nil {
		if *params.Cursor == "" {
			writeJSON(w, http.StatusBadRequest, openapi.ApiError{Code: "PAGE_CURSOR_INVALID", Retryable: false})
			return
		}
		cursor = *params.Cursor
	} else if _, present := r.URL.Query()["cursor"]; present {
		writeJSON(w, http.StatusBadRequest, openapi.ApiError{Code: "PAGE_CURSOR_INVALID", Retryable: false})
		return
	}
	page, err := server.calls.List(r.Context(), limit, cursor)
	if err != nil {
		if errors.Is(err, pagination.ErrCursorInvalid) {
			writeJSON(w, http.StatusBadRequest, openapi.ApiError{Code: "PAGE_CURSOR_INVALID", Retryable: false})
			return
		}
		if errors.Is(err, pagination.ErrLimitInvalid) {
			writeJSON(w, http.StatusBadRequest, openapi.ApiError{Code: "PAGE_LIMIT_INVALID", Retryable: false})
			return
		}
		server.writeCallError(w, r, err)
		return
	}
	response := make([]openapi.Call, 0, len(page.Calls))
	for _, value := range page.Calls {
		response = append(response, callResponse(value))
	}
	result := openapi.CallListResponse{Calls: response}
	if page.NextCursor != "" {
		result.NextCursor = &page.NextCursor
	}
	writeJSON(w, http.StatusOK, result)
}

func (server *Server) DialCall(w http.ResponseWriter, r *http.Request) { server.startCall(w, r, false) }

func (server *Server) SimulateIncomingCall(w http.ResponseWriter, r *http.Request) {
	server.startCall(w, r, true)
}

func (server *Server) startCall(w http.ResponseWriter, r *http.Request, incoming bool) {
	if !server.requireBusinessAPI(w, r) {
		return
	}
	if featureDisabled(server.calls) {
		writeJSON(w, http.StatusServiceUnavailable, openapi.ApiError{Code: "CALLS_UNAVAILABLE", Retryable: false})
		return
	}
	var request openapi.CallStartRequest
	if decodeJSON(w, r, &request) != nil {
		writeJSON(w, http.StatusBadRequest, openapi.ApiError{Code: "CALL_REQUEST_INVALID", Retryable: false})
		return
	}
	var value call.Record
	var replayed bool
	var err error
	if incoming {
		value, replayed, err = server.calls.Incoming(r.Context(), request.OperationId, request.LineId, request.RemoteAddress)
	} else {
		value, replayed, err = server.calls.Dial(r.Context(), request.OperationId, request.LineId, request.RemoteAddress)
	}
	if err != nil {
		server.writeCallError(w, r, err)
		return
	}
	status := http.StatusCreated
	if replayed {
		status = http.StatusOK
	}
	if incoming && !replayed {
		server.enqueueNotice(r.Context(), "call.incoming", value.ID, fmt.Sprintf("[Simplus] 新来电 · 线路 %s", value.LineID))
	}
	attention := realtime.Attention("")
	if incoming && !replayed {
		attention = realtime.AttentionCallIncoming
	}
	server.publish([]realtime.Topic{realtime.TopicCalls}, attention)
	writeJSON(w, status, callResponse(value))
}

func (server *Server) ControlCall(w http.ResponseWriter, r *http.Request, callID string) {
	if !server.requireBusinessAPI(w, r) {
		return
	}
	if featureDisabled(server.calls) {
		writeJSON(w, http.StatusServiceUnavailable, openapi.ApiError{Code: "CALLS_UNAVAILABLE", Retryable: false})
		return
	}
	var request openapi.CallActionRequest
	if decodeJSON(w, r, &request) != nil {
		writeJSON(w, http.StatusBadRequest, openapi.ApiError{Code: "CALL_REQUEST_INVALID", Retryable: false})
		return
	}
	var value call.Record
	var err error
	switch request.Action {
	case openapi.Answer:
		value, err = server.calls.Answer(r.Context(), callID)
	case openapi.Reject:
		value, err = server.calls.Reject(r.Context(), callID)
	case openapi.Hangup:
		value, err = server.calls.Hangup(r.Context(), callID)
	case openapi.Dtmf:
		if request.Digits == nil {
			err = callapp.ErrInvalid
		} else {
			value, err = server.calls.DTMF(r.Context(), callID, *request.Digits)
		}
	default:
		err = callapp.ErrInvalid
	}
	if err != nil {
		server.writeCallError(w, r, err)
		return
	}
	if request.Action == openapi.Reject && value.Direction == call.DirectionInbound {
		server.enqueueNotice(r.Context(), "call.missed", value.ID, fmt.Sprintf("[Simplus] 未接来电 · 线路 %s", value.LineID))
	}
	server.publish([]realtime.Topic{realtime.TopicCalls}, "")
	writeJSON(w, http.StatusOK, callResponse(value))
}

func (server *Server) writeCallError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, callapp.ErrInvalid):
		writeJSON(w, http.StatusBadRequest, openapi.ApiError{Code: "CALL_REQUEST_INVALID", Retryable: false})
	case errors.Is(err, callapp.ErrUnsafeNumber):
		writeJSON(w, http.StatusBadRequest, openapi.ApiError{Code: "CALL_NUMBER_FORBIDDEN", Retryable: false})
	case errors.Is(err, callapp.ErrLineUnavailable):
		writeJSON(w, http.StatusConflict, openapi.ApiError{Code: "CALL_LINE_UNAVAILABLE", Retryable: true})
	case errors.Is(err, callapp.ErrLineBusy):
		writeJSON(w, http.StatusConflict, openapi.ApiError{Code: "CALL_LINE_BUSY", Retryable: true})
	case errors.Is(err, call.ErrNotFound):
		writeJSON(w, http.StatusNotFound, openapi.ApiError{Code: "CALL_NOT_FOUND", Retryable: false})
	case errors.Is(err, call.ErrStateConflict):
		writeJSON(w, http.StatusConflict, openapi.ApiError{Code: "CALL_STATE_CONFLICT", Retryable: false})
	default:
		server.logger.ErrorContext(r.Context(), "call operation failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, openapi.ApiError{Code: "CALL_PERSIST_FAILED", Retryable: true})
	}
}

func (server *Server) ListContacts(w http.ResponseWriter, r *http.Request) {
	if !server.requireBusinessAPI(w, r) {
		return
	}
	if server.contacts == nil {
		writeJSON(w, http.StatusServiceUnavailable, openapi.ApiError{Code: "CONTACTS_UNAVAILABLE", Retryable: true})
		return
	}
	values, err := server.contacts.List(r.Context())
	if err != nil {
		server.logger.ErrorContext(r.Context(), "contacts read failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, openapi.ApiError{Code: "CONTACTS_READ_FAILED", Retryable: true})
		return
	}
	response := make([]openapi.Contact, 0, len(values))
	for _, value := range values {
		response = append(response, contactResponse(value))
	}
	writeJSON(w, http.StatusOK, openapi.ContactListResponse{Contacts: response})
}

func (server *Server) CreateContact(w http.ResponseWriter, r *http.Request) {
	if !server.requireBusinessAPI(w, r) {
		return
	}
	var request openapi.ContactMutationRequest
	if server.contacts == nil || decodeJSON(w, r, &request) != nil {
		writeJSON(w, http.StatusBadRequest, openapi.ApiError{Code: "CONTACT_REQUEST_INVALID", Retryable: false})
		return
	}
	value, err := server.contacts.Create(r.Context(), request.DisplayName, request.PhoneNumber)
	if err != nil {
		server.writeContactError(w, r, err)
		return
	}
	server.publish([]realtime.Topic{realtime.TopicContacts}, "")
	writeJSON(w, http.StatusCreated, contactResponse(value))
}

func (server *Server) UpdateContact(w http.ResponseWriter, r *http.Request, contactID string) {
	if !server.requireBusinessAPI(w, r) {
		return
	}
	var request openapi.ContactMutationRequest
	if server.contacts == nil || decodeJSON(w, r, &request) != nil {
		writeJSON(w, http.StatusBadRequest, openapi.ApiError{Code: "CONTACT_REQUEST_INVALID", Retryable: false})
		return
	}
	value, err := server.contacts.Update(r.Context(), contactID, request.DisplayName, request.PhoneNumber)
	if err != nil {
		server.writeContactError(w, r, err)
		return
	}
	server.publish([]realtime.Topic{realtime.TopicContacts}, "")
	writeJSON(w, http.StatusOK, contactResponse(value))
}

func (server *Server) DeleteContact(w http.ResponseWriter, r *http.Request, contactID string) {
	if !server.requireBusinessAPI(w, r) {
		return
	}
	if server.contacts == nil {
		writeJSON(w, http.StatusServiceUnavailable, openapi.ApiError{Code: "CONTACTS_UNAVAILABLE", Retryable: true})
		return
	}
	if err := server.contacts.Delete(r.Context(), contactID); err != nil {
		server.writeContactError(w, r, err)
		return
	}
	server.publish([]realtime.Topic{realtime.TopicContacts}, "")
	w.WriteHeader(http.StatusNoContent)
}

func (server *Server) writeContactError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, contactapp.ErrInvalid):
		writeJSON(w, http.StatusBadRequest, openapi.ApiError{Code: "CONTACT_REQUEST_INVALID", Retryable: false})
	case errors.Is(err, contact.ErrNotFound):
		writeJSON(w, http.StatusNotFound, openapi.ApiError{Code: "CONTACT_NOT_FOUND", Retryable: false})
	case errors.Is(err, contact.ErrPhoneConflict):
		writeJSON(w, http.StatusConflict, openapi.ApiError{Code: "CONTACT_PHONE_CONFLICT", Retryable: false})
	default:
		server.logger.ErrorContext(r.Context(), "contact mutation failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, openapi.ApiError{Code: "CONTACT_PERSIST_FAILED", Retryable: true})
	}
}

func (server *Server) writeMessageError(w http.ResponseWriter, r *http.Request, err error, operationID, lineID string) {
	switch {
	case errors.Is(err, messageapp.ErrRequestInvalid):
		writeJSON(w, http.StatusBadRequest, openapi.ApiError{Code: "MESSAGE_REQUEST_INVALID", Retryable: false})
	case errors.Is(err, sms.ErrOperationConflict):
		writeJSON(w, http.StatusConflict, openapi.ApiError{Code: "MESSAGE_OPERATION_CONFLICT", Retryable: false})
	case errors.Is(err, messageapp.ErrLineNotFound):
		writeJSON(w, http.StatusNotFound, openapi.ApiError{Code: "MESSAGE_LINE_NOT_FOUND", Retryable: false})
	case errors.Is(err, messageapp.ErrLineUnavailable):
		writeJSON(w, http.StatusConflict, openapi.ApiError{Code: "MESSAGE_LINE_UNAVAILABLE", Retryable: true})
	case errors.Is(err, messageapp.ErrLineUnsupported):
		writeJSON(w, http.StatusUnprocessableEntity, openapi.ApiError{Code: "MESSAGE_LINE_UNSUPPORTED", Retryable: false})
	case errors.Is(err, messageapp.ErrTransportUnavailable):
		writeJSON(w, http.StatusServiceUnavailable, openapi.ApiError{Code: "MESSAGE_TRANSPORT_UNAVAILABLE", Retryable: true})
	case errors.Is(err, messageapp.ErrTransportAmbiguous):
		writeJSON(w, http.StatusConflict, openapi.ApiError{Code: "MESSAGE_TRANSPORT_AMBIGUOUS", Retryable: false})
	case errors.Is(err, messageapp.ErrInventoryUnavailable):
		server.logger.ErrorContext(r.Context(), "message inventory unavailable", "operation_id", operationID, "line_id", lineID, "error", err)
		writeJSON(w, http.StatusServiceUnavailable, openapi.ApiError{Code: "MESSAGE_INVENTORY_UNAVAILABLE", Retryable: true})
	default:
		server.logger.ErrorContext(r.Context(), "message operation failed", "operation_id", operationID, "line_id", lineID, "error", err)
		writeJSON(w, http.StatusInternalServerError, openapi.ApiError{Code: "MESSAGE_PERSIST_FAILED", Retryable: true})
	}
}

func smsMessageResponse(message sms.Message) openapi.SMSMessage {
	response := openapi.SMSMessage{
		Id: message.ID, OperationId: message.OperationID, Direction: openapi.SMSDirection(message.Direction),
		LineId: message.LineID, RemoteAddress: message.RemoteAddress, Body: message.Body,
		Status: openapi.SMSStatus(message.Status), ProviderMessageId: message.ProviderMessageID, ErrorCode: message.ErrorCode,
		CreatedAt: message.CreatedAt.UTC(), UpdatedAt: message.UpdatedAt.UTC(),
	}
	if message.SentAt != nil {
		sentAt := message.SentAt.UTC()
		response.SentAt = &sentAt
	}
	return response
}
