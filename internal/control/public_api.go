package control

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/senhao-xu/vps-status/internal/protocol"
	controlstore "github.com/senhao-xu/vps-status/internal/storage/control"
)

type publicPageConfigDTO struct {
	Enabled     bool      `json:"enabled"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	NodeIDs     []string  `json:"node_ids"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func publicPageDTO(page controlstore.PublicPage) publicPageConfigDTO {
	return publicPageConfigDTO{Enabled: page.Enabled, Title: page.Title, Description: page.Description,
		NodeIDs: append([]string{}, page.NodeIDs...), UpdatedAt: page.UpdatedAt}
}

func (api *adminAPI) publicPageConfig(response http.ResponseWriter, request *http.Request, _ authenticatedRequest) {
	page, err := api.config.Store.PublicPageConfig(request.Context())
	if err != nil {
		writeInternal(response)
		return
	}
	writeJSON(response, http.StatusOK, publicPageDTO(page))
}

type updatePublicPageRequest struct {
	Enabled     bool      `json:"enabled"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	NodeIDs     *[]string `json:"node_ids"`
}

func (api *adminAPI) updatePublicPageConfig(response http.ResponseWriter, request *http.Request, _ authenticatedRequest) {
	var input updatePublicPageRequest
	if !decodeJSON(response, request, &input) {
		return
	}
	page := controlstore.PublicPage{Enabled: input.Enabled, Title: strings.TrimSpace(input.Title),
		Description: strings.TrimSpace(input.Description)}
	if err := api.config.Store.UpdateSiteSettings(request.Context(), page, input.NodeIDs, api.now()); errors.Is(err, controlstore.ErrInvalidPublicConfiguration) {
		writeError(response, http.StatusBadRequest, "invalid_input", "The public page configuration is invalid.")
		return
	} else if err != nil {
		writeInternal(response)
		return
	}
	page, err := api.config.Store.PublicPageConfig(request.Context())
	if err != nil {
		writeInternal(response)
		return
	}
	writeJSON(response, http.StatusOK, publicPageDTO(page))
}

// publicNodeDTO is intentionally independent from nodeDTO. Adding a field to
// the administrator model cannot expand the unauthenticated API.
type publicNodeDTO struct {
	Name                   string   `json:"name"`
	Group                  string   `json:"group"`
	Online                 bool     `json:"online"`
	UptimeSeconds          *uint64  `json:"uptime_seconds"`
	CPUUsagePercent        *float64 `json:"cpu_usage_percent"`
	MemoryUsedBytes        *uint64  `json:"memory_used_bytes"`
	MemoryTotalBytes       *uint64  `json:"memory_total_bytes"`
	DiskUsedBytes          *uint64  `json:"disk_used_bytes"`
	DiskTotalBytes         *uint64  `json:"disk_total_bytes"`
	ReceiveBytesPerSecond  *uint64  `json:"receive_bytes_per_second"`
	TransmitBytesPerSecond *uint64  `json:"transmit_bytes_per_second"`
	MonthReceiveBytes      *uint64  `json:"month_receive_bytes"`
	MonthTransmitBytes     *uint64  `json:"month_transmit_bytes"`
}

type publicStatusDTO struct {
	Page struct {
		Enabled     bool   `json:"enabled"`
		Title       string `json:"title"`
		Description string `json:"description"`
	} `json:"page"`
	Nodes []publicNodeDTO `json:"nodes"`
}

func (api *adminAPI) publicStatus(response http.ResponseWriter, request *http.Request) {
	page, err := api.config.Store.PublicPageConfig(request.Context())
	if err != nil {
		writeInternal(response)
		return
	}
	if !page.Enabled {
		api.auth(false, func(response http.ResponseWriter, request *http.Request, _ authenticatedRequest) {
			api.writeHomepageStatus(response, request, page)
		})(response, request)
		return
	}
	api.writeHomepageStatus(response, request, page)
}

// Both access modes use this single strict public projection. Authentication
// grants access to the homepage without expanding its fields or node set.
func (api *adminAPI) writeHomepageStatus(response http.ResponseWriter, request *http.Request, page controlstore.PublicPage) {
	result := publicStatusDTO{Nodes: make([]publicNodeDTO, 0)}
	result.Page.Enabled, result.Page.Title, result.Page.Description = true, page.Title, page.Description
	nodes, err := api.config.Store.ListPublishedNodes(request.Context())
	if err != nil {
		writeInternal(response)
		return
	}
	month := currentUTCMonth(api.now())
	for _, node := range nodes {
		dto := publicNodeDTO{Name: node.Name, Group: node.Group}
		snapshot := api.config.Live.Snapshot(node.ID)
		dto.Online = snapshot.Online
		if snapshot.Online && snapshot.Report != nil {
			projectPublicReport(&dto, *snapshot.Report)
		}
		if api.config.Telemetry != nil && api.config.Telemetry.State().Available {
			if totals, queryErr := api.config.Telemetry.Traffic(request.Context(), node.ID); queryErr == nil {
				for _, total := range totals {
					if total.Period == month {
						rx, tx := total.ReceiveBytes, total.TransmitBytes
						dto.MonthReceiveBytes, dto.MonthTransmitBytes = &rx, &tx
						break
					}
				}
			}
		}
		result.Nodes = append(result.Nodes, dto)
	}
	writeJSON(response, http.StatusOK, result)
}

func currentUTCMonth(now time.Time) string {
	return now.UTC().Format("2006-01")
}

func projectPublicReport(dto *publicNodeDTO, report protocol.Report) {
	unavailable := make(map[protocol.MetricArea]struct{}, len(report.Unavailable))
	for _, area := range report.Unavailable {
		unavailable[area] = struct{}{}
	}
	if _, missing := unavailable[protocol.AreaUptime]; !missing {
		value := report.UptimeSeconds
		dto.UptimeSeconds = &value
	}
	if _, missing := unavailable[protocol.AreaCPU]; !missing {
		value := report.CPU.UsagePercent
		dto.CPUUsagePercent = &value
	}
	if _, missing := unavailable[protocol.AreaMemory]; !missing {
		used, total := report.Memory.UsedBytes, report.Memory.TotalBytes
		dto.MemoryUsedBytes, dto.MemoryTotalBytes = &used, &total
	}
	if _, missing := unavailable[protocol.AreaDisk]; !missing {
		used, total := report.Disk.UsedBytes, report.Disk.TotalBytes
		dto.DiskUsedBytes, dto.DiskTotalBytes = &used, &total
	}
	if _, missing := unavailable[protocol.AreaNetwork]; !missing {
		rx, tx := report.Network.ReceiveBytesPerSecond, report.Network.TransmitBytesPerSecond
		dto.ReceiveBytesPerSecond, dto.TransmitBytesPerSecond = &rx, &tx
	}
}
