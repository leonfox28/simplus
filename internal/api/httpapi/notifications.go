package httpapi

import (
	"context"
	"errors"
	"net/http"
	"time"

	notificationdomain "github.com/leonfox28/simplus/internal/domain/notification"

	"github.com/leonfox28/simplus/internal/api/openapi"
	notificationapp "github.com/leonfox28/simplus/internal/application/notification"
	"github.com/leonfox28/simplus/internal/application/realtime"
)

func (server *Server) ListNotificationChannels(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.requireAdministrator(w, r, false); !ok {
		return
	}
	if featureDisabled(server.notifications) {
		writeJSON(w, http.StatusServiceUnavailable, openapi.ApiError{Code: "NOTIFICATIONS_UNAVAILABLE", Retryable: true})
		return
	}
	items, err := server.notifications.List(r.Context())
	if err != nil {
		server.writeNotificationError(w, r, err)
		return
	}
	response := make([]openapi.NotificationChannel, 0, len(items))
	for _, item := range items {
		response = append(response, notificationChannelResponse(item))
	}
	writeJSON(w, http.StatusOK, openapi.NotificationChannelList{Channels: response})
}

func (server *Server) CreateNotificationChannel(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.requireAdministrator(w, r, true); !ok {
		return
	}
	if featureDisabled(server.notifications) {
		writeJSON(w, http.StatusServiceUnavailable, openapi.ApiError{Code: "NOTIFICATIONS_UNAVAILABLE", Retryable: true})
		return
	}
	var request openapi.NotificationChannelMutation
	if err := decodeJSON(w, r, &request); err != nil {
		writeJSON(w, http.StatusBadRequest, openapi.ApiError{Code: "NOTIFICATION_CHANNEL_REQUEST_INVALID", Retryable: false})
		return
	}
	item, err := server.notifications.Create(r.Context(), string(request.Provider), request.DisplayName, request.WebhookUrl, request.SigningSecret, request.Enabled, notificationEventStrings(request.EventKinds))
	if err != nil {
		server.writeNotificationError(w, r, err)
		return
	}
	server.publish([]realtime.Topic{realtime.TopicNotifications}, "")
	writeJSON(w, http.StatusCreated, notificationChannelResponse(item))
}

func (server *Server) GetFeishuNotificationBinding(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.requireAdministrator(w, r, false); !ok {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	if featureDisabled(server.notifications) {
		writeJSON(w, http.StatusServiceUnavailable, openapi.ApiError{Code: "FEISHU_BINDING_UNAVAILABLE", Retryable: true})
		return
	}
	writeJSON(w, http.StatusOK, feishuBindingResponse(server.notifications.FeishuBindingStatus()))
}

func (server *Server) StartFeishuNotificationBinding(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.requireAdministrator(w, r, true); !ok {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	if featureDisabled(server.notifications) {
		writeJSON(w, http.StatusServiceUnavailable, openapi.ApiError{Code: "FEISHU_BINDING_UNAVAILABLE", Retryable: true})
		return
	}
	state, err := server.notifications.StartFeishuBinding(r.Context())
	if err != nil {
		server.writeFeishuBindingError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, feishuBindingResponse(state))
}

func (server *Server) CancelFeishuNotificationBinding(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.requireAdministrator(w, r, true); !ok {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	if featureDisabled(server.notifications) {
		writeJSON(w, http.StatusServiceUnavailable, openapi.ApiError{Code: "FEISHU_BINDING_UNAVAILABLE", Retryable: true})
		return
	}
	state, err := server.notifications.CancelFeishuBinding()
	if err != nil {
		server.writeFeishuBindingError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, feishuBindingResponse(state))
}

func (server *Server) writeFeishuBindingError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, notificationapp.ErrBindingActive):
		writeJSON(w, http.StatusConflict, openapi.ApiError{Code: "FEISHU_BINDING_ACTIVE", Retryable: false})
	case errors.Is(err, notificationapp.ErrBindingNotCancelable):
		writeJSON(w, http.StatusConflict, openapi.ApiError{Code: "FEISHU_BINDING_NOT_CANCELLABLE", Retryable: false})
	case errors.Is(err, notificationapp.ErrBindingUnavailable):
		writeJSON(w, http.StatusServiceUnavailable, openapi.ApiError{Code: "FEISHU_BINDING_UNAVAILABLE", Retryable: true})
	case errors.Is(err, notificationapp.ErrFeishuProviderResultInvalid):
		writeJSON(w, http.StatusBadGateway, openapi.ApiError{Code: notificationapp.BindingErrorResultInvalid, Retryable: false})
	default:
		server.logger.WarnContext(r.Context(), "Feishu binding start failed")
		writeJSON(w, http.StatusBadGateway, openapi.ApiError{Code: notificationapp.BindingErrorProviderFailed, Retryable: true})
	}
}

