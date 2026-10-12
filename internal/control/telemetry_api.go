package control

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/senhao-xu/vps-status/internal/storage/telemetry"
)

func (api *adminAPI) telemetryStorage(response http.ResponseWriter, request *http.Request, _ authenticatedRequest) {
	if api.config.Telemetry == nil {
		writeJSON(response, http.StatusOK, map[string]any{"state": telemetry.State{Available: false, Writable: false, Reason: "not_configured"}})
		return
	}
	stats, err := api.config.Telemetry.Stats(request.Context())
	if err != nil {
		if errors.Is(err, telemetry.ErrUnavailable) {
			writeJSON(response, http.StatusOK, map[string]any{"state": api.config.Telemetry.State()})
			return
		}
		writeInternal(response)
		return
	}
	writeJSON(response, http.StatusOK, stats)
}

func (api *adminAPI) telemetryHost(response http.ResponseWriter, request *http.Request, _ authenticatedRequest) {
	if !api.requireTelemetry(response) {
		return
	}
	nodeID := strings.TrimSpace(request.URL.Query().Get("node_id"))
	resolution := telemetry.Resolution(request.URL.Query().Get("resolution"))
	from, fromErr := time.Parse(time.RFC3339, request.URL.Query().Get("from"))
	to, toErr := time.Parse(time.RFC3339, request.URL.Query().Get("to"))
	if nodeID == "" || len(nodeID) > 128 || fromErr != nil || toErr != nil || !from.Before(to) || to.Sub(from) > 366*24*time.Hour {
		writeError(response, http.StatusBadRequest, "invalid_input", "The telemetry query is invalid.")
		return
	}
	points, err := api.config.Telemetry.QueryHost(request.Context(), nodeID, resolution, from, to)
	if errors.Is(err, telemetry.ErrInvalidResolution) {
		writeError(response, http.StatusBadRequest, "invalid_resolution", "The telemetry resolution is invalid.")
		return
	}
	if err != nil {
		api.writeTelemetryError(response)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"points": points})
}

func (api *adminAPI) telemetryTraffic(response http.ResponseWriter, request *http.Request, _ authenticatedRequest) {
	if !api.requireTelemetry(response) {
		return
	}
	nodeID := strings.TrimSpace(request.URL.Query().Get("node_id"))
	if nodeID == "" || len(nodeID) > 128 {
		writeError(response, http.StatusBadRequest, "invalid_input", "The node identifier is invalid.")
		return
	}
	totals, err := api.config.Telemetry.Traffic(request.Context(), nodeID)
	if err != nil {
		api.writeTelemetryError(response)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"traffic": totals})
}

func (api *adminAPI) telemetryProbe(response http.ResponseWriter, request *http.Request, _ authenticatedRequest) {
	if !api.requireTelemetry(response) {
		return
	}
	nodeID := strings.TrimSpace(request.URL.Query().Get("node_id"))
	taskID := strings.TrimSpace(request.URL.Query().Get("task_id"))
	resolution := telemetry.Resolution(request.URL.Query().Get("resolution"))
	from, fromErr := time.Parse(time.RFC3339, request.URL.Query().Get("from"))
	to, toErr := time.Parse(time.RFC3339, request.URL.Query().Get("to"))
	if nodeID == "" || len(nodeID) > 128 || taskID == "" || len(taskID) > 128 || fromErr != nil || toErr != nil || !from.Before(to) || to.Sub(from) > 366*24*time.Hour {
		writeError(response, http.StatusBadRequest, "invalid_input", "The probe telemetry query is invalid.")
		return
	}
	points, err := api.config.Telemetry.QueryProbe(request.Context(), nodeID, taskID, resolution, from, to)
	if errors.Is(err, telemetry.ErrInvalidResolution) {
		writeError(response, http.StatusBadRequest, "invalid_resolution", "The telemetry resolution is invalid.")
		return
	}
	if err != nil {
		api.writeTelemetryError(response)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"points": points})
}

