package control

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/senhao-xu/vps-status/internal/control/webui"
	"github.com/senhao-xu/vps-status/internal/protocol"
	"github.com/senhao-xu/vps-status/internal/security"
	controlstore "github.com/senhao-xu/vps-status/internal/storage/control"
	"github.com/senhao-xu/vps-status/internal/storage/telemetry"
)

const (
	sessionCookieName = "vps_session"
	csrfCookieName    = "vps_csrf"
	csrfHeaderName    = "X-CSRF-Token"
	maxJSONBody       = 64 << 10
	recoveryCodeCount = 10
)

// APIConfig configures the administrator API.
type APIConfig struct {
	Store               *controlstore.Store
	MasterKey           *security.MasterKey
	PasswordParams      security.PasswordParams
	SessionTTL          time.Duration
	Now                 func() time.Time
	Web                 http.Handler
	LoginLimiter        *security.Limiter
	SetupLimiter        *security.Limiter
	EnrollmentLimiter   *security.Limiter
	AgentConnectLimiter *security.Limiter
	Registry            *Registry
	Live                *LiveStore
	Telemetry           *telemetry.Service
	History             *HostHistory
	ProbeHistory        *ProbeHistory
	AgentSessions       AgentSessionConfig
	Alerts              *AlertManager
	Notifications       *NotificationDispatcher
	WorkerContext       context.Context
	NotificationConfig  NotificationDispatcherConfig
	AlertConfig         AlertManagerConfig
}