func (server *Server) UpdateNotificationChannel(w http.ResponseWriter, r *http.Request, channelID string) {
	if _, ok := server.requireAdministrator(w, r, true); !ok {
		return
	}
	if featureDisabled(server.notifications) {
		writeJSON(w, http.StatusServiceUnavailable, openapi.ApiError{Code: "NOTIFICATIONS_UNAVAILABLE", Retryable: true})
		return
	}
	var request openapi.NotificationChannelMutation
	if err := decodeJSON(w, r, &request); err != nil {
		writeJSON(w, http.StatusBadRequest, openapi.ApiError{Code: "NOTIFICATION_CHANNEL_REQUEST_INVALID", Retryable: false})
		return
	}
	item, err := server.notifications.Update(r.Context(), channelID, string(request.Provider), request.DisplayName, request.WebhookUrl, request.SigningSecret, request.Enabled, notificationEventStrings(request.EventKinds))
	if err != nil {
		server.writeNotificationError(w, r, err)
		return
	}
	server.publish([]realtime.Topic{realtime.TopicNotifications}, "")
	writeJSON(w, http.StatusOK, notificationChannelResponse(item))
}

func (server *Server) DeleteNotificationChannel(w http.ResponseWriter, r *http.Request, channelID string) {
	if _, ok := server.requireAdministrator(w, r, true); !ok {
		return
	}
	if featureDisabled(server.notifications) {
		writeJSON(w, http.StatusServiceUnavailable, openapi.ApiError{Code: "NOTIFICATIONS_UNAVAILABLE", Retryable: true})
		return
	}
	if err := server.notifications.Delete(r.Context(), channelID); err != nil {
		server.writeNotificationError(w, r, err)
		return
	}
	server.publish([]realtime.Topic{realtime.TopicNotifications}, "")
	w.WriteHeader(http.StatusNoContent)
}

func (server *Server) TestNotificationChannel(w http.ResponseWriter, r *http.Request, channelID string) {
	if _, ok := server.requireAdministrator(w, r, true); !ok {
		return
	}
	if featureDisabled(server.notifications) {
		writeJSON(w, http.StatusServiceUnavailable, openapi.ApiError{Code: "NOTIFICATIONS_UNAVAILABLE", Retryable: true})
		return
	}
	item, err := server.notifications.Test(r.Context(), channelID)
	if err != nil {
		server.writeNotificationError(w, r, err)
		return
	}
	server.publish([]realtime.Topic{realtime.TopicNotifications}, "")
	writeJSON(w, http.StatusOK, notificationChannelResponse(item))
}

func (server *Server) writeNotificationError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, notificationapp.ErrChannelInvalid):
		writeJSON(w, http.StatusBadRequest, openapi.ApiError{Code: "NOTIFICATION_CHANNEL_REQUEST_INVALID", Retryable: false})
	case errors.Is(err, notificationapp.ErrChannelNotFound):
		writeJSON(w, http.StatusNotFound, openapi.ApiError{Code: "NOTIFICATION_CHANNEL_NOT_FOUND", Retryable: false})
	default:
		server.logger.WarnContext(r.Context(), "notification channel operation failed", "error", err)
		writeJSON(w, http.StatusBadGateway, openapi.ApiError{Code: "NOTIFICATION_DELIVERY_FAILED", Retryable: true})
	}
}

func (server *Server) enqueueNotice(ctx context.Context, event, objectID, message string) {
	if server == nil || featureDisabled(server.notifications) {
		return
	}
	enqueueCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := server.notifications.Enqueue(enqueueCtx, notificationdomain.Event{Key: event + ":" + objectID, Kind: event, ObjectID: objectID, Message: message, ObservedAt: time.Now().UTC()}); err != nil {
		server.logger.Warn("notification enqueue failed", "event", event, "error", err)
	}
	server.publish([]realtime.Topic{realtime.TopicNotifications}, "")
}
