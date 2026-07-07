package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/yourorg/panel/internal/auth"
	"github.com/yourorg/panel/internal/daemonclient"
)

type ServerBackupHandler struct {
	DB         *pgxpool.Pool
	Subusers   *auth.SubuserChecker
	NodeClient func(nodeID int64) (*daemonclient.Client, error)
}

func (h *ServerBackupHandler) resolveServer(w http.ResponseWriter, r *http.Request, permission string) (serverID, nodeID int64, serverUUID uuid.UUID, backupLimit int, ok bool) {
	claims, authOK := auth.FromContext(r.Context())
	if !authOK {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return 0, 0, uuid.UUID{}, 0, false
	}

	rawUUID := chi.URLParam(r, "uuid")
	parsedUUID, err := uuid.Parse(rawUUID)
	if err != nil {
		http.Error(w, "invalid server uuid", http.StatusBadRequest)
		return 0, 0, uuid.UUID{}, 0, false
	}

	var ownerID int64
	if err := h.DB.QueryRow(r.Context(),
		`SELECT id, owner_id, node_id, backup_limit FROM servers WHERE uuid = $1`, rawUUID,
	).Scan(&serverID, &ownerID, &nodeID, &backupLimit); err != nil {
		http.Error(w, "server not found", http.StatusNotFound)
		return 0, 0, uuid.UUID{}, 0, false
	}
	if !claims.HasKeyPermission(permission) || !h.Subusers.CanAccessServer(r.Context(), claims, ownerID, serverID, permission) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return 0, 0, uuid.UUID{}, 0, false
	}

	return serverID, nodeID, parsedUUID, backupLimit, true
}

type serverBackupSummary struct {
	ID           int64      `json:"id"`
	UUID         string     `json:"uuid"`
	Name         string     `json:"name"`
	IgnoredFiles []string   `json:"ignored_files"`
	Bytes        int64      `json:"bytes"`
	Checksum     string     `json:"checksum,omitempty"`
	IsSuccessful bool       `json:"is_successful"`
	CompletedAt  *time.Time `json:"completed_at"`
	CreatedAt    time.Time  `json:"created_at"`
}

func (h *ServerBackupHandler) List(w http.ResponseWriter, r *http.Request) {
	serverID, _, _, _, ok := h.resolveServer(w, r, auth.PermBackupsRead)
	if !ok {
		return
	}

	rows, err := h.DB.Query(r.Context(), `
		SELECT id, uuid, name, ignored_files, bytes, checksum, is_successful, completed_at, created_at
		FROM server_backups WHERE server_id = $1 ORDER BY created_at DESC`, serverID)
	if err != nil {
		http.Error(w, "failed to list backups", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	backups := make([]serverBackupSummary, 0)
	for rows.Next() {
		var b serverBackupSummary
		var backupUUID uuid.UUID
		var ignoredRaw []byte
		var checksum *string
		if err := rows.Scan(&b.ID, &backupUUID, &b.Name, &ignoredRaw, &b.Bytes, &checksum, &b.IsSuccessful, &b.CompletedAt, &b.CreatedAt); err != nil {
			http.Error(w, "failed to read backups", http.StatusInternalServerError)
			return
		}
		b.UUID = backupUUID.String()
		b.IgnoredFiles = []string{}
		_ = json.Unmarshal(ignoredRaw, &b.IgnoredFiles)
		if checksum != nil {
			b.Checksum = *checksum
		}
		backups = append(backups, b)
	}

	writeJSON(w, http.StatusOK, backups)
}

type createServerBackupRequest struct {
	Name         string   `json:"name"`
	IgnoredFiles []string `json:"ignored_files"`
}

func (h *ServerBackupHandler) Create(w http.ResponseWriter, r *http.Request) {
	serverID, nodeID, serverUUID, backupLimit, ok := h.resolveServer(w, r, auth.PermBackupsWrite)
	if !ok {
		return
	}

	var req createServerBackupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}
	if req.IgnoredFiles == nil {
		req.IgnoredFiles = []string{}
	}

	var count int
	if err := h.DB.QueryRow(r.Context(),
		`SELECT count(*) FROM server_backups WHERE server_id = $1`, serverID,
	).Scan(&count); err != nil {
		http.Error(w, "failed to check backup limit", http.StatusInternalServerError)
		return
	}
	if count >= backupLimit {
		http.Error(w, "backup limit reached for this server", http.StatusConflict)
		return
	}

	ignoredJSON, err := json.Marshal(req.IgnoredFiles)
	if err != nil {
		http.Error(w, "invalid ignored_files", http.StatusBadRequest)
		return
	}

	backupUUID := uuid.New()
	var id int64
	var createdAt time.Time
	if err := h.DB.QueryRow(r.Context(), `
		INSERT INTO server_backups (uuid, server_id, name, ignored_files)
		VALUES ($1, $2, $3, $4)
		RETURNING id, created_at`,
		backupUUID, serverID, req.Name, ignoredJSON,
	).Scan(&id, &createdAt); err != nil {
		http.Error(w, "failed to create backup record", http.StatusInternalServerError)
		return
	}

	client, err := h.NodeClient(nodeID)
	if err != nil {
		http.Error(w, "node unavailable: "+err.Error(), http.StatusBadGateway)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 140*time.Second)
	defer cancel()
	resp, err := client.CreateBackup(ctx, serverUUID, daemonclient.CreateBackupRequest{
		BackupUUID: backupUUID.String(), IgnoredFiles: req.IgnoredFiles,
	})
	if err != nil {
		http.Error(w, "backup failed on the node: "+err.Error(), http.StatusBadGateway)
		return
	}

	completedAt := time.Now()
	if _, err := h.DB.Exec(r.Context(),
		`UPDATE server_backups SET bytes = $1, checksum = $2, is_successful = true, completed_at = $3 WHERE id = $4`,
		resp.Bytes, resp.Checksum, completedAt, id,
	); err != nil {
		http.Error(w, "backup completed but failed to record it", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusCreated, serverBackupSummary{
		ID: id, UUID: backupUUID.String(), Name: req.Name, IgnoredFiles: req.IgnoredFiles,
		Bytes: resp.Bytes, Checksum: resp.Checksum, IsSuccessful: true,
		CompletedAt: &completedAt, CreatedAt: createdAt,
	})
}

func (h *ServerBackupHandler) backupUUIDFor(r *http.Request, serverID int64) (uuid.UUID, error) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		return uuid.UUID{}, fmt.Errorf("invalid backup id")
	}
	var backupUUID uuid.UUID
	if err := h.DB.QueryRow(r.Context(),
		`SELECT uuid FROM server_backups WHERE id = $1 AND server_id = $2`, id, serverID,
	).Scan(&backupUUID); err != nil {
		return uuid.UUID{}, fmt.Errorf("backup not found")
	}
	return backupUUID, nil
}

