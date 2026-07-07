package handlers

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/yourorg/panel/internal/activity"
	"github.com/yourorg/panel/internal/auth"
	"github.com/yourorg/panel/internal/crypto"
	"github.com/yourorg/panel/internal/daemonclient"
)

type NodeHandler struct {
	DB            *pgxpool.Pool
	EncryptionKey string
	NodeClient    func(nodeID int64) (*daemonclient.Client, error)
}

type createNodeRequest struct {
	Name               string `json:"name"`
	LocationID         int    `json:"location_id"`
	FQDN               string `json:"fqdn"`
	Scheme             string `json:"scheme"`
	DaemonPort         int    `json:"daemon_port"`
	MemoryMB           int64  `json:"memory_mb"`
	MemoryOverallocate int    `json:"memory_overallocate"`
	DiskMB             int64  `json:"disk_mb"`
	DiskOverallocate   int    `json:"disk_overallocate"`
	IsPublic           *bool  `json:"is_public"`
}

type createNodeResponse struct {
	ID          int64  `json:"id"`
	DaemonToken string `json:"daemon_token"`
}

func (h *NodeHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req createNodeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Scheme == "" {
		req.Scheme = "http"
	}
	if req.DaemonPort == 0 {
		req.DaemonPort = 8443
	}
	isPublic := true
	if req.IsPublic != nil {
		isPublic = *req.IsPublic
	}

	rawToken, err := generateToken(32)
	if err != nil {
		http.Error(w, "failed to generate token", http.StatusInternalServerError)
		return
	}
	tokenHash, err := auth.HashPassword(rawToken)
	if err != nil {
		http.Error(w, "failed to hash token", http.StatusInternalServerError)
		return
	}
	tokenEncrypted, err := crypto.Encrypt(h.EncryptionKey, rawToken)
	if err != nil {
		http.Error(w, "failed to encrypt token", http.StatusInternalServerError)
		return
	}

	var id int64
	err = h.DB.QueryRow(r.Context(), `
		INSERT INTO nodes (name, location_id, fqdn, scheme, daemon_port,
		                    daemon_token_hash, daemon_token_encrypted, memory_mb, memory_overallocate,
		                    disk_mb, disk_overallocate, is_public)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		RETURNING id`,
		req.Name, req.LocationID, req.FQDN, req.Scheme, req.DaemonPort,
		tokenHash, tokenEncrypted, req.MemoryMB, req.MemoryOverallocate,
		req.DiskMB, req.DiskOverallocate, isPublic,
	).Scan(&id)
	if err != nil {
		http.Error(w, "failed to create node", http.StatusInternalServerError)
		return
	}

	if claims, ok := auth.FromContext(r.Context()); ok {
		activity.Record(r.Context(), h.DB, activity.Entry{
			ActorUserID: &claims.UserID,
			NodeID:      &id,
			Event:       "node.create",
			IPAddress:   activity.RequestIP(r),
			Metadata:    map[string]any{"name": req.Name, "fqdn": req.FQDN},
		})
	}

	writeJSON(w, http.StatusCreated, createNodeResponse{ID: id, DaemonToken: rawToken})
}