// NewHandler wires the versioned JSON API ahead of the SPA fallback.
func NewHandler(config APIConfig) (http.Handler, error) {
	if config.Store == nil || config.MasterKey == nil {
		return nil, errors.New("control API storage and master key are required")
	}
	if config.PasswordParams == (security.PasswordParams{}) {
		config.PasswordParams = security.DefaultPasswordParams
	}
	if config.SessionTTL == 0 {
		config.SessionTTL = 7 * 24 * time.Hour
	}
	if config.SessionTTL < time.Minute {
		return nil, errors.New("control API session TTL is too short")
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.Web == nil {
		config.Web = webui.Handler()
	}
	if config.LoginLimiter == nil {
		config.LoginLimiter = security.NewLimiter(10, time.Minute, 4096)
	}
	if config.SetupLimiter == nil {
		config.SetupLimiter = security.NewLimiter(5, 10*time.Minute, 1024)
	}
	if config.EnrollmentLimiter == nil {
		config.EnrollmentLimiter = security.NewLimiter(20, time.Minute, 4096)
	}
	if config.AgentConnectLimiter == nil {
		config.AgentConnectLimiter = security.NewLimiter(240, time.Minute, 4096)
	}
	if config.Registry == nil {
		config.Registry = NewRegistry()
	}
	if config.Live == nil {
		config.Live = NewLiveStore()
	}
	if config.WorkerContext == nil {
		config.WorkerContext = context.Background()
	}
	if config.Notifications == nil {
		var dispatcherErr error
		config.Notifications, dispatcherErr = NewNotificationDispatcher(config.WorkerContext, config.Store, config.MasterKey, config.NotificationConfig)
		if dispatcherErr != nil {
			return nil, fmt.Errorf("initialize notification dispatcher: %w", dispatcherErr)
		}
	}
	if config.Alerts == nil {
		var alertsErr error
		config.Alerts, alertsErr = NewAlertManager(config.WorkerContext, config.Store, config.Live, config.Notifications, config.AlertConfig)
		if alertsErr != nil {
			return nil, fmt.Errorf("initialize alert manager: %w", alertsErr)
		}
	}
	if config.History == nil && config.Telemetry != nil {
		config.History = NewHostHistory(config.Telemetry)
	}
	if config.ProbeHistory == nil {
		config.ProbeHistory = NewProbeHistory(config.Telemetry)
	}
	config.AgentSessions = config.AgentSessions.withDefaults()
	if config.AgentSessions.ReportInterval < protocol.MinReportInterval ||
		config.AgentSessions.ReportInterval > protocol.MaxReportInterval {
		return nil, errors.New("control API report interval is outside protocol bounds")
	}
	dummyHash, err := security.HashPassword("invalid-password-placeholder", config.PasswordParams)
	if err != nil {
		return nil, fmt.Errorf("initialize password verifier: %w", err)
	}
	api := &adminAPI{config: config, dummyPasswordHash: dummyHash}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", health)
	mux.HandleFunc("GET /readyz", health)
	mux.HandleFunc("GET /api/v1/admin/setup", api.setupStatus)
	mux.HandleFunc("POST /api/v1/admin/setup", api.setup)
	mux.HandleFunc("POST /api/v1/admin/login", api.login)
	mux.HandleFunc("GET /api/v1/admin/session", api.auth(false, api.session))
	mux.HandleFunc("POST /api/v1/admin/logout", api.auth(true, api.logout))
	mux.HandleFunc("GET /api/v1/admin/sessions", api.auth(false, api.sessions))
	mux.HandleFunc("DELETE /api/v1/admin/sessions/{id}", api.auth(true, api.revokeSession))
	mux.HandleFunc("POST /api/v1/admin/mfa/totp/setup", api.auth(true, api.setupTOTP))
	mux.HandleFunc("POST /api/v1/admin/mfa/totp/confirm", api.auth(true, api.confirmTOTP))
	mux.HandleFunc("POST /api/v1/admin/mfa/totp/disable", api.auth(true, api.disableTOTP))
	mux.HandleFunc("POST /api/v1/admin/enrollments", api.auth(true, api.createEnrollment))
	mux.HandleFunc("GET /api/v1/admin/nodes", api.auth(false, api.nodes))
	mux.HandleFunc("GET /api/v1/admin/nodes/{id}", api.auth(false, api.node))
	mux.HandleFunc("PATCH /api/v1/admin/nodes/{id}", api.auth(true, api.updateNode))
	mux.HandleFunc("DELETE /api/v1/admin/nodes/{id}", api.auth(true, api.deleteNode))
	mux.HandleFunc("GET /api/v1/admin/live", api.auth(false, api.liveEvents))
	mux.HandleFunc("GET /api/v1/admin/probes", api.auth(false, api.probes))
	mux.HandleFunc("POST /api/v1/admin/probes", api.auth(true, api.createProbe))
	mux.HandleFunc("GET /api/v1/admin/probes/{id}", api.auth(false, api.probe))
	mux.HandleFunc("PATCH /api/v1/admin/probes/{id}", api.auth(true, api.updateProbe))
	mux.HandleFunc("DELETE /api/v1/admin/probes/{id}", api.auth(true, api.deleteProbe))
	mux.HandleFunc("PUT /api/v1/admin/probes/{id}/assignments/{node_id}", api.auth(true, api.addProbeAssignment))
	mux.HandleFunc("DELETE /api/v1/admin/probes/{id}/assignments/{node_id}", api.auth(true, api.removeProbeAssignment))
	mux.HandleFunc("GET /api/v1/admin/probe-results", api.auth(false, api.probeResults))
	mux.HandleFunc("GET /api/v1/admin/notification-channels", api.auth(false, api.notificationChannels))
	mux.HandleFunc("POST /api/v1/admin/notification-channels", api.auth(true, api.createNotificationChannel))
	mux.HandleFunc("GET /api/v1/admin/notification-channels/{id}", api.auth(false, api.notificationChannel))
	mux.HandleFunc("PATCH /api/v1/admin/notification-channels/{id}", api.auth(true, api.updateNotificationChannel))
	mux.HandleFunc("DELETE /api/v1/admin/notification-channels/{id}", api.auth(true, api.deleteNotificationChannel))
	mux.HandleFunc("POST /api/v1/admin/notification-channels/{id}/test", api.auth(true, api.testNotificationChannel))
	mux.HandleFunc("GET /api/v1/admin/alert-rules", api.auth(false, api.alertRules))
	mux.HandleFunc("POST /api/v1/admin/alert-rules", api.auth(true, api.createAlertRule))
	mux.HandleFunc("GET /api/v1/admin/alert-rules/{id}", api.auth(false, api.alertRule))
	mux.HandleFunc("PATCH /api/v1/admin/alert-rules/{id}", api.auth(true, api.updateAlertRule))
	mux.HandleFunc("DELETE /api/v1/admin/alert-rules/{id}", api.auth(true, api.deleteAlertRule))
	mux.HandleFunc("GET /api/v1/admin/alert-rules/{id}/states", api.auth(false, api.alertRuleStates))
	mux.HandleFunc("GET /api/v1/admin/public-page", api.auth(false, api.publicPageConfig))
	mux.HandleFunc("PUT /api/v1/admin/public-page", api.auth(true, api.updatePublicPageConfig))
	mux.HandleFunc("GET /api/v1/admin/telemetry/storage", api.auth(false, api.telemetryStorage))
	mux.HandleFunc("GET /api/v1/admin/telemetry/host", api.auth(false, api.telemetryHost))
	mux.HandleFunc("GET /api/v1/admin/telemetry/traffic", api.auth(false, api.telemetryTraffic))
	mux.HandleFunc("GET /api/v1/admin/telemetry/probe", api.auth(false, api.telemetryProbe))
	mux.HandleFunc("POST /api/v1/admin/telemetry/clear-history", api.auth(true, api.telemetryClearHistory))
	mux.HandleFunc("POST /api/v1/admin/telemetry/reset-all", api.auth(true, api.telemetryResetAll))
	mux.HandleFunc("POST /api/v1/admin/telemetry/checkpoint", api.auth(true, api.telemetryCheckpoint))
	mux.HandleFunc("POST /api/v1/admin/telemetry/incremental-vacuum", api.auth(true, api.telemetryIncrementalVacuum))
	mux.HandleFunc("POST /api/v1/admin/telemetry/full-vacuum", api.auth(true, api.telemetryFullVacuum))
	mux.HandleFunc("POST /api/v1/admin/nodes/{id}/credentials/rotate", api.auth(true, api.rotateNodeCredential))
	mux.HandleFunc("DELETE /api/v1/admin/nodes/{id}/credentials", api.auth(true, api.revokeNodeCredentials))
	agents := newAgentServer(config.Store, config.Registry, config.Live, config.AgentSessions, config.AgentConnectLimiter, config.Now)
	agents.history = config.History
	agents.probes = config.ProbeHistory
	agents.alerts = config.Alerts
	mux.HandleFunc("POST /agent/v1/enroll", api.enrollAgent)
	mux.HandleFunc("GET /agent/v1/ws", agents.serveWS)
	mux.HandleFunc("GET /api/v1/public/status", api.publicStatus)
	mux.Handle("/api/", http.HandlerFunc(api.notFound))
	mux.Handle("/", config.Web)
	return securityHeaders(noStoreAPI(sameOrigin(mux))), nil
}

func health(response http.ResponseWriter, _ *http.Request) {
	response.Header().Set("Content-Type", "text/plain; charset=utf-8")
	response.WriteHeader(http.StatusOK)
	_, _ = response.Write([]byte("ok\n"))
}

type adminAPI struct {
	config            APIConfig
	dummyPasswordHash string
}

type authenticatedRequest struct {
	admin   controlstore.Admin
	session controlstore.Session
	token   [32]byte
}

type authHandler func(http.ResponseWriter, *http.Request, authenticatedRequest)

func (api *adminAPI) auth(requireCSRF bool, next authHandler) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		cookie, err := request.Cookie(sessionCookieName)
		if err != nil || cookie.Value == "" {
			writeError(response, http.StatusUnauthorized, "unauthorized", "Authentication is required.")
			return
		}
		tokenDigest := security.DigestToken(cookie.Value)
		session, err := api.config.Store.SessionByTokenDigest(request.Context(), tokenDigest, api.now())
		if err != nil {
			if errors.Is(err, controlstore.ErrNotFound) {
				writeError(response, http.StatusUnauthorized, "unauthorized", "Authentication is required.")
			} else {
				writeInternal(response)
			}
			return
		}
		if requireCSRF {
			csrfDigest := security.DigestCSRF(request.Header.Get(csrfHeaderName))
			if subtle.ConstantTimeCompare(csrfDigest[:], session.CSRFDigest[:]) != 1 {
				writeError(response, http.StatusForbidden, "csrf_invalid", "The CSRF token is missing or invalid.")
				return
			}
		}
		admin, err := api.config.Store.AdminByID(request.Context(), session.AdminID)
		if err != nil {
			if errors.Is(err, controlstore.ErrNotFound) {
				writeError(response, http.StatusUnauthorized, "unauthorized", "Authentication is required.")
			} else {
				writeInternal(response)
			}
			return
		}
		next(response, request, authenticatedRequest{admin: admin, session: session, token: tokenDigest})
	}
}

