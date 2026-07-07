package handlers

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/yourorg/panel/internal/auth"
	"github.com/yourorg/panel/internal/daemonauth"
)

type SFTPAuthHandler struct {
	DB            *pgxpool.Pool
	Subusers      *auth.SubuserChecker
	EncryptionKey string
}

type sftpAuthRequest struct {
	Username    string `json:"username"`
	Fingerprint string `json:"fingerprint"`
}

type sftpAuthResponse struct {
	Allowed    bool   `json:"allowed"`
	ReadOnly   bool   `json:"read_only"`
	ServerUUID string `json:"server_uuid,omitempty"`
	Reason     string `json:"reason,omitempty"`
}

func (h *SFTPAuthHandler) Authenticate(w http.ResponseWriter, r *http.Request) {
	var req sftpAuthRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	usernamePart, uuidShort, ok := strings.Cut(req.Username, ".")
	if !ok {
		writeJSON(w, http.StatusOK, sftpAuthResponse{Allowed: false, Reason: "username must be <account>.<server-id>"})
		return
	}

	var serverID, ownerID, nodeID int64
	var serverUUID string
	if err := h.DB.QueryRow(r.Context(), `
		SELECT s.id, s.owner_id, s.uuid, n.id
		FROM servers s JOIN nodes n ON n.id = s.node_id
		WHERE s.uuid_short = $1`, uuidShort,
	).Scan(&serverID, &ownerID, &serverUUID, &nodeID); err != nil {
		writeJSON(w, http.StatusOK, sftpAuthResponse{Allowed: false, Reason: "server not found"})
		return
	}

	presented := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !daemonauth.VerifyNodeToken(r.Context(), h.DB, h.EncryptionKey, nodeID, presented) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var userID int64
	var isAdmin bool
	if err := h.DB.QueryRow(r.Context(),
		`SELECT id, is_admin FROM users WHERE username = $1 AND is_active = true`, usernamePart,
	).Scan(&userID, &isAdmin); err != nil {
		writeJSON(w, http.StatusOK, sftpAuthResponse{Allowed: false, Reason: "unknown user"})
		return
	}

	var keyExists bool
	if err := h.DB.QueryRow(r.Context(),
		`SELECT EXISTS(SELECT 1 FROM ssh_keys WHERE user_id = $1 AND fingerprint = $2)`, userID, req.Fingerprint,
	).Scan(&keyExists); err != nil || !keyExists {
		writeJSON(w, http.StatusOK, sftpAuthResponse{Allowed: false, Reason: "unrecognized key"})
		return
	}

	claims := &auth.Claims{UserID: userID, IsAdmin: isAdmin}
	if !h.Subusers.CanAccessServer(r.Context(), claims, ownerID, serverID, auth.PermFilesRead) {
		writeJSON(w, http.StatusOK, sftpAuthResponse{Allowed: false, Reason: "no access to this server"})
		return
	}
	readOnly := !h.Subusers.CanAccessServer(r.Context(), claims, ownerID, serverID, auth.PermFilesWrite)

	writeJSON(w, http.StatusOK, sftpAuthResponse{Allowed: true, ReadOnly: readOnly, ServerUUID: serverUUID})
}
