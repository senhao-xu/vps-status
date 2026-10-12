package control

import (
	"errors"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/senhao-xu/vps-status/internal/protocol"
	"github.com/senhao-xu/vps-status/internal/security"
	controlstore "github.com/senhao-xu/vps-status/internal/storage/control"
)

type probeTaskDTO struct {
	ID              string        `json:"id"`
	Name            string        `json:"name"`
	Host            string        `json:"host"`
	Port            uint16        `json:"port"`
	IntervalSeconds uint32        `json:"interval_seconds"`
	NodeIDs         []string      `json:"node_ids"`
	CreatedAt       time.Time     `json:"created_at"`
	UpdatedAt       time.Time     `json:"updated_at"`
	Latest          []ProbeLatest `json:"latest"`
}

func (api *adminAPI) probeDTO(task controlstore.ProbeTask) probeTaskDTO {
	latest := api.config.ProbeHistory.Latest("", task.ID)
	sort.Slice(latest, func(i, j int) bool { return latest[i].NodeID < latest[j].NodeID })
	return probeTaskDTO{ID: task.ID, Name: task.Name, Host: task.Host, Port: task.Port,
		IntervalSeconds: task.IntervalSeconds, NodeIDs: append([]string{}, task.NodeIDs...),
		CreatedAt: task.CreatedAt, UpdatedAt: task.UpdatedAt, Latest: latest}
}

func (api *adminAPI) probes(response http.ResponseWriter, request *http.Request, _ authenticatedRequest) {
	tasks, err := api.config.Store.ListProbeTasks(request.Context())
	if err != nil {
		writeInternal(response)
		return
	}
	result := make([]probeTaskDTO, 0, len(tasks))
	for _, task := range tasks {
		result = append(result, api.probeDTO(task))
	}
	writeJSON(response, http.StatusOK, map[string]any{"probes": result})
}

func (api *adminAPI) probe(response http.ResponseWriter, request *http.Request, _ authenticatedRequest) {
	task, err := api.config.Store.ProbeTaskByID(request.Context(), request.PathValue("id"))
	if errors.Is(err, controlstore.ErrNotFound) {
		writeError(response, http.StatusNotFound, "not_found", "The TCP probe task was not found.")
		return
	}
	if err != nil {
		writeInternal(response)
		return
	}
	writeJSON(response, http.StatusOK, api.probeDTO(task))
}

type createProbeRequest struct {
	Name            string   `json:"name"`
	Host            string   `json:"host"`
	Port            uint16   `json:"port"`
	IntervalSeconds uint32   `json:"interval_seconds"`
	NodeIDs         []string `json:"node_ids"`
}

func (api *adminAPI) createProbe(response http.ResponseWriter, request *http.Request, _ authenticatedRequest) {
	var input createProbeRequest
	if !decodeJSON(response, request, &input) {
		return
	}
	input.Name, input.Host = strings.TrimSpace(input.Name), strings.TrimSpace(input.Host)
	id, err := security.RandomToken()
	if err != nil {
		writeInternal(response)
		return
	}
	task := controlstore.ProbeTask{ID: id, Name: input.Name, Host: input.Host, Port: input.Port, IntervalSeconds: input.IntervalSeconds}
	if !validProbeConfiguration(task, input.NodeIDs) {
		writeError(response, http.StatusBadRequest, "invalid_input", "The TCP probe configuration is invalid.")
		return
	}
	now := api.now()
	if _, err := api.config.Store.CreateProbeTask(request.Context(), task, input.NodeIDs, now); err != nil {
		api.writeProbeStoreError(response, err)
		return
	}
	task.NodeIDs, task.CreatedAt, task.UpdatedAt = sortedNodeIDs(input.NodeIDs), now, now
	api.syncProbeNodes(task.NodeIDs)
	writeJSON(response, http.StatusCreated, api.probeDTO(task))
}

type updateProbeRequest struct {
	Name            *string   `json:"name"`
	Host            *string   `json:"host"`
	Port            *uint16   `json:"port"`
	IntervalSeconds *uint32   `json:"interval_seconds"`
	NodeIDs         *[]string `json:"node_ids"`
}

func (api *adminAPI) updateProbe(response http.ResponseWriter, request *http.Request, _ authenticatedRequest) {
	current, err := api.config.Store.ProbeTaskByID(request.Context(), request.PathValue("id"))
	if errors.Is(err, controlstore.ErrNotFound) {
		writeError(response, http.StatusNotFound, "not_found", "The TCP probe task was not found.")
		return
	}
	if err != nil {
		writeInternal(response)
		return
	}
	oldNodes := append([]string(nil), current.NodeIDs...)
	var input updateProbeRequest
	if !decodeJSON(response, request, &input) {
		return
	}
	if input.Name != nil {
		current.Name = strings.TrimSpace(*input.Name)
	}
	if input.Host != nil {
		current.Host = strings.TrimSpace(*input.Host)
	}
	if input.Port != nil {
		current.Port = *input.Port
	}
	if input.IntervalSeconds != nil {
		current.IntervalSeconds = *input.IntervalSeconds
	}
	if input.NodeIDs != nil {
		current.NodeIDs = append([]string(nil), (*input.NodeIDs)...)
	}
	if !validProbeConfiguration(current, current.NodeIDs) {
		writeError(response, http.StatusBadRequest, "invalid_input", "The TCP probe configuration is invalid.")
		return
	}
	now := api.now()
	if _, err := api.config.Store.UpdateProbeTask(request.Context(), current, current.NodeIDs, now); err != nil {
		api.writeProbeStoreError(response, err)
		return
	}
	newNodeSet := make(map[string]struct{}, len(current.NodeIDs))
	for _, nodeID := range current.NodeIDs {
		newNodeSet[nodeID] = struct{}{}
	}
	for _, nodeID := range oldNodes {
		if _, remains := newNodeSet[nodeID]; !remains {
			api.config.ProbeHistory.deleteAssignment(nodeID, current.ID)
		}
	}
	current.NodeIDs, current.UpdatedAt = sortedNodeIDs(current.NodeIDs), now
	api.syncProbeNodes(append(oldNodes, current.NodeIDs...))
	writeJSON(response, http.StatusOK, api.probeDTO(current))
}