func (api *adminAPI) setupStatus(response http.ResponseWriter, request *http.Request) {
	configured, err := api.config.Store.AdminExists(request.Context())
	if err != nil {
		writeInternal(response)
		return
	}
	writeJSON(response, http.StatusOK, map[string]bool{"configured": configured})
}

type setupRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (api *adminAPI) setup(response http.ResponseWriter, request *http.Request) {
	if !api.allow(response, request, api.config.SetupLimiter) {
		return
	}
	var input setupRequest
	if !decodeJSON(response, request, &input) {
		return
	}
	input.Username = strings.TrimSpace(input.Username)
	if !validUsername(input.Username) || len(input.Password) < 12 || len(input.Password) > 1024 {
		writeError(response, http.StatusBadRequest, "invalid_input", "Username or password does not meet the requirements.")
		return
	}
	passwordHash, err := security.HashPassword(input.Password, api.config.PasswordParams)
	if err != nil {
		writeInternal(response)
		return
	}
	admin, err := api.config.Store.CreateAdmin(request.Context(), input.Username, passwordHash, api.now())
	if errors.Is(err, controlstore.ErrAdminExists) {
		writeError(response, http.StatusConflict, "already_configured", "The administrator has already been configured.")
		return
	}
	if err != nil {
		writeInternal(response)
		return
	}
	api.issueSession(response, request, admin, http.StatusCreated)
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	MFACode  string `json:"mfa_code"`
}

