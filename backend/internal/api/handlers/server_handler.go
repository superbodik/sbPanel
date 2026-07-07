package handlers

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/yourorg/panel/internal/activity"
	"github.com/yourorg/panel/internal/auth"
	"github.com/yourorg/panel/internal/daemonclient"
	"github.com/yourorg/panel/internal/models"
)

type ServerHandler struct {
	DB         *pgxpool.Pool
	NodeClient func(nodeID int64) (*daemonclient.Client, error)
	Subusers   *auth.SubuserChecker
}

func (h *ServerHandler) List(w http.ResponseWriter, r *http.Request) {
	claims, ok := auth.FromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if !claims.HasKeyPermission(auth.PermServersRead) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	rows, err := h.DB.Query(r.Context(), `
		SELECT DISTINCT s.id, s.uuid, s.uuid_short, s.name, s.description, s.owner_id,
		       s.node_id, s.egg_id, s.docker_image, s.startup_command,
		       s.memory_mb, s.swap_mb, s.disk_mb, s.io_weight, s.cpu_percent,
		       s.allocation_limit, s.database_limit, s.backup_limit,
		       s.status, s.container_id, s.is_suspended, s.created_at, s.updated_at,
		       n.name,
		       (SELECT HOST(a.ip) || ':' || a.port FROM allocations a
		        WHERE a.server_id = s.id ORDER BY a.id LIMIT 1)
		FROM servers s
		JOIN nodes n ON n.id = s.node_id
		LEFT JOIN server_subusers su ON su.server_id = s.id AND su.user_id = $1
		WHERE s.owner_id = $1 OR $2 = true OR su.user_id IS NOT NULL
		ORDER BY s.created_at DESC`, claims.UserID, claims.IsAdmin)
	if err != nil {
		http.Error(w, "failed to list servers", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	servers := make([]models.Server, 0)
	for rows.Next() {
		var s models.Server
		if err := rows.Scan(
			&s.ID, &s.UUID, &s.UUIDShort, &s.Name, &s.Description, &s.OwnerID,
			&s.NodeID, &s.EggID, &s.DockerImage, &s.StartupCommand,
			&s.MemoryMB, &s.SwapMB, &s.DiskMB, &s.IOWeight, &s.CPUPercent,
			&s.AllocationLimit, &s.DatabaseLimit, &s.BackupLimit,
			&s.Status, &s.ContainerID, &s.IsSuspended, &s.CreatedAt, &s.UpdatedAt,
			&s.NodeName, &s.PrimaryAddress,
		); err != nil {
			http.Error(w, "failed to read servers", http.StatusInternalServerError)
			return
		}
		servers = append(servers, s)
	}

	writeJSON(w, http.StatusOK, servers)
}

type createServerRequest struct {
	Name           string            `json:"name"`
	Description    string            `json:"description"`
	NodeID         int64             `json:"node_id"`
	EggID          int               `json:"egg_id"`
	DockerImage    string            `json:"docker_image"`
	StartupCommand string            `json:"startup_command"`
	Environment    map[string]string `json:"environment"`
	MemoryMB       int64             `json:"memory_mb"`
	SwapMB         int64             `json:"swap_mb"`
	DiskMB         int64             `json:"disk_mb"`
	AllocationID   *int64            `json:"allocation_id"`
}

func (h *ServerHandler) Create(w http.ResponseWriter, r *http.Request) {
	claims, ok := auth.FromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if !claims.HasKeyPermission(auth.PermServersWrite) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	var req createServerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Name == "" || req.NodeID == 0 || req.EggID == 0 || req.DockerImage == "" || req.MemoryMB == 0 || req.DiskMB == 0 {
		http.Error(w, "name, node_id, egg_id, docker_image, memory_mb and disk_mb are required", http.StatusBadRequest)
		return
	}

	if !claims.IsAdmin {
		var serverLimit *int
		if err := h.DB.QueryRow(r.Context(),
			`SELECT server_limit FROM users WHERE id = $1`, claims.UserID,
		).Scan(&serverLimit); err != nil {
			http.Error(w, "failed to check server limit", http.StatusInternalServerError)
			return
		}
		if serverLimit != nil {
			var count int
			if err := h.DB.QueryRow(r.Context(),
				`SELECT count(*) FROM servers WHERE owner_id = $1`, claims.UserID,
			).Scan(&count); err != nil {
				http.Error(w, "failed to check server limit", http.StatusInternalServerError)
				return
			}
			if count >= *serverLimit {
				http.Error(w, "server limit reached for your account", http.StatusForbidden)
				return
			}
		}
	}

	var nodeMemoryMB, nodeDiskMB int64
	var memoryOverallocate, diskOverallocate int
	var isPublic, maintenanceMode bool
	if err := h.DB.QueryRow(r.Context(),
		`SELECT memory_mb, memory_overallocate, disk_mb, disk_overallocate, is_public, maintenance_mode
		 FROM nodes WHERE id = $1`, req.NodeID,
	).Scan(&nodeMemoryMB, &memoryOverallocate, &nodeDiskMB, &diskOverallocate, &isPublic, &maintenanceMode); err != nil {
		http.Error(w, "node not found", http.StatusNotFound)
		return
	}
	if maintenanceMode {
		http.Error(w, "this node is in maintenance mode and cannot accept new servers", http.StatusConflict)
		return
	}
	if !isPublic && !claims.IsAdmin {
		http.Error(w, "node not available", http.StatusForbidden)
		return
	}

	var usedMemoryMB, usedDiskMB int64
	if err := h.DB.QueryRow(r.Context(),
		`SELECT COALESCE(SUM(memory_mb), 0), COALESCE(SUM(disk_mb), 0) FROM servers WHERE node_id = $1`, req.NodeID,
	).Scan(&usedMemoryMB, &usedDiskMB); err != nil {
		http.Error(w, "failed to check node capacity", http.StatusInternalServerError)
		return
	}
	memoryCapacity := effectiveCapacity(nodeMemoryMB, memoryOverallocate)
	diskCapacity := effectiveCapacity(nodeDiskMB, diskOverallocate)
	if usedMemoryMB+req.MemoryMB > memoryCapacity {
		http.Error(w, fmt.Sprintf("node does not have enough memory: %d MB used + %d MB requested exceeds %d MB capacity",
			usedMemoryMB, req.MemoryMB, memoryCapacity), http.StatusConflict)
		return
	}
	if usedDiskMB+req.DiskMB > diskCapacity {
		http.Error(w, fmt.Sprintf("node does not have enough disk: %d MB used + %d MB requested exceeds %d MB capacity",
			usedDiskMB, req.DiskMB, diskCapacity), http.StatusConflict)
		return
	}

	client, err := h.NodeClient(req.NodeID)
	if err != nil {
		log.Printf("servers.create: node %d client unavailable: %v", req.NodeID, err)
		http.Error(w, "node unavailable: "+err.Error(), http.StatusBadGateway)
		return
	}

	environment := req.Environment
	if environment == nil {
		environment = map[string]string{}
	}
	environmentJSON, err := json.Marshal(environment)
	if err != nil {
		http.Error(w, "invalid environment", http.StatusBadRequest)
		return
	}

	serverUUID := uuid.New()
	uuidShort := serverUUID.String()[:8]

	ctx := r.Context()
	tx, err := h.DB.Begin(ctx)
	if err != nil {
		http.Error(w, "failed to start transaction", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback(ctx)

	var serverID int64
	err = tx.QueryRow(ctx, `
		INSERT INTO servers (uuid, uuid_short, name, description, owner_id, node_id, egg_id,
		                      docker_image, startup_command, environment, memory_mb, swap_mb, disk_mb, status)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, 'installing')
		RETURNING id`,
		serverUUID, uuidShort, req.Name, req.Description, claims.UserID, req.NodeID, req.EggID,
		req.DockerImage, req.StartupCommand, environmentJSON, req.MemoryMB, req.SwapMB, req.DiskMB,
	).Scan(&serverID)
	if err != nil {
		http.Error(w, "failed to create server", http.StatusInternalServerError)
		return
	}

	portBindings := map[string]string{}
	if req.AllocationID != nil {
		var allocPort int
		err = tx.QueryRow(ctx, `
			UPDATE allocations SET server_id = $1
			WHERE id = $2 AND node_id = $3 AND server_id IS NULL
			RETURNING port`,
			serverID, *req.AllocationID, req.NodeID,
		).Scan(&allocPort)
		if err != nil {
			http.Error(w, "allocation is unavailable", http.StatusConflict)
			return
		}
		portBindings[fmt.Sprintf("%d/tcp", allocPort)] = fmt.Sprintf("%d", allocPort)
	}

	daemonResp, err := client.CreateServer(ctx, daemonclient.CreateServerRequest{
		ServerUUID:     serverUUID,
		DockerImage:    req.DockerImage,
		StartupCommand: req.StartupCommand,
		Environment:    environment,
		MemoryMB:       req.MemoryMB,
		SwapMB:         req.SwapMB,
		DiskMB:         req.DiskMB,
		IOWeight:       500,
		PortBindings:   portBindings,
	})
	if err != nil || !daemonResp.Success {
		if err != nil {
			log.Printf("servers.create: daemon call failed for node %d: %v", req.NodeID, err)
			http.Error(w, "daemon call failed: "+err.Error(), http.StatusBadGateway)
		} else {
			log.Printf("servers.create: daemon rejected create for node %d: %s", req.NodeID, daemonResp.Message)
			http.Error(w, "daemon rejected server creation: "+daemonResp.Message, http.StatusBadGateway)
		}
		return
	}

	if _, err := tx.Exec(ctx, `UPDATE servers SET status = 'offline' WHERE id = $1`, serverID); err != nil {
		http.Error(w, "failed to finalize server", http.StatusInternalServerError)
		return
	}

	if err := tx.Commit(ctx); err != nil {
		http.Error(w, "failed to commit server creation", http.StatusInternalServerError)
		return
	}

	activity.Record(ctx, h.DB, activity.Entry{
		ActorUserID: &claims.UserID,
		ServerID:    &serverID,
		NodeID:      &req.NodeID,
		Event:       "server.create",
		IPAddress:   activity.RequestIP(r),
		Metadata:    map[string]any{"name": req.Name},
	})

	writeJSON(w, http.StatusCreated, map[string]any{"id": serverID, "uuid": serverUUID})
}

func (h *ServerHandler) Get(w http.ResponseWriter, r *http.Request) {
	claims, ok := auth.FromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if !claims.HasKeyPermission(auth.PermServersRead) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	id, err := uuid.Parse(chi.URLParam(r, "uuid"))
	if err != nil {
		http.Error(w, "invalid server uuid", http.StatusBadRequest)
		return
	}

	var s models.Server
	err = h.DB.QueryRow(r.Context(), `
		SELECT s.id, s.uuid, s.uuid_short, s.name, s.description, s.owner_id, s.node_id,
		       s.egg_id, s.docker_image, s.startup_command, s.memory_mb, s.swap_mb,
		       s.disk_mb, s.io_weight, s.cpu_percent, s.allocation_limit,
		       s.database_limit, s.backup_limit, s.status, s.container_id,
		       s.is_suspended, s.created_at, s.updated_at, n.name,
		       (SELECT HOST(a.ip) || ':' || a.port FROM allocations a
		        WHERE a.server_id = s.id ORDER BY a.id LIMIT 1)
		FROM servers s JOIN nodes n ON n.id = s.node_id
		WHERE s.uuid = $1`, id,
	).Scan(
		&s.ID, &s.UUID, &s.UUIDShort, &s.Name, &s.Description, &s.OwnerID,
		&s.NodeID, &s.EggID, &s.DockerImage, &s.StartupCommand,
		&s.MemoryMB, &s.SwapMB, &s.DiskMB, &s.IOWeight, &s.CPUPercent,
		&s.AllocationLimit, &s.DatabaseLimit, &s.BackupLimit,
		&s.Status, &s.ContainerID, &s.IsSuspended, &s.CreatedAt, &s.UpdatedAt,
		&s.NodeName, &s.PrimaryAddress,
	)
	if err == pgx.ErrNoRows {
		http.Error(w, "server not found", http.StatusNotFound)
		return
	} else if err != nil {
		http.Error(w, "failed to load server", http.StatusInternalServerError)
		return
	}

	if !claims.IsAdmin && claims.UserID != s.OwnerID {
		isSub, _ := h.Subusers.IsSubuser(r.Context(), claims.UserID, s.ID)
		if !isSub {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
	}

	writeJSON(w, http.StatusOK, s)
}

func (h *ServerHandler) Delete(w http.ResponseWriter, r *http.Request) {
	claims, ok := auth.FromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if !claims.HasKeyPermission(auth.PermServersWrite) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	id, err := uuid.Parse(chi.URLParam(r, "uuid"))
	if err != nil {
		http.Error(w, "invalid server uuid", http.StatusBadRequest)
		return
	}

	var serverID, nodeID, ownerID int64
	if err := h.DB.QueryRow(r.Context(),
		`SELECT id, node_id, owner_id FROM servers WHERE uuid = $1`, id,
	).Scan(&serverID, &nodeID, &ownerID); err != nil {
		http.Error(w, "server not found", http.StatusNotFound)
		return
	}
	if !claims.IsAdmin && claims.UserID != ownerID {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	force := r.URL.Query().Get("force") == "true"

	client, clientErr := h.NodeClient(nodeID)
	if clientErr != nil {
		if !force {
			http.Error(w, "node unavailable (add ?force=true to delete the record anyway)", http.StatusBadGateway)
			return
		}
	} else if delErr := client.DeleteServer(r.Context(), id); delErr != nil && !force {
		http.Error(w, "daemon failed to delete server (add ?force=true to delete the record anyway)", http.StatusBadGateway)
		return
	}

	if _, err := h.DB.Exec(r.Context(), `DELETE FROM servers WHERE id = $1`, serverID); err != nil {
		http.Error(w, "failed to delete server record", http.StatusInternalServerError)
		return
	}

	activity.Record(r.Context(), h.DB, activity.Entry{
		ActorUserID: &claims.UserID,
		ServerID:    &serverID,
		NodeID:      &nodeID,
		Event:       "server.delete",
		IPAddress:   activity.RequestIP(r),
	})

	w.WriteHeader(http.StatusNoContent)
}

type powerRequest struct {
	Action daemonclient.PowerAction `json:"action"`
}

var powerPermissions = map[daemonclient.PowerAction]string{
	daemonclient.PowerStart:   auth.PermControlStart,
	daemonclient.PowerStop:    auth.PermControlStop,
	daemonclient.PowerRestart: auth.PermControlRestart,
	daemonclient.PowerKill:    auth.PermControlKill,
}

func (h *ServerHandler) Power(w http.ResponseWriter, r *http.Request) {
	claims, ok := auth.FromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	id, err := uuid.Parse(chi.URLParam(r, "uuid"))
	if err != nil {
		http.Error(w, "invalid server uuid", http.StatusBadRequest)
		return
	}

	var req powerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	permission, ok := powerPermissions[req.Action]
	if !ok {
		http.Error(w, "unknown action", http.StatusBadRequest)
		return
	}

	var serverID, nodeID, ownerID int64
	var isSuspended bool
	if err := h.DB.QueryRow(r.Context(),
		`SELECT id, node_id, owner_id, is_suspended FROM servers WHERE uuid = $1`, id,
	).Scan(&serverID, &nodeID, &ownerID, &isSuspended); err != nil {
		http.Error(w, "server not found", http.StatusNotFound)
		return
	}
	if !claims.HasKeyPermission(permission) || !h.Subusers.CanAccessServer(r.Context(), claims, ownerID, serverID, permission) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if isSuspended && (req.Action == daemonclient.PowerStart || req.Action == daemonclient.PowerRestart) {
		http.Error(w, "this server is suspended and cannot be started", http.StatusForbidden)
		return
	}

	client, err := h.NodeClient(nodeID)
	if err != nil {
		http.Error(w, "node unavailable: "+err.Error(), http.StatusBadGateway)
		return
	}

	resp, err := client.Power(r.Context(), id, req.Action)
	if err != nil {
		http.Error(w, "daemon rejected power action: "+err.Error(), http.StatusBadGateway)
		return
	}

	activity.Record(r.Context(), h.DB, activity.Entry{
		ActorUserID: &claims.UserID,
		ServerID:    &serverID,
		NodeID:      &nodeID,
		Event:       "server.power." + string(req.Action),
		IPAddress:   activity.RequestIP(r),
	})

	writeJSON(w, http.StatusOK, resp)
}

func (h *ServerHandler) Suspend(w http.ResponseWriter, r *http.Request) {
	h.setSuspended(w, r, true, "server.suspend")
}

func (h *ServerHandler) Unsuspend(w http.ResponseWriter, r *http.Request) {
	h.setSuspended(w, r, false, "server.unsuspend")
}

func (h *ServerHandler) setSuspended(w http.ResponseWriter, r *http.Request, suspended bool, event string) {
	claims, ok := auth.FromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	id, err := uuid.Parse(chi.URLParam(r, "uuid"))
	if err != nil {
		http.Error(w, "invalid server uuid", http.StatusBadRequest)
		return
	}

	newStatus := models.StatusOffline
	if suspended {
		newStatus = models.StatusSuspended
	}

	var serverID, nodeID int64
	if err := h.DB.QueryRow(r.Context(),
		`UPDATE servers SET is_suspended = $1, status = $2 WHERE uuid = $3 RETURNING id, node_id`,
		suspended, newStatus, id,
	).Scan(&serverID, &nodeID); err != nil {
		http.Error(w, "server not found", http.StatusNotFound)
		return
	}

	if suspended {
		if client, err := h.NodeClient(nodeID); err == nil {
			if _, err := client.Power(r.Context(), id, daemonclient.PowerStop); err != nil {
				log.Printf("servers.suspend: failed to stop server %s on suspend: %v", id, err)
			}
		}
	}

	activity.Record(r.Context(), h.DB, activity.Entry{
		ActorUserID: &claims.UserID,
		ServerID:    &serverID,
		NodeID:      &nodeID,
		Event:       event,
		IPAddress:   activity.RequestIP(r),
	})

	w.WriteHeader(http.StatusNoContent)
}

func effectiveCapacity(totalMB int64, overallocatePercent int) int64 {
	return totalMB * int64(100+overallocatePercent) / 100
}