func (h *NodeHandler) List(w http.ResponseWriter, r *http.Request) {
	claims, ok := auth.FromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	query := `
		SELECT id, name, fqdn, scheme, daemon_port, memory_mb, memory_overallocate,
		       disk_mb, disk_overallocate, is_public, maintenance_mode, last_seen_at
		FROM nodes`
	if !claims.IsAdmin {
		query += ` WHERE is_public = true`
	}
	query += ` ORDER BY name`

	rows, err := h.DB.Query(r.Context(), query)
	if err != nil {
		http.Error(w, "failed to list nodes", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type nodeSummary struct {
		ID                 int64   `json:"id"`
		Name               string  `json:"name"`
		FQDN               string  `json:"fqdn"`
		Scheme             string  `json:"scheme"`
		DaemonPort         int     `json:"daemon_port"`
		MemoryMB           int64   `json:"memory_mb"`
		MemoryOverallocate int     `json:"memory_overallocate"`
		DiskMB             int64   `json:"disk_mb"`
		DiskOverallocate   int     `json:"disk_overallocate"`
		IsPublic           bool    `json:"is_public"`
		MaintenanceMode    bool    `json:"maintenance_mode"`
		LastSeenAt         *string `json:"last_seen_at"`
	}

	nodes := make([]nodeSummary, 0)
	for rows.Next() {
		var n nodeSummary
		if err := rows.Scan(&n.ID, &n.Name, &n.FQDN, &n.Scheme, &n.DaemonPort,
			&n.MemoryMB, &n.MemoryOverallocate, &n.DiskMB, &n.DiskOverallocate,
			&n.IsPublic, &n.MaintenanceMode, &n.LastSeenAt); err != nil {
			http.Error(w, "failed to read nodes", http.StatusInternalServerError)
			return
		}
		nodes = append(nodes, n)
	}

	writeJSON(w, http.StatusOK, nodes)
}

type updateNodeRequest struct {
	Name               string `json:"name"`
	FQDN               string `json:"fqdn"`
	Scheme             string `json:"scheme"`
	DaemonPort         int    `json:"daemon_port"`
	MemoryMB           int64  `json:"memory_mb"`
	MemoryOverallocate int    `json:"memory_overallocate"`
	DiskMB             int64  `json:"disk_mb"`
	DiskOverallocate   int    `json:"disk_overallocate"`
	IsPublic           bool   `json:"is_public"`
	MaintenanceMode    bool   `json:"maintenance_mode"`
}

func (h *NodeHandler) Update(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid node id", http.StatusBadRequest)
		return
	}

	var req updateNodeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Name == "" || req.FQDN == "" || req.Scheme == "" || req.DaemonPort == 0 {
		http.Error(w, "name, fqdn, scheme and daemon_port are required", http.StatusBadRequest)
		return
	}

	tag, err := h.DB.Exec(r.Context(), `
		UPDATE nodes SET name = $1, fqdn = $2, scheme = $3, daemon_port = $4,
		                 memory_mb = $5, memory_overallocate = $6, disk_mb = $7,
		                 disk_overallocate = $8, is_public = $9, maintenance_mode = $10
		WHERE id = $11`,
		req.Name, req.FQDN, req.Scheme, req.DaemonPort, req.MemoryMB, req.MemoryOverallocate,
		req.DiskMB, req.DiskOverallocate, req.IsPublic, req.MaintenanceMode, id,
	)
	if err != nil {
		http.Error(w, "failed to update node", http.StatusInternalServerError)
		return
	}
	if tag.RowsAffected() == 0 {
		http.Error(w, "node not found", http.StatusNotFound)
		return
	}

	if claims, ok := auth.FromContext(r.Context()); ok {
		activity.Record(r.Context(), h.DB, activity.Entry{
			ActorUserID: &claims.UserID,
			NodeID:      &id,
			Event:       "node.update",
			IPAddress:   activity.RequestIP(r),
		})
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *NodeHandler) RegenerateToken(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid node id", http.StatusBadRequest)
		return
	}

	rawToken, err := generateToken(32)
	if err != nil {
		http.Error(w, "failed to generate token", http.StatusInternalServerError)
		return
	}
	tokenHash, err := auth.HashPassword(rawToken)
	if err != nil {
		http.Error(w, "failed to hash token", http.StatusInternalServerError)
		return
	}
	tokenEncrypted, err := crypto.Encrypt(h.EncryptionKey, rawToken)
	if err != nil {
		http.Error(w, "failed to encrypt token", http.StatusInternalServerError)
		return
	}

	tag, err := h.DB.Exec(r.Context(),
		`UPDATE nodes SET daemon_token_hash = $1, daemon_token_encrypted = $2 WHERE id = $3`,
		tokenHash, tokenEncrypted, id,
	)
	if err != nil {
		http.Error(w, "failed to update token", http.StatusInternalServerError)
		return
	}
	if tag.RowsAffected() == 0 {
		http.Error(w, "node not found", http.StatusNotFound)
		return
	}

	if claims, ok := auth.FromContext(r.Context()); ok {
		activity.Record(r.Context(), h.DB, activity.Entry{
			ActorUserID: &claims.UserID,
			NodeID:      &id,
			Event:       "node.regenerate_token",
			IPAddress:   activity.RequestIP(r),
		})
	}

	writeJSON(w, http.StatusOK, createNodeResponse{ID: id, DaemonToken: rawToken})
}

func (h *NodeHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid node id", http.StatusBadRequest)
		return
	}

	var serverCount int
	if err := h.DB.QueryRow(r.Context(),
		`SELECT count(*) FROM servers WHERE node_id = $1`, id,
	).Scan(&serverCount); err != nil {
		http.Error(w, "failed to check node's servers", http.StatusInternalServerError)
		return
	}
	if serverCount > 0 {
		http.Error(w, "this node still has servers on it — delete or move them first", http.StatusConflict)
		return
	}

	res, err := h.DB.Exec(r.Context(), `DELETE FROM nodes WHERE id = $1`, id)
	if err != nil {
		http.Error(w, "failed to delete node", http.StatusInternalServerError)
		return
	}
	if res.RowsAffected() == 0 {
		http.Error(w, "node not found", http.StatusNotFound)
		return
	}

	if claims, ok := auth.FromContext(r.Context()); ok {
		activity.Record(r.Context(), h.DB, activity.Entry{
			ActorUserID: &claims.UserID,
			NodeID:      &id,
			Event:       "node.delete",
			IPAddress:   activity.RequestIP(r),
		})
	}

	w.WriteHeader(http.StatusNoContent)
}

type nodeStatusResponse struct {
	Online bool   `json:"online"`
	Error  string `json:"error,omitempty"`
}

func (h *NodeHandler) Status(w http.ResponseWriter, r *http.Request) {
	nodeID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid node id", http.StatusBadRequest)
		return
	}

	client, err := h.NodeClient(nodeID)
	if err != nil {
		writeJSON(w, http.StatusOK, nodeStatusResponse{Online: false, Error: err.Error()})
		return
	}

	if err := client.Ping(r.Context()); err != nil {
		writeJSON(w, http.StatusOK, nodeStatusResponse{Online: false, Error: err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, nodeStatusResponse{Online: true})
}

func generateToken(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