func (api *adminAPI) deleteProbe(response http.ResponseWriter, request *http.Request, _ authenticatedRequest) {
	taskID := request.PathValue("id")
	_, nodes, err := api.config.Store.DeleteProbeTask(request.Context(), taskID, api.now())
	if errors.Is(err, controlstore.ErrNotFound) {
		writeError(response, http.StatusNotFound, "not_found", "The TCP probe task was not found.")
		return
	}
	if err != nil {
		writeInternal(response)
		return
	}
	api.config.ProbeHistory.deleteTask(taskID)
	api.syncProbeNodes(nodes)
	response.WriteHeader(http.StatusNoContent)
}

func (api *adminAPI) addProbeAssignment(response http.ResponseWriter, request *http.Request, auth authenticatedRequest) {
	api.changeProbeAssignment(response, request, auth, true)
}

func (api *adminAPI) removeProbeAssignment(response http.ResponseWriter, request *http.Request, auth authenticatedRequest) {
	api.changeProbeAssignment(response, request, auth, false)
}

func (api *adminAPI) changeProbeAssignment(response http.ResponseWriter, request *http.Request, _ authenticatedRequest, add bool) {
	task, err := api.config.Store.ProbeTaskByID(request.Context(), request.PathValue("id"))
	if err != nil {
		api.writeProbeStoreError(response, err)
		return
	}
	nodeID := request.PathValue("node_id")
	nodes := append([]string(nil), task.NodeIDs...)
	index := sort.SearchStrings(nodes, nodeID)
	exists := index < len(nodes) && nodes[index] == nodeID
	if add && !exists {
		nodes = append(nodes, nodeID)
	} else if !add && exists {
		nodes = append(nodes[:index], nodes[index+1:]...)
	} else if add {
		writeError(response, http.StatusConflict, "assignment_exists", "The probe assignment already exists.")
		return
	} else {
		writeError(response, http.StatusNotFound, "not_found", "The probe assignment was not found.")
		return
	}
	if !validProbeConfiguration(task, nodes) {
		writeError(response, http.StatusBadRequest, "invalid_input", "The probe assignment is invalid.")
		return
	}
	if _, err := api.config.Store.UpdateProbeTask(request.Context(), task, nodes, api.now()); err != nil {
		api.writeProbeStoreError(response, err)
		return
	}
	if !add {
		api.config.ProbeHistory.deleteAssignment(nodeID, task.ID)
	}
	api.syncProbeNodes([]string{nodeID})
	response.WriteHeader(http.StatusNoContent)
}

func (api *adminAPI) probeResults(response http.ResponseWriter, request *http.Request, _ authenticatedRequest) {
	nodeID := strings.TrimSpace(request.URL.Query().Get("node_id"))
	taskID := strings.TrimSpace(request.URL.Query().Get("task_id"))
	if len(nodeID) > 128 || len(taskID) > protocol.MaxProbeIDBytes {
		writeError(response, http.StatusBadRequest, "invalid_input", "The probe result filter is invalid.")
		return
	}
	result := api.config.ProbeHistory.Latest(nodeID, taskID)
	sort.Slice(result, func(i, j int) bool {
		if result[i].TaskID == result[j].TaskID {
			return result[i].NodeID < result[j].NodeID
		}
		return result[i].TaskID < result[j].TaskID
	})
	writeJSON(response, http.StatusOK, map[string]any{"results": result})
}

func validProbeConfiguration(task controlstore.ProbeTask, nodeIDs []string) bool {
	if task.Name == "" || len(task.Name) > 128 || !validHelloText(task.Name, 128) || len(nodeIDs) > 10000 {
		return false
	}
	if protocol.ValidateProbeTask(protocol.ProbeTask{ID: task.ID, Host: task.Host, Port: task.Port, IntervalSeconds: task.IntervalSeconds}) != nil {
		return false
	}
	return len(sortedNodeIDs(nodeIDs)) == len(nodeIDs)
}

func sortedNodeIDs(nodeIDs []string) []string {
	result := append([]string(nil), nodeIDs...)
	sort.Strings(result)
	write := 0
	for _, nodeID := range result {
		if nodeID == "" || len(nodeID) > 128 || write > 0 && result[write-1] == nodeID {
			continue
		}
		result[write] = nodeID
		write++
	}
	return result[:write]
}

func (api *adminAPI) syncProbeNodes(nodeIDs []string) {
	for _, nodeID := range sortedNodeIDs(nodeIDs) {
		api.config.Registry.SyncProbes(nodeID)
	}
}

func (api *adminAPI) writeProbeStoreError(response http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, controlstore.ErrNotFound):
		writeError(response, http.StatusNotFound, "not_found", "The TCP probe task or node was not found.")
	case errors.Is(err, controlstore.ErrProbeTaskLimit):
		writeError(response, http.StatusConflict, "probe_task_limit", "A node cannot have more than 64 TCP probe tasks.")
	default:
		writeInternal(response)
	}
}