func (api *adminAPI) login(response http.ResponseWriter, request *http.Request) {
	if !api.allow(response, request, api.config.LoginLimiter) {
		return
	}
	var input loginRequest
	if !decodeJSON(response, request, &input) {
		return
	}
	input.Username = strings.TrimSpace(input.Username)
	admin, err := api.config.Store.AdminByUsername(request.Context(), input.Username)
	hash := api.dummyPasswordHash
	if err == nil {
		hash = admin.PasswordHash
	} else if !errors.Is(err, controlstore.ErrNotFound) {
		writeInternal(response)
		return
	}
	passwordOK, verifyErr := security.VerifyPassword(input.Password, hash)
	if verifyErr != nil || err != nil || !passwordOK {
		writeError(response, http.StatusUnauthorized, "invalid_credentials", "The credentials are invalid.")
		return
	}
	if admin.TOTPConfirmed {
		if strings.TrimSpace(input.MFACode) == "" {
			writeError(response, http.StatusUnauthorized, "mfa_required", "A TOTP or recovery code is required.")
			return
		}
		secret, decryptErr := api.config.MasterKey.Decrypt(admin.TOTPSecretCipher)
		if decryptErr != nil {
			writeInternal(response)
			return
		}
		if !security.VerifyTOTP(string(secret), input.MFACode, api.now()) {
			digest := security.DigestRecoveryCode(input.MFACode)
			if err := api.config.Store.ConsumeRecoveryCode(request.Context(), admin.ID, digest, api.now()); err != nil {
				writeError(response, http.StatusUnauthorized, "invalid_credentials", "The credentials are invalid.")
				return
			}
		}
	}
	api.issueSession(response, request, admin, http.StatusOK)
}