type telemetryConfirmation struct {
	Confirm string `json:"confirm"`
	Pages   int    `json:"pages,omitempty"`
}

func (api *adminAPI) telemetryClearHistory(response http.ResponseWriter, request *http.Request, _ authenticatedRequest) {
	input, ok := decodeTelemetryConfirmation(response, request, "clear_history")
	if !ok || !api.requireTelemetry(response) {
		return
	}
	_ = input
	var err error
	if api.config.ProbeHistory != nil {
		err = api.config.ProbeHistory.ClearHistory(request.Context(), api.config.History)
	} else if api.config.History != nil {
		err = api.config.History.ClearHistory(request.Context())
	} else {
		err = api.config.Telemetry.ClearHistory(request.Context())
	}
	if err != nil {
		api.writeTelemetryError(response)
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

func (api *adminAPI) telemetryResetAll(response http.ResponseWriter, request *http.Request, _ authenticatedRequest) {
	_, ok := decodeTelemetryConfirmation(response, request, "reset_all_telemetry")
	if !ok || !api.requireTelemetry(response) {
		return
	}
	var err error
	if api.config.ProbeHistory != nil {
		err = api.config.ProbeHistory.ResetAll(request.Context(), api.config.History)
	} else if api.config.History != nil {
		err = api.config.History.ResetAll(request.Context())
	} else {
		err = api.config.Telemetry.ResetAll(request.Context())
	}
	if err != nil {
		api.writeTelemetryError(response)
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

func (api *adminAPI) telemetryCheckpoint(response http.ResponseWriter, request *http.Request, _ authenticatedRequest) {
	_, ok := decodeTelemetryConfirmation(response, request, "checkpoint")
	if !ok || !api.requireTelemetry(response) {
		return
	}
	if err := api.config.Telemetry.Checkpoint(request.Context()); err != nil {
		api.writeTelemetryError(response)
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

func (api *adminAPI) telemetryIncrementalVacuum(response http.ResponseWriter, request *http.Request, _ authenticatedRequest) {
	input, ok := decodeTelemetryConfirmation(response, request, "incremental_vacuum")
	if !ok || !api.requireTelemetry(response) {
		return
	}
	if input.Pages < 0 || input.Pages > 100000 {
		writeError(response, http.StatusBadRequest, "invalid_input", "The page count is invalid.")
		return
	}
	if err := api.config.Telemetry.IncrementalVacuum(request.Context(), input.Pages); err != nil {
		api.writeTelemetryError(response)
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

func (api *adminAPI) telemetryFullVacuum(response http.ResponseWriter, request *http.Request, _ authenticatedRequest) {
	_, ok := decodeTelemetryConfirmation(response, request, "full_vacuum")
	if !ok || !api.requireTelemetry(response) {
		return
	}
	if err := api.config.Telemetry.FullVacuum(request.Context()); err != nil {
		api.writeTelemetryError(response)
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

func decodeTelemetryConfirmation(response http.ResponseWriter, request *http.Request, want string) (telemetryConfirmation, bool) {
	var input telemetryConfirmation
	if !decodeJSON(response, request, &input) {
		return input, false
	}
	if input.Confirm != want {
		writeError(response, http.StatusBadRequest, "confirmation_required", "The destructive telemetry confirmation is missing.")
		return input, false
	}
	return input, true
}

func (api *adminAPI) requireTelemetry(response http.ResponseWriter) bool {
	if api.config.Telemetry == nil || !api.config.Telemetry.State().Available {
		writeError(response, http.StatusServiceUnavailable, "telemetry_unavailable", "Telemetry history is unavailable.")
		return false
	}
	return true
}

func (api *adminAPI) writeTelemetryError(response http.ResponseWriter) {
	writeError(response, http.StatusServiceUnavailable, "telemetry_unavailable", "Telemetry history is temporarily unavailable.")
}
