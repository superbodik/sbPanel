package api

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/yourorg/panel-daemon/internal/console"
	"github.com/yourorg/panel-daemon/internal/docker"
	"github.com/yourorg/panel-daemon/internal/proxy"
)

type Handlers struct {
	Docker    *docker.Manager
	Console   *console.Hub
	BackupDir string
}

type createServerRequest struct {
	ServerUUID     uuid.UUID         `json:"server_uuid"`
	DockerImage    string            `json:"docker_image"`
	StartupCommand string            `json:"startup_command"`
	Environment    map[string]string `json:"environment"`
	MemoryMB       int64             `json:"memory_mb"`
	SwapMB         int64             `json:"swap_mb"`
	IOWeight       int               `json:"io_weight"`
	CPUPercent     int               `json:"cpu_percent"`
	PortBindings   map[string]string `json:"port_bindings"`
}

type operationResponse struct {
	ServerUUID uuid.UUID `json:"server_uuid"`
	Success    bool      `json:"success"`
	Message    string    `json:"message"`
	State      string    `json:"state"`
}

func (h *Handlers) CreateServer(w http.ResponseWriter, r *http.Request) {
	var req createServerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	_, err := h.Docker.CreateContainer(r.Context(), docker.CreateSpec{
		ServerUUID:     req.ServerUUID,
		DockerImage:    req.DockerImage,
		StartupCommand: req.StartupCommand,
		Environment:    req.Environment,
		MemoryMB:       req.MemoryMB,
		SwapMB:         req.SwapMB,
		IOWeight:       req.IOWeight,
		CPUPercent:     req.CPUPercent,
		PortBindings:   req.PortBindings,
	})
	if err != nil {
		writeJSON(w, http.StatusBadGateway, operationResponse{
			ServerUUID: req.ServerUUID, Success: false, Message: err.Error(), State: "offline",
		})
		return
	}

	writeJSON(w, http.StatusCreated, operationResponse{
		ServerUUID: req.ServerUUID, Success: true, State: "offline",
	})
}

type powerRequest struct {
	Action string `json:"action"`
}

func (h *Handlers) Power(w http.ResponseWriter, r *http.Request) {
	serverUUID, err := uuid.Parse(chi.URLParam(r, "uuid"))
	if err != nil {
		http.Error(w, "invalid server uuid", http.StatusBadRequest)
		return
	}
	var req powerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	containerID := docker.ContainerNameFor(serverUUID)
	ctx := r.Context()

	switch req.Action {
	case "start":
		err = h.Docker.StartContainer(ctx, containerID)
	case "stop":
		err = h.Docker.StopContainer(ctx, containerID, 30)
	case "restart":
		if err = h.Docker.StopContainer(ctx, containerID, 30); err == nil {
			err = h.Docker.StartContainer(ctx, containerID)
		}
	case "kill":
		err = h.Docker.KillContainer(ctx, containerID)
	default:
		http.Error(w, "unknown action", http.StatusBadRequest)
		return
	}

	if err != nil {
		writeJSON(w, http.StatusBadGateway, operationResponse{
			ServerUUID: serverUUID, Success: false, Message: err.Error(),
		})
		return
	}

	state, _ := h.Docker.InspectState(ctx, containerID)
	writeJSON(w, http.StatusOK, operationResponse{ServerUUID: serverUUID, Success: true, State: state})
}