func (api *adminAPI) issueSession(response http.ResponseWriter, request *http.Request, admin controlstore.Admin, status int) {
	token, err := security.RandomToken()
	if err != nil {
		writeInternal(response)
		return
	}
	csrf, err := security.RandomToken()
	if err != nil {
		writeInternal(response)
		return
	}
	id, err := security.RandomToken()
	if err != nil {
		writeInternal(response)
		return
	}
	now := api.now()
	session := controlstore.Session{
		ID: id, AdminID: admin.ID, TokenDigest: security.DigestToken(token),
		CSRFDigest: security.DigestCSRF(csrf), CreatedAt: now, ExpiresAt: now.Add(api.config.SessionTTL),
	}
	if err := api.config.Store.CreateSession(request.Context(), session, admin.TOTPConfirmed); err != nil {
		if errors.Is(err, controlstore.ErrAdminStateChanged) {
			writeError(response, http.StatusConflict, "auth_state_changed", "Authentication settings changed. Try again.")
			return
		}
		writeInternal(response)
		return
	}
	api.setCookies(response, token, csrf, session.ExpiresAt)
	writeJSON(response, status, map[string]any{
		"admin":      map[string]any{"username": admin.Username, "mfa_enabled": admin.TOTPConfirmed},
		"csrf_token": csrf,
	})
}

func (api *adminAPI) session(response http.ResponseWriter, _ *http.Request, auth authenticatedRequest) {
	writeJSON(response, http.StatusOK, map[string]any{
		"admin":   map[string]any{"username": auth.admin.Username, "mfa_enabled": auth.admin.TOTPConfirmed},
		"session": map[string]any{"id": auth.session.ID, "created_at": auth.session.CreatedAt, "expires_at": auth.session.ExpiresAt},
	})
}

func (api *adminAPI) logout(response http.ResponseWriter, request *http.Request, auth authenticatedRequest) {
	if err := api.config.Store.RevokeSessionByToken(request.Context(), auth.token, api.now()); err != nil && !errors.Is(err, controlstore.ErrNotFound) {
		writeInternal(response)
		return
	}
	api.clearCookies(response)
	response.WriteHeader(http.StatusNoContent)
}

func (api *adminAPI) sessions(response http.ResponseWriter, request *http.Request, auth authenticatedRequest) {
	records, err := api.config.Store.ListSessions(request.Context(), auth.admin.ID)
	if err != nil {
		writeInternal(response)
		return
	}
	type sessionDTO struct {
		ID        string     `json:"id"`
		CreatedAt time.Time  `json:"created_at"`
		ExpiresAt time.Time  `json:"expires_at"`
		RevokedAt *time.Time `json:"revoked_at,omitempty"`
		Current   bool       `json:"current"`
	}
	result := make([]sessionDTO, 0, len(records))
	for _, record := range records {
		result = append(result, sessionDTO{
			ID: record.ID, CreatedAt: record.CreatedAt, ExpiresAt: record.ExpiresAt,
			RevokedAt: record.RevokedAt, Current: record.ID == auth.session.ID,
		})
	}
	writeJSON(response, http.StatusOK, map[string]any{"sessions": result})
}

func (api *adminAPI) revokeSession(response http.ResponseWriter, request *http.Request, auth authenticatedRequest) {
	id := request.PathValue("id")
	if id == "" || len(id) > 128 {
		writeError(response, http.StatusNotFound, "not_found", "The session was not found.")
		return
	}
	if err := api.config.Store.RevokeSession(request.Context(), auth.admin.ID, id, api.now()); err != nil {
		if errors.Is(err, controlstore.ErrNotFound) {
			writeError(response, http.StatusNotFound, "not_found", "The session was not found.")
		} else {
			writeInternal(response)
		}
		return
	}
	if id == auth.session.ID {
		api.clearCookies(response)
	}
	response.WriteHeader(http.StatusNoContent)
}

func (api *adminAPI) setupTOTP(response http.ResponseWriter, request *http.Request, auth authenticatedRequest) {
	if auth.admin.TOTPConfirmed {
		writeError(response, http.StatusConflict, "mfa_enabled", "TOTP is already enabled.")
		return
	}
	secret, err := security.GenerateTOTPSecret()
	if err != nil {
		writeInternal(response)
		return
	}
	cipher, err := api.config.MasterKey.Encrypt([]byte(secret))
	if err != nil {
		writeInternal(response)
		return
	}
	if err := api.config.Store.SetPendingTOTP(request.Context(), auth.admin.ID, cipher, api.now()); err != nil {
		writeInternal(response)
		return
	}
	writeJSON(response, http.StatusOK, map[string]string{
		"secret": secret,
		"uri":    security.TOTPURI(secret, auth.admin.Username, "VPS Status"),
	})
}