func (h *ServerBackupHandler) Restore(w http.ResponseWriter, r *http.Request) {
	serverID, nodeID, serverUUID, _, ok := h.resolveServer(w, r, auth.PermBackupsWrite)
	if !ok {
		return
	}

	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid backup id", http.StatusBadRequest)
		return
	}
	var backupUUID uuid.UUID
	var isSuccessful bool
	if err := h.DB.QueryRow(r.Context(),
		`SELECT uuid, is_successful FROM server_backups WHERE id = $1 AND server_id = $2`, id, serverID,
	).Scan(&backupUUID, &isSuccessful); err != nil {
		http.Error(w, "backup not found", http.StatusNotFound)
		return
	}
	if !isSuccessful {
		http.Error(w, "this backup did not complete successfully and cannot be restored", http.StatusConflict)
		return
	}

	client, err := h.NodeClient(nodeID)
	if err != nil {
		http.Error(w, "node unavailable: "+err.Error(), http.StatusBadGateway)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 140*time.Second)
	defer cancel()
	if err := client.RestoreBackup(ctx, serverUUID, backupUUID.String()); err != nil {
		http.Error(w, "restore failed: "+err.Error(), http.StatusBadGateway)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *ServerBackupHandler) Delete(w http.ResponseWriter, r *http.Request) {
	serverID, nodeID, serverUUID, _, ok := h.resolveServer(w, r, auth.PermBackupsWrite)
	if !ok {
		return
	}

	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid backup id", http.StatusBadRequest)
		return
	}
	backupUUID, err := h.backupUUIDFor(r, serverID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	client, err := h.NodeClient(nodeID)
	if err != nil {
		http.Error(w, "node unavailable: "+err.Error(), http.StatusBadGateway)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	if err := client.DeleteBackup(ctx, serverUUID, backupUUID.String()); err != nil {
		http.Error(w, "failed to delete backup on the node: "+err.Error(), http.StatusBadGateway)
		return
	}

	if _, err := h.DB.Exec(r.Context(), `DELETE FROM server_backups WHERE id = $1`, id); err != nil {
		http.Error(w, "failed to delete backup record", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *ServerBackupHandler) Download(w http.ResponseWriter, r *http.Request) {
	serverID, nodeID, serverUUID, _, ok := h.resolveServer(w, r, auth.PermBackupsRead)
	if !ok {
		return
	}

	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid backup id", http.StatusBadRequest)
		return
	}
	var backupUUID uuid.UUID
	var name string
	if err := h.DB.QueryRow(r.Context(),
		`SELECT uuid, name FROM server_backups WHERE id = $1 AND server_id = $2`, id, serverID,
	).Scan(&backupUUID, &name); err != nil {
		http.Error(w, "backup not found", http.StatusNotFound)
		return
	}

	client, err := h.NodeClient(nodeID)
	if err != nil {
		http.Error(w, "node unavailable: "+err.Error(), http.StatusBadGateway)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 140*time.Second)
	defer cancel()
	body, err := client.DownloadBackup(ctx, serverUUID, backupUUID.String())
	if err != nil {
		http.Error(w, "download failed: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer body.Close()

	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.tar.gz"`, name))
	io.Copy(w, body)
}