func (h *Handlers) Delete(w http.ResponseWriter, r *http.Request) {
	serverUUID, err := uuid.Parse(chi.URLParam(r, "uuid"))
	if err != nil {
		http.Error(w, "invalid server uuid", http.StatusBadRequest)
		return
	}
	containerID := docker.ContainerNameFor(serverUUID)
	if err := h.Docker.RemoveContainer(r.Context(), containerID); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type resourceStatsResponse struct {
	ServerUUID    uuid.UUID `json:"server_uuid"`
	CPUPercent    float64   `json:"cpu_percent"`
	MemoryBytes   int64     `json:"memory_bytes"`
	DiskBytes     int64     `json:"disk_bytes"`
	NetworkRx     int64     `json:"network_rx"`
	NetworkTx     int64     `json:"network_tx"`
	UptimeSeconds int64     `json:"uptime_seconds"`
	State         string    `json:"state"`
}

func (h *Handlers) Stats(w http.ResponseWriter, r *http.Request) {
	serverUUID, err := uuid.Parse(chi.URLParam(r, "uuid"))
	if err != nil {
		http.Error(w, "invalid server uuid", http.StatusBadRequest)
		return
	}
	containerID := docker.ContainerNameFor(serverUUID)
	ctx := r.Context()

	state, err := h.Docker.InspectState(ctx, containerID)
	if err != nil {
		state = "offline"
	}
	if state != "running" {
		writeJSON(w, http.StatusOK, resourceStatsResponse{ServerUUID: serverUUID, State: state})
		return
	}

	stats, err := h.Docker.Stats(ctx, containerID)
	if err != nil {
		writeJSON(w, http.StatusOK, resourceStatsResponse{ServerUUID: serverUUID, State: state})
		return
	}

	var networkRx, networkTx int64
	for _, iface := range stats.Networks {
		networkRx += int64(iface.RxBytes)
		networkTx += int64(iface.TxBytes)
	}

	cpuDelta := float64(stats.CPUStats.CPUUsage.TotalUsage) - float64(stats.PreCPUStats.CPUUsage.TotalUsage)
	systemDelta := float64(stats.CPUStats.SystemUsage) - float64(stats.PreCPUStats.SystemUsage)
	var cpuPercent float64
	if systemDelta > 0 && cpuDelta > 0 {
		onlineCPUs := float64(stats.CPUStats.OnlineCPUs)
		if onlineCPUs == 0 {
			onlineCPUs = float64(len(stats.CPUStats.CPUUsage.PercpuUsage))
		}
		if onlineCPUs == 0 {
			onlineCPUs = 1
		}
		cpuPercent = (cpuDelta / systemDelta) * onlineCPUs * 100
	}

	writeJSON(w, http.StatusOK, resourceStatsResponse{
		ServerUUID:  serverUUID,
		CPUPercent:  cpuPercent,
		MemoryBytes: int64(stats.MemoryStats.Usage),
		NetworkRx:   networkRx,
		NetworkTx:   networkTx,
		State:       state,
	})
}

func (h *Handlers) ConsoleSocket(w http.ResponseWriter, r *http.Request) {
	serverUUID, err := uuid.Parse(chi.URLParam(r, "uuid"))
	if err != nil {
		http.Error(w, "invalid server uuid", http.StatusBadRequest)
		return
	}
	containerID := docker.ContainerNameFor(serverUUID)
	h.Console.Serve(w, r, serverUUID, containerID)
}

type sendCommandRequest struct {
	Command string `json:"command"`
}

func (h *Handlers) SendCommand(w http.ResponseWriter, r *http.Request) {
	serverUUID, err := uuid.Parse(chi.URLParam(r, "uuid"))
	if err != nil {
		http.Error(w, "invalid server uuid", http.StatusBadRequest)
		return
	}
	var req sendCommandRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	containerID := docker.ContainerNameFor(serverUUID)
	if err := h.Console.SendCommand(r.Context(), serverUUID, containerID, req.Command); err != nil {
		http.Error(w, "failed to send command", http.StatusBadGateway)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

type addDomainRequest struct {
	Domain string `json:"domain"`
	Port   int    `json:"port"`
	Email  string `json:"email"`
}

func (h *Handlers) AddDomain(w http.ResponseWriter, r *http.Request) {
	var req addDomainRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	tlsStatus, err := proxy.AddDomain(req.Domain, req.Port, req.Email)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"domain": req.Domain, "tls_status": tlsStatus})
}

func (h *Handlers) RemoveDomain(w http.ResponseWriter, r *http.Request) {
	domain := chi.URLParam(r, "domain")
	if err := proxy.RemoveDomain(domain); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
