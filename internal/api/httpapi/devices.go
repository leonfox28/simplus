package httpapi

import (
	"errors"
	"net/http"
	"time"

	"github.com/leonfox28/simplus/internal/api/openapi"
	euiccapp "github.com/leonfox28/simplus/internal/application/euicc"
	lineapp "github.com/leonfox28/simplus/internal/application/line"
	modemapp "github.com/leonfox28/simplus/internal/application/modem"
	"github.com/leonfox28/simplus/internal/application/realtime"
	linedomain "github.com/leonfox28/simplus/internal/domain/line"
	modemdomain "github.com/leonfox28/simplus/internal/domain/modem"
)

func (server *Server) GetInventory(w http.ResponseWriter, r *http.Request) {
	if !server.requireBusinessAPI(w, r) {
		return
	}
	snapshot, err := server.inventory.Snapshot(r.Context())
	if err != nil {
		server.logger.ErrorContext(r.Context(), "inventory snapshot failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, openapi.ApiError{
			Code:      "INVENTORY_SNAPSHOT_UNAVAILABLE",
			Retryable: true,
		})
		return
	}
	writeJSON(w, http.StatusOK, inventoryResponse(snapshot))
}

func (server *Server) GetEUICCState(w http.ResponseWriter, r *http.Request) {
	if !server.requireBusinessAPI(w, r) {
		return
	}
	if featureDisabled(server.euicc) {
		writeJSON(w, http.StatusServiceUnavailable, openapi.ApiError{Code: "EUICC_UNAVAILABLE", Retryable: false})
		return
	}
	state, err := server.euicc.State(r.Context())
	if err != nil {
		server.logger.ErrorContext(r.Context(), "eUICC state failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, openapi.ApiError{Code: "EUICC_READ_FAILED", Retryable: true})
		return
	}
	writeJSON(w, http.StatusOK, euiccResponse(state))
}

func (server *Server) ActivateEUICCProfile(w http.ResponseWriter, r *http.Request, profileID string) {
	if !server.requireBusinessAPI(w, r) {
		return
	}
	if featureDisabled(server.euicc) {
		writeJSON(w, http.StatusServiceUnavailable, openapi.ApiError{Code: "EUICC_UNAVAILABLE", Retryable: false})
		return
	}
	state, err := server.euicc.Switch(r.Context(), profileID)
	if err != nil {
		switch {
		case errors.Is(err, euiccapp.ErrInvalid):
			writeJSON(w, http.StatusBadRequest, openapi.ApiError{Code: "EUICC_REQUEST_INVALID", Retryable: false})
		case errors.Is(err, euiccapp.ErrNotFound):
			writeJSON(w, http.StatusNotFound, openapi.ApiError{Code: "EUICC_PROFILE_NOT_FOUND", Retryable: false})
		default:
			server.logger.ErrorContext(r.Context(), "eUICC switch failed", "error", err)
			writeJSON(w, http.StatusInternalServerError, openapi.ApiError{Code: "EUICC_SWITCH_FAILED", Retryable: true})
		}
		return
	}
	server.publish([]realtime.Topic{realtime.TopicEUICC}, "")
	writeJSON(w, http.StatusOK, euiccResponse(state))
}

func (server *Server) ListManagedModems(w http.ResponseWriter, r *http.Request) {
	if !server.requireBusinessAPI(w, r) {
		return
	}
	if featureDisabled(server.modems) {
		writeJSON(w, http.StatusServiceUnavailable, openapi.ApiError{Code: "MODEM_MANAGEMENT_UNAVAILABLE", Retryable: true})
		return
	}
	items, err := server.modems.List(r.Context())
	if err != nil {
		server.logger.ErrorContext(r.Context(), "managed modem list failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, openapi.ApiError{Code: "MODEM_LIST_UNAVAILABLE", Retryable: true})
		return
	}
	modems := make([]openapi.ManagedModem, 0, len(items))
	for _, item := range items {
		modems = append(modems, managedModemResponse(item))
	}
	writeJSON(w, http.StatusOK, openapi.ManagedModemList{Modems: modems})
}

func (server *Server) ListManagedLines(w http.ResponseWriter, r *http.Request) {
	if !server.requireBusinessAPI(w, r) {
		return
	}
	if featureDisabled(server.lines) {
		writeJSON(w, http.StatusServiceUnavailable, openapi.ApiError{Code: "LINE_MANAGEMENT_UNAVAILABLE", Retryable: true})
		return
	}
	items, err := server.lines.List(r.Context())
	if err != nil {
		server.logger.ErrorContext(r.Context(), "managed line list failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, openapi.ApiError{Code: "LINE_LIST_UNAVAILABLE", Retryable: true})
		return
	}
	lines := make([]openapi.ManagedLine, 0, len(items))
	for _, item := range items {
		lines = append(lines, managedLineResponse(item))
	}
	writeJSON(w, http.StatusOK, openapi.ManagedLineList{Lines: lines})
}

func (server *Server) ListLineCandidates(w http.ResponseWriter, r *http.Request) {
	if !server.requireBusinessAPI(w, r) {
		return
	}
	if featureDisabled(server.lines) {
		writeJSON(w, http.StatusServiceUnavailable, openapi.ApiError{Code: "LINE_MANAGEMENT_UNAVAILABLE", Retryable: true})
		return
	}
	items, err := server.lines.Candidates(r.Context())
	if err != nil {
		server.logger.WarnContext(r.Context(), "managed line candidate resolution failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, openapi.ApiError{Code: "LINE_CANDIDATES_UNAVAILABLE", Retryable: true})
		return
	}
	candidates := make([]openapi.LineCandidate, 0, len(items))
	for _, item := range items {
		candidates = append(candidates, openapi.LineCandidate{
			CandidateId: item.CandidateID, ManagedModemId: item.ManagedModemID,
			ManagedModemDisplayName: item.ManagedModemDisplayName,
			ManagedModemModel:       item.ManagedModemModel, ManagedModemSerialNumber: item.ManagedModemSerialNumber,
			SubscriptionDisplayHint: item.SubscriptionDisplayHint,
			HomeOperatorName:        item.HomeOperatorName, HomeOperatorCode: item.HomeOperatorCode,
			SimPresence:  openapi.ManagedModemSIMPresence(item.SIMPresence),
			Capabilities: hardwareCapabilitiesResponse(item.Capabilities), Addable: item.Addable,
			ReadinessReason: openapi.LineCandidateReadinessReason(item.Readiness),
		})
	}
	writeJSON(w, http.StatusOK, openapi.LineCandidateList{Candidates: candidates})
}

func (server *Server) AddManagedLine(w http.ResponseWriter, r *http.Request) {
	if !server.requireBusinessAPI(w, r) {
		return
	}
	if featureDisabled(server.lines) {
		writeJSON(w, http.StatusServiceUnavailable, openapi.ApiError{Code: "LINE_MANAGEMENT_UNAVAILABLE", Retryable: true})
		return
	}
	var request openapi.AddManagedLineRequest
	if decodeJSON(w, r, &request) != nil {
		writeJSON(w, http.StatusBadRequest, openapi.ApiError{Code: "LINE_ADD_REQUEST_INVALID", Retryable: false})
		return
	}
	item, err := server.lines.Add(r.Context(), request.CandidateId, request.DisplayName)
	if err != nil {
		server.writeManagedLineError(w, r, err, true)
		return
	}
	server.publish([]realtime.Topic{realtime.TopicInventory, realtime.TopicLines}, "")
	w.Header().Set("Location", "/api/v1/lines/"+item.ID)
	writeJSON(w, http.StatusCreated, managedLineResponse(item))
}

func (server *Server) UpdateManagedLine(w http.ResponseWriter, r *http.Request, lineID string) {
	if !server.requireBusinessAPI(w, r) {
		return
	}
	if featureDisabled(server.lines) {
		writeJSON(w, http.StatusServiceUnavailable, openapi.ApiError{Code: "LINE_MANAGEMENT_UNAVAILABLE", Retryable: true})
		return
	}
	var request openapi.UpdateManagedLineRequest
	if decodeJSON(w, r, &request) != nil {
		writeJSON(w, http.StatusBadRequest, openapi.ApiError{Code: "LINE_UPDATE_REQUEST_INVALID", Retryable: false})
		return
	}
	item, err := server.lines.Update(r.Context(), lineID, request.DisplayName)
	if err != nil {
		server.writeManagedLineError(w, r, err, false)
		return
	}
	server.publish([]realtime.Topic{realtime.TopicLines}, "")
	writeJSON(w, http.StatusOK, managedLineResponse(item))
}

func (server *Server) writeManagedLineError(w http.ResponseWriter, r *http.Request, err error, adding bool) {
	switch {
	case errors.Is(err, lineapp.ErrRequestInvalid):
		code := "LINE_UPDATE_REQUEST_INVALID"
		if adding {
			code = "LINE_ADD_REQUEST_INVALID"
		}
		writeJSON(w, http.StatusBadRequest, openapi.ApiError{Code: code, Retryable: false})
	case errors.Is(err, lineapp.ErrCandidateNotFound):
		writeJSON(w, http.StatusNotFound, openapi.ApiError{Code: "LINE_CANDIDATE_NOT_FOUND", Retryable: true})
	case errors.Is(err, linedomain.ErrNotFound):
		writeJSON(w, http.StatusNotFound, openapi.ApiError{Code: "LINE_NOT_FOUND", Retryable: false})
	case errors.Is(err, lineapp.ErrCandidateInvalid):
		writeJSON(w, http.StatusConflict, openapi.ApiError{Code: "LINE_CANDIDATE_NOT_READY", Retryable: true})
	case errors.Is(err, lineapp.ErrAlreadyManaged):
		writeJSON(w, http.StatusConflict, openapi.ApiError{Code: "LINE_ALREADY_ADDED", Retryable: false})
	default:
		server.logger.ErrorContext(r.Context(), "managed line mutation failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, openapi.ApiError{Code: "LINE_PERSIST_FAILED", Retryable: true})
	}
}

func (server *Server) ListModemCandidates(w http.ResponseWriter, r *http.Request) {
	if !server.requireBusinessAPI(w, r) {
		return
	}
	if featureDisabled(server.modems) {
		writeJSON(w, http.StatusServiceUnavailable, openapi.ApiError{Code: "MODEM_MANAGEMENT_UNAVAILABLE", Retryable: true})
		return
	}
	items, err := server.modems.Candidates(r.Context())
	if err != nil {
		server.logger.WarnContext(r.Context(), "modem candidate scan failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, openapi.ApiError{Code: "MODEM_SCAN_FAILED", Retryable: true})
		return
	}
	candidates := make([]openapi.ModemCandidate, 0, len(items))
	for _, item := range items {
		candidates = append(candidates, openapi.ModemCandidate{
			CandidateId: item.CandidateID, UsbAddress: item.USBAddress,
			VendorId: item.USBVendorID, ProductId: item.USBProductID, UsbSerialHint: item.USBSerialHint,
			Model: item.Model, Transport: openapi.DeviceTransport(item.Transport),
			SupportStatus: openapi.ModemSupportStatus(item.Support), Addable: item.Addable,
			ReadinessReason: openapi.ModemCandidateReadinessReason(item.Readiness),
			Capabilities:    hardwareCapabilitiesResponse(item.Capabilities), SimPresence: openapi.ManagedModemSIMPresence(item.SIMPresence),
		})
	}
	writeJSON(w, http.StatusOK, openapi.ModemCandidateList{Candidates: candidates})
}

func (server *Server) AddManagedModem(w http.ResponseWriter, r *http.Request) {
	if !server.requireBusinessAPI(w, r) {
		return
	}
	if featureDisabled(server.modems) {
		writeJSON(w, http.StatusServiceUnavailable, openapi.ApiError{Code: "MODEM_MANAGEMENT_UNAVAILABLE", Retryable: true})
		return
	}
	var request openapi.AddManagedModemRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeJSON(w, http.StatusBadRequest, openapi.ApiError{Code: "MODEM_ADD_REQUEST_INVALID", Retryable: false})
		return
	}
	item, err := server.modems.Add(r.Context(), request.CandidateId)
	if err != nil {
		switch {
		case errors.Is(err, modemapp.ErrCandidateInvalid):
			writeJSON(w, http.StatusBadRequest, openapi.ApiError{Code: "MODEM_ADD_REQUEST_INVALID", Retryable: false})
		case errors.Is(err, modemapp.ErrCandidateNotFound):
			writeJSON(w, http.StatusNotFound, openapi.ApiError{Code: "MODEM_CANDIDATE_NOT_FOUND", Retryable: true})
		case errors.Is(err, modemapp.ErrCandidateNotReady):
			writeJSON(w, http.StatusConflict, openapi.ApiError{Code: "MODEM_CANDIDATE_NOT_READY", Retryable: true})
		case errors.Is(err, modemapp.ErrAlreadyManaged):
			writeJSON(w, http.StatusConflict, openapi.ApiError{Code: "MODEM_ALREADY_ADDED", Retryable: false})
		case errors.Is(err, modemapp.ErrIdentityConflict):
			writeJSON(w, http.StatusConflict, openapi.ApiError{Code: "MODEM_IDENTITY_CONFLICT", Retryable: false})
		default:
			server.logger.ErrorContext(r.Context(), "managed modem add failed", "error", err)
			writeJSON(w, http.StatusInternalServerError, openapi.ApiError{Code: "MODEM_ADD_FAILED", Retryable: true})
		}
		return
	}
	server.publish([]realtime.Topic{realtime.TopicInventory, realtime.TopicModems, realtime.TopicLines}, "")
	w.Header().Set("Location", "/api/v1/modems/"+item.ID)
	writeJSON(w, http.StatusCreated, managedModemResponse(item))
}

func (server *Server) SetManagedModemRFState(w http.ResponseWriter, r *http.Request, modemID string) {
	if !server.requireBusinessAPI(w, r) {
		return
	}
	if featureDisabled(server.modems) {
		writeJSON(w, http.StatusServiceUnavailable, openapi.ApiError{Code: "MODEM_MANAGEMENT_UNAVAILABLE", Retryable: true})
		return
	}
	var request openapi.SetManagedModemRFStateRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeJSON(w, http.StatusBadRequest, openapi.ApiError{Code: "MODEM_RF_REQUEST_INVALID", Retryable: false})
		return
	}
	item, err := server.modems.SetRFState(r.Context(), modemID, request.Enabled)
	if err != nil {
		switch {
		case errors.Is(err, modemapp.ErrModemNotFound):
			writeJSON(w, http.StatusNotFound, openapi.ApiError{Code: "MODEM_NOT_FOUND", Retryable: false})
		case errors.Is(err, modemapp.ErrRFUnavailable):
			writeJSON(w, http.StatusUnprocessableEntity, openapi.ApiError{Code: "MODEM_RF_UNAVAILABLE", Retryable: false})
		default:
			server.logger.WarnContext(r.Context(), "managed modem RF change failed", "modem_id", modemID, "error", err)
			writeJSON(w, http.StatusConflict, openapi.ApiError{Code: "MODEM_RF_CHANGE_FAILED", Retryable: true})
		}
		return
	}
	server.publish([]realtime.Topic{realtime.TopicInventory, realtime.TopicModems, realtime.TopicLines}, "")
	writeJSON(w, http.StatusOK, managedModemResponse(item))
}

func (server *Server) ReadManagedModemEquipmentIdentity(w http.ResponseWriter, r *http.Request, modemID string) {
	if !server.requireBusinessAPI(w, r) {
		return
	}
	if featureDisabled(server.modems) {
		writeJSON(w, http.StatusServiceUnavailable, openapi.ApiError{Code: "MODEM_MANAGEMENT_UNAVAILABLE", Retryable: true})
		return
	}
	imei, err := server.modems.ReadEquipmentIdentity(r.Context(), modemID)
	if err != nil {
		switch {
		case errors.Is(err, modemapp.ErrModemNotFound):
			writeJSON(w, http.StatusNotFound, openapi.ApiError{Code: "MODEM_NOT_FOUND", Retryable: false})
		case errors.Is(err, modemapp.ErrIdentityConflict):
			writeJSON(w, http.StatusConflict, openapi.ApiError{Code: "MODEM_IDENTITY_CONFLICT", Retryable: true})
		case errors.Is(err, modemapp.ErrEquipmentIdentityUnavailable):
			writeJSON(w, http.StatusConflict, openapi.ApiError{Code: "MODEM_IDENTITY_UNAVAILABLE", Retryable: true})
		default:
			server.logger.WarnContext(r.Context(), "managed modem identity read failed", "modem_id", modemID, "error", err)
			writeJSON(w, http.StatusServiceUnavailable, openapi.ApiError{Code: "MODEM_IDENTITY_UNAVAILABLE", Retryable: true})
		}
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	writeJSON(w, http.StatusOK, openapi.ManagedModemEquipmentIdentity{Imei: imei})
}

func managedModemResponse(item modemdomain.View) openapi.ManagedModem {
	if item.Cellular.State == "" {
		item.Cellular = modemdomain.UnavailableCellularStatus()
	}
	registrations := make([]openapi.CellularRegistration, 0, len(item.Cellular.Registrations))
	for _, registration := range item.Cellular.Registrations {
		registrations = append(registrations, openapi.CellularRegistration{
			Domain: openapi.CellularRegistrationDomain(registration.Domain), State: openapi.CellularRegistrationState(registration.State),
		})
	}
	observedAt := ""
	if !item.Cellular.ObservedAt.IsZero() {
		observedAt = item.Cellular.ObservedAt.UTC().Format(time.RFC3339)
	}
	return openapi.ManagedModem{
		Id: item.ID, DisplayName: item.DisplayName, Model: item.Model, SerialNumber: item.SerialNumber,
		Transport: openapi.DeviceTransport(item.Transport), State: openapi.ManagedModemState(item.State),
		Capabilities: hardwareCapabilitiesResponse(item.Capabilities), RfState: openapi.ManagedModemRFState(item.RFState),
		SimPresence: openapi.ManagedModemSIMPresence(item.SIMPresence), Cellular: openapi.ManagedModemCellularStatus{
			State: openapi.CellularState(item.Cellular.State), ErrorCode: openapi.CellularErrorCode(item.Cellular.ErrorCode),
			Registrations: registrations, OperatorName: item.Cellular.OperatorName, OperatorCode: item.Cellular.OperatorCode,
			Rat: item.Cellular.RAT, SignalState: openapi.CellularSignalState(item.Cellular.SignalState),
			SignalRssiDbm: item.Cellular.SignalRSSIDBm, ObservedAt: observedAt,
		}, AddedAt: item.AddedAt,
	}
}

func (server *Server) GetHardwareTopology(w http.ResponseWriter, r *http.Request) {
	if !server.requireBusinessAPI(w, r) {
		return
	}
	topology, err := server.inventory.Topology(r.Context())
	if err != nil {
		server.logger.ErrorContext(r.Context(), "hardware topology failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, openapi.ApiError{Code: "HARDWARE_TOPOLOGY_UNAVAILABLE", Retryable: true})
		return
	}
	writeJSON(w, http.StatusOK, hardwareTopologyResponse(topology))
}
