package control

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/senhao-xu/vps-status/internal/protocol"
	"github.com/senhao-xu/vps-status/internal/security"
	controlstore "github.com/senhao-xu/vps-status/internal/storage/control"
)

const (
	defaultEnrollmentTTL = time.Hour
	minEnrollmentTTL     = 5 * time.Minute
	maxEnrollmentTTL     = 24 * time.Hour
	liveWriteTimeout     = 20 * time.Second
)

type createEnrollmentRequest struct {
	NodeName         string `json:"node_name"`
	ControlURL       string `json:"control_url"`
	ExpiresInSeconds int64  `json:"expires_in_seconds,omitempty"`
}

type enrollmentStatusDTO struct {
	ID        string    `json:"id"`
	NodeName  string    `json:"node_name"`
	ExpiresAt time.Time `json:"expires_at"`
	Status    string    `json:"status"`
	NodeID    string    `json:"node_id,omitempty"`
}

func (api *adminAPI) enrollmentStatus(response http.ResponseWriter, request *http.Request, _ authenticatedRequest) {
	id := request.PathValue("id")
	if id == "" || len(id) > 128 {
		writeError(response, http.StatusNotFound, "not_found", "The enrollment was not found.")
		return
	}
	record, err := api.config.Store.EnrollmentByID(request.Context(), id)
	if errors.Is(err, controlstore.ErrNotFound) {
		writeError(response, http.StatusNotFound, "not_found", "The enrollment was not found.")
		return
	}
	if err != nil {
		writeInternal(response)
		return
	}
	dto := enrollmentStatusDTO{ID: record.ID, NodeName: record.NodeName, ExpiresAt: record.ExpiresAt, Status: "pending", NodeID: record.ConsumedNodeID}
	if dto.NodeID != "" {
		dto.Status = "registered"
		if api.config.Live.Snapshot(dto.NodeID).Online {
			dto.Status = "online"
		}
	} else if !record.ExpiresAt.After(api.now()) {
		dto.Status = "expired"
	}
	writeJSON(response, http.StatusOK, dto)
}

