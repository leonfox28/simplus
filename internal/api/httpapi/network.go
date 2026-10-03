package httpapi

import (
	"errors"
	"net/http"
	"strings"

	"github.com/leonfox28/simplus/internal/api/openapi"
	lineegressapp "github.com/leonfox28/simplus/internal/application/lineegress"
	mihomoapp "github.com/leonfox28/simplus/internal/application/mihomo"
	"github.com/leonfox28/simplus/internal/application/realtime"
	vowifiapp "github.com/leonfox28/simplus/internal/application/vowifi"
)

func (server *Server) GetMihomoCoreStatus(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.requireAdministrator(w, r, false); !ok {
		return
	}
	if featureDisabled(server.mihomoCore) {
		writeJSON(w, http.StatusServiceUnavailable, openapi.ApiError{Code: "MIHOMO_CORE_UNAVAILABLE", Retryable: true})
		return
	}
	status, err := server.mihomoCore.Status()
	if err != nil {
		server.logger.ErrorContext(r.Context(), "read Mihomo core status failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, openapi.ApiError{Code: "MIHOMO_CORE_STATUS_FAILED", Retryable: true})
		return
	}
	writeJSON(w, http.StatusOK, mihomoCoreStatusResponse(status))
}

func (server *Server) GetMihomoDashboardStatus(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.requireAdministrator(w, r, false); !ok {
		return
	}
	if featureDisabled(server.mihomoDashboard) {
		writeJSON(w, http.StatusServiceUnavailable, openapi.ApiError{Code: "MIHOMO_DASHBOARD_UNAVAILABLE", Retryable: true})
		return
	}
	status, err := server.mihomoDashboard.Ensure()
	if err != nil {
		server.logger.ErrorContext(r.Context(), "read Mihomo dashboard status failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, openapi.ApiError{Code: "MIHOMO_DASHBOARD_STATUS_FAILED", Retryable: true})
		return
	}
	writeJSON(w, http.StatusOK, openapi.MihomoDashboardStatus{Available: status.Available, Version: status.Version, ControllerAddress: status.ControllerAddress, Url: status.URL, Secret: status.Secret})
}

func (server *Server) GetLatestMihomoCore(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.requireAdministrator(w, r, false); !ok {
		return
	}
	if featureDisabled(server.mihomoCore) {
		writeJSON(w, http.StatusServiceUnavailable, openapi.ApiError{Code: "MIHOMO_CORE_UNAVAILABLE", Retryable: true})
		return
	}
	candidate, err := server.mihomoCore.CheckLatest(r.Context())
	if err != nil {
		server.logger.WarnContext(r.Context(), "check official Mihomo release failed", "error", err)
		writeJSON(w, http.StatusBadGateway, openapi.ApiError{Code: "MIHOMO_RELEASE_CHECK_FAILED", Retryable: true})
		return
	}
	writeJSON(w, http.StatusOK, openapi.MihomoCoreCandidate{Version: candidate.Version, AssetName: candidate.AssetName, Sha256: candidate.SHA256, Size: candidate.Size, Architecture: openapi.MihomoCoreCandidateArchitecture(candidate.Architecture)})
}

func (server *Server) InstallLatestMihomoCore(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.requireAdministrator(w, r, true); !ok {
		return
	}
	if featureDisabled(server.mihomoCore) {
		writeJSON(w, http.StatusServiceUnavailable, openapi.ApiError{Code: "MIHOMO_CORE_UNAVAILABLE", Retryable: true})
		return
	}
	status, err := server.mihomoCore.InstallLatest(r.Context())
	if errors.Is(err, mihomoapp.ErrVersionAlreadyInstalled) {
		writeJSON(w, http.StatusConflict, openapi.ApiError{Code: "MIHOMO_VERSION_ALREADY_INSTALLED", Retryable: false})
		return
	}
	if err != nil {
		server.logger.WarnContext(r.Context(), "install official Mihomo core failed", "error", err)
		writeJSON(w, http.StatusBadGateway, openapi.ApiError{Code: "MIHOMO_CORE_INSTALL_FAILED", Retryable: true})
		return
	}
	server.publish([]realtime.Topic{realtime.TopicMihomo}, "")
	writeJSON(w, http.StatusOK, mihomoCoreStatusResponse(status))
}

func (server *Server) ListMihomoSubscriptions(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.requireAdministrator(w, r, false); !ok {
		return
	}
	if featureDisabled(server.mihomoSubscriptions) {
		writeJSON(w, http.StatusServiceUnavailable, openapi.ApiError{Code: "MIHOMO_SUBSCRIPTIONS_UNAVAILABLE", Retryable: true})
		return
	}
	items, err := server.mihomoSubscriptions.List(r.Context())
	if err != nil {
		server.writeMihomoSubscriptionError(w, r, err)
		return
	}
	response := make([]openapi.MihomoSubscription, 0, len(items))
	for _, item := range items {
		response = append(response, mihomoSubscriptionResponse(item))
	}
	writeJSON(w, http.StatusOK, openapi.MihomoSubscriptionList{Subscriptions: response})
}

func (server *Server) CreateMihomoSubscription(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.requireAdministrator(w, r, true); !ok {
		return
	}
	if featureDisabled(server.mihomoSubscriptions) {
		writeJSON(w, http.StatusServiceUnavailable, openapi.ApiError{Code: "MIHOMO_SUBSCRIPTIONS_UNAVAILABLE", Retryable: true})
		return
	}
	var request openapi.MihomoSubscriptionCreateRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeJSON(w, http.StatusBadRequest, openapi.ApiError{Code: "MIHOMO_SUBSCRIPTION_REQUEST_INVALID", Retryable: false})
		return
	}
	item, err := server.mihomoSubscriptions.Create(r.Context(), "", request.Url, true)
	if err != nil {
		server.writeMihomoSubscriptionError(w, r, err)
		return
	}
	item, _, err = server.mihomoSubscriptions.Refresh(r.Context(), item.ID)
	if err != nil {
		server.writeMihomoSubscriptionError(w, r, err)
		return
	}
	server.publish([]realtime.Topic{realtime.TopicMihomo}, "")
	writeJSON(w, http.StatusCreated, mihomoSubscriptionResponse(item))
}

func (server *Server) UpdateMihomoSubscription(w http.ResponseWriter, r *http.Request, subscriptionID string) {
	if _, ok := server.requireAdministrator(w, r, true); !ok {
		return
	}
	if featureDisabled(server.mihomoSubscriptions) {
		writeJSON(w, http.StatusServiceUnavailable, openapi.ApiError{Code: "MIHOMO_SUBSCRIPTIONS_UNAVAILABLE", Retryable: true})
		return
	}
	var request openapi.MihomoSubscriptionMutation
	if err := decodeJSON(w, r, &request); err != nil {
		writeJSON(w, http.StatusBadRequest, openapi.ApiError{Code: "MIHOMO_SUBSCRIPTION_REQUEST_INVALID", Retryable: false})
		return
	}
	item, err := server.mihomoSubscriptions.Update(r.Context(), subscriptionID, request.DisplayName, request.Url, request.Enabled)
	if err != nil {
		server.writeMihomoSubscriptionError(w, r, err)
		return
	}
	if strings.TrimSpace(request.Url) != "" {
		item, _, err = server.mihomoSubscriptions.Refresh(r.Context(), subscriptionID)
		if err != nil {
			server.writeMihomoSubscriptionError(w, r, err)
			return
		}
	}
	server.publish([]realtime.Topic{realtime.TopicMihomo}, "")
	writeJSON(w, http.StatusOK, mihomoSubscriptionResponse(item))
}

func (server *Server) DeleteMihomoSubscription(w http.ResponseWriter, r *http.Request, subscriptionID string) {
	if _, ok := server.requireAdministrator(w, r, true); !ok {
		return
	}
	if featureDisabled(server.mihomoSubscriptions) {
		writeJSON(w, http.StatusServiceUnavailable, openapi.ApiError{Code: "MIHOMO_SUBSCRIPTIONS_UNAVAILABLE", Retryable: true})
		return
	}
	if err := server.mihomoSubscriptions.Delete(r.Context(), subscriptionID); err != nil {
		server.writeMihomoSubscriptionError(w, r, err)
		return
	}
	server.publish([]realtime.Topic{realtime.TopicMihomo}, "")
	w.WriteHeader(http.StatusNoContent)
}

func (server *Server) RefreshMihomoSubscription(w http.ResponseWriter, r *http.Request, subscriptionID string) {
	if _, ok := server.requireAdministrator(w, r, true); !ok {
		return
	}
	if featureDisabled(server.mihomoSubscriptions) {
		writeJSON(w, http.StatusServiceUnavailable, openapi.ApiError{Code: "MIHOMO_SUBSCRIPTIONS_UNAVAILABLE", Retryable: true})
		return
	}
	item, nodes, err := server.mihomoSubscriptions.Refresh(r.Context(), subscriptionID)
	if err != nil {
		server.writeMihomoSubscriptionError(w, r, err)
		return
	}
	server.publish([]realtime.Topic{realtime.TopicMihomo}, "")
	writeJSON(w, http.StatusOK, openapi.MihomoSubscriptionRefresh{Subscription: mihomoSubscriptionResponse(item), Nodes: mihomoNodeResponses(nodes)})
}

func (server *Server) ListMihomoSubscriptionNodes(w http.ResponseWriter, r *http.Request, subscriptionID string) {
	if _, ok := server.requireAdministrator(w, r, false); !ok {
		return
	}
	if featureDisabled(server.mihomoSubscriptions) {
		writeJSON(w, http.StatusServiceUnavailable, openapi.ApiError{Code: "MIHOMO_SUBSCRIPTIONS_UNAVAILABLE", Retryable: true})
		return
	}
	nodes, err := server.mihomoSubscriptions.Nodes(r.Context(), subscriptionID)
	if err != nil {
		server.writeMihomoSubscriptionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, openapi.MihomoNodeList{Nodes: mihomoNodeResponses(nodes)})
}

func (server *Server) writeMihomoSubscriptionError(w http.ResponseWriter, r *http.Request, err error) {
	var refreshError *mihomoapp.SubscriptionRefreshError
	switch {
	case errors.Is(err, mihomoapp.ErrSubscriptionInvalid):
		writeJSON(w, http.StatusBadRequest, openapi.ApiError{Code: "MIHOMO_SUBSCRIPTION_REQUEST_INVALID", Retryable: false})
	case errors.Is(err, mihomoapp.ErrSubscriptionNotFound):
		writeJSON(w, http.StatusNotFound, openapi.ApiError{Code: "MIHOMO_SUBSCRIPTION_NOT_FOUND", Retryable: false})
	case errors.As(err, &refreshError):
		server.logger.WarnContext(r.Context(), "Mihomo subscription refresh failed", "error_code", refreshError.Code)
		writeJSON(w, http.StatusBadGateway, openapi.ApiError{Code: "MIHOMO_SUBSCRIPTION_REFRESH_FAILED", Retryable: true})
	default:
		server.logger.WarnContext(r.Context(), "Mihomo subscription operation failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, openapi.ApiError{Code: "MIHOMO_SUBSCRIPTION_PERSIST_FAILED", Retryable: true})
	}
}

func (server *Server) ListLineEgressBindings(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.requireAdministrator(w, r, false); !ok {
		return
	}
	if featureDisabled(server.lineEgress) {
		writeJSON(w, http.StatusServiceUnavailable, openapi.ApiError{Code: "LINE_EGRESS_UNAVAILABLE", Retryable: true})
		return
	}
	items, err := server.lineEgress.List(r.Context())
	if err != nil {
		server.logger.WarnContext(r.Context(), "line egress list failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, openapi.ApiError{Code: "LINE_EGRESS_READ_FAILED", Retryable: true})
		return
	}
	bindings := make([]openapi.LineEgressBinding, 0, len(items))
	for _, item := range items {
		bindings = append(bindings, lineEgressResponse(item))
	}
	writeJSON(w, http.StatusOK, openapi.LineEgressBindingList{Bindings: bindings})
}

func (server *Server) PutLineEgressBinding(w http.ResponseWriter, r *http.Request, lineID string) {
	if _, ok := server.requireAdministrator(w, r, true); !ok {
		return
	}
	if featureDisabled(server.lineEgress) {
		writeJSON(w, http.StatusServiceUnavailable, openapi.ApiError{Code: "LINE_EGRESS_UNAVAILABLE", Retryable: true})
		return
	}
	var request openapi.LineEgressBindingMutation
	if err := decodeJSON(w, r, &request); err != nil || !request.Mode.Valid() {
		writeJSON(w, http.StatusBadRequest, openapi.ApiError{Code: "LINE_EGRESS_REQUEST_INVALID", Retryable: false})
		return
	}
	item, err := server.lineEgress.Put(r.Context(), lineID, string(request.Mode), request.CountryCode)
	if err != nil {
		switch {
		case errors.Is(err, lineegressapp.ErrInvalidBinding):
			writeJSON(w, http.StatusBadRequest, openapi.ApiError{Code: "LINE_EGRESS_REQUEST_INVALID", Retryable: false})
		case errors.Is(err, lineegressapp.ErrLineNotFound):
			writeJSON(w, http.StatusNotFound, openapi.ApiError{Code: "LINE_NOT_FOUND", Retryable: false})
		case errors.Is(err, lineegressapp.ErrLineUnsupported):
			writeJSON(w, http.StatusConflict, openapi.ApiError{Code: "LINE_VOWIFI_UNSUPPORTED", Retryable: false})
		default:
			server.logger.WarnContext(r.Context(), "line egress update failed", "error", err)
			writeJSON(w, http.StatusInternalServerError, openapi.ApiError{Code: "LINE_EGRESS_PERSIST_FAILED", Retryable: true})
		}
		return
	}
	server.publish([]realtime.Topic{realtime.TopicLines, realtime.TopicMihomo, realtime.TopicVoWiFi}, "")
	writeJSON(w, http.StatusOK, lineEgressResponse(item))
}

func lineEgressResponse(item lineegressapp.View) openapi.LineEgressBinding {
	return openapi.LineEgressBinding{
		LineId: item.LineID, Mode: openapi.LineEgressBindingMode(item.Mode), CountryCode: item.CountryCode,
		CountryName: item.CountryName, ListenerPort: item.ListenerPort, Ready: item.Ready,
		ReadinessReason: openapi.LineEgressBindingReadinessReason(item.ReadinessReason),
	}
}

func (server *Server) ListVoWiFiLines(w http.ResponseWriter, r *http.Request) {
	if !server.requireBusinessAPI(w, r) {
		return
	}
	if featureDisabled(server.vowifi) {
		writeJSON(w, http.StatusServiceUnavailable, openapi.ApiError{Code: "VOWIFI_UNAVAILABLE", Retryable: true})
		return
	}
	items, err := server.vowifi.List(r.Context())
	if err != nil {
		server.logger.WarnContext(r.Context(), "Host VoWiFi state list failed", "error", err)
		writeJSON(w, http.StatusServiceUnavailable, openapi.ApiError{Code: "VOWIFI_STATUS_UNAVAILABLE", Retryable: true})
		return
	}
	lines := make([]openapi.VoWiFiLineState, 0, len(items))
	for _, item := range items {
		lines = append(lines, voWiFiStateResponse(item))
	}
	writeJSON(w, http.StatusOK, openapi.VoWiFiLineStateList{Lines: lines})
}

func (server *Server) ActivateVoWiFiLine(w http.ResponseWriter, r *http.Request, lineID string) {
	if !server.requireBusinessAPI(w, r) {
		return
	}
	if featureDisabled(server.vowifi) {
		writeJSON(w, http.StatusServiceUnavailable, openapi.ApiError{Code: "VOWIFI_UNAVAILABLE", Retryable: true})
		return
	}
	state, err := server.vowifi.Activate(r.Context(), lineID)
	if err != nil {
		server.writeVoWiFiError(w, r, err, lineID, true)
		return
	}
	server.publish([]realtime.Topic{realtime.TopicVoWiFi}, "")
	writeJSON(w, http.StatusAccepted, voWiFiStateResponse(state))
}

func (server *Server) DeactivateVoWiFiLine(w http.ResponseWriter, r *http.Request, lineID string) {
	if !server.requireBusinessAPI(w, r) {
		return
	}
	if featureDisabled(server.vowifi) {
		writeJSON(w, http.StatusServiceUnavailable, openapi.ApiError{Code: "VOWIFI_UNAVAILABLE", Retryable: true})
		return
	}
	state, err := server.vowifi.Deactivate(r.Context(), lineID)
	if err != nil {
		server.writeVoWiFiError(w, r, err, lineID, false)
		return
	}
	server.publish([]realtime.Topic{realtime.TopicVoWiFi}, "")
	writeJSON(w, http.StatusOK, voWiFiStateResponse(state))
}

func (server *Server) writeVoWiFiError(w http.ResponseWriter, r *http.Request, err error, lineID string, activating bool) {
	switch {
	case errors.Is(err, vowifiapp.ErrLineNotFound):
		writeJSON(w, http.StatusNotFound, openapi.ApiError{Code: "VOWIFI_LINE_NOT_FOUND", Retryable: false})
	case errors.Is(err, vowifiapp.ErrLineNotReady):
		writeJSON(w, http.StatusConflict, openapi.ApiError{Code: "VOWIFI_LINE_NOT_READY", Retryable: true})
	default:
		action := "deactivation"
		if activating {
			action = "activation"
		}
		server.logger.WarnContext(r.Context(), "Host VoWiFi "+action+" failed", "line_id", lineID, "error", err)
		writeJSON(w, http.StatusServiceUnavailable, openapi.ApiError{Code: "VOWIFI_RUNTIME_FAILED", Retryable: true})
	}
}

func (server *Server) GetMihomoConfigStatus(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.requireAdministrator(w, r, false); !ok {
		return
	}
	if featureDisabled(server.mihomoConfig) {
		writeJSON(w, http.StatusServiceUnavailable, openapi.ApiError{Code: "MIHOMO_CONFIG_UNAVAILABLE", Retryable: true})
		return
	}
	status, err := server.mihomoConfig.Status(r.Context())
	if err != nil {
		server.logger.WarnContext(r.Context(), "Mihomo config status failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, openapi.ApiError{Code: "MIHOMO_CONFIG_STATUS_FAILED", Retryable: true})
		return
	}
	writeJSON(w, http.StatusOK, mihomoConfigResponse(status))
}

func (server *Server) PublishMihomoConfig(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.requireAdministrator(w, r, true); !ok {
		return
	}
	if featureDisabled(server.mihomoConfig) {
		writeJSON(w, http.StatusServiceUnavailable, openapi.ApiError{Code: "MIHOMO_CONFIG_UNAVAILABLE", Retryable: true})
		return
	}
	status, err := server.mihomoConfig.GenerateAndPublish(r.Context())
	if errors.Is(err, mihomoapp.ErrConfigNotReady) {
		writeJSON(w, http.StatusBadRequest, openapi.ApiError{Code: "MIHOMO_CONFIG_NOT_READY", Retryable: false})
		return
	}
	if errors.Is(err, mihomoapp.ErrConfigValidationFailed) {
		writeJSON(w, http.StatusConflict, openapi.ApiError{Code: "MIHOMO_CONFIG_VALIDATION_FAILED", Retryable: false})
		return
	}
	if err != nil {
		server.logger.WarnContext(r.Context(), "Mihomo config publish failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, openapi.ApiError{Code: "MIHOMO_CONFIG_PUBLISH_FAILED", Retryable: true})
		return
	}
	server.publish([]realtime.Topic{realtime.TopicMihomo}, "")
	writeJSON(w, http.StatusOK, mihomoConfigResponse(status))
}

func (server *Server) SelectMihomoSubscription(w http.ResponseWriter, r *http.Request, subscriptionID string) {
	if _, ok := server.requireAdministrator(w, r, true); !ok {
		return
	}
	if featureDisabled(server.mihomoConfig) {
		writeJSON(w, http.StatusServiceUnavailable, openapi.ApiError{Code: "MIHOMO_CONFIG_UNAVAILABLE", Retryable: true})
		return
	}
	status, err := server.mihomoConfig.Select(r.Context(), subscriptionID)
	if errors.Is(err, mihomoapp.ErrConfigNotReady) {
		writeJSON(w, http.StatusBadRequest, openapi.ApiError{Code: "MIHOMO_SUBSCRIPTION_ARTIFACT_NOT_READY", Retryable: false})
		return
	}
	if errors.Is(err, mihomoapp.ErrConfigValidationFailed) {
		writeJSON(w, http.StatusConflict, openapi.ApiError{Code: "MIHOMO_CONFIG_VALIDATION_FAILED", Retryable: false})
		return
	}
	if err != nil {
		server.logger.WarnContext(r.Context(), "Mihomo subscription selection failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, openapi.ApiError{Code: "MIHOMO_SUBSCRIPTION_SELECT_FAILED", Retryable: true})
		return
	}
	server.publish([]realtime.Topic{realtime.TopicMihomo, realtime.TopicVoWiFi}, "")
	writeJSON(w, http.StatusOK, mihomoConfigResponse(status))
}

func (server *Server) GetMihomoRuntimeStatus(w http.ResponseWriter, r *http.Request) {
	server.handleMihomoRuntime(w, r, "status")
}

func (server *Server) StartMihomo(w http.ResponseWriter, r *http.Request) {
	server.handleMihomoRuntime(w, r, "start")
}

func (server *Server) RestartMihomo(w http.ResponseWriter, r *http.Request) {
	server.handleMihomoRuntime(w, r, "restart")
}

func (server *Server) StopMihomo(w http.ResponseWriter, r *http.Request) {
	server.handleMihomoRuntime(w, r, "stop")
}

func (server *Server) handleMihomoRuntime(w http.ResponseWriter, r *http.Request, action string) {
	mutation := action != "status"
	if _, ok := server.requireAdministrator(w, r, mutation); !ok {
		return
	}
	if featureDisabled(server.mihomoRuntime) {
		writeJSON(w, http.StatusServiceUnavailable, openapi.ApiError{Code: "MIHOMO_RUNTIME_UNAVAILABLE", Retryable: true})
		return
	}
	var status mihomoapp.RuntimeStatus
	var err error
	switch action {
	case "status":
		status, err = server.mihomoRuntime.Status(r.Context())
	case "start":
		status, err = server.mihomoRuntime.Start(r.Context())
	case "restart":
		status, err = server.mihomoRuntime.Restart(r.Context())
	case "stop":
		status, err = server.mihomoRuntime.Stop(r.Context())
	}
	if errors.Is(err, mihomoapp.ErrConfigNotReady) || errors.Is(err, mihomoapp.ErrConfigValidationFailed) || errors.Is(err, mihomoapp.ErrRuntimeAlreadyRunning) || errors.Is(err, mihomoapp.ErrRuntimeNotRunning) || errors.Is(err, mihomoapp.ErrRuntimeStartupFailed) {
		writeJSON(w, http.StatusConflict, openapi.ApiError{Code: "MIHOMO_RUNTIME_STATE_CONFLICT", Retryable: false})
		return
	}
	if err != nil {
		server.logger.WarnContext(r.Context(), "Mihomo runtime operation failed", "action", action, "error", err)
		writeJSON(w, http.StatusInternalServerError, openapi.ApiError{Code: "MIHOMO_RUNTIME_OPERATION_FAILED", Retryable: true})
		return
	}
	if mutation {
		server.publish([]realtime.Topic{realtime.TopicMihomo, realtime.TopicVoWiFi}, "")
	}
	writeJSON(w, http.StatusOK, mihomoRuntimeResponse(status))
}