type codeRequest struct {
	Code string `json:"code"`
}

func (api *adminAPI) confirmTOTP(response http.ResponseWriter, request *http.Request, auth authenticatedRequest) {
	var input codeRequest
	if !decodeJSON(response, request, &input) {
		return
	}
	admin, err := api.config.Store.AdminByID(request.Context(), auth.admin.ID)
	if err != nil || admin.TOTPConfirmed || len(admin.TOTPSecretCipher) == 0 {
		writeError(response, http.StatusConflict, "mfa_setup_missing", "Start TOTP setup before confirming it.")
		return
	}
	secret, err := api.config.MasterKey.Decrypt(admin.TOTPSecretCipher)
	if err != nil {
		writeInternal(response)
		return
	}
	if !security.VerifyTOTP(string(secret), input.Code, api.now()) {
		writeError(response, http.StatusBadRequest, "mfa_code_invalid", "The TOTP code is invalid.")
		return
	}
	codes, err := security.GenerateRecoveryCodes(recoveryCodeCount)
	if err != nil {
		writeInternal(response)
		return
	}
	digests := make([][32]byte, len(codes))
	for index, code := range codes {
		digests[index] = security.DigestRecoveryCode(code)
	}
	if err := api.config.Store.ConfirmTOTP(request.Context(), admin.ID, auth.session.ID, digests, api.now()); err != nil {
		writeInternal(response)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"recovery_codes": codes})
}

type disableTOTPRequest struct {
	Password string `json:"password"`
	Code     string `json:"code"`
}

func (api *adminAPI) disableTOTP(response http.ResponseWriter, request *http.Request, auth authenticatedRequest) {
	var input disableTOTPRequest
	if !decodeJSON(response, request, &input) {
		return
	}
	admin, err := api.config.Store.AdminByID(request.Context(), auth.admin.ID)
	if err != nil || !admin.TOTPConfirmed {
		writeError(response, http.StatusConflict, "mfa_disabled", "TOTP is not enabled.")
		return
	}
	passwordOK, err := security.VerifyPassword(input.Password, admin.PasswordHash)
	if err != nil || !passwordOK {
		writeError(response, http.StatusUnauthorized, "invalid_credentials", "The credentials are invalid.")
		return
	}
	secret, err := api.config.MasterKey.Decrypt(admin.TOTPSecretCipher)
	if err != nil {
		writeInternal(response)
		return
	}
	if security.VerifyTOTP(string(secret), input.Code, api.now()) {
		err = api.config.Store.DisableTOTP(request.Context(), admin.ID, api.now())
	} else {
		err = api.config.Store.DisableTOTPWithRecovery(request.Context(), admin.ID, security.DigestRecoveryCode(input.Code), api.now())
	}
	if errors.Is(err, controlstore.ErrRecoveryCode) {
		writeError(response, http.StatusUnauthorized, "invalid_credentials", "The credentials are invalid.")
		return
	}
	if err != nil {
		writeInternal(response)
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

func (api *adminAPI) allow(response http.ResponseWriter, request *http.Request, limiter *security.Limiter) bool {
	allowed, retry := limiter.Allow(clientIP(request.RemoteAddr))
	if allowed {
		return true
	}
	seconds := int(retry.Round(time.Second) / time.Second)
	if seconds < 1 {
		seconds = 1
	}
	response.Header().Set("Retry-After", strconv.Itoa(seconds))
	writeError(response, http.StatusTooManyRequests, "rate_limited", "Too many attempts. Try again later.")
	return false
}

func (api *adminAPI) now() time.Time { return api.config.Now().UTC() }

func (api *adminAPI) setCookies(response http.ResponseWriter, token, csrf string, expires time.Time) {
	http.SetCookie(response, &http.Cookie{
		Name: sessionCookieName, Value: token, Path: "/", Expires: expires,
		HttpOnly: true, Secure: true, SameSite: http.SameSiteStrictMode,
	})
	http.SetCookie(response, &http.Cookie{
		Name: csrfCookieName, Value: csrf, Path: "/", Expires: expires,
		HttpOnly: false, Secure: true, SameSite: http.SameSiteStrictMode,
	})
}

func (api *adminAPI) clearCookies(response http.ResponseWriter) {
	for _, name := range []string{sessionCookieName, csrfCookieName} {
		http.SetCookie(response, &http.Cookie{
			Name: name, Path: "/", MaxAge: -1, Expires: time.Unix(1, 0),
			HttpOnly: name == sessionCookieName, Secure: true, SameSite: http.SameSiteStrictMode,
		})
	}
}

func (api *adminAPI) notFound(response http.ResponseWriter, _ *http.Request) {
	writeError(response, http.StatusNotFound, "not_found", "The API endpoint was not found.")
}

func validUsername(value string) bool {
	if len(value) < 3 || len(value) > 64 {
		return false
	}
	for _, character := range value {
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9' || strings.ContainsRune("._-@", character) {
			continue
		}
		return false
	}
	return true
}

func clientIP(remoteAddress string) string {
	host, _, err := net.SplitHostPort(remoteAddress)
	if err == nil {
		return host
	}
	return remoteAddress
}

func decodeJSON(response http.ResponseWriter, request *http.Request, target any) bool {
	contentType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if err != nil || !strings.EqualFold(contentType, "application/json") {
		writeError(response, http.StatusUnsupportedMediaType, "content_type_invalid", "Content-Type must be application/json.")
		return false
	}
	request.Body = http.MaxBytesReader(response, request.Body, maxJSONBody)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeError(response, http.StatusBadRequest, "json_invalid", "The JSON request is invalid.")
		return false
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		writeError(response, http.StatusBadRequest, "json_invalid", "The JSON request is invalid.")
		return false
	}
	return true
}

