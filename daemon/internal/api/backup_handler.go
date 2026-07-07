package api

import (
	"encoding/json"
	"net/http"
	"os"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/yourorg/panel-daemon/internal/backup"
)

type createBackupRequest struct {
	BackupUUID   string   `json:"backup_uuid"`
	IgnoredFiles []string `json:"ignored_files"`
}

type createBackupResponse struct {
	Bytes    int64  `json:"bytes"`
	Checksum string `json:"checksum"`
}

func (h *Handlers) CreateBackup(w http.ResponseWriter, r *http.Request) {
	serverUUID, err := uuid.Parse(chi.URLParam(r, "uuid"))
	if err != nil {
		http.Error(w, "invalid server uuid", http.StatusBadRequest)
		return
	}
	var req createBackupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.BackupUUID == "" {
		http.Error(w, "backup_uuid is required", http.StatusBadRequest)
		return
	}
	if _, err := uuid.Parse(req.BackupUUID); err != nil {
		http.Error(w, "invalid backup_uuid", http.StatusBadRequest)
		return
	}

	bytesWritten, checksum, err := backup.Create(
		h.Docker.ServerVolumePath(serverUUID), h.BackupDir, serverUUID.String(), req.BackupUUID, req.IgnoredFiles)
	if err != nil {
		http.Error(w, "backup failed: "+err.Error(), http.StatusBadGateway)
		return
	}

	writeJSON(w, http.StatusOK, createBackupResponse{Bytes: bytesWritten, Checksum: checksum})
}

func (h *Handlers) RestoreBackup(w http.ResponseWriter, r *http.Request) {
	serverUUID, err := uuid.Parse(chi.URLParam(r, "uuid"))
	if err != nil {
		http.Error(w, "invalid server uuid", http.StatusBadRequest)
		return
	}
	backupUUID := chi.URLParam(r, "backup_uuid")
	if _, err := uuid.Parse(backupUUID); err != nil {
		http.Error(w, "invalid backup uuid", http.StatusBadRequest)
		return
	}

	if err := backup.Restore(h.Docker.ServerVolumePath(serverUUID), h.BackupDir, serverUUID.String(), backupUUID); err != nil {
		http.Error(w, "restore failed: "+err.Error(), http.StatusBadGateway)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handlers) DeleteBackup(w http.ResponseWriter, r *http.Request) {
	serverUUID, err := uuid.Parse(chi.URLParam(r, "uuid"))
	if err != nil {
		http.Error(w, "invalid server uuid", http.StatusBadRequest)
		return
	}
	backupUUID := chi.URLParam(r, "backup_uuid")
	if _, err := uuid.Parse(backupUUID); err != nil {
		http.Error(w, "invalid backup uuid", http.StatusBadRequest)
		return
	}

	if err := backup.Delete(h.BackupDir, serverUUID.String(), backupUUID); err != nil && !os.IsNotExist(err) {
		http.Error(w, "delete failed: "+err.Error(), http.StatusBadGateway)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handlers) DownloadBackup(w http.ResponseWriter, r *http.Request) {
	serverUUID, err := uuid.Parse(chi.URLParam(r, "uuid"))
	if err != nil {
		http.Error(w, "invalid server uuid", http.StatusBadRequest)
		return
	}
	backupUUID := chi.URLParam(r, "backup_uuid")
	if _, err := uuid.Parse(backupUUID); err != nil {
		http.Error(w, "invalid backup uuid", http.StatusBadRequest)
		return
	}

	f, err := backup.Open(h.BackupDir, serverUUID.String(), backupUUID)
	if err != nil {
		http.Error(w, "backup not found", http.StatusNotFound)
		return
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		http.Error(w, "backup not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/gzip")
	http.ServeContent(w, r, backupUUID+".tar.gz", info.ModTime(), f)
}