func (api *adminAPI) revokeEnrollment(response http.ResponseWriter, request *http.Request, _ authenticatedRequest) {
	id := request.PathValue("id")
	if id == "" || len(id) > 128 {
		writeError(response, http.StatusNotFound, "not_found", "The enrollment was not found.")
		return
	}
	err := api.config.Store.RevokeEnrollment(request.Context(), id)
	if errors.Is(err, controlstore.ErrEnrollmentConsumed) {
		writeError(response, http.StatusConflict, "enrollment_consumed", "The enrollment has already registered a node.")
		return
	}
	if errors.Is(err, controlstore.ErrNotFound) {
		writeError(response, http.StatusNotFound, "not_found", "The enrollment was not found.")
		return
	}
	if err != nil {
		writeInternal(response)
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

func (api *adminAPI) createEnrollment(response http.ResponseWriter, request *http.Request, _ authenticatedRequest) {
	var input createEnrollmentRequest
	if !decodeJSON(response, request, &input) {
		return
	}
	input.NodeName = strings.TrimSpace(input.NodeName)
	controlURL, err := validateControlURL(input.ControlURL)
	if err != nil || input.NodeName == "" || len(input.NodeName) > 128 {
		writeError(response, http.StatusBadRequest, "invalid_input", "The node name or Control URL is invalid.")
		return
	}
	if input.ExpiresInSeconds == 0 {
		input.ExpiresInSeconds = int64(defaultEnrollmentTTL / time.Second)
	}
	if input.ExpiresInSeconds < int64(minEnrollmentTTL/time.Second) || input.ExpiresInSeconds > int64(maxEnrollmentTTL/time.Second) {
		writeError(response, http.StatusBadRequest, "invalid_expiry", "Enrollment expiry must be between 300 and 86400 seconds.")
		return
	}
	ttl := time.Duration(input.ExpiresInSeconds) * time.Second
	plaintext, err := security.RandomToken()
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
	record := controlstore.EnrollmentToken{ID: id, Digest: security.DigestEnrollmentToken(plaintext), NodeName: input.NodeName, CreatedAt: now, ExpiresAt: now.Add(ttl)}
	if err := api.config.Store.CreateEnrollmentToken(request.Context(), record); errors.Is(err, controlstore.ErrEnrollmentTokenLimit) {
		writeError(response, http.StatusConflict, "enrollment_token_limit", "The active enrollment token limit has been reached.")
		return
	} else if err != nil {
		writeInternal(response)
		return
	}
	enrollCommand := fmt.Sprintf("vps-status-agent enroll --control-url %s --token %s", shellQuote(controlURL), shellQuote(plaintext))
	writeJSON(response, http.StatusCreated, map[string]any{
		"id": id, "enrollment_token": plaintext, "expires_at": record.ExpiresAt,
		"install_command": agentInstallCommand(controlURL, plaintext),
		"enroll_command":  enrollCommand,
	})
}

func (api *adminAPI) enrollAgent(response http.ResponseWriter, request *http.Request) {
	if !api.allow(response, request, api.config.EnrollmentLimiter) {
		return
	}
	plaintext, ok := bearerCredential(request)
	if !ok {
		writeError(response, http.StatusUnauthorized, "enrollment_invalid", "The enrollment token is invalid or unavailable.")
		return
	}
	nodeID, err := security.RandomToken()
	if err != nil {
		writeInternal(response)
		return
	}
	credentialID, err := security.RandomToken()
	if err != nil {
		writeInternal(response)
		return
	}
	credential, err := security.RandomToken()
	if err != nil {
		writeInternal(response)
		return
	}
	node, err := api.config.Store.ConsumeEnrollment(request.Context(), security.DigestEnrollmentToken(plaintext), nodeID, credentialID, security.DigestNodeCredential(credential), api.now())
	if err != nil {
		if errors.Is(err, controlstore.ErrEnrollmentInvalid) || errors.Is(err, controlstore.ErrEnrollmentExpired) || errors.Is(err, controlstore.ErrEnrollmentConsumed) {
			writeError(response, http.StatusUnauthorized, "enrollment_invalid", "The enrollment token is invalid or unavailable.")
		} else {
			writeInternal(response)
		}
		return
	}
	writeJSON(response, http.StatusCreated, map[string]any{"node_id": node.ID, "node_name": node.Name, "credential": credential, "websocket_path": "/agent/v1/ws"})
}

func (api *adminAPI) rotateNodeCredential(response http.ResponseWriter, request *http.Request, _ authenticatedRequest) {
	nodeID := request.PathValue("id")
	if nodeID == "" || len(nodeID) > 128 {
		writeError(response, http.StatusNotFound, "not_found", "The node was not found.")
		return
	}
	credential, err := security.RandomToken()
	if err != nil {
		writeInternal(response)
		return
	}
	credentialID, err := security.RandomToken()
	if err != nil {
		writeInternal(response)
		return
	}
	if err := api.config.Store.RotateNodeCredential(request.Context(), nodeID, credentialID, security.DigestNodeCredential(credential), api.now()); err != nil {
		if errors.Is(err, controlstore.ErrNotFound) {
			writeError(response, http.StatusNotFound, "not_found", "The node was not found.")
		} else {
			writeInternal(response)
		}
		return
	}
	api.config.Registry.DisconnectNode(nodeID, "credential_rotated")
	writeJSON(response, http.StatusOK, map[string]string{"node_id": nodeID, "credential": credential})
}

func (api *adminAPI) revokeNodeCredentials(response http.ResponseWriter, request *http.Request, _ authenticatedRequest) {
	nodeID := request.PathValue("id")
	if nodeID == "" || len(nodeID) > 128 {
		writeError(response, http.StatusNotFound, "not_found", "The node was not found.")
		return
	}
	if err := api.config.Store.RevokeNodeCredentials(request.Context(), nodeID, api.now()); err != nil {
		if errors.Is(err, controlstore.ErrNotFound) {
			writeError(response, http.StatusNotFound, "not_found", "The node was not found.")
		} else {
			writeInternal(response)
		}
		return
	}
	api.config.Registry.DisconnectNode(nodeID, "credential_revoked")
	response.WriteHeader(http.StatusNoContent)
}

type nodeDTO struct {
	ID             string           `json:"id"`
	Name           string           `json:"name"`
	Group          string           `json:"group"`
	Hidden         bool             `json:"hidden"`
	Hostname       string           `json:"hostname"`
	OS             string           `json:"os"`
	Kernel         string           `json:"kernel"`
	Architecture   string           `json:"architecture"`
	Virtualization string           `json:"virtualization"`
	CreatedAt      time.Time        `json:"created_at"`
	UpdatedAt      time.Time        `json:"updated_at"`
	Connected      bool             `json:"connected"`
	Online         bool             `json:"online"`
	LastReportAt   *time.Time       `json:"last_report_at,omitempty"`
	Report         *protocol.Report `json:"report,omitempty"`
}

func (api *adminAPI) nodeDTO(node controlstore.Node) nodeDTO {
	snapshot := api.config.Live.Snapshot(node.ID)
	result := nodeDTO{
		ID: node.ID, Name: node.Name, Group: node.Group, Hidden: node.Hidden,
		Hostname: node.Facts.Hostname, OS: node.Facts.OS, Kernel: node.Facts.Kernel,
		Architecture: node.Facts.Architecture, Virtualization: node.Facts.Virtualization,
		CreatedAt: node.CreatedAt, UpdatedAt: node.UpdatedAt,
		Connected: snapshot.Connected, Online: snapshot.Online, Report: snapshot.Report,
	}
	if !snapshot.ReceivedAt.IsZero() {
		receivedAt := snapshot.ReceivedAt
		result.LastReportAt = &receivedAt
	}
	return result
}

func (api *adminAPI) nodes(response http.ResponseWriter, request *http.Request, _ authenticatedRequest) {
	nodes, err := api.config.Store.ListNodes(request.Context())
	if err != nil {
		writeInternal(response)
		return
	}
	result := make([]nodeDTO, 0, len(nodes))
	for _, node := range nodes {
		result = append(result, api.nodeDTO(node))
	}
	writeJSON(response, http.StatusOK, struct {
		Nodes []nodeDTO `json:"nodes"`
	}{Nodes: result})
}

func (api *adminAPI) node(response http.ResponseWriter, request *http.Request, _ authenticatedRequest) {
	node, err := api.config.Store.NodeByID(request.Context(), request.PathValue("id"))
	if errors.Is(err, controlstore.ErrNotFound) {
		writeError(response, http.StatusNotFound, "not_found", "The node was not found.")
		return
	}
	if err != nil {
		writeInternal(response)
		return
	}
	writeJSON(response, http.StatusOK, api.nodeDTO(node))
}

type updateNodeRequest struct {
	Name   *string `json:"name"`
	Group  *string `json:"group"`
	Hidden *bool   `json:"hidden"`
}

func (api *adminAPI) updateNode(response http.ResponseWriter, request *http.Request, _ authenticatedRequest) {
	current, err := api.config.Store.NodeByID(request.Context(), request.PathValue("id"))
	if errors.Is(err, controlstore.ErrNotFound) {
		writeError(response, http.StatusNotFound, "not_found", "The node was not found.")
		return
	}
	if err != nil {
		writeInternal(response)
		return
	}
	var input updateNodeRequest
	if !decodeJSON(response, request, &input) {
		return
	}
	if input.Name != nil {
		current.Name = strings.TrimSpace(*input.Name)
	}
	if input.Group != nil {
		current.Group = strings.TrimSpace(*input.Group)
	}
	if input.Hidden != nil {
		current.Hidden = *input.Hidden
	}
	if current.Name == "" || len(current.Name) > 128 || len(current.Group) > 128 {
		writeError(response, http.StatusBadRequest, "invalid_input", "The node name or group is invalid.")
		return
	}
	now := api.now()
	if err := api.config.Store.UpdateNode(request.Context(), current.ID, current.Name, current.Group, current.Hidden, now); err != nil {
		if errors.Is(err, controlstore.ErrNotFound) {
			writeError(response, http.StatusNotFound, "not_found", "The node was not found.")
		} else {
			writeInternal(response)
		}
		return
	}
	current.UpdatedAt = now.UTC()
	if api.config.Alerts != nil {
		api.config.Alerts.Refresh()
	}
	writeJSON(response, http.StatusOK, api.nodeDTO(current))
}

func (api *adminAPI) deleteNode(response http.ResponseWriter, request *http.Request, _ authenticatedRequest) {
	nodeID := request.PathValue("id")
	now := api.now()
	if err := api.config.Store.DeleteNode(request.Context(), nodeID, now); err != nil {
		if errors.Is(err, controlstore.ErrNotFound) {
			writeError(response, http.StatusNotFound, "not_found", "The node was not found.")
		} else {
			writeInternal(response)
		}
		return
	}
	api.config.Registry.DisconnectNode(nodeID, "node_deleted")
	api.config.Live.deleteNode(nodeID, now)
	if api.config.History != nil {
		api.config.History.deleteNode(nodeID)
	}
	if api.config.ProbeHistory != nil {
		api.config.ProbeHistory.deleteNode(nodeID)
	}
	response.WriteHeader(http.StatusNoContent)
}

type liveEventDTO struct {
	Kind       LiveEventKind    `json:"kind"`
	NodeID     string           `json:"node_id"`
	Connected  bool             `json:"connected"`
	Online     bool             `json:"online"`
	ReceivedAt time.Time        `json:"received_at"`
	Report     *protocol.Report `json:"report,omitempty"`
}

func (api *adminAPI) liveEvents(response http.ResponseWriter, request *http.Request, auth authenticatedRequest) {
	_, ok := response.(http.Flusher)
	if !ok {
		writeInternal(response)
		return
	}
	id, events, err := api.config.Live.Subscribe()
	if errors.Is(err, errLiveSubscriberLimit) {
		writeError(response, http.StatusServiceUnavailable, "live_subscriber_limit", "Too many live streams are active.")
		return
	}
	if err != nil {
		writeInternal(response)
		return
	}
	defer api.config.Live.Unsubscribe(id)
	response.Header().Set("Content-Type", "text/event-stream")
	response.Header().Set("Cache-Control", "no-store")
	response.Header().Set("X-Accel-Buffering", "no")
	response.WriteHeader(http.StatusOK)
	if err := writeLiveEvent(response, []byte(": ready\n\n")); err != nil {
		return
	}

	keepalive := time.NewTicker(15 * time.Second)
	defer keepalive.Stop()
	expires := time.NewTimer(auth.session.ExpiresAt.Sub(api.now()))
	defer expires.Stop()
	for {
		select {
		case <-request.Context().Done():
			return
		case <-expires.C:
			return
		case event, open := <-events:
			if !open {
				return
			}
			data, marshalErr := json.Marshal(liveEventDTO{
				Kind: event.Kind, NodeID: event.NodeID, Connected: event.Connected,
				Online: event.Online, ReceivedAt: event.ReceivedAt, Report: event.Report,
			})
			if marshalErr != nil {
				return
			}
			if err := writeLiveEvent(response, fmt.Appendf(nil, "event: %s\ndata: %s\n\n", event.Kind, data)); err != nil {
				return
			}
		case <-keepalive.C:
			if _, err := api.config.Store.SessionByTokenDigest(request.Context(), auth.token, api.now()); err != nil {
				return
			}
			if err := writeLiveEvent(response, []byte(": keepalive\n\n")); err != nil {
				return
			}
		}
	}
}

func writeLiveEvent(response http.ResponseWriter, data []byte) error {
	controller := http.NewResponseController(response)
	if err := controller.SetWriteDeadline(time.Now().Add(liveWriteTimeout)); err != nil && !errors.Is(err, http.ErrNotSupported) {
		return err
	}
	if _, err := response.Write(data); err != nil {
		return err
	}
	return controller.Flush()
}

func validateControlURL(raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.User != nil || parsed.Host == "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", errors.New("invalid Control URL")
	}
	if parsed.Scheme != "https" {
		host := parsed.Hostname()
		ip := net.ParseIP(host)
		if parsed.Scheme != "http" || !(strings.EqualFold(host, "localhost") || ip != nil && ip.IsLoopback()) {
			return "", errors.New("Control URL must use HTTPS")
		}
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	return parsed.String(), nil
}

func shellQuote(value string) string { return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'" }