func noStoreAPI(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if strings.HasPrefix(request.URL.Path, "/api/") || strings.HasPrefix(request.URL.Path, "/agent/") {
			response.Header().Set("Cache-Control", "no-store")
			response.Header().Set("X-Content-Type-Options", "nosniff")
		}
		next.ServeHTTP(response, request)
	})
}

func sameOrigin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		origin := request.Header.Get("Origin")
		if origin != "" {
			parsed, err := url.Parse(origin)
			if err != nil || parsed.User != nil || parsed.Host == "" || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" ||
				!strings.EqualFold(parsed.Host, request.Host) || parsed.Scheme != requestScheme(request) {
				writeError(response, http.StatusForbidden, "origin_invalid", "Cross-origin requests are not allowed.")
				return
			}
		}
		if strings.EqualFold(request.Header.Get("Sec-Fetch-Site"), "cross-site") {
			writeError(response, http.StatusForbidden, "origin_invalid", "Cross-origin requests are not allowed.")
			return
		}
		next.ServeHTTP(response, request)
	})
}

func requestScheme(request *http.Request) string {
	if request.TLS != nil {
		return "https"
	}
	forwarded := strings.ToLower(strings.TrimSpace(request.Header.Get("X-Forwarded-Proto")))
	if forwarded == "http" || forwarded == "https" {
		return forwarded
	}
	return "http"
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("X-Content-Type-Options", "nosniff")
		response.Header().Set("X-Frame-Options", "DENY")
		response.Header().Set("Referrer-Policy", "no-referrer")
		response.Header().Set("Content-Security-Policy", "default-src 'self'; connect-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; script-src 'self'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'")
		next.ServeHTTP(response, request)
	})
}

func writeInternal(response http.ResponseWriter) {
	writeError(response, http.StatusInternalServerError, "internal_error", "The request could not be completed.")
}

func writeError(response http.ResponseWriter, status int, code, message string) {
	writeJSON(response, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}

func writeJSON(response http.ResponseWriter, status int, body any) {
	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(body)
}
