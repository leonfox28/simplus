package httpapi

import (
	"errors"
	"net/http"

	"github.com/leonfox28/simplus/internal/api/openapi"
	authapp "github.com/leonfox28/simplus/internal/application/auth"
)

func (server *Server) requireAdministrator(w http.ResponseWriter, r *http.Request, requireCSRF bool) (authapp.Session, bool) {
	if server.auth == nil {
		writeJSON(w, http.StatusServiceUnavailable, openapi.ApiError{Code: "AUTH_UNAVAILABLE", Retryable: true})
		return authapp.Session{}, false
	}
	token, csrfToken, ok := administratorTokens(r, requireCSRF)
	if !ok {
		if sessionCookie, err := r.Cookie(adminSessionCookieName); requireCSRF && err == nil && sessionCookie.Value != "" {
			writeJSON(w, http.StatusForbidden, openapi.ApiError{Code: "CSRF_INVALID", Retryable: false})
		} else {
			clearAdministratorCookies(w, r)
			writeJSON(w, http.StatusUnauthorized, openapi.ApiError{Code: "AUTH_SESSION_UNAUTHORIZED", Retryable: false})
		}
		return authapp.Session{}, false
	}
	session, err := server.auth.Authenticate(r.Context(), token, csrfToken, requireCSRF)
	if err != nil {
		server.writeAuthenticationError(w, err, "administrator session validation failed")
		return authapp.Session{}, false
	}
	return session, true
}

func (server *Server) writeAuthenticationError(w http.ResponseWriter, err error, message string) {
	switch {
	case errors.Is(err, authapp.ErrInvalidCredentials):
		writeJSON(w, http.StatusUnauthorized, openapi.ApiError{Code: "LOGIN_INVALID", Retryable: false})
	case errors.Is(err, authapp.ErrUnauthorized):
		writeJSON(w, http.StatusUnauthorized, openapi.ApiError{Code: "AUTH_SESSION_UNAUTHORIZED", Retryable: false})
	case errors.Is(err, authapp.ErrCSRFInvalid):
		writeJSON(w, http.StatusForbidden, openapi.ApiError{Code: "CSRF_INVALID", Retryable: false})
	case errors.Is(err, authapp.ErrLoginRateLimited):
		writeJSON(w, http.StatusTooManyRequests, openapi.ApiError{Code: "LOGIN_RATE_LIMITED", Retryable: true})
	case errors.Is(err, authapp.ErrInstanceNotReady):
		writeJSON(w, http.StatusConflict, openapi.ApiError{Code: "INSTANCE_NOT_READY", Retryable: false})
	case errors.Is(err, authapp.ErrPasswordRequestInvalid):
		writeJSON(w, http.StatusBadRequest, openapi.ApiError{Code: "PASSWORD_REQUEST_INVALID", Retryable: false})
	case errors.Is(err, authapp.ErrCurrentPasswordInvalid):
		writeJSON(w, http.StatusUnauthorized, openapi.ApiError{Code: "CURRENT_PASSWORD_INVALID", Retryable: false})
	default:
		server.logger.Error(message, "error", err)
		writeJSON(w, http.StatusInternalServerError, openapi.ApiError{Code: "AUTH_UNAVAILABLE", Retryable: true})
	}
}

func (server *Server) Login(w http.ResponseWriter, r *http.Request) {
	if server.auth == nil {
		writeJSON(w, http.StatusServiceUnavailable, openapi.ApiError{Code: "AUTH_UNAVAILABLE", Retryable: true})
		return
	}
	var request openapi.LoginRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeJSON(w, http.StatusBadRequest, openapi.ApiError{Code: "LOGIN_REQUEST_INVALID", Retryable: false})
		return
	}
	result, err := server.auth.Login(r.Context(), request.Username, request.Password)
	if err != nil {
		server.writeAuthenticationError(w, err, "administrator login failed")
		return
	}
	setAdministratorCookies(w, r, result.SessionToken, result.CSRFToken, result.ExpiresAt)

	writeJSON(w, http.StatusOK, authSessionResponse(result.User, result.ExpiresAt))
}

func (server *Server) GetAuthSession(w http.ResponseWriter, r *http.Request) {
	session, ok := server.requireAdministrator(w, r, false)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, authSessionResponse(session.User, session.ExpiresAt))
}

func (server *Server) Logout(w http.ResponseWriter, r *http.Request) {
	if server.auth == nil {
		writeJSON(w, http.StatusServiceUnavailable, openapi.ApiError{Code: "AUTH_UNAVAILABLE", Retryable: true})
		return
	}
	token, csrfToken, ok := administratorTokens(r, true)
	if !ok {
		clearAdministratorCookies(w, r)
		writeJSON(w, http.StatusUnauthorized, openapi.ApiError{Code: "AUTH_SESSION_UNAUTHORIZED", Retryable: false})
		return
	}
	if err := server.auth.Logout(r.Context(), token, csrfToken); err != nil {
		server.writeAuthenticationError(w, err, "administrator logout failed")
		return
	}
	clearAdministratorCookies(w, r)
	w.WriteHeader(http.StatusNoContent)
}

func (server *Server) ChangeAdministratorPassword(w http.ResponseWriter, r *http.Request) {
	if server.auth == nil {
		writeJSON(w, http.StatusServiceUnavailable, openapi.ApiError{Code: "AUTH_UNAVAILABLE", Retryable: true})
		return
	}
	token, csrfToken, ok := administratorTokens(r, true)
	if !ok {
		writeJSON(w, http.StatusForbidden, openapi.ApiError{Code: "CSRF_INVALID", Retryable: false})
		return
	}
	var request openapi.ChangeAdministratorPasswordRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeJSON(w, http.StatusBadRequest, openapi.ApiError{Code: "PASSWORD_REQUEST_INVALID", Retryable: false})
		return
	}
	passwordAuth, ok := server.auth.(PasswordAuthenticator)
	if !ok {
		writeJSON(w, http.StatusServiceUnavailable, openapi.ApiError{Code: "AUTH_UNAVAILABLE", Retryable: true})
		return
	}
	if err := passwordAuth.ChangePassword(r.Context(), token, csrfToken, request.CurrentPassword, request.NewPassword, request.NewPasswordConfirmation); err != nil {
		server.writeAuthenticationError(w, err, "administrator password replacement failed")
		return
	}
	clearAdministratorCookies(w, r)
	w.WriteHeader(http.StatusNoContent)
}
