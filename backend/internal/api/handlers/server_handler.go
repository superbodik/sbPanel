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
}

func (h *ServerHandler) List(w http.ResponseWriter, r *http.Request) {
	claims, ok := auth.FromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	rows, err := h.DB.Query(r.Context(), `
		SELECT s.id, s.uuid, s.uuid_short, s.name, s.description, s.owner_id,
		       s.node_id, s.egg_id, s.docker_image, s.startup_command,
		       s.memory_mb, s.swap_mb, s.disk_mb, s.io_weight, s.cpu_percent,
		       s.allocation_limit, s.database_limit, s.backup_limit,
		       s.status, s.container_id, s.is_suspended, s.created_at, s.updated_at
		FROM servers s
		WHERE s.owner_id = $1 OR $2 = true
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

	var req createServerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Name == "" || req.NodeID == 0 || req.EggID == 0 || req.DockerImage == "" || req.MemoryMB == 0 || req.DiskMB == 0 {
		http.Error(w, "name, node_id, egg_id, docker_image, memory_mb and disk_mb are required", http.StatusBadRequest)
		return
	}

	client, err := h.NodeClient(req.NodeID)
	if err != nil {
		log.Printf("servers.create: node %d client unavailable: %v", req.NodeID, err)
		http.Error(w, "node unavailable", http.StatusBadGateway)
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
		} else {
			log.Printf("servers.create: daemon rejected create for node %d: %s", req.NodeID, daemonResp.Message)
		}
		http.Error(w, "daemon failed to create server", http.StatusBadGateway)
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
	id, err := uuid.Parse(chi.URLParam(r, "uuid"))
	if err != nil {
		http.Error(w, "invalid server uuid", http.StatusBadRequest)
		return
	}

	var s models.Server
	err = h.DB.QueryRow(r.Context(), `
		SELECT id, uuid, uuid_short, name, description, owner_id, node_id,
		       egg_id, docker_image, startup_command, memory_mb, swap_mb,
		       disk_mb, io_weight, cpu_percent, allocation_limit,
		       database_limit, backup_limit, status, container_id,
		       is_suspended, created_at, updated_at
		FROM servers WHERE uuid = $1`, id,
	).Scan(
		&s.ID, &s.UUID, &s.UUIDShort, &s.Name, &s.Description, &s.OwnerID,
		&s.NodeID, &s.EggID, &s.DockerImage, &s.StartupCommand,
		&s.MemoryMB, &s.SwapMB, &s.DiskMB, &s.IOWeight, &s.CPUPercent,
		&s.AllocationLimit, &s.DatabaseLimit, &s.BackupLimit,
		&s.Status, &s.ContainerID, &s.IsSuspended, &s.CreatedAt, &s.UpdatedAt,
	)
	if err == pgx.ErrNoRows {
		http.Error(w, "server not found", http.StatusNotFound)
		return
	} else if err != nil {
		http.Error(w, "failed to load server", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, s)
}

func (h *ServerHandler) Delete(w http.ResponseWriter, r *http.Request) {
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

func (h *ServerHandler) Power(w http.ResponseWriter, r *http.Request) {
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

	var serverID, nodeID int64
	if err := h.DB.QueryRow(r.Context(),
		`SELECT id, node_id FROM servers WHERE uuid = $1`, id,
	).Scan(&serverID, &nodeID); err != nil {
		http.Error(w, "server not found", http.StatusNotFound)
		return
	}

	client, err := h.NodeClient(nodeID)
	if err != nil {
		http.Error(w, "node unavailable", http.StatusBadGateway)
		return
	}

	resp, err := client.Power(r.Context(), id, req.Action)
	if err != nil {
		http.Error(w, "daemon rejected power action", http.StatusBadGateway)
		return
	}

	if claims, ok := auth.FromContext(r.Context()); ok {
		activity.Record(r.Context(), h.DB, activity.Entry{
			ActorUserID: &claims.UserID,
			ServerID:    &serverID,
			NodeID:      &nodeID,
			Event:       "server.power." + string(req.Action),
			IPAddress:   activity.RequestIP(r),
		})
	}

	writeJSON(w, http.StatusOK, resp)
}
