package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/yourorg/panel/internal/auth"
	"github.com/yourorg/panel/internal/models"
)

type UserHandler struct {
	DB *pgxpool.Pool
}

func (h *UserHandler) List(w http.ResponseWriter, r *http.Request) {
	rows, err := h.DB.Query(r.Context(), `
		SELECT id, uuid, email, username, is_admin, totp_enabled, is_active,
		       server_limit, last_login_at, created_at, updated_at
		FROM users ORDER BY created_at`)
	if err != nil {
		http.Error(w, "failed to list users", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	users := make([]models.User, 0)
	for rows.Next() {
		var u models.User
		if err := rows.Scan(
			&u.ID, &u.UUID, &u.Email, &u.Username, &u.IsAdmin, &u.TOTPEnabled, &u.IsActive,
			&u.ServerLimit, &u.LastLoginAt, &u.CreatedAt, &u.UpdatedAt,
		); err != nil {
			http.Error(w, "failed to read users", http.StatusInternalServerError)
			return
		}
		users = append(users, u)
	}

	writeJSON(w, http.StatusOK, users)
}

type createUserRequest struct {
	Email       string `json:"email"`
	Username    string `json:"username"`
	Password    string `json:"password"`
	IsAdmin     bool   `json:"is_admin"`
	ServerLimit *int   `json:"server_limit"`
}

func (h *UserHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req createUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Email == "" || req.Username == "" || len(req.Password) < 8 {
		http.Error(w, "email, username, and a password of at least 8 characters are required", http.StatusBadRequest)
		return
	}

	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		http.Error(w, "failed to hash password", http.StatusInternalServerError)
		return
	}

	var id int64
	err = h.DB.QueryRow(r.Context(), `
		INSERT INTO users (email, username, password_hash, is_admin, is_active, server_limit)
		VALUES ($1, $2, $3, $4, true, $5)
		RETURNING id`,
		req.Email, req.Username, hash, req.IsAdmin, req.ServerLimit,
	).Scan(&id)
	if err != nil {
		http.Error(w, "failed to create user (email or username already in use?)", http.StatusConflict)
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{"id": id})
}

type updateUserRequest struct {
	IsAdmin     bool `json:"is_admin"`
	IsActive    bool `json:"is_active"`
	ServerLimit *int `json:"server_limit"`
}

func (h *UserHandler) Update(w http.ResponseWriter, r *http.Request) {
	claims, ok := auth.FromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid user id", http.StatusBadRequest)
		return
	}

	var req updateUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if id == claims.UserID && (!req.IsAdmin || !req.IsActive) {
		http.Error(w, "cannot remove your own admin status or deactivate yourself", http.StatusBadRequest)
		return
	}

	tag, err := h.DB.Exec(r.Context(),
		`UPDATE users SET is_admin = $1, is_active = $2, server_limit = $3, updated_at = now() WHERE id = $4`,
		req.IsAdmin, req.IsActive, req.ServerLimit, id,
	)
	if err != nil {
		http.Error(w, "failed to update user", http.StatusInternalServerError)
		return
	}
	if tag.RowsAffected() == 0 {
		http.Error(w, "user not found", http.StatusNotFound)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
